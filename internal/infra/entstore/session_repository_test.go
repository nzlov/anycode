package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
)

func TestSessionRepositorySaveFindListLastConfigAndAppendPrompt(t *testing.T) {
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

	repo := store.Sessions()
	now := time.Now().UTC()
	projectID := session.ProjectID("project-1")
	oldProjectID := session.ProjectID("project-2")
	input := session.Session{
		ID:             session.ID("session-1"),
		ProjectID:      projectID,
		Requirement:    "Build session persistence",
		Mode:           session.ModeChat,
		Status:         session.StatusRunning,
		BaseBranch:     "main",
		WorktreePath:   "/worktrees/session-1",
		CodexSessionID: "codex-1",
		Config: session.Config{
			CodexModel:      "gpt-5.4",
			ReasoningEffort: "high",
			PermissionMode:  "workspace-write",
		},
		LastRunAt: &now,
		CreatedAt: now.Add(-10 * time.Minute),
		UpdatedAt: now.Add(-5 * time.Minute),
	}
	if err := repo.Save(ctx, input); err != nil {
		t.Fatalf("save session: %v", err)
	}

	found, err := repo.Find(ctx, input.ID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	assertSessionEqual(t, found, input)

	updatedAt := now.Add(time.Minute)
	input.Status = session.StatusStopped
	input.Config.CodexModel = "gpt-5.4-mini"
	input.UpdatedAt = updatedAt
	if err := repo.Save(ctx, input); err != nil {
		t.Fatalf("update session: %v", err)
	}
	found, err = repo.Find(ctx, input.ID)
	if err != nil {
		t.Fatalf("find updated session: %v", err)
	}
	if found.Status != session.StatusStopped || found.Config.CodexModel != "gpt-5.4-mini" {
		t.Fatalf("updated session mismatch: %#v", found)
	}

	recentRun := now.Add(-24 * time.Hour)
	historyRun := now.Add(-5 * 24 * time.Hour)
	otherProjectRun := now.Add(-2 * time.Hour)
	saveSessions(t, ctx, repo,
		session.Session{
			ID:          session.ID("session-2"),
			ProjectID:   projectID,
			Requirement: "Fix GraphQL card list",
			Mode:        session.ModeWorkflow,
			Status:      session.StatusCompleted,
			BaseBranch:  "develop",
			Config: session.Config{
				CodexModel:      "gpt-5.4-last",
				ReasoningEffort: "medium",
				PermissionMode:  "read-only",
			},
			LastRunAt: &recentRun,
			CreatedAt: now.Add(-8 * time.Minute),
			UpdatedAt: now.Add(-4 * time.Minute),
		},
		session.Session{
			ID:          session.ID("session-3"),
			ProjectID:   projectID,
			Requirement: "Old history card",
			Mode:        session.ModeChat,
			Status:      session.StatusStopped,
			BaseBranch:  "main",
			LastRunAt:   &historyRun,
			CreatedAt:   now.Add(-9 * time.Minute),
			UpdatedAt:   now.Add(-3 * time.Minute),
		},
		session.Session{
			ID:          session.ID("session-4"),
			ProjectID:   oldProjectID,
			Requirement: "Other project",
			Mode:        session.ModeChat,
			Status:      session.StatusRunning,
			LastRunAt:   &otherProjectRun,
			CreatedAt:   now.Add(-6 * time.Minute),
			UpdatedAt:   now.Add(-2 * time.Minute),
		},
	)

	cards, total, err := repo.ListCards(ctx, session.ListQuery{
		ProjectID: &projectID,
		Range:     "recent3d",
		Page:      1,
		PageSize:  1,
		Sort:      "created_at asc",
	})
	if err != nil {
		t.Fatalf("list recent cards: %v", err)
	}
	if total != 2 || len(cards) != 1 || cards[0].ID != "session-1" {
		t.Fatalf("recent cards mismatch: total=%d cards=%#v", total, cards)
	}

	cards, total, err = repo.ListCards(ctx, session.ListQuery{
		ProjectID: &projectID,
		Range:     "history7d",
		Page:      1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("list history cards: %v", err)
	}
	if total != 1 || len(cards) != 1 || cards[0].ID != "session-3" {
		t.Fatalf("history cards mismatch: total=%d cards=%#v", total, cards)
	}

	cards, total, err = repo.ListCards(ctx, session.ListQuery{
		Scope:    "overview",
		Range:    "all",
		Filter:   "graphql",
		Page:     1,
		PageSize: 10,
		Sort:     "-updated_at",
	})
	if err != nil {
		t.Fatalf("list filtered overview cards: %v", err)
	}
	if total != 1 || len(cards) != 1 || cards[0].ID != "session-2" {
		t.Fatalf("filtered overview mismatch: total=%d cards=%#v", total, cards)
	}

	config, ok, err := repo.LastConfigForProject(ctx, projectID)
	if err != nil {
		t.Fatalf("last config: %v", err)
	}
	if !ok {
		t.Fatal("last config not found")
	}
	if config.CodexModel != "gpt-5.4-last" || config.ReasoningEffort != "medium" || config.PermissionMode != "read-only" {
		t.Fatalf("last config mismatch: %#v", config)
	}

	appendAt := now.Add(2 * time.Minute)
	if err := repo.AppendPrompt(ctx, session.PromptAppend{
		ID:        "append-1",
		SessionID: input.ID,
		Body:      "continue with tests",
		CreatedAt: appendAt,
	}); err != nil {
		t.Fatalf("append prompt: %v", err)
	}
	appends, err := repo.ListPromptAppends(ctx, input.ID)
	if err != nil {
		t.Fatalf("list prompt appends: %v", err)
	}
	if len(appends) != 1 || appends[0].SessionID != input.ID || appends[0].Body != "continue with tests" {
		t.Fatalf("prompt append mismatch: %#v", appends)
	}
}

func saveSessions(t *testing.T, ctx context.Context, repo *SessionRepository, sessions ...session.Session) {
	t.Helper()
	for _, s := range sessions {
		if err := repo.Save(ctx, s); err != nil {
			t.Fatalf("save session %s: %v", s.ID, err)
		}
	}
}

func assertSessionEqual(t *testing.T, got, want session.Session) {
	t.Helper()
	if got.ID != want.ID ||
		got.ProjectID != want.ProjectID ||
		got.Requirement != want.Requirement ||
		got.Mode != want.Mode ||
		got.Status != want.Status ||
		got.BaseBranch != want.BaseBranch ||
		got.WorktreePath != want.WorktreePath ||
		got.CodexSessionID != want.CodexSessionID ||
		got.Config != want.Config {
		t.Fatalf("session mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(*want.LastRunAt) {
		t.Fatalf("last run mismatch: got=%v want=%v", got.LastRunAt, want.LastRunAt)
	}
}
