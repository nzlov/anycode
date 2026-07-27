package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/mindmap"
)

func TestMigrateAddsNormalizedMindMapStorageWithoutChangingLegacyTables(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE mind_map_graphs (
		id TEXT PRIMARY KEY, nodes JSON NOT NULL DEFAULT '[]', edges JSON NOT NULL DEFAULT '[]',
		history JSON NOT NULL DEFAULT '[]', updated_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE mind_map_overlays (
		id TEXT PRIMARY KEY, project_id TEXT NOT NULL, changes JSON NOT NULL DEFAULT '[]', updated_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"nodes", "edges", "history"} {
		if exists, err := store.columnExists(ctx, "mind_map_graphs", column); err != nil || !exists {
			t.Fatalf("mind_map_graphs.%s exists=%v err=%v", column, exists, err)
		}
	}
	if exists, err := store.columnExists(ctx, "mind_map_overlays", "changes"); err != nil || !exists {
		t.Fatalf("mind_map_overlays.changes exists=%v err=%v", exists, err)
	}
	for _, table := range []string{"mind_map_nodes", "mind_map_edges"} {
		if exists, err := store.tableExists(ctx, table); err != nil || !exists {
			t.Fatalf("table %s exists=%v err=%v", table, exists, err)
		}
	}
	if exists, err := store.tableExists(ctx, "mind_map_changes"); err != nil || exists {
		t.Fatalf("mind_map_changes exists=%v err=%v", exists, err)
	}
}

func TestMindMapRepositoryPersistsNormalizedGraphAndOverlay(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.MindMaps()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	titleRoot, titleFeature, label := "AnyCode", "Feature", "contains"
	files := []mindmap.NodeFile{{File: "internal/feature.go", Method: "Run", StartLine: 10, EndLine: 20}}
	source, target := mindmap.RootNodeID, mindmap.NodeID("feature")
	changes := []mindmap.Change{
		{Kind: mindmap.ChangeUpsertNode, EntityID: string(mindmap.RootNodeID), Title: &titleRoot, OccurredAt: now},
		{Kind: mindmap.ChangeUpsertNode, EntityID: "feature", Title: &titleFeature, Files: &files, OccurredAt: now},
		{Kind: mindmap.ChangeUpsertEdge, EntityID: "root-feature", SourceID: &source, TargetID: &target, Label: &label, OccurredAt: now},
	}
	graph := mindmap.Graph{ProjectID: "project-1"}
	for _, change := range changes {
		mindmap.Apply(&graph, change)
	}
	graph.UpdatedAt = now
	if err := repo.SaveGraph(ctx, graph, changes); err != nil {
		t.Fatal(err)
	}
	if count, err := store.client.MindMapNode.Query().Count(ctx); err != nil || count != 2 {
		t.Fatalf("node count=%d err=%v", count, err)
	}
	if count, err := store.client.MindMapEdge.Query().Count(ctx); err != nil || count != 1 {
		t.Fatalf("edge count=%d err=%v", count, err)
	}
	persisted, found, err := repo.FindGraph(ctx, "project-1")
	if err != nil || !found {
		t.Fatalf("FindGraph() found=%v err=%v", found, err)
	}
	if len(persisted.Nodes) != 2 || len(persisted.Nodes[1].Files) != 1 || persisted.Nodes[1].Files[0].Method != "Run" || len(persisted.Edges) != 1 || persisted.History != nil {
		t.Fatalf("persisted graph=%#v", persisted)
	}

	content := "Updated in card"
	overlayFiles := []mindmap.NodeFile{{File: "internal/feature.go", Method: "Update", StartLine: 30, EndLine: 45}}
	deletedAt := now.Add(time.Minute)
	overlay := mindmap.Overlay{
		ProjectID: "project-1", SessionID: "session-1", UpdatedAt: deletedAt,
		Changes: []mindmap.Change{
			{ProjectID: "project-1", SessionID: "session-1", Kind: mindmap.ChangeUpsertNode, EntityID: "feature", Content: &content, Files: &overlayFiles, OccurredAt: deletedAt},
			{ProjectID: "project-1", SessionID: "session-1", Kind: mindmap.ChangeDeleteEdge, EntityID: "root-feature", OccurredAt: deletedAt},
		},
	}
	if err := repo.SaveOverlay(ctx, overlay); err != nil {
		t.Fatal(err)
	}
	persistedOverlay, found, err := repo.FindOverlay(ctx, "session-1")
	if err != nil || !found {
		t.Fatalf("FindOverlay() found=%v err=%v", found, err)
	}
	current := mindmap.Materialize(persisted, persistedOverlay.Changes)
	if len(current.Nodes) != 2 || current.Nodes[1].Content != content || len(current.Nodes[1].Files) != 1 || current.Nodes[1].Files[0].Method != "Update" || len(current.Edges) != 0 {
		t.Fatalf("materialized graph=%#v changes=%#v", current, persistedOverlay.Changes)
	}
	if err := repo.DeleteOverlay(ctx, "session-1"); err != nil {
		t.Fatal(err)
	}
	if count, err := store.client.MindMapNode.Query().Count(ctx); err != nil || count != 2 {
		t.Fatalf("node count after overlay delete=%d err=%v", count, err)
	}
}
