package entstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/domain/session"
)

func TestDatabaseTargetForOptions(t *testing.T) {
	tests := []struct {
		name       string
		opts       OpenOptions
		wantDriver string
		wantURL    string
		wantToken  string
		wantErr    string
	}{
		{
			name:    "legacy file URL is rejected",
			opts:    OpenOptions{DatabaseURL: "file:/tmp/anycode.db"},
			wantErr: "file: URLs are not supported",
		},
		{
			name:       "local path uses turso",
			opts:       OpenOptions{DatabaseURL: "/tmp/anycode.db"},
			wantDriver: tursoDriverName,
			wantURL:    "/tmp/anycode.db",
		},
		{
			name:       "empty URL uses data directory",
			opts:       OpenOptions{DataDir: "/tmp/anycode-data"},
			wantDriver: tursoDriverName,
			wantURL:    "/tmp/anycode-data/anycode.turso.db",
		},
		{
			name: "remote turso uses libsql",
			opts: OpenOptions{
				DatabaseURL: "libsql://anycode-example.turso.io",
				AuthToken:   "secret-token",
			},
			wantDriver: libsqlDriverName,
			wantURL:    "libsql://anycode-example.turso.io",
			wantToken:  "secret-token",
		},
		{
			name: "remote scheme is case insensitive",
			opts: OpenOptions{
				DatabaseURL: "LIBSQL://anycode-example.turso.io",
				AuthToken:   "secret-token",
			},
			wantDriver: libsqlDriverName,
			wantURL:    "libsql://anycode-example.turso.io",
			wantToken:  "secret-token",
		},
		{
			name:    "remote turso requires token",
			opts:    OpenOptions{DatabaseURL: "https://anycode-example.turso.io"},
			wantErr: "TURSO_AUTH_TOKEN is required",
		},
		{
			name:    "insecure remote URL is rejected",
			opts:    OpenOptions{DatabaseURL: "http://anycode-example.turso.io", AuthToken: "secret-token"},
			wantErr: "insecure http database URL is not supported",
		},
		{
			name:    "unknown scheme is rejected",
			opts:    OpenOptions{DatabaseURL: "postgres://database.example/anycode"},
			wantErr: "unsupported database URL scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := databaseTargetForOptions(tt.opts)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("databaseTargetForOptions() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("databaseTargetForOptions() error = %v", err)
			}
			if target.DriverName != tt.wantDriver || target.DatabaseURL != tt.wantURL || target.AuthToken != tt.wantToken {
				t.Fatalf("databaseTargetForOptions() = %#v", target)
			}
		})
	}
}

func TestOpenCreatesLocalTursoDataDir(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "nested", "data")
	store, err := Open(ctx, OpenOptions{DataDir: dataDir})
	if err != nil {
		t.Fatalf("open local Turso store: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(filepath.Join(dataDir, "anycode.turso.db")); err != nil {
		t.Fatalf("stat local Turso database: %v", err)
	}
}

func TestMigrateDropsSupersededWorkflowStorage(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("create current schema: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE workflow_runs (id text PRIMARY KEY)`,
		`CREATE TABLE codex_transcript_sources (id integer PRIMARY KEY)`,
		`ALTER TABLE node_runs ADD COLUMN workflow_run_id text NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN queue_workflow_run_id text NOT NULL DEFAULT ''`,
		`ALTER TABLE question_batches ADD COLUMN workflow_run_id text NULL`,
		`ALTER TABLE event_records ADD COLUMN workflow_run_id text NOT NULL DEFAULT ''`,
		`INSERT INTO workflow_runs (id) VALUES ('workflow-run-1')`,
	} {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("prepare superseded storage with %q: %v", statement, err)
		}
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	for _, table := range []string{"workflow_runs", "codex_transcript_sources"} {
		var count int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("inspect table %q: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("superseded table %q still exists", table)
		}
	}
	for _, item := range []struct {
		table  string
		column string
	}{
		{table: "node_runs", column: "workflow_run_id"},
		{table: "sessions", column: "queue_workflow_run_id"},
		{table: "question_batches", column: "workflow_run_id"},
		{table: "event_records", column: "workflow_run_id"},
	} {
		if exists, err := store.columnExists(ctx, item.table, item.column); err != nil || exists {
			t.Fatalf("%s.%s exists = %v, error = %v", item.table, item.column, exists, err)
		}
	}
	if exists, err := store.columnExists(ctx, "node_runs", "session_id"); err != nil || !exists {
		t.Fatalf("node_runs.session_id exists = %v, error = %v", exists, err)
	}
}

