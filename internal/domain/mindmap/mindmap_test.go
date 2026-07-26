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

func timePointer(value time.Time) *time.Time {
	return &value
}
