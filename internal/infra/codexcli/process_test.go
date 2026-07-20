package codexcli

import (
	"context"
	"errors"
	"fmt"
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
	stdinFile := filepath.Join(dir, "stdin")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
pwd > "$CODEX_PWD_FILE"
cat > "$CODEX_STDIN_FILE"
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-1"}'
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
	t.Setenv("CODEX_STDIN_FILE", stdinFile)
	t.Setenv("CODEX_HOME", codexHome)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{
		ProcessRunID:    "process-run-1",
		SessionID:       "session-1",
		Workdir:         dir,
		Prompt:          "implement adapter",
		Model:           "gpt-test",
		ReasoningEffort: "medium",
		PermissionMode:  "workspace-write",
		FastMode:        true,
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

	source := eventContent[process.CodexTranscriptSource](t, got[0])
	if got[0].Type != process.CodexEventTranscriptBound || source.CodexSessionID != "codex-session-1" {
		t.Fatalf("first event = %+v", got[0])
	}
	if got[1].Type != process.CodexEventCommand || got[1].Phase != process.CodexPhaseStarted {
		t.Fatalf("command start event = %+v", got[1])
	}
	startedCommand := eventContent[process.CodexCommandContent](t, got[1])
	if len(startedCommand.Commands) != 1 || startedCommand.Commands[0].Command != "go test ./..." || got[1].CorrelationID != "call-command" {
		t.Fatalf("command event = %#v", got[1])
	}
	if got[2].Type != process.CodexEventCommand || got[2].Phase != process.CodexPhaseCompleted {
		t.Fatalf("command result event = %+v", got[2])
	}
	commandResult, ok := got[2].Content.(process.CodexCommandContent)
	if !ok || commandResult.Kind != process.CodexCommandShell || len(commandResult.Commands) != 1 || commandResult.Commands[0].Command != "go test ./..." || !commandResult.Commands[0].HasOutput || commandResult.Commands[0].Output != "ok" {
		t.Fatalf("typed command result = %#v", got[2].Content)
	}
	if got[2].CorrelationID != "call-command" {
		t.Fatalf("command result correlation = %#v", got[2])
	}
	if got[1].EventID == got[2].EventID {
		t.Fatalf("command lifecycle reused event id %q", got[1].EventID)
	}
	if got[3].Type != process.CodexEventFileChange {
		t.Fatalf("file change event = %+v", got[3])
	}
	fileChange := eventContent[process.CodexFileChangeContent](t, got[3])
	if got[3].CorrelationID != "call-patch" || len(fileChange.Changes) != 1 || fileChange.Changes[0].Path != "probe.txt" {
		t.Fatalf("file change = %#v", got[3])
	}

	args := strings.TrimSpace(readFile(t, argsFile))
	want := `exec --json --skip-git-repo-check -C ` + dir + ` -m gpt-test -c model_reasoning_effort="medium" -c service_tier="priority" --sandbox workspace-write -i /kept/in/input.png -`
	if args != want {
		t.Fatalf("args = %q, want %q", args, want)
	}
	if got := readFile(t, stdinFile); got != "implement adapter" {
		t.Fatalf("stdin = %q, want prompt", got)
	}
	if gotDir := strings.TrimSpace(readFile(t, pwdFile)); gotDir != dir {
		t.Fatalf("pwd = %q, want %q", gotDir, dir)
	}
}

func TestEventsWaitsForDelayedSessionLogWhileProcessRuns(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
sleep 6
mkdir -p "$CODEX_HOME/sessions/2026/07/15"
cat > "$CODEX_HOME/sessions/2026/07/15/rollout-delayed-session.jsonl" <<EOF
{"timestamp":"2026-07-15T12:00:00Z","type":"session_meta","payload":{"session_id":"delayed-session","cwd":"$PWD","originator":"codex_exec"}}
EOF
printf '%s\n' '{"type":"thread.started","thread_id":"delayed-session"}'
`)
	t.Setenv("CODEX_HOME", codexHome)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "process-run-delayed-session-log",
		Workdir:      dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}

	startedAt := time.Now()
	select {
	case event := <-events:
		source := eventContent[process.CodexTranscriptSource](t, event)
		if event.Type != process.CodexEventTranscriptBound || source.CodexSessionID != "delayed-session" {
			t.Fatalf("first event = %+v", event)
		}
		if elapsed := time.Since(startedAt); elapsed < 5*time.Second {
			t.Fatalf("session log arrived after %s, want delay beyond old timeout", elapsed)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for delayed session log")
	}

	got := collectEvents(t, events, 1)
	exit := eventContent[process.ExitResult](t, got[0])
	if got[0].Type != process.CodexEventProcessExit || exit.ExitCode == nil || *exit.ExitCode != 0 {
		t.Fatalf("exit event = %+v", got[0])
	}
}

func TestEventsStopsWaitingWhenProcessExitsWithoutSessionLog(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
exit 0
`)
	t.Setenv("CODEX_HOME", codexHome)

	handle, err := New(bin).Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "process-run-without-session-log",
		Workdir:      dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := New(bin).Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-events:
		if event.Type != process.CodexEventProcessExit {
			t.Fatalf("event = %+v", event)
		}
		exit := eventContent[process.ExitResult](t, event)
		if exit.FailureCode != "codex_transcript_unavailable" || !strings.Contains(exit.FailureReason, process.ErrTranscriptUnavailable.Error()) {
			t.Fatalf("exit content = %+v", exit)
		}
	case <-time.After(time.Second):
		t.Fatal("process exit did not stop the session log wait")
	}
}

func TestCodexTranscriptProjectorKeepsMissingTimestampInSourceOrder(t *testing.T) {
	projector := newCodexTranscriptProjector()
	previous := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"turn-1"}}`), "/workspace/project", "rollout.jsonl", 10)
	missing := parseSessionLogLine([]byte(`{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"turn-1"}}`), "/workspace/project", "rollout.jsonl", 20)
	next := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"task_complete","turn_id":"turn-1"}}`), "/workspace/project", "rollout.jsonl", 30)
	projector.project(previous)
	projector.project(missing)
	projector.project(next)
	if !missing[0].CreatedAt.Equal(previous[0].CreatedAt) {
		t.Fatalf("missing timestamp = %s, want previous %s", missing[0].CreatedAt, previous[0].CreatedAt)
	}
	if !missing[0].CreatedAt.Before(next[0].CreatedAt) || missing[0].SourceOffset <= previous[0].SourceOffset {
		t.Fatalf("source order = previous %#v, missing %#v, next %#v", previous[0], missing[0], next[0])
	}
}

func TestPrimeCodexTranscriptProjectorCorrelatesCommandAcrossResumeBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-resume-command.jsonl")
	prefix := `{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-1","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call","call_id":"call-shell","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}"}}
`
	if err := os.WriteFile(path, []byte(prefix), 0o644); err != nil {
		t.Fatal(err)
	}
	projector := newCodexTranscriptProjector()
	_, resumeOffset, skipLeadingLineTerminator, err := primeCodexTranscriptProjector(path, int64(len(prefix)), "/workspace/project", filepath.Base(path), projector)
	if err != nil {
		t.Fatal(err)
	}
	if resumeOffset != int64(len(prefix)) {
		t.Fatalf("resume offset = %d, want %d", resumeOffset, len(prefix))
	}
	if skipLeadingLineTerminator {
		t.Fatal("newline-terminated prefix must not skip a future line terminator")
	}
	completed := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:02Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-shell","output":"ok"}}`), "/workspace/project", filepath.Base(path), int64(len(prefix)))
	projector.project(completed)
	command, ok := completed[0].Content.(process.CodexCommandContent)
	if !ok || len(command.Commands) != 1 || command.Commands[0].Command != "go test ./..." || !command.Commands[0].HasOutput || command.Commands[0].Output != "ok" {
		t.Fatalf("resumed command completion = %#v", completed[0].Content)
	}
}

func TestPrimeCodexTranscriptProjectorDoesNotReplayMessageMirrorAcrossResumeBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-resume-message.jsonl")
	prefix := `{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-1","cwd":"/workspace/project"}}
`
	mirror := `{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"working"}}
`
	canonical := `{"timestamp":"2026-07-08T09:00:01.022Z","type":"response_item","payload":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"working"}]}}
`
	full := prefix + mirror + canonical
	if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		baseline  int64
		wantCount int
	}{
		{name: "before mirror", baseline: int64(len(prefix)), wantCount: 1},
		{name: "after mirror", baseline: int64(len(prefix + mirror)), wantCount: 0},
		{name: "after canonical", baseline: int64(len(full)), wantCount: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projector := newCodexTranscriptProjector()
			_, resumeOffset, skipLeadingLineTerminator, err := primeCodexTranscriptProjector(path, test.baseline, "/workspace/project", filepath.Base(path), projector)
			if err != nil {
				t.Fatal(err)
			}
			if skipLeadingLineTerminator {
				t.Fatal("newline-terminated baseline must not skip a future line terminator")
			}

			var resumed []codexLogEvent
			tail := strings.TrimSuffix(full[resumeOffset:], "\n")
			lineOffset := resumeOffset
			if tail != "" {
				for _, line := range strings.Split(tail, "\n") {
					parsed := parseSessionLogLine([]byte(line), "/workspace/project", filepath.Base(path), lineOffset)
					resumed = append(resumed, projector.project(parsed)...)
					lineOffset += int64(len(line) + 1)
				}
			}
			resumed = append(resumed, projector.flushPending()...)
			if len(resumed) != test.wantCount {
				t.Fatalf("resumed events = %d, want %d: %#v", len(resumed), test.wantCount, resumed)
			}
			if test.wantCount == 1 && !strings.Contains(resumed[0].EventID, "msg-1") {
				t.Fatalf("resumed event id = %q, want canonical message", resumed[0].EventID)
			}
		})
	}
}

func TestPrimedMessageMirrorDoesNotDelayNewCanonicalMessagePastWindow(t *testing.T) {
	projector := newCodexTranscriptProjector()
	historicalMirror := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"old"}}`), "/workspace/project", "rollout.jsonl", 0)
	newCanonical := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01.022Z","type":"response_item","payload":{"type":"message","id":"msg-new","role":"assistant","content":[{"type":"output_text","text":"new"}]}}`), "/workspace/project", "rollout.jsonl", 1)
	projector.prime(historicalMirror)
	got := projector.project(newCanonical)
	if len(got) != 1 || !strings.Contains(got[0].EventID, "msg-new") {
		t.Fatalf("new canonical after primed mirror = %#v", got)
	}
}

func TestCodexTranscriptProjectorKeepsMirrorPendingAcrossInterleavedEvent(t *testing.T) {
	projector := newCodexTranscriptProjector()
	lines := []string{
		`{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"working"}}`,
		`{"timestamp":"2026-07-08T09:00:01.010Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}}`,
		`{"timestamp":"2026-07-08T09:00:01.022Z","type":"response_item","payload":{"type":"message","id":"msg-working","role":"assistant","content":[{"type":"output_text","text":"working"}]}}`,
	}
	var got []codexLogEvent
	for index, line := range lines {
		parsed := parseSessionLogLine([]byte(line), "/workspace/project", "rollout.jsonl", int64(index))
		got = append(got, projector.project(parsed)...)
	}
	got = append(got, projector.flushPending()...)
	if len(got) != 2 || got[0].Type != "token_count" || !strings.Contains(got[1].EventID, "msg-working") {
		t.Fatalf("interleaved mirror events = %#v", got)
	}
}

func TestCodexTranscriptProjectorCorrelatesMultiplePendingMirrors(t *testing.T) {
	projector := newCodexTranscriptProjector()
	lines := []string{
		`{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"first"}}`,
		`{"timestamp":"2026-07-08T09:00:01.010Z","type":"event_msg","payload":{"type":"agent_message","message":"second"}}`,
		`{"timestamp":"2026-07-08T09:00:01.020Z","type":"response_item","payload":{"type":"message","id":"msg-first","role":"assistant","content":[{"type":"output_text","text":"first"}]}}`,
		`{"timestamp":"2026-07-08T09:00:01.030Z","type":"response_item","payload":{"type":"message","id":"msg-second","role":"assistant","content":[{"type":"output_text","text":"second"}]}}`,
	}
	var got []codexLogEvent
	for index, line := range lines {
		parsed := parseSessionLogLine([]byte(line), "/workspace/project", "rollout.jsonl", int64(index))
		got = append(got, projector.project(parsed)...)
	}
	got = append(got, projector.flushPending()...)
	if len(got) != 2 || !strings.Contains(got[0].EventID, "msg-first") || !strings.Contains(got[1].EventID, "msg-second") {
		t.Fatalf("multiple pending mirror events = %#v", got)
	}
}

