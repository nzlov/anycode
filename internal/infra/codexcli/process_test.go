package codexcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

func TestStartBuildsExecJSONCommandAndStreamsEvents(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	pwdFile := filepath.Join(dir, "pwd")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
pwd > "$CODEX_PWD_FILE"
printf '%s\n' '{"id":"evt-1","type":"session.created","session_id":"codex-session-1"}'
printf '%s\n' '{"unexpected":true}'
`)
	t.Setenv("CODEX_ARGS_FILE", argsFile)
	t.Setenv("CODEX_PWD_FILE", pwdFile)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{
		ProcessRunID:    "process-run-1",
		SessionID:       "session-1",
		Workdir:         dir,
		Prompt:          "implement adapter",
		Model:           "gpt-test",
		ReasoningEffort: "medium",
		PermissionMode:  "workspace-write",
		AttachmentPaths: []string{"/kept/in/input.png"},
		ImagePaths:      []string{"/kept/in/input.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.ProcessRunID != "process-run-1" {
		t.Fatalf("ProcessRunID = %q", handle.ProcessRunID)
	}
	if handle.PID == 0 {
		t.Fatal("PID is empty")
	}

	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 2)

	if got[0].EventID != "evt-1" || got[0].Type != "session.created" {
		t.Fatalf("first event = %+v", got[0])
	}
	if got[1].Type != "unknown" {
		t.Fatalf("unknown event type = %q", got[1].Type)
	}
	if string(got[1].Raw) != `{"unexpected":true}` {
		t.Fatalf("unknown raw = %q", got[1].Raw)
	}

	args := strings.TrimSpace(readFile(t, argsFile))
	want := `exec --json --skip-git-repo-check -C ` + dir + ` -m gpt-test -c model_reasoning_effort="medium" --sandbox workspace-write -i /kept/in/input.png implement adapter`
	if args != want {
		t.Fatalf("args = %q, want %q", args, want)
	}
	if gotDir := strings.TrimSpace(readFile(t, pwdFile)); gotDir != dir {
		t.Fatalf("pwd = %q, want %q", gotDir, dir)
	}
}

func TestResumeBuildsResumeJSONCommandInWorkdir(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	pwdFile := filepath.Join(dir, "pwd")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
pwd > "$CODEX_PWD_FILE"
`)
	t.Setenv("CODEX_ARGS_FILE", argsFile)
	t.Setenv("CODEX_PWD_FILE", pwdFile)

	handle, err := New(bin).Resume(context.Background(), process.CodexResumeInput{
		ProcessRunID:   "process-run-2",
		SessionID:      "session-1",
		CodexSessionID: "codex-session-1",
		Workdir:        dir,
		Prompt:         "next node",
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.CodexSessionID != "codex-session-1" {
		t.Fatalf("CodexSessionID = %q", handle.CodexSessionID)
	}
	waitForFile(t, argsFile)
	waitForFile(t, pwdFile)

	if args := strings.TrimSpace(readFile(t, argsFile)); args != "exec resume --json --skip-git-repo-check codex-session-1 next node" {
		t.Fatalf("args = %q", args)
	}
	if gotDir := strings.TrimSpace(readFile(t, pwdFile)); gotDir != dir {
		t.Fatalf("pwd = %q, want %q", gotDir, dir)
	}
}

func TestStartProcessOutlivesCallerContextCancellation(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
sleep 0.2
printf '%s\n' '{"id":"evt-after-cancel","type":"agent_message","text":"still running"}'
`)
	ctx, cancel := context.WithCancel(context.Background())
	handle, err := New(bin).Start(ctx, process.CodexStartInput{ProcessRunID: "process-run-context"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 1)
	if got[0].EventID != "evt-after-cancel" {
		t.Fatalf("event after caller context cancel = %+v", got[0])
	}
}

func TestStartInjectsMCPServerConfig(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	tokenFile := filepath.Join(dir, "token")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
printf '%s\n' "$ANYCODE_MCP_TOKEN" > "$CODEX_TOKEN_FILE"
`)
	t.Setenv("CODEX_ARGS_FILE", argsFile)
	t.Setenv("CODEX_TOKEN_FILE", tokenFile)

	handle, err := New(bin, WithMCP("http://127.0.0.1:8080", "secret")).Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "process-run-mcp",
		SessionID:    "session-1",
		Workdir:      dir,
		Prompt:       "use answer_user when needed",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, argsFile)
	waitForFile(t, tokenFile)
	if handle.ProcessRunID != "process-run-mcp" {
		t.Fatalf("ProcessRunID = %q", handle.ProcessRunID)
	}
	args := strings.TrimSpace(readFile(t, argsFile))
	for _, want := range []string{
		`-c mcp_servers.anycode.type="streamable_http"`,
		`-c mcp_servers.anycode.url="http://127.0.0.1:8080/mcp/sessions/session-1"`,
		`-c mcp_servers.anycode.bearer_token_env_var="ANYCODE_MCP_TOKEN"`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if token := strings.TrimSpace(readFile(t, tokenFile)); token != "secret" {
		t.Fatalf("ANYCODE_MCP_TOKEN = %q", token)
	}
}

