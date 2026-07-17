package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entnoderun "github.com/nzlov/anycode/internal/infra/entstore/ent/noderun"
	entsession "github.com/nzlov/anycode/internal/infra/entstore/ent/session"
	entworkflowdefinition "github.com/nzlov/anycode/internal/infra/entstore/ent/workflowdefinition"
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

func (r *WorkflowRepository) FindRun(ctx context.Context, sessionID workflow.SessionID) (workflow.Run, error) {
	row, err := r.client.Session.Query().
		Where(entsession.IDEQ(string(sessionID)), entsession.WorkflowDefinitionIDNEQ("")).
		Only(ctx)
	if err != nil {
		return workflow.Run{}, fmt.Errorf("find workflow run: %w", err)
	}
	return toDomainWorkflowRun(row), nil
}

func (r *WorkflowRepository) FindLatestNodeRun(ctx context.Context, sessionID workflow.SessionID, nodeID string) (workflow.NodeRun, error) {
	row, err := r.client.NodeRun.Query().
		Where(
			entnoderun.SessionIDEQ(string(sessionID)),
			entnoderun.NodeIDEQ(nodeID),
		).
		Order(ent.Desc(entnoderun.FieldStartedAt), ent.Desc(entnoderun.FieldID)).
		First(ctx)
	if err != nil {
		return workflow.NodeRun{}, fmt.Errorf("find latest node run: %w", err)
	}
	nodeRun, err := toDomainNodeRun(row)
	if err != nil {
		return workflow.NodeRun{}, err
	}
	return nodeRun, nil
}

func (r *WorkflowRepository) MarkNodeWaitingUser(ctx context.Context, sessionID workflow.SessionID, nodeRunID workflow.NodeRunID) error {
	updated, err := r.client.NodeRun.Update().
		Where(
			entnoderun.IDEQ(string(nodeRunID)),
			entnoderun.SessionIDEQ(string(sessionID)),
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
		if row.SessionID != string(sessionID) || row.Status != string(workflow.NodeWaitingUser) {
			return fmt.Errorf("workflow node %s cannot wait for user from status %q", nodeRunID, row.Status)
		}
	}
	return nil
}

