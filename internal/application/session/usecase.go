package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/expr-lang/expr"
	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/application/port"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	gitdiffdomain "github.com/nzlov/anycode/internal/domain/gitdiff"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	domain "github.com/nzlov/anycode/internal/domain/session"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
)

type UseCase interface {
	CreateSession(ctx context.Context, input CreateSessionInput) (DTO, error)
	RecoverInterruptedSessions(ctx context.Context) (int, error)
	ExecuteSession(ctx context.Context, id domain.ID) (DTO, error)
	ExecuteSessionWithOptions(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error)
	StartSession(ctx context.Context, id domain.ID) (DTO, error)
	StartSessionWithOptions(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error)
	SetSessionPriority(ctx context.Context, input SetSessionPriorityInput) (DTO, error)
	StopSession(ctx context.Context, id domain.ID) (DTO, error)
	StopProjectSessions(ctx context.Context, projectID domain.ProjectID) (int, error)
	ResumeSession(ctx context.Context, id domain.ID) (DTO, error)
	ResumeSessionWithOptions(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error)
	DrainQueuedSessions(ctx context.Context) (int, error)
	ReconcileWorktreeCleanup(ctx context.Context) (int, error)
	DrainWorktreeCleanup(ctx context.Context) (int, error)
	RetryWorktreeCleanup(ctx context.Context, id domain.ID) (DTO, error)
	StartWorktreeCleanupCoordinator()
	CloseSession(ctx context.Context, input CloseSessionInput) (DTO, error)
	UpdateSessionConfig(ctx context.Context, input UpdateSessionConfigInput) (DTO, error)
	RequestUserAnswer(ctx context.Context, input RequestUserAnswerInput) (questionapp.BatchDTO, error)
	AcknowledgeUserAnswerDelivery(ctx context.Context, input AcknowledgeUserAnswerDeliveryInput) error
	FailUserAnswerDelivery(ctx context.Context, input FailUserAnswerDeliveryInput) error
	SubmitQuestionBatch(ctx context.Context, input questionapp.SubmitBatchInput) (questionapp.BatchDTO, error)
	AppendPrompt(ctx context.Context, input AppendPromptInput) (PromptAppendDTO, error)
	UpdatePromptAppend(ctx context.Context, input UpdatePromptAppendInput) (PromptAppendDTO, error)
	SubmitWorkflowApproval(ctx context.Context, input SubmitWorkflowApprovalInput) (WorkflowRunDTO, error)
	GetSession(ctx context.Context, id domain.ID) (DetailDTO, error)
	GetSessionCard(ctx context.Context, id domain.ID) (CardDTO, error)
	LastSessionConfigForProject(ctx context.Context, projectID domain.ProjectID) (*ConfigDTO, error)
	ListSessions(ctx context.Context, input ListSessionsInput) (port.Page[CardDTO], error)
}

type ConfigInput struct {
	CodexModel      string
	ReasoningEffort string
	PermissionMode  string
	FastMode        *bool
}

type CreateSessionInput struct {
	ProjectID           domain.ProjectID
	Requirement         string
	Mode                domain.Mode
	BaseBranch          string
	Config              ConfigInput
	Priority            domain.Priority
	StagedAttachmentIDs []domain.StagedAttachmentID
}

type StartSessionOptions struct {
	Force                bool
	prompt               string
	resumeCodexSessionID string
	queueKind            domain.QueueKind
}

type CloseSessionInput struct {
	SessionID              domain.ID
	Reason                 domain.CloseReason
	appliedSystemCommandID string
}

type SetSessionPriorityInput struct {
	SessionID domain.ID
	Priority  domain.Priority
}

type UpdateSessionConfigInput struct {
	SessionID domain.ID
	Config    ConfigInput
}

type AppendPromptInput struct {
	SessionID           domain.ID
	Body                string
	StagedAttachmentIDs []domain.StagedAttachmentID
}

type UpdatePromptAppendInput struct {
	SessionID      domain.ID
	PromptAppendID string
	Body           string
}

type SubmitWorkflowApprovalInput struct {
	WorkflowRunID domain.WorkflowRunID
	NodeID        string
	Approved      bool
	Comment       string
}

type RequestUserAnswerInput struct {
	SessionID domain.ID
	Questions []questiondomain.Question
}

type AcknowledgeUserAnswerDeliveryInput struct {
	SessionID domain.ID
	BatchID   questiondomain.BatchID
}

type UserAnswerDeliveryFailureKind string

const UserAnswerDeliveryTransportClosed UserAnswerDeliveryFailureKind = "transport_closed"

type FailUserAnswerDeliveryInput struct {
	SessionID domain.ID
	BatchID   questiondomain.BatchID
	Kind      UserAnswerDeliveryFailureKind
}

type answerUserFallbackReason string

const (
	answerUserFallbackTimeout   answerUserFallbackReason = "answer_wait_timeout"
	answerUserFallbackTransport answerUserFallbackReason = "transport_closed"
	answerUserFallbackAckFailed answerUserFallbackReason = "delivery_ack_failed"
)

type ListSessionsInput struct {
	ProjectID *domain.ProjectID
	Scope     string
	Range     string
	Page      int
	PageSize  int
	Filter    string
	Sort      string
}

type DTO struct {
	ID                 domain.ID
	ProjectID          domain.ProjectID
	Requirement        string
	Mode               domain.Mode
	Status             domain.Status
	Priority           domain.Priority
	BaseBranch         string
	WorktreeBranch     string
	WorktreePath       string
	WorktreeBaseCommit string
	WorktreeCleanup    WorktreeCleanupDTO
	CodexSessionID     string
	Config             domain.Config
	ArtifactCount      int
	FilesChanged       int
	AvailableActions   []string
	LastRunAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type WorktreeCleanupDTO struct {
	Status      domain.WorktreeCleanupStatus
	Attempts    int
	RequestedAt *time.Time
	CompletedAt *time.Time
	Error       *WorktreeCleanupErrorDTO
}

type WorktreeCleanupErrorDTO struct {
	Code      string
	Message   string
	Retryable bool
}

type ConfigDTO struct {
	CodexModel      string
	ReasoningEffort string
	PermissionMode  string
	FastMode        bool
}

type CardDTO struct {
	DTO
	ProjectName        string
	RequirementSummary string
	CurrentNodeTitle   string
	PendingApproval    *PendingApprovalDTO
	PendingQuestion    bool
	TodoList           domain.TodoList
	Attachments        []domain.SessionAttachment
	AvailableActions   []string
}

type DetailDTO struct {
	DTO
	CloseReason      *domain.CloseReason
	CurrentNodeTitle string
	PendingApproval  *PendingApprovalDTO
	Attachments      []domain.SessionAttachment
	PromptAppends    []PromptAppendDTO
	AvailableActions []string
	CanResume        bool
}

type PromptAppendDTO struct {
	ID          string
	SessionID   domain.ID
	Body        string
	CreatedAt   time.Time
	Attachments []domain.SessionAttachment
}

type WorkflowRunDTO struct {
	ID            domain.WorkflowRunID
	SessionID     domain.ID
	Status        string
	CurrentNodeID string
	Context       map[string]any
}

type PendingApprovalDTO struct {
	WorkflowRunID    domain.WorkflowRunID
	NodeID           string
	NodeRunID        string
	CurrentNodeTitle string
	Phase            string
	Result           map[string]any
}

const (
	defaultPage              = 1
	defaultPageSize          = 20
	maxPageSize              = 100
	processCleanupTimeout    = 5 * time.Second
	processExitRetryMaxDelay = 30 * time.Second
	worktreeCleanupInterval  = 5 * time.Second
	worktreeCleanupBatchSize = 100

	maxSessionIDAttempts = 100
)

var ErrProcessLifecycleNotWired = errors.New("session process lifecycle is not wired")

type workflowApprovalPostCommitAdvance struct {
	session        domain.Session
	advance        domain.WorkflowAdvance
	commandEventID eventdomain.ID
	pendingEvent   eventdomain.DomainEvent
}

var (
	errWorkdirBusy           = errors.New("session workdir already has an active process")
	errProcessCleanupPending = errors.New("codex process may still be running")
)
var errWorkflowResumeStateNotPersisted = errors.New("workflow resume failure state was not persisted")
var errWorkflowResultFailureNotPersisted = errors.New("workflow result failure state was not persisted")

const (
	workflowSystemAdvancePendingEvent   = "workflow.system_advance_pending"
	workflowSystemAdvanceCompletedEvent = "workflow.system_advance_completed"
	answerUserWarmWaitTimeout           = 5 * time.Minute
)

var (
	errCloseRequiresStop     = errors.New("session must stop before close")
	errClosePreparationStale = errors.New("session changed while preparing close")
)
var fallbackEventSequence atomic.Uint64

type Service struct {
	repo                domain.Repository
	uow                 port.UnitOfWork
	locker              port.SessionLocker
	projects            projectdomain.Repository
	attachments         domain.AttachmentRepository
	files               domain.AttachmentStore
	artifacts           domain.ArtifactStore
	artifactScanner     artifactScanner
	artifactPublisher   domain.ArtifactPublisher
	worktrees           domain.WorktreeManager
	worktreeInitializer domain.WorktreeInitializer
	workflows           domain.WorkflowStarter
	merge               gitdiffdomain.MergePort
	diffCounter         sessionDiffCounter
	processes           processdomain.Repository
	codex               processdomain.CodexProcess
	processConsumers    sync.Map
	workdirMu           sync.Mutex
	activeWorkdirs      map[string]domain.ID
	events              eventdomain.Store
	publisher           eventdomain.Publisher
	questions           questionCoordinator
	now                 func() time.Time
	generateID          func() (domain.ID, error)
	maxConcurrentAgents int
	queueDrainScheduler func(*Service)
	processExitDelay    func(int) time.Duration
	answerUserTimer     func(time.Duration) (<-chan time.Time, func())
	lifecycleCtx        context.Context
	lifecycleCancel     context.CancelFunc
	cleanupWake         chan struct{}
	cleanupStartOnce    sync.Once
	cleanupWG           sync.WaitGroup
}

type questionCoordinator interface {
	CreateBatch(ctx context.Context, input questionapp.CreateBatchInput) (questionapp.BatchDTO, error)
	CancelPendingBySession(ctx context.Context, sessionID questiondomain.SessionID, reason string) error
}

type artifactScanner interface {
	Scan(ctx context.Context, input domain.ArtifactScanRequest) ([]domain.SessionAttachment, error)
}

type sessionDiffCounter interface {
	CountSessionChangedFiles(ctx context.Context, sessionID domain.ID) (int, error)
}

type answerQuestionCoordinator interface {
	questionCoordinator
	SubmitBatch(ctx context.Context, input questionapp.SubmitBatchInput) (questionapp.BatchDTO, error)
	GetBatch(ctx context.Context, id questiondomain.BatchID) (questionapp.BatchDTO, error)
	QuestionBatchUpdates(ctx context.Context, sessionID questiondomain.SessionID) (<-chan questionapp.BatchDTO, error)
	PublishBatch(batch questionapp.BatchDTO)
}

type workflowApprovalRepositoryRunner interface {
	SubmitApprovalForSessionWithRepositories(ctx context.Context, input domain.WorkflowApprovalInput, repo workflowdomain.Repository, events eventdomain.Store) (domain.WorkflowApprovalResult, []eventdomain.DomainEvent, error)
}

type workflowResumeFailureRepositoryRunner interface {
	MarkResumeFailedForSessionWithRepositories(ctx context.Context, input domain.WorkflowResumeFailureInput, repo workflowdomain.Repository, events eventdomain.Store) (domain.WorkflowRunSnapshot, []eventdomain.DomainEvent, error)
}

type Option func(*Service)

func WithAttachments(repo domain.AttachmentRepository, store domain.AttachmentStore) Option {
	return func(s *Service) {
		s.attachments = repo
		s.files = store
		if artifacts, ok := store.(domain.ArtifactStore); ok {
			s.artifacts = artifacts
		}
	}
}

func WithArtifactScanner(scanner artifactScanner) Option {
	return func(s *Service) {
		s.artifactScanner = scanner
	}
}

func WithArtifactPublisher(publisher domain.ArtifactPublisher) Option {
	return func(s *Service) {
		s.artifactPublisher = publisher
	}
}

func WithWorktrees(worktrees domain.WorktreeManager) Option {
	return func(s *Service) {
		s.worktrees = worktrees
	}
}

func WithWorktreeInitializer(initializer domain.WorktreeInitializer) Option {
	return func(s *Service) {
		s.worktreeInitializer = initializer
	}
}

func WithWorkflows(workflows domain.WorkflowStarter) Option {
	return func(s *Service) {
		s.workflows = workflows
	}
}

func WithMergePort(merge gitdiffdomain.MergePort) Option {
	return func(s *Service) {
		s.merge = merge
	}
}

func WithDiffCounter(counter sessionDiffCounter) Option {
	return func(s *Service) {
		s.diffCounter = counter
	}
}

func WithProcesses(repo processdomain.Repository, codex processdomain.CodexProcess) Option {
	return func(s *Service) {
		s.processes = repo
		s.codex = codex
	}
}

func WithEvents(store eventdomain.Store) Option {
	return func(s *Service) {
		s.events = store
	}
}

func WithEventPublisher(publisher eventdomain.Publisher) Option {
	return func(s *Service) {
		s.publisher = publisher
	}
}

func WithQuestions(questions questionCoordinator) Option {
	return func(s *Service) {
		s.questions = questions
	}
}

func WithUnitOfWork(uow port.UnitOfWork) Option {
	return func(s *Service) {
		s.uow = uow
	}
}

func WithSessionLocker(locker port.SessionLocker) Option {
	return func(s *Service) {
		s.locker = locker
	}
}

func WithMaxConcurrentAgents(max int) Option {
	return func(s *Service) {
		s.maxConcurrentAgents = max
	}
}

func WithAutoQueueDrain() Option {
	return func(s *Service) {
		s.queueDrainScheduler = func(service *Service) {
			go service.drainQueuedSessions(context.Background())
		}
	}
}

func New(repo domain.Repository, projects projectdomain.Repository, options ...Option) *Service {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	service := &Service{
		repo:                repo,
		projects:            projects,
		now:                 time.Now,
		generateID:          generateID,
		maxConcurrentAgents: 1,
		queueDrainScheduler: func(*Service) {},
		processExitDelay:    processExitRetryDelay,
		answerUserTimer: func(duration time.Duration) (<-chan time.Time, func()) {
			timer := time.NewTimer(duration)
			return timer.C, func() { timer.Stop() }
		},
		lifecycleCtx:    lifecycleCtx,
		lifecycleCancel: lifecycleCancel,
		cleanupWake:     make(chan struct{}, 1),
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Close() {
	if s != nil && s.lifecycleCancel != nil {
		s.lifecycleCancel()
		s.cleanupWG.Wait()
	}
}

func (s *Service) StartWorktreeCleanupCoordinator() {
	if s == nil {
		return
	}
	s.cleanupStartOnce.Do(func() {
		s.cleanupWG.Add(1)
		go s.runWorktreeCleanupCoordinator()
	})
	s.scheduleWorktreeCleanup()
}

func (s *Service) runWorktreeCleanupCoordinator() {
	defer s.cleanupWG.Done()
	ticker := time.NewTicker(worktreeCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.lifecycleCtx.Done():
			return
		case <-s.cleanupWake:
		case <-ticker.C:
		}
		if _, err := s.DrainWorktreeCleanup(s.lifecycleCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("drain session worktree cleanup: %v", err)
		}
	}
}

func (s *Service) scheduleWorktreeCleanup() {
	if s == nil || s.cleanupWake == nil {
		return
	}
	select {
	case s.cleanupWake <- struct{}{}:
	default:
	}
}

func (s *Service) ReconcileWorktreeCleanup(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	reconciled := 0
	for {
		sessions, err := s.repo.ListProvisioningWorktrees(ctx, worktreeCleanupBatchSize)
		if err != nil {
			return reconciled, err
		}
		if len(sessions) == 0 {
			return reconciled, nil
		}
		for _, candidate := range sessions {
			if err := s.withSessionLock(ctx, candidate.ID, func(ctx context.Context) error {
				current, err := s.repo.Find(ctx, candidate.ID)
				if err != nil {
					return fmt.Errorf("find provisioning session worktree: %w", err)
				}
				if current.WorktreeCleanup.Status != domain.WorktreeCleanupProvisioning {
					return nil
				}
				now := s.now()
				if current.Status != domain.StatusClosed && current.Status != domain.StatusFailed {
					if err := transitionSession(&current, domain.StatusFailed, now); err != nil {
						return err
					}
				}
				if err := current.RequestWorktreeCleanup(now); err != nil {
					return err
				}
				if err := s.saveSessionWithEvent(ctx, current, "session.worktree_cleanup_requested", map[string]any{
					"reason":         "service_restarted_during_provisioning",
					"worktreeBranch": current.WorktreeBranch,
				}); err != nil {
					return err
				}
				reconciled++
				return nil
			}); err != nil {
				return reconciled, err
			}
		}
		if len(sessions) < worktreeCleanupBatchSize {
			return reconciled, nil
		}
	}
}

func (s *Service) DrainWorktreeCleanup(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil || s.worktrees == nil || s.projects == nil {
		return 0, nil
	}
	processed := 0
	for {
		sessions, err := s.repo.ListWorktreeCleanupDue(ctx, s.now(), worktreeCleanupBatchSize)
		if err != nil {
			return processed, err
		}
		if len(sessions) == 0 {
			return processed, nil
		}
		for _, candidate := range sessions {
			if err := s.withSessionLock(ctx, candidate.ID, func(ctx context.Context) error {
				current, err := s.repo.Find(ctx, candidate.ID)
				if err != nil {
					return fmt.Errorf("find session for worktree cleanup: %w", err)
				}
				if !worktreeCleanupDue(current, s.now()) {
					return nil
				}
				if err := s.cleanupSessionWorktree(ctx, current); err != nil {
					return err
				}
				processed++
				return nil
			}); err != nil {
				return processed, err
			}
		}
		if len(sessions) < worktreeCleanupBatchSize {
			return processed, nil
		}
	}
}

func (s *Service) RetryWorktreeCleanup(ctx context.Context, id domain.ID) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		current, err := s.repo.Find(ctx, id)
		if err != nil {
			return fmt.Errorf("find session for worktree cleanup retry: %w", err)
		}
		if current.Status != domain.StatusClosed || current.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || !current.WorktreeCleanup.Retryable {
			return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session worktree cleanup cannot be retried").WithDetails(map[string]any{
				"sessionId": string(id),
				"status":    string(current.WorktreeCleanup.Status),
			})
		}
		if err := current.RequestWorktreeCleanup(s.now()); err != nil {
			return err
		}
		if err := s.saveSessionWithEvent(ctx, current, "session.worktree_cleanup_requested", map[string]any{
			"reason":         "user_retry",
			"worktreeBranch": current.WorktreeBranch,
		}); err != nil {
			return err
		}
		dto = toDTO(current)
		return nil
	})
	if err != nil {
		return DTO{}, err
	}
	s.scheduleWorktreeCleanup()
	return dto, nil
}

func (s *Service) cleanupSessionWorktree(ctx context.Context, session domain.Session) error {
	if session.Status != domain.StatusClosed && session.Status != domain.StatusFailed {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_cleanup_state_invalid", "worktree cleanup requires a closed or failed session; no resources were deleted", false)
	}
	expectedPath := strings.TrimSpace(s.worktrees.PathForSession(session.ProjectID, session.ID))
	if expectedPath == "" || strings.TrimSpace(session.WorktreePath) != expectedPath {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_path_mismatch", "persisted worktree path does not match the managed session path", false)
	}
	if strings.TrimSpace(session.WorktreeBranch) != worktreeBranchName(session.ID) || session.WorktreeBranch == strings.TrimSpace(session.BaseBranch) {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_branch_mismatch", "persisted worktree branch is not owned by the session", false)
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
	if err != nil {
		return s.recordWorktreeCleanupFailure(ctx, session, "project_lookup_failed", err.Error(), true)
	}
	if !project.IsGit {
		return s.recordWorktreeCleanupFailure(ctx, session, "project_not_git", "session owns a worktree but project is not a git repository", false)
	}
	ownership, err := s.worktrees.InspectOwnership(ctx, project.Path.Value, session.WorktreePath, session.WorktreeBranch, session.WorktreeCleanup.OwnershipToken)
	if err != nil {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_inspect_failed", err.Error(), true)
	}
	noResources := !ownership.PathExists && !ownership.BranchExists && !ownership.Registered
	if !ownership.TokenMatches {
		if noResources && !ownership.MarkerExists {
			return s.completeWorktreeCleanup(ctx, session)
		}
		code := "worktree_ownership_unconfirmed"
		message := "worktree ownership token could not be confirmed; no resources were deleted"
		if session.WorktreeCleanup.OwnershipConfirmedAt != nil {
			code = "worktree_ownership_changed"
			message = "managed worktree ownership marker no longer matches; no resources were deleted"
		}
		return s.recordWorktreeCleanupFailure(ctx, session, code, message, false)
	}
	if session.WorktreeCleanup.OwnershipConfirmedAt == nil {
		if noResources {
			if err := s.worktrees.ReleaseOwnership(ctx, session.WorktreePath, session.WorktreeCleanup.OwnershipToken); err != nil {
				return s.recordWorktreeCleanupFailure(ctx, session, "worktree_ownership_release_failed", err.Error(), true)
			}
			return s.completeWorktreeCleanup(ctx, session)
		}
		if !ownership.Matches {
			return s.recordWorktreeCleanupFailure(ctx, session, "worktree_ownership_unconfirmed", "worktree is not linked to the persisted project, path, and branch; no resources were deleted", false)
		}
		now := s.now()
		if err := session.ConfirmWorktreeOwnership(now); err != nil {
			return err
		}
		if err := s.saveSessionWithEvent(ctx, session, "session.worktree_ownership_confirmed", map[string]any{
			"worktreeBranch": session.WorktreeBranch,
		}); err != nil {
			return err
		}
	}
	if ownership.PathExists && !ownership.Matches {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_ownership_changed", "managed worktree path no longer matches the persisted branch; no resources were deleted", false)
	}
	if ownership.PathExists {
		if err := s.worktrees.Remove(ctx, session.WorktreePath); err != nil {
			return s.recordWorktreeCleanupFailure(ctx, session, "worktree_remove_failed", err.Error(), true)
		}
	}
	if err := s.worktrees.DeleteBranch(ctx, project.Path.Value, session.WorktreeBranch); err != nil {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_branch_delete_failed", err.Error(), true)
	}
	if err := s.worktrees.ReleaseOwnership(ctx, session.WorktreePath, session.WorktreeCleanup.OwnershipToken); err != nil {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_ownership_release_failed", err.Error(), true)
	}
	return s.completeWorktreeCleanup(ctx, session)
}

func (s *Service) completeWorktreeCleanup(ctx context.Context, session domain.Session) error {
	if err := session.CompleteWorktreeCleanup(s.now()); err != nil {
		return err
	}
	return s.saveSessionWithEvent(ctx, session, "session.worktree_cleanup_completed", map[string]any{
		"attempts":       session.WorktreeCleanup.Attempts,
		"worktreeBranch": session.WorktreeBranch,
	})
}

func (s *Service) recordWorktreeCleanupFailure(ctx context.Context, session domain.Session, code string, message string, retryable bool) error {
	now := s.now()
	var nextAt *time.Time
	if retryable {
		next := now.Add(worktreeCleanupRetryDelay(session.WorktreeCleanup.Attempts + 1))
		nextAt = &next
	}
	if err := session.FailWorktreeCleanup(code, message, retryable, nextAt, now); err != nil {
		return err
	}
	return s.saveSessionWithEvent(ctx, session, "session.worktree_cleanup_failed", map[string]any{
		"attempts":       session.WorktreeCleanup.Attempts,
		"code":           code,
		"error":          message,
		"retryable":      retryable,
		"nextAttemptAt":  nextAt,
		"worktreeBranch": session.WorktreeBranch,
	})
}

func worktreeCleanupDue(session domain.Session, now time.Time) bool {
	switch session.WorktreeCleanup.Status {
	case domain.WorktreeCleanupPending:
		return true
	case domain.WorktreeCleanupFailed:
		return session.WorktreeCleanup.Retryable && session.WorktreeCleanup.NextAt != nil && !session.WorktreeCleanup.NextAt.After(now)
	default:
		return false
	}
}

func worktreeCleanupRetryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}
	if attempt <= 1 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}

func (s *Service) scheduleQueueDrain() {
	if s == nil || s.queueDrainScheduler == nil {
		return
	}
	s.queueDrainScheduler(s)
}

const restartRecoveryPrompt = "Continue the interrupted task after the AnyCode service restart. Inspect the current state before changing files, preserve completed work, and continue from the same task."

func (s *Service) RecoverInterruptedSessions(ctx context.Context) (int, error) {
	if s == nil {
		return 0, errors.New("session usecase: nil service")
	}
	systemAdvanceSessions, err := s.recoverAllPendingSystemAdvances(ctx)
	if err != nil {
		return 0, err
	}
	answerSessions, err := s.recoverAnswerUserSessions(ctx)
	if err != nil {
		return 0, err
	}
	withCodexSession, err := s.repo.ListInterruptedWithCodexSession(ctx)
	if err != nil {
		return 0, fmt.Errorf("list interrupted sessions: %w", err)
	}
	withoutCodexSession, err := s.listInterruptedWithoutCodexSession(ctx)
	if err != nil {
		return 0, err
	}
	sessions := append(withCodexSession, withoutCodexSession...)
	seen := make(map[domain.ID]struct{}, len(sessions))
	recovered := len(answerSessions) + len(systemAdvanceSessions)
	for _, interrupted := range sessions {
		if systemAdvanceSessions[interrupted.ID] {
			continue
		}
		if answerSessions[interrupted.ID] {
			continue
		}
		if _, ok := seen[interrupted.ID]; ok {
			continue
		}
		seen[interrupted.ID] = struct{}{}
		var activeSnapshot *processdomain.Run
		if s.processes != nil {
			active, found, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(interrupted.ID))
			if err != nil {
				return recovered, fmt.Errorf("snapshot interrupted process for session %s: %w", interrupted.ID, err)
			}
			if found {
				activeSnapshot = &active
			}
		}
		if err := s.withSessionLock(ctx, interrupted.ID, func(ctx context.Context) error {
			return s.recoverInterruptedSession(ctx, interrupted.ID, activeSnapshot)
		}); err != nil {
			return recovered, fmt.Errorf("recover interrupted session %s: %w", interrupted.ID, err)
		}
		recovered++
	}
	return recovered, nil
}

func (s *Service) recoverAllPendingSystemAdvances(ctx context.Context) (map[domain.ID]bool, error) {
	handled := map[domain.ID]bool{}
	if s.events == nil {
		return handled, nil
	}
	sessionIDs := []domain.ID{}
	for page := 1; ; page++ {
		sessions, total, err := s.repo.ListCards(ctx, domain.ListQuery{Page: page, PageSize: maxPageSize, Sort: "updated_at asc"})
		if err != nil {
			return nil, fmt.Errorf("list sessions for pending system advances: %w", err)
		}
		for _, session := range sessions {
			if session.Mode != domain.ModeWorkflow {
				continue
			}
			sessionIDs = append(sessionIDs, session.ID)
		}
		if page*maxPageSize >= total || len(sessions) == 0 {
			break
		}
	}
	for _, sessionID := range sessionIDs {
		commands, err := s.pendingSystemAdvances(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if len(commands) == 0 {
			continue
		}
		if err := s.withSessionLock(ctx, sessionID, func(ctx context.Context) error {
			session, err := s.repo.Find(ctx, sessionID)
			if err != nil {
				return err
			}
			_, err = s.recoverPendingSystemAdvance(ctx, session)
			return err
		}); err != nil {
			return nil, err
		}
		handled[sessionID] = true
	}
	return handled, nil
}

func (s *Service) MarkInterruptedSessionsRecoverable(ctx context.Context) (int, error) {
	return s.RecoverInterruptedSessions(ctx)
}

func (s *Service) recoverInterruptedSession(ctx context.Context, sessionID domain.ID, activeSnapshot *processdomain.Run) error {
	session, err := s.repo.Find(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("find interrupted session: %w", err)
	}
	if !isInterruptedRecoveryStatus(session) {
		return nil
	}
	var currentActive processdomain.Run
	hasCurrentActive := false
	if s.processes != nil {
		currentActive, hasCurrentActive, err = s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
		if err != nil {
			return fmt.Errorf("find interrupted process run: %w", err)
		}
	}
	if activeSnapshot == nil && hasCurrentActive {
		return nil
	}
	if activeSnapshot != nil && hasCurrentActive && currentActive.ID != activeSnapshot.ID {
		return nil
	}
	if activeSnapshot != nil && hasCurrentActive {
		if err := s.stopDetachedProcess(ctx, *activeSnapshot); err != nil {
			return s.persistInterruptedRecoveryFailure(ctx, session, "detached_process_ownership_unverified", err.Error())
		}
	}
	interruptedRun := activeSnapshot
	if session.Status == domain.StatusStopping && session.CloseReason != nil {
		if interruptedRun != nil {
			if err := s.settleInterruptedRun(ctx, session, *interruptedRun); err != nil {
				return err
			}
		}
		_, err := s.closeSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: *session.CloseReason})
		return err
	}
	if session.Mode == domain.ModeWorkflow && s.events != nil {
		recovered, err := s.recoverPendingSystemAdvance(ctx, session)
		if err != nil {
			return fmt.Errorf("recover pending workflow system advance: %w", err)
		}
		if recovered {
			return nil
		}
	}
	if session.Mode == domain.ModeWorkflow && s.workflows != nil && s.events != nil {
		_, checkpoint, err := s.latestWorkflowProcessExitInput(ctx, session.ID)
		if err != nil {
			return err
		}
		if checkpoint {
			if interruptedRun != nil {
				if err := s.settleInterruptedRun(ctx, session, *interruptedRun); err != nil {
					return err
				}
			}
			workflowRecovered, err := s.recoverWorkflowProcessExit(ctx, session, s.now())
			if err != nil {
				return fmt.Errorf("recover workflow process exit: %w", err)
			}
			if workflowRecovered {
				return nil
			}
		}
	}
	switch session.Status {
	case domain.StatusStopping:
		return s.persistStoppedAfterRestart(ctx, session, interruptedRun)
	case domain.StatusStarting, domain.StatusRunning:
		if strings.TrimSpace(session.CodexSessionID) == "" {
			return s.persistInterruptedRecoveryFailureWithRun(ctx, session, interruptedRun, "service_restarted_without_codex_session_id", "interrupted session has no Codex session id")
		}
		return s.queueInterruptedSessionResume(ctx, session, interruptedRun)
	case domain.StatusWaitingUser:
		return s.recoverInternalWaitingUserSession(ctx, session, interruptedRun)
	case domain.StatusQueued:
		if session.Queue.Kind == domain.QueueKindAnswerUser {
			return s.recoverInternalWaitingUserSession(ctx, session, interruptedRun)
		}
	}
	return nil
}

func (s *Service) settleInterruptedRun(ctx context.Context, session domain.Session, run processdomain.Run) error {
	result := processdomain.ExitResult{FailureReason: "service_restarted", FinishedAt: s.now()}
	return s.markProcessExitedWithSessionEventsAndSettlement(ctx, run.ID, result, session, false, []sessionEventInput{{
		eventType: "process.exited",
		payload:   processExitPayload(run.ID, result),
	}}, promptAppendSettlementRelease)
}

func isInterruptedRecoveryStatus(session domain.Session) bool {
	switch session.Status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping:
		return true
	case domain.StatusQueued:
		return session.Queue.Kind == domain.QueueKindAnswerUser
	default:
		return false
	}
}

func (s *Service) recoverAnswerUserSessions(ctx context.Context) (map[domain.ID]bool, error) {
	handled := map[domain.ID]bool{}
	if s.uow == nil {
		return handled, nil
	}
	var batches []questiondomain.Batch
	if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo, ok := tx.Questions().(questiondomain.AgentRepository)
		if !ok {
			return nil
		}
		var err error
		batches, err = repo.ListAgentBatchesForRecovery(ctx)
		return err
	}); err != nil {
		return nil, fmt.Errorf("list answer_user recovery batches: %w", err)
	}
	for _, batch := range batches {
		if isInternalQuestionBatch(batch) {
			continue
		}
		sessionID := domain.ID(batch.SessionID)
		var recovered bool
		err := s.withSessionLock(ctx, sessionID, func(ctx context.Context) error {
			var err error
			recovered, err = s.recoverAnswerUserBatch(ctx, batch.ID)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("recover answer_user batch %s: %w", batch.ID, err)
		}
		if recovered {
			handled[sessionID] = true
		}
	}
	return handled, nil
}

