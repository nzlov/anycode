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
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	sessioneventapp "github.com/nzlov/anycode/internal/application/sessionevent"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
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

func TestGraphQLWebSocketTransportSendsGraphQLTransportPing(t *testing.T) {
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{}))
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
	if err := conn.SetReadDeadline(time.Now().Add(11 * time.Second)); err != nil {
		t.Fatalf("set websocket ping read deadline: %v", err)
	}
	var message map[string]any
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("read websocket ping: %v", err)
	}
	if message["type"] != "ping" {
		t.Fatalf("websocket message type = %#v, want %q", message, "ping")
	}
	writeSocketJSON(t, conn, map[string]any{"type": "pong"})
}

func TestGraphQLWebSocketSessionEventsSubscriptionReceivesTranscript(t *testing.T) {
	sessionEvents := &fakeSessionEventUseCase{events: make(chan timelineapp.DTO, 1), subscribed: make(chan struct{})}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{SessionEvents: sessionEvents}))
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
					id
					orderKey
					phase
					content {
						__typename
						... on TranscriptStatusContent {
							code
						}
					}
				}
			}`,
			"variables": map[string]any{"sessionId": "session-1"},
		},
	})

	select {
	case <-sessionEvents.subscribed:
	case <-time.After(time.Second):
		t.Fatal("sessionEvents subscription was not opened")
	}

	sessionEvents.events <- timelineapp.DTO{
		ID:         "event-1",
		Type:       processdomain.CodexEventStatus,
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
	event, ok := data["sessionEvents"].(map[string]any)
	if !ok {
		t.Fatalf("sessionEvents payload = %#v", data["sessionEvents"])
	}
	content, _ := event["content"].(map[string]any)
	if event["id"] != "event-1" || event["orderKey"] != "order-1" || event["phase"] != "STANDALONE" || content["code"] != "session.running" {
		t.Fatalf("session event = %#v", event)
	}
}

func TestGraphQLWebSocketSessionUpdatesSubscriptionReceivesWaitingUserStatus(t *testing.T) {
	stream := make(chan sessioneventapp.UpdateDTO, 1)
	sessionEvents := &fakeSessionEventUseCase{updates: stream}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{
		SessionEvents: sessionEvents,
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
			"query": `subscription {
				sessionUpdates {
					eventType
					sessionId
					status {
						status
						currentNodeTitle
						availableActions
					}
				}
			}`,
		},
	})
	status := sessionapp.CardStatusDTO{
		Status: sessiondomain.StatusWaitingUser, CurrentNodeTitle: "Need input",
		AvailableActions: []string{"close"}, UpdatedAt: time.Now().UTC(),
	}
	stream <- sessioneventapp.UpdateDTO{
		ID: "status-1", Type: sessioneventapp.TypeStatus, SessionID: "session-1", Status: &status,
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
	stateItem, ok := data["sessionUpdates"].(map[string]any)
	if !ok {
		t.Fatalf("sessionUpdates payload = %#v", data["sessionUpdates"])
	}
	statusItem, ok := stateItem["status"].(map[string]any)
	if !ok {
		t.Fatalf("session state item = %#v", stateItem)
	}
	if stateItem["eventType"] != sessioneventapp.TypeStatus || stateItem["sessionId"] != "session-1" || statusItem["status"] != "waiting_user" || statusItem["currentNodeTitle"] != "Need input" {
		t.Fatalf("session status = %#v", stateItem)
	}
}

func TestAttachmentPreviewRequiresBearer(t *testing.T) {
	useCase := &fakeAttachmentUseCase{}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithAttachmentUseCase(useCase))

	req := httptest.NewRequest(http.MethodGet, "/files/attachment-1/preview", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/files/attachment-1/preview", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/files/attachment-1/download", nil)
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

func TestFileDownloadSupportsRange(t *testing.T) {
	reader := &testReadSeekCloser{Reader: strings.NewReader("0123456789")}
	useCase := &fakeAttachmentUseCase{
		stream: attachmentapp.Stream{
			Filename:   "video.mp4",
			MimeType:   "video/mp4",
			Size:       10,
			ModifiedAt: time.Unix(100, 0).UTC(),
			Reader:     reader,
			Seeker:     reader,
		},
	}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithAttachmentUseCase(useCase))
	req := httptest.NewRequest(http.MethodGet, "/files/artifact-1/download", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" {
		t.Fatalf("range response status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("range headers = %#v", rec.Header())
	}
}

func TestAttachmentPreviewWritesStructuredApplicationError(t *testing.T) {
	useCase := &fakeAttachmentUseCase{err: attachmentapp.ErrNotPreviewable}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithAttachmentUseCase(useCase))

	req := httptest.NewRequest(http.MethodGet, "/files/attachment-1/preview", nil)
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

type fakeSessionEventUseCase struct {
	events     chan timelineapp.DTO
	updates    chan sessioneventapp.UpdateDTO
	subscribed chan struct{}
}

func (u *fakeSessionEventUseCase) SessionEvents(context.Context, sessiondomain.ID) (<-chan timelineapp.DTO, error) {
	if u.subscribed == nil {
		u.subscribed = make(chan struct{})
	}
	close(u.subscribed)
	return u.events, nil
}

func (u *fakeSessionEventUseCase) SessionUpdates(context.Context) (<-chan sessioneventapp.UpdateDTO, error) {
	return u.updates, nil
}

type testReadSeekCloser struct {
	*strings.Reader
}

func (r *testReadSeekCloser) Close() error { return nil }

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
