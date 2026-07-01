package project

import (
	"context"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/project"
)

type UseCase interface {
	CreateProject(ctx context.Context, input CreateProjectInput) (DTO, error)
	BrowseDirectory(ctx context.Context, input BrowseDirectoryInput) (DirectoryPageDTO, error)
	SetDefaultWorkflow(ctx context.Context, input SetDefaultWorkflowInput) (DTO, error)
	ListProjects(ctx context.Context) ([]DTO, error)
}

type CreateProjectInput struct {
	Path string
	Name string
}

type BrowseDirectoryInput struct {
	Path string
}

type SetDefaultWorkflowInput struct {
	ProjectID  domain.ID
	WorkflowID domain.WorkflowDefinitionID
}

type DTO struct {
	ID                domain.ID
	Name              string
	Path              string
	IsGit             bool
	DefaultWorkflowID *domain.WorkflowDefinitionID
	GitState          domain.GitState
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type DirectoryPageDTO struct {
	Path    string
	Parent  string
	Entries []domain.DirectoryEntry
}
