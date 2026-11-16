package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	"github.com/nzlov/anycode/internal/application/sharing"
	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
)

type sharedSessions struct {
	sessionapp.UseCase
	reads, stops int
}

func TestSharingChangesAreDistributedOverAuthenticatedWebSocket(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dir, "db"), DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []session.ID{"allowed", "other"} {
		if err := store.Sessions().Create(ctx, session.Session{ID: id, ProjectID: "project", Mode: session.ModeChat, Status: session.StatusStopped}); err != nil {
			t.Fatal(err)
		}
	}
	shares := sharing.New(store.Shares(), store.Sessions(), &sharedSessions{}, nil, nil, nil, sharing.WithEvents(eventapp.New()))
	server := httptest.NewServer(NewHandler(config.Config{AccessKey: "owner"}, WithGraphQLUseCases(graph.UseCases{Sharing: shares})))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/graphql", http.Header{"Sec-WebSocket-Protocol": []string{"graphql-transport-ws"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	writeSocketJSON(t, conn, map[string]any{"type": "connection_init", "payload": map[string]any{"Authorization": "Bearer owner"}})
	assertSocketMessageType(t, conn, "connection_ack")
	writeSocketJSON(t, conn, map[string]any{
		"id": "sharing", "type": "subscribe",
		"payload": map[string]any{
			"query": `subscription { sharingUpdates(sessionId: "allowed") { eventType sessionId shareId messageId status occurredAt } }`,
		},
	})
	globalActive := false
	expect := func(kind, shareID, messageID, status string) {
		t.Helper()
		count := 1
		if globalActive {
			count = 2
		}
		seen := map[string]bool{}
		for range count {
			message := readSocketMessage(t, conn)
			payload, _ := message["payload"].(map[string]any)
			data, _ := payload["data"].(map[string]any)
			update, _ := data["sharingUpdates"].(map[string]any)
			operation, _ := message["id"].(string)
			if message["type"] != "next" || (operation != "sharing" && operation != "all-sharing") || seen[operation] || update["eventType"] != kind || update["sessionId"] != "allowed" || update["occurredAt"] == nil {
				t.Fatalf("sharing message = %#v", message)
			}
			seen[operation] = true
			for key, value := range map[string]string{"shareId": shareID, "messageId": messageID, "status": status} {
				if (value == "" && update[key] != nil) || (value != "" && update[key] != value) {
					t.Fatalf("sharing %s = %v, want %q", key, update[key], value)
				}
			}
		}
	}
	expect(sharing.TypeReady, "", "", "")
	writeSocketJSON(t, conn, map[string]any{
		"id": "all-sharing", "type": "subscribe",
		"payload": map[string]any{"query": `subscription { sharingUpdates { eventType sessionId shareId messageId status occurredAt } }`},
	})
	ready := readSocketMessage(t, conn)
	readyPayload, _ := ready["payload"].(map[string]any)
	readyData, _ := readyPayload["data"].(map[string]any)
	readyUpdate, _ := readyData["sharingUpdates"].(map[string]any)
	if ready["id"] != "all-sharing" || readyUpdate["eventType"] != sharing.TypeReady || readyUpdate["sessionId"] != nil {
		t.Fatalf("global subscription ready = %#v", ready)
	}
	globalActive = true
	// The global listener receives other cards; the scoped listener must ignore them.
	if _, _, err := shares.Create(ctx, "other"); err != nil {
		t.Fatal(err)
	}
	other := readSocketMessage(t, conn)
	otherPayload, _ := other["payload"].(map[string]any)
	otherData, _ := otherPayload["data"].(map[string]any)
	otherUpdate, _ := otherData["sharingUpdates"].(map[string]any)
	if other["id"] != "all-sharing" || otherUpdate["sessionId"] != "other" || otherUpdate["eventType"] != sharing.TypeLinkCreated {
		t.Fatalf("other card event = %#v", other)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/sessions/allowed/shares", nil)
	request.Header.Set("Authorization", "Bearer owner")
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Share session.ShareLink `json:"share"`
		Token string            `json:"token"`
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("create share = %d, %v", response.StatusCode, err)
	}
	expect(sharing.TypeLinkCreated, result.Share.ID, "", "")
	_, credential, err := shares.Claim(ctx, result.Share.ID, result.Token)
	if err != nil {
		t.Fatal(err)
	}
	expect(sharing.TypeLinkClaimed, result.Share.ID, "", "")
	msg, err := shares.Submit(ctx, result.Share.ID, credential, "review me", session.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	expect(sharing.TypeMessageSubmitted, result.Share.ID, msg.ID, "")
	if _, err := shares.Review(ctx, msg.ID, false); err != nil {
		t.Fatal(err)
	}
	expect(sharing.TypeMessageStatusChanged, result.Share.ID, msg.ID, "rejected")
	if err := shares.Revoke(ctx, result.Share.ID); err != nil {
		t.Fatal(err)
	}
	expect(sharing.TypeLinkRevoked, result.Share.ID, "", "")
	writeSocketJSON(t, conn, map[string]any{"id": "sharing", "type": "complete"})
}