func TestProjectRepositoryWithLocalTurso(t *testing.T) {
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

	repo := store.Projects()
	createdAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	input := project.Project{
		ID:                  project.ID("project-1"),
		Name:                "AnyCode",
		Path:                project.ProjectPath{Value: "/workspaces/anycode"},
		IsGit:               true,
		WorktreeInitCommand: "echo first\necho second\n",
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}
	if err := repo.Save(ctx, input); err != nil {
		t.Fatalf("save project: %v", err)
	}

	found, err := repo.Find(ctx, input.ID)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	if found.ID != input.ID || found.Name != input.Name || found.Path.Value != input.Path.Value || !found.IsGit || found.WorktreeInitCommand != input.WorktreeInitCommand {
		t.Fatalf("found project mismatch: %#v", found)
	}

	projects, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != input.ID {
		t.Fatalf("list projects mismatch: %#v", projects)
	}

	workflowID := project.WorkflowDefinitionID("workflow-1")
	if err := repo.UpdateDefaultWorkflow(ctx, input.ID, workflowID); err != nil {
		t.Fatalf("update default workflow: %v", err)
	}
	found, err = repo.Find(ctx, input.ID)
	if err != nil {
		t.Fatalf("find project after update: %v", err)
	}
	if found.DefaultWorkflowID == nil || *found.DefaultWorkflowID != workflowID {
		t.Fatalf("default workflow mismatch: %#v", found.DefaultWorkflowID)
	}
}

func TestUnitOfWorkCommitsAndRollsBackRepositories(t *testing.T) {
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

	rollbackErr := errors.New("rollback")
	err = store.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		if err := tx.Projects().Save(ctx, project.Project{
			ID:        "project-rollback",
			Name:      "rollback",
			Path:      project.ProjectPath{Value: "/workspaces/rollback"},
			CreatedAt: time.Unix(1, 0).UTC(),
			UpdatedAt: time.Unix(1, 0).UTC(),
		}); err != nil {
			return err
		}
		if err := tx.Events().Append(ctx, eventdomain.DomainEvent{
			ID:        "event-rollback",
			Scope:     eventdomain.Scope{ProjectID: "project-rollback"},
			Type:      "project.rollback",
			Payload:   map[string]any{"status": "rollback"},
			CreatedAt: time.Unix(1, 0).UTC(),
		}); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("Do rollback error = %v", err)
	}
	if projects, err := store.Projects().List(ctx); err != nil {
		t.Fatalf("list projects after rollback: %v", err)
	} else if len(projects) != 0 {
		t.Fatalf("projects after rollback = %#v", projects)
	}
	if events, err := store.Events().After(ctx, eventdomain.Scope{ProjectID: "project-rollback"}, ""); err != nil {
		t.Fatalf("list events after rollback: %v", err)
	} else if len(events) != 0 {
		t.Fatalf("events after rollback = %#v", events)
	}

	err = store.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		if err := tx.Projects().Save(ctx, project.Project{
			ID:        "project-commit",
			Name:      "commit",
			Path:      project.ProjectPath{Value: "/workspaces/commit"},
			CreatedAt: time.Unix(2, 0).UTC(),
			UpdatedAt: time.Unix(2, 0).UTC(),
		}); err != nil {
			return err
		}
		return tx.Events().Append(ctx, eventdomain.DomainEvent{
			ID:        "event-commit",
			Scope:     eventdomain.Scope{ProjectID: "project-commit"},
			Type:      "project.commit",
			Payload:   map[string]any{"status": "commit"},
			CreatedAt: time.Unix(2, 0).UTC(),
		})
	})
	if err != nil {
		t.Fatalf("Do commit error = %v", err)
	}
	if projects, err := store.Projects().List(ctx); err != nil {
		t.Fatalf("list projects after commit: %v", err)
	} else if len(projects) != 1 || projects[0].ID != "project-commit" {
		t.Fatalf("projects after commit = %#v", projects)
	}
	if events, err := store.Events().After(ctx, eventdomain.Scope{ProjectID: "project-commit"}, ""); err != nil {
		t.Fatalf("list events after commit: %v", err)
	} else if len(events) != 1 || events[0].ID != "event-commit" {
		t.Fatalf("events after commit = %#v", events)
	}
}

