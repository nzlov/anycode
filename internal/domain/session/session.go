package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var (
	ErrSessionAlreadyExists    = errors.New("session already exists")
	ErrSessionNotFound         = errors.New("session not found")
	ErrInvalidStatusTransition = errors.New("invalid session status transition")
	ErrInvalidQueueIntent      = errors.New("invalid session queue intent")
	ErrInvalidWorktreeCleanup  = errors.New("invalid worktree cleanup transition")
	ErrWorktreeUnavailable     = errors.New("session worktree is unavailable")
	ErrSessionFileNotFound     = errors.New("session file not found")
)

type ID string
type ProjectID string
type WorkflowDefinitionID string
type NodeRunID string
type AttachmentID string
type StagedAttachmentID string
type SessionFileID string
type SessionAttachmentID = SessionFileID

type AttachmentSourceType string

const (
	AttachmentSourceRequirement  AttachmentSourceType = "requirement"
	AttachmentSourcePromptAppend AttachmentSourceType = "prompt_append"
	AttachmentSourceCodex        AttachmentSourceType = "codex_artifact"
	AttachmentSourcePlaywright   AttachmentSourceType = "playwright_artifact"
)

type FileRole string

const (
	FileRoleInput    FileRole = "input"
	FileRoleArtifact FileRole = "artifact"
)

type ArtifactKind string

const (
	ArtifactKindImage   ArtifactKind = "image"
	ArtifactKindPDF     ArtifactKind = "pdf"
	ArtifactKindVideo   ArtifactKind = "video"
	ArtifactKindAudio   ArtifactKind = "audio"
	ArtifactKindArchive ArtifactKind = "archive"
	ArtifactKindText    ArtifactKind = "text"
	ArtifactKindFile    ArtifactKind = "file"
)

type PreviewKind string

const (
	PreviewKindImage PreviewKind = "image"
	PreviewKindPDF   PreviewKind = "pdf"
	PreviewKindVideo PreviewKind = "video"
	PreviewKindAudio PreviewKind = "audio"
	PreviewKindText  PreviewKind = "text"
	PreviewKindNone  PreviewKind = "none"
)

type Mode string

const (
	ModeWorkflow Mode = "workflow"
	ModeChat     Mode = "chat"
)

type Status string

