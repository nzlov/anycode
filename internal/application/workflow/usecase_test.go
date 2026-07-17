package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	domain "github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/infra/entstore"
)

func TestStartForSessionCreatesRunningNodeRunForExecutableStartNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build", Prompt: "Validate build"},
				{ID: "done", Type: "codex", Title: "Done"},
			},
			Edges: []domain.Edge{{From: "build", To: "done"}},
		},
	}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	ids := []string{"node-run-1"}
	service.generateID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.StartForSession(ctx, sessiondomain.WorkflowStartInput{
		ProjectID:            "project-1",
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Requirement:          "ship it",
	})
	if err != nil {
		t.Fatalf("StartForSession() error = %v", err)
	}
	if !got.RequiresCodex || got.SessionID != "session-1" || got.NodeRunID == nil || *got.NodeRunID != "node-run-1" {
		t.Fatalf("StartForSession() = %#v", got)
	}
	if got.CurrentNodeID != "build" || got.CurrentNodeTitle != "Build" {
		t.Fatalf("current node = %q/%q", got.CurrentNodeID, got.CurrentNodeTitle)
	}
	if !strings.Contains(got.Prompt, "Validate build\n\nUser requirement:\nship it") || !strings.Contains(got.Prompt, "Workflow input params JSON") {
		t.Fatalf("Prompt = %q", got.Prompt)
	}
	if len(repo.runs) != 1 || repo.runs[0].Status != domain.RunRunning || repo.runs[0].CurrentNodeID != "build" {
		t.Fatalf("runs = %#v", repo.runs)
	}
	if len(repo.nodeRuns) != 1 || repo.nodeRuns[0].Status != domain.NodeRunning || repo.nodeRuns[0].Attempt != 1 {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestWorkflowMutationsWriteEventsInUnitOfWorkAndIgnorePublishError(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{
		DatabaseURL: filepath.Join(t.TempDir(), "anycode.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	definition := domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "Default",
		Version:   1,
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build"}},
		},
	}
	if err := store.Workflows().SaveDefinition(ctx, definition); err != nil {
		t.Fatalf("save definition: %v", err)
	}
	if err := store.Client().Session.Create().
		SetID("session-1").
		SetProjectID("project-1").
		SetMode("workflow").
		SetStatus("running").
		Exec(ctx); err != nil {
		t.Fatalf("create session: %v", err)
	}
	service := New(store.Workflows(), WithUnitOfWork(store), WithEvents(store.Events()), WithEventPublisher(failingPublisher{}))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	ids := []string{"node-run-1", "event-1", "event-2"}
	service.generateID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	start, err := service.StartForSession(ctx, sessiondomain.WorkflowStartInput{
		ProjectID:            "project-1",
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
	})
	if err != nil {
		t.Fatalf("StartForSession() error = %v", err)
	}
	if _, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: start.SessionID,
		NodeRunID: *start.NodeRunID,
		Output:    resultOutput(map[string]any{"ok": true}),
	}); err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}

	run, err := store.Workflows().FindRun(ctx, "session-1")
	if err != nil {
		t.Fatalf("find run: %v", err)
	}
	if run.Status != domain.RunCompleted {
		t.Fatalf("workflow run status = %q", run.Status)
	}
	sessionID := eventdomain.SessionID("session-1")
	events, err := store.Events().After(ctx, eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID}, "")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 || events[0].Type != "workflow.started" || events[1].Type != "workflow.completed" {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Payload["currentNodeTitle"] != "Build" || events[1].Payload["sessionId"] != "session-1" {
		t.Fatalf("event payloads = %#v / %#v", events[0].Payload, events[1].Payload)
	}
	if _, found := events[0].Payload["status"]; found || events[0].Payload["workflowStatus"] == nil || events[0].Payload["workflowAdvanceStatus"] == nil {
		t.Fatalf("workflow event status fields = %#v", events[0].Payload)
	}
}

func TestSaveDefinitionRejectsInvalidCondition(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := New(repo)
	service.generateID = func() (string, error) { return "workflow-1", nil }

	_, err := service.SaveDefinition(ctx, SaveDefinitionInput{
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build"}, {ID: "verify"}},
			Edges: []domain.Edge{
				{From: "build", To: "verify", Condition: domain.Condition{Field: "last.status", Op: "script", Value: "return true"}},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported workflow condition op") {
		t.Fatalf("SaveDefinition() error = %v", err)
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeValidationFailed || appErr.Category != apperror.CategoryWorkflowError {
		t.Fatalf("SaveDefinition() app error = %#v", err)
	}
	if len(repo.definitions) != 0 {
		t.Fatalf("definition should not be saved: %#v", repo.definitions)
	}
}

func TestDefinitionRejectsAfterRunApprovalOnApprovalNodes(t *testing.T) {
	for _, nodeType := range []string{"approval", "manual_approval"} {
		t.Run(nodeType, func(t *testing.T) {
			ctx := context.Background()
			repo := newFakeRepository()
			service := New(repo)
			service.generateID = func() (string, error) { return "workflow-1", nil }
			graph := domain.Graph{Nodes: []domain.Node{{
				ID: "approve", Type: nodeType, Approval: domain.ApprovalConfig{AfterRun: true},
			}}}

			if _, err := service.SaveDefinition(ctx, SaveDefinitionInput{
				ProjectID: "project-1", Name: "invalid", Graph: graph,
			}); err == nil || !strings.Contains(err.Error(), "cannot require after-run approval") {
				t.Fatalf("SaveDefinition() error = %v", err)
			}
			repo.definitions["workflow-invalid"] = domain.Definition{
				ID: "workflow-invalid", ProjectID: "project-1", Name: "invalid", Graph: graph,
			}
			if _, err := service.GetDefinition(ctx, "workflow-invalid"); err == nil || !strings.Contains(err.Error(), "cannot require after-run approval") {
				t.Fatalf("GetDefinition() error = %v", err)
			}
			if _, err := service.StartForSession(ctx, sessiondomain.WorkflowStartInput{
				ProjectID: "project-1", SessionID: "session-1", WorkflowDefinitionID: "workflow-invalid",
			}); err == nil || !strings.Contains(err.Error(), "cannot require after-run approval") {
				t.Fatalf("StartForSession() error = %v", err)
			}
			if len(repo.runs) != 0 || len(repo.nodeRuns) != 0 {
				t.Fatalf("invalid approval definition created a run: runs=%#v nodeRuns=%#v", repo.runs, repo.nodeRuns)
			}

			repo.runs = []domain.Run{{
				SessionID: "session-invalid", WorkflowDefinitionID: "workflow-invalid",
				Status: domain.RunWaitingApproval, CurrentNodeID: "approve",
			}}
			repo.nodeRuns = []domain.NodeRun{{
				ID: "node-run-invalid", SessionID: "session-invalid", NodeID: "approve",
				Status: domain.NodeWaitingApproval, Attempt: 1,
			}}
			if _, err := service.SubmitApproval(ctx, SubmitApprovalInput{
				SessionID: "session-invalid", NodeID: "approve", Approved: true,
			}); err == nil || !strings.Contains(err.Error(), "cannot require after-run approval") {
				t.Fatalf("SubmitApproval() error = %v", err)
			}
			if len(repo.nodeRuns) != 1 || repo.nodeRuns[0].Status != domain.NodeWaitingApproval {
				t.Fatalf("invalid approval advanced into a loop: %#v", repo.nodeRuns)
			}
		})
	}
}

func TestSaveDefinitionRemovesApprovalFromNodeOutputFields(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := New(repo)
	service.generateID = func() (string, error) { return "workflow-1", nil }

	got, err := service.SaveDefinition(ctx, SaveDefinitionInput{
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{
					ID:   "approve",
					Type: "approval",
					OutputFields: []domain.OutputField{{
						Key:         "customNote",
						Description: "审批备注",
						ValueType:   "string",
					}, {
						Key:         "approval.approved",
						Description: "user supplied",
						ValueType:   "unsupported",
					}},
				},
				{ID: "merge", Type: "merge"},
			},
			Edges: []domain.Edge{{From: "approve", To: "merge"}},
		},
	})
	if err != nil {
		t.Fatalf("SaveDefinition() error = %v", err)
	}

	approve := got.Graph.Nodes[0]
	if hasOutputField(approve.OutputFields, "approval.approved", "boolean") || !hasOutputField(approve.OutputFields, "customNote", "string") {
		t.Fatalf("approval output fields = %#v", approve.OutputFields)
	}
	merge := got.Graph.Nodes[1]
	if !hasOutputField(merge.OutputFields, "merge.status", "string") ||
		!hasOutputField(merge.OutputFields, "merge.failureCode", "string") ||
		!hasOutputField(merge.OutputFields, "merge.failureReason", "string") {
		t.Fatalf("merge output fields = %#v", merge.OutputFields)
	}

	saved := repo.definitions["workflow-1"]
	if hasOutputField(saved.Graph.Nodes[0].OutputFields, "approval.approved", "boolean") ||
		!hasOutputField(saved.Graph.Nodes[1].OutputFields, "merge.status", "string") ||
		!hasOutputField(saved.Graph.Nodes[1].OutputFields, "merge.failureCode", "string") ||
		!hasOutputField(saved.Graph.Nodes[1].OutputFields, "merge.failureReason", "string") {
		t.Fatalf("saved graph = %#v", saved.Graph)
	}
}

func TestGetDefinitionKeepsApprovalOutsideNodeOutputFields(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "approve", Type: "approval"},
				{ID: "legacy-approve", Type: "manual_approval"},
				{ID: "approval-config", Type: "codex", Approval: domain.ApprovalConfig{BeforeRun: true}},
				{ID: "merge", Type: "merge"},
				{ID: "merge-config", Merge: &domain.MergeConfig{Strategy: "rebase"}},
			},
			Edges: []domain.Edge{
				{From: "approve", To: "legacy-approve"},
				{From: "legacy-approve", To: "approval-config"},
				{From: "approval-config", To: "merge"},
				{From: "merge", To: "merge-config"},
			},
		},
	}
	service := New(repo)

	got, err := service.GetDefinition(ctx, "workflow-1")
	if err != nil {
		t.Fatalf("GetDefinition() error = %v", err)
	}
	if hasOutputField(got.Graph.Nodes[0].OutputFields, "approval.approved", "boolean") {
		t.Fatalf("approval output fields = %#v", got.Graph.Nodes[0].OutputFields)
	}
	if hasOutputField(got.Graph.Nodes[1].OutputFields, "approval.approved", "boolean") {
		t.Fatalf("manual approval output fields = %#v", got.Graph.Nodes[1].OutputFields)
	}
	if hasOutputField(got.Graph.Nodes[2].OutputFields, "approval.approved", "boolean") {
		t.Fatalf("approval config output fields = %#v", got.Graph.Nodes[2].OutputFields)
	}
	if !hasOutputField(got.Graph.Nodes[3].OutputFields, "merge.status", "string") ||
		!hasOutputField(got.Graph.Nodes[3].OutputFields, "merge.failureCode", "string") ||
		!hasOutputField(got.Graph.Nodes[3].OutputFields, "merge.failureReason", "string") {
		t.Fatalf("merge output fields = %#v", got.Graph.Nodes[3].OutputFields)
	}
	if !hasOutputField(got.Graph.Nodes[4].OutputFields, "merge.status", "string") ||
		!hasOutputField(got.Graph.Nodes[4].OutputFields, "merge.failureCode", "string") ||
		!hasOutputField(got.Graph.Nodes[4].OutputFields, "merge.failureReason", "string") {
		t.Fatalf("merge config output fields = %#v", got.Graph.Nodes[4].OutputFields)
	}
}

