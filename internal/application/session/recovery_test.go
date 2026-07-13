package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	questionapp "github.com/nzlov/anycode/internal/application/question"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

func TestRecoverInterruptedSessionsQueuesResumeAndSettlesStopping(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	running := domain.Session{
		ID: "running", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-running", WorktreePath: "/workspace/running", Priority: domain.PriorityHigh,
	}
	stopping := domain.Session{
		ID: "stopping", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopping,
		CodexSessionID: "codex-stopping", WorktreePath: "/workspace/stopping",
	}
	starting := domain.Session{
		ID: "starting", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStarting,
		CodexSessionID: "codex-starting", WorktreePath: "/workspace/starting",
	}
	repo.sessions[running.ID] = running
	repo.sessions[stopping.ID] = stopping
	repo.sessions[starting.ID] = starting
	repo.interruptedSessions = []domain.Session{running, stopping, starting}
	processes := newFakeProcessRepository()
	processes.activeBySession = map[processdomain.SessionID]processdomain.Run{
		"running": {
			ID: "process-running", SessionID: "running", Status: processdomain.StatusRunning,
			PID: intPointer(101), CodexSessionID: "codex-running",
		},
		"stopping": {
			ID: "process-stopping", SessionID: "stopping", Status: processdomain.StatusStopping,
			PID: intPointer(102), CodexSessionID: "codex-stopping",
		},
	}
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(&fakeEventStore{}), WithSessionLocker(NewMemorySessionLocker()))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-recovery", nil }

	count, err := service.RecoverInterruptedSessions(ctx)
	if err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	if count != 3 {
		t.Fatalf("RecoverInterruptedSessions() = %d, want 3", count)
	}
	gotRunning := repo.sessions[running.ID]
	if gotRunning.Status != domain.StatusQueued || gotRunning.Queue.Kind != domain.QueueKindResume || gotRunning.Queue.ResumeCodexSessionID != "codex-running" {
		t.Fatalf("running recovery = %#v", gotRunning)
	}
	if !strings.Contains(gotRunning.Queue.Prompt, "service restart") {
		t.Fatalf("running recovery prompt = %q", gotRunning.Queue.Prompt)
	}
	if gotStopping := repo.sessions[stopping.ID]; gotStopping.Status != domain.StatusStopped {
		t.Fatalf("stopping recovery = %#v", gotStopping)
	}
	if gotStarting := repo.sessions[starting.ID]; gotStarting.Status != domain.StatusQueued || gotStarting.Queue.Kind != domain.QueueKindResume {
		t.Fatalf("starting recovery = %#v", gotStarting)
	}
	if len(codex.detachedStops) != 2 {
		t.Fatalf("detached stops = %#v", codex.detachedStops)
	}
}

func TestRecoverInterruptedSessionFailsClosedWhenOwnerCannotBeVerified(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: intPointer(101)}
	processes.hasActive = true
	codex := &fakeCodexProcess{detachedErr: processdomain.ErrProcessOwnershipUnverified}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(&fakeEventStore{}))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	if got := repo.sessions[session.ID]; got.Status != domain.StatusResumeFailed || got.Queue.Kind != "" {
		t.Fatalf("fail-closed recovery = %#v", got)
	}
	if !processes.hasActive || processes.exitedID != "" {
		t.Fatalf("unverified process was settled: active=%v exited=%q", processes.hasActive, processes.exitedID)
	}
}

func TestRecoverInterruptedSessionCommitsRunQueueAndEventInOneTransaction(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: intPointer(101)}
	processes.hasActive = true
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithEvents(events), WithUnitOfWork(uow))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	if uow.calls != 1 {
		t.Fatalf("unit of work calls = %d, want 1", uow.calls)
	}
	if processes.exitedID != "process-1" || repo.sessions[session.ID].Status != domain.StatusQueued {
		t.Fatalf("transaction result: exited=%q session=%#v", processes.exitedID, repo.sessions[session.ID])
	}
	gotEvents := events.snapshot()
	if len(gotEvents) != 2 || gotEvents[0].Type != "process.exited" || gotEvents[1].Type != "session.queued" {
		t.Fatalf("transaction events = %#v", gotEvents)
	}
}