const (
	StatusCreated         Status = "created"
	StatusInitializing    Status = "initializing"
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
	QueueKindStart        QueueKind = "start"
	QueueKindResume       QueueKind = "resume"
	QueueKindPromptAppend QueueKind = "prompt_append"
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

type WorktreeCleanupStatus string

const (
	WorktreeCleanupNotApplicable WorktreeCleanupStatus = "not_applicable"
	WorktreeCleanupProvisioning  WorktreeCleanupStatus = "provisioning"
	WorktreeCleanupActive        WorktreeCleanupStatus = "active"
	WorktreeCleanupPending       WorktreeCleanupStatus = "pending"
	WorktreeCleanupFailed        WorktreeCleanupStatus = "failed"
	WorktreeCleanupCleaned       WorktreeCleanupStatus = "cleaned"
)

type Session struct {
	ID                      ID
	ProjectID               ProjectID
	Requirement             string
	Mentions                []PromptMention
	Mode                    Mode
	Status                  Status
	Priority                Priority
	CloseReason             *CloseReason
	BaseBranch              string
	WorktreePath            string
	WorktreeBranch          string
	WorktreeBaseCommit      string
	WorktreeHeadCommit      string
	WorktreeCleanup         WorktreeCleanup
	InitializationErrorCode string
	InitializationError     string
	CodexSessionID          string
	Config                  Config
	TodoList                TodoList
	Usage                   TokenUsage
	ArtifactCount           int
	FilesChanged            int
	QueuedAt                *time.Time
	Queue                   QueueIntent
	AppliedSystemCommands   map[string]bool
	LastRunAt               *time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
	ClosedAt                *time.Time
}

type TokenUsage struct {
	InputTokens                  int `json:"inputTokens"`
	CachedInputTokens            int `json:"cachedInputTokens"`
	OutputTokens                 int `json:"outputTokens"`
	ReasoningOutputTokens        int `json:"reasoningOutputTokens"`
	TotalTokens                  int `json:"totalTokens"`
	ContextWindow                int `json:"contextWindow"`
	CurrentInputTokens           int `json:"currentInputTokens"`
	CurrentCachedInputTokens     int `json:"currentCachedInputTokens"`
	CurrentOutputTokens          int `json:"currentOutputTokens"`
	CurrentReasoningOutputTokens int `json:"currentReasoningOutputTokens"`
	CurrentTotalTokens           int `json:"currentTotalTokens"`
	CompactionCount              int `json:"compactionCount"`
}

func (u TokenUsage) IsZero() bool {
	return u == (TokenUsage{})
}

type WorktreeCleanup struct {
	Status               WorktreeCleanupStatus
	Attempts             int
	OwnershipToken       string
	OwnershipConfirmedAt *time.Time
	RequestedAt          *time.Time
	LastAt               *time.Time
	NextAt               *time.Time
	CompletedAt          *time.Time
	ErrorCode            string
	Error                string
	Retryable            bool
}

type QueueIntent struct {
	Kind                    QueueKind
	Priority                QueuePriority
	InitialStart            bool
	ReviewAfterReuseFailure bool
	NodeRunID               *NodeRunID
	Prompt                  string
	ResumeCodexSessionID    string
	ResumeOfProcessRunID    string
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

func (s *Session) BeginWorktreeProvisioning(path string, branch string, ownershipToken string, now time.Time) error {
	path = strings.TrimSpace(path)
	branch = strings.TrimSpace(branch)
	ownershipToken = strings.TrimSpace(ownershipToken)
	if path == "" || branch == "" || ownershipToken == "" || branch != strings.TrimSpace(string(s.ID)) || branch == strings.TrimSpace(s.BaseBranch) {
		return fmt.Errorf("%w: path=%q branch=%q", ErrInvalidWorktreeCleanup, path, branch)
	}
	if s.WorktreeCleanup.Status != "" && s.WorktreeCleanup.Status != WorktreeCleanupNotApplicable {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidWorktreeCleanup, s.WorktreeCleanup.Status, WorktreeCleanupProvisioning)
	}
	s.WorktreePath = path
	s.WorktreeBranch = branch
	s.WorktreeCleanup = WorktreeCleanup{Status: WorktreeCleanupProvisioning, OwnershipToken: ownershipToken}
	s.UpdatedAt = now
	return nil
}

func (s *Session) ActivateWorktree(now time.Time) error {
	if s.WorktreeCleanup.Status != WorktreeCleanupProvisioning {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidWorktreeCleanup, s.WorktreeCleanup.Status, WorktreeCleanupActive)
	}
	if err := s.ConfirmWorktreeOwnership(now); err != nil {
		return err
	}
	s.WorktreeCleanup.Status = WorktreeCleanupActive
	s.WorktreeCleanup.ErrorCode = ""
	s.WorktreeCleanup.Error = ""
	s.WorktreeCleanup.Retryable = false
	s.UpdatedAt = now
	return nil
}

func (s *Session) ConfirmWorktreeOwnership(now time.Time) error {
	switch s.WorktreeCleanup.Status {
	case WorktreeCleanupProvisioning, WorktreeCleanupPending, WorktreeCleanupFailed:
	default:
		return fmt.Errorf("%w: confirm ownership from %s", ErrInvalidWorktreeCleanup, s.WorktreeCleanup.Status)
	}
	if strings.TrimSpace(s.WorktreePath) == "" || strings.TrimSpace(s.WorktreeBranch) == "" || strings.TrimSpace(s.WorktreeCleanup.OwnershipToken) == "" {
		return fmt.Errorf("%w: worktree ownership is incomplete", ErrInvalidWorktreeCleanup)
	}
	if s.WorktreeCleanup.OwnershipConfirmedAt == nil {
		s.WorktreeCleanup.OwnershipConfirmedAt = timePtr(now)
	}
	s.UpdatedAt = now
	return nil
}

