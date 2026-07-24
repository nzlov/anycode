package codexcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

type testToolHandler struct {
	calls chan process.DynamicToolCall
}

func (h *testToolHandler) HandleDynamicTool(_ context.Context, call process.DynamicToolCall) (process.DynamicToolResult, error) {
	h.calls <- call
	return process.DynamicToolResult{Success: true, Content: []process.DynamicToolContent{{Type: "inputText", Text: `{"answer":"yes"}`}}}, nil
}

func TestThreadParamsExposeWritableArtifactDirectory(t *testing.T) {
	params := appServerThreadParams("/workspace", "/outputs/session-1", "AnyCode rules", "gpt-test", "workspace-write", true)
	if _, exists := params["approvalPolicy"]; exists {
		t.Fatalf("thread params override Codex approval policy: %#v", params)
	}
	config, ok := params["config"].(map[string]any)
	if !ok {
		t.Fatalf("config = %#v", params["config"])
	}
	environment := config["shell_environment_policy"].(map[string]any)["set"].(map[string]string)
	if environment["ANYCODE_ARTIFACT_DIR"] != "/outputs/session-1" {
		t.Fatalf("shell environment = %#v", environment)
	}
	sandbox := config["sandbox_workspace_write"].(map[string]any)
	if roots := sandbox["writable_roots"].([]string); len(roots) != 1 || roots[0] != "/outputs/session-1" {
		t.Fatalf("writable roots = %#v", roots)
	}
	if params["developerInstructions"] != "AnyCode rules" || params["serviceTier"] != "priority" {
		t.Fatalf("thread params = %#v", params)
	}

	readOnly := appServerThreadParams("/workspace", "/outputs/session-1", "", "", "read-only", false)
	if readOnly["serviceTier"] != "default" {
		t.Fatalf("read-only thread params = %#v", readOnly)
	}
	readOnlyConfig := readOnly["config"].(map[string]any)
	if _, exists := readOnlyConfig["sandbox_workspace_write"]; exists {
		t.Fatalf("read-only config has writable roots: %#v", readOnlyConfig)
	}
}

func TestSandboxPolicyFromPermissionMode(t *testing.T) {
	tests := []struct {
		name           string
		permissionMode string
		artifactDir    string
		wantType       string
		wantRoot       string
	}{
		{name: "read only", permissionMode: "read-only", wantType: "readOnly"},
		{name: "workspace write", permissionMode: " workspace-write ", artifactDir: " /outputs/session-1 ", wantType: "workspaceWrite", wantRoot: "/outputs/session-1"},
		{name: "full access", permissionMode: "danger-full-access", wantType: "dangerFullAccess"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := appServerSandboxPolicy(test.permissionMode, test.artifactDir)
			if policy["type"] != test.wantType {
				t.Fatalf("sandbox policy = %#v", policy)
			}
			roots, _ := policy["writableRoots"].([]string)
			if test.wantRoot == "" && len(roots) != 0 {
				t.Fatalf("writable roots = %#v", roots)
			}
			if test.wantRoot != "" && (len(roots) != 1 || roots[0] != test.wantRoot) {
				t.Fatalf("writable roots = %#v", roots)
			}
		})
	}
	if policy := appServerSandboxPolicy("", "/outputs/session-1"); policy != nil {
		t.Fatalf("empty permission sandbox policy = %#v", policy)
	}
}

