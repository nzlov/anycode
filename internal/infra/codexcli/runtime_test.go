package codexcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	responses := filepath.Join(t.TempDir(), "responses")
	t.Setenv("APP_SERVER_RESPONSES", responses)
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux","userAgent":"codex-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
IFS= read -r request
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}'
printf '%s\n' '{"id":90,"method":"item/tool/call","params":{"threadId":"thread-1","turnId":"turn-1","callId":"call-1","tool":"questions","arguments":{"questions":[{"title":"Continue?"}]}}}'
IFS= read -r response
printf '%s\n' "$response" > "$APP_SERVER_RESPONSES"
printf '%s\n' '{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","completedAtMs":1784678400000,"item":{"id":"call-1","type":"dynamicToolCall","tool":"questions","arguments":{"questions":[{"title":"Continue?"}]},"status":"completed","success":true,"contentItems":[{"type":"inputText","text":"{\"answer\":\"yes\"}"}]}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[],"completedAt":1784678400}}}'
cat >/dev/null
`)
	client := New(bin)
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
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	var got []process.CodexEvent
	for event := range events {
		got = append(got, event)
	}
	if len(got) != 3 || got[0].Type != process.CodexEventTool || got[1].Type != process.CodexEventStatus || got[2].Type != process.CodexEventProcessExit {
		t.Fatalf("events = %#v", got)
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
	steerRequest := filepath.Join(t.TempDir(), "steer-request")
	t.Setenv("APP_SERVER_STEER_REQUEST", steerRequest)
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"userAgent":"codex-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' '{"id":2,"result":{"thread":{"id":"thread-1"}}}'
IFS= read -r request
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1","status":"inProgress","items":[]}}}'
IFS= read -r request
printf '%s\n' "$request" > "$APP_SERVER_STEER_REQUEST"
printf '%s\n' '{"id":4,"result":{"turnId":"turn-1"}}'
printf '%s\n' '{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"user-2","type":"userMessage","content":[{"type":"text","text":"follow up"}]}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}}'
cat >/dev/null
`)
	client := New(bin)
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
	if len(got) < 3 || got[0].Type != process.CodexEventMessage || got[len(got)-1].Type != process.CodexEventProcessExit {
		t.Fatalf("steer events = %#v", got)
	}
}

func TestResumeRegistersDynamicTools(t *testing.T) {
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
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"completed","items":[]}}}'
cat >/dev/null
`)
	client := New(bin)
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
	for range events {
	}
	content, err := os.ReadFile(resumeRequest)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Method string `json:"method"`
		Params struct {
			DeveloperInstructions string `json:"developerInstructions"`
			DynamicTools          []struct {
				Name string `json:"name"`
			} `json:"dynamicTools"`
		} `json:"params"`
	}
	if json.Unmarshal(content, &request) != nil || request.Method != "thread/resume" || request.Params.DeveloperInstructions != "AnyCode rules" || len(request.Params.DynamicTools) != 2 || request.Params.DynamicTools[0].Name != "questions" || request.Params.DynamicTools[1].Name != "publish_artifact" {
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
		ctx: ctx, cancel: cancel, events: make(chan process.CodexEvent, 1), closed: make(chan struct{}),
	}
	runtime.register(route)
	runtime.handleNotification("thread/compacted", json.RawMessage(`{"threadId":"thread-1"}`))
	if route.isClosed() {
		t.Fatal("compaction completed the active run")
	}
	select {
	case event := <-route.events:
		if event.Type != process.CodexEventStatus {
			t.Fatalf("compaction event = %#v", event)
		}
	default:
		t.Fatal("compaction status event was not emitted")
	}
	runtime.removeRoute(route)
}

func TestHistoryPageUsesAppServerTurnCursor(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux","userAgent":"codex-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' '{"id":2,"result":{"data":[{"id":"turn-2","status":"completed","startedAt":1784678400,"items":[{"id":"user-2","type":"userMessage","content":[{"type":"text","text":"second"}]},{"id":"agent-2","type":"agentMessage","text":"done"}]}],"nextCursor":"older"}}'
cat >/dev/null
`)
	client := New(bin)
	t.Cleanup(func() { _ = client.Close() })
	page, err := client.HistoryPage(context.Background(), process.CodexHistoryPageInput{ThreadID: "thread-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor != "older" || len(page.Events) != 2 {
		t.Fatalf("page = %+v", page)
	}
	message, ok := page.Events[1].Content.(process.CodexMessageContent)
	if !ok || message.Role != "assistant" || message.Text != "done" {
		t.Fatalf("agent event = %#v", page.Events[1])
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

func TestNewUsesCODEXBIN(t *testing.T) {
	t.Setenv("CODEX_BIN", "/custom/codex")
	if got := New("").Bin(); got != "/custom/codex" {
		t.Fatalf("Bin() = %q", got)
	}
}

var _ = strings.TrimSpace
