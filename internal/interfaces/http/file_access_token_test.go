package http

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	"github.com/nzlov/anycode/internal/infra/config"
)

func TestFilePreviewTokenAuthorizesOnlyBoundFile(t *testing.T) {
	useCase := &fakeAttachmentUseCase{stream: attachmentapp.Stream{
		Filename: "image.png",
		MimeType: "image/png",
		Reader:   newReadSeekCloser("png-bytes"),
		Seeker:   newReadSeekCloser("png-bytes"),
	}}
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithAttachmentUseCase(useCase))

	req := httptest.NewRequest(http.MethodPost, "/files/attachment-1/preview-token", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token without bearer status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/files/attachment-1/preview-token", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if payload.URL == "" || strings.Contains(payload.URL, "secret") {
		t.Fatalf("preview URL = %q", payload.URL)
	}

	req = httptest.NewRequest(http.MethodGet, payload.URL, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "png-bytes" {
		t.Fatalf("token preview status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("token preview security headers = %#v", rec.Header())
	}

	req = httptest.NewRequest(http.MethodGet, strings.Replace(payload.URL, "attachment-1", "attachment-2", 1), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token reused for another file status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, strings.Replace(payload.URL, "/preview?", "/download?", 1), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("preview token reused for download status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestFilePreviewTokenExpires(t *testing.T) {
	now := time.Unix(1_000, 0)
	tokens := newFileAccessTokens("secret")
	tokens.now = func() time.Time { return now }
	token := tokens.issue("attachment-1")
	if !tokens.valid("attachment-1", token) {
		t.Fatal("new token should be valid")
	}
	now = now.Add(fileAccessTokenTTL + time.Second)
	if tokens.valid("attachment-1", token) {
		t.Fatal("expired token should be invalid")
	}
}

func newReadSeekCloser(value string) *testReadSeekCloser {
	return &testReadSeekCloser{Reader: strings.NewReader(value)}
}

func TestFileDownloadTokenStreamsOnlyBoundDownload(t *testing.T) {
	reader := newReadSeekCloser("0123456789")
	useCase := &fakeAttachmentUseCase{stream: attachmentapp.Stream{
		Filename: "报告.zip", MimeType: "application/zip", Size: 10, Reader: reader, Seeker: reader,
	}}
	handler := NewHandler(config.Config{AccessKey: "secret", ArtifactPreviewMaxBytes: 1}, WithAttachmentUseCase(useCase))
	target := requestFileToken(t, handler, "/files/artifact-1/download-token")
	if useCase.openedID != "" {
		t.Fatal("issuing a download token must not open or read the file")
	}
	for _, address := range []string{
		"/files/artifact-1/download",
		strings.Replace(target, "artifact-1", "artifact-2", 1),
		strings.Replace(target, "/download?", "/preview?", 1),
		target + "tampered",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, address, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unbound download %q status = %d", address, rec.Code)
		}
	}
	if useCase.openedID != "" {
		t.Fatal("unauthorized downloads must not open the file")
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" {
		t.Fatalf("direct range download status=%d body=%q", rec.Code, rec.Body.String())
	}
	if useCase.openedMode != attachmentapp.OpenDownload {
		t.Fatalf("download opened mode = %q", useCase.openedMode)
	}
	disposition, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
	if err != nil || disposition != "attachment" || params["filename"] != "报告.zip" {
		t.Fatalf("download disposition = %q", rec.Header().Get("Content-Disposition"))
	}
	if rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("download security headers = %#v", rec.Header())
	}
}

func TestWorkspaceDownloadTokenBindsSessionPathAndMode(t *testing.T) {
	useCase := &fakeWorkspaceFileUseCase{}
	handler := NewHandler(config.Config{AccessKey: "secret", ArtifactPreviewMaxBytes: 1}, WithWorkspaceFileUseCase(useCase))
	path := "src/报告 + #&.java"
	target := requestFileToken(t, handler, "/api/sessions/session-1/workspace-file/download-token?"+url.Values{"path": []string{path}}.Encode())
	if useCase.calls != 0 {
		t.Fatal("issuing a workspace download token must not read the file")
	}
	for _, mutate := range []func(*url.URL){
		func(u *url.URL) { u.Path = strings.Replace(u.Path, "session-1", "session-2", 1) },
		func(u *url.URL) { q := u.Query(); q.Set("path", "other.txt"); u.RawQuery = q.Encode() },
		func(u *url.URL) { q := u.Query(); q.Del("download"); u.RawQuery = q.Encode() },
		func(u *url.URL) { q := u.Query(); q.Del("token"); u.RawQuery = q.Encode() },
	} {
		u, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		mutate(u)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u.String(), nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("changed workspace target %q status=%d", u, rec.Code)
		}
	}
	if useCase.calls != 0 {
		t.Fatal("unauthorized workspace downloads must not open the file")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "class App {}\n" {
		t.Fatalf("workspace download status=%d body=%q", rec.Code, rec.Body.String())
	}
	if useCase.input.SessionID != "session-1" || useCase.input.Path != path {
		t.Fatalf("download input = %#v", useCase.input)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("download disposition = %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestLargeWorkspaceFileMetadataAllowsDownload(t *testing.T) {
	handler := NewHandler(config.Config{AccessKey: "secret", ArtifactPreviewMaxBytes: 1}, WithWorkspaceFileUseCase(&fakeWorkspaceFileUseCase{}))
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		req := httptest.NewRequest(method, "/api/sessions/session-1/workspace-file?path=App.java", nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if method == http.MethodHead {
			if rec.Code != http.StatusOK || rec.Header().Get("Content-Length") != "13" || rec.Body.Len() != 0 {
				t.Fatalf("large file metadata status=%d headers=%v body=%q", rec.Code, rec.Header(), rec.Body.String())
			}
		} else if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("large file preview status=%d", rec.Code)
		}
	}
}

func TestFileAccessTokenRejectsExpiredOrForgedCredentials(t *testing.T) {
	now := time.Unix(1000, 0)
	tokens := newFileAccessTokens("secret")
	tokens.now = func() time.Time { return now }
	resource := "/files/file-1/download?"
	token := tokens.issue(resource)
	otherKey := newFileAccessTokens("other-secret")
	otherKey.now = tokens.now
	if otherKey.valid(resource, token) {
		t.Fatal("token accepted with a different access key")
	}
	for _, invalid := range []string{"", "v2.1060.bad", "v1.invalid.bad", token + "x"} {
		if tokens.valid(resource, invalid) {
			t.Fatalf("accepted invalid token %q", invalid)
		}
	}
	now = now.Add(fileAccessTokenTTL + time.Second)
	handler := fileAccessAuth("secret", tokens, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("expired token reached file handler") }))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, resource+"token="+token, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired download status = %d", rec.Code)
	}
}

func requestFileToken(t *testing.T, handler http.Handler, endpoint string) string {
	t.Helper()
	for _, auth := range []string{"", "Bearer incorrect"} {
		req := httptest.NewRequest(http.MethodPost, endpoint, nil)
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("token issuer accepted %q: status=%d", auth, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, endpoint, nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token issuer status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.URL == "" || strings.Contains(payload.URL, "secret") || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("invalid token response headers=%v body=%s", rec.Header(), rec.Body.String())
	}
	return payload.URL
}