func TestUnitOfWorkDeletesSessionArtifactsAndPreservesInputs(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(10, 0).UTC()
	card := session.Session{
		ID: "session-1", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusCreated,
		ArtifactCount: 2, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Sessions().Create(ctx, card); err != nil {
		t.Fatal(err)
	}
	files := []session.SessionFile{
		{ID: "artifact-1", SessionID: card.ID, Role: session.FileRoleArtifact, SourceType: session.AttachmentSourceCodex, SourceID: "run-1", SourceKey: "source-1", LogicalPath: "one.txt", Filename: "one.txt", Path: "/archive/one.txt", CreatedAt: now},
		{ID: "artifact-2", SessionID: card.ID, Role: session.FileRoleArtifact, SourceType: session.AttachmentSourceCodex, SourceID: "run-1", SourceKey: "source-2", LogicalPath: "two.txt", Filename: "two.txt", Path: "/archive/two.txt", CreatedAt: now.Add(time.Second)},
		{ID: "input-1", SessionID: card.ID, Role: session.FileRoleInput, SourceType: session.AttachmentSourceRequirement, SourceID: "requirement", Filename: "one.txt", Path: "/inputs/one.txt", CreatedAt: now.Add(2 * time.Second)},
	}
	for _, file := range files {
		if err := store.Attachments().SaveSessionAttachment(ctx, file); err != nil {
			t.Fatal(err)
		}
	}

	deletedAt := now.Add(time.Minute)
	rollbackErr := errors.New("rollback artifact cleanup")
	err = store.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		result, err := tx.DeleteSessionArtifacts(ctx, port.DeleteSessionArtifactsInput{SessionID: card.ID, DeletedAt: deletedAt})
		if err != nil {
			return err
		}
		if len(result.Artifacts) != 2 {
			t.Fatalf("deleted artifacts = %#v", result.Artifacts)
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback error = %v", err)
	}
	for _, id := range []session.SessionFileID{"artifact-1", "artifact-2"} {
		found, err := store.Attachments().FindSessionAttachment(ctx, id)
		if err != nil || found.DeletedAt != nil {
			t.Fatalf("artifact after rollback = %#v err=%v", found, err)
		}
	}
	if found, err := store.Sessions().Find(ctx, card.ID); err != nil || found.ArtifactCount != 2 {
		t.Fatalf("session after rollback = %#v err=%v", found, err)
	}

	var deleted []session.SessionFile
	err = store.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		result, err := tx.DeleteSessionArtifacts(ctx, port.DeleteSessionArtifactsInput{SessionID: card.ID, DeletedAt: deletedAt})
		deleted = result.Artifacts
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 || deleted[0].DeletedAt == nil || deleted[1].DeletedAt == nil {
		t.Fatalf("deleted artifacts = %#v", deleted)
	}
	for _, id := range []session.SessionFileID{"artifact-1", "artifact-2"} {
		found, err := store.Attachments().FindSessionAttachment(ctx, id)
		if err != nil || found.DeletedAt == nil || !found.DeletedAt.Equal(deletedAt) {
			t.Fatalf("deleted artifact = %#v err=%v", found, err)
		}
	}
	inputs, err := store.Attachments().ListSessionAttachments(ctx, card.ID)
	if err != nil || len(inputs) != 1 || inputs[0].ID != "input-1" || inputs[0].DeletedAt != nil {
		t.Fatalf("inputs after cleanup = %#v err=%v", inputs, err)
	}
	if found, err := store.Sessions().Find(ctx, card.ID); err != nil || found.ArtifactCount != 0 {
		t.Fatalf("session after cleanup = %#v err=%v", found, err)
	}
}
