package port

import (
	"context"

	"github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/domain/question"
	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/domain/workflow"
)

type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
}

type ExecutionClaimStatus string

const (
	ExecutionClaimed       ExecutionClaimStatus = "claimed"
	ExecutionAlreadyActive ExecutionClaimStatus = "already_active"
	ExecutionStale         ExecutionClaimStatus = "stale_queue"
	ExecutionAtCapacity    ExecutionClaimStatus = "at_capacity"
)

type ExecutionClaimInput struct {
	ExpectedSession session.Session
	StartingSession session.Session
	Run             process.Run
	MaxActive       int
}

type ExecutionClaimResult struct {
	Status    ExecutionClaimStatus
	Session   session.Session
	ActiveRun *process.Run
}

type ClosePreparationStatus string

const (
	ClosePrepared      ClosePreparationStatus = "prepared"
	CloseAlreadyClosed ClosePreparationStatus = "already_closed"
	CloseActive        ClosePreparationStatus = "active_execution"
	CloseStale         ClosePreparationStatus = "stale_session"
)

type ClosePreparationInput struct {
	ExpectedSession session.Session
	ClosingSession  session.Session
}

type ClosePreparationResult struct {
	Status    ClosePreparationStatus
	Session   session.Session
	ActiveRun *process.Run
}

type Tx interface {
	ClaimExecution(ctx context.Context, input ExecutionClaimInput) (ExecutionClaimResult, error)
	PrepareClose(ctx context.Context, input ClosePreparationInput) (ClosePreparationResult, error)
	Projects() project.Repository
	Sessions() session.Repository
	Workflows() workflow.Repository
	Questions() question.Repository
	Processes() process.Repository
	Events() event.Store
}

type SessionLocker interface {
	WithSessionLock(ctx context.Context, id session.ID, fn func(context.Context) error) error
}
