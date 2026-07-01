package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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