func TestPrimeCodexTranscriptProjectorResumesAtIncompleteLineStart(t *testing.T) {
	prefix := `{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-1","cwd":"/workspace/project"}}
`
	started := `{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call","call_id":"call-shell","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}"}}
`
	completed := `{"timestamp":"2026-07-08T09:00:02Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-shell","output":"ok"}}
`
	full := prefix + started + completed
	tests := []struct {
		name                   string
		baselineLength         int
		wantResumeOffset       int64
		wantSkipLineTerminator bool
		wantResumedEventCount  int
	}{
		{
			name:                  "partial JSON body",
			baselineLength:        strings.Index(started, "test"),
			wantResumeOffset:      int64(len(prefix)),
			wantResumedEventCount: 2,
		},
		{
			name:                   "complete JSON without newline",
			baselineLength:         len(strings.TrimSuffix(started, "\n")),
			wantResumeOffset:       int64(len(prefix) + len(strings.TrimSuffix(started, "\n"))),
			wantSkipLineTerminator: true,
			wantResumedEventCount:  1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "rollout-resume-partial.jsonl")
			baseline := int64(len(prefix) + test.baselineLength)
			if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
				t.Fatal(err)
			}

			projector := newCodexTranscriptProjector()
			_, resumeOffset, skipLeadingLineTerminator, err := primeCodexTranscriptProjector(path, baseline, "/workspace/project", filepath.Base(path), projector)
			if err != nil {
				t.Fatal(err)
			}
			if resumeOffset != test.wantResumeOffset {
				t.Fatalf("resume offset = %d, want %d", resumeOffset, test.wantResumeOffset)
			}
			if skipLeadingLineTerminator != test.wantSkipLineTerminator {
				t.Fatalf("skip line terminator = %t, want %t", skipLeadingLineTerminator, test.wantSkipLineTerminator)
			}

			tail := full[resumeOffset:]
			if skipLeadingLineTerminator {
				tail = strings.TrimPrefix(tail, "\n")
			}
			lines := strings.Split(strings.TrimSuffix(tail, "\n"), "\n")
			lineOffset := resumeOffset
			var resumed []codexLogEvent
			for _, line := range lines {
				parsed := parseSessionLogLine([]byte(line), "/workspace/project", filepath.Base(path), lineOffset)
				parsed = projector.project(parsed)
				resumed = append(resumed, parsed...)
				lineOffset += int64(len(line) + 1)
			}
			if len(resumed) != test.wantResumedEventCount {
				t.Fatalf("resumed events = %d, want %d", len(resumed), test.wantResumedEventCount)
			}
			command, ok := resumed[len(resumed)-1].Content.(process.CodexCommandContent)
			if !ok || len(command.Commands) != 1 || command.Commands[0].Command != "go test ./..." || !command.Commands[0].HasOutput || command.Commands[0].Output != "ok" {
				t.Fatalf("resumed command completion = %#v", resumed[len(resumed)-1].Content)
			}
			for _, event := range resumed {
				if event.Type == "invalid_json" {
					t.Fatalf("resumed partial line emitted invalid JSON: %#v", resumed)
				}
			}
		})
	}
}

func TestCodexTranscriptProjectorUsesStartedTypeForTimedResults(t *testing.T) {
	projector := newCodexTranscriptProjector()
	commandStarted := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"function_call","call_id":"call-shell","name":"exec_command","arguments":"{\"cmd\":\"go test ./...\"}"}}`), "/workspace/project", "rollout.jsonl", 0)
	commandCompleted := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-shell","output":"failed","exit_code":7,"duration_ms":125}}`), "/workspace/project", "rollout.jsonl", 1)
	projector.project(commandStarted)
	projector.project(commandCompleted)
	command, ok := commandCompleted[0].Content.(process.CodexCommandContent)
	if !ok || len(command.Commands) != 1 || command.Commands[0].Command != "go test ./..." || command.Commands[0].ExitCode == nil || *command.Commands[0].ExitCode != 7 || command.Commands[0].DurationMS == nil || *command.Commands[0].DurationMS != 125 || command.DurationMS == nil || *command.DurationMS != 125 || commandCompleted[0].Phase != process.CodexPhaseFailed {
		t.Fatalf("correlated command result = %#v, phase %q", commandCompleted[0].Content, commandCompleted[0].Phase)
	}

	toolStarted := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:02Z","type":"response_item","payload":{"type":"function_call","call_id":"call-tool","name":"answer_user","arguments":"{\"questions\":[]}"}}`), "/workspace/project", "rollout.jsonl", 2)
	toolCompleted := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:03Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-tool","output":"answered","duration_ms":25}}`), "/workspace/project", "rollout.jsonl", 3)
	projector.project(toolStarted)
	projector.project(toolCompleted)
	tool, ok := toolCompleted[0].Content.(process.CodexToolContent)
	if !ok || tool.Output.Text != "answered" {
		t.Fatalf("timed tool result = %#v", toolCompleted[0].Content)
	}
}

func TestCodexTranscriptProjectorCorrelatesCustomExecBatchOutput(t *testing.T) {
	projector := newCodexTranscriptProjector()
	started := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-exec","name":"exec","input":"const results = await Promise.all([tools.exec_command({\"cmd\":\"npm test\",\"workdir\":\"/workspace/web\"}), tools.exec_command({\"cmd\":\"go test ./...\"})]);"}}`), "/workspace/project", "rollout.jsonl", 0)
	completed := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-exec","output":[{"type":"input_text","text":"Script failed\nWall time 1.2 seconds\nOutput:\n"},{"type":"input_text","text":"transport diagnostic"},{"type":"input_text","text":"result one\n{\"chunk_id\":\"first\",\"wall_time_seconds\":1.001,\"exit_code\":0,\"output\":\"\"}"},{"type":"input_text","text":"result two\n{\"chunk_id\":\"second\",\"wall_time_seconds\":0.157,\"exit_code\":1,\"output\":\"failed tests\\n\"}"}]}}`), "/workspace/project", "rollout.jsonl", 1)
	projectedStarted := projector.project(started)
	projector.project(completed)

	command, ok := completed[0].Content.(process.CodexCommandContent)
	if !ok || command.Kind != process.CodexCommandExec || completed[0].CorrelationID != "call-exec" || len(command.Commands) != 2 || command.Commands[0].Command != "npm test" || command.Commands[0].Workdir != "/workspace/web" || !command.Commands[0].HasOutput || command.Commands[0].Output != "" || command.Commands[0].ExitCode == nil || *command.Commands[0].ExitCode != 0 || command.Commands[1].Command != "go test ./..." || !command.Commands[1].HasOutput || command.Commands[1].Output != "failed tests\n" || command.Commands[1].ExitCode == nil || *command.Commands[1].ExitCode != 1 || completed[0].Phase != process.CodexPhaseFailed {
		t.Fatalf("completed batch = %#v, correlationID %q", completed[0].Content, completed[0].CorrelationID)
	}
	startedCommand, ok := projectedStarted[0].Content.(process.CodexCommandContent)
	if !ok || startedCommand.Commands[0].HasOutput || startedCommand.Commands[1].HasOutput {
		t.Fatalf("started command was mutated by completion: %#v", projectedStarted[0].Content)
	}
}

func TestCodexTranscriptProjectorStripsCustomExecTransportSummary(t *testing.T) {
	projector := newCodexTranscriptProjector()
	started := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-exec","name":"exec","input":"const result = await tools.exec_command({\"cmd\":\"go test ./...\"}); text(result.output);"}}`), "/workspace/project", "rollout.jsonl", 0)
	completed := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-exec","output":[{"type":"input_text","text":"Script completed\nWall time 0.3 seconds\nOutput:\n"},{"type":"input_text","text":"all passed\n"}]}}`), "/workspace/project", "rollout.jsonl", 1)
	projector.project(started)
	projector.project(completed)

	command, ok := completed[0].Content.(process.CodexCommandContent)
	if !ok || len(command.Commands) != 1 || !command.Commands[0].HasOutput || command.Commands[0].Output != "all passed\n" || command.Commands[0].DurationMS == nil || *command.Commands[0].DurationMS != 300 {
		t.Fatalf("completed command = %#v", completed[0].Content)
	}
}

func TestCodexTranscriptProjectorUnwrapsWriteStdinResult(t *testing.T) {
	projector := newCodexTranscriptProjector()
	started := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-write","name":"exec","input":"const result = await tools.write_stdin({session_id: 60296, chars: \"\"}); text(result);"}}`), "/workspace/project", "rollout.jsonl", 0)
	completed := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-write","output":[{"type":"input_text","text":"Script completed\nWall time 0.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"chunk_id\":\"686ebc\",\"wall_time_seconds\":0.000008804,\"exit_code\":0,\"original_token_count\":44,\"output\":\"remote: Processed 1 reference\\nmaster -> master\\n\"}"}]}}`), "/workspace/project", "rollout.jsonl", 1)
	projector.project(started)
	projector.project(completed)

	command, ok := completed[0].Content.(process.CodexCommandContent)
	if !ok || len(command.Commands) != 1 || command.Commands[0].Output != "remote: Processed 1 reference\nmaster -> master\n" || command.Commands[0].ExitCode == nil || *command.Commands[0].ExitCode != 0 {
		t.Fatalf("write_stdin command = %#v", completed[0].Content)
	}
}

func TestCodexTranscriptProjectorKeepsRunningCommandOutput(t *testing.T) {
	projector := newCodexTranscriptProjector()
	started := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-write","name":"exec","input":"const result = await tools.write_stdin({session_id: 60296, chars: \"\"}); text(result);"}}`), "/workspace/project", "rollout.jsonl", 0)
	progress := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-write","output":[{"type":"input_text","text":"Script completed\nWall time 10.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"session_id\":60296,\"wall_time_seconds\":10,\"original_token_count\":2,\"output\":\"still running\\n\"}"}]}}`), "/workspace/project", "rollout.jsonl", 1)
	projector.project(started)
	projector.project(progress)

	command, ok := progress[0].Content.(process.CodexCommandContent)
	if !ok || len(command.Commands) != 1 || progress[0].Phase != process.CodexPhaseProgress || command.Commands[0].Output != "still running\n" {
		t.Fatalf("running command = %#v, phase %q", progress[0].Content, progress[0].Phase)
	}
}

func TestCodexTranscriptProjectorKeepsBatchRunningWhenOneCommandIsRunning(t *testing.T) {
	projector := newCodexTranscriptProjector()
	started := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-exec-running","name":"exec","input":"const results = await Promise.all([tools.exec_command({\"cmd\":\"npm test\"}), tools.exec_command({\"cmd\":\"go test ./...\"})]);"}}`), "/workspace/project", "rollout.jsonl", 0)
	progress := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call-exec-running","output":[{"type":"input_text","text":"Script completed\nWall time 10.0 seconds\nOutput:\n"},{"type":"input_text","text":"{\"chunk_id\":\"first\",\"wall_time_seconds\":1,\"exit_code\":0,\"original_token_count\":1,\"output\":\"passed\\n\"}"},{"type":"input_text","text":"{\"session_id\":60296,\"wall_time_seconds\":10,\"original_token_count\":2,\"output\":\"still running\\n\"}"}]}}`), "/workspace/project", "rollout.jsonl", 1)
	projector.project(started)
	projector.project(progress)

	command, ok := progress[0].Content.(process.CodexCommandContent)
	if !ok || progress[0].Phase != process.CodexPhaseProgress || len(command.Commands) != 2 || command.Commands[0].Output != "passed\n" || command.Commands[1].Output != "still running\n" {
		t.Fatalf("running batch = %#v, phase %q", progress[0].Content, progress[0].Phase)
	}
	if _, ok := projector.commands["call-exec-running"]; !ok {
		t.Fatal("running batch command state was discarded")
	}
}

func TestNormalizeCustomToolOutputPreservesNonTransportText(t *testing.T) {
	for _, value := range []any{
		"Script completed\nWall time 0.3 seconds\nOutput:\nuser-owned text",
		`{"output":"domain value","kind":"application_result"}`,
		"command output\n{\"output\":\"domain value\",\"exit_code\":0}",
		"{\"chunk_id\":\"domain\",\"wall_time_seconds\":1}\n{\"output\":\"business\"}",
		[]any{map[string]any{"type": "input_text", "text": "Script completed\nWall time unknown\nOutput:\n"}},
	} {
		want := textFromValue(value)
		if got := normalizeCustomToolOutput(value).output; got != want {
			t.Fatalf("normalizeCustomToolOutput(%#v) = %q, want %q", value, got, want)
		}
	}
}

