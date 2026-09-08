package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

func TestStartSideForksEphemeralReadOnlyThreadWithoutSavingSession(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = sideTestSession()
	codex := &fakeCodexProcess{forkHandle: processdomain.CodexHandle{CodexSessionID: "side-thread", TurnID: "turn-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex))
	service.generateID = func() (domain.ID, error) { return "side-run-1", nil }

	run, err := service.StartSide(context.Background(), SideInput{SessionID: "session-1", Prompt: "  inspect the parser  "})
	if err != nil {
		t.Fatal(err)
	}
	if run.CodexSessionID != "side-thread" || run.ProcessRunID != "side-run-1" || run.TurnID != "turn-1" {
		t.Fatalf("side run = %#v", run)
	}
	input := codex.forkInput
	if !codex.forkCalled || !input.Ephemeral || input.SourceCodexSessionID != "source-thread" {
		t.Fatalf("side fork = %#v", input)
	}
	if input.PermissionMode != "read-only" || input.Workdir != "/workspace/session-1" || len(input.Input) != 1 || input.Input[0].Text != "inspect the parser" {
		t.Fatalf("side start input = %#v", input.CodexStartInput)
	}
	if !strings.Contains(input.DeveloperInstructions, "temporary Side question") || len(repo.saved) != 0 {
		t.Fatalf("side persistence/instructions = saved:%d instructions:%q", len(repo.saved), input.DeveloperInstructions)
	}
}

func TestContinueSideUsesLoadedEphemeralThread(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = sideTestSession()
	codex := &fakeCodexProcess{loadedHandle: processdomain.CodexHandle{TurnID: "turn-2"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex))
	service.generateID = func() (domain.ID, error) { return "side-run-2", nil }

	run, err := service.ContinueSide(context.Background(), SideInput{
		SessionID: "session-1", CodexSessionID: "side-thread", Prompt: "follow up",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !codex.loadedCalled || codex.resumeCalled || codex.loadedInput.CodexSessionID != "side-thread" || codex.loadedInput.PermissionMode != "read-only" {
		t.Fatalf("loaded continuation = %#v", codex.loadedInput)
	}
	if run.CodexSessionID != "side-thread" || run.ProcessRunID != "side-run-2" || run.TurnID != "turn-2" {
		t.Fatalf("continued side = %#v", run)
	}
}

func TestSideOverridesAndAttachmentsDoNotChangeSourceSession(t *testing.T) {
	for _, continuing := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "continue"}[continuing], func(t *testing.T) {
			repo := newFakeRepository()
			repo.sessions["session-1"] = sideTestSession()
			codex := &fakeCodexProcess{}
			service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex))
			input := SideInput{
				SessionID: "session-1", CodexSessionID: "side-thread", Prompt: "inspect attachments",
				Config: &SideConfigInput{CodexModel: " other-model ", ReasoningEffort: " low ", FastMode: false},
				Files: []SideFileInput{
					{Filename: "screenshot.png", MimeType: "image/png", Reader: strings.NewReader("png")},
					{Filename: "notes.txt", MimeType: "text/plain", Reader: strings.NewReader("notes")},
					{Filename: "notes.txt", Reader: strings.NewReader("")},
				},
			}
			var items []processdomain.CodexInputItem
			if continuing {
				if _, err := service.ContinueSide(context.Background(), input); err != nil {
					t.Fatal(err)
				}
				got := codex.loadedInput
				if got.Model != "other-model" || got.ReasoningEffort != "low" || got.FastMode || got.PermissionMode != "read-only" {
					t.Fatalf("continued config = %#v", got)
				}
				items = got.Input
			} else {
				if _, err := service.StartSide(context.Background(), input); err != nil {
					t.Fatal(err)
				}
				got := codex.forkInput
				if got.Model != "other-model" || got.ReasoningEffort != "low" || got.FastMode || got.PermissionMode != "read-only" {
					t.Fatalf("fork config = %#v", got)
				}
				items = got.Input
			}
			if len(items) != 4 || items[1].Type != "localImage" || string(items[1].Data) != "png" || items[2].Type != "mention" || string(items[2].Data) != "notes" || items[3].Data == nil {
				t.Fatalf("Side inputs = %#v", items)
			}
			if len(repo.saved) != 0 || repo.sessions["session-1"].Config != sideTestSession().Config {
				t.Fatal("Side mutated the source config")
			}
		})
	}
}

func TestSideAllowsAttachmentOnlyAndRejectsInvalidInputBeforeStarting(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     SideInput
		wantError bool
	}{
		{name: "attachment only", input: SideInput{Files: []SideFileInput{{Filename: "empty.txt", Reader: strings.NewReader("")}}}},
		{name: "empty", wantError: true},
		{name: "incomplete config", input: SideInput{Prompt: "hello", Config: &SideConfigInput{CodexModel: "test"}}, wantError: true},
		{name: "missing reader", input: SideInput{Files: []SideFileInput{{Filename: "a.txt"}}}, wantError: true},
		{name: "failed read", input: SideInput{Files: []SideFileInput{{Filename: "a.txt", Reader: failingSideReader{}}}}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository()
			repo.sessions["session-1"] = sideTestSession()
			codex := &fakeCodexProcess{}
			service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex))
			tc.input.SessionID = "session-1"
			_, err := service.StartSide(context.Background(), tc.input)
			if (err != nil) != tc.wantError || codex.forkCalled == tc.wantError {
				t.Fatalf("error = %v, started = %v", err, codex.forkCalled)
			}
		})
	}
}

type failingSideReader struct{}

func (failingSideReader) Read([]byte) (int, error) { return 0, errors.New("upload read failed") }

func TestStopSideAndSideEventsDelegateWithoutPersistence(t *testing.T) {
	repo := newFakeRepository()
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{EventID: "message-1", Type: processdomain.CodexEventMessage}
	close(source)
	codex := &fakeCodexProcess{events: source}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex))

	events, err := service.SideEvents(context.Background(), "side-run-1")
	if err != nil || (<-events).EventID != "message-1" {
		t.Fatalf("SideEvents() event/error = %#v/%v", events, err)
	}
	if err := service.StopSide(context.Background(), "side-run-1"); err != nil {
		t.Fatal(err)
	}
	if codex.stoppedID != "side-run-1" || len(repo.saved) != 0 {
		t.Fatalf("stop/persistence = stopped:%q saved:%d", codex.stoppedID, len(repo.saved))
	}
}

func sideTestSession() domain.Session {
	return domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		BaseBranch: "main", WorktreePath: "/workspace/session-1", CodexSessionID: "source-thread",
		Config: domain.Config{CodexModel: "gpt-test", ReasoningEffort: "high", PermissionMode: "workspace-write", FastMode: true},
	}
}
