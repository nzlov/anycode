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

func TestStartBuildsExecCommandAndStreamsSessionLogEvents(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	pwdFile := filepath.Join(dir, "pwd")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
pwd > "$CODEX_PWD_FILE"
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-test-codex-session-1.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-1","id":"codex-session-1","cwd":"$PWD","originator":"codex_exec"}}
{"timestamp":"2026-07-08T09:16:07.034Z","type":"response_item","payload":{"type":"function_call","call_id":"call-command","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}"}}
{"timestamp":"2026-07-08T09:16:08.034Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-command","output":"ok"}}
{"timestamp":"2026-07-08T09:16:09.034Z","type":"event_msg","payload":{"type":"patch_apply_end","call_id":"call-patch","stdout":"Success","stderr":"","success":true,"changes":{"$PWD/probe.txt":{"type":"update","unified_diff":"@@ -1 +1 @@\n-old\n+new\n","move_path":null}},"status":"completed"}}
EOF
`)
	t.Setenv("CODEX_ARGS_FILE", argsFile)
	t.Setenv("CODEX_PWD_FILE", pwdFile)
	t.Setenv("CODEX_HOME", codexHome)

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
	got := collectEvents(t, events, 4)

	if got[0].Type != "thread.started" || got[0].Payload["thread_id"] != "codex-session-1" {
		t.Fatalf("first event = %+v", got[0])
	}
	if got[1].Type != "item.started" {
		t.Fatalf("command start event = %+v", got[1])
	}
	item, ok := got[1].Payload["item"].(map[string]any)
	if !ok || item["type"] != "command_execution" || item["command"] != "go test ./..." || item["id"] != "call-command" {
		t.Fatalf("command item = %#v", got[1].Payload["item"])
	}
	if got[2].Type != "item.completed" {
		t.Fatalf("command result event = %+v", got[2])
	}
	if got[3].Type != "item.completed" {
		t.Fatalf("file change event = %+v", got[3])
	}
	fileItem, ok := got[3].Payload["item"].(map[string]any)
	if !ok || fileItem["type"] != "file_change" || fileItem["id"] != "call-patch" {
		t.Fatalf("file item = %#v", got[3].Payload["item"])
	}

	args := strings.TrimSpace(readFile(t, argsFile))
	want := `exec --skip-git-repo-check -C ` + dir + ` -m gpt-test -c model_reasoning_effort="medium" --sandbox workspace-write -i /kept/in/input.png implement adapter`
	if args != want {
		t.Fatalf("args = %q, want %q", args, want)
	}
	if gotDir := strings.TrimSpace(readFile(t, pwdFile)); gotDir != dir {
		t.Fatalf("pwd = %q, want %q", gotDir, dir)
	}
}

func TestResumeBuildsResumeCommandInWorkdir(t *testing.T) {
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
		ProcessRunID:    "process-run-2",
		SessionID:       "session-1",
		CodexSessionID:  "codex-session-1",
		Workdir:         dir,
		Prompt:          "next node",
		Model:           "gpt-test",
		ReasoningEffort: "high",
		PermissionMode:  "danger-full-access",
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.CodexSessionID != "codex-session-1" {
		t.Fatalf("CodexSessionID = %q", handle.CodexSessionID)
	}
	waitForFile(t, argsFile)
	waitForFile(t, pwdFile)

	wantArgs := `exec resume --skip-git-repo-check -m gpt-test -c model_reasoning_effort="high" codex-session-1 next node`
	if args := strings.TrimSpace(readFile(t, argsFile)); args != wantArgs {
		t.Fatalf("args = %q", args)
	}
	if gotDir := strings.TrimSpace(readFile(t, pwdFile)); gotDir != dir {
		t.Fatalf("pwd = %q, want %q", gotDir, dir)
	}
}

func TestResumeStreamsOnlyNewSessionLogEvents(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-resume.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-1","cwd":"`+dir+`"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"event_msg","payload":{"type":"agent_message","message":"old"}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := fakeCodex(t, `#!/bin/sh
sleep 0.2
cat >> "$CODEX_HOME/sessions/2026/07/08/rollout-resume.jsonl" <<EOF
{"timestamp":"2026-07-08T09:02:00Z","type":"event_msg","payload":{"type":"agent_message","message":"new"}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	handle, err := New(bin).Resume(context.Background(), process.CodexResumeInput{
		ProcessRunID:   "process-run-resume",
		SessionID:      "session-1",
		CodexSessionID: "codex-session-1",
		Workdir:        dir,
		Prompt:         "continue",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 1)
	item, ok := got[0].Payload["item"].(map[string]any)
	if !ok || item["aggregated_output"] != "new" {
		t.Fatalf("resume replayed wrong event = %+v", got[0])
	}
}

func TestStartAllowsConcurrentActiveReadersForSameWorkdir(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	bin := fakeCodex(t, `#!/bin/sh
touch "$CODEX_STARTED_FILE"
sleep 30
`)
	t.Setenv("CODEX_STARTED_FILE", started)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-first", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, started)
	second, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-second", Workdir: dir})
	if err != nil {
		t.Fatalf("second start error = %v", err)
	}
	if err := client.Stop(context.Background(), handle.ProcessRunID); err != nil {
		t.Fatal(err)
	}
	if err := client.Stop(context.Background(), second.ProcessRunID); err != nil {
		t.Fatal(err)
	}
}

func TestStartProcessOutlivesCallerContextCancellation(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
sleep 0.2
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-test-after-cancel.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-after-cancel","id":"codex-session-after-cancel","cwd":"$PWD","originator":"codex_exec"}}
{"timestamp":"2026-07-08T09:16:07.034Z","type":"event_msg","payload":{"type":"agent_message","message":"still running"}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)
	ctx, cancel := context.WithCancel(context.Background())
	handle, err := New(bin).Start(ctx, process.CodexStartInput{ProcessRunID: "process-run-context", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	cancel()

	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 2)
	if got[1].Type != "item.completed" {
		t.Fatalf("event after caller context cancel = %+v", got[1])
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

func TestStartInjectsStdioMCPServerConfig(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	tokenFile := filepath.Join(dir, "token")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
printf '%s\n' "$ANYCODE_MCP_TOKEN" > "$CODEX_TOKEN_FILE"
`)
	t.Setenv("CODEX_ARGS_FILE", argsFile)
	t.Setenv("CODEX_TOKEN_FILE", tokenFile)

	_, err := New(bin, WithMCPStdio("/app/anycode", "/data/codex/mcp.sock", "secret")).Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "process-run-mcp-stdio",
		SessionID:    "session-1",
		Workdir:      dir,
		Prompt:       "use answer_user when needed",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, argsFile)
	waitForFile(t, tokenFile)
	args := strings.TrimSpace(readFile(t, argsFile))
	for _, want := range []string{
		`-c mcp_servers.anycode.type="stdio"`,
		`-c mcp_servers.anycode.command="/app/anycode"`,
		`-c mcp_servers.anycode.args=["mcp-stdio","--session-id","session-1","--socket","/data/codex/mcp.sock"]`,
		`-c mcp_servers.anycode.env_vars=["ANYCODE_MCP_TOKEN"]`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if strings.Contains(args, "secret") {
		t.Fatalf("args leaked MCP token: %q", args)
	}
	if token := strings.TrimSpace(readFile(t, tokenFile)); token != "secret" {
		t.Fatalf("ANYCODE_MCP_TOKEN = %q", token)
	}
}

func TestStartInjectsUnixSocketPermissionProfileForStdioMCP(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
`)
	t.Setenv("CODEX_ARGS_FILE", argsFile)

	_, err := New(bin, WithMCPStdio("/app/anycode", "/tmp/anycode-1000/mcp.sock", "secret")).Start(context.Background(), process.CodexStartInput{
		ProcessRunID:   "process-run-mcp-profile",
		SessionID:      "session-1",
		Workdir:        dir,
		PermissionMode: "workspace-write",
		Prompt:         "use answer_user when needed",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, argsFile)
	args := strings.TrimSpace(readFile(t, argsFile))
	for _, want := range []string{
		`-c features.network_proxy.enabled=true`,
		`-c default_permissions="anycode-mcp"`,
		`-c permissions.anycode-mcp.extends=":workspace"`,
		`-c permissions.anycode-mcp.network.enabled=true`,
		`-c permissions.anycode-mcp.network.mode="limited"`,
		`-c permissions.anycode-mcp.network.unix_sockets={"/tmp/anycode-1000/mcp.sock"="allow"}`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if strings.Contains(args, "--sandbox workspace-write") {
		t.Fatalf("args should use default_permissions instead of --sandbox: %q", args)
	}
}

func TestEventsParsesNestedMessageTypeAndInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-test-invalid-json.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-invalid","id":"codex-session-invalid","cwd":"$PWD","originator":"codex_exec"}}
{"timestamp":"2026-07-08T09:16:07.034Z","type":"event_msg","payload":{"type":"agent_message","message":"hello"}}
not-json
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{})
	if err == nil {
		t.Fatal("expected missing process run id error")
	}

	handle, err = New(bin).Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-3", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 3)
	if got[1].Type != "item.completed" {
		t.Fatalf("agent message event = %+v", got[1])
	}
	if got[2].Type != "invalid_json" || got[2].Payload["byteCount"] != 8 {
		t.Fatalf("invalid event = %+v", got[2])
	}
	if !strings.HasPrefix(got[2].EventID, "source:rollout-test-invalid-json.jsonl:") {
		t.Fatalf("invalid event id = %q", got[2].EventID)
	}
}

func TestSessionEventsMapsReasoningResponseItem(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-reasoning.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-reasoning","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"response_item","payload":{"id":"rs_1","type":"reasoning","summary":[{"type":"summary_text","text":"Checked the session transcript"}]}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), process.CodexTranscriptInput{CodexSessionID: "codex-session-reasoning"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got), got)
	}
	item, ok := got[1].Payload["item"].(map[string]any)
	if !ok || item["type"] != "reasoning" || item["aggregated_output"] != "Checked the session transcript" {
		t.Fatalf("reasoning item = %#v", got[1].Payload["item"])
	}
}

