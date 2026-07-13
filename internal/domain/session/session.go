package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrSessionAlreadyExists    = errors.New("session already exists")
	ErrInvalidStatusTransition = errors.New("invalid session status transition")
	ErrInvalidQueueIntent      = errors.New("invalid session queue intent")
)

type ID string
type ProjectID string
type WorkflowDefinitionID string
type WorkflowRunID string
type NodeRunID string
type AttachmentID string
type StagedAttachmentID string
type SessionAttachmentID string

type AttachmentSourceType string

const (
	AttachmentSourceRequirement  AttachmentSourceType = "requirement"
	AttachmentSourcePromptAppend AttachmentSourceType = "prompt_append"
)

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
	CloseReasonUserClosed     CloseReason = "user_closed"
	CloseReasonMergedClosed   CloseReason = "merged_closed"
	CloseReasonWorkflowClosed CloseReason = "workflow_closed"
)

type Session struct {
	ID                 ID
	ProjectID          ProjectID
	Requirement        string
	Mode               Mode
	Status             Status
	Priority           Priority
	CloseReason        *CloseReason
	BaseBranch         string
	WorktreePath       string
	WorktreeBaseCommit string
	CodexSessionID     string
	Config             Config
	TodoList           TodoList
	QueuedAt           *time.Time
	Queue              QueueIntent
	LastRunAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ClosedAt           *time.Time
}

type QueueIntent struct {
	Kind                    QueueKind
	Priority                QueuePriority
	InitialStart            bool
	ReviewAfterReuseFailure bool
	WorkflowRunID           WorkflowRunID
	NodeRunID               *NodeRunID
	Prompt                  string
	ResumeCodexSessionID    string
}

func (s *Session) QueueExecution(intent QueueIntent, now time.Time) error {
	if !validQueueKind(intent.Kind) || !validQueuePriority(intent.Priority) {
		return fmt.Errorf("%w: kind=%q priority=%q", ErrInvalidQueueIntent, intent.Kind, intent.Priority)
	}
	if !canTransition(s.Status, StatusQueued) {
		return invalidTransition(s.Status, StatusQueued)
	}
	s.Status = StatusQueued
	s.Queue = intent
	s.QueuedAt = timePtr(now)
	s.LastRunAt = timePtr(now)
	s.UpdatedAt = now
	return nil
}

func (s *Session) TransitionTo(next Status, now time.Time) error {
	if next == StatusQueued {
		return fmt.Errorf("%w: use QueueExecution", ErrInvalidQueueIntent)
	}
	if !canTransition(s.Status, next) {
		return invalidTransition(s.Status, next)
	}
	s.Status = next
	s.UpdatedAt = now
	if next == StatusStarting {
		s.LastRunAt = timePtr(now)
	}
	s.clearQueue()
	return nil
}

func (s *Session) Close(reason CloseReason, now time.Time) error {
	if err := s.TransitionTo(StatusClosed, now); err != nil {
		return err
	}
	s.CloseReason = &reason
	s.ClosedAt = timePtr(now)
	return nil
}

func (s *Session) clearQueue() {
	s.QueuedAt = nil
	s.Queue = QueueIntent{}
}

