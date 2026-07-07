package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	questions := questionapp.New(store.Questions(), questionapp.NewMemoryAnswerWaiter())
	events := eventapp.New(store.Events())
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
						"allowCustom":true,
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
		smokeAssertMCPAnswer(t, rec.Body.Bytes(), pending.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("mcp answer_user did not resume after GraphQL submitQuestionBatch")
	}

	stopped := smokeGraphQL[string](t, handler, `mutation($id: ID!) {
		stopSession(id: $id) { id status }
	}`, map[string]any{"id": sessionID}, "stopSession.status")
	if stopped != "stopped" {
		t.Fatalf("stopSession status = %q, want stopped", stopped)
	}
	if codex.stoppedRunID == "" {
		t.Fatal("codex Stop was not called")
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

func smokeAssertMCPAnswer(t *testing.T, body []byte, batchID string) {
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
		Answers []any  `json:"answers"`
	}
	if err := json.Unmarshal([]byte(response.Result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode mcp content text: %v; text=%s", err, response.Result.Content[0].Text)
	}
	if payload.BatchID != batchID || len(payload.Answers) != 1 {
		t.Fatalf("mcp answer payload = %#v, want batch %q with one answer", payload, batchID)
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
	events       chan processdomain.CodexEvent
	stoppedRunID processdomain.RunID
}

func (p *smokeCodexProcess) Probe(context.Context) (processdomain.CodexCapabilities, error) {
	return processdomain.CodexCapabilities{Version: "smoke", SupportsExec: true, SupportsResume: true}, nil
}

func (p *smokeCodexProcess) Start(_ context.Context, input processdomain.CodexStartInput) (processdomain.CodexHandle, error) {
	p.startInput = input
	return processdomain.CodexHandle{ProcessRunID: input.ProcessRunID, PID: 1234, CodexSessionID: "codex-smoke-session"}, nil
}

func (p *smokeCodexProcess) Resume(_ context.Context, input processdomain.CodexResumeInput) (processdomain.CodexHandle, error) {
	return processdomain.CodexHandle{ProcessRunID: input.ProcessRunID, PID: 1234, CodexSessionID: input.CodexSessionID}, nil
}

func (p *smokeCodexProcess) Stop(_ context.Context, processRunID processdomain.RunID) error {
	p.stoppedRunID = processRunID
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
