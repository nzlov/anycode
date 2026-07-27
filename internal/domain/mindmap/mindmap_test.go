package mindmap

import (
	"slices"
	"testing"
	"time"
)

func TestEnsureRootKeepsProjectRootAtCenter(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	graph := Graph{ProjectID: "project-1", Nodes: []Node{{
		ID: RootNodeID, Title: "old", DeletedAt: timePointer(now.Add(-time.Hour)),
	}}}

	EnsureRoot(&graph, "AnyCode", now)

	if len(graph.Nodes) != 1 {
		t.Fatalf("nodes = %#v", graph.Nodes)
	}
	root := graph.Nodes[0]
	if root.ID != RootNodeID || root.Title != "AnyCode" || root.DeletedAt != nil {
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

func TestApplyAndCompactOverlayTrackNodeFiles(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	files := []NodeFile{{File: "internal/domain/mindmap/mindmap.go", Method: "Apply", StartLine: 205, EndLine: 222}}
	base := Graph{ProjectID: "project-1", Nodes: []Node{{ID: "node-1", Title: "Node"}}}
	overlay := Overlay{ProjectID: "project-1", SessionID: "session-1", Changes: []Change{{
		ID: "files", Kind: ChangeUpsertNode, EntityID: "node-1", Files: &files, OccurredAt: now,
	}}}

	current := Materialize(base, overlay.Changes)
	if len(current.Nodes) != 1 || !slices.Equal(current.Nodes[0].Files, files) {
		t.Fatalf("materialized files = %#v", current.Nodes)
	}
	compacted := CompactOverlay(base, overlay)
	if len(compacted.Changes) != 1 || compacted.Changes[0].Files == nil || !slices.Equal(*compacted.Changes[0].Files, files) {
		t.Fatalf("compacted files = %#v", compacted.Changes)
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

func TestVisibleRepairsDuplicateRecordsAndDanglingEdges(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	newer := base.Add(time.Minute)
	graph := Graph{
		Nodes: []Node{
			{ID: RootNodeID, Title: "AnyCode", TitleUpdatedAt: base},
			{ID: "duplicate", Title: "old", Content: "kept", TitleUpdatedAt: base, ContentUpdatedAt: newer},
			{ID: "duplicate", Title: "new", TitleUpdatedAt: newer},
		},
		Edges: []Edge{
			{ID: "duplicate-edge", SourceID: RootNodeID, TargetID: "duplicate", Label: "old", LabelUpdatedAt: base},
			{ID: "duplicate-edge", SourceID: RootNodeID, TargetID: "duplicate", Label: "new", LabelUpdatedAt: newer},
			{ID: "dangling", SourceID: RootNodeID, TargetID: "missing"},
		},
	}

	visible := Visible(graph)
	if len(visible.Nodes) != 2 || visible.Nodes[1].Title != "new" || visible.Nodes[1].Content != "kept" {
		t.Fatalf("normalized nodes = %#v", visible.Nodes)
	}
	if len(visible.Edges) != 1 || visible.Edges[0].ID != "duplicate-edge" || visible.Edges[0].Label != "new" {
		t.Fatalf("normalized edges = %#v", visible.Edges)
	}
}

func TestDeleteNodeCascadesIncidentEdgesWithoutResurrection(t *testing.T) {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	title := "node"
	source, target := RootNodeID, NodeID("node")
	graph := Graph{Nodes: []Node{{ID: RootNodeID, Title: "AnyCode"}}}
	Apply(&graph, Change{Kind: ChangeUpsertNode, EntityID: "node", Title: &title, OccurredAt: base})
	Apply(&graph, Change{Kind: ChangeUpsertEdge, EntityID: "edge", SourceID: &source, TargetID: &target, OccurredAt: base})
	Apply(&graph, Change{Kind: ChangeDeleteNode, EntityID: "node", OccurredAt: base.Add(time.Minute)})
	Apply(&graph, Change{Kind: ChangeUpsertNode, EntityID: "node", Title: &title, OccurredAt: base.Add(2 * time.Minute)})

	visible := Visible(graph)
	if len(visible.Nodes) != 2 || len(visible.Edges) != 0 {
		t.Fatalf("deleted relationship resurrected = %#v", visible)
	}
}

func TestTouchAlwaysAdvancesGraphRevision(t *testing.T) {
	current := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	graph := Graph{UpdatedAt: current}

	Touch(&graph, current.Add(-time.Hour))
	if !graph.UpdatedAt.After(current) {
		t.Fatalf("updated at did not advance: %s", graph.UpdatedAt)
	}
}

func TestCompactOverlayKeepsOnlyEffectiveEntityDeltas(t *testing.T) {
	baseTime := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	root, existing, removed := RootNodeID, NodeID("existing"), NodeID("removed")
	base := Graph{
		ProjectID: "project-1",
		Nodes: []Node{
			{ID: root, Title: "AnyCode", TitleUpdatedAt: baseTime},
			{ID: existing, Title: "Existing", Content: "old", TitleUpdatedAt: baseTime, ContentUpdatedAt: baseTime},
			{ID: removed, Title: "Removed", TitleUpdatedAt: baseTime},
		},
		Edges: []Edge{{ID: "root-removed", SourceID: root, TargetID: removed, SourceUpdatedAt: baseTime, TargetUpdatedAt: baseTime}},
	}
	added, ghost := NodeID("added"), NodeID("ghost")
	addedTitle, addedContent, ghostTitle := "Added", "details", "Ghost"
	newContent, oldContent := "new", "old"
	label := "contains"
	overlay := Overlay{ProjectID: "project-1", SessionID: "session-1", Changes: []Change{
		{ID: "1", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeUpsertNode, EntityID: string(added), Title: &addedTitle, OccurredAt: baseTime.Add(time.Minute)},
		{ID: "2", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeUpsertNode, EntityID: string(added), Content: &addedContent, OccurredAt: baseTime.Add(2 * time.Minute)},
		{ID: "3", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeUpsertNode, EntityID: string(existing), Content: &newContent, OccurredAt: baseTime.Add(3 * time.Minute)},
		{ID: "4", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeUpsertNode, EntityID: string(existing), Content: &oldContent, OccurredAt: baseTime.Add(4 * time.Minute)},
		{ID: "5", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeUpsertEdge, EntityID: "root-added", SourceID: &root, TargetID: &added, Label: &label, OccurredAt: baseTime.Add(5 * time.Minute)},
		{ID: "6", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeDeleteNode, EntityID: string(removed), OccurredAt: baseTime.Add(6 * time.Minute)},
		{ID: "7", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeUpsertNode, EntityID: string(ghost), Title: &ghostTitle, OccurredAt: baseTime.Add(7 * time.Minute)},
		{ID: "8", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeDeleteNode, EntityID: string(ghost), OccurredAt: baseTime.Add(8 * time.Minute)},
	}}

	compacted := CompactOverlay(base, overlay)
	if len(compacted.Changes) != 4 {
		t.Fatalf("compacted changes = %#v", compacted.Changes)
	}
	byEntity := make(map[string][]Change, len(compacted.Changes))
	for _, change := range compacted.Changes {
		byEntity[change.EntityID] = append(byEntity[change.EntityID], change)
	}
	addedChanges := byEntity[string(added)]
	if len(addedChanges) != 2 || addedChanges[0].Title == nil || addedChanges[1].Content == nil || *addedChanges[0].Title != addedTitle || *addedChanges[1].Content != addedContent {
		t.Fatalf("added node changes = %#v", addedChanges)
	}
	if changes := byEntity["root-added"]; len(changes) != 1 || changes[0].Kind != ChangeUpsertEdge || changes[0].SourceID == nil || changes[0].TargetID == nil {
		t.Fatalf("added edge change = %#v", changes)
	}
	if changes := byEntity[string(removed)]; len(changes) != 1 || changes[0].Kind != ChangeDeleteNode {
		t.Fatalf("deleted node change = %#v", changes)
	}
	if _, found := byEntity[string(existing)]; found {
		t.Fatalf("reverted node retained a delta: %#v", compacted.Changes)
	}
	if _, found := byEntity[string(ghost)]; found {
		t.Fatalf("added then deleted node retained a delta: %#v", compacted.Changes)
	}

	want := Materialize(base, overlay.Changes)
	got := Materialize(base, compacted.Changes)
	if len(got.Nodes) != len(want.Nodes) || len(got.Edges) != len(want.Edges) {
		t.Fatalf("materialized compact graph = %#v, want %#v", got, want)
	}
	gotAdded := got.Nodes[nodeIndex(got.Nodes, added)]
	wantAdded := want.Nodes[nodeIndex(want.Nodes, added)]
	if !gotAdded.TitleUpdatedAt.Equal(wantAdded.TitleUpdatedAt) || !gotAdded.ContentUpdatedAt.Equal(wantAdded.ContentUpdatedAt) {
		t.Fatalf("compacted node timestamps = %#v, want %#v", gotAdded, wantAdded)
	}
}

func TestCompactOverlayPreservesCascadedEdgeDeletionAfterNodeRecreation(t *testing.T) {
	baseTime := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	root, child := RootNodeID, NodeID("child")
	base := Graph{
		ProjectID: "project-1",
		Nodes: []Node{
			{ID: root, Title: "AnyCode", TitleUpdatedAt: baseTime},
			{ID: child, Title: "Child", TitleUpdatedAt: baseTime},
		},
		Edges: []Edge{{ID: "root-child", SourceID: root, TargetID: child, SourceUpdatedAt: baseTime, TargetUpdatedAt: baseTime}},
	}
	title := "Child"
	overlay := Overlay{ProjectID: "project-1", SessionID: "session-1", Changes: []Change{
		{ID: "delete", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeDeleteNode, EntityID: string(child), OccurredAt: baseTime.Add(time.Minute)},
		{ID: "recreate", ProjectID: "project-1", SessionID: "session-1", Kind: ChangeUpsertNode, EntityID: string(child), Title: &title, OccurredAt: baseTime.Add(2 * time.Minute)},
	}}

	compacted := CompactOverlay(base, overlay)
	if len(compacted.Changes) != 1 || compacted.Changes[0].Kind != ChangeDeleteEdge || compacted.Changes[0].EntityID != "root-child" {
		t.Fatalf("compacted changes = %#v", compacted.Changes)
	}
	want := Materialize(base, overlay.Changes)
	got := Materialize(base, compacted.Changes)
	if len(got.Nodes) != len(want.Nodes) || len(got.Edges) != len(want.Edges) {
		t.Fatalf("materialized compact graph = %#v, want %#v", got, want)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}
