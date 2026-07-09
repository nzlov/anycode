package entstore

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/session"
)

func TestProcessRepositoryPersistsRunLifecycle(t *testing.T) {
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

func TestProcessRepositoryListsCodexSessionIDsForSession(t *testing.T) {
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

	startedAt := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
	if err := store.Sessions().Save(ctx, session.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      session.ModeChat,
		Status:    session.StatusStopped,
		CreatedAt: startedAt,
		UpdatedAt: startedAt,
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	repo := store.Processes()
	runs := []process.Run{
		{ID: "process-run-1", SessionID: "session-1", Status: process.StatusExited, CodexSessionID: "codex-session-1", StartedAt: startedAt},
		{ID: "process-run-2", SessionID: "session-1", Status: process.StatusExited, CodexSessionID: "codex-session-2", StartedAt: startedAt.Add(time.Minute)},
		{ID: "process-run-3", SessionID: "session-1", Status: process.StatusExited, CodexSessionID: "codex-session-1", StartedAt: startedAt.Add(2 * time.Minute)},
		{ID: "process-run-empty", SessionID: "session-1", Status: process.StatusExited, StartedAt: startedAt.Add(3 * time.Minute)},
	}
	for _, run := range runs {
		if err := repo.CreateRun(ctx, run); err != nil {
			t.Fatalf("create run %s: %v", run.ID, err)
		}
	}

	got, err := repo.CodexSessionIDs(ctx, "session-1")
	if err != nil {
		t.Fatalf("CodexSessionIDs() error = %v", err)
	}
	if want := []string{"codex-session-1", "codex-session-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CodexSessionIDs() = %#v, want %#v", got, want)
	}
}
