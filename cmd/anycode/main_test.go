package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
)

func TestEnsureCodexReady(t *testing.T) {
	got, err := ensureCodexReady(context.Background(), fakeCodexProber{
		capabilities: processdomain.CodexCapabilities{
			Version:           "codex 1.2.3",
			SupportsAppServer: true,
			Models:            []processdomain.CodexModel{{Slug: "gpt-5.6-sol"}},
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

func TestEnsureCodexReadyRequiresAppServer(t *testing.T) {
	_, err := ensureCodexReady(context.Background(), fakeCodexProber{
		capabilities: processdomain.CodexCapabilities{
			Models: []processdomain.CodexModel{{Slug: "gpt-5.6-sol"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "app-server") {
		t.Fatalf("ensureCodexReady() error = %v", err)
	}
}

func TestEnsureCodexReadyRequiresModelOptions(t *testing.T) {
	_, err := ensureCodexReady(context.Background(), fakeCodexProber{
		capabilities: processdomain.CodexCapabilities{
			SupportsAppServer: true,
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
	if err := reconcileWorktreeCleanup(context.Background(), sessions); err != nil {
		t.Fatalf("reconcileWorktreeCleanup() error = %v", err)
	}
	if err := drainQueuedSessions(context.Background(), sessions); err != nil {
		t.Fatalf("drainQueuedSessions() error = %v", err)
	}
	if strings.Join(sessions.calls, ",") != "recover,worktree,drain" {
		t.Fatalf("startup calls = %#v", sessions.calls)
	}
}

type fakeCodexProber struct {
	capabilities processdomain.CodexCapabilities
	err          error
}

type fakeRecoverySessions struct {
	calls         []string
	recoverCount  int
	drainCount    int
	worktreeCount int
	recoverErr    error
	drainErr      error
	worktreeErr   error
}

func (s *fakeRecoverySessions) RecoverInterruptedSessions(context.Context) (int, error) {
	s.calls = append(s.calls, "recover")
	return s.recoverCount, s.recoverErr
}

func (s *fakeRecoverySessions) DrainQueuedSessions(context.Context) (int, error) {
	s.calls = append(s.calls, "drain")
	return s.drainCount, s.drainErr
}

func (s *fakeRecoverySessions) ReconcileWorktreeCleanup(context.Context) (int, error) {
	s.calls = append(s.calls, "worktree")
	return s.worktreeCount, s.worktreeErr
}

func (p fakeCodexProber) Probe(context.Context) (processdomain.CodexCapabilities, error) {
	return p.capabilities, p.err
}
