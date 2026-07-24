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
	pathpkg "path"
	"path/filepath"
	"reflect"
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
	RecoverInitializingSessions(ctx context.Context) (int, error)
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
	RetrySessionInitialization(ctx context.Context, id domain.ID) (DTO, error)
	StartWorktreeCleanupCoordinator()
	CloseSession(ctx context.Context, input CloseSessionInput) (DTO, error)
	UpdateSessionConfig(ctx context.Context, input UpdateSessionConfigInput) (DTO, error)
	RequestQuestions(ctx context.Context, input RequestQuestionsInput) (questionapp.RequestDTO, error)
	SubmitQuestionRequest(ctx context.Context, input questionapp.SubmitRequestInput) (questionapp.RequestDTO, error)
	AppendPrompt(ctx context.Context, input AppendPromptInput) (PromptAppendDTO, error)
	UpdatePromptAppend(ctx context.Context, input UpdatePromptAppendInput) (PromptAppendDTO, error)
	DeleteSessionFile(ctx context.Context, id domain.SessionFileID) error
	SubmitWorkflowApproval(ctx context.Context, input SubmitWorkflowApprovalInput) (WorkflowRunDTO, error)
	GetSession(ctx context.Context, id domain.ID) (DetailDTO, error)
	GetSessionCard(ctx context.Context, id domain.ID) (CardDTO, error)
	GetSessionCardStatus(ctx context.Context, id domain.ID) (CardStatusDTO, error)
	ListSessions(ctx context.Context, input ListSessionsInput) (port.Page[CardDTO], error)
	CleanupSessions(ctx context.Context, input CleanupSessionsInput) (int, error)
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
	Mentions            []domain.PromptMention
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

type CleanupSessionsInput struct {
	ProjectID     *domain.ProjectID
	Scope         string
	Filter        string
	OlderThanDays int
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
	ArtifactIDs         []domain.SessionFileID
	Mentions            []domain.PromptMention
}

type UpdatePromptAppendInput struct {
	SessionID      domain.ID
	PromptAppendID string
	Body           string
}

type SubmitWorkflowApprovalInput struct {
	SessionID domain.ID
	NodeID    string
	Approved  bool
	Comment   string
}

type RequestQuestionsInput struct {
	RequestID questiondomain.RequestID
	SessionID domain.ID
	Questions []questiondomain.Question
}

type ListSessionsInput struct {
	ProjectID     *domain.ProjectID
	Scope         string
	Range         string
	OlderThanDays int
	Page          int
	PageSize      int
	Filter        string
	Sort          string
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
	Usage              *domain.TokenUsage
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

type CardDTO struct {
	DTO
	ProjectName        string
	RequirementSummary string
	CurrentNodeTitle   string
	TodoList           domain.TodoList
	Attachments        []domain.SessionAttachment
	AvailableActions   []string
}

type CardStatusDTO struct {
	Status           domain.Status
	CurrentNodeTitle string
	AvailableActions []string
	UpdatedAt        time.Time
}

type DetailDTO struct {
	DTO
	ProjectName      string
	CloseReason      *domain.CloseReason
	CurrentNodeTitle string
	PendingApproval  *PendingApprovalDTO
	TodoList         domain.TodoList
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
	Artifacts   []domain.SessionFile
}

type WorkflowRunDTO struct {
	SessionID     domain.ID
	Status        string
	CurrentNodeID string
	Context       map[string]any
}

type PendingApprovalDTO struct {
	SessionID        domain.ID
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
	publishEvents  []eventdomain.DomainEvent
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
	sessionStatusUpdatedEvent           = "session.status_updated"
)

var (
	errCloseRequiresStop     = errors.New("session must stop before close")
	errClosePreparationStale = errors.New("session changed while preparing close")
)
var fallbackEventSequence atomic.Uint64

type Service struct {
	repo                    domain.Repository
	uow                     port.UnitOfWork
	locker                  port.SessionLocker
	projects                projectdomain.Repository
	attachments             domain.StagedAttachmentRepository
	files                   domain.AttachmentStore
	artifacts               domain.ArtifactStore
	artifactPublisher       domain.ArtifactPublisher
	worktrees               domain.WorktreeManager
	worktreeInitializer     domain.WorktreeInitializer
	workflows               domain.WorkflowStarter
	merge                   gitdiffdomain.MergePort
	diffCounter             sessionDiffCounter
	processes               processdomain.Repository
	codex                   processdomain.CodexProcess
	codexSessionCleaner     processdomain.CodexSessionCleaner
	historyPurger           port.SessionHistoryPurger
	processConsumers        sync.Map
	workdirMu               sync.Mutex
	activeWorkdirs          map[string]domain.ID
	events                  eventdomain.Store
	publisher               eventdomain.Publisher
	codexPublisher          codexEventPublisher
	questions               questionCoordinator
	tunnels                 tunnelCleaner
	now                     func() time.Time
	generateID              func() (domain.ID, error)
	maxConcurrentAgents     int
	initializationScheduler func(*Service, domain.ID, bool)
	initializationWG        sync.WaitGroup
	queueDrainScheduler     func(*Service)
	processExitDelay        func(int) time.Duration
	lifecycleCtx            context.Context
	lifecycleCancel         context.CancelFunc
	cleanupWake             chan struct{}
	cleanupStartOnce        sync.Once
	cleanupWG               sync.WaitGroup
}

type questionCoordinator interface {
	CreateRequest(ctx context.Context, input questionapp.CreateRequestInput) (questionapp.RequestDTO, error)
	CancelPendingRequestsBySession(ctx context.Context, sessionID questiondomain.SessionID, reason string) error
}

type tunnelCleaner interface {
	CloseTunnelsForSession(ctx context.Context, sessionID domain.ID) error
}

type codexEventPublisher interface {
	PublishCodexEvent(ctx context.Context, event processdomain.CodexEvent) error
}

type sessionDiffCounter interface {
	CountSessionChangedFiles(ctx context.Context, sessionID domain.ID) (int, error)
}

type questionRequestCoordinator interface {
	questionCoordinator
	SubmitRequest(ctx context.Context, input questionapp.SubmitRequestInput) (questionapp.RequestDTO, error)
	GetRequest(ctx context.Context, id questiondomain.RequestID) (questionapp.RequestDTO, error)
	QuestionRequestUpdates(ctx context.Context, sessionID questiondomain.SessionID) (<-chan questionapp.RequestDTO, error)
	PublishRequest(request questionapp.RequestDTO)
}

type workflowApprovalRepositoryRunner interface {
	SubmitApprovalForSessionWithRepositories(ctx context.Context, input domain.WorkflowApprovalInput, repo workflowdomain.Repository, events eventdomain.Store) (domain.WorkflowApprovalResult, []eventdomain.DomainEvent, error)
}

type workflowResumeFailureRepositoryRunner interface {
	MarkResumeFailedForSessionWithRepositories(ctx context.Context, input domain.WorkflowResumeFailureInput, repo workflowdomain.Repository, events eventdomain.Store) (domain.WorkflowRunSnapshot, []eventdomain.DomainEvent, error)
}

type Option func(*Service)

func WithAttachments(repo domain.StagedAttachmentRepository, store domain.AttachmentStore) Option {
	return func(s *Service) {
		s.attachments = repo
		s.files = store
		if artifacts, ok := store.(domain.ArtifactStore); ok {
			s.artifacts = artifacts
		}
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
		if cleaner, ok := codex.(processdomain.CodexSessionCleaner); ok {
			s.codexSessionCleaner = cleaner
		}
	}
}

func WithSessionHistoryPurger(purger port.SessionHistoryPurger) Option {
	return func(s *Service) {
		s.historyPurger = purger
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
		if codexPublisher, ok := publisher.(codexEventPublisher); ok {
			s.codexPublisher = codexPublisher
		}
	}
}

func WithQuestions(questions questionCoordinator) Option {
	return func(s *Service) {
		s.questions = questions
	}
}

func WithTunnels(tunnels tunnelCleaner) Option {
	return func(s *Service) {
		s.tunnels = tunnels
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

func WithAutoSessionInitialization() Option {
	return func(s *Service) {
		s.initializationScheduler = func(service *Service, id domain.ID, recovery bool) {
			service.initializationWG.Add(1)
			go func() {
				defer service.initializationWG.Done()
				if _, err := service.initializeSession(service.lifecycleCtx, id, recovery); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("initialize session: session=%s recovery=%t error=%v", id, recovery, err)
				}
			}()
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
		lifecycleCtx:        lifecycleCtx,
		lifecycleCancel:     lifecycleCancel,
		cleanupWake:         make(chan struct{}, 1),
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Close() {
	if s != nil && s.lifecycleCancel != nil {
		s.lifecycleCancel()
		s.initializationWG.Wait()
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
				statusChanged := current.Status != domain.StatusClosed && current.Status != domain.StatusFailed
				if statusChanged {
					if err := transitionSession(&current, domain.StatusFailed, now); err != nil {
						return err
					}
				}
				if err := current.RequestWorktreeCleanup(now); err != nil {
					return err
				}
				payload := worktreeUpdatePayload(current, map[string]any{
					"reason":         "service_restarted_during_provisioning",
					"worktreeBranch": current.WorktreeBranch,
				})
				var saveErr error
				if statusChanged {
					saveErr = s.saveSessionWithStatusUpdate(ctx, current, "session.worktree_cleanup_requested", payload)
				} else {
					saveErr = s.saveSessionWithEvent(ctx, current, "session.worktree_cleanup_requested", payload)
				}
				if saveErr != nil {
					return saveErr
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
		if err := s.saveSessionWithEvent(ctx, current, "session.worktree_cleanup_requested", worktreeUpdatePayload(current, map[string]any{
			"reason":         "user_retry",
			"worktreeBranch": current.WorktreeBranch,
		})); err != nil {
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

func (s *Service) RetrySessionInitialization(ctx context.Context, id domain.ID) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		current, err := s.repo.Find(ctx, id)
		if err != nil {
			return fmt.Errorf("find session for initialization retry: %w", err)
		}
		if current.Status != domain.StatusFailed || strings.TrimSpace(current.InitializationErrorCode) == "" {
			return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session initialization cannot be retried").WithDetails(map[string]any{
				"sessionId": string(id),
				"status":    string(current.Status),
			})
		}
		current.InitializationErrorCode = ""
		current.InitializationError = ""
		if err := transitionSession(&current, domain.StatusInitializing, s.now()); err != nil {
			return err
		}
		if err := s.saveSessionWithStatusUpdate(ctx, current, "session.initialization_retry_requested", map[string]any{
			"reason": "user_retry",
		}); err != nil {
			return err
		}
		dto = toDTO(current)
		return nil
	})
	if err != nil {
		return DTO{}, err
	}
	if s.initializationScheduler != nil {
		s.initializationScheduler(s, id, true)
		return dto, nil
	}
	return s.initializeSession(ctx, id, true)
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
			if err := s.retainSessionWorktreeHead(ctx, project.Path.Value, session); err != nil {
				return s.recordWorktreeCleanupFailure(ctx, session, "worktree_head_retain_failed", err.Error(), true)
			}
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
		if err := s.saveSessionWithEvent(ctx, session, "session.worktree_ownership_confirmed", worktreeUpdatePayload(session, map[string]any{
			"worktreeBranch": session.WorktreeBranch,
		})); err != nil {
			return err
		}
	}
	if ownership.PathExists && !ownership.Matches {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_ownership_changed", "managed worktree path no longer matches the persisted branch; no resources were deleted", false)
	}
	if err := s.retainSessionWorktreeHead(ctx, project.Path.Value, session); err != nil {
		return s.recordWorktreeCleanupFailure(ctx, session, "worktree_head_retain_failed", err.Error(), true)
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

func (s *Service) retainSessionWorktreeHead(ctx context.Context, projectPath string, session domain.Session) error {
	if strings.TrimSpace(session.WorktreeHeadCommit) == "" {
		return nil
	}
	return s.worktrees.RetainCommit(ctx, projectPath, session.ID, session.WorktreeHeadCommit)
}

func (s *Service) completeWorktreeCleanup(ctx context.Context, session domain.Session) error {
	if err := session.CompleteWorktreeCleanup(s.now()); err != nil {
		return err
	}
	return s.saveSessionWithEvent(ctx, session, "session.worktree_cleanup_completed", worktreeUpdatePayload(session, map[string]any{
		"attempts":       session.WorktreeCleanup.Attempts,
		"worktreeBranch": session.WorktreeBranch,
	}))
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
	return s.saveSessionWithEvent(ctx, session, "session.worktree_cleanup_failed", worktreeUpdatePayload(session, map[string]any{
		"attempts":       session.WorktreeCleanup.Attempts,
		"code":           code,
		"error":          message,
		"retryable":      retryable,
		"nextAttemptAt":  nextAt,
		"worktreeBranch": session.WorktreeBranch,
	}))
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
	recovered := len(systemAdvanceSessions)
	for _, interrupted := range sessions {
		if systemAdvanceSessions[interrupted.ID] {
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
	default:
		return false
	}
}

func (s *Service) persistStoppedAfterRestart(ctx context.Context, session domain.Session, run *processdomain.Run) error {
	expected := session
	previousStatus := session.Status
	if err := transitionSession(&session, domain.StatusStopped, s.now()); err != nil {
		return err
	}
	return s.saveInterruptedRecoveryState(ctx, expected, session, run, true, "session.stopped", map[string]any{
		"reason":         "service_restarted_while_stopping",
		"cause":          "stop_requested",
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
	options.sessionID = advance.SessionID
	options.nodeRunID = workflowNodeRunID(advance.NodeRunID)
	if options.prompt == "" {
		options.prompt = advance.Prompt
	}
	return options, nil
}

func (s *Service) recoverInternalWaitingUserSession(ctx context.Context, session domain.Session, run *processdomain.Run) error {
	request, ok, err := s.latestQuestionRequest(ctx, session.ID)
	if err != nil {
		return err
	}
	if !ok {
		return s.persistInterruptedRecoveryFailureWithRun(ctx, session, run, "questions_recovery_missing", "interrupted waiting-user session has no recoverable question request")
	}
	if !isInternalQuestionRequest(request) {
		if s.questions != nil {
			if err := s.questions.CancelPendingRequestsBySession(ctx, questiondomain.SessionID(session.ID), "codex app-server restarted"); err != nil {
				return err
			}
		}
		return s.persistInterruptedRecoveryFailureWithRun(ctx, session, run, "app_server_restarted_during_questions", "the active questions call ended when Codex app-server restarted")
	}
	if request.Status == questiondomain.RequestPending {
		if run == nil && session.Status == domain.StatusWaitingUser {
			return nil
		}
		expected := session
		if err := transitionSession(&session, domain.StatusWaitingUser, s.now()); err != nil {
			return err
		}
		return s.saveInterruptedRecoveryState(ctx, expected, session, run, true, "session.recovery_waiting_user", map[string]any{
			"requestId": string(request.ID),
			"reason":    "service_restarted",
		})
	}
	if request.Status != questiondomain.RequestAnswered {
		return s.persistInterruptedRecoveryFailureWithRun(ctx, session, run, "questions_recovery_missing", "interrupted question request is no longer recoverable")
	}
	if run != nil {
		if err := s.settleInterruptedRun(ctx, session, *run); err != nil {
			return err
		}
	}
	action, metadata, _, err := mergeFailureDecision(questionRequestDTO(request))
	if err != nil {
		return err
	}
	return s.applyMergeFailureDecision(ctx, session, request.ID, action, metadata)
}

func (s *Service) latestQuestionRequest(ctx context.Context, sessionID domain.ID) (questiondomain.Request, bool, error) {
	if s.uow == nil {
		return questiondomain.Request{}, false, errors.New("unit of work is required for interrupted question recovery")
	}
	var request questiondomain.Request
	var found bool
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo := tx.Questions()
		var err error
		request, found, err = repo.FindLatestRequestBySession(ctx, questiondomain.SessionID(sessionID))
		return err
	})
	if err != nil {
		return questiondomain.Request{}, false, fmt.Errorf("find latest question request: %w", err)
	}
	return request, found, nil
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
	events := make([]eventdomain.DomainEvent, 0, 3)
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
	var err error
	events, err = s.addStatusUpdateEvent(session, events)
	if err != nil {
		return err
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

func isInternalQuestionRequest(request questiondomain.Request) bool {
	for _, question := range request.Questions {
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
			"reason":    "workflow_process_failed",
			"sessionId": string(advance.SessionID),
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
		"sessionId":         string(advance.SessionID),
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
		"sessionId":        string(advance.SessionID),
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
	sessionID := strings.TrimSpace(stringFromMap(payload, "sessionId"))
	nodeRunID := strings.TrimSpace(stringFromMap(payload, "nodeRunId"))
	if sessionID == "" || nodeRunID == "" {
		return domain.WorkflowAdvance{}, errors.New("pending workflow system advance is missing workflow or node run id")
	}
	parsedNodeRunID := domain.NodeRunID(nodeRunID)
	advance := domain.WorkflowAdvance{
		SessionID:        domain.ID(sessionID),
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
	fastMode := input.Config.FastMode != nil && *input.Config.FastMode
	config := configFromInput(input.Config, fastMode)
	mentions, err := normalizePromptMentions(input.Mentions)
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
			Mentions:     mentions,
			Mode:         mode,
			Status:       domain.StatusInitializing,
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

		if _, err := s.archiveStagedAttachments(ctx, id, domain.AttachmentSourceRequirement, string(id), stagedAttachments); err != nil {
			return DTO{}, s.failCreatedSessionWithCleanup(ctx, session, err, "attachment_archive_failed")
		}
		return toDTO(session), nil
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
		if err != nil {
			return DTO{}, err
		}
		if s.initializationScheduler != nil {
			s.initializationScheduler(s, id, false)
			return dto, nil
		}
		return s.initializeSession(ctx, id, false)
	}
	return DTO{}, fmt.Errorf("create session: exhausted %d session id attempts", maxSessionIDAttempts)
}

func (s *Service) RecoverInitializingSessions(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	repo, ok := s.repo.(domain.InitializationRepository)
	if !ok {
		return 0, errors.New("session initialization repository is required")
	}
	sessions, err := repo.ListInitializing(ctx)
	if err != nil {
		return 0, err
	}
	for _, session := range sessions {
		if s.initializationScheduler != nil {
			s.initializationScheduler(s, session.ID, true)
			continue
		}
		if _, err := s.initializeSession(ctx, session.ID, true); err != nil {
			return 0, err
		}
	}
	return len(sessions), nil
}

func (s *Service) initializeSession(ctx context.Context, id domain.ID, recovery bool) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		session, err := s.repo.Find(ctx, id)
		if err != nil {
			return fmt.Errorf("find initializing session: %w", err)
		}
		if session.Status != domain.StatusInitializing {
			dto = toDTO(session)
			return nil
		}
		project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
		if err != nil {
			return s.failSessionInitialization(ctx, session, fmt.Errorf("find initialization project: %w", err), "project_lookup_failed")
		}
		if project.IsGit {
			if err := s.provisionSessionWorktree(ctx, &session, project, recovery); err != nil {
				return s.failSessionInitialization(ctx, session, err, "worktree_initialization_failed")
			}
			if strings.TrimSpace(project.WorktreeInitCommand) != "" {
				if err := s.initializeWorktree(ctx, session, project.WorktreeInitCommand); err != nil {
					return s.failSessionInitialization(ctx, session, err, "worktree_init_command_failed")
				}
			}
			switch session.WorktreeCleanup.Status {
			case domain.WorktreeCleanupProvisioning:
				if err := session.ActivateWorktree(s.now()); err != nil {
					return s.failSessionInitialization(ctx, session, err, "worktree_activation_failed")
				}
			case domain.WorktreeCleanupActive:
			default:
				return s.failSessionInitialization(ctx, session, fmt.Errorf("worktree is not provisioned: %s", session.WorktreeCleanup.Status), "worktree_state_invalid")
			}
			if err := s.repo.Save(ctx, session); err != nil {
				return s.failSessionInitialization(ctx, session, fmt.Errorf("save initialized session worktree: %w", err), "worktree_active_save_failed")
			}
		}
		if session.Mode == domain.ModeWorkflow {
			if project.DefaultWorkflowID == nil || strings.TrimSpace(string(*project.DefaultWorkflowID)) == "" {
				return s.failSessionInitialization(ctx, session, errors.New("project default workflow is required for workflow mode"), "workflow_missing")
			}
			dto, err = s.startWorkflowSession(ctx, session, domain.WorkflowDefinitionID(*project.DefaultWorkflowID), true, "")
		} else {
			dto, err = s.enqueueCodex(ctx, session, codexStartOptions{initialStart: true}, queuePriorityForSession(session))
		}
		if err != nil {
			return s.failSessionInitialization(ctx, session, err, "session_queue_failed")
		}
		return nil
	})
	return dto, err
}

func (s *Service) provisionSessionWorktree(ctx context.Context, session *domain.Session, project projectdomain.Project, recovery bool) error {
	worktreePath := s.worktrees.PathForSession(session.ProjectID, session.ID)
	if worktreePath == "" || worktreePath != session.WorktreePath {
		return apperror.New(apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "persisted worktree path does not match managed path")
	}
	useExisting := false
	if recovery {
		ownership, err := s.worktrees.InspectOwnership(ctx, project.Path.Value, session.WorktreePath, session.WorktreeBranch, session.WorktreeCleanup.OwnershipToken)
		if err != nil {
			return apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "inspect initializing session worktree failed")
		}
		useExisting = ownership.PathExists && ownership.Matches
		noWorktree := !ownership.PathExists && !ownership.BranchExists && !ownership.Registered && !ownership.MarkerExists
		recoverableClaim := ownership.TokenMatches && ownership.MarkerExists && !ownership.Registered
		if !useExisting && recoverableClaim {
			if ownership.PathExists {
				if err := s.worktrees.Remove(ctx, session.WorktreePath); err != nil {
					return apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "remove incomplete initializing session worktree failed")
				}
			}
			if ownership.BranchExists {
				if err := s.worktrees.DeleteBranch(ctx, project.Path.Value, session.WorktreeBranch); err != nil {
					return apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "delete incomplete initializing session branch failed")
				}
			}
			if err := s.worktrees.ReleaseOwnership(ctx, session.WorktreePath, session.WorktreeCleanup.OwnershipToken); err != nil {
				return apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "release incomplete initializing session ownership failed")
			}
			noWorktree = true
		}
		if !useExisting && !noWorktree {
			return apperror.New(apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "initializing session worktree ownership is incomplete or changed")
		}
	}
	if !useExisting {
		createdPath, err := s.worktrees.Create(ctx, project.Path.Value, session.ProjectID, session.ID, session.WorktreeBranch, session.BaseBranch, session.WorktreeCleanup.OwnershipToken)
		if err != nil {
			return apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "create session worktree failed").WithRetryable(true)
		}
		if createdPath != worktreePath {
			return apperror.New(apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "created worktree path does not match persisted ownership")
		}
	}
	baseCommit, err := s.worktrees.HeadCommit(ctx, worktreePath, "")
	if err != nil {
		return apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "read session worktree base commit failed").WithRetryable(true)
	}
	session.WorktreeBaseCommit = baseCommit
	return nil
}

