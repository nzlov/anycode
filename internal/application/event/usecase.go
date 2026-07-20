package event

import (
	"context"
	"errors"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
)

const subscriptionMailboxSize = 16

type UseCase interface {
	LiveSessionEvents(ctx context.Context, input LiveSessionEventsInput) (<-chan DTO, error)
	LiveCodexEvents(ctx context.Context, sessionID processdomain.SessionID) (<-chan processdomain.CodexEvent, error)
}

type LiveSessionEventsInput struct {
	Scope domain.Scope
}

type DTO struct {
	ID        domain.ID
	Scope     domain.Scope
	SessionID *domain.SessionID
	Type      string
	Payload   map[string]any
	Causality domain.Causality
	CreatedAt string
}

type Service struct {
	hub      *hub
	observer Observer
}

type Observation struct {
	Name    string
	Outcome string
}

type Observer interface {
	Observe(Observation)
}

type Option func(*Service)

func WithObserver(observer Observer) Option {
	return func(service *Service) { service.observer = observer }
}

func New(options ...Option) *Service {
	service := &Service{}
	for _, option := range options {
		option(service)
	}
	service.hub = newHub()
	go service.hub.run()
	return service
}

func (s *Service) LiveCodexEvents(ctx context.Context, sessionID processdomain.SessionID) (<-chan processdomain.CodexEvent, error) {
	if s == nil || s.hub == nil {
		return nil, errors.New("event usecase: nil service")
	}
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	client, out := newHubClient(ctx, "codex_subscription", hubRoute{
		kind: liveEventCodex, scope: hubRouteSession, id: string(sessionID),
	}, func(event liveEvent) (processdomain.CodexEvent, bool) {
		return event.codexEvent, event.kind == liveEventCodex && event.codexEvent.SessionID == sessionID
	})
	if err := s.register(ctx, client); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) PublishCodexEvent(_ context.Context, event processdomain.CodexEvent) error {
	if s == nil || s.hub == nil {
		return errors.New("event usecase: nil service")
	}
	s.observeAll(s.hub.publish(liveEvent{kind: liveEventCodex, codexEvent: event}))
	return nil
}

func (s *Service) LiveSessionEvents(ctx context.Context, input LiveSessionEventsInput) (<-chan DTO, error) {
	if s == nil || s.hub == nil {
		return nil, errors.New("event usecase: nil service")
	}
	client, out := newHubClient(ctx, "subscription", domainSubscriptionRoute(input.Scope), func(event liveEvent) (DTO, bool) {
		return event.domainEvent, event.kind == liveEventDomain && scopeMatches(input.Scope, event.domainEvent.Scope)
	})
	if err := s.register(ctx, client); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) PublishAfterCommit(_ context.Context, event domain.DomainEvent) error {
	if s == nil || s.hub == nil {
		return errors.New("event usecase: nil service")
	}
	s.observeAll(s.hub.publish(liveEvent{kind: liveEventDomain, domainEvent: toDTO(event)}))
	return nil
}

func (s *Service) register(ctx context.Context, client *hubClient) error {
	if err := s.hub.registerClient(ctx, client); err != nil {
		client.close()
		return err
	}
	s.observe(Observation{Name: client.observationName + ".lifecycle", Outcome: "opened"})
	go func() {
		<-ctx.Done()
		if s.hub.unregisterClient(client) {
			s.observe(Observation{Name: client.observationName + ".lifecycle", Outcome: "closed"})
		}
	}()
	return nil
}

func (s *Service) observe(observation Observation) {
	if s.observer != nil {
		s.observer.Observe(observation)
	}
}

func (s *Service) observeAll(observations []Observation) {
	for _, observation := range observations {
		s.observe(observation)
	}
}

type liveEventKind uint8

const (
	liveEventUnknown liveEventKind = iota
	liveEventDomain
	liveEventCodex
)

// GLUE: the hub serializes two typed publisher contracts internally. The value
// stays private so transcript and session-change consumers keep their own types.
type liveEvent struct {
	kind        liveEventKind
	domainEvent DTO
	codexEvent  processdomain.CodexEvent
}

type hub struct {
	clients    map[hubRoute]map[*hubClient]struct{}
	register   chan *hubClient
	unregister chan hubUnregister
	broadcast  chan hubBroadcast
}

type hubBroadcast struct {
	event        liveEvent
	observations chan []Observation
}

type hubUnregister struct {
	client  *hubClient
	removed chan bool
}

type hubClient struct {
	observationName string
	route           hubRoute
	deliver         func(liveEvent) deliveryResult
	close           func()
}

func newHub() *hub {
	return &hub{
		clients:    map[hubRoute]map[*hubClient]struct{}{},
		register:   make(chan *hubClient),
		unregister: make(chan hubUnregister),
		broadcast:  make(chan hubBroadcast),
	}
}

