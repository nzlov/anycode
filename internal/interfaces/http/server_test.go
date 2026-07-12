package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gorilla/websocket"
	"github.com/nzlov/anycode/internal/application/apperror"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
)

func TestAPIHealthzBearerAuth(t *testing.T) {
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{}))

	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("healthz without bearer status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("healthz with bearer status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestGraphQLAllowsLocalDevelopmentWithoutAccessKey(t *testing.T) {
	handler := NewHandler(config.Config{}, WithGraphQLUseCases(graph.UseCases{}))

	rec := doGraphQL(handler, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("graphql without configured key status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"__typename":"Query"`) {
		t.Fatalf("graphql response missing query typename: %s", rec.Body.String())
	}
}

func TestGraphQLRequiresBearerWhenAccessKeyIsConfigured(t *testing.T) {
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{}))

	rec := doGraphQL(handler, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("graphql without bearer status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertErrorCode(t, rec, "auth_failed")

	rec = doGraphQL(handler, "Bearer secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("graphql with bearer status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"__typename":"Query"`) {
		t.Fatalf("graphql response missing query typename: %s", rec.Body.String())
	}
}

func TestGraphQLWebSocketHandshakeUsesConnectionInitAuth(t *testing.T) {
	called := false
	handler := NewHandler(
		config.Config{AccessKey: "secret"},
		WithGraphQLHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusSwitchingProtocols)
		})),
	)

	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !called {
		t.Fatal("websocket upgrade should reach graphql handler without HTTP bearer")
	}
}

func TestWebSocketInitFuncRequiresAuthorizationPayload(t *testing.T) {
	initFunc := websocketInitFunc("secret")
	if _, _, err := initFunc(context.Background(), transport.InitPayload{}); err == nil {
		t.Fatal("InitFunc() expected unauthorized error")
	}
	ctx, _, err := initFunc(context.Background(), transport.InitPayload{"Authorization": "Bearer secret"})
	if err != nil {
		t.Fatalf("InitFunc() error = %v", err)
	}
	principal, ok := graph.PrincipalFromContext(ctx)
	if !ok || principal.Kind != "websocket_connection_init" {
		t.Fatalf("principal = %#v ok=%v", principal, ok)
	}
}

func TestGraphQLWebSocketSessionEventsSubscriptionReceivesPublishedEvent(t *testing.T) {
	timeline := &fakeTimelineUseCase{ch: make(chan timelineapp.DTO, 1), subscribed: make(chan struct{})}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{Timeline: timeline}))
	server := httptest.NewServer(handler)
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/graphql"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Sec-WebSocket-Protocol": []string{"graphql-transport-ws"}})
	if err != nil {
		t.Fatalf("dial graphql websocket: %v", err)
	}
	defer conn.Close()

	writeSocketJSON(t, conn, map[string]any{
		"type":    "connection_init",
		"payload": map[string]any{"Authorization": "Bearer secret"},
	})
	assertSocketMessageType(t, conn, "connection_ack")
	writeSocketJSON(t, conn, map[string]any{
		"id":   "sub-1",
		"type": "subscribe",
		"payload": map[string]any{
			"query": `subscription($sessionId: ID!) {
				sessionEvents(sessionId: $sessionId) {
					ready
						event {
							id
							orderKey
							phase
							content {
								__typename
								... on SessionStatusContent {
									code
								}
							}
					}
				}
			}`,
			"variables": map[string]any{"sessionId": "session-1"},
		},
	})

	select {
	case <-timeline.subscribed:
	case <-time.After(time.Second):
		t.Fatal("sessionEvents subscription was not opened")
	}
	readyMessage := readSocketMessage(t, conn)
	readyPayload, ok := readyMessage["payload"].(map[string]any)
	if !ok {
		t.Fatalf("ready payload = %#v", readyMessage["payload"])
	}
	readyData, ok := readyPayload["data"].(map[string]any)
	if !ok {
		t.Fatalf("ready data = %#v", readyPayload["data"])
	}
	readyItem, ok := readyData["sessionEvents"].(map[string]any)
	if !ok || readyItem["ready"] != true || readyItem["event"] != nil {
		t.Fatalf("sessionEvents ready item = %#v", readyData["sessionEvents"])
	}

	timeline.ch <- timelineapp.DTO{
		ID:         "event-1",
		OrderKey:   "order-1",
		Phase:      processdomain.CodexPhaseStandalone,
		Content:    processdomain.CodexStatusContent{Code: "session.running", Level: "info"},
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	message := readSocketMessage(t, conn)
	if message["type"] != "next" || message["id"] != "sub-1" {
		t.Fatalf("websocket message = %#v, want next for sub-1", message)
	}
	payload, ok := message["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", message["payload"])
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("payload data = %#v", payload["data"])
	}
	streamItem, ok := data["sessionEvents"].(map[string]any)
	if !ok {
		t.Fatalf("sessionEvents payload = %#v", data["sessionEvents"])
	}
	event, ok := streamItem["event"].(map[string]any)
	if !ok || streamItem["ready"] != false {
		t.Fatalf("sessionEvents stream item = %#v", streamItem)
	}
	content, _ := event["content"].(map[string]any)
	if event["id"] != "event-1" || event["orderKey"] != "order-1" || event["phase"] != "STANDALONE" || content["code"] != "session.running" {
		t.Fatalf("session event = %#v", event)
	}
}

func TestGraphQLWebSocketSessionStateUpdatesSubscriptionReceivesBatch(t *testing.T) {
	pending := make(chan questionapp.BatchDTO, 1)
	questions := &fakeQuestionUseCase{pendingCh: pending}
	sessions := &fakeGraphQLSessionUseCase{getSessionResult: sessionapp.DetailDTO{DTO: sessionapp.DTO{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    sessiondomain.StatusWaitingUser,
	}}}
	events := &fakeEventUseCase{ch: make(chan eventapp.DTO)}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{
		Events: events, Questions: questions, Sessions: sessions,
	}))
	server := httptest.NewServer(handler)
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/graphql"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Sec-WebSocket-Protocol": []string{"graphql-transport-ws"}})
	if err != nil {
		t.Fatalf("dial graphql websocket: %v", err)
	}
	defer conn.Close()

	writeSocketJSON(t, conn, map[string]any{
		"type":    "connection_init",
		"payload": map[string]any{"Authorization": "Bearer secret"},
	})
	assertSocketMessageType(t, conn, "connection_ack")
	writeSocketJSON(t, conn, map[string]any{
		"id":   "state-1",
		"type": "subscribe",
		"payload": map[string]any{
			"query": `subscription($sessionId: ID!) {
				sessionStateUpdates(sessionId: $sessionId) {
					ready
					session { id status }
					questionBatch {
						id
						sessionId
						status
						questions {
							id
							title
							allowCustom
							options {
								id
								label
							}
						}
					}
				}
			}`,
			"variables": map[string]any{"sessionId": "session-1"},
		},
	})
	readyMessage := readSocketMessage(t, conn)
	readyPayload := readyMessage["payload"].(map[string]any)
	readyData := readyPayload["data"].(map[string]any)
	readyItem := readyData["sessionStateUpdates"].(map[string]any)
	if readyItem["ready"] != true {
		t.Fatalf("session state ready item = %#v", readyItem)
	}

	pending <- questionapp.BatchDTO{
		ID:        "batch-1",
		SessionID: "session-1",
		Status:    questiondomain.BatchPending,
		Questions: []questiondomain.Question{
			{
				ID:          "question-1",
				BatchID:     "batch-1",
				Title:       "Choose next step",
				Type:        "options",
				AllowCustom: true,
				Options: []questiondomain.Option{
					{ID: "continue", Label: "Continue"},
				},
			},
		},
	}

	message := readSocketMessage(t, conn)
	if message["type"] != "next" || message["id"] != "state-1" {
		t.Fatalf("websocket message = %#v, want next for state-1", message)
	}
	payload, ok := message["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", message["payload"])
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("payload data = %#v", payload["data"])
	}
	stateItem, ok := data["sessionStateUpdates"].(map[string]any)
	if !ok {
		t.Fatalf("sessionStateUpdates payload = %#v", data["sessionStateUpdates"])
	}
	batch, ok := stateItem["questionBatch"].(map[string]any)
	if !ok || stateItem["ready"] != false {
		t.Fatalf("session state item = %#v", stateItem)
	}
	if batch["id"] != "batch-1" || batch["sessionId"] != "session-1" || batch["status"] != "pending" {
		t.Fatalf("question batch = %#v", batch)
	}
	items, ok := batch["questions"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("batch questions = %#v", batch["questions"])
	}
	question, ok := items[0].(map[string]any)
	if !ok || question["id"] != "question-1" || question["allowCustom"] != true {
		t.Fatalf("question = %#v", items[0])
	}
}

func TestAttachmentPreviewRequiresBearer(t *testing.T) {
	useCase := &fakeAttachmentUseCase{}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithAttachmentUseCase(useCase))

	req := httptest.NewRequest(http.MethodGet, "/attachments/attachment-1/preview", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("attachment preview without bearer status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertErrorCode(t, rec, "auth_failed")
	if useCase.calls != 0 {
		t.Fatalf("attachment usecase calls = %d, want 0", useCase.calls)
	}
}

func TestAttachmentPreviewStreamsContent(t *testing.T) {
	useCase := &fakeAttachmentUseCase{
		stream: attachmentapp.Stream{
			Filename: "image.png",
			MimeType: "image/png",
			Reader:   io.NopCloser(strings.NewReader("png-bytes")),
		},
	}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithAttachmentUseCase(useCase))

	req := httptest.NewRequest(http.MethodGet, "/attachments/attachment-1/preview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("attachment preview status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if useCase.openedID != "attachment-1" || useCase.openedMode != attachmentapp.OpenPreview {
		t.Fatalf("OpenAttachment called with id=%q mode=%q", useCase.openedID, useCase.openedMode)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want %q", got, "image/png")
	}
	if got := rec.Header().Get("Content-Disposition"); got != `inline; filename=image.png` {
		t.Fatalf("Content-Disposition = %q, want inline filename", got)
	}
	if rec.Body.String() != "png-bytes" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "png-bytes")
	}
}

func TestAttachmentDownloadSetsContentDisposition(t *testing.T) {
	useCase := &fakeAttachmentUseCase{
		stream: attachmentapp.Stream{
			Filename: "report.txt",
			MimeType: "text/plain",
			Reader:   io.NopCloser(strings.NewReader("hello")),
		},
	}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithAttachmentUseCase(useCase))

	req := httptest.NewRequest(http.MethodGet, "/attachments/attachment-1/download", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("attachment download status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if useCase.openedMode != attachmentapp.OpenDownload {
		t.Fatalf("OpenAttachment mode = %q, want %q", useCase.openedMode, attachmentapp.OpenDownload)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename=report.txt` {
		t.Fatalf("Content-Disposition = %q, want attachment filename", got)
	}
}

func TestAttachmentPreviewWritesStructuredApplicationError(t *testing.T) {
	useCase := &fakeAttachmentUseCase{err: attachmentapp.ErrNotPreviewable}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithAttachmentUseCase(useCase))

	req := httptest.NewRequest(http.MethodGet, "/attachments/attachment-1/preview", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("attachment preview status = %d, want %d; body: %s", rec.Code, http.StatusUnsupportedMediaType, rec.Body.String())
	}
	assertErrorCode(t, rec, "attachment_failed")
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestApplicationErrorResponsePreservesFields(t *testing.T) {
	err := apperror.Wrap(errors.New("open /home/nzlov/workspaces/github/project token=secret"), apperror.CodeInternal, apperror.CategoryInfraError, "").
		WithDetails(map[string]any{
			"worktreePath": "/home/nzlov/workspaces/github/project",
			"accessKey":    "secret",
		})
	rec := httptest.NewRecorder()

	writeApplicationError(rec, http.StatusInternalServerError, err)

	var body struct {
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &body); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	if body.Message != "open /home/nzlov/workspaces/github/project token=secret" {
		t.Fatalf("message = %q", body.Message)
	}
	if body.Details["worktreePath"] != "/home/nzlov/workspaces/github/project" || body.Details["accessKey"] != "secret" {
		t.Fatalf("details = %#v", body.Details)
	}
}

func TestMCPRequiresBearerAndListsAnswerUserTool(t *testing.T) {
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{Questions: &fakeQuestionUseCase{}}))

	req := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-1", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("mcp without bearer status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-1", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mcp tools/list status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"answer_user"`) {
		t.Fatalf("mcp tools/list missing answer_user: %s", rec.Body.String())
	}
}

