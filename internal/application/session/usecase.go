package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
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
)

type UseCase interface {
	CreateSession(ctx context.Context, input CreateSessionInput) (DTO, error)
	MarkInterruptedSessionsRecoverable(ctx context.Context) (int, error)
	StartSession(ctx context.Context, id domain.ID) (DTO, error)
	StartSessionWithOptions(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error)
	SetSessionPriority(ctx context.Context, input SetSessionPriorityInput) (DTO, error)
	StopSession(ctx context.Context, id domain.ID) (DTO, error)
	StopProjectSessions(ctx context.Context, projectID domain.ProjectID) (int, error)
	ResumeSession(ctx context.Context, id domain.ID) (DTO, error)
	ResumeSessionWithOptions(ctx context.Context, id domain.ID, options StartSessionOptions) (DTO, error)
	DrainQueuedSessions(ctx context.Context) (int, error)
	CloseSession(ctx context.Context, input CloseSessionInput) (DTO, error)
	UpdateSessionConfig(ctx context.Context, input UpdateSessionConfigInput) (DTO, error)
	MarkWaitingUser(ctx context.Context, id domain.ID) (DTO, error)
	MarkRunningAfterUserWait(ctx context.Context, id domain.ID) (DTO, error)
	AppendPrompt(ctx context.Context, input AppendPromptInput) (PromptAppendDTO, error)
	SubmitWorkflowApproval(ctx context.Context, input SubmitWorkflowApprovalInput) (WorkflowRunDTO, error)
	HandleQuestionBatchAnswered(ctx context.Context, batch questionapp.BatchDTO) error
	GetSession(ctx context.Context, id domain.ID) (DetailDTO, error)
	GetSessionCard(ctx context.Context, id domain.ID) (CardDTO, error)
	ListSessions(ctx context.Context, input ListSessionsInput) (port.Page[CardDTO], error)
}

type CreateSessionInput struct {
	ProjectID           domain.ProjectID
	Requirement         string
	Mode                domain.Mode
	BaseBranch          string
	Config              domain.Config
	Priority            domain.Priority
	StagedAttachmentIDs []domain.StagedAttachmentID
}

type StartSessionOptions struct {
	Force                bool
	prompt               string
	resumeCodexSessionID string
}

type CloseSessionInput struct {
	SessionID domain.ID
	Reason    domain.CloseReason
}

type SetSessionPriorityInput struct {
	SessionID domain.ID
	Priority  domain.Priority
}

type UpdateSessionConfigInput struct {
	SessionID domain.ID
	Config    domain.Config
}

type AppendPromptInput struct {
	SessionID           domain.ID
	Body                string
	StagedAttachmentIDs []domain.StagedAttachmentID
}

type SubmitWorkflowApprovalInput struct {
	WorkflowRunID domain.WorkflowRunID
	NodeID        string
	Approved      bool
	Comment       string
}

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
	WorktreeHeadCommit string
	CodexSessionID     string
	Config             domain.Config
	AvailableActions   []string
	LastRunAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CardDTO struct {
	DTO
	ProjectName        string
	RequirementSummary string
	CurrentNodeTitle   string
	PendingQuestion    bool
	TodoList           domain.TodoList
	Attachments        []domain.SessionAttachment
	AvailableActions   []string
}

type DetailDTO struct {
	DTO
	CloseReason      *domain.CloseReason
	CurrentNodeTitle string
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

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100

	maxSessionIDAttempts = 100
)

var ErrProcessLifecycleNotWired = errors.New("session process lifecycle is not wired")

var errStaleAnswerUserQueue = errors.New("answer_user queue has no active process")
var errWorkdirBusy = errors.New("session workdir already has an active process")

type Service struct {
	repo                domain.Repository
	uow                 port.UnitOfWork
	locker              port.SessionLocker
	projects            projectdomain.Repository
	attachments         domain.AttachmentRepository
	files               domain.AttachmentStore
	worktrees           domain.WorktreeManager
	workflows           domain.WorkflowStarter
	merge               gitdiffdomain.MergePort
	processes           processdomain.Repository
	codex               processdomain.CodexProcess
	workdirMu           sync.Mutex
	activeWorkdirs      map[string]domain.ID
	events              eventdomain.Store
	publisher           eventdomain.Publisher
	questions           questionCoordinator
	launchMu            sync.Mutex
	now                 func() time.Time
	generateID          func() (domain.ID, error)
	maxConcurrentAgents int
	queueDrainScheduler func(*Service)
}

type questionCoordinator interface {
	CreateBatch(ctx context.Context, input questionapp.CreateBatchInput) (questionapp.BatchDTO, error)
	CancelPendingBySession(ctx context.Context, sessionID questiondomain.SessionID, reason string) error
}

type Option func(*Service)

func WithAttachments(repo domain.AttachmentRepository, store domain.AttachmentStore) Option {
	return func(s *Service) {
		s.attachments = repo
		s.files = store
	}
}

