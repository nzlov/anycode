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

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
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
	h := NewHandler(config.Config{AccessKey: "owner"}, WithGraphQLUseCases(graph.UseCases{Sessions: sessions, Sharing: shares}))
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
