package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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
	run := workflow.Run{
		ID:                   "workflow-run-1",
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
		ID:            "node-run-1",
		WorkflowRunID: run.ID,
		NodeID:        "build",
		Status:        workflow.NodeRunning,
		Attempt:       1,
		ProcessRunID:  &processRunID,
		StartedAt:     &startedAt,
		Output:        map[string]any{"ok": true},
	}
	if err := repo.SaveNodeRun(ctx, nodeRun); err != nil {
		t.Fatalf("save node run: %v", err)
	}
	latest, err := repo.FindLatestNodeRun(ctx, run.ID, "build")
	if err != nil {
		t.Fatalf("find latest node run: %v", err)
	}
	if latest.ID != nodeRun.ID || latest.ProcessRunID == nil || *latest.ProcessRunID != processRunID {
		t.Fatalf("latest node run mismatch: %#v", latest)
	}

	persistedRun, err := store.Client().WorkflowRun.Get(ctx, string(run.ID))
	if err != nil {
		t.Fatalf("find workflow run: %v", err)
	}
	if persistedRun.CurrentNodeID != "build" || persistedRun.Context["requirement"] != "ship" {
		t.Fatalf("workflow run mismatch: %#v", persistedRun)
	}
	run.Status = workflow.RunWaitingResumeAction
	run.Context.Values["resume"] = map[string]any{"status": "failed"}
	if err := repo.UpdateRunState(ctx, run); err != nil {
		t.Fatalf("update run state: %v", err)
	}
	latestRun, err := repo.FindLatestRunBySession(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find latest run by session: %v", err)
	}
	if latestRun.ID != run.ID || latestRun.Status != workflow.RunWaitingResumeAction {
		t.Fatalf("latest run mismatch: %#v", latestRun)
	}
	if latestRun.Context.Values["resume"] == nil {
		t.Fatalf("latest run context missing resume: %#v", latestRun.Context)
	}
	run.Status = workflow.RunRunning
	run.Context.Values["resume"] = map[string]any{"status": "rerun_requested"}
	nextNodeRun := workflow.NodeRun{
		ID:            "node-run-2",
		WorkflowRunID: run.ID,
		NodeID:        "build",
		Status:        workflow.NodeRunning,
		Attempt:       2,
		StartedAt:     &startedAt,
	}
	if err := repo.CreateNodeRunAndUpdateRun(ctx, run, nextNodeRun); err != nil {
		t.Fatalf("create node run and update run: %v", err)
	}
	latestRun, err = repo.FindLatestRunBySession(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("find latest run by session after rerun: %v", err)
	}
	if latestRun.Status != workflow.RunRunning || latestRun.Context.Values["resume"] == nil {
		t.Fatalf("latest rerun run mismatch: %#v", latestRun)
	}
	latest, err = repo.FindLatestNodeRun(ctx, run.ID, "build")
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
	if persistedNodeRun.WorkflowRunID != string(run.ID) || persistedNodeRun.ProcessRunID == nil || *persistedNodeRun.ProcessRunID != string(processRunID) {
		t.Fatalf("node run mismatch: %#v", persistedNodeRun)
	}
	if persistedNodeRun.Output["ok"] != true {
		t.Fatalf("node run output mismatch: %#v", persistedNodeRun.Output)
	}
}

func TestWorkflowRepositoryTransitionsNodeThroughAnswerUserResume(t *testing.T) {
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
	if err := repo.CreateRun(ctx, workflow.Run{
		ID: "workflow-run-1", SessionID: "session-1", WorkflowDefinitionID: "workflow-1", Status: workflow.RunRunning, CurrentNodeID: "build", StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveNodeRun(ctx, workflow.NodeRun{
		ID: "node-run-1", WorkflowRunID: "workflow-run-1", NodeID: "build", Status: workflow.NodeRunning, Attempt: 1, StartedAt: &now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkNodeWaitingUser(ctx, "workflow-run-1", "node-run-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkNodeRunning(ctx, "workflow-run-1", "node-run-1", "process-run-resume"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindLatestNodeRun(ctx, "workflow-run-1", "build")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != workflow.NodeRunning || got.Attempt != 1 || got.ProcessRunID == nil || *got.ProcessRunID != "process-run-resume" {
		t.Fatalf("node run = %#v", got)
	}
}