func TestLegacyAfterRunApprovalDoesNotBecomeAgentResultData(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{
					ID: "plan", Type: "codex", Title: "Plan",
					Approval: domain.ApprovalConfig{AfterRun: true},
					OutputFields: []domain.OutputField{
						{Key: "approval.approved", Description: "Whether the human approval passed.", ValueType: "boolean"},
						{Key: "planPath", Description: "Plan path.", ValueType: "string"},
					},
				},
				{ID: "build", Type: "codex", Title: "Build"},
			},
			Edges: []domain.Edge{{From: "plan", To: "build"}},
		},
	}
	service := New(repo)
	ids := []string{"node-run-1"}
	service.generateID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	start, err := service.StartForSession(ctx, sessiondomain.WorkflowStartInput{
		ProjectID: "project-1", SessionID: "session-1", WorkflowDefinitionID: "workflow-1", Requirement: "write a plan",
	})
	if err != nil {
		t.Fatalf("StartForSession() error = %v", err)
	}
	if strings.Contains(start.Prompt, "approval.approved") || strings.Contains(start.Prompt, "Whether the human approval passed") {
		t.Fatalf("start prompt exposes workflow approval as agent output: %q", start.Prompt)
	}
	if !strings.Contains(start.Prompt, "Workflow-managed before-run or after-run approval") {
		t.Fatalf("start prompt does not distinguish workflow approval: %q", start.Prompt)
	}

	advance, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{"planPath": "docs/plan/example.md"}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if advance.RequireResultRetry || advance.Status != string(domain.RunWaitingApproval) || advance.ApprovalPhase != "after_run" {
		t.Fatalf("CompleteNode() = %#v", advance)
	}
	if len(repo.nodeRuns) != 1 || repo.nodeRuns[0].Result == nil || repo.nodeRuns[0].Result.Data["approval"] != nil {
		t.Fatalf("node result contains agent approval data: %#v", repo.nodeRuns)
	}
}

func TestStartForSessionWaitsWhenStartNodeRequiresApproval(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "approve", Type: "approval", Title: "Approve"}},
		},
	}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	ids := []string{"node-run-1"}
	service.generateID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.StartForSession(ctx, sessiondomain.WorkflowStartInput{
		ProjectID:            "project-1",
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
	})
	if err != nil {
		t.Fatalf("StartForSession() error = %v", err)
	}
	if got.RequiresCodex {
		t.Fatalf("approval node should not require codex: %#v", got)
	}
	if len(repo.runs) != 1 || repo.runs[0].Status != domain.RunWaitingApproval {
		t.Fatalf("runs = %#v", repo.runs)
	}
	if len(repo.nodeRuns) != 0 || got.NodeRunID != nil || repo.runs[0].PendingApproval == nil {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestStartForSessionReturnsMergeAdvanceForMergeStartNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "merge", Type: "merge", Title: "Merge", Merge: &domain.MergeConfig{Strategy: "rebase"}}},
		},
	}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	ids := []string{"node-run-1"}
	service.generateID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.StartForSession(ctx, sessiondomain.WorkflowStartInput{
		ProjectID:            "project-1",
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
	})
	if err != nil {
		t.Fatalf("StartForSession() error = %v", err)
	}
	if got.RequiresCodex || got.Merge == nil || got.Merge.Strategy != "rebase" {
		t.Fatalf("StartForSession() = %#v", got)
	}
	if len(repo.nodeRuns) != 1 || repo.nodeRuns[0].Status != domain.NodeRunning {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestStartForSessionClassifiesExprAsReady(t *testing.T) {
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID: "workflow-1", ProjectID: "project-1", Name: "default",
		Graph: domain.Graph{Nodes: []domain.Node{{ID: "expr", Type: "expr", Title: "Expr", Prompt: "params"}}},
	}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	ids := []string{"node-run-1"}
	service.generateID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	start, err := service.StartForSession(context.Background(), sessiondomain.WorkflowStartInput{
		ProjectID: "project-1", SessionID: "session-1", WorkflowDefinitionID: "workflow-1",
	})
	if err != nil {
		t.Fatalf("StartForSession() error = %v", err)
	}
	event := workflowEventInputFromStart(start)
	if start.Expr == nil || start.Status != string(domain.RunRunning) || event.eventType != "workflow.expr_ready" {
		t.Fatalf("start=%#v event=%#v", start, event)
	}
}

