package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
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
	if uow.calls != 2 {
		t.Fatalf("unit of work calls = %d, want answer recovery scan plus atomic session recovery", uow.calls)
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

func TestRecoverInterruptedSessionDoesNotReplaceNewActiveRun(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-1", WorktreePath: "/workspace/session-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	processes := newFakeProcessRepository()
	oldRun := processdomain.Run{
		ID: "process-old", SessionID: "session-1", Status: processdomain.StatusRunning,
		PID: intPointer(101), CodexSessionID: "codex-1",
	}
	newRun := processdomain.Run{
		ID: "process-new", SessionID: "session-1", Status: processdomain.StatusRunning,
		PID: intPointer(202), CodexSessionID: "codex-1",
	}
	processes.active = oldRun
	processes.hasActive = true
	codex := &fakeCodexProcess{detachedHook: func(processdomain.DetachedProcess) {
		processes.mu.Lock()
		processes.active = newRun
		processes.hasActive = true
		processes.mu.Unlock()
		repo.sessions[session.ID] = session
	}}
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithUnitOfWork(uow))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-recovery", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusRunning || got.Queue != (domain.QueueIntent{}) {
		t.Fatalf("replacement session = %#v, want running without queue", got)
	}
	active, ok, err := processes.FindActiveBySession(ctx, "session-1")
	if err != nil || !ok || active.ID != newRun.ID {
		t.Fatalf("active process = %#v, %v, %v", active, ok, err)
	}
	for _, event := range events.snapshot() {
		if event.Type == "session.queued" {
			t.Fatalf("stale recovery queued replacement run: %#v", events.snapshot())
		}
	}
}

func TestRecoverInterruptedSessionSkipsRunCreatedAfterSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-1", WorktreePath: "/workspace/session-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	processes := newFakeProcessRepository()
	newRun := processdomain.Run{
		ID: "process-new", SessionID: "session-1", Status: processdomain.StatusRunning,
		PID: intPointer(202), CodexSessionID: "codex-1",
	}
	locker := &fakeSessionLocker{hook: func() {
		processes.mu.Lock()
		processes.active = newRun
		processes.hasActive = true
		processes.mu.Unlock()
	}}
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithSessionLocker(locker))

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusRunning || got.Queue != (domain.QueueIntent{}) {
		t.Fatalf("replacement session = %#v", got)
	}
	if len(codex.detachedStops) != 0 {
		t.Fatalf("new process was stopped: %#v", codex.detachedStops)
	}
}

func TestRecoverInterruptedSessionCompletesPreparedClose(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	reason := domain.CloseReasonUserClosed
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopping,
		CloseReason: &reason,
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusClosed || got.CloseReason == nil || *got.CloseReason != reason || got.ClosedAt == nil {
		t.Fatalf("recovered close session = %#v", got)
	}
}

func TestRecoverInterruptedSessionDoesNotOverwritePreparedClose(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-1", WorktreePath: "/workspace/session-1", UpdatedAt: time.Unix(90, 0).UTC(),
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID: "process-old", SessionID: "session-1", Status: processdomain.StatusRunning,
		PID: intPointer(101), CodexSessionID: "codex-1",
	}
	processes.hasActive = true
	reason := domain.CloseReasonUserClosed
	codex := &fakeCodexProcess{detachedHook: func(processdomain.DetachedProcess) {
		processes.mu.Lock()
		processes.hasActive = false
		processes.mu.Unlock()
		closing := session
		closing.Status = domain.StatusStopping
		closing.CloseReason = &reason
		closing.UpdatedAt = time.Unix(100, 0).UTC()
		repo.sessions[session.ID] = closing
	}}
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithUnitOfWork(uow))
	service.now = func() time.Time { return time.Unix(110, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-recovery", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusStopping || got.CloseReason == nil || *got.CloseReason != reason || got.Queue != (domain.QueueIntent{}) {
		t.Fatalf("prepared close was overwritten = %#v", got)
	}
	for _, event := range events.snapshot() {
		if event.Type == "session.queued" {
			t.Fatalf("stale recovery queued prepared close: %#v", events.snapshot())
		}
	}
}

func processNodeRunID(value string) *processdomain.NodeRunID {
	id := processdomain.NodeRunID(value)
	return &id
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
