package mindmap

import (
	"testing"
	"time"
)

func TestEnsureRootKeepsProjectRootAtCenter(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	graph := Graph{ProjectID: "project-1", Nodes: []Node{{
		ID: RootNodeID, Title: "old", X: 42, Y: -8, DeletedAt: timePointer(now.Add(-time.Hour)),
	}}}

	EnsureRoot(&graph, "AnyCode", now)

	if len(graph.Nodes) != 1 {
		t.Fatalf("nodes = %#v", graph.Nodes)
	}
	root := graph.Nodes[0]
	if root.ID != RootNodeID || root.Title != "AnyCode" || root.X != 0 || root.Y != 0 || root.DeletedAt != nil {
		t.Fatalf("root = %#v", root)
	}
	if !graph.UpdatedAt.Equal(now) {
		t.Fatalf("updated at = %s, want %s", graph.UpdatedAt, now)
	}
}

func TestApplyUsesLatestFieldTimestampAndIgnoresOlderDelete(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	title := "new title"
	content := "new content"
	graph := Graph{ProjectID: "project-1"}

	Apply(&graph, Change{Kind: ChangeUpsertNode, EntityID: "node-1", Title: &title, OccurredAt: base.Add(2 * time.Minute)})
	Apply(&graph, Change{Kind: ChangeUpsertNode, EntityID: "node-1", Content: &content, OccurredAt: base.Add(3 * time.Minute)})
	Apply(&graph, Change{Kind: ChangeDeleteNode, EntityID: "node-1", OccurredAt: base.Add(time.Minute)})

	if len(graph.Nodes) != 1 || graph.Nodes[0].DeletedAt != nil {
		t.Fatalf("older delete hid newer node: %#v", graph.Nodes)
	}
	olderTitle := "old title"
	Apply(&graph, Change{Kind: ChangeUpsertNode, EntityID: "node-1", Title: &olderTitle, OccurredAt: base})
	if graph.Nodes[0].Title != title || graph.Nodes[0].Content != content {
		t.Fatalf("node fields lost latest values: %#v", graph.Nodes[0])
	}
}

func TestMergeOverlayPreservesHistoryAndHidesDanglingEdges(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	titleA, titleB := "A", "B"
	source, target := NodeID("a"), NodeID("b")
	graph := Graph{ProjectID: "project-1"}
	overlay := Overlay{ProjectID: "project-1", SessionID: "session-1", Changes: []Change{
		{ID: "3", Kind: ChangeUpsertEdge, EntityID: "edge-1", SourceID: &source, TargetID: &target, OccurredAt: base.Add(3 * time.Minute)},
		{ID: "1", Kind: ChangeUpsertNode, EntityID: "a", Title: &titleA, OccurredAt: base.Add(time.Minute)},
		{ID: "2", Kind: ChangeUpsertNode, EntityID: "b", Title: &titleB, OccurredAt: base.Add(2 * time.Minute)},
	}}

	MergeOverlay(&graph, overlay)
	if len(graph.History) != 3 || len(Visible(graph).Edges) != 1 {
		t.Fatalf("merged graph = %#v", graph)
	}
	Apply(&graph, Change{Kind: ChangeDeleteNode, EntityID: "b", OccurredAt: base.Add(4 * time.Minute)})
	visible := Visible(graph)
	if len(visible.Nodes) != 1 || len(visible.Edges) != 0 {
		t.Fatalf("visible graph contains dangling relationship: %#v", visible)
	}
}

func TestVisibleDoesNotMutateSourceGraphWhileFilteringDeletedEntries(t *testing.T) {
	deletedAt := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	graph := Graph{
		Nodes: []Node{
			{ID: RootNodeID},
			{ID: "deleted", DeletedAt: &deletedAt},
			{ID: "remaining"},
		},
		Edges: []Edge{
			{ID: "deleted-edge", SourceID: RootNodeID, TargetID: "deleted"},
			{ID: "remaining-edge", SourceID: RootNodeID, TargetID: "remaining"},
		},
	}

	visible := Visible(graph)

	if len(visible.Nodes) != 2 || visible.Nodes[1].ID != "remaining" {
		t.Fatalf("visible nodes = %#v", visible.Nodes)
	}
	if len(visible.Edges) != 1 || visible.Edges[0].ID != "remaining-edge" {
		t.Fatalf("visible edges = %#v", visible.Edges)
	}
	if len(graph.Nodes) != 3 || graph.Nodes[1].ID != "deleted" || graph.Nodes[2].ID != "remaining" {
		t.Fatalf("source nodes mutated = %#v", graph.Nodes)
	}
	if len(graph.Edges) != 2 || graph.Edges[0].ID != "deleted-edge" || graph.Edges[1].ID != "remaining-edge" {
		t.Fatalf("source edges mutated = %#v", graph.Edges)
	}
}

func TestDeleteMarksEveryDuplicateEntityRecord(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	deletedAt := base.Add(time.Minute)
	graph := Graph{
		Nodes: []Node{
			{ID: RootNodeID},
			{ID: "duplicate", Title: "duplicate", TitleUpdatedAt: base},
			{ID: "duplicate", Title: "duplicate", TitleUpdatedAt: base},
		},
		Edges: []Edge{
			{ID: "duplicate-edge", SourceID: RootNodeID, TargetID: "duplicate", SourceUpdatedAt: base, TargetUpdatedAt: base},
			{ID: "duplicate-edge", SourceID: RootNodeID, TargetID: "duplicate", SourceUpdatedAt: base, TargetUpdatedAt: base},
		},
	}

	Apply(&graph, Change{Kind: ChangeDeleteEdge, EntityID: "duplicate-edge", OccurredAt: deletedAt})
	Apply(&graph, Change{Kind: ChangeDeleteNode, EntityID: "duplicate", OccurredAt: deletedAt})

	for _, node := range graph.Nodes[1:] {
		if node.DeletedAt == nil || !node.DeletedAt.Equal(deletedAt) {
			t.Fatalf("duplicate node was not deleted: %#v", graph.Nodes)
		}
	}
	for _, edge := range graph.Edges {
		if edge.DeletedAt == nil || !edge.DeletedAt.Equal(deletedAt) {
			t.Fatalf("duplicate edge was not deleted: %#v", graph.Edges)
		}
	}
	visible := Visible(graph)
	if len(visible.Nodes) != 1 || visible.Nodes[0].ID != RootNodeID || len(visible.Edges) != 0 {
		t.Fatalf("visible graph retained duplicate entities: %#v", visible)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
