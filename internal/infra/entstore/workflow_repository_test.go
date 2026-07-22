package entstore

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/workflow"
)

func TestWorkflowRepositoryPersistsDefinitionsRunsAndNodeRuns(t *testing.T) {
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

	repo := store.Workflows()
	createdAt := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
	definition := workflow.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "Default",
		Version:   1,
		Graph: workflow.Graph{
			Nodes: []workflow.Node{{ID: "build", Type: "codex", Title: "Build", Prompt: "run tests"}},
		},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	if err := repo.SaveDefinition(ctx, definition); err != nil {
		t.Fatalf("save definition: %v", err)
	}
	if err := repo.ActivateDefinition(ctx, definition.ID); err != nil {
		t.Fatalf("activate definition: %v", err)
	}
	active, err := repo.FindActive(ctx, definition.ProjectID)
	if err != nil {
		t.Fatalf("find active: %v", err)
	}
	if active.ID != definition.ID || len(active.Graph.Nodes) != 1 || active.Graph.Nodes[0].Prompt != "run tests" {
		t.Fatalf("active definition mismatch: %#v", active)
	}

	startedAt := createdAt.Add(time.Minute)
	createWorkflowTestSession(t, ctx, store, "session-1")
	if _, err := repo.FindRun(ctx, "session-1"); err == nil {
		t.Fatal("session without workflow state was returned as a workflow run")
	}
	run := workflow.Run{
		SessionID:            "session-1",
		WorkflowDefinitionID: definition.ID,
		Status:               workflow.RunRunning,
		CurrentNodeID:        "build",
		Context:              workflow.Context{Values: map[string]any{"requirement": "ship"}},
		StartedAt:            &startedAt,
	}
	if err := repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	processRunID := workflow.ProcessRunID("process-run-1")
	nodeRun := workflow.NodeRun{
		ID:           "node-run-1",
		SessionID:    run.SessionID,
		NodeID:       "build",
		Status:       workflow.NodeRunning,
		Attempt:      1,
		ProcessRunID: &processRunID,
		StartedAt:    &startedAt,
		Result:       &workflow.Result{Version: workflow.ResultVersion, Outcome: workflow.ResultSuccess, Summary: "completed", Data: map[string]any{"ok": true}},
	}
	if err := repo.SaveNodeRun(ctx, nodeRun); err != nil {
		t.Fatalf("save node run: %v", err)
	}
	latest, err := repo.FindLatestNodeRun(ctx, run.SessionID, "build")
	if err != nil {
		t.Fatalf("find latest node run: %v", err)
	}
	if latest.ID != nodeRun.ID || latest.ProcessRunID == nil || *latest.ProcessRunID != processRunID {
		t.Fatalf("latest node run mismatch: %#v", latest)
	}

	persistedRun, err := store.Client().Session.Get(ctx, string(run.SessionID))
	if err != nil {
		t.Fatalf("find workflow run: %v", err)
	}
	if persistedRun.WorkflowCurrentNodeID != "build" || persistedRun.WorkflowContext["requirement"] != "ship" {
		t.Fatalf("workflow run mismatch: %#v", persistedRun)
	}
	run.Status = workflow.RunWaitingResumeAction
	run.Context.Values["resume"] = map[string]any{"status": "failed"}
	run.PendingApproval = &workflow.PendingApproval{Phase: workflow.ApprovalBeforeRun, NodeID: "build", Attempt: 2}
	if err := repo.UpdateRunState(ctx, run); err != nil {
		t.Fatalf("update run state: %v", err)
	}
	latestRun, err := repo.FindRun(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find latest run by session: %v", err)
	}
	if latestRun.SessionID != run.SessionID || latestRun.Status != workflow.RunWaitingResumeAction {
		t.Fatalf("latest run mismatch: %#v", latestRun)
	}
	if latestRun.Context.Values["resume"] == nil {
		t.Fatalf("latest run context missing resume: %#v", latestRun.Context)
	}
	if latestRun.PendingApproval == nil || latestRun.PendingApproval.Phase != workflow.ApprovalBeforeRun || latestRun.PendingApproval.Attempt != 2 {
		t.Fatalf("latest run pending approval mismatch: %#v", latestRun.PendingApproval)
	}
	run.Status = workflow.RunRunning
	run.PendingApproval = nil
	run.Context.Values["resume"] = map[string]any{"status": "rerun_requested"}
	nextNodeRun := workflow.NodeRun{
		ID:        "node-run-2",
		SessionID: run.SessionID,
		NodeID:    "build",
		Status:    workflow.NodeRunning,
		Attempt:   2,
		StartedAt: &startedAt,
	}
	if err := repo.CreateNodeRunAndUpdateRun(ctx, run, nextNodeRun); err != nil {
		t.Fatalf("create node run and update run: %v", err)
	}
	latestRun, err = repo.FindRun(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find latest run by session after rerun: %v", err)
	}
	if latestRun.Status != workflow.RunRunning || latestRun.Context.Values["resume"] == nil {
		t.Fatalf("latest rerun run mismatch: %#v", latestRun)
	}
	latest, err = repo.FindLatestNodeRun(ctx, run.SessionID, "build")
	if err != nil {
		t.Fatalf("find latest node run after rerun: %v", err)
	}
	if latest.ID != nextNodeRun.ID || latest.Attempt != 2 {
		t.Fatalf("latest rerun node mismatch: %#v", latest)
	}
	persistedNodeRun, err := store.Client().NodeRun.Get(ctx, string(nodeRun.ID))
	if err != nil {
		t.Fatalf("find node run: %v", err)
	}
	if persistedNodeRun.SessionID != string(run.SessionID) || persistedNodeRun.ProcessRunID == nil || *persistedNodeRun.ProcessRunID != string(processRunID) {
		t.Fatalf("node run mismatch: %#v", persistedNodeRun)
	}
	data, ok := persistedNodeRun.Output["data"].(map[string]any)
	if !ok || data["ok"] != true {
		t.Fatalf("node run output mismatch: %#v", persistedNodeRun.Output)
	}
}