var allowedStatusTransitions = map[Status][]Status{
	StatusCreated:         {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusQueued:          {StatusStarting, StatusRunning, StatusStopping, StatusStopped, StatusResumeFailed, StatusFailed, StatusBlocked, StatusClosed},
	StatusStarting:        {StatusRunning, StatusWaitingUser, StatusStopping, StatusStopped, StatusResumeFailed, StatusFailed, StatusClosed},
	StatusRunning:         {StatusQueued, StatusWaitingUser, StatusWaitingApproval, StatusStopping, StatusStopped, StatusResumeFailed, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusWaitingUser:     {StatusQueued, StatusRunning, StatusWaitingApproval, StatusStopping, StatusStopped, StatusResumeFailed, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusWaitingApproval: {StatusQueued, StatusStarting, StatusRunning, StatusWaitingUser, StatusStopped, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusStopping:        {StatusStopped, StatusResumeFailed, StatusFailed, StatusClosed},
	StatusStopped:         {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusResumeFailed:    {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusStopping, StatusStopped, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusFailed:          {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusBlocked, StatusCompleted, StatusClosed},
	StatusBlocked:         {StatusClosed},
	StatusCompleted:       {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusFailed, StatusBlocked, StatusClosed},
	StatusClosed:          {},
}

func canTransition(current Status, next Status) bool {
	if current == next {
		return true
	}
	for _, candidate := range allowedStatusTransitions[current] {
		if candidate == next {
			return true
		}
	}
	return false
}

func validQueueKind(kind QueueKind) bool {
	return kind == QueueKindStart || kind == QueueKindResume || kind == QueueKindAnswerUser
}

func validQueuePriority(priority QueuePriority) bool {
	return priority == QueuePriorityImmediate || priority == QueuePriorityHigh || priority == QueuePriorityMedium || priority == QueuePriorityLow
}

func invalidTransition(current Status, next Status) error {
	return fmt.Errorf("%w: %s -> %s", ErrInvalidStatusTransition, current, next)
}

func timePtr(value time.Time) *time.Time {
	copy := value
	return &copy
}

type Config struct {
	CodexModel      string
	ReasoningEffort string
	PermissionMode  string
	FastMode        bool
}

type TodoList struct {
	Items []TodoItem `json:"items"`
}

type TodoItem struct {
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}

func (l TodoList) Total() int {
	return len(l.Items)
}

func (l TodoList) Completed() int {
	count := 0
	for _, item := range l.Items {
		if item.Completed {
			count++
		}
	}
	return count
}

type SessionAttachment struct {
	ID          SessionAttachmentID
	SessionID   ID
	SourceType  AttachmentSourceType
	SourceID    string
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

type PromptAppendStatus string

const (
	PromptAppendPending    PromptAppendStatus = "pending"
	PromptAppendInflight   PromptAppendStatus = "inflight"
	PromptAppendDispatched PromptAppendStatus = "dispatched"
)

type PromptAppend struct {
	ID                     string
	SessionID              ID
	Body                   string
	Status                 PromptAppendStatus
	DispatchedAt           *time.Time
	DispatchedProcessRunID string
	CreatedAt              time.Time
	Attachments            []SessionAttachment
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
	Create(ctx context.Context, session Session) error
	Save(ctx context.Context, session Session) error
	Find(ctx context.Context, id ID) (Session, error)
	ListCards(ctx context.Context, query ListQuery) ([]Session, int, error)
	ListQueued(ctx context.Context) ([]Session, error)
	ListInterruptedWithCodexSession(ctx context.Context) ([]Session, error)
	CountByProject(ctx context.Context, projectID ProjectID) (int, error)
	LastConfigForProject(ctx context.Context, projectID ProjectID) (Config, bool, error)
	AppendPrompt(ctx context.Context, append PromptAppend) error
	UpdatePendingPromptAppendBody(ctx context.Context, sessionID ID, id string, body string) (PromptAppend, bool, error)
	DeletePromptAppend(ctx context.Context, id string) error
	ListPromptAppends(ctx context.Context, sessionID ID) ([]PromptAppend, error)
	ListPendingPromptAppends(ctx context.Context, sessionID ID) ([]PromptAppend, error)
	MarkPromptAppendsInflight(ctx context.Context, ids []string, processRunID string) error
	CompletePromptAppends(ctx context.Context, processRunID string, dispatchedAt time.Time) error
	ReleasePromptAppends(ctx context.Context, processRunID string) error
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
	ListPromptAppendAttachments(ctx context.Context, sessionID ID, appendID string) ([]SessionAttachment, error)
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
	Exists(ctx context.Context, path string) (bool, error)
	HeadCommit(ctx context.Context, path string, ref string) (string, error)
	Remove(ctx context.Context, path string) error
	DeleteBranch(ctx context.Context, projectPath string, branch string) error
	PathForSession(projectID ProjectID, sessionID ID) string
}

type WorktreeInitResult struct {
	Success         bool
	ExitCode        *int
	Output          string
	OutputTruncated bool
}

type WorktreeInitializer interface {
	Run(ctx context.Context, worktreePath string, script string) (WorktreeInitResult, error)
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
	Close            bool
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

type WorkflowProcessExitInput struct {
	WorkflowRunID  WorkflowRunID
	NodeRunID      NodeRunID
	Failed         bool
	FailureCode    string
	FailureMessage string
	Output         map[string]any
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
	Run              WorkflowRunSnapshot
	Advance          WorkflowAdvance
	RejectedAfterRun bool
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
	Close            bool
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
	RecoverProcessExit(ctx context.Context, input WorkflowProcessExitInput) (WorkflowAdvance, error)
	SubmitApprovalForSession(ctx context.Context, input WorkflowApprovalInput) (WorkflowApprovalResult, error)
}