func TestStartForSessionDefersSystemNodeSideEffectsUntilBeforeRunApproval(t *testing.T) {
	cases := []struct {
		name string
		node domain.Node
	}{
		{name: "merge", node: domain.Node{ID: "merge", Type: "merge", Title: "Merge", Approval: domain.ApprovalConfig{BeforeRun: true}}},
		{name: "expr", node: domain.Node{ID: "expr", Type: "expr", Title: "Expr", Prompt: "params", Approval: domain.ApprovalConfig{BeforeRun: true}}},
		{name: "close", node: domain.Node{ID: "close", Type: "close", Title: "Close", Approval: domain.ApprovalConfig{BeforeRun: true}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			repo.definitions["workflow-1"] = domain.Definition{
				ID: "workflow-1", ProjectID: "project-1", Name: "default",
				Graph: domain.Graph{Nodes: []domain.Node{tt.node}},
			}
			service := New(repo)
			service.now = func() time.Time { return time.Unix(10, 0).UTC() }
			ids := []string{"node-run-1"}
			service.generateID = func() (string, error) {
				id := ids[0]
				ids = ids[1:]
				return id, nil
			}

			start, err := service.StartForSession(context.Background(), sessiondomain.WorkflowStartInput{
				ProjectID: "project-1", SessionID: "session-1", WorkflowDefinitionID: "workflow-1",
			})
			if err != nil {
				t.Fatalf("StartForSession() error = %v", err)
			}
			if start.Status != string(domain.RunWaitingApproval) || start.RequiresCodex || start.Merge != nil || start.Expr != nil || start.Close {
				t.Fatalf("StartForSession() exposed side effect before approval: %#v", start)
			}
			if start.Result != nil {
				t.Fatalf("StartForSession() before-run result = %#v, want nil", start.Result)
			}
			if repo.runs[0].StoppedAt != nil || len(repo.nodeRuns) != 0 || repo.runs[0].PendingApproval == nil {
				t.Fatalf("run=%#v nodeRuns=%#v", repo.runs[0], repo.nodeRuns)
			}

			approved, err := service.SubmitApprovalForSession(context.Background(), sessiondomain.WorkflowApprovalInput{
				SessionID: "session-1", NodeID: tt.node.ID, Approved: true,
			})
			if err != nil {
				t.Fatalf("SubmitApproval() error = %v", err)
			}
			switch tt.name {
			case "merge":
				if approved.Advance.Merge == nil || approved.Advance.Expr != nil || approved.Advance.Close {
					t.Fatalf("SubmitApproval() = %#v", approved.Advance)
				}
			case "expr":
				if approved.Advance.Expr == nil || approved.Advance.Merge != nil || approved.Advance.Close {
					t.Fatalf("SubmitApproval() = %#v", approved.Advance)
				}
				if event := workflowEventInputFromAdvance(approved.Advance); event.eventType != "workflow.expr_ready" {
					t.Fatalf("approved expr event = %#v", event)
				}
			case "close":
				if !approved.Advance.Close || approved.Run.Status != string(domain.RunCompleted) || len(repo.nodeRuns) != 1 || repo.nodeRuns[0].Status != domain.NodeSucceeded {
					t.Fatalf("SubmitApproval() = %#v nodeRuns=%#v", approved, repo.nodeRuns)
				}
			}
		})
	}
}

func TestCompleteNodeDefersNextSystemNodeSideEffectsUntilBeforeRunApproval(t *testing.T) {
	cases := []domain.Node{
		{ID: "merge", Type: "merge", Title: "Merge", Approval: domain.ApprovalConfig{BeforeRun: true}},
		{ID: "expr", Type: "expr", Title: "Expr", Prompt: "params", Approval: domain.ApprovalConfig{BeforeRun: true}},
		{ID: "close", Type: "close", Title: "Close", Approval: domain.ApprovalConfig{BeforeRun: true}},
	}
	for _, target := range cases {
		t.Run(target.Type, func(t *testing.T) {
			repo := newFakeRepository()
			repo.definitions["workflow-1"] = domain.Definition{
				ID: "workflow-1", ProjectID: "project-1", Name: "default",
				Graph: domain.Graph{
					Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build"}, target},
					Edges: []domain.Edge{{From: "build", To: target.ID}},
				},
			}
			repo.runs = []domain.Run{{
				SessionID: "session-1", WorkflowDefinitionID: "workflow-1",
				Status: domain.RunRunning, CurrentNodeID: "build", Context: domain.Context{Values: map[string]any{}},
			}}
			repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
			service := New(repo)
			service.now = func() time.Time { return time.Unix(10, 0).UTC() }
			service.generateID = func() (string, error) { return "node-run-2", nil }

			advance, err := service.CompleteNode(context.Background(), sessiondomain.WorkflowNodeCompleteInput{
				SessionID: "session-1", NodeRunID: "node-run-1", Output: resultOutput(map[string]any{"ok": true}),
			})
			if err != nil {
				t.Fatalf("CompleteNode() error = %v", err)
			}
			if advance.Status != string(domain.RunWaitingApproval) || advance.Merge != nil || advance.Expr != nil || advance.Close {
				t.Fatalf("CompleteNode() exposed side effect before approval: %#v", advance)
			}
			if advance.Result != nil {
				t.Fatalf("CompleteNode() before-run result = %#v, want nil", advance.Result)
			}
			if repo.runs[0].StoppedAt != nil || len(repo.nodeRuns) != 1 || repo.runs[0].PendingApproval == nil || advance.NodeRunID != nil {
				t.Fatalf("run=%#v nodeRuns=%#v", repo.runs[0], repo.nodeRuns)
			}
		})
	}
}

func TestStartForSessionRejectsAmbiguousStartNodes(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "one", Type: "codex"},
				{ID: "two", Type: "codex"},
			},
		},
	}
	service := New(repo)
	service.generateID = func() (string, error) { return "unused", nil }

	if _, err := service.StartForSession(ctx, sessiondomain.WorkflowStartInput{
		ProjectID:            "project-1",
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
	}); err == nil {
		t.Fatal("StartForSession() expected ambiguous start node error")
	} else {
		appErr, ok := apperror.From(err)
		if !ok || appErr.Code != apperror.CodeValidationFailed || appErr.Category != apperror.CategoryWorkflowError {
			t.Fatalf("StartForSession() app error = %#v", err)
		}
	}
	if len(repo.runs) != 0 || len(repo.nodeRuns) != 0 {
		t.Fatalf("workflow run should not be created: runs=%#v nodeRuns=%#v", repo.runs, repo.nodeRuns)
	}
}