func TestRecoverInterruptedWorkflowKeepsCurrentNodeAttempt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning,
		CodexSessionID: "codex-1", WorktreePath: "/workspace/session-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	nodeRunID := domain.NodeRunID("node-run-1")
	workflows := &fakeWorkflowStarter{resumeNodeAdvance: domain.WorkflowAdvance{
		WorkflowRunID: "workflow-run-1", NodeRunID: &nodeRunID, CurrentNodeID: "build", RequiresCodex: true, Prompt: "Build",
	}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID: "process-1", SessionID: "session-1", NodeRunID: processNodeRunID("node-run-1"), Status: processdomain.StatusRunning,
		PID: intPointer(101), CodexSessionID: "codex-1",
	}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithWorkflows(workflows), WithEvents(&fakeEventStore{}))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusQueued || got.Queue.WorkflowRunID != "workflow-run-1" || got.Queue.NodeRunID == nil || *got.Queue.NodeRunID != nodeRunID {
		t.Fatalf("workflow recovery = %#v", got)
	}
	if workflows.resumeNodeInput.SessionID != session.ID {
		t.Fatalf("workflow resume input = %#v", workflows.resumeNodeInput)
	}
}

func processNodeRunID(value string) *processdomain.NodeRunID {
	id := processdomain.NodeRunID(value)
	return &id
}

func TestRecoverWaitingUserPersistsAndDeliversAnswerContinuation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusWaitingUser,
		CodexSessionID: "codex-1", WorktreePath: "/workspace/session-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	questions := &fakeQuestionRecoveryRepository{batch: questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchPending, Delivery: questiondomain.DeliveryPending,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice"}},
	}}
	processes := newFakeProcessRepository()
	events := &fakeEventStore{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{}),
		WithEvents(events),
		WithQuestionRecovery(questions),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	drainSchedules := 0
	service.queueDrainScheduler = func(*Service) { drainSchedules++ }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }

	if count, err := service.RecoverInterruptedSessions(ctx); err != nil || count != 1 {
		t.Fatalf("RecoverInterruptedSessions() = %d, %v", count, err)
	}
	if got := repo.sessions[session.ID]; got.Status != domain.StatusWaitingUser || got.Queue.Kind != "" {
		t.Fatalf("pending answer recovery session = %#v", got)
	}
	if questions.batch.Delivery != questiondomain.DeliveryRecoveryRequired {
		t.Fatalf("pending answer delivery = %q", questions.batch.Delivery)
	}
	if drainSchedules != 0 {
		t.Fatalf("startup recovery scheduled queue drain %d times", drainSchedules)
	}
	firstEventCount := len(events.snapshot())
	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("second RecoverInterruptedSessions() error = %v", err)
	}
	if len(events.snapshot()) != firstEventCount {
		t.Fatalf("second coordinator appended events: %#v", events.snapshot())
	}

	optionID := questiondomain.OptionID("approve")
	questions.batch.Status = questiondomain.BatchAnswered
	questions.batch.Questions[0].SelectedOptionID = &optionID
	questions.batch.Questions[0].CustomAnswer = "ship it"
	questions.batch.Questions[0].Answer = map[string]any{"approved": true}
	answered := questionapp.BatchDTO{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchAnswered,
		Delivery: questiondomain.DeliveryRecoveryRequired, Questions: questions.batch.Questions,
	}
	if err := service.HandleQuestionBatchAnswered(ctx, answered); err != nil {
		t.Fatalf("HandleQuestionBatchAnswered() error = %v", err)
	}
	queued := repo.sessions[session.ID]
	if queued.Status != domain.StatusQueued || queued.Queue.Kind != domain.QueueKindResume || queued.Queue.RecoveryBatchID != "batch-1" {
		t.Fatalf("answered recovery queue = %#v", queued)
	}
	for _, fragment := range []string{`"batchId":"batch-1"`, `"selectedOptionId":"approve"`, `"customAnswer":"ship it"`, `"approved":true`} {
		if !strings.Contains(queued.Queue.Prompt, fragment) {
			t.Fatalf("recovery prompt missing %q: %s", fragment, queued.Queue.Prompt)
		}
	}
	if questions.batch.Delivery != questiondomain.DeliveryRecoveryQueued {
		t.Fatalf("answered delivery = %q", questions.batch.Delivery)
	}
	if drainSchedules != 1 {
		t.Fatalf("answered recovery scheduled queue drain %d times, want 1", drainSchedules)
	}

	running := queued
	if err := transitionSession(&running, domain.StatusStarting, time.Unix(101, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	if err := transitionSession(&running, domain.StatusRunning, time.Unix(102, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	repo.sessions[session.ID] = running
	processes.active = processdomain.Run{ID: "process-2", SessionID: "session-1", Status: processdomain.StatusRunning, PID: intPointer(202), CodexSessionID: "codex-1"}
	processes.hasActive = true
	if err := service.persistCodexEventForRecovery(ctx, session.ID, processdomain.CodexHandle{
		ProcessRunID: "process-2", PID: 202, CodexSessionID: "codex-1",
	}, processdomain.CodexEvent{Type: "turn.started", CreatedAt: time.Unix(103, 0).UTC()}, "batch-1"); err != nil {
		t.Fatalf("persist recovery acknowledgement: %v", err)
	}
	if questions.batch.Delivery != questiondomain.DeliveryDelivered {
		t.Fatalf("acknowledged delivery = %q", questions.batch.Delivery)
	}
}

func TestRecoverPendingQueuedAnswerUserRestoresWaitingUser(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(90, 0).UTC()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusQueued,
		CodexSessionID: "codex-1", QueuedAt: &queuedAt,
		Queue: domain.QueueIntent{Kind: domain.QueueKindAnswerUser, Priority: domain.QueuePriorityImmediate},
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	questions := &fakeQuestionRecoveryRepository{batch: questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchPending, Delivery: questiondomain.DeliveryPending,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice"}},
	}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}), WithQuestionRecovery(questions))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusWaitingUser || got.Queue.Kind != "" || got.QueuedAt != nil {
		t.Fatalf("queued answer_user recovery = %#v", got)
	}
	if questions.batch.Delivery != questiondomain.DeliveryRecoveryRequired {
		t.Fatalf("pending answer delivery = %q", questions.batch.Delivery)
	}
}