func TestRuntimeCompletesDynamicToolOnOriginalTurn(t *testing.T) {
	codexHome := t.TempDir()
	responses := filepath.Join(t.TempDir(), "responses")
	startRequest := filepath.Join(t.TempDir(), "start-request")
	t.Setenv("APP_SERVER_RESPONSES", responses)
	t.Setenv("APP_SERVER_START_REQUEST", startRequest)
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux","userAgent":"codex-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' "$request" > "$APP_SERVER_START_REQUEST"
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
mkdir -p "$CODEX_HOME/sessions/2026/07/22"
printf '%s\n' '{"timestamp":"2026-07-22T00:00:00Z","type":"session_meta","payload":{"id":"thread-1","cwd":"/workspace"}}' > "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
IFS= read -r request
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}'
printf '%s\n' '{"id":90,"method":"item/tool/call","params":{"threadId":"thread-1","turnId":"turn-1","callId":"call-1","tool":"questions","arguments":{"questions":[{"body":"Continue?"}]}}}'
IFS= read -r response
printf '%s\n' "$response" > "$APP_SERVER_RESPONSES"
printf '%s\n' '{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-1","name":"questions","input":"{\"questions\":[{\"body\":\"Continue?\"}]}"}}' >> "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
printf '%s\n' '{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-1","output":[{"type":"input_text","text":"{\"answer\":\"yes\"}"}]}}' >> "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
printf '%s\n' '{"timestamp":"2026-07-22T00:00:03Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1"}}' >> "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"ignored","type":"agentMessage","text":"not from transcript"}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[],"completedAt":1784678400}}}'
cat >/dev/null
`)
	client := New(bin, WithCodexHome(codexHome))
	t.Cleanup(func() { _ = client.Close() })
	handler := &testToolHandler{calls: make(chan process.DynamicToolCall, 1)}
	client.SetDynamicToolHandler(handler)

	handle, err := client.Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "run-1", SessionID: "session-1", Workdir: t.TempDir(),
		Input: []process.CodexInputItem{{Type: "text", Text: "ask"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.CodexSessionID != "thread-1" || handle.TurnID != "turn-1" {
		t.Fatalf("handle = %+v", handle)
	}
	content, err := os.ReadFile(startRequest)
	if err != nil {
		t.Fatal(err)
	}
	var startEnvelope struct {
		Method string `json:"method"`
		Params struct {
			HistoryMode string `json:"historyMode"`
			ServiceTier string `json:"serviceTier"`
		} `json:"params"`
	}
	if json.Unmarshal(content, &startEnvelope) != nil || startEnvelope.Method != "thread/start" || startEnvelope.Params.HistoryMode != "paginated" || startEnvelope.Params.ServiceTier != "default" {
		t.Fatalf("start request = %s", content)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	var got []process.CodexEvent
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 4 || got[0].Type != process.CodexEventTool || got[1].Type != process.CodexEventTool || got[2].Type != process.CodexEventStatus || got[3].Type != process.CodexEventProcessExit {
		t.Fatalf("events = %#v", got)
	}
	if got[0].Phase != process.CodexPhaseStarted || got[1].Phase != process.CodexPhaseCompleted {
		t.Fatalf("tool lifecycle = %#v", got[:2])
	}
	call := <-handler.calls
	if call.ProcessRunID != "run-1" || call.SessionID != "session-1" || call.Tool != "questions" {
		t.Fatalf("tool call = %+v", call)
	}
	deadline := time.Now().Add(time.Second)
	for {
		content, readErr := os.ReadFile(responses)
		if readErr == nil {
			var envelope struct {
				ID     int `json:"id"`
				Result struct {
					Success bool `json:"success"`
				} `json:"result"`
			}
			if json.Unmarshal(content, &envelope) != nil || envelope.ID != 90 || !envelope.Result.Success {
				t.Fatalf("dynamic response = %s", content)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStopFindsRunAfterEventsAreClaimed(t *testing.T) {
	interruptRequest := filepath.Join(t.TempDir(), "interrupt-request")
	t.Setenv("APP_SERVER_INTERRUPT_REQUEST", interruptRequest)
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"userAgent":"codex-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
IFS= read -r request
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}'
IFS= read -r request
printf '%s\n' "$request" > "$APP_SERVER_INTERRUPT_REQUEST"
printf '%s\n' '{"id":4,"result":{}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"interrupted","items":[]}}}'
cat >/dev/null
`)
	client := New(bin)
	t.Cleanup(func() { _ = client.Close() })
	handle, err := client.Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "run-1", SessionID: "session-1", Workdir: t.TempDir(),
		Input: []process.CodexInputItem{{Type: "text", Text: "wait"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Events(context.Background(), handle); err != nil {
		t.Fatal(err)
	}
	if err := client.Stop(context.Background(), handle.ProcessRunID); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(interruptRequest)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Method string `json:"method"`
		Params struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
		} `json:"params"`
	}
	if json.Unmarshal(content, &request) != nil || request.Method != "turn/interrupt" || request.Params.ThreadID != "thread-1" || request.Params.TurnID != "turn-1" {
		t.Fatalf("interrupt request = %s", content)
	}
}