func TestCompleteNodeAdvancesToNextExecutableNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build"},
				{ID: "verify", Type: "codex", Title: "Verify", Prompt: "Verify result"},
			},
			Edges: []domain.Edge{{From: "build", To: "verify"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{"requirement": "ship"}},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    domain.NodeRunning,
		Attempt:   1,
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{"ok": true}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if !got.RequiresCodex || got.NodeRunID == nil || *got.NodeRunID != "node-run-2" || got.CurrentNodeTitle != "Verify" {
		t.Fatalf("CompleteNode() = %#v", got)
	}
	if repo.runs[0].CurrentNodeID != "verify" || repo.runs[0].Status != domain.RunRunning {
		t.Fatalf("run = %#v", repo.runs[0])
	}
	if repo.nodeRuns[0].Status != domain.NodeSucceeded || len(repo.nodeRuns) != 2 || repo.nodeRuns[1].NodeID != "verify" {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestCompleteNodeWaitsForAfterRunApproval(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build", Approval: domain.ApprovalConfig{AfterRun: true}},
				{ID: "verify", Type: "codex", Title: "Verify"},
			},
			Edges: []domain.Edge{{From: "build", To: "verify"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{"status": "passed"}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if got.Status != string(domain.RunWaitingApproval) || got.RequiresCodex || got.CurrentNodeID != "build" {
		t.Fatalf("CompleteNode() = %#v", got)
	}
	if repo.runs[0].Status != domain.RunWaitingApproval || repo.nodeRuns[0].Status != domain.NodeWaitingApproval || repo.nodeRuns[0].FinishedAt != nil {
		t.Fatalf("run=%#v nodeRun=%#v", repo.runs[0], repo.nodeRuns[0])
	}
	if repo.nodeRuns[0].Result == nil || repo.nodeRuns[0].Result.Data["status"] != "passed" {
		t.Fatalf("node result = %#v", repo.nodeRuns[0].Result)
	}
}

func TestCompleteNodeAdvancesToCloseNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build"},
				{ID: "close", Type: "close", Title: "Close"},
			},
			Edges: []domain.Edge{{From: "build", To: "close"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    domain.NodeRunning,
		Attempt:   1,
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-close", nil }

	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{"status": "done"}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if !got.Close || got.CurrentNodeID != "close" || got.RequiresCodex {
		t.Fatalf("CompleteNode() = %#v", got)
	}
	if repo.runs[0].Status != domain.RunCompleted || repo.runs[0].CurrentNodeID != "close" {
		t.Fatalf("run = %#v", repo.runs[0])
	}
	if len(repo.nodeRuns) != 2 || repo.nodeRuns[1].NodeID != "close" || repo.nodeRuns[1].Status != domain.NodeSucceeded {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestRecoverProcessExitReturnsPersistedAdvanceWithoutCompletingNodeAgain(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Graph: domain.Graph{Nodes: []domain.Node{
			{ID: "build", Type: "codex", Title: "Build"},
			{ID: "verify", Type: "codex", Title: "Verify", Prompt: "Verify result"},
		}},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "verify",
		Context:              domain.Context{Values: map[string]any{"params": map[string]any{"artifact": "ready"}}},
	}}
	repo.nodeRuns = []domain.NodeRun{
		{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeSucceeded, Attempt: 1},
		{ID: "node-run-2", SessionID: "session-1", NodeID: "verify", Status: domain.NodeRunning, Attempt: 1},
	}
	service := New(repo)

	got, err := service.RecoverProcessExit(ctx, sessiondomain.WorkflowProcessExitInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{"artifact": "ready"}),
	})
	if err != nil {
		t.Fatalf("RecoverProcessExit() error = %v", err)
	}
	if !got.RequiresCodex || got.NodeRunID == nil || *got.NodeRunID != "node-run-2" || got.CurrentNodeID != "verify" {
		t.Fatalf("RecoverProcessExit() = %#v", got)
	}
	if len(repo.nodeRuns) != 2 || repo.nodeRuns[0].Status != domain.NodeSucceeded {
		t.Fatalf("node runs mutated during recovery: %#v", repo.nodeRuns)
	}
}

func TestCompleteNodeReturnsPersistedAdvanceForStaleNodeRun(t *testing.T) {
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID: "workflow-1", ProjectID: "project-1",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "expr"}, {ID: "verify", Type: "codex", Title: "Verify"}},
			Edges: []domain.Edge{{From: "build", To: "verify"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID: "session-1", WorkflowDefinitionID: "workflow-1",
		Status: domain.RunRunning, CurrentNodeID: "verify", Context: domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{
		{ID: "node-run-build", SessionID: "session-1", NodeID: "build", Status: domain.NodeSucceeded, Attempt: 1, Result: successResult(map[string]any{})},
		{ID: "node-run-verify", SessionID: "session-1", NodeID: "verify", Status: domain.NodeRunning, Attempt: 1},
	}

	advance, err := New(repo).CompleteNode(context.Background(), sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1", NodeRunID: "node-run-build", CommandID: "command-1", Output: resultOutput(map[string]any{}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if advance.CurrentNodeID != "verify" || advance.NodeRunID == nil || *advance.NodeRunID != "node-run-verify" || !advance.RequiresCodex {
		t.Fatalf("CompleteNode() = %#v", advance)
	}
	if repo.nodeRuns[0].Status != domain.NodeSucceeded || len(repo.nodeRuns) != 2 {
		t.Fatalf("stale completion mutated node runs = %#v", repo.nodeRuns)
	}
}

func TestRecoverProcessExitKeepsPersistedSystemNodeWaitingForApproval(t *testing.T) {
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID: "workflow-1", ProjectID: "project-1",
		Graph: domain.Graph{Nodes: []domain.Node{{
			ID: "merge", Type: "merge", Title: "Merge", Approval: domain.ApprovalConfig{BeforeRun: true},
		}}},
	}
	repo.runs = []domain.Run{{
		SessionID: "session-1", WorkflowDefinitionID: "workflow-1",
		Status: domain.RunWaitingApproval, CurrentNodeID: "merge", Context: domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID: "node-run-1", SessionID: "session-1", NodeID: "merge", Status: domain.NodeWaitingApproval, Attempt: 1,
	}}
	service := New(repo)

	advance, err := service.RecoverProcessExit(context.Background(), sessiondomain.WorkflowProcessExitInput{
		SessionID: "session-1", NodeRunID: "node-run-1",
	})
	if err != nil {
		t.Fatalf("RecoverProcessExit() error = %v", err)
	}
	if advance.Status != string(domain.RunWaitingApproval) || advance.ApprovalPhase != "before_run" || advance.Merge != nil || advance.Expr != nil || advance.Close {
		t.Fatalf("RecoverProcessExit() exposed side effect before approval: %#v", advance)
	}
}

func TestRecoverProcessExitReturnsPersistedFailedRun(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.runs = []domain.Run{{
		SessionID:     "session-1",
		Status:        domain.RunFailed,
		CurrentNodeID: "build",
		Context:       domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeFailed, Attempt: 1}}
	service := New(repo)

	got, err := service.RecoverProcessExit(ctx, sessiondomain.WorkflowProcessExitInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
	})
	if err != nil {
		t.Fatalf("RecoverProcessExit() error = %v", err)
	}
	if got.SessionID != "session-1" || got.Status != "failed" {
		t.Fatalf("RecoverProcessExit() = %#v", got)
	}
}

func TestCompleteNodeEvaluatesExprAndPassesResultsAsNextParams(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build"},
				{ID: "verify", Type: "codex", Title: "Verify", Prompt: "Verify result"},
			},
			Edges: []domain.Edge{{
				From:      "build",
				To:        "verify",
				Condition: domain.Condition{Mode: "expr", Expr: `results.data.status == "passed" && params.requirement == "ship"`},
			}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{"params": map[string]any{"requirement": "ship"}}},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    domain.NodeRunning,
		Attempt:   1,
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{"status": "passed", "artifact": "ready"}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if !got.RequiresCodex || got.CurrentNodeID != "verify" {
		t.Fatalf("CompleteNode() = %#v", got)
	}
	if !strings.Contains(got.Prompt, `"status": "passed"`) || strings.Contains(got.Prompt, `"requirement": "ship"`) {
		t.Fatalf("Prompt = %q", got.Prompt)
	}
	params, ok := repo.runs[0].Context.Values["params"].(map[string]any)
	data, dataOK := params["data"].(map[string]any)
	if !ok || !dataOK || data["status"] != "passed" || data["artifact"] != "ready" {
		t.Fatalf("params = %#v", repo.runs[0].Context.Values["params"])
	}
}

func TestCompleteNodeRequestsResultRetryWhenCodexOutputMissingResults(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build"},
				{ID: "verify", Type: "codex", Title: "Verify"},
			},
			Edges: []domain.Edge{{From: "build", To: "verify"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)

	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    map[string]any{"text": "done"},
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if !got.RequiresCodex || !got.RequireResultRetry || got.NodeRunID == nil || *got.NodeRunID != "node-run-1" || got.CurrentNodeID != "build" {
		t.Fatalf("CompleteNode() = %#v", got)
	}
	if !strings.Contains(got.Prompt, "ANYCODE_WORKFLOW_RESULT_RETRY") {
		t.Fatalf("Prompt = %q", got.Prompt)
	}
	if repo.nodeRuns[0].Status != domain.NodeRunning || repo.runs[0].CurrentNodeID != "build" {
		t.Fatalf("workflow should stay on current node: run=%#v nodeRuns=%#v", repo.runs[0], repo.nodeRuns)
	}
}

