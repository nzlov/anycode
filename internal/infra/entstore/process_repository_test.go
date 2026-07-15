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
	foundRun, err := repo.FindRun(ctx, run.ID)
	if err != nil || foundRun.ID != run.ID {
		t.Fatalf("FindRun() = %#v, %v", foundRun, err)
	}

	if err := repo.MarkRunning(ctx, run.ID, 1234, "codex-session-1"); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := repo.MarkStopping(ctx, run.ID); err != nil {
		t.Fatalf("mark stopping: %v", err)
	}
	active, ok, err = repo.FindActiveBySession(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find stopping active: %v", err)
	}
	if !ok || active.Status != process.StatusStopping {
		t.Fatalf("stopping run mismatch: ok=%v run=%#v", ok, active)
	}
	activeCount, err := repo.CountActive(ctx)
	if err != nil || activeCount != 1 {
		t.Fatalf("stopping active count = %d, %v", activeCount, err)
	}
	if err := repo.MarkRunning(ctx, run.ID, 1234, "codex-session-1"); err != nil {
		t.Fatalf("restore running: %v", err)
	}
	active, ok, err = repo.FindActiveBySession(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find running active: %v", err)
	}
	if !ok || active.Status != process.StatusRunning || active.PID == nil || *active.PID != 1234 || active.CodexSessionID != "codex-session-1" {
		t.Fatalf("running run mismatch: ok=%v run=%#v", ok, active)
	}
	activeCount, err = repo.CountActive(ctx)
	if err != nil {
		t.Fatalf("count active: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active count = %d", activeCount)
	}
	if err := repo.MarkWaitingUser(ctx, run.ID); err != nil {
		t.Fatalf("mark waiting user: %v", err)
	}
	if err := store.Sessions().Save(ctx, session.Session{
		ID: "session-1", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusWaitingUser, CreatedAt: startedAt, UpdatedAt: startedAt,
	}); err != nil {
		t.Fatalf("save waiting session: %v", err)
	}
	activeCount, err = repo.CountActive(ctx)
	if err != nil || activeCount != 1 {
		t.Fatalf("warm waiting active count = %d, %v", activeCount, err)
	}
	if err := repo.MarkRunning(ctx, run.ID, 1234, "codex-session-1"); err != nil {
		t.Fatalf("restore running after wait: %v", err)
	}
	if err := store.Sessions().Save(ctx, session.Session{
		ID: "session-1", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusRunning, CreatedAt: startedAt, UpdatedAt: startedAt,
	}); err != nil {
		t.Fatalf("restore running session: %v", err)
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
	resumeOf := run.ID
	resumed := process.Run{
		ID: "process-run-2", SessionID: run.SessionID, NodeRunID: &nodeRunID,
		Status: process.StatusStarting, ResumeOf: &resumeOf, StartedAt: finishedAt.Add(time.Minute),
	}
	if err := repo.CreateRun(ctx, resumed); err != nil {
		t.Fatalf("create resumed run: %v", err)
	}
	latest, ok, err := repo.FindLatestBySession(ctx, run.SessionID)
	if err != nil || !ok || latest.ID != resumed.ID || latest.ResumeOf == nil || *latest.ResumeOf != run.ID {
		t.Fatalf("latest resumed run = %#v ok=%v error=%v", latest, ok, err)
	}
}

func TestProcessRepositoryHasAnyBySessionIncludesTerminalRuns(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	repo := store.Processes()
	exists, err := repo.HasAnyBySession(ctx, "session-1")
	if err != nil {
		t.Fatalf("HasAnyBySession() before create error = %v", err)
	}
	if exists {
		t.Fatal("HasAnyBySession() before create = true")
	}
	if err := repo.CreateRun(ctx, process.Run{
		ID:        "process-run-1",
		SessionID: "session-1",
		Status:    process.StatusExited,
	}); err != nil {
		t.Fatalf("create terminal run: %v", err)
	}
	exists, err = repo.HasAnyBySession(ctx, "session-1")
	if err != nil {
		t.Fatalf("HasAnyBySession() error = %v", err)
	}
	if !exists {
		t.Fatal("HasAnyBySession() = false, want true")
	}
}

func TestProcessRepositoryRejectsSecondActiveRunForSession(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	repo := store.Processes()
	if err := repo.CreateRun(ctx, process.Run{ID: "process-run-1", SessionID: "session-1", Status: process.StatusRunning}); err != nil {
		t.Fatalf("create first run: %v", err)
	}
	if err := repo.CreateRun(ctx, process.Run{ID: "process-run-2", SessionID: "session-1", Status: process.StatusStarting}); err == nil {
		t.Fatal("second active process run was accepted")
	}
	if err := repo.CreateRun(ctx, process.Run{ID: "process-run-exited", SessionID: "session-1", Status: process.StatusExited}); err != nil {
		t.Fatalf("create terminal run: %v", err)
	}
}

func TestMigrateCollapsesLegacyDuplicateActiveRuns(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP INDEX process_runs_one_active_per_session"); err != nil {
		t.Fatalf("drop active run index: %v", err)
	}

	repo := store.Processes()
	startedAt := time.Unix(1, 0).UTC()
	for _, run := range []process.Run{
		{ID: "process-run-old", SessionID: "session-1", Status: process.StatusRunning, StartedAt: startedAt},
		{ID: "process-run-new", SessionID: "session-1", Status: process.StatusStarting, StartedAt: startedAt.Add(time.Second)},
	} {
		if err := repo.CreateRun(ctx, run); err != nil {
			t.Fatalf("create legacy run %s: %v", run.ID, err)
		}
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate legacy runs: %v", err)
	}

	active, ok, err := repo.FindActiveBySession(ctx, "session-1")
	if err != nil {
		t.Fatalf("find active: %v", err)
	}
	if !ok || active.ID != "process-run-new" {
		t.Fatalf("active run = %#v ok=%v", active, ok)
	}
	old, err := store.client.ProcessRun.Get(ctx, "process-run-old")
	if err != nil {
		t.Fatalf("get old run: %v", err)
	}
	if old.Status != string(process.StatusExited) || old.FinishedAt == nil || old.FailureReason == "" {
		t.Fatalf("old run was not collapsed: %#v", old)
	}
	if err := repo.CreateRun(ctx, process.Run{ID: "process-run-third", SessionID: "session-1", Status: process.StatusRunning}); err == nil {
		t.Fatal("active run uniqueness index was not recreated")
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
