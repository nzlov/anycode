package codexcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

	if got[0].Type != "thread.started" || got[0].Payload["session_id"] != "codex-session-1" || got[0].Payload["originator"] != "codex_exec" {
		t.Fatalf("first event = %+v", got[0])
	}
	if got[1].Type != "item.started" {
		t.Fatalf("command start event = %+v", got[1])
	}
	item, ok := got[1].Payload["item"].(map[string]any)
	normalized := eventNormalizedItem(t, got[1])
	if !ok || normalized["type"] != "command_execution" || normalized["command"] != "go test ./..." || item["call_id"] != "call-command" {
		t.Fatalf("command item = %#v", got[1].Payload["item"])
	}
	if got[2].Type != "item.completed" {
		t.Fatalf("command result event = %+v", got[2])
	}
	resultItem, ok := got[2].Payload["item"].(map[string]any)
	resultNormalized := eventNormalizedItem(t, got[2])
	if !ok || resultNormalized["type"] != "tool_result" || resultItem["call_id"] != "call-command" {
		t.Fatalf("command result item = %#v", got[2].Payload["item"])
	}
	if got[3].Type != "item.completed" {
		t.Fatalf("file change event = %+v", got[3])
	}
	fileItem, ok := got[3].Payload["item"].(map[string]any)
	fileNormalized := eventNormalizedItem(t, got[3])
	if !ok || fileItem["type"] != "patch_apply_end" || fileNormalized["type"] != "file_change" || fileItem["call_id"] != "call-patch" || fileItem["stdout"] != "Success" || fileItem["success"] != true {
		t.Fatalf("file item = %#v", got[3].Payload["item"])
	}
	if _, ok := fileItem["changes"].(map[string]any); !ok {
		t.Fatalf("original file changes = %#v", fileItem["changes"])
	}
	if _, ok := fileNormalized["changes"].([]any); !ok {
		t.Fatalf("normalized file changes = %#v", fileNormalized["changes"])
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
{"timestamp":"2026-07-08T09:01:00Z","type":"response_item","payload":{"type":"message","id":"msg-old","role":"assistant","content":[{"type":"output_text","text":"old"}]}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := fakeCodex(t, `#!/bin/sh
sleep 0.2
cat >> "$CODEX_HOME/sessions/2026/07/08/rollout-resume.jsonl" <<EOF
{"timestamp":"2026-07-08T09:02:00Z","type":"response_item","payload":{"type":"message","id":"msg-new","role":"assistant","content":[{"type":"output_text","text":"new"}]}}
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
	_, ok := got[0].Payload["item"].(map[string]any)
	if !ok || eventNormalizedItem(t, got[0])["output"] != "new" {
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
{"timestamp":"2026-07-08T09:16:07.034Z","type":"response_item","payload":{"type":"message","id":"msg-running","role":"assistant","content":[{"type":"output_text","text":"still running"}]}}
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
{"timestamp":"2026-07-08T09:16:07.034Z","type":"response_item","payload":{"type":"message","id":"msg-hello","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}
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
	if !ok || item["type"] != "reasoning" || eventNormalizedItem(t, got[1])["type"] != "reasoning" || eventNormalizedItem(t, got[1])["output"] != "Checked the session transcript" {
		t.Fatalf("reasoning item = %#v", got[1].Payload["item"])
	}
	summary, ok := item["summary"].([]any)
	if !ok || len(summary) != 1 {
		t.Fatalf("reasoning summary = %#v", item["summary"])
	}
}

func TestSessionEventsUsesResponseItemMessagesAndIgnoresEventMessageMirrors(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-agent-messages.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-agent-messages","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"response_item","payload":{"type":"message","id":"msg-user","role":"user","content":[{"type":"input_text","text":"clone the repo"},{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"high"},{"type":"input_text","text":"use the screenshot"}]}}
{"timestamp":"2026-07-08T09:01:00.001Z","type":"event_msg","payload":{"type":"user_message","message":"clone the repo","images":[]}}
{"timestamp":"2026-07-08T09:02:00Z","type":"response_item","payload":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"working"}]}}
{"timestamp":"2026-07-08T09:02:00.001Z","type":"event_msg","payload":{"type":"agent_message","message":"working"}}
{"timestamp":"2026-07-08T09:03:00Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","last_agent_message":"working","duration_ms":1000}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), process.CodexTranscriptInput{CodexSessionID: "codex-session-agent-messages"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("events len = %d, want 4: %#v", len(got), got)
	}
	userItem, ok := got[1].Payload["item"].(map[string]any)
	if !ok || userItem["type"] != "message" || eventNormalizedItem(t, got[1])["type"] != "user_message" || eventNormalizedItem(t, got[1])["output"] != "clone the repo\nuse the screenshot" {
		t.Fatalf("user message item = %#v", got[1].Payload["item"])
	}
	content, ok := userItem["content"].([]any)
	if !ok || len(content) != 3 {
		t.Fatalf("user message content = %#v", userItem["content"])
	}
	image, ok := content[1].(map[string]any)
	if !ok || image["type"] != "input_image" || image["image_url"] != "data:image/png;base64,AAAA" || image["detail"] != "high" {
		t.Fatalf("user message image = %#v", content[1])
	}
	if !strings.Contains(got[1].EventID, "msg-user") {
		t.Fatalf("user message event id = %q, want response_item id", got[1].EventID)
	}
	assistantItem, ok := got[2].Payload["item"].(map[string]any)
	if !ok || assistantItem["type"] != "message" || eventNormalizedItem(t, got[2])["type"] != "agent_message" || eventNormalizedItem(t, got[2])["output"] != "working" {
		t.Fatalf("assistant item = %#v", got[2].Payload["item"])
	}
	if _, duplicated := got[3].Payload["lastAgentMessage"]; got[3].Type != "task.completed" || duplicated {
		t.Fatalf("task completion event = %#v", got[3])
	}
}

func TestSessionEventsPreservesStructuredCustomToolOutput(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-custom-output.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-custom-output","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-image","output":[{"type":"input_text","text":"captured screenshot"},{"type":"input_image","image_url":"data:image/png;base64,AAAA","detail":"high"}]}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), process.CodexTranscriptInput{CodexSessionID: "codex-session-custom-output"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got), got)
	}
	item, ok := got[1].Payload["item"].(map[string]any)
	if !ok || item["type"] != "custom_tool_call_output" || eventNormalizedItem(t, got[1])["type"] != "custom_tool_call" || eventNormalizedItem(t, got[1])["output"] != "captured screenshot" {
		t.Fatalf("custom tool output item = %#v", got[1].Payload["item"])
	}
	output, ok := item["output"].([]any)
	if !ok || len(output) != 2 {
		t.Fatalf("custom tool output = %#v", item["output"])
	}
	image, ok := output[1].(map[string]any)
	if !ok || image["type"] != "input_image" || image["image_url"] != "data:image/png;base64,AAAA" {
		t.Fatalf("custom tool image = %#v", output[1])
	}
}

func TestSessionEventsKeepsSourceFieldsSeparateFromNormalizedView(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-normalized-conflict.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-normalized-conflict","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"response_item","payload":{"type":"message","id":"msg-user","role":"user","content":[{"type":"input_text","text":"actual user text"}],"normalized_type":"source type","normalized_status":"source status","normalized_output":"source output","normalized_input":"source input","normalized_command":"source command","normalized_changes":"source changes","qualified_name":"source name"}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), process.CodexTranscriptInput{CodexSessionID: "codex-session-normalized-conflict"})
	if err != nil {
		t.Fatal(err)
	}
	item := got[1].Payload["item"].(map[string]any)
	for key, want := range map[string]any{
		"normalized_type":    "source type",
		"normalized_status":  "source status",
		"normalized_output":  "source output",
		"normalized_input":   "source input",
		"normalized_command": "source command",
		"normalized_changes": "source changes",
		"qualified_name":     "source name",
	} {
		if item[key] != want {
			t.Fatalf("source item %s = %#v, want %#v", key, item[key], want)
		}
	}
	normalized := got[1].Payload["normalizedItem"].(map[string]any)
	if normalized["type"] != "user_message" || normalized["status"] != "completed" || normalized["output"] != "actual user text" {
		t.Fatalf("normalized item = %#v", normalized)
	}
}

func TestSessionEventsMapsCodexJSONLRecordTypes(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-record-types.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-record-types","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","model_context_window":258400}}
{"timestamp":"2026-07-08T09:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":42}}}}
{"timestamp":"2026-07-08T09:02:10Z","type":"response_item","payload":{"type":"function_call","id":"fc-browser","call_id":"call-browser","name":"browser_resize","namespace":"mcp__playwright","arguments":"{\"width\":1440}","internal_chat_message_metadata_passthrough":{"turn_id":"turn-1"}}}
{"timestamp":"2026-07-08T09:02:20Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-browser","output":"ok"}}
{"timestamp":"2026-07-08T09:03:00Z","type":"response_item","payload":{"type":"custom_tool_call","id":"ct-patch","call_id":"call-patch","name":"apply_patch","input":"*** Begin Patch","internal_chat_message_metadata_passthrough":{"turn_id":"turn-1"}}}
{"timestamp":"2026-07-08T09:04:00Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-patch","output":"Success"}}
{"timestamp":"2026-07-08T09:05:00Z","type":"response_item","payload":{"type":"tool_search_call","id":"ts-call","call_id":"call-search","arguments":{"query":"playwright"},"internal_chat_message_metadata_passthrough":{"turn_id":"turn-1"}}}
{"timestamp":"2026-07-08T09:06:00Z","type":"response_item","payload":{"type":"tool_search_output","id":"ts-output","call_id":"call-search","tools":[{"name":"mcp__playwright"}],"internal_chat_message_metadata_passthrough":{"turn_id":"turn-1"}}}
{"timestamp":"2026-07-08T09:07:00Z","type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"call-mcp","invocation":{"server":"playwright","tool":"browser_resize"},"result":{"Ok":{"isError":false}}}}
{"timestamp":"2026-07-08T09:08:00Z","type":"event_msg","payload":{"type":"turn_aborted","turn_id":"turn-1","reason":"interrupted"}}
{"timestamp":"2026-07-08T09:09:00Z","type":"event_msg","payload":{"type":"context_compacted","summary":"short"}}
{"timestamp":"2026-07-08T09:10:00Z","type":"compacted","payload":{"message":"summary"}}
{"timestamp":"2026-07-08T09:11:00Z","type":"turn_context","payload":{"turn_id":"turn-2","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:12:00Z","type":"world_state","payload":{"full":true,"state":{"agents_md":{"text":"huge"}}}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), process.CodexTranscriptInput{CodexSessionID: "codex-session-record-types"})
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, 0, len(got))
	itemTypes := []string{}
	for _, event := range got {
		types = append(types, event.Type)
		if _, ok := event.Payload["item"].(map[string]any); ok {
			itemTypes = append(itemTypes, stringValue(eventNormalizedItem(t, event), "type"))
		}
	}
	wantTypes := []string{
		"thread.started",
		"task.started",
		"token_count",
		"item.started",
		"item.completed",
		"item.started",
		"item.completed",
		"item.started",
		"item.completed",
		"mcp_tool_call_end",
		"turn.aborted",
		"context.compacted",
		"context.compacted",
		"turn.context",
		"world.state",
	}
	if !reflect.DeepEqual(types, wantTypes) {
		t.Fatalf("types = %#v, want %#v", types, wantTypes)
	}
	wantItemTypes := []string{"tool_call", "tool_result", "custom_tool_call", "custom_tool_call", "tool_search", "tool_search"}
	if !reflect.DeepEqual(itemTypes, wantItemTypes) {
		t.Fatalf("item types = %#v, want %#v", itemTypes, wantItemTypes)
	}
	for index, want := range []struct {
		eventIndex int
		id         string
		field      string
	}{
		{eventIndex: 3, id: "fc-browser", field: "arguments"},
		{eventIndex: 5, id: "ct-patch", field: "input"},
		{eventIndex: 7, id: "ts-call", field: "arguments"},
		{eventIndex: 8, id: "ts-output", field: "tools"},
	} {
		item := got[want.eventIndex].Payload["item"].(map[string]any)
		if item["id"] != want.id || item[want.field] == nil || item["internal_chat_message_metadata_passthrough"] == nil {
			t.Fatalf("preserved item %d = %#v", index, item)
		}
	}
	functionItem := got[3].Payload["item"].(map[string]any)
	functionNormalized := eventNormalizedItem(t, got[3])
	if functionItem["type"] != "function_call" || functionItem["name"] != "browser_resize" || functionNormalized["type"] != "tool_call" || functionNormalized["qualifiedName"] != "mcp__playwright.browser_resize" {
		t.Fatalf("function item = %#v", functionItem)
	}
	if got[9].Payload["invocation"] == nil || got[9].Payload["result"] == nil {
		t.Fatalf("mcp tool end payload = %#v", got[9].Payload)
	}
	if got[11].Payload["type"] != "context_compacted" || got[11].Payload["summary"] != "short" {
		t.Fatalf("context compacted payload = %#v", got[11].Payload)
	}
	if got[12].Payload["message"] != "summary" {
		t.Fatalf("compacted payload = %#v", got[12].Payload)
	}
	world := got[len(got)-1].Payload
	if world["state"] == nil || world["full"] != true {
		t.Fatalf("world state payload = %#v", world)
	}
}

func TestParseSessionLogLinePreservesKnownPayloads(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantType  string
		wantKey   string
		wantValue any
	}{
		{
			name:      "session meta",
			raw:       `{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session","id":"meta-id","cwd":"/workspace/project","originator":"codex_exec"}}`,
			wantType:  "thread.started",
			wantKey:   "originator",
			wantValue: "codex_exec",
		},
		{
			name:      "task started",
			raw:       `{"timestamp":"2026-07-08T09:01:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1","model_context_window":258400,"extra":"kept"}}`,
			wantType:  "task.started",
			wantKey:   "extra",
			wantValue: "kept",
		},
		{
			name:      "task complete",
			raw:       `{"timestamp":"2026-07-08T09:02:00Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1","last_agent_message":"done","completed_at":1,"duration_ms":2}}`,
			wantType:  "task.completed",
			wantKey:   "last_agent_message",
			wantValue: "done",
		},
		{
			name:      "token count",
			raw:       `{"timestamp":"2026-07-08T09:03:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":42}},"rate_limits":{"plan_type":"pro"}}}`,
			wantType:  "token_count",
			wantKey:   "rate_limits",
			wantValue: map[string]any{"plan_type": "pro"},
		},
		{
			name:      "turn aborted",
			raw:       `{"timestamp":"2026-07-08T09:04:00Z","type":"event_msg","payload":{"type":"turn_aborted","turn_id":"turn-1","reason":"interrupted","duration_ms":3}}`,
			wantType:  "turn.aborted",
			wantKey:   "duration_ms",
			wantValue: float64(3),
		},
		{
			name:      "turn context",
			raw:       `{"timestamp":"2026-07-08T09:05:00Z","type":"turn_context","payload":{"turn_id":"turn-2","cwd":"/workspace/project","extra":"kept"}}`,
			wantType:  "turn.context",
			wantKey:   "extra",
			wantValue: "kept",
		},
		{
			name:      "world state",
			raw:       `{"timestamp":"2026-07-08T09:06:00Z","type":"world_state","payload":{"full":true,"state":{"agents_md":{"text":"kept"}}}}`,
			wantType:  "world.state",
			wantKey:   "state",
			wantValue: map[string]any{"agents_md": map[string]any{"text": "kept"}},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := parseSessionLogLine([]byte(test.raw), "/workspace/project", "rollout.jsonl", int64(index))
			if len(events) != 1 || events[0].Type != test.wantType {
				t.Fatalf("events = %#v", events)
			}
			if !reflect.DeepEqual(events[0].Payload[test.wantKey], test.wantValue) {
				t.Fatalf("payload = %#v, want %s=%#v", events[0].Payload, test.wantKey, test.wantValue)
			}
		})
	}
}

