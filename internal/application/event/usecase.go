package event

import (
	"context"

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