func TestSteerUsesActiveTurnAndContinuesEventStream(t *testing.T) {
	codexHome := t.TempDir()
	steerRequest := filepath.Join(t.TempDir(), "steer-request")
	t.Setenv("APP_SERVER_STEER_REQUEST", steerRequest)
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"userAgent":"codex-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
mkdir -p "$CODEX_HOME/sessions/2026/07/22"
printf '%s\n' '{"timestamp":"2026-07-22T00:00:00Z","type":"session_meta","payload":{"id":"thread-1"}}' > "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
IFS= read -r request
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}'
IFS= read -r request
printf '%s\n' "$request" > "$APP_SERVER_STEER_REQUEST"
printf '%s\n' '{"id":4,"result":{"turnId":"turn-1"}}'
printf '%s\n' '{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"message","id":"user-2","role":"user","content":[{"type":"input_text","text":"follow up"}]}}' >> "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
printf '%s\n' '{"timestamp":"2026-07-22T00:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1"}}' >> "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"user-2","type":"userMessage","content":[{"type":"text","text":"follow up"}]}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}}'
cat >/dev/null
`)
	client := New(bin, WithCodexHome(codexHome))
	t.Cleanup(func() { _ = client.Close() })
	handle, err := client.Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "run-1", SessionID: "session-1", Workdir: t.TempDir(),
		Input: []process.CodexInputItem{{Type: "text", Text: "start"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Steer(context.Background(), process.CodexSteerInput{
		ProcessRunID: "run-1",
		Input:        []process.CodexInputItem{{Type: "text", Text: "follow up"}},
	}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(steerRequest)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Method string `json:"method"`
		Params struct {
			ThreadID       string `json:"threadId"`
			ExpectedTurnID string `json:"expectedTurnId"`
			Input          []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"input"`
		} `json:"params"`
	}
	if json.Unmarshal(content, &request) != nil || request.Method != "turn/steer" || request.Params.ThreadID != "thread-1" || request.Params.ExpectedTurnID != "turn-1" || len(request.Params.Input) != 1 || request.Params.Input[0].Text != "follow up" {
		t.Fatalf("steer request = %s", content)
	}
	var got []process.CodexEvent
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 3 || got[0].Type != process.CodexEventMessage || got[1].Type != process.CodexEventStatus || got[2].Type != process.CodexEventProcessExit {
		t.Fatalf("steer events = %#v", got)
	}
}