func TestSessionEventsUsesSourceOffsetIDsForAgentMessagesWithoutNativeID(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-agent-messages.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-agent-messages","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"event_msg","payload":{"type":"agent_message","message":"first"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"event_msg","payload":{"type":"agent_message","message":"second"}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), process.CodexTranscriptInput{CodexSessionID: "codex-session-agent-messages"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("events len = %d, want 3: %#v", len(got), got)
	}
	if got[1].EventID == "" || got[2].EventID == "" || got[1].EventID == got[2].EventID {
		t.Fatalf("agent message event ids = %q %q", got[1].EventID, got[2].EventID)
	}
	if !strings.HasPrefix(got[1].EventID, "source:rollout-agent-messages.jsonl:") || !strings.HasPrefix(got[2].EventID, "source:rollout-agent-messages.jsonl:") {
		t.Fatalf("agent message event ids = %q %q", got[1].EventID, got[2].EventID)
	}
}

func TestEventsEmitsProcessExitWithFailureCode(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-test-exit.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-exit","id":"codex-session-exit","cwd":"$PWD","originator":"codex_exec"}}
{"timestamp":"2026-07-08T09:16:07.034Z","type":"event_msg","payload":{"type":"agent_message","message":"before exit"}}
EOF
echo "model gpt-test is not supported" >&2
exit 7
`)
	t.Setenv("CODEX_HOME", codexHome)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-exit", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 3)
	if got[2].Type != "process.exit" {
		t.Fatalf("exit event type = %q", got[2].Type)
	}
	if got[2].Payload["exitCode"] != 7 {
		t.Fatalf("exit payload = %#v", got[2].Payload)
	}
	if got[2].Payload["failureReason"] == "" {
		t.Fatalf("exit payload missing failureReason: %#v", got[2].Payload)
	}
	if !strings.Contains(got[2].Payload["failureReason"].(string), "model gpt-test is not supported") {
		t.Fatalf("exit payload missing stderr: %#v", got[2].Payload)
	}
}

func TestEventsChoosesEarliestAdvancedSessionLogForSameWorkdir(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-ambiguous-a.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-a","id":"codex-session-a","cwd":"$PWD","originator":"codex_exec"}}
EOF
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-ambiguous-b.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:03.939Z","type":"session_meta","payload":{"session_id":"codex-session-b","id":"codex-session-b","cwd":"$PWD","originator":"codex_exec"}}
{"timestamp":"2026-07-08T09:16:04.939Z","type":"event_msg","payload":{"type":"agent_message","message":"latest"}}
EOF
sleep 0.2
`)
	t.Setenv("CODEX_HOME", codexHome)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-ambiguous", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 1)
	if got[0].Type != "thread.started" || got[0].Payload["thread_id"] != "codex-session-a" {
		t.Fatalf("selected wrong session log = %+v", got[0])
	}
}