func TestCompleteNodeAcceptsEmptyResultsJSON(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build"},
				{ID: "verify", Type: "codex", Title: "Verify"},
			},
			Edges: []domain.Edge{{From: "build", To: "verify"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if got.RequireResultRetry || got.NodeRunID == nil || *got.NodeRunID != "node-run-2" || got.CurrentNodeID != "verify" {
		t.Fatalf("CompleteNode() = %#v", got)
	}
}

func TestCompleteNodeRequiresAnswerUserBeforeFinalResult(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID: "workflow-1", ProjectID: "project-1", Name: "default",
		Graph: domain.Graph{Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build"}}},
	}
	repo.runs = []domain.Run{{
		SessionID: "session-1", WorkflowDefinitionID: "workflow-1",
		Status: domain.RunRunning, CurrentNodeID: "build", Context: domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)

	invalid := resultOutput(map[string]any{"status": "ready"})
	invalid["results"].(map[string]any)["questions"] = []any{"Proceed?"}
	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1", NodeRunID: "node-run-1", Output: invalid,
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if !got.RequiresCodex || !got.RequireResultRetry || got.NodeRunID == nil || *got.NodeRunID != "node-run-1" {
		t.Fatalf("CompleteNode() = %#v", got)
	}
	if !strings.Contains(got.Prompt, "`answer_user`") || !strings.Contains(got.Prompt, "Do not describe pending questions") {
		t.Fatalf("correction prompt = %q", got.Prompt)
	}
	if repo.nodeRuns[0].Status != domain.NodeRunning || repo.nodeRuns[0].Result != nil {
		t.Fatalf("node completed before answer_user: %#v", repo.nodeRuns[0])
	}
}

func TestCompleteNodeFailsAfterResultRetryStillMissingResults(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex"}, {ID: "verify", Type: "codex"}},
			Edges: []domain.Edge{{From: "build", To: "verify"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)

	_, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    map[string]any{"resultRetry": true, "text": "still not json"},
	})
	if err == nil || !strings.Contains(err.Error(), "workflow node result is invalid after correction") {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if repo.nodeRuns[0].Status != domain.NodeRunning || repo.runs[0].Status != domain.RunRunning {
		t.Fatalf("workflow should not complete node: run=%#v nodeRuns=%#v", repo.runs[0], repo.nodeRuns)
	}
}

func TestCompleteNodeAdvancesToMergeNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build"},
				{ID: "merge", Type: "merge", Title: "Merge", Merge: &domain.MergeConfig{Strategy: "merge"}},
			},
			Edges: []domain.Edge{{From: "build", To: "merge"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    domain.NodeRunning,
		Attempt:   1,
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{"ok": true}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if got.RequiresCodex || got.Merge == nil || got.Merge.Strategy != "merge" || got.NodeRunID == nil || *got.NodeRunID != "node-run-2" {
		t.Fatalf("CompleteNode() = %#v", got)
	}
	if repo.runs[0].CurrentNodeID != "merge" || repo.runs[0].Status != domain.RunRunning {
		t.Fatalf("run = %#v", repo.runs[0])
	}
	if repo.nodeRuns[0].Status != domain.NodeSucceeded || len(repo.nodeRuns) != 2 || repo.nodeRuns[1].NodeID != "merge" {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestCompleteNodeBlocksWhenNoEdgeMatches(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex"}, {ID: "verify", Type: "codex"}},
			Edges: []domain.Edge{{From: "build", To: "verify", Condition: domain.Condition{Field: "last.output.ok", Op: "eq", Value: true}}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.CompleteNode(ctx, sessiondomain.WorkflowNodeCompleteInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Output:    resultOutput(map[string]any{"ok": false}),
	})
	if err != nil {
		t.Fatalf("CompleteNode() error = %v", err)
	}
	if !got.Blocked || got.BlockedReason == "" {
		t.Fatalf("CompleteNode() = %#v", got)
	}
	if repo.runs[0].Status != domain.RunBlocked {
		t.Fatalf("run = %#v", repo.runs[0])
	}
}

func TestFailNodeRetriesCurrentNodeBeforeMaxAttempts(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build", Prompt: "Build it", Retry: domain.RetryConfig{MaxAttempts: 2}}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.FailNode(ctx, sessiondomain.WorkflowNodeFailInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Code:      "codex_start_failed",
		Message:   "temporary failure",
	})
	if err != nil {
		t.Fatalf("FailNode() error = %v", err)
	}
	if !got.RequiresCodex || got.NodeRunID == nil || *got.NodeRunID != "node-run-2" || got.CurrentNodeID != "build" {
		t.Fatalf("FailNode() = %#v", got)
	}
	if repo.nodeRuns[0].Status != domain.NodeFailed || len(repo.nodeRuns) != 2 || repo.nodeRuns[1].Attempt != 2 || repo.nodeRuns[1].NodeID != "build" {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
	if repo.runs[0].Status != domain.RunRunning || repo.runs[0].CurrentNodeID != "build" {
		t.Fatalf("run = %#v", repo.runs[0])
	}
}

func TestFailNodeBlocksAfterRetriesExhaustedWithoutEvaluatingEdges(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		target domain.Node
		edge   domain.Edge
	}{
		{
			name:   "conditional failure edge",
			target: domain.Node{ID: "repair", Type: "codex", Title: "Repair"},
			edge:   domain.Edge{From: "build", To: "repair", Condition: domain.Condition{Field: "last.status", Op: "eq", Value: "failed"}},
		},
		{
			name:   "unconditional edge",
			target: domain.Node{ID: "push", Type: "codex", Title: "Push"},
			edge:   domain.Edge{From: "build", To: "push"},
		},
		{
			name:   "close edge",
			target: domain.Node{ID: "close", Type: "close", Title: "Close"},
			edge:   domain.Edge{From: "build", To: "close"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			repo.definitions["workflow-1"] = domain.Definition{
				ID: "workflow-1", ProjectID: "project-1", Name: "default",
				Graph: domain.Graph{
					Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build", Retry: domain.RetryConfig{MaxAttempts: 2}}, tt.target},
					Edges: []domain.Edge{tt.edge},
				},
			}
			repo.runs = []domain.Run{{
				SessionID: "session-1", WorkflowDefinitionID: "workflow-1",
				Status: domain.RunRunning, CurrentNodeID: "build", Context: domain.Context{Values: map[string]any{}},
			}}
			repo.nodeRuns = []domain.NodeRun{{ID: "node-run-2", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 2}}
			service := New(repo)
			service.now = func() time.Time { return time.Unix(11, 0).UTC() }

			got, err := service.FailNode(ctx, sessiondomain.WorkflowNodeFailInput{
				SessionID: "session-1", NodeRunID: "node-run-2",
				Code: "codex_start_failed", Message: "permanent failure",
			})
			if err != nil {
				t.Fatalf("FailNode() error = %v", err)
			}
			if !got.Blocked || got.Close || got.RequiresCodex || got.BlockedReason != "permanent failure" {
				t.Fatalf("FailNode() = %#v", got)
			}
			if repo.runs[0].Status != domain.RunBlocked || repo.runs[0].CurrentNodeID != "build" {
				t.Fatalf("run = %#v", repo.runs[0])
			}
			if repo.nodeRuns[0].Status != domain.NodeFailed || len(repo.nodeRuns) != 1 {
				t.Fatalf("node runs = %#v", repo.nodeRuns)
			}
		})
	}
}

func TestFailNodeUsesProvidedOutputForFailureBranchContext(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "merge", Type: "merge", Title: "Merge"},
				{ID: "repair", Type: "codex", Title: "Repair merge"},
			},
			Edges: []domain.Edge{{
				From:      "merge",
				To:        "repair",
				Condition: domain.Condition{Field: "results.merge.status", Op: "eq", Value: "failed"},
			}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "merge",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-merge", SessionID: "session-1", NodeID: "merge", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(12, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-repair", nil }

	got, err := service.FailNode(ctx, sessiondomain.WorkflowNodeFailInput{
		SessionID: "session-1",
		NodeRunID: "node-run-merge",
		Code:      "dirty_worktree",
		Message:   "worktree has uncommitted changes",
		Output: resultOutput(map[string]any{
			"merge": map[string]any{
				"status":        "failed",
				"failureCode":   "dirty_worktree",
				"failureReason": "worktree has uncommitted changes",
			},
		}),
	})
	if err != nil {
		t.Fatalf("FailNode() error = %v", err)
	}
	if !got.Blocked || got.RequiresCodex || got.CurrentNodeID != "" {
		t.Fatalf("FailNode() = %#v", got)
	}
	if repo.nodeRuns[0].Status != domain.NodeFailed || len(repo.nodeRuns) != 1 {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
	results, ok := repo.runs[0].Context.Values["results"].(map[string]any)
	if !ok {
		t.Fatalf("results context = %#v", repo.runs[0].Context.Values)
	}
	data, ok := results["data"].(map[string]any)
	if !ok {
		t.Fatalf("result data = %#v", results)
	}
	merge, ok := data["merge"].(map[string]any)
	if !ok || merge["status"] != "failed" || merge["failureCode"] != "dirty_worktree" {
		t.Fatalf("merge context = %#v", repo.runs[0].Context.Values)
	}
	failure, ok := data["failure"].(map[string]any)
	if !ok || failure["code"] != "dirty_worktree" || failure["message"] != "worktree has uncommitted changes" {
		t.Fatalf("failure context = %#v", repo.runs[0].Context.Values)
	}
}

func TestFailNodeBlocksWhenNoFailureBranchMatches(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(12, 0).UTC() }

	got, err := service.FailNode(ctx, sessiondomain.WorkflowNodeFailInput{
		SessionID: "session-1",
		NodeRunID: "node-run-1",
		Code:      "codex_start_failed",
		Message:   "failed",
	})
	if err != nil {
		t.Fatalf("FailNode() error = %v", err)
	}
	if !got.Blocked || got.BlockedReason != "failed" || got.BlockedCode != "codex_start_failed" || got.BlockedMessage != "failed" {
		t.Fatalf("FailNode() = %#v", got)
	}
	if repo.runs[0].Status != domain.RunBlocked {
		t.Fatalf("run = %#v", repo.runs[0])
	}
	blockedFailure, ok := repo.runs[0].Context.Values["blockedFailure"].(map[string]any)
	if !ok || blockedFailure["code"] != "codex_start_failed" || blockedFailure["message"] != "failed" {
		t.Fatalf("blocked failure = %#v", repo.runs[0].Context.Values["blockedFailure"])
	}
}

