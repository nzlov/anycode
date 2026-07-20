package event

import (
	"context"
	"errors"
	"sync"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
)

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
	mu               sync.Mutex
	nextSubID        int64
	subscribers      map[string]map[int64]*subscription
	codexSubscribers map[processdomain.SessionID]map[int64]*codexSubscription
	observer         Observer
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
	service := &Service{
		subscribers:      map[string]map[int64]*subscription{},
		codexSubscribers: map[processdomain.SessionID]map[int64]*codexSubscription{},
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) LiveCodexEvents(ctx context.Context, sessionID processdomain.SessionID) (<-chan processdomain.CodexEvent, error) {
	if s == nil {
		return nil, errors.New("event usecase: nil service")
	}
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	out := make(chan processdomain.CodexEvent, 16)
	sub := s.subscribeCodex(sessionID, out, ctx.Done())
	go func() {
		<-ctx.Done()
		s.removeCodexSubscription(sub)
	}()
	return out, nil
}

func (s *Service) PublishCodexEvent(_ context.Context, event processdomain.CodexEvent) error {
	if s == nil {
		return errors.New("event usecase: nil service")
	}
	s.mu.Lock()
	bucket := s.codexSubscribers[event.SessionID]
	subscribers := make([]*codexSubscription, 0, len(bucket))
	for _, sub := range bucket {
		subscribers = append(subscribers, sub)
	}
	s.mu.Unlock()
	for _, sub := range subscribers {
		switch sub.trySend(event) {
		case deliveryMailboxFull:
			s.observe(Observation{Name: "codex_subscription.delivery", Outcome: "overflow"})
			s.removeCodexSubscription(sub)
		case deliveryUnavailable:
			s.removeCodexSubscription(sub)
		}
	}
	return nil
}

func (s *Service) LiveSessionEvents(ctx context.Context, input LiveSessionEventsInput) (<-chan DTO, error) {
	if s == nil {
		return nil, errors.New("event usecase: nil service")
	}
	out := make(chan DTO, 16)
	sub := s.subscribe(input.Scope, out, ctx.Done())
	go func() {
		<-ctx.Done()
		s.removeSubscription(sub)
	}()
	return out, nil
}

func (s *Service) PublishAfterCommit(_ context.Context, event domain.DomainEvent) error {
	if s == nil {
		return errors.New("event usecase: nil service")
	}
	dto := toDTO(event)
	for _, sub := range s.matchingSubscribers(event.Scope) {
		switch sub.trySend(dto) {
		case deliveryMailboxFull:
			s.observe(Observation{Name: "subscription.delivery", Outcome: "overflow"})
			s.removeSubscription(sub)
		case deliveryUnavailable:
			s.removeSubscription(sub)
		}
	}
	return nil
}

type subscription struct {
	id     int64
	key    string
	ch     chan DTO
	done   <-chan struct{}
	mu     sync.Mutex
	closed bool
}

type codexSubscription struct {
	id        int64
	sessionID processdomain.SessionID
	ch        chan processdomain.CodexEvent
	done      <-chan struct{}
	mu        sync.Mutex
	closed    bool
}

type deliveryResult uint8

const (
	deliverySent deliveryResult = iota
	deliveryUnavailable
	deliveryMailboxFull
)

func (s *Service) subscribe(scope domain.Scope, ch chan DTO, done <-chan struct{}) *subscription {
	s.mu.Lock()
	s.nextSubID++
	key := subscriptionKey(scope)
	sub := &subscription{id: s.nextSubID, key: key, ch: ch, done: done}
	if s.subscribers[key] == nil {
		s.subscribers[key] = map[int64]*subscription{}
	}
	s.subscribers[key][sub.id] = sub
	s.mu.Unlock()
	s.observe(Observation{Name: "subscription.lifecycle", Outcome: "opened"})
	return sub
}

func (s *Service) removeSubscription(sub *subscription) {
	s.mu.Lock()
	bucket := s.subscribers[sub.key]
	removed := false
	if bucket[sub.id] == sub {
		delete(bucket, sub.id)
		removed = true
		if len(bucket) == 0 {
			delete(s.subscribers, sub.key)
		}
	}
	s.mu.Unlock()
	if !removed {
		return
	}
	sub.close()
	s.observe(Observation{Name: "subscription.lifecycle", Outcome: "closed"})
}

func (s *Service) subscribeCodex(sessionID processdomain.SessionID, ch chan processdomain.CodexEvent, done <-chan struct{}) *codexSubscription {
	s.mu.Lock()
	s.nextSubID++
	sub := &codexSubscription{id: s.nextSubID, sessionID: sessionID, ch: ch, done: done}
	if s.codexSubscribers[sessionID] == nil {
		s.codexSubscribers[sessionID] = map[int64]*codexSubscription{}
	}
	s.codexSubscribers[sessionID][sub.id] = sub
	s.mu.Unlock()
	s.observe(Observation{Name: "codex_subscription.lifecycle", Outcome: "opened"})
	return sub
}

func (s *Service) removeCodexSubscription(sub *codexSubscription) {
	s.mu.Lock()
	removed := false
	bucket := s.codexSubscribers[sub.sessionID]
	if bucket[sub.id] == sub {
		delete(bucket, sub.id)
		removed = true
		if len(bucket) == 0 {
			delete(s.codexSubscribers, sub.sessionID)
		}
	}
	s.mu.Unlock()
	if !removed {
		return
	}
	sub.close()
	s.observe(Observation{Name: "codex_subscription.lifecycle", Outcome: "closed"})
}

func (s *Service) observe(observation Observation) {
	if s.observer != nil {
		s.observer.Observe(observation)
	}
}

func (s *Service) matchingSubscribers(scope domain.Scope) []*subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := eventKeys(scope)
	count := 0
	for _, key := range keys {
		count += len(s.subscribers[key])
	}
	subscribers := make([]*subscription, 0, count)
	for _, key := range keys {
		for _, sub := range s.subscribers[key] {
			subscribers = append(subscribers, sub)
		}
	}
	return subscribers
}

func (s *subscription) trySend(dto DTO) deliveryResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return deliveryUnavailable
	}
	select {
	case <-s.done:
		return deliveryUnavailable
	default:
	}
	select {
	case <-s.done:
		return deliveryUnavailable
	case s.ch <- dto:
		return deliverySent
	default:
		return deliveryMailboxFull
	}
}

func (s *subscription) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (s *codexSubscription) trySend(event processdomain.CodexEvent) deliveryResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return deliveryUnavailable
	}
	select {
	case <-s.done:
		return deliveryUnavailable
	default:
	}
	select {
	case <-s.done:
		return deliveryUnavailable
	case s.ch <- event:
		return deliverySent
	default:
		return deliveryMailboxFull
	}
}

func (s *codexSubscription) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

const globalSubscriptionKey = "global"

func subscriptionKey(scope domain.Scope) string {
	if scope.SessionID != nil {
		return "session:" + string(*scope.SessionID)
	}
	if scope.ProjectID != "" {
		return "project:" + scope.ProjectID
	}
	return globalSubscriptionKey
}

func eventKeys(scope domain.Scope) []string {
	keys := []string{globalSubscriptionKey}
	if scope.ProjectID != "" {
		keys = append(keys, "project:"+scope.ProjectID)
	}
	if scope.SessionID != nil {
		keys = append(keys, "session:"+string(*scope.SessionID))
	}
	return keys
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