func (s *Service) recoverAnswerUserBatch(ctx context.Context, batchID questiondomain.BatchID) (bool, error) {
	var result questiondomain.Batch
	var events []eventdomain.DomainEvent
	preparedCloseRecovery := false
	manualWorkflowRecovery := false
	workflowRecoveryCode := ""
	workflowRecoveryMessage := ""
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo, ok := tx.Questions().(questiondomain.AgentRepository)
		if !ok {
			return nil
		}
		batch, err := repo.FindBatch(ctx, batchID)
		if err != nil {
			return err
		}
		session, err := tx.Sessions().Find(ctx, domain.ID(batch.SessionID))
		if err != nil {
			return err
		}
		if session.Status == domain.StatusClosed || session.Status == domain.StatusStopping || (session.Status == domain.StatusStopped && batch.OriginProcessRunID != nil) {
			active, found, err := tx.Processes().FindActiveBySession(ctx, processdomain.SessionID(session.ID))
			if err != nil {
				return err
			}
			if found {
				exitResult := processdomain.ExitResult{FailureReason: "service restarted after session stop", FinishedAt: s.now()}
				if err := tx.Processes().MarkExited(ctx, active.ID, exitResult); err != nil {
					return err
				}
				if err := tx.Sessions().ReleasePromptAppends(ctx, string(active.ID)); err != nil {
					return err
				}
				event, ok, err := s.newSessionEvent(session, "process.exited", processExitPayload(active.ID, exitResult))
				if err != nil {
					return err
				}
				if ok {
					if err := tx.Events().Append(ctx, event); err != nil {
						return err
					}
					events = append(events, event)
				}
			}
			if session.Status == domain.StatusStopping && session.CloseReason != nil {
				preparedCloseRecovery = true
			} else if session.Status == domain.StatusStopping {
				if err := transitionSession(&session, domain.StatusStopped, s.now()); err != nil {
					return err
				}
				if err := tx.Sessions().Save(ctx, session); err != nil {
					return err
				}
				event, ok, err := s.newSessionEvent(session, "session.stopped", map[string]any{"reason": "service_restarted"})
				if err != nil {
					return err
				}
				if ok {
					if err := tx.Events().Append(ctx, event); err != nil {
						return err
					}
					events = append(events, event)
				}
			}
			switch batch.Status {
			case questiondomain.BatchPending:
				cancelled, err := repo.CancelPendingBySession(ctx, batch.SessionID, "session stopped before answer_user recovery")
				if err != nil {
					return err
				}
				for _, item := range cancelled {
					if item.ID == batch.ID {
						result = item
						break
					}
				}
			case questiondomain.BatchAnswered:
				cancelled, err := repo.CancelUndeliveredBySession(ctx, batch.SessionID)
				if err != nil {
					return err
				}
				for _, item := range cancelled {
					if item.ID == batch.ID {
						result = item
						break
					}
				}
			}
			return nil
		}
		if batch.OriginProcessRunID == nil {
			active, found, err := tx.Processes().FindActiveBySession(ctx, processdomain.SessionID(session.ID))
			if err != nil {
				return err
			}
			origin := active
			originFound := found && strings.TrimSpace(active.CodexSessionID) != ""
			if !originFound {
				finder, ok := tx.Processes().(processdomain.HistoricalRunFinder)
				if !ok {
					return errors.New("historical process run finder is required for legacy answer_user recovery")
				}
				origin, originFound, err = finder.FindLatestRunBySessionBefore(ctx, processdomain.SessionID(session.ID), batch.CreatedAt)
				if err != nil {
					return err
				}
				originFound = originFound && strings.TrimSpace(origin.CodexSessionID) != ""
			}
			if originFound {
				originID := questiondomain.ProcessRunID(origin.ID)
				if err := repo.SetOriginProcessRun(ctx, batch.ID, originID); err != nil {
					return err
				}
				batch.OriginProcessRunID = &originID
			} else {
				switch batch.Status {
				case questiondomain.BatchPending:
					cancelled, _, err := repo.CancelPendingBatch(ctx, batch.ID, "legacy answer_user origin could not be recovered")
					if err != nil {
						return err
					}
					result = cancelled
				case questiondomain.BatchAnswered:
					cancelled, err := repo.CancelUndeliveredBySession(ctx, batch.SessionID)
					if err != nil {
						return err
					}
					for _, item := range cancelled {
						if item.ID == batch.ID {
							result = item
							break
						}
					}
				}
				if err := transitionSession(&session, domain.StatusResumeFailed, s.now()); err != nil {
					return err
				}
				event, ok, err := s.newSessionEvent(session, "session.resume_failed", map[string]any{
					"reason": "legacy_answer_user_origin_ambiguous", "batchId": string(batch.ID),
				})
				if err != nil {
					return err
				}
				if err := tx.Sessions().Save(ctx, session); err != nil {
					return err
				}
				if ok {
					if err := tx.Events().Append(ctx, event); err != nil {
						return err
					}
					events = append(events, event)
				}
				manualWorkflowRecovery = session.Mode == domain.ModeWorkflow
				workflowRecoveryCode = "legacy_answer_user_origin_ambiguous"
				workflowRecoveryMessage = "answer_user origin process could not be recovered"
				if result.ID == "" {
					result = batch
				}
				return nil
			}
		}
		finder, ok := tx.Processes().(processdomain.RunFinder)
		if !ok {
			return errors.New("process run finder is required for answer_user recovery")
		}
		origin, err := finder.FindRun(ctx, processdomain.RunID(*batch.OriginProcessRunID))
		if err != nil {
			return err
		}
		if session.CodexSessionID == "" {
			session.CodexSessionID = origin.CodexSessionID
		}
		switch batch.Status {
		case questiondomain.BatchPending:
			if processRunIsActive(origin.Status) {
				if err := tx.Processes().MarkExited(ctx, origin.ID, processdomain.ExitResult{FailureReason: "service restarted while suspended for user", FinishedAt: s.now()}); err != nil {
					return err
				}
				if err := tx.Sessions().CompletePromptAppends(ctx, string(origin.ID), s.now()); err != nil {
					return err
				}
			}
			if err := transitionSession(&session, domain.StatusWaitingUser, s.now()); err != nil {
				return err
			}
			if err := markWorkflowNodeWaiting(ctx, tx.Workflows(), batch, origin); err != nil {
				return err
			}
			event, ok, err := s.newSessionEvent(session, "process.suspended_for_user", map[string]any{
				"processRunId": string(origin.ID), "batchId": string(batch.ID), "reason": "service_restarted",
			})
			if err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return err
			}
			if ok {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
				events = append(events, event)
			}
		case questiondomain.BatchAnswered:
			if batch.DeliveryStatus == questiondomain.DeliveryInflight {
				deliveryRun, found, err := tx.Processes().FindActiveBySession(ctx, processdomain.SessionID(session.ID))
				if err != nil {
					return err
				}
				if found {
					exitResult := processdomain.ExitResult{FailureReason: "service restarted before answer delivery", FinishedAt: s.now()}
					if err := tx.Processes().MarkExited(ctx, deliveryRun.ID, exitResult); err != nil {
						return err
					}
					if err := tx.Sessions().ReleasePromptAppends(ctx, string(deliveryRun.ID)); err != nil {
						return err
					}
					event, ok, err := s.newSessionEvent(session, "process.exited", processExitPayload(deliveryRun.ID, exitResult))
					if err != nil {
						return err
					}
					if ok {
						if err := tx.Events().Append(ctx, event); err != nil {
							return err
						}
						events = append(events, event)
					}
				}
				if err := repo.ResetDeliveryAwaitingResume(ctx, batch.ID); err != nil {
					return err
				}
				batch.DeliveryStatus = questiondomain.DeliveryAwaitingResume
				batch.DeliveryProcessRunID = nil
			}
			if session.Status == domain.StatusResumeFailed {
				manualWorkflowRecovery = session.Mode == domain.ModeWorkflow
				workflowRecoveryCode = "answer_delivery_resume_failed"
				workflowRecoveryMessage = "answer_user delivery requires a resume action"
				result = batch
				return nil
			}
			if strings.TrimSpace(origin.CodexSessionID) == "" {
				if err := transitionSession(&session, domain.StatusResumeFailed, s.now()); err != nil {
					return err
				}
				event, ok, err := s.newSessionEvent(session, "session.resume_failed", map[string]any{
					"reason": "answer_user_origin_missing_codex_session", "batchId": string(batch.ID),
				})
				if err != nil {
					return err
				}
				if err := tx.Sessions().Save(ctx, session); err != nil {
					return err
				}
				if ok {
					if err := tx.Events().Append(ctx, event); err != nil {
						return err
					}
					events = append(events, event)
				}
				manualWorkflowRecovery = session.Mode == domain.ModeWorkflow
				workflowRecoveryCode = "answer_user_origin_missing_codex_session"
				workflowRecoveryMessage = "answer_user origin process has no Codex session id"
				break
			}
			options := answerResumeOptions(batch, origin)
			queued, queuedEvent, hasQueuedEvent, err := s.prepareQueuedSession(session, options, domain.QueuePriorityHigh, domain.QueueKindAnswerUser)
			if err != nil {
				return err
			}
			if err := markWorkflowNodeWaiting(ctx, tx.Workflows(), batch, origin); err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, queued); err != nil {
				return err
			}
			if hasQueuedEvent {
				if err := tx.Events().Append(ctx, queuedEvent); err != nil {
					return err
				}
				events = append(events, queuedEvent)
			}
		}
		result = batch
		return nil
	})
	if err != nil {
		return false, err
	}
	for _, event := range events {
		s.publishSessionEvent(ctx, event)
	}
	if result.ID != "" {
		s.publishQuestionBatch(questionBatchDTO(result))
	}
	if manualWorkflowRecovery && s.workflows != nil {
		_, err := s.workflows.MarkResumeFailedForSession(ctx, domain.WorkflowResumeFailureInput{
			SessionID: domain.ID(result.SessionID), Code: workflowRecoveryCode, Message: workflowRecoveryMessage,
		})
		if err != nil {
			return false, err
		}
	}
	return result.ID != "" && !preparedCloseRecovery, nil
}

func markWorkflowNodeWaiting(ctx context.Context, repo workflowdomain.Repository, batch questiondomain.Batch, origin processdomain.Run) error {
	if batch.WorkflowRunID == nil || origin.NodeRunID == nil {
		return nil
	}
	nodes, ok := repo.(workflowdomain.NodeExecutionRepository)
	if !ok {
		return errors.New("workflow node execution repository is required")
	}
	return nodes.MarkNodeWaitingUser(ctx, workflowdomain.RunID(*batch.WorkflowRunID), workflowdomain.NodeRunID(*origin.NodeRunID))
}

func processRunIsActive(status processdomain.Status) bool {
	switch status {
	case processdomain.StatusStarting, processdomain.StatusRunning, processdomain.StatusWaitingUser, processdomain.StatusStopping:
		return true
	default:
		return false
	}
}

func (s *Service) stopDetachedProcess(ctx context.Context, run processdomain.Run) error {
	if run.PID == nil || *run.PID <= 0 {
		return fmt.Errorf("%w: process run %s has no persisted pid", processdomain.ErrProcessOwnershipUnverified, run.ID)
	}
	controller, ok := s.codex.(processdomain.DetachedProcessController)
	if !ok {
		return fmt.Errorf("%w: detached process controller is unavailable", processdomain.ErrProcessOwnershipUnverified)
	}
	cleanupCtx, cancel := detachedCleanupContext(ctx)
	defer cancel()
	return controller.StopDetached(cleanupCtx, processdomain.DetachedProcess{ProcessRunID: run.ID, PID: *run.PID})
}

func (s *Service) persistStoppedAfterRestart(ctx context.Context, session domain.Session, run *processdomain.Run) error {
	expected := session
	previousStatus := session.Status
	if err := transitionSession(&session, domain.StatusStopped, s.now()); err != nil {
		return err
	}
	return s.saveInterruptedRecoveryState(ctx, expected, session, run, true, "session.stopped", map[string]any{
		"reason":         "service_restarted_while_stopping",
		"previousStatus": string(previousStatus),
	})
}

func (s *Service) persistInterruptedRecoveryFailure(ctx context.Context, session domain.Session, code string, message string) error {
	return s.persistInterruptedRecoveryFailureWithRun(ctx, session, nil, code, message)
}

func (s *Service) persistInterruptedRecoveryFailureWithRun(ctx context.Context, session domain.Session, run *processdomain.Run, code string, message string) error {
	expected := session
	previousStatus := session.Status
	if err := transitionSession(&session, domain.StatusResumeFailed, s.now()); err != nil {
		return err
	}
	if err := s.saveInterruptedRecoveryState(ctx, expected, session, run, run != nil, "session.resume_failed", map[string]any{
		"reason":         code,
		"message":        message,
		"previousStatus": string(previousStatus),
	}); err != nil {
		return err
	}
	if session.Mode == domain.ModeWorkflow && s.workflows != nil {
		if _, err := s.workflows.MarkResumeFailedForSession(ctx, domain.WorkflowResumeFailureInput{
			SessionID: session.ID,
			Code:      code,
			Message:   message,
		}); err != nil {
			return fmt.Errorf("mark workflow waiting resume action: %w", err)
		}
	}
	return nil
}

func (s *Service) queueInterruptedSessionResume(ctx context.Context, session domain.Session, run *processdomain.Run) error {
	options, err := s.recoveryCodexOptions(ctx, session, restartRecoveryPrompt)
	if err != nil {
		return s.persistInterruptedRecoveryFailureWithRun(ctx, session, run, "recovery_prepare_failed", err.Error())
	}
	if run != nil {
		options.resumeOfProcessRunID = run.ID
	}
	queued, event, hasEvent, err := s.prepareQueuedSession(session, options, queuePriorityForSession(session), domain.QueueKindResume)
	if err != nil {
		return err
	}
	return s.saveInterruptedRecoveryStateAndEvent(ctx, session, queued, run, true, event, hasEvent)
}

func (s *Service) recoveryCodexOptions(ctx context.Context, session domain.Session, prompt string) (codexStartOptions, error) {
	options := codexStartOptions{
		resumeCodexSessionID: strings.TrimSpace(session.CodexSessionID),
		prompt:               strings.TrimSpace(prompt),
	}
	if session.Mode != domain.ModeWorkflow {
		return options, nil
	}
	if s.workflows == nil {
		return codexStartOptions{}, errors.New("session workflow starter is required for workflow recovery")
	}
	advance, err := s.workflows.ResumeCurrentNodeForSession(ctx, domain.WorkflowResumeCurrentNodeInput{
		SessionID: session.ID,
		Reason:    "service restarted",
	})
	if err != nil {
		return codexStartOptions{}, fmt.Errorf("resume workflow current node: %w", err)
	}
	options.workflowRunID = advance.WorkflowRunID
	options.nodeRunID = workflowNodeRunID(advance.NodeRunID)
	if options.prompt == "" {
		options.prompt = advance.Prompt
	}
	return options, nil
}

func (s *Service) recoverInternalWaitingUserSession(ctx context.Context, session domain.Session, run *processdomain.Run) error {
	batch, ok, err := s.latestQuestionBatch(ctx, session.ID)
	if err != nil {
		return err
	}
	if !ok || !isInternalQuestionBatch(batch) {
		return s.persistInterruptedRecoveryFailureWithRun(ctx, session, run, "answer_user_recovery_missing", "interrupted waiting-user session has no recoverable question batch")
	}
	if batch.Status == questiondomain.BatchPending {
		if run == nil && session.Status == domain.StatusWaitingUser {
			return nil
		}
		expected := session
		if err := transitionSession(&session, domain.StatusWaitingUser, s.now()); err != nil {
			return err
		}
		return s.saveInterruptedRecoveryState(ctx, expected, session, run, true, "session.recovery_waiting_user", map[string]any{
			"batchId": string(batch.ID),
			"reason":  "service_restarted",
		})
	}
	if batch.Status != questiondomain.BatchAnswered {
		return s.persistInterruptedRecoveryFailureWithRun(ctx, session, run, "answer_user_recovery_missing", "interrupted question batch is no longer recoverable")
	}
	if run != nil {
		if err := s.settleInterruptedRun(ctx, session, *run); err != nil {
			return err
		}
	}
	action, metadata, _, err := mergeFailureDecision(questionBatchDTO(batch))
	if err != nil {
		return err
	}
	return s.applyMergeFailureDecision(ctx, session, batch.ID, action, metadata)
}

func (s *Service) latestQuestionBatch(ctx context.Context, sessionID domain.ID) (questiondomain.Batch, bool, error) {
	if s.uow == nil {
		return questiondomain.Batch{}, false, errors.New("unit of work is required for interrupted question recovery")
	}
	var batch questiondomain.Batch
	var found bool
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo, ok := tx.Questions().(questiondomain.AgentRepository)
		if !ok {
			return errors.New("question recovery repository is required")
		}
		var err error
		batch, found, err = repo.FindLatestBySession(ctx, questiondomain.SessionID(sessionID))
		return err
	})
	if err != nil {
		return questiondomain.Batch{}, false, fmt.Errorf("find latest question batch: %w", err)
	}
	return batch, found, nil
}

func (s *Service) saveInterruptedRecoveryState(ctx context.Context, expected domain.Session, session domain.Session, run *processdomain.Run, guardReplacement bool, eventType string, payload map[string]any) error {
	event, hasEvent, err := s.newSessionEvent(session, eventType, payload)
	if err != nil {
		return err
	}
	return s.saveInterruptedRecoveryStateAndEvent(ctx, expected, session, run, guardReplacement, event, hasEvent)
}

func (s *Service) saveInterruptedRecoveryStateAndEvent(ctx context.Context, expected domain.Session, session domain.Session, run *processdomain.Run, guardReplacement bool, event eventdomain.DomainEvent, hasEvent bool) error {
	result := processdomain.ExitResult{}
	events := make([]eventdomain.DomainEvent, 0, 2)
	superseded := false
	if run != nil {
		result = processdomain.ExitResult{FailureReason: "service_restarted", FinishedAt: s.now()}
		processEvent, ok, err := s.newSessionEvent(session, "process.exited", processExitPayload(run.ID, result))
		if err != nil {
			return err
		}
		if ok {
			events = append(events, processEvent)
		}
	}
	if hasEvent {
		events = append(events, event)
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			current, err := tx.Sessions().Find(ctx, expected.ID)
			if err != nil {
				return fmt.Errorf("find session during interrupted recovery: %w", err)
			}
			if !current.MatchesLifecycleSnapshot(expected) {
				superseded = true
				return nil
			}
			active, found, err := tx.Processes().FindActiveBySession(ctx, processdomain.SessionID(session.ID))
			if err != nil {
				return fmt.Errorf("find replacement process during recovery: %w", err)
			}
			if guardReplacement && found && (run == nil || active.ID != run.ID) {
				superseded = true
				return nil
			}
			if run != nil {
				if err := tx.Processes().MarkExited(ctx, run.ID, result); err != nil {
					return fmt.Errorf("mark interrupted process exited: %w", err)
				}
				if err := tx.Sessions().ReleasePromptAppends(ctx, string(run.ID)); err != nil {
					return err
				}
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save recovery session: %w", err)
			}
			for _, event := range events {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	} else {
		current, err := s.repo.Find(ctx, expected.ID)
		if err != nil {
			return fmt.Errorf("find session during interrupted recovery: %w", err)
		}
		if !current.MatchesLifecycleSnapshot(expected) {
			return nil
		}
		if s.processes != nil {
			active, found, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
			if err != nil {
				return fmt.Errorf("find replacement process during recovery: %w", err)
			}
			if guardReplacement && found && (run == nil || active.ID != run.ID) {
				return nil
			}
		}
		if run != nil {
			if err := s.processes.MarkExited(ctx, run.ID, result); err != nil {
				return fmt.Errorf("mark interrupted process exited: %w", err)
			}
			if err := s.repo.ReleasePromptAppends(ctx, string(run.ID)); err != nil {
				return err
			}
		}
		if err := s.repo.Save(ctx, session); err != nil {
			return fmt.Errorf("save recovery session: %w", err)
		}
		for _, event := range events {
			if s.events != nil {
				if err := s.events.Append(ctx, event); err != nil {
					return err
				}
			}
		}
	}
	if superseded {
		return nil
	}
	for _, event := range events {
		s.publishSessionEvent(ctx, event)
	}
	return nil
}

func isInternalQuestionBatch(batch questiondomain.Batch) bool {
	for _, question := range batch.Questions {
		if question.Type == "merge_failure_action" {
			return true
		}
		if kind, _ := question.Metadata["kind"].(string); kind == "merge_failure_action" {
			return true
		}
	}
	return false
}

func (s *Service) recoverWorkflowProcessExit(ctx context.Context, session domain.Session, now time.Time) (bool, error) {
	if session.Mode != domain.ModeWorkflow || s.workflows == nil || s.events == nil {
		return false, nil
	}
	if s.processes != nil {
		_, active, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
		if err != nil {
			return false, err
		}
		if active {
			return false, nil
		}
	}
	input, ok, err := s.latestWorkflowProcessExitInput(ctx, session.ID)
	if err != nil || !ok {
		return false, err
	}
	advance, err := s.workflows.RecoverProcessExit(ctx, input)
	if err != nil {
		return true, err
	}
	if advance.Status == "failed" {
		if err := transitionSession(&session, domain.StatusFailed, now); err != nil {
			return true, err
		}
		return true, s.saveInterruptedSessionWithEvent(ctx, session, now, "workflow_process_failed", "workflow.failed", map[string]any{
			"reason":        "workflow_process_failed",
			"workflowRunId": string(advance.WorkflowRunID),
		})
	}
	if advance.Status == "waiting_resume_action" {
		return true, s.saveWorkflowExitResumeFailed(ctx, session, now, advance, false)
	}
	if workflowAdvanceHasExternalEffects(advance) {
		if _, err := s.workflows.MarkResumeFailedForSession(ctx, domain.WorkflowResumeFailureInput{
			SessionID: session.ID,
			Code:      "workflow_advance_interrupted",
			Message:   "service restarted before workflow advance side effect was durably completed",
		}); err != nil {
			return true, err
		}
		return true, s.saveWorkflowExitResumeFailed(ctx, session, now, advance, true)
	}
	_, err = s.applyWorkflowAdvance(ctx, session, advance, workflowAdvanceOptions{})
	return true, err
}

func (s *Service) saveWorkflowExitResumeFailed(ctx context.Context, session domain.Session, now time.Time, advance domain.WorkflowAdvance, markedWorkflow bool) error {
	previousStatus := session.Status
	if err := transitionSession(&session, domain.StatusResumeFailed, now); err != nil {
		return err
	}
	session.CodexSessionID = ""
	return s.saveInterruptedSessionWithEvent(ctx, session, now, "workflow_advance_interrupted", "session.resume_failed", map[string]any{
		"reason":            "workflow_advance_interrupted",
		"previousStatus":    string(previousStatus),
		"workflowRunId":     string(advance.WorkflowRunID),
		"nodeRunId":         stringValuePtr(advance.NodeRunID),
		"workflowRunMarked": markedWorkflow,
	})
}

func (s *Service) latestWorkflowProcessExitInput(ctx context.Context, sessionID domain.ID) (domain.WorkflowProcessExitInput, bool, error) {
	eventSessionID := eventdomain.SessionID(sessionID)
	events, err := s.events.List(ctx, eventdomain.Scope{SessionID: &eventSessionID})
	if err != nil {
		return domain.WorkflowProcessExitInput{}, false, err
	}
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		if event.Type != "workflow.exit_pending" || event.SessionID == nil || *event.SessionID != eventSessionID {
			continue
		}
		input, err := workflowProcessExitInputFromPayload(event.Payload)
		if err != nil {
			return domain.WorkflowProcessExitInput{}, false, err
		}
		return input, true, nil
	}
	return domain.WorkflowProcessExitInput{}, false, nil
}

func (s *Service) recoverPendingSystemAdvance(ctx context.Context, session domain.Session) (bool, error) {
	handled := false
	for {
		commands, err := s.pendingSystemAdvances(ctx, session.ID)
		if err != nil {
			return handled, err
		}
		if len(commands) == 0 {
			return handled, nil
		}
		pending := commands[0]
		current, findErr := s.repo.Find(ctx, session.ID)
		if findErr != nil {
			return handled, findErr
		}
		pending.session = current
		if err := s.executePendingSystemAdvance(ctx, pending); err != nil {
			return handled, err
		}
		handled = true
	}
}

func (s *Service) pendingSystemAdvances(ctx context.Context, sessionID domain.ID) ([]workflowApprovalPostCommitAdvance, error) {
	sessionEventID := eventdomain.SessionID(sessionID)
	events, err := s.events.List(ctx, eventdomain.Scope{SessionID: &sessionEventID})
	if err != nil {
		return nil, err
	}
	completed := map[string]bool{}
	for _, event := range events {
		if event.Type == workflowSystemAdvanceCompletedEvent {
			completed[strings.TrimSpace(stringFromMap(event.Payload, "commandEventId"))] = true
		}
	}
	commands := make([]workflowApprovalPostCommitAdvance, 0)
	for _, event := range events {
		if event.Type != workflowSystemAdvancePendingEvent || completed[string(event.ID)] {
			continue
		}
		advance, err := workflowAdvanceFromPendingPayload(event.Payload)
		if err != nil {
			return nil, err
		}
		commands = append(commands, workflowApprovalPostCommitAdvance{advance: advance, commandEventID: event.ID})
	}
	return commands, nil
}

func workflowAdvancePendingPayload(advance domain.WorkflowAdvance) map[string]any {
	payload := map[string]any{
		"workflowRunId":    string(advance.WorkflowRunID),
		"nodeRunId":        stringValuePtr(advance.NodeRunID),
		"currentNodeId":    advance.CurrentNodeID,
		"currentNodeTitle": advance.CurrentNodeTitle,
		"status":           advance.Status,
		"close":            advance.Close,
	}
	if advance.Merge != nil {
		payload["merge"] = map[string]any{"strategy": advance.Merge.Strategy}
	}
	if advance.Expr != nil {
		payload["expr"] = map[string]any{"script": advance.Expr.Script, "params": mapOrEmpty(advance.Expr.Params)}
	}
	return payload
}

func workflowAdvanceFromPendingPayload(payload map[string]any) (domain.WorkflowAdvance, error) {
	workflowRunID := strings.TrimSpace(stringFromMap(payload, "workflowRunId"))
	nodeRunID := strings.TrimSpace(stringFromMap(payload, "nodeRunId"))
	if workflowRunID == "" || nodeRunID == "" {
		return domain.WorkflowAdvance{}, errors.New("pending workflow system advance is missing workflow or node run id")
	}
	parsedNodeRunID := domain.NodeRunID(nodeRunID)
	advance := domain.WorkflowAdvance{
		WorkflowRunID:    domain.WorkflowRunID(workflowRunID),
		NodeRunID:        &parsedNodeRunID,
		CurrentNodeID:    strings.TrimSpace(stringFromMap(payload, "currentNodeId")),
		CurrentNodeTitle: strings.TrimSpace(stringFromMap(payload, "currentNodeTitle")),
		Status:           strings.TrimSpace(stringFromMap(payload, "status")),
	}
	advance.Close, _ = payload["close"].(bool)
	if merge, ok := payload["merge"].(map[string]any); ok {
		advance.Merge = &domain.WorkflowMerge{Strategy: strings.TrimSpace(stringFromMap(merge, "strategy"))}
	}
	if expr, ok := payload["expr"].(map[string]any); ok {
		advance.Expr = &domain.WorkflowExpr{Script: strings.TrimSpace(stringFromMap(expr, "script")), Params: mapOrEmpty(mapFromValue(expr["params"]))}
	}
	if !workflowAdvanceHasExternalEffects(advance) {
		return domain.WorkflowAdvance{}, errors.New("pending workflow system advance has no external effect")
	}
	return advance, nil
}

func mapFromValue(value any) map[string]any {
	mapped, _ := value.(map[string]any)
	return mapped
}

func (s *Service) listInterruptedWithoutCodexSession(ctx context.Context) ([]domain.Session, error) {
	statuses := []domain.Status{
		domain.StatusQueued,
		domain.StatusStarting,
		domain.StatusRunning,
		domain.StatusWaitingUser,
		domain.StatusStopping,
	}
	seen := map[domain.ID]bool{}
	result := []domain.Session{}
	for _, status := range statuses {
		for page := 1; ; page++ {
			rows, total, err := s.repo.ListCards(ctx, domain.ListQuery{
				Scope:    string(status),
				Page:     page,
				PageSize: maxPageSize,
				Sort:     "updated_at asc",
			})
			if err != nil {
				return nil, fmt.Errorf("list interrupted sessions without codex session id: %w", err)
			}
			for _, row := range rows {
				if seen[row.ID] || strings.TrimSpace(row.CodexSessionID) != "" {
					continue
				}
				if row.Status != status {
					continue
				}
				if row.Status == domain.StatusQueued && row.Queue.Kind != domain.QueueKindAnswerUser {
					continue
				}
				seen[row.ID] = true
				result = append(result, row)
			}
			if page*maxPageSize >= total || len(rows) == 0 {
				break
			}
		}
	}
	return result, nil
}

func (s *Service) CreateSession(ctx context.Context, input CreateSessionInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	if input.ProjectID == "" {
		return DTO{}, errors.New("project id is required")
	}
	if s.projects == nil {
		return DTO{}, errors.New("project repository is required")
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(input.ProjectID))
	if err != nil {
		return DTO{}, fmt.Errorf("find project: %w", err)
	}
	requirement := strings.TrimSpace(input.Requirement)
	if requirement == "" {
		return DTO{}, errors.New("session requirement is required")
	}
	mode := input.Mode
	if mode == "" {
		mode = domain.ModeChat
	}
	if mode != domain.ModeChat && mode != domain.ModeWorkflow {
		return DTO{}, fmt.Errorf("unsupported session mode %q", mode)
	}
	config, err := s.resolveSessionConfig(ctx, input.ProjectID, input.Config)
	if err != nil {
		return DTO{}, err
	}
	if mode == domain.ModeWorkflow {
		if s.workflows == nil {
			return DTO{}, errors.New("session workflow starter is required for workflow mode")
		}
		if project.DefaultWorkflowID == nil || strings.TrimSpace(string(*project.DefaultWorkflowID)) == "" {
			return DTO{}, apperror.New(apperror.CodeWorkflowBlocked, apperror.CategoryWorkflowError, "project default workflow is required for workflow mode").WithUserAction("configure_project_workflow")
		}
	}
	generatedID, err := s.generateID()
	if err != nil {
		return DTO{}, fmt.Errorf("generate session id: %w", err)
	}
	stagedAttachments, err := s.findStagedAttachments(ctx, input.StagedAttachmentIDs)
	if err != nil {
		return DTO{}, err
	}
	now := s.now()
	baseBranch := ""
	worktreeOwnershipToken := ""
	if project.IsGit {
		baseBranch = strings.TrimSpace(input.BaseBranch)
		if baseBranch == "" {
			return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "base branch is required for git project").WithDetails(map[string]any{
				"projectId": string(input.ProjectID),
			}).WithUserAction("select_base_branch")
		}
		if s.worktrees == nil {
			return DTO{}, errors.New("session worktree manager is required for git project")
		}
		worktreeOwnershipToken, err = generateWorktreeOwnershipToken()
		if err != nil {
			return DTO{}, fmt.Errorf("generate worktree ownership token: %w", err)
		}
	}

	createWithID := func(ctx context.Context, id domain.ID) (DTO, error) {
		worktreePath := project.Path.Value
		if project.IsGit {
			worktreePath = s.worktrees.PathForSession(input.ProjectID, id)
		}
		session := domain.Session{
			ID:           id,
			ProjectID:    input.ProjectID,
			Requirement:  requirement,
			Mode:         mode,
			Status:       domain.StatusCreated,
			Priority:     normalizePriority(input.Priority),
			BaseBranch:   baseBranch,
			WorktreePath: worktreePath,
			WorktreeCleanup: domain.WorktreeCleanup{
				Status: domain.WorktreeCleanupNotApplicable,
			},
			Config:    config,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if project.IsGit {
			if err := session.BeginWorktreeProvisioning(worktreePath, worktreeBranchName(id), worktreeOwnershipToken, now); err != nil {
				return DTO{}, err
			}
		}
		if err := s.repo.Create(ctx, session); err != nil {
			return DTO{}, fmt.Errorf("create session: %w", err)
		}
		if s.artifacts != nil {
			if _, err := s.artifacts.EnsureArtifactDir(ctx, id); err != nil {
				return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, fmt.Errorf("create session artifact directory: %w", err), "artifact_directory_failed")
			}
		}

		if project.IsGit {
			createdPath, err := s.worktrees.Create(ctx, project.Path.Value, input.ProjectID, id, session.WorktreeBranch, baseBranch, session.WorktreeCleanup.OwnershipToken)
			if err != nil {
				createErr := apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "create session worktree failed").WithDetails(map[string]any{
					"projectId":  string(input.ProjectID),
					"sessionId":  string(id),
					"baseBranch": baseBranch,
				}).WithRetryable(true)
				return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, createErr, "worktree_create_failed")
			}
			if createdPath != worktreePath {
				createErr := apperror.New(apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "created worktree path does not match persisted ownership").WithDetails(map[string]any{
					"sessionId":    string(id),
					"expectedPath": worktreePath,
					"actualPath":   createdPath,
				})
				return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, createErr, "worktree_path_mismatch")
			}
			baseCommit, err := s.worktrees.HeadCommit(ctx, createdPath, "")
			if err != nil {
				baseErr := apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "read session worktree base commit failed").WithDetails(map[string]any{
					"projectId": string(input.ProjectID),
					"sessionId": string(id),
				}).WithRetryable(true)
				return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, baseErr, "worktree_base_commit_failed")
			}
			session.WorktreeBaseCommit = baseCommit
			if err := session.ActivateWorktree(s.now()); err != nil {
				return DTO{}, err
			}
			if err := s.repo.Save(ctx, session); err != nil {
				return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, fmt.Errorf("save active session worktree: %w", err), "worktree_active_save_failed")
			}
		}
		if project.IsGit && strings.TrimSpace(project.WorktreeInitCommand) != "" {
			if err := s.initializeWorktree(ctx, session, project.WorktreeInitCommand); err != nil {
				return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, err, "worktree_init_cancelled")
			}
		}
		if _, err := s.archiveStagedAttachments(ctx, id, domain.AttachmentSourceRequirement, string(id), stagedAttachments); err != nil {
			return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, err, "attachment_archive_failed")
		}
		if mode == domain.ModeWorkflow {
			dto, startErr := s.startWorkflowSession(ctx, session, domain.WorkflowDefinitionID(*project.DefaultWorkflowID), true, "")
			if startErr != nil {
				return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, startErr, "workflow_start_failed")
			}
			return dto, nil
		}
		dto, startErr := s.enqueueCodex(ctx, session, codexStartOptions{initialStart: true}, queuePriorityForSession(session))
		if startErr != nil {
			return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, startErr, "session_start_failed")
		}
		return dto, nil
	}

	for attempt := 0; attempt < maxSessionIDAttempts; attempt++ {
		id, err := s.sessionIDForProject(ctx, project, generatedID, attempt)
		if err != nil {
			return DTO{}, err
		}
		var dto DTO
		err = s.withSessionLock(ctx, id, func(ctx context.Context) error {
			var createErr error
			dto, createErr = createWithID(ctx, id)
			return createErr
		})
		if isRandomHexID(generatedID) && errors.Is(err, domain.ErrSessionAlreadyExists) {
			continue
		}
		return dto, err
	}
	return DTO{}, fmt.Errorf("create session: exhausted %d session id attempts", maxSessionIDAttempts)
}

func (s *Service) initializeWorktree(ctx context.Context, session domain.Session, script string) error {
	var result domain.WorktreeInitResult
	var runErr error
	if s.worktreeInitializer == nil {
		runErr = errors.New("session worktree initializer is required")
	} else {
		result, runErr = s.worktreeInitializer.Run(ctx, session.WorktreePath, script)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if runErr == nil && result.Success {
		return nil
	}

	var exitCode any
	if result.ExitCode != nil {
		exitCode = *result.ExitCode
	}
	payload := map[string]any{
		"exitCode":        exitCode,
		"output":          result.Output,
		"outputTruncated": result.OutputTruncated,
	}
	if runErr != nil {
		payload["error"] = runErr.Error()
	}
	if err := s.recordSessionEvent(ctx, session, "session.worktree_init_failed", payload); err != nil {
		log.Printf("record worktree init failure event: project=%s session=%s error=%v", session.ProjectID, session.ID, err)
	}
	return nil
}

func (s *Service) recordSessionEvent(ctx context.Context, session domain.Session, eventType string, payload map[string]any) error {
	event, ok, err := s.newSessionEvent(session, eventType, payload)
	if err != nil || !ok {
		return err
	}
	if err := s.events.Append(ctx, event); err != nil {
		return err
	}
	s.publishSessionEvent(ctx, event)
	return nil
}

func (s *Service) failCreatedSessionWithCleanup(ctx context.Context, session domain.Session, cause error, reason string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), processCleanupTimeout)
	defer cancel()
	now := s.now()
	if session.Status != domain.StatusFailed {
		if err := transitionSession(&session, domain.StatusFailed, now); err != nil {
			return errors.Join(cause, err)
		}
	}
	if strings.TrimSpace(session.BaseBranch) != "" {
		if err := session.RequestWorktreeCleanup(now); err != nil {
			return errors.Join(cause, err)
		}
	}
	eventType := "session.failed"
	if strings.TrimSpace(session.BaseBranch) != "" {
		eventType = "session.worktree_cleanup_requested"
	}
	if err := s.saveSessionWithEvent(cleanupCtx, session, eventType, map[string]any{
		"reason":         reason,
		"worktreeBranch": session.WorktreeBranch,
	}); err != nil {
		return errors.Join(cause, err)
	}
	s.scheduleWorktreeCleanup()
	return cause
}

func (s *Service) StartSession(ctx context.Context, id domain.ID) (DTO, error) {
	return s.StartSessionWithOptions(ctx, id, StartSessionOptions{})
}

func (s *Service) ExecuteSession(ctx context.Context, id domain.ID) (DTO, error) {
	return s.ExecuteSessionWithOptions(ctx, id, StartSessionOptions{})
}

func (s *Service) ExecuteSessionWithOptions(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		var err error
		dto, err = s.executeSession(ctx, id, options)
		return err
	})
	return dto, err
}

func (s *Service) executeSession(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	if session.Status == domain.StatusQueued {
		if !options.Force {
			return toDTO(session), nil
		}
		return s.startQueuedSession(ctx, session, true)
	}
	if canResume(session) {
		return s.resumeLoadedSession(ctx, session, options)
	}
	return s.startLoadedSession(ctx, session, options)
}

func (s *Service) StartSessionWithOptions(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		var err error
		dto, err = s.startSession(ctx, id, options)
		return err
	})
	return dto, err
}

func (s *Service) startSession(ctx context.Context, id domain.ID, startOptions StartSessionOptions) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	return s.startLoadedSession(ctx, session, startOptions)
}

func (s *Service) startLoadedSession(ctx context.Context, session domain.Session, startOptions StartSessionOptions) (DTO, error) {
	if err := requireActiveWorktree(session); err != nil {
		return DTO{}, err
	}
	if session.Status == domain.StatusResumeFailed {
		if err := s.settleDetachedProcessBeforeRecoveryAction(ctx, session); err != nil {
			return DTO{}, err
		}
		if _, _, awaiting, err := s.awaitingAnswerDelivery(ctx, session.ID); err != nil {
			return DTO{}, err
		} else if awaiting {
			if err := s.cancelPendingQuestions(ctx, session.ID, "answer delivery abandoned by rerun"); err != nil {
				return DTO{}, err
			}
		}
	}
	if session.Mode == domain.ModeWorkflow {
		switch session.Status {
		case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusWaitingApproval, domain.StatusStopping:
			return toDTO(session), nil
		case domain.StatusQueued:
			if !startOptions.Force {
				return toDTO(session), nil
			}
			return s.startQueuedSession(ctx, session, true)
		case domain.StatusResumeFailed:
			return s.rerunWorkflowCurrentNode(ctx, session)
		case domain.StatusCreated, domain.StatusStopped, domain.StatusFailed, domain.StatusCompleted:
		default:
			return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot start from current status").WithDetails(map[string]any{"status": string(session.Status)})
		}
		project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
		if err != nil {
			return DTO{}, fmt.Errorf("find project: %w", err)
		}
		if project.DefaultWorkflowID == nil || strings.TrimSpace(string(*project.DefaultWorkflowID)) == "" {
			return DTO{}, apperror.New(apperror.CodeWorkflowBlocked, apperror.CategoryWorkflowError, "project default workflow is required for workflow mode").WithUserAction("configure_project_workflow")
		}
		return s.startWorkflowSession(ctx, session, domain.WorkflowDefinitionID(*project.DefaultWorkflowID), session.Status == domain.StatusCreated, startOptions.queueKind)
	}
	switch session.Status {
	case domain.StatusQueued:
		if !startOptions.Force {
			return toDTO(session), nil
		}
		return s.startQueuedSession(ctx, session, true)
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser:
		return toDTO(session), nil
	case domain.StatusCreated, domain.StatusStopped, domain.StatusFailed, domain.StatusCompleted, domain.StatusResumeFailed:
	default:
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot start from current status").WithDetails(map[string]any{"status": string(session.Status)})
	}
	options := codexStartOptions{initialStart: session.Status == domain.StatusCreated, queueKind: startOptions.queueKind}
	if session.Status == domain.StatusResumeFailed {
		options.reviewAfterReuseFailure = true
	}
	if startOptions.Force {
		return s.startCodex(ctx, session, options, true)
	}
	return s.enqueueCodex(ctx, session, options, queuePriorityForSession(session))
}

