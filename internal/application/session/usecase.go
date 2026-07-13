package session

import (
	"context"
	"crypto/rand"
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
	MarkInterruptedSessionsRecoverable(ctx context.Context) (int, error)
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
	CloseSession(ctx context.Context, input CloseSessionInput) (DTO, error)
	UpdateSessionConfig(ctx context.Context, input UpdateSessionConfigInput) (DTO, error)
	RequestUserAnswer(ctx context.Context, input RequestUserAnswerInput) (questionapp.BatchDTO, error)
	SubmitQuestionBatch(ctx context.Context, input questionapp.SubmitBatchInput) (questionapp.BatchDTO, error)
	AppendPrompt(ctx context.Context, input AppendPromptInput) (PromptAppendDTO, error)
	UpdatePromptAppend(ctx context.Context, input UpdatePromptAppendInput) (PromptAppendDTO, error)
	SubmitWorkflowApproval(ctx context.Context, input SubmitWorkflowApprovalInput) (WorkflowRunDTO, error)
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
}

const (
	defaultPage              = 1
	defaultPageSize          = 20
	maxPageSize              = 100
	processCleanupTimeout    = 5 * time.Second
	processExitRetryMaxDelay = 30 * time.Second

	maxSessionIDAttempts = 100
)

var ErrProcessLifecycleNotWired = errors.New("session process lifecycle is not wired")

type workflowApprovalPostCommitAdvance struct {
	session domain.Session
	advance domain.WorkflowAdvance
}

var (
	errWorkdirBusy           = errors.New("session workdir already has an active process")
	errProcessCleanupPending = errors.New("codex process may still be running")
)
var errWorkflowResumeStateNotPersisted = errors.New("workflow resume failure state was not persisted")
var errCloseRequiresStop = errors.New("session must stop before close")
var fallbackEventSequence atomic.Uint64

type Service struct {
	repo                domain.Repository
	uow                 port.UnitOfWork
	locker              port.SessionLocker
	projects            projectdomain.Repository
	attachments         domain.AttachmentRepository
	files               domain.AttachmentStore
	worktrees           domain.WorktreeManager
	worktreeInitializer domain.WorktreeInitializer
	workflows           domain.WorkflowStarter
	merge               gitdiffdomain.MergePort
	processes           processdomain.Repository
	codex               processdomain.CodexProcess
	processConsumers    sync.Map
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
	processExitDelay    func(int) time.Duration
	lifecycleCtx        context.Context
	lifecycleCancel     context.CancelFunc
}

type questionCoordinator interface {
	CreateBatch(ctx context.Context, input questionapp.CreateBatchInput) (questionapp.BatchDTO, error)
	CancelPendingBySession(ctx context.Context, sessionID questiondomain.SessionID, reason string) error
}

type answerQuestionCoordinator interface {
	questionCoordinator
	SubmitBatch(ctx context.Context, input questionapp.SubmitBatchInput) (questionapp.BatchDTO, error)
	GetBatch(ctx context.Context, id questiondomain.BatchID) (questionapp.BatchDTO, error)
	PublishBatch(batch questionapp.BatchDTO)
}

