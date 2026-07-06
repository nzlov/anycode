package process

import (
	"context"
	"time"
)

type RunID string
type SessionID string
type NodeRunID string

type Status string

const (
	StatusStarting     Status = "starting"
	StatusRunning      Status = "running"
	StatusWaitingUser  Status = "waiting_user"
	StatusStopping     Status = "stopping"
	StatusExited       Status = "exited"
	StatusFailed       Status = "failed"
	StatusResumeFailed Status = "resume_failed"
	StatusCanceled     Status = "canceled"
)

type Run struct {
	ID             RunID
	SessionID      SessionID
	NodeRunID      *NodeRunID
	Status         Status
	PID            *int
	CodexSessionID string
	ResumeOf       *RunID
	ExitCode       *int
	FailureReason  string
	StartedAt      time.Time
	FinishedAt     *time.Time
}

type Event struct {
	ID           string
	SessionID    SessionID
	ProcessRunID *RunID
	EventID      string
	Type         string
	Payload      map[string]any
	CreatedAt    time.Time
}

type CodexEvent struct {
	EventID string
	Type    string
	Payload map[string]any
	Raw     []byte
}

type ExitResult struct {
	ExitCode      *int
	FailureReason string
	FinishedAt    time.Time
}

type Repository interface {
	CreateRun(ctx context.Context, run Run) error
	FindActiveBySession(ctx context.Context, sessionID SessionID) (Run, bool, error)
	CountActive(ctx context.Context) (int, error)
	MarkWaitingUser(ctx context.Context, id RunID) error
	MarkRunning(ctx context.Context, id RunID, pid int, codexSessionID string) error
	MarkExited(ctx context.Context, id RunID, result ExitResult) error
	SaveEvent(ctx context.Context, event Event) error
}

type CodexStartInput struct {
	ProcessRunID    RunID
	SessionID       SessionID
	Workdir         string
	Prompt          string
	Model           string
	ReasoningEffort string
	PermissionMode  string
	AttachmentPaths []string
	ImagePaths      []string
}

type CodexResumeInput struct {
	ProcessRunID   RunID
	SessionID      SessionID
	CodexSessionID string
	Workdir        string
}

type CodexHandle struct {
	ProcessRunID   RunID
	PID            int
	CodexSessionID string
}

type CodexCapabilities struct {
	Version        string
	SupportsExec   bool
	SupportsResume bool
}

type CodexProcess interface {
	Probe(ctx context.Context) (CodexCapabilities, error)
	Start(ctx context.Context, input CodexStartInput) (CodexHandle, error)
	Resume(ctx context.Context, input CodexResumeInput) (CodexHandle, error)
	Stop(ctx context.Context, processRunID RunID) error
	Events(ctx context.Context, handle CodexHandle) (<-chan CodexEvent, error)
}
