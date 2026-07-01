package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
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

	rec = doGraphQL(handler, "Bearer secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("graphql with bearer status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"__typename":"Query"`) {
		t.Fatalf("graphql response missing query typename: %s", rec.Body.String())
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