func TestCodexTranscriptProjectorNamesNestedExecTool(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
	}{
		{
			name:     "nested tool call",
			input:    `const result = await tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "string and comment are ignored",
			input:    "const note = \"tools.fake()\"; // tools.comment()\nconst result = tools.update_plan({plan: []});",
			wantName: "tools.update_plan",
		},
		{
			name:     "plain exec input",
			input:    `npm test`,
			wantName: "exec",
		},
		{
			name:     "template expression",
			input:    "const label = `${await tools.update_plan({plan: []})}`;",
			wantName: "tools.update_plan",
		},
		{
			name:     "regex after control condition is ignored",
			input:    `if (ready) /tools.fake()/.test(input); const result = tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "dynamic command still exposes tool name",
			input:    `const result = tools.exec_command({cmd: command});`,
			wantName: "tools.exec_command",
		},
		{
			name:     "regex after block is ignored",
			input:    `if (ready) {} /tools.fake()/.test(input); const result = tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "automatic semicolon after call",
			input:    "notify(\"starting\")\ntools.update_plan({plan: []})",
			wantName: "tools.update_plan",
		},
		{
			name:     "division after object literal",
			input:    `const ratio = {value: 1} / tools.update_plan({plan: []}) / 2;`,
			wantName: "tools.update_plan",
		},
		{
			name:     "comment before control block",
			input:    `if (ready) /* block */ {} /tools.fake()/.test(input); tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "regex after function block",
			input:    `function helper(input = {}) {} /tools.fake()/.test(input); tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "regex after class block",
			input:    `class Helper {} /tools.fake()/.test(input); tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "switch catch and arrow blocks",
			input:    `try { switch (value) {} } catch (err) {} const helper = () => {}; /tools.fake()/.test(input); tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "case and label blocks",
			input:    `switch (value) { case ready ? 1 : 2: label: {} /tools.fake()/.test(input); break; } tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "declaration keywords used as property keys",
			input:    `const meta = {class: true, function: true, ratio: {value: 1} / tools.update_plan({plan: []}) / 2};`,
			wantName: "tools.update_plan",
		},
		{
			name:     "case expression object and nullish coalescing",
			input:    `switch (value) { case ({key: primary ?? fallback}).key: {} /tools.fake()/.test(input); break; } tools.update_plan({plan: []});`,
			wantName: "tools.update_plan",
		},
		{
			name:     "case keywords used as property keys",
			input:    `const meta = {case: {value: 1} / tools.update_plan({plan: []}) / 2, default: true};`,
			wantName: "tools.update_plan",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := `{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-exec","name":"exec","input":` + strconv.Quote(test.input) + `}}`
			events := parseSessionLogLine([]byte(raw), "/workspace/project", "rollout.jsonl", 0)
			if len(events) != 1 {
				t.Fatalf("events = %d, want 1", len(events))
			}
			tool, ok := events[0].Content.(process.CodexToolContent)
			if !ok || tool.QualifiedName != test.wantName {
				t.Fatalf("content = %#v, want tool name %q", events[0].Content, test.wantName)
			}
		})
	}
}

func TestResumeBuildsResumeCommandInWorkdir(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	pwdFile := filepath.Join(dir, "pwd")
	stdinFile := filepath.Join(dir, "stdin")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
cat > "$CODEX_STDIN_FILE"
pwd > "$CODEX_PWD_FILE"
`)
	t.Setenv("CODEX_ARGS_FILE", argsFile)
	t.Setenv("CODEX_PWD_FILE", pwdFile)
	t.Setenv("CODEX_STDIN_FILE", stdinFile)
	t.Setenv("CODEX_HOME", codexHome)
	writeTranscript(t, codexHome, "codex-session-1", "2026/07/08/rollout-resume-command.jsonl", dir)

	handle, err := New(bin).Resume(context.Background(), process.CodexResumeInput{
		ProcessRunID:    "process-run-2",
		SessionID:       "session-1",
		CodexSessionID:  "codex-session-1",
		Transcript:      transcriptInput(t, codexHome, "codex-session-1").Source,
		Workdir:         dir,
		Prompt:          "next node",
		Model:           "gpt-test",
		ReasoningEffort: "high",
		PermissionMode:  "danger-full-access",
		FastMode:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.CodexSessionID != "codex-session-1" {
		t.Fatalf("CodexSessionID = %q", handle.CodexSessionID)
	}
	waitForFile(t, argsFile)
	waitForFile(t, pwdFile)

	wantArgs := `exec resume --json --skip-git-repo-check -m gpt-test -c model_reasoning_effort="high" -c service_tier="priority" codex-session-1 -`
	if args := strings.TrimSpace(readFile(t, argsFile)); args != wantArgs {
		t.Fatalf("args = %q", args)
	}
	if got := readFile(t, stdinFile); got != "next node" {
		t.Fatalf("stdin = %q, want prompt", got)
	}
	if gotDir := strings.TrimSpace(readFile(t, pwdFile)); gotDir != dir {
		t.Fatalf("pwd = %q, want %q", gotDir, dir)
	}
}

func TestBuildArgsOmitServiceTierWhenFastModeIsDisabled(t *testing.T) {
	client := New("codex")
	for name, args := range map[string][]string{
		"start":  client.buildStartArgs(process.CodexStartInput{Model: "gpt-test"}),
		"resume": client.buildResumeArgs(process.CodexResumeInput{Model: "gpt-test", CodexSessionID: "codex-session-1"}),
	} {
		if joined := strings.Join(args, " "); strings.Contains(joined, "service_tier") {
			t.Fatalf("%s args contain service_tier with FastMode disabled: %q", name, joined)
		}
	}
}

func TestBuildResumeArgsIncludesImagePaths(t *testing.T) {
	args := New("codex").buildResumeArgs(process.CodexResumeInput{
		CodexSessionID: "codex-session-1",
		ImagePaths:     []string{"/archive/first.png", "", "/archive/second.png"},
	})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-i /archive/first.png -i /archive/second.png") {
		t.Fatalf("resume args = %q", joined)
	}
}

func TestArtifactDirectoryIsWritableAndInjectedAcrossStartAndResume(t *testing.T) {
	client := New("codex")
	artifactDir := "/data/attachments/outputs/session-1"
	startArgs := strings.Join(client.buildStartArgs(process.CodexStartInput{
		Workdir: "/workspace", ArtifactDir: artifactDir,
	}), " ")
	if !strings.Contains(startArgs, "--add-dir "+artifactDir) {
		t.Fatalf("start args = %q", startArgs)
	}
	resumeArgs := strings.Join(client.buildResumeArgs(process.CodexResumeInput{
		CodexSessionID: "codex-session-1", ArtifactDir: artifactDir, PermissionMode: "workspace-write",
	}), " ")
	if !strings.Contains(resumeArgs, `sandbox_workspace_write.writable_roots=["`+artifactDir+`"]`) {
		t.Fatalf("resume args = %q", resumeArgs)
	}

	dir := t.TempDir()
	envFile := filepath.Join(dir, "artifact-env")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s' "$ANYCODE_ARTIFACT_DIR" > "$ARTIFACT_ENV_FILE"
`)
	t.Setenv("ARTIFACT_ENV_FILE", envFile)
	if _, err := New(bin).Start(context.Background(), process.CodexStartInput{
		ProcessRunID: "process-1", ArtifactDir: artifactDir,
	}); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, envFile)
	if got := readFile(t, envFile); got != artifactDir {
		t.Fatalf("ANYCODE_ARTIFACT_DIR = %q", got)
	}
}

func TestCodexImagesRecognizesInlineOutputImage(t *testing.T) {
	images := codexImages(map[string]any{
		"output": []any{map[string]any{
			"type": "output_image", "image_url": "data:image/png;base64,cG5n", "detail": "high",
		}},
	})
	if len(images) != 1 || images[0].SourceKind != "inline" || images[0].Source != "data:image/png;base64,cG5n" || images[0].Detail != "high" {
		t.Fatalf("images = %#v", images)
	}
}

func TestCodexImagesRecognizesInlineAudioAndEmbeddedResourceBlobs(t *testing.T) {
	artifacts := codexImages(map[string]any{
		"output": []any{
			map[string]any{"type": "audio", "data": "YXVkaW8=", "mimeType": "audio/mpeg"},
			map[string]any{"type": "resource", "resource": map[string]any{"blob": "cGRm", "mimeType": "application/pdf", "uri": "memory://report"}},
			map[string]any{"type": "resource", "resource": map[string]any{"uri": "https://example.invalid/report.pdf", "mimeType": "application/pdf"}},
		},
	})
	if len(artifacts) != 3 || artifacts[0].SourceKind != "inline_base64" || artifacts[0].MimeType != "audio/mpeg" {
		t.Fatalf("audio artifacts = %#v", artifacts)
	}
	if artifacts[1].SourceKind != "inline_base64" || artifacts[1].MimeType != "application/pdf" {
		t.Fatalf("resource artifact = %#v", artifacts[1])
	}
	if artifacts[2].SourceKind != "remote" {
		t.Fatalf("resource link = %#v", artifacts[2])
	}
}

func TestBuildArgsInjectsIsolatedPlaywrightOutputPerProcessRun(t *testing.T) {
	client := New("codex", WithPlaywrightMCP("playwright-mcp", "/usr/bin/chromium"))
	args := strings.Join(client.buildStartArgs(process.CodexStartInput{
		ProcessRunID: "process-1", ArtifactDir: "/data/attachments/outputs/session-1",
	}), " ")
	for _, expected := range []string{
		`mcp_servers.playwright.command="playwright-mcp"`,
		`mcp_servers.playwright.args=["--headless","--isolated","--image-responses","allow","--output-dir","/data/attachments/outputs/session-1/browser/process-1","--executable-path","/usr/bin/chromium"]`,
		`mcp_servers.playwright.tool_timeout_sec=86400`,
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("playwright args %q missing %q", args, expected)
		}
	}
	resumeArgs := strings.Join(client.buildResumeArgs(process.CodexResumeInput{
		ProcessRunID: "process-2", ArtifactDir: "/data/attachments/outputs/session-1", CodexSessionID: "codex-1",
	}), " ")
	if !strings.Contains(resumeArgs, "/browser/process-2") {
		t.Fatalf("resume playwright args = %q", resumeArgs)
	}
}

func TestResumeStreamsOnlyNewSessionLogEvents(t *testing.T) {
	tests := []struct {
		name            string
		writeTerminator string
	}{
		{name: "LF", writeTerminator: `printf '\n' >> "$CODEX_HOME/sessions/2026/07/08/rollout-resume.jsonl"`},
		{name: "split CRLF", writeTerminator: `printf '\r' >> "$CODEX_HOME/sessions/2026/07/08/rollout-resume.jsonl"
sleep 0.15
printf '\n' >> "$CODEX_HOME/sessions/2026/07/08/rollout-resume.jsonl"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			codexHome := t.TempDir()
			sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-resume.jsonl")
			if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-1","cwd":"`+dir+`"}}
{"timestamp":"2026-07-08T09:00:30Z","type":"response_item","payload":{"type":"function_call","call_id":"plan-old","name":"update_plan","arguments":"{\"plan\":[{\"step\":\"old plan\",\"status\":\"completed\"}]}"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"response_item","payload":{"type":"message","id":"msg-old","role":"assistant","content":[{"type":"output_text","text":"old"}]}}`), 0o644); err != nil {
				t.Fatal(err)
			}
			bin := fakeCodex(t, `#!/bin/sh
sleep 0.2
`+test.writeTerminator+`
cat >> "$CODEX_HOME/sessions/2026/07/08/rollout-resume.jsonl" <<EOF
{"timestamp":"2026-07-08T09:02:00Z","type":"response_item","payload":{"type":"message","id":"msg-new","role":"assistant","content":[{"type":"output_text","text":"new"}]}}
EOF
`)
			t.Setenv("CODEX_HOME", codexHome)

			handle, err := New(bin).Resume(context.Background(), process.CodexResumeInput{
				ProcessRunID:   "process-run-resume",
				SessionID:      "session-1",
				CodexSessionID: "codex-session-1",
				Transcript:     transcriptInput(t, codexHome, "codex-session-1").Source,
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
			got := collectEvents(t, events, 2)
			if got[0].Type != process.CodexEventTranscriptBound {
				t.Fatalf("resume transcript binding = %+v", got[0])
			}
			message, ok := got[1].Content.(process.CodexMessageContent)
			if !ok || message.Text != "new" {
				t.Fatalf("resume replayed wrong event = %+v", got[1])
			}
		})
	}
}

