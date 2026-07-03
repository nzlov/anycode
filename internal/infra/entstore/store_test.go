package entstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/project"
)

func TestProjectRepositoryWithLocalSQLite(t *testing.T) {
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
		ID:        project.ID("project-1"),
		Name:      "AnyCode",
		Path:      project.ProjectPath{Value: "/workspaces/anycode"},
		IsGit:     true,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if err := repo.Save(ctx, input); err != nil {
		t.Fatalf("save project: %v", err)
	}

	found, err := repo.Find(ctx, input.ID)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	if found.ID != input.ID || found.Name != input.Name || found.Path.Value != input.Path.Value || !found.IsGit {
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
