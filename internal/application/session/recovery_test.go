package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	questionapp "github.com/nzlov/anycode/internal/application/question"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	domain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore"
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
			CodexSessionID: "codex-running",
		},
		"stopping": {
			ID: "process-stopping", SessionID: "stopping", Status: processdomain.StatusStopping,
			CodexSessionID: "codex-stopping",
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
	processes.active = processdomain.Run{ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithEvents(events), WithUnitOfWork(uow))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	if uow.calls != 1 {
		t.Fatalf("unit of work calls = %d, want atomic session recovery", uow.calls)
	}
	if processes.exitedID != "process-1" || repo.sessions[session.ID].Status != domain.StatusQueued {
		t.Fatalf("transaction result: exited=%q session=%#v", processes.exitedID, repo.sessions[session.ID])
	}
	gotEvents := events.snapshot()
	requireSessionEventTypes(t, gotEvents, "process.exited", "session.queued", sessionStatusUpdatedEvent)
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
		SessionID: "session-1", NodeRunID: &nodeRunID, CurrentNodeID: "build", RequiresCodex: true, Prompt: "Build",
	}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID: "process-1", SessionID: "session-1", NodeRunID: processNodeRunID("node-run-1"), Status: processdomain.StatusRunning,
		CodexSessionID: "codex-1",
	}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithWorkflows(workflows), WithEvents(&fakeEventStore{}))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.RecoverInterruptedSessions(ctx); err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	got := repo.sessions[session.ID]
	if got.Status != domain.StatusQueued || got.ID != "session-1" || got.Queue.NodeRunID == nil || *got.Queue.NodeRunID != nodeRunID {
		t.Fatalf("workflow recovery = %#v", got)
	}
	if workflows.resumeNodeInput.SessionID != session.ID {
		t.Fatalf("workflow resume input = %#v", workflows.resumeNodeInput)
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
		CodexSessionID: "codex-1",
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

func TestRecoverInterruptedSessionCompletesPreparedCloseWithPendingQuestion(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	now := time.Date(2026, 7, 14, 14, 0, 0, 0, time.UTC)
	reason := domain.CloseReasonUserClosed
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopping,
		CloseReason: &reason, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	request := questiondomain.Request{
		ID: "request-1", SessionID: "session-1", Status: questiondomain.RequestPending, CreatedAt: now,
		Questions: []questiondomain.Question{{
			ID: "question-1", RequestID: "request-1", Title: "Choose", Body: "Continue?", Type: "choice", Status: string(questiondomain.RequestPending),
		}},
	}
	if err := store.Questions().CreateRequest(ctx, request); err != nil {
		t.Fatalf("create question request: %v", err)
	}
	service := New(store.Sessions(), newFakeProjectRepository("project-1"), WithEvents(store.Events()), WithUnitOfWork(store), WithQuestions(questionapp.New(store.Questions())))
	service.now = func() time.Time { return now.Add(time.Minute) }

	count, err := service.RecoverInterruptedSessions(ctx)
	if err != nil {
		t.Fatalf("RecoverInterruptedSessions() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("RecoverInterruptedSessions() = %d, want 1", count)
	}
	got, err := store.Sessions().Find(ctx, session.ID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if got.Status != domain.StatusClosed || got.CloseReason == nil || *got.CloseReason != reason || got.ClosedAt == nil {
		t.Fatalf("recovered close session = %#v", got)
	}
	gotRequest, err := store.Questions().FindRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("find question request: %v", err)
	}
	if gotRequest.Status != questiondomain.RequestCancelled {
		t.Fatalf("question request status = %q", gotRequest.Status)
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
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{CodexSessionID: "codex-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.generateID = func() (domain.ID, error) { return "process-2", nil }

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSessionWithOptions() error = %v", err)
	}
	if len(processes.created) != 2 || processes.created[1].ResumeOf == nil || *processes.created[1].ResumeOf != "process-1" {
		t.Fatalf("process runs = %#v", processes.created)
	}
}

func TestRetryResumeSettlesInterruptedProcessBeforeQueueing(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusResumeFailed,
		CodexSessionID: "codex-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning}
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