func TestResumeIgnoresStdoutPlansBeforeNewTurn(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-resume-plan.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-resume-plan","cwd":"`+dir+`"}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-resume-plan"}'
printf '%s\n' '{"type":"item.updated","item":{"id":"old-plan","type":"todo_list","items":[{"text":"Historical plan","status":"in_progress"}]}}'
printf '%s\n' '{"type":"item.updated","item":{"id":"old-plan","type":"todo_list","items":[{"text":"Historical plan","status":"completed"}]}}'
printf '%s\n' '{"type":"turn.started","turn_id":"new-turn"}'
printf '%s\n' '{"type":"item.updated","item":{"id":"new-plan","type":"todo_list","items":[{"text":"Current plan","status":"in_progress"}]}}'
sleep 0.2
cat >> "$CODEX_HOME/sessions/2026/07/08/rollout-resume-plan.jsonl" <<EOF
{"timestamp":"2026-07-08T09:02:00Z","type":"response_item","payload":{"type":"message","id":"msg-new","role":"assistant","content":[{"type":"output_text","text":"new"}]}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	handle, err := client.Resume(context.Background(), process.CodexResumeInput{
		ProcessRunID:   "process-run-resume-plan",
		SessionID:      "session-1",
		CodexSessionID: "codex-session-resume-plan",
		Transcript:     transcriptInput(t, codexHome, "codex-session-resume-plan").Source,
		Workdir:        dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	var plans []process.PlanUpdate
	for event := range events {
		if update, ok := event.Content.(process.PlanUpdate); ok {
			plans = append(plans, update)
		}
	}
	if len(plans) != 1 || len(plans[0].Items) != 1 || plans[0].Items[0].Step != "Current plan" {
		t.Fatalf("resume stdout plans = %#v", plans)
	}
}

func TestStdoutPlanUpdateParsesTypedItemsWithStableID(t *testing.T) {
	raw := []byte(`{"type":"item.updated","item":{"id":"plan-1","type":"todo_list","items":[{"text":"Inspect stream","completed":true},{"text":"Persist TODO","completed":false}]}}`)
	first, ok := stdoutPlanUpdate(raw)
	firstUpdate, contentOK := first.Content.(process.PlanUpdate)
	if !ok || !contentOK {
		t.Fatalf("stdout plan update = %#v, %v", first, ok)
	}
	second, ok := stdoutPlanUpdate(raw)
	if !ok || second.EventID != first.EventID || first.EventID == "" {
		t.Fatalf("stable event ids = %q and %q", first.EventID, second.EventID)
	}
	if first.Type != process.CodexEventPlan || first.CorrelationID != "plan-1" || len(firstUpdate.Items) != 2 {
		t.Fatalf("parsed plan update = %#v", first)
	}
	transcript := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call","call_id":"different-call-id","name":"update_plan","arguments":"{\"plan\":[{\"step\":\"Inspect stream\",\"status\":\"completed\"},{\"step\":\"Persist TODO\",\"status\":\"pending\"}]}"}}`), "", "rollout-plan.jsonl", 10)
	if len(transcript) != 1 || transcript[0].PlanUpdate == nil || transcript[0].PlanUpdate.EventID != firstUpdate.EventID {
		t.Fatalf("cross-source plan ids = %#v and %#v", first, transcript)
	}
	if transcript[0].EventID == transcript[0].PlanUpdate.EventID {
		t.Fatalf("transcript event id was replaced by plan id: %#v", transcript[0])
	}
	if firstUpdate.Items[0].Status != process.PlanItemCompleted || firstUpdate.Items[1].Status != process.PlanItemPending {
		t.Fatalf("plan statuses = %#v", firstUpdate.Items)
	}
	inProgress := firstUpdate
	inProgress.Items = append([]process.PlanItem(nil), firstUpdate.Items...)
	inProgress.Items[1].Status = process.PlanItemInProgress
	if stablePlanUpdateEventID(inProgress) == firstUpdate.EventID {
		t.Fatal("pending and in-progress plans reused one event id")
	}
}

func TestStdoutPlanUpdateRejectsTodoPayloadOnUnrelatedEvent(t *testing.T) {
	raw := []byte(`{"type":"assistant_message","item":{"type":"todo_list","items":[{"text":"Do not persist","status":"completed"}]}}`)
	if event, ok := stdoutPlanUpdate(raw); ok {
		t.Fatalf("unrelated event parsed as plan update: %#v", event)
	}
}

func TestEventsMergesStdoutAndSessionPlanUpdatesWithoutDuplicates(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-plan"}'
printf '%s\n' '{"type":"item.updated","item":{"id":"stdout-plan-item","type":"todo_list","items":[{"text":"Inspect stream","status":"completed"},{"text":"Persist TODO","status":"in_progress"}]}}'
sleep 0.2
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-plan.jsonl" <<EOF
{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-plan","cwd":"$PWD"}}
{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call","call_id":"session-plan-call","name":"update_plan","arguments":"{\"plan\":[{\"step\":\"Inspect stream\",\"status\":\"completed\"},{\"step\":\"Persist TODO\",\"status\":\"in_progress\"}]}"}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-plan", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	var planEvents []process.CodexEvent
	var allEvents []process.CodexEvent
	for event := range events {
		allEvents = append(allEvents, event)
		if event.Type == process.CodexEventPlan {
			planEvents = append(planEvents, event)
		}
	}
	if len(planEvents) != 1 {
		t.Fatalf("plan event count = %d, want 1: %#v", len(planEvents), planEvents)
	}
	update := eventContent[process.PlanUpdate](t, planEvents[0])
	if len(update.Items) != 2 || planEvents[0].EventID == "" {
		t.Fatalf("merged plan event = %#v", planEvents[0])
	}
	if len(allEvents) == 0 || allEvents[0].Type != process.CodexEventPlan {
		t.Fatalf("stdout plan was not emitted before delayed transcript: %#v", allEvents)
	}
}

func TestEventsMergesSessionAndStdoutPlanUpdatesWithoutDuplicates(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-plan-transcript-first"}'
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-plan-transcript-first.jsonl" <<EOF
{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-plan-transcript-first","cwd":"$PWD"}}
{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call","call_id":"session-plan-call","name":"update_plan","arguments":"{\"plan\":[{\"step\":\"Inspect stream\",\"status\":\"completed\"},{\"step\":\"Persist TODO\",\"status\":\"in_progress\"}]}"}}
EOF
sleep 0.2
printf '%s\n' '{"type":"item.updated","item":{"id":"stdout-plan-item","type":"todo_list","items":[{"text":"Inspect stream","status":"completed"},{"text":"Persist TODO","status":"in_progress"}]}}'
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-plan-transcript-first", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	var planEvents []process.CodexEvent
	for event := range events {
		if event.Type == process.CodexEventPlan {
			planEvents = append(planEvents, event)
		}
	}
	if len(planEvents) != 1 {
		t.Fatalf("plan event count = %d, want 1: %#v", len(planEvents), planEvents)
	}
	update := eventContent[process.PlanUpdate](t, planEvents[0])
	if len(update.Items) != 2 || planEvents[0].EventID == "" {
		t.Fatalf("merged plan event = %#v", planEvents[0])
	}
}

func TestEventsPreservesRepeatedPlanAfterIntermediateChange(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-plan-repeat"}'
printf '%s\n' '{"type":"item.updated","item":{"id":"plan-item","type":"todo_list","items":[{"text":"Inspect stream","status":"in_progress"}]}}'
printf '%s\n' '{"type":"item.updated","item":{"id":"plan-item","type":"todo_list","items":[{"text":"Inspect stream","status":"completed"}]}}'
printf '%s\n' '{"type":"item.updated","item":{"id":"plan-item","type":"todo_list","items":[{"text":"Inspect stream","status":"in_progress"}]}}'
sleep 0.2
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-plan-repeat.jsonl" <<EOF
{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-plan-repeat","cwd":"$PWD"}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-plan-repeat", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []process.PlanItemStatus
	for event := range events {
		if update, ok := event.Content.(process.PlanUpdate); ok {
			statuses = append(statuses, update.Items[0].Status)
		}
	}
	want := []process.PlanItemStatus{process.PlanItemInProgress, process.PlanItemCompleted, process.PlanItemInProgress}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("plan statuses = %#v, want %#v", statuses, want)
	}
}

func TestEventsDoesNotPairPlanAcrossInterveningDifferentSessionUpdate(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-plan-gap"}'
printf '%s\n' '{"type":"item.updated","item":{"id":"stdout-plan","type":"todo_list","items":[{"text":"Inspect stream","status":"in_progress"}]}}'
sleep 0.2
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-plan-gap.jsonl" <<EOF
{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-plan-gap","cwd":"$PWD"}}
{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call","call_id":"session-plan-b","name":"update_plan","arguments":"{\"plan\":[{\"step\":\"Inspect stream\",\"status\":\"completed\"}]}"}}
{"timestamp":"2026-07-08T09:00:02Z","type":"response_item","payload":{"type":"function_call","call_id":"session-plan-a","name":"update_plan","arguments":"{\"plan\":[{\"step\":\"Inspect stream\",\"status\":\"in_progress\"}]}"}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-plan-gap", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []process.PlanItemStatus
	for event := range events {
		if update, ok := event.Content.(process.PlanUpdate); ok {
			statuses = append(statuses, update.Items[0].Status)
		}
	}
	want := []process.PlanItemStatus{process.PlanItemInProgress, process.PlanItemCompleted, process.PlanItemInProgress}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("plan statuses = %#v, want %#v", statuses, want)
	}
}

func TestTailSessionLogDrainsStdoutPlanWhileTranscriptHasBacklog(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-backlog.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	log.WriteString(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-backlog","cwd":"` + dir + `"}}` + "\n")
	for index := 0; index < 200; index++ {
		log.WriteString(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"message","id":"msg-` + strconv.Itoa(index) + `","role":"assistant","content":[{"type":"output_text","text":"message"}]}}` + "\n")
	}
	if err := os.WriteFile(sessionFile, []byte(log.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, ok := stdoutPlanUpdate([]byte(`{"type":"item.updated","item":{"id":"plan-backlog","type":"todo_list","items":[{"text":"Realtime plan","status":"in_progress"}]}}`))
	if !ok {
		t.Fatal("stdout plan was not parsed")
	}
	stdoutPlans := make(chan process.CodexEvent, 1)
	stdoutPlans <- plan
	close(stdoutPlans)
	sessionIDs := make(chan string)
	close(sessionIDs)
	exited := make(chan process.ExitResult, 1)
	exited <- process.ExitResult{}
	events := make(chan process.CodexEvent, 256)
	active := &activeProcess{
		home:                   codexHome,
		workdir:                dir,
		codexSessionID:         "codex-session-backlog",
		transcriptPath:         sessionFile,
		transcriptRelativePath: "2026/07/08/rollout-backlog.jsonl",
	}
	if _, err := tailSessionLog(context.Background(), active, events, exited, sessionIDs, stdoutPlans); err != nil {
		t.Fatal(err)
	}
	planIndex := -1
	for index := 0; len(events) > 0; index++ {
		if event := <-events; event.Type == process.CodexEventPlan {
			planIndex = index
			break
		}
	}
	if planIndex < 0 || planIndex > 2 {
		t.Fatalf("realtime plan index = %d, want before transcript backlog", planIndex)
	}
}

func TestTailSessionLogAnnouncesTranscriptSourceOnce(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-source-once.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-source-once","cwd":"` + dir + `"}}
{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"first"}]}}
{"timestamp":"2026-07-08T09:00:02Z","type":"response_item","payload":{"type":"message","id":"msg-2","role":"assistant","content":[{"type":"output_text","text":"second"}]}}
`
	if err := os.WriteFile(sessionFile, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	exited := make(chan process.ExitResult, 1)
	exited <- process.ExitResult{}
	events := make(chan process.CodexEvent, 8)
	recorder := &observationRecorder{}
	active := &activeProcess{
		home:           codexHome,
		workdir:        dir,
		codexSessionID: "codex-session-source-once",
		transcriptPath: sessionFile,
		observer:       recorder,
	}
	if _, err := tailSessionLog(context.Background(), active, events, exited, nil, nil); err != nil {
		t.Fatal(err)
	}
	announcements := 0
	for len(events) > 0 {
		if event := <-events; event.Type == process.CodexEventTranscriptBound {
			announcements++
		}
	}
	if announcements != 1 {
		t.Fatalf("transcript source announcements = %d, want 1", announcements)
	}
	if len(recorder.items) != 1 || recorder.items[0].Name != "transcript.bind" || recorder.items[0].Outcome != "success" {
		t.Fatalf("bind observations = %#v", recorder.items)
	}
}

func TestEventsDeduplicatesMessageMirrorBeforeCanonicalMessage(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-mirror-first"}'
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-mirror-first.jsonl" <<EOF
{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-mirror-first","cwd":"$PWD"}}
{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"working"}}
EOF
sleep 0.02
cat >> "$CODEX_HOME/sessions/2026/07/08/rollout-mirror-first.jsonl" <<EOF
{"timestamp":"2026-07-08T09:00:01.022Z","type":"response_item","payload":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"working"}]}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-mirror-first", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 3)
	if got[0].Type != process.CodexEventTranscriptBound || !strings.Contains(got[1].EventID, "msg-1") || got[2].Type != process.CodexEventProcessExit {
		t.Fatalf("live mirror events = %#v", got)
	}
}

