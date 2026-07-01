package workflow

import "testing"

func TestDefaultConditionEvaluator(t *testing.T) {
	context := Context{Values: map[string]any{
		"status": "passed",
		"score":  90,
		"files":  []any{"main.go", "README.md"},
		"nested": map[string]any{"ready": true},
	}}
	tests := []struct {
		name      string
		condition Condition
		want      bool
		wantErr   bool
	}{
		{
			name:      "empty condition matches unconditional edge",
			condition: Condition{},
			want:      true,
		},
		{
			name:      "eq matches nested fields",
			condition: Condition{Field: "nested.ready", Op: "eq", Value: true},
			want:      true,
		},
		{
			name:      "ne matches missing fields",
			condition: Condition{Field: "missing", Op: "ne", Value: "anything"},
			want:      true,
		},
		{
			name:      "exists requires non nil field",
			condition: Condition{Field: "status", Op: "exists"},
			want:      true,
		},
		{
			name:      "contains supports arrays",
			condition: Condition{Field: "files", Op: "contains", Value: "main.go"},
			want:      true,
		},
		{
			name:      "numeric comparisons coerce numbers",
			condition: Condition{Field: "score", Op: "gte", Value: float64(90)},
			want:      true,
		},
		{
			name: "all requires every child",
			condition: Condition{All: []Condition{
				{Field: "status", Op: "eq", Value: "passed"},
				{Field: "score", Op: "gt", Value: 80},
			}},
			want: true,
		},
		{
			name: "any matches one child",
			condition: Condition{Any: []Condition{
				{Field: "status", Op: "eq", Value: "failed"},
				{Field: "score", Op: "gt", Value: 80},
			}},
			want: true,
		},
		{
			name:      "not inverts child",
			condition: Condition{Not: &Condition{Field: "status", Op: "eq", Value: "failed"}},
			want:      true,
		},
		{
			name:      "unknown op returns error",
			condition: Condition{Field: "status", Op: "script", Value: "passed"},
			wantErr:   true,
		},
	}

	evaluator := DefaultConditionEvaluator{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.Evaluate(tt.condition, context)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultPlannerNextNode(t *testing.T) {
	planner := DefaultPlanner{}
	def := Definition{Graph: Graph{Edges: []Edge{
		{From: "build", To: "fallback", Priority: 20, Condition: Condition{}},
		{From: "build", To: "verify", Priority: 10, Condition: Condition{Field: "status", Op: "eq", Value: "passed"}},
	}}}
	run := Run{CurrentNodeID: "build"}
	decision, err := planner.NextNode(def, run, Context{Values: map[string]any{"status": "passed"}})
	if err != nil {
		t.Fatalf("NextNode() error = %v", err)
	}
	if decision.NextNodeID != "verify" || decision.Blocked {
		t.Fatalf("NextNode() = %+v, want verify", decision)
	}
}

func TestDefaultPlannerBlocksWhenNoEdgeMatches(t *testing.T) {
	planner := DefaultPlanner{}
	def := Definition{Graph: Graph{Edges: []Edge{
		{From: "build", To: "verify", Condition: Condition{Field: "status", Op: "eq", Value: "passed"}},
	}}}
	decision, err := planner.NextNode(def, Run{CurrentNodeID: "build"}, Context{Values: map[string]any{"status": "failed"}})
	if err != nil {
		t.Fatalf("NextNode() error = %v", err)
	}
	if !decision.Blocked || decision.NextNodeID != "" {
		t.Fatalf("NextNode() = %+v, want blocked", decision)
	}
}

func TestDefaultPlannerTerminalNodeHasNoDecision(t *testing.T) {
	planner := DefaultPlanner{}
	decision, err := planner.NextNode(Definition{Graph: Graph{}}, Run{CurrentNodeID: "done"}, Context{})
	if err != nil {
		t.Fatalf("NextNode() error = %v", err)
	}
	if decision.Blocked || decision.NextNodeID != "" {
		t.Fatalf("NextNode() = %+v, want empty terminal decision", decision)
	}
}

func TestDefaultPlannerShouldRetry(t *testing.T) {
	planner := DefaultPlanner{}
	node := Node{Retry: RetryConfig{MaxAttempts: 3}}
	if !planner.ShouldRetry(node, 2, NodeFailure{}) {
		t.Fatal("expected retry before max attempts")
	}
	if planner.ShouldRetry(node, 3, NodeFailure{}) {
		t.Fatal("expected no retry at max attempts")
	}
}