func TestMCPAnswerUserCreatesBatchAndReturnsAnswers(t *testing.T) {
	optionID := questiondomain.OptionID("continue")
	questions := &fakeQuestionUseCase{
		waitAnswers: []questiondomain.Answer{{QuestionID: "question-1", SelectedOptionID: &optionID}},
	}
	sessions := &fakeMCPSessionUseCase{}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{Questions: questions, Sessions: sessions}))
	body := `{
		"jsonrpc":"2.0",
		"id":2,
		"method":"tools/call",
		"params":{
			"name":"answer_user",
			"arguments":{
				"questions":[{
					"title":"Choose next step",
					"body":"How should Codex continue?",
					"allowCustom":true,
					"options":[{"id":"continue","label":"Continue","description":"Proceed"}]
				}]
			}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-1", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mcp tools/call status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if questions.created.SessionID != "session-1" || len(questions.created.Questions) != 1 {
		t.Fatalf("created batch input = %#v", questions.created)
	}
	if questions.created.Questions[0].Title != "Choose next step" || !questions.created.Questions[0].AllowCustom {
		t.Fatalf("created question = %#v", questions.created.Questions[0])
	}
	if questions.waitedBatchID != "batch-1" {
		t.Fatalf("waited batch id = %q", questions.waitedBatchID)
	}
	if sessions.waitingSessionID != "session-1" || sessions.runningSessionID != "session-1" {
		t.Fatalf("session state calls = waiting %q running %q", sessions.waitingSessionID, sessions.runningSessionID)
	}
	var response struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode mcp response: %v", err)
	}
	if len(response.Result.Content) != 1 || response.Result.Content[0].Type != "text" {
		t.Fatalf("mcp content = %#v", response.Result.Content)
	}
	if !strings.Contains(response.Result.Content[0].Text, `"batchId":"batch-1"`) || !strings.Contains(response.Result.Content[0].Text, `"QuestionID":"question-1"`) {
		t.Fatalf("mcp answer text = %s", response.Result.Content[0].Text)
	}
}

func TestMCPAnswerUserWritesStructuredApplicationError(t *testing.T) {
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{Questions: &fakeQuestionUseCase{}}))
	body := `{
		"jsonrpc":"2.0",
		"id":2,
		"method":"tools/call",
		"params":{
			"name":"answer_user",
			"arguments":{"questions":[]}
		}
	}`

	req := httptest.NewRequest(http.MethodPost, "/mcp/sessions/session-1", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mcp tools/call status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Error struct {
			Code int `json:"code"`
			Data struct {
				Code     string `json:"code"`
				Category string `json:"category"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode mcp error response: %v", err)
	}
	if response.Error.Code != -32602 || response.Error.Data.Code != apperror.CodeValidationFailed || response.Error.Data.Category != string(apperror.CategoryValidationError) {
		t.Fatalf("mcp error response = %#v; body=%s", response, rec.Body.String())
	}
}