func TestResumeRegistersDynamicTools(t *testing.T) {
	codexHome := t.TempDir()
	writeSessionLog(t, codexHome, "thread-1", `{"timestamp":"2026-07-22T00:00:00.500Z","type":"response_item","payload":{"type":"message","id":"old","role":"assistant","content":[{"type":"output_text","text":"old"}]}}`)
	resumeRequest := filepath.Join(t.TempDir(), "resume-request")
	t.Setenv("APP_SERVER_RESUME_REQUEST", resumeRequest)
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"userAgent":"codex-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' "$request" > "$APP_SERVER_RESUME_REQUEST"
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
IFS= read -r request
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}'
printf '%s\n' '{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"message","id":"new","role":"assistant","content":[{"type":"output_text","text":"new"}]}}' >> "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
printf '%s\n' '{"timestamp":"2026-07-22T00:00:02Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1"}}' >> "$CODEX_HOME/sessions/2026/07/22/rollout-thread-1.jsonl"
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}}'
cat >/dev/null
`)
	client := New(bin, WithCodexHome(codexHome))
	t.Cleanup(func() { _ = client.Close() })
	handle, err := client.Resume(context.Background(), process.CodexResumeInput{
		ProcessRunID: "run-1", SessionID: "session-1", CodexSessionID: "thread-1", Workdir: t.TempDir(),
		Input: []process.CodexInputItem{{Type: "text", Text: "continue"}}, DeveloperInstructions: "AnyCode rules",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	var resumed []process.CodexEvent
	for event := range events {
		resumed = append(resumed, event)
	}
	if len(resumed) != 3 {
		t.Fatalf("resume events = %#v", resumed)
	}
	message, ok := resumed[0].Content.(process.CodexMessageContent)
	if !ok || message.Text != "new" {
		t.Fatalf("resume message = %#v", resumed[0])
	}
	content, err := os.ReadFile(resumeRequest)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Method string `json:"method"`
		Params struct {
			DeveloperInstructions string `json:"developerInstructions"`
			ServiceTier           string `json:"serviceTier"`
			DynamicTools          []struct {
				Name string `json:"name"`
			} `json:"dynamicTools"`
		} `json:"params"`
	}
	if json.Unmarshal(content, &request) != nil || request.Method != "thread/resume" || request.Params.DeveloperInstructions != "AnyCode rules" || request.Params.ServiceTier != "default" || len(request.Params.DynamicTools) != 5 || request.Params.DynamicTools[0].Name != "questions" || request.Params.DynamicTools[1].Name != "publish_artifact" || request.Params.DynamicTools[2].Name != "tunnel_create" || request.Params.DynamicTools[3].Name != "tunnel_list" || request.Params.DynamicTools[4].Name != "tunnel_close" {
		t.Fatalf("resume request = %s", content)
	}
}

func TestPlanTurnKeepsAnyCodeDeveloperInstructions(t *testing.T) {
	turnRequest := filepath.Join(t.TempDir(), "turn-request")
	t.Setenv("APP_SERVER_TURN_REQUEST", turnRequest)
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"userAgent":"codex-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
IFS= read -r request
printf '%s\n' "$request" > "$APP_SERVER_TURN_REQUEST"
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}}'
cat >/dev/null
`)
	client := New(bin)
	t.Cleanup(func() { _ = client.Close() })
	handle, err := client.Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "run-1", SessionID: "session-1", Workdir: t.TempDir(),
		Input:  []process.CodexInputItem{{Type: "text", Text: "make a plan"}},
		Action: process.CodexActionPlan, DeveloperInstructions: "AnyCode rules",
		Model: "gpt-test", ReasoningEffort: "high", PermissionMode: "danger-full-access",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	content, err := os.ReadFile(turnRequest)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Method string `json:"method"`
		Params struct {
			SandboxPolicy struct {
				Type string `json:"type"`
			} `json:"sandboxPolicy"`
			CollaborationMode struct {
				Mode     string `json:"mode"`
				Settings struct {
					Model                 string `json:"model"`
					ReasoningEffort       string `json:"reasoning_effort"`
					DeveloperInstructions string `json:"developer_instructions"`
				} `json:"settings"`
			} `json:"collaborationMode"`
		} `json:"params"`
	}
	if json.Unmarshal(content, &request) != nil || request.Method != "turn/start" || request.Params.SandboxPolicy.Type != "dangerFullAccess" || request.Params.CollaborationMode.Mode != "plan" || request.Params.CollaborationMode.Settings.Model != "gpt-test" || request.Params.CollaborationMode.Settings.ReasoningEffort != "high" || request.Params.CollaborationMode.Settings.DeveloperInstructions != "AnyCode rules" {
		t.Fatalf("plan turn request = %s", content)
	}
}

func TestCompactionNotificationDoesNotCompleteRun(t *testing.T) {
	runtime := &appServerRuntime{routes: map[process.RunID]*appServerRun{}, threads: map[string]*appServerRun{}}
	ctx, cancel := context.WithCancel(context.Background())
	route := &appServerRun{
		handle: process.CodexHandle{ProcessRunID: "run-1", CodexSessionID: "thread-1"}, sessionID: "session-1",
		ctx: ctx, cancel: cancel, events: make(chan process.CodexEvent, 1), closed: make(chan struct{}), finished: make(chan process.ExitResult, 1),
	}
	runtime.register(route)
	runtime.handleNotification("thread/compacted", json.RawMessage(`{"threadId":"thread-1"}`))
	if route.isClosed() {
		t.Fatal("compaction completed the active run")
	}
	select {
	case event := <-route.events:
		t.Fatalf("app-server notification leaked into transcript stream: %#v", event)
	default:
	}
	runtime.removeRoute(route)
}

