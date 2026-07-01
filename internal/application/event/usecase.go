package event

import (
	"context"
	"errors"
	"fmt"
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
	store domain.Store
}

func New(store domain.Store) *Service {
	return &Service{store: store}
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
	ch := make(chan DTO, len(events))
	for _, event := range events {
		ch <- toDTO(event)
	}
	close(ch)
	return ch, nil
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
		Payload:   event.Payload,
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
