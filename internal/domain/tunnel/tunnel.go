package tunnel

import (
	"context"
	"time"
)

type ID string
type SessionID string

type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
)

type Tunnel struct {
	ID        ID
	SessionID SessionID
	Name      string
	Port      int
	Hostname  string
	URL       string
	AccessURL string
	Status    Status
	CreatedAt time.Time
}

type StartInput struct {
	Tunnel Tunnel
	Auth   string
}

type Runtime interface {
	Start(ctx context.Context, input StartInput) (Tunnel, error)
	List(ctx context.Context) ([]Tunnel, error)
	Close(ctx context.Context, id ID) error
	CloseSession(ctx context.Context, sessionID SessionID) error
	CloseAll(ctx context.Context) error
}