func TestHistoryPageUsesSessionFile(t *testing.T) {
	codexHome := t.TempDir()
	writeSessionLog(t, codexHome, "thread-1", `
{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"message","id":"user-2","role":"user","content":[{"type":"input_text","text":"second"}]}}
{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"command-2","name":"exec","input":"const result = await tools.exec_command({\"cmd\":\"go test ./...\",\"workdir\":\"/workspace\"}); text(result);"}}
{"timestamp":"2026-07-22T00:00:03Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"command-2","output":[{"type":"input_text","text":"Script completed\\nWall time 0.025 seconds\\nOutput:\\n"},{"type":"input_text","text":"{\"chunk_id\":\"one\",\"wall_time_seconds\":0.025,\"exit_code\":0,\"output\":\"ok\"}"}]}}
{"timestamp":"2026-07-22T00:00:04Z","type":"event_msg","payload":{"type":"patch_apply_end","call_id":"file-2","status":"completed","changes":{"/workspace/main.go":{"type":"update","unified_diff":"@@ -1 +1 @@"}}}}
{"timestamp":"2026-07-22T00:00:05Z","type":"response_item","payload":{"type":"message","id":"agent-2","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`)
	client := New("codex", WithCodexHome(codexHome))
	page, err := client.HistoryPage(context.Background(), process.CodexHistoryPageInput{ThreadID: "thread-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "" || len(page.Events) != 5 {
		t.Fatalf("page = %+v", page)
	}
	command, ok := page.Events[1].Content.(process.CodexCommandContent)
	if !ok || len(command.Commands) != 1 || command.Commands[0].Command != "go test ./..." || command.Commands[0].Workdir != "/workspace" {
		t.Fatalf("command event = %#v", page.Events[1])
	}
	completedCommand, ok := page.Events[2].Content.(process.CodexCommandContent)
	if !ok || len(completedCommand.Commands) != 1 || completedCommand.Commands[0].Output != "ok" || completedCommand.Commands[0].ExitCode == nil || *completedCommand.Commands[0].ExitCode != 0 {
		t.Fatalf("completed command event = %#v", page.Events[2])
	}
	fileChange, ok := page.Events[3].Content.(process.CodexFileChangeContent)
	if !ok || len(fileChange.Changes) != 1 || fileChange.Changes[0].Path != "main.go" || fileChange.Changes[0].UnifiedDiff != "@@ -1 +1 @@" {
		t.Fatalf("file change event = %#v", page.Events[3])
	}
	message, ok := page.Events[4].Content.(process.CodexMessageContent)
	if !ok || message.Role != "assistant" || message.Text != "done" {
		t.Fatalf("agent event = %#v", page.Events[4])
	}
}

func TestHistoryPageResolvesWaitsForwardOnlyForPageCommands(t *testing.T) {
	codexHome := t.TempDir()
	execStart := `{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"exec-1","name":"exec","input":"const r = await tools.exec_command({\"cmd\":\"go test ./...\",\"workdir\":\"/workspace\",\"yield_time_ms\":1000}); text(r);"}}`
	execRunning := `{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"exec-1","output":[{"type":"input_text","text":"Script running with cell ID 37\nWall time 1.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"chunk_id\":\"one\",\"session_id\":37,\"wall_time_seconds\":1,\"output\":\"first\"}"}]}}`
	var body strings.Builder
	body.WriteString(execStart)
	body.WriteByte('\n')
	body.WriteString(execRunning)
	body.WriteByte('\n')
	for index := 0; index < 140; index++ {
		body.WriteString(`{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"reasoning","id":"filler-`)
		body.WriteString(strconv.Itoa(index))
		body.WriteString(`","summary":[{"type":"summary_text","text":"filler"}]}}`)
		body.WriteByte('\n')
	}
	body.WriteString(`{"timestamp":"2026-07-22T00:00:03Z","type":"response_item","payload":{"type":"function_call","name":"wait","arguments":"{\"cell_id\":\"37\"}","call_id":"wait-1"}}`)
	body.WriteByte('\n')
	body.WriteString(`{"timestamp":"2026-07-22T00:00:04Z","type":"response_item","payload":{"type":"function_call_output","call_id":"wait-1","output":[{"type":"input_text","text":"Script completed\nWall time 0.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"chunk_id\":\"two\",\"wall_time_seconds\":0,\"exit_code\":0,\"output\":\"second\"}"}]}}`)
	writeSessionLog(t, codexHome, "thread-wait", body.String())

	client := New("codex", WithCodexHome(codexHome))
	latest, err := client.HistoryPage(context.Background(), process.CodexHistoryPageInput{ThreadID: "thread-wait", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(latest.Events) != 0 {
		t.Fatalf("wait-only page events = %#v, want none", latest.Events)
	}

	path, err := sessionLogByID(codexHome, "thread-wait")
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fillerOffset := strings.Index(string(content), `{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"reasoning","id":"filler-0"`)
	if fillerOffset < 0 {
		t.Fatal("filler offset not found")
	}
	page, err := client.HistoryPage(context.Background(), process.CodexHistoryPageInput{
		ThreadID: "thread-wait", Cursor: strconv.Itoa(fillerOffset), Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].Phase != process.CodexPhaseStarted || page.Events[1].Phase != process.CodexPhaseCompleted {
		t.Fatalf("exec page events = %#v", page.Events)
	}
	terminal, ok := page.Events[1].Content.(process.CodexCommandContent)
	if !ok || len(terminal.Commands) != 1 || terminal.Commands[0].Command != "" || terminal.Commands[0].Output != "firstsecond" || terminal.DurationMS == nil || *terminal.DurationMS != 3000 {
		t.Fatalf("terminal history event = %#v", page.Events[1])
	}

	writeSessionLog(t, codexHome, "thread-near-wait", strings.Join([]string{
		execStart,
		execRunning,
		`{"timestamp":"2026-07-22T00:00:03Z","type":"response_item","payload":{"type":"function_call","name":"wait","arguments":"{\"cell_id\":\"37\"}","call_id":"wait-1"}}`,
		`{"timestamp":"2026-07-22T00:00:04Z","type":"response_item","payload":{"type":"function_call_output","call_id":"wait-1","output":[{"type":"input_text","text":"Script completed\nWall time 0.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"chunk_id\":\"two\",\"wall_time_seconds\":0,\"exit_code\":0,\"output\":\"second\"}"}]}}`,
	}, "\n"))
	nearWait, err := client.HistoryPage(context.Background(), process.CodexHistoryPageInput{ThreadID: "thread-near-wait", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(nearWait.Events) != 0 {
		t.Fatalf("wait-only page used preceding exec context: %#v", nearWait.Events)
	}
}

func TestSessionFileEmitsPlanAndCommandFromSameExecRecord(t *testing.T) {
	codexHome := t.TempDir()
	writeSessionLog(t, codexHome, "thread-plan", `
{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-mixed","name":"exec","input":"const p = await tools.update_plan({plan:[{step:\"Inspect\",status:\"completed\"},{step:\"Verify\",status:\"in_progress\"}]}); const r = await tools.exec_command({cmd:\"go test ./...\"}); text(r);"}}
{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-mixed","output":[{"type":"input_text","text":"Script completed\\nWall time 0.1 seconds\\nOutput:\\n"},{"type":"input_text","text":"{\"chunk_id\":\"one\",\"wall_time_seconds\":0.1,\"exit_code\":0,\"output\":\"ok\"}"}]}}`)
	page, err := New("codex", WithCodexHome(codexHome)).HistoryPage(context.Background(), process.CodexHistoryPageInput{ThreadID: "thread-plan"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 3 || page.Events[0].Type != process.CodexEventPlan || page.Events[1].Type != process.CodexEventCommand || page.Events[2].Type != process.CodexEventCommand {
		t.Fatalf("mixed exec events = %#v", page.Events)
	}
	plan, ok := page.Events[0].Content.(process.PlanUpdate)
	if !ok || len(plan.Items) != 2 || plan.Items[1].Status != process.PlanItemInProgress {
		t.Fatalf("plan event = %#v", page.Events[0])
	}
}

func TestSessionFileHidesUpdatePlanCustomToolCompletion(t *testing.T) {
	codexHome := t.TempDir()
	writeSessionLog(t, codexHome, "thread-plan-only", `
{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-plan-only","name":"exec","input":"const r = await tools.update_plan({plan:[{step:\"Inspect\",status:\"completed\"},{step:\"Verify\",status:\"in_progress\"}]}); text(r);"}}
{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-plan-only","output":[{"type":"input_text","text":"Script completed\nWall time 0.1 seconds\nOutput:\n"},{"type":"input_text","text":"{}"}]}}`)
	page, err := New("codex", WithCodexHome(codexHome)).HistoryPage(context.Background(), process.CodexHistoryPageInput{ThreadID: "thread-plan-only"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Type != process.CodexEventPlan {
		t.Fatalf("plan-only events = %#v", page.Events)
	}
	plan, ok := page.Events[0].Content.(process.PlanUpdate)
	if !ok || len(plan.Items) != 2 || plan.Items[1].Status != process.PlanItemInProgress {
		t.Fatalf("plan event = %#v", page.Events[0])
	}
}

func TestQuestionsDynamicToolDescriptionMatchesOptionalOptions(t *testing.T) {
	tools := anyCodeDynamicTools()
	if len(tools) == 0 {
		t.Fatal("questions dynamic tool is missing")
	}
	description, _ := tools[0]["description"].(string)
	if description != "Ask the user one or more questions and wait for their answers. Each question requires a body; options are optional." {
		t.Fatalf("questions description = %q", description)
	}
	inputSchema := tools[0]["inputSchema"].(map[string]any)
	questionsSchema := inputSchema["properties"].(map[string]any)["questions"].(map[string]any)
	questionSchema := questionsSchema["items"].(map[string]any)
	required := questionSchema["required"].([]string)
	properties := questionSchema["properties"].(map[string]any)
	if len(required) != 1 || required[0] != "body" {
		t.Fatalf("question required fields = %#v", required)
	}
	if _, exists := properties["title"]; exists {
		t.Fatalf("question schema still exposes title: %#v", properties)
	}
}

func TestProbeUsesAppServerCatalog(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux","userAgent":"codex 9.0"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' '{"id":2,"result":{"data":[{"id":"gpt-test","model":"gpt-test","displayName":"GPT Test","description":"test","defaultReasoningEffort":"low","supportedReasoningEfforts":[{"reasoningEffort":"low","description":"Fast"}],"hidden":false,"isDefault":true}]}}'
IFS= read -r request
printf '%s\n' '{"id":3,"result":{"data":[{"name":"image_generation","stage":"stable","defaultEnabled":true,"enabled":true}]}}'
cat >/dev/null
`)
	client := New(bin)
	t.Cleanup(func() { _ = client.Close() })
	capabilities, err := client.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Version != "codex 9.0" || !capabilities.SupportsAppServer || !capabilities.SupportsImageGeneration || len(capabilities.Models) != 1 {
		t.Fatalf("capabilities = %+v", capabilities)
	}
}

func TestSlashCommandsMatchesAppServerActions(t *testing.T) {
	commands := New("codex").SlashCommands()
	if len(commands) != 4 || commands[0].Name != "/review" || commands[3].Name != "/plan" {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestAppServerInputUsesStructuredMentions(t *testing.T) {
	items, err := appServerInput([]process.CodexInputItem{
		{Type: "text", Text: "inspect the selected file"},
		{Type: "mention", Name: "src/main.go", Path: "/workspace/src/main.go"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1]["type"] != "mention" || items[1]["name"] != "src/main.go" || items[1]["path"] != "/workspace/src/main.go" {
		t.Fatalf("input = %#v", items)
	}
}

func TestAppServerInputResolvesRelativeMentionInsideWorkdir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "src", "main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	items, err := appServerInput([]process.CodexInputItem{{Type: "mention", Name: "src/main.go", Path: "src/main.go"}}, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0]["path"] != path {
		t.Fatalf("input = %#v", items)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := appServerInput([]process.CodexInputItem{{Type: "mention", Name: "link.txt", Path: "link.txt"}}, root); err == nil {
		t.Fatal("symlink escaping workdir was accepted")
	}
}

func fakeCodex(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake is not portable on windows")
	}
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeSessionLog(t *testing.T, codexHome string, threadID string, body string) {
	t.Helper()
	path := filepath.Join(codexHome, "sessions", "2026", "07", "22", "rollout-"+threadID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"timestamp":"2026-07-22T00:00:00Z","type":"session_meta","payload":{"id":"` + threadID + `","cwd":"/workspace"}}`
	if err := os.WriteFile(path, []byte(meta+"\n"+strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNewUsesCODEXBIN(t *testing.T) {
	t.Setenv("CODEX_BIN", "/custom/codex")
	if got := New("").Bin(); got != "/custom/codex" {
		t.Fatalf("Bin() = %q", got)
	}
}

var _ = strings.TrimSpace