func TestMarkResumeFailedForSessionKeepsCurrentNodeAndWaitsForAction(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunRunning,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{"requirement": "ship"}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(12, 0).UTC() }

	got, err := service.MarkResumeFailedForSession(ctx, sessiondomain.WorkflowResumeFailureInput{
		SessionID: "session-1",
		Code:      "resume_failed",
		Message:   "codex session missing",
	})
	if err != nil {
		t.Fatalf("MarkResumeFailedForSession() error = %v", err)
	}
	if got.Status != string(domain.RunWaitingResumeAction) || got.CurrentNodeID != "build" {
		t.Fatalf("MarkResumeFailedForSession() = %#v", got)
	}
	if repo.runs[0].Status != domain.RunWaitingResumeAction || repo.runs[0].CurrentNodeID != "build" {
		t.Fatalf("run = %#v", repo.runs[0])
	}
	if repo.nodeRuns[0].Status != domain.NodeRunning || repo.nodeRuns[0].FinishedAt != nil {
		t.Fatalf("node run = %#v", repo.nodeRuns[0])
	}
	resume, ok := repo.runs[0].Context.Values["resume"].(map[string]any)
	if !ok || resume["status"] != "failed" || resume["code"] != "resume_failed" {
		t.Fatalf("resume context = %#v", repo.runs[0].Context.Values["resume"])
	}
}

func TestMarkResumeFailedForSessionIsIdempotentWhileWaitingForAction(t *testing.T) {
	ctx := context.Background()
	finishedAt := time.Unix(8, 0).UTC()
	repo := newFakeRepository()
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingResumeAction,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{"resume": map[string]any{"status": "failed"}}},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID:         "node-run-1",
		SessionID:  "session-1",
		NodeID:     "build",
		Status:     domain.NodeFailed,
		Attempt:    1,
		FinishedAt: &finishedAt,
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(12, 0).UTC() }

	got, err := service.MarkResumeFailedForSession(ctx, sessiondomain.WorkflowResumeFailureInput{
		SessionID: "session-1",
		Code:      "resume_failed",
		Message:   "resume unavailable",
	})
	if err != nil {
		t.Fatalf("MarkResumeFailedForSession() error = %v", err)
	}
	if got.Status != string(domain.RunWaitingResumeAction) {
		t.Fatalf("MarkResumeFailedForSession() = %#v", got)
	}
	if repo.nodeRuns[0].FinishedAt == nil || !repo.nodeRuns[0].FinishedAt.Equal(finishedAt) {
		t.Fatalf("node run was rewritten: %#v", repo.nodeRuns[0])
	}
}

func TestResumeCurrentNodeForSessionBindsExistingAttempt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build", Prompt: "Build now", Retry: domain.RetryConfig{MaxAttempts: 3}}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingResumeAction,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{"requirement": "ship"}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeFailed, Attempt: 1}}
	service := New(repo)

	got, err := service.ResumeCurrentNodeForSession(ctx, sessiondomain.WorkflowResumeCurrentNodeInput{
		SessionID: "session-1",
		Reason:    "try resume again",
	})
	if err != nil {
		t.Fatalf("ResumeCurrentNodeForSession() error = %v", err)
	}
	if !got.RequiresCodex || got.NodeRunID == nil || *got.NodeRunID != "node-run-1" || got.CurrentNodeID != "build" {
		t.Fatalf("ResumeCurrentNodeForSession() = %#v", got)
	}
	if len(repo.nodeRuns) != 1 {
		t.Fatalf("resume should not create a new node run: %#v", repo.nodeRuns)
	}
	if repo.runs[0].Status != domain.RunRunning || repo.runs[0].CurrentNodeID != "build" {
		t.Fatalf("run = %#v", repo.runs[0])
	}
	resume, ok := repo.runs[0].Context.Values["resume"].(map[string]any)
	if !ok || resume["status"] != "retry_requested" {
		t.Fatalf("resume context = %#v", repo.runs[0].Context.Values["resume"])
	}
}

func TestResumeCurrentNodeForSessionRejectsNonCodexNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Graph: domain.Graph{Nodes: []domain.Node{{
			ID: "merge", Type: "merge", Title: "Merge", Merge: &domain.MergeConfig{Strategy: "merge"},
		}}},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingResumeAction,
		CurrentNodeID:        "merge",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "merge", Status: domain.NodeFailed, Attempt: 1}}
	service := New(repo)

	_, err := service.ResumeCurrentNodeForSession(ctx, sessiondomain.WorkflowResumeCurrentNodeInput{SessionID: "session-1"})
	if err == nil || !strings.Contains(err.Error(), "cannot resume a Codex process") {
		t.Fatalf("ResumeCurrentNodeForSession() error = %v", err)
	}
}

func TestRerunCurrentNodeForSessionCreatesNextAttempt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build", Prompt: "Build now", Retry: domain.RetryConfig{MaxAttempts: 3}}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingResumeAction,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{"requirement": "ship"}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(12, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.RerunCurrentNodeForSession(ctx, sessiondomain.WorkflowRerunCurrentNodeInput{
		SessionID: "session-1",
		Reason:    "resume failed",
	})
	if err != nil {
		t.Fatalf("RerunCurrentNodeForSession() error = %v", err)
	}
	if !got.RequiresCodex || got.NodeRunID == nil || *got.NodeRunID != "node-run-2" || got.CurrentNodeID != "build" {
		t.Fatalf("RerunCurrentNodeForSession() = %#v", got)
	}
	if repo.runs[0].Status != domain.RunRunning || repo.runs[0].CurrentNodeID != "build" {
		t.Fatalf("run = %#v", repo.runs[0])
	}
	if len(repo.nodeRuns) != 2 || repo.nodeRuns[1].Attempt != 2 || repo.nodeRuns[1].NodeID != "build" {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
	resume, ok := repo.runs[0].Context.Values["resume"].(map[string]any)
	if !ok || resume["status"] != "rerun_requested" {
		t.Fatalf("resume context = %#v", repo.runs[0].Context.Values["resume"])
	}
}

func TestRerunCurrentNodeForSessionBlocksWhenRetryLimitReached(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "build", Type: "codex", Title: "Build", Retry: domain.RetryConfig{MaxAttempts: 1}}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingResumeAction,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeRunning, Attempt: 1}}
	service := New(repo)

	got, err := service.RerunCurrentNodeForSession(ctx, sessiondomain.WorkflowRerunCurrentNodeInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("RerunCurrentNodeForSession() error = %v", err)
	}
	if !got.Blocked || got.BlockedReason != "workflow node retry limit reached" {
		t.Fatalf("RerunCurrentNodeForSession() = %#v", got)
	}
	if repo.runs[0].Status != domain.RunBlocked || len(repo.nodeRuns) != 1 {
		t.Fatalf("run=%#v nodeRuns=%#v", repo.runs[0], repo.nodeRuns)
	}
}

