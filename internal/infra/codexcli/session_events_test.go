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

func TestCommandProjectorHidesWaitProgressAndEmitsResultOnlyTerminal(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-07-22T00:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"exec-1","name":"exec","input":"const r = await tools.exec_command({\"cmd\":\"go test ./...\",\"workdir\":\"/workspace\",\"yield_time_ms\":1000}); text(r);"}}`,
		`{"timestamp":"2026-07-22T00:00:02Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"exec-1","output":[{"type":"input_text","text":"Script running with cell ID 37\nWall time 1.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"chunk_id\":\"one\",\"session_id\":37,\"wall_time_seconds\":1,\"output\":\"first\"}"}]}}`,
		`{"timestamp":"2026-07-22T00:00:03Z","type":"response_item","payload":{"type":"function_call","name":"wait","arguments":"{\"cell_id\":\"37\",\"yield_time_ms\":1000}","call_id":"wait-1"}}`,
		`{"timestamp":"2026-07-22T00:00:04Z","type":"response_item","payload":{"type":"function_call_output","call_id":"wait-1","output":[{"type":"input_text","text":"Script running with cell ID 37\nWall time 1.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"chunk_id\":\"two\",\"session_id\":37,\"wall_time_seconds\":1,\"output\":\"second\"}"}]}}`,
		`{"timestamp":"2026-07-22T00:00:05Z","type":"response_item","payload":{"type":"function_call","name":"wait","arguments":"{\"cell_id\":\"37\",\"yield_time_ms\":1000}","call_id":"wait-2"}}`,
		`{"timestamp":"2026-07-22T00:00:06Z","type":"response_item","payload":{"type":"function_call_output","call_id":"wait-2","output":[{"type":"input_text","text":"Script completed\nWall time 0.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"chunk_id\":\"three\",\"wall_time_seconds\":0,\"exit_code\":0,\"output\":\"third\"}"}]}}`,
	}
	projector := newCodexTranscriptProjector()
	var projected []codexLogEvent
	for index, line := range lines {
		projected = append(projected, projector.project(parseSessionLogLine([]byte(line), "/workspace", "rollout.jsonl", int64(index)))...)
	}
	if len(projected) != 2 {
		t.Fatalf("projected events = %#v, want started and terminal only", projected)
	}
	started, ok := projected[0].Content.(process.CodexCommandContent)
	if !ok || projected[0].Phase != process.CodexPhaseStarted || projected[0].CorrelationID != "exec-1" || len(started.Commands) != 1 || started.Commands[0].Command != "go test ./..." {
		t.Fatalf("started event = %#v", projected[0])
	}
	terminal, ok := projected[1].Content.(process.CodexCommandContent)
	if !ok || projected[1].Phase != process.CodexPhaseCompleted || projected[1].CorrelationID != "exec-1" || len(terminal.Commands) != 1 {
		t.Fatalf("terminal event = %#v", projected[1])
	}
	command := terminal.Commands[0]
	if command.Command != "" || command.Workdir != "" || command.Output != "firstsecondthird" || command.ExitCode == nil || *command.ExitCode != 0 {
		t.Fatalf("terminal command = %#v", command)
	}
	if terminal.DurationMS == nil || *terminal.DurationMS != 5000 || command.DurationMS == nil || *command.DurationMS != 5000 {
		t.Fatalf("terminal duration = content:%v command:%v", terminal.DurationMS, command.DurationMS)
	}
}