type workflowApprovalRepositoryRunner interface {
	SubmitApprovalForSessionWithRepositories(ctx context.Context, input domain.WorkflowApprovalInput, repo workflowdomain.Repository, events eventdomain.Store) (domain.WorkflowApprovalResult, []eventdomain.DomainEvent, error)
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
		lifecycleCtx:        lifecycleCtx,
		lifecycleCancel:     lifecycleCancel,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Close() {
	if s != nil && s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}
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
	answerSessions, err := s.recoverAnswerUserSessions(ctx)
	if err != nil {
		return 0, err
	}
	recoveredCount := len(answerSessions)
	sessions, err := s.repo.ListInterruptedWithCodexSession(ctx)
	if err != nil {
		return 0, fmt.Errorf("list interrupted sessions: %w", err)
	}
	now := s.now()
	for _, session := range sessions {
		if answerSessions[session.ID] {
			continue
		}
		recovered, err := s.recoverWorkflowProcessExit(ctx, session, now)
		if err != nil {
			return 0, fmt.Errorf("recover workflow process exit for session %s: %w", session.ID, err)
		}
		if recovered {
			recoveredCount++
			continue
		}
		previousStatus := session.Status
		if err := transitionSession(&session, domain.StatusStopped, now); err != nil {
			return 0, err
		}
		if err := s.saveInterruptedSessionWithEvent(ctx, session, now, "service_restarted", "session.recoverable", map[string]any{
			"reason":         "service_restarted",
			"previousStatus": string(previousStatus),
			"codexSessionId": session.CodexSessionID,
		}); err != nil {
			return 0, fmt.Errorf("mark session %s recoverable: %w", session.ID, err)
		}
		recoveredCount++
	}
	unresumableSessions, err := s.listInterruptedWithoutCodexSession(ctx)
	if err != nil {
		return 0, err
	}
	for _, session := range unresumableSessions {
		if answerSessions[session.ID] {
			continue
		}
		recovered, err := s.recoverWorkflowProcessExit(ctx, session, now)
		if err != nil {
			return 0, fmt.Errorf("recover workflow process exit for session %s: %w", session.ID, err)
		}
		if recovered {
			recoveredCount++
			continue
		}
		previousStatus := session.Status
		if err := transitionSession(&session, domain.StatusResumeFailed, now); err != nil {
			return 0, err
		}
		if err := s.saveInterruptedSessionWithEvent(ctx, session, now, "service_restarted", "session.resume_failed", map[string]any{
			"reason":         "service_restarted_without_codex_session_id",
			"previousStatus": string(previousStatus),
		}); err != nil {
			return 0, fmt.Errorf("mark session %s resume failed: %w", session.ID, err)
		}
		recoveredCount++
	}
	return recoveredCount, nil
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
			if session.Status == domain.StatusStopping {
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
	return result.ID != "", nil
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
			if transitionErr := transitionSession(&session, domain.StatusFailed, failedAt); transitionErr != nil {
				return DTO{}, transitionErr
			}
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
	if project.IsGit && strings.TrimSpace(project.WorktreeInitCommand) != "" {
		if err := s.initializeWorktree(ctx, session, project.WorktreeInitCommand); err != nil {
			return DTO{}, err
		}
	}
	if _, err := s.archiveStagedAttachments(ctx, id, domain.AttachmentSourceRequirement, string(id), stagedAttachments); err != nil {
		failedAt := s.now()
		if transitionErr := transitionSession(&session, domain.StatusFailed, failedAt); transitionErr != nil {
			return DTO{}, transitionErr
		}
		_ = s.repo.Save(ctx, session)
		if createdWorktree {
			if cleanupErr := s.cleanupCreatedWorktree(ctx, project.Path.Value, worktreePath, id); cleanupErr != nil {
				return DTO{}, errors.Join(err, fmt.Errorf("cleanup created worktree: %w", cleanupErr))
			}
		}
		return DTO{}, err
	}
	if mode == domain.ModeWorkflow {
		dto, startErr := s.startWorkflowSession(ctx, session, domain.WorkflowDefinitionID(*project.DefaultWorkflowID), true)
		if startErr != nil {
			failedAt := s.now()
			if transitionErr := transitionSession(&session, domain.StatusFailed, failedAt); transitionErr != nil {
				return DTO{}, transitionErr
			}
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
	return s.enqueueCodex(ctx, session, codexStartOptions{initialStart: true}, queuePriorityForSession(session))
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
	if session.Status == domain.StatusResumeFailed {
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
		return s.startWorkflowSession(ctx, session, domain.WorkflowDefinitionID(*project.DefaultWorkflowID), session.Status == domain.StatusCreated)
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
	options := codexStartOptions{initialStart: session.Status == domain.StatusCreated}
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
}

func (s *Service) startWorkflowSession(ctx context.Context, session domain.Session, workflowDefinitionID domain.WorkflowDefinitionID, initialStart bool) (DTO, error) {
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
		initialStart:      initialStart,
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
	if session.Status == domain.StatusStopped {
		cleanupCtx, cancel := detachedCleanupContext(ctx)
		defer cancel()
		if err := s.cancelPendingQuestions(cleanupCtx, session.ID, "session stopped"); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	}
	switch session.Status {
	case domain.StatusQueued, domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping, domain.StatusResumeFailed:
	default:
		return DTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session cannot stop from current status").WithDetails(map[string]any{"status": string(session.Status)})
	}
	if session.Status == domain.StatusQueued && session.Queue.Kind != domain.QueueKindAnswerUser {
		return s.stopSessionWithoutActiveProcess(ctx, session, "queue_cancelled", false)
	}
	if s.processes == nil || s.codex == nil {
		return DTO{}, apperror.Wrap(ErrProcessLifecycleNotWired, apperror.CodeCodexStartFailed, apperror.CategoryInfraError, "session process lifecycle is not wired")
	}
	active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(id))
	if err != nil {
		return DTO{}, fmt.Errorf("find active process run: %w", err)
	}
	if !ok {
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
		if options.prompt == "" {
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

func (s *Service) RequestUserAnswer(ctx context.Context, input RequestUserAnswerInput) (questionapp.BatchDTO, error) {
	if s == nil {
		return questionapp.BatchDTO{}, errors.New("session usecase: nil service")
	}
	if input.SessionID == "" || len(input.Questions) == 0 {
		return questionapp.BatchDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session id and questions are required")
	}
	var batch questionapp.BatchDTO
	var origin processdomain.Run
	err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
		var err error
		batch, origin, err = s.requestUserAnswer(ctx, input)
		if err != nil {
			return err
		}
		stopCtx, stopCancel := detachedCleanupContext(ctx)
		stopErr := s.codex.Stop(stopCtx, origin.ID)
		stopCancel()
		if stopErr != nil && !errors.Is(stopErr, processdomain.ErrProcessNotFound) {
			rollbackCtx, rollbackCancel := detachedCleanupContext(ctx)
			defer rollbackCancel()
			rollbackErr := s.rollbackUserAnswerSuspension(rollbackCtx, batch.ID, origin)
			return errors.Join(fmt.Errorf("suspend codex for answer_user: %w", stopErr), rollbackErr)
		}
		return nil
	})
	if err != nil {
		return questionapp.BatchDTO{}, err
	}
	cleanupCtx, cancel := detachedCleanupContext(ctx)
	defer cancel()
	hadConsumer := false
	if done, ok := s.processConsumerDone(origin.ID); ok {
		hadConsumer = true
		select {
		case <-cleanupCtx.Done():
			return questionapp.BatchDTO{}, fmt.Errorf("wait for answer_user suspension: %w", cleanupCtx.Err())
		case <-done:
		}
	}
	if !hadConsumer {
		if err := s.withSessionLock(cleanupCtx, input.SessionID, func(ctx context.Context) error {
			_, _, err := s.persistCodexProcessExit(ctx, domain.Session{ID: input.SessionID}, processdomain.CodexHandle{
				ProcessRunID: origin.ID,
				PID:          intValue(origin.PID),
			}, codexStartOptions{}, processdomain.ExitResult{
				FailureReason: "suspended for user answer",
				FinishedAt:    s.now(),
			}, nil)
			return err
		}); err != nil {
			return questionapp.BatchDTO{}, fmt.Errorf("persist answer_user suspension: %w", err)
		}
	}
	return batch, nil
}

func (s *Service) rollbackUserAnswerSuspension(ctx context.Context, batchID questiondomain.BatchID, origin processdomain.Run) error {
	var cancelled questiondomain.Batch
	var events []eventdomain.DomainEvent
	if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		repo, ok := tx.Questions().(questiondomain.AgentRepository)
		if !ok {
			return errors.New("agent question repository is required")
		}
		batch, transitioned, err := repo.CancelPendingBatch(ctx, batchID, "answer_user process suspension failed")
		if err != nil {
			return err
		}
		if !transitioned {
			return fmt.Errorf("question batch %s cannot be cancelled after suspension failure", batchID)
		}
		cancelled = batch
		session, err := tx.Sessions().Find(ctx, domain.ID(batch.SessionID))
		if err != nil {
			return err
		}
		if err := transitionSession(&session, domain.StatusRunning, s.now()); err != nil {
			return err
		}
		if err := tx.Processes().MarkRunning(ctx, origin.ID, intValue(origin.PID), origin.CodexSessionID); err != nil {
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
			{eventType: "question.cancelled", payload: map[string]any{"batchId": string(batch.ID), "reason": "process_suspension_failed"}},
			{eventType: "session.running", payload: map[string]any{"processRunId": string(origin.ID), "reason": "answer_user_suspension_failed"}},
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
	}); err != nil {
		return fmt.Errorf("rollback answer_user suspension: %w", err)
	}
	s.publishQuestionBatch(questionBatchDTO(cancelled))
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
		return err
	})
	if err != nil {
		return questionapp.BatchDTO{}, err
	}
	s.publishQuestionBatch(batch)
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
		if err := repo.MarkDeliveryAwaitingResume(ctx, persisted.ID); err != nil {
			return err
		}
		persisted.DeliveryStatus = questiondomain.DeliveryAwaitingResume
		finder, ok := tx.Processes().(processdomain.RunFinder)
		if !ok {
			return errors.New("process run finder is required")
		}
		origin, err := finder.FindRun(ctx, processdomain.RunID(*persisted.OriginProcessRunID))
		if err != nil {
			return err
		}
		if strings.TrimSpace(origin.CodexSessionID) == "" {
			return apperror.New(apperror.CodeResumeFailed, apperror.CategoryCodexError, "origin process has no Codex session id").WithRetryable(true)
		}
		session, err := tx.Sessions().Find(ctx, domain.ID(persisted.SessionID))
		if err != nil {
			return err
		}
		options := codexStartOptions{
			resumeCodexSessionID: origin.CodexSessionID,
			resumeOfProcessRunID: origin.ID,
			answerBatchID:        persisted.ID,
			prompt:               answerResumePrompt(persisted),
		}
		if persisted.WorkflowRunID != nil {
			options.workflowRunID = domain.WorkflowRunID(*persisted.WorkflowRunID)
		}
		if origin.NodeRunID != nil {
			options.nodeRunID = origin.NodeRunID
		}
		queued, event, hasEvent, err := s.prepareQueuedSession(session, options, domain.QueuePriorityHigh, domain.QueueKindAnswerUser)
		if err != nil {
			return err
		}
		if err := tx.Sessions().Save(ctx, queued); err != nil {
			return err
		}
		answerEvent, hasAnswerEvent, err := s.newSessionEvent(queued, "session.answer_resume_queued", map[string]any{
			"batchId": string(persisted.ID), "originProcessRunId": string(origin.ID),
		})
		if err != nil {
			return err
		}
		if hasEvent {
			if err := tx.Events().Append(ctx, event); err != nil {
				return err
			}
			publishedEvents = append(publishedEvents, event)
		}
		if hasAnswerEvent {
			if err := tx.Events().Append(ctx, answerEvent); err != nil {
				return err
			}
			publishedEvents = append(publishedEvents, answerEvent)
		}
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

type codexStartOptions struct {
	resumeCodexSessionID    string
	resumeOfProcessRunID    processdomain.RunID
	answerBatchID           questiondomain.BatchID
	workflowRunID           domain.WorkflowRunID
	nodeRunID               *processdomain.NodeRunID
	prompt                  string
	promptAppendIDs         []string
	workflowJSONRetry       bool
	reviewAfterReuseFailure bool
	initialStart            bool
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
			return s.queueCodex(ctx, session, options, queuePriorityForStartOptions(session, options), queueKindForStartOptions(options))
		}
	}
	dto, err := s.startCodexWithWorkdirReservation(ctx, session, options)
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

func (s *Service) startCodexNow(ctx context.Context, session domain.Session, options codexStartOptions, workdir string) (DTO, error) {
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
	run := processdomain.Run{
		ID:        runID,
		SessionID: processdomain.SessionID(session.ID),
		NodeRunID: options.nodeRunID,
		Status:    processdomain.StatusStarting,
		StartedAt: now,
	}
	if options.resumeOfProcessRunID != "" {
		value := options.resumeOfProcessRunID
		run.ResumeOf = &value
	}
	if err := transitionSession(&session, domain.StatusStarting, now); err != nil {
		return DTO{}, err
	}
	if err := s.createProcessRunWithSessionEvent(ctx, run, session, options, "session.starting", map[string]any{"processRunId": string(runID)}); err != nil {
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
	if handle.CodexSessionID != "" {
		session.CodexSessionID = handle.CodexSessionID
	}
	if err := s.markProcessRunningWithSessionEvent(ctx, runID, handle.PID, handle.CodexSessionID, session, "session.running", map[string]any{
		"processRunId":   string(runID),
		"pid":            handle.PID,
		"codexSessionId": handle.CodexSessionID,
	}); err != nil {
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

func (s *Service) startCodexWithWorkdirReservation(ctx context.Context, session domain.Session, options codexStartOptions) (DTO, error) {
	workdir, err := s.codexWorkdir(ctx, session)
	if err != nil {
		return DTO{}, err
	}
	if !s.reserveWorkdir(workdir, session.ID) {
		return DTO{}, errWorkdirBusy
	}
	dto, err := s.startCodexNow(ctx, session, options, workdir)
	if err != nil && !errors.Is(err, errProcessCleanupPending) {
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
			if _, err := s.startCodexWithWorkdirReservation(ctx, current, codexStartOptionsFromQueue(current)); err != nil {
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
		reviewAfterReuseFailure: session.Queue.ReviewAfterReuseFailure,
		workflowJSONRetry:       isWorkflowJSONRetryPrompt(session.Queue.Prompt),
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
	// GLUE: InitialStart remains in QueueIntent for prompt guidance; remove when workflow state owns it.
	session.Queue = domain.QueueIntent{InitialStart: initialStart}
	return nil
}

func (s *Service) startCodexProcess(ctx context.Context, session domain.Session, runID processdomain.RunID, options codexStartOptions, workdir string) (processdomain.CodexHandle, error) {
	attachmentPaths, imagePaths, err := s.codexAttachmentPaths(ctx, session.ID)
	if err != nil {
		return processdomain.CodexHandle{}, err
	}
	prompt := strings.TrimSpace(options.prompt)
	if options.initialStart {
		prompt = promptWithSessionGuidance(prompt, session)
	}
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
	if options.resumeCodexSessionID != "" {
		if session.Mode != domain.ModeWorkflow && options.answerBatchID == "" && len(pendingIDs) == 0 {
			return codexStartOptions{}, pendingPromptRequiredError(session.ID)
		}
		prompt = joinPromptParts(prompt, pendingPrompt)
	} else if session.Mode != domain.ModeWorkflow || options.reviewAfterReuseFailure {
		prompt = rebuiltSessionPrompt(session, prompt, options.reviewAfterReuseFailure, appends)
	} else {
		prompt = joinPromptParts(prompt, pendingPrompt)
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
const answerUserPromptGuidance = "AnyCode 提供 `answer_user` MCP 工具，可用于向用户提出选项问题。若需求、验收标准、执行取舍或下一步不确定，请使用 `answer_user` 咨询用户；如果上下文足够明确，请直接继续执行，不要无意义打断用户。`request_user_input` 不是 AnyCode 会话内的用户提问工具，可能只属于外层平台或特定计划模式；即使你在说明中看到它，也不要使用 `request_user_input` 来代替 AnyCode 的 `answer_user`。"
const worktreePromptGuidance = "当前工作目录是 AnyCode 管理的卡片工作树。不得删除、移动、重建或清理当前工作树，也不得执行会移除该工作树的命令；若必须手动合并，请使用当前卡片分支名执行非 fast-forward merge，并保留 Git 默认合并提交信息，以便工作树缺失时从基础分支日志恢复 Diff；卡片关闭时由 AnyCode 负责清理仍存在的工作树。"

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
			s.persistCodexEventWithRetry(session.ID, handle, event)
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

func (s *Service) persistCodexEventWithRetry(sessionID domain.ID, handle processdomain.CodexHandle, event processdomain.CodexEvent) {
	retryAcknowledgement := codexEventAcknowledgesPrompt(event)
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
			return
		}
		err := s.withSessionLock(retryCtx, sessionID, func(ctx context.Context) error {
			return s.persistCodexEvent(ctx, sessionID, handle, event)
		})
		if err == nil {
			return
		}
		if !retryAcknowledgement {
			log.Printf("persist codex event: session=%s process_run=%s type=%s error=%v", sessionID, handle.ProcessRunID, event.Type, err)
			return
		}
		timer := time.NewTimer(retryDelay(attempt))
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return
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
	if s.processes != nil {
		active, ok, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(sessionID))
		if err != nil {
			return fmt.Errorf("find active process for codex event: %w", err)
		}
		activeRun = ok && active.ID == handle.ProcessRunID
	}
	saveSession := false
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
		if codexSessionID := codexSessionIDFromEvent(event); codexSessionID != "" && current.CodexSessionID != codexSessionID {
			current.CodexSessionID = codexSessionID
			current.UpdatedAt = s.now()
			if err := s.saveProcessRunningSession(ctx, handle.ProcessRunID, handle.PID, codexSessionID, current); err != nil {
				return err
			}
			saveSession = false
		}
	}
	promptDelivered := codexEventAcknowledgesPrompt(event)
	return s.publishCodexEventWithSessionUpdates(ctx, current, handle.ProcessRunID, event, saveSession, promptDelivered, extraEvents...)
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

func (s *Service) workflowAdvanceAfterProcessExit(ctx context.Context, handle processdomain.CodexHandle, options codexStartOptions, result processdomain.ExitResult, workflowResults map[string]any) (domain.WorkflowAdvance, error) {
	if s.workflows == nil || options.nodeRunID == nil {
		return domain.WorkflowAdvance{}, errors.New("workflow process exit is missing workflow state")
	}
	input := workflowProcessExitInput(handle, options, result, workflowResults)
	advance, err := s.workflows.RecoverProcessExit(ctx, input)
	if err != nil {
		if appErr, ok := apperror.From(err); ok && appErr.Code == apperror.CodeWorkflowJSONRequired {
			nodeRunID := input.NodeRunID
			_ = s.workflows.MarkStartFailed(ctx, domain.WorkflowStartFailureInput{
				WorkflowRunID: options.workflowRunID,
				NodeRunID:     &nodeRunID,
				Code:          appErr.Code,
				Message:       appErr.Error(),
			})
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
	if options.workflowJSONRetry {
		output["jsonRetry"] = true
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
	if session.Queue.Kind == "" {
		session.Queue = domain.QueueIntent{}
	}
	switch {
	case advance.Blocked:
		if err := transitionSession(&session, domain.StatusBlocked, s.now()); err != nil {
			return DTO{}, err
		}
		if err := s.saveSessionWithEvent(ctx, session, "session.blocked", map[string]any{
			"workflowRunId": string(advance.WorkflowRunID),
			"reason":        advance.BlockedReason,
		}); err != nil {
			return DTO{}, err
		}
		return toDTO(session), nil
	case advance.Close:
		return s.closeWorkflowSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: domain.CloseReasonWorkflowClosed})
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
			initialStart:            initialStart,
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
		Output:        map[string]any{"results": results},
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
		return s.closeWorkflowSession(ctx, CloseSessionInput{SessionID: session.ID, Reason: domain.CloseReasonMergedClosed})
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
	if err := transitionSession(&session, domain.StatusWaitingUser, s.now()); err != nil {
		return DTO{}, err
	}
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

func (s *Service) publishCodexEventWithSessionUpdates(ctx context.Context, session domain.Session, processRunID processdomain.RunID, event processdomain.CodexEvent, saveSession bool, promptDelivered bool, extraInputs ...sessionEventInput) error {
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

func (s *Service) createProcessRunWithSessionEvent(ctx context.Context, run processdomain.Run, session domain.Session, options codexStartOptions, eventType string, payload map[string]any) error {
	if s.uow != nil {
		event, ok, err := s.newSessionEvent(session, eventType, payload)
		if err != nil {
			return err
		}
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := tx.Processes().CreateRun(ctx, run); err != nil {
				return fmt.Errorf("create process run: %w", err)
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
			if err := tx.Sessions().Save(ctx, session); err != nil {
				return fmt.Errorf("save session: %w", err)
			}
			if err := tx.Sessions().MarkPromptAppendsInflight(ctx, options.promptAppendIDs, string(run.ID)); err != nil {
				return err
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
	if options.answerBatchID != "" {
		return errors.New("answer_user process creation requires a unit of work")
	}
	if err := s.processes.CreateRun(ctx, run); err != nil {
		return fmt.Errorf("create process run: %w", err)
	}
	if err := s.repo.MarkPromptAppendsInflight(ctx, options.promptAppendIDs, string(run.ID)); err != nil {
		_ = s.processes.MarkExited(ctx, run.ID, processdomain.ExitResult{FailureReason: err.Error(), FinishedAt: s.now()})
		return err
	}
	if err := s.saveSessionWithEvent(ctx, session, eventType, payload); err != nil {
		result := processdomain.ExitResult{FailureReason: err.Error(), FinishedAt: s.now()}
		_ = s.processes.MarkExited(ctx, run.ID, result)
		_ = s.repo.ReleasePromptAppends(ctx, string(run.ID))
		return err
	}
	return nil
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
			if err := tx.Processes().MarkExited(ctx, runID, result); err != nil {
				return fmt.Errorf("mark process exited: %w", err)
			}
			if repo, ok := tx.Questions().(questiondomain.AgentRepository); ok {
				batches, err := repo.ResetDeliveryAwaitingResumeByProcessRun(ctx, questiondomain.ProcessRunID(runID))
				if err != nil {
					return err
				}
				resetBatches = batches
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
	}, true
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
		var dto DTO
		err := s.withSessionLock(ctx, input.SessionID, func(ctx context.Context) error {
			var err error
			dto, err = s.closeSession(ctx, input)
			return err
		})
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
		return toDTO(session), nil
	}
	requiresStop, err := closeRequiresStop(ctx, s, session)
	if err != nil {
		return DTO{}, err
	}
	if requiresStop {
		return DTO{}, errCloseRequiresStop
	}
	if session.Status == domain.StatusWaitingUser || session.Queue.Kind == domain.QueueKindAnswerUser {
		now := s.now()
		if err := transitionSession(&session, domain.StatusStopped, now); err != nil {
			return DTO{}, err
		}
		if err := s.saveSessionWithEvent(ctx, session, "session.stopped", map[string]any{"reason": "closing_without_active_process"}); err != nil {
			return DTO{}, err
		}
	}
	if err := s.cancelPendingQuestions(ctx, session.ID, "session closed"); err != nil {
		return DTO{}, err
	}
	closedPayload := map[string]any{"reason": string(reason)}
	if strings.TrimSpace(session.BaseBranch) != "" && strings.TrimSpace(session.WorktreePath) != "" {
		if s.worktrees == nil {
			return DTO{}, apperror.New(apperror.CodeCloseFailed, apperror.CategoryInfraError, "session worktree manager is required").WithDetails(map[string]any{
				"sessionId": string(session.ID),
			})
		}
		exists, err := s.worktrees.Exists(ctx, session.WorktreePath)
		if err != nil {
			return DTO{}, apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "check session worktree existence failed").WithDetails(map[string]any{
				"sessionId": string(session.ID),
			}).WithRetryable(true)
		}
		var project projectdomain.Project
		if exists {
			project, err = s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
			if err != nil {
				return DTO{}, fmt.Errorf("find project for session worktree cleanup: %w", err)
			}
			if project.IsGit {
				if err := s.worktrees.Remove(ctx, session.WorktreePath); err != nil {
					return DTO{}, apperror.Wrap(err, apperror.CodeCloseFailed, apperror.CategoryInfraError, "remove session worktree failed").WithDetails(map[string]any{
						"sessionId": string(session.ID),
					}).WithRetryable(true)
				}
			}
		} else {
			project, err = s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
			if err != nil {
				closedPayload["branchCleanupFailed"] = true
				closedPayload["branchCleanupError"] = fmt.Sprintf("find project for branch cleanup: %v", err)
			}
		}
		if project.IsGit {
			if err := s.worktrees.DeleteBranch(ctx, project.Path.Value, worktreeBranchName(session.ID)); err != nil {
				closedPayload["branchCleanupFailed"] = true
				closedPayload["branchCleanupError"] = err.Error()
			}
		}
		session.WorktreePath = ""
	}
	now := s.now()
	if err := session.Close(reason, now); err != nil {
		return DTO{}, fmt.Errorf("close session %s: %w", session.ID, err)
	}
	if err := s.saveSessionWithEvent(ctx, session, "session.closed", closedPayload); err != nil {
		return DTO{}, err
	}
	return toDTO(session), nil
}

func (s *Service) closeWorkflowSession(ctx context.Context, input CloseSessionInput) (DTO, error) {
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return DTO{}, fmt.Errorf("find workflow session to close: %w", err)
	}
	requiresStop, err := closeRequiresStop(ctx, s, session)
	if err != nil {
		return DTO{}, err
	}
	if requiresStop {
		if s.processes == nil {
			return DTO{}, errors.New("session process repository is required")
		}
		_, active, err := s.processes.FindActiveBySession(ctx, processdomain.SessionID(session.ID))
		if err != nil {
			return DTO{}, fmt.Errorf("find active workflow process before close: %w", err)
		}
		if active {
			return DTO{}, errCloseRequiresStop
		}
		if err := transitionSession(&session, domain.StatusStopped, s.now()); err != nil {
			return DTO{}, err
		}
		if err := s.saveSessionWithEvent(ctx, session, "session.stopped", map[string]any{"reason": "workflow_closing_without_active_process"}); err != nil {
			return DTO{}, err
		}
	}
	return s.closeSession(ctx, input)
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
			options := codexStartOptions{}
			kind := domain.QueueKindStart
			if codexSessionID := strings.TrimSpace(session.CodexSessionID); codexSessionID != "" {
				options.resumeCodexSessionID = codexSessionID
				kind = domain.QueueKindResume
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
			if _, err := s.resumeSession(ctx, input.SessionID, StartSessionOptions{resumeCodexSessionID: session.CodexSessionID}); err != nil {
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
			queued, queuedEvent, hasQueuedEvent, err := s.queueApprovalRejectionPrompt(ctx, tx, session, approvalResult.Advance, strings.TrimSpace(input.Comment))
			if err != nil {
				return err
			}
			session = queued
			if hasQueuedEvent {
				publishEvents = append(publishEvents, queuedEvent)
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
			postCommitAdvance = &workflowApprovalPostCommitAdvance{
				session: session,
				advance: approvalResult.Advance,
			}
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
		if _, err := s.applyWorkflowAdvance(ctx, postCommitAdvance.session, postCommitAdvance.advance, workflowAdvanceOptions{}); err != nil {
			return WorkflowRunDTO{}, err
		}
	}
	s.scheduleQueueDrain()
	return toWorkflowRunDTO(result.Run), nil
}

func (s *Service) queueApprovalAdvance(ctx context.Context, tx port.Tx, session domain.Session, advance domain.WorkflowAdvance) (domain.Session, eventdomain.DomainEvent, bool, error) {
	options := codexStartOptions{
		workflowRunID: advance.WorkflowRunID,
		nodeRunID:     workflowNodeRunID(advance.NodeRunID),
		prompt:        advance.Prompt,
		initialStart:  session.Queue.InitialStart,
	}
	queued, event, hasEvent, err := s.prepareQueuedSession(session, options, queuePriorityForSession(session), domain.QueueKindStart)
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
	return &PendingApprovalDTO{
		WorkflowRunID:    domain.WorkflowRunID(workflowRunID),
		NodeID:           strings.TrimSpace(nodeID),
		NodeRunID:        strings.TrimSpace(nodeRunID),
		CurrentNodeTitle: strings.TrimSpace(title),
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