func TestSessionEventsPreservesUnknownJSONLRecords(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-unknown-records.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-unknown-records","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"future_record","payload":{"value":"top-level"}}
{"timestamp":"2026-07-08T09:02:00Z","type":"response_item","payload":{"type":"future_item","value":"response-item"}}
{"timestamp":"2026-07-08T09:03:00Z","type":"event_msg","payload":{"type":"future_event","value":"event-message"}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), process.CodexTranscriptInput{CodexSessionID: "codex-session-unknown-records"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("events len = %d, want 4: %#v", len(got), got)
	}
	wantTypes := []string{"thread.started", "future_record", "future_item", "future_event"}
	for index, wantType := range wantTypes {
		if got[index].Type != wantType {
			t.Fatalf("event %d type = %q, want %q", index, got[index].Type, wantType)
		}
	}
	for index, wantValue := range []string{"top-level", "response-item", "event-message"} {
		if got[index+1].Payload["value"] != wantValue {
			t.Fatalf("event %d payload = %#v, want value %q", index+1, got[index+1].Payload, wantValue)
		}
	}
}

func TestEventsEmitsProcessExitWithFailureCode(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-test-exit.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-exit","id":"codex-session-exit","cwd":"$PWD","originator":"codex_exec"}}
{"timestamp":"2026-07-08T09:16:07.034Z","type":"response_item","payload":{"type":"message","id":"msg-before-exit","role":"assistant","content":[{"type":"output_text","text":"before exit"}]}}
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
{"timestamp":"2026-07-08T09:16:04.939Z","type":"response_item","payload":{"type":"message","id":"msg-latest","role":"assistant","content":[{"type":"output_text","text":"latest"}]}}
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
	if got[0].Type != "thread.started" || got[0].Payload["session_id"] != "codex-session-a" {
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
	_, ok := got[1].Payload["item"].(map[string]any)
	if !ok || eventNormalizedItem(t, got[1])["output"] != longOutput {
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
	if item["type"] != "patch_apply_end" || item["call_id"] != "call-patch" || item["status"] != "completed" {
		t.Fatalf("original item = %#v", item)
	}
	if _, ok := item["changes"].(map[string]any); !ok {
		t.Fatalf("original changes = %#v", item["changes"])
	}
	changes, ok := eventNormalizedItem(t, events[0])["changes"].([]any)
	if !ok || len(changes) != 1 {
		t.Fatalf("changes = %#v", eventNormalizedItem(t, events[0])["changes"])
	}
	change, ok := changes[0].(map[string]any)
	if !ok {
		t.Fatalf("change = %#v", changes[0])
	}
	if change["path"] != "internal/file.go" || change["movePath"] != "internal/renamed.go" {
		t.Fatalf("normalized change = %#v", change)
	}
}

func TestPatchApplyEndKeepsAbsolutePathsOutsideSessionCWD(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "repo")
	outside := filepath.Join(t.TempDir(), "other", "file.go")
	raw := []byte(`{"timestamp":"2026-07-08T09:16:09.034Z","type":"event_msg","payload":{"type":"patch_apply_end","call_id":"call-patch","changes":{"` + filepath.ToSlash(outside) + `":{"type":"update"}},"status":"completed"}}`)

	events := parseSessionLogLine(raw, cwd, "rollout-test.jsonl", 42)
	changes := eventNormalizedItem(t, events[0])["changes"].([]any)
	change := changes[0].(map[string]any)
	if change["path"] != filepath.ToSlash(outside) {
		t.Fatalf("outside path = %q, want %q", change["path"], filepath.ToSlash(outside))
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

func eventNormalizedItem(t *testing.T, event process.CodexEvent) map[string]any {
	t.Helper()
	item, ok := event.Payload["normalizedItem"].(map[string]any)
	if !ok {
		t.Fatalf("normalized item = %#v", event.Payload["normalizedItem"])
	}
	return item
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
