package codexcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileAnalysisUsesIsolatedEphemeralReadOnlyThread(t *testing.T) {
	requestLog := filepath.Join(t.TempDir(), "request.json")
	turnLog := filepath.Join(t.TempDir(), "turn.json")
	t.Setenv("PROFILE_REQUEST_LOG", requestLog)
	t.Setenv("PROFILE_TURN_LOG", turnLog)
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"userAgent":"profile-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' "$request" > "$PROFILE_REQUEST_LOG"
printf '%s\n' '{"id":2,"result":{"thread":{"id":"profile-thread"}}}'
IFS= read -r request
printf '%s\n' "$request" > "$PROFILE_TURN_LOG"
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"item/completed","params":{"threadId":"profile-thread","turnId":"turn-1","item":{"id":"answer","type":"agentMessage","text":"{\"prompt\":\"常见问题：遗漏验证。避免方式：报告实际验证结果。\",\"contextPromptIds\":[]}"}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"profile-thread","turn":{"id":"turn-1","status":"completed","items":[]}}}'
cat >/dev/null
`)
	client := New(bin, WithCodexHome(t.TempDir()))
	defer client.Close()
	result, err := client.AnalyzeProfile(context.Background(), "previous", "history", "test-model", "high")
	if err != nil || !strings.Contains(result.Prompt, "避免方式") {
		t.Fatalf("result = %#v, %v", result, err)
	}
	data, err := os.ReadFile(requestLog)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Params struct {
			Ephemeral bool   `json:"ephemeral"`
			Tools     []any  `json:"dynamicTools"`
			Model     string `json:"model"`
			Sandbox   string `json:"sandbox"`
			Cwd       string `json:"cwd"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if !request.Params.Ephemeral || len(request.Params.Tools) != 0 || request.Params.Model != "test-model" || request.Params.Sandbox != "read-only" {
		t.Fatalf("unsafe analysis request: %s", data)
	}
	if _, err := os.Stat(request.Params.Cwd); !os.IsNotExist(err) {
		t.Fatalf("temporary analysis directory retained: %v", err)
	}
	data, err = os.ReadFile(turnLog)
	if err != nil {
		t.Fatal(err)
	}
	var turn struct {
		Params struct {
			Model  string `json:"model"`
			Effort string `json:"effort"`
		} `json:"params"`
	}
	if err := json.Unmarshal(data, &turn); err != nil {
		t.Fatal(err)
	}
	if turn.Params.Model != "test-model" || turn.Params.Effort != "high" {
		t.Fatalf("analysis model/effort not forwarded: %s", data)
	}
}