func TestEventsFlushesUnmatchedMessageMirrorWhileProcessIsRunning(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	exitMarker := filepath.Join(dir, "exiting")
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-mirror-running"}'
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-mirror-running.jsonl" <<EOF
{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-mirror-running","cwd":"$PWD"}}
{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"legacy output"}}
EOF
sleep 0.4
touch "$CODEX_EXIT_MARKER"
`)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("CODEX_EXIT_MARKER", exitMarker)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-mirror-running", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 2)
	if message, ok := got[1].Content.(process.CodexMessageContent); got[0].Type != process.CodexEventTranscriptBound || !ok || message.Text != "legacy output" {
		t.Fatalf("bounded mirror events = %#v", got)
	}
	if _, err := os.Stat(exitMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mirror was not flushed before process exit marker: %v", err)
	}
	if exit := collectEvents(t, events, 1)[0]; exit.Type != process.CodexEventProcessExit {
		t.Fatalf("final event = %#v", exit)
	}
}

func TestEventsFlushesUnmatchedMessageMirrorOnProcessExit(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-mirror-exit"}'
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-mirror-exit.jsonl" <<EOF
{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-mirror-exit","cwd":"$PWD"}}
{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"legacy output"}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-mirror-exit", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 3)
	if message, ok := got[1].Content.(process.CodexMessageContent); got[0].Type != process.CodexEventTranscriptBound || !ok || message.Text != "legacy output" || got[2].Type != process.CodexEventProcessExit {
		t.Fatalf("exit mirror events = %#v", got)
	}
}

func TestObserveStdoutDoesNotBlockWhenPlanBufferIsFull(t *testing.T) {
	var stdout strings.Builder
	for index := 0; index < 100; index++ {
		stdout.WriteString(`{"type":"item.updated","item":{"id":"plan","type":"todo_list","items":[{"text":"step-` + strconv.Itoa(index) + `","status":"in_progress"}]}}` + "\n")
	}
	_, plans := observeStdout(strings.NewReader(stdout.String()), false)
	var got []process.CodexEvent
	for event := range plans {
		got = append(got, event)
	}
	if len(got) == 0 || eventContent[process.PlanUpdate](t, got[len(got)-1]).Items[0].Step != "step-99" {
		t.Fatalf("buffered plans = %#v", got)
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

func TestEventsBindConcurrentSameWorkdirProcessesToStdoutThreadID(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
prompt=$(cat)
case "$prompt" in
  *first*) id=a ;;
  *second*) id=b ;;
  *) exit 2 ;;
esac
printf '{"type":"thread.started","thread_id":"codex-session-%s"}\n' "$id"
touch "$CODEX_HOME/$id.started"
while [ ! -f "$CODEX_HOME/a.started" ] || [ ! -f "$CODEX_HOME/b.started" ]; do
  sleep 0.01
done
if [ "$id" = a ]; then
  sleep 0.15
fi
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-concurrent-$id.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-$id","id":"codex-session-$id","cwd":"$PWD","originator":"codex_exec"}}
{"timestamp":"2026-07-08T09:16:03.939Z","type":"response_item","payload":{"type":"message","id":"msg-$id","role":"assistant","content":[{"type":"output_text","text":"message-$id"}]}}
EOF
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	first, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-concurrent-first", Workdir: dir, Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-concurrent-second", Workdir: dir, Prompt: "second"})
	if err != nil {
		t.Fatal(err)
	}
	firstEvents, err := client.Events(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	secondEvents, err := client.Events(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}

	firstGot := collectEvents(t, firstEvents, 2)
	secondGot := collectEvents(t, secondEvents, 2)
	firstSource := eventContent[process.CodexTranscriptSource](t, firstGot[0])
	firstMessage := eventContent[process.CodexMessageContent](t, firstGot[1])
	if firstSource.CodexSessionID != "codex-session-a" || firstMessage.Text != "message-a" {
		t.Fatalf("first process read wrong transcript = %#v", firstGot)
	}
	secondSource := eventContent[process.CodexTranscriptSource](t, secondGot[0])
	secondMessage := eventContent[process.CodexMessageContent](t, secondGot[1])
	if secondSource.CodexSessionID != "codex-session-b" || secondMessage.Text != "message-b" {
		t.Fatalf("second process read wrong transcript = %#v", secondGot)
	}
}

func TestEventsBuffersPartialSessionLogLine(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
mkdir -p "$CODEX_HOME/sessions/2026/07/08"
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-partial"}'
cat > "$CODEX_HOME/sessions/2026/07/08/rollout-partial.jsonl" <<EOF
{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-partial","id":"codex-session-partial","cwd":"$PWD","originator":"codex_exec"}}
EOF
printf '%s' '{"timestamp":"2026-07-08T09:16:03.939Z","type":"response_item","payload":{"type":"message","id":"msg-partial","role":"assistant","content":[{"type":"output_text","text":"part' >> "$CODEX_HOME/sessions/2026/07/08/rollout-partial.jsonl"
sleep 0.15
printf '%s' 'ial"}]}}' >> "$CODEX_HOME/sessions/2026/07/08/rollout-partial.jsonl"
`)
	t.Setenv("CODEX_HOME", codexHome)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-partial", Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	events, err := client.Events(context.Background(), handle)
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(t, events, 2)
	if got[0].Type != process.CodexEventTranscriptBound {
		t.Fatalf("first event = %#v", got[0])
	}
	message := eventContent[process.CodexMessageContent](t, got[1])
	if got[1].Type != process.CodexEventMessage || message.Text != "partial" {
		t.Fatalf("partial line event = %#v", got[1])
	}
}