func TestRecoverPendingMergeFailureQuestionDoesNotCreateCodexContinuation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusWaitingUser,
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	questions := &fakeQuestionRecoveryRepository{batch: questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchPending, Delivery: questiondomain.DeliveryPending,
		Questions: []questiondomain.Question{{
			ID: "question-1", BatchID: "batch-1", Type: "merge_failure_action",
			Metadata: map[string]any{"kind": "merge_failure_action", "workflowRunId": "workflow-run-1", "nodeRunId": "node-run-1"},
		}},
	}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}), WithQuestionRecovery(questions))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusWaitingUser || got.Queue.Kind != "" {
		t.Fatalf("pending merge recovery = %#v", got)
	}
	if questions.batch.Delivery != questiondomain.DeliveryPending {
		t.Fatalf("merge delivery = %q", questions.batch.Delivery)
	}
}

func TestRecoverAnsweredMergeFailureQuestionAppliesPersistedDecision(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusWaitingUser,
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	stopOption := questiondomain.OptionID("stop_session")
	questions := &fakeQuestionRecoveryRepository{batch: questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchAnswered, Delivery: questiondomain.DeliveryPending,
		Questions: []questiondomain.Question{{
			ID: "question-1", BatchID: "batch-1", Type: "merge_failure_action", SelectedOptionID: &stopOption,
			Metadata: map[string]any{"kind": "merge_failure_action", "workflowRunId": "workflow-run-1", "nodeRunId": "node-run-1"},
			Options:  []questiondomain.Option{{ID: stopOption, Payload: map[string]any{"action": "stop_session"}}},
		}},
	}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}), WithQuestionRecovery(questions))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusStopped || got.Queue.Kind != "" {
		t.Fatalf("answered merge recovery = %#v", got)
	}
	if questions.batch.Delivery != questiondomain.DeliveryPending {
		t.Fatalf("merge delivery = %q", questions.batch.Delivery)
	}
}

