package tunnel

import (
	"context"
	"time"
)

type ID string
type SessionID string

type Mode string

const (
	ModeCF    Mode = "cf"
	ModeLocal Mode = "local"
)

func (m Mode) Valid() bool { return m == ModeCF || m == ModeLocal }

type ConfigurationRepository interface {
	TunnelMode(context.Context) (Mode, error)
	SetTunnelMode(context.Context, Mode) error
}

type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
)

type Tunnel struct {
	Mode      Mode
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
	SwitchMode(context.Context, ID, Mode) (Tunnel, error)
	Start(ctx context.Context, input StartInput) (Tunnel, error)
	List(ctx context.Context) ([]Tunnel, error)
	Close(ctx context.Context, id ID) error
	CloseSession(ctx context.Context, sessionID SessionID) error
	CloseAll(ctx context.Context) error
}