func (s *Session) RequestWorktreeCleanup(now time.Time) error {
	switch s.WorktreeCleanup.Status {
	case WorktreeCleanupActive, WorktreeCleanupProvisioning, WorktreeCleanupFailed:
	case WorktreeCleanupPending:
		return nil
	default:
		return fmt.Errorf("%w: %s -> %s", ErrInvalidWorktreeCleanup, s.WorktreeCleanup.Status, WorktreeCleanupPending)
	}
	s.WorktreeCleanup.Status = WorktreeCleanupPending
	if s.WorktreeCleanup.RequestedAt == nil {
		s.WorktreeCleanup.RequestedAt = timePtr(now)
	}
	s.WorktreeCleanup.NextAt = timePtr(now)
	s.WorktreeCleanup.ErrorCode = ""
	s.WorktreeCleanup.Error = ""
	s.WorktreeCleanup.Retryable = true
	s.UpdatedAt = now
	return nil
}

func (s *Session) FailWorktreeCleanup(code string, message string, retryable bool, nextAt *time.Time, now time.Time) error {
	if s.WorktreeCleanup.Status != WorktreeCleanupPending && s.WorktreeCleanup.Status != WorktreeCleanupFailed {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidWorktreeCleanup, s.WorktreeCleanup.Status, WorktreeCleanupFailed)
	}
	s.WorktreeCleanup.Status = WorktreeCleanupFailed
	s.WorktreeCleanup.Attempts++
	s.WorktreeCleanup.LastAt = timePtr(now)
	s.WorktreeCleanup.NextAt = nextAt
	s.WorktreeCleanup.CompletedAt = nil
	s.WorktreeCleanup.ErrorCode = strings.TrimSpace(code)
	s.WorktreeCleanup.Error = strings.TrimSpace(message)
	s.WorktreeCleanup.Retryable = retryable
	s.UpdatedAt = now
	return nil
}

func (s *Session) CompleteWorktreeCleanup(now time.Time) error {
	if s.WorktreeCleanup.Status != WorktreeCleanupPending && s.WorktreeCleanup.Status != WorktreeCleanupFailed {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidWorktreeCleanup, s.WorktreeCleanup.Status, WorktreeCleanupCleaned)
	}
	s.WorktreeCleanup.Status = WorktreeCleanupCleaned
	s.WorktreeCleanup.Attempts++
	s.WorktreeCleanup.LastAt = timePtr(now)
	s.WorktreeCleanup.NextAt = nil
	s.WorktreeCleanup.CompletedAt = timePtr(now)
	s.WorktreeCleanup.ErrorCode = ""
	s.WorktreeCleanup.Error = ""
	s.WorktreeCleanup.Retryable = false
	s.UpdatedAt = now
	return nil
}

func (s Session) RequireActiveWorktree() error {
	if strings.TrimSpace(s.BaseBranch) == "" {
		return nil
	}
	if s.WorktreeCleanup.Status != WorktreeCleanupActive || strings.TrimSpace(s.WorktreePath) == "" || strings.TrimSpace(s.WorktreeBranch) == "" {
		return fmt.Errorf("%w: cleanup_status=%q", ErrWorktreeUnavailable, s.WorktreeCleanup.Status)
	}
	return nil
}

func (s *Session) clearQueue() {
	s.QueuedAt = nil
	s.Queue = QueueIntent{}
}

func (s Session) MatchesLifecycleSnapshot(expected Session) bool {
	return s.ID == expected.ID &&
		s.Status == expected.Status &&
		s.UpdatedAt.Equal(expected.UpdatedAt) &&
		queueIntentsEqual(s.Queue, expected.Queue) &&
		timePointersEqual(s.QueuedAt, expected.QueuedAt) &&
		closeReasonsEqual(s.CloseReason, expected.CloseReason)
}