func (s *Service) rerunWorkflowCurrentNode(ctx context.Context, session domain.Session) (DTO, error) {
	if s.workflows == nil {
		return DTO{}, errors.New("session workflow starter is required for workflow mode")
	}
	advance, err := s.workflows.RerunCurrentNodeForSession(ctx, domain.WorkflowRerunCurrentNodeInput{
		SessionID: session.ID,
		Reason:    "user requested rerun after resume failure",
	})
	if err != nil {
		return DTO{}, fmt.Errorf("rerun workflow current node: %w", err)
	}
	return s.applyWorkflowAdvance(ctx, session, advance, workflowAdvanceOptions{forceNewCodexSession: true})
}

type workflowAdvanceOptions struct {
	forceNewCodexSession bool
	initialStart         bool
	commandID            string
}

func (s *Service) startWorkflowSession(ctx context.Context, session domain.Session, workflowDefinitionID domain.WorkflowDefinitionID, initialStart bool, queueKind domain.QueueKind) (DTO, error) {
	if s.workflows == nil {
		return DTO{}, errors.New("session workflow starter is required for workflow mode")
	}
	// A new WorkflowRun starts a new Codex conversation; nodes inside that run reuse it.
	session.CodexSessionID = ""
	start, err := s.workflows.StartForSession(ctx, domain.WorkflowStartInput{
		ProjectID:            session.ProjectID,
		SessionID:            session.ID,
		WorkflowDefinitionID: workflowDefinitionID,
		Requirement:          session.Requirement,
	})
	if err != nil {
		return DTO{}, fmt.Errorf("start workflow: %w", err)
	}
	if start.Close {
		return s.closeWorkflowSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: domain.CloseReasonWorkflowClosed})
	}
	if start.Merge != nil {
		return s.executeWorkflowMerge(ctx, session, domain.WorkflowAdvance{
			WorkflowRunID:    start.WorkflowRunID,
			NodeRunID:        start.NodeRunID,
			CurrentNodeID:    start.CurrentNodeID,
			CurrentNodeTitle: start.CurrentNodeTitle,
			Status:           start.Status,
			Merge:            start.Merge,
		})
	}
	if start.Expr != nil {
		return s.executeWorkflowExpr(ctx, session, domain.WorkflowAdvance{
			WorkflowRunID:    start.WorkflowRunID,
			NodeRunID:        start.NodeRunID,
			CurrentNodeID:    start.CurrentNodeID,
			CurrentNodeTitle: start.CurrentNodeTitle,
			Status:           start.Status,
			Expr:             start.Expr,
		}, workflowAdvanceOptions{initialStart: initialStart})
	}
	if !start.RequiresCodex {
		if err := transitionSessionToWaitingApproval(&session, initialStart, s.now()); err != nil {
			return DTO{}, err
		}
		if err := s.saveSessionWithEvent(ctx, session, "session.waiting_approval", map[string]any{
			"workflowRunId":    string(start.WorkflowRunID),
			"nodeRunId":        stringValuePtr(start.NodeRunID),
			"currentNodeId":    start.CurrentNodeID,
			"currentNodeTitle": start.CurrentNodeTitle,
			"approvalPhase":    start.ApprovalPhase,
			"result":           start.Result,
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	}
	dto, err := s.enqueueCodex(ctx, session, codexStartOptions{
		workflowRunID:       start.WorkflowRunID,
		nodeRunID:           workflowNodeRunID(start.NodeRunID),
		prompt:              start.Prompt,
		queueKind:           queueKind,
		workflowResultRetry: start.RequireResultRetry,
		initialStart:        initialStart,
	}, queuePriorityForSession(session))
	if err != nil {
		return s.handleWorkflowNodeFailure(ctx, session, start.WorkflowRunID, start.NodeRunID, "codex_start_failed", err.Error())
	}
	return dto, nil
}

func (s *Service) resolveSessionConfig(ctx context.Context, projectID domain.ProjectID, requested ConfigInput) (domain.Config, error) {
	if strings.TrimSpace(requested.CodexModel) != "" &&
		strings.TrimSpace(requested.ReasoningEffort) != "" &&
		strings.TrimSpace(requested.PermissionMode) != "" &&
		requested.FastMode != nil {
		return configFromInput(requested, *requested.FastMode), nil
	}
	previous, ok, err := s.repo.LastConfigForProject(ctx, projectID)
	if err != nil {
		return domain.Config{}, fmt.Errorf("last config for project: %w", err)
	}
	fastMode := false
	if requested.FastMode != nil {
		fastMode = *requested.FastMode
	}
	config := configFromInput(requested, fastMode)
	if !ok {
		return config, nil
	}
	previous = trimConfig(previous)
	if config.CodexModel == "" {
		config.CodexModel = previous.CodexModel
	}
	if config.ReasoningEffort == "" {
		config.ReasoningEffort = previous.ReasoningEffort
	}
	if config.PermissionMode == "" {
		config.PermissionMode = previous.PermissionMode
	}
	if requested.FastMode == nil {
		config.FastMode = previous.FastMode
	}
	return config, nil
}

func configFromInput(input ConfigInput, fastMode bool) domain.Config {
	return domain.Config{
		CodexModel:      strings.TrimSpace(input.CodexModel),
		ReasoningEffort: strings.TrimSpace(input.ReasoningEffort),
		PermissionMode:  strings.TrimSpace(input.PermissionMode),
		FastMode:        fastMode,
	}
}

func trimConfig(config domain.Config) domain.Config {
	return domain.Config{
		CodexModel:      strings.TrimSpace(config.CodexModel),
		ReasoningEffort: strings.TrimSpace(config.ReasoningEffort),
		PermissionMode:  strings.TrimSpace(config.PermissionMode),
		FastMode:        config.FastMode,
	}
}

func (s *Service) sessionIDForProject(ctx context.Context, project projectdomain.Project, generated domain.ID, attempt int) (domain.ID, error) {
	if !isRandomHexID(generated) {
		return generated, nil
	}
	count, err := s.repo.CountByProject(ctx, domain.ProjectID(project.ID))
	if err != nil {
		return "", fmt.Errorf("count project sessions: %w", err)
	}
	return domain.ID(fmt.Sprintf("p%s-c%d", projectIDCode(project.ID), count+attempt+1)), nil
}

func (s *Service) LastSessionConfigForProject(ctx context.Context, projectID domain.ProjectID) (*ConfigDTO, error) {
	if s == nil {
		return nil, errors.New("session usecase: nil service")
	}
	if projectID == "" {
		return nil, errors.New("project id is required")
	}
	config, ok, err := s.repo.LastConfigForProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("last config for project: %w", err)
	}
	if !ok {
		return nil, nil
	}
	return &ConfigDTO{
		CodexModel:      config.CodexModel,
		ReasoningEffort: config.ReasoningEffort,
		PermissionMode:  config.PermissionMode,
		FastMode:        config.FastMode,
	}, nil
}

func isRandomHexID(id domain.ID) bool {
	value := string(id)
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func projectIDCode(id projectdomain.ID) string {
	const maxLen = 8
	var builder strings.Builder
	for _, char := range strings.ToLower(string(id)) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			if builder.Len() >= maxLen {
				break
			}
		}
	}
	if builder.Len() == 0 {
		return "project"
	}
	return builder.String()
}

func normalizePriority(priority domain.Priority) domain.Priority {
	switch priority {
	case domain.PriorityHigh, domain.PriorityLow:
		return priority
	default:
		return domain.PriorityMedium
	}
}

func (s *Service) SetSessionPriority(ctx context.Context, input SetSessionPriorityInput) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
		var err error
		dto, err = s.setSessionPriority(ctx, input)
		return err
	})
	return dto, err
}

func (s *Service) setSessionPriority(ctx context.Context, input SetSessionPriorityInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	if input.SessionID == "" {
		return DTO{}, errors.New("session id is required")
	}
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	if session.Status == domain.StatusClosed {
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "closed session priority cannot be changed")
	}
	session.Priority = normalizePriority(input.Priority)
	session.UpdatedAt = s.now()
	if err := s.saveSessionWithEvent(ctx, session, "session.priority_changed", map[string]any{
		"priority": string(session.Priority),
	}); err != nil {
		return DTO{}, err
	}
	return toDTO(session), nil
}

func (s *Service) UpdateSessionConfig(ctx context.Context, input UpdateSessionConfigInput) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
		var err error
		dto, err = s.updateSessionConfig(ctx, input)
		return err
	})
	return dto, err
}

func (s *Service) updateSessionConfig(ctx context.Context, input UpdateSessionConfigInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	if input.SessionID == "" {
		return DTO{}, errors.New("session id is required")
	}
	fastMode := false
	if input.Config.FastMode != nil {
		fastMode = *input.Config.FastMode
	}
	config := configFromInput(input.Config, fastMode)
	if config.CodexModel == "" || config.ReasoningEffort == "" || config.PermissionMode == "" {
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session config is incomplete")
	}
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	if input.Config.FastMode == nil {
		config.FastMode = session.Config.FastMode
	}
	if session.Status == domain.StatusClosed {
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "closed session config cannot be changed")
	}
	session.Config = config
	session.UpdatedAt = s.now()
	if err := s.saveSessionWithEvent(ctx, session, "session.config_changed", map[string]any{
		"codexModel":      session.Config.CodexModel,
		"reasoningEffort": session.Config.ReasoningEffort,
		"permissionMode":  session.Config.PermissionMode,
		"fastMode":        session.Config.FastMode,
	}); err != nil {
		return DTO{}, err
	}
	return toDTO(session), nil
}

func (s *Service) StopSession(ctx context.Context, id domain.ID) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		var err error
		dto, err = s.stopSession(ctx, id)
		return err
	})
	if err == nil && dto.Status == domain.StatusStopping {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		defer cancel()
		dto, err = s.waitForStopCompletion(cleanupCtx, id)
	}
	return dto, err
}

func (s *Service) waitForStopCompletion(ctx context.Context, id domain.ID) (DTO, error) {
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(id))
	if err != nil {
		return DTO{}, fmt.Errorf("find stopping process: %w", err)
	}
	if ok {
		if done, exists := s.processConsumerDone(active.ID); exists {
			select {
			case <-ctx.Done():
				return DTO{}, fmt.Errorf("wait for stopped process: %w", ctx.Err())
			case <-done:
			}
		}
	}
	for {
		session, err := s.repo.Find(ctx, id)
		if err != nil {
			return DTO{}, fmt.Errorf("find stopped session: %w", err)
		}
		if session.Status == domain.StatusStopped || session.Status == domain.StatusClosed {
			return toDTO(session), nil
		}
		if session.Status != domain.StatusStopping {
			return DTO{}, fmt.Errorf("session stop did not complete: status %q", session.Status)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return DTO{}, fmt.Errorf("wait for stopped session: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (s *Service) stopSession(ctx context.Context, id domain.ID) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	if session.Status == domain.StatusClosed {
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot stop from current status").WithDetails(map[string]any{"status": string(session.Status)})
	}
	if s.processes == nil || s.codex == nil {
		if session.Status == domain.StatusStopped {
			cleanupCtx, cancel := detachedCleanupContext(ctx)
			defer cancel()
			if err := s.cancelPendingQuestions(cleanupCtx, session.ID, "session stopped"); err != nil {
				return DTO{}, err
			}
			return toDTO(session), nil
		}
		if session.Status == domain.StatusQueued && session.Queue.Kind != domain.QueueKindAnswerUser {
			return s.stopSessionWithoutActiveProcess(ctx, session, "queue_cancelled", false)
		}
		return DTO{}, apperror.Wrap(ErrProcessLifecycleNotWired, apperror.CodeCodexStartFailed, apperror.CategoryInfraError, "session process lifecycle is not wired")
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(id))
	if err != nil {
		return DTO{}, fmt.Errorf("find active process run: %w", err)
	}
	if session.Status == domain.StatusStopped && !ok {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		defer cancel()
		if err := s.cancelPendingQuestions(cleanupCtx, session.ID, "session stopped"); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	}
	if session.Status == domain.StatusQueued && session.Queue.Kind != domain.QueueKindAnswerUser && !ok {
		return s.stopSessionWithoutActiveProcess(ctx, session, "queue_cancelled", false)
	}
	if !ok {
		switch session.Status {
		case domain.StatusQueued, domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping, domain.StatusResumeFailed:
		default:
			return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot stop from current status").WithDetails(map[string]any{"status": string(session.Status)})
		}
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		defer cancel()
		stopReason := "no_active_process"
		if session.Status == domain.StatusQueued {
			stopReason = "queue_cancelled"
		}
		return s.stopSessionWithoutActiveProcess(cleanupCtx, session, stopReason, true)
	}
	now := s.now()
	if err := transitionSession(&session, domain.StatusStopping, now); err != nil {
		return DTO{}, err
	}
	if err := s.markProcessStoppingWithSessionEvent(ctx, active.ID, session, "session.stopping", map[string]any{"processRunId": string(active.ID)}); err != nil {
		return DTO{}, err
	}
	cleanupCtx, cancel := detachedCleanupContext(ctx)
	defer cancel()
	stopErr := s.codex.Stop(cleanupCtx, active.ID)
	processMissing := errors.Is(stopErr, processdomain.ErrProcessNotFound)
	if processMissing {
		stopErr = s.stopDetachedProcess(cleanupCtx, active)
		processMissing = false
	}
	if stopErr != nil && !processMissing {
		return DTO{}, fmt.Errorf("stop codex process: %w", stopErr)
	}
	if s.hasProcessConsumer(active.ID) {
		if err := s.cancelPendingQuestions(cleanupCtx, session.ID, "session stopped"); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	}
	finishedAt := s.now()
	if err := transitionSession(&session, domain.StatusStopped, finishedAt); err != nil {
		return DTO{}, err
	}
	if err := s.markProcessExitedWithSessionEventsAndSettlement(cleanupCtx, active.ID, processdomain.ExitResult{
		FailureReason: "stopped by user",
		FinishedAt:    finishedAt,
	}, session, true, []sessionEventInput{{
		eventType: "session.stopped",
		payload: map[string]any{
			"processRunId": string(active.ID),
			"reason":       "user_stopped",
		},
	}}, promptAppendSettlementRelease); err != nil {
		return DTO{}, err
	}
	if err := s.cancelPendingQuestions(cleanupCtx, session.ID, "session stopped"); err != nil {
		return DTO{}, err
	}
	s.scheduleQueueDrain()
	return toDTO(session), nil
}

func (s *Service) stopSessionWithoutActiveProcess(ctx context.Context, session domain.Session, reason string, cancelQuestions bool) (DTO, error) {
	if err := transitionSession(&session, domain.StatusStopped, s.now()); err != nil {
		return DTO{}, err
	}
	if err := s.saveSessionWithEvent(ctx, session, "session.stopped", map[string]any{"reason": reason}); err != nil {
		return DTO{}, err
	}
	s.scheduleQueueDrain()
	if cancelQuestions {
		if err := s.cancelPendingQuestions(ctx, session.ID, "session stopped"); err != nil {
			return DTO{}, err
		}
	}
	return toDTO(session), nil
}

func detachedCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), processCleanupTimeout)
}

func (s *Service) StopProjectSessions(ctx context.Context, projectID domain.ProjectID) (int, error) {
	if s == nil {
		return 0, errors.New("session usecase: nil service")
	}
	if projectID == "" {
		return 0, errors.New("project id is required")
	}
	statuses := []domain.Status{
		domain.StatusQueued,
		domain.StatusStarting,
		domain.StatusRunning,
		domain.StatusWaitingUser,
		domain.StatusStopping,
		domain.StatusResumeFailed,
	}
	stopped := 0
	for _, status := range statuses {
		sessions, err := s.listProjectSessionsByStatus(ctx, projectID, status)
		if err != nil {
			return stopped, err
		}
		for _, session := range sessions {
			if _, err := s.StopSession(ctx, session.ID); err != nil {
				return stopped, fmt.Errorf("stop project session %s: %w", session.ID, err)
			}
			stopped++
		}
	}
	return stopped, nil
}

func (s *Service) listProjectSessionsByStatus(ctx context.Context, projectID domain.ProjectID, status domain.Status) ([]domain.Session, error) {
	result := []domain.Session{}
	for page := 1; ; page++ {
		rows, total, err := s.repo.ListCards(ctx, domain.ListQuery{
			ProjectID: &projectID,
			Scope:     string(status),
			Page:      page,
			PageSize:  maxPageSize,
			Sort:      "updated_at asc",
		})
		if err != nil {
			return nil, fmt.Errorf("list project sessions: %w", err)
		}
		result = append(result, rows...)
		if page*maxPageSize >= total || len(rows) == 0 {
			break
		}
	}
	return result, nil
}

func (s *Service) ResumeSession(ctx context.Context, id domain.ID) (DTO, error) {
	return s.ResumeSessionWithOptions(ctx, id, StartSessionOptions{})
}

func (s *Service) ResumeSessionWithOptions(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		var err error
		dto, err = s.resumeSession(ctx, id, options)
		return err
	})
	return dto, err
}

func (s *Service) resumeSession(ctx context.Context, id domain.ID, startOptions StartSessionOptions) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	return s.resumeLoadedSession(ctx, session, startOptions)
}

func (s *Service) resumeLoadedSession(ctx context.Context, session domain.Session, startOptions StartSessionOptions) (DTO, error) {
	if err := requireActiveWorktree(session); err != nil {
		return DTO{}, err
	}
	switch session.Status {
	case domain.StatusStopped, domain.StatusResumeFailed:
	case domain.StatusQueued:
		if !startOptions.Force {
			return toDTO(session), nil
		}
		return s.startQueuedSession(ctx, session, true)
	default:
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot resume from current status").WithDetails(map[string]any{"status": string(session.Status)})
	}
	if session.Status == domain.StatusResumeFailed {
		if err := s.settleDetachedProcessBeforeRecoveryAction(ctx, session); err != nil {
			return DTO{}, err
		}
	}
	if batch, origin, ok, err := s.awaitingAnswerDelivery(ctx, session.ID); err != nil {
		return DTO{}, err
	} else if ok {
		options := answerResumeOptions(batch, origin)
		if session.Mode == domain.ModeWorkflow {
			if s.workflows == nil {
				return DTO{}, errors.New("session workflow starter is required for workflow mode")
			}
			advance, err := s.workflows.ResumeCurrentNodeForSession(ctx, domain.WorkflowResumeCurrentNodeInput{
				SessionID: session.ID,
				Reason:    "user retried answer delivery",
			})
			if err != nil {
				return DTO{}, err
			}
			options.workflowRunID = advance.WorkflowRunID
			options.nodeRunID = workflowNodeRunID(advance.NodeRunID)
		}
		if startOptions.Force {
			return s.startCodex(ctx, session, options, true)
		}
		dto, err := s.queueCodex(ctx, session, options, domain.QueuePriorityHigh, domain.QueueKindAnswerUser)
		if err == nil {
			s.scheduleQueueDrain()
		}
		return dto, err
	}
	resumeCodexSessionID := strings.TrimSpace(startOptions.resumeCodexSessionID)
	if resumeCodexSessionID == "" {
		resumeCodexSessionID = strings.TrimSpace(session.CodexSessionID)
	}
	if resumeCodexSessionID == "" {
		return DTO{}, apperror.New(apperror.CodeResumeFailed, apperror.CategoryValidationError, "session cannot resume without codex session id").WithUserAction("rerun_session")
	}
	session.CodexSessionID = resumeCodexSessionID
	options := codexStartOptions{
		resumeCodexSessionID: resumeCodexSessionID,
		prompt:               strings.TrimSpace(startOptions.prompt),
		queueKind:            startOptions.queueKind,
	}
	if session.Mode == domain.ModeWorkflow {
		if s.workflows == nil {
			return DTO{}, errors.New("session workflow starter is required for workflow mode")
		}
		if session.Status == domain.StatusResumeFailed {
			if _, err := s.workflows.MarkResumeFailedForSession(ctx, domain.WorkflowResumeFailureInput{
				SessionID: session.ID,
				Code:      "resume_failed",
				Message:   "reconcile workflow before retrying codex resume",
			}); err != nil {
				return DTO{}, fmt.Errorf("reconcile workflow resume failure: %w", err)
			}
		}
		advance, err := s.workflows.ResumeCurrentNodeForSession(ctx, domain.WorkflowResumeCurrentNodeInput{
			SessionID: session.ID,
			Reason:    "user requested codex resume",
		})
		if err != nil {
			return DTO{}, fmt.Errorf("resume workflow current node: %w", err)
		}
		options.workflowRunID = advance.WorkflowRunID
		options.nodeRunID = workflowNodeRunID(advance.NodeRunID)
		if options.prompt == "" || options.queueKind == domain.QueueKindPromptAppend {
			options.prompt = advance.Prompt
		}
	} else if err := s.requirePendingChatResume(ctx, session.ID); err != nil {
		return DTO{}, err
	}
	if startOptions.Force {
		return s.startCodex(ctx, session, options, true)
	}
	return s.enqueueCodex(ctx, session, options, queuePriorityForSession(session))
}

func (s *Service) settleDetachedProcessBeforeRecoveryAction(ctx context.Context, session domain.Session) error {
	if s.processes == nil {
		return nil
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
	if err != nil {
		return fmt.Errorf("find detached process before recovery action: %w", err)
	}
	if !ok {
		return nil
	}
	if err := s.stopDetachedProcess(ctx, active); err != nil {
		return apperror.Wrap(err, apperror.CodeResumeFailed, apperror.CategoryCodexError, "cannot verify the interrupted Codex process is stopped").
			WithDetails(map[string]any{"sessionId": string(session.ID), "processRunId": string(active.ID)}).
			WithRetryable(true).
			WithUserAction("stop_session")
	}
	return s.settleInterruptedRun(ctx, session, active)
}

func (s *Service) RequestUserAnswer(ctx context.Context, input RequestUserAnswerInput) (questionapp.BatchDTO, error) {
	if s == nil {
		return questionapp.BatchDTO{}, errors.New("session usecase: nil service")
	}
	if input.SessionID == "" || len(input.Questions) == 0 {
		return questionapp.BatchDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session id and questions are required")
	}
	var batch questionapp.BatchDTO
	err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
		var err error
		batch, _, err = s.requestUserAnswer(ctx, input)
		return err
	})
	if err != nil {
		return questionapp.BatchDTO{}, err
	}
	return s.waitForUserAnswer(ctx, batch)
}

func (s *Service) waitForUserAnswer(ctx context.Context, batch questionapp.BatchDTO) (questionapp.BatchDTO, error) {
	questions, ok := s.questions.(answerQuestionCoordinator)
	if !ok {
		return questionapp.BatchDTO{}, errors.New("answer question coordinator is required")
	}
	waitCtx, cancelWait := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWait()
	updates, err := questions.QuestionBatchUpdates(waitCtx, batch.SessionID)
	if err != nil {
		return questionapp.BatchDTO{}, err
	}
	current, err := questions.GetBatch(waitCtx, batch.ID)
	if err != nil {
		return questionapp.BatchDTO{}, err
	}
	if current.Status != questiondomain.BatchPending {
		return current, nil
	}
	timer := s.answerUserTimer
	if timer == nil {
		timer = func(duration time.Duration) (<-chan time.Time, func()) {
			timer := time.NewTimer(duration)
			return timer.C, func() { timer.Stop() }
		}
	}
	timeout, stopTimer := timer(answerUserWarmWaitTimeout)
	defer stopTimer()
	var processDone <-chan struct{}
	if current.OriginProcessRunID != nil {
		originID := processdomain.RunID(*current.OriginProcessRunID)
		processDone, _ = s.processConsumerDone(originID)
		if processDone == nil {
			finder, ok := s.processes.(processdomain.RunFinder)
			if !ok {
				return questionapp.BatchDTO{}, errors.New("process run finder is required")
			}
			origin, err := finder.FindRun(waitCtx, originID)
			if err != nil {
				return questionapp.BatchDTO{}, err
			}
			if origin.Status != processdomain.StatusWaitingUser {
				return current, nil
			}
		}
	}
	for {
		select {
		case update, open := <-updates:
			if !open {
				return questionapp.BatchDTO{}, ctx.Err()
			}
			if update.ID == batch.ID && update.Status != questiondomain.BatchPending {
				return update, nil
			}
		case <-timeout:
			if err := s.fallbackUserAnswer(context.WithoutCancel(ctx), domain.ID(batch.SessionID), batch.ID, answerUserFallbackTimeout); err != nil {
				return questionapp.BatchDTO{}, err
			}
			current, err := questions.GetBatch(context.WithoutCancel(ctx), batch.ID)
			if err != nil {
				return questionapp.BatchDTO{}, err
			}
			return current, nil
		case <-processDone:
			return batch, nil
		case <-ctx.Done():
			if err := s.fallbackUserAnswer(context.WithoutCancel(ctx), domain.ID(batch.SessionID), batch.ID, answerUserFallbackTransport); err != nil {
				return questionapp.BatchDTO{}, err
			}
			return questionapp.BatchDTO{}, ctx.Err()
		}
	}
}

func (s *Service) fallbackUserAnswer(ctx context.Context, sessionID domain.ID, batchID questiondomain.BatchID, reason answerUserFallbackReason) error {
	if s.processes == nil || s.codex == nil || s.uow == nil {
		return errors.New("answer_user durable lifecycle is not wired")
	}
	var origin processdomain.Run
	claimed := false
	var fallbackEvent eventdomain.DomainEvent
	err := s.withSessionLock(ctx, sessionID, func(ctx context.Context) error {
		return s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			repo, ok := tx.Questions().(questiondomain.AgentRepository)
			if !ok {
				return errors.New("agent question repository is required")
			}
			batch, err := repo.FindBatch(ctx, batchID)
			if err != nil {
				return err
			}
			if batch.SessionID != questiondomain.SessionID(sessionID) {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question batch does not belong to the session")
			}
			var originID *questiondomain.ProcessRunID
			switch {
			case batch.Status == questiondomain.BatchPending:
				originID = batch.OriginProcessRunID
			case reason != answerUserFallbackTimeout && batch.Status == questiondomain.BatchAnswered && batch.DeliveryStatus == questiondomain.DeliveryInflight:
				originID = batch.DeliveryProcessRunID
			default:
				return nil
			}
			if originID == nil {
				return nil
			}
			finder, ok := tx.Processes().(processdomain.RunFinder)
			if !ok {
				return errors.New("process run finder is required")
			}
			origin, err = finder.FindRun(ctx, processdomain.RunID(*originID))
			if err != nil {
				return err
			}
			if origin.Status != processdomain.StatusWaitingUser {
				return nil
			}
			if err := tx.Processes().MarkStopping(ctx, origin.ID); err != nil {
				return err
			}
			session, err := tx.Sessions().Find(ctx, sessionID)
			if err != nil {
				return err
			}
			var hasEvent bool
			fallbackEvent, hasEvent, err = s.newSessionEvent(session, "process.answer_wait_fallback", map[string]any{
				"batchId": string(batchID), "processRunId": string(origin.ID), "reason": string(reason),
			})
			if err != nil {
				return err
			}
			claimed = true
			if hasEvent {
				return tx.Events().Append(ctx, fallbackEvent)
			}
			return nil
		})
	})
	if err != nil || !claimed {
		return err
	}
	if fallbackEvent.ID != "" {
		s.publishSessionEvent(ctx, fallbackEvent)
	}
	stopCtx, stopCancel := detachedCleanupContext(ctx)
	stopErr := s.codex.Stop(stopCtx, origin.ID)
	stopCancel()
	if stopErr != nil && !errors.Is(stopErr, processdomain.ErrProcessNotFound) {
		persistErr := s.persistAnswerFallbackStopFailure(context.WithoutCancel(ctx), sessionID, batchID, origin.ID, stopErr)
		return errors.Join(fmt.Errorf("suspend codex for answer_user: %w", stopErr), persistErr)
	}
	cleanupCtx, cancel := detachedCleanupContext(ctx)
	defer cancel()
	if done, ok := s.processConsumerDone(origin.ID); ok {
		select {
		case <-cleanupCtx.Done():
			return fmt.Errorf("wait for answer_user suspension: %w", cleanupCtx.Err())
		case <-done:
		}
		return nil
	}
	return s.withSessionLock(cleanupCtx, sessionID, func(ctx context.Context) error {
		_, _, err := s.persistCodexProcessExit(ctx, domain.Session{ID: sessionID}, processdomain.CodexHandle{
			ProcessRunID: origin.ID,
			PID:          intValue(origin.PID),
		}, codexStartOptions{}, processdomain.ExitResult{
			FailureReason: "suspended for user answer",
			FinishedAt:    s.now(),
		}, nil)
		return err
	})
}

func (s *Service) persistAnswerFallbackStopFailure(ctx context.Context, sessionID domain.ID, batchID questiondomain.BatchID, processRunID processdomain.RunID, stopErr error) error {
	var events []eventdomain.DomainEvent
	var workflowEvents []eventdomain.DomainEvent
	var session domain.Session
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		var err error
		session, err = tx.Sessions().Find(ctx, sessionID)
		if err != nil {
			return err
		}
		if err := transitionSession(&session, domain.StatusResumeFailed, s.now()); err != nil {
			return err
		}
		if err := tx.Sessions().Save(ctx, session); err != nil {
			return err
		}
		events, err = s.newSessionEvents(session, []sessionEventInput{{
			eventType: "session.resume_failed",
			payload: map[string]any{
				"batchId": string(batchID), "processRunId": string(processRunID), "reason": "answer_user_stop_failed", "error": stopErr.Error(),
			},
		}})
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := tx.Events().Append(ctx, event); err != nil {
				return err
			}
		}
		if session.Mode == domain.ModeWorkflow {
			runner, ok := s.workflows.(workflowResumeFailureRepositoryRunner)
			if !ok {
				return errors.New("workflow resume failure requires transactional workflow repository runner")
			}
			_, recorded, err := runner.MarkResumeFailedForSessionWithRepositories(ctx, domain.WorkflowResumeFailureInput{
				SessionID: session.ID, Code: "answer_user_stop_failed", Message: stopErr.Error(),
			}, tx.Workflows(), tx.Events())
			if err != nil {
				return fmt.Errorf("mark workflow resume failed: %w", err)
			}
			workflowEvents = recorded
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, event := range events {
		s.publishSessionEvent(ctx, event)
	}
	for _, event := range workflowEvents {
		s.publishSessionEvent(ctx, event)
	}
	return nil
}

func (s *Service) FailUserAnswerDelivery(ctx context.Context, input FailUserAnswerDeliveryInput) error {
	if s == nil {
		return errors.New("session usecase: nil service")
	}
	if input.SessionID == "" || input.BatchID == "" || input.Kind != UserAnswerDeliveryTransportClosed {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "valid session id, question batch id, and delivery failure kind are required")
	}
	return s.fallbackUserAnswer(context.WithoutCancel(ctx), input.SessionID, input.BatchID, answerUserFallbackTransport)
}

func (s *Service) AcknowledgeUserAnswerDelivery(ctx context.Context, input AcknowledgeUserAnswerDeliveryInput) error {
	if s == nil {
		return errors.New("session usecase: nil service")
	}
	if input.SessionID == "" || input.BatchID == "" {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session id and question batch id are required")
	}
	if s.processes == nil || s.uow == nil {
		return errors.New("answer_user durable lifecycle is not wired")
	}
	var delivered questiondomain.Batch
	var events []eventdomain.DomainEvent
	fallbackEligible := false
	err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
		return s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			repo, ok := tx.Questions().(questiondomain.AgentRepository)
			if !ok {
				return errors.New("agent question repository is required")
			}
			batch, err := repo.FindBatch(ctx, input.BatchID)
			if err != nil {
				return err
			}
			if batch.SessionID != questiondomain.SessionID(input.SessionID) || batch.DeliveryProcessRunID == nil {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question delivery does not belong to the session")
			}
			if batch.DeliveryStatus == questiondomain.DeliveryDelivered {
				return nil
			}
			fallbackEligible = batch.DeliveryStatus == questiondomain.DeliveryInflight
			finder, ok := tx.Processes().(processdomain.RunFinder)
			if !ok {
				return errors.New("process run finder is required")
			}
			origin, err := finder.FindRun(ctx, processdomain.RunID(*batch.DeliveryProcessRunID))
			if err != nil {
				return err
			}
			if origin.Status != processdomain.StatusWaitingUser {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "answer delivery origin is no longer waiting for user")
			}
			delivered, _, err = repo.MarkDeliveryDelivered(ctx, batch.ID, questiondomain.ProcessRunID(origin.ID), s.now())
			if err != nil {
				return err
			}
			if err := tx.Processes().MarkRunning(ctx, origin.ID, intValue(origin.PID), origin.CodexSessionID); err != nil {
				return err
			}
			session, err := tx.Sessions().Find(ctx, input.SessionID)
			if err != nil {
				return err
			}
			if err := transitionSession(&session, domain.StatusRunning, s.now()); err != nil {
				return err
			}
			if batch.WorkflowRunID != nil && origin.NodeRunID != nil {
				nodes, ok := tx.Workflows().(workflowdomain.NodeExecutionRepository)
				if !ok {
					return errors.New("workflow node execution repository is required")
				}
				if err := nodes.MarkNodeRunning(ctx, workflowdomain.RunID(*batch.WorkflowRunID), workflowdomain.NodeRunID(*origin.NodeRunID), workflowdomain.ProcessRunID(origin.ID)); err != nil {
					return err
				}
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return err
			}
			events, err = s.newSessionEvents(session, []sessionEventInput{
				{eventType: "question.answer_delivered", payload: map[string]any{"batchId": string(batch.ID), "processRunId": string(origin.ID)}},
				{eventType: "session.running", payload: map[string]any{"processRunId": string(origin.ID), "reason": "answer_user_delivered"}},
			})
			if err != nil {
				return err
			}
			for _, event := range events {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err != nil && fallbackEligible {
		fallbackErr := s.fallbackUserAnswer(context.WithoutCancel(ctx), input.SessionID, input.BatchID, answerUserFallbackAckFailed)
		return errors.Join(err, fallbackErr)
	}
	if err != nil {
		return err
	}
	if delivered.ID != "" {
		s.publishQuestionBatch(questionBatchDTO(delivered))
	}
	for _, event := range events {
		s.publishSessionEvent(ctx, event)
	}
	return nil
}

func (s *Service) requestUserAnswer(ctx context.Context, input RequestUserAnswerInput) (questionapp.BatchDTO, processdomain.Run, error) {
	if s.uow == nil || s.processes == nil || s.codex == nil || s.questions == nil {
		return questionapp.BatchDTO{}, processdomain.Run{}, errors.New("answer_user durable lifecycle is not wired")
	}
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return questionapp.BatchDTO{}, processdomain.Run{}, fmt.Errorf("find session: %w", err)
	}
	if session.Status != domain.StatusStarting && session.Status != domain.StatusRunning {
		return questionapp.BatchDTO{}, processdomain.Run{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot request user input from current status").WithDetails(map[string]any{"status": string(session.Status)})
	}
	active, err := s.activeProcessWithCodexSession(ctx, session.ID)
	if err != nil {
		return questionapp.BatchDTO{}, processdomain.Run{}, err
	}
	questions := make([]questiondomain.Question, len(input.Questions))
	batchIDValue, err := s.generateID()
	if err != nil {
		return questionapp.BatchDTO{}, processdomain.Run{}, fmt.Errorf("generate question batch id: %w", err)
	}
	batchID := questiondomain.BatchID(batchIDValue)
	for i, item := range input.Questions {
		questions[i] = item
		if questions[i].ID == "" {
			id, err := s.generateID()
			if err != nil {
				return questionapp.BatchDTO{}, processdomain.Run{}, fmt.Errorf("generate question id: %w", err)
			}
			questions[i].ID = questiondomain.QuestionID(id)
		}
		questions[i].BatchID = batchID
	}
	now := s.now()
	originID := questiondomain.ProcessRunID(active.ID)
	batch := questiondomain.Batch{
		ID:                 batchID,
		SessionID:          questiondomain.SessionID(session.ID),
		OriginProcessRunID: &originID,
		Status:             questiondomain.BatchPending,
		DeliveryStatus:     questiondomain.DeliveryNone,
		Questions:          questions,
		CreatedAt:          now,
	}
	var workflowRunID *workflowdomain.RunID
	if session.Mode == domain.ModeWorkflow && active.NodeRunID == nil {
		return questionapp.BatchDTO{}, processdomain.Run{}, errors.New("workflow answer_user origin is missing node run id")
	}
	if err := transitionSession(&session, domain.StatusWaitingUser, now); err != nil {
		return questionapp.BatchDTO{}, processdomain.Run{}, err
	}
	events, err := s.newSessionEvents(session, []sessionEventInput{
		{eventType: "question.pending", payload: map[string]any{"batchId": string(batch.ID), "processRunId": string(active.ID)}},
		{eventType: "session.waiting_user", payload: map[string]any{"batchId": string(batch.ID), "processRunId": string(active.ID)}},
	})
	if err != nil {
		return questionapp.BatchDTO{}, processdomain.Run{}, err
	}
	if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		if session.Mode == domain.ModeWorkflow {
			run, err := tx.Workflows().FindLatestRunBySession(ctx, workflowdomain.SessionID(session.ID))
			if err != nil {
				return err
			}
			workflowRunID = &run.ID
			value := questiondomain.WorkflowRunID(run.ID)
			batch.WorkflowRunID = &value
		}
		pending, err := tx.Questions().ListPendingBySession(ctx, questiondomain.SessionID(session.ID))
		if err != nil {
			return err
		}
		for _, existing := range pending {
			if !isInternalQuestionBatch(existing) {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session already has a pending agent question")
			}
		}
		if err := tx.Questions().CreateBatch(ctx, batch); err != nil {
			return err
		}
		if err := tx.Processes().MarkWaitingUser(ctx, active.ID); err != nil {
			return err
		}
		if err := tx.Sessions().Save(ctx, session); err != nil {
			return err
		}
		if workflowRunID != nil {
			repo, ok := tx.Workflows().(workflowdomain.NodeExecutionRepository)
			if !ok {
				return errors.New("workflow node execution repository is required")
			}
			if err := repo.MarkNodeWaitingUser(ctx, *workflowRunID, workflowdomain.NodeRunID(*active.NodeRunID)); err != nil {
				return err
			}
		}
		for _, event := range events {
			if err := tx.Events().Append(ctx, event); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return questionapp.BatchDTO{}, processdomain.Run{}, err
	}
	dto := questionBatchDTO(batch)
	s.publishQuestionBatch(dto)
	for _, event := range events {
		s.publishSessionEvent(ctx, event)
	}
	return dto, active, nil
}