func TestBeforeRunApprovalPreservesRetryAttemptWithoutPlaceholderNodeRun(t *testing.T) {
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{ID: "workflow-1", ProjectID: "project-1", Graph: domain.Graph{Nodes: []domain.Node{{
		ID: "build", Type: "codex", Title: "Build", Approval: domain.ApprovalConfig{BeforeRun: true}, Retry: domain.RetryConfig{MaxAttempts: 2},
	}}}}
	repo.runs = []domain.Run{{SessionID: "session-1", WorkflowDefinitionID: "workflow-1", Status: domain.RunWaitingResumeAction, CurrentNodeID: "build", Context: domain.Context{Values: map[string]any{}}}}
	repo.nodeRuns = []domain.NodeRun{{ID: "node-run-1", SessionID: "session-1", NodeID: "build", Status: domain.NodeFailed, Attempt: 1}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	advance, err := service.RerunCurrentNodeForSession(context.Background(), sessiondomain.WorkflowRerunCurrentNodeInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("RerunCurrentNodeForSession() error = %v", err)
	}
	if advance.NodeRunID != nil || repo.runs[0].PendingApproval == nil || repo.runs[0].PendingApproval.Attempt != 2 || len(repo.nodeRuns) != 1 {
		t.Fatalf("advance=%#v run=%#v nodeRuns=%#v", advance, repo.runs[0], repo.nodeRuns)
	}
	if _, err := service.SubmitApproval(context.Background(), SubmitApprovalInput{SessionID: "session-1", NodeID: "build", Approved: true}); err != nil {
		t.Fatalf("SubmitApproval() error = %v", err)
	}
	if len(repo.nodeRuns) != 2 || repo.nodeRuns[1].Attempt != 2 || repo.nodeRuns[1].ID != "node-run-2" {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestSubmitApprovalApprovesAndAdvancesWorkflow(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "approve", Type: "approval", Title: "Approve"},
				{ID: "build", Type: "codex", Title: "Build"},
			},
			Edges: []domain.Edge{{From: "approve", To: "build"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingApproval,
		CurrentNodeID:        "approve",
		Context:              domain.Context{Values: map[string]any{}},
		PendingApproval:      &domain.PendingApproval{Phase: domain.ApprovalBeforeRun, NodeID: "approve"},
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.SubmitApproval(ctx, SubmitApprovalInput{
		SessionID: "session-1",
		NodeID:    "approve",
		Approved:  true,
		Comment:   "go",
	})
	if err != nil {
		t.Fatalf("SubmitApproval() error = %v", err)
	}
	if got.Status != domain.RunRunning || got.CurrentNodeID != "build" {
		t.Fatalf("SubmitApproval() = %#v", got)
	}
	if len(repo.nodeRuns) != 1 || repo.nodeRuns[0].NodeID != "build" || repo.nodeRuns[0].Status != domain.NodeRunning {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestSubmitAfterRunApprovalApprovesAndAdvancesWorkflow(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build", Approval: domain.ApprovalConfig{AfterRun: true}},
				{ID: "verify", Type: "codex", Title: "Verify"},
			},
			Edges: []domain.Edge{{From: "build", To: "verify", Condition: domain.Condition{Field: "approval.approved", Op: "eq", Value: true}}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingApproval,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
		PendingApproval:      &domain.PendingApproval{Phase: domain.ApprovalAfterRun, NodeID: "build", Attempt: 1},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    domain.NodeWaitingApproval,
		Attempt:   1,
		Result:    successResult(map[string]any{"status": "passed"}),
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	got, err := service.SubmitApproval(ctx, SubmitApprovalInput{
		SessionID: "session-1",
		NodeID:    "build",
		Approved:  true,
	})
	if err != nil {
		t.Fatalf("SubmitApproval() error = %v", err)
	}
	if got.Status != domain.RunRunning || got.CurrentNodeID != "verify" {
		t.Fatalf("SubmitApproval() = %#v", got)
	}
	if len(repo.nodeRuns) != 2 || repo.nodeRuns[0].Status != domain.NodeSucceeded || repo.nodeRuns[1].NodeID != "verify" {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
	if _, exists := repo.runs[0].Context.Values["approval"]; exists {
		t.Fatalf("approval leaked into next node context = %#v", repo.runs[0].Context)
	}
}

func TestSubmitAfterRunApprovalRejectsAndRerunsCurrentNodeWithoutBeforeApproval(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{
					ID:       "build",
					Type:     "codex",
					Title:    "Build",
					Approval: domain.ApprovalConfig{BeforeRun: true, AfterRun: true},
				},
			},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingApproval,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
		PendingApproval:      &domain.PendingApproval{Phase: domain.ApprovalAfterRun, NodeID: "build", Attempt: 1},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    domain.NodeWaitingApproval,
		Attempt:   1,
		Result:    successResult(map[string]any{"status": "failed"}),
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	result, err := service.SubmitApprovalForSession(ctx, sessiondomain.WorkflowApprovalInput{
		SessionID: "session-1",
		NodeID:    "build",
		Approved:  false,
		Comment:   "fix it",
	})
	if err != nil {
		t.Fatalf("SubmitApprovalForSession() error = %v", err)
	}
	if !result.RejectedAfterRun || result.Advance.Status != string(domain.RunRunning) || !result.Advance.RequiresCodex {
		t.Fatalf("SubmitApprovalForSession() = %#v", result)
	}
	if len(repo.nodeRuns) != 1 || repo.nodeRuns[0].Status != domain.NodeRunning || repo.nodeRuns[0].Attempt != 1 || repo.nodeRuns[0].Result != nil {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestSubmitAfterRunApprovalRejectsAndRerunsCurrentNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{
				{ID: "build", Type: "codex", Title: "Build", Approval: domain.ApprovalConfig{AfterRun: true}},
				{ID: "verify", Type: "codex", Title: "Verify"},
			},
			Edges: []domain.Edge{{From: "build", To: "verify"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingApproval,
		CurrentNodeID:        "build",
		Context:              domain.Context{Values: map[string]any{}},
		PendingApproval:      &domain.PendingApproval{Phase: domain.ApprovalAfterRun, NodeID: "build", Attempt: 1},
	}}
	repo.nodeRuns = []domain.NodeRun{{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    domain.NodeWaitingApproval,
		Attempt:   1,
		Result:    successResult(map[string]any{"status": "failed"}),
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-2", nil }

	result, err := service.SubmitApprovalForSession(ctx, sessiondomain.WorkflowApprovalInput{
		SessionID: "session-1",
		NodeID:    "build",
		Approved:  false,
		Comment:   "fix the failing checks",
	})
	if err != nil {
		t.Fatalf("SubmitApprovalForSession() error = %v", err)
	}
	if !result.RejectedAfterRun || !result.Advance.RequiresCodex || result.Advance.CurrentNodeID != "build" {
		t.Fatalf("SubmitApprovalForSession() = %#v", result)
	}
	if len(repo.nodeRuns) != 1 || repo.nodeRuns[0].Status != domain.NodeRunning || repo.nodeRuns[0].ID != "node-run-1" || repo.nodeRuns[0].Attempt != 1 || repo.nodeRuns[0].Result != nil {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
	if repo.runs[0].Status != domain.RunRunning || repo.runs[0].CurrentNodeID != "build" {
		t.Fatalf("run = %#v", repo.runs[0])
	}
}

func TestSubmitApprovalRejectsTerminalApprovalNodeWithoutNodeRun(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Graph: domain.Graph{
			Nodes: []domain.Node{{ID: "approve", Type: "approval", Title: "Approve"}},
		},
	}
	repo.runs = []domain.Run{{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               domain.RunWaitingApproval,
		CurrentNodeID:        "approve",
		Context:              domain.Context{Values: map[string]any{}},
	}}
	repo.runs[0].PendingApproval = &domain.PendingApproval{Phase: domain.ApprovalBeforeRun, NodeID: "approve"}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.SubmitApproval(ctx, SubmitApprovalInput{
		SessionID: "session-1",
		NodeID:    "approve",
		Approved:  false,
		Comment:   "not now",
	})
	if err != nil {
		t.Fatalf("SubmitApproval() error = %v", err)
	}
	if got.Status != domain.RunCompleted {
		t.Fatalf("SubmitApproval() = %#v", got)
	}
	if len(repo.nodeRuns) != 0 {
		t.Fatalf("node runs = %#v", repo.nodeRuns)
	}
}

func TestSubmitApprovalRejectionUsesApprovalConditionWithoutPlaceholderNodeRun(t *testing.T) {
	repo := newFakeRepository()
	repo.definitions["workflow-1"] = domain.Definition{ID: "workflow-1", ProjectID: "project-1", Graph: domain.Graph{
		Nodes: []domain.Node{{ID: "approve", Type: "approval"}, {ID: "revise", Type: "codex"}, {ID: "ship", Type: "codex"}},
		Edges: []domain.Edge{
			{From: "approve", To: "revise", Priority: 1, Condition: domain.Condition{Field: "approval.approved", Op: "eq", Value: false}},
			{From: "approve", To: "ship", Priority: 2, Condition: domain.Condition{Field: "approval.approved", Op: "eq", Value: true}},
		},
	}}
	repo.runs = []domain.Run{{
		SessionID: "session-1", WorkflowDefinitionID: "workflow-1", Status: domain.RunWaitingApproval,
		CurrentNodeID: "approve", Context: domain.Context{Values: map[string]any{}}, PendingApproval: &domain.PendingApproval{Phase: domain.ApprovalBeforeRun, NodeID: "approve", Attempt: 1},
	}}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (string, error) { return "node-run-revise", nil }

	got, err := service.SubmitApproval(context.Background(), SubmitApprovalInput{SessionID: "session-1", NodeID: "approve", Approved: false, Comment: "revise"})
	if err != nil {
		t.Fatalf("SubmitApproval() error = %v", err)
	}
	if got.Status != domain.RunRunning || got.CurrentNodeID != "revise" || len(repo.nodeRuns) != 1 || repo.nodeRuns[0].NodeID != "revise" {
		t.Fatalf("run=%#v nodeRuns=%#v", got, repo.nodeRuns)
	}
	if _, exists := repo.runs[0].Context.Values["approval"]; exists {
		t.Fatalf("approval leaked into next node context: %#v", repo.runs[0].Context)
	}
}

type fakeRepository struct {
	definitions map[domain.DefinitionID]domain.Definition
	runs        []domain.Run
	nodeRuns    []domain.NodeRun
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{definitions: map[domain.DefinitionID]domain.Definition{}}
}

func hasOutputField(fields []domain.OutputField, key string, valueType string) bool {
	for _, field := range fields {
		if field.Key == key && field.ValueType == valueType {
			return true
		}
	}
	return false
}

func successResult(data map[string]any) *domain.Result {
	return &domain.Result{Version: domain.ResultVersion, Outcome: domain.ResultSuccess, Summary: "completed", Data: data}
}

func resultOutput(data map[string]any) map[string]any {
	return map[string]any{
		"results": map[string]any{
			"version": domain.ResultVersion, "outcome": string(domain.ResultSuccess), "summary": "completed", "data": data,
			"checks": []any{}, "warnings": []any{}, "artifacts": []any{},
		},
	}
}

func TestWorkflowResultContractDefinesArtifactLogicalPathReferences(t *testing.T) {
	contract := workflowResultContract(domain.Node{})
	if !strings.Contains(contract, "ANYCODE_ARTIFACT_DIR") || !strings.Contains(contract, "results.artifacts[].ref") || !strings.Contains(contract, "logicalPath") || !strings.Contains(contract, "publish_artifact") {
		t.Fatalf("workflow result contract missing artifact reference rule: %s", contract)
	}
}

func (r *fakeRepository) SaveDefinition(_ context.Context, definition domain.Definition) error {
	r.definitions[definition.ID] = definition
	return nil
}

func (r *fakeRepository) FindDefinition(_ context.Context, id domain.DefinitionID) (domain.Definition, error) {
	return r.definitions[id], nil
}

func (r *fakeRepository) FindActive(_ context.Context, projectID domain.ProjectID) (domain.Definition, error) {
	for _, definition := range r.definitions {
		if definition.ProjectID == projectID && definition.Active {
			return definition, nil
		}
	}
	return domain.Definition{}, nil
}

func (r *fakeRepository) FindRun(_ context.Context, id domain.SessionID) (domain.Run, error) {
	for _, run := range r.runs {
		if run.SessionID == id {
			return run, nil
		}
	}
	return domain.Run{}, nil
}

func (r *fakeRepository) FindLatestNodeRun(_ context.Context, runID domain.SessionID, nodeID string) (domain.NodeRun, error) {
	for i := len(r.nodeRuns) - 1; i >= 0; i-- {
		if r.nodeRuns[i].SessionID == runID && r.nodeRuns[i].NodeID == nodeID {
			return r.nodeRuns[i], nil
		}
	}
	return domain.NodeRun{}, nil
}

func (r *fakeRepository) ActivateDefinition(context.Context, domain.DefinitionID) error {
	return nil
}

func (r *fakeRepository) CreateInitialRun(_ context.Context, run domain.Run, nodeRun domain.NodeRun) error {
	r.runs = append(r.runs, run)
	r.nodeRuns = append(r.nodeRuns, nodeRun)
	return nil
}

func (r *fakeRepository) CreateRun(_ context.Context, run domain.Run) error {
	r.runs = append(r.runs, run)
	return nil
}

func (r *fakeRepository) UpdateRunState(_ context.Context, run domain.Run) error {
	for i := range r.runs {
		if r.runs[i].SessionID == run.SessionID {
			r.runs[i] = run
			return nil
		}
	}
	r.runs = append(r.runs, run)
	return nil
}

func (r *fakeRepository) SaveNodeRun(_ context.Context, run domain.NodeRun) error {
	r.nodeRuns = append(r.nodeRuns, run)
	return nil
}

func (r *fakeRepository) CreateNodeRunAndUpdateRun(_ context.Context, run domain.Run, nodeRun domain.NodeRun) error {
	for i := range r.runs {
		if r.runs[i].SessionID == run.SessionID {
			r.runs[i] = run
		}
	}
	r.nodeRuns = append(r.nodeRuns, nodeRun)
	return nil
}

func (r *fakeRepository) CompleteNodeAndAdvance(_ context.Context, completedNodeRun domain.NodeRun, run domain.Run, nextNodeRun *domain.NodeRun) error {
	for i := range r.nodeRuns {
		if r.nodeRuns[i].ID == completedNodeRun.ID {
			r.nodeRuns[i].Status = completedNodeRun.Status
			r.nodeRuns[i].FinishedAt = completedNodeRun.FinishedAt
			r.nodeRuns[i].Result = completedNodeRun.Result
		}
	}
	for i := range r.runs {
		if r.runs[i].SessionID == run.SessionID {
			r.runs[i] = run
		}
	}
	if nextNodeRun != nil {
		r.nodeRuns = append(r.nodeRuns, *nextNodeRun)
	}
	return nil
}

func (r *fakeRepository) ResumeNodeAndUpdateRun(_ context.Context, nodeRun domain.NodeRun, run domain.Run) error {
	for i := range r.nodeRuns {
		if r.nodeRuns[i].ID == nodeRun.ID {
			r.nodeRuns[i] = nodeRun
		}
	}
	return r.UpdateRunState(context.Background(), run)
}

func (r *fakeRepository) UpdateRunContext(context.Context, domain.SessionID, domain.Context) error {
	return nil
}

func (r *fakeRepository) MarkRunFailed(_ context.Context, runID domain.SessionID, nodeRunID domain.NodeRunID, failure domain.NodeFailure, finishedAt time.Time) error {
	for i := range r.runs {
		if r.runs[i].SessionID == runID {
			r.runs[i].Status = domain.RunFailed
			r.runs[i].StoppedAt = &finishedAt
		}
	}
	for i := range r.nodeRuns {
		if r.nodeRuns[i].ID == nodeRunID {
			r.nodeRuns[i].Status = domain.NodeFailed
			r.nodeRuns[i].FinishedAt = &finishedAt
			r.nodeRuns[i].Result = &domain.Result{Version: domain.ResultVersion, Outcome: domain.ResultFailure, Summary: failure.Message, Data: map[string]any{"failure": map[string]any{"code": failure.Code, "message": failure.Message}}}
		}
	}
	return nil
}

type failingPublisher struct{}

func (failingPublisher) PublishAfterCommit(context.Context, eventdomain.DomainEvent) error {
	return errors.New("publish failed")
}
