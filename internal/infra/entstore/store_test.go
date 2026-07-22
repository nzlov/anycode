package entstore

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
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

func TestMigrateRemovesSessionAttachmentsAndPreservesInputFiles(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := Open(ctx, OpenOptions{
		DatabaseURL: filepath.Join(dataDir, "anycode.db"),
		DataDir:     dataDir,
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("create current schema: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE session_attachments (
		id text PRIMARY KEY,
		session_id text NOT NULL,
		role text NOT NULL,
		source_type text NOT NULL,
		source_id text NOT NULL,
		path text NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy session attachments: %v", err)
	}

	legacyRoot := filepath.Join(dataDir, "attachments", "sessions", "session-1")
	requirementPath := filepath.Join(legacyRoot, "requirement-file", "requirement.txt")
	appendPath := filepath.Join(legacyRoot, "append-file", "append.txt")
	artifactPath := filepath.Join(legacyRoot, "artifact-file", "result.txt")
	outsideRoot := filepath.Join(t.TempDir(), "sessions", "session-1")
	outsideInputPath := filepath.Join(outsideRoot, "outside-input", "input.txt")
	outsideArtifactPath := filepath.Join(outsideRoot, "outside-artifact", "artifact.txt")
	for path, body := range map[string]string{
		requirementPath:     "requirement",
		appendPath:          "append",
		artifactPath:        "legacy artifact copy",
		outsideInputPath:    "outside input",
		outsideArtifactPath: "outside artifact",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		id, role, sourceType, sourceID, path string
	}{
		{id: "requirement-file", role: string(sessiondomain.FileRoleInput), sourceType: string(sessiondomain.AttachmentSourceRequirement), sourceID: "session-1", path: requirementPath},
		{id: "append-file", role: string(sessiondomain.FileRoleInput), sourceType: string(sessiondomain.AttachmentSourcePromptAppend), sourceID: "append-1", path: appendPath},
		{id: "artifact-file", role: string(sessiondomain.FileRoleArtifact), sourceType: string(sessiondomain.AttachmentSourceCodex), sourceID: "run-1", path: artifactPath},
		{id: "outside-input", role: string(sessiondomain.FileRoleInput), sourceType: string(sessiondomain.AttachmentSourceRequirement), sourceID: "session-1", path: outsideInputPath},
		{id: "outside-artifact", role: string(sessiondomain.FileRoleArtifact), sourceType: string(sessiondomain.AttachmentSourceCodex), sourceID: "run-1", path: outsideArtifactPath},
	} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO session_attachments
			(id, session_id, role, source_type, source_id, path) VALUES (?, 'session-1', ?, ?, ?, ?)`,
			row.id, row.role, row.sourceType, row.sourceID, row.path); err != nil {
			t.Fatalf("insert legacy attachment %s: %v", row.id, err)
		}
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	if exists, err := store.tableExists(ctx, "session_attachments"); err != nil || exists {
		t.Fatalf("session_attachments exists = %v, error = %v", exists, err)
	}
	for _, migrated := range []struct {
		path string
		body string
	}{
		{
			path: filepath.Join(legacyRoot, string(sessiondomain.AttachmentSourceRequirement), base64.RawURLEncoding.EncodeToString([]byte("session-1")), "requirement-file", "requirement.txt"),
			body: "requirement",
		},
		{
			path: filepath.Join(legacyRoot, string(sessiondomain.AttachmentSourcePromptAppend), base64.RawURLEncoding.EncodeToString([]byte("append-1")), "append-file", "append.txt"),
			body: "append",
		},
	} {
		body, err := os.ReadFile(migrated.path)
		if err != nil || string(body) != migrated.body {
			t.Fatalf("migrated file %q = %q, %v", migrated.path, body, err)
		}
	}
	if _, err := os.Stat(filepath.Dir(artifactPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy artifact copy still exists: %v", err)
	}
	for path, want := range map[string]string{outsideInputPath: "outside input", outsideArtifactPath: "outside artifact"} {
		body, err := os.ReadFile(path)
		if err != nil || string(body) != want {
			t.Fatalf("outside file %q = %q, %v", path, body, err)
		}
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
