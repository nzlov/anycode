package entstore

import (
	"context"
	"database/sql"
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
		TodoList: session.TodoList{Items: []session.TodoItem{
			{Text: "梳理需求", Completed: true},
			{Text: "实现卡片展示", Completed: false},
		}},
		Queue: session.QueueIntent{
			Kind:            session.QueueKindStart,
			InitialStart:    true,
			RecoveryBatchID: "batch-1",
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
	if !found.Queue.InitialStart {
		t.Fatalf("queue initial start = false, want true: %#v", found.Queue)
	}
	if found.Queue.RecoveryBatchID != "batch-1" {
		t.Fatalf("queue recovery batch id = %q", found.Queue.RecoveryBatchID)
	}

	updatedAt := now.Add(time.Minute)
	input.Status = session.StatusStopped
	input.Config.CodexModel = "gpt-5.4-mini"
	input.Queue.InitialStart = false
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
	if config.CodexModel != "gpt-5.4-last" || config.ReasoningEffort != "medium" || config.PermissionMode != "read-only" {
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

func TestSessionRepositoryMigrateAddsFieldsToExistingSessions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "anycode.db")
	db, err := sql.Open(sqliteDriverName, dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
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
		got.CodexSessionID != want.CodexSessionID ||
		got.Config != want.Config ||
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