func queueIntentsEqual(left QueueIntent, right QueueIntent) bool {
	if left.Kind != right.Kind || left.Priority != right.Priority || left.InitialStart != right.InitialStart ||
		left.ReviewAfterReuseFailure != right.ReviewAfterReuseFailure ||
		left.Prompt != right.Prompt || left.ResumeCodexSessionID != right.ResumeCodexSessionID ||
		left.ResumeOfProcessRunID != right.ResumeOfProcessRunID {
		return false
	}
	if left.NodeRunID == nil || right.NodeRunID == nil {
		return left.NodeRunID == nil && right.NodeRunID == nil
	}
	return *left.NodeRunID == *right.NodeRunID
}

func timePointersEqual(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func closeReasonsEqual(left *CloseReason, right *CloseReason) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

var allowedStatusTransitions = map[Status][]Status{
	StatusCreated:         {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusStopping, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusInitializing:    {StatusQueued, StatusWaitingApproval, StatusStopping, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusQueued:          {StatusStarting, StatusRunning, StatusWaitingUser, StatusStopping, StatusStopped, StatusResumeFailed, StatusFailed, StatusBlocked, StatusClosed},
	StatusStarting:        {StatusQueued, StatusRunning, StatusWaitingUser, StatusStopping, StatusStopped, StatusResumeFailed, StatusFailed, StatusClosed},
	StatusRunning:         {StatusQueued, StatusWaitingUser, StatusWaitingApproval, StatusStopping, StatusStopped, StatusResumeFailed, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusWaitingUser:     {StatusQueued, StatusRunning, StatusWaitingApproval, StatusStopping, StatusStopped, StatusResumeFailed, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusWaitingApproval: {StatusQueued, StatusStarting, StatusRunning, StatusWaitingUser, StatusStopping, StatusStopped, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusStopping:        {StatusStopped, StatusResumeFailed, StatusFailed, StatusClosed},
	StatusStopped:         {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusStopping, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusResumeFailed:    {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusStopping, StatusStopped, StatusFailed, StatusBlocked, StatusCompleted, StatusClosed},
	StatusFailed:          {StatusInitializing, StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusStopping, StatusBlocked, StatusCompleted, StatusClosed},
	StatusBlocked:         {StatusStopping, StatusClosed},
	StatusCompleted:       {StatusQueued, StatusStarting, StatusWaitingUser, StatusWaitingApproval, StatusStopping, StatusFailed, StatusBlocked, StatusClosed},
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
	return kind == QueueKindStart || kind == QueueKindResume || kind == QueueKindPromptAppend
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

type SessionFile struct {
	ID           SessionFileID
	SessionID    ID
	Role         FileRole
	SourceType   AttachmentSourceType
	SourceID     string
	Kind         string
	ArtifactKind ArtifactKind
	LogicalPath  string
	Filename     string
	Path         string
	MimeType     string
	Size         int64
	Previewable  bool
	PreviewKind  PreviewKind
	CreatedAt    time.Time
}

func (f SessionFile) IsArtifact() bool {
	return f.Role == FileRoleArtifact
}

// SessionAttachment names an input-role SessionFile in attachment workflows.
type SessionAttachment = SessionFile

type ArtifactQuery struct {
	SessionID ID
	Kind      ArtifactKind
	Source    AttachmentSourceType
	Filter    string
	Sort      string
}

type InlineArtifactRequest struct {
	SessionID ID
	Data      []byte
	Filename  string
	SourceKey string
}

type ArtifactPublisher interface {
	PublishInlineArtifact(ctx context.Context, input InlineArtifactRequest) (SessionFile, error)
}

type ArtifactQuarantine struct {
	SessionID  ID
	Path       string
	ModifiedAt time.Time
}

type ArtifactOutputDirectory struct {
	SessionID  ID
	ModifiedAt time.Time
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
	Seeker   io.ReadSeeker
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
	Mentions               []PromptMention
	Status                 PromptAppendStatus
	DispatchedAt           *time.Time
	DispatchedProcessRunID string
	CreatedAt              time.Time
	Attachments            []SessionAttachment
	ArtifactIDs            []SessionFileID
	Artifacts              []SessionFile
}

type PromptMention struct {
	Path string `json:"path"`
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
	ProjectID     *ProjectID
	Scope         string
	Range         string
	UpdatedBefore *time.Time
	Page          int
	PageSize      int
	Filter        string
	Sort          string
}

type Repository interface {
	Create(ctx context.Context, session Session) error
	Save(ctx context.Context, session Session) error
	UpdateArtifactCount(ctx context.Context, id ID, artifactCount int) error
	UpdateFilesChanged(ctx context.Context, id ID, filesChanged int) error
	Find(ctx context.Context, id ID) (Session, error)
	ListCards(ctx context.Context, query ListQuery) ([]Session, int, error)
	ListQueued(ctx context.Context) ([]Session, error)
	ListProvisioningWorktrees(ctx context.Context, limit int) ([]Session, error)
	ListWorktreeCleanupDue(ctx context.Context, now time.Time, limit int) ([]Session, error)
	ListInterruptedWithCodexSession(ctx context.Context) ([]Session, error)
	CountByProject(ctx context.Context, projectID ProjectID) (int, error)
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

type InitializationRepository interface {
	ListInitializing(ctx context.Context) ([]Session, error)
}

type UsageRepository interface {
	UpdateUsage(ctx context.Context, id ID, usage TokenUsage) error
}

type MergeCommandRepository interface {
	FindMergeRecord(ctx context.Context, id string) (MergeRecord, bool, error)
}

type StagedAttachmentRepository interface {
	SaveStagedAttachment(ctx context.Context, attachment StagedAttachment) error
	FindStagedAttachment(ctx context.Context, id StagedAttachmentID) (StagedAttachment, error)
	DeleteStagedAttachment(ctx context.Context, id StagedAttachmentID) error
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

type PromoteAttachmentInput struct {
	Staged     StagedAttachment
	SessionID  ID
	SourceType AttachmentSourceType
	SourceID   string
}

type AttachmentStore interface {
	Stage(ctx context.Context, input StageAttachmentInput) (StagedAttachment, error)
	Promote(ctx context.Context, input PromoteAttachmentInput) (SessionAttachment, error)
	DeleteStaged(ctx context.Context, id StagedAttachmentID) error
	FindSessionFile(ctx context.Context, id SessionFileID) (SessionFile, error)
	ListSessionAttachments(ctx context.Context, sessionID ID) ([]SessionAttachment, error)
	ListPromptAppendAttachments(ctx context.Context, sessionID ID, appendID string) ([]SessionAttachment, error)
	DeleteSession(ctx context.Context, id SessionAttachmentID) error
	Open(ctx context.Context, path string) (AttachmentStream, error)
}

type InspectArtifactInput struct {
	SessionID  ID
	SourcePath string
	MaxBytes   int64
}

type WriteInlineArtifactInput struct {
	SessionID ID
	Data      []byte
	Filename  string
	SourceKey string
	MaxBytes  int64
}

type ArtifactStore interface {
	EnsureArtifactDir(ctx context.Context, sessionID ID) (string, error)
	ArtifactDir(sessionID ID) string
	InspectArtifact(ctx context.Context, input InspectArtifactInput) (SessionFile, error)
	WriteInlineArtifact(ctx context.Context, input WriteInlineArtifactInput) (SessionFile, error)
	FindArtifact(ctx context.Context, id SessionFileID) (SessionFile, error)
	ListArtifacts(ctx context.Context, query ArtifactQuery) ([]SessionFile, error)
	ResolveArtifacts(ctx context.Context, sessionID ID, logicalPaths []string) ([]SessionFile, error)
	SumArtifactSize(ctx context.Context, sessionID ID) (int64, error)
	CountArtifacts(ctx context.Context, sessionID ID) (int, error)
	DeleteArtifact(ctx context.Context, id SessionFileID) (SessionFile, error)
	OpenArtifact(ctx context.Context, id SessionFileID) (AttachmentStream, error)
	WatchArtifactDir(ctx context.Context, sessionID ID) (<-chan struct{}, error)
	QuarantineArtifactDir(ctx context.Context, sessionID ID, token string) (string, error)
	RestoreArtifactDir(ctx context.Context, sessionID ID, quarantinePath string) error
	DeleteQuarantine(ctx context.Context, quarantinePath string) error
	ListArtifactQuarantines(ctx context.Context) ([]ArtifactQuarantine, error)
	ListArtifactOutputDirectories(ctx context.Context) ([]ArtifactOutputDirectory, error)
	DeleteArtifactOutputDirectory(ctx context.Context, sessionID ID) error
}

type WorktreeManager interface {
	Create(ctx context.Context, projectPath string, projectID ProjectID, sessionID ID, branch string, baseBranch string, ownershipToken string) (string, error)
	InspectOwnership(ctx context.Context, projectPath string, path string, branch string, ownershipToken string) (WorktreeOwnership, error)
	HeadCommit(ctx context.Context, path string, ref string) (string, error)
	RetainCommit(ctx context.Context, projectPath string, sessionID ID, commit string) error
	Remove(ctx context.Context, path string) error
	DeleteBranch(ctx context.Context, projectPath string, branch string) error
	ReleaseOwnership(ctx context.Context, path string, ownershipToken string) error
	PathForSession(projectID ProjectID, sessionID ID) string
}

type WorktreeOwnership struct {
	PathExists   bool
	BranchExists bool
	Registered   bool
	MarkerExists bool
	TokenMatches bool
	Matches      bool
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
	SessionID          ID
	NodeRunID          *NodeRunID
	CurrentNodeID      string
	CurrentNodeTitle   string
	Status             string
	RequiresCodex      bool
	RequireResultRetry bool
	ApprovalPhase      string
	Result             map[string]any
	Prompt             string
	Merge              *WorkflowMerge
	Expr               *WorkflowExpr
	Close              bool
}

type WorkflowStartFailureInput struct {
	SessionID ID
	NodeRunID *NodeRunID
	Code      string
	Message   string
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
	SessionID ID
	NodeRunID NodeRunID
	Code      string
	Message   string
	Output    map[string]any
}

type WorkflowNodeCompleteInput struct {
	SessionID ID
	NodeRunID NodeRunID
	CommandID string
	Output    map[string]any
}

type WorkflowProcessExitInput struct {
	SessionID      ID
	NodeRunID      NodeRunID
	Failed         bool
	FailureCode    string
	FailureMessage string
	Output         map[string]any
}

type WorkflowApprovalInput struct {
	SessionID ID
	NodeID    string
	Approved  bool
	Comment   string
}

type WorkflowRunSnapshot struct {
	SessionID     ID
	Status        string
	CurrentNodeID string
	Context       map[string]any
}

type WorkflowApprovalResult struct {
	Run              WorkflowRunSnapshot
	Advance          WorkflowAdvance
	RejectedAfterRun bool
	Rejected         bool
}

type WorkflowAdvance struct {
	SessionID          ID
	NodeRunID          *NodeRunID
	CurrentNodeID      string
	CurrentNodeTitle   string
	Status             string
	RequiresCodex      bool
	RequireResultRetry bool
	ApprovalPhase      string
	Result             map[string]any
	Prompt             string
	Merge              *WorkflowMerge
	Expr               *WorkflowExpr
	Close              bool
	Completed          bool
	Blocked            bool
	BlockedReason      string
	BlockedCode        string
	BlockedMessage     string
	CommandID          string
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
