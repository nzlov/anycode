package codexcli

import (
	"encoding/json"
	"testing"

	"github.com/nzlov/anycode/internal/domain/process"
)

func TestProjectorConvertsSuccessfulExecApplyPatchToFileChanges(t *testing.T) {
	quotedPatch := "*** Begin Patch\n*** Update File: /workspace/project/main.go\n@@ -1 +1 @@\n-old\n+new\n*** End Patch"
	quotedPatchJSON, err := json.Marshal(quotedPatch)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		source      string
		result      string
		wantKind    string
		wantPath    string
		wantMove    string
		wantFailure bool
	}{
		{
			name:     "quoted string update",
			source:   "const patch = " + string(quotedPatchJSON) + ";\ntext(await tools.apply_patch(patch));",
			result:   "Script completed\nWall time 0.1 seconds\nOutput:\n",
			wantKind: "modified",
			wantPath: "main.go",
		},
		{
			name: "raw template rename",
			source: "const patch = String.raw`*** Begin Patch\n" +
				"*** Update File: /workspace/project/old.go\n" +
				"*** Move to: /workspace/project/new.go\n" +
				"@@ -1 +1 @@\n-old\n+new\n*** End Patch`;\n" +
				"text(await tools.apply_patch(patch));",
			result:   "Script completed\nWall time 0.1 seconds\nOutput:\n",
			wantKind: "renamed",
			wantPath: "old.go",
			wantMove: "new.go",
		},
		{
			name: "failed patch remains exec tool",
			source: "const patch = String.raw`*** Begin Patch\n" +
				"*** Delete File: /workspace/project/main.go\n*** End Patch`;\n" +
				"text(await tools.apply_patch(patch));",
			result:      "Script failed\nWall time 0.1 seconds\nOutput:\n",
			wantFailure: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projector := newCodexTranscriptProjector()
			lines := []map[string]any{
				{
					"timestamp": "2026-07-24T00:00:01Z",
					"type":      "response_item",
					"payload": map[string]any{
						"type": "custom_tool_call", "call_id": "patch-1", "name": "exec", "input": test.source,
					},
				},
				{
					"timestamp": "2026-07-24T00:00:02Z",
					"type":      "response_item",
					"payload": map[string]any{
						"type": "custom_tool_call_output", "call_id": "patch-1",
						"output": []any{
							map[string]any{"type": "input_text", "text": test.result},
							map[string]any{"type": "input_text", "text": "{}"},
						},
					},
				},
			}
			var projected []codexLogEvent
			for index, line := range lines {
				raw, err := json.Marshal(line)
				if err != nil {
					t.Fatal(err)
				}
				projected = append(projected, projector.project(parseSessionLogLine(raw, "/workspace/project", "rollout.jsonl", int64(index)))...)
			}
			if len(projected) != 1 {
				t.Fatalf("projected events = %#v, want one terminal event", projected)
			}
			if test.wantFailure {
				tool, ok := projected[0].Content.(process.CodexToolContent)
				if !ok || projected[0].Phase != process.CodexPhaseFailed || tool.QualifiedName != "exec" || tool.Input.Text != test.source {
					t.Fatalf("failed patch event = %#v", projected[0])
				}
				return
			}
			content, ok := projected[0].Content.(process.CodexFileChangeContent)
			if !ok || projected[0].Phase != process.CodexPhaseStandalone || len(content.Changes) != 1 {
				t.Fatalf("file change event = %#v", projected[0])
			}
			change := content.Changes[0]
			if change.Kind != test.wantKind || change.Path != test.wantPath || change.MovePath != test.wantMove || change.UnifiedDiff == "" {
				t.Fatalf("file change = %#v", change)
			}
		})
	}
}

func TestExtractExecApplyPatchHandlesMultipleWorkspaceFiles(t *testing.T) {
	source := "text(await tools.apply_patch(String.raw`*** Begin Patch\n" +
		"*** Add File: /workspace/project/new.go\n+package sample\n" +
		"*** Delete File: /workspace/project/old.go\n*** End Patch`));"
	changes, ok := extractExecApplyPatch(source, "/workspace/project")
	if !ok || len(changes) != 2 {
		t.Fatalf("changes = %#v, want added and deleted files", changes)
	}
	if changes[0].Kind != "added" || changes[0].Path != "new.go" || changes[1].Kind != "deleted" || changes[1].Path != "old.go" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestExtractExecApplyPatchRejectsPathsOutsideWorkspace(t *testing.T) {
	source := "text(await tools.apply_patch(String.raw`*** Begin Patch\n" +
		"*** Add File: /outputs/session-1/report.txt\n+report\n*** End Patch`));"
	if changes, ok := extractExecApplyPatch(source, "/workspace/project"); ok || len(changes) != 0 {
		t.Fatalf("external changes = %#v, want no transcript file paths", changes)
	}
}

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
