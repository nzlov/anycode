package terminal

import (
	"context"
	"errors"
)

var (
	ErrRunNotFound = errors.New("terminal run is not active")
	ErrRunActive   = errors.New("terminal run is already active")
)

type SessionID string
type RunID string

type StartInput struct {
	SessionID SessionID
	Workdir   string
	Cols      uint16
	Rows      uint16
}

type ExitResult struct {
	RunID    RunID
	ExitCode *int
	Err      error
}

type Handle struct {
	RunID RunID
	Exit  <-chan ExitResult
}

type OutputSubscription struct {
	Replay []byte
	Output <-chan []byte
	Close  func()
}

type Summary struct {
	CurrentDirectory string
	Commands         []string
}

type Runtime interface {
	Start(ctx context.Context, input StartInput) (Handle, error)
	Write(sessionID SessionID, data []byte) error
	Resize(sessionID SessionID, cols uint16, rows uint16) error
	Stop(ctx context.Context, sessionID SessionID) error
	Subscribe(sessionID SessionID) (OutputSubscription, error)
	Summary(sessionID SessionID) (Summary, error)
	Close() error
}
