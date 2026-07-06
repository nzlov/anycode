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
		UpdatedAt: now.Add(-25 * time.Minute),
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
			CreatedAt:   now.Add(-6 * 24 * time.Hour),
			UpdatedAt:   historyRun,
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

	cards, total, err = repo.ListCards(ctx, session.ListQuery{
		Scope:    string(session.StatusRunning),
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list running cards: %v", err)
	}
	if total != 1 || len(cards) != 1 {
		t.Fatalf("running status filter mismatch: total=%d cards=%#v", total, cards)
	}
	for _, card := range cards {
		if card.Status != session.StatusRunning {
			t.Fatalf("running status filter returned %#v", card)
		}
	}
	saveSessions(t, ctx, repo,
		session.Session{
			ID:             "session-8",
			ProjectID:      projectID,
			Requirement:    "Interrupted running",
			Mode:           session.ModeChat,
			Status:         session.StatusRunning,
			CodexSessionID: "codex-8",
			CreatedAt:      now.Add(-40 * time.Minute),
			UpdatedAt:      now.Add(-40 * time.Minute),
		},
		session.Session{
			ID:             "session-5",
			ProjectID:      projectID,
			Requirement:    "Interrupted waiting user",
			Mode:           session.ModeChat,
			Status:         session.StatusWaitingUser,
			CodexSessionID: "codex-5",
			CreatedAt:      now.Add(-39 * time.Minute),
			UpdatedAt:      now.Add(-39 * time.Minute),
		},
		session.Session{
			ID:             "session-9",
			ProjectID:      projectID,
			Requirement:    "Queued answer user",
			Mode:           session.ModeChat,
			Status:         session.StatusQueued,
			CodexSessionID: "codex-9",
			Queue:          session.QueueIntent{Kind: session.QueueKindAnswerUser},
			CreatedAt:      now.Add(-38 * time.Minute),
			UpdatedAt:      now.Add(-38 * time.Minute),
		},
		session.Session{
			ID:             "session-10",
			ProjectID:      projectID,
			Requirement:    "Queued normal start",
			Mode:           session.ModeChat,
			Status:         session.StatusQueued,
			CodexSessionID: "codex-10",
			Queue:          session.QueueIntent{Kind: session.QueueKindStart},
			CreatedAt:      now.Add(-37 * time.Minute),
			UpdatedAt:      now.Add(-37 * time.Minute),
		},
		session.Session{
			ID:          "session-6",
			ProjectID:   projectID,
			Requirement: "Running without codex session id",
			Mode:        session.ModeChat,
			Status:      session.StatusRunning,
			CreatedAt:   now.Add(-36 * time.Minute),
			UpdatedAt:   now.Add(-36 * time.Minute),
		},
		session.Session{
			ID:             "session-7",
			ProjectID:      projectID,
			Requirement:    "Already stopped",
			Mode:           session.ModeChat,
			Status:         session.StatusStopped,
			CodexSessionID: "codex-7",
			CreatedAt:      now.Add(-35 * time.Minute),
			UpdatedAt:      now.Add(-35 * time.Minute),
		},
	)
	interrupted, err := repo.ListInterruptedWithCodexSession(ctx)
	if err != nil {
		t.Fatalf("list interrupted sessions: %v", err)
	}
	gotInterruptedIDs := make([]session.ID, 0, len(interrupted))
	for _, item := range interrupted {
		gotInterruptedIDs = append(gotInterruptedIDs, item.ID)
	}
	wantInterruptedIDs := []session.ID{"session-8", "session-5", "session-9"}
	if len(gotInterruptedIDs) != len(wantInterruptedIDs) {
		t.Fatalf("interrupted sessions = %#v, want %#v", gotInterruptedIDs, wantInterruptedIDs)
	}
	for i := range wantInterruptedIDs {
		if gotInterruptedIDs[i] != wantInterruptedIDs[i] {
			t.Fatalf("interrupted sessions = %#v, want %#v", gotInterruptedIDs, wantInterruptedIDs)
		}
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

	if err := repo.AddMergeRecord(ctx, session.MergeRecord{
		ID:             "merge-record-failed",
		SessionID:      input.ID,
		Strategy:       "merge",
		BaseBranch:     "main",
		WorktreeBranch: "feature/session-1",
		BaseCommit:     "base-0",
		HeadCommit:     "head-0",
		Status:         "failed",
		FailureCode:    "merge_conflict",
		CreatedAt:      now.Add(3 * time.Minute),
	}); err != nil {
		t.Fatalf("add failed merge record: %v", err)
	}
	if err := repo.AddMergeRecord(ctx, session.MergeRecord{
		ID:             "merge-record-old",
		SessionID:      input.ID,
		Strategy:       "merge",
		BaseBranch:     "main",
		WorktreeBranch: "feature/session-1",
		BaseCommit:     "base-1",
		HeadCommit:     "head-1",
		MergeCommit:    "merge-1",
		Status:         "merged",
		CreatedAt:      now.Add(4 * time.Minute),
	}); err != nil {
		t.Fatalf("add old merge record: %v", err)
	}
	if err := repo.AddMergeRecord(ctx, session.MergeRecord{
		ID:             "merge-record-new",
		SessionID:      input.ID,
		Strategy:       "rebase",
		BaseBranch:     "main",
		WorktreeBranch: "feature/session-1",
		BaseCommit:     "base-2",
		HeadCommit:     "head-2",
		MergeCommit:    "merge-2",
		Status:         "merged",
		MergedAt:       &appendAt,
		CreatedAt:      now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("add new merge record: %v", err)
	}
	mergeRecord, ok, err := repo.LatestSuccessfulMergeRecord(ctx, input.ID)
	if err != nil {
		t.Fatalf("latest successful merge record: %v", err)
	}
	if !ok || mergeRecord.ID != "merge-record-new" || mergeRecord.Strategy != "rebase" || mergeRecord.MergeCommit != "merge-2" {
		t.Fatalf("latest merge record mismatch: ok=%v record=%#v", ok, mergeRecord)
	}
}

func TestAttachmentRepositoryPersistsLifecycleMetadata(t *testing.T) {
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

	repo := store.Attachments()
	createdAt := time.Now().UTC()
	staged := session.StagedAttachment{
		ID:           "staged-1",
		OwnerKeyHash: "owner",
		Filename:     "note.txt",
		Path:         "/data/attachments/staged/staged-1/note.txt",
		MimeType:     "text/plain",
		Size:         12,
		Previewable:  false,
		CreatedAt:    createdAt,
	}
	if err := repo.SaveStagedAttachment(ctx, staged); err != nil {
		t.Fatalf("save staged attachment: %v", err)
	}
	foundStaged, err := repo.FindStagedAttachment(ctx, staged.ID)
	if err != nil {
		t.Fatalf("find staged attachment: %v", err)
	}
	if foundStaged.ID != staged.ID || foundStaged.Path != staged.Path || foundStaged.OwnerKeyHash != "owner" {
		t.Fatalf("staged attachment mismatch: %#v", foundStaged)
	}

	attachment := session.SessionAttachment{
		ID:          "attachment-1",
		SessionID:   "session-1",
		Kind:        "file",
		Filename:    "note.txt",
		Path:        "/data/attachments/sessions/session-1/attachment-1/note.txt",
		MimeType:    "text/plain",
		Size:        12,
		Previewable: false,
		CreatedAt:   createdAt.Add(time.Second),
	}
	if err := repo.SaveSessionAttachment(ctx, attachment); err != nil {
		t.Fatalf("save session attachment: %v", err)
	}
	attachments, err := repo.ListSessionAttachments(ctx, "session-1")
	if err != nil {
		t.Fatalf("list session attachments: %v", err)
	}
	if len(attachments) != 1 || attachments[0].ID != attachment.ID || attachments[0].Path != attachment.Path {
		t.Fatalf("session attachments mismatch: %#v", attachments)
	}
	if err := repo.DeleteStagedAttachment(ctx, staged.ID); err != nil {
		t.Fatalf("delete staged attachment: %v", err)
	}
	if err := repo.DeleteSessionAttachment(ctx, attachment.ID); err != nil {
		t.Fatalf("delete session attachment: %v", err)
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
