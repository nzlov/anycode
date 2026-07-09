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
	subscribers map[int64]subscription
}

func New() *Service {
	return &Service{subscribers: map[int64]subscription{}}
}

func (s *Service) LiveSessionEvents(ctx context.Context, input LiveSessionEventsInput) (<-chan DTO, error) {
	if s == nil {
		return nil, errors.New("event usecase: nil service")
	}
	out := make(chan DTO, 16)
	id := s.subscribe(input.Scope, out, ctx.Done())
	go func() {
		<-ctx.Done()
		s.unsubscribe(id)
	}()
	return out, nil
}

func (s *Service) PublishAfterCommit(ctx context.Context, event domain.DomainEvent) error {
	if s == nil {
		return errors.New("event usecase: nil service")
	}
	dto := toDTO(event)
	for _, sub := range s.matchingSubscribers(event.Scope) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case sub.ch <- dto:
		case <-sub.done:
		}
	}
	return nil
}

type subscription struct {
	scope domain.Scope
	ch    chan DTO
	done  <-chan struct{}
}

func (s *Service) subscribe(scope domain.Scope, ch chan DTO, done <-chan struct{}) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSubID++
	id := s.nextSubID
	s.subscribers[id] = subscription{scope: scope, ch: ch, done: done}
	return id
}

func (s *Service) unsubscribe(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscribers, id)
}

func (s *Service) matchingSubscribers(scope domain.Scope) []subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	subscribers := make([]subscription, 0, len(s.subscribers))
	for _, sub := range s.subscribers {
		if scopeMatches(sub.scope, scope) {
			subscribers = append(subscribers, sub)
		}
	}
	return subscribers
}

func scopeMatches(filter domain.Scope, scope domain.Scope) bool {
	if filter.ProjectID != "" && filter.ProjectID != scope.ProjectID {
		return false
	}
	if filter.SessionID != nil {
		if scope.SessionID == nil || *filter.SessionID != *scope.SessionID {
			return false
		}
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
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}
