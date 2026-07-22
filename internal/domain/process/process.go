package process

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProcessNotFound            = errors.New("codex process run is not active")
	ErrProcessOwnershipUnverified = errors.New("codex process ownership could not be verified")
	ErrTranscriptUnavailable      = errors.New("codex transcript is unavailable")
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

type CodexTranscriptSource struct {
	CodexSessionID string
	RelativePath   string
	BoundAt        time.Time
}

type CodexEvent struct {
	EventID        string
	Type           CodexEventType
	SessionID      SessionID
	ProcessRunID   RunID
	CodexSessionID string
	CorrelationID  string
	Phase          CodexPhase
	Content        CodexEventContent
	SourceOffset   int64
	SourceIndex    int
	CreatedAt      time.Time
}

type CodexEventType string

const (
	CodexEventTranscriptBound CodexEventType = "transcript.bound"
	CodexEventMessage         CodexEventType = "message"
	CodexEventReasoning       CodexEventType = "reasoning"
	CodexEventCommand         CodexEventType = "command"
	CodexEventTool            CodexEventType = "tool"
	CodexEventFileChange      CodexEventType = "file_change"
	CodexEventPlan            CodexEventType = "plan"
	CodexEventUsage           CodexEventType = "usage"
	CodexEventStatus          CodexEventType = "status"
	CodexEventProcessExit     CodexEventType = "process.exit"
	CodexEventUnknown         CodexEventType = "unknown"
)

type PlanItemStatus string

const (
	PlanItemPending    PlanItemStatus = "pending"
	PlanItemInProgress PlanItemStatus = "in_progress"
	PlanItemCompleted  PlanItemStatus = "completed"
)

type PlanUpdate struct {
	EventID string
	Items   []PlanItem
}

type PlanItem struct {
	Step   string
	Status PlanItemStatus
}

func CanonicalCodexEventID(codexSessionID string, eventID string) string {
	if codexSessionID == "" || eventID == "" {
		return ""
	}
	return "codex:" + codexSessionID + ":" + eventID
}

type CodexTranscriptInput struct {
	Source CodexTranscriptSource
}

type CodexTranscriptPageInput struct {
	Source       CodexTranscriptSource
	BeforeOffset int64
	Limit        int
}

type CodexTranscriptPage struct {
	Events      []CodexEvent
	StartOffset int64
	EndOffset   int64
	HasMore     bool
}

type ExitResult struct {
	ExitCode      *int
	FailureCode   string
	FailureReason string
	FinishedAt    time.Time
}

type Repository interface {
	CreateRun(ctx context.Context, run Run) error
	HasAnyBySession(ctx context.Context, sessionID SessionID) (bool, error)
	FindActiveBySession(ctx context.Context, sessionID SessionID) (Run, bool, error)
	CountActive(ctx context.Context) (int, error)
	MarkStarted(ctx context.Context, id RunID, pid int) error
	BindTranscript(ctx context.Context, id RunID, pid int, source CodexTranscriptSource) error
	FindTranscriptSource(ctx context.Context, sessionID SessionID, codexSessionID string) (CodexTranscriptSource, bool, error)
	TranscriptSources(ctx context.Context, sessionID SessionID) ([]CodexTranscriptSource, error)
	MarkWaitingUser(ctx context.Context, id RunID) error
	MarkRunning(ctx context.Context, id RunID, pid int, codexSessionID string) error
	MarkStopping(ctx context.Context, id RunID) error
	MarkExited(ctx context.Context, id RunID, result ExitResult) error
}

type HistoryRepository interface {
	Repository
	FindLatestBySession(ctx context.Context, sessionID SessionID) (Run, bool, error)
}

type RunFinder interface {
	FindRun(ctx context.Context, id RunID) (Run, error)
}

type HistoricalRunFinder interface {
	FindLatestRunBySessionBefore(ctx context.Context, sessionID SessionID, before time.Time) (Run, bool, error)
}

type CodexStartInput struct {
	ProcessRunID    RunID
	SessionID       SessionID
	Workdir         string
	ArtifactDir     string
	Prompt          string
	Model           string
	ReasoningEffort string
	PermissionMode  string
	FastMode        bool
	AttachmentPaths []string
	ImagePaths      []string
}

type CodexResumeInput struct {
	ProcessRunID    RunID
	SessionID       SessionID
	CodexSessionID  string
	Transcript      CodexTranscriptSource
	Workdir         string
	ArtifactDir     string
	Prompt          string
	Model           string
	ReasoningEffort string
	PermissionMode  string
	FastMode        bool
	ImagePaths      []string
}

type CodexHandle struct {
	ProcessRunID   RunID
	PID            int
	CodexSessionID string
}

type DetachedProcess struct {
	ProcessRunID RunID
	PID          int
}

type DetachedProcessController interface {
	StopDetached(ctx context.Context, process DetachedProcess) error
}

type CodexCapabilities struct {
	Version                 string
	SupportsAppServer       bool
	SupportsMCPToolTimeout  bool
	SupportsImageGeneration bool
	ImageGenerationStatus   string
	Models                  []CodexModel
}

type CodexSlashCommand struct {
	Name           string
	Description    string
	AcceptsArgs    bool
	RequiresThread bool
}

type CodexFileMatch struct {
	Path    string
	Score   uint32
	Indices []uint32
}

type CodexPromptCompletionProvider interface {
	SlashCommands() []CodexSlashCommand
	SearchFiles(ctx context.Context, root string, query string) ([]CodexFileMatch, error)
}

type CodexModel struct {
	Slug                     string
	DisplayName              string
	DefaultReasoningLevel    string
	SupportedReasoningLevels []CodexReasoningLevel
}

type CodexReasoningLevel struct {
	Effort      string
	Description string
}

type CodexProcess interface {
	Probe(ctx context.Context) (CodexCapabilities, error)
	Start(ctx context.Context, input CodexStartInput) (CodexHandle, error)
	Resume(ctx context.Context, input CodexResumeInput) (CodexHandle, error)
	Stop(ctx context.Context, processRunID RunID) error
	Events(ctx context.Context, handle CodexHandle) (<-chan CodexEvent, error)
}

type CodexSessionCleaner interface {
	DeleteSession(ctx context.Context, source CodexTranscriptSource) error
}
