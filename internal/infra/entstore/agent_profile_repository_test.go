package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/agentprofile"
	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/session"
)

func TestAgentProfilePersistenceAndIndependentConfigurationUpdates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(dir, "profile.db"), DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.AgentProfiles()
	p, err := repo.Get(ctx)
	if err != nil || !p.Configuration.Enabled || p.Analysis.Status != "idle" {
		t.Fatalf("default = %#v, %v", p, err)
	}
	now := time.Now().UTC()
	a := agentprofile.Analysis{Prompt: "先验证再汇报", Through: &now, Status: "completed"}
	if err := repo.SaveAnalysis(ctx, a); err != nil {
		t.Fatal(err)
	}
	cfg := agentprofile.Configuration{Enabled: false, Cron: "0 3 * * *", Model: "test", ReasoningEffort: "high"}
	if err := repo.SaveConfiguration(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	a.Status = "running"
	a.Progress = agentprofile.AnalysisProgress{Stage: "refining", Percent: 42, CompletedMessages: 3, TotalMessages: 8}
	if err := repo.SaveAnalysis(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	p, err = repo.Get(ctx)
	if err != nil || p.Configuration != cfg || p.Analysis.Prompt != a.Prompt || p.Analysis.Status != "running" || !p.Analysis.Through.Equal(now) || p.Analysis.Progress != a.Progress {
		t.Fatalf("persisted = %#v, %v", p, err)
	}
}

func TestAgentProfileAppendsAreIncrementalAndResolveTheirOriginalRun(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(dir, "threads.db"), DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, card := range []session.Session{
		{ID: "s1", ProjectID: "p1", Requirement: "one", Status: session.StatusClosed, CodexSessionID: "current", Mode: session.ModeChat, CreatedAt: now, UpdatedAt: now},
		{ID: "s2", ProjectID: "p2", Requirement: "two", Status: session.StatusRunning, CodexSessionID: "other", Mode: session.ModeChat, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.Sessions().Save(ctx, card); err != nil {
			t.Fatal(err)
		}
	}
	for i, thread := range []string{"current", "previous"} {
		if err := store.Processes().CreateRun(ctx, process.Run{ID: process.RunID(thread), SessionID: "s1", CodexSessionID: thread, StartedAt: now.Add(-time.Duration(i+1) * time.Hour), FinishedAt: &now, Status: process.StatusExited}); err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range []string{"old", "new", "future"} {
		if err := store.Sessions().AppendPrompt(ctx, session.PromptAppend{ID: id, SessionID: "s1", Body: "纠正", CreatedAt: now.Add(time.Duration(i-1) * time.Minute), DispatchedProcessRunID: "previous"}); err != nil {
			t.Fatal(err)
		}
	}
	appends, err := store.AgentProfiles().ListPromptAppends(ctx, now.Add(-time.Minute), now)
	if err != nil || len(appends) != 1 || appends[0].ID != "new" {
		t.Fatalf("appends=%#v,%v", appends, err)
	}
	thread, err := store.AgentProfiles().ThreadForPrompt(ctx, "new")
	if err != nil || thread != "previous" {
		t.Fatalf("thread=%q,%v", thread, err)
	}
}
