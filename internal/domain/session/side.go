package session

import (
	"context"
	"time"
)

type SideStatus string

const (
	SideStatusRunning   SideStatus = "running"
	SideStatusCompleted SideStatus = "completed"
	SideStatusFailed    SideStatus = "failed"
)

type Side struct {
	ID           string
	SessionID    ID
	ProcessRunID string
	TurnID       string
	Prompt       string
	FollowUps    []string
	Status       SideStatus
	Error        string
	TurnIndex    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SideEvent struct {
	ID            string
	SideID        string
	SessionID     ID
	ProcessRunID  string
	EventID       string
	Type          string
	CorrelationID string
	TurnID        string
	Phase         string
	ContentKind   string
	Content       map[string]any
	TurnIndex     int
	Sequence      int64
	CreatedAt     time.Time
}

type SideRepository interface {
	CreateSide(ctx context.Context, side Side) error
	FindSide(ctx context.Context, id string) (Side, error)
	FindSideByProcessRun(ctx context.Context, processRunID string) (Side, error)
	ListSides(ctx context.Context, sessionID ID) ([]Side, error)
	ListRunningSides(ctx context.Context) ([]Side, error)
	BeginSideTurn(ctx context.Context, id string, processRunID string, turnID string, followUp string, now time.Time) (Side, error)
	CompleteSideTurn(ctx context.Context, id string, processRunID string, status SideStatus, failure string, now time.Time) error
	AppendSideEvent(ctx context.Context, event SideEvent) error
	ListSideEvents(ctx context.Context, sideID string) ([]SideEvent, error)
	ListSideRunEvents(ctx context.Context, processRunID string) ([]SideEvent, error)
	DeleteSide(ctx context.Context, id string) error
	DeleteSidesBySession(ctx context.Context, sessionID ID) error
}
