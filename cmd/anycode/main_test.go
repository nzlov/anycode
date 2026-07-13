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

func TestStartupReconcilesBeforeQueueDrain(t *testing.T) {
	sessions := &fakeRecoverySessions{recoverCount: 2, drainCount: 1}
	if err := reconcileInterruptedSessions(context.Background(), sessions); err != nil {
		t.Fatalf("reconcileInterruptedSessions() error = %v", err)
	}
	if strings.Join(sessions.calls, ",") != "recover" {
		t.Fatalf("reconcile calls = %#v", sessions.calls)
	}
	if err := drainQueuedSessions(context.Background(), sessions); err != nil {
		t.Fatalf("drainQueuedSessions() error = %v", err)
	}
	if strings.Join(sessions.calls, ",") != "recover,drain" {
		t.Fatalf("startup calls = %#v", sessions.calls)
	}
}

type fakeCodexProber struct {
	capabilities processdomain.CodexCapabilities
	err          error
}

type fakeRecoverySessions struct {
	calls        []string
	recoverCount int
	drainCount   int
	recoverErr   error
	drainErr     error
}

func (s *fakeRecoverySessions) RecoverInterruptedSessions(context.Context) (int, error) {
	s.calls = append(s.calls, "recover")
	return s.recoverCount, s.recoverErr
}

func (s *fakeRecoverySessions) DrainQueuedSessions(context.Context) (int, error) {
	s.calls = append(s.calls, "drain")
	return s.drainCount, s.drainErr
}

func (p fakeCodexProber) Probe(context.Context) (processdomain.CodexCapabilities, error) {
	return p.capabilities, p.err
}