func TestMCPApplicationErrorPreservesFields(t *testing.T) {
	err := apperror.Wrap(errors.New("read /home/nzlov/workspaces/github/project authorization=Bearer secret"), apperror.CodeInternal, apperror.CategoryInfraError, "").
		WithDetails(map[string]any{
			"repoPath":      "/home/nzlov/workspaces/github/project",
			"authorization": "Bearer secret",
		})
	rec := httptest.NewRecorder()

	writeMCPApplicationError(rec, json.RawMessage(`3`), -32603, err)

	var response struct {
		Error struct {
			Message string `json:"message"`
			Data    struct {
				Details map[string]any `json:"details"`
			} `json:"data"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode mcp response: %v", decodeErr)
	}
	if response.Error.Message != "read /home/nzlov/workspaces/github/project authorization=Bearer secret" {
		t.Fatalf("mcp message = %q", response.Error.Message)
	}
	if response.Error.Data.Details["repoPath"] != "/home/nzlov/workspaces/github/project" || response.Error.Data.Details["authorization"] != "Bearer secret" {
		t.Fatalf("mcp details = %#v", response.Error.Data.Details)
	}
}

func TestSPAFallbackStillServesIndex(t *testing.T) {
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{}))

	req := httptest.NewRequest(http.MethodGet, "/sessions/unknown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("spa fallback status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if !strings.Contains(string(body), `<div id=q-app>`) {
		t.Fatalf("spa fallback did not serve index.html: %s", string(body))
	}
}

func doGraphQL(handler http.Handler, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewBufferString(`{"query":"{ __typename }"}`))
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, rec.Body.String())
	}
	if body.Code != want {
		t.Fatalf("error code = %q, want %q; body=%s", body.Code, want, rec.Body.String())
	}
}

func writeSocketJSON(t *testing.T, conn *websocket.Conn, payload any) {
	t.Helper()
	if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set websocket write deadline: %v", err)
	}
	if err := conn.WriteJSON(payload); err != nil {
		t.Fatalf("write websocket json: %v", err)
	}
}

func readSocketMessage(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set websocket read deadline: %v", err)
	}
	var message map[string]any
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("read websocket json: %v", err)
	}
	return message
}

func assertSocketMessageType(t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	message := readSocketMessage(t, conn)
	if message["type"] != want {
		t.Fatalf("websocket message type = %#v, want %q", message, want)
	}
}

type fakeEventUseCase struct {
	ch         chan eventapp.DTO
	subscribed chan struct{}
}

type fakeTimelineUseCase struct {
	ch         chan timelineapp.DTO
	subscribed chan struct{}
}

type fakeGraphQLSessionUseCase struct {
	sessionapp.UseCase
	getSessionResult sessionapp.DetailDTO
}

func (u *fakeGraphQLSessionUseCase) GetSession(context.Context, sessiondomain.ID) (sessionapp.DetailDTO, error) {
	return u.getSessionResult, nil
}

func (u *fakeEventUseCase) LiveSessionEvents(context.Context, eventapp.LiveSessionEventsInput) (<-chan eventapp.DTO, error) {
	if u.subscribed == nil {
		u.subscribed = make(chan struct{})
	}
	close(u.subscribed)
	return u.ch, nil
}

func (u *fakeTimelineUseCase) ListSessionEvents(context.Context, timelineapp.ListSessionEventsInput) (timelineapp.Page, error) {
	return timelineapp.Page{}, nil
}

func (u *fakeTimelineUseCase) SessionEvents(context.Context, timelineapp.SessionEventsInput) (<-chan timelineapp.DTO, error) {
	if u.subscribed == nil {
		u.subscribed = make(chan struct{})
	}
	close(u.subscribed)
	return u.ch, nil
}

type fakeQuestionUseCase struct {
	created       questionapp.CreateBatchInput
	waitedBatchID questiondomain.BatchID
	waitAnswers   []questiondomain.Answer
	pendingCh     chan questionapp.BatchDTO
}

type fakeMCPSessionUseCase struct {
	sessionapp.UseCase
	waitingSessionID sessiondomain.ID
	runningSessionID sessiondomain.ID
}

func (u *fakeMCPSessionUseCase) MarkWaitingUser(_ context.Context, id sessiondomain.ID) (sessionapp.DTO, error) {
	u.waitingSessionID = id
	return sessionapp.DTO{ID: id}, nil
}

func (u *fakeMCPSessionUseCase) MarkRunningAfterUserWait(_ context.Context, id sessiondomain.ID) (sessionapp.DTO, error) {
	u.runningSessionID = id
	return sessionapp.DTO{ID: id}, nil
}

func (u *fakeQuestionUseCase) CreateBatch(_ context.Context, input questionapp.CreateBatchInput) (questionapp.BatchDTO, error) {
	u.created = input
	return questionapp.BatchDTO{
		ID:        "batch-1",
		SessionID: input.SessionID,
		Status:    questiondomain.BatchPending,
		Questions: input.Questions,
	}, nil
}

func (u *fakeQuestionUseCase) Wait(_ context.Context, id questiondomain.BatchID) ([]questiondomain.Answer, error) {
	u.waitedBatchID = id
	return u.waitAnswers, nil
}

func (u *fakeQuestionUseCase) SubmitBatch(context.Context, questionapp.SubmitBatchInput) (questionapp.BatchDTO, error) {
	return questionapp.BatchDTO{}, nil
}

func (u *fakeQuestionUseCase) GetBatch(context.Context, questiondomain.BatchID) (questionapp.BatchDTO, error) {
	return questionapp.BatchDTO{}, nil
}

func (u *fakeQuestionUseCase) ListPendingBySession(context.Context, questiondomain.SessionID) ([]questionapp.BatchDTO, error) {
	return nil, nil
}

func (u *fakeQuestionUseCase) QuestionBatchUpdates(context.Context, questiondomain.SessionID) (<-chan questionapp.BatchDTO, error) {
	if u.pendingCh != nil {
		return u.pendingCh, nil
	}
	ch := make(chan questionapp.BatchDTO)
	close(ch)
	return ch, nil
}

func (u *fakeQuestionUseCase) CancelPendingBySession(context.Context, questiondomain.SessionID, string) error {
	return nil
}

type fakeAttachmentUseCase struct {
	stream     attachmentapp.Stream
	err        error
	calls      int
	openedID   sessiondomain.AttachmentID
	openedMode attachmentapp.OpenMode
}

func (u *fakeAttachmentUseCase) StageAttachment(context.Context, attachmentapp.StageAttachmentInput) (attachmentapp.AttachmentDTO, error) {
	return attachmentapp.AttachmentDTO{}, nil
}

func (u *fakeAttachmentUseCase) DeleteStagedAttachment(context.Context, sessiondomain.StagedAttachmentID) error {
	return nil
}

func (u *fakeAttachmentUseCase) DeleteSessionAttachment(context.Context, sessiondomain.SessionAttachmentID) error {
	return nil
}

func (u *fakeAttachmentUseCase) OpenAttachment(_ context.Context, id sessiondomain.AttachmentID, mode attachmentapp.OpenMode) (attachmentapp.Stream, error) {
	u.calls++
	u.openedID = id
	u.openedMode = mode
	return u.stream, u.err
}
