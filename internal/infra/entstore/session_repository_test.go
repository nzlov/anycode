package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
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
	ownershipConfirmedAt := now.Add(-3 * time.Minute)
	cleanupRequestedAt := now.Add(-2 * time.Minute)
	cleanupNextAt := now.Add(time.Minute)
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
		WorktreeBranch: "session-1",
		WorktreeCleanup: session.WorktreeCleanup{
			Status:               session.WorktreeCleanupFailed,
			Attempts:             2,
			OwnershipToken:       "owner-token",
			OwnershipConfirmedAt: &ownershipConfirmedAt,
			RequestedAt:          &cleanupRequestedAt,
			LastAt:               &now,
			NextAt:               &cleanupNextAt,
			ErrorCode:            "branch_checked_out",
			Error:                "branch is checked out",
			Retryable:            true,
		},
		CodexSessionID: "codex-1",
		Config: session.Config{
			CodexModel:      "gpt-5.4",
			ReasoningEffort: "high",
			PermissionMode:  "workspace-write",
			FastMode:        true,
		},
		TodoList: session.TodoList{Items: []session.TodoItem{
			{Text: "梳理需求", Completed: true},
			{Text: "实现卡片展示", Completed: false},
		}},
		ArtifactCount: 3,
		FilesChanged:  5,
		Queue: session.QueueIntent{
			Kind:                 session.QueueKindAnswerUser,
			InitialStart:         true,
			ResumeCodexSessionID: "codex-1",
			ResumeOfProcessRunID: "process-run-1",
			AnswerBatchID:        "batch-1",
		},
		AppliedSystemCommands: map[string]bool{"command-1": true},
		LastRunAt:             &now,
		CreatedAt:             now.Add(-10 * time.Minute),
		UpdatedAt:             now.Add(-25 * time.Minute),
	}
	if err := repo.Save(ctx, input); err != nil {
		t.Fatalf("save session: %v", err)
	}

	found, err := repo.Find(ctx, input.ID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	assertSessionEqual(t, found, input)
	if found.WorktreeCleanup.RequestedAt == nil || !found.WorktreeCleanup.RequestedAt.Equal(cleanupRequestedAt) ||
		found.WorktreeCleanup.OwnershipConfirmedAt == nil || !found.WorktreeCleanup.OwnershipConfirmedAt.Equal(ownershipConfirmedAt) ||
		found.WorktreeCleanup.LastAt == nil || !found.WorktreeCleanup.LastAt.Equal(now) ||
		found.WorktreeCleanup.NextAt == nil || !found.WorktreeCleanup.NextAt.Equal(cleanupNextAt) {
		t.Fatalf("worktree cleanup timestamps = %#v", found.WorktreeCleanup)
	}
	if !found.Queue.InitialStart {
		t.Fatalf("queue initial start = false, want true: %#v", found.Queue)
	}
	if found.Queue.ResumeOfProcessRunID != "process-run-1" || found.Queue.AnswerBatchID != "batch-1" {
		t.Fatalf("answer queue metadata = %#v", found.Queue)
	}
	if !found.AppliedSystemCommands["command-1"] {
		t.Fatalf("applied system commands = %#v", found.AppliedSystemCommands)
	}

	updatedAt := now.Add(time.Minute)
	input.Status = session.StatusStopped
	input.Config.CodexModel = "gpt-5.4-mini"
	input.Config.FastMode = false
	input.Queue.InitialStart = false
	input.UpdatedAt = updatedAt
	if err := repo.Save(ctx, input); err != nil {
		t.Fatalf("update session: %v", err)
	}
	found, err = repo.Find(ctx, input.ID)
	if err != nil {
		t.Fatalf("find updated session: %v", err)
	}
	if found.Status != session.StatusStopped || found.Config.CodexModel != "gpt-5.4-mini" || found.Config.FastMode {
		t.Fatalf("updated session mismatch: %#v", found)
	}
	if found.Queue.InitialStart {
		t.Fatalf("updated queue initial start = true, want false: %#v", found.Queue)
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
				FastMode:        true,
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
			ID:          session.ID("session-closed"),
			ProjectID:   projectID,
			Requirement: "Closed card",
			Mode:        session.ModeChat,
			Status:      session.StatusClosed,
			BaseBranch:  "main",
			LastRunAt:   &recentRun,
			CreatedAt:   now.Add(-9 * time.Minute),
			UpdatedAt:   now.Add(-3 * time.Minute),
		},
		session.Session{
			ID:          session.ID("session-closed-old"),
			ProjectID:   projectID,
			Requirement: "Older closed card",
			Mode:        session.ModeChat,
			Status:      session.StatusClosed,
			BaseBranch:  "main",
			CreatedAt:   now.Add(-5 * 24 * time.Hour),
			UpdatedAt:   now.Add(-4 * 24 * time.Hour),
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
		Range:     "latest",
		Page:      1,
		PageSize:  1,
		Sort:      "updated_at desc",
	})
	if err != nil {
		t.Fatalf("list latest cards: %v", err)
	}
	if total != 3 || len(cards) != 1 || cards[0].ID != "session-1" {
		t.Fatalf("latest cards mismatch: total=%d cards=%#v", total, cards)
	}

	cards, total, err = repo.ListCards(ctx, session.ListQuery{
		ProjectID: &projectID,
		Range:     "history",
		Page:      1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("list history cards: %v", err)
	}
	if total != 2 || len(cards) != 2 || cards[0].ID != "session-closed" || cards[1].ID != "session-closed-old" {
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
	if config.CodexModel != "gpt-5.4-last" || config.ReasoningEffort != "medium" || config.PermissionMode != "read-only" || !config.FastMode {
		t.Fatalf("last config mismatch: %#v", config)
	}

	appendAt := now.Add(2 * time.Minute)
	if err := repo.AppendPrompt(ctx, session.PromptAppend{
		ID:        "append-1",
		SessionID: input.ID,
		Body:      "continue with tests",
		Status:    session.PromptAppendPending,
		CreatedAt: appendAt,
	}); err != nil {
		t.Fatalf("append prompt: %v", err)
	}
	appends, err := repo.ListPromptAppends(ctx, input.ID)
	if err != nil {
		t.Fatalf("list prompt appends: %v", err)
	}
	if len(appends) != 1 || appends[0].SessionID != input.ID || appends[0].Body != "continue with tests" || appends[0].Status != session.PromptAppendPending {
		t.Fatalf("prompt append mismatch: %#v", appends)
	}
	pending, err := repo.ListPendingPromptAppends(ctx, input.ID)
	if err != nil {
		t.Fatalf("list pending prompt appends: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "append-1" {
		t.Fatalf("pending prompt appends = %#v", pending)
	}
	dispatchedAt := appendAt.Add(time.Minute)
	if err := repo.MarkPromptAppendsInflight(ctx, []string{"append-1"}, "process-run-1"); err != nil {
		t.Fatalf("mark prompt append inflight: %v", err)
	}
	pending, err = repo.ListPendingPromptAppends(ctx, input.ID)
	if err != nil {
		t.Fatalf("list inflight prompt appends: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending prompt appends while inflight = %#v", pending)
	}
	appends, err = repo.ListPromptAppends(ctx, input.ID)
	if err != nil {
		t.Fatalf("list inflight prompt append: %v", err)
	}
	if len(appends) != 1 || appends[0].Status != session.PromptAppendInflight || appends[0].DispatchedAt != nil || appends[0].DispatchedProcessRunID != "process-run-1" {
		t.Fatalf("inflight prompt append = %#v", appends)
	}
	if err := repo.ReleasePromptAppends(ctx, "process-run-1"); err != nil {
		t.Fatalf("release prompt append: %v", err)
	}
	appends, err = repo.ListPromptAppends(ctx, input.ID)
	if err != nil {
		t.Fatalf("list released prompt append: %v", err)
	}
	if len(appends) != 1 || appends[0].Status != session.PromptAppendPending || appends[0].DispatchedProcessRunID != "" {
		t.Fatalf("released prompt append = %#v", appends)
	}
	if err := repo.MarkPromptAppendsInflight(ctx, []string{"append-1"}, "process-run-2"); err != nil {
		t.Fatalf("mark prompt append inflight again: %v", err)
	}
	if err := repo.CompletePromptAppends(ctx, "process-run-2", dispatchedAt); err != nil {
		t.Fatalf("complete prompt append: %v", err)
	}
	appends, err = repo.ListPromptAppends(ctx, input.ID)
	if err != nil {
		t.Fatalf("list dispatched prompt append: %v", err)
	}
	if len(appends) != 1 || appends[0].Status != session.PromptAppendDispatched || appends[0].DispatchedAt == nil || !appends[0].DispatchedAt.Equal(dispatchedAt) || appends[0].DispatchedProcessRunID != "process-run-2" {
		t.Fatalf("dispatched prompt append = %#v", appends)
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

func TestSessionRepositoryRoundTripsPromptAppendQueue(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	input := session.Session{
		ID:        "session-prompt-append",
		ProjectID: "project-1",
		Mode:      session.ModeWorkflow,
		Status:    session.StatusQueued,
		Queue: session.QueueIntent{
			Kind:                 session.QueueKindPromptAppend,
			Priority:             session.QueuePriorityHigh,
			Prompt:               "workflow fallback prompt",
			ResumeCodexSessionID: "codex-1",
		},
	}
	repo := store.Sessions()
	if err := repo.Save(ctx, input); err != nil {
		t.Fatalf("save prompt append queue: %v", err)
	}
	found, err := repo.Find(ctx, input.ID)
	if err != nil {
		t.Fatalf("find prompt append queue: %v", err)
	}
	if found.Queue != input.Queue {
		t.Fatalf("prompt append queue = %#v, want %#v", found.Queue, input.Queue)
	}
}

func TestSessionRepositoryListsWorktreeCleanupDue(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	repo := store.Sessions()
	for _, input := range []session.Session{
		{ID: "provisioning", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusFailed, WorktreeCleanup: session.WorktreeCleanup{Status: session.WorktreeCleanupProvisioning}, CreatedAt: now, UpdatedAt: now},
		{ID: "pending", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusClosed, WorktreeCleanup: session.WorktreeCleanup{Status: session.WorktreeCleanupPending}, CreatedAt: now, UpdatedAt: now},
		{ID: "failed-due", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusClosed, WorktreeCleanup: session.WorktreeCleanup{Status: session.WorktreeCleanupFailed, Retryable: true, NextAt: &past}, CreatedAt: now, UpdatedAt: now},
		{ID: "failed-future", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusClosed, WorktreeCleanup: session.WorktreeCleanup{Status: session.WorktreeCleanupFailed, Retryable: true, NextAt: &future}, CreatedAt: now, UpdatedAt: now},
		{ID: "failed-terminal", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusClosed, WorktreeCleanup: session.WorktreeCleanup{Status: session.WorktreeCleanupFailed, Retryable: false}, CreatedAt: now, UpdatedAt: now},
		{ID: "active", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusStopped, WorktreeCleanup: session.WorktreeCleanup{Status: session.WorktreeCleanupActive}, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.Save(ctx, input); err != nil {
			t.Fatalf("save session %s: %v", input.ID, err)
		}
	}

	due, err := repo.ListWorktreeCleanupDue(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListWorktreeCleanupDue() error = %v", err)
	}
	if len(due) != 2 || due[0].ID != "pending" || due[1].ID != "failed-due" {
		t.Fatalf("ListWorktreeCleanupDue() = %#v", due)
	}
	provisioning, err := repo.ListProvisioningWorktrees(ctx, 10)
	if err != nil {
		t.Fatalf("ListProvisioningWorktrees() error = %v", err)
	}
	if len(provisioning) != 1 || provisioning[0].ID != "provisioning" {
		t.Fatalf("ListProvisioningWorktrees() = %#v", provisioning)
	}
}

func TestSessionRepositoryUpdatesOnlyMatchingPendingPromptAppendBody(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	repo := store.Sessions()
	createdAt := time.Unix(20, 0).UTC()
	if err := repo.AppendPrompt(ctx, session.PromptAppend{
		ID:        "append-pending",
		SessionID: "session-1",
		Body:      "before",
		Status:    session.PromptAppendPending,
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("append pending prompt: %v", err)
	}
	attachment := session.SessionAttachment{
		ID:         "attachment-1",
		SessionID:  "session-1",
		SourceType: session.AttachmentSourcePromptAppend,
		SourceID:   "append-pending",
		Kind:       "file",
		Filename:   "notes.txt",
		Path:       "/attachments/notes.txt",
		MimeType:   "text/plain",
		Size:       12,
		CreatedAt:  createdAt,
	}
	if err := store.Attachments().SaveSessionAttachment(ctx, attachment); err != nil {
		t.Fatalf("save prompt append attachment: %v", err)
	}
	if err := repo.AppendPrompt(ctx, session.PromptAppend{
		ID:                     "append-dispatched",
		SessionID:              "session-1",
		Body:                   "dispatched",
		Status:                 session.PromptAppendDispatched,
		DispatchedProcessRunID: "process-1",
		CreatedAt:              createdAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("append dispatched prompt: %v", err)
	}

	updated, ok, err := repo.UpdatePendingPromptAppendBody(ctx, "session-1", "append-pending", "after")
	if err != nil {
		t.Fatalf("UpdatePendingPromptAppendBody() error = %v", err)
	}
	if !ok || updated.Body != "after" || updated.Status != session.PromptAppendPending || !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("UpdatePendingPromptAppendBody() = %#v ok=%v", updated, ok)
	}
	if _, ok, err := repo.UpdatePendingPromptAppendBody(ctx, "session-2", "append-pending", "wrong session"); err != nil || ok {
		t.Fatalf("wrong-session update = ok:%v err:%v", ok, err)
	}
	if _, ok, err := repo.UpdatePendingPromptAppendBody(ctx, "session-1", "append-dispatched", "wrong status"); err != nil || ok {
		t.Fatalf("dispatched update = ok:%v err:%v", ok, err)
	}

	appends, err := repo.ListPromptAppends(ctx, "session-1")
	if err != nil {
		t.Fatalf("list prompt appends: %v", err)
	}
	if len(appends) != 2 || appends[0].Body != "after" || appends[1].Body != "dispatched" || appends[1].DispatchedProcessRunID != "process-1" {
		t.Fatalf("prompt appends = %#v", appends)
	}
	attachments, err := store.Attachments().ListPromptAppendAttachments(ctx, "session-1", "append-pending")
	if err != nil {
		t.Fatalf("list prompt append attachments: %v", err)
	}
	if len(attachments) != 1 || attachments[0].ID != attachment.ID || attachments[0].SourceID != "append-pending" {
		t.Fatalf("prompt append attachments = %#v", attachments)
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
		SourceType:  session.AttachmentSourceRequirement,
		SourceID:    "session-1",
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

func TestArtifactMetadataAndEventsCommitAtomically(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.Attachments()
	now := time.Now().UTC()
	if err := store.Sessions().Create(ctx, session.Session{ID: "session-1", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	sessionID := eventdomain.SessionID("session-1")
	artifact := session.SessionAttachment{
		ID: "artifact-1", SessionID: "session-1", Role: session.FileRoleArtifact,
		SourceType: session.AttachmentSourceCodex, SourceKey: "source-1", ArtifactKind: session.ArtifactKindImage,
		Filename: "result.png", Path: "/archive/result.png", MimeType: "image/png", PreviewKind: session.PreviewKindImage,
		CreatedAt: now,
	}
	published := eventdomain.DomainEvent{
		ID: "artifact.published:artifact-1", Scope: eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		SessionID: &sessionID, Type: "artifact.published", Payload: map[string]any{"id": "artifact-1"}, CreatedAt: now,
	}
	if err := repo.SaveArtifactWithEvent(ctx, artifact, published); err != nil {
		t.Fatal(err)
	}
	if found, err := repo.FindSessionAttachment(ctx, artifact.ID); err != nil || found.ID != artifact.ID {
		t.Fatalf("stored artifact = %#v err=%v", found, err)
	}
	events, err := store.Events().List(ctx, eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"})
	if err != nil || len(events) != 1 || events[0].Type != "artifact.published" {
		t.Fatalf("published events = %#v err=%v", events, err)
	}
	deletedAt := now.Add(time.Second)
	artifact.DeletedAt = &deletedAt
	deleted := eventdomain.DomainEvent{
		ID: "artifact.deleted:artifact-1", Scope: published.Scope, SessionID: &sessionID,
		Type: "artifact.deleted", Payload: map[string]any{"id": "artifact-1"}, CreatedAt: deletedAt,
	}
	if err := repo.DeleteArtifactWithEvent(ctx, artifact, deleted); err != nil {
		t.Fatal(err)
	}
	if found, err := repo.FindSessionAttachment(ctx, artifact.ID); err != nil || found.DeletedAt == nil {
		t.Fatalf("deleted artifact = %#v err=%v", found, err)
	}

	conflict := published
	conflict.ID = "artifact.published:artifact-conflict"
	conflict.Type = "other.event"
	if err := store.Events().Append(ctx, conflict); err != nil {
		t.Fatal(err)
	}
	artifact.ID = "artifact-conflict"
	artifact.SourceKey = "source-conflict"
	artifact.DeletedAt = nil
	published.ID = "artifact.published:artifact-conflict"
	if err := repo.SaveArtifactWithEvent(ctx, artifact, published); err == nil {
		t.Fatal("conflicting event transaction succeeded")
	}
	if _, err := repo.FindSessionAttachment(ctx, artifact.ID); err == nil {
		t.Fatal("artifact from rolled back transaction exists")
	}
	if card, err := store.Sessions().Find(ctx, "session-1"); err != nil || card.ArtifactCount != 0 {
		t.Fatalf("artifact count after rollback = %d, %v", card.ArtifactCount, err)
	}
}

func TestResolveLatestSessionArtifactsByLogicalPaths(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.Attachments()
	now := time.Now().UTC()
	for _, id := range []session.ID{"session-1", "session-2"} {
		if err := store.Sessions().Create(ctx, session.Session{ID: id, ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	deletedAt := now.Add(3 * time.Second)
	artifacts := []session.SessionFile{
		{ID: "old", SessionID: "session-1", Role: session.FileRoleArtifact, SourceKey: "old", LogicalPath: "reports/result.txt", Filename: "result.txt", CreatedAt: now},
		{ID: "new", SessionID: "session-1", Role: session.FileRoleArtifact, SourceKey: "new", LogicalPath: "reports/result.txt", Filename: "result.txt", CreatedAt: now.Add(time.Second)},
		{ID: "deleted", SessionID: "session-1", Role: session.FileRoleArtifact, SourceKey: "deleted", LogicalPath: "image.png", Filename: "image.png", CreatedAt: now.Add(2 * time.Second), DeletedAt: &deletedAt},
		{ID: "other-session", SessionID: "session-2", Role: session.FileRoleArtifact, SourceKey: "other", LogicalPath: "reports/result.txt", Filename: "result.txt", CreatedAt: now.Add(4 * time.Second)},
	}
	for _, artifact := range artifacts {
		if err := repo.SaveSessionAttachment(ctx, artifact); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := repo.ResolveLatestSessionArtifactsByLogicalPaths(ctx, "session-1", []string{"reports/result.txt"})
	if err != nil || len(latest) != 1 || latest[0].ID != "new" {
		t.Fatalf("latest artifact = %#v err=%v", latest, err)
	}
	if _, err := repo.SoftDeleteArtifact(ctx, "deleted", deletedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SoftDeleteArtifact(ctx, "new", deletedAt); err != nil {
		t.Fatal(err)
	}

	got, err := repo.ResolveLatestSessionArtifactsByLogicalPaths(ctx, "session-1", []string{"image.png", "reports/result.txt", "missing.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "old" {
		t.Fatalf("resolved artifacts = %#v", got)
	}
}

func TestSessionRepositoryMigrateAddsFieldsToExistingTursoSessions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "anycode.db")
	db, err := sql.Open(tursoDriverName, dbPath)
	if err != nil {
		t.Fatalf("open local turso: %v", err)
	}
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `CREATE TABLE sessions (
		id text NOT NULL PRIMARY KEY,
		project_id text NOT NULL,
		requirement text NOT NULL DEFAULT '',
		mode text NOT NULL,
		status text NOT NULL,
		priority text NOT NULL DEFAULT 'medium',
		close_reason text NULL,
		base_branch text NOT NULL DEFAULT '',
		worktree_path text NOT NULL DEFAULT '',
		codex_session_id text NOT NULL DEFAULT '',
		codex_model text NOT NULL DEFAULT '',
		reasoning_effort text NOT NULL DEFAULT '',
		permission_mode text NOT NULL DEFAULT '',
		queued_at datetime NULL,
		queue_kind text NOT NULL DEFAULT '',
		queue_priority text NOT NULL DEFAULT 'medium',
		queue_workflow_run_id text NOT NULL DEFAULT '',
		queue_node_run_id text NOT NULL DEFAULT '',
		queue_prompt text NOT NULL DEFAULT '',
		queue_resume_codex_session_id text NOT NULL DEFAULT '',
		last_run_at datetime NULL,
		created_at datetime NOT NULL,
		updated_at datetime NOT NULL,
		closed_at datetime NULL
	)`); err != nil {
		db.Close()
		t.Fatalf("create legacy sessions table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE process_runs (
		id text NOT NULL PRIMARY KEY,
		session_id text NOT NULL,
		node_run_id text NULL,
		status text NOT NULL,
		pid integer NULL,
		codex_session_id text NOT NULL DEFAULT '',
		resume_of text NULL,
		exit_code integer NULL,
		failure_reason text NOT NULL DEFAULT '',
		started_at datetime NOT NULL,
		finished_at datetime NULL
	)`); err != nil {
		db.Close()
		t.Fatalf("create legacy process runs table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (
		id, project_id, requirement, mode, status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"legacy-session", "project-1", "legacy row", string(session.ModeChat), string(session.StatusStopped), now, now); err != nil {
		db.Close()
		t.Fatalf("insert legacy session: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (
		id, project_id, requirement, mode, status, queue_kind, queue_prompt, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-queued-session", "project-1", "legacy queued row", string(session.ModeChat), string(session.StatusQueued), string(session.QueueKindStart), "legacy prompt", now, now); err != nil {
		db.Close()
		t.Fatalf("insert legacy queued session: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (
		id, project_id, requirement, mode, status, queue_kind, queue_prompt, queue_resume_codex_session_id, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-resume-session", "project-1", "legacy resume row", string(session.ModeChat), string(session.StatusQueued), string(session.QueueKindResume), "resume prompt", "codex-legacy", now, now); err != nil {
		db.Close()
		t.Fatalf("insert legacy resume session: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions (
		id, project_id, requirement, mode, status, queue_kind, queue_prompt, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-retry-session", "project-1", "legacy retry row", string(session.ModeChat), string(session.StatusQueued), string(session.QueueKindStart), "retry prompt", now, now); err != nil {
		db.Close()
		t.Fatalf("insert legacy retry session: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO process_runs (
		id, session_id, status, started_at
	) VALUES (?, ?, ?, ?)`, "legacy-process-run", "legacy-retry-session", "failed", now); err != nil {
		db.Close()
		t.Fatalf("insert legacy process run: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close setup db: %v", err)
	}

	store, err := Open(ctx, OpenOptions{DatabaseURL: dbPath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	found, err := store.Sessions().Find(ctx, "legacy-session")
	if err != nil {
		t.Fatalf("find migrated session: %v", err)
	}
	if found.TodoList.Total() != 0 {
		t.Fatalf("todo list = %#v, want empty", found.TodoList)
	}
	if found.Config.FastMode {
		t.Fatalf("legacy session fast mode = true, want false: %#v", found.Config)
	}
	queued, err := store.Sessions().Find(ctx, "legacy-queued-session")
	if err != nil {
		t.Fatalf("find migrated queued session: %v", err)
	}
	if !queued.Queue.InitialStart || queued.Queue.Prompt != "legacy prompt" {
		t.Fatalf("migrated queued session = %#v", queued)
	}
	resumed, err := store.Sessions().Find(ctx, "legacy-resume-session")
	if err != nil {
		t.Fatalf("find migrated resume session: %v", err)
	}
	if resumed.Queue.InitialStart {
		t.Fatalf("migrated resume session should not be initial: %#v", resumed)
	}
	retried, err := store.Sessions().Find(ctx, "legacy-retry-session")
	if err != nil {
		t.Fatalf("find migrated retry session: %v", err)
	}
	if retried.Queue.InitialStart {
		t.Fatalf("migrated retry session should not be initial: %#v", retried)
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
		got.WorktreeBranch != want.WorktreeBranch ||
		got.WorktreeCleanup.Status != want.WorktreeCleanup.Status ||
		got.WorktreeCleanup.Attempts != want.WorktreeCleanup.Attempts ||
		got.WorktreeCleanup.OwnershipToken != want.WorktreeCleanup.OwnershipToken ||
		!equalTimePtr(got.WorktreeCleanup.OwnershipConfirmedAt, want.WorktreeCleanup.OwnershipConfirmedAt) ||
		got.WorktreeCleanup.ErrorCode != want.WorktreeCleanup.ErrorCode ||
		got.WorktreeCleanup.Error != want.WorktreeCleanup.Error ||
		got.WorktreeCleanup.Retryable != want.WorktreeCleanup.Retryable ||
		got.CodexSessionID != want.CodexSessionID ||
		got.Config != want.Config ||
		got.ArtifactCount != want.ArtifactCount ||
		got.FilesChanged != want.FilesChanged ||
		len(got.TodoList.Items) != len(want.TodoList.Items) {
		t.Fatalf("session mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}
	for index := range want.TodoList.Items {
		if got.TodoList.Items[index] != want.TodoList.Items[index] {
			t.Fatalf("todo item %d mismatch: got=%#v want=%#v", index, got.TodoList.Items[index], want.TodoList.Items[index])
		}
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(*want.LastRunAt) {
		t.Fatalf("last run mismatch: got=%v want=%v", got.LastRunAt, want.LastRunAt)
	}
}

func TestArtifactLatestVersionListAndCount(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Create(ctx, session.Session{ID: "session-1", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusCreated, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	repo := store.Attachments()
	sessionID := eventdomain.SessionID("session-1")
	publish := func(id string, logicalPath string, createdAt time.Time) session.SessionFile {
		t.Helper()
		artifact := session.SessionFile{
			ID: session.SessionFileID(id), SessionID: "session-1", Role: session.FileRoleArtifact, SourceKey: id,
			LogicalPath: logicalPath, Filename: filepath.Base(logicalPath), CreatedAt: createdAt,
		}
		event := eventdomain.DomainEvent{
			ID: eventdomain.ID("artifact.published:" + id), Scope: eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
			SessionID: &sessionID, Type: "artifact.published", CreatedAt: createdAt,
		}
		if err := repo.SaveArtifactWithEvent(ctx, artifact, event); err != nil {
			t.Fatal(err)
		}
		return artifact
	}
	old := publish("old", "reports/result.txt", now)
	latest := publish("latest", "reports/result.txt", now.Add(time.Second))
	publish("image", "image.png", now.Add(2*time.Second))

	card, err := store.Sessions().Find(ctx, "session-1")
	if err != nil || card.ArtifactCount != 2 {
		t.Fatalf("artifact count after publish = %d, %v", card.ArtifactCount, err)
	}
	files, err := repo.ListSessionArtifacts(ctx, session.ArtifactQuery{SessionID: "session-1"})
	if err != nil || len(files) != 2 || files[0].ID != "image" || files[1].ID != latest.ID {
		t.Fatalf("current artifacts = %#v, %v", files, err)
	}

	deleteArtifact := func(artifact session.SessionFile, deletedAt time.Time) {
		t.Helper()
		artifact.DeletedAt = &deletedAt
		event := eventdomain.DomainEvent{
			ID: eventdomain.ID("artifact.deleted:" + string(artifact.ID)), Scope: eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
			SessionID: &sessionID, Type: "artifact.deleted", CreatedAt: deletedAt,
		}
		if err := repo.DeleteArtifactWithEvent(ctx, artifact, event); err != nil {
			t.Fatal(err)
		}
	}
	deleteArtifact(latest, now.Add(3*time.Second))
	card, _ = store.Sessions().Find(ctx, "session-1")
	files, err = repo.ListSessionArtifacts(ctx, session.ArtifactQuery{SessionID: "session-1"})
	if err != nil || card.ArtifactCount != 2 || len(files) != 2 || files[1].ID != old.ID {
		t.Fatalf("artifacts after latest delete: count=%d files=%#v err=%v", card.ArtifactCount, files, err)
	}
	deleteArtifact(old, now.Add(4*time.Second))
	card, _ = store.Sessions().Find(ctx, "session-1")
	if card.ArtifactCount != 1 {
		t.Fatalf("artifact count after final version delete = %d", card.ArtifactCount)
	}
	filtered, err := repo.ListSessionArtifacts(ctx, session.ArtifactQuery{SessionID: "session-1", Filter: "IMAGE", Sort: "filename_asc"})
	if err != nil || len(filtered) != 1 || filtered[0].ID != "image" {
		t.Fatalf("filtered current artifacts = %#v, %v", filtered, err)
	}
	for index := range 105 {
		id := session.SessionFileID(fmt.Sprintf("bulk-%03d", index))
		if err := repo.SaveSessionAttachment(ctx, session.SessionFile{
			ID: id, SessionID: "session-1", Role: session.FileRoleArtifact, SourceKey: string(id),
			LogicalPath: fmt.Sprintf("bulk/%03d.txt", index), Filename: fmt.Sprintf("%03d.txt", index), CreatedAt: now.Add(time.Duration(index+5) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	files, err = repo.ListSessionArtifacts(ctx, session.ArtifactQuery{SessionID: "session-1"})
	card, _ = store.Sessions().Find(ctx, "session-1")
	if err != nil || len(files) != 106 || card.ArtifactCount != 106 {
		t.Fatalf("unpaginated artifacts: count=%d files=%d err=%v", card.ArtifactCount, len(files), err)
	}
}

func TestSessionRepositorySavePreservesCardProjectionCounts(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	stale := session.Session{
		ID: "session-1", ProjectID: "project-1", Requirement: "before", Mode: session.ModeChat,
		Status: session.StatusCreated, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Sessions().Create(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if err := store.Attachments().SaveSessionAttachment(ctx, session.SessionFile{
		ID: "artifact-1", SessionID: stale.ID, Role: session.FileRoleArtifact, SourceKey: "artifact-1",
		LogicalPath: "reports/result.txt", Filename: "result.txt", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Sessions().UpdateFilesChanged(ctx, stale.ID, 4); err != nil {
		t.Fatal(err)
	}

	stale.Requirement = "after"
	stale.UpdatedAt = now.Add(time.Second)
	if err := store.Sessions().Save(ctx, stale); err != nil {
		t.Fatal(err)
	}
	got, err := store.Sessions().Find(ctx, stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Requirement != "after" || got.ArtifactCount != 1 || got.FilesChanged != 4 {
		t.Fatalf("session after stale save = %#v", got)
	}
	if err := store.Sessions().UpdateFilesChanged(ctx, stale.ID, -1); err == nil {
		t.Fatal("UpdateFilesChanged(-1) expected error")
	}
	got, err = store.Sessions().Find(ctx, stale.ID)
	if err != nil || got.FilesChanged != 4 {
		t.Fatalf("files changed after rejected update = %d, %v", got.FilesChanged, err)
	}
}

func equalTimePtr(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
