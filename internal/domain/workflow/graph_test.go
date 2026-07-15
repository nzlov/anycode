package workflow

import (
	"reflect"
	"testing"
)

func TestCanonicalGraphReservesApprovalOutputNamespace(t *testing.T) {
	input := Graph{
		Nodes: []Node{
			{
				ID:   "build",
				Type: "codex",
				OutputFields: []OutputField{
					{Key: "result", Description: "result", ValueType: "string"},
					{Key: " approval.approved ", Description: "legacy", ValueType: "boolean"},
					{Key: "approval.note", Description: "legacy note", ValueType: "string"},
					{Key: "approval", Description: "legacy object", ValueType: "object"},
				},
			},
			{
				ID:   "merge",
				Type: "merge",
				OutputFields: []OutputField{
					{Key: "merge.status", Description: "old", ValueType: "number"},
					{Key: "custom", Description: "custom", ValueType: "boolean"},
				},
			},
		},
		Edges: []Edge{{From: "build", To: "merge", Priority: 2}},
	}

	got := CanonicalGraph(input)
	wantBuild := []OutputField{{Key: "result", Description: "result", ValueType: "string"}}
	if !reflect.DeepEqual(got.Nodes[0].OutputFields, wantBuild) {
		t.Fatalf("build output fields = %#v, want %#v", got.Nodes[0].OutputFields, wantBuild)
	}
	wantMerge := []OutputField{
		{Key: "merge.status", Description: "Merge result status.", ValueType: "string"},
		{Key: "custom", Description: "custom", ValueType: "boolean"},
		{Key: "merge.failureCode", Description: "Merge failure code when the merge did not complete.", ValueType: "string"},
		{Key: "merge.failureReason", Description: "Merge failure reason when the merge did not complete.", ValueType: "string"},
	}
	if !reflect.DeepEqual(got.Nodes[1].OutputFields, wantMerge) {
		t.Fatalf("merge output fields = %#v, want %#v", got.Nodes[1].OutputFields, wantMerge)
	}
	if !reflect.DeepEqual(CanonicalGraph(got), got) {
		t.Fatal("CanonicalGraph is not idempotent")
	}
	if len(input.Nodes[0].OutputFields) != 4 || input.Nodes[1].OutputFields[0].ValueType != "number" {
		t.Fatalf("CanonicalGraph mutated input: %#v", input)
	}
}