func (r *WorkflowRepository) MarkNodeRunning(ctx context.Context, sessionID workflow.SessionID, nodeRunID workflow.NodeRunID, processRunID workflow.ProcessRunID) error {
	updated, err := r.client.NodeRun.Update().
		Where(
			entnoderun.IDEQ(string(nodeRunID)),
			entnoderun.SessionIDEQ(string(sessionID)),
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
	update := r.client.Session.UpdateOneID(string(run.SessionID)).
		SetWorkflowDefinitionID(string(run.WorkflowDefinitionID)).
		SetWorkflowStatus(string(run.Status)).
		SetWorkflowCurrentNodeID(run.CurrentNodeID).
		SetWorkflowContext(payloadOrEmpty(run.Context.Values)).
		SetWorkflowPendingApproval(pendingApprovalToMap(run.PendingApproval))
	if run.StartedAt != nil {
		update.SetWorkflowStartedAt(*run.StartedAt)
	}
	if run.StoppedAt != nil {
		update.SetWorkflowStoppedAt(*run.StoppedAt)
	} else {
		update.ClearWorkflowStoppedAt()
	}
	if err := update.Exec(ctx); err != nil {
		return fmt.Errorf("create workflow run: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) UpdateRunState(ctx context.Context, run workflow.Run) error {
	update := r.client.Session.UpdateOneID(string(run.SessionID)).
		SetWorkflowStatus(string(run.Status)).
		SetWorkflowCurrentNodeID(run.CurrentNodeID).
		SetWorkflowContext(payloadOrEmpty(run.Context.Values)).
		SetWorkflowPendingApproval(pendingApprovalToMap(run.PendingApproval))
	if run.StoppedAt != nil {
		update.SetWorkflowStoppedAt(*run.StoppedAt)
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
	updateRun := tx.Session.UpdateOneID(string(run.SessionID)).
		SetWorkflowStatus(string(run.Status)).
		SetWorkflowCurrentNodeID(run.CurrentNodeID).
		SetWorkflowContext(payloadOrEmpty(run.Context.Values)).
		SetWorkflowPendingApproval(pendingApprovalToMap(run.PendingApproval))
	if run.StoppedAt != nil {
		updateRun.SetWorkflowStoppedAt(*run.StoppedAt)
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
	result, err := resultToMap(completedNodeRun.Result)
	if err != nil {
		return fmt.Errorf("encode completed node result: %w", err)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow advance transaction: %w", err)
	}
	updateNode := tx.NodeRun.UpdateOneID(string(completedNodeRun.ID)).
		SetStatus(string(completedNodeRun.Status)).
		SetOutput(result)
	if completedNodeRun.FinishedAt != nil {
		updateNode.SetFinishedAt(*completedNodeRun.FinishedAt)
	}
	if err := updateNode.Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("complete node run: %w", err)
	}
	updateRun := tx.Session.UpdateOneID(string(run.SessionID)).
		SetWorkflowStatus(string(run.Status)).
		SetWorkflowCurrentNodeID(run.CurrentNodeID).
		SetWorkflowContext(payloadOrEmpty(run.Context.Values)).
		SetWorkflowPendingApproval(pendingApprovalToMap(run.PendingApproval))
	if run.StoppedAt != nil {
		updateRun.SetWorkflowStoppedAt(*run.StoppedAt)
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

func (r *WorkflowRepository) ResumeNodeAndUpdateRun(ctx context.Context, nodeRun workflow.NodeRun, run workflow.Run) error {
	if r.inTx {
		if err := resumeNodeRun(ctx, r.client, nodeRun); err != nil {
			return err
		}
		return updateWorkflowRun(ctx, r.client, run)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow approval resume transaction: %w", err)
	}
	if err := resumeNodeRun(ctx, tx.Client(), nodeRun); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := updateWorkflowRun(ctx, tx.Client(), run); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow approval resume transaction: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) MarkRunFailed(ctx context.Context, sessionID workflow.SessionID, nodeRunID workflow.NodeRunID, failure workflow.NodeFailure, finishedAt time.Time) error {
	if r.inTx {
		if err := markWorkflowRunFailed(ctx, r.client, sessionID, finishedAt); err != nil {
			return err
		}
		return markNodeRunFailed(ctx, r.client, nodeRunID, failure, finishedAt)
	}
	result, err := resultToMap(failureResult(failure))
	if err != nil {
		return fmt.Errorf("encode failed node result: %w", err)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow failure transaction: %w", err)
	}
	if err := tx.Session.UpdateOneID(string(sessionID)).
		SetWorkflowStatus(string(workflow.RunFailed)).
		SetWorkflowStoppedAt(finishedAt).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("mark workflow run failed: %w", err)
	}
	if err := tx.NodeRun.UpdateOneID(string(nodeRunID)).
		SetStatus(string(workflow.NodeFailed)).
		SetFinishedAt(finishedAt).
		SetOutput(result).
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
	update := client.Session.UpdateOneID(string(run.SessionID)).
		SetWorkflowDefinitionID(string(run.WorkflowDefinitionID)).
		SetWorkflowStatus(string(run.Status)).
		SetWorkflowCurrentNodeID(run.CurrentNodeID).
		SetWorkflowContext(payloadOrEmpty(run.Context.Values)).
		SetWorkflowPendingApproval(pendingApprovalToMap(run.PendingApproval))
	if run.StartedAt != nil {
		update.SetWorkflowStartedAt(*run.StartedAt)
	}
	if run.StoppedAt != nil {
		update.SetWorkflowStoppedAt(*run.StoppedAt)
	} else {
		update.ClearWorkflowStoppedAt()
	}
	if err := update.Exec(ctx); err != nil {
		return fmt.Errorf("create workflow run: %w", err)
	}
	return nil
}

func createNodeRun(ctx context.Context, client *ent.Client, run workflow.NodeRun) error {
	result, err := resultToMap(run.Result)
	if err != nil {
		return fmt.Errorf("encode node result: %w", err)
	}
	create := client.NodeRun.Create().
		SetID(string(run.ID)).
		SetSessionID(string(run.SessionID)).
		SetNodeID(run.NodeID).
		SetStatus(string(run.Status)).
		SetAttempt(run.Attempt).
		SetOutput(result)
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
	updateRun := client.Session.UpdateOneID(string(run.SessionID)).
		SetWorkflowStatus(string(run.Status)).
		SetWorkflowCurrentNodeID(run.CurrentNodeID).
		SetWorkflowContext(payloadOrEmpty(run.Context.Values)).
		SetWorkflowPendingApproval(pendingApprovalToMap(run.PendingApproval))
	if run.StoppedAt != nil {
		updateRun.SetWorkflowStoppedAt(*run.StoppedAt)
	}
	if err := updateRun.Exec(ctx); err != nil {
		return fmt.Errorf("update workflow run: %w", err)
	}
	return nil
}

func resumeNodeRun(ctx context.Context, client *ent.Client, nodeRun workflow.NodeRun) error {
	update := client.NodeRun.UpdateOneID(string(nodeRun.ID)).
		SetStatus(string(nodeRun.Status)).
		SetOutput(map[string]any{}).
		ClearFinishedAt()
	if err := update.Exec(ctx); err != nil {
		return fmt.Errorf("resume node run after approval: %w", err)
	}
	return nil
}

func updateNodeRunComplete(ctx context.Context, client *ent.Client, completedNodeRun workflow.NodeRun) error {
	result, err := resultToMap(completedNodeRun.Result)
	if err != nil {
		return fmt.Errorf("encode completed node result: %w", err)
	}
	updateNode := client.NodeRun.UpdateOneID(string(completedNodeRun.ID)).
		SetStatus(string(completedNodeRun.Status)).
		SetOutput(result)
	if completedNodeRun.FinishedAt != nil {
		updateNode.SetFinishedAt(*completedNodeRun.FinishedAt)
	}
	if err := updateNode.Exec(ctx); err != nil {
		return fmt.Errorf("complete node run: %w", err)
	}
	return nil
}

func markWorkflowRunFailed(ctx context.Context, client *ent.Client, sessionID workflow.SessionID, finishedAt time.Time) error {
	if err := client.Session.UpdateOneID(string(sessionID)).
		SetWorkflowStatus(string(workflow.RunFailed)).
		SetWorkflowStoppedAt(finishedAt).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark workflow run failed: %w", err)
	}
	return nil
}

func markNodeRunFailed(ctx context.Context, client *ent.Client, nodeRunID workflow.NodeRunID, failure workflow.NodeFailure, finishedAt time.Time) error {
	result, err := resultToMap(failureResult(failure))
	if err != nil {
		return fmt.Errorf("encode failed node result: %w", err)
	}
	if err := client.NodeRun.UpdateOneID(string(nodeRunID)).
		SetStatus(string(workflow.NodeFailed)).
		SetFinishedAt(finishedAt).
		SetOutput(result).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark node run failed: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) UpdateRunContext(ctx context.Context, sessionID workflow.SessionID, contextValue workflow.Context) error {
	if err := r.client.Session.UpdateOneID(string(sessionID)).
		SetWorkflowContext(payloadOrEmpty(contextValue.Values)).
		Exec(ctx); err != nil {
		return fmt.Errorf("update workflow run context: %w", err)
	}
	return nil
}

func (r *WorkflowRepository) setRunStatus(ctx context.Context, sessionID workflow.SessionID, status workflow.RunStatus) error {
	if err := r.client.Session.UpdateOneID(string(sessionID)).
		SetWorkflowStatus(string(status)).
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

func toDomainWorkflowRun(row *ent.Session) workflow.Run {
	return workflow.Run{
		SessionID:            workflow.SessionID(row.ID),
		WorkflowDefinitionID: workflow.DefinitionID(row.WorkflowDefinitionID),
		Status:               workflow.RunStatus(row.WorkflowStatus),
		CurrentNodeID:        row.WorkflowCurrentNodeID,
		Context:              workflow.Context{Values: payloadOrEmpty(row.WorkflowContext)},
		PendingApproval:      pendingApprovalFromMap(row.WorkflowPendingApproval),
		StartedAt:            row.WorkflowStartedAt,
		StoppedAt:            row.WorkflowStoppedAt,
	}
}

func pendingApprovalToMap(approval *workflow.PendingApproval) map[string]any {
	if approval == nil {
		return map[string]any{}
	}
	value := map[string]any{"phase": string(approval.Phase), "nodeId": approval.NodeID, "attempt": approval.Attempt}
	if approval.NodeRunID != nil {
		value["nodeRunId"] = string(*approval.NodeRunID)
	}
	return value
}

func pendingApprovalFromMap(value map[string]any) *workflow.PendingApproval {
	phase, _ := value["phase"].(string)
	nodeID, _ := value["nodeId"].(string)
	if phase == "" || nodeID == "" {
		return nil
	}
	approval := &workflow.PendingApproval{Phase: workflow.ApprovalPhase(phase), NodeID: nodeID}
	if attempt, ok := value["attempt"].(float64); ok {
		approval.Attempt = int(attempt)
	} else if attempt, ok := value["attempt"].(int); ok {
		approval.Attempt = attempt
	}
	if raw, _ := value["nodeRunId"].(string); raw != "" {
		id := workflow.NodeRunID(raw)
		approval.NodeRunID = &id
	}
	return approval
}

func toDomainNodeRun(row *ent.NodeRun) (workflow.NodeRun, error) {
	var processRunID *workflow.ProcessRunID
	if row.ProcessRunID != nil {
		value := workflow.ProcessRunID(*row.ProcessRunID)
		processRunID = &value
	}
	result, err := resultFromMap(row.Output)
	if err != nil {
		return workflow.NodeRun{}, fmt.Errorf("decode node run result: %w", err)
	}
	return workflow.NodeRun{
		ID:           workflow.NodeRunID(row.ID),
		SessionID:    workflow.SessionID(row.SessionID),
		NodeID:       row.NodeID,
		Status:       workflow.NodeRunStatus(row.Status),
		Attempt:      row.Attempt,
		ProcessRunID: processRunID,
		StartedAt:    row.StartedAt,
		FinishedAt:   row.FinishedAt,
		Result:       result,
	}, nil
}

func latestNodeRunQuery(client *ent.Client, sessionID workflow.SessionID) *ent.NodeRunQuery {
	return client.NodeRun.Query().
		Where(entnoderun.SessionIDEQ(string(sessionID))).
		Order(ent.Desc(entnoderun.FieldStartedAt), ent.Desc(entnoderun.FieldID))
}

var _ = toDomainWorkflowRun
var _ = latestNodeRunQuery

func resultToMap(result *workflow.Result) (map[string]any, error) {
	if result == nil {
		return map[string]any{}, nil
	}
	canonical := *result
	canonical.Normalize()
	data, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, err
	}
	return output, nil
}

func resultFromMap(output map[string]any) (*workflow.Result, error) {
	if len(output) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, err
	}
	var result workflow.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	result.Normalize()
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return &result, nil
}

func failureResult(failure workflow.NodeFailure) *workflow.Result {
	return &workflow.Result{
		Version: workflow.ResultVersion,
		Outcome: workflow.ResultFailure,
		Summary: failure.Message,
		Data: map[string]any{
			"failure": map[string]any{"code": failure.Code, "message": failure.Message},
		},
	}
}