func TestStartProcessOutlivesCallerContextCancellation(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-after-cancel"}'
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
	if got[1].Type != process.CodexEventMessage {
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
		`-c mcp_servers.anycode.tool_timeout_sec=86400`,
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
		`-c mcp_servers.anycode.tool_timeout_sec=86400`,
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

	_, err := New(bin, WithMCPStdio("/app/anycode", "/tmp/anycode-1000/mcp-2000.sock", "secret")).Start(context.Background(), process.CodexStartInput{
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
		`-c mcp_servers.anycode.args=["mcp-stdio","--session-id","session-1","--socket","/tmp/anycode-1000/mcp-2000.sock"]`,
		`-c mcp_servers.anycode.tool_timeout_sec=86400`,
		`-c features.network_proxy.enabled=true`,
		`-c default_permissions="anycode-mcp"`,
		`-c permissions.anycode-mcp.extends=":workspace"`,
		`-c permissions.anycode-mcp.network.enabled=true`,
		`-c permissions.anycode-mcp.network.mode="limited"`,
		`-c permissions.anycode-mcp.network.unix_sockets={"/tmp/anycode-1000/mcp-2000.sock"="allow"}`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if strings.Contains(args, "--sandbox workspace-write") {
		t.Fatalf("args should use default_permissions instead of --sandbox: %q", args)
	}
}

func TestResumeInjectsUnixSocketPermissionProfileForStdioMCP(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' "$*" > "$CODEX_ARGS_FILE"
`)
	t.Setenv("CODEX_ARGS_FILE", argsFile)
	t.Setenv("CODEX_HOME", codexHome)
	writeTranscript(t, codexHome, "codex-session-1", "2026/07/08/rollout-mcp-resume.jsonl", dir)

	_, err := New(bin, WithMCPStdio("/app/anycode", "/tmp/anycode-1000/mcp-2000.sock", "secret")).Resume(context.Background(), process.CodexResumeInput{
		ProcessRunID:   "process-run-mcp-profile-resume",
		SessionID:      "session-1",
		CodexSessionID: "codex-session-1",
		Transcript:     transcriptInput(t, codexHome, "codex-session-1").Source,
		Workdir:        dir,
		PermissionMode: "workspace-write",
		Prompt:         "continue with answer_user when needed",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, argsFile)
	args := strings.TrimSpace(readFile(t, argsFile))
	for _, want := range []string{
		`-c mcp_servers.anycode.args=["mcp-stdio","--session-id","session-1","--socket","/tmp/anycode-1000/mcp-2000.sock"]`,
		`-c mcp_servers.anycode.tool_timeout_sec=86400`,
		`-c features.network_proxy.enabled=true`,
		`-c default_permissions="anycode-mcp"`,
		`-c permissions.anycode-mcp.extends=":workspace"`,
		`-c permissions.anycode-mcp.network.enabled=true`,
		`-c permissions.anycode-mcp.network.mode="limited"`,
		`-c permissions.anycode-mcp.network.unix_sockets={"/tmp/anycode-1000/mcp-2000.sock"="allow"}`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
	if strings.Contains(args, "--sandbox workspace-write") {
		t.Fatalf("resume args should use default_permissions instead of --sandbox: %q", args)
	}
}

func TestEventsParsesNestedMessageTypeAndInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-invalid"}'
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
	if got[1].Type != process.CodexEventMessage {
		t.Fatalf("agent message event = %+v", got[1])
	}
	status := eventContent[process.CodexStatusContent](t, got[2])
	if got[2].Type != process.CodexEventStatus || status.Code != "invalid_json" || status.Details["byteCount"] != 8 {
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

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-reasoning"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got), got)
	}
	reasoning := eventContent[process.CodexReasoningContent](t, got[1])
	if got[1].Type != process.CodexEventReasoning || reasoning.Text != "Checked the session transcript" {
		t.Fatalf("reasoning event = %#v", got[1])
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

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-agent-messages"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("events len = %d, want 4: %#v", len(got), got)
	}
	userMessage := eventContent[process.CodexMessageContent](t, got[1])
	if userMessage.Role != "user" || userMessage.Text != "clone the repo\nuse the screenshot" || len(userMessage.Images) != 1 {
		t.Fatalf("user message = %#v", userMessage)
	}
	if userMessage.Images[0].Source != "data:image/png;base64,AAAA" || userMessage.Images[0].Detail != "high" {
		t.Fatalf("user message image = %#v", userMessage.Images[0])
	}
	if !strings.Contains(got[1].EventID, "msg-user") {
		t.Fatalf("user message event id = %q, want response_item id", got[1].EventID)
	}
	assistantMessage := eventContent[process.CodexMessageContent](t, got[2])
	if assistantMessage.Role != "assistant" || assistantMessage.Text != "working" {
		t.Fatalf("assistant message = %#v", assistantMessage)
	}
	status := eventContent[process.CodexStatusContent](t, got[3])
	if got[3].Type != process.CodexEventStatus || status.Code != "task.completed" {
		t.Fatalf("task completion event = %#v", got[3])
	}
}

func TestSessionEventsUsesResponseItemMessageWhenEventMessageMirrorArrivesFirst(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-agent-message-mirror-first.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-agent-message-mirror-first","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"event_msg","payload":{"type":"agent_message","message":"working"}}
{"timestamp":"2026-07-08T09:01:00.022Z","type":"response_item","payload":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"working"}]}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-agent-message-mirror-first"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got), got)
	}
	if !strings.Contains(got[1].EventID, "msg-1") {
		t.Fatalf("assistant message event id = %q, want response_item id", got[1].EventID)
	}
	if message, ok := got[1].Content.(process.CodexMessageContent); !ok || message.Role != "assistant" || message.Text != "working" {
		t.Fatalf("assistant message content = %#v", got[1].Content)
	}
}

func TestSessionEventsKeepsEventMessageAgentMessageWhenNoCanonicalMessageExists(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-event-agent-message.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-event-agent-message","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"event_msg","payload":{"type":"agent_message","message":"普通助手输出"}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-event-agent-message"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got), got)
	}
	message, ok := got[1].Content.(process.CodexMessageContent)
	if !ok || message.Role != "assistant" || message.Text != "普通助手输出" {
		t.Fatalf("event_msg agent message content = %#v", got[1].Content)
	}
}

func TestSessionEventsKeepsRepeatedCanonicalAssistantMessages(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-repeated-agent-messages.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-repeated-agent-messages","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:01:00Z","type":"response_item","payload":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}
{"timestamp":"2026-07-08T09:01:01Z","type":"response_item","payload":{"type":"message","id":"msg-2","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-repeated-agent-messages"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("events len = %d, want 3: %#v", len(got), got)
	}
	for index := 1; index <= 2; index++ {
		message, ok := got[index].Content.(process.CodexMessageContent)
		if !ok || message.Role != "assistant" || message.Text != "ok" {
			t.Fatalf("assistant message %d content = %#v", index, got[index].Content)
		}
		if !strings.Contains(got[index].EventID, "msg-"+strconv.Itoa(index)) {
			t.Fatalf("assistant message %d event id = %q", index, got[index].EventID)
		}
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

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-custom-output"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(got), got)
	}
	tool := eventContent[process.CodexToolContent](t, got[1])
	if got[1].Type != process.CodexEventTool || tool.Output.Text != "captured screenshot" || len(tool.Images) != 1 {
		t.Fatalf("custom tool output = %#v", tool)
	}
	if tool.Images[0].Source != "data:image/png;base64,AAAA" {
		t.Fatalf("custom tool image = %#v", tool.Images[0])
	}
}

func TestSessionEventsProjectsMessagesIntoCanonicalContent(t *testing.T) {
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

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-normalized-conflict"))
	if err != nil {
		t.Fatal(err)
	}
	message := eventContent[process.CodexMessageContent](t, got[1])
	if got[1].Type != process.CodexEventMessage || message.Role != "user" || message.Text != "actual user text" {
		t.Fatalf("canonical message = %#v", got[1])
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
{"timestamp":"2026-07-08T09:06:10Z","type":"response_item","payload":{"type":"web_search_call","id":"ws-call","status":"completed","action":{"type":"search","query":"AnyCode transcript"},"internal_chat_message_metadata_passthrough":{"turn_id":"turn-1"}}}
{"timestamp":"2026-07-08T09:06:11Z","type":"event_msg","payload":{"type":"web_search_end","call_id":"ws-call","query":"AnyCode transcript"}}
{"timestamp":"2026-07-08T09:07:00Z","type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"call-mcp","invocation":{"server":"playwright","tool":"browser_resize"},"result":{"Ok":{"isError":false}}}}
{"timestamp":"2026-07-08T09:08:00Z","type":"event_msg","payload":{"type":"turn_aborted","turn_id":"turn-1","reason":"interrupted"}}
{"timestamp":"2026-07-08T09:09:00Z","type":"event_msg","payload":{"type":"context_compacted","summary":"short"}}
{"timestamp":"2026-07-08T09:10:00Z","type":"compacted","payload":{"message":"summary"}}
{"timestamp":"2026-07-08T09:11:00Z","type":"turn_context","payload":{"turn_id":"turn-2","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:12:00Z","type":"world_state","payload":{"full":true,"state":{"agents_md":{"text":"huge"}}}}
{"timestamp":"2026-07-08T09:13:00Z","type":"agent_message","payload":{"message":"top-level assistant note","phase":"commentary"}}
{"timestamp":"2026-07-08T09:14:00Z","type":"response_item","payload":{"type":"agent_message","id":"sub-agent-message","author":"/root/review","content":[{"type":"input_text","text":"sub-agent assistant note"}]}}
{"timestamp":"2026-07-08T09:15:00Z","type":"inter_agent_communication_metadata","payload":{"trigger_turn":true}}
{"timestamp":"2026-07-08T09:16:00Z","type":"event_msg","payload":{"type":"sub_agent_activity","event_id":"call-sub","agent_path":"/root/review","kind":"started"}}
{"timestamp":"2026-07-08T09:17:00Z","type":"event_msg","payload":{"type":"thread_settings_applied","cwd":"/workspace/project","thread_settings":{"model":"codex-auto-review"}}}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-record-types"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Type != process.CodexEventStatus || eventContent[process.CodexStatusContent](t, got[0]).Code != "thread.started" {
		t.Fatalf("first event = %#v", got)
	}
	var usageCount, toolCount, statusCount, messageCount int
	messageTexts := map[string]bool{}
	for _, event := range got {
		switch content := event.Content.(type) {
		case process.CodexUsageContent:
			usageCount++
		case process.CodexToolContent:
			toolCount++
			if content.QualifiedName == "apply_patch" {
				t.Fatalf("internal apply_patch event was not filtered: %#v", event)
			}
		case process.CodexStatusContent:
			statusCount++
		case process.CodexMessageContent:
			messageCount++
			messageTexts[content.Text] = true
		}
		if event.Type == process.CodexEventUnknown {
			unknown := eventContent[process.CodexUnknownContent](t, event)
			switch unknown.RawType {
			case "agent_message", "inter_agent_communication_metadata", "sub_agent_activity", "thread_settings_applied":
				t.Fatalf("standard event fell back to unknown: %#v", event)
			}
		}
	}
	if usageCount != 1 || toolCount < 4 || statusCount < 5 || messageCount != 2 {
		t.Fatalf("canonical event category counts: usage=%d tool=%d status=%d message=%d events=%#v", usageCount, toolCount, statusCount, messageCount, got)
	}
	if !messageTexts["top-level assistant note"] || !messageTexts["sub-agent assistant note"] {
		t.Fatalf("assistant messages = %#v", messageTexts)
	}
}

func TestParseSessionLogLineFiltersEncryptedReasoning(t *testing.T) {
	encryptedOnly := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"reasoning","id":"reasoning-1","summary":[],"encrypted_content":"secret"}}`), "/workspace/project", "rollout.jsonl", 1)
	if len(encryptedOnly) != 0 {
		t.Fatalf("encrypted-only reasoning events = %#v, want none", encryptedOnly)
	}

	readable := parseSessionLogLine([]byte(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"reasoning","id":"reasoning-2","summary":[{"type":"summary_text","text":"visible summary"}],"encrypted_content":"secret"}}`), "/workspace/project", "rollout.jsonl", 2)
	if len(readable) != 1 {
		t.Fatalf("readable reasoning events = %#v, want one", readable)
	}
	item, ok := readable[0].Payload["item"].(map[string]any)
	if !ok {
		t.Fatalf("readable reasoning item = %#v", readable[0].Payload["item"])
	}
	if _, ok := item["encrypted_content"]; ok {
		t.Fatalf("readable reasoning item leaked encrypted_content: %#v", item)
	}
	if got := stringValue(eventNormalizedItem(t, readable[0]), "output"); got != "visible summary" {
		t.Fatalf("readable reasoning output = %q", got)
	}
}

func TestParseSessionLogLineBuildsTypedTimelineEvents(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantPhase   process.CodexPhase
		wantID      string
		assertValue func(*testing.T, process.CodexEventContent)
	}{
		{
			name:      "command started",
			raw:       `{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"function_call","call_id":"call-shell","name":"exec","arguments":"{\"cmd\":\"/bin/bash -lc 'npm test'\",\"workdir\":\"/workspace/web\"}"}}`,
			wantPhase: process.CodexPhaseStarted,
			wantID:    "call-shell",
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				command, ok := content.(process.CodexCommandContent)
				if !ok || len(command.Commands) != 1 || command.Commands[0].Command != "npm test" || command.Commands[0].Workdir != "/workspace/web" {
					t.Fatalf("command content = %#v", content)
				}
			},
		},
		{
			name:      "tool completed",
			raw:       `{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-shell","output":"ok"}}`,
			wantPhase: process.CodexPhaseCompleted,
			wantID:    "call-shell",
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				tool, ok := content.(process.CodexToolContent)
				if !ok || tool.Output.Text != "ok" {
					t.Fatalf("tool result content = %#v", content)
				}
			},
		},
		{
			name:      "uncorrelated result with command metadata remains tool",
			raw:       `{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call-shell","output":"failed","exit_code":7,"duration_ms":125}}`,
			wantPhase: process.CodexPhaseCompleted,
			wantID:    "call-shell",
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				tool, ok := content.(process.CodexToolContent)
				if !ok || tool.Output.Text != "failed" {
					t.Fatalf("tool result content = %#v", content)
				}
			},
		},
		{
			name:      "web search cancelled",
			raw:       `{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"web_search_call","id":"search-1","status":"cancelled","action":{"type":"search","query":"AnyCode"}}}`,
			wantPhase: process.CodexPhaseCancelled,
			wantID:    "search-1",
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				if _, ok := content.(process.CodexToolContent); !ok {
					t.Fatalf("web search content = %#v", content)
				}
			},
		},
		{
			name:      "mcp tool failed",
			raw:       `{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"call-mcp","invocation":{"server":"browser","tool":"open"},"result":{"Ok":{"isError":true}}}}`,
			wantPhase: process.CodexPhaseFailed,
			wantID:    "call-mcp",
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				if _, ok := content.(process.CodexToolContent); !ok {
					t.Fatalf("mcp content = %#v", content)
				}
			},
		},
		{
			name:      "mcp tool inline audio",
			raw:       `{"timestamp":"2026-07-08T09:00:01Z","type":"event_msg","payload":{"type":"mcp_tool_call_end","call_id":"call-audio","invocation":{"server":"media","tool":"render"},"result":{"Ok":{"isError":false,"content":[{"type":"audio","data":"YXVkaW8=","mimeType":"audio/mpeg"}]}}}}`,
			wantPhase: process.CodexPhaseCompleted,
			wantID:    "call-audio",
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				tool, ok := content.(process.CodexToolContent)
				if !ok || len(tool.Images) != 1 || tool.Images[0].SourceKind != "inline_base64" || tool.Images[0].MimeType != "audio/mpeg" {
					t.Fatalf("mcp audio content = %#v", content)
				}
			},
		},
		{
			name:      "assistant message",
			raw:       `{"timestamp":"2026-07-08T09:00:02Z","type":"response_item","payload":{"type":"message","id":"message-1","role":"assistant","content":[{"type":"output_text","text":"**done**"}]}}`,
			wantPhase: process.CodexPhaseStandalone,
			wantID:    "message-1",
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				message, ok := content.(process.CodexMessageContent)
				if !ok || message.Role != "assistant" || message.Format != process.CodexTextMarkdown || message.Text != "**done**" {
					t.Fatalf("message content = %#v", content)
				}
			},
		},
		{
			name:      "file change",
			raw:       `{"timestamp":"2026-07-08T09:00:03Z","type":"event_msg","payload":{"type":"patch_apply_end","call_id":"call-patch","status":"completed","changes":{"/workspace/project/a.txt":{"type":"create","unified_diff":"@@ -0,0 +1 @@\\n+hello"}}}}`,
			wantPhase: process.CodexPhaseStandalone,
			wantID:    "call-patch",
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				change, ok := content.(process.CodexFileChangeContent)
				if !ok || len(change.Changes) != 1 || change.Changes[0].Kind != "added" || change.Changes[0].Path != "a.txt" {
					t.Fatalf("file change content = %#v", content)
				}
			},
		},
		{
			name:      "usage",
			raw:       `{"timestamp":"2026-07-08T09:00:04Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"cached_input_tokens":6,"output_tokens":5,"total_tokens":42},"last_token_usage":{"input_tokens":7,"cached_input_tokens":4,"output_tokens":3,"reasoning_output_tokens":2,"total_tokens":10},"model_context_window":200000}}}`,
			wantPhase: process.CodexPhaseStandalone,
			assertValue: func(t *testing.T, content process.CodexEventContent) {
				usage, ok := content.(process.CodexUsageContent)
				if !ok || usage.InputTokens != 10 || usage.CachedInputTokens != 6 || usage.TotalTokens != 42 || usage.ContextWindow != 200000 || usage.CurrentInputTokens != 7 || usage.CurrentCachedInputTokens != 4 || usage.CurrentOutputTokens != 3 || usage.CurrentReasoningOutputTokens != 2 || usage.CurrentTotalTokens != 10 {
					t.Fatalf("usage content = %#v", content)
				}
			},
		},
	}

	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := parseSessionLogLine([]byte(test.raw), "/workspace/project", "rollout.jsonl", int64(index+100))
			if len(events) != 1 {
				t.Fatalf("events = %#v", events)
			}
			event := events[0]
			if event.Phase != test.wantPhase || event.CorrelationID != test.wantID {
				t.Fatalf("event phase/id = %q/%q, want %q/%q", event.Phase, event.CorrelationID, test.wantPhase, test.wantID)
			}
			if event.SourceOffset != int64(index+100) || event.SourceIndex != 0 {
				t.Fatalf("event source order = %d/%d", event.SourceOffset, event.SourceIndex)
			}
			test.assertValue(t, event.Content)
		})
	}
}

