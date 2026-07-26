package entstore

import (
	"context"
	"fmt"

	"github.com/nzlov/anycode/internal/domain/mindmap"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entoverlay "github.com/nzlov/anycode/internal/infra/entstore/ent/mindmapoverlay"
	enttask "github.com/nzlov/anycode/internal/infra/entstore/ent/mindmaptask"
)

type MindMapRepository struct {
	client *ent.Client
}

func NewMindMapRepository(client *ent.Client) *MindMapRepository {
	return &MindMapRepository{client: client}
}

func (r *MindMapRepository) FindGraph(ctx context.Context, projectID mindmap.ProjectID) (mindmap.Graph, bool, error) {
	row, err := r.client.MindMapGraph.Get(ctx, string(projectID))
	if ent.IsNotFound(err) {
		return mindmap.Graph{ProjectID: projectID}, false, nil
	}
	if err != nil {
		return mindmap.Graph{}, false, fmt.Errorf("find mind map graph: %w", err)
	}
	nodes, err := decodeMindMapJSON[mindmap.Node](row.Nodes)
	if err != nil {
		return mindmap.Graph{}, false, fmt.Errorf("decode mind map nodes: %w", err)
	}
	edges, err := decodeMindMapJSON[mindmap.Edge](row.Edges)
	if err != nil {
		return mindmap.Graph{}, false, fmt.Errorf("decode mind map edges: %w", err)
	}
	history, err := decodeMindMapJSON[mindmap.Change](row.History)
	if err != nil {
		return mindmap.Graph{}, false, fmt.Errorf("decode mind map history: %w", err)
	}
	return mindmap.Graph{ProjectID: projectID, Nodes: nodes, Edges: edges, History: history, UpdatedAt: row.UpdatedAt}, true, nil
}

func (r *MindMapRepository) SaveGraph(ctx context.Context, graph mindmap.Graph) error {
	nodes, err := encodeMindMapJSON(graph.Nodes)
	if err != nil {
		return fmt.Errorf("encode mind map nodes: %w", err)
	}
	edges, err := encodeMindMapJSON(graph.Edges)
	if err != nil {
		return fmt.Errorf("encode mind map edges: %w", err)
	}
	history, err := encodeMindMapJSON(graph.History)
	if err != nil {
		return fmt.Errorf("encode mind map history: %w", err)
	}
	update := r.client.MindMapGraph.UpdateOneID(string(graph.ProjectID)).
		SetNodes(nodes).SetEdges(edges).SetHistory(history)
	if !graph.UpdatedAt.IsZero() {
		update.SetUpdatedAt(graph.UpdatedAt)
	}
	if err := update.Exec(ctx); err == nil {
		return nil
	} else if !ent.IsNotFound(err) {
		return fmt.Errorf("update mind map graph: %w", err)
	}
	create := r.client.MindMapGraph.Create().SetID(string(graph.ProjectID)).SetNodes(nodes).SetEdges(edges).SetHistory(history)
	if !graph.UpdatedAt.IsZero() {
		create.SetUpdatedAt(graph.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create mind map graph: %w", err)
	}
	return nil
}

func (r *MindMapRepository) FindOverlay(ctx context.Context, sessionID mindmap.SessionID) (mindmap.Overlay, bool, error) {
	row, err := r.client.MindMapOverlay.Get(ctx, string(sessionID))
	if ent.IsNotFound(err) {
		return mindmap.Overlay{SessionID: sessionID}, false, nil
	}
	if err != nil {
		return mindmap.Overlay{}, false, fmt.Errorf("find mind map overlay: %w", err)
	}
	overlay, err := toDomainMindMapOverlay(row)
	return overlay, true, err
}

func (r *MindMapRepository) ListOverlays(ctx context.Context, projectID mindmap.ProjectID) ([]mindmap.Overlay, error) {
	rows, err := r.client.MindMapOverlay.Query().
		Where(entoverlay.ProjectIDEQ(string(projectID))).
		Order(ent.Desc(entoverlay.FieldUpdatedAt)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mind map overlays: %w", err)
	}
	result := make([]mindmap.Overlay, 0, len(rows))
	for _, row := range rows {
		overlay, err := toDomainMindMapOverlay(row)
		if err != nil {
			return nil, err
		}
		result = append(result, overlay)
	}
	return result, nil
}

func (r *MindMapRepository) SaveOverlay(ctx context.Context, overlay mindmap.Overlay) error {
	changes, err := encodeMindMapJSON(overlay.Changes)
	if err != nil {
		return fmt.Errorf("encode mind map changes: %w", err)
	}
	update := r.client.MindMapOverlay.UpdateOneID(string(overlay.SessionID)).
		SetProjectID(string(overlay.ProjectID)).SetChanges(changes)
	if !overlay.UpdatedAt.IsZero() {
		update.SetUpdatedAt(overlay.UpdatedAt)
	}
	if err := update.Exec(ctx); err == nil {
		return nil
	} else if !ent.IsNotFound(err) {
		return fmt.Errorf("update mind map overlay: %w", err)
	}
	create := r.client.MindMapOverlay.Create().SetID(string(overlay.SessionID)).SetProjectID(string(overlay.ProjectID)).SetChanges(changes)
	if !overlay.UpdatedAt.IsZero() {
		create.SetUpdatedAt(overlay.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create mind map overlay: %w", err)
	}
	return nil
}

func (r *MindMapRepository) DeleteOverlay(ctx context.Context, sessionID mindmap.SessionID) error {
	if err := r.client.MindMapOverlay.DeleteOneID(string(sessionID)).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("delete mind map overlay: %w", err)
	}
	return nil
}

func (r *MindMapRepository) SaveTask(ctx context.Context, task mindmap.Task) error {
	update := r.client.MindMapTask.UpdateOneID(string(task.ID)).
		SetProjectID(string(task.ProjectID)).SetSessionID(string(task.SessionID)).SetStatus(string(task.Status)).
		SetProcessRunID(task.ProcessRunID).SetAttempts(task.Attempts).SetError(task.Error)
	setMindMapTaskTimesOnUpdate(update, task)
	if err := update.Exec(ctx); err == nil {
		return nil
	} else if !ent.IsNotFound(err) {
		return fmt.Errorf("update mind map task: %w", err)
	}
	create := r.client.MindMapTask.Create().SetID(string(task.ID)).SetProjectID(string(task.ProjectID)).
		SetSessionID(string(task.SessionID)).SetStatus(string(task.Status)).SetProcessRunID(task.ProcessRunID).
		SetAttempts(task.Attempts).SetError(task.Error)
	if !task.CreatedAt.IsZero() {
		create.SetCreatedAt(task.CreatedAt)
	}
	if task.StartedAt != nil {
		create.SetStartedAt(*task.StartedAt)
	}
	if task.FinishedAt != nil {
		create.SetFinishedAt(*task.FinishedAt)
	}
	if !task.UpdatedAt.IsZero() {
		create.SetUpdatedAt(task.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create mind map task: %w", err)
	}
	return nil
}

func (r *MindMapRepository) FindTask(ctx context.Context, id mindmap.TaskID) (mindmap.Task, error) {
	row, err := r.client.MindMapTask.Get(ctx, string(id))
	if err != nil {
		return mindmap.Task{}, fmt.Errorf("find mind map task: %w", err)
	}
	return toDomainMindMapTask(row), nil
}

func (r *MindMapRepository) FindTaskBySession(ctx context.Context, sessionID mindmap.SessionID) (mindmap.Task, bool, error) {
	row, err := r.client.MindMapTask.Query().Where(enttask.SessionIDEQ(string(sessionID))).First(ctx)
	if ent.IsNotFound(err) {
		return mindmap.Task{}, false, nil
	}
	if err != nil {
		return mindmap.Task{}, false, fmt.Errorf("find mind map task by session: %w", err)
	}
	return toDomainMindMapTask(row), true, nil
}

func (r *MindMapRepository) ListTasks(ctx context.Context, projectID mindmap.ProjectID) ([]mindmap.Task, error) {
	query := r.client.MindMapTask.Query()
	if projectID != "" {
		query.Where(enttask.ProjectIDEQ(string(projectID)))
	}
	rows, err := query.Order(ent.Desc(enttask.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mind map tasks: %w", err)
	}
	result := make([]mindmap.Task, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDomainMindMapTask(row))
	}
	return result, nil
}

func (r *MindMapRepository) ListQueuedTasks(ctx context.Context, limit int) ([]mindmap.Task, error) {
	query := r.client.MindMapTask.Query().Where(enttask.StatusEQ(string(mindmap.TaskQueued))).Order(ent.Asc(enttask.FieldCreatedAt))
	if limit > 0 {
		query.Limit(limit)
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list queued mind map tasks: %w", err)
	}
	result := make([]mindmap.Task, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDomainMindMapTask(row))
	}
	return result, nil
}

func (r *MindMapRepository) CountRunningTasks(ctx context.Context) (int, error) {
	count, err := r.client.MindMapTask.Query().Where(enttask.StatusEQ(string(mindmap.TaskRunning))).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count running mind map tasks: %w", err)
	}
	return count, nil
}