func WithWorktrees(worktrees domain.WorktreeManager) Option {
	return func(s *Service) {
		s.worktrees = worktrees
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
	service := &Service{
		repo:                repo,
		projects:            projects,
		now:                 time.Now,
		generateID:          generateID,
		maxConcurrentAgents: 1,
		queueDrainScheduler: func(*Service) {},
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) scheduleQueueDrain() {
	if s == nil || s.queueDrainScheduler == nil {
		return
	}
	s.queueDrainScheduler(s)
}

func (s *Service) MarkInterruptedSessionsRecoverable(ctx context.Context) (int, error) {
	if s == nil {
		return 0, errors.New("session usecase: nil service")
	}
	sessions, err := s.repo.ListInterruptedWithCodexSession(ctx)
	if err != nil {
		return 0, fmt.Errorf("list interrupted sessions: %w", err)
	}
	now := s.now()
	for _, session := range sessions {
		previousStatus := session.Status
		session.Status = domain.StatusStopped
		session.UpdatedAt = now
		clearQueue(&session)
		if err := s.saveInterruptedSessionWithEvent(ctx, session, now, "session.recoverable", map[string]any{
			"reason":         "service_restarted",
			"previousStatus": string(previousStatus),
			"codexSessionId": session.CodexSessionID,
		}); err != nil {
			return 0, fmt.Errorf("mark session %s recoverable: %w", session.ID, err)
		}
	}
	unresumableSessions, err := s.listInterruptedWithoutCodexSession(ctx)
	if err != nil {
		return 0, err
	}
	for _, session := range unresumableSessions {
		previousStatus := session.Status
		session.Status = domain.StatusResumeFailed
		session.UpdatedAt = now
		clearQueue(&session)
		if err := s.saveInterruptedSessionWithEvent(ctx, session, now, "session.resume_failed", map[string]any{
			"reason":         "service_restarted_without_codex_session_id",
			"previousStatus": string(previousStatus),
		}); err != nil {
			return 0, fmt.Errorf("mark session %s resume failed: %w", session.ID, err)
		}
	}
	return len(sessions) + len(unresumableSessions), nil
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
	baseBranch := strings.TrimSpace(input.BaseBranch)
	if project.IsGit {
		if baseBranch == "" {
			return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "base branch is required for git project").WithDetails(map[string]any{
				"projectId": string(input.ProjectID),
			}).WithUserAction("select_base_branch")
		}
		if s.worktrees == nil {
			return DTO{}, errors.New("session worktree manager is required for git project")
		}
	}

	var id domain.ID
	var session domain.Session
	var worktreePath string
	createdSession := false
	for attempt := 0; attempt < maxSessionIDAttempts; attempt++ {
		id, err = s.sessionIDForProject(ctx, project, generatedID, attempt)
		if err != nil {
			return DTO{}, err
		}
		worktreePath = project.Path.Value
		if project.IsGit {
			worktreePath = s.worktrees.PathForSession(input.ProjectID, id)
		}
		session = domain.Session{
			ID:           id,
			ProjectID:    input.ProjectID,
			Requirement:  requirement,
			Mode:         mode,
			Status:       domain.StatusCreated,
			Priority:     normalizePriority(input.Priority),
			BaseBranch:   baseBranch,
			WorktreePath: worktreePath,
			Config:       config,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := s.repo.Create(ctx, session); err != nil {
			if isRandomHexID(generatedID) && errors.Is(err, domain.ErrSessionAlreadyExists) {
				continue
			}
			return DTO{}, fmt.Errorf("create session: %w", err)
		}
		createdSession = true
		break
	}
	if !createdSession {
		return DTO{}, fmt.Errorf("create session: exhausted %d session id attempts", maxSessionIDAttempts)
	}

	createdWorktree := false
	if project.IsGit {
		createdPath, err := s.worktrees.Create(ctx, project.Path.Value, input.ProjectID, id, baseBranch)
		if err != nil {
			failedAt := s.now()
			session.Status = domain.StatusFailed
			session.UpdatedAt = failedAt
			_ = s.repo.Save(ctx, session)
			return DTO{}, apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "create session worktree failed").WithDetails(map[string]any{
				"projectId":  string(input.ProjectID),
				"sessionId":  string(id),
				"baseBranch": baseBranch,
			}).WithRetryable(true)
		}
		createdWorktree = true
		pathChanged := createdPath != worktreePath
		worktreePath = createdPath
		baseCommit, err := s.worktrees.HeadCommit(ctx, createdPath, "")
		if err != nil {
			if cleanupErr := s.cleanupCreatedWorktree(ctx, project.Path.Value, worktreePath, id); cleanupErr != nil {
				return DTO{}, errors.Join(fmt.Errorf("read session worktree base commit: %w", err), fmt.Errorf("cleanup created worktree: %w", cleanupErr))
			}
			return DTO{}, apperror.Wrap(err, apperror.CodeWorktreeFailed, apperror.CategoryInfraError, "read session worktree base commit failed").WithDetails(map[string]any{
				"projectId": string(input.ProjectID),
				"sessionId": string(id),
			}).WithRetryable(true)
		}
		session.WorktreePath = createdPath
		session.WorktreeBaseCommit = baseCommit
		if pathChanged || strings.TrimSpace(baseCommit) != "" {
			session.UpdatedAt = s.now()
			if err := s.repo.Save(ctx, session); err != nil {
				if cleanupErr := s.cleanupCreatedWorktree(ctx, project.Path.Value, worktreePath, id); cleanupErr != nil {
					return DTO{}, errors.Join(fmt.Errorf("save session worktree snapshot: %w", err), fmt.Errorf("cleanup created worktree: %w", cleanupErr))
				}
				return DTO{}, fmt.Errorf("save session worktree snapshot: %w", err)
			}
		}
	}
	if _, err := s.archiveStagedAttachments(ctx, id, domain.AttachmentSourceRequirement, string(id), stagedAttachments); err != nil {
		failedAt := s.now()
		session.Status = domain.StatusFailed
		session.UpdatedAt = failedAt
		_ = s.repo.Save(ctx, session)
		if createdWorktree {
			if cleanupErr := s.cleanupCreatedWorktree(ctx, project.Path.Value, worktreePath, id); cleanupErr != nil {
				return DTO{}, errors.Join(err, fmt.Errorf("cleanup created worktree: %w", cleanupErr))
			}
		}
		return DTO{}, err
	}
	if mode == domain.ModeWorkflow {
		dto, startErr := s.startWorkflowSession(ctx, session, domain.WorkflowDefinitionID(*project.DefaultWorkflowID))
		if startErr != nil {
			failedAt := s.now()
			session.Status = domain.StatusFailed
			session.UpdatedAt = failedAt
			_ = s.repo.Save(ctx, session)
			if createdWorktree {
				if cleanupErr := s.cleanupCreatedWorktree(ctx, project.Path.Value, worktreePath, id); cleanupErr != nil {
					return DTO{}, errors.Join(startErr, fmt.Errorf("cleanup created worktree: %w", cleanupErr))
				}
			}
			return DTO{}, startErr
		}
		return dto, nil
	}
	return s.enqueueCodex(ctx, session, codexStartOptions{}, queuePriorityForSession(session))
}

func (s *Service) cleanupCreatedWorktree(ctx context.Context, projectPath string, worktreePath string, sessionID domain.ID) error {
	if s == nil || s.worktrees == nil {
		return nil
	}
	return errors.Join(
		s.worktrees.Remove(ctx, worktreePath),
		s.worktrees.DeleteBranch(ctx, projectPath, worktreeBranchName(sessionID)),
	)
}

func (s *Service) StartSession(ctx context.Context, id domain.ID) (DTO, error) {
	return s.StartSessionWithOptions(ctx, id, StartSessionOptions{})
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
		return s.startWorkflowSession(ctx, session, domain.WorkflowDefinitionID(*project.DefaultWorkflowID))
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
	options := codexStartOptions{}
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
}

func (s *Service) startWorkflowSession(ctx context.Context, session domain.Session, workflowDefinitionID domain.WorkflowDefinitionID) (DTO, error) {
	if s.workflows == nil {
		return DTO{}, errors.New("session workflow starter is required for workflow mode")
	}
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
		return s.closeSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: domain.CloseReasonWorkflowClosed})
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
		})
	}
	if !start.RequiresCodex {
		session.Status = domain.StatusWaitingApproval
		session.UpdatedAt = s.now()
		if err := s.saveSessionWithEvent(ctx, session, "session.waiting_approval", map[string]any{
			"workflowRunId": string(start.WorkflowRunID),
			"nodeRunId":     stringValuePtr(start.NodeRunID),
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	}
	dto, err := s.enqueueCodex(ctx, session, codexStartOptions{
		workflowRunID:     start.WorkflowRunID,
		nodeRunID:         workflowNodeRunID(start.NodeRunID),
		prompt:            start.Prompt,
		workflowJSONRetry: start.RequireJSONRetry,
	}, queuePriorityForSession(session))
	if err != nil {
		return s.handleWorkflowNodeFailure(ctx, session, start.WorkflowRunID, start.NodeRunID, "codex_start_failed", err.Error())
	}
	return dto, nil
}

func (s *Service) resolveSessionConfig(ctx context.Context, projectID domain.ProjectID, requested domain.Config) (domain.Config, error) {
	if strings.TrimSpace(requested.CodexModel) != "" &&
		strings.TrimSpace(requested.ReasoningEffort) != "" &&
		strings.TrimSpace(requested.PermissionMode) != "" {
		return trimConfig(requested), nil
	}
	previous, ok, err := s.repo.LastConfigForProject(ctx, projectID)
	if err != nil {
		return domain.Config{}, fmt.Errorf("last config for project: %w", err)
	}
	config := trimConfig(requested)
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
	return config, nil
}

func trimConfig(config domain.Config) domain.Config {
	return domain.Config{
		CodexModel:      strings.TrimSpace(config.CodexModel),
		ReasoningEffort: strings.TrimSpace(config.ReasoningEffort),
		PermissionMode:  strings.TrimSpace(config.PermissionMode),
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
	config := trimConfig(input.Config)
	if config.CodexModel == "" || config.ReasoningEffort == "" || config.PermissionMode == "" {
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session config is incomplete")
	}
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
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
	return dto, err
}

func (s *Service) stopSession(ctx context.Context, id domain.ID) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	switch session.Status {
	case domain.StatusQueued, domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping, domain.StatusResumeFailed:
	default:
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot stop from current status").WithDetails(map[string]any{"status": string(session.Status)})
	}
	if s.processes == nil || s.codex == nil {
		return DTO{}, apperror.Wrap(ErrProcessLifecycleNotWired, apperror.CodeCodexStartFailed, apperror.CategoryInfraError, "session process lifecycle is not wired")
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(id))
	if err != nil {
		return DTO{}, fmt.Errorf("find active process run: %w", err)
	}
	if !ok {
		now := s.now()
		session.Status = domain.StatusStopped
		session.UpdatedAt = now
		clearQueue(&session)
		if err := s.saveSessionWithEvent(ctx, session, "session.stopped", map[string]any{"reason": "no_active_process"}); err != nil {
			return DTO{}, err
		}
		if err := s.cancelPendingQuestions(ctx, session.ID, "session stopped"); err != nil {
			return DTO{}, err
		}
		s.scheduleQueueDrain()
		return toDTO(session), nil
	}
	now := s.now()
	session.Status = domain.StatusStopping
	session.UpdatedAt = now
	if err := s.saveSessionWithEvent(ctx, session, "session.stopping", map[string]any{"processRunId": string(active.ID)}); err != nil {
		return DTO{}, err
	}
	if err := s.codex.Stop(ctx, active.ID); err != nil {
		return DTO{}, fmt.Errorf("stop codex process: %w", err)
	}
	finishedAt := s.now()
	session.Status = domain.StatusStopped
	session.UpdatedAt = finishedAt
	if err := s.markProcessExitedWithSessionEvents(ctx, active.ID, processdomain.ExitResult{
		FailureReason: "stopped by user",
		FinishedAt:    finishedAt,
	}, session, true, []sessionEventInput{{
		eventType: "session.stopped",
		payload: map[string]any{
			"processRunId": string(active.ID),
			"reason":       "user_stopped",
		},
	}}); err != nil {
		return DTO{}, err
	}
	if err := s.cancelPendingQuestions(ctx, session.ID, "session stopped"); err != nil {
		return DTO{}, err
	}
	s.scheduleQueueDrain()
	return toDTO(session), nil
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
	}
	if session.Mode == domain.ModeWorkflow {
		if s.workflows == nil {
			return DTO{}, errors.New("session workflow starter is required for workflow mode")
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
		if options.prompt == "" {
			options.prompt = advance.Prompt
		}
	}
	if startOptions.Force {
		return s.startCodex(ctx, session, options, true)
	}
	return s.enqueueCodex(ctx, session, options, queuePriorityForSession(session))
}

func (s *Service) MarkWaitingUser(ctx context.Context, id domain.ID) (DTO, error) {
	var dto DTO
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		var err error
		dto, err = s.markWaitingUser(ctx, id)
		return err
	})
	return dto, err
}

