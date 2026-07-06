package entstore

import (
	"context"
	"fmt"
	"time"

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
	_, err := r.client.Project.Get(ctx, string(p.ID))
	if err == nil {
		update := r.client.Project.UpdateOneID(string(p.ID)).
			SetName(p.Name).
			SetPath(p.Path.Value).
			SetIsGit(p.IsGit)
		if p.DefaultWorkflowID != nil {
			update.SetDefaultWorkflowID(string(*p.DefaultWorkflowID))
		} else {
			update.ClearDefaultWorkflowID()
		}
		if !p.UpdatedAt.IsZero() {
			update.SetUpdatedAt(p.UpdatedAt)
		}
		if p.RemovedAt != nil {
			update.SetRemovedAt(*p.RemovedAt)
		} else {
			update.ClearRemovedAt()
		}
		if err := update.Exec(ctx); err != nil {
			return fmt.Errorf("save project: %w", err)
		}
		return nil
	}
	if !ent.IsNotFound(err) {
		return fmt.Errorf("find project before save: %w", err)
	}
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
	if p.RemovedAt != nil {
		create.SetRemovedAt(*p.RemovedAt)
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

func (r *ProjectRepository) FindByPath(ctx context.Context, path string) (project.Project, bool, error) {
	row, err := r.client.Project.Query().
		Where(entproject.PathEQ(path)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return project.Project{}, false, nil
		}
		return project.Project{}, false, fmt.Errorf("find project by path: %w", err)
	}
	return toDomainProject(row), true, nil
}

func (r *ProjectRepository) List(ctx context.Context) ([]project.Project, error) {
	rows, err := r.client.Project.Query().
		Where(entproject.RemovedAtIsNil()).
		Order(ent.Asc(entproject.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	projects := make([]project.Project, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, toDomainProject(row))
	}
	return projects, nil
}

func (r *ProjectRepository) Remove(ctx context.Context, id project.ID, removedAt time.Time) error {
	if err := r.client.Project.UpdateOneID(string(id)).
		SetRemovedAt(removedAt).
		Exec(ctx); err != nil {
		return fmt.Errorf("remove project: %w", err)
	}
	return nil
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
		RemovedAt:         row.RemovedAt,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}
