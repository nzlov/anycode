package mindmap

import (
	"context"
	"path/filepath"
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
	source, target := domain.NodeID("source"), domain.NodeID("target")
	service := &Service{now: time.Now, generateID: func() (domain.ChangeID, error) { return "change", nil }}
	tests := []struct {
		name      string
		operation OperationInput
	}{
		{name: "node without fields", operation: OperationInput{Kind: domain.ChangeUpsertNode, ID: "node"}},
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
	if _, err := service.buildChanges(UpdateInput{ProjectID: "project", Operations: []OperationInput{{
		Kind: domain.ChangeUpsertNode, ID: "node", Content: &content,
	}}}); err != nil {
		t.Fatalf("content-only node update failed: %v", err)
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
