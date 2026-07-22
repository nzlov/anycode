package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/infra/mcpstdio"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
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

func TestLocalMCPSocketPathIsProcessScoped(t *testing.T) {
	want := filepath.Join(os.TempDir(), "anycode-1000", "mcp-2000.sock")
	if got := localMCPSocketPath(1000, 2000); got != want {
		t.Fatalf("localMCPSocketPath() = %q, want %q", got, want)
	}
	if first, second := localMCPSocketPath(1000, 2000), localMCPSocketPath(1000, 2000); first != second {
		t.Fatalf("same process has unstable MCP sockets %q and %q", first, second)
	}
	if first, second := localMCPSocketPath(1000, 2000), localMCPSocketPath(1000, 2001); first == second {
		t.Fatalf("different processes share MCP socket %q", first)
	}
	if got := len(localMCPSocketPath(1000, 2000)); got >= 100 {
		t.Fatalf("MCP socket path length = %d, want < 100", got)
	}
}

func TestMCPUnixServersAreProcessIsolated(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "mcp-1001.sock")
	secondPath := filepath.Join(dir, "mcp-1002.sock")
	firstSessions := &isolatedMCPSessionUseCase{batchID: "batch-from-first"}
	secondSessions := &isolatedMCPSessionUseCase{batchID: "batch-from-second"}

	stopFirst, err := startMCPUnixServer(config.Config{}, graph.UseCases{Sessions: firstSessions}, firstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer stopFirst()
	assertSocketPermissions(t, firstPath)
	assertMCPStdioResponse(t, firstPath, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, `"answer_user"`)
	assertMCPStdioResponse(t, firstPath, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, `"name":"anycode"`)
	firstInfo, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}

	stopSecond, err := startMCPUnixServer(config.Config{}, graph.UseCases{Sessions: secondSessions}, secondPath)
	if err != nil {
		t.Fatal(err)
	}
	assertMCPStdioResponse(t, firstPath, answerUserRequest(), "batch-from-first")
	assertMCPStdioResponse(t, secondPath, answerUserRequest(), "batch-from-second")
	if firstSessions.calls != 1 || secondSessions.calls != 1 {
		t.Fatalf("answer_user calls: first=%d second=%d", firstSessions.calls, secondSessions.calls)
	}
	stopSecond()
	assertMCPStdioResponse(t, firstPath, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, `"answer_user"`)
	assertMCPStdioResponse(t, firstPath, answerUserRequest(), "batch-from-first")
	assertSameSocket(t, firstPath, firstInfo)

	blockedParent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := startMCPUnixServer(config.Config{}, graph.UseCases{}, filepath.Join(blockedParent, "mcp-1003.sock")); err == nil {
		t.Fatal("expected second MCP server startup to fail")
	}
	assertMCPStdioResponse(t, firstPath, answerUserRequest(), "batch-from-first")
	assertSameSocket(t, firstPath, firstInfo)
}

type isolatedMCPSessionUseCase struct {
	sessionapp.UseCase
	batchID questiondomain.BatchID
	calls   int
}

func (u *isolatedMCPSessionUseCase) RequestUserAnswer(_ context.Context, input sessionapp.RequestUserAnswerInput) (questionapp.BatchDTO, error) {
	u.calls++
	return questionapp.BatchDTO{ID: u.batchID, SessionID: questiondomain.SessionID(input.SessionID), Questions: input.Questions}, nil
}

func answerUserRequest() string {
	return `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"answer_user","arguments":{"questions":[{"title":"Continue?","options":[{"id":"yes","label":"Yes"}]}]}}}`
}

func assertMCPStdioResponse(t *testing.T, socketPath string, request string, want string) {
	t.Helper()
	input := strings.NewReader(fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(request), request))
	var output bytes.Buffer
	if err := mcpstdio.Run(context.Background(), input, &output, mcpstdio.Config{SessionID: "session-1", Socket: socketPath}); err != nil {
		t.Fatalf("mcp-stdio via %s: %v", filepath.Base(socketPath), err)
	}
	if !strings.Contains(output.String(), want) {
		t.Fatalf("mcp-stdio via %s response %q missing %q", filepath.Base(socketPath), output.String(), want)
	}
}

func assertSameSocket(t *testing.T, path string, before os.FileInfo) {
	t.Helper()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatalf("socket %s was replaced", filepath.Base(path))
	}
}

func assertSocketPermissions(t *testing.T, socketPath string) {
	t.Helper()
	dirInfo, err := os.Stat(filepath.Dir(socketPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("MCP socket directory mode = %o, want 700", got)
	}
	socketInfo, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := socketInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("MCP socket mode = %o, want 600", got)
	}
}

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