func toDomainMindMapOverlay(row *ent.MindMapOverlay) (mindmap.Overlay, error) {
	changes, err := decodeMindMapJSON[mindmap.Change](row.Changes)
	if err != nil {
		return mindmap.Overlay{}, fmt.Errorf("decode mind map changes: %w", err)
	}
	return mindmap.Overlay{ProjectID: mindmap.ProjectID(row.ProjectID), SessionID: mindmap.SessionID(row.ID), Changes: changes, UpdatedAt: row.UpdatedAt}, nil
}

func toDomainMindMapTask(row *ent.MindMapTask) mindmap.Task {
	return mindmap.Task{
		ID: mindmap.TaskID(row.ID), ProjectID: mindmap.ProjectID(row.ProjectID), SessionID: mindmap.SessionID(row.SessionID),
		Status: mindmap.TaskStatus(row.Status), ProcessRunID: row.ProcessRunID, Attempts: row.Attempts, Error: row.Error,
		CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, UpdatedAt: row.UpdatedAt,
	}
}

func setMindMapTaskTimesOnUpdate(update *ent.MindMapTaskUpdateOne, task mindmap.Task) {
	if task.StartedAt != nil {
		update.SetStartedAt(*task.StartedAt)
	} else {
		update.ClearStartedAt()
	}
	if task.FinishedAt != nil {
		update.SetFinishedAt(*task.FinishedAt)
	} else {
		update.ClearFinishedAt()
	}
	if !task.UpdatedAt.IsZero() {
		update.SetUpdatedAt(task.UpdatedAt)
	}
}

func encodeMindMapJSON[T any](items []T) ([]map[string]any, error) {
	var raw []map[string]any
	if err := roundTripJSON(items, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return []map[string]any{}, nil
	}
	return raw, nil
}

func decodeMindMapJSON[T any](raw []map[string]any) ([]T, error) {
	var items []T
	if err := roundTripJSON(raw, &items); err != nil {
		return nil, err
	}
	if items == nil {
		return []T{}, nil
	}
	return items, nil
}
