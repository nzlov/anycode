package process

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrProcessNotFound            = errors.New("codex process run is not active")
	ErrProcessOwnershipUnverified = errors.New("codex process ownership could not be verified")
	ErrThreadUnavailable          = errors.New("codex thread is unavailable")
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
	CodexSessionID string
	ResumeOf       *RunID
	ExitCode       *int
	FailureReason  string
	StartedAt      time.Time
	FinishedAt     *time.Time
}

type CodexEvent struct {
	EventID        string
	Type           CodexEventType
	SessionID      SessionID
	ProcessRunID   RunID
	CodexSessionID string
	CorrelationID  string
	TurnID         string
	Phase          CodexPhase
	Content        CodexEventContent
	Sequence       int64
	CreatedAt      time.Time
}

type CodexEventType string

const (
	CodexEventMessage     CodexEventType = "message"
	CodexEventReasoning   CodexEventType = "reasoning"
	CodexEventCommand     CodexEventType = "command"
	CodexEventTool        CodexEventType = "tool"
	CodexEventFileChange  CodexEventType = "file_change"
	CodexEventPlan        CodexEventType = "plan"
	CodexEventUsage       CodexEventType = "usage"
	CodexEventStatus      CodexEventType = "status"
	CodexEventProcessExit CodexEventType = "process.exit"
	CodexEventUnknown     CodexEventType = "unknown"
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

type CodexHistoryPageInput struct {
	ThreadID string
	Cursor   string
	Limit    int
}

type CodexHistoryPage struct {
	Events     []CodexEvent
	NextCursor string
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
	MarkWaitingUser(ctx context.Context, id RunID) error
	MarkRunning(ctx context.Context, id RunID, codexSessionID string) error
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
	ProcessRunID          RunID
	SessionID             SessionID
	Workdir               string
	ArtifactDir           string
	Input                 []CodexInputItem
	Action                CodexAction
	ActionArgument        string
	DeveloperInstructions string
	Model                 string
	ReasoningEffort       string
	PermissionMode        string
	FastMode              bool
}

type CodexResumeInput struct {
	ProcessRunID          RunID
	SessionID             SessionID
	CodexSessionID        string
	Workdir               string
	ArtifactDir           string
	Input                 []CodexInputItem
	Action                CodexAction
	ActionArgument        string
	DeveloperInstructions string
	Model                 string
	ReasoningEffort       string
	PermissionMode        string
	FastMode              bool
}

type CodexInputItem struct {
	Type string
	Text string
	Path string
	Name string
}

type CodexAction string

const (
	CodexActionTurn    CodexAction = "turn"
	CodexActionPlan    CodexAction = "plan"
	CodexActionReview  CodexAction = "review"
	CodexActionCompact CodexAction = "compact"
	CodexActionGoal    CodexAction = "goal"
)

type CodexSteerInput struct {
	ProcessRunID RunID
	Input        []CodexInputItem
}

type CodexHandle struct {
	ProcessRunID   RunID
	CodexSessionID string
	TurnID         string
}

type CodexCapabilities struct {
	Version                 string
	SupportsAppServer       bool
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
	Steer(ctx context.Context, input CodexSteerInput) error
	Stop(ctx context.Context, processRunID RunID) error
	Events(ctx context.Context, handle CodexHandle) (<-chan CodexEvent, error)
}

type CodexSessionCleaner interface {
	DeleteThread(ctx context.Context, threadID string) error
}

type CodexHistory interface {
	HistoryPage(ctx context.Context, input CodexHistoryPageInput) (CodexHistoryPage, error)
}

type DynamicToolCall struct {
	ProcessRunID RunID
	SessionID    SessionID
	ThreadID     string
	TurnID       string
	CallID       string
	Tool         string
	Arguments    json.RawMessage
}

type DynamicToolContent struct {
	Type     string
	Text     string
	ImageURL string
	AudioURL string
}

type DynamicToolResult struct {
	Success bool
	Content []DynamicToolContent
}

type DynamicToolHandler interface {
	HandleDynamicTool(ctx context.Context, call DynamicToolCall) (DynamicToolResult, error)
}

type DynamicToolHandlerRegistrar interface {
	SetDynamicToolHandler(handler DynamicToolHandler)
}
