package session

import (
	"context"
	"io"
	"time"
)

type ID string
type ProjectID string
type WorkflowDefinitionID string
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
	StatusCreated         Status = "created"
	StatusQueued          Status = "queued"
	StatusStarting        Status = "starting"
	StatusRunning         Status = "running"
	StatusWaitingUser     Status = "waiting_user"
	StatusWaitingApproval Status = "waiting_approval"
	StatusStopping        Status = "stopping"
	StatusStopped         Status = "stopped"
	StatusResumeFailed    Status = "resume_failed"
	StatusFailed          Status = "failed"
	StatusBlocked         Status = "blocked"
	StatusCompleted       Status = "completed"
	StatusClosed          Status = "closed"
)

type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

type QueueKind string

const (
	QueueKindStart      QueueKind = "start"
	QueueKindResume     QueueKind = "resume"
	QueueKindAnswerUser QueueKind = "answer_user"
)

type QueuePriority string

const (
	QueuePriorityImmediate QueuePriority = "immediate"
	QueuePriorityHigh      QueuePriority = "high"
	QueuePriorityMedium    QueuePriority = "medium"
	QueuePriorityLow       QueuePriority = "low"
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
	Priority       Priority
	CloseReason    *CloseReason
	BaseBranch     string
	WorktreePath   string
	CodexSessionID string
	Config         Config
	QueuedAt       *time.Time
	Queue          QueueIntent
	LastRunAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ClosedAt       *time.Time
}

type QueueIntent struct {
	Kind                 QueueKind
	Priority             QueuePriority
	WorkflowRunID        WorkflowRunID
	NodeRunID            *NodeRunID
	Prompt               string
	ResumeCodexSessionID string
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
	ListQueued(ctx context.Context) ([]Session, error)
	ListInterruptedWithCodexSession(ctx context.Context) ([]Session, error)
	LastConfigForProject(ctx context.Context, projectID ProjectID) (Config, bool, error)
	AppendPrompt(ctx context.Context, append PromptAppend) error
	ListPromptAppends(ctx context.Context, sessionID ID) ([]PromptAppend, error)
	AddMergeRecord(ctx context.Context, record MergeRecord) error
	LatestSuccessfulMergeRecord(ctx context.Context, sessionID ID) (MergeRecord, bool, error)
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
	Create(ctx context.Context, projectPath string, projectID ProjectID, sessionID ID, baseBranch string) (string, error)
	Remove(ctx context.Context, path string) error
	DeleteBranch(ctx context.Context, projectPath string, branch string) error
	PathForSession(projectID ProjectID, sessionID ID) string
}

type WorkflowStartInput struct {
	ProjectID            ProjectID
	SessionID            ID
	WorkflowDefinitionID WorkflowDefinitionID
	Requirement          string
}

type WorkflowStart struct {
	WorkflowRunID    WorkflowRunID
	NodeRunID        *NodeRunID
	CurrentNodeID    string
	CurrentNodeTitle string
	Status           string
	RequiresCodex    bool
	RequireJSONRetry bool
	Prompt           string
	Merge            *WorkflowMerge
	Expr             *WorkflowExpr
}

type WorkflowStartFailureInput struct {
	WorkflowRunID WorkflowRunID
	NodeRunID     *NodeRunID
	Code          string
	Message       string
}

type WorkflowResumeFailureInput struct {
	SessionID ID
	Code      string
	Message   string
}

type WorkflowRerunCurrentNodeInput struct {
	SessionID ID
	Reason    string
}

type WorkflowResumeCurrentNodeInput struct {
	SessionID ID
	Reason    string
}

type WorkflowNodeFailInput struct {
	WorkflowRunID WorkflowRunID
	NodeRunID     NodeRunID
	Code          string
	Message       string
	Output        map[string]any
}

type WorkflowNodeCompleteInput struct {
	WorkflowRunID WorkflowRunID
	NodeRunID     NodeRunID
	Output        map[string]any
}

type WorkflowApprovalInput struct {
	WorkflowRunID WorkflowRunID
	NodeID        string
	Approved      bool
	Comment       string
}

type WorkflowRunSnapshot struct {
	ID            WorkflowRunID
	SessionID     ID
	Status        string
	CurrentNodeID string
	Context       map[string]any
}

type WorkflowApprovalResult struct {
	Run     WorkflowRunSnapshot
	Advance WorkflowAdvance
}

type WorkflowAdvance struct {
	WorkflowRunID    WorkflowRunID
	NodeRunID        *NodeRunID
	CurrentNodeID    string
	CurrentNodeTitle string
	Status           string
	RequiresCodex    bool
	RequireJSONRetry bool
	Prompt           string
	Merge            *WorkflowMerge
	Expr             *WorkflowExpr
	Completed        bool
	Blocked          bool
	BlockedReason    string
}

type WorkflowMerge struct {
	Strategy string
}

type WorkflowExpr struct {
	Script string
	Params map[string]any
}

type WorkflowStarter interface {
	StartForSession(ctx context.Context, input WorkflowStartInput) (WorkflowStart, error)
	MarkStartFailed(ctx context.Context, input WorkflowStartFailureInput) error
	MarkResumeFailedForSession(ctx context.Context, input WorkflowResumeFailureInput) (WorkflowRunSnapshot, error)
	ResumeCurrentNodeForSession(ctx context.Context, input WorkflowResumeCurrentNodeInput) (WorkflowAdvance, error)
	RerunCurrentNodeForSession(ctx context.Context, input WorkflowRerunCurrentNodeInput) (WorkflowAdvance, error)
	CompleteNode(ctx context.Context, input WorkflowNodeCompleteInput) (WorkflowAdvance, error)
	FailNode(ctx context.Context, input WorkflowNodeFailInput) (WorkflowAdvance, error)
	SubmitApprovalForSession(ctx context.Context, input WorkflowApprovalInput) (WorkflowApprovalResult, error)
}