func TestRecoverAnsweredWorkflowQuestionKeepsNodeRun(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusWaitingUser,
		CodexSessionID: "codex-1",
	}
	repo.sessions[session.ID] = session
	nodeRunID := domain.NodeRunID("node-run-1")
	workflows := &fakeWorkflowStarter{resumeNodeAdvance: domain.WorkflowAdvance{
		WorkflowRunID: "workflow-run-1", NodeRunID: &nodeRunID, CurrentNodeID: "build", RequiresCodex: true,
	}}
	questions := &fakeQuestionRecoveryRepository{batch: questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchAnswered, Delivery: questiondomain.DeliveryRecoveryRequired,
		Questions: []questiondomain.Question{{ID: "question-1", SelectedOptionID: questionOptionID("yes")}},
	}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}), WithWorkflows(workflows), WithQuestionRecovery(questions))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if err := service.HandleQuestionBatchAnswered(ctx, questionapp.BatchDTO{
		ID: questions.batch.ID, SessionID: questions.batch.SessionID, Status: questions.batch.Status,
		Delivery: questions.batch.Delivery, Questions: questions.batch.Questions,
	}); err != nil {
		t.Fatalf("HandleQuestionBatchAnswered() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Queue.WorkflowRunID != "workflow-run-1" || got.Queue.NodeRunID == nil || *got.Queue.NodeRunID != nodeRunID {
		t.Fatalf("workflow answer recovery = %#v", got)
	}
	if workflows.resumeNodeInput.SessionID != session.ID {
		t.Fatalf("workflow resume input = %#v", workflows.resumeNodeInput)
	}
}

func TestRecoverQueuesAnswerPersistedBeforeRestart(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusWaitingUser,
		CodexSessionID: "codex-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	questions := &fakeQuestionRecoveryRepository{batch: questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchAnswered, Delivery: questiondomain.DeliveryPending,
		Questions: []questiondomain.Question{{ID: "question-1", CustomAnswer: "continue", Answer: map[string]any{"custom": true}}},
	}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}), WithQuestionRecovery(questions))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusQueued || got.Queue.RecoveryBatchID != "batch-1" || !strings.Contains(got.Queue.Prompt, `"custom":true`) {
		t.Fatalf("persisted answer recovery = %#v", got)
	}
	if questions.batch.Delivery != questiondomain.DeliveryRecoveryQueued {
		t.Fatalf("persisted answer delivery = %q", questions.batch.Delivery)
	}
}

func TestResumeProcessRunReferencesPreviousRun(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	processes := newFakeProcessRepository()
	processes.created = []processdomain.Run{{ID: "process-1", SessionID: "session-1", Status: processdomain.StatusExited}}
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 202, CodexSessionID: "codex-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.generateID = func() (domain.ID, error) { return "process-2", nil }

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSessionWithOptions() error = %v", err)
	}
	if len(processes.created) != 2 || processes.created[1].ResumeOf == nil || *processes.created[1].ResumeOf != "process-1" {
		t.Fatalf("process runs = %#v", processes.created)
	}
}

func TestRetryResumeDoesNotQueueWhileDetachedProcessOwnershipIsUnverified(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusResumeFailed,
		CodexSessionID: "codex-1",
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: intPointer(101)}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{detachedErr: processdomain.ErrProcessOwnershipUnverified}))

	if _, err := service.ResumeSession(ctx, "session-1"); !errors.Is(err, processdomain.ErrProcessOwnershipUnverified) {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	got := repo.sessions["session-1"]
	if got.Status != domain.StatusResumeFailed || got.Queue.Kind != "" {
		t.Fatalf("resume failure session = %#v", got)
	}
	if !processes.hasActive || processes.exitedID != "" {
		t.Fatalf("unverified process changed: active=%v exited=%q", processes.hasActive, processes.exitedID)
	}
}