func (s *Service) SubmitQuestionBatch(ctx context.Context, input questionapp.SubmitBatchInput) (questionapp.BatchDTO, error) {
	if s == nil || s.questions == nil {
		return questionapp.BatchDTO{}, errors.New("question lifecycle is not wired")
	}
	questions, ok := s.questions.(answerQuestionCoordinator)
	if !ok {
		return questionapp.BatchDTO{}, errors.New("answer question coordinator is required")
	}
	existing, err := questions.GetBatch(ctx, input.BatchID)
	if err != nil {
		return questionapp.BatchDTO{}, err
	}
	if existing.OriginProcessRunID == nil {
		batch, err := questions.SubmitBatch(ctx, input)
		if err != nil {
			return questionapp.BatchDTO{}, err
		}
		if err := s.HandleQuestionBatchAnswered(ctx, batch); err != nil {
			return questionapp.BatchDTO{}, err
		}
		return batch, nil
	}
	var batch questionapp.BatchDTO
	err = s.withSessionLock(ctx, domain.ID(existing.SessionID), func(ctx context.Context) error {
		var err error
		batch, err = s.submitAgentQuestionBatch(ctx, input)
		if err == nil {
			s.publishQuestionBatch(batch)
		}
		return err
	})
	if err != nil {
		return questionapp.BatchDTO{}, err
	}
	s.scheduleQueueDrain()
	return batch, nil
}

func (s *Service) submitAgentQuestionBatch(ctx context.Context, input questionapp.SubmitBatchInput) (questionapp.BatchDTO, error) {
	if s.uow == nil {
		return questionapp.BatchDTO{}, errors.New("answer_user durable lifecycle requires a unit of work")
	}
	var result questiondomain.Batch
	var publishedEvents []eventdomain.DomainEvent
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo, ok := tx.Questions().(questiondomain.AgentRepository)
		if !ok {
			return errors.New("agent question repository is required")
		}
		batch, err := repo.FindBatch(ctx, input.BatchID)
		if err != nil {
			return err
		}
		if batch.OriginProcessRunID == nil {
			return errors.New("question batch has no origin process run")
		}
		if batch.Status == questiondomain.BatchAnswered {
			if !questionAnswersMatch(batch, input.Answers) {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers do not match existing answers")
			}
			result = batch
			return nil
		}
		if err := (questiondomain.DefaultPolicy{}).CanSubmit(batch, input.Answers); err != nil {
			return apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers are invalid")
		}
		persisted, transitioned, err := repo.SubmitAnswers(ctx, input.BatchID, input.Answers)
		if err != nil {
			return err
		}
		if !transitioned {
			if persisted.Status != questiondomain.BatchAnswered || !questionAnswersMatch(persisted, input.Answers) {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question batch is no longer pending")
			}
			result = persisted
			return nil
		}
		finder, ok := tx.Processes().(processdomain.RunFinder)
		if !ok {
			return errors.New("process run finder is required")
		}
		origin, err := finder.FindRun(ctx, processdomain.RunID(*persisted.OriginProcessRunID))
		if err != nil {
			return err
		}
		if origin.Status == processdomain.StatusWaitingUser {
			if err := repo.MarkDeliveryInflight(ctx, persisted.ID, questiondomain.ProcessRunID(origin.ID)); err != nil {
				return err
			}
			deliveryID := questiondomain.ProcessRunID(origin.ID)
			persisted.DeliveryStatus = questiondomain.DeliveryInflight
			persisted.DeliveryProcessRunID = &deliveryID
			session, err := tx.Sessions().Find(ctx, domain.ID(persisted.SessionID))
			if err != nil {
				return err
			}
			event, ok, err := s.newSessionEvent(session, "question.answer_delivery_inflight", map[string]any{
				"batchId": string(persisted.ID), "processRunId": string(origin.ID),
			})
			if err != nil {
				return err
			}
			if ok {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
				publishedEvents = append(publishedEvents, event)
			}
			result = persisted
			return nil
		}
		resumeEvents, err := s.queueAnswerResumeInTx(ctx, tx, persisted, origin)
		if err != nil {
			return err
		}
		persisted.DeliveryStatus = questiondomain.DeliveryAwaitingResume
		persisted.DeliveryProcessRunID = nil
		publishedEvents = append(publishedEvents, resumeEvents...)
		result = persisted
		return nil
	})
	if err != nil {
		return questionapp.BatchDTO{}, err
	}
	for _, event := range publishedEvents {
		s.publishSessionEvent(ctx, event)
	}
	return questionBatchDTO(result), nil
}

func (s *Service) queueAnswerResumeInTx(ctx context.Context, tx port.Tx, batch questiondomain.Batch, origin processdomain.Run) ([]eventdomain.DomainEvent, error) {
	repo, ok := tx.Questions().(questiondomain.AgentRepository)
	if !ok {
		return nil, errors.New("agent question repository is required")
	}
	if strings.TrimSpace(origin.CodexSessionID) == "" {
		return nil, apperror.New(apperror.CodeResumeFailed, apperror.CategoryCodexError, "origin process has no Codex session id").WithRetryable(true)
	}
	if err := repo.MarkDeliveryAwaitingResume(ctx, batch.ID); err != nil {
		return nil, err
	}
	session, err := tx.Sessions().Find(ctx, domain.ID(batch.SessionID))
	if err != nil {
		return nil, err
	}
	options := codexStartOptions{
		resumeCodexSessionID: origin.CodexSessionID,
		resumeOfProcessRunID: origin.ID,
		answerBatchID:        batch.ID,
		prompt:               answerResumePrompt(batch),
	}
	if batch.WorkflowRunID != nil {
		options.workflowRunID = domain.WorkflowRunID(*batch.WorkflowRunID)
	}
	if origin.NodeRunID != nil {
		options.nodeRunID = origin.NodeRunID
	}
	queued, queuedEvent, hasQueuedEvent, err := s.prepareQueuedSession(session, options, domain.QueuePriorityHigh, domain.QueueKindAnswerUser)
	if err != nil {
		return nil, err
	}
	if err := tx.Sessions().Save(ctx, queued); err != nil {
		return nil, err
	}
	answerEvent, hasAnswerEvent, err := s.newSessionEvent(queued, "session.answer_resume_queued", map[string]any{
		"batchId": string(batch.ID), "originProcessRunId": string(origin.ID),
	})
	if err != nil {
		return nil, err
	}
	events := make([]eventdomain.DomainEvent, 0, 2)
	for _, item := range []struct {
		event eventdomain.DomainEvent
		ok    bool
	}{{queuedEvent, hasQueuedEvent}, {answerEvent, hasAnswerEvent}} {
		if !item.ok {
			continue
		}
		if err := tx.Events().Append(ctx, item.event); err != nil {
			return nil, err
		}
		events = append(events, item.event)
	}
	return events, nil
}

type codexStartOptions struct {
	resumeCodexSessionID    string
	resumeOfProcessRunID    processdomain.RunID
	answerBatchID           questiondomain.BatchID
	workflowRunID           domain.WorkflowRunID
	nodeRunID               *processdomain.NodeRunID
	prompt                  string
	fallbackPrompt          string
	promptAppendIDs         []string
	queueKind               domain.QueueKind
	workflowResultRetry     bool
	resumeAcknowledged      bool
	reviewAfterReuseFailure bool
	initialStart            bool
}

type executionClaimNotAcquiredError struct {
	result port.ExecutionClaimResult
}

func (e *executionClaimNotAcquiredError) Error() string {
	return "session execution claim was not acquired: " + string(e.result.Status)
}

func (s *Service) activeProcessWithCodexSession(ctx context.Context, sessionID domain.ID) (processdomain.Run, error) {
	for attempt := 0; attempt < 20; attempt++ {
		active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(sessionID))
		if err != nil {
			return processdomain.Run{}, fmt.Errorf("find active process run: %w", err)
		}
		if !ok {
			return processdomain.Run{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "answer_user requires an active Codex process")
		}
		if strings.TrimSpace(active.CodexSessionID) != "" {
			return active, nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return processdomain.Run{}, ctx.Err()
		case <-timer.C:
		}
	}
	return processdomain.Run{}, apperror.New(apperror.CodeResumeFailed, apperror.CategoryCodexError, "active process has no Codex session id").WithRetryable(true)
}

func (s *Service) answerOriginStillActive(ctx context.Context, session domain.Session) (bool, error) {
	originID := processdomain.RunID(session.Queue.ResumeOfProcessRunID)
	if originID == "" {
		return false, nil
	}
	finder, ok := s.processes.(processdomain.RunFinder)
	if !ok {
		return false, errors.New("process run finder is required for answer_user queue")
	}
	run, err := finder.FindRun(ctx, originID)
	if err != nil {
		return false, err
	}
	switch run.Status {
	case processdomain.StatusStarting, processdomain.StatusRunning, processdomain.StatusWaitingUser, processdomain.StatusStopping:
		return true, nil
	default:
		return false, nil
	}
}

func (s *Service) pendingAgentBatchForProcess(ctx context.Context, runID processdomain.RunID) (questiondomain.Batch, bool, error) {
	if s.uow == nil {
		return questiondomain.Batch{}, false, nil
	}
	var batch questiondomain.Batch
	var found bool
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo, ok := tx.Questions().(questiondomain.AgentRepository)
		if !ok {
			return nil
		}
		var err error
		batch, found, err = repo.FindPendingByOriginProcessRun(ctx, questiondomain.ProcessRunID(runID))
		return err
	})
	return batch, found, err
}

func (s *Service) inflightAgentBatchForProcess(ctx context.Context, runID processdomain.RunID) (questiondomain.Batch, bool, error) {
	if s.uow == nil {
		return questiondomain.Batch{}, false, nil
	}
	var batch questiondomain.Batch
	var found bool
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo, ok := tx.Questions().(questiondomain.AgentRepository)
		if !ok {
			return nil
		}
		var err error
		batch, found, err = repo.FindInflightByDeliveryProcessRun(ctx, questiondomain.ProcessRunID(runID))
		return err
	})
	return batch, found, err
}

func (s *Service) awaitingAnswerDelivery(ctx context.Context, sessionID domain.ID) (questiondomain.Batch, processdomain.Run, bool, error) {
	if s.uow == nil {
		return questiondomain.Batch{}, processdomain.Run{}, false, nil
	}
	var batch questiondomain.Batch
	var origin processdomain.Run
	var found bool
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo, ok := tx.Questions().(questiondomain.AgentRepository)
		if !ok {
			return nil
		}
		var err error
		batch, found, err = repo.FindAwaitingDeliveryBySession(ctx, questiondomain.SessionID(sessionID))
		if err != nil || !found {
			return err
		}
		if batch.OriginProcessRunID == nil {
			return errors.New("awaiting answer delivery has no origin process run")
		}
		finder, ok := tx.Processes().(processdomain.RunFinder)
		if !ok {
			return errors.New("process run finder is required")
		}
		origin, err = finder.FindRun(ctx, processdomain.RunID(*batch.OriginProcessRunID))
		return err
	})
	return batch, origin, found, err
}

func (s *Service) answerBatch(ctx context.Context, batchID questiondomain.BatchID) (questiondomain.Batch, bool, error) {
	if s.uow == nil || batchID == "" {
		return questiondomain.Batch{}, false, nil
	}
	var batch questiondomain.Batch
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		var err error
		batch, err = tx.Questions().FindBatch(ctx, batchID)
		return err
	})
	if err != nil {
		return questiondomain.Batch{}, false, err
	}
	return batch, true, nil
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func questionBatchDTO(batch questiondomain.Batch) questionapp.BatchDTO {
	return questionapp.BatchDTO{
		ID:                   batch.ID,
		SessionID:            batch.SessionID,
		WorkflowRunID:        batch.WorkflowRunID,
		OriginProcessRunID:   batch.OriginProcessRunID,
		Status:               batch.Status,
		DeliveryStatus:       batch.DeliveryStatus,
		DeliveryProcessRunID: batch.DeliveryProcessRunID,
		Questions:            append([]questiondomain.Question(nil), batch.Questions...),
	}
}

func (s *Service) publishQuestionBatch(batch questionapp.BatchDTO) {
	if questions, ok := s.questions.(answerQuestionCoordinator); ok {
		questions.PublishBatch(batch)
	}
}

func questionAnswersMatch(batch questiondomain.Batch, answers []questiondomain.Answer) bool {
	if len(batch.Questions) != len(answers) {
		return false
	}
	byQuestion := make(map[questiondomain.QuestionID]questiondomain.Answer, len(answers))
	for _, answer := range answers {
		if _, exists := byQuestion[answer.QuestionID]; exists {
			return false
		}
		byQuestion[answer.QuestionID] = answer
	}
	for _, question := range batch.Questions {
		answer, ok := byQuestion[question.ID]
		if !ok || !sameQuestionOption(question.SelectedOptionID, answer.SelectedOptionID) {
			return false
		}
		if strings.TrimSpace(question.CustomAnswer) != strings.TrimSpace(answer.CustomAnswer) {
			return false
		}
		if (len(question.Answer) > 0 || len(answer.Payload) > 0) && !reflect.DeepEqual(question.Answer, answer.Payload) {
			return false
		}
	}
	return true
}

func sameQuestionOption(left, right *questiondomain.OptionID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func answerResumePrompt(batch questiondomain.Batch) string {
	type answerItem struct {
		QuestionID       string         `json:"questionId"`
		Title            string         `json:"title,omitempty"`
		Body             string         `json:"body,omitempty"`
		SelectedOptionID string         `json:"selectedOptionId,omitempty"`
		SelectedLabel    string         `json:"selectedLabel,omitempty"`
		CustomAnswer     string         `json:"customAnswer,omitempty"`
		Payload          map[string]any `json:"payload,omitempty"`
	}
	payload := struct {
		BatchID string       `json:"batchId"`
		Answers []answerItem `json:"answers"`
	}{BatchID: string(batch.ID), Answers: make([]answerItem, 0, len(batch.Questions))}
	for _, question := range batch.Questions {
		item := answerItem{
			QuestionID:   string(question.ID),
			Title:        question.Title,
			Body:         question.Body,
			CustomAnswer: question.CustomAnswer,
			Payload:      question.Answer,
		}
		if question.SelectedOptionID != nil {
			item.SelectedOptionID = string(*question.SelectedOptionID)
			for _, option := range question.Options {
				if option.ID == *question.SelectedOptionID {
					item.SelectedLabel = option.Label
					break
				}
			}
		}
		payload.Answers = append(payload.Answers, item)
	}
	encoded, _ := json.Marshal(payload)
	return "Continue the suspended answer_user call using the user's persisted answers below. Do not ask this question batch again.\n\n" + string(encoded)
}

func answerResumeOptions(batch questiondomain.Batch, origin processdomain.Run) codexStartOptions {
	options := codexStartOptions{
		resumeCodexSessionID: origin.CodexSessionID,
		resumeOfProcessRunID: origin.ID,
		answerBatchID:        batch.ID,
		prompt:               answerResumePrompt(batch),
		nodeRunID:            origin.NodeRunID,
	}
	if batch.WorkflowRunID != nil {
		options.workflowRunID = domain.WorkflowRunID(*batch.WorkflowRunID)
	}
	return options
}

func (s *Service) startCodex(ctx context.Context, session domain.Session, options codexStartOptions, force bool) (DTO, error) {
	if s.processes == nil || s.codex == nil {
		return DTO{}, apperror.Wrap(ErrProcessLifecycleNotWired, apperror.CodeCodexStartFailed, apperror.CategoryInfraError, "session process lifecycle is not wired")
	}
	maxActive := 0
	if !force {
		maxActive = s.maxConcurrentAgents
	}
	dto, err := s.startCodexWithWorkdirReservation(ctx, session, options, maxActive)
	var claimErr *executionClaimNotAcquiredError
	if errors.As(err, &claimErr) {
		if claimErr.result.Status == port.ExecutionAtCapacity {
			return s.queueCodex(ctx, claimErr.result.Session, options, queuePriorityForStartOptions(claimErr.result.Session, options), queueKindForStartOptions(options))
		}
		return toDTO(claimErr.result.Session), nil
	}
	if errors.Is(err, errWorkdirBusy) {
		return s.queueCodex(ctx, session, options, queuePriorityForStartOptions(session, options), queueKindForStartOptions(options))
	}
	return dto, err
}

func (s *Service) enqueueCodex(ctx context.Context, session domain.Session, options codexStartOptions, priority domain.QueuePriority) (DTO, error) {
	dto, err := s.queueCodex(ctx, session, options, priority, queueKindForStartOptions(options))
	if err != nil {
		return DTO{}, err
	}
	s.scheduleQueueDrain()
	return dto, nil
}

func (s *Service) startCodexNow(ctx context.Context, session domain.Session, options codexStartOptions, workdir string, maxActive int) (DTO, error) {
	resolved, err := s.resolveCodexInput(ctx, session, options)
	if err != nil {
		return DTO{}, err
	}
	options = resolved
	runIDValue, err := s.generateID()
	if err != nil {
		return DTO{}, fmt.Errorf("generate process run id: %w", err)
	}
	runID := processdomain.RunID(runIDValue)
	now := s.now()
	var resumeOf *processdomain.RunID
	if options.resumeCodexSessionID != "" {
		if history, ok := s.processes.(processdomain.HistoryRepository); ok {
			latest, found, err := history.FindLatestBySession(ctx, processdomain.SessionID(session.ID))
			if err != nil {
				return DTO{}, fmt.Errorf("find process run resume parent: %w", err)
			}
			if found {
				value := latest.ID
				resumeOf = &value
			}
		}
	}
	run := processdomain.Run{
		ID:        runID,
		SessionID: processdomain.SessionID(session.ID),
		NodeRunID: options.nodeRunID,
		Status:    processdomain.StatusStarting,
		ResumeOf:  resumeOf,
		StartedAt: now,
	}
	if options.resumeOfProcessRunID != "" {
		value := options.resumeOfProcessRunID
		run.ResumeOf = &value
	}
	expectedSession := session
	if err := transitionSession(&session, domain.StatusStarting, now); err != nil {
		return DTO{}, err
	}
	claim, err := s.createProcessRunWithSessionEvent(ctx, expectedSession, run, session, options, maxActive, "session.starting", map[string]any{"processRunId": string(runID)})
	if err != nil {
		return DTO{}, err
	}
	if claim.Status != port.ExecutionClaimed {
		return toDTO(claim.Session), &executionClaimNotAcquiredError{result: claim}
	}

	handle, err := s.startCodexProcess(ctx, session, runID, options, workdir)
	if err != nil {
		failedAt := s.now()
		processEventType := "start_failed"
		sessionEventType := "session.failed"
		status := domain.StatusFailed
		if options.resumeCodexSessionID != "" {
			processEventType = "resume_failed"
			sessionEventType = "session.resume_failed"
			status = domain.StatusResumeFailed
		}
		if transitionErr := transitionSession(&session, status, failedAt); transitionErr != nil {
			return DTO{}, transitionErr
		}
		if saveErr := s.markProcessExitedWithSessionEvents(ctx, runID, processdomain.ExitResult{
			FailureReason: err.Error(),
			FinishedAt:    failedAt,
		}, session, true, []sessionEventInput{
			{
				eventType: "process." + processEventType,
				payload: map[string]any{
					"processRunId": string(runID),
					"reason":       err.Error(),
				},
			},
			{
				eventType: sessionEventType,
				payload: map[string]any{
					"processRunId": string(runID),
					"reason":       err.Error(),
				},
			},
		}); saveErr != nil {
			return DTO{}, saveErr
		}
		s.scheduleQueueDrain()
		workflowResumeStateErr := s.markWorkflowResumeFailed(ctx, session, options, processEventType, err.Error())
		code := apperror.CodeCodexStartFailed
		if options.resumeCodexSessionID != "" {
			code = apperror.CodeResumeFailed
		}
		startErr := apperror.Wrap(err, code, apperror.CategoryCodexError, "start codex process failed").WithDetails(map[string]any{
			"processRunId": string(runID),
			"sessionId":    string(session.ID),
		}).WithRetryable(options.resumeCodexSessionID != "")
		if workflowResumeStateErr != nil {
			return DTO{}, errors.Join(startErr, workflowResumeStateErr)
		}
		return DTO{}, startErr
	}
	if err := s.processes.MarkStarted(ctx, runID, handle.PID); err != nil {
		cleanupErr := s.cleanupStartedCodexAfterPersistenceFailure(ctx, session.ID, handle, options, err)
		return DTO{}, errors.Join(err, cleanupErr)
	}
	s.consumeCodexEvents(handle, session, options, workdir)
	return toDTO(session), nil
}

func (s *Service) cleanupStartedCodexAfterPersistenceFailure(ctx context.Context, sessionID domain.ID, handle processdomain.CodexHandle, options codexStartOptions, persistenceErr error) error {
	cleanupCtx, cancel := detachedCleanupContext(ctx)
	defer cancel()
	stopErr := s.codex.Stop(cleanupCtx, handle.ProcessRunID)
	if errors.Is(stopErr, processdomain.ErrProcessNotFound) {
		stopErr = nil
	}
	if stopErr != nil {
		return errors.Join(errProcessCleanupPending, fmt.Errorf("stop codex after running persistence failure: %w", stopErr))
	}
	current, findErr := s.repo.Find(cleanupCtx, sessionID)
	if findErr != nil {
		return errors.Join(stopErr, fmt.Errorf("find session after running persistence failure: %w", findErr))
	}
	status := domain.StatusFailed
	eventType := "session.failed"
	if options.resumeCodexSessionID != "" {
		status = domain.StatusResumeFailed
		eventType = "session.resume_failed"
	}
	if err := transitionSession(&current, status, s.now()); err != nil {
		return errors.Join(stopErr, err)
	}
	result := processdomain.ExitResult{
		FailureReason: "persist running process: " + persistenceErr.Error(),
		FinishedAt:    s.now(),
	}
	persistErr := s.markProcessExitedWithSessionEvents(cleanupCtx, handle.ProcessRunID, result, current, true, []sessionEventInput{
		{eventType: "process.exited", payload: processExitPayload(handle.ProcessRunID, result)},
		{eventType: eventType, payload: map[string]any{
			"processRunId": string(handle.ProcessRunID),
			"reason":       result.FailureReason,
		}},
	})
	if persistErr != nil {
		return persistErr
	}
	return s.markWorkflowResumeFailed(cleanupCtx, current, options, "resume_failed", result.FailureReason)
}

func (s *Service) markWorkflowResumeFailed(ctx context.Context, session domain.Session, options codexStartOptions, code string, message string) error {
	if options.resumeCodexSessionID == "" || session.Mode != domain.ModeWorkflow || s.workflows == nil {
		return nil
	}
	run, err := s.workflows.MarkResumeFailedForSession(ctx, domain.WorkflowResumeFailureInput{
		SessionID: session.ID,
		Code:      code,
		Message:   message,
	})
	if err != nil {
		s.appendSessionEvent(ctx, session, "workflow.resume_action_failed", map[string]any{"reason": err.Error()})
		return errors.Join(errWorkflowResumeStateNotPersisted, fmt.Errorf("mark workflow resume failed: %w", err))
	}
	s.appendSessionEvent(ctx, session, "workflow.waiting_resume_action", map[string]any{
		"workflowRunId": string(run.ID),
		"currentNodeId": run.CurrentNodeID,
	})
	return nil
}

func (s *Service) startCodexWithWorkdirReservation(ctx context.Context, session domain.Session, options codexStartOptions, maxActive int) (DTO, error) {
	workdir, err := s.codexWorkdir(ctx, session)
	if err != nil {
		return DTO{}, err
	}
	if !s.reserveWorkdir(workdir, session.ID) {
		return DTO{}, errWorkdirBusy
	}
	dto, err := s.startCodexNow(ctx, session, options, workdir, maxActive)
	if err != nil && !errors.Is(err, errProcessCleanupPending) {
		s.releaseWorkdir(workdir, session.ID)
	}
	return dto, err
}

func (s *Service) codexWorkdir(ctx context.Context, session domain.Session) (string, error) {
	if err := requireActiveWorktree(session); err != nil {
		return "", err
	}
	workdir := strings.TrimSpace(session.WorktreePath)
	if workdir != "" {
		return workdir, nil
	}
	if s.projects == nil {
		return "", errors.New("project repository is required")
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
	if err != nil {
		return "", fmt.Errorf("find session project: %w", err)
	}
	workdir = strings.TrimSpace(project.Path.Value)
	if workdir == "" {
		return "", errors.New("session workdir is empty")
	}
	return workdir, nil
}

func (s *Service) reserveWorkdir(workdir string, sessionID domain.ID) bool {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return false
	}
	s.workdirMu.Lock()
	defer s.workdirMu.Unlock()
	if s.activeWorkdirs == nil {
		s.activeWorkdirs = map[string]domain.ID{}
	}
	if owner, ok := s.activeWorkdirs[workdir]; ok && owner != sessionID {
		return false
	}
	s.activeWorkdirs[workdir] = sessionID
	return true
}

func (s *Service) releaseWorkdir(workdir string, sessionID domain.ID) {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return
	}
	s.workdirMu.Lock()
	defer s.workdirMu.Unlock()
	if s.activeWorkdirs[workdir] == sessionID {
		delete(s.activeWorkdirs, workdir)
	}
}

func (s *Service) queueCodex(ctx context.Context, session domain.Session, options codexStartOptions, priority domain.QueuePriority, kind domain.QueueKind) (DTO, error) {
	queued, err := s.queueCodexSession(ctx, session, options, priority, kind)
	if err != nil {
		return DTO{}, err
	}
	return toDTO(queued), nil
}

func (s *Service) queueCodexSession(ctx context.Context, session domain.Session, options codexStartOptions, priority domain.QueuePriority, kind domain.QueueKind) (domain.Session, error) {
	queued, event, hasEvent, err := s.prepareQueuedSession(session, options, priority, kind)
	if err != nil {
		return domain.Session{}, err
	}
	if err := s.saveSessionAndEvent(ctx, queued, event, hasEvent); err != nil {
		return domain.Session{}, err
	}
	return queued, nil
}

func (s *Service) prepareQueuedSession(session domain.Session, options codexStartOptions, priority domain.QueuePriority, kind domain.QueueKind) (domain.Session, eventdomain.DomainEvent, bool, error) {
	now := s.now()
	if err := session.QueueExecution(domain.QueueIntent{
		Kind:                    kind,
		Priority:                normalizeQueuePriority(priority),
		InitialStart:            options.initialStart,
		ReviewAfterReuseFailure: options.reviewAfterReuseFailure,
		WorkflowRunID:           options.workflowRunID,
		NodeRunID:               queueNodeRunID(options.nodeRunID),
		Prompt:                  strings.TrimSpace(options.prompt),
		ResumeCodexSessionID:    options.resumeCodexSessionID,
		ResumeOfProcessRunID:    string(options.resumeOfProcessRunID),
		AnswerBatchID:           string(options.answerBatchID),
	}, now); err != nil {
		return domain.Session{}, eventdomain.DomainEvent{}, false, fmt.Errorf("queue session %s: %w", session.ID, err)
	}
	event, hasEvent, err := s.newSessionEvent(session, "session.queued", map[string]any{
		"priority":            string(session.Queue.Priority),
		"sessionPriority":     string(normalizePriority(session.Priority)),
		"queueKind":           string(session.Queue.Kind),
		"workflowRunId":       string(session.Queue.WorkflowRunID),
		"nodeRunId":           stringValuePtr(session.Queue.NodeRunID),
		"maxConcurrentAgents": s.maxConcurrentAgents,
	})
	if err != nil {
		return domain.Session{}, eventdomain.DomainEvent{}, false, err
	}
	return session, event, hasEvent, nil
}

func (s *Service) startQueuedSession(ctx context.Context, session domain.Session, force bool) (DTO, error) {
	if session.Status != domain.StatusQueued {
		return toDTO(session), nil
	}
	if !force {
		return toDTO(session), nil
	}
	if session.Queue.Kind == domain.QueueKindAnswerUser {
		active, err := s.answerOriginStillActive(ctx, session)
		if err != nil {
			return DTO{}, err
		}
		if active {
			return toDTO(session), nil
		}
	}
	return s.startCodex(ctx, session, codexStartOptionsFromQueue(session), true)
}

func (s *Service) DrainQueuedSessions(ctx context.Context) (int, error) {
	if s == nil {
		return 0, errors.New("session usecase: nil service")
	}
	return s.drainQueuedSessions(ctx)
}

func (s *Service) drainQueuedSessions(ctx context.Context) (int, error) {
	if s.processes == nil || s.repo == nil {
		return 0, nil
	}
	started := 0
	for {
		session, ok, err := s.nextQueuedSession(ctx)
		if err != nil {
			return started, err
		}
		if !ok {
			return started, nil
		}
		launched := false
		atCapacity := false
		if err := s.withSessionLock(ctx, session.ID, func(ctx context.Context) error {
			current, err := s.repo.Find(ctx, session.ID)
			if err != nil {
				return fmt.Errorf("find queued session: %w", err)
			}
			if current.Status != domain.StatusQueued {
				return nil
			}
			if current.Queue.Kind == domain.QueueKindAnswerUser {
				active, err := s.answerOriginStillActive(ctx, current)
				if err != nil {
					return err
				}
				if active {
					atCapacity = true
					return nil
				}
			}
			if s.codex == nil {
				return ErrProcessLifecycleNotWired
			}
			if _, err := s.startCodexWithWorkdirReservation(ctx, current, codexStartOptionsFromQueue(current), s.maxConcurrentAgents); err != nil {
				var claimErr *executionClaimNotAcquiredError
				if errors.As(err, &claimErr) {
					if claimErr.result.Status == port.ExecutionAtCapacity {
						atCapacity = true
					}
					return nil
				}
				if errors.Is(err, errWorkdirBusy) {
					atCapacity = true
					return nil
				}
				if errors.Is(err, errProcessCleanupPending) {
					return err
				}
				saved, findErr := s.repo.Find(ctx, current.ID)
				if findErr != nil {
					return fmt.Errorf("find failed queued session: %w", findErr)
				}
				if saved.Status == domain.StatusResumeFailed {
					if errors.Is(err, errWorkflowResumeStateNotPersisted) {
						return err
					}
					launched = true
					return nil
				}
				if current.Mode == domain.ModeWorkflow && current.Queue.WorkflowRunID != "" && current.Queue.NodeRunID != nil {
					if _, failErr := s.handleWorkflowNodeFailure(ctx, saved, current.Queue.WorkflowRunID, current.Queue.NodeRunID, "codex_start_failed", err.Error()); failErr != nil {
						return failErr
					}
					launched = true
					return nil
				}
				if saved.Status == domain.StatusFailed || saved.Status == domain.StatusResumeFailed {
					launched = true
					return nil
				}
				return err
			}
			launched = true
			return nil
		}); err != nil {
			return started, err
		}
		if atCapacity {
			return started, nil
		}
		if launched {
			started++
		}
	}
}

func (s *Service) nextQueuedSession(ctx context.Context) (domain.Session, bool, error) {
	queued, err := s.repo.ListQueued(ctx)
	if err != nil {
		return domain.Session{}, false, err
	}
	if len(queued) == 0 {
		return domain.Session{}, false, nil
	}
	sort.SliceStable(queued, func(i, j int) bool {
		leftQueuePriority := queuePriorityRank(queued[i].Queue.Priority)
		rightQueuePriority := queuePriorityRank(queued[j].Queue.Priority)
		if leftQueuePriority != rightQueuePriority {
			return leftQueuePriority < rightQueuePriority
		}
		leftPriority := priorityRank(queued[i].Priority)
		rightPriority := priorityRank(queued[j].Priority)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftQueuedAt := queued[i].UpdatedAt
		if queued[i].QueuedAt != nil {
			leftQueuedAt = *queued[i].QueuedAt
		}
		rightQueuedAt := queued[j].UpdatedAt
		if queued[j].QueuedAt != nil {
			rightQueuedAt = *queued[j].QueuedAt
		}
		if !leftQueuedAt.Equal(rightQueuedAt) {
			return leftQueuedAt.Before(rightQueuedAt)
		}
		return queued[i].ID < queued[j].ID
	})
	return queued[0], true, nil
}

func priorityRank(priority domain.Priority) int {
	switch normalizePriority(priority) {
	case domain.PriorityHigh:
		return 0
	case domain.PriorityLow:
		return 2
	default:
		return 1
	}
}

func queuePriorityRank(priority domain.QueuePriority) int {
	switch normalizeQueuePriority(priority) {
	case domain.QueuePriorityImmediate:
		return 0
	case domain.QueuePriorityHigh:
		return 1
	case domain.QueuePriorityLow:
		return 3
	default:
		return 2
	}
}

func queuePriorityForSession(session domain.Session) domain.QueuePriority {
	switch normalizePriority(session.Priority) {
	case domain.PriorityHigh:
		return domain.QueuePriorityHigh
	case domain.PriorityLow:
		return domain.QueuePriorityLow
	default:
		return domain.QueuePriorityMedium
	}
}

func normalizeQueuePriority(priority domain.QueuePriority) domain.QueuePriority {
	switch priority {
	case domain.QueuePriorityImmediate, domain.QueuePriorityHigh, domain.QueuePriorityLow:
		return priority
	default:
		return domain.QueuePriorityMedium
	}
}

func queueKindForStartOptions(options codexStartOptions) domain.QueueKind {
	if options.answerBatchID != "" {
		return domain.QueueKindAnswerUser
	}
	if options.queueKind != "" {
		return options.queueKind
	}
	if options.resumeCodexSessionID != "" {
		return domain.QueueKindResume
	}
	return domain.QueueKindStart
}

func queuePriorityForStartOptions(session domain.Session, options codexStartOptions) domain.QueuePriority {
	if options.answerBatchID != "" {
		return domain.QueuePriorityHigh
	}
	return queuePriorityForSession(session)
}

func codexStartOptionsFromQueue(session domain.Session) codexStartOptions {
	nodeRunID := queueProcessNodeRunID(session.Queue.NodeRunID)
	return codexStartOptions{
		resumeCodexSessionID:    session.Queue.ResumeCodexSessionID,
		resumeOfProcessRunID:    processdomain.RunID(session.Queue.ResumeOfProcessRunID),
		answerBatchID:           questiondomain.BatchID(session.Queue.AnswerBatchID),
		workflowRunID:           session.Queue.WorkflowRunID,
		nodeRunID:               nodeRunID,
		prompt:                  session.Queue.Prompt,
		queueKind:               session.Queue.Kind,
		reviewAfterReuseFailure: session.Queue.ReviewAfterReuseFailure,
		workflowResultRetry:     isWorkflowResultRetryPrompt(session.Queue.Prompt),
		initialStart:            session.Queue.InitialStart,
	}
}

func queueNodeRunID(id *processdomain.NodeRunID) *domain.NodeRunID {
	if id == nil {
		return nil
	}
	value := domain.NodeRunID(*id)
	return &value
}

