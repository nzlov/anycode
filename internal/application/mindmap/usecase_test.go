package mindmap

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/mindmap"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
	"github.com/nzlov/anycode/internal/infra/entstore"
)

type testMindMapSettings struct {
	configuration settingdomain.MindMapConfiguration
}

func (s *testMindMapSettings) MindMapConfiguration(context.Context) (settingdomain.MindMapConfiguration, error) {
	return s.configuration, nil
}

func TestServiceMaintainsFreeFormGraphWithSystemRoot(t *testing.T) {
	ctx := context.Background()
	store := openMindMapTestStore(t)
	settings := &testMindMapSettings{configuration: settingdomain.MindMapConfiguration{
		Enabled: true, Mode: settingdomain.MindMapModeRealtime, MaxConcurrent: 1,
	}}
	project, session := saveMindMapTestProjectAndSession(t, store, "session-1")
	service := New(store.MindMaps(), store.Projects(), store.Sessions(), settings, store)
	service.now = func() time.Time { return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) }
	service.generateID = func() (domain.ChangeID, error) { return "change-1", nil }

	graph, err := service.Get(ctx, GetInput{ProjectID: domain.ProjectID(project.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].ID != domain.RootNodeID || graph.Nodes[0].Title != project.Name {
		t.Fatalf("initial graph = %#v", graph)
	}
	if graph.UpdatedAt.IsZero() {
		t.Fatal("initial graph has a zero updated time")
	}
	title, content := "自由节点", "由 Agent 自主维护"
	graph, err = service.Update(ctx, UpdateInput{ProjectID: domain.ProjectID(project.ID), Operations: []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "free-node", Title: &title, Content: &content,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 2 || graph.Nodes[1].Title != title {
		t.Fatalf("updated graph = %#v", graph)
	}
	updatedContent := "Agent 可更新已有节点内容"
	graph, err = service.Update(ctx, UpdateInput{ProjectID: domain.ProjectID(project.ID), Operations: []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "free-node", Content: &updatedContent,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 2 || graph.Nodes[1].Title != title || graph.Nodes[1].Content != updatedContent {
		t.Fatalf("content-only update = %#v", graph)
	}
	graph, err = service.Update(ctx, UpdateInput{ProjectID: domain.ProjectID(project.ID), Operations: []OperationInput{{
		Kind: domain.ChangeDeleteNode, ID: "free-node",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].ID != domain.RootNodeID {
		t.Fatalf("deleted graph = %#v", graph)
	}
	if _, err := service.Update(ctx, UpdateInput{ProjectID: domain.ProjectID(project.ID), Operations: []OperationInput{{
		Kind: domain.ChangeDeleteNode, ID: string(domain.RootNodeID),
	}}}); err == nil {
		t.Fatal("deleting the project root succeeded")
	}

	cardTitle := "关闭卡片仍可由用户编辑"
	if _, err := service.Update(ctx, UpdateInput{ProjectID: domain.ProjectID(project.ID), SessionID: domain.SessionID(session.ID), Operations: []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "card-node", Title: &cardTitle,
	}}}); err != nil {
		t.Fatalf("update closed card overlay: %v", err)
	}
}

func TestCardGraphDerivesNodeChangesWithoutExposingDeletedNodesToAgentQueries(t *testing.T) {
	ctx := context.Background()
	store := openMindMapTestStore(t)
	settings := &testMindMapSettings{configuration: settingdomain.MindMapConfiguration{
		Enabled: true, Mode: settingdomain.MindMapModeRealtime, MaxConcurrent: 1,
	}}
	project, session := saveMindMapTestProjectAndSession(t, store, "session-diff")
	session.Status = sessiondomain.StatusRunning
	session.ClosedAt = nil
	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatal(err)
	}
	service := New(store.MindMaps(), store.Projects(), store.Sessions(), settings, store)
	service.now = func() time.Time { return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) }
	service.generateID = func() (domain.ChangeID, error) { return "change", nil }
	unchangedTitle, modifiedTitle, deletedTitle := "Unchanged", "Modified", "Deleted"
	if _, err := service.Update(ctx, UpdateInput{
		ProjectID: domain.ProjectID(project.ID),
		Operations: []OperationInput{
			{Kind: domain.ChangeUpsertNode, ID: "unchanged", Title: &unchangedTitle},
			{Kind: domain.ChangeUpsertNode, ID: "modified", Title: &modifiedTitle},
			{Kind: domain.ChangeUpsertNode, ID: "deleted", Title: &deletedTitle},
		},
	}); err != nil {
		t.Fatal(err)
	}
	modifiedContent, addedTitle := "Updated in card", "Added"
	cardGraph, err := service.Update(ctx, UpdateInput{
		ProjectID: domain.ProjectID(project.ID), SessionID: domain.SessionID(session.ID),
		Operations: []OperationInput{
			{Kind: domain.ChangeUpsertNode, ID: "modified", Content: &modifiedContent},
			{Kind: domain.ChangeUpsertNode, ID: "added", Title: &addedTitle},
			{Kind: domain.ChangeDeleteNode, ID: "deleted"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cards, err := service.ListCards(ctx, domain.ProjectID(project.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || !cards[0].HasChanges {
		t.Fatalf("card availability = %#v", cards)
	}
	changeByID := make(map[domain.NodeID]NodeChangeType, len(cardGraph.Nodes))
	for _, node := range cardGraph.Nodes {
		changeByID[node.ID] = node.ChangeType
	}
	if changeByID[domain.RootNodeID] != NodeUnchanged || changeByID["unchanged"] != NodeUnchanged ||
		changeByID["modified"] != NodeModified || changeByID["added"] != NodeAdded || changeByID["deleted"] != NodeDeleted {
		t.Fatalf("card changes = %#v", changeByID)
	}

	agentGraph, err := service.GetForSession(ctx, domain.SessionID(session.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range agentGraph.Nodes {
		if node.ID == "deleted" || node.ChangeType != NodeUnchanged {
			t.Fatalf("agent graph contains diff-only node state: %#v", agentGraph.Nodes)
		}
	}
}

func TestActiveAsyncTaskCanFinishAfterGlobalModeIsDisabled(t *testing.T) {
	ctx := context.Background()
	store := openMindMapTestStore(t)
	settings := &testMindMapSettings{configuration: settingdomain.MindMapConfiguration{
		Enabled: true, Mode: settingdomain.MindMapModeAsync, Model: "gpt-test", ReasoningEffort: "high", MaxConcurrent: 1,
	}}
	project, session := saveMindMapTestProjectAndSession(t, store, "session-async")
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	if err := store.MindMaps().SaveTask(ctx, domain.Task{
		ID: "task-1", ProjectID: domain.ProjectID(project.ID), SessionID: domain.SessionID(session.ID),
		Status: domain.TaskRunning, ProcessRunID: "run-1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	service := New(store.MindMaps(), store.Projects(), store.Sessions(), settings, store)
	service.now = func() time.Time { return now }
	service.generateID = func() (domain.ChangeID, error) { return "async-change", nil }
	cards, err := service.ListCards(ctx, domain.ProjectID(project.ID))
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].HasChanges {
		t.Fatalf("pending card availability = %#v", cards)
	}
	settings.configuration.Enabled = false
	title := "异步整理结果"

	graph, err := service.UpdateForProcess(ctx, "run-1", domain.SessionID(session.ID), []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "async-node", Title: &title,
	}})
	if err != nil {
		t.Fatalf("active async update after disable: %v", err)
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("async graph = %#v", graph)
	}
	if _, err := service.UpdateForSession(ctx, domain.SessionID(session.ID), []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "realtime-node", Title: &title,
	}}); err == nil {
		t.Fatal("regular session retained update tool while realtime mode was disabled")
	}
}

func TestBuildChangesRejectsOperationFieldsOutsideTheirKind(t *testing.T) {
	title := "node"
	emptyTitle := "   "
	source, target := domain.NodeID("source"), domain.NodeID("target")
	invalidFiles := []domain.NodeFile{{File: "file.go", Method: "Run", StartLine: 20, EndLine: 10}}
	service := &Service{now: time.Now, generateID: func() (domain.ChangeID, error) { return "change", nil }}
	tests := []struct {
		name      string
		operation OperationInput
	}{
		{name: "node without fields", operation: OperationInput{Kind: domain.ChangeUpsertNode, ID: "node"}},
		{name: "node with empty title", operation: OperationInput{Kind: domain.ChangeUpsertNode, ID: "node", Title: &emptyTitle}},
		{name: "node with invalid files", operation: OperationInput{Kind: domain.ChangeUpsertNode, ID: "node", Title: &title, Files: &invalidFiles}},
		{name: "node with edge field", operation: OperationInput{Kind: domain.ChangeUpsertNode, ID: "node", Title: &title, SourceID: &source}},
		{name: "edge without endpoint", operation: OperationInput{Kind: domain.ChangeUpsertEdge, ID: "edge", SourceID: &source}},
		{name: "delete with payload", operation: OperationInput{Kind: domain.ChangeDeleteNode, ID: "node", Content: &title}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.buildChanges(UpdateInput{ProjectID: "project", Operations: []OperationInput{test.operation}}); err == nil {
				t.Fatalf("operation succeeded: %#v", test.operation)
			}
		})
	}
	if _, err := service.buildChanges(UpdateInput{ProjectID: "project", Operations: []OperationInput{{
		Kind: domain.ChangeUpsertEdge, ID: "edge", SourceID: &source, TargetID: &target,
	}}}); err != nil {
		t.Fatalf("valid edge failed: %v", err)
	}
	content := "updated content"
	files := []domain.NodeFile{{File: " mindmap.go ", Method: " Apply ", StartLine: 10, EndLine: 20}}
	if _, err := service.buildChanges(UpdateInput{ProjectID: "project", Operations: []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "node", Content: &content, Files: &files,
	}}}); err != nil {
		t.Fatalf("content-only node update failed: %v", err)
	}
}

func TestValidateVisibleGraphRequiresNodeTitle(t *testing.T) {
	err := validateVisibleGraph(domain.Graph{Nodes: []domain.Node{{ID: "untitled", Content: "content"}}})
	if err == nil || !strings.Contains(err.Error(), "requires a title") {
		t.Fatalf("validateVisibleGraph() error = %v", err)
	}
}

func TestSearchGraphMatchesContentAndReturnsOneHopRelationships(t *testing.T) {
	graph := GraphDTO{
		ProjectID: "project-1", SessionID: "session-1",
		Nodes: []NodeDTO{
			{ID: "agent", Title: "Agent", Content: "Maintains project knowledge"},
			{ID: "mind-map", Title: "Mind map", Content: "Search nodes and durable concepts"},
			{ID: "storage", Title: "Storage", Content: "Persists graphs"},
		},
		Edges: []EdgeDTO{
			{ID: "agent-map", SourceID: "agent", TargetID: "mind-map", Label: "updates"},
			{ID: "map-storage", SourceID: "mind-map", TargetID: "storage", Label: "persists through"},
		},
	}

	result := searchGraph(graph, "durable concepts", 20)

	if result.TotalMatches != 1 || result.Truncated || len(result.Matches) != 1 || result.Matches[0].Node.ID != "mind-map" {
		t.Fatalf("matches = %#v", result)
	}
	if len(result.Matches[0].MatchedFields) != 1 || result.Matches[0].MatchedFields[0] != "content" {
		t.Fatalf("matched fields = %#v", result.Matches[0].MatchedFields)
	}
	if len(result.RelatedNodes) != 2 || len(result.Edges) != 2 {
		t.Fatalf("one-hop result = %#v", result)
	}
}

func TestSearchReturnsMainAndVisibleCardNodeScopes(t *testing.T) {
	ctx := context.Background()
	store := openMindMapTestStore(t)
	settings := &testMindMapSettings{configuration: settingdomain.MindMapConfiguration{
		Enabled: true, Mode: settingdomain.MindMapModeRealtime, MaxConcurrent: 1,
	}}
	project, session := saveMindMapTestProjectAndSession(t, store, "session-search")
	session.Status = sessiondomain.StatusRunning
	session.ClosedAt = nil
	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatal(err)
	}
	service := New(store.MindMaps(), store.Projects(), store.Sessions(), settings, store)
	changeIndex := 0
	service.generateID = func() (domain.ChangeID, error) {
		changeIndex++
		return domain.ChangeID(fmt.Sprintf("search-change-%d", changeIndex)), nil
	}
	mainTitle, cardTitle := "Shared Search", "Card Search"
	if _, err := service.Update(ctx, UpdateInput{ProjectID: domain.ProjectID(project.ID), Operations: []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "main-node", Title: &mainTitle,
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(ctx, UpdateInput{
		ProjectID: domain.ProjectID(project.ID), SessionID: domain.SessionID(session.ID),
		Operations: []OperationInput{{Kind: domain.ChangeUpsertNode, ID: "card-node", Title: &cardTitle}},
	}); err != nil {
		t.Fatal(err)
	}

	mainResult, err := service.Search(ctx, SearchInput{ProjectID: domain.ProjectID(project.ID), Query: "shared search"})
	if err != nil {
		t.Fatal(err)
	}
	if len(mainResult.Matches) != 1 || mainResult.Matches[0].NodeID != "main-node" || mainResult.Matches[0].SessionID != "" {
		t.Fatalf("main search result = %#v", mainResult)
	}
	cardResult, err := service.Search(ctx, SearchInput{ProjectID: domain.ProjectID(project.ID), Query: "card search"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cardResult.Matches) != 1 || cardResult.Matches[0].NodeID != "card-node" || cardResult.Matches[0].SessionID != domain.SessionID(session.ID) {
		t.Fatalf("card search result = %#v", cardResult)
	}
}

func TestCardDeltaContainsOnlyAddedNodesAndTheirRelationships(t *testing.T) {
	base := domain.Graph{
		Nodes: []domain.Node{
			{ID: domain.RootNodeID, Title: "AnyCode"},
			{ID: "modified", Title: "Before"},
			{ID: "deleted", Title: "Deleted"},
		},
	}
	current := domain.Graph{
		Nodes: []domain.Node{
			{ID: domain.RootNodeID, Title: "AnyCode"},
			{ID: "modified", Title: "After"},
			{ID: "added", Title: "Added"},
		},
		Edges: []domain.Edge{{ID: "root-added", SourceID: domain.RootNodeID, TargetID: "added", Label: "contains"}},
	}

	nodes, edges, modifiedNodeIDs, deletedNodeIDs := cardDeltaDTO(base, current)

	if len(nodes) != 1 || nodes[0].ID != "added" || nodes[0].ChangeType != NodeAdded {
		t.Fatalf("card nodes = %#v", nodes)
	}
	if len(edges) != 1 || edges[0].ID != "root-added" {
		t.Fatalf("card edges = %#v", edges)
	}
	if len(modifiedNodeIDs) != 1 || modifiedNodeIDs[0] != "modified" {
		t.Fatalf("modified node ids = %#v", modifiedNodeIDs)
	}
	if len(deletedNodeIDs) != 1 || deletedNodeIDs[0] != "deleted" {
		t.Fatalf("deleted node ids = %#v", deletedNodeIDs)
	}
}

func TestWatchEmitsMinimalChangeAfterGraphUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := openMindMapTestStore(t)
	settings := &testMindMapSettings{configuration: settingdomain.MindMapConfiguration{Enabled: true, Mode: settingdomain.MindMapModeRealtime}}
	project, _ := saveMindMapTestProjectAndSession(t, store, "session-watch")
	service := New(store.MindMaps(), store.Projects(), store.Sessions(), settings, store)
	service.watchInterval = 5 * time.Millisecond
	changes, err := service.Watch(ctx, GetInput{ProjectID: domain.ProjectID(project.ID)})
	if err != nil {
		t.Fatal(err)
	}
	title := "feature"
	if _, err := service.Update(ctx, UpdateInput{ProjectID: domain.ProjectID(project.ID), Operations: []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "feature", Title: &title,
	}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-changes:
		if change.ProjectID != domain.ProjectID(project.ID) || change.SessionID != "" || change.UpdatedAt.IsZero() {
			t.Fatalf("change = %#v", change)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mind map change")
	}
}

func TestProjectWatchEmitsAfterCardOverlayUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := openMindMapTestStore(t)
	settings := &testMindMapSettings{configuration: settingdomain.MindMapConfiguration{Enabled: true, Mode: settingdomain.MindMapModeRealtime}}
	project, session := saveMindMapTestProjectAndSession(t, store, "session-card-watch")
	service := New(store.MindMaps(), store.Projects(), store.Sessions(), settings, store)
	service.watchInterval = 5 * time.Millisecond
	service.now = func() time.Time { return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) }
	service.generateID = func() (domain.ChangeID, error) { return "card-watch-change", nil }
	changes, err := service.Watch(ctx, GetInput{ProjectID: domain.ProjectID(project.ID)})
	if err != nil {
		t.Fatal(err)
	}
	title := "card feature"
	if _, err := service.Update(ctx, UpdateInput{
		ProjectID: domain.ProjectID(project.ID), SessionID: domain.SessionID(session.ID),
		Operations: []OperationInput{{Kind: domain.ChangeUpsertNode, ID: "card-feature", Title: &title}},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-changes:
		if change.ProjectID != domain.ProjectID(project.ID) || change.SessionID != "" || change.UpdatedAt.IsZero() {
			t.Fatalf("change = %#v", change)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for card mind map change")
	}
}

func TestProjectGraphPagesLoadEveryNodeAndEdgeOnce(t *testing.T) {
	ctx := context.Background()
	store := openMindMapTestStore(t)
	settings := &testMindMapSettings{configuration: settingdomain.MindMapConfiguration{Enabled: true, Mode: settingdomain.MindMapModeRealtime}}
	project, _ := saveMindMapTestProjectAndSession(t, store, "session-pages")
	service := New(store.MindMaps(), store.Projects(), store.Sessions(), settings, store)
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	sequence := 0
	service.generateID = func() (domain.ChangeID, error) {
		sequence++
		return domain.ChangeID(fmt.Sprintf("change-%d", sequence)), nil
	}
	operations := make([]OperationInput, 0, 49)
	for index := 0; index < 25; index++ {
		id := fmt.Sprintf("node-%02d", index)
		title := fmt.Sprintf("Node %02d", index)
		operation := OperationInput{Kind: domain.ChangeUpsertNode, ID: id, Title: &title}
		if index == 0 {
			files := []domain.NodeFile{{File: "internal/node.go", Method: "Node00", StartLine: 1, EndLine: 10}}
			operation.Files = &files
		}
		operations = append(operations, operation)
		if index > 0 {
			source, target := domain.NodeID(fmt.Sprintf("node-%02d", index-1)), domain.NodeID(id)
			operations = append(operations, OperationInput{Kind: domain.ChangeUpsertEdge, ID: fmt.Sprintf("edge-%02d", index), SourceID: &source, TargetID: &target})
		}
	}
	update, err := service.Apply(ctx, UpdateInput{ProjectID: domain.ProjectID(project.ID), Operations: operations})
	if err != nil {
		t.Fatal(err)
	}
	if update.UpdatedAt.IsZero() || len(update.Nodes) != 0 || len(update.Edges) != 0 {
		t.Fatalf("minimal update result=%#v", update)
	}
	nodeIDs, edgeIDs := map[domain.NodeID]struct{}{}, map[domain.EdgeID]struct{}{}
	foundFiles := false
	var nodeAfter domain.NodeID
	var edgeAfter domain.EdgeID
	includeNodes, includeEdges := true, true
	for includeNodes || includeEdges {
		page, err := service.GetPage(ctx, GetPageInput{
			ProjectID: domain.ProjectID(project.ID), NodeAfter: nodeAfter, EdgeAfter: edgeAfter,
			IncludeNodes: includeNodes, IncludeEdges: includeEdges, PageSize: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range page.Nodes {
			if _, duplicate := nodeIDs[node.ID]; duplicate {
				t.Fatalf("duplicate node %q", node.ID)
			}
			nodeIDs[node.ID] = struct{}{}
			if node.ID == "node-00" && len(node.Files) == 1 && node.Files[0].Method == "Node00" {
				foundFiles = true
			}
		}
		for _, edge := range page.Edges {
			if _, duplicate := edgeIDs[edge.ID]; duplicate {
				t.Fatalf("duplicate edge %q", edge.ID)
			}
			edgeIDs[edge.ID] = struct{}{}
		}
		nodeAfter, edgeAfter = page.NextNodeCursor, page.NextEdgeCursor
		includeNodes, includeEdges = nodeAfter != "", edgeAfter != ""
	}
	if len(nodeIDs) != 26 || len(edgeIDs) != 24 || !foundFiles {
		t.Fatalf("loaded nodes=%d edges=%d foundFiles=%v", len(nodeIDs), len(edgeIDs), foundFiles)
	}
}

func openMindMapTestStore(t *testing.T) *entstore.Store {
	t.Helper()
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}

func saveMindMapTestProjectAndSession(t *testing.T, store *entstore.Store, sessionID sessiondomain.ID) (projectdomain.Project, sessiondomain.Session) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	project := projectdomain.Project{
		ID: "project-1", Name: "AnyCode", Path: projectdomain.ProjectPath{Value: t.TempDir()},
		MindMapEnabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Projects().Save(ctx, project); err != nil {
		t.Fatal(err)
	}
	closedAt := now
	session := sessiondomain.Session{
		ID: sessionID, ProjectID: sessiondomain.ProjectID(project.ID), Requirement: "维护项目思维图",
		Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusClosed, ClosedAt: &closedAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatal(err)
	}
	return project, session
}
