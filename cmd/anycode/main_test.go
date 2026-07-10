package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
)

func TestLocalHTTPBaseURL(t *testing.T) {
	tests := map[string]string{
		"":                      "http://127.0.0.1:8080",
		":8080":                 "http://127.0.0.1:8080",
		"127.0.0.1:18080":       "http://127.0.0.1:18080",
		"http://localhost:8080": "http://localhost:8080",
	}
	for input, want := range tests {
		if got := localHTTPBaseURL(input); got != want {
			t.Fatalf("localHTTPBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLocalMCPSocketPathUsesTempDir(t *testing.T) {
	want := filepath.Join(os.TempDir(), fmt.Sprintf("anycode-%d", os.Getuid()), "mcp.sock")
	if got := localMCPSocketPath(); got != want {
		t.Fatalf("localMCPSocketPath() = %q, want %q", got, want)
	}
}

func TestEnsureCodexReady(t *testing.T) {
	got, err := ensureCodexReady(context.Background(), fakeCodexProber{
		capabilities: processdomain.CodexCapabilities{
			Version:        "codex 1.2.3",
			SupportsExec:   true,
			SupportsResume: true,
			Models:         []processdomain.CodexModel{{Slug: "gpt-5.6-sol"}},
		},
	})
	if err != nil {
		t.Fatalf("ensureCodexReady() error = %v", err)
	}
	if len(got.Models) != 1 {
		t.Fatalf("ensureCodexReady() capabilities = %+v", got)
	}
}

func TestEnsureCodexReadyRejectsProbeFailure(t *testing.T) {
	_, err := ensureCodexReady(context.Background(), fakeCodexProber{err: errors.New("not found")})
	if err == nil || !strings.Contains(err.Error(), "probe codex cli") {
		t.Fatalf("ensureCodexReady() error = %v", err)
	}
}

func TestEnsureCodexReadyRequiresExecAndResume(t *testing.T) {
	_, err := ensureCodexReady(context.Background(), fakeCodexProber{
		capabilities: processdomain.CodexCapabilities{
			SupportsExec: true,
			Models:       []processdomain.CodexModel{{Slug: "gpt-5.6-sol"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "exec resume") {
		t.Fatalf("ensureCodexReady() error = %v", err)
	}

	_, err = ensureCodexReady(context.Background(), fakeCodexProber{
		capabilities: processdomain.CodexCapabilities{
			SupportsResume: true,
			Models:         []processdomain.CodexModel{{Slug: "gpt-5.6-sol"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "support exec") {
		t.Fatalf("ensureCodexReady() error = %v", err)
	}
}

func TestEnsureCodexReadyRequiresModelOptions(t *testing.T) {
	_, err := ensureCodexReady(context.Background(), fakeCodexProber{
		capabilities: processdomain.CodexCapabilities{
			SupportsExec:   true,
			SupportsResume: true,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "model options") {
		t.Fatalf("ensureCodexReady() error = %v", err)
	}
}

type fakeCodexProber struct {
	capabilities processdomain.CodexCapabilities
	err          error
}

func (p fakeCodexProber) Probe(context.Context) (processdomain.CodexCapabilities, error) {
	return p.capabilities, p.err
}