func TestEventsParsesNestedMessageTypeAndInvalidJSON(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"msg":{"type":"agent_message"},"text":"hello"}'
printf '%s\n' 'not-json'
`)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{})
	if err == nil {
		t.Fatal("expected missing process run id error")
	}

	handle, err = New(bin).Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-3"})
	if err != nil {
		t.Fatal(err)
	}
	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 2)
	if got[0].Type != "agent_message" {
		t.Fatalf("nested type = %q", got[0].Type)
	}
	if got[1].Type != "invalid_json" || string(got[1].Raw) != "not-json" {
		t.Fatalf("invalid event = %+v", got[1])
	}
}

func TestEventsEmitsProcessExitWithFailureCode(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"agent_message","text":"before exit"}'
echo "model gpt-test is not supported" >&2
exit 7
`)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-exit"})
	if err != nil {
		t.Fatal(err)
	}
	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 2)
	if got[1].Type != "process.exit" {
		t.Fatalf("exit event type = %q", got[1].Type)
	}
	if got[1].Payload["exitCode"] != 7 {
		t.Fatalf("exit payload = %#v", got[1].Payload)
	}
	if got[1].Payload["failureReason"] == "" {
		t.Fatalf("exit payload missing failureReason: %#v", got[1].Payload)
	}
	if !strings.Contains(got[1].Payload["failureReason"].(string), "model gpt-test is not supported") {
		t.Fatalf("exit payload missing stderr: %#v", got[1].Payload)
	}
}

func TestStopKillsActiveProcess(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	childPIDFile := filepath.Join(dir, "child-pid")
	bin := fakeCodex(t, `#!/bin/sh
touch "$CODEX_STARTED_FILE"
sleep 30 &
printf '%s\n' "$!" > "$CODEX_CHILD_PID_FILE"
trap 'exit 0' TERM INT
wait
`)
	t.Setenv("CODEX_STARTED_FILE", started)
	t.Setenv("CODEX_CHILD_PID_FILE", childPIDFile)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{})
	if err == nil {
		t.Fatal("expected missing process run id error")
	}

	handle, err = client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-4"})
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, started)
	waitForFile(t, childPIDFile)

	if err := client.Stop(context.Background(), handle.ProcessRunID); err != nil {
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(readFile(t, childPIDFile)))
	if err != nil {
		t.Fatal(err)
	}
	waitForProcessExit(t, childPID)
	if err := client.Stop(context.Background(), "missing"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("missing stop error = %v", err)
	}
}

func collectEvents(t *testing.T, events <-chan process.CodexEvent, count int) []process.CodexEvent {
	t.Helper()
	got := make([]process.CodexEvent, 0, count)
	timeout := time.After(2 * time.Second)
	for len(got) < count {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("event channel closed after %d events", len(got))
			}
			got = append(got, event)
		case <-timeout:
			t.Fatalf("timed out waiting for event %d", len(got)+1)
		}
	}
	return got
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d is still alive", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