func queueProcessNodeRunID(id *domain.NodeRunID) *processdomain.NodeRunID {
	if id == nil {
		return nil
	}
	value := processdomain.NodeRunID(*id)
	return &value
}

func transitionSession(session *domain.Session, next domain.Status, now time.Time) error {
	if err := session.TransitionTo(next, now); err != nil {
		return fmt.Errorf("transition session %s: %w", session.ID, err)
	}
	return nil
}

func transitionSessionToWaitingApproval(session *domain.Session, initialStart bool, now time.Time) error {
	if err := transitionSession(session, domain.StatusWaitingApproval, now); err != nil {
		return err
	}
	// GLUE: InitialStart survives approval for first-execution validation; remove when workflow state owns it.
	session.Queue = domain.QueueIntent{InitialStart: initialStart}
	return nil
}

func (s *Service) startCodexProcess(ctx context.Context, session domain.Session, runID processdomain.RunID, options codexStartOptions, workdir string) (processdomain.CodexHandle, error) {
	attachmentPaths, imagePaths, err := s.codexAttachmentPaths(ctx, session.ID)
	if err != nil {
		return processdomain.CodexHandle{}, err
	}
	prompt := strings.TrimSpace(options.prompt)
	artifactDir := ""
	if s.artifacts != nil {
		var err error
		artifactDir, err = s.artifacts.EnsureArtifactDir(ctx, session.ID)
		if err != nil {
			return processdomain.CodexHandle{}, fmt.Errorf("prepare artifact directory: %w", err)
		}
	}
	if options.resumeCodexSessionID != "" {
		transcript, found, err := s.processes.FindTranscriptSource(ctx, options.resumeCodexSessionID)
		if err != nil {
			return processdomain.CodexHandle{}, err
		}
		if !found && options.queueKind == domain.QueueKindPromptAppend {
			return s.startCodexFallback(ctx, session, runID, options, workdir, artifactDir, attachmentPaths, imagePaths)
		}
		if !found {
			return processdomain.CodexHandle{}, processdomain.ErrTranscriptUnavailable
		}
		handle, err := s.codex.Resume(ctx, processdomain.CodexResumeInput{
			ProcessRunID:    runID,
			SessionID:       processdomain.SessionID(session.ID),
			CodexSessionID:  options.resumeCodexSessionID,
			Transcript:      transcript,
			Workdir:         workdir,
			ArtifactDir:     artifactDir,
			Prompt:          prompt,
			Model:           strings.TrimSpace(session.Config.CodexModel),
			ReasoningEffort: strings.TrimSpace(session.Config.ReasoningEffort),
			PermissionMode:  strings.TrimSpace(session.Config.PermissionMode),
			FastMode:        session.Config.FastMode,
		})
		if errors.Is(err, processdomain.ErrTranscriptUnavailable) && options.queueKind == domain.QueueKindPromptAppend {
			return s.startCodexFallback(ctx, session, runID, options, workdir, artifactDir, attachmentPaths, imagePaths)
		}
		return handle, err
	}
	prompt = promptWithArtifactGuidance(prompt, artifactDir)
	return s.codex.Start(ctx, newCodexStartInput(session, runID, workdir, artifactDir, prompt, attachmentPaths, imagePaths))
}

func (s *Service) startCodexFallback(ctx context.Context, session domain.Session, runID processdomain.RunID, options codexStartOptions, workdir string, artifactDir string, attachmentPaths []string, imagePaths []string) (processdomain.CodexHandle, error) {
	prompt := promptWithArtifactGuidance(options.fallbackPrompt, artifactDir)
	return s.codex.Start(ctx, newCodexStartInput(session, runID, workdir, artifactDir, prompt, attachmentPaths, imagePaths))
}

func newCodexStartInput(session domain.Session, runID processdomain.RunID, workdir string, artifactDir string, prompt string, attachmentPaths []string, imagePaths []string) processdomain.CodexStartInput {
	return processdomain.CodexStartInput{
		ProcessRunID:    runID,
		SessionID:       processdomain.SessionID(session.ID),
		Workdir:         workdir,
		ArtifactDir:     artifactDir,
		Prompt:          promptWithAttachments(promptWithAnyCodeGuidance(prompt, session), attachmentPaths),
		Model:           strings.TrimSpace(session.Config.CodexModel),
		ReasoningEffort: strings.TrimSpace(session.Config.ReasoningEffort),
		PermissionMode:  strings.TrimSpace(session.Config.PermissionMode),
		FastMode:        session.Config.FastMode,
		AttachmentPaths: attachmentPaths,
		ImagePaths:      imagePaths,
	}
}

func (s *Service) requirePendingChatResume(ctx context.Context, sessionID domain.ID) error {
	pending, err := s.repo.ListPendingPromptAppends(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list pending prompt appends: %w", err)
	}
	if len(pending) == 0 {
		return pendingPromptRequiredError(sessionID)
	}
	return nil
}

func pendingPromptRequiredError(sessionID domain.ID) error {
	return apperror.New(apperror.CodePendingPromptRequired, apperror.CategoryValidationError, "没有待执行的追加描述，请先输入追加描述").
		WithDetails(map[string]any{"sessionId": string(sessionID)})
}

func (s *Service) resolveCodexInput(ctx context.Context, session domain.Session, options codexStartOptions) (codexStartOptions, error) {
	appends, err := s.repo.ListPromptAppends(ctx, session.ID)
	if err != nil {
		return codexStartOptions{}, fmt.Errorf("list prompt appends: %w", err)
	}
	pendingPrompt, pendingIDs, err := s.pendingPromptInput(ctx, session.ID, appends)
	if err != nil {
		return codexStartOptions{}, err
	}
	prompt := strings.TrimSpace(options.prompt)
	if options.queueKind == domain.QueueKindPromptAppend {
		if len(pendingIDs) == 0 {
			return codexStartOptions{}, pendingPromptRequiredError(session.ID)
		}
		options.fallbackPrompt = rebuiltSessionPrompt(session, prompt, true, appends)
		if options.resumeCodexSessionID != "" {
			prompt = pendingPrompt
		} else {
			prompt = options.fallbackPrompt
		}
	} else if options.reviewAfterReuseFailure {
		prompt = rebuiltSessionPrompt(session, prompt, true, appends)
	} else if session.Mode == domain.ModeWorkflow {
		pendingIDs = nil
	} else if options.resumeCodexSessionID != "" {
		if session.Mode != domain.ModeWorkflow && options.answerBatchID == "" && len(pendingIDs) == 0 {
			return codexStartOptions{}, pendingPromptRequiredError(session.ID)
		}
		prompt = joinPromptParts(prompt, pendingPrompt)
	} else if session.Mode != domain.ModeWorkflow {
		prompt = rebuiltSessionPrompt(session, prompt, options.reviewAfterReuseFailure, appends)
	}
	if strings.TrimSpace(prompt) == "" && !options.initialStart {
		return codexStartOptions{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "Codex 执行提示不能为空")
	}
	options.prompt = prompt
	options.promptAppendIDs = pendingIDs
	return options, nil
}

func (s *Service) pendingPromptInput(ctx context.Context, sessionID domain.ID, appends []domain.PromptAppend) (string, []string, error) {
	parts := make([]string, 0, len(appends))
	ids := make([]string, 0, len(appends))
	for _, promptAppend := range appends {
		if promptAppend.Status != domain.PromptAppendPending {
			continue
		}
		body := strings.TrimSpace(promptAppend.Body)
		if body == "" {
			continue
		}
		attachments, err := s.listPromptAppendAttachments(ctx, sessionID, promptAppend.ID)
		if err != nil {
			return "", nil, err
		}
		paths, _ := attachmentPathsFromAttachments(attachments)
		parts = append(parts, promptWithAttachments(body, paths))
		ids = append(ids, promptAppend.ID)
	}
	return strings.Join(parts, "\n\n"), ids, nil
}

func joinPromptParts(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

const rebuiltPromptNotice = "无法复用已有 Codex 会话，请基于以下上下文复查当前状态并继续处理。"
const anyCodePromptGuidance = "AnyCode 提供 `answer_user` MCP 工具，可用于向用户提出选项问题。若需求、验收标准、执行取舍或下一步不确定，请使用 `answer_user` 咨询用户；如果上下文足够明确，请直接继续执行，不要无意义打断用户。`request_user_input` 不是 AnyCode 会话内的用户提问工具，可能只属于外层平台或特定计划模式；即使你在说明中看到它，也不要使用 `request_user_input` 来代替 AnyCode 的 `answer_user`。\n\nAnyCode 卡片的 TODO List 仅来自 Codex 的结构化计划事件。处理包含多个可执行步骤的任务时，必须调用 `update_plan` 创建计划，并在步骤状态变化后持续调用 `update_plan` 更新状态；不要只在回复中输出 Markdown checklist。单步骤任务或纯问答无需创建计划。"
const managedWorktreePromptGuidance = "当前工作目录是 AnyCode 管理的卡片工作树。不得删除、移动、重建或清理当前工作树，也不得执行会移除该工作树的命令；若必须手动合并，请使用当前卡片分支名执行非 fast-forward merge，并保留 Git 默认合并提交信息，以便工作树缺失时从基础分支日志恢复 Diff；卡片关闭时由 AnyCode 负责清理仍存在的工作树。"
const artifactPromptGuidance = "本卡片生成的图片、截图、PDF、音视频、压缩包和其他产物统一写入环境变量 `ANYCODE_ARTIFACT_DIR` 指向的目录。需要生图时直接使用 Codex 可用的图片生成能力，并将结果保存到该目录；不要把生成物写入项目工作树。"

func promptWithArtifactGuidance(prompt string, artifactDir string) string {
	if strings.TrimSpace(artifactDir) == "" {
		return prompt
	}
	return joinPromptParts(prompt, artifactPromptGuidance)
}

func promptWithAnyCodeGuidance(prompt string, session domain.Session) string {
	guidance := anyCodePromptGuidance
	if strings.TrimSpace(session.BaseBranch) != "" {
		guidance += "\n\n" + managedWorktreePromptGuidance
	}
	return joinPromptParts(prompt, guidance)
}

func rebuiltSessionPrompt(session domain.Session, nodePrompt string, reviewAfterReuseFailure bool, appends []domain.PromptAppend) string {
	original := strings.TrimSpace(session.Requirement)
	nodePrompt = strings.TrimSpace(nodePrompt)
	bodies := make([]string, 0, len(appends))
	for _, promptAppend := range appends {
		body := strings.TrimSpace(promptAppend.Body)
		if body == "" {
			continue
		}
		bodies = append(bodies, body)
	}
	if len(bodies) == 0 && !reviewAfterReuseFailure {
		if nodePrompt != "" {
			return nodePrompt
		}
		return original
	}
	parts := []string{rebuiltPromptNotice}
	if original != "" {
		parts = append(parts, "原始需求：\n"+original)
	}
	for _, body := range bodies {
		parts = append(parts, "追加描述：\n"+body)
	}
	if nodePrompt != "" {
		parts = append(parts, "当前流程节点提示词：\n"+nodePrompt)
	}
	return strings.Join(parts, "\n\n")
}

func (s *Service) codexAttachmentPaths(ctx context.Context, sessionID domain.ID) ([]string, []string, error) {
	attachments, err := s.listSessionAttachments(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	paths, imagePaths := attachmentPathsFromAttachments(attachments)
	return paths, imagePaths, nil
}

func attachmentPathsFromAttachments(attachments []domain.SessionAttachment) ([]string, []string) {
	paths := make([]string, 0, len(attachments))
	imagePaths := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		path := strings.TrimSpace(attachment.Path)
		if path == "" {
			continue
		}
		paths = append(paths, path)
		if strings.HasPrefix(strings.ToLower(attachment.MimeType), "image/") {
			imagePaths = append(imagePaths, path)
		}
	}
	return paths, imagePaths
}

func promptWithAttachments(prompt string, paths []string) string {
	if len(paths) == 0 {
		return prompt
	}
	var builder strings.Builder
	builder.WriteString(prompt)
	builder.WriteString("\n\nAttached files available on disk:\n")
	for _, path := range paths {
		builder.WriteString("- ")
		builder.WriteString(path)
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (s *Service) consumeCodexEvents(handle processdomain.CodexHandle, session domain.Session, options codexStartOptions, workdir string) {
	done := make(chan struct{})
	s.processConsumers.Store(handle.ProcessRunID, (<-chan struct{})(done))
	go func() {
		defer func() {
			s.processConsumers.Delete(handle.ProcessRunID)
			close(done)
		}()
		events, err := s.codex.Events(context.Background(), handle)
		if err != nil {
			cleanupCtx, cancel := detachedCleanupContext(context.Background())
			stopErr := s.codex.Stop(cleanupCtx, handle.ProcessRunID)
			cancel()
			if errors.Is(stopErr, processdomain.ErrProcessNotFound) {
				stopErr = nil
			}
			if stopErr != nil {
				log.Printf("stop codex after event stream failure: session=%s process_run=%s error=%v", session.ID, handle.ProcessRunID, stopErr)
				return
			}
			s.releaseWorkdir(workdir, session.ID)
			s.handleCodexProcessExit(session, handle, options, processdomain.ExitResult{
				FailureReason: fmt.Sprintf("consume codex events: %v", err),
				FinishedAt:    s.now(),
			}, nil)
			return
		}
		exitResult := processdomain.ExitResult{}
		workflowResults := map[string]any(nil)
		var persistenceFailure *processdomain.ExitResult
		for event := range events {
			if result, ok := exitResultFromEvent(event); ok {
				exitResult = result
			}
			workflowResults = workflowResultsAfterEvent(workflowResults, event)
			if codexEventAcknowledgesPrompt(event) {
				options.resumeAcknowledged = true
			}
			if event.Type == "process.exit" {
				continue
			}
			if persistenceFailure != nil {
				continue
			}
			if err := s.persistCodexEventWithRetry(session.ID, handle, event); err != nil {
				cleanupCtx, cancel := detachedCleanupContext(context.Background())
				stopErr := s.codex.Stop(cleanupCtx, handle.ProcessRunID)
				cancel()
				if errors.Is(stopErr, processdomain.ErrProcessNotFound) {
					stopErr = nil
				}
				reason := fmt.Sprintf("bind codex transcript: %v", err)
				if stopErr != nil {
					log.Printf("stop codex after transcript bind failure: session=%s process_run=%s error=%v", session.ID, handle.ProcessRunID, stopErr)
					reason += fmt.Sprintf("; stop codex: %v", stopErr)
				}
				failure := processdomain.ExitResult{
					FailureCode:   "codex_transcript_unavailable",
					FailureReason: reason,
					FinishedAt:    s.now(),
				}
				if stopErr == nil {
					s.releaseWorkdir(workdir, session.ID)
					s.handleCodexProcessExit(session, handle, options, failure, nil)
					return
				}
				persistenceFailure = &failure
			}
		}
		if persistenceFailure != nil {
			exitResult = *persistenceFailure
			workflowResults = nil
		}
		s.releaseWorkdir(workdir, session.ID)
		s.handleCodexProcessExit(session, handle, options, exitResult, workflowResults)
	}()
}

func (s *Service) hasProcessConsumer(runID processdomain.RunID) bool {
	_, ok := s.processConsumers.Load(runID)
	return ok
}

func (s *Service) processConsumerDone(runID processdomain.RunID) (<-chan struct{}, bool) {
	value, ok := s.processConsumers.Load(runID)
	if !ok {
		return nil, false
	}
	done, ok := value.(<-chan struct{})
	return done, ok
}

func (s *Service) persistCodexEventWithRetry(sessionID domain.ID, handle processdomain.CodexHandle, event processdomain.CodexEvent) error {
	retryAcknowledgement := event.Transcript != nil || codexEventAcknowledgesPrompt(event)
	retryDelay := s.processExitDelay
	if retryDelay == nil {
		retryDelay = processExitRetryDelay
	}
	retryCtx := s.lifecycleCtx
	if retryCtx == nil {
		retryCtx = context.Background()
	}
	for attempt := 0; ; attempt++ {
		if retryCtx.Err() != nil {
			return retryCtx.Err()
		}
		err := s.withSessionLock(retryCtx, sessionID, func(ctx context.Context) error {
			return s.persistCodexEvent(ctx, sessionID, handle, event)
		})
		if err == nil {
			return nil
		}
		if event.Transcript != nil {
			return err
		}
		if !retryAcknowledgement {
			log.Printf("persist codex event: session=%s process_run=%s type=%s error=%v", sessionID, handle.ProcessRunID, event.Type, err)
			return nil
		}
		timer := time.NewTimer(retryDelay(attempt))
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return retryCtx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) persistCodexEvent(ctx context.Context, sessionID domain.ID, handle processdomain.CodexHandle, event processdomain.CodexEvent) error {
	current, err := s.repo.Find(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("find session for codex event: %w", err)
	}
	activeRun := false
	active := processdomain.Run{}
	if s.processes != nil {
		found, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(sessionID))
		if err != nil {
			return fmt.Errorf("find active process for codex event: %w", err)
		}
		active = found
		activeRun = ok && active.ID == handle.ProcessRunID
	}
	if activeRun && active.Status == processdomain.StatusStarting && event.Transcript != nil {
		if err := s.bindProcessTranscript(ctx, &current, handle, *event.Transcript); err != nil {
			return err
		}
	}
	saveSession := false
	saveFilesChanged := false
	extraEvents := []sessionEventInput(nil)
	if activeRun && codexEventCanUpdateSession(current.Status) {
		if todoList, ok := todoListFromCodexEvent(event); ok && !slices.Equal(current.TodoList.Items, todoList.Items) {
			current.TodoList = todoList
			current.UpdatedAt = s.now()
			saveSession = true
			extraEvents = append(extraEvents, sessionEventInput{
				eventType: "session.todo_list_updated",
				payload: map[string]any{
					"completed": todoList.Completed(),
					"total":     todoList.Total(),
				},
			})
		}
		if completedCodexFileChange(event) && s.diffCounter != nil {
			filesChanged, countErr := s.diffCounter.CountSessionChangedFiles(ctx, sessionID)
			if countErr != nil {
				log.Printf("refresh session diff count: session=%s event=%s error=%v", sessionID, event.EventID, countErr)
			} else if filesChanged < 0 {
				log.Printf("refresh session diff count: session=%s event=%s error=negative count %d", sessionID, event.EventID, filesChanged)
			} else if filesChanged != current.FilesChanged {
				current.FilesChanged = filesChanged
				current.UpdatedAt = s.now()
				saveSession = true
				saveFilesChanged = true
				extraEvents = append(extraEvents, sessionEventInput{
					eventType: "session.diff_changed",
					payload:   map[string]any{"filesChanged": filesChanged},
				})
			}
		}
	}
	promptDelivered := codexEventAcknowledgesPrompt(event)
	extraEvents = append(extraEvents, s.archiveCodexEventImages(ctx, current, handle, &event)...)
	return s.publishCodexEventWithSessionUpdates(ctx, current, handle.ProcessRunID, event, saveSession, saveFilesChanged, promptDelivered, extraEvents...)
}

func completedCodexFileChange(event processdomain.CodexEvent) bool {
	if event.Phase != processdomain.CodexPhaseCompleted {
		return false
	}
	content, ok := event.Content.(processdomain.CodexFileChangeContent)
	return ok && len(content.Changes) > 0
}

func (s *Service) bindProcessTranscript(ctx context.Context, current *domain.Session, handle processdomain.CodexHandle, source processdomain.CodexTranscriptSource) error {
	if current == nil {
		return errors.New("bind process transcript: nil session")
	}
	if source.CodexSessionID == "" || source.RelativePath == "" {
		return processdomain.ErrTranscriptUnavailable
	}
	if err := transitionSession(current, domain.StatusRunning, s.now()); err != nil {
		return err
	}
	current.CodexSessionID = source.CodexSessionID
	current.UpdatedAt = s.now()
	events, err := s.newSessionEvents(*current, []sessionEventInput{
		{eventType: "process.transcript_bound", payload: map[string]any{
			"processRunId": string(handle.ProcessRunID), "codexSessionId": source.CodexSessionID,
		}},
		{eventType: "session.running", payload: map[string]any{
			"processRunId": string(handle.ProcessRunID), "pid": handle.PID, "codexSessionId": source.CodexSessionID,
		}},
	})
	if err != nil {
		return err
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().BindTranscript(ctx, handle.ProcessRunID, handle.PID, source); err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, *current); err != nil {
				return err
			}
			for _, event := range events {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	} else {
		// GLUE: isolated application tests may omit transactions; production always injects a unit of work.
		if err := s.processes.BindTranscript(ctx, handle.ProcessRunID, handle.PID, source); err != nil {
			return err
		}
		if err := s.repo.Save(ctx, *current); err != nil {
			return err
		}
		for _, event := range events {
			if s.events != nil {
				if err := s.events.Append(ctx, event); err != nil {
					return err
				}
			}
		}
	}
	for _, event := range events {
		s.publishSessionEvent(ctx, event)
	}
	return nil
}

func (s *Service) archiveCodexEventImages(ctx context.Context, current domain.Session, handle processdomain.CodexHandle, event *processdomain.CodexEvent) []sessionEventInput {
	if event == nil || event.Content == nil {
		return nil
	}
	archive := func(images []processdomain.CodexImage, allowInline bool) ([]processdomain.CodexImage, []sessionEventInput) {
		if !allowInline || s.artifactPublisher == nil {
			return nil, nil
		}
		stored := make([]processdomain.CodexImage, 0, len(images))
		failures := make([]sessionEventInput, 0)
		for index, image := range images {
			if image.SourceKind != "inline" && image.SourceKind != "inline_base64" && !strings.HasPrefix(image.Source, "data:") {
				continue
			}
			data, mimeType, err := decodeInlineArtifact(image.Source, image.MimeType)
			if err != nil {
				failures = append(failures, artifactArchiveFailure(event.EventID, index, image.MimeType))
				continue
			}
			if image.MimeType != "" {
				mimeType = image.MimeType
			}
			artifact, err := s.artifactPublisher.PublishInlineArtifact(ctx, domain.InlineArtifactRequest{
				SessionID:     current.ID,
				Data:          data,
				Filename:      inlineArtifactFilename(event.EventID, index, mimeType),
				MimeType:      mimeType,
				SourceType:    domain.AttachmentSourceCodex,
				SourceID:      event.EventID,
				SourceKey:     fmt.Sprintf("%s:%s:%d", handle.ProcessRunID, event.EventID, index),
				ProcessRunID:  string(handle.ProcessRunID),
				CorrelationID: event.CorrelationID,
			})
			if err != nil {
				failures = append(failures, artifactArchiveFailure(event.EventID, index, mimeType))
				continue
			}
			if artifact.PreviewKind == domain.PreviewKindImage {
				stored = append(stored, processdomain.CodexImage{
					Source:     "/files/" + string(artifact.ID) + "/preview",
					Detail:     image.Detail,
					SourceKind: "stored",
					MimeType:   artifact.MimeType,
				})
			}
		}
		return stored, failures
	}
	var failures []sessionEventInput
	switch content := event.Content.(type) {
	case processdomain.CodexMessageContent:
		images, archiveFailures := archive(content.Images, strings.EqualFold(content.Role, "assistant"))
		content.Images = images
		event.Content = content
		failures = append(failures, archiveFailures...)
	case processdomain.CodexToolContent:
		content.Output = sanitizeCodexArtifactOutput(content.Output, content.Images)
		qualifiedName := strings.ToLower(strings.TrimSpace(content.QualifiedName))
		if strings.Contains(qualifiedName, "anycode") && strings.HasSuffix(qualifiedName, "publish_artifact") {
			content.Images = nil
			event.Content = content
			break
		}
		images, archiveFailures := archive(content.Images, true)
		content.Images = images
		event.Content = content
		failures = append(failures, archiveFailures...)
	case processdomain.CodexUnknownContent:
		payload, _ := sanitizeCodexPayloadValue(content.Payload, false).(map[string]any)
		content.Payload = payload
		event.Content = content
	}
	return failures
}

func sanitizeCodexArtifactOutput(output processdomain.CodexStructuredText, artifacts []processdomain.CodexImage) processdomain.CodexStructuredText {
	text := strings.TrimSpace(output.Text)
	if text == "" {
		return output
	}
	var value any
	if json.Unmarshal([]byte(text), &value) == nil {
		if encoded, err := json.Marshal(sanitizeCodexPayloadValue(value, false)); err == nil {
			output.Text = string(encoded)
			return output
		}
	}
	for _, artifact := range artifacts {
		if source := strings.TrimSpace(artifact.Source); source != "" {
			output.Text = strings.ReplaceAll(output.Text, source, "[artifact source omitted]")
		}
	}
	if len(output.Text) > maxPersistedCodexStringBytes {
		output.Text = "[omitted large value]"
	}
	return output
}

func artifactArchiveFailure(eventID string, index int, mimeType string) sessionEventInput {
	return sessionEventInput{
		eventType: "session.artifact_archive_failed",
		payload: map[string]any{
			"message":       "Codex 产物归档失败，原始内容未写入会话历史",
			"codexEventId":  eventID,
			"artifactIndex": index,
			"mimeType":      strings.TrimSpace(mimeType),
		},
	}
}

func decodeInlineArtifact(source string, declaredMimeType string) ([]byte, string, error) {
	header, encoded, ok := strings.Cut(source, ",")
	if !ok || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		if strings.TrimSpace(declaredMimeType) == "" {
			return nil, "", errors.New("unsupported inline artifact encoding")
		}
		header = "data:" + strings.TrimSpace(declaredMimeType) + ";base64"
		encoded = source
	}
	if base64.StdEncoding.DecodedLen(len(encoded)) > 25<<20 {
		return nil, "", errors.New("inline artifact exceeds 25 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", err
	}
	mimeType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return data, mimeType, nil
}

func inlineArtifactFilename(eventID string, index int, mimeType string) string {
	extension := map[string]string{
		"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif",
		"application/pdf": ".pdf", "audio/mpeg": ".mp3", "audio/wav": ".wav", "audio/ogg": ".ogg",
		"video/mp4": ".mp4", "video/webm": ".webm", "application/json": ".json", "text/plain": ".txt",
	}[strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))]
	name := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, eventID)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "output"
	}
	return fmt.Sprintf("%s-%d%s", name, index+1, extension)
}

func codexEventAcknowledgesPrompt(event processdomain.CodexEvent) bool {
	switch strings.ToLower(strings.TrimSpace(event.Type)) {
	case "task.started", "turn.started":
		return true
	}
	message, ok := event.Content.(processdomain.CodexMessageContent)
	return ok && strings.EqualFold(strings.TrimSpace(message.Role), "user")
}

func codexEventCanUpdateSession(status domain.Status) bool {
	switch status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser:
		return true
	default:
		return false
	}
}

func (s *Service) handleCodexProcessExit(session domain.Session, handle processdomain.CodexHandle, options codexStartOptions, exitResult processdomain.ExitResult, workflowResults map[string]any) {
	if exitResult.FinishedAt.IsZero() {
		exitResult.FinishedAt = s.now()
	}
	retryDelay := s.processExitDelay
	if retryDelay == nil {
		retryDelay = processExitRetryDelay
	}
	retryCtx := s.lifecycleCtx
	if retryCtx == nil {
		retryCtx = context.Background()
	}
	processPersisted := false
	continueWorkflow := false
	transitionAttempted := false
	var workflowAdvance *domain.WorkflowAdvance
	var workflowTransitionErr error
	var workflowApplyErr error
	for attempt := 0; ; attempt++ {
		if retryCtx.Err() != nil {
			return
		}
		err := s.withSessionLock(retryCtx, session.ID, func(ctx context.Context) error {
			if !processPersisted {
				_, shouldContinue, err := s.persistCodexProcessExit(ctx, session, handle, options, exitResult, workflowResults)
				if err != nil {
					return err
				}
				processPersisted = true
				continueWorkflow = shouldContinue
			}
			if !continueWorkflow {
				return nil
			}
			current, err := s.repo.Find(ctx, session.ID)
			if err != nil {
				return fmt.Errorf("find workflow session after process exit: %w", err)
			}
			if !workflowProcessExitPending(current.Status) {
				continueWorkflow = false
				return nil
			}
			if !transitionAttempted {
				transitionAttempted = true
				advance, err := s.workflowAdvanceAfterProcessExit(ctx, handle, options, exitResult, workflowResults)
				if err != nil {
					if errors.Is(err, errWorkflowResultFailureNotPersisted) {
						transitionAttempted = false
						return err
					}
					workflowTransitionErr = err
				} else {
					workflowAdvance = &advance
				}
			}
			if workflowTransitionErr != nil {
				if err := transitionSession(&current, domain.StatusFailed, s.now()); err != nil {
					return err
				}
				return s.saveSessionWithEvent(ctx, current, "workflow.failed", map[string]any{
					"workflowRunId": string(options.workflowRunID),
					"nodeRunId":     string(domain.NodeRunID(*options.nodeRunID)),
					"reason":        workflowTransitionErr.Error(),
				})
			}
			if workflowAdvance == nil {
				return errors.New("workflow advance is missing after process exit")
			}
			if workflowApplyErr != nil {
				if err := transitionSession(&current, domain.StatusFailed, s.now()); err != nil {
					return err
				}
				return s.saveSessionWithEvent(ctx, current, "workflow.failed", map[string]any{
					"workflowRunId": string(options.workflowRunID),
					"nodeRunId":     string(domain.NodeRunID(*options.nodeRunID)),
					"reason":        workflowApplyErr.Error(),
				})
			}
			_, err = s.applyWorkflowAdvance(ctx, current, *workflowAdvance, workflowAdvanceOptions{})
			if err == nil || !workflowAdvanceHasExternalEffects(*workflowAdvance) {
				return err
			}
			workflowApplyErr = err
			latest, findErr := s.repo.Find(ctx, session.ID)
			if findErr != nil {
				return fmt.Errorf("find workflow session after apply failure: %w", findErr)
			}
			if !workflowProcessExitPending(latest.Status) {
				continueWorkflow = false
				return nil
			}
			if err := transitionSession(&latest, domain.StatusFailed, s.now()); err != nil {
				return err
			}
			return s.saveSessionWithEvent(ctx, latest, "workflow.failed", map[string]any{
				"workflowRunId": string(options.workflowRunID),
				"nodeRunId":     string(domain.NodeRunID(*options.nodeRunID)),
				"reason":        workflowApplyErr.Error(),
			})
		})
		if err == nil {
			break
		}
		timer := time.NewTimer(retryDelay(attempt))
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	s.scheduleQueueDrain()
}

func workflowProcessExitPending(status domain.Status) bool {
	switch status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser:
		return true
	default:
		return false
	}
}

func workflowAdvanceHasExternalEffects(advance domain.WorkflowAdvance) bool {
	return advance.Close || advance.Merge != nil || advance.Expr != nil
}

func processExitRetryDelay(attempt int) time.Duration {
	delay := 100 * time.Millisecond
	for index := 0; index < attempt && delay < processExitRetryMaxDelay; index++ {
		delay *= 2
		if delay > processExitRetryMaxDelay {
			return processExitRetryMaxDelay
		}
	}
	return delay
}

func (s *Service) persistCodexProcessExit(ctx context.Context, session domain.Session, handle processdomain.CodexHandle, options codexStartOptions, exitResult processdomain.ExitResult, workflowResults map[string]any) (domain.Session, bool, error) {
	current, err := s.repo.Find(ctx, session.ID)
	if err != nil {
		return domain.Session{}, false, fmt.Errorf("find session after process exit: %w", err)
	}
	if s.artifactScanner != nil {
		nodeRunID := ""
		if options.nodeRunID != nil {
			nodeRunID = string(*options.nodeRunID)
		}
		if _, scanErr := s.artifactScanner.Scan(ctx, domain.ArtifactScanRequest{
			SessionID:    session.ID,
			SourceType:   domain.AttachmentSourceCodex,
			SourceID:     string(handle.ProcessRunID),
			ProcessRunID: string(handle.ProcessRunID),
			NodeRunID:    nodeRunID,
		}); scanErr != nil {
			log.Printf("scan session artifacts after process exit: session=%s process=%s error=%v", session.ID, handle.ProcessRunID, scanErr)
		}
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
	if err != nil {
		return domain.Session{}, false, fmt.Errorf("find active process after exit: %w", err)
	}
	processEvent := sessionEventInput{eventType: "process.exited", payload: processExitPayload(handle.ProcessRunID, exitResult)}
	if batch, pending, err := s.pendingAgentBatchForProcess(ctx, handle.ProcessRunID); err != nil {
		return domain.Session{}, false, err
	} else if pending {
		inputs := []sessionEventInput{
			processEvent,
			{eventType: "process.suspended_for_user", payload: map[string]any{
				"processRunId": string(handle.ProcessRunID),
				"batchId":      string(batch.ID),
			}},
		}
		if err := s.markProcessExitedWithSessionEventsAndSettlement(ctx, handle.ProcessRunID, exitResult, current, false, inputs, promptAppendSettlementComplete); err != nil {
			return domain.Session{}, false, err
		}
		return current, false, nil
	}
	if batch, inflight, err := s.inflightAgentBatchForProcess(ctx, handle.ProcessRunID); err != nil {
		return domain.Session{}, false, err
	} else if inflight {
		inputs := []sessionEventInput{
			processEvent,
			{eventType: "process.answer_delivery_interrupted", payload: map[string]any{
				"processRunId": string(handle.ProcessRunID), "batchId": string(batch.ID),
			}},
		}
		var resumeEvents []eventdomain.DomainEvent
		var processEvents []eventdomain.DomainEvent
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			var err error
			processEvents, err = s.newSessionEvents(current, inputs)
			if err != nil {
				return err
			}
			if _, err := persistProcessExitInTx(ctx, tx, handle.ProcessRunID, exitResult, current, false, processEvents, promptAppendSettlementComplete); err != nil {
				return err
			}
			finder, ok := tx.Processes().(processdomain.RunFinder)
			if !ok {
				return errors.New("process run finder is required")
			}
			origin, err := finder.FindRun(ctx, handle.ProcessRunID)
			if err != nil {
				return err
			}
			resumeEvents, err = s.queueAnswerResumeInTx(ctx, tx, batch, origin)
			return err
		}); err != nil {
			if persistErr := s.persistAnswerDeliveryResumeFailure(ctx, current, handle.ProcessRunID, exitResult, batch, err, inputs); persistErr != nil {
				return domain.Session{}, false, errors.Join(err, persistErr)
			}
			return current, false, nil
		}
		for _, event := range processEvents {
			s.publishSessionEvent(ctx, event)
		}
		batch.DeliveryStatus = questiondomain.DeliveryAwaitingResume
		batch.DeliveryProcessRunID = nil
		s.publishQuestionBatch(questionBatchDTO(batch))
		for _, event := range resumeEvents {
			s.publishSessionEvent(ctx, event)
		}
		s.scheduleQueueDrain()
		return current, false, nil
	}
	if batch, origin, awaiting, err := s.awaitingAnswerDelivery(ctx, current.ID); err != nil {
		return domain.Session{}, false, err
	} else if awaiting && origin.ID == handle.ProcessRunID {
		inputs := []sessionEventInput{
			processEvent,
			{eventType: "process.suspended_for_user", payload: map[string]any{
				"processRunId": string(handle.ProcessRunID), "batchId": string(batch.ID),
			}},
		}
		var processEvents []eventdomain.DomainEvent
		var resumeEvents []eventdomain.DomainEvent
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			var err error
			processEvents, err = s.newSessionEvents(current, inputs)
			if err != nil {
				return err
			}
			if _, err := persistProcessExitInTx(ctx, tx, handle.ProcessRunID, exitResult, current, false, processEvents, promptAppendSettlementComplete); err != nil {
				return err
			}
			if current.Status == domain.StatusQueued && current.Queue.Kind == domain.QueueKindAnswerUser && current.Queue.AnswerBatchID == string(batch.ID) {
				return nil
			}
			resumeEvents, err = s.queueAnswerResumeInTx(ctx, tx, batch, origin)
			return err
		}); err != nil {
			return domain.Session{}, false, err
		}
		for _, event := range processEvents {
			s.publishSessionEvent(ctx, event)
		}
		for _, event := range resumeEvents {
			s.publishSessionEvent(ctx, event)
		}
		if len(resumeEvents) > 0 {
			s.publishQuestionBatch(questionBatchDTO(batch))
			s.scheduleQueueDrain()
		}
		return current, false, nil
	}
	if ok && active.ID != handle.ProcessRunID {
		return domain.Session{}, false, s.markProcessExitedWithSessionEvents(ctx, handle.ProcessRunID, exitResult, current, false, []sessionEventInput{processEvent})
	}
	if options.answerBatchID != "" && current.Status != domain.StatusStopping && current.Status != domain.StatusStopped && current.Status != domain.StatusClosed {
		batch, found, err := s.answerBatch(ctx, options.answerBatchID)
		if err != nil {
			return domain.Session{}, false, err
		}
		if found && batch.DeliveryStatus != questiondomain.DeliveryDelivered {
			if err := transitionSession(&current, domain.StatusResumeFailed, exitResult.FinishedAt); err != nil {
				return domain.Session{}, false, err
			}
			inputs := []sessionEventInput{
				processEvent,
				{eventType: "session.resume_failed", payload: map[string]any{
					"processRunId": string(handle.ProcessRunID), "batchId": string(batch.ID), "reason": "answer_delivery_unconfirmed",
				}},
			}
			if err := s.markProcessExitedWithSessionEventsAndSettlement(ctx, handle.ProcessRunID, exitResult, current, true, inputs, promptAppendSettlementRelease); err != nil {
				return domain.Session{}, false, err
			}
			if err := s.markWorkflowResumeFailed(ctx, current, options, "answer_delivery_unconfirmed", "Codex exited before confirming answer delivery"); err != nil {
				return domain.Session{}, false, err
			}
			return current, false, nil
		}
	}
	if current.Mode == domain.ModeWorkflow && options.workflowRunID != "" && options.nodeRunID != nil &&
		options.resumeCodexSessionID != "" && !options.resumeAcknowledged &&
		current.Status != domain.StatusStopping && current.Status != domain.StatusStopped && current.Status != domain.StatusClosed {
		message := strings.TrimSpace(exitResult.FailureReason)
		if message == "" {
			message = "Codex resume exited before acknowledging the workflow node prompt"
		}
		resumeFailureResult := exitResult
		resumeFailureResult.FailureReason = message
		inputs := []sessionEventInput{
			{eventType: "process.exited", payload: processExitPayload(handle.ProcessRunID, resumeFailureResult)},
			{eventType: "process.resume_failed", payload: map[string]any{
				"processRunId": string(handle.ProcessRunID), "reason": message,
			}},
			{eventType: "session.resume_failed", payload: map[string]any{
				"processRunId": string(handle.ProcessRunID), "reason": message,
			}},
		}
		if err := s.persistWorkflowResumeFailure(ctx, current, handle.ProcessRunID, resumeFailureResult, message, inputs); err != nil {
			return domain.Session{}, false, err
		}
		return current, false, nil
	}
	if current.Mode == domain.ModeWorkflow && options.workflowRunID != "" && options.nodeRunID != nil {
		workflowExitInput := workflowProcessExitInput(handle, options, exitResult, workflowResults)
		workflowInputs := []sessionEventInput{
			processEvent,
			{eventType: "workflow.exit_pending", payload: workflowProcessExitPayload(workflowExitInput)},
		}
		saveSession := false
		if current.Status == domain.StatusStopping {
			if err := transitionSession(&current, domain.StatusStopped, exitResult.FinishedAt); err != nil {
				return domain.Session{}, false, err
			}
			saveSession = true
			workflowInputs = append(workflowInputs, sessionEventInput{
				eventType: "session.stopped",
				payload: map[string]any{
					"processRunId": string(handle.ProcessRunID),
					"reason":       "process_exited",
				},
			})
		}
		if err := s.markProcessExitedWithSessionEventsAndSettlement(ctx, handle.ProcessRunID, exitResult, current, saveSession, workflowInputs, promptAppendSettlementForExitedSession(current.Status)); err != nil {
			return domain.Session{}, false, err
		}
		switch current.Status {
		case domain.StatusStopping, domain.StatusStopped, domain.StatusClosed:
			return domain.Session{}, false, nil
		default:
			return current, true, nil
		}
	}
	inputs := []sessionEventInput{processEvent}
	switch current.Status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping:
		settlement := promptAppendSettlementAutomatic
		if current.Status == domain.StatusStopping {
			settlement = promptAppendSettlementRelease
		}
		nextStatus := domain.StatusStopped
		if processExitFailed(exitResult) && current.Status != domain.StatusStopping {
			if options.resumeCodexSessionID != "" {
				nextStatus = domain.StatusResumeFailed
			} else {
				nextStatus = domain.StatusFailed
			}
		}
		if err := transitionSession(&current, nextStatus, exitResult.FinishedAt); err != nil {
			return domain.Session{}, false, err
		}
		eventType := "session.stopped"
		if current.Status == domain.StatusResumeFailed {
			eventType = "session.resume_failed"
		} else if current.Status == domain.StatusFailed {
			eventType = "session.failed"
		}
		payload := processExitPayload(handle.ProcessRunID, exitResult)
		payload["reason"] = "process_exited"
		inputs = append(inputs, sessionEventInput{eventType: eventType, payload: payload})
		if err := s.markProcessExitedWithSessionEventsAndSettlement(ctx, handle.ProcessRunID, exitResult, current, true, inputs, settlement); err != nil {
			return domain.Session{}, false, err
		}
		return domain.Session{}, false, nil
	default:
		return domain.Session{}, false, s.markProcessExitedWithSessionEventsAndSettlement(ctx, handle.ProcessRunID, exitResult, current, false, inputs, promptAppendSettlementForExitedSession(current.Status))
	}
}