func TestParseCustomExecCommandCalls(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantCommands []any
		wantCommand  bool
	}{
		{
			name:        "single command with workdir",
			input:       `const result = await tools.exec_command({"cmd":"go test ./...","workdir":"/workspace/project"});`,
			wantCommand: true,
			wantCommands: []any{
				map[string]any{"command": "go test ./...", "workdir": "/workspace/project"},
			},
		},
		{
			name: "promise all preserves source order and unquoted fields",
			input: `const [first, second] = await Promise.all([
  tools.exec_command({ cmd: "npm test", workdir: "/workspace/web" }),
  tools.exec_command({cmd: "go test ./...", max_output_tokens: 12000}),
]);`,
			wantCommand: true,
			wantCommands: []any{
				map[string]any{"command": "npm test", "workdir": "/workspace/web"},
				map[string]any{"command": "go test ./...", "workdir": ""},
			},
		},
		{
			name:        "escaped JSON string",
			input:       `const result = await tools.exec_command({"cmd":"printf \"a\\nb\""});`,
			wantCommand: true,
			wantCommands: []any{
				map[string]any{"command": "printf \"a\\nb\"", "workdir": ""},
			},
		},
		{
			name:        "regex in ignored property preserves object boundaries",
			input:       `const result = tools.exec_command({cmd: "npm test", filter: /a,b/});`,
			wantCommand: true,
			wantCommands: []any{
				map[string]any{"command": "npm test", "workdir": ""},
			},
		},
		{
			name:        "call text inside ignored property regex is ignored",
			input:       `const result = tools.exec_command({cmd: "npm test", filter: /tools.exec_command({"cmd":"false"})/});`,
			wantCommand: true,
			wantCommands: []any{
				map[string]any{"command": "npm test", "workdir": ""},
			},
		},
		{
			name:        "call text inside string is ignored",
			input:       `const example = "tools.exec_command({\\\"cmd\\\":\\\"false\\\"})";`,
			wantCommand: false,
		},
		{
			name:        "call text inside comment is ignored",
			input:       `// tools.exec_command({"cmd":"false"})`,
			wantCommand: false,
		},
		{
			name:        "call text inside regex is ignored",
			input:       `const pattern = /tools.exec_command({"cmd":"false"})/;`,
			wantCommand: false,
		},
		{
			name:        "member tools call is ignored across comment trivia",
			input:       `const result = runner./* adapter */tools.exec_command({"cmd":"false"});`,
			wantCommand: false,
		},
		{
			name:        "member tools call after numeric literal is ignored",
			input:       `const result = 1..tools.exec_command({"cmd":"false"});`,
			wantCommand: false,
		},
		{
			name:        "dynamic command falls back",
			input:       `const result = await tools.exec_command({cmd: command});`,
			wantCommand: false,
		},
		{
			name:        "dynamic workdir falls back",
			input:       `const result = await tools.exec_command({cmd: "npm test", workdir});`,
			wantCommand: false,
		},
		{
			name:        "truncated call falls back",
			input:       `const result = await tools.exec_command({"cmd":"go test ./..."`,
			wantCommand: false,
		},
		{
			name: "one invalid call makes the outer exec fall back",
			input: `const results = await Promise.all([
  tools.exec_command({"cmd":"npm test"}),
  tools.exec_command({cmd: command}),
]);`,
			wantCommand: false,
		},
		{
			name: "nested static calls are all extracted",
			input: `const result = tools.exec_command({
  cmd: "outer",
  metadata: { nested: tools.exec_command({cmd: "inner"}) },
});`,
			wantCommand: true,
			wantCommands: []any{
				map[string]any{"command": "outer", "workdir": ""},
				map[string]any{"command": "inner", "workdir": ""},
			},
		},
		{
			name: "dynamic nested call makes the outer exec fall back",
			input: `const result = tools.exec_command({
  cmd: "outer",
  metadata: { nested: tools.exec_command({cmd: command}) },
});`,
			wantCommand: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := `{"timestamp":"2026-07-08T09:00:00Z","type":"response_item","payload":{"type":"custom_tool_call","call_id":"call-exec","name":"exec","input":` + strconv.Quote(test.input) + `}}`
			events := parseSessionLogLine([]byte(raw), "/workspace/project", "rollout.jsonl", 0)
			if len(events) != 1 {
				t.Fatalf("events = %d, want 1", len(events))
			}
			_, isCommand := events[0].Content.(process.CodexCommandContent)
			if isCommand != test.wantCommand {
				t.Fatalf("content = %#v, want command = %t", events[0].Content, test.wantCommand)
			}
			normalized := eventNormalizedItem(t, events[0])
			if test.wantCommand {
				if normalized["type"] != "command_execution" || !reflect.DeepEqual(normalized["commands"], test.wantCommands) {
					t.Fatalf("normalized item = %#v, want commands %#v", normalized, test.wantCommands)
				}
				return
			}
			if normalized["type"] != "custom_tool_call" || normalized["input"] != test.input {
				t.Fatalf("fallback normalized item = %#v", normalized)
			}
		})
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

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-unknown-records"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("events len = %d, want 4: %#v", len(got), got)
	}
	wantTypes := []process.CodexEventType{process.CodexEventStatus, process.CodexEventUnknown, process.CodexEventUnknown, process.CodexEventUnknown}
	for index, wantType := range wantTypes {
		if got[index].Type != wantType {
			t.Fatalf("event %d type = %q, want %q", index, got[index].Type, wantType)
		}
	}
	for index, want := range []struct {
		rawType string
		value   string
	}{{"future_record", "top-level"}, {"future_item", "response-item"}, {"future_event", "event-message"}} {
		unknown := eventContent[process.CodexUnknownContent](t, got[index+1])
		if unknown.RawType != want.rawType || unknown.Payload["value"] != want.value {
			t.Fatalf("event %d content = %#v, want %#v", index+1, unknown, want)
		}
	}
}

