package diff

import (
	"context"

	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/gitdiff"
	"github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	GetSessionDiff(ctx context.Context, input SessionDiffInput) (SessionDiffDTO, error)
}

type SessionDiffInput struct {
	SessionID session.ID
	Mode      string
	FilePath  string
	Page      int
	PageSize  int
}

type SessionDiffDTO struct {
	Mode      string
	FilePath  string
	Files     port.Page[gitdiff.DiffFile]
	FileDiff  *gitdiff.FileDiff
	AllDiff   []gitdiff.FileDiff
	Available bool
}
