package codexcli

import (
	"testing"

	"github.com/nzlov/anycode/internal/domain/process"
)

func TestParseSessionLogLineFiltersCanonicalItemLifecycleMirrors(t *testing.T) {
	tests := []string{
		`{"timestamp":"2026-07-22T13:08:01Z","type":"event_msg","payload":{"type":"item_started","thread_id":"thread-1","turn_id":"turn-1","item":{"type":"CommandExecution","id":"exec-1","status":"in_progress"}}}`,
		`{"timestamp":"2026-07-22T13:08:02Z","type":"event_msg","payload":{"type":"item_completed","thread_id":"thread-1","turn_id":"turn-1","item":{"type":"CommandExecution","id":"exec-1","status":"completed","stdout":"passed"}}}`,
	}
	for index, raw := range tests {
		if got := parseSessionLogLine([]byte(raw), "/workspace/project", "rollout.jsonl", int64(index)); len(got) != 0 {
			t.Fatalf("item lifecycle mirror %d events = %#v, want none", index, got)
		}
	}
}

func TestNormalizeCustomToolOutputParsesSingleStringScriptSummary(t *testing.T) {
	result := normalizeCustomToolOutput("Script running with cell ID 55\nWall time 11.0 seconds\nOutput:\n")

	if result.status != "running" || result.durationMS == nil || *result.durationMS != 11000 || result.executionID != "55" {
		t.Fatalf("normalized result = %#v", result)
	}
}

func TestProjectorMergesWaitIntoRunningCommand(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"command-1","name":"exec","input":"const r = await tools.exec_command({cmd:\"go test ./...\",workdir:\"/workspace\"}); text(r);"}}`,
		`{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"command-1","output":"Script running with cell ID 55\nWall time 11.0 seconds\nOutput:\n"}}`,
		`{"timestamp":"2026-07-22T00:00:03Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"wait-1","name":"exec","input":"const r = await tools.wait({cell_id:\"55\",yield_time_ms:30000,max_tokens:24000}); text(r);"}}`,
		`{"timestamp":"2026-07-22T00:00:04Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"wait-1","output":"Script completed\nWall time 2.0 seconds\nOutput:\n\nok"}}`,
	}
	projector := newCodexTranscriptProjector()
	var projected []codexLogEvent
	for index, line := range lines {
		projected = append(projected, projector.project(parseSessionLogLine([]byte(line), "/workspace", "rollout.jsonl", int64(index)))...)
	}

	if len(projected) != 4 {
		t.Fatalf("projected events = %#v", projected)
	}
	for index, event := range projected {
		if event.CorrelationID != "command-1" {
			t.Fatalf("event %d correlation = %q", index, event.CorrelationID)
		}
		if _, ok := event.Content.(process.CodexCommandContent); !ok {
			t.Fatalf("event %d content = %#v", index, event.Content)
		}
	}
	completed := projected[len(projected)-1]
	command := completed.Content.(process.CodexCommandContent)
	if completed.Phase != process.CodexPhaseCompleted || command.DurationMS == nil || *command.DurationMS != 13000 {
		t.Fatalf("completed event = %#v", completed)
	}
	if len(command.Commands) != 1 || command.Commands[0].Output != "ok" || command.Commands[0].DurationMS == nil || *command.Commands[0].DurationMS != 13000 {
		t.Fatalf("completed command = %#v", command)
	}
}

func TestProjectorMergesFunctionWaitIntoRunningShell(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"function_call","call_id":"shell-1","name":"shell","arguments":"{\"command\":\"go test ./...\"}"}}`,
		`{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"function_call_output","call_id":"shell-1","output":"Script running with cell ID 77\nWall time 10.0 seconds\nOutput:\n"}}`,
		`{"timestamp":"2026-07-22T00:00:03Z","type":"response_item","payload":{"type":"function_call","call_id":"wait-2","name":"wait","arguments":"{\"cell_id\":77}"}}`,
		`{"timestamp":"2026-07-22T00:00:04Z","type":"response_item","payload":{"type":"function_call_output","call_id":"wait-2","exit_code":0,"output":"Script completed\nWall time 3.0 seconds\nOutput:\n\npassed"}}`,
	}
	projector := newCodexTranscriptProjector()
	var projected []codexLogEvent
	for index, line := range lines {
		projected = append(projected, projector.project(parseSessionLogLine([]byte(line), "/workspace", "rollout.jsonl", int64(index)))...)
	}

	if len(projected) != 4 {
		t.Fatalf("projected events = %#v", projected)
	}
	for index, event := range projected {
		command, ok := event.Content.(process.CodexCommandContent)
		if event.CorrelationID != "shell-1" || !ok || command.Kind != process.CodexCommandShell {
			t.Fatalf("event %d = %#v", index, event)
		}
	}
	completed := projected[len(projected)-1]
	command := completed.Content.(process.CodexCommandContent)
	if completed.Phase != process.CodexPhaseCompleted || command.DurationMS == nil || *command.DurationMS != 13000 || command.Commands[0].Output != "passed" || command.Commands[0].ExitCode == nil || *command.Commands[0].ExitCode != 0 {
		t.Fatalf("completed event = %#v", completed)
	}
}
