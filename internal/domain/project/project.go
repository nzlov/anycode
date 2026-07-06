package project

import (
	"context"
	"time"
)

type ID string
type WorkflowDefinitionID string

type Project struct {
	ID                ID
	Name              string
	Path              ProjectPath
	IsGit             bool
	DefaultWorkflowID *WorkflowDefinitionID
	RemovedAt         *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ProjectPath struct {
	Value string
}

type GitState struct {
	IsRepository  bool
	CurrentBranch string
	Branches      []GitBranch
	ErrorCode     string
	ErrorMessage  string
}

type GitBranch struct {
	Name      string
	IsCurrent bool
}

type DirectoryListing struct {
	Path    string
	Parent  string
	Entries []DirectoryEntry
}

type DirectoryEntry struct {
	Name      string
	Path      string
	IsDir     bool
	IsGit     bool
	CanRead   bool
	ErrorCode string
}

type Repository interface {
	Save(ctx context.Context, project Project) error
	Find(ctx context.Context, id ID) (Project, error)
	FindByPath(ctx context.Context, path string) (Project, bool, error)
	List(ctx context.Context) ([]Project, error)
	Remove(ctx context.Context, id ID, removedAt time.Time) error
	UpdateDefaultWorkflow(ctx context.Context, id ID, workflowID WorkflowDefinitionID) error
}

type DirectoryBrowser interface {
	List(ctx context.Context, path string) (DirectoryListing, error)
}

type GitInspector interface {
	Detect(ctx context.Context, path string) (GitState, error)
	Branches(ctx context.Context, path string) ([]GitBranch, error)
	HeadCommit(ctx context.Context, path string, branch string) (string, error)
}