func (s *Service) persistAnswerDeliveryResumeFailure(ctx context.Context, session domain.Session, runID processdomain.RunID, result processdomain.ExitResult, batch questiondomain.Batch, cause error, inputs []sessionEventInput) error {
	if err := transitionSession(&session, domain.StatusResumeFailed, result.FinishedAt); err != nil {
		return err
	}
	inputs = append(inputs, sessionEventInput{eventType: "session.resume_failed", payload: map[string]any{
		"batchId": string(batch.ID), "processRunId": string(runID), "reason": "answer_delivery_resume_failed", "error": cause.Error(),
	}})
	if session.Mode == domain.ModeWorkflow {
		return s.persistWorkflowResumeFailure(ctx, session, runID, result, cause.Error(), inputs)
	}
	return s.markProcessExitedWithSessionEventsAndSettlement(ctx, runID, result, session, true, inputs, promptAppendSettlementComplete)
}

func promptAppendSettlementForExitedSession(status domain.Status) promptAppendSettlement {
	switch status {
	case domain.StatusStopping, domain.StatusStopped, domain.StatusClosed:
		return promptAppendSettlementRelease
	default:
		return promptAppendSettlementAutomatic
	}
}

func exitResultFromEvent(event processdomain.CodexEvent) (processdomain.ExitResult, bool) {
	if event.Type != "process.exit" {
		return processdomain.ExitResult{}, false
	}
	result := processdomain.ExitResult{}
	if value, ok := event.Payload["exitCode"].(int); ok {
		result.ExitCode = &value
	} else if value, ok := event.Payload["exitCode"].(float64); ok {
		code := int(value)
		result.ExitCode = &code
	}
	if reason, ok := event.Payload["failureReason"].(string); ok {
		result.FailureReason = strings.TrimSpace(reason)
	}
	if code, ok := event.Payload["failureCode"].(string); ok {
		result.FailureCode = strings.TrimSpace(code)
	}
	return result, true
}

func workflowResultsFromEvent(event processdomain.CodexEvent) (map[string]any, bool) {
	text, ok := completedAssistantOutput(event)
	if !ok {
		return nil, false
	}
	return workflowResultsFromText(text)
}

func workflowResultsAfterEvent(current map[string]any, event processdomain.CodexEvent) map[string]any {
	if !isAssistantOutputEvent(event) {
		return current
	}
	results, _ := workflowResultsFromEvent(event)
	return results
}

func isAssistantOutputEvent(event processdomain.CodexEvent) bool {
	_, ok := completedAssistantOutput(event)
	return ok
}

func completedAssistantOutput(event processdomain.CodexEvent) (string, bool) {
	if event.Type != "item.completed" {
		return "", false
	}
	normalized, ok := event.Payload["normalizedItem"].(map[string]any)
	if !ok || normalized["type"] != "agent_message" || normalized["status"] != "completed" {
		return "", false
	}
	output, _ := normalized["output"].(string)
	output = strings.TrimSpace(output)
	return output, true
}

func workflowResultsFromText(text string) (map[string]any, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(text), &envelope); err != nil || len(envelope) != 1 {
		return nil, false
	}
	results, ok := envelope["results"].(map[string]any)
	return results, ok
}

func processExitPayload(processRunID processdomain.RunID, result processdomain.ExitResult) map[string]any {
	payload := map[string]any{"processRunId": string(processRunID)}
	if result.ExitCode != nil {
		payload["exitCode"] = *result.ExitCode
	}
	if result.FailureReason != "" {
		payload["failureReason"] = result.FailureReason
		payload["failureCode"] = codexProcessFailureCode(result)
	} else if result.FailureCode != "" {
		payload["failureCode"] = result.FailureCode
	}
	return payload
}

func processExitFailed(result processdomain.ExitResult) bool {
	if result.FailureReason != "" {
		return true
	}
	return result.ExitCode != nil && *result.ExitCode != 0
}

func codexProcessFailureCode(result processdomain.ExitResult) string {
	if result.FailureCode != "" {
		return result.FailureCode
	}
	reason := strings.ToLower(result.FailureReason)
	if strings.Contains(reason, "model_reasoning_effort") ||
		strings.Contains(reason, "service_tier") ||
		strings.Contains(reason, "service tier") ||
		strings.Contains(reason, "reasoning effort") ||
		strings.Contains(reason, "unsupported model") ||
		strings.Contains(reason, "model ") && strings.Contains(reason, "not supported") ||
		strings.Contains(reason, "--model") && strings.Contains(reason, "invalid") ||
		strings.Contains(reason, "--sandbox") && strings.Contains(reason, "invalid") ||
		strings.Contains(reason, "unknown sandbox") {
		return apperror.CodeCodexParamRejected
	}
	return "codex_process_failed"
}

func (s *Service) workflowAdvanceAfterProcessExit(ctx context.Context, handle processdomain.CodexHandle, options codexStartOptions, result processdomain.ExitResult, workflowResults map[string]any) (domain.WorkflowAdvance, error) {
	if s.workflows == nil || options.nodeRunID == nil {
		return domain.WorkflowAdvance{}, errors.New("workflow process exit is missing workflow state")
	}
	input := workflowProcessExitInput(handle, options, result, workflowResults)
	advance, err := s.workflows.RecoverProcessExit(ctx, input)
	if err != nil {
		if appErr, ok := apperror.From(err); ok && appErr.Code == apperror.CodeWorkflowResultInvalid {
			nodeRunID := input.NodeRunID
			if markErr := s.workflows.MarkStartFailed(ctx, domain.WorkflowStartFailureInput{
				WorkflowRunID: options.workflowRunID,
				NodeRunID:     &nodeRunID,
				Code:          appErr.Code,
				Message:       appErr.Error(),
			}); markErr != nil {
				return domain.WorkflowAdvance{}, errors.Join(errWorkflowResultFailureNotPersisted, err, fmt.Errorf("mark invalid workflow result failed: %w", markErr))
			}
		}
		return domain.WorkflowAdvance{}, err
	}
	return advance, nil
}

func workflowProcessExitInput(handle processdomain.CodexHandle, options codexStartOptions, result processdomain.ExitResult, workflowResults map[string]any) domain.WorkflowProcessExitInput {
	output := map[string]any{
		"processRunId": string(handle.ProcessRunID),
	}
	input := domain.WorkflowProcessExitInput{
		WorkflowRunID: options.workflowRunID,
		NodeRunID:     domain.NodeRunID(*options.nodeRunID),
		Output:        output,
	}
	if processExitFailed(result) {
		message := strings.TrimSpace(result.FailureReason)
		if message == "" {
			message = "codex process exited unsuccessfully"
		}
		input.Failed = true
		input.FailureCode = codexProcessFailureCode(result)
		input.FailureMessage = message
		output["exit"] = "failed"
		return input
	}
	output["exit"] = "completed"
	if options.workflowResultRetry {
		output["resultRetry"] = true
	}
	if workflowResults != nil {
		output["results"] = workflowResults
	}
	return input
}

func workflowProcessExitPayload(input domain.WorkflowProcessExitInput) map[string]any {
	return map[string]any{
		"workflowRunId":  string(input.WorkflowRunID),
		"nodeRunId":      string(input.NodeRunID),
		"failed":         input.Failed,
		"failureCode":    input.FailureCode,
		"failureMessage": input.FailureMessage,
		"output":         copyPayload(input.Output),
	}
}

func workflowProcessExitInputFromPayload(payload map[string]any) (domain.WorkflowProcessExitInput, error) {
	workflowRunID, _ := payload["workflowRunId"].(string)
	nodeRunID, _ := payload["nodeRunId"].(string)
	if strings.TrimSpace(workflowRunID) == "" || strings.TrimSpace(nodeRunID) == "" {
		return domain.WorkflowProcessExitInput{}, errors.New("workflow process exit checkpoint is missing workflow run or node run id")
	}
	failed, _ := payload["failed"].(bool)
	failureCode, _ := payload["failureCode"].(string)
	failureMessage, _ := payload["failureMessage"].(string)
	output, _ := payload["output"].(map[string]any)
	return domain.WorkflowProcessExitInput{
		WorkflowRunID:  domain.WorkflowRunID(workflowRunID),
		NodeRunID:      domain.NodeRunID(nodeRunID),
		Failed:         failed,
		FailureCode:    failureCode,
		FailureMessage: failureMessage,
		Output:         copyPayload(output),
	}, nil
}

func (s *Service) applyWorkflowAdvance(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance, advanceOptions workflowAdvanceOptions) (DTO, error) {
	initialStart := advanceOptions.initialStart || session.Queue.InitialStart
	if advanceOptions.commandID != "" {
		markSystemCommandApplied(&session, advanceOptions.commandID)
	}
	if session.Queue.Kind == "" {
		session.Queue = domain.QueueIntent{}
	}
	if advanceOptions.commandID != "" && advance.CommandID == "" && workflowAdvanceHasExternalEffects(advance) {
		if err := s.persistChainedSystemAdvance(ctx, session, advance); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	}
	switch {
	case advance.Blocked:
		if err := transitionSession(&session, domain.StatusBlocked, s.now()); err != nil {
			return DTO{}, err
		}
		if err := s.saveSessionWithEvent(ctx, session, "session.blocked", map[string]any{
			"workflowRunId":  string(advance.WorkflowRunID),
			"reason":         advance.BlockedReason,
			"failureCode":    advance.BlockedCode,
			"failureMessage": advance.BlockedMessage,
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	case advance.Close:
		return s.closeWorkflowSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: domain.CloseReasonWorkflowClosed, appliedSystemCommandID: advanceOptions.commandID})
	case advance.Completed:
		if err := transitionSession(&session, domain.StatusCompleted, s.now()); err != nil {
			return DTO{}, err
		}
		if err := s.saveSessionWithEvent(ctx, session, "session.completed", map[string]any{
			"workflowRunId": string(advance.WorkflowRunID),
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	case advance.Merge != nil:
		return s.executeWorkflowMerge(ctx, session, advance)
	case advance.Expr != nil:
		return s.executeWorkflowExpr(ctx, session, advance, workflowAdvanceOptions{
			forceNewCodexSession: advanceOptions.forceNewCodexSession,
			initialStart:         initialStart,
			commandID:            advanceOptions.commandID,
		})
	case !advance.RequiresCodex:
		if err := transitionSessionToWaitingApproval(&session, initialStart, s.now()); err != nil {
			return DTO{}, err
		}
		if err := s.saveSessionWithEvent(ctx, session, "session.waiting_approval", map[string]any{
			"workflowRunId":    string(advance.WorkflowRunID),
			"nodeRunId":        stringValuePtr(advance.NodeRunID),
			"currentNodeId":    advance.CurrentNodeID,
			"currentNodeTitle": advance.CurrentNodeTitle,
			"approvalPhase":    advance.ApprovalPhase,
			"result":           advance.Result,
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	default:
		options := workflowCodexStartOptions(session, advance, advanceOptions)
		dto, err := s.enqueueCodex(ctx, session, options, queuePriorityForSession(session))
		if err != nil {
			return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, advance.NodeRunID, "codex_start_failed", err.Error())
		}
		return dto, nil
	}
}

func (s *Service) executeWorkflowExpr(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance, advanceOptions workflowAdvanceOptions) (DTO, error) {
	if s.workflows == nil {
		return DTO{}, errors.New("session workflow starter is required for workflow mode")
	}
	if advance.Expr == nil {
		return DTO{}, errors.New("workflow expr node is missing expression")
	}
	if advance.NodeRunID == nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, nil, "workflow_expr_failed", "expr node run id is missing")
	}
	results, err := runWorkflowExpr(advance.Expr.Script, advance.Expr.Params)
	if err != nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, advance.NodeRunID, "workflow_expr_failed", err.Error())
	}
	next, err := s.workflows.CompleteNode(ctx, domain.WorkflowNodeCompleteInput{
		WorkflowRunID: advance.WorkflowRunID,
		NodeRunID:     *advance.NodeRunID,
		CommandID:     advance.CommandID,
		Output: workflowResultOutput(workflowdomain.Result{
			Version: workflowdomain.ResultVersion,
			Outcome: workflowdomain.ResultSuccess,
			Summary: "Expression completed successfully",
			Data:    results,
		}),
	})
	if err != nil {
		return DTO{}, err
	}
	return s.applyWorkflowAdvance(ctx, session, next, advanceOptions)
}

func runWorkflowExpr(script string, params map[string]any) (map[string]any, error) {
	script = strings.TrimSpace(script)
	if script == "" {
		return nil, errors.New("expr script is required")
	}
	env := map[string]any{"params": mapOrEmpty(params)}
	program, err := expr.Compile(script, expr.Env(env))
	if err != nil {
		return nil, fmt.Errorf("compile expr node: %w", err)
	}
	output, err := expr.Run(program, env)
	if err != nil {
		return nil, fmt.Errorf("run expr node: %w", err)
	}
	results, ok := output.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expr node must return object, got %T", output)
	}
	return results, nil
}

func mapOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}

func firstPayload(payloads []map[string]any) map[string]any {
	if len(payloads) == 0 {
		return nil
	}
	return payloads[0]
}

func isWorkflowResultRetryPrompt(prompt string) bool {
	return strings.Contains(prompt, "ANYCODE_WORKFLOW_RESULT_RETRY")
}

func (s *Service) executeWorkflowMerge(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance) (DTO, error) {
	if s.merge == nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, advance.NodeRunID, apperror.CodeMergeFailed, "merge port is not configured")
	}
	if advance.NodeRunID == nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, nil, apperror.CodeMergeFailed, "merge node run id is missing")
	}
	strategy := "merge"
	if advance.Merge != nil && strings.TrimSpace(advance.Merge.Strategy) != "" {
		strategy = strings.TrimSpace(strings.ToLower(advance.Merge.Strategy))
	}
	var result gitdiffdomain.MergeResult
	var err error
	resultPersisted := false
	if advance.CommandID != "" {
		if commands, ok := s.repo.(domain.MergeCommandRepository); ok {
			record, found, findErr := commands.FindMergeRecord(ctx, advance.CommandID)
			if findErr != nil {
				return DTO{}, findErr
			}
			if found {
				result = mergeResultFromRecord(record)
				resultPersisted = true
			}
		}
	}
	if !resultPersisted {
		switch strategy {
		case "rebase":
			result, err = s.merge.RebaseOntoBase(ctx, gitdiffdomain.RebaseInput{
				WorktreePath: session.WorktreePath,
				BaseBranch:   session.BaseBranch,
			})
		default:
			strategy = "merge"
			result, err = s.merge.MergeToBase(ctx, gitdiffdomain.MergeInput{
				WorktreePath: session.WorktreePath,
				BaseBranch:   session.BaseBranch,
			})
		}
	}
	if err != nil {
		result = gitdiffdomain.MergeResult{
			Strategy:      strategy,
			BaseBranch:    session.BaseBranch,
			Status:        "failed",
			FailureCode:   apperror.CodeMergeFailed,
			FailureReason: err.Error(),
		}
	}
	if result.Strategy == "" {
		result.Strategy = strategy
	}
	if result.BaseBranch == "" {
		result.BaseBranch = session.BaseBranch
	}
	if !resultPersisted {
		if recordErr := s.recordMergeResult(ctx, session, *advance.NodeRunID, advance.CommandID, result); recordErr != nil {
			return DTO{}, recordErr
		}
	}
	s.appendSessionEvent(ctx, session, "workflow.merge", map[string]any{
		"workflowRunId": string(advance.WorkflowRunID),
		"nodeRunId":     stringValuePtr(advance.NodeRunID),
		"strategy":      result.Strategy,
		"status":        result.Status,
		"failureCode":   result.FailureCode,
	})
	if result.Status != "merged" {
		code := result.FailureCode
		if code == "" {
			code = apperror.CodeMergeFailed
		}
		if s.questions != nil {
			return s.askMergeFailure(ctx, session, advance, result, code)
		}
		return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, advance.NodeRunID, code, result.FailureReason, mergeOutput(result))
	}
	return s.completeWorkflowMergeNode(ctx, session, advance, result)
}

func (s *Service) completeWorkflowMergeNode(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance, result gitdiffdomain.MergeResult) (DTO, error) {
	if advance.NodeRunID == nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, nil, apperror.CodeMergeFailed, "merge node run id is missing")
	}
	next, err := s.workflows.CompleteNode(ctx, domain.WorkflowNodeCompleteInput{
		WorkflowRunID: advance.WorkflowRunID,
		NodeRunID:     *advance.NodeRunID,
		CommandID:     advance.CommandID,
		Output:        mergeOutput(result),
	})
	if err != nil {
		return DTO{}, err
	}
	if next.Completed {
		return s.closeWorkflowSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: domain.CloseReasonMergedClosed, appliedSystemCommandID: advance.CommandID})
	}
	return s.applyWorkflowAdvance(ctx, session, next, workflowAdvanceOptions{commandID: advance.CommandID})
}

func markSystemCommandApplied(session *domain.Session, commandID string) {
	commandID = strings.TrimSpace(commandID)
	if session == nil || commandID == "" {
		return
	}
	if session.AppliedSystemCommands == nil {
		session.AppliedSystemCommands = map[string]bool{}
	}
	session.AppliedSystemCommands[commandID] = true
}

func systemCommandApplied(session domain.Session, commandID string) bool {
	return session.AppliedSystemCommands[strings.TrimSpace(commandID)]
}

func (s *Service) recordMergeResult(ctx context.Context, session domain.Session, nodeRunID domain.NodeRunID, commandID string, result gitdiffdomain.MergeResult) error {
	id := domain.ID(commandID)
	if id == "" {
		var err error
		id, err = s.generateID()
		if err != nil {
			return fmt.Errorf("generate merge record id: %w", err)
		}
	}
	now := s.now()
	mergedAt := (*time.Time)(nil)
	if result.Status == "merged" {
		mergedAt = &now
	}
	return s.repo.AddMergeRecord(ctx, domain.MergeRecord{
		ID:             string(id),
		SessionID:      session.ID,
		NodeRunID:      &nodeRunID,
		Strategy:       result.Strategy,
		BaseBranch:     result.BaseBranch,
		WorktreeBranch: result.WorktreeBranch,
		BaseCommit:     result.BaseCommit,
		HeadCommit:     result.HeadCommit,
		MergeCommit:    result.MergeCommit,
		Status:         result.Status,
		FailureCode:    result.FailureCode,
		FailureReason:  result.FailureReason,
		MergedAt:       mergedAt,
		CreatedAt:      now,
	})
}

func mergeResultFromRecord(record domain.MergeRecord) gitdiffdomain.MergeResult {
	return gitdiffdomain.MergeResult{
		Strategy: record.Strategy, BaseBranch: record.BaseBranch, WorktreeBranch: record.WorktreeBranch,
		BaseCommit: record.BaseCommit, HeadCommit: record.HeadCommit, MergeCommit: record.MergeCommit,
		Status: record.Status, FailureCode: record.FailureCode, FailureReason: record.FailureReason,
	}
}

func (s *Service) askMergeFailure(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance, result gitdiffdomain.MergeResult, code string) (DTO, error) {
	if advance.NodeRunID == nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, nil, code, result.FailureReason)
	}
	workflowRunID := questiondomain.WorkflowRunID(advance.WorkflowRunID)
	metadata := mergeFailureQuestionMetadata(advance, result, code)
	batchID := questiondomain.BatchID("")
	if advance.CommandID != "" {
		batchID = questiondomain.BatchID("merge-failure-" + advance.CommandID)
	}
	batch, err := s.questions.CreateBatch(ctx, questionapp.CreateBatchInput{
		BatchID:       batchID,
		SessionID:     questiondomain.SessionID(session.ID),
		WorkflowRunID: &workflowRunID,
		Questions: []questiondomain.Question{
			{
				Title:    "合并失败处理",
				Body:     mergeFailureQuestionBody(result),
				Type:     "merge_failure_action",
				Metadata: metadata,
				Status:   string(questiondomain.BatchPending),
				Options: []questiondomain.Option{
					{
						ID:          "retry_merge",
						Label:       "我已处理，重试合并",
						Description: "适用于已手动解决冲突或清理 worktree 后重新执行当前合并节点。",
						Payload:     mergeFailureActionPayload(metadata, "retry_merge"),
					},
					{
						ID:          "fail_node",
						Label:       "标记节点失败",
						Description: "执行节点失败处理；有剩余重试时重跑当前节点，耗尽后阻塞流程。",
						Payload:     mergeFailureActionPayload(metadata, "fail_node"),
					},
					{
						ID:          "stop_session",
						Label:       "停止卡片",
						Description: "停止当前卡片，保留事件、附件和 worktree。",
						Payload:     mergeFailureActionPayload(metadata, "stop_session"),
					},
				},
			},
		},
	})
	if err != nil {
		return DTO{}, fmt.Errorf("create merge failure question: %w", err)
	}
	if err := transitionSession(&session, domain.StatusWaitingUser, s.now()); err != nil {
		return DTO{}, err
	}
	if err := s.repo.Save(ctx, session); err != nil {
		if advance.CommandID == "" && batch.Created {
			if cancelErr := s.questions.CancelPendingBySession(ctx, questiondomain.SessionID(session.ID), "merge failure question abandoned"); cancelErr != nil {
				return DTO{}, fmt.Errorf("save session: %w; cancel merge failure question: %v", err, cancelErr)
			}
		}
		return DTO{}, fmt.Errorf("save session: %w", err)
	}
	s.appendSessionEvent(ctx, session, "workflow.merge_waiting_user", map[string]any{
		"workflowRunId": string(advance.WorkflowRunID),
		"nodeRunId":     stringValuePtr(advance.NodeRunID),
		"batchId":       string(batch.ID),
		"failureCode":   code,
		"failureReason": result.FailureReason,
	})
	return toDTO(session), nil
}

func (s *Service) HandleQuestionBatchAnswered(ctx context.Context, batch questionapp.BatchDTO) error {
	if s == nil {
		return errors.New("session usecase: nil service")
	}
	if batch.Status != questiondomain.BatchAnswered {
		return nil
	}
	action, metadata, ok, err := mergeFailureDecision(batch)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return s.withSessionLock(ctx, domain.ID(batch.SessionID), func(ctx context.Context) error {
		session, err := s.repo.Find(ctx, domain.ID(batch.SessionID))
		if err != nil {
			return fmt.Errorf("find session: %w", err)
		}
		return s.applyMergeFailureDecision(ctx, session, batch.ID, action, metadata)
	})
}

func (s *Service) applyMergeFailureDecision(ctx context.Context, session domain.Session, batchID questiondomain.BatchID, action string, metadata map[string]any) error {
	workflowRunID := domain.WorkflowRunID(stringFromMap(metadata, "workflowRunId"))
	nodeRunIDValue := domain.NodeRunID(stringFromMap(metadata, "nodeRunId"))
	if workflowRunID == "" || nodeRunIDValue == "" {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "merge failure question metadata is incomplete").
			WithDetails(map[string]any{"sessionId": string(session.ID), "batchId": string(batchID)})
	}
	nodeRunID := nodeRunIDValue
	code := stringFromMap(metadata, "failureCode")
	if code == "" {
		code = apperror.CodeMergeFailed
	}
	reason := stringFromMap(metadata, "failureReason")
	switch action {
	case "retry_merge":
		strategy := stringFromMap(metadata, "strategy")
		if strategy == "" {
			strategy = "merge"
		}
		_, err := s.executeWorkflowMerge(ctx, session, domain.WorkflowAdvance{
			WorkflowRunID: workflowRunID,
			NodeRunID:     &nodeRunID,
			Status:        "running",
			Merge:         &domain.WorkflowMerge{Strategy: strategy},
		})
		return err
	case "stop_session":
		_, err := s.stopSession(ctx, session.ID)
		return err
	case "fail_node":
		_, err := s.handleWorkflowNodeFailure(ctx, session, workflowRunID, &nodeRunID, code, reason, mergeFailureOutputFromMetadata(metadata))
		return err
	default:
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "unsupported merge failure action").
			WithDetails(map[string]any{"action": action})
	}
}

func mergeFailureOutputFromMetadata(metadata map[string]any) map[string]any {
	result := gitdiffdomain.MergeResult{
		Strategy:       stringFromMap(metadata, "strategy"),
		BaseBranch:     stringFromMap(metadata, "baseBranch"),
		WorktreeBranch: stringFromMap(metadata, "worktreeBranch"),
		BaseCommit:     stringFromMap(metadata, "baseCommit"),
		HeadCommit:     stringFromMap(metadata, "headCommit"),
		MergeCommit:    stringFromMap(metadata, "mergeCommit"),
		Status:         stringFromMap(metadata, "status"),
		FailureCode:    stringFromMap(metadata, "failureCode"),
		FailureReason:  stringFromMap(metadata, "failureReason"),
	}
	if result.Status == "" {
		result.Status = "failed"
	}
	return mergeOutput(result)
}

func mergeFailureQuestionMetadata(advance domain.WorkflowAdvance, result gitdiffdomain.MergeResult, code string) map[string]any {
	metadata := map[string]any{
		"kind":           "merge_failure_action",
		"workflowRunId":  string(advance.WorkflowRunID),
		"strategy":       result.Strategy,
		"baseBranch":     result.BaseBranch,
		"worktreeBranch": result.WorktreeBranch,
		"baseCommit":     result.BaseCommit,
		"headCommit":     result.HeadCommit,
		"mergeCommit":    result.MergeCommit,
		"status":         result.Status,
		"failureCode":    code,
		"failureReason":  result.FailureReason,
	}
	if advance.NodeRunID != nil {
		metadata["nodeRunId"] = string(*advance.NodeRunID)
	}
	return metadata
}

func mergeFailureActionPayload(metadata map[string]any, action string) map[string]any {
	payload := mapStringAnyClone(metadata)
	payload["action"] = action
	return payload
}

func mergeFailureQuestionBody(result gitdiffdomain.MergeResult) string {
	reason := strings.TrimSpace(result.FailureReason)
	if reason == "" {
		reason = "Codex 合并节点执行失败，需要选择后续处理方式。"
	}
	return fmt.Sprintf("策略：%s\n状态：%s\n原因：%s", result.Strategy, result.Status, reason)
}

func mergeFailureDecision(batch questionapp.BatchDTO) (string, map[string]any, bool, error) {
	for _, question := range batch.Questions {
		if question.Type != "merge_failure_action" && stringFromMap(question.Metadata, "kind") != "merge_failure_action" {
			continue
		}
		metadata := mapStringAnyClone(question.Metadata)
		action := mergeFailureAction(question)
		if action == "" && strings.TrimSpace(question.CustomAnswer) != "" {
			action = "retry_merge"
		}
		if action == "" {
			return "", nil, true, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "merge failure question answer is missing action")
		}
		switch action {
		case "retry_merge", "fail_node", "stop_session":
		default:
			return "", nil, true, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "unsupported merge failure action").
				WithDetails(map[string]any{"action": action})
		}
		return action, metadata, true, nil
	}
	return "", nil, false, nil
}

func isMergeFailureQuestionBatch(questions []questiondomain.Question) bool {
	for _, question := range questions {
		if question.Type == "merge_failure_action" || stringFromMap(question.Metadata, "kind") == "merge_failure_action" {
			return true
		}
	}
	return false
}

func mergeFailureAction(question questiondomain.Question) string {
	if question.SelectedOptionID != nil {
		for _, option := range question.Options {
			if option.ID == *question.SelectedOptionID {
				return stringFromMap(option.Payload, "action")
			}
		}
	}
	return ""
}

