package session

import (
	"context"
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

	run, err := service.StartSide(context.Background(), StartSideInput{SessionID: "session-1", Prompt: "  inspect the parser  "})
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

	run, err := service.ContinueSide(context.Background(), ContinueSideInput{
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