func (s *Service) failSessionInitialization(ctx context.Context, session domain.Session, cause error, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if session.Status == domain.StatusInitializing {
		if err := transitionSession(&session, domain.StatusFailed, s.now()); err != nil {
			return errors.Join(cause, err)
		}
	}
	session.InitializationErrorCode = reason
	session.InitializationError = cause.Error()
	if err := s.saveSessionWithStatusUpdate(ctx, session, "session.failed", map[string]any{
		"reason": reason,
		"error":  cause.Error(),
	}); err != nil {
		return errors.Join(cause, err)
	}
	return cause
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
	if runErr != nil {
		return fmt.Errorf("run worktree init command: %w", runErr)
	}
	if result.ExitCode != nil {
		return fmt.Errorf("worktree init command exited with code %d", *result.ExitCode)
	}
	return errors.New("worktree init command failed")
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
	payload := map[string]any{
		"reason":         reason,
		"worktreeBranch": session.WorktreeBranch,
	}
	if strings.TrimSpace(session.BaseBranch) != "" {
		eventType = "session.worktree_cleanup_requested"
		payload = worktreeUpdatePayload(session, payload)
	}
	if err := s.saveSessionWithStatusUpdate(cleanupCtx, session, eventType, payload); err != nil {
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
		if err := s.settleInterruptedRunBeforeRecoveryAction(ctx, session); err != nil {
			return DTO{}, err
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
			SessionID:        start.SessionID,
			NodeRunID:        start.NodeRunID,
			CurrentNodeID:    start.CurrentNodeID,
			CurrentNodeTitle: start.CurrentNodeTitle,
			Status:           start.Status,
			Merge:            start.Merge,
		})
	}
	if start.Expr != nil {
		return s.executeWorkflowExpr(ctx, session, domain.WorkflowAdvance{
			SessionID:        start.SessionID,
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
		if err := s.saveSessionWithStatusUpdate(ctx, session, "session.waiting_approval", map[string]any{
			"sessionId":        string(start.SessionID),
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
		sessionID:           start.SessionID,
		nodeRunID:           workflowNodeRunID(start.NodeRunID),
		prompt:              start.Prompt,
		queueKind:           queueKind,
		workflowResultRetry: start.RequireResultRetry,
		initialStart:        initialStart,
	}, queuePriorityForSession(session))
	if err != nil {
		return s.handleWorkflowNodeFailure(ctx, session, start.SessionID, start.NodeRunID, "codex_start_failed", err.Error())
	}
	return dto, nil
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
		"priority":  session.Priority,
		"updatedAt": session.UpdatedAt,
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
		"config":    session.Config,
		"updatedAt": session.UpdatedAt,
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
		cleanupCtx, cancel := processCleanupContext(ctx)
		defer cancel()
		dto, err = s.waitForStopCompletion(cleanupCtx, id)
	}
	if err == nil && (dto.Status == domain.StatusStopped || dto.Status == domain.StatusClosed) && s.tunnels != nil {
		cleanupCtx, cancel := processCleanupContext(ctx)
		defer cancel()
		if cleanupErr := s.tunnels.CloseTunnelsForSession(cleanupCtx, id); cleanupErr != nil {
			err = fmt.Errorf("close session tunnels: %w", cleanupErr)
		}
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
			cleanupCtx, cancel := processCleanupContext(ctx)
			defer cancel()
			if err := s.cancelPendingQuestions(cleanupCtx, session.ID, "session stopped"); err != nil {
				return DTO{}, err
			}
			return toDTO(session), nil
		}
		if session.Status == domain.StatusQueued {
			return s.stopSessionWithoutActiveProcess(ctx, session, "queue_cancelled", false)
		}
		return DTO{}, apperror.Wrap(ErrProcessLifecycleNotWired, apperror.CodeCodexStartFailed, apperror.CategoryInfraError, "session process lifecycle is not wired")
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(id))
	if err != nil {
		return DTO{}, fmt.Errorf("find active process run: %w", err)
	}
	if session.Status == domain.StatusStopped && !ok {
		cleanupCtx, cancel := processCleanupContext(ctx)
		defer cancel()
		if err := s.cancelPendingQuestions(cleanupCtx, session.ID, "session stopped"); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	}
	if session.Status == domain.StatusQueued && !ok {
		return s.stopSessionWithoutActiveProcess(ctx, session, "queue_cancelled", false)
	}
	if !ok {
		switch session.Status {
		case domain.StatusQueued, domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping, domain.StatusResumeFailed:
		default:
			return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot stop from current status").WithDetails(map[string]any{"status": string(session.Status)})
		}
		cleanupCtx, cancel := processCleanupContext(ctx)
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
	cleanupCtx, cancel := processCleanupContext(ctx)
	defer cancel()
	stopErr := s.codex.Stop(cleanupCtx, active.ID)
	processMissing := errors.Is(stopErr, processdomain.ErrProcessNotFound)
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
			"cause":        "stop_requested",
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
	if err := s.saveSessionWithStatusUpdate(ctx, session, "session.stopped", map[string]any{"reason": reason, "cause": "stop_requested"}); err != nil {
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

func processCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
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
		if err := s.settleInterruptedRunBeforeRecoveryAction(ctx, session); err != nil {
			return DTO{}, err
		}
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
		options.sessionID = advance.SessionID
		options.nodeRunID = workflowNodeRunID(advance.NodeRunID)
		if options.prompt == "" || options.queueKind == domain.QueueKindPromptAppend {
			options.prompt = advance.Prompt
		}
	}
	if startOptions.Force {
		return s.startCodex(ctx, session, options, true)
	}
	return s.enqueueCodex(ctx, session, options, queuePriorityForSession(session))
}

func (s *Service) settleInterruptedRunBeforeRecoveryAction(ctx context.Context, session domain.Session) error {
	if s.processes == nil {
		return nil
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
	if err != nil {
		return fmt.Errorf("find interrupted process before recovery action: %w", err)
	}
	if !ok {
		return nil
	}
	return s.settleInterruptedRun(ctx, session, active)
}

func (s *Service) RequestQuestions(ctx context.Context, input RequestQuestionsInput) (questionapp.RequestDTO, error) {
	if s == nil {
		return questionapp.RequestDTO{}, errors.New("session usecase: nil service")
	}
	if input.RequestID == "" || input.SessionID == "" || len(input.Questions) == 0 {
		return questionapp.RequestDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "request id, session id, and questions are required")
	}
	var request questionapp.RequestDTO
	err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
		var err error
		request, _, err = s.requestQuestions(ctx, input)
		return err
	})
	if err != nil {
		return questionapp.RequestDTO{}, err
	}
	return s.waitForUserAnswer(ctx, request)
}