func mapStringAnyClone(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func stringFromMap(input map[string]any, key string) string {
	value, ok := input[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func mergeOutput(result gitdiffdomain.MergeResult) map[string]any {
	outcome := workflowdomain.ResultSuccess
	if result.Status != "merged" {
		outcome = workflowdomain.ResultFailure
	}
	summary := "Merge completed successfully"
	if outcome == workflowdomain.ResultFailure {
		summary = strings.TrimSpace(result.FailureReason)
		if summary == "" {
			summary = "Merge failed"
		}
	}
	return workflowResultOutput(workflowdomain.Result{
		Version: workflowdomain.ResultVersion,
		Outcome: outcome,
		Summary: summary,
		Data: map[string]any{
			"merge": map[string]any{
				"strategy":       result.Strategy,
				"baseBranch":     result.BaseBranch,
				"worktreeBranch": result.WorktreeBranch,
				"baseCommit":     result.BaseCommit,
				"headCommit":     result.HeadCommit,
				"mergeCommit":    result.MergeCommit,
				"status":         result.Status,
				"failureCode":    result.FailureCode,
				"failureReason":  result.FailureReason,
			},
		},
		Checks: []workflowdomain.ResultCheck{{
			ID: "merge_status", Label: "Merge status", Status: map[bool]string{true: "passed", false: "failed"}[result.Status == "merged"], Source: "system",
		}},
	})
}

func workflowResultOutput(result workflowdomain.Result) map[string]any {
	result.Normalize()
	data, err := json.Marshal(result)
	if err != nil {
		return map[string]any{}
	}
	var encoded map[string]any
	if json.Unmarshal(data, &encoded) != nil {
		return map[string]any{}
	}
	return map[string]any{"results": encoded}
}

func (s *Service) handleWorkflowNodeFailure(ctx context.Context, session domain.Session, workflowRunID domain.WorkflowRunID, nodeRunID *domain.NodeRunID, code string, message string, output ...map[string]any) (DTO, error) {
	if s.workflows == nil || nodeRunID == nil {
		if err := transitionSession(&session, domain.StatusFailed, s.now()); err != nil {
			return DTO{}, err
		}
		if err := s.repo.Save(ctx, session); err != nil {
			return DTO{}, fmt.Errorf("save session: %w", err)
		}
		return toDTO(session), nil
	}
	if session.Status == domain.StatusQueued || session.Status == domain.StatusStarting {
		wasStarting := session.Status == domain.StatusStarting
		if err := transitionSession(&session, domain.StatusFailed, s.now()); err != nil {
			return DTO{}, err
		}
		failureEvent := sessionEventInput{eventType: "session.failed", payload: map[string]any{
			"code":   code,
			"reason": message,
		}}
		failurePersisted := false
		if wasStarting && s.processes != nil {
			active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
			if err != nil {
				return DTO{}, fmt.Errorf("find active process after workflow start failure: %w", err)
			}
			if ok {
				if err := s.markProcessExitedWithSessionEvents(ctx, active.ID, processdomain.ExitResult{
					FailureReason: message,
					FinishedAt:    s.now(),
				}, session, true, []sessionEventInput{failureEvent}); err != nil {
					return DTO{}, err
				}
				failurePersisted = true
			}
		}
		if !failurePersisted {
			if err := s.saveSessionWithEvent(ctx, session, failureEvent.eventType, failureEvent.payload); err != nil {
				return DTO{}, err
			}
		}
	}
	advance, err := s.workflows.FailNode(ctx, domain.WorkflowNodeFailInput{
		WorkflowRunID: workflowRunID,
		NodeRunID:     *nodeRunID,
		Code:          code,
		Message:       message,
		Output:        firstPayload(output),
	})
	if err != nil {
		_ = s.workflows.MarkStartFailed(ctx, domain.WorkflowStartFailureInput{
			WorkflowRunID: workflowRunID,
			NodeRunID:     nodeRunID,
			Code:          code,
			Message:       message,
		})
		return DTO{}, err
	}
	return s.applyWorkflowAdvance(ctx, session, advance, workflowAdvanceOptions{})
}

type sessionEventInput struct {
	eventType string
	payload   map[string]any
}

func (s *Service) publishCodexEventWithSessionUpdates(ctx context.Context, session domain.Session, processRunID processdomain.RunID, event processdomain.CodexEvent, saveSession bool, saveFilesChanged bool, promptDelivered bool, extraInputs ...sessionEventInput) error {
	var codexEvent eventdomain.DomainEvent
	publishCodexEvent := s.publisher != nil && !event.RealtimeOnly
	if publishCodexEvent {
		var err error
		codexEvent, err = s.newCodexSessionEvent(session, processRunID, event)
		if err != nil {
			return err
		}
	}
	extraEvents, err := s.newSessionEvents(session, extraInputs)
	if err != nil {
		return err
	}
	var deliveredBatches []questiondomain.Batch
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if promptDelivered {
				if err := tx.Sessions().CompletePromptAppends(ctx, string(processRunID), promptDeliveryTime(event, s.now())); err != nil {
					return err
				}
				if repo, ok := tx.Questions().(questiondomain.AgentRepository); ok {
					batches, err := repo.MarkDeliveryDeliveredByProcessRun(ctx, questiondomain.ProcessRunID(processRunID), promptDeliveryTime(event, s.now()))
					if err != nil {
						return err
					}
					deliveredBatches = batches
				}
			}
			if saveSession {
				if err := tx.Sessions().Save(ctx, session); err != nil {
					return fmt.Errorf("save session: %w", err)
				}
			}
			if saveFilesChanged {
				if err := tx.Sessions().UpdateFilesChanged(ctx, session.ID, session.FilesChanged); err != nil {
					return err
				}
			}
			for _, event := range extraEvents {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if publishCodexEvent {
			s.publishSessionEvent(ctx, codexEvent)
		}
		for _, event := range extraEvents {
			s.publishSessionEvent(ctx, event)
		}
		if s.questions != nil {
			for _, batch := range deliveredBatches {
				s.publishQuestionBatch(questionBatchDTO(batch))
			}
		}
		return nil
	}
	if promptDelivered {
		if err := s.repo.CompletePromptAppends(ctx, string(processRunID), promptDeliveryTime(event, s.now())); err != nil {
			return err
		}
	}
	if saveSession {
		if err := s.repo.Save(ctx, session); err != nil {
			return fmt.Errorf("save session: %w", err)
		}
	}
	if saveFilesChanged {
		if err := s.repo.UpdateFilesChanged(ctx, session.ID, session.FilesChanged); err != nil {
			return err
		}
	}
	if publishCodexEvent {
		s.publishSessionEvent(ctx, codexEvent)
	}
	for _, event := range extraEvents {
		if s.events != nil {
			if err := s.events.Append(ctx, event); err != nil {
				return err
			}
		}
		s.publishSessionEvent(ctx, event)
	}
	return nil
}

func promptDeliveryTime(event processdomain.CodexEvent, fallback time.Time) time.Time {
	if event.CreatedAt.IsZero() {
		return fallback
	}
	return event.CreatedAt
}

func (s *Service) newCodexSessionEvent(session domain.Session, processRunID processdomain.RunID, event processdomain.CodexEvent) (eventdomain.DomainEvent, error) {
	var id domain.ID
	var err error
	codexSessionID := strings.TrimSpace(session.CodexSessionID)
	if codexSessionID == "" {
		codexSessionID = strings.TrimSpace(codexSessionIDFromEvent(event))
	}
	if canonicalID := processdomain.CanonicalCodexEventID(codexSessionID, event.EventID); canonicalID != "" {
		id = domain.ID(canonicalID)
	} else {
		id, err = s.generateID()
		if err != nil {
			return eventdomain.DomainEvent{}, err
		}
	}
	sessionID := eventdomain.SessionID(session.ID)
	payload := codexSessionEventPayload(codexSessionID, event)
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = s.now()
	}
	return eventdomain.DomainEvent{
		ID: eventdomain.ID(id),
		Scope: eventdomain.Scope{
			SessionID: &sessionID,
			ProjectID: string(session.ProjectID),
		},
		SessionID: &sessionID,
		Type:      "process.codex_event",
		Payload:   payload,
		Causality: eventdomain.Causality{
			ProcessRunID:  string(processRunID),
			CorrelationID: event.CorrelationID,
			SessionStatus: string(session.Status),
		},
		CreatedAt: createdAt,
	}, nil
}

func codexSessionEventPayload(codexSessionID string, event processdomain.CodexEvent) map[string]any {
	payload := processEventPayload(event)
	payload["codexType"] = event.Type
	payload["codexSessionId"] = codexSessionID
	payload["codexCorrelationId"] = event.CorrelationID
	payload["codexPhase"] = string(event.Phase)
	payload["codexContent"] = event.Content
	payload["codexSourceOffset"] = event.SourceOffset
	payload["codexSourceIndex"] = event.SourceIndex
	if event.EventID != "" {
		payload["codexEventId"] = event.EventID
	}
	return payload
}

func (s *Service) createProcessRunWithSessionEvent(ctx context.Context, expectedSession domain.Session, run processdomain.Run, session domain.Session, options codexStartOptions, maxActive int, eventType string, payload map[string]any) (port.ExecutionClaimResult, error) {
	if s.uow != nil {
		event, ok, err := s.newSessionEvent(session, eventType, payload)
		if err != nil {
			return port.ExecutionClaimResult{}, err
		}
		var result port.ExecutionClaimResult
		var publishedEvent *eventdomain.DomainEvent
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			claim, err := tx.ClaimExecution(ctx, port.ExecutionClaimInput{
				ExpectedSession: expectedSession,
				StartingSession: session,
				Run:             run,
				MaxActive:       maxActive,
			})
			if err != nil {
				return err
			}
			result = claim
			switch claim.Status {
			case port.ExecutionAlreadyActive:
				if claim.ActiveRun == nil {
					return errors.New("active execution claim has no process run")
				}
				reconciled, err := reconcileSessionWithActiveRun(claim.Session, *claim.ActiveRun, s.now())
				if err != nil {
					return err
				}
				if err := tx.Sessions().Save(ctx, reconciled); err != nil {
					return fmt.Errorf("reconcile active execution session: %w", err)
				}
				result.Session = reconciled
				reconcileEvent, hasEvent, err := s.newSessionEvent(reconciled, "session.execution_already_active", map[string]any{
					"processRunId": string(claim.ActiveRun.ID),
				})
				if err != nil {
					return err
				}
				if hasEvent {
					if err := tx.Events().Append(ctx, reconcileEvent); err != nil {
						return err
					}
					publishedEvent = &reconcileEvent
				}
				return nil
			case port.ExecutionStale, port.ExecutionAtCapacity:
				return nil
			case port.ExecutionClaimed:
			default:
				return fmt.Errorf("unsupported execution claim status %q", claim.Status)
			}
			if options.answerBatchID != "" {
				repo, ok := tx.Questions().(questiondomain.AgentRepository)
				if !ok {
					return errors.New("agent question repository is required")
				}
				if err := repo.MarkDeliveryInflight(ctx, options.answerBatchID, questiondomain.ProcessRunID(run.ID)); err != nil {
					return err
				}
			}
			if options.answerBatchID != "" && options.workflowRunID != "" && options.nodeRunID != nil {
				repo, ok := tx.Workflows().(workflowdomain.NodeExecutionRepository)
				if !ok {
					return errors.New("workflow node execution repository is required")
				}
				if err := repo.MarkNodeRunning(ctx, workflowdomain.RunID(options.workflowRunID), workflowdomain.NodeRunID(*options.nodeRunID), workflowdomain.ProcessRunID(run.ID)); err != nil {
					return err
				}
			}
			if err := tx.Sessions().MarkPromptAppendsInflight(ctx, options.promptAppendIDs, string(run.ID)); err != nil {
				return err
			}
			if ok {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
				publishedEvent = &event
			}
			return nil
		}); err != nil {
			return port.ExecutionClaimResult{}, err
		}
		if publishedEvent != nil {
			s.publishSessionEvent(ctx, *publishedEvent)
		}
		return result, nil
	}
	if options.answerBatchID != "" {
		return port.ExecutionClaimResult{}, errors.New("answer_user process creation requires a unit of work")
	}
	// GLUE: Legacy in-memory wiring has no transaction boundary; production entstore execution uses the atomic claim above.
	active, found, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(expectedSession.ID))
	if err != nil {
		return port.ExecutionClaimResult{}, err
	}
	if found {
		reconciled, err := reconcileSessionWithActiveRun(expectedSession, active, s.now())
		if err != nil {
			return port.ExecutionClaimResult{}, err
		}
		if err := s.saveSessionWithEvent(ctx, reconciled, "session.execution_already_active", map[string]any{"processRunId": string(active.ID)}); err != nil {
			return port.ExecutionClaimResult{}, err
		}
		return port.ExecutionClaimResult{Status: port.ExecutionAlreadyActive, Session: reconciled, ActiveRun: &active}, nil
	}
	if maxActive > 0 {
		count, err := s.processes.CountActive(ctx)
		if err != nil {
			return port.ExecutionClaimResult{}, err
		}
		if count >= maxActive {
			return port.ExecutionClaimResult{Status: port.ExecutionAtCapacity, Session: expectedSession}, nil
		}
	}
	if err := s.processes.CreateRun(ctx, run); err != nil {
		return port.ExecutionClaimResult{}, fmt.Errorf("create process run: %w", err)
	}
	if err := s.repo.MarkPromptAppendsInflight(ctx, options.promptAppendIDs, string(run.ID)); err != nil {
		_ = s.processes.MarkExited(ctx, run.ID, processdomain.ExitResult{FailureReason: err.Error(), FinishedAt: s.now()})
		return port.ExecutionClaimResult{}, err
	}
	if err := s.saveSessionWithEvent(ctx, session, eventType, payload); err != nil {
		result := processdomain.ExitResult{FailureReason: err.Error(), FinishedAt: s.now()}
		_ = s.processes.MarkExited(ctx, run.ID, result)
		_ = s.repo.ReleasePromptAppends(ctx, string(run.ID))
		return port.ExecutionClaimResult{}, err
	}
	return port.ExecutionClaimResult{Status: port.ExecutionClaimed, Session: session}, nil
}

func reconcileSessionWithActiveRun(session domain.Session, run processdomain.Run, now time.Time) (domain.Session, error) {
	status := domain.StatusRunning
	switch run.Status {
	case processdomain.StatusStarting:
		status = domain.StatusStarting
	case processdomain.StatusRunning:
		status = domain.StatusRunning
	case processdomain.StatusWaitingUser:
		status = domain.StatusWaitingUser
	case processdomain.StatusStopping:
		status = domain.StatusStopping
	default:
		return domain.Session{}, fmt.Errorf("process run %s is not active", run.ID)
	}
	if err := transitionSession(&session, status, now); err != nil {
		return domain.Session{}, err
	}
	if strings.TrimSpace(run.CodexSessionID) != "" {
		session.CodexSessionID = run.CodexSessionID
	}
	return session, nil
}

func (s *Service) markProcessRunningWithSessionEvent(ctx context.Context, runID processdomain.RunID, pid int, codexSessionID string, session domain.Session, eventType string, payload map[string]any) error {
	if s.uow != nil {
		event, ok, err := s.newSessionEvent(session, eventType, payload)
		if err != nil {
			return err
		}
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().MarkRunning(ctx, runID, pid, codexSessionID); err != nil {
				return fmt.Errorf("mark process running: %w", err)
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			if ok {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if ok {
			s.publishSessionEvent(ctx, event)
		}
		return nil
	}
	if err := s.processes.MarkRunning(ctx, runID, pid, codexSessionID); err != nil {
		return fmt.Errorf("mark process running: %w", err)
	}
	return s.saveSessionWithEvent(ctx, session, eventType, payload)
}

func (s *Service) markProcessWaitingWithSessionEvent(ctx context.Context, runID processdomain.RunID, session domain.Session, eventType string, payload map[string]any) error {
	if s.uow != nil {
		event, ok, err := s.newSessionEvent(session, eventType, payload)
		if err != nil {
			return err
		}
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().MarkWaitingUser(ctx, runID); err != nil {
				return fmt.Errorf("mark process waiting user: %w", err)
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			if ok {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if ok {
			s.publishSessionEvent(ctx, event)
		}
		return nil
	}
	if err := s.processes.MarkWaitingUser(ctx, runID); err != nil {
		return fmt.Errorf("mark process waiting user: %w", err)
	}
	return s.saveSessionWithEvent(ctx, session, eventType, payload)
}

func (s *Service) markProcessStoppingWithSessionEvent(ctx context.Context, runID processdomain.RunID, session domain.Session, eventType string, payload map[string]any) error {
	if s.uow != nil {
		event, ok, err := s.newSessionEvent(session, eventType, payload)
		if err != nil {
			return err
		}
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().MarkStopping(ctx, runID); err != nil {
				return fmt.Errorf("mark process stopping: %w", err)
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			if ok {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if ok {
			s.publishSessionEvent(ctx, event)
		}
		return nil
	}
	if err := s.processes.MarkStopping(ctx, runID); err != nil {
		return fmt.Errorf("mark process stopping: %w", err)
	}
	return s.saveSessionWithEvent(ctx, session, eventType, payload)
}

func (s *Service) saveProcessRunningSession(ctx context.Context, runID processdomain.RunID, pid int, codexSessionID string, session domain.Session) error {
	if s.uow != nil {
		return s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().MarkRunning(ctx, runID, pid, codexSessionID); err != nil {
				return fmt.Errorf("mark process running: %w", err)
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			return nil
		})
	}
	if err := s.processes.MarkRunning(ctx, runID, pid, codexSessionID); err != nil {
		return fmt.Errorf("mark process running: %w", err)
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (s *Service) markProcessExitedWithSessionEvents(ctx context.Context, runID processdomain.RunID, result processdomain.ExitResult, session domain.Session, saveSession bool, inputs []sessionEventInput) error {
	return s.markProcessExitedWithSessionEventsAndSettlement(ctx, runID, result, session, saveSession, inputs, promptAppendSettlementAutomatic)
}

type promptAppendSettlement uint8

const (
	promptAppendSettlementAutomatic promptAppendSettlement = iota
	promptAppendSettlementRelease
	promptAppendSettlementComplete
)

func (s *Service) markProcessExitedWithSessionEventsAndSettlement(ctx context.Context, runID processdomain.RunID, result processdomain.ExitResult, session domain.Session, saveSession bool, inputs []sessionEventInput, settlement promptAppendSettlement) error {
	events, err := s.newSessionEvents(session, inputs)
	if err != nil {
		return err
	}
	var resetBatches []questiondomain.Batch
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			var err error
			resetBatches, err = persistProcessExitInTx(ctx, tx, runID, result, session, saveSession, events, settlement)
			return err
		}); err != nil {
			return err
		}
		for _, event := range events {
			s.publishSessionEvent(ctx, event)
		}
		if s.questions != nil {
			for _, batch := range resetBatches {
				s.publishQuestionBatch(questionBatchDTO(batch))
			}
		}
		return nil
	}
	if err := s.processes.MarkExited(ctx, runID, result); err != nil {
		return fmt.Errorf("mark process exited: %w", err)
	}
	if err := settlePromptAppends(ctx, s.repo, runID, result, settlement); err != nil {
		return err
	}
	if saveSession {
		if err := s.repo.Save(ctx, session); err != nil {
			return fmt.Errorf("save session: %w", err)
		}
	}
	for _, event := range events {
		if err := s.events.Append(ctx, event); err != nil {
			return err
		}
		s.publishSessionEvent(ctx, event)
	}
	return nil
}

func (s *Service) persistWorkflowResumeFailure(ctx context.Context, session domain.Session, runID processdomain.RunID, result processdomain.ExitResult, message string, inputs []sessionEventInput) error {
	runner, ok := s.workflows.(workflowResumeFailureRepositoryRunner)
	if s.uow == nil || !ok {
		return errors.New("workflow resume failure requires transactional workflow repository runner")
	}
	if err := transitionSession(&session, domain.StatusResumeFailed, result.FinishedAt); err != nil {
		return err
	}
	events, err := s.newSessionEvents(session, inputs)
	if err != nil {
		return err
	}
	var workflowEvents []eventdomain.DomainEvent
	var resetBatches []questiondomain.Batch
	err = s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		var err error
		resetBatches, err = persistProcessExitInTx(ctx, tx, runID, result, session, true, events, promptAppendSettlementRelease)
		if err != nil {
			return err
		}
		_, recorded, err := runner.MarkResumeFailedForSessionWithRepositories(ctx, domain.WorkflowResumeFailureInput{
			SessionID: session.ID,
			Code:      "resume_failed",
			Message:   message,
		}, tx.Workflows(), tx.Events())
		if err != nil {
			return fmt.Errorf("mark workflow resume failed: %w", err)
		}
		workflowEvents = recorded
		return nil
	})
	if err != nil {
		return err
	}
	for _, event := range events {
		s.publishSessionEvent(ctx, event)
	}
	for _, event := range workflowEvents {
		s.publishSessionEvent(ctx, event)
	}
	if s.questions != nil {
		for _, batch := range resetBatches {
			s.publishQuestionBatch(questionBatchDTO(batch))
		}
	}
	return nil
}

func persistProcessExitInTx(ctx context.Context, tx port.Tx, runID processdomain.RunID, result processdomain.ExitResult, session domain.Session, saveSession bool, events []eventdomain.DomainEvent, settlement promptAppendSettlement) ([]questiondomain.Batch, error) {
	if err := tx.Processes().MarkExited(ctx, runID, result); err != nil {
		return nil, fmt.Errorf("mark process exited: %w", err)
	}
	var resetBatches []questiondomain.Batch
	if repo, ok := tx.Questions().(questiondomain.AgentRepository); ok {
		batches, err := repo.ResetDeliveryAwaitingResumeByProcessRun(ctx, questiondomain.ProcessRunID(runID))
		if err != nil {
			return nil, err
		}
		resetBatches = batches
	}
	if err := settlePromptAppends(ctx, tx.Sessions(), runID, result, settlement); err != nil {
		return nil, err
	}
	if saveSession {
		if err := tx.Sessions().Save(ctx, session); err != nil {
			return nil, fmt.Errorf("save session: %w", err)
		}
	}
	for _, event := range events {
		if err := tx.Events().Append(ctx, event); err != nil {
			return nil, err
		}
	}
	return resetBatches, nil
}

func settlePromptAppends(ctx context.Context, repo domain.Repository, runID processdomain.RunID, result processdomain.ExitResult, settlement promptAppendSettlement) error {
	switch settlement {
	case promptAppendSettlementRelease:
		return repo.ReleasePromptAppends(ctx, string(runID))
	case promptAppendSettlementComplete:
		return repo.CompletePromptAppends(ctx, string(runID), result.FinishedAt)
	}
	if processExitFailed(result) {
		return repo.ReleasePromptAppends(ctx, string(runID))
	}
	return repo.CompletePromptAppends(ctx, string(runID), result.FinishedAt)
}

func (s *Service) saveSessionWithEvent(ctx context.Context, session domain.Session, eventType string, payload map[string]any) error {
	event, ok, err := s.newSessionEvent(session, eventType, payload)
	if err != nil {
		return err
	}
	return s.saveSessionAndEvent(ctx, session, event, ok)
}

func (s *Service) saveSessionWithEvents(ctx context.Context, session domain.Session, inputs []sessionEventInput) (bool, error) {
	events, err := s.newSessionEvents(session, inputs)
	if err != nil {
		return false, err
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			for _, event := range events {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return false, err
		}
		for _, event := range events {
			s.publishSessionEvent(ctx, event)
		}
		return true, nil
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return false, fmt.Errorf("save session: %w", err)
	}
	for _, event := range events {
		if err := s.events.Append(ctx, event); err != nil {
			return true, err
		}
		s.publishSessionEvent(ctx, event)
	}
	return true, nil
}

func (s *Service) saveSessionAndEvent(ctx context.Context, session domain.Session, event eventdomain.DomainEvent, hasEvent bool) error {
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			if hasEvent {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if hasEvent {
			s.publishSessionEvent(ctx, event)
		}
		return nil
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	if hasEvent {
		if err := s.events.Append(ctx, event); err != nil {
			return err
		}
		s.publishSessionEvent(ctx, event)
	}
	return nil
}

func (s *Service) saveSession(ctx context.Context, session domain.Session) error {
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			return nil
		}); err != nil {
			return err
		}
		return nil
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (s *Service) saveInterruptedSessionWithEvent(ctx context.Context, session domain.Session, finishedAt time.Time, failureReason string, eventType string, payload map[string]any) error {
	events, err := s.newSessionEvents(session, []sessionEventInput{{eventType: eventType, payload: payload}})
	if err != nil {
		return err
	}
	exitResult := processdomain.ExitResult{
		FailureReason: failureReason,
		FinishedAt:    finishedAt,
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			runID, err := markInterruptedProcessExited(ctx, tx.Processes(), session, exitResult)
			if err != nil {
				return err
			}
			if err := tx.Sessions().ReleasePromptAppends(ctx, string(runID)); err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			for _, event := range events {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		for _, event := range events {
			s.publishSessionEvent(ctx, event)
		}
		return nil
	}
	runID, err := markInterruptedProcessExited(ctx, s.processes, session, exitResult)
	if err != nil {
		return err
	}
	if err := s.repo.ReleasePromptAppends(ctx, string(runID)); err != nil {
		return err
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	for _, event := range events {
		if err := s.events.Append(ctx, event); err != nil {
			return err
		}
		s.publishSessionEvent(ctx, event)
	}
	return nil
}

func markInterruptedProcessExited(ctx context.Context, processes processdomain.Repository, session domain.Session, result processdomain.ExitResult) (processdomain.RunID, error) {
	if processes == nil {
		return "", nil
	}
	active, ok, err := processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
	if err != nil {
		return "", fmt.Errorf("find interrupted process run: %w", err)
	}
	if !ok {
		return "", nil
	}
	if err := processes.MarkExited(ctx, active.ID, result); err != nil {
		return "", fmt.Errorf("mark interrupted process exited: %w", err)
	}
	return active.ID, nil
}

func (s *Service) newSessionEvents(session domain.Session, inputs []sessionEventInput) ([]eventdomain.DomainEvent, error) {
	events := make([]eventdomain.DomainEvent, 0, len(inputs))
	for _, input := range inputs {
		event, ok, err := s.newSessionEvent(session, input.eventType, input.payload)
		if err != nil {
			return nil, err
		}
		if ok {
			events = append(events, event)
		}
	}
	return events, nil
}

func (s *Service) appendSessionEvent(ctx context.Context, session domain.Session, eventType string, payload map[string]any) {
	event, ok, err := s.newSessionEvent(session, eventType, payload)
	if err != nil || !ok {
		return
	}
	if err := s.events.Append(ctx, event); err == nil {
		s.publishSessionEvent(ctx, event)
	}
}

func (s *Service) newSessionEvent(session domain.Session, eventType string, payload map[string]any) (eventdomain.DomainEvent, bool, error) {
	if s.events == nil {
		return eventdomain.DomainEvent{}, false, nil
	}
	id, err := s.generateID()
	if err != nil {
		id = fallbackSessionEventID(session.ID)
	}
	event, ok := s.newSessionEventWithID(session, eventType, payload, id)
	return event, ok, nil
}

func (s *Service) newSessionEventWithID(session domain.Session, eventType string, payload map[string]any, id domain.ID) (eventdomain.DomainEvent, bool) {
	if s.events == nil {
		return eventdomain.DomainEvent{}, false
	}
	sessionID := eventdomain.SessionID(session.ID)
	eventPayload := copyPayload(payload)
	return eventdomain.DomainEvent{
		ID: eventdomain.ID(id),
		Scope: eventdomain.Scope{
			SessionID: &sessionID,
			ProjectID: string(session.ProjectID),
		},
		SessionID: &sessionID,
		Type:      eventType,
		Payload:   eventPayload,
		Causality: eventdomain.Causality{
			ProcessRunID:  payloadString(eventPayload, "processRunId"),
			WorkflowRunID: payloadString(eventPayload, "workflowRunId"),
			NodeRunID:     payloadString(eventPayload, "nodeRunId"),
			CorrelationID: payloadString(eventPayload, "correlationId"),
			SessionStatus: string(session.Status),
		},
		CreatedAt: s.now(),
	}, true
}

func payloadString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func fallbackSessionEventID(sessionID domain.ID) domain.ID {
	return domain.ID(fmt.Sprintf("fallback-%d-%d-%s", time.Now().UnixNano(), fallbackEventSequence.Add(1), sessionID))
}

func (s *Service) publishSessionEvent(ctx context.Context, event eventdomain.DomainEvent) {
	if s.publisher != nil {
		_ = s.publisher.PublishAfterCommit(ctx, event)
	}
}

func processEventPayload(event processdomain.CodexEvent) map[string]any {
	value, _ := sanitizeCodexPayloadValue(event.Payload, false).(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func copyPayload(input map[string]any) map[string]any {
	payload := make(map[string]any, len(input))
	for key, value := range input {
		payload[key] = value
	}
	return payload
}

const maxPersistedCodexStringBytes = 1 << 20

func sanitizeCodexPayloadValue(value any, artifactContext bool) any {
	switch typed := value.(type) {
	case map[string]any:
		contentType, _ := typed["type"].(string)
		artifactContext = artifactContext || isArtifactContentType(contentType)
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if artifactContext && isArtifactSourceField(key) {
				continue
			}
			result[key] = sanitizeCodexPayloadValue(child, artifactContext)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, child := range typed {
			result = append(result, sanitizeCodexPayloadValue(child, artifactContext))
		}
		return result
	case string:
		if len(typed) > maxPersistedCodexStringBytes {
			return "[omitted large value]"
		}
		return typed
	default:
		return value
	}
}

func isArtifactContentType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "input_image", "output_image", "image", "input_audio", "audio", "resource", "embedded_resource":
		return true
	default:
		return false
	}
}

func isArtifactSourceField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "image_url", "imageurl", "data", "blob", "audio", "url", "uri", "path", "text":
		return true
	default:
		return false
	}
}

func todoListFromCodexEvent(event processdomain.CodexEvent) (domain.TodoList, bool) {
	if event.PlanUpdate == nil {
		return domain.TodoList{}, false
	}
	list := domain.TodoList{Items: make([]domain.TodoItem, 0, len(event.PlanUpdate.Items))}
	for _, item := range event.PlanUpdate.Items {
		if strings.TrimSpace(item.Step) == "" {
			continue
		}
		list.Items = append(list.Items, domain.TodoItem{
			Text:      item.Step,
			Completed: item.Status == processdomain.PlanItemCompleted,
		})
	}
	return list, true
}

func codexSessionIDFromEvent(event processdomain.CodexEvent) string {
	for _, key := range codexSessionIDKeys() {
		if value, ok := event.Payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if msg, ok := event.Payload["msg"].(map[string]any); ok {
		for _, key := range codexSessionIDKeys() {
			if value, ok := msg[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func codexSessionIDKeys() []string {
	return []string{"session_id", "sessionId", "codex_session_id", "codexSessionId", "thread_id", "threadId", "conversation_id", "conversationId"}
}

func (s *Service) CloseSession(ctx context.Context, input CloseSessionInput) (DTO, error) {
	for {
		if err := ctx.Err(); err != nil {
			return DTO{}, err
		}
		var dto DTO
		err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
			var err error
			dto, err = s.closeSession(ctx, input)
			return err
		})
		if errors.Is(err, errClosePreparationStale) {
			continue
		}
		if !errors.Is(err, errCloseRequiresStop) {
			return dto, err
		}
		if _, err := s.StopSession(ctx, input.SessionID); err != nil {
			return DTO{}, err
		}
	}
}

func (s *Service) closeSession(ctx context.Context, input CloseSessionInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	reason := input.Reason
	if reason == "" {
		reason = domain.CloseReasonUserClosed
	}
	if reason != domain.CloseReasonUserClosed && reason != domain.CloseReasonMergedClosed && reason != domain.CloseReasonWorkflowClosed {
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "unsupported close reason").WithDetails(map[string]any{"reason": string(reason)})
	}
	if session.Status == domain.StatusClosed {
		if session.WorktreeCleanup.Status == domain.WorktreeCleanupPending || (session.WorktreeCleanup.Status == domain.WorktreeCleanupFailed && session.WorktreeCleanup.Retryable) {
			s.scheduleWorktreeCleanup()
		}
		return toDTO(session), nil
	}
	prepared, err := s.prepareSessionClose(ctx, session, reason)
	if err != nil {
		return DTO{}, err
	}
	switch prepared.Status {
	case port.CloseAlreadyClosed:
		return toDTO(prepared.Session), nil
	case port.CloseActive:
		return DTO{}, errCloseRequiresStop
	case port.CloseStale:
		return DTO{}, errClosePreparationStale
	case port.ClosePrepared:
		session = prepared.Session
	default:
		return DTO{}, fmt.Errorf("unsupported close preparation status %q", prepared.Status)
	}
	quarantinePath := ""
	releaseClose := func(cause error) error {
		if quarantinePath != "" && s.artifacts != nil {
			if restoreErr := s.artifacts.RestoreArtifactDir(context.WithoutCancel(ctx), session.ID, quarantinePath); restoreErr != nil {
				cause = errors.Join(cause, fmt.Errorf("restore artifact directory: %w", restoreErr))
			}
		}
		return s.releaseClosePreparation(ctx, prepared.Session, cause)
	}
	if s.artifactScanner != nil {
		if _, err := s.artifactScanner.Scan(ctx, domain.ArtifactScanRequest{
			SessionID:  session.ID,
			SourceType: domain.AttachmentSourceReconciled,
			SourceID:   "session_close",
		}); err != nil {
			return DTO{}, releaseClose(fmt.Errorf("final artifact scan: %w", err))
		}
	}
	if s.artifacts != nil {
		token, tokenErr := s.generateID()
		if tokenErr != nil {
			return DTO{}, releaseClose(fmt.Errorf("generate artifact quarantine token: %w", tokenErr))
		}
		quarantinePath, err = s.artifacts.QuarantineArtifactDir(ctx, session.ID, string(token))
		if err != nil {
			return DTO{}, releaseClose(fmt.Errorf("quarantine artifact directory: %w", err))
		}
	}
	if input.appliedSystemCommandID != "" {
		markSystemCommandApplied(&session, input.appliedSystemCommandID)
	}
	if err := s.cancelPendingQuestions(ctx, session.ID, "session closed"); err != nil {
		return DTO{}, releaseClose(err)
	}
	now := s.now()
	cleanupRequested := false
	if strings.TrimSpace(session.BaseBranch) != "" {
		switch session.WorktreeCleanup.Status {
		case domain.WorktreeCleanupPending:
			cleanupRequested = true
		case domain.WorktreeCleanupActive, domain.WorktreeCleanupProvisioning, domain.WorktreeCleanupFailed:
			if err := session.RequestWorktreeCleanup(now); err != nil {
				return DTO{}, releaseClose(err)
			}
			cleanupRequested = true
		case domain.WorktreeCleanupCleaned:
		case domain.WorktreeCleanupNotApplicable, "":
			closeErr := apperror.New(apperror.CodeCloseFailed, apperror.CategoryValidationError, "git session worktree ownership is not persisted").WithDetails(map[string]any{
				"sessionId": string(session.ID),
			})
			return DTO{}, releaseClose(closeErr)
		}
	}
	if err := session.Close(reason, now); err != nil {
		closeErr := fmt.Errorf("close session %s: %w", session.ID, err)
		return DTO{}, releaseClose(closeErr)
	}
	events := []sessionEventInput{{eventType: "session.closed", payload: map[string]any{"reason": string(reason)}}}
	if cleanupRequested {
		events = append(events, sessionEventInput{eventType: "session.worktree_cleanup_requested", payload: map[string]any{
			"worktreeBranch": session.WorktreeBranch,
		}})
	}
	committed, err := s.saveSessionWithEvents(ctx, session, events)
	if err != nil {
		if committed {
			if quarantinePath != "" && s.artifacts != nil {
				if cleanupErr := s.artifacts.DeleteQuarantine(context.WithoutCancel(ctx), quarantinePath); cleanupErr != nil {
					log.Printf("delete committed session artifact quarantine: session=%s error=%v", session.ID, cleanupErr)
				}
			}
			if cleanupRequested {
				s.scheduleWorktreeCleanup()
			}
			return DTO{}, err
		}
		return DTO{}, releaseClose(err)
	}
	if quarantinePath != "" && s.artifacts != nil {
		if cleanupErr := s.artifacts.DeleteQuarantine(context.WithoutCancel(ctx), quarantinePath); cleanupErr != nil {
			log.Printf("delete closed session artifact quarantine: session=%s error=%v", session.ID, cleanupErr)
		}
	}
	if cleanupRequested {
		s.scheduleWorktreeCleanup()
	}
	return toDTO(session), nil
}

func (s *Service) closeWorkflowSession(ctx context.Context, input CloseSessionInput) (DTO, error) {
	for {
		if err := ctx.Err(); err != nil {
			return DTO{}, err
		}
		dto, err := s.closeSession(ctx, input)
		if errors.Is(err, errClosePreparationStale) {
			continue
		}
		return dto, err
	}
}

func (s *Service) prepareSessionClose(ctx context.Context, expected domain.Session, reason domain.CloseReason) (port.ClosePreparationResult, error) {
	closing := expected
	if err := transitionSession(&closing, domain.StatusStopping, s.now()); err != nil {
		return port.ClosePreparationResult{}, err
	}
	closing.CloseReason = &reason
	if s.uow != nil {
		event, hasEvent, err := s.newSessionEvent(closing, "session.closing", map[string]any{"reason": string(reason)})
		if err != nil {
			return port.ClosePreparationResult{}, err
		}
		var result port.ClosePreparationResult
		var publish bool
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			prepared, err := tx.PrepareClose(ctx, port.ClosePreparationInput{ExpectedSession: expected, ClosingSession: closing})
			if err != nil {
				return err
			}
			result = prepared
			if prepared.Status == port.ClosePrepared && hasEvent {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
				publish = true
			}
			return nil
		}); err != nil {
			return port.ClosePreparationResult{}, err
		}
		if publish {
			s.publishSessionEvent(ctx, event)
		}
		return result, nil
	}
	// GLUE: Legacy in-memory wiring relies on the process-local session lock; production entstore uses the atomic close preparation above.
	if expected.Status == domain.StatusStopping && s.processes == nil {
		return port.ClosePreparationResult{Status: port.ClosePrepared, Session: expected}, nil
	}
	requiresStop, err := closeRequiresStop(ctx, s, expected)
	if err != nil {
		return port.ClosePreparationResult{}, err
	}
	if requiresStop {
		var activeRun *processdomain.Run
		if s.processes != nil {
			active, found, findErr := s.processes.FindActiveBySession(ctx, processdomain.SessionID(expected.ID))
			if findErr != nil {
				return port.ClosePreparationResult{}, findErr
			}
			if found {
				activeRun = &active
			}
		}
		return port.ClosePreparationResult{Status: port.CloseActive, Session: expected, ActiveRun: activeRun}, nil
	}
	if err := s.saveSessionWithEvent(ctx, closing, "session.closing", map[string]any{"reason": string(reason)}); err != nil {
		return port.ClosePreparationResult{}, err
	}
	return port.ClosePreparationResult{Status: port.ClosePrepared, Session: closing}, nil
}

func (s *Service) releaseClosePreparation(ctx context.Context, closing domain.Session, cause error) error {
	if closing.Status != domain.StatusStopping {
		return cause
	}
	closing.CloseReason = nil
	if err := transitionSession(&closing, domain.StatusStopped, s.now()); err != nil {
		return errors.Join(cause, err)
	}
	if err := s.saveSessionWithEvent(ctx, closing, "session.close_failed", map[string]any{"reason": cause.Error()}); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func closeRequiresStop(ctx context.Context, s *Service, session domain.Session) (bool, error) {
	if session.Status == domain.StatusClosed {
		return false, nil
	}
	if s != nil && s.processes != nil {
		_, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
		if err != nil {
			return false, fmt.Errorf("find active process run: %w", err)
		}
		return ok, nil
	}
	switch session.Status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusStopping, domain.StatusResumeFailed:
		return true, nil
	default:
		return false, nil
	}
}

func (s *Service) withSessionLock(ctx context.Context, id domain.ID, fn func(context.Context) error) error {
	if s == nil || s.locker == nil || id == "" {
		return fn(ctx)
	}
	return s.locker.WithSessionLock(ctx, id, fn)
}

func (s *Service) cancelPendingQuestions(ctx context.Context, sessionID domain.ID, reason string) error {
	if s.questions == nil {
		return nil
	}
	if err := s.questions.CancelPendingBySession(ctx, questiondomain.SessionID(sessionID), reason); err != nil {
		return apperror.Wrap(err, apperror.CodeAnswerUserCancelled, apperror.CategoryInfraError, "cancel pending questions failed").WithRetryable(true)
	}
	return nil
}

func (s *Service) AppendPrompt(ctx context.Context, input AppendPromptInput) (PromptAppendDTO, error) {
	if s == nil {
		return PromptAppendDTO{}, errors.New("session usecase: nil service")
	}
	if input.SessionID == "" {
		return PromptAppendDTO{}, errors.New("session id is required")
	}
	body := strings.TrimSpace(input.Body)
	stagedAttachments, err := s.findStagedAttachments(ctx, input.StagedAttachmentIDs)
	if err != nil {
		return PromptAppendDTO{}, err
	}
	if body == "" {
		if len(stagedAttachments) == 0 {
			return PromptAppendDTO{}, errors.New("prompt append body is required")
		}
		body = "追加附件"
	}
	var append domain.PromptAppend
	err = s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) (err error) {
		var archivedAttachments []domain.SessionAttachment
		appendSaved := false
		defer func() {
			if err == nil || appendSaved {
				return
			}
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if rollbackErr := s.rollbackPromptAppendArtifacts(cleanupCtx, append.ID, appendSaved, archivedAttachments); rollbackErr != nil {
				err = errors.Join(err, rollbackErr)
			}
		}()
		session, err := s.repo.Find(ctx, input.SessionID)
		if err != nil {
			return fmt.Errorf("find session: %w", err)
		}
		if session.Status == domain.StatusClosed {
			return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "closed session cannot be appended")
		}
		if err := requireActiveWorktree(session); err != nil {
			return err
		}
		id, err := s.generateID()
		if err != nil {
			return fmt.Errorf("generate prompt append id: %w", err)
		}
		archivedAttachments, err = s.archiveStagedAttachments(ctx, input.SessionID, domain.AttachmentSourcePromptAppend, string(id), stagedAttachments)
		if err != nil {
			return err
		}
		append = domain.PromptAppend{
			ID:          string(id),
			SessionID:   input.SessionID,
			Body:        body,
			Status:      domain.PromptAppendPending,
			CreatedAt:   s.now(),
			Attachments: archivedAttachments,
		}
		if session.Mode == domain.ModeChat && session.Status == domain.StatusStopped {
			options := codexStartOptions{queueKind: domain.QueueKindPromptAppend}
			kind := domain.QueueKindPromptAppend
			if codexSessionID := strings.TrimSpace(session.CodexSessionID); codexSessionID != "" {
				options.resumeCodexSessionID = codexSessionID
			}
			_, appendSaved, err = s.appendPromptAndQueue(ctx, session, append, options, queuePriorityForSession(session), kind)
			if err != nil {
				return err
			}
			s.scheduleQueueDrain()
			return nil
		}
		if err := s.repo.AppendPrompt(ctx, append); err != nil {
			return fmt.Errorf("append prompt: %w", err)
		}
		appendSaved = true
		if !canAutoStartAfterAppend(session) {
			return nil
		}
		if canReuseCodexSessionAfterAppend(session) {
			if _, err := s.resumeSession(ctx, input.SessionID, StartSessionOptions{resumeCodexSessionID: session.CodexSessionID, queueKind: domain.QueueKindPromptAppend}); err != nil {
				return fmt.Errorf("resume session after prompt append: %w", err)
			}
			return nil
		}
		if _, err := s.startSession(ctx, input.SessionID, StartSessionOptions{queueKind: domain.QueueKindPromptAppend}); err != nil {
			return fmt.Errorf("start session after prompt append: %w", err)
		}
		return nil
	})
	if err != nil {
		return PromptAppendDTO{}, err
	}
	return toPromptAppendDTO(append), nil
}

