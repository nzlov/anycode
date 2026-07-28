package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	tokens := newFilePreviewTokens("secret")
	tokens.now = func() time.Time { return now }
	token := tokens.issue("attachment-1")
	if !tokens.valid("attachment-1", token) {
		t.Fatal("new token should be valid")
	}
	now = now.Add(filePreviewTokenTTL + time.Second)
	if tokens.valid("attachment-1", token) {
		t.Fatal("expired token should be invalid")
	}
}

func newReadSeekCloser(value string) *testReadSeekCloser {
	return &testReadSeekCloser{Reader: strings.NewReader(value)}
}