func (s *Service) waitForUserAnswer(ctx context.Context, request questionapp.RequestDTO) (questionapp.RequestDTO, error) {
	questions, ok := s.questions.(questionRequestCoordinator)
	if !ok {
		return questionapp.RequestDTO{}, errors.New("question request coordinator is required")
	}
	updates, err := questions.QuestionRequestUpdates(ctx, request.SessionID)
	if err != nil {
		return questionapp.RequestDTO{}, err
	}
	current, err := questions.GetRequest(ctx, request.ID)
	if err != nil {
		return questionapp.RequestDTO{}, err
	}
	if current.Status != questiondomain.RequestPending {
		return current, nil
	}
	for {
		select {
		case update, open := <-updates:
			if !open {
				if err := ctx.Err(); err != nil {
					return questionapp.RequestDTO{}, err
				}
				return questionapp.RequestDTO{}, errors.New("question updates closed")
			}
			if update.ID == request.ID && update.Status != questiondomain.RequestPending {
				return update, nil
			}
		case <-ctx.Done():
			return questionapp.RequestDTO{}, ctx.Err()
		}
	}
}

func (s *Service) requestQuestions(ctx context.Context, input RequestQuestionsInput) (questionapp.RequestDTO, processdomain.Run, error) {
	if s.uow == nil || s.processes == nil || s.codex == nil || s.questions == nil {
		return questionapp.RequestDTO{}, processdomain.Run{}, errors.New("questions lifecycle is not wired")
	}
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return questionapp.RequestDTO{}, processdomain.Run{}, fmt.Errorf("find session: %w", err)
	}
	if session.Status != domain.StatusStarting && session.Status != domain.StatusRunning {
		return questionapp.RequestDTO{}, processdomain.Run{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot request user input from current status").WithDetails(map[string]any{"status": string(session.Status)})
	}
	active, err := s.activeProcessWithCodexSession(ctx, session.ID)
	if err != nil {
		return questionapp.RequestDTO{}, processdomain.Run{}, err
	}
	requestID := input.RequestID
	if requestID == "" {
		return questionapp.RequestDTO{}, processdomain.Run{}, errors.New("question request id is required")
	}
	questions := make([]questiondomain.Question, len(input.Questions))
	for i, item := range input.Questions {
		questions[i] = item
		questions[i].ID = questiondomain.QuestionID(fmt.Sprintf("%s:%d", requestID, i))
		questions[i].RequestID = requestID
	}
	now := s.now()
	originID := questiondomain.ProcessRunID(active.ID)
	request := questiondomain.Request{
		ID:                 requestID,
		SessionID:          questiondomain.SessionID(session.ID),
		OriginProcessRunID: &originID,
		Status:             questiondomain.RequestPending,
		Questions:          questions,
		CreatedAt:          now,
	}
	if session.Mode == domain.ModeWorkflow && active.NodeRunID == nil {
		return questionapp.RequestDTO{}, processdomain.Run{}, errors.New("workflow questions origin is missing node run id")
	}
	if err := transitionSession(&session, domain.StatusWaitingUser, now); err != nil {
		return questionapp.RequestDTO{}, processdomain.Run{}, err
	}
	events, err := s.newSessionEvents(session, statusUpdateInputs(
		sessionEventInput{eventType: "question.pending", payload: map[string]any{"requestId": string(request.ID), "processRunId": string(active.ID)}},
		sessionEventInput{eventType: "session.waiting_user", payload: map[string]any{"requestId": string(request.ID), "processRunId": string(active.ID)}},
	))
	if err != nil {
		return questionapp.RequestDTO{}, processdomain.Run{}, err
	}
	if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		if session.Mode == domain.ModeWorkflow {
			if _, err := tx.Workflows().FindRun(ctx, workflowdomain.SessionID(session.ID)); err != nil {
				return err
			}
		}
		pending, err := tx.Questions().ListPendingRequestsBySession(ctx, questiondomain.SessionID(session.ID))
		if err != nil {
			return err
		}
		for _, existing := range pending {
			if !isInternalQuestionRequest(existing) {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session already has a pending agent question")
			}
		}
		if err := tx.Questions().CreateRequest(ctx, request); err != nil {
			return err
		}
		if err := tx.Processes().MarkWaitingUser(ctx, active.ID); err != nil {
			return err
		}
		if err := tx.Sessions().Save(ctx, session); err != nil {
			return err
		}
		if session.Mode == domain.ModeWorkflow {
			repo, ok := tx.Workflows().(workflowdomain.NodeExecutionRepository)
			if !ok {
				return errors.New("workflow node execution repository is required")
			}
			if err := repo.MarkNodeWaitingUser(ctx, workflowdomain.SessionID(session.ID), workflowdomain.NodeRunID(*active.NodeRunID)); err != nil {
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
		return questionapp.RequestDTO{}, processdomain.Run{}, err
	}
	dto := questionRequestDTO(request)
	s.publishQuestionRequest(dto)
	for _, event := range events {
		s.publishSessionEvent(ctx, event)
	}
	return dto, active, nil
}

func (s *Service) SubmitQuestionRequest(ctx context.Context, input questionapp.SubmitRequestInput) (questionapp.RequestDTO, error) {
	if s == nil || s.questions == nil {
		return questionapp.RequestDTO{}, errors.New("question lifecycle is not wired")
	}
	questions, ok := s.questions.(questionRequestCoordinator)
	if !ok {
		return questionapp.RequestDTO{}, errors.New("question request coordinator is required")
	}
	existing, err := questions.GetRequest(ctx, input.RequestID)
	if err != nil {
		return questionapp.RequestDTO{}, err
	}
	if existing.OriginProcessRunID == nil {
		request, err := questions.SubmitRequest(ctx, input)
		if err != nil {
			return questionapp.RequestDTO{}, err
		}
		if err := s.HandleQuestionRequestAnswered(ctx, request); err != nil {
			return questionapp.RequestDTO{}, err
		}
		return request, nil
	}
	var request questionapp.RequestDTO
	err = s.withSessionLock(ctx, domain.ID(existing.SessionID), func(ctx context.Context) error {
		var err error
		request, err = s.submitAgentQuestionRequest(ctx, input)
		if err == nil {
			s.publishQuestionRequest(request)
		}
		return err
	})
	if err != nil {
		return questionapp.RequestDTO{}, err
	}
	s.scheduleQueueDrain()
	return request, nil
}

func (s *Service) submitAgentQuestionRequest(ctx context.Context, input questionapp.SubmitRequestInput) (questionapp.RequestDTO, error) {
	if s.uow == nil {
		return questionapp.RequestDTO{}, errors.New("questions lifecycle requires a unit of work")
	}
	var result questiondomain.Request
	var publishedEvents []eventdomain.DomainEvent
	err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo := tx.Questions()
		request, err := repo.FindRequest(ctx, input.RequestID)
		if err != nil {
			return err
		}
		if request.OriginProcessRunID == nil {
			return errors.New("question request has no origin process run")
		}
		if request.Status == questiondomain.RequestAnswered {
			if !questionAnswersMatch(request, input.Answers) {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers do not match existing answers")
			}
			result = request
			return nil
		}
		if err := (questiondomain.DefaultPolicy{}).CanSubmit(request, input.Answers); err != nil {
			return apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers are invalid")
		}
		persisted, transitioned, err := repo.SubmitAnswers(ctx, input.RequestID, input.Answers)
		if err != nil {
			return err
		}
		if !transitioned {
			if persisted.Status != questiondomain.RequestAnswered || !questionAnswersMatch(persisted, input.Answers) {
				return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question request is no longer pending")
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
		if origin.Status != processdomain.StatusWaitingUser {
			return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "questions turn is no longer waiting")
		}
		if err := tx.Processes().MarkRunning(ctx, origin.ID, origin.CodexSessionID); err != nil {
			return err
		}
		session, err := tx.Sessions().Find(ctx, domain.ID(persisted.SessionID))
		if err != nil {
			return err
		}
		if err := transitionSession(&session, domain.StatusRunning, s.now()); err != nil {
			return err
		}
		if err := tx.Sessions().Save(ctx, session); err != nil {
			return err
		}
		if session.Mode == domain.ModeWorkflow && origin.NodeRunID != nil {
			nodes, ok := tx.Workflows().(workflowdomain.NodeExecutionRepository)
			if !ok {
				return errors.New("workflow node execution repository is required")
			}
			if err := nodes.MarkNodeRunning(ctx, workflowdomain.SessionID(session.ID), workflowdomain.NodeRunID(*origin.NodeRunID), workflowdomain.ProcessRunID(origin.ID)); err != nil {
				return err
			}
		}
		publishedEvents, err = s.newSessionEvents(session, statusUpdateInputs(
			sessionEventInput{eventType: "question.answered", payload: map[string]any{"requestId": string(persisted.ID), "processRunId": string(origin.ID)}},
			sessionEventInput{eventType: "session.running", payload: map[string]any{"processRunId": string(origin.ID), "reason": "questions_answered"}},
		))
		if err != nil {
			return err
		}
		for _, event := range publishedEvents {
			if err := tx.Events().Append(ctx, event); err != nil {
				return err
			}
		}
		result = persisted
		return nil
	})
	if err != nil {
		return questionapp.RequestDTO{}, err
	}
	for _, event := range publishedEvents {
		s.publishSessionEvent(ctx, event)
	}
	return questionRequestDTO(result), nil
}

