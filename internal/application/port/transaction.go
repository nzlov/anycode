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

type Tx interface {
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
