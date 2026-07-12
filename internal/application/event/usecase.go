package event

import (
	"context"
	"errors"
	"sync"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/event"
)

type UseCase interface {
	LiveSessionEvents(ctx context.Context, input LiveSessionEventsInput) (<-chan DTO, error)
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
	CreatedAt string
}

type Service struct {
	mu          sync.Mutex
	nextSubID   int64
	subscribers map[string]map[int64]*subscription
}

func New() *Service {
	return &Service{subscribers: map[string]map[int64]*subscription{}}
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
		if !sub.trySend(dto) {
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

func (s *Service) subscribe(scope domain.Scope, ch chan DTO, done <-chan struct{}) *subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSubID++
	key := subscriptionKey(scope)
	sub := &subscription{id: s.nextSubID, key: key, ch: ch, done: done}
	if s.subscribers[key] == nil {
		s.subscribers[key] = map[int64]*subscription{}
	}
	s.subscribers[key][sub.id] = sub
	return sub
}

func (s *Service) removeSubscription(sub *subscription) {
	s.mu.Lock()
	bucket := s.subscribers[sub.key]
	if bucket[sub.id] == sub {
		delete(bucket, sub.id)
		if len(bucket) == 0 {
			delete(s.subscribers, sub.key)
		}
	}
	s.mu.Unlock()
	sub.close()
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

func (s *subscription) trySend(dto DTO) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case <-s.done:
		return false
	case s.ch <- dto:
		return true
	default:
		return false
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
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}