func (s *Service) markWaitingUser(ctx context.Context, id domain.ID) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	switch session.Status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser:
	default:
		return DTO{}, fmt.Errorf("session cannot wait for user from status %q", session.Status)
	}
	var active processdomain.Run
	hasActive := false
	if s.processes != nil {
		var err error
		active, hasActive, err = s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
		if err != nil {
			return DTO{}, fmt.Errorf("find active process run: %w", err)
		}
	}
	session.Status = domain.StatusWaitingUser
	session.UpdatedAt = s.now()
	if hasActive {
		if err := s.markProcessWaitingWithSessionEvent(ctx, active.ID, session, "session.waiting_user", nil); err != nil {
			return DTO{}, err
		}
	} else {
		if err := s.saveSessionWithEvent(ctx, session, "session.waiting_user", nil); err != nil {
			return DTO{}, err
		}
	}
	return toDTO(session), nil
}

func (s *Service) MarkRunningAfterUserWait(ctx context.Context, id domain.ID) (DTO, error) {
	var dto DTO
	var queued domain.Session
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		var err error
		queued, dto, err = s.queueAfterUserWait(ctx, id)
		return err
	})
	if err != nil || queued.ID == "" {
		return dto, err
	}
	return s.waitAnswerUserQueueReleased(ctx, queued)
}

func (s *Service) queueAfterUserWait(ctx context.Context, id domain.ID) (domain.Session, DTO, error) {
	if s == nil {
		return domain.Session{}, DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return domain.Session{}, DTO{}, fmt.Errorf("find session: %w", err)
	}
	if session.Status != domain.StatusWaitingUser {
		return domain.Session{}, toDTO(session), nil
	}
	if s.processes == nil {
		return domain.Session{}, DTO{}, errors.New("process repository is required for answer_user queue")
	}
	queued, err := s.queueCodexSession(ctx, session, codexStartOptions{}, domain.QueuePriorityImmediate, domain.QueueKindAnswerUser)
	if err != nil {
		return domain.Session{}, DTO{}, err
	}
	return queued, toDTO(queued), nil
}

type codexStartOptions struct {
	resumeCodexSessionID    string
	workflowRunID           domain.WorkflowRunID
	nodeRunID               *processdomain.NodeRunID
	prompt                  string
	workflowJSONRetry       bool
	reviewAfterReuseFailure bool
}