func (s *Service) UpdatePromptAppend(ctx context.Context, input UpdatePromptAppendInput) (PromptAppendDTO, error) {
	if s == nil {
		return PromptAppendDTO{}, errors.New("session usecase: nil service")
	}
	if input.SessionID == "" {
		return PromptAppendDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session id is required")
	}
	promptAppendID := strings.TrimSpace(input.PromptAppendID)
	if promptAppendID == "" {
		return PromptAppendDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "prompt append id is required")
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return PromptAppendDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "追加提示正文不能为空")
	}
	if s.locker == nil || s.uow == nil {
		return PromptAppendDTO{}, apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "prompt append editing is not configured").WithRetryable(true)
	}

	var updated domain.PromptAppend
	err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
		var attachments []domain.SessionAttachment
		if s.attachments != nil {
			var err error
			attachments, err = s.attachments.ListPromptAppendAttachments(ctx, input.SessionID, promptAppendID)
			if err != nil {
				return fmt.Errorf("list prompt append attachments: %w", err)
			}
		}
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			processes := tx.Processes()
			sessions := tx.Sessions()
			if processes == nil || sessions == nil {
				return apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "prompt append editing transaction is not configured").WithRetryable(true)
			}
			session, err := sessions.Find(ctx, input.SessionID)
			if err != nil {
				return fmt.Errorf("find session for prompt append editing: %w", err)
			}
			if session.Status == domain.StatusClosed {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "已关闭卡片不能编辑追加提示").
					WithDetails(map[string]any{"sessionId": string(input.SessionID)})
			}
			started, err := processes.HasAnyBySession(ctx, processdomain.SessionID(input.SessionID))
			if err != nil {
				return fmt.Errorf("check session process history: %w", err)
			}
			if started {
				return promptEditAfterStartError(input.SessionID, promptAppendID)
			}
			updatedAppend, ok, err := sessions.UpdatePendingPromptAppendBody(ctx, input.SessionID, promptAppendID, body)
			if err != nil {
				return err
			}
			if !ok {
				return apperror.New(apperror.CodeNotFound, apperror.CategoryValidationError, "待编辑的追加提示不存在或已投递").
					WithDetails(map[string]any{
						"sessionId":      string(input.SessionID),
						"promptAppendId": promptAppendID,
					})
			}
			updated = updatedAppend
			return nil
		}); err != nil {
			return err
		}
		updated.Attachments = attachments
		return nil
	})
	if err != nil {
		return PromptAppendDTO{}, err
	}
	return toPromptAppendDTO(updated), nil
}

func promptEditAfterStartError(sessionID domain.ID, promptAppendID string) error {
	return apperror.New(apperror.CodePromptEditAfterStart, apperror.CategoryValidationError, "流程已开始运行，无法编辑追加提示").
		WithDetails(map[string]any{
			"sessionId":      string(sessionID),
			"promptAppendId": promptAppendID,
		}).
		WithRetryable(false).
		WithUserAction("review_session")
}

func (s *Service) appendPromptAndQueue(ctx context.Context, session domain.Session, promptAppend domain.PromptAppend, options codexStartOptions, priority domain.QueuePriority, kind domain.QueueKind) (domain.Session, bool, error) {
	queued, event, hasEvent, err := s.prepareQueuedSession(session, options, priority, kind)
	if err != nil {
		return domain.Session{}, false, err
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Sessions().AppendPrompt(ctx, promptAppend); err != nil {
				return fmt.Errorf("append prompt: %w", err)
			}
			if err := tx.Sessions().Save(ctx, queued); err != nil {
				return fmt.Errorf("save queued session: %w", err)
			}
			if hasEvent {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return domain.Session{}, false, err
		}
		if hasEvent {
			s.publishSessionEvent(ctx, event)
		}
		return queued, true, nil
	}
	if err := s.repo.AppendPrompt(ctx, promptAppend); err != nil {
		return domain.Session{}, false, fmt.Errorf("append prompt: %w", err)
	}
	if err := s.saveSessionAndEvent(ctx, queued, event, hasEvent); err != nil {
		return domain.Session{}, true, err
	}
	return queued, true, nil
}

func canAutoStartAfterAppend(session domain.Session) bool {
	switch session.Status {
	case domain.StatusCreated, domain.StatusStopped, domain.StatusFailed, domain.StatusCompleted, domain.StatusResumeFailed:
		return true
	default:
		return false
	}
}

func canReuseCodexSessionAfterAppend(session domain.Session) bool {
	return session.Status == domain.StatusStopped && strings.TrimSpace(session.CodexSessionID) != ""
}

func (s *Service) SubmitWorkflowApproval(ctx context.Context, input SubmitWorkflowApprovalInput) (WorkflowRunDTO, error) {
	if s == nil {
		return WorkflowRunDTO{}, errors.New("session usecase: nil service")
	}
	if s.workflows == nil {
		return WorkflowRunDTO{}, errors.New("session workflow starter is required for workflow approval")
	}
	if input.WorkflowRunID == "" {
		return WorkflowRunDTO{}, errors.New("workflow run id is required")
	}
	if strings.TrimSpace(input.NodeID) == "" {
		return WorkflowRunDTO{}, errors.New("workflow node id is required")
	}
	if !input.Approved && strings.TrimSpace(input.Comment) == "" {
		return WorkflowRunDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "approval rejection prompt is required")
	}
	runner, ok := s.workflows.(workflowApprovalRepositoryRunner)
	if s.uow == nil || !ok {
		return WorkflowRunDTO{}, errors.New("workflow approval requires transactional workflow repository runner")
	}
	return s.submitWorkflowApprovalInTx(ctx, input, runner)
}

func (s *Service) submitWorkflowApprovalInTx(ctx context.Context, input SubmitWorkflowApprovalInput, runner workflowApprovalRepositoryRunner) (WorkflowRunDTO, error) {
	var result domain.WorkflowApprovalResult
	var publishEvents []eventdomain.DomainEvent
	var postCommitAdvance *workflowApprovalPostCommitAdvance
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		workflowInput := domain.WorkflowApprovalInput{
			WorkflowRunID: input.WorkflowRunID,
			NodeID:        input.NodeID,
			Approved:      input.Approved,
			Comment:       input.Comment,
		}
		approvalResult, workflowEvents, err := runner.SubmitApprovalForSessionWithRepositories(ctx, workflowInput, tx.Workflows(), tx.Events())
		if err != nil {
			return fmt.Errorf("submit workflow approval: %w", err)
		}
		session, err := tx.Sessions().Find(ctx, approvalResult.Run.SessionID)
		if err != nil {
			return fmt.Errorf("find session: %w", err)
		}
		if session.Mode != domain.ModeWorkflow {
			return fmt.Errorf("session %q is not workflow mode", session.ID)
		}
		ackEvents, err := s.ackAppliedSystemCommandsInTx(ctx, tx, &session)
		if err != nil {
			return err
		}
		publishEvents = append(publishEvents, ackEvents...)
		publishEvents = append(publishEvents, workflowEvents...)
		approvalEvent, hasApprovalEvent, err := s.newSessionEvent(session, "workflow.approval_submitted", map[string]any{
			"workflowRunId": string(input.WorkflowRunID),
			"nodeId":        input.NodeID,
			"approved":      input.Approved,
		})
		if err != nil {
			return err
		}
		if hasApprovalEvent {
			if err := tx.Events().Append(ctx, approvalEvent); err != nil {
				return err
			}
			publishEvents = append(publishEvents, approvalEvent)
		}
		switch {
		case approvalResult.RejectedAfterRun:
			if workflowAdvanceHasExternalEffects(approvalResult.Advance) {
				pending, err := s.persistPendingSystemAdvanceInTx(ctx, tx, session, approvalResult.Advance)
				if err != nil {
					return err
				}
				postCommitAdvance = &pending
			} else {
				queued, queuedEvent, hasQueuedEvent, err := s.queueApprovalRejectionPrompt(ctx, tx, session, approvalResult.Advance, strings.TrimSpace(input.Comment))
				if err != nil {
					return err
				}
				session = queued
				if hasQueuedEvent {
					publishEvents = append(publishEvents, queuedEvent)
				}
			}
		case approvalResult.Advance.Blocked:
			if err := transitionSession(&session, domain.StatusBlocked, s.now()); err != nil {
				return err
			}
			blockedEvent, hasBlockedEvent, err := s.newSessionEvent(session, "session.blocked", map[string]any{
				"workflowRunId": string(approvalResult.Advance.WorkflowRunID),
				"reason":        approvalResult.Advance.BlockedReason,
			})
			if err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			if hasBlockedEvent {
				if err := tx.Events().Append(ctx, blockedEvent); err != nil {
					return err
				}
				publishEvents = append(publishEvents, blockedEvent)
			}
		case approvalResult.Advance.Completed:
			if err := transitionSession(&session, domain.StatusCompleted, s.now()); err != nil {
				return err
			}
			completedEvent, hasCompletedEvent, err := s.newSessionEvent(session, "session.completed", map[string]any{
				"workflowRunId": string(approvalResult.Advance.WorkflowRunID),
			})
			if err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			if hasCompletedEvent {
				if err := tx.Events().Append(ctx, completedEvent); err != nil {
					return err
				}
				publishEvents = append(publishEvents, completedEvent)
			}
		case approvalResult.Advance.RequiresCodex:
			queued, queuedEvent, hasQueuedEvent, err := s.queueApprovalAdvance(ctx, tx, session, approvalResult.Advance)
			if err != nil {
				return err
			}
			session = queued
			if hasQueuedEvent {
				publishEvents = append(publishEvents, queuedEvent)
			}
		case !approvalResult.Advance.RequiresCodex && approvalResult.Advance.Merge == nil && approvalResult.Advance.Expr == nil && !approvalResult.Advance.Close:
			if err := transitionSessionToWaitingApproval(&session, false, s.now()); err != nil {
				return err
			}
			waitingEvent, hasWaitingEvent, err := s.newSessionEvent(session, "session.waiting_approval", map[string]any{
				"workflowRunId":    string(approvalResult.Advance.WorkflowRunID),
				"nodeRunId":        stringValuePtr(approvalResult.Advance.NodeRunID),
				"currentNodeId":    approvalResult.Advance.CurrentNodeID,
				"currentNodeTitle": approvalResult.Advance.CurrentNodeTitle,
				"approvalPhase":    approvalResult.Advance.ApprovalPhase,
				"result":           approvalResult.Advance.Result,
			})
			if err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			if hasWaitingEvent {
				if err := tx.Events().Append(ctx, waitingEvent); err != nil {
					return err
				}
				publishEvents = append(publishEvents, waitingEvent)
			}
		case workflowAdvanceHasExternalEffects(approvalResult.Advance):
			pending, err := s.persistPendingSystemAdvanceInTx(ctx, tx, session, approvalResult.Advance)
			if err != nil {
				return err
			}
			postCommitAdvance = &pending
		default:
			return errors.New("workflow approval returned unsupported transactional advance")
		}
		result = approvalResult
		return nil
	})
	if err != nil {
		return WorkflowRunDTO{}, err
	}
	for _, event := range publishEvents {
		s.publishSessionEvent(ctx, event)
	}
	if postCommitAdvance != nil {
		s.publishSessionEvent(ctx, postCommitAdvance.pendingEvent)
		if _, err := s.recoverPendingSystemAdvance(ctx, postCommitAdvance.session); err != nil {
			return WorkflowRunDTO{}, err
		}
	}
	s.scheduleQueueDrain()
	return toWorkflowRunDTO(result.Run), nil
}

func (s *Service) ackAppliedSystemCommandsInTx(ctx context.Context, tx port.Tx, session *domain.Session) ([]eventdomain.DomainEvent, error) {
	if session == nil || len(session.AppliedSystemCommands) == 0 {
		return nil, nil
	}
	sessionID := eventdomain.SessionID(session.ID)
	events, err := tx.Events().List(ctx, eventdomain.Scope{SessionID: &sessionID})
	if err != nil {
		return nil, err
	}
	completed := map[string]bool{}
	for _, event := range events {
		if event.Type == workflowSystemAdvanceCompletedEvent {
			completed[strings.TrimSpace(stringFromMap(event.Payload, "commandEventId"))] = true
		}
	}
	created := []eventdomain.DomainEvent{}
	for _, event := range events {
		commandID := string(event.ID)
		if event.Type != workflowSystemAdvancePendingEvent || completed[commandID] || !systemCommandApplied(*session, commandID) {
			continue
		}
		ack, ok, err := s.newSessionEvent(*session, workflowSystemAdvanceCompletedEvent, map[string]any{"commandEventId": commandID})
		if err != nil {
			return nil, err
		}
		if ok {
			if err := tx.Events().Append(ctx, ack); err != nil {
				return nil, err
			}
			created = append(created, ack)
			delete(session.AppliedSystemCommands, commandID)
		}
	}
	if len(created) > 0 {
		if err := tx.Sessions().Save(ctx, *session); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func (s *Service) persistPendingSystemAdvanceInTx(ctx context.Context, tx port.Tx, session domain.Session, advance domain.WorkflowAdvance) (workflowApprovalPostCommitAdvance, error) {
	if !workflowAdvanceHasExternalEffects(advance) {
		return workflowApprovalPostCommitAdvance{}, errors.New("workflow system advance has no external effect")
	}
	if session.Status != domain.StatusRunning {
		if err := transitionSession(&session, domain.StatusRunning, s.now()); err != nil {
			return workflowApprovalPostCommitAdvance{}, err
		}
	}
	event, ok, err := s.newSessionEvent(session, workflowSystemAdvancePendingEvent, workflowAdvancePendingPayload(advance))
	if err != nil {
		return workflowApprovalPostCommitAdvance{}, err
	}
	if !ok {
		return workflowApprovalPostCommitAdvance{}, errors.New("workflow system advance requires an event store")
	}
	if err := tx.Sessions().Save(ctx, session); err != nil {
		return workflowApprovalPostCommitAdvance{}, fmt.Errorf("save pending workflow system advance session: %w", err)
	}
	if err := tx.Events().Append(ctx, event); err != nil {
		return workflowApprovalPostCommitAdvance{}, err
	}
	return workflowApprovalPostCommitAdvance{session: session, advance: advance, commandEventID: event.ID, pendingEvent: event}, nil
}

func (s *Service) persistChainedSystemAdvance(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance) error {
	if s.uow == nil {
		return errors.New("chained workflow system advance requires unit of work")
	}
	var pending workflowApprovalPostCommitAdvance
	if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		created, err := s.persistPendingSystemAdvanceInTx(ctx, tx, session, advance)
		pending = created
		return err
	}); err != nil {
		return err
	}
	s.publishSessionEvent(ctx, pending.pendingEvent)
	return nil
}

func (s *Service) executePendingSystemAdvance(ctx context.Context, pending workflowApprovalPostCommitAdvance) error {
	session := pending.session
	if session.ID == "" {
		return errors.New("pending workflow system advance is missing session")
	}
	pending.advance.CommandID = string(pending.commandEventID)
	commandID := string(pending.commandEventID)
	if !systemCommandApplied(session, commandID) {
		if _, err := s.applyWorkflowAdvance(ctx, session, pending.advance, workflowAdvanceOptions{commandID: commandID}); err != nil {
			return err
		}
	}
	if err := s.completeSystemAdvanceCommand(ctx, session.ID, commandID); err != nil {
		return err
	}
	return nil
}

func (s *Service) completeSystemAdvanceCommand(ctx context.Context, sessionID domain.ID, commandID string) error {
	if s.uow == nil {
		session, err := s.repo.Find(ctx, sessionID)
		if err != nil {
			return err
		}
		event, ok, err := s.newSessionEvent(session, workflowSystemAdvanceCompletedEvent, map[string]any{"commandEventId": commandID})
		if err != nil || !ok {
			return err
		}
		if err := s.events.Append(ctx, event); err != nil {
			return err
		}
		s.publishSessionEvent(ctx, event)
		return nil
	}
	var published eventdomain.DomainEvent
	if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		session, err := tx.Sessions().Find(ctx, sessionID)
		if err != nil {
			return err
		}
		event, ok, err := s.newSessionEvent(session, workflowSystemAdvanceCompletedEvent, map[string]any{"commandEventId": commandID})
		if err != nil || !ok {
			return err
		}
		delete(session.AppliedSystemCommands, commandID)
		if err := tx.Sessions().Save(ctx, session); err != nil {
			return err
		}
		if err := tx.Events().Append(ctx, event); err != nil {
			return err
		}
		published = event
		return nil
	}); err != nil {
		return err
	}
	s.publishSessionEvent(ctx, published)
	return nil
}

func (s *Service) queueApprovalAdvance(ctx context.Context, tx port.Tx, session domain.Session, advance domain.WorkflowAdvance) (domain.Session, eventdomain.DomainEvent, bool, error) {
	options := workflowCodexStartOptions(session, advance, workflowAdvanceOptions{})
	queued, event, hasEvent, err := s.prepareQueuedSession(session, options, queuePriorityForSession(session), queueKindForStartOptions(options))
	if err != nil {
		return domain.Session{}, eventdomain.DomainEvent{}, false, err
	}
	if err := tx.Sessions().Save(ctx, queued); err != nil {
		return domain.Session{}, eventdomain.DomainEvent{}, false, fmt.Errorf("save queued session: %w", err)
	}
	if hasEvent {
		if err := tx.Events().Append(ctx, event); err != nil {
			return domain.Session{}, eventdomain.DomainEvent{}, false, err
		}
	}
	return queued, event, hasEvent, nil
}

func (s *Service) queueApprovalRejectionPrompt(ctx context.Context, tx port.Tx, session domain.Session, advance domain.WorkflowAdvance, body string) (domain.Session, eventdomain.DomainEvent, bool, error) {
	id, err := s.generateID()
	if err != nil {
		return domain.Session{}, eventdomain.DomainEvent{}, false, fmt.Errorf("generate prompt append id: %w", err)
	}
	promptAppend := domain.PromptAppend{
		ID:        string(id),
		SessionID: session.ID,
		Body:      body,
		Status:    domain.PromptAppendPending,
		CreatedAt: s.now(),
	}
	if err := tx.Sessions().AppendPrompt(ctx, promptAppend); err != nil {
		return domain.Session{}, eventdomain.DomainEvent{}, false, fmt.Errorf("append prompt: %w", err)
	}
	return s.queueApprovalAdvance(ctx, tx, session, advance)
}

func workflowCodexStartOptions(session domain.Session, advance domain.WorkflowAdvance, advanceOptions workflowAdvanceOptions) codexStartOptions {
	options := codexStartOptions{
		workflowRunID:           advance.WorkflowRunID,
		nodeRunID:               workflowNodeRunID(advance.NodeRunID),
		prompt:                  advance.Prompt,
		workflowResultRetry:     advance.RequireResultRetry,
		reviewAfterReuseFailure: advanceOptions.forceNewCodexSession,
		initialStart:            advanceOptions.initialStart || session.Queue.InitialStart,
	}
	if strings.TrimSpace(session.CodexSessionID) != "" && !advanceOptions.forceNewCodexSession {
		options.resumeCodexSessionID = session.CodexSessionID
	}
	return options
}

func (s *Service) GetSession(ctx context.Context, id domain.ID) (DetailDTO, error) {
	if s == nil {
		return DetailDTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DetailDTO{}, fmt.Errorf("find session: %w", err)
	}
	appends, err := s.repo.ListPromptAppends(ctx, id)
	if err != nil {
		return DetailDTO{}, fmt.Errorf("list prompt appends: %w", err)
	}
	attachments, err := s.listSessionAttachments(ctx, id)
	if err != nil {
		return DetailDTO{}, err
	}
	appends = attachPromptAppendAttachments(appends, attachments)
	currentNodeTitle, pendingApproval, err := s.currentNodeState(ctx, session)
	if err != nil {
		return DetailDTO{}, err
	}
	return toDetailDTO(session, attachments, appends, currentNodeTitle, pendingApproval), nil
}

func (s *Service) GetSessionCard(ctx context.Context, id domain.ID) (CardDTO, error) {
	if s == nil {
		return CardDTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return CardDTO{}, fmt.Errorf("find session: %w", err)
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
	if err != nil {
		return CardDTO{}, fmt.Errorf("find session project: %w", err)
	}
	attachments, err := s.listSessionAttachments(ctx, id)
	if err != nil {
		return CardDTO{}, err
	}
	currentNodeTitle, pendingApproval, err := s.currentNodeState(ctx, session)
	if err != nil {
		return CardDTO{}, err
	}
	card := toCardDTO(session, attachments, currentNodeTitle, pendingApproval)
	card.ProjectName = project.Name
	return card, nil
}

func (s *Service) ListSessions(ctx context.Context, input ListSessionsInput) (port.Page[CardDTO], error) {
	if s == nil {
		return port.Page[CardDTO]{}, errors.New("session usecase: nil service")
	}
	page, pageSize := normalizePage(input.Page, input.PageSize)
	query := domain.ListQuery{
		ProjectID: input.ProjectID,
		Scope:     input.Scope,
		Range:     input.Range,
		Page:      page,
		PageSize:  pageSize,
		Filter:    input.Filter,
		Sort:      input.Sort,
	}
	sessions, total, err := s.repo.ListCards(ctx, query)
	if err != nil {
		return port.Page[CardDTO]{}, fmt.Errorf("list session cards: %w", err)
	}
	items := make([]CardDTO, 0, len(sessions))
	projectNames := make(map[domain.ProjectID]string)
	for _, session := range sessions {
		projectName, ok := projectNames[session.ProjectID]
		if !ok {
			project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
			if err != nil {
				return port.Page[CardDTO]{}, fmt.Errorf("find session project: %w", err)
			}
			if project.RemovedAt != nil {
				continue
			}
			projectName = project.Name
			projectNames[session.ProjectID] = projectName
		}
		attachments, err := s.listSessionAttachments(ctx, session.ID)
		if err != nil {
			return port.Page[CardDTO]{}, err
		}
		currentNodeTitle, pendingApproval, err := s.currentNodeState(ctx, session)
		if err != nil {
			return port.Page[CardDTO]{}, err
		}
		item := toCardDTO(session, attachments, currentNodeTitle, pendingApproval)
		item.ProjectName = projectName
		items = append(items, item)
	}
	return port.Page[CardDTO]{
		Items:    items,
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    total,
	}, nil
}

func toDTO(session domain.Session) DTO {
	return DTO{
		ID:                 session.ID,
		ProjectID:          session.ProjectID,
		Requirement:        session.Requirement,
		Mode:               session.Mode,
		Status:             session.Status,
		Priority:           normalizePriority(session.Priority),
		BaseBranch:         session.BaseBranch,
		WorktreeBranch:     worktreeBranchForSession(session),
		WorktreePath:       session.WorktreePath,
		WorktreeBaseCommit: session.WorktreeBaseCommit,
		WorktreeCleanup:    toWorktreeCleanupDTO(session.WorktreeCleanup),
		CodexSessionID:     session.CodexSessionID,
		Config:             session.Config,
		ArtifactCount:      session.ArtifactCount,
		FilesChanged:       session.FilesChanged,
		AvailableActions:   availableActions(session),
		LastRunAt:          session.LastRunAt,
		CreatedAt:          session.CreatedAt,
		UpdatedAt:          session.UpdatedAt,
	}
}

func worktreeBranchForSession(session domain.Session) string {
	if strings.TrimSpace(session.BaseBranch) == "" {
		return ""
	}
	return strings.TrimSpace(session.WorktreeBranch)
}

func worktreeBranchName(sessionID domain.ID) string {
	return strings.TrimSpace(string(sessionID))
}

func toWorktreeCleanupDTO(cleanup domain.WorktreeCleanup) WorktreeCleanupDTO {
	dto := WorktreeCleanupDTO{
		Status:      cleanup.Status,
		Attempts:    cleanup.Attempts,
		RequestedAt: cleanup.RequestedAt,
		CompletedAt: cleanup.CompletedAt,
	}
	if cleanup.ErrorCode != "" || cleanup.Error != "" {
		dto.Error = &WorktreeCleanupErrorDTO{
			Code:      cleanup.ErrorCode,
			Message:   cleanup.Error,
			Retryable: cleanup.Retryable,
		}
	}
	return dto
}

func toWorkflowRunDTO(run domain.WorkflowRunSnapshot) WorkflowRunDTO {
	values := map[string]any{}
	for key, value := range run.Context {
		values[key] = value
	}
	return WorkflowRunDTO{
		ID:            run.ID,
		SessionID:     run.SessionID,
		Status:        run.Status,
		CurrentNodeID: run.CurrentNodeID,
		Context:       values,
	}
}

func (s *Service) currentNodeState(ctx context.Context, session domain.Session) (string, *PendingApprovalDTO, error) {
	if s.events == nil || session.Mode != domain.ModeWorkflow {
		return "", nil, nil
	}
	sessionID := eventdomain.SessionID(session.ID)
	events, err := s.events.After(ctx, eventdomain.Scope{ProjectID: string(session.ProjectID), SessionID: &sessionID}, "")
	if err != nil {
		return "", nil, fmt.Errorf("list session events for current node: %w", err)
	}
	title := ""
	var pendingApproval *PendingApprovalDTO
	for i := len(events) - 1; i >= 0; i-- {
		if pendingApproval == nil && session.Status == domain.StatusWaitingApproval {
			if approval := pendingApprovalFromEvent(events[i]); approval != nil {
				pendingApproval = approval
				if strings.TrimSpace(title) == "" {
					title = approval.CurrentNodeTitle
				}
			}
		}
		if strings.TrimSpace(title) == "" && strings.HasPrefix(events[i].Type, "workflow.") {
			if value, ok := events[i].Payload["currentNodeTitle"].(string); ok {
				title = strings.TrimSpace(value)
			}
		}
		if strings.TrimSpace(title) != "" && (pendingApproval != nil || session.Status != domain.StatusWaitingApproval) {
			return title, pendingApproval, nil
		}
	}
	return title, pendingApproval, nil
}

func pendingApprovalFromEvent(event eventdomain.DomainEvent) *PendingApprovalDTO {
	switch event.Type {
	case "workflow.waiting_approval", "session.waiting_approval":
	default:
		return nil
	}
	workflowRunID, _ := event.Payload["workflowRunId"].(string)
	nodeID, _ := event.Payload["currentNodeId"].(string)
	if strings.TrimSpace(workflowRunID) == "" || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	nodeRunID, _ := event.Payload["nodeRunId"].(string)
	title, _ := event.Payload["currentNodeTitle"].(string)
	phase, _ := event.Payload["approvalPhase"].(string)
	result, _ := event.Payload["result"].(map[string]any)
	return &PendingApprovalDTO{
		WorkflowRunID:    domain.WorkflowRunID(workflowRunID),
		NodeID:           strings.TrimSpace(nodeID),
		NodeRunID:        strings.TrimSpace(nodeRunID),
		CurrentNodeTitle: strings.TrimSpace(title),
		Phase:            strings.TrimSpace(phase),
		Result:           result,
	}
}

func toCardDTO(session domain.Session, attachments []domain.SessionAttachment, currentNodeTitle string, pendingApproval *PendingApprovalDTO) CardDTO {
	return CardDTO{
		DTO:                toDTO(session),
		RequirementSummary: session.Requirement,
		CurrentNodeTitle:   currentNodeTitle,
		PendingApproval:    pendingApproval,
		PendingQuestion:    session.Status == domain.StatusWaitingUser,
		TodoList:           session.TodoList,
		Attachments:        attachments,
		AvailableActions:   availableActions(session),
	}
}

func toDetailDTO(session domain.Session, attachments []domain.SessionAttachment, appends []domain.PromptAppend, currentNodeTitle string, pendingApproval *PendingApprovalDTO) DetailDTO {
	promptAppends := make([]PromptAppendDTO, 0, len(appends))
	for _, promptAppend := range appends {
		promptAppends = append(promptAppends, toPromptAppendDTO(promptAppend))
	}
	return DetailDTO{
		DTO:              toDTO(session),
		CloseReason:      session.CloseReason,
		CurrentNodeTitle: currentNodeTitle,
		PendingApproval:  pendingApproval,
		Attachments:      attachments,
		PromptAppends:    promptAppends,
		AvailableActions: availableActions(session),
		CanResume:        canResume(session),
	}
}

func (s *Service) findStagedAttachments(ctx context.Context, ids []domain.StagedAttachmentID) ([]domain.StagedAttachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if s.attachments == nil {
		return nil, errors.New("attachment repository is required")
	}
	if s.files == nil {
		return nil, errors.New("attachment store is required")
	}
	attachments := make([]domain.StagedAttachment, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			return nil, errors.New("staged attachment id is required")
		}
		staged, err := s.attachments.FindStagedAttachment(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("find staged attachment %s: %w", id, err)
		}
		attachments = append(attachments, staged)
	}
	return attachments, nil
}

func (s *Service) archiveStagedAttachments(ctx context.Context, sessionID domain.ID, sourceType domain.AttachmentSourceType, sourceID string, stagedAttachments []domain.StagedAttachment) ([]domain.SessionAttachment, error) {
	archived := make([]domain.SessionAttachment, 0, len(stagedAttachments))
	for _, staged := range stagedAttachments {
		attachment, err := s.files.Promote(ctx, staged, sessionID)
		if err != nil {
			return archived, fmt.Errorf("promote staged attachment %s: %w", staged.ID, err)
		}
		attachment.SourceType = sourceType
		attachment.SourceID = sourceID
		if err := s.attachments.SaveSessionAttachment(ctx, attachment); err != nil {
			_ = s.files.DeleteSession(ctx, attachment.ID)
			return archived, fmt.Errorf("save session attachment %s: %w", attachment.ID, err)
		}
		archived = append(archived, attachment)
		if err := s.attachments.DeleteStagedAttachment(ctx, staged.ID); err != nil {
			return archived, fmt.Errorf("delete staged attachment %s: %w", staged.ID, err)
		}
	}
	return archived, nil
}

func (s *Service) listPromptAppendAttachments(ctx context.Context, sessionID domain.ID, appendID string) ([]domain.SessionAttachment, error) {
	if s.attachments == nil {
		return []domain.SessionAttachment{}, nil
	}
	attachments, err := s.attachments.ListPromptAppendAttachments(ctx, sessionID, appendID)
	if err != nil {
		return nil, fmt.Errorf("list prompt append attachments: %w", err)
	}
	return attachments, nil
}

func (s *Service) rollbackPromptAppendArtifacts(ctx context.Context, appendID string, appendSaved bool, attachments []domain.SessionAttachment) error {
	var errs []error
	if appendSaved && appendID != "" {
		if err := s.repo.DeletePromptAppend(ctx, appendID); err != nil {
			errs = append(errs, fmt.Errorf("delete prompt append %s: %w", appendID, err))
		}
	}
	for _, attachment := range attachments {
		if s.attachments != nil {
			if err := s.attachments.DeleteSessionAttachment(ctx, attachment.ID); err != nil {
				errs = append(errs, fmt.Errorf("delete session attachment metadata %s: %w", attachment.ID, err))
			}
		}
		if s.files != nil {
			if err := s.files.DeleteSession(ctx, attachment.ID); err != nil {
				errs = append(errs, fmt.Errorf("delete session attachment file %s: %w", attachment.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) listSessionAttachments(ctx context.Context, sessionID domain.ID) ([]domain.SessionAttachment, error) {
	if s.attachments == nil {
		return []domain.SessionAttachment{}, nil
	}
	attachments, err := s.attachments.ListSessionAttachments(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session attachments: %w", err)
	}
	return attachments, nil
}

func attachPromptAppendAttachments(appends []domain.PromptAppend, attachments []domain.SessionAttachment) []domain.PromptAppend {
	byAppend := make(map[string][]domain.SessionAttachment)
	for _, attachment := range attachments {
		if attachment.SourceType != domain.AttachmentSourcePromptAppend || attachment.SourceID == "" {
			continue
		}
		byAppend[attachment.SourceID] = append(byAppend[attachment.SourceID], attachment)
	}
	for index := range appends {
		appends[index].Attachments = append([]domain.SessionAttachment(nil), byAppend[appends[index].ID]...)
	}
	return appends
}

func toPromptAppendDTO(append domain.PromptAppend) PromptAppendDTO {
	return PromptAppendDTO{
		ID:          append.ID,
		SessionID:   append.SessionID,
		Body:        append.Body,
		CreatedAt:   append.CreatedAt,
		Attachments: append.Attachments,
	}
}

func availableActions(session domain.Session) []string {
	if strings.TrimSpace(session.BaseBranch) != "" && session.WorktreeCleanup.Status != domain.WorktreeCleanupActive {
		if session.Status == domain.StatusClosed && session.WorktreeCleanup.Status == domain.WorktreeCleanupFailed && session.WorktreeCleanup.Retryable {
			return []string{"retry_worktree_cleanup"}
		}
		if session.Status != domain.StatusClosed {
			return []string{"close"}
		}
		return []string{}
	}
	switch session.Status {
	case domain.StatusCreated, domain.StatusStopped, domain.StatusFailed, domain.StatusCompleted:
		return []string{"execute", "close"}
	case domain.StatusQueued:
		return []string{"execute", "stop", "close"}
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping:
		return []string{"stop"}
	case domain.StatusWaitingApproval:
		return []string{"close"}
	case domain.StatusBlocked:
		return []string{"close"}
	case domain.StatusResumeFailed:
		return []string{"execute", "stop", "close"}
	case domain.StatusClosed:
		return []string{}
	default:
		return []string{"close"}
	}
}

func stringValuePtr(value *domain.NodeRunID) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func workflowNodeRunID(value *domain.NodeRunID) *processdomain.NodeRunID {
	if value == nil {
		return nil
	}
	id := processdomain.NodeRunID(*value)
	return &id
}

func canResume(session domain.Session) bool {
	return strings.TrimSpace(session.CodexSessionID) != "" &&
		(strings.TrimSpace(session.BaseBranch) == "" || session.WorktreeCleanup.Status == domain.WorktreeCleanupActive) &&
		(session.Status == domain.StatusStopped || session.Status == domain.StatusResumeFailed)
}

func requireActiveWorktree(session domain.Session) error {
	if err := session.RequireActiveWorktree(); err != nil {
		return apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryUserActionRequired, "session worktree is not available for execution").WithDetails(map[string]any{
			"sessionId":             string(session.ID),
			"worktreeCleanupStatus": string(session.WorktreeCleanup.Status),
		}).WithRetryable(session.WorktreeCleanup.Retryable).WithUserAction("wait_for_worktree_cleanup")
	}
	return nil
}

func generateID() (domain.ID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return domain.ID(hex.EncodeToString(b[:])), nil
}

func generateWorktreeOwnershipToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = defaultPage
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}
