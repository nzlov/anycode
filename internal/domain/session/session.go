package session

import (
	"context"
	"io"
	"time"
)

type ID string
type ProjectID string
type WorkflowRunID string
type NodeRunID string
type AttachmentID string
type StagedAttachmentID string
type SessionAttachmentID string

type Mode string

const (
	ModeWorkflow Mode = "workflow"
	ModeChat     Mode = "chat"
)

type Status string

const (
	StatusCreated      Status = "created"
	StatusStarting     Status = "starting"
	StatusRunning      Status = "running"
	StatusWaitingUser  Status = "waiting_user"
	StatusStopping     Status = "stopping"
	StatusStopped      Status = "stopped"
	StatusResumeFailed Status = "resume_failed"
	StatusFailed       Status = "failed"
	StatusCompleted    Status = "completed"
	StatusClosed       Status = "closed"
)

type CloseReason string

const (
	CloseReasonUserClosed   CloseReason = "user_closed"
	CloseReasonMergedClosed CloseReason = "merged_closed"
)

type Session struct {
	ID             ID
	ProjectID      ProjectID
	Requirement    string
	Mode           Mode
	Status         Status
	CloseReason    *CloseReason
	BaseBranch     string
	WorktreePath   string
	CodexSessionID string
	Config         Config
	LastRunAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ClosedAt       *time.Time
}

type Config struct {
	CodexModel      string
	ReasoningEffort string
	PermissionMode  string
}

type SessionAttachment struct {
	ID          SessionAttachmentID
	SessionID   ID
	Kind        string
	Filename    string
	Path        string
	MimeType    string
	Size        int64
	Previewable bool
	CreatedAt   time.Time
}

type StagedAttachment struct {
	ID           StagedAttachmentID
	OwnerKeyHash string
	Filename     string
	Path         string
	MimeType     string
	Size         int64
	Previewable  bool
	CreatedAt    time.Time
}

type StageAttachmentInput struct {
	OwnerKeyHash string
	Filename     string
	MimeType     string
	Size         int64
	Reader       io.Reader
}

type AttachmentStream struct {
	Filename string
	MimeType string
	Reader   io.ReadCloser
}

type PromptAppend struct {
	ID        string
	SessionID ID
	Body      string
	CreatedAt time.Time
}

type MergeRecord struct {
	ID             string
	SessionID      ID
	NodeRunID      *NodeRunID
	Strategy       string
	BaseBranch     string
	WorktreeBranch string
	BaseCommit     string
	HeadCommit     string
	MergeCommit    string
	Status         string
	FailureCode    string
	FailureReason  string
	MergedAt       *time.Time
	CreatedAt      time.Time
}

type ListQuery struct {
	ProjectID *ProjectID
	Scope     string
	Range     string
	Page      int
	PageSize  int
	Filter    string
	Sort      string
}

type Repository interface {
	Save(ctx context.Context, session Session) error
	Find(ctx context.Context, id ID) (Session, error)
	ListCards(ctx context.Context, query ListQuery) ([]Session, int, error)
	LastConfigForProject(ctx context.Context, projectID ProjectID) (Config, bool, error)
	AppendPrompt(ctx context.Context, append PromptAppend) error
	ListPromptAppends(ctx context.Context, sessionID ID) ([]PromptAppend, error)
	AddMergeRecord(ctx context.Context, record MergeRecord) error
}

type AttachmentRepository interface {
	SaveStagedAttachment(ctx context.Context, attachment StagedAttachment) error
	FindStagedAttachment(ctx context.Context, id StagedAttachmentID) (StagedAttachment, error)
	DeleteStagedAttachment(ctx context.Context, id StagedAttachmentID) error
	SaveSessionAttachment(ctx context.Context, attachment SessionAttachment) error
	FindSessionAttachment(ctx context.Context, id SessionAttachmentID) (SessionAttachment, error)
	ListSessionAttachments(ctx context.Context, sessionID ID) ([]SessionAttachment, error)
	DeleteSessionAttachment(ctx context.Context, id SessionAttachmentID) error
}

type Policy interface {
	CanStart(session Session) error
	CanStop(session Session) error
	CanResume(session Session) error
	CanClose(session Session) error
}

type ConfigPolicy interface {
	ResolveDefaults(previous *Config, requested Config) Config
}

type AttachmentStore interface {
	Stage(ctx context.Context, input StageAttachmentInput) (StagedAttachment, error)
	Promote(ctx context.Context, staged StagedAttachment, sessionID ID) (SessionAttachment, error)
	DeleteStaged(ctx context.Context, id StagedAttachmentID) error
	DeleteSession(ctx context.Context, id SessionAttachmentID) error
	Open(ctx context.Context, path string) (AttachmentStream, error)
}

type WorktreeManager interface {
	Create(ctx context.Context, projectID ProjectID, sessionID ID, baseBranch string) (string, error)
	Remove(ctx context.Context, path string) error
	PathForSession(projectID ProjectID, sessionID ID) string
}