type codexStartOptions struct {
	resumeCodexSessionID    string
	resumeOfProcessRunID    processdomain.RunID
	sessionID               domain.ID
	nodeRunID               *processdomain.NodeRunID
	prompt                  string
	fallbackPrompt          string
	promptAppendIDs         []string
	promptFiles             []domain.SessionFile
	promptMentions          []domain.PromptMention
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
			return processdomain.Run{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "questions requires an active Codex process")
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

func questionRequestDTO(request questiondomain.Request) questionapp.RequestDTO {
	return questionapp.RequestDTO{
		ID:                 request.ID,
		SessionID:          request.SessionID,
		OriginProcessRunID: request.OriginProcessRunID,
		Status:             request.Status,
		Questions:          append([]questiondomain.Question(nil), request.Questions...),
	}
}

func (s *Service) publishQuestionRequest(request questionapp.RequestDTO) {
	if questions, ok := s.questions.(questionRequestCoordinator); ok {
		questions.PublishRequest(request)
	}
}

func questionAnswersMatch(request questiondomain.Request, answers []questiondomain.Answer) bool {
	if len(request.Questions) != len(answers) {
		return false
	}
	byQuestion := make(map[questiondomain.QuestionID]questiondomain.Answer, len(answers))
	for _, answer := range answers {
		if _, exists := byQuestion[answer.QuestionID]; exists {
			return false
		}
		byQuestion[answer.QuestionID] = answer
	}
	for _, question := range request.Questions {
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
	if errors.Is(err, errNoEffectivePromptAppend) {
		s.releaseWorkdir(workdir, session.ID)
		return s.settleEmptyPromptAppendQueue(ctx, session)
	}
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
	if err := transitionSession(&session, domain.StatusRunning, s.now()); err != nil {
		return DTO{}, err
	}
	session.CodexSessionID = handle.CodexSessionID
	session.UpdatedAt = s.now()
	if err := s.markProcessRunningWithSessionEvent(ctx, runID, handle.CodexSessionID, session, "session.running", map[string]any{
		"processRunId": string(runID), "codexSessionId": handle.CodexSessionID, "turnId": handle.TurnID,
	}); err != nil {
		cleanupErr := s.cleanupStartedCodexAfterPersistenceFailure(ctx, session.ID, handle, options, err)
		return DTO{}, errors.Join(err, cleanupErr)
	}
	s.consumeCodexEvents(handle, session, options, workdir)
	return toDTO(session), nil
}

func (s *Service) cleanupStartedCodexAfterPersistenceFailure(ctx context.Context, sessionID domain.ID, handle processdomain.CodexHandle, options codexStartOptions, persistenceErr error) error {
	cleanupCtx, cancel := processCleanupContext(ctx)
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
		"sessionId":     string(run.SessionID),
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
	events := []eventdomain.DomainEvent{}
	if hasEvent {
		events = append(events, event)
	}
	events, err = s.addStatusUpdateEvent(queued, events)
	if err != nil {
		return domain.Session{}, err
	}
	if err := s.saveSessionAndEvents(ctx, queued, events); err != nil {
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
		NodeRunID:               queueNodeRunID(options.nodeRunID),
		Prompt:                  strings.TrimSpace(options.prompt),
		ResumeCodexSessionID:    options.resumeCodexSessionID,
		ResumeOfProcessRunID:    string(options.resumeOfProcessRunID),
	}, now); err != nil {
		return domain.Session{}, eventdomain.DomainEvent{}, false, fmt.Errorf("queue session %s: %w", session.ID, err)
	}
	event, hasEvent, err := s.newSessionEvent(session, "session.queued", map[string]any{
		"priority":            string(session.Queue.Priority),
		"sessionPriority":     string(normalizePriority(session.Priority)),
		"queueKind":           string(session.Queue.Kind),
		"sessionId":           string(session.ID),
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
				if current.Mode == domain.ModeWorkflow && current.Queue.NodeRunID != nil {
					if _, failErr := s.handleWorkflowNodeFailure(ctx, saved, current.ID, current.Queue.NodeRunID, "codex_start_failed", err.Error()); failErr != nil {
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
	if options.queueKind != "" {
		return options.queueKind
	}
	if options.resumeCodexSessionID != "" {
		return domain.QueueKindResume
	}
	return domain.QueueKindStart
}

func queuePriorityForStartOptions(session domain.Session, options codexStartOptions) domain.QueuePriority {
	return queuePriorityForSession(session)
}

func codexStartOptionsFromQueue(session domain.Session) codexStartOptions {
	nodeRunID := queueProcessNodeRunID(session.Queue.NodeRunID)
	return codexStartOptions{
		resumeCodexSessionID:    session.Queue.ResumeCodexSessionID,
		resumeOfProcessRunID:    processdomain.RunID(session.Queue.ResumeOfProcessRunID),
		sessionID:               session.ID,
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
	prompt := strings.TrimSpace(options.prompt)
	prompt, action, actionArgument := codexAction(prompt)
	artifactDir := ""
	if s.artifacts != nil {
		var err error
		artifactDir, err = s.artifacts.EnsureArtifactDir(ctx, session.ID)
		if err != nil {
			return processdomain.CodexHandle{}, fmt.Errorf("prepare artifact directory: %w", err)
		}
	}
	if options.resumeCodexSessionID != "" {
		return s.codex.Resume(ctx, processdomain.CodexResumeInput{
			ProcessRunID:          runID,
			SessionID:             processdomain.SessionID(session.ID),
			CodexSessionID:        options.resumeCodexSessionID,
			Workdir:               workdir,
			ArtifactDir:           artifactDir,
			Input:                 codexInput(prompt, options.promptFiles, options.promptMentions),
			Action:                action,
			ActionArgument:        actionArgument,
			DeveloperInstructions: anyCodeDeveloperInstructions(session, artifactDir),
			Model:                 strings.TrimSpace(session.Config.CodexModel),
			ReasoningEffort:       strings.TrimSpace(session.Config.ReasoningEffort),
			PermissionMode:        strings.TrimSpace(session.Config.PermissionMode),
			FastMode:              session.Config.FastMode,
		})
	}
	files, err := s.listSessionAttachments(ctx, session.ID)
	if err != nil {
		return processdomain.CodexHandle{}, err
	}
	files = appendUniqueSessionFiles(files, options.promptFiles...)
	mentions := appendUniquePromptMentions(append([]domain.PromptMention(nil), session.Mentions...), options.promptMentions...)
	return s.codex.Start(ctx, newCodexStartInput(session, runID, workdir, artifactDir, prompt, files, mentions, action, actionArgument))
}

func newCodexStartInput(session domain.Session, runID processdomain.RunID, workdir string, artifactDir string, prompt string, files []domain.SessionFile, mentions []domain.PromptMention, action processdomain.CodexAction, actionArgument string) processdomain.CodexStartInput {
	if action != processdomain.CodexActionTurn && action != processdomain.CodexActionPlan {
		prompt = ""
	}
	return processdomain.CodexStartInput{
		ProcessRunID:          runID,
		SessionID:             processdomain.SessionID(session.ID),
		Workdir:               workdir,
		ArtifactDir:           artifactDir,
		Input:                 codexInput(prompt, files, mentions),
		Action:                action,
		ActionArgument:        actionArgument,
		DeveloperInstructions: anyCodeDeveloperInstructions(session, artifactDir),
		Model:                 strings.TrimSpace(session.Config.CodexModel),
		ReasoningEffort:       strings.TrimSpace(session.Config.ReasoningEffort),
		PermissionMode:        strings.TrimSpace(session.Config.PermissionMode),
		FastMode:              session.Config.FastMode,
	}
}

func codexInput(prompt string, files []domain.SessionFile, mentions []domain.PromptMention) []processdomain.CodexInputItem {
	input := make([]processdomain.CodexInputItem, 0, 1+len(files)+len(mentions))
	if prompt != "" {
		input = append(input, processdomain.CodexInputItem{Type: "text", Text: prompt})
	}
	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		switch {
		case strings.HasPrefix(strings.ToLower(file.MimeType), "image/"):
			input = append(input, processdomain.CodexInputItem{Type: "localImage", Path: path})
		case strings.HasPrefix(strings.ToLower(file.MimeType), "audio/"):
			input = append(input, processdomain.CodexInputItem{Type: "localAudio", Path: path})
		default:
			name := strings.TrimSpace(file.LogicalPath)
			if name == "" {
				name = strings.TrimSpace(file.Filename)
			}
			if name == "" {
				name = filepath.Base(path)
			}
			input = append(input, processdomain.CodexInputItem{Type: "mention", Path: path, Name: name})
		}
	}
	for _, mention := range mentions {
		path := strings.TrimSpace(mention.Path)
		if path == "" {
			continue
		}
		input = append(input, processdomain.CodexInputItem{Type: "mention", Path: path, Name: path})
	}
	return input
}

func codexAction(prompt string) (string, processdomain.CodexAction, string) {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "/compact" {
		return "", processdomain.CodexActionCompact, ""
	}
	for _, command := range []struct {
		name   string
		action processdomain.CodexAction
	}{
		{name: "/review", action: processdomain.CodexActionReview},
		{name: "/goal", action: processdomain.CodexActionGoal},
		{name: "/plan", action: processdomain.CodexActionPlan},
	} {
		if trimmed == command.name {
			return "", command.action, ""
		}
		if strings.HasPrefix(trimmed, command.name) && len(trimmed) > len(command.name) {
			rest := trimmed[len(command.name):]
			if len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r') {
				argument := strings.TrimSpace(rest)
				if command.action == processdomain.CodexActionPlan {
					return argument, command.action, ""
				}
				return "", command.action, argument
			}
		}
	}
	return trimmed, processdomain.CodexActionTurn, ""
}

func appendUniqueSessionFiles(files []domain.SessionFile, additions ...domain.SessionFile) []domain.SessionFile {
	seen := make(map[string]struct{}, len(files)+len(additions))
	for _, file := range files {
		if file.Path != "" {
			seen[file.Path] = struct{}{}
		}
	}
	for _, file := range additions {
		if file.Path == "" {
			continue
		}
		if _, ok := seen[file.Path]; ok {
			continue
		}
		seen[file.Path] = struct{}{}
		files = append(files, file)
	}
	return files
}

func appendUniquePromptMentions(mentions []domain.PromptMention, additions ...domain.PromptMention) []domain.PromptMention {
	seen := make(map[string]struct{}, len(mentions)+len(additions))
	for _, mention := range mentions {
		seen[mention.Path] = struct{}{}
	}
	for _, mention := range additions {
		if mention.Path == "" {
			continue
		}
		if _, ok := seen[mention.Path]; ok {
			continue
		}
		seen[mention.Path] = struct{}{}
		mentions = append(mentions, mention)
	}
	return mentions
}

func normalizePromptMentions(mentions []domain.PromptMention) ([]domain.PromptMention, error) {
	normalized := make([]domain.PromptMention, 0, len(mentions))
	seen := make(map[string]struct{}, len(mentions))
	for _, mention := range mentions {
		candidate := strings.ReplaceAll(strings.TrimSpace(mention.Path), "\\", "/")
		if candidate == "" || strings.ContainsRune(candidate, '\x00') || strings.HasPrefix(candidate, "/") || filepath.IsAbs(candidate) {
			return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "mention path must be project-relative")
		}
		candidate = pathpkg.Clean(candidate)
		if candidate == "." || candidate == ".." || strings.HasPrefix(candidate, "../") {
			return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "mention path escapes project")
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		normalized = append(normalized, domain.PromptMention{Path: candidate})
	}
	return normalized, nil
}

var errNoEffectivePromptAppend = errors.New("no effective prompt append")

func (s *Service) settleEmptyPromptAppendQueue(ctx context.Context, session domain.Session) (DTO, error) {
	if session.Status != domain.StatusQueued || session.Queue.Kind != domain.QueueKindPromptAppend {
		return toDTO(session), nil
	}
	if err := transitionSession(&session, domain.StatusStopped, s.now()); err != nil {
		return DTO{}, err
	}
	if err := s.saveSessionWithStatusUpdate(ctx, session, "session.prompt_append_cancelled", map[string]any{"reason": "attachments_unavailable"}); err != nil {
		return DTO{}, err
	}
	s.scheduleQueueDrain()
	return toDTO(session), nil
}

func (s *Service) resolveCodexInput(ctx context.Context, session domain.Session, options codexStartOptions) (codexStartOptions, error) {
	appends, err := s.repo.ListPromptAppends(ctx, session.ID)
	if err != nil {
		return codexStartOptions{}, fmt.Errorf("list prompt appends: %w", err)
	}
	pendingPrompt, pendingIDs, promptFiles, promptMentions, cancelledIDs, err := s.pendingPromptInput(ctx, session.ID, appends)
	if err != nil {
		return codexStartOptions{}, err
	}
	for _, id := range cancelledIDs {
		if err := s.repo.DeletePromptAppend(ctx, id); err != nil {
			return codexStartOptions{}, fmt.Errorf("cancel empty prompt append %s: %w", id, err)
		}
	}
	prompt := strings.TrimSpace(options.prompt)
	basePrompt := prompt
	if options.resumeCodexSessionID != "" {
		options.fallbackPrompt = rebuiltSessionPrompt(session, basePrompt, true, appends)
	}
	if options.queueKind == domain.QueueKindPromptAppend {
		if len(pendingIDs) == 0 {
			return codexStartOptions{}, errNoEffectivePromptAppend
		}
		if options.resumeCodexSessionID != "" {
			prompt = pendingPrompt
		} else {
			prompt = rebuiltSessionPrompt(session, basePrompt, true, appends)
		}
	} else if options.reviewAfterReuseFailure {
		prompt = rebuiltSessionPrompt(session, basePrompt, true, appends)
	} else if options.resumeCodexSessionID != "" {
		if session.Mode != domain.ModeWorkflow && basePrompt == "" && len(pendingIDs) == 0 {
			basePrompt = strings.TrimSpace(session.Requirement)
		}
		prompt = joinPromptParts(basePrompt, pendingPrompt)
	} else if session.Mode != domain.ModeWorkflow {
		prompt = rebuiltSessionPrompt(session, basePrompt, options.reviewAfterReuseFailure, appends)
	} else {
		prompt = joinPromptParts(basePrompt, pendingPrompt)
	}
	if strings.TrimSpace(prompt) == "" && !options.initialStart {
		return codexStartOptions{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "Codex 执行提示不能为空")
	}
	options.prompt = prompt
	options.promptAppendIDs = pendingIDs
	options.promptFiles = promptFiles
	options.promptMentions = promptMentions
	return options, nil
}

func (s *Service) pendingPromptInput(ctx context.Context, sessionID domain.ID, appends []domain.PromptAppend) (string, []string, []domain.SessionFile, []domain.PromptMention, []string, error) {
	parts := make([]string, 0, len(appends))
	ids := make([]string, 0, len(appends))
	inputFiles := make([]domain.SessionFile, 0)
	mentions := make([]domain.PromptMention, 0)
	cancelledIDs := make([]string, 0)
	for _, promptAppend := range appends {
		if promptAppend.Status != domain.PromptAppendPending {
			continue
		}
		attachments, err := s.listPromptAppendAttachments(ctx, sessionID, promptAppend.ID)
		if err != nil {
			return "", nil, nil, nil, nil, err
		}
		artifacts, err := s.resolvePromptArtifacts(ctx, sessionID, promptAppend.ArtifactIDs, true)
		if err != nil {
			return "", nil, nil, nil, nil, err
		}
		currentFiles := append(append([]domain.SessionFile(nil), attachments...), artifacts...)
		body := strings.TrimSpace(promptAppend.Body)
		if body == "" && len(currentFiles) == 0 && len(promptAppend.Mentions) == 0 {
			cancelledIDs = append(cancelledIDs, promptAppend.ID)
			continue
		}
		parts = append(parts, body)
		inputFiles = appendUniqueSessionFiles(inputFiles, currentFiles...)
		mentions = appendUniquePromptMentions(mentions, promptAppend.Mentions...)
		ids = append(ids, promptAppend.ID)
	}
	return strings.Join(parts, "\n\n"), ids, inputFiles, mentions, cancelledIDs, nil
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
const anyCodePromptGuidance = "AnyCode 提供 `questions` App Server 动态工具，可用于向用户提出选项问题。若需求、验收标准、执行取舍或下一步不确定，请使用 `questions` 咨询用户；如果上下文足够明确，请直接继续执行，不要无意义打断用户。`request_user_input` 不是 AnyCode 会话内的用户提问工具，可能只属于外层平台或特定计划模式；即使你在说明中看到它，也不要使用 `request_user_input` 来代替 AnyCode 的 `questions`。\n\nAnyCode 卡片的 TODO List 仅来自 Codex 的结构化计划事件。处理包含多个可执行步骤的任务时，必须调用 `update_plan` 创建计划，并在步骤状态变化后持续调用 `update_plan` 更新状态；不要只在回复中输出 Markdown checklist。单步骤任务或纯问答无需创建计划。"
const managedWorktreePromptGuidance = "当前工作目录是 AnyCode 管理的卡片工作树。不得删除、移动、重建或清理当前工作树，也不得执行会移除该工作树的命令；卡片关闭时由 AnyCode 负责清理仍存在的工作树。"
const artifactPromptGuidance = "本卡片生成的图片、截图、PDF、音视频、压缩包和其他临时文件统一写入环境变量 `ANYCODE_ARTIFACT_DIR` 指向的目录。需要生图时直接使用 Codex 可用的图片生成能力，并将结果保存到该目录；不要把生成物写入项目工作树。"

func anyCodeDeveloperInstructions(session domain.Session, artifactDir string) string {
	parts := []string{anyCodePromptGuidance}
	if strings.TrimSpace(session.BaseBranch) != "" {
		parts = append(parts, managedWorktreePromptGuidance)
	}
	if strings.TrimSpace(artifactDir) != "" {
		parts = append(parts, artifactPromptGuidance)
	}
	return joinPromptParts(parts...)
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

func (s *Service) consumeCodexEvents(handle processdomain.CodexHandle, session domain.Session, options codexStartOptions, workdir string) {
	done := make(chan struct{})
	s.processConsumers.Store(handle.ProcessRunID, (<-chan struct{})(done))
	stopArtifactWatcher := s.startArtifactWatcher(session.ID, handle.ProcessRunID)
	go func() {
		defer func() {
			stopArtifactWatcher()
			s.processConsumers.Delete(handle.ProcessRunID)
			close(done)
		}()
		events, err := s.codex.Events(context.Background(), handle)
		if err != nil {
			cleanupCtx, cancel := processCleanupContext(context.Background())
			stopErr := s.codex.Stop(cleanupCtx, handle.ProcessRunID)
			cancel()
			if errors.Is(stopErr, processdomain.ErrProcessNotFound) {
				stopErr = nil
			}
			if stopErr != nil {
				log.Printf("stop codex after event stream failure: session=%s process_run=%s error=%v", session.ID, handle.ProcessRunID, stopErr)
				return
			}
			stopArtifactWatcher()
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
			event.SessionID = processdomain.SessionID(session.ID)
			event.ProcessRunID = handle.ProcessRunID
			if event.CodexSessionID == "" {
				event.CodexSessionID = handle.CodexSessionID
			}
			if result, ok := exitResultFromEvent(event); ok {
				exitResult = result
			}
			workflowResults = workflowResultsAfterEvent(workflowResults, event)
			if codexEventAcknowledgesPrompt(event) {
				options.resumeAcknowledged = true
			}
			if event.Type == processdomain.CodexEventProcessExit {
				continue
			}
			extraEvents := s.archiveCodexEventImages(context.Background(), session, handle, &event)
			s.publishCodexRuntimeEvent(event)
			if persistenceFailure != nil {
				continue
			}
			if err := s.handleCodexEventWithRetry(session.ID, handle, event, extraEvents...); err != nil {
				cleanupCtx, cancel := processCleanupContext(context.Background())
				stopErr := s.codex.Stop(cleanupCtx, handle.ProcessRunID)
				cancel()
				if errors.Is(stopErr, processdomain.ErrProcessNotFound) {
					stopErr = nil
				}
				reason := fmt.Sprintf("persist codex event: %v", err)
				if stopErr != nil {
					log.Printf("stop codex after event persistence failure: session=%s process_run=%s error=%v", session.ID, handle.ProcessRunID, stopErr)
					reason += fmt.Sprintf("; stop codex: %v", stopErr)
				}
				failure := processdomain.ExitResult{
					FailureCode:   "codex_event_persistence_failed",
					FailureReason: reason,
					FinishedAt:    s.now(),
				}
				if stopErr == nil {
					stopArtifactWatcher()
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
		stopArtifactWatcher()
		s.releaseWorkdir(workdir, session.ID)
		s.handleCodexProcessExit(session, handle, options, exitResult, workflowResults)
	}()
}

func (s *Service) startArtifactWatcher(sessionID domain.ID, runID processdomain.RunID) func() {
	if s == nil || s.artifacts == nil || s.processes == nil {
		return func() {}
	}
	parent := s.lifecycleCtx
	if parent == nil {
		parent = context.Background()
	}
	watchCtx, cancel := context.WithCancel(parent)
	active, found, err := s.processes.FindActiveBySession(watchCtx, processdomain.SessionID(sessionID))
	if err != nil || !found || active.ID != runID {
		cancel()
		if err != nil {
			log.Printf("verify active process before watching session artifacts: session=%s process=%s error=%v", sessionID, runID, err)
		}
		return func() {}
	}
	changes, err := s.artifacts.WatchArtifactDir(watchCtx, sessionID)
	if err != nil {
		cancel()
		log.Printf("watch session artifacts: session=%s process=%s error=%v", sessionID, runID, err)
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.recordArtifactDirectoryUpdate(watchCtx, sessionID, runID); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("initialize session artifacts: session=%s process=%s error=%v", sessionID, runID, err)
		}
		for range changes {
			if err := s.recordArtifactDirectoryUpdate(watchCtx, sessionID, runID); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("update session artifacts: session=%s process=%s error=%v", sessionID, runID, err)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
			flushCtx, flushCancel := context.WithTimeout(context.Background(), processCleanupTimeout)
			defer flushCancel()
			if err := s.recordArtifactDirectoryUpdate(flushCtx, sessionID, runID); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("flush session artifacts: session=%s process=%s error=%v", sessionID, runID, err)
			}
		})
	}
}

func (s *Service) recordArtifactDirectoryUpdate(ctx context.Context, sessionID domain.ID, runID processdomain.RunID) error {
	return s.withSessionLock(ctx, sessionID, func(ctx context.Context) error {
		if s.processes != nil {
			active, found, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(sessionID))
			if err != nil {
				return fmt.Errorf("find active process for artifact update: %w", err)
			}
			if !found || active.ID != runID {
				return nil
			}
		}
		current, err := s.repo.Find(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("find session for artifact update: %w", err)
		}
		count, err := s.artifacts.CountArtifacts(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("count session artifacts: %w", err)
		}
		current.ArtifactCount = count
		return s.updateArtifactCountWithEvent(ctx, current, count, "session.artifacts_updated", map[string]any{
			"artifactCount": count,
			"processRunId":  string(runID),
		})
	})
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

func (s *Service) handleCodexEventWithRetry(sessionID domain.ID, handle processdomain.CodexHandle, event processdomain.CodexEvent, extraEvents ...sessionEventInput) error {
	retryPersistence := event.Type == processdomain.CodexEventPlan || codexEventAcknowledgesPrompt(event)
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
			return s.handleCodexEvent(ctx, sessionID, handle, event, extraEvents...)
		})
		if err == nil {
			return nil
		}
		if !retryPersistence {
			log.Printf("handle codex event: session=%s process_run=%s type=%s error=%v", sessionID, handle.ProcessRunID, event.Type, err)
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

func (s *Service) handleCodexEvent(ctx context.Context, sessionID domain.ID, handle processdomain.CodexHandle, event processdomain.CodexEvent, archivedEvents ...sessionEventInput) error {
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
	saveSession := false
	saveUsage := false
	saveFilesChanged := false
	extraEvents := append([]sessionEventInput(nil), archivedEvents...)
	if activeRun && updateSessionUsageFromCodexEvent(&current, event) {
		saveUsage = true
		extraEvents = append(extraEvents, sessionEventInput{
			eventType: "session.usage_updated",
			payload: map[string]any{
				"processRunId": string(handle.ProcessRunID),
				"usage":        current.Usage,
			},
		})
	}
	if activeRun && codexEventCanUpdateSession(current.Status) {
		if todoList, ok := todoListFromCodexEvent(event); ok {
			current.TodoList = todoList
			current.UpdatedAt = s.now()
			saveSession = true
			extraEvents = append(extraEvents, sessionEventInput{
				eventType: "session.todo_list_updated",
				payload: map[string]any{
					"completed": todoList.Completed(),
					"total":     todoList.Total(),
					"todoList":  todoList,
				},
			})
		}
	}
	if activeRun && codexEventCanRefreshDiff(current.Status) && shouldReconcileSessionDiff(event) && s.diffCounter != nil {
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
	promptDelivered := codexEventAcknowledgesPrompt(event)
	return s.publishCodexEventWithSessionUpdates(ctx, current, handle.ProcessRunID, event, saveSession, saveUsage, saveFilesChanged, promptDelivered, extraEvents...)
}

func updateSessionUsageFromCodexEvent(current *domain.Session, event processdomain.CodexEvent) bool {
	if current == nil {
		return false
	}
	if usage, ok := event.Content.(processdomain.CodexUsageContent); ok {
		next := domain.TokenUsage{
			InputTokens:                  nonNegativeTokenCount(usage.InputTokens),
			CachedInputTokens:            nonNegativeTokenCount(usage.CachedInputTokens),
			OutputTokens:                 nonNegativeTokenCount(usage.OutputTokens),
			ReasoningOutputTokens:        nonNegativeTokenCount(usage.ReasoningOutputTokens),
			TotalTokens:                  nonNegativeTokenCount(usage.TotalTokens),
			ContextWindow:                nonNegativeTokenCount(usage.ContextWindow),
			CurrentInputTokens:           nonNegativeTokenCount(usage.CurrentInputTokens),
			CurrentCachedInputTokens:     nonNegativeTokenCount(usage.CurrentCachedInputTokens),
			CurrentOutputTokens:          nonNegativeTokenCount(usage.CurrentOutputTokens),
			CurrentReasoningOutputTokens: nonNegativeTokenCount(usage.CurrentReasoningOutputTokens),
			CurrentTotalTokens:           nonNegativeTokenCount(usage.CurrentTotalTokens),
			CompactionCount:              current.Usage.CompactionCount,
		}
		if next == current.Usage {
			return false
		}
		current.Usage = next
		return true
	}
	status, ok := event.Content.(processdomain.CodexStatusContent)
	if !ok || status.Code != "context.compacted" {
		return false
	}
	current.Usage.CompactionCount++
	return true
}

func nonNegativeTokenCount(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func hasCodexFileChanges(event processdomain.CodexEvent) bool {
	if event.Type != processdomain.CodexEventFileChange {
		return false
	}
	content, ok := event.Content.(processdomain.CodexFileChangeContent)
	return ok && len(content.Changes) > 0
}

func shouldReconcileSessionDiff(event processdomain.CodexEvent) bool {
	if event.Type == processdomain.CodexEventProcessExit || hasCodexFileChanges(event) {
		return true
	}
	status, ok := event.Content.(processdomain.CodexStatusContent)
	if !ok {
		return false
	}
	return status.Code == "task.completed" || status.Code == "turn.completed"
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
				SessionID: current.ID,
				Data:      data,
				Filename:  inlineArtifactFilename(event.EventID, index, mimeType),
				SourceKey: fmt.Sprintf("%s:%s:%d", handle.ProcessRunID, event.EventID, index),
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
		if qualifiedName == "publish_artifact" {
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
			"message":       "Codex 临时文件保存失败，原始内容未写入会话历史",
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
	if status, ok := event.Content.(processdomain.CodexStatusContent); ok {
		switch status.Code {
		case "task.started", "turn.started":
			return true
		}
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

func codexEventCanRefreshDiff(status domain.Status) bool {
	switch status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping:
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
				if err := s.handleCodexEvent(ctx, session.ID, handle, processdomain.CodexEvent{
					Type:           processdomain.CodexEventProcessExit,
					SessionID:      processdomain.SessionID(session.ID),
					ProcessRunID:   handle.ProcessRunID,
					CodexSessionID: handle.CodexSessionID,
					CreatedAt:      exitResult.FinishedAt,
				}); err != nil {
					return fmt.Errorf("reconcile session diff after process exit: %w", err)
				}
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
				return s.saveSessionWithStatusUpdate(ctx, current, "workflow.failed", map[string]any{
					"sessionId": string(options.sessionID),
					"nodeRunId": string(domain.NodeRunID(*options.nodeRunID)),
					"reason":    workflowTransitionErr.Error(),
				})
			}
			if workflowAdvance == nil {
				return errors.New("workflow advance is missing after process exit")
			}
			if workflowApplyErr != nil {
				if err := transitionSession(&current, domain.StatusFailed, s.now()); err != nil {
					return err
				}
				return s.saveSessionWithStatusUpdate(ctx, current, "workflow.failed", map[string]any{
					"sessionId": string(options.sessionID),
					"nodeRunId": string(domain.NodeRunID(*options.nodeRunID)),
					"reason":    workflowApplyErr.Error(),
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
			return s.saveSessionWithStatusUpdate(ctx, latest, "workflow.failed", map[string]any{
				"sessionId": string(options.sessionID),
				"nodeRunId": string(domain.NodeRunID(*options.nodeRunID)),
				"reason":    workflowApplyErr.Error(),
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
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
	if err != nil {
		return domain.Session{}, false, fmt.Errorf("find active process after exit: %w", err)
	}
	processEvent := sessionEventInput{eventType: "process.exited", payload: processExitPayload(handle.ProcessRunID, exitResult)}
	if ok && active.ID != handle.ProcessRunID {
		return domain.Session{}, false, s.markProcessExitedWithSessionEvents(ctx, handle.ProcessRunID, exitResult, current, false, []sessionEventInput{processEvent})
	}
	if current.Mode == domain.ModeWorkflow && options.sessionID != "" && options.nodeRunID != nil &&
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
	if current.Mode == domain.ModeWorkflow && options.sessionID != "" && options.nodeRunID != nil {
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
					"cause":        "stop_requested",
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
		wasStopping := current.Status == domain.StatusStopping
		settlement := promptAppendSettlementAutomatic
		if wasStopping {
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
		if eventType == "session.stopped" {
			payload["cause"] = "completed"
			if wasStopping {
				payload["cause"] = "stop_requested"
			}
		}
		inputs = append(inputs, sessionEventInput{eventType: eventType, payload: payload})
		if err := s.markProcessExitedWithSessionEventsAndSettlement(ctx, handle.ProcessRunID, exitResult, current, true, inputs, settlement); err != nil {
			return domain.Session{}, false, err
		}
		return domain.Session{}, false, nil
	default:
		return domain.Session{}, false, s.markProcessExitedWithSessionEventsAndSettlement(ctx, handle.ProcessRunID, exitResult, current, false, inputs, promptAppendSettlementForExitedSession(current.Status))
	}
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
	if event.Type != processdomain.CodexEventProcessExit {
		return processdomain.ExitResult{}, false
	}
	result, ok := event.Content.(processdomain.ExitResult)
	return result, ok
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
	if event.Type != processdomain.CodexEventMessage {
		return "", false
	}
	message, ok := event.Content.(processdomain.CodexMessageContent)
	if !ok || message.Role != "assistant" {
		return "", false
	}
	output := strings.TrimSpace(message.Text)
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
				SessionID: options.sessionID,
				NodeRunID: &nodeRunID,
				Code:      appErr.Code,
				Message:   appErr.Error(),
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
		SessionID: options.sessionID,
		NodeRunID: domain.NodeRunID(*options.nodeRunID),
		Output:    output,
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
		"sessionId":      string(input.SessionID),
		"nodeRunId":      string(input.NodeRunID),
		"failed":         input.Failed,
		"failureCode":    input.FailureCode,
		"failureMessage": input.FailureMessage,
		"output":         copyPayload(input.Output),
	}
}

func workflowProcessExitInputFromPayload(payload map[string]any) (domain.WorkflowProcessExitInput, error) {
	sessionID, _ := payload["sessionId"].(string)
	nodeRunID, _ := payload["nodeRunId"].(string)
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(nodeRunID) == "" {
		return domain.WorkflowProcessExitInput{}, errors.New("workflow process exit checkpoint is missing workflow run or node run id")
	}
	failed, _ := payload["failed"].(bool)
	failureCode, _ := payload["failureCode"].(string)
	failureMessage, _ := payload["failureMessage"].(string)
	output, _ := payload["output"].(map[string]any)
	return domain.WorkflowProcessExitInput{
		SessionID:      domain.ID(sessionID),
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
		if err := s.saveSessionWithStatusUpdate(ctx, session, "session.blocked", map[string]any{
			"sessionId":      string(advance.SessionID),
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
		if err := s.saveSessionWithStatusUpdate(ctx, session, "session.completed", map[string]any{
			"sessionId": string(advance.SessionID),
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
		if err := s.saveSessionWithStatusUpdate(ctx, session, "session.waiting_approval", map[string]any{
			"sessionId":        string(advance.SessionID),
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
			return s.handleWorkflowNodeFailure(ctx, session, advance.SessionID, advance.NodeRunID, "codex_start_failed", err.Error())
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
		return s.handleWorkflowNodeFailure(ctx, session, advance.SessionID, nil, "workflow_expr_failed", "expr node run id is missing")
	}
	results, err := runWorkflowExpr(advance.Expr.Script, advance.Expr.Params)
	if err != nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.SessionID, advance.NodeRunID, "workflow_expr_failed", err.Error())
	}
	next, err := s.workflows.CompleteNode(ctx, domain.WorkflowNodeCompleteInput{
		SessionID: advance.SessionID,
		NodeRunID: *advance.NodeRunID,
		CommandID: advance.CommandID,
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
		return s.handleWorkflowNodeFailure(ctx, session, advance.SessionID, advance.NodeRunID, apperror.CodeMergeFailed, "merge port is not configured")
	}
	if advance.NodeRunID == nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.SessionID, nil, apperror.CodeMergeFailed, "merge node run id is missing")
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
		"sessionId":   string(advance.SessionID),
		"nodeRunId":   stringValuePtr(advance.NodeRunID),
		"strategy":    result.Strategy,
		"status":      result.Status,
		"failureCode": result.FailureCode,
	})
	if result.Status != "merged" {
		code := result.FailureCode
		if code == "" {
			code = apperror.CodeMergeFailed
		}
		if s.questions != nil {
			return s.askMergeFailure(ctx, session, advance, result, code)
		}
		return s.handleWorkflowNodeFailure(ctx, session, advance.SessionID, advance.NodeRunID, code, result.FailureReason, mergeOutput(result))
	}
	return s.completeWorkflowMergeNode(ctx, session, advance, result)
}

func (s *Service) completeWorkflowMergeNode(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance, result gitdiffdomain.MergeResult) (DTO, error) {
	if advance.NodeRunID == nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.SessionID, nil, apperror.CodeMergeFailed, "merge node run id is missing")
	}
	next, err := s.workflows.CompleteNode(ctx, domain.WorkflowNodeCompleteInput{
		SessionID: advance.SessionID,
		NodeRunID: *advance.NodeRunID,
		CommandID: advance.CommandID,
		Output:    mergeOutput(result),
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
		return s.handleWorkflowNodeFailure(ctx, session, advance.SessionID, nil, code, result.FailureReason)
	}
	metadata := mergeFailureQuestionMetadata(advance, result, code)
	requestID := questiondomain.RequestID("")
	if advance.CommandID != "" {
		requestID = questiondomain.RequestID("merge-failure-" + advance.CommandID)
	}
	request, err := s.questions.CreateRequest(ctx, questionapp.CreateRequestInput{
		RequestID: requestID,
		SessionID: questiondomain.SessionID(session.ID),
		Questions: []questiondomain.Question{
			{
				Body:     mergeFailureQuestionBody(result),
				Type:     "merge_failure_action",
				Metadata: metadata,
				Status:   string(questiondomain.RequestPending),
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
	if err := s.saveSessionWithStatusUpdate(ctx, session, "workflow.merge_waiting_user", map[string]any{
		"sessionId":     string(advance.SessionID),
		"nodeRunId":     stringValuePtr(advance.NodeRunID),
		"requestId":     string(request.ID),
		"failureCode":   code,
		"failureReason": result.FailureReason,
	}); err != nil {
		if advance.CommandID == "" && request.Created {
			if cancelErr := s.questions.CancelPendingRequestsBySession(ctx, questiondomain.SessionID(session.ID), "merge failure question abandoned"); cancelErr != nil {
				return DTO{}, fmt.Errorf("save session: %w; cancel merge failure question: %v", err, cancelErr)
			}
		}
		return DTO{}, fmt.Errorf("save session: %w", err)
	}
	return toDTO(session), nil
}

func (s *Service) HandleQuestionRequestAnswered(ctx context.Context, request questionapp.RequestDTO) error {
	if s == nil {
		return errors.New("session usecase: nil service")
	}
	if request.Status != questiondomain.RequestAnswered {
		return nil
	}
	action, metadata, ok, err := mergeFailureDecision(request)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return s.withSessionLock(ctx, domain.ID(request.SessionID), func(ctx context.Context) error {
		session, err := s.repo.Find(ctx, domain.ID(request.SessionID))
		if err != nil {
			return fmt.Errorf("find session: %w", err)
		}
		return s.applyMergeFailureDecision(ctx, session, request.ID, action, metadata)
	})
}

func (s *Service) applyMergeFailureDecision(ctx context.Context, session domain.Session, requestID questiondomain.RequestID, action string, metadata map[string]any) error {
	nodeRunIDValue := domain.NodeRunID(stringFromMap(metadata, "nodeRunId"))
	if nodeRunIDValue == "" {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "merge failure question metadata is incomplete").
			WithDetails(map[string]any{"sessionId": string(session.ID), "requestId": string(requestID)})
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
			SessionID: session.ID,
			NodeRunID: &nodeRunID,
			Status:    "running",
			Merge:     &domain.WorkflowMerge{Strategy: strategy},
		})
		return err
	case "stop_session":
		_, err := s.stopSession(ctx, session.ID)
		return err
	case "fail_node":
		_, err := s.handleWorkflowNodeFailure(ctx, session, session.ID, &nodeRunID, code, reason, mergeFailureOutputFromMetadata(metadata))
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

func mergeFailureDecision(request questionapp.RequestDTO) (string, map[string]any, bool, error) {
	for _, question := range request.Questions {
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

func isMergeFailureQuestionRequest(questions []questiondomain.Question) bool {
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

func (s *Service) handleWorkflowNodeFailure(ctx context.Context, session domain.Session, sessionID domain.ID, nodeRunID *domain.NodeRunID, code string, message string, output ...map[string]any) (DTO, error) {
	if s.workflows == nil || nodeRunID == nil {
		if err := transitionSession(&session, domain.StatusFailed, s.now()); err != nil {
			return DTO{}, err
		}
		if err := s.saveSessionWithStatusUpdate(ctx, session, "session.failed", map[string]any{
			"code": code, "reason": message,
		}); err != nil {
			return DTO{}, err
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
			if err := s.saveSessionWithStatusUpdate(ctx, session, failureEvent.eventType, failureEvent.payload); err != nil {
				return DTO{}, err
			}
		}
	}
	advance, err := s.workflows.FailNode(ctx, domain.WorkflowNodeFailInput{
		SessionID: sessionID,
		NodeRunID: *nodeRunID,
		Code:      code,
		Message:   message,
		Output:    firstPayload(output),
	})
	if err != nil {
		_ = s.workflows.MarkStartFailed(ctx, domain.WorkflowStartFailureInput{
			SessionID: sessionID,
			NodeRunID: nodeRunID,
			Code:      code,
			Message:   message,
		})
		return DTO{}, err
	}
	return s.applyWorkflowAdvance(ctx, session, advance, workflowAdvanceOptions{})
}

type sessionEventInput struct {
	eventType string
	payload   map[string]any
}

func statusUpdateInputs(inputs ...sessionEventInput) []sessionEventInput {
	result := make([]sessionEventInput, 0, len(inputs)+1)
	result = append(result, inputs...)
	return append(result, sessionEventInput{eventType: sessionStatusUpdatedEvent})
}

func (s *Service) addStatusUpdateEvent(session domain.Session, events []eventdomain.DomainEvent) ([]eventdomain.DomainEvent, error) {
	if s.events == nil {
		return events, nil
	}
	var id domain.ID
	if len(events) > 0 {
		id = domain.ID(string(events[len(events)-1].ID) + "-status")
	} else {
		id = fallbackSessionEventID(session.ID)
	}
	event, ok := s.newSessionEventWithID(session, sessionStatusUpdatedEvent, nil, id)
	if ok {
		events = append(events, event)
	}
	return events, nil
}

func (s *Service) publishCodexEventWithSessionUpdates(ctx context.Context, session domain.Session, processRunID processdomain.RunID, event processdomain.CodexEvent, saveSession bool, saveUsage bool, saveFilesChanged bool, promptDelivered bool, extraInputs ...sessionEventInput) error {
	extraEvents, err := s.newSessionEvents(session, extraInputs)
	if err != nil {
		return err
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if promptDelivered {
				if err := tx.Sessions().CompletePromptAppends(ctx, string(processRunID), promptDeliveryTime(event, s.now())); err != nil {
					return err
				}
			}
			if saveSession {
				if err := tx.Sessions().Save(ctx, session); err != nil {
					return fmt.Errorf("save session: %w", err)
				}
			}
			if saveUsage {
				if err := updateSessionUsage(ctx, tx.Sessions(), session.ID, session.Usage); err != nil {
					return err
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
		for _, event := range extraEvents {
			s.publishSessionEvent(ctx, event)
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
	if saveUsage {
		if err := updateSessionUsage(ctx, s.repo, session.ID, session.Usage); err != nil {
			return err
		}
	}
	if saveFilesChanged {
		if err := s.repo.UpdateFilesChanged(ctx, session.ID, session.FilesChanged); err != nil {
			return err
		}
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

func updateSessionUsage(ctx context.Context, repo domain.Repository, sessionID domain.ID, usage domain.TokenUsage) error {
	writer, ok := repo.(domain.UsageRepository)
	if !ok {
		return errors.New("session usage repository is required")
	}
	if err := writer.UpdateUsage(ctx, sessionID, usage); err != nil {
		return fmt.Errorf("update session usage: %w", err)
	}
	return nil
}

func (s *Service) publishCodexRuntimeEvent(event processdomain.CodexEvent) {
	if s.codexPublisher == nil {
		return
	}
	switch event.Type {
	case processdomain.CodexEventPlan, processdomain.CodexEventProcessExit:
		return
	}
	_ = s.codexPublisher.PublishCodexEvent(context.Background(), event)
}

func promptDeliveryTime(event processdomain.CodexEvent, fallback time.Time) time.Time {
	if event.CreatedAt.IsZero() {
		return fallback
	}
	return event.CreatedAt
}

func (s *Service) createProcessRunWithSessionEvent(ctx context.Context, expectedSession domain.Session, run processdomain.Run, session domain.Session, options codexStartOptions, maxActive int, eventType string, payload map[string]any) (port.ExecutionClaimResult, error) {
	if s.uow != nil {
		events, err := s.newSessionEvents(session, statusUpdateInputs(sessionEventInput{eventType: eventType, payload: payload}))
		if err != nil {
			return port.ExecutionClaimResult{}, err
		}
		var result port.ExecutionClaimResult
		var publishedEvents []eventdomain.DomainEvent
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
				reconcileEvents, err := s.newSessionEvents(reconciled, statusUpdateInputs(sessionEventInput{
					eventType: "session.execution_already_active",
					payload:   map[string]any{"processRunId": string(claim.ActiveRun.ID)},
				}))
				if err != nil {
					return err
				}
				for _, event := range reconcileEvents {
					if err := tx.Events().Append(ctx, event); err != nil {
						return err
					}
					publishedEvents = append(publishedEvents, event)
				}
				return nil
			case port.ExecutionStale, port.ExecutionAtCapacity:
				return nil
			case port.ExecutionClaimed:
			default:
				return fmt.Errorf("unsupported execution claim status %q", claim.Status)
			}
			if err := tx.Sessions().MarkPromptAppendsInflight(ctx, options.promptAppendIDs, string(run.ID)); err != nil {
				return err
			}
			for _, event := range events {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
				publishedEvents = append(publishedEvents, event)
			}
			return nil
		}); err != nil {
			return port.ExecutionClaimResult{}, err
		}
		for _, event := range publishedEvents {
			s.publishSessionEvent(ctx, event)
		}
		return result, nil
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
		if err := s.saveSessionWithStatusUpdate(ctx, reconciled, "session.execution_already_active", map[string]any{"processRunId": string(active.ID)}); err != nil {
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
	if err := s.saveSessionWithStatusUpdate(ctx, session, eventType, payload); err != nil {
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

func (s *Service) markProcessRunningWithSessionEvent(ctx context.Context, runID processdomain.RunID, codexSessionID string, session domain.Session, eventType string, payload map[string]any) error {
	if s.uow != nil {
		events, err := s.newSessionEvents(session, statusUpdateInputs(sessionEventInput{eventType: eventType, payload: payload}))
		if err != nil {
			return err
		}
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().MarkRunning(ctx, runID, codexSessionID); err != nil {
				return fmt.Errorf("mark process running: %w", err)
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
	if err := s.processes.MarkRunning(ctx, runID, codexSessionID); err != nil {
		return fmt.Errorf("mark process running: %w", err)
	}
	return s.saveSessionWithStatusUpdate(ctx, session, eventType, payload)
}

func (s *Service) markProcessWaitingWithSessionEvent(ctx context.Context, runID processdomain.RunID, session domain.Session, eventType string, payload map[string]any) error {
	if s.uow != nil {
		events, err := s.newSessionEvents(session, statusUpdateInputs(sessionEventInput{eventType: eventType, payload: payload}))
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
	if err := s.processes.MarkWaitingUser(ctx, runID); err != nil {
		return fmt.Errorf("mark process waiting user: %w", err)
	}
	return s.saveSessionWithStatusUpdate(ctx, session, eventType, payload)
}

func (s *Service) markProcessStoppingWithSessionEvent(ctx context.Context, runID processdomain.RunID, session domain.Session, eventType string, payload map[string]any) error {
	if s.uow != nil {
		events, err := s.newSessionEvents(session, statusUpdateInputs(sessionEventInput{eventType: eventType, payload: payload}))
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
	if err := s.processes.MarkStopping(ctx, runID); err != nil {
		return fmt.Errorf("mark process stopping: %w", err)
	}
	return s.saveSessionWithStatusUpdate(ctx, session, eventType, payload)
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
	if saveSession {
		inputs = statusUpdateInputs(inputs...)
	}
	events, err := s.newSessionEvents(session, inputs)
	if err != nil {
		return err
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			return persistProcessExitInTx(ctx, tx, runID, result, session, saveSession, events, settlement)
		}); err != nil {
			return err
		}
		for _, event := range events {
			s.publishSessionEvent(ctx, event)
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
	events, err := s.newSessionEvents(session, statusUpdateInputs(inputs...))
	if err != nil {
		return err
	}
	var workflowEvents []eventdomain.DomainEvent
	err = s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		if err := persistProcessExitInTx(ctx, tx, runID, result, session, true, events, promptAppendSettlementRelease); err != nil {
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
	return nil
}

func persistProcessExitInTx(ctx context.Context, tx port.Tx, runID processdomain.RunID, result processdomain.ExitResult, session domain.Session, saveSession bool, events []eventdomain.DomainEvent, settlement promptAppendSettlement) error {
	if err := tx.Processes().MarkExited(ctx, runID, result); err != nil {
		return fmt.Errorf("mark process exited: %w", err)
	}
	if err := settlePromptAppends(ctx, tx.Sessions(), runID, result, settlement); err != nil {
		return err
	}
	if saveSession {
		if err := tx.Sessions().Save(ctx, session); err != nil {
			return fmt.Errorf("save session: %w", err)
		}
	}
	for _, event := range events {
		if err := tx.Events().Append(ctx, event); err != nil {
			return err
		}
	}
	return nil
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

func (s *Service) saveSessionWithStatusUpdate(ctx context.Context, session domain.Session, eventType string, payload map[string]any) error {
	_, err := s.saveSessionWithEvents(ctx, session, statusUpdateInputs(sessionEventInput{eventType: eventType, payload: payload}))
	return err
}

func (s *Service) updateArtifactCountWithEvent(ctx context.Context, session domain.Session, count int, eventType string, payload map[string]any) error {
	event, ok, err := s.newSessionEvent(session, eventType, payload)
	if err != nil {
		return err
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Sessions().UpdateArtifactCount(ctx, session.ID, count); err != nil {
				return err
			}
			if ok {
				return tx.Events().Append(ctx, event)
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
	if err := s.repo.UpdateArtifactCount(ctx, session.ID, count); err != nil {
		return err
	}
	if ok {
		if err := s.events.Append(ctx, event); err != nil {
			return err
		}
		s.publishSessionEvent(ctx, event)
	}
	return nil
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
			if err := tx.Sessions().UpdateArtifactCount(ctx, session.ID, session.ArtifactCount); err != nil {
				return err
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
	if err := s.repo.UpdateArtifactCount(ctx, session.ID, session.ArtifactCount); err != nil {
		return true, err
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
	events := []eventdomain.DomainEvent{}
	if hasEvent {
		events = append(events, event)
	}
	return s.saveSessionAndEvents(ctx, session, events)
}

func (s *Service) saveSessionAndEvents(ctx context.Context, session domain.Session, events []eventdomain.DomainEvent) error {
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
			return err
		}
		for _, event := range events {
			s.publishSessionEvent(ctx, event)
		}
		return nil
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
	events, err := s.newSessionEvents(session, statusUpdateInputs(sessionEventInput{eventType: eventType, payload: payload}))
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
		if input.eventType == sessionStatusUpdatedEvent {
			var err error
			events, err = s.addStatusUpdateEvent(session, events)
			if err != nil {
				return nil, err
			}
			continue
		}
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
	update, ok := event.Content.(processdomain.PlanUpdate)
	if !ok {
		return domain.TodoList{}, false
	}
	list := domain.TodoList{Items: make([]domain.TodoItem, 0, len(update.Items))}
	for _, item := range update.Items {
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
			if err == nil && dto.Status == domain.StatusClosed && s.tunnels != nil {
				cleanupCtx, cancel := processCleanupContext(ctx)
				cleanupErr := s.tunnels.CloseTunnelsForSession(cleanupCtx, input.SessionID)
				cancel()
				if cleanupErr != nil {
					return dto, fmt.Errorf("close session tunnels: %w", cleanupErr)
				}
			}
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
	if strings.TrimSpace(session.BaseBranch) != "" && session.WorktreeCleanup.Status != domain.WorktreeCleanupCleaned {
		headCommit, headErr := s.captureSessionWorktreeHead(ctx, session)
		if headErr != nil {
			closeErr := apperror.Wrap(headErr, apperror.CodeCloseFailed, apperror.CategoryInfraError, "capture session worktree head failed").WithDetails(map[string]any{
				"sessionId": string(session.ID),
			}).WithRetryable(true)
			return DTO{}, releaseClose(closeErr)
		}
		session.WorktreeHeadCommit = headCommit
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
	artifactCountChanged := s.artifacts != nil && session.ArtifactCount != 0
	if s.artifacts != nil {
		session.ArtifactCount = 0
	}
	events := make([]sessionEventInput, 0, 4)
	if artifactCountChanged {
		events = append(events, sessionEventInput{eventType: "session.artifacts_updated", payload: map[string]any{"artifactCount": 0}})
	}
	events = append(events, sessionEventInput{eventType: "session.closed", payload: map[string]any{"reason": string(reason)}})
	events = append(events, sessionEventInput{eventType: sessionStatusUpdatedEvent})
	if cleanupRequested {
		events = append(events, sessionEventInput{eventType: "session.worktree_cleanup_requested", payload: worktreeUpdatePayload(session, map[string]any{
			"worktreeBranch": session.WorktreeBranch,
		})})
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

func (s *Service) captureSessionWorktreeHead(ctx context.Context, session domain.Session) (string, error) {
	if s.worktrees == nil {
		return strings.TrimSpace(session.WorktreeHeadCommit), nil
	}
	if headCommit := strings.TrimSpace(session.WorktreeHeadCommit); headCommit != "" {
		return headCommit, nil
	}
	headCommit, err := s.worktrees.HeadCommit(ctx, session.WorktreePath, "")
	if err == nil {
		return strings.TrimSpace(headCommit), nil
	}
	if s.projects == nil {
		return "", err
	}
	project, projectErr := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
	if projectErr != nil {
		return "", errors.Join(err, projectErr)
	}
	headCommit, branchErr := s.worktrees.HeadCommit(ctx, project.Path.Value, session.WorktreeBranch)
	if branchErr != nil {
		return "", errors.Join(err, branchErr)
	}
	return strings.TrimSpace(headCommit), nil
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
		events, err := s.newSessionEvents(closing, statusUpdateInputs(sessionEventInput{
			eventType: "session.closing", payload: map[string]any{"reason": string(reason)},
		}))
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
			if prepared.Status == port.ClosePrepared {
				for _, event := range events {
					if err := tx.Events().Append(ctx, event); err != nil {
						return err
					}
				}
				publish = true
			}
			return nil
		}); err != nil {
			return port.ClosePreparationResult{}, err
		}
		if publish {
			for _, event := range events {
				s.publishSessionEvent(ctx, event)
			}
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
	if err := s.saveSessionWithStatusUpdate(ctx, closing, "session.closing", map[string]any{"reason": string(reason)}); err != nil {
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
	if err := s.saveSessionWithStatusUpdate(ctx, closing, "session.close_failed", map[string]any{"reason": cause.Error()}); err != nil {
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

func (s *Service) DeleteSessionFile(ctx context.Context, id domain.SessionFileID) error {
	if s == nil || s.artifacts == nil {
		return errors.New("session artifact store is not configured")
	}
	if id == "" {
		return errors.New("session file id is required")
	}
	artifact, err := s.artifacts.FindArtifact(ctx, id)
	if err != nil {
		return err
	}
	return s.withSessionLock(ctx, artifact.SessionID, func(ctx context.Context) error {
		if _, err = s.artifacts.DeleteArtifact(ctx, id); err != nil {
			return err
		}
		count, err := s.artifacts.CountArtifacts(ctx, artifact.SessionID)
		if err != nil {
			return fmt.Errorf("count session artifacts after deletion: %w", err)
		}
		current, err := s.repo.Find(ctx, artifact.SessionID)
		if err != nil {
			return fmt.Errorf("find session after artifact deletion: %w", err)
		}
		current.ArtifactCount = count
		return s.updateArtifactCountWithEvent(ctx, current, count, "session.artifacts_updated", map[string]any{"artifactCount": count})
	})
}

func (s *Service) cancelPendingQuestions(ctx context.Context, sessionID domain.ID, reason string) error {
	if s.questions == nil {
		return nil
	}
	if err := s.questions.CancelPendingRequestsBySession(ctx, questiondomain.SessionID(sessionID), reason); err != nil {
		return apperror.Wrap(err, apperror.CodeQuestionsCancelled, apperror.CategoryInfraError, "cancel pending questions failed").WithRetryable(true)
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
	artifactIDs, err := normalizeArtifactIDs(input.ArtifactIDs)
	if err != nil {
		return PromptAppendDTO{}, err
	}
	mentions, err := normalizePromptMentions(input.Mentions)
	if err != nil {
		return PromptAppendDTO{}, err
	}
	stagedAttachments, err := s.findStagedAttachments(ctx, input.StagedAttachmentIDs)
	if err != nil {
		return PromptAppendDTO{}, err
	}
	if body == "" {
		if len(stagedAttachments) == 0 && len(artifactIDs) == 0 && len(mentions) == 0 {
			return PromptAppendDTO{}, errors.New("prompt append body is required")
		}
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
		artifacts, err := s.resolvePromptArtifacts(ctx, input.SessionID, artifactIDs, false)
		if err != nil {
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
			Mentions:    mentions,
			Status:      domain.PromptAppendPending,
			CreatedAt:   s.now(),
			Attachments: archivedAttachments,
			ArtifactIDs: artifactIDs,
			Artifacts:   artifacts,
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
		if canSteerAfterAppend(session) && s.processes != nil && s.codex != nil {
			active, found, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
			if err != nil {
				return fmt.Errorf("find active process for prompt append: %w", err)
			}
			if found {
				if err := s.repo.MarkPromptAppendsInflight(ctx, []string{append.ID}, string(active.ID)); err != nil {
					return fmt.Errorf("mark prompt append inflight: %w", err)
				}
				append.Status = domain.PromptAppendInflight
				append.DispatchedProcessRunID = string(active.ID)
				files := make([]domain.SessionFile, len(archivedAttachments))
				copy(files, archivedAttachments)
				files = appendUniqueSessionFiles(files, artifacts...)
				if err := s.codex.Steer(ctx, processdomain.CodexSteerInput{
					ProcessRunID: active.ID,
					Input:        codexInput(body, files, mentions),
				}); err != nil {
					releaseErr := s.repo.ReleasePromptAppends(ctx, string(active.ID))
					append.Status = domain.PromptAppendPending
					append.DispatchedProcessRunID = ""
					return errors.Join(fmt.Errorf("steer active codex turn: %w", err), releaseErr)
				}
				return nil
			}
		}
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

func normalizeArtifactIDs(ids []domain.SessionFileID) ([]domain.SessionFileID, error) {
	seen := make(map[domain.SessionFileID]struct{}, len(ids))
	result := make([]domain.SessionFileID, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "artifact id is required")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

func (s *Service) resolvePromptArtifacts(ctx context.Context, sessionID domain.ID, ids []domain.SessionFileID, allowMissing bool) ([]domain.SessionFile, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if s.files == nil {
		return nil, errors.New("session file store is required")
	}
	artifacts := make([]domain.SessionFile, 0, len(ids))
	for _, id := range ids {
		artifact, err := s.files.FindSessionFile(ctx, id)
		if err != nil {
			if errors.Is(err, domain.ErrSessionFileNotFound) {
				if allowMissing {
					continue
				}
				return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "artifact is unavailable").WithDetails(map[string]any{"artifactId": string(id)})
			}
			return nil, fmt.Errorf("find prompt artifact %s: %w", id, err)
		}
		if artifact.SessionID != sessionID || artifact.Role != domain.FileRoleArtifact {
			if allowMissing {
				continue
			}
			return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "artifact is unavailable").WithDetails(map[string]any{"artifactId": string(id)})
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
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
		if s.files != nil {
			var err error
			attachments, err = s.files.ListPromptAppendAttachments(ctx, input.SessionID, promptAppendID)
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
		artifacts, err := s.resolvePromptArtifacts(ctx, input.SessionID, updated.ArtifactIDs, true)
		if err != nil {
			return err
		}
		updated.Artifacts = artifacts
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
	events := []eventdomain.DomainEvent{}
	if hasEvent {
		events = append(events, event)
	}
	events, err = s.addStatusUpdateEvent(queued, events)
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
			for _, event := range events {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return domain.Session{}, false, err
		}
		for _, event := range events {
			s.publishSessionEvent(ctx, event)
		}
		return queued, true, nil
	}
	if err := s.repo.AppendPrompt(ctx, promptAppend); err != nil {
		return domain.Session{}, false, fmt.Errorf("append prompt: %w", err)
	}
	if err := s.saveSessionAndEvents(ctx, queued, events); err != nil {
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

func canSteerAfterAppend(session domain.Session) bool {
	return session.Status == domain.StatusRunning || session.Status == domain.StatusWaitingUser
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
	if input.SessionID == "" {
		return WorkflowRunDTO{}, errors.New("session id is required")
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
			SessionID: input.SessionID,
			NodeID:    input.NodeID,
			Approved:  input.Approved,
			Comment:   input.Comment,
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
			"sessionId": string(input.SessionID),
			"nodeId":    input.NodeID,
			"approved":  input.Approved,
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
		if approvalResult.Rejected {
			if err := s.appendApprovalRejectionPrompt(ctx, tx, session, strings.TrimSpace(input.Comment)); err != nil {
				return err
			}
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
				queued, queuedEvents, err := s.queueApprovalAdvance(ctx, tx, session, approvalResult.Advance)
				if err != nil {
					return err
				}
				session = queued
				publishEvents = append(publishEvents, queuedEvents...)
			}
		case approvalResult.Advance.Blocked:
			if err := transitionSession(&session, domain.StatusBlocked, s.now()); err != nil {
				return err
			}
			blockedEvents, err := s.newSessionEvents(session, statusUpdateInputs(sessionEventInput{
				eventType: "session.blocked",
				payload: map[string]any{
					"sessionId": string(approvalResult.Advance.SessionID),
					"reason":    approvalResult.Advance.BlockedReason,
				},
			}))
			if err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			for _, event := range blockedEvents {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
				publishEvents = append(publishEvents, event)
			}
		case approvalResult.Advance.Completed:
			if err := transitionSession(&session, domain.StatusCompleted, s.now()); err != nil {
				return err
			}
			completedEvents, err := s.newSessionEvents(session, statusUpdateInputs(sessionEventInput{
				eventType: "session.completed",
				payload:   map[string]any{"sessionId": string(approvalResult.Advance.SessionID)},
			}))
			if err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			for _, event := range completedEvents {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
				publishEvents = append(publishEvents, event)
			}
		case approvalResult.Advance.RequiresCodex:
			queued, queuedEvents, err := s.queueApprovalAdvance(ctx, tx, session, approvalResult.Advance)
			if err != nil {
				return err
			}
			session = queued
			publishEvents = append(publishEvents, queuedEvents...)
		case !approvalResult.Advance.RequiresCodex && approvalResult.Advance.Merge == nil && approvalResult.Advance.Expr == nil && !approvalResult.Advance.Close:
			if err := transitionSessionToWaitingApproval(&session, false, s.now()); err != nil {
				return err
			}
			waitingEvents, err := s.newSessionEvents(session, statusUpdateInputs(sessionEventInput{
				eventType: "session.waiting_approval",
				payload: map[string]any{
					"sessionId":        string(approvalResult.Advance.SessionID),
					"nodeRunId":        stringValuePtr(approvalResult.Advance.NodeRunID),
					"currentNodeId":    approvalResult.Advance.CurrentNodeID,
					"currentNodeTitle": approvalResult.Advance.CurrentNodeTitle,
					"approvalPhase":    approvalResult.Advance.ApprovalPhase,
					"result":           approvalResult.Advance.Result,
				},
			}))
			if err != nil {
				return err
			}
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			for _, event := range waitingEvents {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
				publishEvents = append(publishEvents, event)
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
		for _, event := range postCommitAdvance.publishEvents {
			s.publishSessionEvent(ctx, event)
		}
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
	statusChanged := session.Status != domain.StatusRunning
	if statusChanged {
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
	events := []eventdomain.DomainEvent{event}
	if statusChanged {
		events, err = s.addStatusUpdateEvent(session, events)
		if err != nil {
			return workflowApprovalPostCommitAdvance{}, err
		}
	}
	for _, published := range events {
		if err := tx.Events().Append(ctx, published); err != nil {
			return workflowApprovalPostCommitAdvance{}, err
		}
	}
	return workflowApprovalPostCommitAdvance{
		session: session, advance: advance, commandEventID: event.ID, pendingEvent: event, publishEvents: events,
	}, nil
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
	for _, event := range pending.publishEvents {
		s.publishSessionEvent(ctx, event)
	}
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

func (s *Service) queueApprovalAdvance(ctx context.Context, tx port.Tx, session domain.Session, advance domain.WorkflowAdvance) (domain.Session, []eventdomain.DomainEvent, error) {
	options := workflowCodexStartOptions(session, advance, workflowAdvanceOptions{})
	queued, event, hasEvent, err := s.prepareQueuedSession(session, options, queuePriorityForSession(session), queueKindForStartOptions(options))
	if err != nil {
		return domain.Session{}, nil, err
	}
	if err := tx.Sessions().Save(ctx, queued); err != nil {
		return domain.Session{}, nil, fmt.Errorf("save queued session: %w", err)
	}
	events := []eventdomain.DomainEvent{}
	if hasEvent {
		events = append(events, event)
	}
	events, err = s.addStatusUpdateEvent(queued, events)
	if err != nil {
		return domain.Session{}, nil, err
	}
	for _, event := range events {
		if err := tx.Events().Append(ctx, event); err != nil {
			return domain.Session{}, nil, err
		}
	}
	return queued, events, nil
}

func (s *Service) appendApprovalRejectionPrompt(ctx context.Context, tx port.Tx, session domain.Session, body string) error {
	id, err := s.generateID()
	if err != nil {
		return fmt.Errorf("generate prompt append id: %w", err)
	}
	promptAppend := domain.PromptAppend{
		ID:        string(id),
		SessionID: session.ID,
		Body:      body,
		Status:    domain.PromptAppendPending,
		CreatedAt: s.now(),
	}
	if err := tx.Sessions().AppendPrompt(ctx, promptAppend); err != nil {
		return fmt.Errorf("append prompt: %w", err)
	}
	return nil
}

func workflowCodexStartOptions(session domain.Session, advance domain.WorkflowAdvance, advanceOptions workflowAdvanceOptions) codexStartOptions {
	options := codexStartOptions{
		sessionID:               advance.SessionID,
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
	project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
	if err != nil {
		return DetailDTO{}, fmt.Errorf("find session project: %w", err)
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
	if err := s.attachPromptAppendArtifacts(ctx, id, appends); err != nil {
		return DetailDTO{}, err
	}
	currentNodeTitle, pendingApproval, err := s.currentNodeState(ctx, session)
	if err != nil {
		return DetailDTO{}, err
	}
	return toDetailDTO(session, project.Name, attachments, appends, currentNodeTitle, pendingApproval), nil
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
	currentNodeTitle, _, err := s.currentNodeState(ctx, session)
	if err != nil {
		return CardDTO{}, err
	}
	card := toCardDTO(session, attachments, currentNodeTitle)
	card.ProjectName = project.Name
	return card, nil
}

func (s *Service) GetSessionCardStatus(ctx context.Context, id domain.ID) (CardStatusDTO, error) {
	if s == nil {
		return CardStatusDTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return CardStatusDTO{}, fmt.Errorf("find session: %w", err)
	}
	currentNodeTitle, _, err := s.currentNodeState(ctx, session)
	if err != nil {
		return CardStatusDTO{}, err
	}
	return CardStatusDTO{
		Status:           session.Status,
		CurrentNodeTitle: currentNodeTitle,
		AvailableActions: availableActions(session),
		UpdatedAt:        session.UpdatedAt,
	}, nil
}

func (s *Service) ListSessions(ctx context.Context, input ListSessionsInput) (port.Page[CardDTO], error) {
	if s == nil {
		return port.Page[CardDTO]{}, errors.New("session usecase: nil service")
	}
	updatedBefore, err := s.olderThanCutoff(input.OlderThanDays)
	if err != nil {
		return port.Page[CardDTO]{}, err
	}
	page, pageSize := normalizePage(input.Page, input.PageSize)
	query := domain.ListQuery{
		ProjectID:     input.ProjectID,
		Scope:         input.Scope,
		Range:         input.Range,
		UpdatedBefore: updatedBefore,
		Page:          page,
		PageSize:      pageSize,
		Filter:        input.Filter,
		Sort:          input.Sort,
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
		currentNodeTitle, _, err := s.currentNodeState(ctx, session)
		if err != nil {
			return port.Page[CardDTO]{}, err
		}
		item := toCardDTO(session, attachments, currentNodeTitle)
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

func (s *Service) CleanupSessions(ctx context.Context, input CleanupSessionsInput) (int, error) {
	if s == nil {
		return 0, errors.New("session usecase: nil service")
	}
	scope := strings.ToLower(strings.TrimSpace(input.Scope))
	if scope == "" {
		scope = string(domain.StatusClosed)
	}
	if scope != string(domain.StatusClosed) {
		return 0, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "only closed sessions can be cleaned")
	}
	if input.OlderThanDays == 0 {
		return 0, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "olderThanDays is required")
	}
	updatedBefore, err := s.olderThanCutoff(input.OlderThanDays)
	if err != nil {
		return 0, err
	}
	if s.historyPurger == nil {
		return 0, errors.New("session history purger is required")
	}

	candidates, err := s.listCleanupCandidates(ctx, domain.ListQuery{
		ProjectID:     input.ProjectID,
		Scope:         scope,
		UpdatedBefore: updatedBefore,
		Filter:        input.Filter,
		Sort:          "updated_at asc",
	})
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	for _, candidate := range candidates {
		if candidate.Status != domain.StatusClosed {
			return 0, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "cleanup matched a session that is not closed").WithDetails(map[string]any{"sessionId": string(candidate.ID)})
		}
		if strings.TrimSpace(candidate.BaseBranch) != "" && candidate.WorktreeCleanup.Status != domain.WorktreeCleanupCleaned {
			return 0, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session worktree cleanup must finish before history cleanup").WithDetails(map[string]any{"sessionId": string(candidate.ID)})
		}
	}

	for _, candidate := range candidates {
		if err := s.deleteSessionFiles(ctx, candidate.ID); err != nil {
			return 0, err
		}
		if err := s.deleteCodexSessions(ctx, candidate); err != nil {
			return 0, err
		}
	}
	ids := make([]domain.ID, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	if err := s.historyPurger.PurgeSessions(ctx, ids); err != nil {
		return 0, fmt.Errorf("purge session history: %w", err)
	}
	return len(ids), nil
}

func (s *Service) olderThanCutoff(days int) (*time.Time, error) {
	if days == 0 {
		return nil, nil
	}
	if days != 3 && days != 7 && days != 30 {
		return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "olderThanDays must be 3, 7, or 30")
	}
	cutoff := s.now().Add(-time.Duration(days) * 24 * time.Hour)
	return &cutoff, nil
}

func (s *Service) listCleanupCandidates(ctx context.Context, query domain.ListQuery) ([]domain.Session, error) {
	result := make([]domain.Session, 0)
	query.PageSize = maxPageSize
	for query.Page = 1; ; query.Page++ {
		rows, total, err := s.repo.ListCards(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("list sessions for cleanup: %w", err)
		}
		result = append(result, rows...)
		if len(rows) == 0 || len(result) >= total {
			return result, nil
		}
	}
}

func (s *Service) deleteSessionFiles(ctx context.Context, sessionID domain.ID) error {
	if s.files != nil {
		attachments, err := s.files.ListSessionAttachments(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("list session attachments for cleanup: %w", err)
		}
		for _, attachment := range attachments {
			if err := s.files.DeleteSession(ctx, attachment.ID); err != nil {
				return fmt.Errorf("delete session attachment %s: %w", attachment.ID, err)
			}
		}
	}
	if s.artifacts != nil {
		if err := s.artifacts.DeleteArtifactOutputDirectory(ctx, sessionID); err != nil {
			return fmt.Errorf("delete session artifact output: %w", err)
		}
	}
	return nil
}

func (s *Service) deleteCodexSessions(ctx context.Context, candidate domain.Session) error {
	threadID := strings.TrimSpace(candidate.CodexSessionID)
	if threadID == "" {
		return nil
	}
	if s.codexSessionCleaner == nil {
		return errors.New("Codex session cleaner is required")
	}
	if err := s.codexSessionCleaner.DeleteThread(ctx, threadID); err != nil {
		return fmt.Errorf("delete Codex thread %s: %w", threadID, err)
	}
	return nil
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
		Usage:              sessionUsageDTO(session.Usage),
		ArtifactCount:      session.ArtifactCount,
		FilesChanged:       session.FilesChanged,
		AvailableActions:   availableActions(session),
		LastRunAt:          session.LastRunAt,
		CreatedAt:          session.CreatedAt,
		UpdatedAt:          session.UpdatedAt,
	}
}

func sessionUsageDTO(usage domain.TokenUsage) *domain.TokenUsage {
	if usage.IsZero() {
		return nil
	}
	return &usage
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

func worktreeUpdatePayload(session domain.Session, payload map[string]any) map[string]any {
	result := copyPayload(payload)
	result["worktreeCleanup"] = toWorktreeCleanupDTO(session.WorktreeCleanup)
	result["availableActions"] = availableActions(session)
	result["updatedAt"] = session.UpdatedAt
	return result
}

func toWorkflowRunDTO(run domain.WorkflowRunSnapshot) WorkflowRunDTO {
	values := map[string]any{}
	for key, value := range run.Context {
		values[key] = value
	}
	return WorkflowRunDTO{
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
	sessionID, _ := event.Payload["sessionId"].(string)
	nodeID, _ := event.Payload["currentNodeId"].(string)
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	nodeRunID, _ := event.Payload["nodeRunId"].(string)
	title, _ := event.Payload["currentNodeTitle"].(string)
	phase, _ := event.Payload["approvalPhase"].(string)
	result, _ := event.Payload["result"].(map[string]any)
	return &PendingApprovalDTO{
		SessionID:        domain.ID(sessionID),
		NodeID:           strings.TrimSpace(nodeID),
		NodeRunID:        strings.TrimSpace(nodeRunID),
		CurrentNodeTitle: strings.TrimSpace(title),
		Phase:            strings.TrimSpace(phase),
		Result:           result,
	}
}

func toCardDTO(session domain.Session, attachments []domain.SessionAttachment, currentNodeTitle string) CardDTO {
	return CardDTO{
		DTO:                toDTO(session),
		RequirementSummary: session.Requirement,
		CurrentNodeTitle:   currentNodeTitle,
		TodoList:           session.TodoList,
		Attachments:        attachments,
		AvailableActions:   availableActions(session),
	}
}

func toDetailDTO(session domain.Session, projectName string, attachments []domain.SessionAttachment, appends []domain.PromptAppend, currentNodeTitle string, pendingApproval *PendingApprovalDTO) DetailDTO {
	promptAppends := make([]PromptAppendDTO, 0, len(appends))
	for _, promptAppend := range appends {
		promptAppends = append(promptAppends, toPromptAppendDTO(promptAppend))
	}
	return DetailDTO{
		DTO:              toDTO(session),
		ProjectName:      projectName,
		CloseReason:      session.CloseReason,
		CurrentNodeTitle: currentNodeTitle,
		PendingApproval:  pendingApproval,
		TodoList:         session.TodoList,
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
		attachment, err := s.files.Promote(ctx, domain.PromoteAttachmentInput{
			Staged:     staged,
			SessionID:  sessionID,
			SourceType: sourceType,
			SourceID:   sourceID,
		})
		if err != nil {
			return archived, fmt.Errorf("promote staged attachment %s: %w", staged.ID, err)
		}
		archived = append(archived, attachment)
		if err := s.attachments.DeleteStagedAttachment(ctx, staged.ID); err != nil {
			return archived, fmt.Errorf("delete staged attachment %s: %w", staged.ID, err)
		}
	}
	return archived, nil
}

func (s *Service) listPromptAppendAttachments(ctx context.Context, sessionID domain.ID, appendID string) ([]domain.SessionAttachment, error) {
	if s.files == nil {
		return []domain.SessionAttachment{}, nil
	}
	attachments, err := s.files.ListPromptAppendAttachments(ctx, sessionID, appendID)
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
		if s.files != nil {
			if err := s.files.DeleteSession(ctx, attachment.ID); err != nil {
				errs = append(errs, fmt.Errorf("delete session attachment file %s: %w", attachment.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (s *Service) listSessionAttachments(ctx context.Context, sessionID domain.ID) ([]domain.SessionAttachment, error) {
	if s.files == nil {
		return []domain.SessionAttachment{}, nil
	}
	attachments, err := s.files.ListSessionAttachments(ctx, sessionID)
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

func (s *Service) attachPromptAppendArtifacts(ctx context.Context, sessionID domain.ID, appends []domain.PromptAppend) error {
	for index := range appends {
		artifacts, err := s.resolvePromptArtifacts(ctx, sessionID, appends[index].ArtifactIDs, true)
		if err != nil {
			return fmt.Errorf("resolve prompt append artifacts: %w", err)
		}
		appends[index].Artifacts = artifacts
	}
	return nil
}

func toPromptAppendDTO(append domain.PromptAppend) PromptAppendDTO {
	return PromptAppendDTO{
		ID:          append.ID,
		SessionID:   append.SessionID,
		Body:        append.Body,
		CreatedAt:   append.CreatedAt,
		Attachments: append.Attachments,
		Artifacts:   append.Artifacts,
	}
}

func availableActions(session domain.Session) []string {
	if session.Status == domain.StatusFailed && strings.TrimSpace(session.InitializationErrorCode) != "" {
		return []string{"retry_initialization", "close"}
	}
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