func TestSessionEventsReadsLongJSONLLines(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-long.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	longOutput := strings.Repeat("x", 80*1024)
	content := `{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-long","id":"codex-session-long","cwd":"/workspace/long","originator":"codex_exec","note":"` + longOutput + `"}}
{"timestamp":"2026-07-08T09:16:03.939Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-long","output":"` + longOutput + `"}}
`
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), process.CodexTranscriptInput{CodexSessionID: "codex-session-long"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events length = %d, want 2", len(got))
	}
	item, ok := got[1].Payload["item"].(map[string]any)
	if !ok || item["aggregated_output"] != longOutput {
		t.Fatalf("long output item = %#v", got[1].Payload["item"])
	}
}

func TestPatchApplyEndNormalizesAbsolutePathsAgainstSessionCWD(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "repo")
	raw := []byte(`{"timestamp":"2026-07-08T09:16:09.034Z","type":"event_msg","payload":{"type":"patch_apply_end","call_id":"call-patch","changes":{"` + filepath.ToSlash(filepath.Join(cwd, "internal", "file.go")) + `":{"type":"update","unified_diff":"@@ -1 +1 @@\n-old\n+new\n","move_path":"` + filepath.ToSlash(filepath.Join(cwd, "internal", "renamed.go")) + `"}},"status":"completed"}}`)

	events := parseSessionLogLine(raw, cwd, "rollout-test.jsonl", 42)
	if len(events) != 1 {
		t.Fatalf("events length = %d", len(events))
	}
	item, ok := events[0].Payload["item"].(map[string]any)
	if !ok {
		t.Fatalf("item = %#v", events[0].Payload["item"])
	}
	changes, ok := item["changes"].([]any)
	if !ok || len(changes) != 1 {
		t.Fatalf("changes = %#v", item["changes"])
	}
	change, ok := changes[0].(map[string]any)
	if !ok {
		t.Fatalf("change = %#v", changes[0])
	}
	if change["path"] != "internal/file.go" || change["movePath"] != "internal/renamed.go" {
		t.Fatalf("normalized change = %#v", change)
	}
}

func TestSessionLogMatchesSessionIDReadsLongMetaLine(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-long-meta.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-long-meta","id":"codex-session-long-meta","cwd":"/workspace/long","note":"` + strings.Repeat("x", 80*1024) + `"}}
`
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := sessionLogByID(codexHome, "codex-session-long-meta")
	if err != nil {
		t.Fatal(err)
	}
	if path != sessionFile {
		t.Fatalf("path = %q, want %q", path, sessionFile)
	}
}

func TestStopKillsActiveProcess(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	childPIDFile := filepath.Join(dir, "child-pid")
	bin := fakeCodex(t, `#!/bin/sh
touch "$CODEX_STARTED_FILE"
sleep 30 &
child="$!"
printf '%s\n' "$child" > "$CODEX_CHILD_PID_FILE"
trap 'kill "$child" 2>/dev/null; wait "$child" 2>/dev/null; exit 0' TERM INT
wait "$child"
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
