package event

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	domain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	ListSessionEvents(ctx context.Context, input ListSessionEventsInput) (port.Page[DTO], error)
	SessionEvents(ctx context.Context, input SessionEventsInput) (<-chan DTO, error)
}

type ListSessionEventsInput struct {
	SessionID    session.ID
	AfterEventID domain.ID
	Page         int
	PageSize     int
}

type SessionEventsInput struct {
	Scope        domain.Scope
	AfterEventID domain.ID
}

type DTO struct {
	ID        domain.ID
	Scope     domain.Scope
	SessionID *domain.SessionID
	Type      string
	Payload   map[string]any
	CreatedAt string
}

const (
	defaultPage     = 1
	defaultPageSize = 50
	maxPageSize     = 200
)

type Service struct {
	store       domain.Store
	mu          sync.Mutex
	nextSubID   int64
	subscribers map[int64]subscription
}

func New(store domain.Store) *Service {
	return &Service{store: store, subscribers: map[int64]subscription{}}
}

func (s *Service) ListSessionEvents(ctx context.Context, input ListSessionEventsInput) (port.Page[DTO], error) {
	if s == nil {
		return port.Page[DTO]{}, errors.New("event usecase: nil service")
	}
	if s.store == nil {
		return port.Page[DTO]{}, errors.New("event store is required")
	}
	if input.SessionID == "" {
		return port.Page[DTO]{}, errors.New("session id is required")
	}
	page, pageSize := normalizePage(input.Page, input.PageSize)
	sessionID := domain.SessionID(input.SessionID)
	events, err := s.store.After(ctx, domain.Scope{SessionID: &sessionID}, input.AfterEventID)
	if err != nil {
		return port.Page[DTO]{}, fmt.Errorf("list session events: %w", err)
	}
	start := (page - 1) * pageSize
	if start > len(events) {
		start = len(events)
	}
	end := start + pageSize
	if end > len(events) {
		end = len(events)
	}
	items := toDTOs(events[start:end])
	nextCursor := ""
	if end < len(events) && len(items) > 0 {
		nextCursor = string(items[len(items)-1].ID)
	}
	return port.Page[DTO]{
		Items:      items,
		Page:       page,
		PageSize:   pageSize,
		Total:      len(events),
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) SessionEvents(ctx context.Context, input SessionEventsInput) (<-chan DTO, error) {
	if s == nil {
		return nil, errors.New("event usecase: nil service")
	}
	if s.store == nil {
		return nil, errors.New("event store is required")
	}
	events, err := s.store.After(ctx, input.Scope, input.AfterEventID)
	if err != nil {
		return nil, fmt.Errorf("list session events: %w", err)
	}
	ch := make(chan DTO, len(events)+16)
	for _, event := range events {
		ch <- toDTO(event)
	}
	id := s.subscribe(input.Scope, ch)
	go func() {
		<-ctx.Done()
		s.unsubscribe(id)
		close(ch)
	}()
	return ch, nil
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
		default:
		}
	}
	return nil
}

type subscription struct {
	scope domain.Scope
	ch    chan DTO
}

func (s *Service) subscribe(scope domain.Scope, ch chan DTO) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSubID++
	id := s.nextSubID
	s.subscribers[id] = subscription{scope: scope, ch: ch}
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

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = defaultPage
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func toDTOs(events []domain.DomainEvent) []DTO {
	items := make([]DTO, 0, len(events))
	for _, event := range events {
		items = append(items, toDTO(event))
	}
	return items
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