func TestWorkflowRepositoryTransitionsNodeThroughQuestionsResume(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.Workflows()
	now := time.Now().UTC()
	createWorkflowTestSession(t, ctx, store, "session-1")
	if err := repo.CreateRun(ctx, workflow.Run{
		SessionID: "session-1", WorkflowDefinitionID: "workflow-1", Status: workflow.RunRunning, CurrentNodeID: "build", StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveNodeRun(ctx, workflow.NodeRun{
		ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: workflow.NodeRunning, Attempt: 1, StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkNodeWaitingUser(ctx, "session-1", "node-run-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkNodeRunning(ctx, "session-1", "node-run-1", "process-run-resume"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindLatestNodeRun(ctx, "session-1", "build")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != workflow.NodeRunning || got.Attempt != 1 || got.ProcessRunID == nil || *got.ProcessRunID != "process-run-resume" {
		t.Fatalf("node run = %#v", got)
	}
}

func TestMigrateCanonicalizesWorkflowApprovalFieldsAndPreservesAuditData(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	repo := store.Workflows()
	definition := workflow.Definition{
		ID: "workflow-legacy", ProjectID: "project-1", Name: "Legacy", Version: 7, Active: true,
		CreatedAt: now, UpdatedAt: now,
		Graph: workflow.Graph{Nodes: []workflow.Node{
			{
				ID: "plan", Type: "codex", Approval: workflow.ApprovalConfig{AfterRun: true},
				OutputFields: []workflow.OutputField{
					{Key: "approval.approved", ValueType: "boolean"},
					{Key: "approval.note", ValueType: "string"},
					{Key: "planPath", ValueType: "string"},
				},
			},
			{
				ID: "merge", Type: "merge",
				OutputFields: []workflow.OutputField{{Key: "merge.status", Description: "legacy", ValueType: "number"}},
			},
		}},
	}
	if err := repo.SaveDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	runContext := workflow.Context{Values: map[string]any{"results": map[string]any{"data": map[string]any{"approval": map[string]any{"approved": true}}}}}
	createWorkflowTestSession(t, ctx, store, "session-1")
	if err := repo.CreateRun(ctx, workflow.Run{
		SessionID: "session-1", WorkflowDefinitionID: definition.ID,
		Status: workflow.RunCompleted, CurrentNodeID: "plan", Context: runContext, StartedAt: &now, StoppedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	historicalResult := &workflow.Result{
		Version: workflow.ResultVersion, Outcome: workflow.ResultSuccess, Summary: "legacy",
		Data: map[string]any{"approval": map[string]any{"approved": true}},
	}
	if err := repo.SaveNodeRun(ctx, workflow.NodeRun{
		ID: "node-run-1", SessionID: "session-1", NodeID: "plan", Status: workflow.NodeSucceeded,
		Attempt: 1, StartedAt: &now, FinishedAt: &now, Result: historicalResult,
	}); err != nil {
		t.Fatal(err)
	}
	eventSessionID := eventdomain.SessionID("session-1")
	event := eventdomain.DomainEvent{
		ID: "event-1", Scope: eventdomain.Scope{ProjectID: "project-1", SessionID: &eventSessionID},
		Type: "workflow.exit_pending", Payload: map[string]any{"results": map[string]any{"data": map[string]any{"approval": map[string]any{"approved": true}}}}, CreatedAt: now,
	}
	if err := store.Events().Append(ctx, event); err != nil {
		t.Fatal(err)
	}
	rawGraph := map[string]any{
		"Nodes": []any{map[string]any{
			"ID": "raw-node",
			"OutputFields": []any{
				map[string]any{"Key": "approval.approved", "ValueType": "boolean", "UnknownField": "remove-with-field"},
				map[string]any{"Key": "result", "ValueType": "string", "UnknownField": "keep-with-field"},
			},
			"UnknownNodeField": map[string]any{"keep": true},
		}},
		"UnknownGraphField": []any{"keep"},
	}
	if err := store.Client().WorkflowDefinition.Create().
		SetID("workflow-raw").SetProjectID("project-1").SetName("Raw").SetGraph(rawGraph).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate legacy workflow fields: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}

	got, err := repo.FindDefinition(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != definition.Version || got.Active != definition.Active || !got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
		t.Fatalf("definition metadata changed: %#v", got)
	}
	wantFields := []workflow.OutputField{{Key: "planPath", ValueType: "string"}}
	if !reflect.DeepEqual(got.Graph.Nodes[0].OutputFields, wantFields) {
		t.Fatalf("migrated output fields = %#v, want %#v", got.Graph.Nodes[0].OutputFields, wantFields)
	}
	wantMergeFields := []workflow.OutputField{{Key: "merge.status", Description: "legacy", ValueType: "number"}}
	if !reflect.DeepEqual(got.Graph.Nodes[1].OutputFields, wantMergeFields) {
		t.Fatalf("migration changed unrelated merge fields = %#v, want %#v", got.Graph.Nodes[1].OutputFields, wantMergeFields)
	}
	raw, err := store.Client().WorkflowDefinition.Get(ctx, "workflow-raw")
	if err != nil {
		t.Fatal(err)
	}
	wantRawGraph := map[string]any{
		"Nodes": []any{map[string]any{
			"ID": "raw-node",
			"OutputFields": []any{
				map[string]any{"Key": "result", "ValueType": "string", "UnknownField": "keep-with-field"},
			},
			"UnknownNodeField": map[string]any{"keep": true},
		}},
		"UnknownGraphField": []any{"keep"},
	}
	if !reflect.DeepEqual(raw.Graph, wantRawGraph) {
		t.Fatalf("migration changed unrelated raw graph data = %#v, want %#v", raw.Graph, wantRawGraph)
	}
	gotRun, err := repo.FindRun(ctx, "session-1")
	if err != nil || !reflect.DeepEqual(gotRun.Context, runContext) {
		t.Fatalf("historical workflow context changed: %#v, err=%v", gotRun.Context, err)
	}
	gotNodeRun, err := repo.FindLatestNodeRun(ctx, "session-1", "plan")
	if err != nil || !reflect.DeepEqual(gotNodeRun.Result.Data, historicalResult.Data) {
		t.Fatalf("historical node result changed: %#v, err=%v", gotNodeRun.Result, err)
	}
	events, err := store.Events().After(ctx, event.Scope, "")
	if err != nil || len(events) != 1 || !reflect.DeepEqual(events[0].Payload, event.Payload) {
		t.Fatalf("historical event changed: %#v, err=%v", events, err)
	}
}

func createWorkflowTestSession(t *testing.T, ctx context.Context, store *Store, id string) {
	t.Helper()
	if err := store.Client().Session.Create().
		SetID(id).
		SetProjectID("project-1").
		SetMode("workflow").
		SetStatus("stopped").
		Exec(ctx); err != nil {
		t.Fatalf("create workflow test session: %v", err)
	}
}

func TestMigrateDoesNotPartiallyCanonicalizeInvalidWorkflowDefinitions(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.Workflows()
	legacy := workflow.Definition{
		ID: "workflow-a", ProjectID: "project-1", Name: "Legacy",
		Graph: workflow.Graph{Nodes: []workflow.Node{{ID: "plan", OutputFields: []workflow.OutputField{{Key: "approval.approved", ValueType: "boolean"}}}}},
	}
	if err := repo.SaveDefinition(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if err := store.Client().WorkflowDefinition.Create().
		SetID("workflow-b").SetProjectID("project-1").SetName("Invalid").SetGraph(map[string]any{"Nodes": "invalid"}).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	if err := store.Migrate(ctx); err == nil {
		t.Fatal("Migrate() succeeded with an invalid workflow definition")
	}
	got, err := repo.FindDefinition(ctx, legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Graph.Nodes[0].OutputFields) != 1 || got.Graph.Nodes[0].OutputFields[0].Key != "approval.approved" {
		t.Fatalf("legacy definition was partially migrated: %#v", got.Graph)
	}
}