func (s *Service) startCodex(ctx context.Context, session domain.Session, options codexStartOptions, force bool) (DTO, error) {
	if s.processes == nil || s.codex == nil {
		return DTO{}, apperror.Wrap(ErrProcessLifecycleNotWired, apperror.CodeCodexStartFailed, apperror.CategoryInfraError, "session process lifecycle is not wired")
	}
	_, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
	if err != nil {
		return DTO{}, fmt.Errorf("find active process run: %w", err)
	}
	if ok {
		return toDTO(session), nil
	}
	s.launchMu.Lock()
	defer s.launchMu.Unlock()
	if !force && s.maxConcurrentAgents > 0 {
		activeCount, err := s.processes.CountActive(ctx)
		if err != nil {
			return DTO{}, fmt.Errorf("count active process runs: %w", err)
		}
		if activeCount >= s.maxConcurrentAgents {
			return s.queueCodex(ctx, session, options, queuePriorityForSession(session), queueKindForStartOptions(options))
		}
	}
	dto, err := s.startCodexWithWorkdirReservation(ctx, session, options)
	if errors.Is(err, errWorkdirBusy) {
		return s.queueCodex(ctx, session, options, queuePriorityForSession(session), queueKindForStartOptions(options))
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

func (s *Service) startCodexNow(ctx context.Context, session domain.Session, options codexStartOptions, workdir string) (DTO, error) {
	runIDValue, err := s.generateID()
	if err != nil {
		return DTO{}, fmt.Errorf("generate process run id: %w", err)
	}
	runID := processdomain.RunID(runIDValue)
	now := s.now()
	run := processdomain.Run{
		ID:        runID,
		SessionID: processdomain.SessionID(session.ID),
		NodeRunID: options.nodeRunID,
		Status:    processdomain.StatusStarting,
		StartedAt: now,
	}
	clearQueue(&session)
	session.Status = domain.StatusStarting
	session.LastRunAt = &now
	session.UpdatedAt = now
	if err := s.createProcessRunWithSessionEvent(ctx, run, session, "session.starting", map[string]any{"processRunId": string(runID)}); err != nil {
		return DTO{}, err
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
		session.Status = status
		session.UpdatedAt = failedAt
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
		if options.resumeCodexSessionID != "" && session.Mode == domain.ModeWorkflow && s.workflows != nil {
			if run, markErr := s.workflows.MarkResumeFailedForSession(ctx, domain.WorkflowResumeFailureInput{
				SessionID: session.ID,
				Code:      processEventType,
				Message:   err.Error(),
			}); markErr == nil {
				s.appendSessionEvent(ctx, session, "workflow.waiting_resume_action", map[string]any{
					"workflowRunId": string(run.ID),
					"currentNodeId": run.CurrentNodeID,
				})
			} else {
				s.appendSessionEvent(ctx, session, "workflow.resume_action_failed", map[string]any{
					"reason": markErr.Error(),
				})
			}
		}
		code := apperror.CodeCodexStartFailed
		if options.resumeCodexSessionID != "" {
			code = apperror.CodeResumeFailed
		}
		return DTO{}, apperror.Wrap(err, code, apperror.CategoryCodexError, "start codex process failed").WithDetails(map[string]any{
			"processRunId": string(runID),
			"sessionId":    string(session.ID),
		}).WithRetryable(options.resumeCodexSessionID != "")
	}
	session.Status = domain.StatusRunning
	if handle.CodexSessionID != "" {
		session.CodexSessionID = handle.CodexSessionID
	}
	session.UpdatedAt = s.now()
	if err := s.markProcessRunningWithSessionEvent(ctx, runID, handle.PID, handle.CodexSessionID, session, "session.running", map[string]any{
		"processRunId":   string(runID),
		"pid":            handle.PID,
		"codexSessionId": handle.CodexSessionID,
	}); err != nil {
		return DTO{}, err
	}
	s.consumeCodexEvents(handle, session, options, workdir)
	return toDTO(session), nil
}

func (s *Service) startCodexWithWorkdirReservation(ctx context.Context, session domain.Session, options codexStartOptions) (DTO, error) {
	workdir, err := s.codexWorkdir(ctx, session)
	if err != nil {
		return DTO{}, err
	}
	if !s.reserveWorkdir(workdir, session.ID) {
		return DTO{}, errWorkdirBusy
	}
	dto, err := s.startCodexNow(ctx, session, options, workdir)
	if err != nil {
		s.releaseWorkdir(workdir, session.ID)
	}
	return dto, err
}

func (s *Service) codexWorkdir(ctx context.Context, session domain.Session) (string, error) {
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
	now := s.now()
	prompt := strings.TrimSpace(options.prompt)
	if kind == domain.QueueKindStart {
		var err error
		prompt, err = s.rebuiltSessionPrompt(ctx, session, prompt, options.reviewAfterReuseFailure)
		if err != nil {
			return domain.Session{}, err
		}
	}
	session.Status = domain.StatusQueued
	session.LastRunAt = &now
	session.UpdatedAt = now
	session.QueuedAt = &now
	session.Queue = domain.QueueIntent{
		Kind:                 kind,
		Priority:             normalizeQueuePriority(priority),
		WorkflowRunID:        options.workflowRunID,
		NodeRunID:            queueNodeRunID(options.nodeRunID),
		Prompt:               prompt,
		ResumeCodexSessionID: options.resumeCodexSessionID,
	}
	if err := s.saveSessionWithEvent(ctx, session, "session.queued", map[string]any{
		"priority":            string(session.Queue.Priority),
		"sessionPriority":     string(normalizePriority(session.Priority)),
		"queueKind":           string(session.Queue.Kind),
		"workflowRunId":       string(session.Queue.WorkflowRunID),
		"nodeRunId":           stringValuePtr(session.Queue.NodeRunID),
		"maxConcurrentAgents": s.maxConcurrentAgents,
	}); err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

func (s *Service) startQueuedSession(ctx context.Context, session domain.Session, force bool) (DTO, error) {
	if session.Status != domain.StatusQueued {
		return toDTO(session), nil
	}
	if !force {
		return toDTO(session), nil
	}
	if session.Queue.Kind == domain.QueueKindAnswerUser {
		s.launchMu.Lock()
		defer s.launchMu.Unlock()
		released, err := s.releaseQueuedAnswerUser(ctx, session)
		if err != nil {
			if errors.Is(err, errStaleAnswerUserQueue) {
				stopped, stopErr := s.stopStaleQueuedAnswerUser(ctx, session)
				if stopErr != nil {
					return DTO{}, stopErr
				}
				return toDTO(stopped), nil
			}
			return DTO{}, err
		}
		return toDTO(released), nil
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
			s.launchMu.Lock()
			defer s.launchMu.Unlock()
			if s.maxConcurrentAgents > 0 {
				activeCount, err := s.processes.CountActive(ctx)
				if err != nil {
					return fmt.Errorf("count active process runs: %w", err)
				}
				if activeCount >= s.maxConcurrentAgents {
					atCapacity = true
					return nil
				}
			}
			current, err = s.repo.Find(ctx, session.ID)
			if err != nil {
				return fmt.Errorf("find queued session: %w", err)
			}
			if current.Status != domain.StatusQueued {
				return nil
			}
			if current.Queue.Kind == domain.QueueKindAnswerUser {
				if _, err := s.releaseQueuedAnswerUser(ctx, current); err != nil {
					if errors.Is(err, errStaleAnswerUserQueue) {
						if _, stopErr := s.stopStaleQueuedAnswerUser(ctx, current); stopErr != nil {
							return stopErr
						}
						launched = true
						return nil
					}
					return err
				}
				launched = true
				return nil
			}
			if s.codex == nil {
				return ErrProcessLifecycleNotWired
			}
			if _, err := s.startCodexWithWorkdirReservation(ctx, current, codexStartOptionsFromQueue(current)); err != nil {
				if errors.Is(err, errWorkdirBusy) {
					atCapacity = true
					return nil
				}
				if current.Mode == domain.ModeWorkflow && current.Queue.WorkflowRunID != "" && current.Queue.NodeRunID != nil {
					if _, failErr := s.handleWorkflowNodeFailure(ctx, current, current.Queue.WorkflowRunID, current.Queue.NodeRunID, "codex_start_failed", err.Error()); failErr != nil {
						return failErr
					}
					launched = true
					return nil
				}
				saved, findErr := s.repo.Find(ctx, current.ID)
				if findErr != nil {
					return fmt.Errorf("find failed queued session: %w", findErr)
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

func (s *Service) releaseQueuedAnswerUser(ctx context.Context, session domain.Session) (domain.Session, error) {
	if session.Queue.Kind != domain.QueueKindAnswerUser {
		return session, nil
	}
	if s.processes == nil {
		return domain.Session{}, errors.New("process repository is required for answer_user queue release")
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
	if err != nil {
		return domain.Session{}, fmt.Errorf("find waiting process run: %w", err)
	}
	if !ok {
		return domain.Session{}, errStaleAnswerUserQueue
	}
	pid := 0
	if active.PID != nil {
		pid = *active.PID
	}
	clearQueue(&session)
	session.Status = domain.StatusRunning
	session.UpdatedAt = s.now()
	if err := s.markProcessRunningWithSessionEvent(ctx, active.ID, pid, active.CodexSessionID, session, "session.running", map[string]any{
		"processRunId": string(active.ID),
		"reason":       "user_answered",
	}); err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

func (s *Service) stopStaleQueuedAnswerUser(ctx context.Context, session domain.Session) (domain.Session, error) {
	clearQueue(&session)
	session.Status = domain.StatusStopped
	session.UpdatedAt = s.now()
	if err := s.saveSessionWithEvent(ctx, session, "session.stopped", map[string]any{"reason": "stale_answer_user_queue"}); err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

func (s *Service) tryReleaseQueuedAnswerUser(ctx context.Context, id domain.ID) (domain.Session, bool, bool, error) {
	if s.processes == nil {
		return domain.Session{}, false, false, errors.New("process repository is required for answer_user queue release")
	}
	var result domain.Session
	released := false
	stale := false
	err := s.withSessionLock(ctx, id, func(ctx context.Context) error {
		current, err := s.repo.Find(ctx, id)
		if err != nil {
			return fmt.Errorf("find queued answer_user session: %w", err)
		}
		result = current
		if current.Status != domain.StatusQueued || current.Queue.Kind != domain.QueueKindAnswerUser {
			return nil
		}
		s.launchMu.Lock()
		defer s.launchMu.Unlock()
		if s.maxConcurrentAgents > 0 {
			activeCount, err := s.processes.CountActive(ctx)
			if err != nil {
				return fmt.Errorf("count active process runs: %w", err)
			}
			if activeCount >= s.maxConcurrentAgents {
				return nil
			}
		}
		releasedSession, err := s.releaseQueuedAnswerUser(ctx, current)
		if err != nil {
			if errors.Is(err, errStaleAnswerUserQueue) {
				stopped, stopErr := s.stopStaleQueuedAnswerUser(ctx, current)
				if stopErr != nil {
					return stopErr
				}
				result = stopped
				stale = true
				return nil
			}
			return err
		}
		result = releasedSession
		released = true
		return nil
	})
	if err != nil {
		return domain.Session{}, false, false, err
	}
	return result, released, stale, nil
}

func (s *Service) waitAnswerUserQueueReleased(ctx context.Context, session domain.Session) (DTO, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if session.Status != domain.StatusQueued || session.Queue.Kind != domain.QueueKindAnswerUser {
			return toDTO(session), nil
		}
		current, released, _, err := s.tryReleaseQueuedAnswerUser(ctx, session.ID)
		if err != nil {
			return DTO{}, err
		}
		if released || current.Status != domain.StatusQueued || current.Queue.Kind != domain.QueueKindAnswerUser {
			return toDTO(current), nil
		}
		session = current
		select {
		case <-ctx.Done():
			return DTO{}, ctx.Err()
		case <-ticker.C:
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
	if options.resumeCodexSessionID != "" {
		return domain.QueueKindResume
	}
	return domain.QueueKindStart
}

func codexStartOptionsFromQueue(session domain.Session) codexStartOptions {
	nodeRunID := queueProcessNodeRunID(session.Queue.NodeRunID)
	return codexStartOptions{
		resumeCodexSessionID: session.Queue.ResumeCodexSessionID,
		workflowRunID:        session.Queue.WorkflowRunID,
		nodeRunID:            nodeRunID,
		prompt:               session.Queue.Prompt,
		workflowJSONRetry:    isWorkflowJSONRetryPrompt(session.Queue.Prompt),
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

func clearQueue(session *domain.Session) {
	session.QueuedAt = nil
	session.Queue = domain.QueueIntent{}
}

func (s *Service) startCodexProcess(ctx context.Context, session domain.Session, runID processdomain.RunID, options codexStartOptions, workdir string) (processdomain.CodexHandle, error) {
	attachmentPaths, imagePaths, err := s.codexAttachmentPaths(ctx, session.ID)
	if err != nil {
		return processdomain.CodexHandle{}, err
	}
	prompt := strings.TrimSpace(options.prompt)
	if prompt == "" && options.resumeCodexSessionID == "" {
		var err error
		prompt, err = s.rebuiltSessionPrompt(ctx, session, prompt, options.reviewAfterReuseFailure)
		if err != nil {
			return processdomain.CodexHandle{}, err
		}
	}
	prompt = promptWithSessionGuidance(prompt, session)
	if options.resumeCodexSessionID != "" {
		return s.codex.Resume(ctx, processdomain.CodexResumeInput{
			ProcessRunID:    runID,
			SessionID:       processdomain.SessionID(session.ID),
			CodexSessionID:  options.resumeCodexSessionID,
			Workdir:         workdir,
			Prompt:          prompt,
			Model:           strings.TrimSpace(session.Config.CodexModel),
			ReasoningEffort: strings.TrimSpace(session.Config.ReasoningEffort),
			PermissionMode:  strings.TrimSpace(session.Config.PermissionMode),
		})
	}
	return s.codex.Start(ctx, processdomain.CodexStartInput{
		ProcessRunID:    runID,
		SessionID:       processdomain.SessionID(session.ID),
		Workdir:         workdir,
		Prompt:          promptWithAttachments(prompt, attachmentPaths),
		Model:           strings.TrimSpace(session.Config.CodexModel),
		ReasoningEffort: strings.TrimSpace(session.Config.ReasoningEffort),
		PermissionMode:  strings.TrimSpace(session.Config.PermissionMode),
		AttachmentPaths: attachmentPaths,
		ImagePaths:      imagePaths,
	})
}

const rebuiltPromptNotice = "无法复用已有 Codex 会话，请基于以下上下文复查当前状态并继续处理。"
const answerUserPromptGuidance = "AnyCode 提供 `answer_user` MCP 工具，可用于向用户提出选项问题。若需求、验收标准、执行取舍或下一步不确定，请使用 `answer_user` 咨询用户；如果上下文足够明确，请直接继续执行，不要无意义打断用户。`request_user_input` 不是 AnyCode 会话内的用户提问工具，可能只属于外层平台或特定计划模式；即使你在说明中看到它，也不要使用 `request_user_input` 来代替 AnyCode 的 `answer_user`。"
const worktreePromptGuidance = "当前工作目录是 AnyCode 管理的卡片工作树。不得删除、移动、重建或清理当前工作树，也不得执行会移除该工作树的命令；卡片关闭时由 AnyCode 负责保存 Diff 快照并清理工作树。"

func promptWithSessionGuidance(prompt string, session domain.Session) string {
	prompt = promptWithAnswerUserGuidance(prompt)
	if strings.TrimSpace(session.BaseBranch) == "" || strings.Contains(prompt, worktreePromptGuidance) {
		return prompt
	}
	return prompt + "\n\n" + worktreePromptGuidance
}

func promptWithAnswerUserGuidance(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return answerUserPromptGuidance
	}
	if strings.Contains(prompt, answerUserPromptGuidance) {
		return prompt
	}
	return prompt + "\n\n" + answerUserPromptGuidance
}

func (s *Service) rebuiltSessionPrompt(ctx context.Context, session domain.Session, nodePrompt string, reviewAfterReuseFailure bool) (string, error) {
	original := strings.TrimSpace(session.Requirement)
	nodePrompt = strings.TrimSpace(nodePrompt)
	appends, err := s.repo.ListPromptAppends(ctx, session.ID)
	if err != nil {
		return "", fmt.Errorf("list prompt appends: %w", err)
	}
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
			return nodePrompt, nil
		}
		return original, nil
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
	return strings.Join(parts, "\n\n"), nil
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
	go func() {
		defer s.releaseWorkdir(workdir, session.ID)
		events, err := s.codex.Events(context.Background(), handle)
		if err != nil {
			return
		}
		exitResult := processdomain.ExitResult{}
		workflowResults := map[string]any(nil)
		for event := range events {
			if result, ok := exitResultFromEvent(event); ok {
				exitResult = result
			}
			if results, ok := workflowResultsFromEvent(event); ok {
				workflowResults = results
			}
			if event.Type == "process.exit" {
				continue
			}
			eventSession := session
			saveEventSession := false
			extraEvents := []sessionEventInput(nil)
			if todoList, ok := todoListFromCodexEvent(event); ok {
				if current, err := s.repo.Find(context.Background(), session.ID); err == nil {
					current.TodoList = todoList
					current.UpdatedAt = s.now()
					eventSession = current
					session = current
					saveEventSession = true
					extraEvents = append(extraEvents, sessionEventInput{
						eventType: "session.todo_list_updated",
						payload: map[string]any{
							"completed": todoList.Completed(),
							"total":     todoList.Total(),
						},
					})
				}
			}
			if codexSessionID := codexSessionIDFromEvent(event); codexSessionID != "" {
				current := eventSession
				if !saveEventSession {
					if latest, err := s.repo.Find(context.Background(), session.ID); err == nil {
						current = latest
					}
				}
				if current.CodexSessionID != codexSessionID {
					current.CodexSessionID = codexSessionID
					current.UpdatedAt = s.now()
					if err := s.saveProcessRunningSession(context.Background(), handle.ProcessRunID, handle.PID, codexSessionID, current); err == nil {
						session = current
						eventSession = current
					}
				}
			}
			_ = s.publishCodexEventWithSessionUpdates(context.Background(), eventSession, handle.ProcessRunID, event, saveEventSession, extraEvents...)
		}
		finishedAt := s.now()
		exitResult.FinishedAt = finishedAt
		_ = s.markProcessExitedWithSessionEvents(context.Background(), handle.ProcessRunID, exitResult, session, false, []sessionEventInput{
			{eventType: "process.exited", payload: processExitPayload(handle.ProcessRunID, exitResult)},
		})
		if s.sessionHasDifferentActiveRun(context.Background(), session.ID, handle.ProcessRunID) {
			s.scheduleQueueDrain()
			return
		}
		if current, err := s.repo.Find(context.Background(), session.ID); err == nil {
			if current.Mode == domain.ModeWorkflow && options.workflowRunID != "" && options.nodeRunID != nil {
				switch current.Status {
				case domain.StatusStopping, domain.StatusStopped, domain.StatusClosed:
					s.scheduleQueueDrain()
					return
				}
				if processExitFailed(exitResult) {
					s.failWorkflowAfterProcessExit(current, handle, options, exitResult)
					s.scheduleQueueDrain()
					return
				}
				s.advanceWorkflowAfterProcessExit(current, handle, options, workflowResults)
				s.scheduleQueueDrain()
				return
			}
			switch current.Status {
			case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping:
				if processExitFailed(exitResult) && current.Status != domain.StatusStopping {
					current.Status = domain.StatusFailed
				} else {
					current.Status = domain.StatusStopped
				}
				current.UpdatedAt = finishedAt
				eventType := "session.stopped"
				if current.Status == domain.StatusFailed {
					eventType = "session.failed"
				}
				payload := processExitPayload(handle.ProcessRunID, exitResult)
				payload["reason"] = "process_exited"
				_ = s.saveSessionWithEvent(context.Background(), current, eventType, payload)
			}
		}
		s.scheduleQueueDrain()
	}()
}

func (s *Service) failWorkflowAfterProcessExit(session domain.Session, handle processdomain.CodexHandle, options codexStartOptions, result processdomain.ExitResult) {
	if s.workflows == nil || options.nodeRunID == nil {
		return
	}
	nodeRunID := domain.NodeRunID(*options.nodeRunID)
	message := result.FailureReason
	if strings.TrimSpace(message) == "" {
		message = "codex process exited unsuccessfully"
	}
	advance, err := s.workflows.FailNode(context.Background(), domain.WorkflowNodeFailInput{
		WorkflowRunID: options.workflowRunID,
		NodeRunID:     nodeRunID,
		Code:          codexProcessFailureCode(result),
		Message:       message,
	})
	if err != nil {
		session.Status = domain.StatusFailed
		session.UpdatedAt = s.now()
		_ = s.saveSessionWithEvent(context.Background(), session, "workflow.failed", map[string]any{
			"workflowRunId": string(options.workflowRunID),
			"nodeRunId":     string(nodeRunID),
			"reason":        err.Error(),
		})
		return
	}
	_, _ = s.applyWorkflowAdvance(context.Background(), session, advance, workflowAdvanceOptions{})
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
	return result, true
}

func workflowResultsFromEvent(event processdomain.CodexEvent) (map[string]any, bool) {
	if !isAssistantOutputEvent(event) {
		return nil, false
	}
	for _, text := range eventTextCandidates(event.Payload) {
		if results, ok := workflowResultsFromText(text); ok {
			return results, true
		}
	}
	return nil, false
}

func isAssistantOutputEvent(event processdomain.CodexEvent) bool {
	eventType := strings.ToLower(strings.TrimSpace(event.Type))
	if strings.Contains(eventType, "agent_message") || strings.Contains(eventType, "assistant") {
		return true
	}
	if eventType == "item.completed" || eventType == "response.output_item.done" {
		if item, ok := event.Payload["item"].(map[string]any); ok {
			itemType := strings.ToLower(strings.TrimSpace(stringField(item, "type")))
			role := strings.ToLower(strings.TrimSpace(stringField(item, "role")))
			switch itemType {
			case "command_execution", "file_change":
				return false
			case "agent_message", "assistant_message", "message":
				return role == "" || role == "assistant"
			default:
				return role == "assistant"
			}
		}
	}
	return false
}

func stringField(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}

func eventTextCandidates(payload map[string]any) []string {
	candidates := []string{}
	if payload == nil {
		return candidates
	}
	if text, ok := payload["text"].(string); ok {
		candidates = append(candidates, text)
	}
	if text, ok := payload["content"].(string); ok {
		candidates = append(candidates, text)
	}
	if text, ok := payload["output"].(string); ok {
		candidates = append(candidates, text)
	}
	if message, ok := payload["message"].(map[string]any); ok {
		candidates = append(candidates, textCandidatesFromValue(message)...)
	}
	if item, ok := payload["item"].(map[string]any); ok {
		candidates = append(candidates, textCandidatesFromValue(item)...)
	}
	return candidates
}

func textCandidatesFromValue(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		candidates := []string{}
		for _, item := range typed {
			candidates = append(candidates, textCandidatesFromValue(item)...)
		}
		return candidates
	case map[string]any:
		candidates := []string{}
		for _, key := range []string{"text", "content", "output", "aggregated_output", "aggregatedOutput"} {
			candidates = append(candidates, textCandidatesFromValue(typed[key])...)
		}
		return candidates
	default:
		return nil
	}
}

func workflowResultsFromText(text string) (map[string]any, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	for _, candidate := range jsonObjectCandidates(text) {
		var value map[string]any
		if err := json.Unmarshal([]byte(candidate), &value); err != nil {
			continue
		}
		if results, ok := value["results"].(map[string]any); ok {
			return results, true
		}
		return value, true
	}
	return nil, false
}

func jsonObjectCandidates(text string) []string {
	candidates := []string{}
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 {
			candidates = append(candidates, strings.Join(lines[1:len(lines)-1], "\n"))
		}
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		candidates = append(candidates, text[start:end+1])
	}
	return candidates
}

func processExitPayload(processRunID processdomain.RunID, result processdomain.ExitResult) map[string]any {
	payload := map[string]any{"processRunId": string(processRunID)}
	if result.ExitCode != nil {
		payload["exitCode"] = *result.ExitCode
	}
	if result.FailureReason != "" {
		payload["failureReason"] = result.FailureReason
		payload["failureCode"] = codexProcessFailureCode(result)
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
	reason := strings.ToLower(result.FailureReason)
	if strings.Contains(reason, "model_reasoning_effort") ||
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

func (s *Service) advanceWorkflowAfterProcessExit(session domain.Session, handle processdomain.CodexHandle, options codexStartOptions, workflowResults map[string]any) {
	if s.workflows == nil {
		return
	}
	nodeRunID := domain.NodeRunID(*options.nodeRunID)
	output := map[string]any{
		"processRunId": string(handle.ProcessRunID),
		"exit":         "completed",
	}
	if options.workflowJSONRetry {
		output["jsonRetry"] = true
	}
	if workflowResults != nil {
		output["results"] = workflowResults
	}
	advance, err := s.workflows.CompleteNode(context.Background(), domain.WorkflowNodeCompleteInput{
		WorkflowRunID: options.workflowRunID,
		NodeRunID:     nodeRunID,
		Output:        output,
	})
	if err != nil {
		if appErr, ok := apperror.From(err); ok && appErr.Code == apperror.CodeWorkflowJSONRequired {
			_ = s.workflows.MarkStartFailed(context.Background(), domain.WorkflowStartFailureInput{
				WorkflowRunID: options.workflowRunID,
				NodeRunID:     &nodeRunID,
				Code:          appErr.Code,
				Message:       appErr.Error(),
			})
		}
		session.Status = domain.StatusFailed
		session.UpdatedAt = s.now()
		_ = s.saveSessionWithEvent(context.Background(), session, "workflow.failed", map[string]any{
			"workflowRunId": string(options.workflowRunID),
			"nodeRunId":     string(nodeRunID),
			"reason":        err.Error(),
		})
		return
	}
	_, _ = s.applyWorkflowAdvance(context.Background(), session, advance, workflowAdvanceOptions{})
}

func (s *Service) applyWorkflowAdvance(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance, advanceOptions workflowAdvanceOptions) (DTO, error) {
	switch {
	case advance.Blocked:
		session.Status = domain.StatusBlocked
		session.UpdatedAt = s.now()
		if err := s.saveSessionWithEvent(ctx, session, "session.blocked", map[string]any{
			"workflowRunId": string(advance.WorkflowRunID),
			"reason":        advance.BlockedReason,
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	case advance.Close:
		return s.closeSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: domain.CloseReasonWorkflowClosed})
	case advance.Completed:
		session.Status = domain.StatusCompleted
		session.UpdatedAt = s.now()
		if err := s.saveSessionWithEvent(ctx, session, "session.completed", map[string]any{
			"workflowRunId": string(advance.WorkflowRunID),
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	case advance.Merge != nil:
		return s.executeWorkflowMerge(ctx, session, advance)
	case advance.Expr != nil:
		return s.executeWorkflowExpr(ctx, session, advance)
	case !advance.RequiresCodex:
		session.Status = domain.StatusWaitingApproval
		session.UpdatedAt = s.now()
		if err := s.saveSessionWithEvent(ctx, session, "session.waiting_approval", map[string]any{
			"workflowRunId":    string(advance.WorkflowRunID),
			"nodeRunId":        stringValuePtr(advance.NodeRunID),
			"currentNodeId":    advance.CurrentNodeID,
			"currentNodeTitle": advance.CurrentNodeTitle,
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	default:
		options := codexStartOptions{
			workflowRunID:           advance.WorkflowRunID,
			nodeRunID:               workflowNodeRunID(advance.NodeRunID),
			prompt:                  advance.Prompt,
			workflowJSONRetry:       advance.RequireJSONRetry,
			reviewAfterReuseFailure: advanceOptions.forceNewCodexSession,
		}
		if session.CodexSessionID != "" && !advanceOptions.forceNewCodexSession {
			options.resumeCodexSessionID = session.CodexSessionID
		}
		dto, err := s.enqueueCodex(ctx, session, options, queuePriorityForSession(session))
		if err != nil {
			return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, advance.NodeRunID, "codex_start_failed", err.Error())
		}
		return dto, nil
	}
}

func (s *Service) executeWorkflowExpr(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance) (DTO, error) {
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
		Output:        map[string]any{"results": results},
	})
	if err != nil {
		return DTO{}, err
	}
	return s.applyWorkflowAdvance(ctx, session, next, workflowAdvanceOptions{})
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

func isWorkflowJSONRetryPrompt(prompt string) bool {
	return strings.Contains(prompt, "ANYCODE_WORKFLOW_JSON_RETRY")
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
	if recordErr := s.recordMergeResult(ctx, session, *advance.NodeRunID, result); recordErr != nil {
		return DTO{}, recordErr
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
		Output:        mergeOutput(result),
	})
	if err != nil {
		return DTO{}, err
	}
	if next.Completed {
		return s.closeSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: domain.CloseReasonMergedClosed})
	}
	return s.applyWorkflowAdvance(ctx, session, next, workflowAdvanceOptions{})
}

func (s *Service) recordMergeResult(ctx context.Context, session domain.Session, nodeRunID domain.NodeRunID, result gitdiffdomain.MergeResult) error {
	id, err := s.generateID()
	if err != nil {
		return fmt.Errorf("generate merge record id: %w", err)
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

func (s *Service) askMergeFailure(ctx context.Context, session domain.Session, advance domain.WorkflowAdvance, result gitdiffdomain.MergeResult, code string) (DTO, error) {
	if advance.NodeRunID == nil {
		return s.handleWorkflowNodeFailure(ctx, session, advance.WorkflowRunID, nil, code, result.FailureReason)
	}
	workflowRunID := questiondomain.WorkflowRunID(advance.WorkflowRunID)
	metadata := mergeFailureQuestionMetadata(advance, result, code)
	batch, err := s.questions.CreateBatch(ctx, questionapp.CreateBatchInput{
		SessionID:     questiondomain.SessionID(session.ID),
		WorkflowRunID: &workflowRunID,
		Questions: []questiondomain.Question{
			{
				Title:       "合并失败处理",
				Body:        mergeFailureQuestionBody(result),
				Type:        "merge_failure_action",
				AllowCustom: true,
				Metadata:    metadata,
				Status:      string(questiondomain.BatchPending),
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
						Description: "执行节点失败处理，按流程重试、失败分支或阻塞规则继续。",
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
	session.Status = domain.StatusWaitingUser
	session.UpdatedAt = s.now()
	if err := s.repo.Save(ctx, session); err != nil {
		if cancelErr := s.questions.CancelPendingBySession(ctx, questiondomain.SessionID(session.ID), "merge failure question abandoned"); cancelErr != nil {
			return DTO{}, fmt.Errorf("save session: %w; cancel merge failure question: %v", err, cancelErr)
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
	session, err := s.repo.Find(ctx, domain.ID(batch.SessionID))
	if err != nil {
		return fmt.Errorf("find session: %w", err)
	}
	workflowRunID := domain.WorkflowRunID(stringFromMap(metadata, "workflowRunId"))
	nodeRunIDValue := domain.NodeRunID(stringFromMap(metadata, "nodeRunId"))
	if workflowRunID == "" || nodeRunIDValue == "" {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "merge failure question metadata is incomplete").
			WithDetails(map[string]any{"batchId": string(batch.ID)})
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
		_, err := s.StopSession(ctx, session.ID)
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
	return map[string]any{
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
	}
}

func (s *Service) sessionHasDifferentActiveRun(ctx context.Context, sessionID domain.ID, runID processdomain.RunID) bool {
	if s.processes == nil {
		return false
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(sessionID))
	if err != nil || !ok {
		return false
	}
	return active.ID != runID
}

func (s *Service) handleWorkflowNodeFailure(ctx context.Context, session domain.Session, workflowRunID domain.WorkflowRunID, nodeRunID *domain.NodeRunID, code string, message string, output ...map[string]any) (DTO, error) {
	if s.workflows == nil || nodeRunID == nil {
		session.Status = domain.StatusFailed
		session.UpdatedAt = s.now()
		if err := s.repo.Save(ctx, session); err != nil {
			return DTO{}, fmt.Errorf("save session: %w", err)
		}
		return toDTO(session), nil
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

func (s *Service) publishCodexEventWithSessionUpdates(ctx context.Context, session domain.Session, processRunID processdomain.RunID, event processdomain.CodexEvent, saveSession bool, extraInputs ...sessionEventInput) error {
	var codexEvent eventdomain.DomainEvent
	publishCodexEvent := s.publisher != nil
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
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if saveSession {
				if err := tx.Sessions().Save(ctx, session); err != nil {
					return fmt.Errorf("save session: %w", err)
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
		return nil
	}
	if saveSession {
		if err := s.repo.Save(ctx, session); err != nil {
			return fmt.Errorf("save session: %w", err)
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

func (s *Service) newCodexSessionEvent(session domain.Session, _ processdomain.RunID, event processdomain.CodexEvent) (eventdomain.DomainEvent, error) {
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
		CreatedAt: createdAt,
	}, nil
}

func codexSessionEventPayload(codexSessionID string, event processdomain.CodexEvent) map[string]any {
	payload := processEventPayload(event)
	payload["codexType"] = event.Type
	payload["codexSessionId"] = codexSessionID
	if event.EventID != "" {
		payload["codexEventId"] = event.EventID
	}
	return payload
}

func (s *Service) createProcessRunWithSessionEvent(ctx context.Context, run processdomain.Run, session domain.Session, eventType string, payload map[string]any) error {
	if s.uow != nil {
		event, ok, err := s.newSessionEvent(session, eventType, payload)
		if err != nil {
			return err
		}
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().CreateRun(ctx, run); err != nil {
				return fmt.Errorf("create process run: %w", err)
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
	if err := s.processes.CreateRun(ctx, run); err != nil {
		return fmt.Errorf("create process run: %w", err)
	}
	return s.saveSessionWithEvent(ctx, session, eventType, payload)
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
	events, err := s.newSessionEvents(session, inputs)
	if err != nil {
		return err
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().MarkExited(ctx, runID, result); err != nil {
				return fmt.Errorf("mark process exited: %w", err)
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

func (s *Service) saveSessionWithEvent(ctx context.Context, session domain.Session, eventType string, payload map[string]any) error {
	event, ok, err := s.newSessionEvent(session, eventType, payload)
	if err != nil {
		return err
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
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
	if err := s.repo.Save(ctx, session); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	if ok {
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

func (s *Service) saveInterruptedSessionWithEvent(ctx context.Context, session domain.Session, finishedAt time.Time, eventType string, payload map[string]any) error {
	events, err := s.newSessionEvents(session, []sessionEventInput{{eventType: eventType, payload: payload}})
	if err != nil {
		return err
	}
	exitResult := processdomain.ExitResult{
		FailureReason: "service_restarted",
		FinishedAt:    finishedAt,
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := markInterruptedProcessExited(ctx, tx.Processes(), session, exitResult); err != nil {
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
	if err := markInterruptedProcessExited(ctx, s.processes, session, exitResult); err != nil {
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

func markInterruptedProcessExited(ctx context.Context, processes processdomain.Repository, session domain.Session, result processdomain.ExitResult) error {
	if processes == nil {
		return nil
	}
	active, ok, err := processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
	if err != nil {
		return fmt.Errorf("find interrupted process run: %w", err)
	}
	if !ok {
		return nil
	}
	if err := processes.MarkExited(ctx, active.ID, result); err != nil {
		return fmt.Errorf("mark interrupted process exited: %w", err)
	}
	return nil
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
		return eventdomain.DomainEvent{}, false, err
	}
	sessionID := eventdomain.SessionID(session.ID)
	eventPayload := copyPayload(payload)
	eventPayload["status"] = string(session.Status)
	return eventdomain.DomainEvent{
		ID: eventdomain.ID(id),
		Scope: eventdomain.Scope{
			SessionID: &sessionID,
			ProjectID: string(session.ProjectID),
		},
		SessionID: &sessionID,
		Type:      eventType,
		Payload:   eventPayload,
		CreatedAt: s.now(),
	}, true, nil
}

func (s *Service) publishSessionEvent(ctx context.Context, event eventdomain.DomainEvent) {
	if s.publisher != nil {
		_ = s.publisher.PublishAfterCommit(ctx, event)
	}
}

func processEventPayload(event processdomain.CodexEvent) map[string]any {
	return copyPayload(event.Payload)
}

func copyPayload(input map[string]any) map[string]any {
	payload := make(map[string]any, len(input))
	for key, value := range input {
		payload[key] = value
	}
	return payload
}

func todoListFromCodexEvent(event processdomain.CodexEvent) (domain.TodoList, bool) {
	if isTodoListPlanEventType(event.Type) {
		return todoListFromPayload(event.Payload)
	}
	if isTodoListItemEventType(event.Type) {
		return todoListFromTodoListItemPayload(event.Payload)
	}
	return todoListFromUpdatePlanToolPayload(event.Payload)
}

func isTodoListPlanEventType(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "plan_update", "turn/plan/updated", "turn.plan.updated", "plan.updated":
		return true
	default:
		return false
	}
}

func isTodoListItemEventType(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "item.started", "item.updated":
		return true
	default:
		return false
	}
}

func todoListFromUpdatePlanToolPayload(payload map[string]any) (domain.TodoList, bool) {
	if payload == nil {
		return domain.TodoList{}, false
	}
	if isUpdatePlanToolName(payload) {
		if list, ok := todoListFromToolArguments(payload); ok {
			return list, true
		}
	}
	for _, key := range []string{"item", "msg", "message", "params"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if list, found := todoListFromUpdatePlanToolPayload(nested); found {
				return list, true
			}
		}
	}
	return domain.TodoList{}, false
}

func isUpdatePlanToolName(payload map[string]any) bool {
	name := firstNonEmptyString(payload, "name", "tool", "tool_name", "toolName", "function_name", "functionName")
	if function, ok := payload["function"].(map[string]any); ok && name == "" {
		name = firstNonEmptyString(function, "name")
	}
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "update_plan" || strings.HasSuffix(name, ".update_plan")
}

func todoListFromToolArguments(payload map[string]any) (domain.TodoList, bool) {
	for _, key := range []string{"arguments", "input"} {
		switch value := payload[key].(type) {
		case string:
			var nested map[string]any
			if err := json.Unmarshal([]byte(value), &nested); err == nil {
				if list, found := todoListFromPayload(nested); found {
					return list, true
				}
			}
		case map[string]any:
			if list, found := todoListFromPayload(value); found {
				return list, true
			}
		}
	}
	return todoListFromPayload(payload)
}

func todoListFromTodoListItemPayload(payload map[string]any) (domain.TodoList, bool) {
	if payload == nil {
		return domain.TodoList{}, false
	}
	item := payload
	if nested, ok := payload["item"].(map[string]any); ok {
		item = nested
	}
	if strings.ToLower(strings.TrimSpace(stringFromMap(item, "type"))) != "todo_list" {
		return domain.TodoList{}, false
	}
	return todoListFromPayload(item)
}

func todoListFromPayload(payload map[string]any) (domain.TodoList, bool) {
	if payload == nil {
		return domain.TodoList{}, false
	}
	for _, key := range []string{"plan", "todoList", "todo_list", "todos", "items"} {
		if items, ok := payload[key].([]any); ok {
			return todoListFromItems(items), true
		}
	}
	for _, key := range []string{"item", "msg", "message", "params"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if list, found := todoListFromPayload(nested); found {
				return list, true
			}
		}
	}
	return domain.TodoList{}, false
}

func todoListFromItems(items []any) domain.TodoList {
	list := domain.TodoList{Items: make([]domain.TodoItem, 0, len(items))}
	for _, item := range items {
		typed, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text := firstNonEmptyString(typed, "step", "text", "title", "content")
		if text == "" {
			continue
		}
		list.Items = append(list.Items, domain.TodoItem{
			Text:      text,
			Completed: todoItemCompleted(typed),
		})
	}
	return list
}

func firstNonEmptyString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromMap(input, key); value != "" {
			return value
		}
	}
	return ""
}

func todoItemCompleted(item map[string]any) bool {
	if completed, ok := item["completed"].(bool); ok {
		return completed
	}
	switch strings.ToLower(stringFromMap(item, "status")) {
	case "complete", "completed", "done", "success", "succeeded":
		return true
	default:
		return false
	}
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
	var dto DTO
	err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
		var err error
		dto, err = s.closeSession(ctx, input)
		return err
	})
	return dto, err
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
		if err := s.deleteSessionBranch(ctx, session); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	}
	requiresStop, err := closeRequiresStop(ctx, s, session)
	if err != nil {
		return DTO{}, err
	}
	if requiresStop {
		if _, err := s.stopSession(ctx, session.ID); err != nil {
			return DTO{}, err
		}
		session, err = s.repo.Find(ctx, input.SessionID)
		if err != nil {
			return DTO{}, fmt.Errorf("find stopped session: %w", err)
		}
	}
	var cleanupErr error
	if s.worktrees != nil && strings.TrimSpace(session.WorktreePath) != "" {
		project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
		if err != nil {
			return DTO{}, fmt.Errorf("find project for session worktree cleanup: %w", err)
		}
		if project.IsGit {
			exists, err := s.worktrees.Exists(ctx, session.WorktreePath)
			if err != nil {
				return DTO{}, apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "check session worktree existence failed").WithDetails(map[string]any{
					"sessionId": string(session.ID),
				}).WithRetryable(true)
			}
			if !exists {
				if strings.TrimSpace(session.WorktreeBaseCommit) == "" {
					return DTO{}, apperror.New(apperror.CodeCloseFailed, apperror.CategoryInfraError, "session worktree is missing before diff snapshot was fully stored").WithDetails(map[string]any{
						"sessionId": string(session.ID),
					})
				}
				if strings.TrimSpace(session.WorktreeHeadCommit) == "" {
					branch := worktreeBranchName(session.ID)
					headCommit, err := s.worktrees.HeadCommit(ctx, project.Path.Value, branch)
					if err != nil {
						return DTO{}, apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "read missing session worktree branch head failed").WithDetails(map[string]any{
							"sessionId":      string(session.ID),
							"worktreeBranch": branch,
						}).WithRetryable(true)
					}
					session.WorktreeHeadCommit = strings.TrimSpace(headCommit)
					if session.WorktreeHeadCommit == "" {
						return DTO{}, apperror.New(apperror.CodeCloseFailed, apperror.CategoryInfraError, "session worktree is missing before diff snapshot was fully stored").WithDetails(map[string]any{
							"sessionId":      string(session.ID),
							"worktreeBranch": branch,
						})
					}
				}
			} else {
				if strings.TrimSpace(session.WorktreeBaseCommit) == "" {
					baseCommit, err := s.worktrees.MergeBase(ctx, session.WorktreePath, session.BaseBranch)
					if err != nil {
						return DTO{}, apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "read session worktree merge base failed").WithDetails(map[string]any{
							"sessionId": string(session.ID),
						}).WithRetryable(true)
					}
					session.WorktreeBaseCommit = baseCommit
				}
				headCommit, err := s.worktrees.SnapshotCommit(ctx, session.WorktreePath, worktreeBranchName(session.ID))
				if err != nil {
					return DTO{}, apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "snapshot session worktree failed").WithDetails(map[string]any{
						"sessionId": string(session.ID),
					}).WithRetryable(true)
				}
				session.WorktreeHeadCommit = headCommit
				if err := s.saveSession(ctx, session); err != nil {
					return DTO{}, err
				}
				if err := s.worktrees.Remove(ctx, session.WorktreePath); err != nil {
					return DTO{}, apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "remove session worktree failed").WithDetails(map[string]any{
						"sessionId": string(session.ID),
					}).WithRetryable(true)
				}
			}
			session.WorktreePath = ""
			if err := s.saveSession(ctx, session); err != nil {
				return DTO{}, err
			}
			if err := s.worktrees.DeleteBranch(ctx, project.Path.Value, worktreeBranchName(session.ID)); err != nil {
				cleanupErr = apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "delete session worktree branch failed").WithDetails(map[string]any{
					"sessionId":      string(session.ID),
					"worktreeBranch": worktreeBranchName(session.ID),
				}).WithRetryable(true)
			}
		}
	}
	now := s.now()
	session.Status = domain.StatusClosed
	session.CloseReason = &reason
	session.ClosedAt = &now
	session.UpdatedAt = now
	clearQueue(&session)
	if err := s.saveSessionWithEvent(ctx, session, "session.closed", map[string]any{
		"reason": string(reason),
	}); err != nil {
		return DTO{}, err
	}
	if err := s.cancelPendingQuestions(ctx, session.ID, "session closed"); err != nil {
		return DTO{}, err
	}
	if cleanupErr != nil {
		return DTO{}, cleanupErr
	}
	return toDTO(session), nil
}

func (s *Service) deleteSessionBranch(ctx context.Context, session domain.Session) error {
	if s == nil || s.worktrees == nil || strings.TrimSpace(session.BaseBranch) == "" {
		return nil
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
	if err != nil {
		return fmt.Errorf("find project for session branch cleanup: %w", err)
	}
	if !project.IsGit {
		return nil
	}
	if err := s.worktrees.DeleteBranch(ctx, project.Path.Value, worktreeBranchName(session.ID)); err != nil {
		return apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "delete session worktree branch failed").WithDetails(map[string]any{
			"sessionId":      string(session.ID),
			"worktreeBranch": worktreeBranchName(session.ID),
		}).WithRetryable(true)
	}
	return nil
}

func closeRequiresStop(ctx context.Context, s *Service, session domain.Session) (bool, error) {
	switch session.Status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusStopping, domain.StatusResumeFailed:
		return true, nil
	case domain.StatusQueued:
		if s == nil || s.processes == nil {
			return false, nil
		}
		_, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
		if err != nil {
			return false, fmt.Errorf("find active process run: %w", err)
		}
		return ok, nil
	case domain.StatusWaitingUser:
		if s == nil || s.processes == nil || s.codex == nil {
			return false, nil
		}
		_, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
		if err != nil {
			return false, fmt.Errorf("find active process run: %w", err)
		}
		return ok, nil
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
			if err == nil {
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
			CreatedAt:   s.now(),
			Attachments: archivedAttachments,
		}
		if err := s.repo.AppendPrompt(ctx, append); err != nil {
			return fmt.Errorf("append prompt: %w", err)
		}
		appendSaved = true
		if !canAutoStartAfterAppend(session) {
			return nil
		}
		if canReuseCodexSessionAfterAppend(session) {
			newAttachments, err := s.listPromptAppendAttachments(ctx, input.SessionID, append.ID)
			if err != nil {
				return err
			}
			newAttachmentPaths, _ := attachmentPathsFromAttachments(newAttachments)
			prompt := promptWithAttachments(body, newAttachmentPaths)
			if _, err := s.resumeSession(ctx, input.SessionID, StartSessionOptions{prompt: prompt, resumeCodexSessionID: session.CodexSessionID}); err != nil {
				return fmt.Errorf("resume session after prompt append: %w", err)
			}
			return nil
		}
		if _, err := s.startSession(ctx, input.SessionID, StartSessionOptions{}); err != nil {
			return fmt.Errorf("start session after prompt append: %w", err)
		}
		return nil
	})
	if err != nil {
		return PromptAppendDTO{}, err
	}
	return toPromptAppendDTO(append), nil
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
	result, err := s.workflows.SubmitApprovalForSession(ctx, domain.WorkflowApprovalInput{
		WorkflowRunID: input.WorkflowRunID,
		NodeID:        input.NodeID,
		Approved:      input.Approved,
		Comment:       input.Comment,
	})
	if err != nil {
		return WorkflowRunDTO{}, fmt.Errorf("submit workflow approval: %w", err)
	}
	session, err := s.repo.Find(ctx, result.Run.SessionID)
	if err != nil {
		return WorkflowRunDTO{}, fmt.Errorf("find session: %w", err)
	}
	if session.Mode != domain.ModeWorkflow {
		return WorkflowRunDTO{}, fmt.Errorf("session %q is not workflow mode", session.ID)
	}
	s.appendSessionEvent(ctx, session, "workflow.approval_submitted", map[string]any{
		"workflowRunId": string(input.WorkflowRunID),
		"nodeId":        input.NodeID,
		"approved":      input.Approved,
	})
	if _, err := s.applyWorkflowAdvance(ctx, session, result.Advance, workflowAdvanceOptions{}); err != nil {
		return WorkflowRunDTO{}, err
	}
	return toWorkflowRunDTO(result.Run), nil
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
	currentNodeTitle, err := s.currentNodeTitle(ctx, session)
	if err != nil {
		return DetailDTO{}, err
	}
	return toDetailDTO(session, attachments, appends, currentNodeTitle), nil
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
	currentNodeTitle, err := s.currentNodeTitle(ctx, session)
	if err != nil {
		return CardDTO{}, err
	}
	card := toCardDTO(session, attachments, currentNodeTitle)
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
		currentNodeTitle, err := s.currentNodeTitle(ctx, session)
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
		WorktreeHeadCommit: session.WorktreeHeadCommit,
		CodexSessionID:     session.CodexSessionID,
		Config:             session.Config,
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
	return worktreeBranchName(session.ID)
}

func worktreeBranchName(sessionID domain.ID) string {
	return strings.TrimSpace(string(sessionID))
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

func (s *Service) currentNodeTitle(ctx context.Context, session domain.Session) (string, error) {
	if s.events == nil || session.Mode != domain.ModeWorkflow {
		return "", nil
	}
	sessionID := eventdomain.SessionID(session.ID)
	events, err := s.events.After(ctx, eventdomain.Scope{ProjectID: string(session.ProjectID), SessionID: &sessionID}, "")
	if err != nil {
		return "", fmt.Errorf("list session events for current node: %w", err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if !strings.HasPrefix(events[i].Type, "workflow.") {
			continue
		}
		if title, ok := events[i].Payload["currentNodeTitle"].(string); ok {
			return strings.TrimSpace(title), nil
		}
	}
	return "", nil
}

func toCardDTO(session domain.Session, attachments []domain.SessionAttachment, currentNodeTitle string) CardDTO {
	return CardDTO{
		DTO:                toDTO(session),
		RequirementSummary: session.Requirement,
		CurrentNodeTitle:   currentNodeTitle,
		PendingQuestion:    session.Status == domain.StatusWaitingUser,
		TodoList:           session.TodoList,
		Attachments:        attachments,
		AvailableActions:   availableActions(session),
	}
}

func toDetailDTO(session domain.Session, attachments []domain.SessionAttachment, appends []domain.PromptAppend, currentNodeTitle string) DetailDTO {
	promptAppends := make([]PromptAppendDTO, 0, len(appends))
	for _, promptAppend := range appends {
		promptAppends = append(promptAppends, toPromptAppendDTO(promptAppend))
	}
	return DetailDTO{
		DTO:              toDTO(session),
		CloseReason:      session.CloseReason,
		CurrentNodeTitle: currentNodeTitle,
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
	switch session.Status {
	case domain.StatusCreated, domain.StatusStopped, domain.StatusFailed, domain.StatusCompleted:
		actions := []string{"run", "close"}
		if canResume(session) {
			actions = []string{"run", "resume", "close"}
		}
		return actions
	case domain.StatusQueued:
		return []string{"run", "stop", "close"}
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping:
		return []string{"stop"}
	case domain.StatusWaitingApproval:
		return []string{"close"}
	case domain.StatusBlocked:
		return []string{"close"}
	case domain.StatusResumeFailed:
		actions := []string{"run", "stop", "close"}
		if canResume(session) {
			actions = []string{"run", "resume", "stop", "close"}
		}
		return actions
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
		(session.Status == domain.StatusStopped || session.Status == domain.StatusResumeFailed)
}

func generateID() (domain.ID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return domain.ID(hex.EncodeToString(b[:])), nil
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