func newHubClient[T any](ctx context.Context, observationName string, route hubRoute, selectEvent func(liveEvent) (T, bool)) (*hubClient, <-chan T) {
	send := make(chan T, subscriptionMailboxSize)
	client := &hubClient{observationName: observationName, route: route}
	client.deliver = func(event liveEvent) deliveryResult {
		value, ok := selectEvent(event)
		if !ok {
			return deliveryIgnored
		}
		select {
		case <-ctx.Done():
			return deliveryUnavailable
		default:
		}
		select {
		case <-ctx.Done():
			return deliveryUnavailable
		case send <- value:
			return deliverySent
		default:
			return deliveryMailboxFull
		}
	}
	client.close = func() { close(send) }
	return client, send
}

func (h *hub) run() {
	for {
		select {
		case client := <-h.register:
			if h.clients[client.route] == nil {
				h.clients[client.route] = map[*hubClient]struct{}{}
			}
			h.clients[client.route][client] = struct{}{}
		case request := <-h.unregister:
			request.removed <- h.remove(request.client)
		case request := <-h.broadcast:
			request.observations <- h.deliver(request.event)
		}
	}
}

func (h *hub) registerClient(ctx context.Context, client *hubClient) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case h.register <- client:
		return nil
	}
}

func (h *hub) unregisterClient(client *hubClient) bool {
	removed := make(chan bool)
	h.unregister <- hubUnregister{client: client, removed: removed}
	return <-removed
}

func (h *hub) publish(event liveEvent) []Observation {
	observations := make(chan []Observation)
	h.broadcast <- hubBroadcast{event: event, observations: observations}
	return <-observations
}

func (h *hub) remove(client *hubClient) bool {
	bucket := h.clients[client.route]
	if _, ok := bucket[client]; !ok {
		return false
	}
	delete(bucket, client)
	if len(bucket) == 0 {
		delete(h.clients, client.route)
	}
	client.close()
	return true
}

func (h *hub) deliver(event liveEvent) []Observation {
	var observations []Observation
	switch event.kind {
	case liveEventCodex:
		return h.deliverRoute(hubRoute{kind: liveEventCodex, scope: hubRouteSession, id: string(event.codexEvent.SessionID)}, event, observations)
	case liveEventDomain:
		observations = h.deliverRoute(hubRoute{kind: liveEventDomain, scope: hubRouteGlobal}, event, observations)
		if event.domainEvent.Scope.ProjectID != "" {
			observations = h.deliverRoute(hubRoute{
				kind: liveEventDomain, scope: hubRouteProject, id: event.domainEvent.Scope.ProjectID,
			}, event, observations)
		}
		if event.domainEvent.Scope.SessionID != nil {
			observations = h.deliverRoute(hubRoute{
				kind: liveEventDomain, scope: hubRouteSession, id: string(*event.domainEvent.Scope.SessionID),
			}, event, observations)
		}
	}
	return observations
}

func (h *hub) deliverRoute(route hubRoute, event liveEvent, observations []Observation) []Observation {
	for client := range h.clients[route] {
		switch client.deliver(event) {
		case deliveryMailboxFull:
			observations = append(observations, Observation{Name: client.observationName + ".delivery", Outcome: "overflow"})
			if h.remove(client) {
				observations = append(observations, Observation{Name: client.observationName + ".lifecycle", Outcome: "closed"})
			}
		case deliveryUnavailable:
			if h.remove(client) {
				observations = append(observations, Observation{Name: client.observationName + ".lifecycle", Outcome: "closed"})
			}
		}
	}
	return observations
}

type hubRouteScope uint8

const (
	hubRouteGlobal hubRouteScope = iota
	hubRouteProject
	hubRouteSession
)

type hubRoute struct {
	kind  liveEventKind
	scope hubRouteScope
	id    string
}

func domainSubscriptionRoute(scope domain.Scope) hubRoute {
	if scope.SessionID != nil {
		return hubRoute{kind: liveEventDomain, scope: hubRouteSession, id: string(*scope.SessionID)}
	}
	if scope.ProjectID != "" {
		return hubRoute{kind: liveEventDomain, scope: hubRouteProject, id: scope.ProjectID}
	}
	return hubRoute{kind: liveEventDomain, scope: hubRouteGlobal}
}

type deliveryResult uint8

const (
	deliveryIgnored deliveryResult = iota
	deliverySent
	deliveryUnavailable
	deliveryMailboxFull
)

func scopeMatches(subscription domain.Scope, event domain.Scope) bool {
	if subscription.SessionID != nil {
		return event.SessionID != nil && *subscription.SessionID == *event.SessionID
	}
	if subscription.ProjectID != "" {
		return subscription.ProjectID == event.ProjectID
	}
	return true
}

func toDTO(event domain.DomainEvent) DTO {
	return DTO{
		ID:        event.ID,
		Scope:     event.Scope,
		SessionID: event.SessionID,
		Type:      event.Type,
		Payload:   payloadOrEmpty(event.Payload),
		Causality: event.Causality,
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}
