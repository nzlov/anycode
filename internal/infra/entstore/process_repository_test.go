package entstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/session"
)

func TestTranscriptBindingRollsBackSourceAndRunStateWithTransaction(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().CreateRun(ctx, process.Run{ID: "process-1", SessionID: "session-1", Status: process.StatusStarting}); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected session save failure")
	err = store.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		if err := tx.Processes().BindTranscript(ctx, "process-1", 1234, process.CodexTranscriptSource{
			CodexSessionID: "codex-1", RelativePath: "2026/07/15/codex-1.jsonl", BoundAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("transaction error = %v", err)
	}
	if _, found, err := store.Processes().FindTranscriptSource(ctx, "session-1", "codex-1"); err != nil || found {
		t.Fatalf("source after rollback found=%v error=%v", found, err)
	}
	run, err := store.Processes().FindRun(ctx, "process-1")
	if err != nil || run.Status != process.StatusStarting || run.CodexSessionID != "" {
		t.Fatalf("run after rollback = %#v, %v", run, err)
	}
}

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

func TestProcessRepositoryBindsAndListsTranscriptSourcesForSession(t *testing.T) {
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
	source1 := process.CodexTranscriptSource{CodexSessionID: "codex-session-1", RelativePath: "2026/07/02/one.jsonl", BoundAt: startedAt}
	source2 := process.CodexTranscriptSource{CodexSessionID: "codex-session-2", RelativePath: "2026/07/02/two.jsonl", BoundAt: startedAt.Add(time.Minute)}
	for _, binding := range []struct {
		run    process.Run
		source process.CodexTranscriptSource
	}{
		{process.Run{ID: "process-run-1", SessionID: "session-1", Status: process.StatusStarting, StartedAt: startedAt}, source1},
		{process.Run{ID: "process-run-2", SessionID: "session-1", Status: process.StatusStarting, StartedAt: startedAt.Add(time.Minute)}, source2},
		{process.Run{ID: "process-run-3", SessionID: "session-1", Status: process.StatusStarting, StartedAt: startedAt.Add(2 * time.Minute)}, source1},
	} {
		if err := repo.CreateRun(ctx, binding.run); err != nil {
			t.Fatalf("create run %s: %v", binding.run.ID, err)
		}
		if err := repo.MarkStarted(ctx, binding.run.ID, 1234); err != nil {
			t.Fatalf("mark %s started: %v", binding.run.ID, err)
		}
		if err := repo.BindTranscript(ctx, binding.run.ID, 1234, binding.source); err != nil {
			t.Fatalf("bind %s: %v", binding.run.ID, err)
		}
		if err := repo.BindTranscript(ctx, binding.run.ID, 1234, binding.source); err != nil {
			t.Fatalf("idempotent bind %s: %v", binding.run.ID, err)
		}
		if binding.run.ID == "process-run-1" {
			conflict := source1
			conflict.RelativePath = "2026/07/02/conflict.jsonl"
			if err := repo.BindTranscript(ctx, binding.run.ID, 1234, conflict); err == nil {
				t.Fatal("conflicting transcript path was accepted")
			}
		}
		if err := repo.MarkExited(ctx, binding.run.ID, process.ExitResult{FinishedAt: startedAt.Add(4 * time.Minute)}); err != nil {
			t.Fatalf("restore %s exited: %v", binding.run.ID, err)
		}
	}
	if err := store.Sessions().Save(ctx, session.Session{
		ID: "session-2", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusStopped,
		CreatedAt: startedAt, UpdatedAt: startedAt,
	}); err != nil {
		t.Fatalf("save second session: %v", err)
	}
	session2Source := source1
	session2Source.RelativePath = "2026/07/02/session-2.jsonl"
	if err := repo.CreateRun(ctx, process.Run{ID: "process-run-4", SessionID: "session-2", Status: process.StatusStarting, StartedAt: startedAt}); err != nil {
		t.Fatalf("create second session run: %v", err)
	}
	if err := repo.BindTranscript(ctx, "process-run-4", 5678, session2Source); err != nil {
		t.Fatalf("bind second session transcript: %v", err)
	}
	if err := repo.MarkExited(ctx, "process-run-4", process.ExitResult{FinishedAt: startedAt.Add(4 * time.Minute)}); err != nil {
		t.Fatalf("exit second session run: %v", err)
	}
	if err := repo.CreateRun(ctx, process.Run{ID: "process-run-empty", SessionID: "session-1", Status: process.StatusExited, StartedAt: startedAt.Add(3 * time.Minute)}); err != nil {
		t.Fatalf("create empty run: %v", err)
	}
	if err := repo.BindTranscript(ctx, "process-run-1", 1234, source1); err == nil {
		t.Fatal("exited process run was rebound")
	}
	if err := repo.MarkStarted(ctx, "process-run-1", 1234); err == nil {
		t.Fatal("exited process run was marked started")
	}
	stopping := process.Run{ID: "process-run-stopping", SessionID: "session-1", Status: process.StatusStopping, StartedAt: startedAt.Add(5 * time.Minute)}
	if err := repo.CreateRun(ctx, stopping); err != nil {
		t.Fatalf("create stopping run: %v", err)
	}
	if err := repo.MarkStarted(ctx, stopping.ID, 4321); err != nil {
		t.Fatalf("record PID for stopping run: %v", err)
	}
	if err := repo.MarkExited(ctx, stopping.ID, process.ExitResult{FinishedAt: startedAt.Add(6 * time.Minute)}); err != nil {
		t.Fatalf("mark stopping run exited: %v", err)
	}
	for _, invalid := range []string{"../escape.jsonl", "/absolute.jsonl"} {
		invalidSource := process.CodexTranscriptSource{CodexSessionID: "invalid-" + invalid, RelativePath: invalid}
		if err := repo.BindTranscript(ctx, "process-run-1", 1234, invalidSource); err == nil {
			t.Fatalf("invalid path %q was accepted", invalid)
		}
	}

	got, err := repo.TranscriptSources(ctx, "session-1")
	if err != nil {
		t.Fatalf("TranscriptSources() error = %v", err)
	}
	if len(got) != 2 || got[0].CodexSessionID != source1.CodexSessionID || got[0].RelativePath != source1.RelativePath || got[1].CodexSessionID != source2.CodexSessionID {
		t.Fatalf("TranscriptSources() = %#v", got)
	}
	if source, found, err := repo.FindTranscriptSource(ctx, "session-1", source1.CodexSessionID); err != nil || !found || source.RelativePath != source1.RelativePath {
		t.Fatalf("session-1 transcript source = %#v, found=%v, error=%v", source, found, err)
	}
	if source, found, err := repo.FindTranscriptSource(ctx, "session-2", source1.CodexSessionID); err != nil || !found || source.RelativePath != session2Source.RelativePath {
		t.Fatalf("session-2 transcript source = %#v, found=%v, error=%v", source, found, err)
	}
	transcriptRuns, err := repo.TranscriptRuns(ctx, "session-1")
	if err != nil || len(transcriptRuns) != 3 || transcriptRuns[0].ID != "process-run-1" || transcriptRuns[2].ID != "process-run-3" {
		t.Fatalf("TranscriptRuns() = %#v, %v", transcriptRuns, err)
	}
}
