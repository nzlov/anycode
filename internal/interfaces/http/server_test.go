package http

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