func TestRetryResumeSettlesExitedDetachedProcessBeforeQueueing(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusResumeFailed,
		CodexSessionID: "codex-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: intPointer(101)}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	got, err := service.ResumeSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	queued := repo.sessions["session-1"]
	if got.Status != domain.StatusQueued || queued.Queue.Kind != domain.QueueKindResume {
		t.Fatalf("queued retry = %#v saved=%#v", got, queued)
	}
	if processes.hasActive || processes.exitedID != "process-1" {
		t.Fatalf("settled process: active=%v exited=%q", processes.hasActive, processes.exitedID)
	}
}

func TestRetryResumeRequeuesUnacknowledgedAnswerContinuation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusResumeFailed,
		CodexSessionID: "codex-1",
	}
	questions := &fakeQuestionRecoveryRepository{batch: questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchAnswered, Delivery: questiondomain.DeliveryRecoveryQueued,
		Questions: []questiondomain.Question{{ID: "question-1", CustomAnswer: "retry this answer"}},
	}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}), WithQuestionRecovery(questions))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	got, err := service.ResumeSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued || repo.sessions["session-1"].Queue.RecoveryBatchID != "batch-1" {
		t.Fatalf("retry recovery = %#v saved=%#v", got, repo.sessions["session-1"])
	}
	if !strings.Contains(repo.sessions["session-1"].Queue.Prompt, "retry this answer") {
		t.Fatalf("retry prompt = %q", repo.sessions["session-1"].Queue.Prompt)
	}
}

func questionOptionID(value string) *questiondomain.OptionID {
	id := questiondomain.OptionID(value)
	return &id
}

type fakeQuestionRecoveryRepository struct {
	batch    questiondomain.Batch
	setCalls []questiondomain.DeliveryStatus
}

func (r *fakeQuestionRecoveryRepository) CreateBatch(_ context.Context, batch questiondomain.Batch) error {
	r.batch = batch
	return nil
}

func (r *fakeQuestionRecoveryRepository) FindBatch(_ context.Context, id questiondomain.BatchID) (questiondomain.Batch, error) {
	if r.batch.ID != id {
		return questiondomain.Batch{}, fmt.Errorf("question batch %s not found", id)
	}
	return r.batch, nil
}

func (r *fakeQuestionRecoveryRepository) ListPendingBySession(_ context.Context, sessionID questiondomain.SessionID) ([]questiondomain.Batch, error) {
	if r.batch.SessionID == sessionID && r.batch.Status == questiondomain.BatchPending {
		return []questiondomain.Batch{r.batch}, nil
	}
	return nil, nil
}

func (r *fakeQuestionRecoveryRepository) SubmitAnswers(_ context.Context, id questiondomain.BatchID, _ []questiondomain.Answer) (questiondomain.Batch, bool, error) {
	if r.batch.ID != id {
		return questiondomain.Batch{}, false, fmt.Errorf("question batch %s not found", id)
	}
	return r.batch, false, nil
}

func (r *fakeQuestionRecoveryRepository) CancelPendingBySession(_ context.Context, _ questiondomain.SessionID, _ string) ([]questiondomain.Batch, error) {
	return nil, nil
}

func (r *fakeQuestionRecoveryRepository) FindLatestBySession(_ context.Context, sessionID questiondomain.SessionID) (questiondomain.Batch, bool, error) {
	if r.batch.SessionID != sessionID || r.batch.ID == "" {
		return questiondomain.Batch{}, false, nil
	}
	return r.batch, true, nil
}

func (r *fakeQuestionRecoveryRepository) SetDeliveryStatus(_ context.Context, id questiondomain.BatchID, status questiondomain.DeliveryStatus) (questiondomain.Batch, bool, error) {
	if r.batch.ID != id {
		return questiondomain.Batch{}, false, fmt.Errorf("question batch %s not found", id)
	}
	changed := r.batch.Delivery != status
	r.batch.Delivery = status
	r.setCalls = append(r.setCalls, status)
	return r.batch, changed, nil
}