func TestSharedFilesBindShareResourceAndRevocation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dir, "db"), DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.Sessions().Create(ctx, session.Session{ID: "allowed", ProjectID: "p", Mode: session.ModeChat, Status: session.StatusStopped}); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dir)
	attachments := attachmentapp.New(store.Attachments(), files)
	makeFile := func(id session.ID) string {
		a, e := files.Stage(ctx, session.StageAttachmentInput{Filename: "test.txt", MimeType: "text/plain", Size: 7, Reader: strings.NewReader("content")})
		if e != nil {
			t.Fatal(e)
		}
		v, e := files.Promote(ctx, session.PromoteAttachmentInput{Staged: a, SessionID: id, SourceType: session.AttachmentSourceRequirement, SourceID: string(id)})
		if e != nil {
			t.Fatal(e)
		}
		return string(v.ID)
	}
	allowedFile := makeFile("allowed")
	otherFile := makeFile("other")
	shares := sharing.New(store.Shares(), store.Sessions(), &sharedSessions{}, attachments, store.Attachments(), files)
	link, token, _ := shares.Create(ctx, "allowed")
	_, guest, _ := shares.Claim(ctx, link.ID, token)
	h := NewHandler(config.Config{AccessKey: "owner", ArtifactPreviewMaxBytes: 128 << 20}, WithGraphQLUseCases(graph.UseCases{Sharing: shares, Attachments: attachments}))
	request := func(method, path, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	base := "/api/shared/" + link.ID + "/files/"
	if w := request("POST", base+otherFile+"/preview-token", guest); w.Code != 403 {
		t.Fatalf("other file authorized: %d", w.Code)
	}
	if w := request("POST", base+allowedFile+"/preview-token", ""); w.Code != 403 {
		t.Fatal("anonymous token issued")
	}
	w := request("POST", base+allowedFile+"/preview-token", guest)
	if w.Code != 200 {
		t.Fatalf("issue: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		URL string `json:"url"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	w = request("GET", response.URL, "")
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	body, _ := io.ReadAll(w.Result().Body)
	if string(body) != "content" {
		t.Fatalf("content=%q", body)
	}
	if w = request("GET", strings.Replace(response.URL, allowedFile, otherFile, 1), ""); w.Code != 403 {
		t.Fatal("token crossed files")
	}
	if w = request("GET", strings.Replace(response.URL, "/preview?", "/download?", 1), ""); w.Code != 403 {
		t.Fatal("preview token enabled download")
	}
	if w = request("GET", strings.TrimPrefix(response.URL, "/api/shared/"+link.ID), ""); w.Code != 401 {
		t.Fatal("guest token reached owner file route")
	}
	shares.Revoke(ctx, link.ID)
	if w = request("GET", response.URL, ""); w.Code != 403 {
		t.Fatal("issued file token survived revocation")
	}
}

func (s *sharedSessions) GetSession(_ context.Context, id session.ID) (sessionapp.DetailDTO, error) {
	s.reads++
	return sessionapp.DetailDTO{DTO: sessionapp.DTO{ID: id, Mode: session.ModeChat, Status: session.StatusStopped}}, nil
}
func (s *sharedSessions) StopSession(_ context.Context, id session.ID) (sessionapp.DTO, error) {
	s.stops++
	return sessionapp.DTO{ID: id}, nil
}
func (s *sharedSessions) AppendPrompt(context.Context, sessionapp.AppendPromptInput) (sessionapp.PromptAppendDTO, error) {
	panic("share bypassed review")
}
func TestSharedGraphQLScopesAliasesFragmentsAndMutations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dir, "db"), DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store.Sessions().Create(ctx, session.Session{ID: "allowed", ProjectID: "p", Mode: session.ModeChat, Status: session.StatusStopped})
	sessions := &sharedSessions{}
	shares := sharing.New(store.Shares(), store.Sessions(), sessions, nil, nil, nil)
	link, token, err := shares.Create(ctx, "allowed")
	if err != nil {
		t.Fatal(err)
	}
	_, guest, err := shares.Claim(ctx, link.ID, token)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := shares.Submit(ctx, link.ID, guest, "pending review", session.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := shares.Submit(ctx, link.ID, guest, "already reviewed", session.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shares.Review(ctx, rejected.ID, false); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(config.Config{AccessKey: "owner"}, WithGraphQLUseCases(graph.UseCases{Sessions: sessions, Sharing: shares}))
	for _, path := range []string{"/api/share-messages/pending", "/api/share-messages/" + pending.ID} {
		for _, key := range []string{"", guest, "owner"} {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if key != "owner" {
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("guest accessed review query %s: %d", path, w.Code)
				}
				continue
			}
			if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("review query %s: %d %s", path, w.Code, w.Body.String())
			}
			if strings.HasSuffix(path, "/pending") {
				var messages []session.SharedMessage
				if err := json.Unmarshal(w.Body.Bytes(), &messages); err != nil || len(messages) != 1 || messages[0].ID != pending.ID {
					t.Fatalf("pending messages = %s, error = %v", w.Body.String(), err)
				}
			} else {
				var message session.SharedMessage
				if err := json.Unmarshal(w.Body.Bytes(), &message); err != nil || message.ID != pending.ID || message.Content.Body != "pending review" {
					t.Fatalf("message detail = %s, error = %v", w.Body.String(), err)
				}
			}
		}
	}
	request := func(path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+key)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, tt := range []struct {
		query   string
		allowed bool
	}{
		{`{ alias: session(id:"allowed") { id } }`, true},
		{`query { ...A } fragment A on Query { session(id:"allowed") { id } }`, true},
		{`{ session(id:"other") { id } }`, false},
		{`{ projects { id } }`, false},
		{`{ generalSettings { maxConcurrentSessions } }`, false},
		{`mutation { stopSession(id:"allowed") { id } }`, true},
		{`mutation { stopSession(id:"other") { id } }`, false},
		{`mutation { appendPrompt(input:{sessionId:"allowed",body:"bypass"}) { id } }`, false},
		{`mutation { openSessionTerminal(sessionId:"allowed") { id } }`, false},
		{`{ sessionFiles(input:{sessionId:"other"}) { id } }`, false},
	} {
		t.Run(tt.query, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"query": tt.query})
			w := request("/api/shared/"+link.ID+"/graphql", string(body), guest)
			hasError := bytes.Contains(w.Body.Bytes(), []byte(`"errors"`))
			if hasError == tt.allowed {
				t.Fatalf("status %d response %s", w.Code, w.Body.String())
			}
		})
	}
	if sessions.reads != 2 || sessions.stops != 1 {
		t.Fatalf("reads %d stops %d", sessions.reads, sessions.stops)
	}
	if w := request("/graphql", `{"query":"{ projects { id } }"}`, guest); w.Code != 401 {
		t.Fatal("guest reached owner graphql")
	}
	if w := request("/api/sessions/allowed/shares", "", guest); w.Code != 401 {
		t.Fatal("guest created share")
	}
	shares.Revoke(ctx, link.ID)
	if w := request("/api/shared/"+link.ID+"/graphql", `{"query":"{ session(id:\"allowed\") { id } }"}`, guest); w.Code != 403 {
		t.Fatalf("revoked request %d", w.Code)
	}
}