func TestEventsEmitsProcessExitWithFailureCode(t *testing.T) {
	dir := t.TempDir()
	codexHome := t.TempDir()
	bin := fakeCodex(t, `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"codex-session-exit"}'
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
	if got[2].Type != process.CodexEventProcessExit {
		t.Fatalf("exit event type = %q", got[2].Type)
	}
	exit := eventContent[process.ExitResult](t, got[2])
	if exit.ExitCode == nil || *exit.ExitCode != 7 || exit.FailureReason == "" {
		t.Fatalf("exit content = %#v", exit)
	}
	if !strings.Contains(exit.FailureReason, "model gpt-test is not supported") {
		t.Fatalf("exit content missing stderr: %#v", exit)
	}
}

func TestEventsRejectsAmbiguousSessionLogsWithoutStdoutThreadID(t *testing.T) {
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
	if got[0].Type != process.CodexEventProcessExit {
		t.Fatalf("ambiguous transcript event = %+v", got[0])
	}
	exit := eventContent[process.ExitResult](t, got[0])
	if !strings.Contains(exit.FailureReason, "codex transcript is unavailable") {
		t.Fatalf("ambiguous transcript failure = %+v", got[0])
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

	got, err := New("codex", WithCodexHome(codexHome)).SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-long"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("events length = %d, want 2", len(got))
	}
	tool := eventContent[process.CodexToolContent](t, got[1])
	if tool.Output.Text != longOutput {
		t.Fatalf("long output = %#v", tool.Output)
	}
}

func TestSessionEventPageUsesLineHeadByteCursor(t *testing.T) {
	codexHome := t.TempDir()
	relativePath := "2026/07/08/rollout-page.jsonl"
	sessionFile := filepath.Join(codexHome, "sessions", filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"timestamp":"2026-07-08T09:16:01Z","type":"session_meta","payload":{"session_id":"codex-session-page","cwd":"/workspace/page"}}
{"timestamp":"2026-07-08T09:16:02Z","type":"agent_message","payload":{"message":"one"}}
{"timestamp":"2026-07-08T09:16:03Z","type":"agent_message","payload":{"message":"two"}}
{"timestamp":"2026-07-08T09:16:04Z","type":"agent_message","payload":{"message":"three"}}
{"timestamp":"2026-07-08T09:16:05Z","type":"agent_message","payload":{"message":"four"}}
{"timestamp":"2026-07-08T09:16:06Z","type":"agent_message","payload":{"message":"five"}}
`
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	client := New("codex", WithCodexHome(codexHome))
	source := process.CodexTranscriptSource{CodexSessionID: "codex-session-page", RelativePath: relativePath}
	twoOffset := int64(strings.Index(content, `{"timestamp":"2026-07-08T09:16:03Z"`))
	threeOffset := int64(strings.Index(content, `{"timestamp":"2026-07-08T09:16:04Z"`))
	fourOffset := int64(strings.Index(content, `{"timestamp":"2026-07-08T09:16:05Z"`))
	fiveOffset := int64(strings.Index(content, `{"timestamp":"2026-07-08T09:16:06Z"`))

	latest, err := client.SessionEventPage(context.Background(), process.CodexTranscriptPageInput{Source: source, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if latest.StartOffset != fourOffset || latest.EndOffset != int64(len(content)) || !latest.HasMore || len(latest.Events) != 2 {
		t.Fatalf("latest page = %#v", latest)
	}
	if latest.Events[0].SourceOffset != fourOffset || latest.Events[1].SourceOffset != fiveOffset {
		t.Fatalf("latest source offsets = %d, %d", latest.Events[0].SourceOffset, latest.Events[1].SourceOffset)
	}
	if got := eventContent[process.CodexMessageContent](t, latest.Events[0]).Text; got != "four" {
		t.Fatalf("latest first message = %q", got)
	}

	older, err := client.SessionEventPage(context.Background(), process.CodexTranscriptPageInput{
		Source: source, BeforeOffset: latest.StartOffset, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if older.StartOffset != twoOffset || older.EndOffset != fourOffset || !older.HasMore || len(older.Events) != 2 {
		t.Fatalf("older page = %#v", older)
	}
	if got := eventContent[process.CodexMessageContent](t, older.Events[0]).Text; got != "two" {
		t.Fatalf("older first message = %q", got)
	}

	aligned, err := client.SessionEventPage(context.Background(), process.CodexTranscriptPageInput{
		Source: source, BeforeOffset: threeOffset + 12, Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if aligned.EndOffset != threeOffset || len(aligned.Events) != 2 {
		t.Fatalf("aligned page = %#v", aligned)
	}
	if got := eventContent[process.CodexMessageContent](t, aligned.Events[0]).Text; got != "one" {
		t.Fatalf("aligned first message = %q", got)
	}
}

func TestSessionEventPageReadsCompleteFinalJSONAndSkipsPartialLine(t *testing.T) {
	codexHome := t.TempDir()
	relativePath := "2026/07/08/rollout-final-line.jsonl"
	sessionFile := filepath.Join(codexHome, "sessions", filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	message := strings.Repeat("complete", 12*1024)
	complete := `{"timestamp":"2026-07-08T09:16:01Z","type":"session_meta","payload":{"session_id":"codex-session-final","cwd":"/workspace/final"}}
{"timestamp":"2026-07-08T09:16:02Z","type":"agent_message","payload":{"message":` + strconv.Quote(message) + `}}`
	if err := os.WriteFile(sessionFile, []byte(complete), 0o644); err != nil {
		t.Fatal(err)
	}
	client := New("codex", WithCodexHome(codexHome))
	source := process.CodexTranscriptSource{CodexSessionID: "codex-session-final", RelativePath: relativePath}
	page, err := client.SessionEventPage(context.Background(), process.CodexTranscriptPageInput{Source: source, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || eventContent[process.CodexMessageContent](t, page.Events[0]).Text != message {
		t.Fatalf("complete final line page = %#v", page)
	}
	if err := os.WriteFile(sessionFile, []byte(complete+"\n{\"timestamp\":"), 0o644); err != nil {
		t.Fatal(err)
	}
	page, err = client.SessionEventPage(context.Background(), process.CodexTranscriptPageInput{Source: source, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || eventContent[process.CodexMessageContent](t, page.Events[0]).Text != message {
		t.Fatalf("partial final line page = %#v", page)
	}
}

func TestSessionEventPageReadsBoundedTailWindow(t *testing.T) {
	codexHome := t.TempDir()
	relativePath := "2026/07/08/rollout-large-page.jsonl"
	sessionFile := filepath.Join(codexHome, "sessions", filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"timestamp":"2026-07-08T09:16:01Z","type":"session_meta","payload":{"session_id":"codex-session-large","cwd":"/workspace/large","large_after_cwd":"` + strings.Repeat("x", 2*1024*1024) + `"}}` + "\n"
	line := `{"timestamp":"2026-07-08T09:16:02Z","type":"agent_message","payload":{"message":"page"}}` + "\n"
	content := meta + strings.Repeat(line, 10_000)
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recorder := &observationRecorder{}
	client := New("codex", WithCodexHome(codexHome), WithObserver(recorder))
	page, err := client.SessionEventPage(context.Background(), process.CodexTranscriptPageInput{
		Source: process.CodexTranscriptSource{CodexSessionID: "codex-session-large", RelativePath: relativePath},
		Limit:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || len(recorder.items) != 1 {
		t.Fatalf("page=%#v observations=%#v", page, recorder.items)
	}
	read := recorder.items[0]
	if read.Name != "transcript.read_page" || read.Bytes >= 256*1024 {
		t.Fatalf("tail read observation = %#v, file bytes = %d", read, len(content))
	}
}

func TestSessionEventsRejectsMissingAndEscapingTranscriptSources(t *testing.T) {
	codexHome := t.TempDir()
	client := New("codex", WithCodexHome(codexHome))
	for _, source := range []process.CodexTranscriptSource{
		{},
		{CodexSessionID: "codex-session-1", RelativePath: "../escape.jsonl"},
		{CodexSessionID: "codex-session-1", RelativePath: "/absolute.jsonl"},
	} {
		if _, err := client.SessionEvents(context.Background(), process.CodexTranscriptInput{Source: source}); !errors.Is(err, process.ErrTranscriptUnavailable) {
			t.Fatalf("SessionEvents(%#v) error = %v", source, err)
		}
	}
	missing := process.CodexTranscriptSource{CodexSessionID: "codex-session-1", RelativePath: "2026/07/08/missing.jsonl"}
	if _, err := client.SessionEvents(context.Background(), process.CodexTranscriptInput{Source: missing}); err == nil || strings.Contains(err.Error(), codexHome) {
		t.Fatalf("missing transcript error = %v", err)
	}
}

func TestTranscriptObserverRecordsOnlyAllowlistedReadMetadata(t *testing.T) {
	codexHome := t.TempDir()
	writeTranscript(t, codexHome, "codex-session-1", "2026/07/08/observed.jsonl", "/workspace/project")
	recorder := &observationRecorder{}
	client := New("codex", WithCodexHome(codexHome), WithObserver(recorder))
	if _, err := client.SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-1")); err != nil {
		t.Fatal(err)
	}
	if len(recorder.items) != 1 || recorder.items[0].Name != "transcript.read" || recorder.items[0].Outcome != "success" || recorder.items[0].Bytes <= 0 || recorder.items[0].Duration <= 0 {
		t.Fatalf("observations = %#v", recorder.items)
	}
	if strings.Contains(fmt.Sprintf("%#v", recorder.items), "codex-session-1") || strings.Contains(fmt.Sprintf("%#v", recorder.items), codexHome) {
		t.Fatalf("observation leaked locator data: %#v", recorder.items)
	}
}

func TestTranscriptObserverRecordsFailureReasonsWithoutContent(t *testing.T) {
	codexHome := t.TempDir()
	writeTranscript(t, codexHome, "codex-session-1", "2026/07/08/observed.jsonl", "/workspace/project")
	recorder := &observationRecorder{}
	client := New("codex", WithCodexHome(codexHome), WithObserver(recorder))

	if _, err := client.SessionEvents(context.Background(), process.CodexTranscriptInput{}); !errors.Is(err, process.ErrTranscriptUnavailable) {
		t.Fatalf("missing source error = %v", err)
	}
	deadlineCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := client.SessionEvents(deadlineCtx, transcriptInput(t, codexHome, "codex-session-1")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}

	if len(recorder.items) != 2 || recorder.items[0].Reason != "unavailable" || recorder.items[1].Reason != "deadline" {
		t.Fatalf("observations = %#v", recorder.items)
	}
	encoded := fmt.Sprintf("%#v", recorder.items)
	if strings.Contains(encoded, "codex-session-1") || strings.Contains(encoded, codexHome) || strings.Contains(encoded, "/workspace/project") {
		t.Fatalf("observations leaked transcript data: %s", encoded)
	}
}

func BenchmarkSessionEvents4MB(b *testing.B) {
	codexHome := b.TempDir()
	relativePath := "2026/07/08/rollout-benchmark.jsonl"
	path := filepath.Join(codexHome, "sessions", filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	var log strings.Builder
	log.WriteString(`{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":"codex-session-benchmark","cwd":"/workspace/project"}}` + "\n")
	message := strings.Repeat("x", 2800)
	for index := 0; index < 1500; index++ {
		log.WriteString(`{"timestamp":"2026-07-08T09:00:01Z","type":"response_item","payload":{"type":"message","id":"msg-`)
		log.WriteString(strconv.Itoa(index))
		log.WriteString(`","role":"assistant","content":[{"type":"output_text","text":`)
		log.WriteString(strconv.Quote(message))
		log.WriteString(`}]}}` + "\n")
	}
	if err := os.WriteFile(path, []byte(log.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	client := New("codex", WithCodexHome(codexHome))
	input := process.CodexTranscriptInput{Source: process.CodexTranscriptSource{
		CodexSessionID: "codex-session-benchmark",
		RelativePath:   relativePath,
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := client.SessionEvents(context.Background(), input); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSessionEventsIgnoresIncompleteFinalLineUntilNextSnapshot(t *testing.T) {
	codexHome := t.TempDir()
	sessionFile := filepath.Join(codexHome, "sessions", "2026", "07", "08", "rollout-snapshot-partial.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"timestamp":"2026-07-08T09:16:02.939Z","type":"session_meta","payload":{"session_id":"codex-session-snapshot-partial","cwd":"/workspace/project"}}
{"timestamp":"2026-07-08T09:16:03.939Z","type":"response_item","payload":{"type":"message","id":"msg-partial","role":"assistant","content":[{"type":"output_text","text":"part`
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	client := New("codex", WithCodexHome(codexHome))

	first, err := client.SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-snapshot-partial"))
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Type != process.CodexEventStatus {
		t.Fatalf("partial snapshot events = %#v", first)
	}

	file, err := os.OpenFile(sessionFile, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`ial"}]}}
`); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := client.SessionEvents(context.Background(), transcriptInput(t, codexHome, "codex-session-snapshot-partial"))
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[1].Type != process.CodexEventMessage || eventContent[process.CodexMessageContent](t, second[1]).Text != "partial" {
		t.Fatalf("completed snapshot events = %#v", second)
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
printf '%s\n' "$child" > "$CODEX_CHILD_PID_FILE.tmp"
mv "$CODEX_CHILD_PID_FILE.tmp" "$CODEX_CHILD_PID_FILE"
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
	waitForProcessExit(t, handle.PID)
	childPID, err := strconv.Atoi(strings.TrimSpace(readFile(t, childPIDFile)))
	if err != nil {
		t.Fatal(err)
	}
	waitForProcessExit(t, childPID)
	if err := client.Stop(context.Background(), "missing"); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("missing stop error = %v", err)
	}
}

func TestStartInjectsProcessRunOwnerToken(t *testing.T) {
	dir := t.TempDir()
	ownerFile := filepath.Join(dir, "owner")
	bin := fakeCodex(t, `#!/bin/sh
printf '%s' "$ANYCODE_PROCESS_RUN_ID" > "$CODEX_OWNER_FILE"
sleep 30
`)
	t.Setenv("CODEX_OWNER_FILE", ownerFile)

	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-owner-1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Stop(context.Background(), handle.ProcessRunID) })
	waitForFile(t, ownerFile)
	if got := strings.TrimSpace(readFile(t, ownerFile)); got != "process-owner-1" {
		t.Fatalf("owner token = %q", got)
	}
}

func TestStopDetachedRequiresMatchingOwnerToken(t *testing.T) {
	client := New("codex")
	signals := []syscall.Signal{}
	client.detached = detachedProcessOps{
		groupAlive: func(int) (bool, error) { return true, nil },
		groupOwnedBy: func(_ int, runID process.RunID) (bool, error) {
			return runID == "process-detached-1", nil
		},
		signalGroup: func(_ int, signal syscall.Signal) error {
			signals = append(signals, signal)
			return nil
		},
		waitExit: func(context.Context, int, time.Duration) (bool, error) { return true, nil },
	}

	err := client.StopDetached(context.Background(), process.DetachedProcess{
		ProcessRunID: "another-run",
		PID:          1234,
	})
	if !errors.Is(err, process.ErrProcessOwnershipUnverified) {
		t.Fatalf("mismatched StopDetached() error = %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("mismatched process signals = %#v", signals)
	}

	if err := client.StopDetached(context.Background(), process.DetachedProcess{
		ProcessRunID: "process-detached-1",
		PID:          1234,
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(signals, []syscall.Signal{syscall.SIGTERM}) {
		t.Fatalf("matching process signals = %#v", signals)
	}
}

func TestStopDetachedEscalatesToKillAndTreatsExitedGroupAsSuccess(t *testing.T) {
	client := New("codex")
	signals := []syscall.Signal{}
	waits := 0
	client.detached = detachedProcessOps{
		groupAlive:   func(int) (bool, error) { return true, nil },
		groupOwnedBy: func(int, process.RunID) (bool, error) { return true, nil },
		signalGroup: func(_ int, signal syscall.Signal) error {
			signals = append(signals, signal)
			return nil
		},
		waitExit: func(context.Context, int, time.Duration) (bool, error) {
			waits++
			return waits == 2, nil
		},
	}
	if err := client.StopDetached(context.Background(), process.DetachedProcess{ProcessRunID: "process-1", PID: 1234}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(signals, []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}) {
		t.Fatalf("signals = %#v", signals)
	}

	client.detached.groupAlive = func(int) (bool, error) { return false, nil }
	signals = nil
	if err := client.StopDetached(context.Background(), process.DetachedProcess{ProcessRunID: "process-1", PID: 1234}); err != nil {
		t.Fatal(err)
	}
	if len(signals) != 0 {
		t.Fatalf("exited group signals = %#v", signals)
	}
}

func TestStopDetachedTreatsExitDuringOwnershipCheckAsSuccess(t *testing.T) {
	client := New("codex")
	aliveChecks := 0
	client.detached = detachedProcessOps{
		groupAlive: func(int) (bool, error) {
			aliveChecks++
			return aliveChecks == 1, nil
		},
		groupOwnedBy: func(int, process.RunID) (bool, error) { return false, nil },
		signalGroup: func(int, syscall.Signal) error {
			t.Fatal("exited process group must not be signalled")
			return nil
		},
		waitExit: func(context.Context, int, time.Duration) (bool, error) {
			t.Fatal("exited process group must not be waited")
			return false, nil
		},
	}

	if err := client.StopDetached(context.Background(), process.DetachedProcess{ProcessRunID: "process-1", PID: 1234}); err != nil {
		t.Fatal(err)
	}
	if aliveChecks != 2 {
		t.Fatalf("alive checks = %d, want 2", aliveChecks)
	}
}

func TestExitedProcessIsReapedWithoutStartingEventConsumer(t *testing.T) {
	bin := fakeCodex(t, `#!/bin/sh
exit 0
`)
	client := New(bin)
	handle, err := client.Start(context.Background(), process.CodexStartInput{ProcessRunID: "process-run-reaped"})
	if err != nil {
		t.Fatal(err)
	}

	waitForProcessExit(t, handle.PID)
	if err := client.Stop(context.Background(), handle.ProcessRunID); err != nil {
		t.Fatal(err)
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

func transcriptInput(t *testing.T, codexHome, codexSessionID string) process.CodexTranscriptInput {
	t.Helper()
	path, err := sessionLogByID(codexHome, codexSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatalf("transcript for %q was not found", codexSessionID)
	}
	source, err := transcriptSourceForPath(codexHome, codexSessionID, path)
	if err != nil {
		t.Fatal(err)
	}
	return process.CodexTranscriptInput{Source: source}
}

func writeTranscript(t *testing.T, codexHome, codexSessionID, relativePath, workdir string) string {
	t.Helper()
	path := filepath.Join(codexHome, "sessions", filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"timestamp":"2026-07-08T09:00:00Z","type":"session_meta","payload":{"session_id":` + strconv.Quote(codexSessionID) + `,"cwd":` + strconv.Quote(workdir) + `}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func eventNormalizedItem(t *testing.T, event codexLogEvent) map[string]any {
	t.Helper()
	item, ok := event.Payload["normalizedItem"].(map[string]any)
	if !ok {
		t.Fatalf("normalized item = %#v", event.Payload["normalizedItem"])
	}
	return item
}

func eventContent[T process.CodexEventContent](t *testing.T, event process.CodexEvent) T {
	t.Helper()
	content, ok := event.Content.(T)
	if !ok {
		var zero T
		t.Fatalf("event content = %T, want %T: %#v", event.Content, zero, event)
	}
	return content
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

type observationRecorder struct {
	items []Observation
}

func (r *observationRecorder) Observe(observation Observation) {
	r.items = append(r.items, observation)
}
