package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
)

func TestSmokeHTTPGraphQLMCPAnswerUserSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatalf("open entstore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close entstore: %v", err)
		}
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate entstore: %v", err)
	}

	fileStore := filestore.New(dataDir)
	attachments := attachmentapp.New(store.Attachments(), fileStore)
	questions := questionapp.New(store.Questions())
	events := eventapp.New()
	workflows := workflowapp.New(store.Workflows(), workflowapp.WithUnitOfWork(store), workflowapp.WithEvents(store.Events()), workflowapp.WithEventPublisher(events))
	codex := &smokeCodexProcess{events: make(chan processdomain.CodexEvent)}
	sessions := sessionapp.New(
		store.Sessions(),
		store.Projects(),
		sessionapp.WithAttachments(store.Attachments(), fileStore),
		sessionapp.WithWorkflows(workflows),
		sessionapp.WithProcesses(store.Processes(), codex),
		sessionapp.WithEvents(store.Events()),
		sessionapp.WithEventPublisher(events),
		sessionapp.WithQuestions(questions),
		sessionapp.WithUnitOfWork(store),
	)
	projects := projectapp.New(store.Projects(), smokeDirectoryBrowser{}, smokeGitInspector{})
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{
		Projects:    projects,
		Sessions:    sessions,
		Events:      events,
		Attachments: attachments,
		Workflows:   workflows,
		Questions:   questions,
	}))

	projectID := smokeGraphQL[string](t, handler, `mutation($input: CreateProjectInput!) {
		createProject(input: $input) { id name path isGit }
	}`, map[string]any{"input": map[string]any{
		"path": filepath.Join(dataDir, "repo"),
		"name": "Smoke Project",
	}}, "createProject.id")
	if projectID == "" {
		t.Fatal("createProject returned empty id")
	}

	workflowID := smokeGraphQL[string](t, handler, `mutation($input: SaveWorkflowDefinitionInput!) {
		saveWorkflowDefinition(input: $input) { id projectId graph { nodes { id type title } } }
	}`, map[string]any{"input": map[string]any{
		"projectId": projectID,
		"name":      "Smoke approval flow",
		"graph": map[string]any{
			"nodes": []map[string]any{{
				"id":       "approval",
				"type":     "approval",
				"title":    "Human approval",
				"position": map[string]any{"x": 0, "y": 0},
			}},
			"edges": []map[string]any{},
		},
	}}, "saveWorkflowDefinition.id")
	if workflowID == "" {
		t.Fatal("saveWorkflowDefinition returned empty id")
	}
	defaultWorkflowProjectID := smokeGraphQL[string](t, handler, `mutation($input: SetDefaultWorkflowInput!) {
		setDefaultWorkflow(input: $input) { id defaultWorkflowId }
	}`, map[string]any{"input": map[string]any{
		"projectId":  projectID,
		"workflowId": workflowID,
	}}, "setDefaultWorkflow.id")
	if defaultWorkflowProjectID != projectID {
		t.Fatalf("setDefaultWorkflow project id = %q, want %q", defaultWorkflowProjectID, projectID)
	}
	workflowStatus := smokeGraphQL[string](t, handler, `mutation($input: CreateSessionInput!) {
		createSession(input: $input) { id status mode }
	}`, map[string]any{"input": map[string]any{
		"projectId":           projectID,
		"requirement":         "Run the configured approval flow",
		"mode":                "workflow",
		"config":              map[string]any{"codexModel": "gpt-5.4-mini", "reasoningEffort": "low", "permissionMode": "workspace-write"},
		"stagedAttachmentIds": []string{},
	}}, "createSession.status")
	if workflowStatus != "waiting_approval" {
		t.Fatalf("workflow createSession status = %q, want waiting_approval", workflowStatus)
	}

	sessionID := smokeGraphQL[string](t, handler, `mutation($input: CreateSessionInput!) {
		createSession(input: $input) { id status mode worktreePath }
	}`, map[string]any{"input": map[string]any{
		"projectId":           projectID,
		"requirement":         "Ask the user before continuing",
		"mode":                "chat",
		"baseBranch":          "",
		"config":              map[string]any{"codexModel": "gpt-5.4-mini", "reasoningEffort": "low", "permissionMode": "workspace-write"},
		"stagedAttachmentIds": []string{},
	}}, "createSession.id")
	if sessionID == "" {
		t.Fatal("createSession returned empty id")
	}

	status := smokeGraphQL[string](t, handler, `mutation($id: ID!) {
		startSession(id: $id) { id status codexSessionId }
	}`, map[string]any{"id": sessionID}, "startSession.status")
	if status != "queued" {
		t.Fatalf("startSession status = %q, want queued", status)
	}
	if _, err := sessions.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("drain queued sessions: %v", err)
	}
	if codex.startInput.SessionID != processdomain.SessionID(sessionID) || codex.startInput.Workdir == "" {
		t.Fatalf("codex Start input = %#v", codex.startInput)
	}
	codex.events <- processdomain.CodexEvent{
		EventID:        "transcript:codex-smoke-session",
		Type:           processdomain.CodexEventTranscriptBound,
		CodexSessionID: "codex-smoke-session",
		Content: processdomain.CodexTranscriptSource{
			CodexSessionID: "codex-smoke-session",
			RelativePath:   "test/codex-smoke-session.jsonl",
			BoundAt:        time.Now().UTC(),
		},
	}
	smokeWaitSessionStatus(t, store, sessionID, sessiondomain.StatusRunning)

	mcpDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body := `{
			"jsonrpc":"2.0",
			"id":1,
			"method":"tools/call",
			"params":{
				"name":"answer_user",
				"arguments":{
					"questions":[{
						"title":"Choose next step",
						"body":"How should Codex continue?",
						"type":"choice",
						"options":[{"id":"continue","label":"Continue","description":"Proceed"}]
					}]
				}
			}
		}`
		req := httptest.NewRequest(http.MethodPost, "/mcp/sessions/"+sessionID, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		mcpDone <- rec
	}()

	pending := smokeWaitPendingBatch(t, handler, sessionID)
	if pending.ID == "" || pending.QuestionID == "" {
		t.Fatalf("pending batch missing ids: %#v", pending)
	}
	if pending.Status != "pending" || pending.OptionID != "continue" {
		t.Fatalf("pending batch = %#v", pending)
	}
	answeredStatus := smokeGraphQL[string](t, handler, `mutation($input: SubmitQuestionBatchInput!) {
		submitQuestionBatch(input: $input) { id status questions { id selectedOptionId status } }
	}`, map[string]any{"input": map[string]any{
		"batchId": pending.ID,
		"answers": []map[string]any{{
			"questionId":       pending.QuestionID,
			"selectedOptionId": pending.OptionID,
		}},
	}}, "submitQuestionBatch.status")
	if answeredStatus != "answered" {
		t.Fatalf("submitQuestionBatch status = %q, want answered", answeredStatus)
	}
	select {
	case rec := <-mcpDone:
		if rec.Code != http.StatusOK {
			t.Fatalf("mcp answer_user status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		smokeAssertMCPStatus(t, rec.Body.Bytes(), pending.ID, "answered")
	case <-time.After(2 * time.Second):
		t.Fatal("mcp answer_user did not return the direct answer")
	}
	if codex.resumeInput.CodexSessionID != "" || codex.stoppedRunID != "" {
		t.Fatalf("direct answer unexpectedly stopped or resumed Codex: stop=%q resume=%#v", codex.stoppedRunID, codex.resumeInput)
	}
}

func TestAnswerUserStopFailureRestoresRunningLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusRunning, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	pid := 1234
	if err := store.Processes().CreateRun(ctx, processdomain.Run{
		ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: &pid, CodexSessionID: "codex-1", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	questions := questionapp.New(store.Questions())
	service := sessionapp.New(
		store.Sessions(),
		store.Projects(),
		sessionapp.WithProcesses(store.Processes(), &retryCodexProcess{stopErr: errors.New("stop unavailable")}),
		sessionapp.WithEvents(store.Events()),
		sessionapp.WithQuestions(questions),
		sessionapp.WithUnitOfWork(store),
		sessionapp.WithSessionLocker(sessionapp.NewMemorySessionLocker()),
	)

	waitCtx, cancelWait := context.WithCancel(ctx)
	waitDone := make(chan error, 1)
	go func() {
		_, waitErr := service.RequestUserAnswer(waitCtx, sessionapp.RequestUserAnswerInput{
			SessionID: "session-1",
			Questions: []questiondomain.Question{{Title: "Continue?", Type: "choice", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
		})
		waitDone <- waitErr
	}()
	for {
		pending, findErr := store.Questions().ListPendingBySession(ctx, "session-1")
		if findErr != nil {
			t.Fatal(findErr)
		}
		if len(pending) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancelWait()
	err = <-waitDone
	if err == nil || !strings.Contains(err.Error(), "stop unavailable") {
		t.Fatalf("RequestUserAnswer() fallback error = %v", err)
	}
	session, err := store.Sessions().Find(ctx, "session-1")
	if err != nil || session.Status != sessiondomain.StatusResumeFailed {
		t.Fatalf("session = %#v, %v", session, err)
	}
	run, err := store.Processes().FindRun(ctx, "process-1")
	if err != nil || run.Status != processdomain.StatusStopping || run.PID == nil || *run.PID != pid || run.CodexSessionID != "codex-1" {
		t.Fatalf("process = %#v, %v", run, err)
	}
	pending, err := store.Questions().ListPendingBySession(ctx, "session-1")
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending batches = %#v, %v", pending, err)
	}
	rows, err := store.Client().QuestionBatch.Query().All(ctx)
	if err != nil || len(rows) != 1 || rows[0].Status != string(questiondomain.BatchPending) {
		t.Fatalf("question rows = %#v, %v", rows, err)
	}
}

func TestRestartKeepsPendingAnswerUserSuspended(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusWaitingUser, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().CreateRun(ctx, processdomain.Run{
		ID: "process-1", SessionID: "session-1", Status: processdomain.StatusWaitingUser, CodexSessionID: "codex-1", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	origin := questiondomain.ProcessRunID("process-1")
	if err := store.Questions().CreateBatch(ctx, questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", OriginProcessRunID: &origin, Status: questiondomain.BatchPending, DeliveryStatus: questiondomain.DeliveryNone,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice", Status: "pending"}}, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	service := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithProcesses(store.Processes(), &smokeCodexProcess{}), sessionapp.WithEvents(store.Events()), sessionapp.WithUnitOfWork(store))
	if count, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil || count != 1 {
		t.Fatalf("MarkInterruptedSessionsRecoverable() = %d, %v", count, err)
	}
	session, err := store.Sessions().Find(ctx, "session-1")
	if err != nil || session.Status != sessiondomain.StatusWaitingUser {
		t.Fatalf("session = %#v, %v", session, err)
	}
	run, err := store.Processes().FindRun(ctx, "process-1")
	if err != nil || run.Status != processdomain.StatusExited {
		t.Fatalf("process = %#v, %v", run, err)
	}
}

func TestRestartRecoversLegacyAnswerUserOriginFromHistory(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusStopped, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().CreateRun(ctx, processdomain.Run{
		ID: "process-1", SessionID: "session-1", Status: processdomain.StatusExited, CodexSessionID: "codex-1", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Questions().CreateBatch(ctx, questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchPending, DeliveryStatus: questiondomain.DeliveryNone,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice", Status: "pending"}}, CreatedAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	service := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithProcesses(store.Processes(), &smokeCodexProcess{}), sessionapp.WithEvents(store.Events()), sessionapp.WithUnitOfWork(store))
	if count, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil || count != 1 {
		t.Fatalf("MarkInterruptedSessionsRecoverable() = %d, %v", count, err)
	}
	session, err := store.Sessions().Find(ctx, "session-1")
	if err != nil || session.Status != sessiondomain.StatusWaitingUser {
		t.Fatalf("session = %#v, %v", session, err)
	}
	batch, err := store.Questions().FindBatch(ctx, "batch-1")
	if err != nil || batch.Status != questiondomain.BatchPending || batch.OriginProcessRunID == nil || *batch.OriginProcessRunID != "process-1" {
		t.Fatalf("batch = %#v, %v", batch, err)
	}
}

func TestRestartCancelsUnrecoverableLegacyAnswerUserBatch(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusWaitingUser, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Questions().CreateBatch(ctx, questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchPending, DeliveryStatus: questiondomain.DeliveryNone,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice", Status: "pending"}}, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	service := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithProcesses(store.Processes(), &smokeCodexProcess{}), sessionapp.WithEvents(store.Events()), sessionapp.WithUnitOfWork(store))
	if count, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil || count != 1 {
		t.Fatalf("MarkInterruptedSessionsRecoverable() = %d, %v", count, err)
	}
	session, err := store.Sessions().Find(ctx, "session-1")
	if err != nil || session.Status != sessiondomain.StatusResumeFailed {
		t.Fatalf("session = %#v, %v", session, err)
	}
	batch, err := store.Questions().FindBatch(ctx, "batch-1")
	if err != nil || batch.Status != questiondomain.BatchCancelled {
		t.Fatalf("batch = %#v, %v", batch, err)
	}
}

func TestRestartClosesStoppedAnswerUserProcess(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusStopping, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().CreateRun(ctx, processdomain.Run{
		ID: "process-1", SessionID: "session-1", Status: processdomain.StatusStopping, CodexSessionID: "codex-1", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	origin := questiondomain.ProcessRunID("process-1")
	if err := store.Questions().CreateBatch(ctx, questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", OriginProcessRunID: &origin, Status: questiondomain.BatchPending, DeliveryStatus: questiondomain.DeliveryNone,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice", Status: "pending"}}, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	service := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithProcesses(store.Processes(), &smokeCodexProcess{}), sessionapp.WithEvents(store.Events()), sessionapp.WithUnitOfWork(store))
	if count, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil || count != 1 {
		t.Fatalf("MarkInterruptedSessionsRecoverable() = %d, %v", count, err)
	}
	session, err := store.Sessions().Find(ctx, "session-1")
	if err != nil || session.Status != sessiondomain.StatusStopped {
		t.Fatalf("session = %#v, %v", session, err)
	}
	run, err := store.Processes().FindRun(ctx, "process-1")
	if err != nil || run.Status != processdomain.StatusExited {
		t.Fatalf("process = %#v, %v", run, err)
	}
	batch, err := store.Questions().FindBatch(ctx, "batch-1")
	if err != nil || batch.Status != questiondomain.BatchCancelled {
		t.Fatalf("batch = %#v, %v", batch, err)
	}
}

func TestRestartRequeuesInflightAnswerDelivery(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusRunning, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().CreateRun(ctx, processdomain.Run{
		ID: "process-origin", SessionID: "session-1", Status: processdomain.StatusStarting, StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().BindTranscript(ctx, "process-origin", 4321, processdomain.CodexTranscriptSource{
		CodexSessionID: "codex-1", RelativePath: "test/codex-1.jsonl", BoundAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().MarkExited(ctx, "process-origin", processdomain.ExitResult{FinishedAt: now}); err != nil {
		t.Fatal(err)
	}
	origin := questiondomain.ProcessRunID("process-origin")
	delivery := questiondomain.ProcessRunID("process-delivery")
	selected := questiondomain.OptionID("continue")
	answeredAt := now.Add(time.Minute)
	if err := store.Questions().CreateBatch(ctx, questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", OriginProcessRunID: &origin, Status: questiondomain.BatchAnswered, DeliveryStatus: questiondomain.DeliveryInflight, DeliveryProcessRunID: &delivery,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice", Status: "answered", SelectedOptionID: &selected, Options: []questiondomain.Option{{ID: selected, Label: "Continue"}}}}, CreatedAt: now, AnsweredAt: &answeredAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().CreateRun(ctx, processdomain.Run{
		ID: "process-delivery", SessionID: "session-1", Status: processdomain.StatusRunning, CodexSessionID: "codex-1", ResumeOf: func() *processdomain.RunID { id := processdomain.RunID("process-origin"); return &id }(), StartedAt: answeredAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Sessions().AppendPrompt(ctx, sessiondomain.PromptAppend{
		ID: "append-1", SessionID: "session-1", Body: "keep this context", Status: sessiondomain.PromptAppendInflight, DispatchedProcessRunID: "process-delivery", CreatedAt: answeredAt,
	}); err != nil {
		t.Fatal(err)
	}
	service := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithProcesses(store.Processes(), &smokeCodexProcess{}), sessionapp.WithEvents(store.Events()), sessionapp.WithUnitOfWork(store))
	if _, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := store.Sessions().Find(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != sessiondomain.StatusQueued || session.Queue.Kind != sessiondomain.QueueKindAnswerUser || session.Queue.ResumeOfProcessRunID != "process-origin" || session.Queue.AnswerBatchID != "batch-1" {
		t.Fatalf("session queue = %#v", session)
	}
	batch, err := store.Questions().FindBatch(ctx, "batch-1")
	if err != nil || batch.DeliveryStatus != questiondomain.DeliveryAwaitingResume || batch.DeliveryProcessRunID != nil {
		t.Fatalf("batch = %#v, %v", batch, err)
	}
	appends, err := store.Sessions().ListPendingPromptAppends(ctx, "session-1")
	if err != nil || len(appends) != 1 || appends[0].ID != "append-1" || appends[0].DispatchedProcessRunID != "" {
		t.Fatalf("pending prompt appends = %#v, %v", appends, err)
	}
}

func TestAnswerDeliveryResumeFailureCanRetrySameBatch(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusResumeFailed, WorktreePath: t.TempDir(), CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().CreateRun(ctx, processdomain.Run{
		ID: "process-origin", SessionID: "session-1", Status: processdomain.StatusStarting, StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().BindTranscript(ctx, "process-origin", 4321, processdomain.CodexTranscriptSource{
		CodexSessionID: "codex-1", RelativePath: "test/codex-1.jsonl", BoundAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().MarkExited(ctx, "process-origin", processdomain.ExitResult{FinishedAt: now}); err != nil {
		t.Fatal(err)
	}
	origin := questiondomain.ProcessRunID("process-origin")
	selected := questiondomain.OptionID("continue")
	answeredAt := now.Add(time.Minute)
	if err := store.Questions().CreateBatch(ctx, questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", OriginProcessRunID: &origin, Status: questiondomain.BatchAnswered, DeliveryStatus: questiondomain.DeliveryAwaitingResume,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice", Status: "answered", SelectedOptionID: &selected, Options: []questiondomain.Option{{ID: selected, Label: "Continue"}}}}, CreatedAt: now, AnsweredAt: &answeredAt,
	}); err != nil {
		t.Fatal(err)
	}
	codex := &retryCodexProcess{failResume: true}
	questions := questionapp.New(store.Questions())
	service := sessionapp.New(
		store.Sessions(), store.Projects(), sessionapp.WithProcesses(store.Processes(), codex), sessionapp.WithEvents(store.Events()), sessionapp.WithQuestions(questions), sessionapp.WithUnitOfWork(store),
	)
	if _, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil {
		t.Fatal(err)
	}
	recoveredSession, err := store.Sessions().Find(ctx, "session-1")
	if err != nil || recoveredSession.Status != sessiondomain.StatusResumeFailed {
		t.Fatalf("resume failure should wait for user action after restart: %#v, %v", recoveredSession, err)
	}
	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", sessionapp.StartSessionOptions{Force: true}); err == nil {
		t.Fatal("first answer delivery resume should fail")
	}
	batch, err := store.Questions().FindBatch(ctx, "batch-1")
	if err != nil || batch.DeliveryStatus != questiondomain.DeliveryAwaitingResume {
		t.Fatalf("batch after failed resume = %#v, %v", batch, err)
	}
	codex.failResume = false
	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", sessionapp.StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("retry answer delivery: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		batch, err = store.Questions().FindBatch(ctx, "batch-1")
		if err != nil {
			t.Fatal(err)
		}
		if batch.DeliveryStatus == questiondomain.DeliveryDelivered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("answer delivery was not confirmed: %#v", batch)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if codex.resumeInput.CodexSessionID != "codex-1" || !strings.Contains(codex.resumeInput.Prompt, "batch-1") {
		t.Fatalf("resume input = %#v", codex.resumeInput)
	}
}

func TestRestartReconcilesWorkflowAnswerResumeFailure(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: sessiondomain.ModeWorkflow, Status: sessiondomain.StatusResumeFailed, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	definition := workflowdomain.Definition{
		ID: "workflow-1", ProjectID: "project-1", Name: "workflow", Version: 1, Active: true,
		Graph: workflowdomain.Graph{Nodes: []workflowdomain.Node{{ID: "node-1", Type: "codex", Title: "Run"}}}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Workflows().SaveDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	processRunID := workflowdomain.ProcessRunID("process-origin")
	if err := store.Workflows().CreateInitialRun(ctx, workflowdomain.Run{
		SessionID: "session-1", WorkflowDefinitionID: definition.ID, Status: workflowdomain.RunRunning, CurrentNodeID: "node-1", Context: workflowdomain.Context{Values: map[string]any{}}, StartedAt: &now,
	}, workflowdomain.NodeRun{
		ID: "node-run-1", SessionID: "session-1", NodeID: "node-1", Status: workflowdomain.NodeWaitingUser, Attempt: 1, ProcessRunID: &processRunID, StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	nodeRunID := processdomain.NodeRunID("node-run-1")
	if err := store.Processes().CreateRun(ctx, processdomain.Run{
		ID: "process-origin", SessionID: "session-1", NodeRunID: &nodeRunID, Status: processdomain.StatusExited, CodexSessionID: "codex-1", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	origin := questiondomain.ProcessRunID("process-origin")
	selected := questiondomain.OptionID("continue")
	answeredAt := now.Add(time.Minute)
	if err := store.Questions().CreateBatch(ctx, questiondomain.Batch{
		ID: "batch-1", SessionID: "session-1", OriginProcessRunID: &origin, Status: questiondomain.BatchAnswered, DeliveryStatus: questiondomain.DeliveryAwaitingResume,
		Questions: []questiondomain.Question{{ID: "question-1", BatchID: "batch-1", Type: "choice", Status: "answered", SelectedOptionID: &selected, Options: []questiondomain.Option{{ID: selected, Label: "Continue"}}}}, CreatedAt: now, AnsweredAt: &answeredAt,
	}); err != nil {
		t.Fatal(err)
	}
	workflowService := workflowapp.New(store.Workflows(), workflowapp.WithUnitOfWork(store), workflowapp.WithEvents(store.Events()))
	service := sessionapp.New(
		store.Sessions(), store.Projects(), sessionapp.WithProcesses(store.Processes(), &smokeCodexProcess{}), sessionapp.WithEvents(store.Events()), sessionapp.WithWorkflows(workflowService), sessionapp.WithUnitOfWork(store),
	)
	if count, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil || count != 1 {
		t.Fatalf("MarkInterruptedSessionsRecoverable() = %d, %v", count, err)
	}
	run, err := store.Workflows().FindRun(ctx, "session-1")
	if err != nil || run.Status != workflowdomain.RunWaitingResumeAction {
		t.Fatalf("workflow run = %#v, %v", run, err)
	}
}

type smokePendingBatch struct {
	ID         string
	Status     string
	QuestionID string
	OptionID   string
}

func smokeWaitPendingBatch(t *testing.T, handler http.Handler, sessionID string) smokePendingBatch {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		batches := smokeGraphQL[[]any](t, handler, `query($sessionId: ID!) {
			pendingQuestionBatches(sessionId: $sessionId) {
				id
				status
				questions { id options { id } }
			}
		}`, map[string]any{"sessionId": sessionID}, "pendingQuestionBatches")
		if len(batches) > 0 {
			batch := batches[0].(map[string]any)
			question := batch["questions"].([]any)[0].(map[string]any)
			option := question["options"].([]any)[0].(map[string]any)
			return smokePendingBatch{
				ID:         batch["id"].(string),
				Status:     batch["status"].(string),
				QuestionID: question["id"].(string),
				OptionID:   option["id"].(string),
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("pendingQuestionBatches did not contain answer_user batch")
	return smokePendingBatch{}
}

func smokeWaitSessionStatus(t *testing.T, store *entstore.Store, sessionID string, status sessiondomain.Status) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session, err := store.Sessions().Find(context.Background(), sessiondomain.ID(sessionID))
		if err == nil && session.Status == status {
			return
		}
		time.Sleep(time.Millisecond)
	}
	session, _ := store.Sessions().Find(context.Background(), sessiondomain.ID(sessionID))
	t.Fatalf("session %q status = %q, want %q", sessionID, session.Status, status)
}

func smokeAssertMCPStatus(t *testing.T, body []byte, batchID string, status string) {
	t.Helper()
	var response struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode mcp response: %v; body=%s", err, string(body))
	}
	if len(response.Result.Content) != 1 {
		t.Fatalf("mcp content = %#v; body=%s", response.Result.Content, string(body))
	}
	var payload struct {
		BatchID string `json:"batchId"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal([]byte(response.Result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode mcp content text: %v; text=%s", err, response.Result.Content[0].Text)
	}
	if payload.BatchID != batchID || payload.Status != status {
		t.Fatalf("mcp payload = %#v, want batch %q status %q", payload, batchID, status)
	}
}

func TestSaveWorkflowDefinitionAcceptsPrimitiveConditionValue(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatalf("open entstore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close entstore: %v", err)
		}
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate entstore: %v", err)
	}

	projects := projectapp.New(store.Projects(), smokeDirectoryBrowser{}, smokeGitInspector{})
	workflows := workflowapp.New(store.Workflows())
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{
		Projects:  projects,
		Workflows: workflows,
	}))

	projectID := smokeGraphQL[string](t, handler, `mutation($input: CreateProjectInput!) {
		createProject(input: $input) { id }
	}`, map[string]any{"input": map[string]any{
		"path": filepath.Join(dataDir, "repo"),
		"name": "Smoke Project",
	}}, "createProject.id")

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "true", value: true, want: true},
		{name: "false", value: false, want: false},
		{name: "string", value: "passed", want: "passed"},
		{name: "number", value: 7.5, want: float64(7.5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowName := "Primitive condition flow " + tt.name
			saveResult := smokeGraphQL[map[string]any](t, handler, `mutation($input: SaveWorkflowDefinitionInput!) {
				saveWorkflowDefinition(input: $input) {
					id
					graph {
						edges {
							condition {
								value
							}
						}
					}
				}
			}`, map[string]any{"input": map[string]any{
				"projectId": projectID,
				"name":      workflowName,
				"graph": map[string]any{
					"nodes": []map[string]any{
						{"id": "build", "type": "codex", "title": "Build", "position": map[string]any{"x": 0, "y": 0}},
						{"id": "ship", "type": "close", "title": "Ship", "position": map[string]any{"x": 100, "y": 0}},
					},
					"edges": []map[string]any{{
						"from":      "build",
						"to":        "ship",
						"priority":  0,
						"condition": map[string]any{"field": "results.status", "op": "eq", "value": tt.value},
					}},
				},
			}}, "saveWorkflowDefinition")

			savedCondition := smokePath(t, saveResult, "graph.edges.0.condition").(map[string]any)
			if !reflect.DeepEqual(savedCondition["value"], tt.want) {
				t.Fatalf("saved condition value = %#v, want %#v", savedCondition["value"], tt.want)
			}

			workflowID, ok := saveResult["id"].(string)
			if !ok || workflowID == "" {
				t.Fatalf("saved workflow id = %#v", saveResult["id"])
			}
			readValue := smokeGraphQL[any](t, handler, `query($id: ID!) {
				workflowDefinition(id: $id) {
					graph {
						edges {
							condition {
								value
							}
						}
					}
				}
			}`, map[string]any{"id": workflowID}, "workflowDefinition.graph.edges.0.condition.value")
			if !reflect.DeepEqual(readValue, tt.want) {
				t.Fatalf("read condition value = %#v, want %#v", readValue, tt.want)
			}
		})
	}
}

func smokeGraphQL[T any](t *testing.T, handler http.Handler, query string, variables map[string]any, path string) T {
	t.Helper()
	reqBody, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		t.Fatalf("marshal graphql request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("graphql status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode graphql response: %v; body=%s", err, rec.Body.String())
	}
	if len(response.Errors) > 0 {
		t.Fatalf("graphql errors = %#v; body=%s", response.Errors, rec.Body.String())
	}
	value := smokePath(t, response.Data, path)
	typed, ok := value.(T)
	if !ok {
		t.Fatalf("graphql path %q = %#v (%T), cannot cast to requested type", path, value, value)
	}
	return typed
}

func smokePath(t *testing.T, data map[string]any, path string) any {
	t.Helper()
	var current any = data
	start := 0
	for i := 0; i <= len(path); i++ {
		if i != len(path) && path[i] != '.' {
			continue
		}
		key := path[start:i]
		if items, ok := current.([]any); ok {
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(items) {
				t.Fatalf("graphql path %q segment %q parent = %#v", path, key, current)
			}
			current = items[index]
			start = i + 1
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("graphql path %q segment %q parent = %#v", path, key, current)
		}
		current, ok = object[key]
		if !ok {
			t.Fatalf("graphql path %q missing segment %q in %#v", path, key, object)
		}
		start = i + 1
	}
	return current
}

type smokeCodexProcess struct {
	startInput   processdomain.CodexStartInput
	resumeInput  processdomain.CodexResumeInput
	events       chan processdomain.CodexEvent
	stoppedRunID processdomain.RunID
	stopOnce     sync.Once
}

type retryCodexProcess struct {
	failResume  bool
	stopErr     error
	resumeInput processdomain.CodexResumeInput
	events      chan processdomain.CodexEvent
}

func (p *retryCodexProcess) Probe(context.Context) (processdomain.CodexCapabilities, error) {
	return processdomain.CodexCapabilities{SupportsExec: true, SupportsResume: true}, nil
}

func (p *retryCodexProcess) Start(context.Context, processdomain.CodexStartInput) (processdomain.CodexHandle, error) {
	return processdomain.CodexHandle{}, errors.New("unexpected start")
}

func (p *retryCodexProcess) Resume(_ context.Context, input processdomain.CodexResumeInput) (processdomain.CodexHandle, error) {
	p.resumeInput = input
	if p.failResume {
		return processdomain.CodexHandle{}, errors.New("resume unavailable")
	}
	p.events = make(chan processdomain.CodexEvent, 1)
	p.events <- processdomain.CodexEvent{
		Type: processdomain.CodexEventStatus, EventID: "answer-delivered",
		Content: processdomain.CodexStatusContent{Code: "task.started"}, CreatedAt: time.Now().UTC(),
	}
	close(p.events)
	return processdomain.CodexHandle{ProcessRunID: input.ProcessRunID, PID: 4321, CodexSessionID: input.CodexSessionID}, nil
}

func (p *retryCodexProcess) Stop(context.Context, processdomain.RunID) error {
	return p.stopErr
}

func (p *retryCodexProcess) Events(context.Context, processdomain.CodexHandle) (<-chan processdomain.CodexEvent, error) {
	return p.events, nil
}

func (p *smokeCodexProcess) Probe(context.Context) (processdomain.CodexCapabilities, error) {
	return processdomain.CodexCapabilities{Version: "smoke", SupportsExec: true, SupportsResume: true}, nil
}

func (p *smokeCodexProcess) Start(_ context.Context, input processdomain.CodexStartInput) (processdomain.CodexHandle, error) {
	p.startInput = input
	return processdomain.CodexHandle{ProcessRunID: input.ProcessRunID, PID: 1234, CodexSessionID: "codex-smoke-session"}, nil
}

func (p *smokeCodexProcess) Resume(_ context.Context, input processdomain.CodexResumeInput) (processdomain.CodexHandle, error) {
	p.resumeInput = input
	return processdomain.CodexHandle{ProcessRunID: input.ProcessRunID, PID: 1234, CodexSessionID: input.CodexSessionID}, nil
}

func (p *smokeCodexProcess) Stop(_ context.Context, processRunID processdomain.RunID) error {
	p.stoppedRunID = processRunID
	p.stopOnce.Do(func() { close(p.events) })
	return nil
}

func (p *smokeCodexProcess) Events(context.Context, processdomain.CodexHandle) (<-chan processdomain.CodexEvent, error) {
	return p.events, nil
}

type smokeGitInspector struct{}

func (smokeGitInspector) Detect(context.Context, string) (projectdomain.GitState, error) {
	return projectdomain.GitState{IsRepository: false}, nil
}

func (smokeGitInspector) Branches(context.Context, string) ([]projectdomain.GitBranch, error) {
	return nil, nil
}

func (smokeGitInspector) HeadCommit(context.Context, string, string) (string, error) {
	return "", nil
}

type smokeDirectoryBrowser struct{}

func (smokeDirectoryBrowser) List(_ context.Context, path string) (projectdomain.DirectoryListing, error) {
	return projectdomain.DirectoryListing{Path: path}, nil
}
