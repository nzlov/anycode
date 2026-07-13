package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entnoderun "github.com/nzlov/anycode/internal/infra/entstore/ent/noderun"
	entworkflowdefinition "github.com/nzlov/anycode/internal/infra/entstore/ent/workflowdefinition"
	entworkflowrun "github.com/nzlov/anycode/internal/infra/entstore/ent/workflowrun"
)

var _ workflow.Repository = (*WorkflowRepository)(nil)

type WorkflowRepository struct {
	client *ent.Client
	inTx   bool
}

func NewWorkflowRepository(client *ent.Client) *WorkflowRepository {
	return &WorkflowRepository{client: client}
}

func newWorkflowRepositoryInTx(client *ent.Client) *WorkflowRepository {
	return &WorkflowRepository{client: client, inTx: true}
}

func (r *WorkflowRepository) SaveDefinition(ctx context.Context, definition workflow.Definition) error {
	graph, err := graphToMap(definition.Graph)
	if err != nil {
		return err
	}
	create := r.client.WorkflowDefinition.Create().
		SetID(string(definition.ID)).
		SetProjectID(string(definition.ProjectID)).
		SetName(definition.Name).
		SetVersion(definition.Version).
		SetGraph(graph).
		SetActive(definition.Active)
	if !definition.CreatedAt.IsZero() {
		create.SetCreatedAt(definition.CreatedAt)
	}
	if !definition.UpdatedAt.IsZero() {
		create.SetUpdatedAt(definition.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("save workflow definition: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) FindDefinition(ctx context.Context, id workflow.DefinitionID) (workflow.Definition, error) {
	row, err := r.client.WorkflowDefinition.Get(ctx, string(id))
	if err != nil {
		return workflow.Definition{}, fmt.Errorf("find workflow definition: %w", err)
	}
	definition, err := toDomainWorkflowDefinition(row)
	if err != nil {
		return workflow.Definition{}, err
	}
	return definition, nil
}

func (r *WorkflowRepository) FindActive(ctx context.Context, projectID workflow.ProjectID) (workflow.Definition, error) {
	row, err := r.client.WorkflowDefinition.Query().
		Where(
			entworkflowdefinition.ProjectIDEQ(string(projectID)),
			entworkflowdefinition.Active(true),
		).
		Order(ent.Desc(entworkflowdefinition.FieldUpdatedAt), ent.Desc(entworkflowdefinition.FieldID)).
		First(ctx)
	if err != nil {
		return workflow.Definition{}, fmt.Errorf("find active workflow definition: %w", err)
	}
	definition, err := toDomainWorkflowDefinition(row)
	if err != nil {
		return workflow.Definition{}, err
	}
	return definition, nil
}

func (r *WorkflowRepository) FindRun(ctx context.Context, id workflow.RunID) (workflow.Run, error) {
	row, err := r.client.WorkflowRun.Get(ctx, string(id))
	if err != nil {
		return workflow.Run{}, fmt.Errorf("find workflow run: %w", err)
	}
	return toDomainWorkflowRun(row), nil
}

func (r *WorkflowRepository) FindLatestRunBySession(ctx context.Context, sessionID workflow.SessionID) (workflow.Run, error) {
	row, err := r.client.WorkflowRun.Query().
		Where(entworkflowrun.SessionIDEQ(string(sessionID))).
		Order(ent.Desc(entworkflowrun.FieldStartedAt), ent.Desc(entworkflowrun.FieldID)).
		First(ctx)
	if err != nil {
		return workflow.Run{}, fmt.Errorf("find latest workflow run by session: %w", err)
	}
	return toDomainWorkflowRun(row), nil
}

func (r *WorkflowRepository) FindLatestNodeRun(ctx context.Context, runID workflow.RunID, nodeID string) (workflow.NodeRun, error) {
	row, err := r.client.NodeRun.Query().
		Where(
			entnoderun.WorkflowRunIDEQ(string(runID)),
			entnoderun.NodeIDEQ(nodeID),
		).
		Order(ent.Desc(entnoderun.FieldStartedAt), ent.Desc(entnoderun.FieldID)).
		First(ctx)
	if err != nil {
		return workflow.NodeRun{}, fmt.Errorf("find latest node run: %w", err)
	}
	return toDomainNodeRun(row), nil
}

func (r *WorkflowRepository) MarkNodeWaitingUser(ctx context.Context, runID workflow.RunID, nodeRunID workflow.NodeRunID) error {
	updated, err := r.client.NodeRun.Update().
		Where(
			entnoderun.IDEQ(string(nodeRunID)),
			entnoderun.WorkflowRunIDEQ(string(runID)),
			entnoderun.StatusEQ(string(workflow.NodeRunning)),
		).
		SetStatus(string(workflow.NodeWaitingUser)).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("mark workflow node waiting user: %w", err)
	}
	if updated == 0 {
		row, err := r.client.NodeRun.Get(ctx, string(nodeRunID))
		if err != nil {
			return fmt.Errorf("find workflow node after waiting transition: %w", err)
		}
		if row.WorkflowRunID != string(runID) || row.Status != string(workflow.NodeWaitingUser) {
			return fmt.Errorf("workflow node %s cannot wait for user from status %q", nodeRunID, row.Status)
		}
	}
	return nil
}

func (r *WorkflowRepository) MarkNodeRunning(ctx context.Context, runID workflow.RunID, nodeRunID workflow.NodeRunID, processRunID workflow.ProcessRunID) error {
	updated, err := r.client.NodeRun.Update().
		Where(
			entnoderun.IDEQ(string(nodeRunID)),
			entnoderun.WorkflowRunIDEQ(string(runID)),
			entnoderun.StatusIn(string(workflow.NodeWaitingUser), string(workflow.NodeRunning)),
		).
		SetStatus(string(workflow.NodeRunning)).
		SetProcessRunID(string(processRunID)).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("mark workflow node running: %w", err)
	}
	if updated == 0 {
		return fmt.Errorf("workflow node %s cannot resume", nodeRunID)
	}
	return nil
}

func (r *WorkflowRepository) ActivateDefinition(ctx context.Context, id workflow.DefinitionID) error {
	definition, err := r.client.WorkflowDefinition.Get(ctx, string(id))
	if err != nil {
		return fmt.Errorf("find workflow definition to activate: %w", err)
	}
	if err := r.client.WorkflowDefinition.Update().
		Where(entworkflowdefinition.ProjectIDEQ(definition.ProjectID)).
		SetActive(false).
		Exec(ctx); err != nil {
		return fmt.Errorf("deactivate project workflow definitions: %w", err)
	}
	if err := r.client.WorkflowDefinition.UpdateOneID(string(id)).
		SetActive(true).
		Exec(ctx); err != nil {
		return fmt.Errorf("activate workflow definition: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) CreateRun(ctx context.Context, run workflow.Run) error {
	create := r.client.WorkflowRun.Create().
		SetID(string(run.ID)).
		SetSessionID(string(run.SessionID)).
		SetWorkflowDefinitionID(string(run.WorkflowDefinitionID)).
		SetStatus(string(run.Status)).
		SetCurrentNodeID(run.CurrentNodeID).
		SetContext(payloadOrEmpty(run.Context.Values))
	if run.StartedAt != nil {
		create.SetStartedAt(*run.StartedAt)
	}
	if run.StoppedAt != nil {
		create.SetStoppedAt(*run.StoppedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create workflow run: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) UpdateRunState(ctx context.Context, run workflow.Run) error {
	update := r.client.WorkflowRun.UpdateOneID(string(run.ID)).
		SetStatus(string(run.Status)).
		SetCurrentNodeID(run.CurrentNodeID).
		SetContext(payloadOrEmpty(run.Context.Values))
	if run.StoppedAt != nil {
		update.SetStoppedAt(*run.StoppedAt)
	}
	if err := update.Exec(ctx); err != nil {
		return fmt.Errorf("update workflow run state: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) CreateInitialRun(ctx context.Context, run workflow.Run, nodeRun workflow.NodeRun) error {
	if r.inTx {
		if err := createWorkflowRun(ctx, r.client, run); err != nil {
			return err
		}
		return createNodeRun(ctx, r.client, nodeRun)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow initial run transaction: %w", err)
	}
	if err := createWorkflowRun(ctx, tx.Client(), run); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := createNodeRun(ctx, tx.Client(), nodeRun); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow initial run transaction: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) SaveNodeRun(ctx context.Context, run workflow.NodeRun) error {
	if err := createNodeRun(ctx, r.client, run); err != nil {
		return err
	}
	return nil
}

func (r *WorkflowRepository) CreateNodeRunAndUpdateRun(ctx context.Context, run workflow.Run, nodeRun workflow.NodeRun) error {
	if r.inTx {
		if err := updateWorkflowRun(ctx, r.client, run); err != nil {
			return err
		}
		return createNodeRun(ctx, r.client, nodeRun)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow node rerun transaction: %w", err)
	}
	updateRun := tx.WorkflowRun.UpdateOneID(string(run.ID)).
		SetStatus(string(run.Status)).
		SetCurrentNodeID(run.CurrentNodeID).
		SetContext(payloadOrEmpty(run.Context.Values))
	if run.StoppedAt != nil {
		updateRun.SetStoppedAt(*run.StoppedAt)
	}
	if err := updateRun.Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update workflow run for node rerun: %w", err)
	}
	if err := createNodeRun(ctx, tx.Client(), nodeRun); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow node rerun transaction: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) CompleteNodeAndAdvance(ctx context.Context, completedNodeRun workflow.NodeRun, run workflow.Run, nextNodeRun *workflow.NodeRun) error {
	if r.inTx {
		if err := updateNodeRunComplete(ctx, r.client, completedNodeRun); err != nil {
			return err
		}
		if err := updateWorkflowRun(ctx, r.client, run); err != nil {
			return err
		}
		if nextNodeRun != nil {
			return createNodeRun(ctx, r.client, *nextNodeRun)
		}
		return nil
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow advance transaction: %w", err)
	}
	updateNode := tx.NodeRun.UpdateOneID(string(completedNodeRun.ID)).
		SetStatus(string(completedNodeRun.Status)).
		SetOutput(payloadOrEmpty(completedNodeRun.Output))
	if completedNodeRun.FinishedAt != nil {
		updateNode.SetFinishedAt(*completedNodeRun.FinishedAt)
	}
	if err := updateNode.Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("complete node run: %w", err)
	}
	updateRun := tx.WorkflowRun.UpdateOneID(string(run.ID)).
		SetStatus(string(run.Status)).
		SetCurrentNodeID(run.CurrentNodeID).
		SetContext(payloadOrEmpty(run.Context.Values))
	if run.StoppedAt != nil {
		updateRun.SetStoppedAt(*run.StoppedAt)
	}
	if err := updateRun.Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update workflow run after node: %w", err)
	}
	if nextNodeRun != nil {
		if err := createNodeRun(ctx, tx.Client(), *nextNodeRun); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow advance transaction: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) MarkRunFailed(ctx context.Context, runID workflow.RunID, nodeRunID workflow.NodeRunID, failure workflow.NodeFailure, finishedAt time.Time) error {
	if r.inTx {
		if err := markWorkflowRunFailed(ctx, r.client, runID, finishedAt); err != nil {
			return err
		}
		return markNodeRunFailed(ctx, r.client, nodeRunID, failure, finishedAt)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow failure transaction: %w", err)
	}
	if err := tx.WorkflowRun.UpdateOneID(string(runID)).
		SetStatus(string(workflow.RunFailed)).
		SetStoppedAt(finishedAt).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("mark workflow run failed: %w", err)
	}
	if err := tx.NodeRun.UpdateOneID(string(nodeRunID)).
		SetStatus(string(workflow.NodeFailed)).
		SetFinishedAt(finishedAt).
		SetOutput(map[string]any{
			"failure": map[string]any{
				"code":    failure.Code,
				"message": failure.Message,
			},
		}).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("mark node run failed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow failure transaction: %w", err)
	}
	return nil
}

func createWorkflowRun(ctx context.Context, client *ent.Client, run workflow.Run) error {
	create := client.WorkflowRun.Create().
		SetID(string(run.ID)).
		SetSessionID(string(run.SessionID)).
		SetWorkflowDefinitionID(string(run.WorkflowDefinitionID)).
		SetStatus(string(run.Status)).
		SetCurrentNodeID(run.CurrentNodeID).
		SetContext(payloadOrEmpty(run.Context.Values))
	if run.StartedAt != nil {
		create.SetStartedAt(*run.StartedAt)
	}
	if run.StoppedAt != nil {
		create.SetStoppedAt(*run.StoppedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create workflow run: %w", err)
	}
	return nil
}

func createNodeRun(ctx context.Context, client *ent.Client, run workflow.NodeRun) error {
	create := client.NodeRun.Create().
		SetID(string(run.ID)).
		SetWorkflowRunID(string(run.WorkflowRunID)).
		SetNodeID(run.NodeID).
		SetStatus(string(run.Status)).
		SetAttempt(run.Attempt).
		SetOutput(payloadOrEmpty(run.Output))
	if run.ProcessRunID != nil {
		create.SetProcessRunID(string(*run.ProcessRunID))
	}
	if run.StartedAt != nil {
		create.SetStartedAt(*run.StartedAt)
	}
	if run.FinishedAt != nil {
		create.SetFinishedAt(*run.FinishedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("save node run: %w", err)
	}
	return nil
}

func updateWorkflowRun(ctx context.Context, client *ent.Client, run workflow.Run) error {
	updateRun := client.WorkflowRun.UpdateOneID(string(run.ID)).
		SetStatus(string(run.Status)).
		SetCurrentNodeID(run.CurrentNodeID).
		SetContext(payloadOrEmpty(run.Context.Values))
	if run.StoppedAt != nil {
		updateRun.SetStoppedAt(*run.StoppedAt)
	}
	if err := updateRun.Exec(ctx); err != nil {
		return fmt.Errorf("update workflow run: %w", err)
	}
	return nil
}

func updateNodeRunComplete(ctx context.Context, client *ent.Client, completedNodeRun workflow.NodeRun) error {
	updateNode := client.NodeRun.UpdateOneID(string(completedNodeRun.ID)).
		SetStatus(string(completedNodeRun.Status)).
		SetOutput(payloadOrEmpty(completedNodeRun.Output))
	if completedNodeRun.FinishedAt != nil {
		updateNode.SetFinishedAt(*completedNodeRun.FinishedAt)
	}
	if err := updateNode.Exec(ctx); err != nil {
		return fmt.Errorf("complete node run: %w", err)
	}
	return nil
}

func markWorkflowRunFailed(ctx context.Context, client *ent.Client, runID workflow.RunID, finishedAt time.Time) error {
	if err := client.WorkflowRun.UpdateOneID(string(runID)).
		SetStatus(string(workflow.RunFailed)).
		SetStoppedAt(finishedAt).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark workflow run failed: %w", err)
	}
	return nil
}

func markNodeRunFailed(ctx context.Context, client *ent.Client, nodeRunID workflow.NodeRunID, failure workflow.NodeFailure, finishedAt time.Time) error {
	if err := client.NodeRun.UpdateOneID(string(nodeRunID)).
		SetStatus(string(workflow.NodeFailed)).
		SetFinishedAt(finishedAt).
		SetOutput(map[string]any{
			"failure": map[string]any{
				"code":    failure.Code,
				"message": failure.Message,
			},
		}).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark node run failed: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) UpdateRunContext(ctx context.Context, id workflow.RunID, contextValue workflow.Context) error {
	if err := r.client.WorkflowRun.UpdateOneID(string(id)).
		SetContext(payloadOrEmpty(contextValue.Values)).
		Exec(ctx); err != nil {
		return fmt.Errorf("update workflow run context: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) setRunStatus(ctx context.Context, id workflow.RunID, status workflow.RunStatus) error {
	if err := r.client.WorkflowRun.UpdateOneID(string(id)).
		SetStatus(string(status)).
		Exec(ctx); err != nil {
		return fmt.Errorf("set workflow run status: %w", err)
	}
	return nil
}

func toDomainWorkflowDefinition(row *ent.WorkflowDefinition) (workflow.Definition, error) {
	graph, err := mapToGraph(row.Graph)
	if err != nil {
		return workflow.Definition{}, err
	}
	return workflow.Definition{
		ID:        workflow.DefinitionID(row.ID),
		ProjectID: workflow.ProjectID(row.ProjectID),
		Name:      row.Name,
		Version:   row.Version,
		Graph:     graph,
		Active:    row.Active,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}

func graphToMap(graph workflow.Graph) (map[string]any, error) {
	data, err := json.Marshal(graph)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow graph: %w", err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("unmarshal workflow graph map: %w", err)
	}
	return payloadOrEmpty(value), nil
}

func mapToGraph(value map[string]any) (workflow.Graph, error) {
	data, err := json.Marshal(payloadOrEmpty(value))
	if err != nil {
		return workflow.Graph{}, fmt.Errorf("marshal workflow graph map: %w", err)
	}
	var graph workflow.Graph
	if err := json.Unmarshal(data, &graph); err != nil {
		return workflow.Graph{}, fmt.Errorf("unmarshal workflow graph: %w", err)
	}
	return graph, nil
}

func toDomainWorkflowRun(row *ent.WorkflowRun) workflow.Run {
	return workflow.Run{
		ID:                   workflow.RunID(row.ID),
		SessionID:            workflow.SessionID(row.SessionID),
		WorkflowDefinitionID: workflow.DefinitionID(row.WorkflowDefinitionID),
		Status:               workflow.RunStatus(row.Status),
		CurrentNodeID:        row.CurrentNodeID,
		Context:              workflow.Context{Values: payloadOrEmpty(row.Context)},
		StartedAt:            row.StartedAt,
		StoppedAt:            row.StoppedAt,
	}
}

func toDomainNodeRun(row *ent.NodeRun) workflow.NodeRun {
	var processRunID *workflow.ProcessRunID
	if row.ProcessRunID != nil {
		value := workflow.ProcessRunID(*row.ProcessRunID)
		processRunID = &value
	}
	return workflow.NodeRun{
		ID:            workflow.NodeRunID(row.ID),
		WorkflowRunID: workflow.RunID(row.WorkflowRunID),
		NodeID:        row.NodeID,
		Status:        workflow.NodeRunStatus(row.Status),
		Attempt:       row.Attempt,
		ProcessRunID:  processRunID,
		StartedAt:     row.StartedAt,
		FinishedAt:    row.FinishedAt,
		Output:        payloadOrEmpty(row.Output),
	}
}

func latestNodeRunQuery(client *ent.Client, workflowRunID workflow.RunID) *ent.NodeRunQuery {
	return client.NodeRun.Query().
		Where(entnoderun.WorkflowRunIDEQ(string(workflowRunID))).
		Order(ent.Desc(entnoderun.FieldStartedAt), ent.Desc(entnoderun.FieldID))
}

var _ = toDomainWorkflowRun
var _ = toDomainNodeRun
var _ = latestNodeRunQuery
