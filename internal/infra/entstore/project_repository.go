package entstore

import (
	"context"
	"fmt"

	"github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entproject "github.com/nzlov/anycode/internal/infra/entstore/ent/project"
)

type ProjectRepository struct {
	client *ent.Client
}

func NewProjectRepository(client *ent.Client) *ProjectRepository {
	return &ProjectRepository{client: client}
}

func (r *ProjectRepository) Save(ctx context.Context, p project.Project) error {
	create := r.client.Project.Create().
		SetID(string(p.ID)).
		SetName(p.Name).
		SetPath(p.Path.Value).
		SetIsGit(p.IsGit)
	if p.DefaultWorkflowID != nil {
		create.SetDefaultWorkflowID(string(*p.DefaultWorkflowID))
	}
	if !p.CreatedAt.IsZero() {
		create.SetCreatedAt(p.CreatedAt)
	}
	if !p.UpdatedAt.IsZero() {
		create.SetUpdatedAt(p.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("save project: %w", err)
	}
	return nil
}

func (r *ProjectRepository) Find(ctx context.Context, id project.ID) (project.Project, error) {
	row, err := r.client.Project.Get(ctx, string(id))
	if err != nil {
		return project.Project{}, fmt.Errorf("find project: %w", err)
	}
	return toDomainProject(row), nil
}

func (r *ProjectRepository) List(ctx context.Context) ([]project.Project, error) {
	rows, err := r.client.Project.Query().Order(ent.Asc(entproject.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	projects := make([]project.Project, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, toDomainProject(row))
	}
	return projects, nil
}

func (r *ProjectRepository) UpdateDefaultWorkflow(ctx context.Context, id project.ID, workflowID project.WorkflowDefinitionID) error {
	if err := r.client.Project.UpdateOneID(string(id)).
		SetDefaultWorkflowID(string(workflowID)).
		Exec(ctx); err != nil {
		return fmt.Errorf("update project default workflow: %w", err)
	}
	return nil
}

func toDomainProject(row *ent.Project) project.Project {
	var defaultWorkflowID *project.WorkflowDefinitionID
	if row.DefaultWorkflowID != nil {
		id := project.WorkflowDefinitionID(*row.DefaultWorkflowID)
		defaultWorkflowID = &id
	}
	return project.Project{
		ID:                project.ID(row.ID),
		Name:              row.Name,
		Path:              project.ProjectPath{Value: row.Path},
		IsGit:             row.IsGit,
		DefaultWorkflowID: defaultWorkflowID,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
