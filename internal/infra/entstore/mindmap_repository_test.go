package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/mindmap"
)

func TestMigrateRepairsLegacyMindMapDuplicatesAndDanglingEdges(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	nodes, err := encodeMindMapJSON([]mindmap.Node{
		{ID: mindmap.RootNodeID, Title: "AnyCode", TitleUpdatedAt: now},
		{ID: "duplicate", Title: "feature", TitleUpdatedAt: now},
		{ID: "duplicate", Title: "feature", TitleUpdatedAt: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	edges, err := encodeMindMapJSON([]mindmap.Edge{
		{ID: "duplicate-edge", SourceID: mindmap.RootNodeID, TargetID: "duplicate"},
		{ID: "duplicate-edge", SourceID: mindmap.RootNodeID, TargetID: "duplicate"},
		{ID: "dangling", SourceID: mindmap.RootNodeID, TargetID: "missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.client.MindMapGraph.Create().SetID("project-1").SetNodes(nodes).SetEdges(edges).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := store.client.MindMapGraph.Get(ctx, "project-1")
	if err != nil {
		t.Fatal(err)
	}
	repairedNodes, err := decodeMindMapJSON[mindmap.Node](row.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	repairedEdges, err := decodeMindMapJSON[mindmap.Edge](row.Edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(repairedNodes) != 2 || len(repairedEdges) != 1 || repairedEdges[0].ID != "duplicate-edge" {
		t.Fatalf("repaired graph nodes=%#v edges=%#v", repairedNodes, repairedEdges)
	}
}
