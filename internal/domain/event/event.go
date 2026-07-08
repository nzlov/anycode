package event

import (
	"context"
	"time"
)

type ID string
type SessionID string

type Scope struct {
	SessionID *SessionID
	ProjectID string
}

type DomainEvent struct {
	ID        ID
	Scope     Scope
	SessionID *SessionID
	Type      string
	Payload   map[string]any
	CreatedAt time.Time
}

type Store interface {
	Append(ctx context.Context, event DomainEvent) error
	After(ctx context.Context, scope Scope, after ID) ([]DomainEvent, error)
	Before(ctx context.Context, scope Scope, before ID, limit int) ([]DomainEvent, int, bool, error)
}

type Publisher interface {
	PublishAfterCommit(ctx context.Context, event DomainEvent) error
}
