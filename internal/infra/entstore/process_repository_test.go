package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/session"
	entprocessevent "github.com/nzlov/anycode/internal/infra/entstore/ent/processevent"
)

func TestProcessRepositoryPersistsRunLifecycleAndEvents(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{
		DatabaseURL: filepath.Join(t.TempDir(), "anycode.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	repo := store.Processes()
	startedAt := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
	if err := store.Sessions().Save(ctx, session.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      session.ModeChat,
		Status:    session.StatusRunning,
		CreatedAt: startedAt,
		UpdatedAt: startedAt,
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	nodeRunID := process.NodeRunID("node-run-1")
	run := process.Run{
		ID:        process.RunID("process-run-1"),
		SessionID: process.SessionID("session-1"),
		NodeRunID: &nodeRunID,
		Status:    process.StatusStarting,
		StartedAt: startedAt,
	}
	if err := repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}

	active, ok, err := repo.FindActiveBySession(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find active: %v", err)
	}
	if !ok {
		t.Fatal("active run not found")
	}
	if active.ID != run.ID || active.SessionID != run.SessionID || active.Status != process.StatusStarting {
		t.Fatalf("active run mismatch: %#v", active)
	}
	if active.NodeRunID == nil || *active.NodeRunID != nodeRunID {
		t.Fatalf("node run id mismatch: %#v", active.NodeRunID)
	}

	if err := repo.MarkRunning(ctx, run.ID, 1234, "codex-session-1"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	active, ok, err = repo.FindActiveBySession(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find running active: %v", err)
	}
	if !ok || active.Status != process.StatusRunning || active.PID == nil || *active.PID != 1234 || active.CodexSessionID != "codex-session-1" {
		t.Fatalf("running run mismatch: ok=%v run=%#v", ok, active)
	}
	activeCount, err := repo.CountActive(ctx)
	if err != nil {
		t.Fatalf("count active: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active count = %d", activeCount)
	}

	terminalSessionID := session.ID("session-terminal")
	if err := store.Sessions().Save(ctx, session.Session{
		ID:        terminalSessionID,
		ProjectID: "project-1",
		Mode:      session.ModeChat,
		Status:    session.StatusResumeFailed,
		CreatedAt: startedAt,
		UpdatedAt: startedAt,
	}); err != nil {
		t.Fatalf("save terminal session: %v", err)
	}
	if err := repo.CreateRun(ctx, process.Run{
		ID:        "process-run-terminal",
		SessionID: process.SessionID(terminalSessionID),
		Status:    process.StatusRunning,
		StartedAt: startedAt,
	}); err != nil {
		t.Fatalf("create terminal run: %v", err)
	}
	activeCount, err = repo.CountActive(ctx)
	if err != nil {
		t.Fatalf("count active with terminal run: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active count with terminal run = %d", activeCount)
	}

	eventAt := startedAt.Add(time.Minute)
	if err := repo.SaveEvent(ctx, process.Event{
		ID:           "process-event-1",
		SessionID:    run.SessionID,
		ProcessRunID: &run.ID,
		EventID:      "codex-event-1",
		Type:         "message",
		Payload: map[string]any{
			"text": "hello",
		},
		CreatedAt: eventAt,
	}); err != nil {
		t.Fatalf("save event: %v", err)
	}
	event, err := store.Client().ProcessEvent.Query().
		Where(entprocessevent.IDEQ("process-event-1")).
		Only(ctx)
	if err != nil {
		t.Fatalf("find process event: %v", err)
	}
	if event.SessionID != string(run.SessionID) || event.ProcessRunID == nil || *event.ProcessRunID != string(run.ID) || event.EventID != "codex-event-1" || event.Type != "message" {
		t.Fatalf("process event mismatch: %#v", event)
	}
	if event.Payload["text"] != "hello" {
		t.Fatalf("process event payload mismatch: %#v", event.Payload)
	}

	if err := repo.SaveEvent(ctx, process.Event{
		ID:        "process-event-secret",
		SessionID: run.SessionID,
		EventID:   "codex-event-secret",
		Type:      "message",
		Payload: map[string]any{
			"authorization": "Bearer secret",
			"workdir":       "/home/nzlov/workspaces/github/project",
		},
		CreatedAt: eventAt,
	}); err != nil {
		t.Fatalf("save secret event: %v", err)
	}
	secretEvent, err := store.Client().ProcessEvent.Query().
		Where(entprocessevent.IDEQ("process-event-secret")).
		Only(ctx)
	if err != nil {
		t.Fatalf("find secret process event: %v", err)
	}
	if secretEvent.Payload["authorization"] != "[redacted]" || secretEvent.Payload["workdir"] != "[redacted_path]" {
		t.Fatalf("secret process event payload mismatch: %#v", secretEvent.Payload)
	}

	exitCode := 0
	finishedAt := startedAt.Add(2 * time.Minute)
	if err := repo.MarkExited(ctx, run.ID, process.ExitResult{
		ExitCode:   &exitCode,
		FinishedAt: finishedAt,
	}); err != nil {
		t.Fatalf("mark exited: %v", err)
	}
	active, ok, err = repo.FindActiveBySession(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find active after exit: %v", err)
	}
	if ok {
		t.Fatalf("exited run should not be active: %#v", active)
	}
}
