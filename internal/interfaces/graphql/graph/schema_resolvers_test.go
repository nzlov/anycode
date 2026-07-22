package graph

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/nzlov/anycode/internal/application/apperror"
	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	diffapp "github.com/nzlov/anycode/internal/application/diff"
	notificationapp "github.com/nzlov/anycode/internal/application/notification"
	"github.com/nzlov/anycode/internal/application/port"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	sessioneventapp "github.com/nzlov/anycode/internal/application/sessionevent"
	settingapp "github.com/nzlov/anycode/internal/application/setting"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	authdomain "github.com/nzlov/anycode/internal/domain/auth"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestQuerySessionFilesReturnsUnpaginatedFiles(t *testing.T) {
	artifacts := &fakeArtifactUseCase{files: []sessiondomain.SessionFile{{ID: "artifact-1", SessionID: "session-1"}}}
	files, err := NewResolver(UseCases{Artifacts: artifacts}).Query().SessionFiles(context.Background(), model.ListSessionFilesInput{
		SessionID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.query.SessionID != "session-1" {
		t.Fatalf("artifact query = %#v", artifacts.query)
	}
	if len(files) != 1 || files[0].ID != "artifact-1" {
		t.Fatalf("session files = %#v", files)
	}
}

func TestResolveSessionArtifactsMapsSafeFiles(t *testing.T) {
	artifacts := &fakeArtifactUseCase{resolved: []sessiondomain.SessionFile{{
		ID: "artifact-1", SessionID: "session-1", Role: sessiondomain.FileRoleArtifact,
		LogicalPath: "reports/result.txt", Filename: "result.txt", Path: "/private/result.txt",
		PreviewKind: sessiondomain.PreviewKindText,
	}}}
	got, err := NewResolver(UseCases{Artifacts: artifacts}).Query().ResolveSessionArtifacts(context.Background(), model.ResolveSessionArtifactsInput{
		SessionID: "session-1", LogicalPaths: []string{"reports/result.txt", "missing.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(artifacts.resolvePaths, []string{"reports/result.txt", "missing.txt"}) {
		t.Fatalf("resolve paths = %#v", artifacts.resolvePaths)
	}
	if len(got) != 1 || got[0].LogicalPath != "reports/result.txt" || got[0].File.ID != "artifact-1" {
		t.Fatalf("resolved artifacts = %#v", got)
	}
	if got[0].File.PreviewURL == nil || *got[0].File.PreviewURL != "/files/artifact-1/preview" || got[0].File.DownloadURL != "/files/artifact-1/download" {
		t.Fatalf("resolved file URLs = %#v", got[0].File)
	}
}

func TestDeleteSessionFileUsesSessionCommand(t *testing.T) {
	sessions := &fakeSessionUseCase{}
	deleted, err := NewResolver(UseCases{Sessions: sessions}).Mutation().DeleteSessionFile(context.Background(), "artifact-1")
	if err != nil || !deleted {
		t.Fatalf("DeleteSessionFile() = %v, %v", deleted, err)
	}
	if sessions.gotDeleteSessionFileID != "artifact-1" {
		t.Fatalf("deleted session file id = %q", sessions.gotDeleteSessionFileID)
	}
}

func TestCleanupSessionsMapsFilterInput(t *testing.T) {
	sessions := &fakeSessionUseCase{cleanupResult: 4}
	projectID := "project-1"
	scope := "closed"
	filter := "archive"
	count, err := NewResolver(UseCases{Sessions: sessions}).Mutation().CleanupSessions(context.Background(), model.CleanupSessionsInput{
		ProjectID: &projectID, Scope: &scope, Filter: &filter, OlderThanDays: 7,
	})
	if err != nil || count != 4 {
		t.Fatalf("CleanupSessions() = %d, %v", count, err)
	}
	if sessions.gotCleanup.ProjectID == nil || *sessions.gotCleanup.ProjectID != "project-1" || sessions.gotCleanup.Scope != "closed" || sessions.gotCleanup.Filter != "archive" || sessions.gotCleanup.OlderThanDays != 7 {
		t.Fatalf("cleanup input = %#v", sessions.gotCleanup)
	}
}

type fakeArtifactUseCase struct {
	artifactapp.UseCase
	query        sessiondomain.ArtifactQuery
	files        []sessiondomain.SessionFile
	resolvePaths []string
	resolved     []sessiondomain.SessionFile
}

func (f *fakeArtifactUseCase) List(_ context.Context, query sessiondomain.ArtifactQuery) ([]sessiondomain.SessionFile, error) {
	f.query = query
	return f.files, nil
}

func (f *fakeArtifactUseCase) Resolve(_ context.Context, _ sessiondomain.ID, logicalPaths []string) ([]sessiondomain.SessionFile, error) {
	f.resolvePaths = append([]string(nil), logicalPaths...)
	return f.resolved, nil
}

func TestMapPendingApprovalIncludesResultProjection(t *testing.T) {
	got := mapPendingApproval(&sessionapp.PendingApprovalDTO{
		SessionID:        "session-1",
		NodeID:           "build",
		NodeRunID:        "node-run-1",
		CurrentNodeTitle: "Build",
		Phase:            "after_run",
		Result:           map[string]any{"version": 1, "outcome": "success", "summary": "done", "data": map[string]any{}},
	})
	if got == nil || got.Phase != "after_run" || got.Result["outcome"] != "success" {
		t.Fatalf("mapPendingApproval() = %#v", got)
	}
}

func TestMapPendingApprovalKeepsBeforeRunResultNull(t *testing.T) {
	got := mapPendingApproval(&sessionapp.PendingApprovalDTO{
		SessionID: "session-1", NodeID: "build", NodeRunID: "node-run-1", Phase: "before_run",
	})
	if got == nil || got.Result != nil {
		t.Fatalf("mapPendingApproval() = %#v", got)
	}
}

func TestQueryCodexModelOptionsReturnsStartupCatalog(t *testing.T) {
	resolver := NewResolver(UseCases{
		CodexModels: []processdomain.CodexModel{
			{
				Slug:                  "gpt-5.6-sol",
				DisplayName:           "GPT-5.6-Sol",
				DefaultReasoningLevel: "low",
				SupportedReasoningLevels: []processdomain.CodexReasoningLevel{
					{Effort: "low", Description: "Fast responses"},
					{Effort: "ultra", Description: "Delegated maximum"},
				},
			},
		},
	}).Query()

	got, err := resolver.CodexModelOptions(context.Background())
	if err != nil {
		t.Fatalf("CodexModelOptions() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("CodexModelOptions() = %#v", got)
	}
	if got[0].Value != "gpt-5.6-sol" || got[0].Label != "GPT-5.6-Sol" || got[0].DefaultReasoningEffort != "low" {
		t.Fatalf("CodexModelOptions()[0] = %#v", got[0])
	}
	if len(got[0].ReasoningEfforts) != 2 || got[0].ReasoningEfforts[1].Value != "ultra" {
		t.Fatalf("ReasoningEfforts = %#v", got[0].ReasoningEfforts)
	}
}

func TestQuickCommandResolversForwardSettingsUseCase(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	settings := &fakeSettingUseCase{
		listResult: port.Page[settingapp.QuickCommandDTO]{
			Items: []settingapp.QuickCommandDTO{
				{ID: "command-1", Content: "检查测试", CreatedAt: now},
				{ID: "command-2", Content: "检查测试", CreatedAt: now.Add(time.Second)},
			},
			Page: 2, PageSize: 20, Total: 22,
		},
		createResult: settingapp.QuickCommandDTO{ID: "command-3", Content: "总结变更", CreatedAt: now.Add(2 * time.Second)},
	}
	resolver := NewResolver(UseCases{Settings: settings})

	pageValue := 2
	pageSize := 20
	listed, err := resolver.Query().QuickCommands(context.Background(), &model.ListQuickCommandsInput{Page: &pageValue, PageSize: &pageSize})
	if err != nil {
		t.Fatalf("QuickCommands() error = %v", err)
	}
	if settings.listInput.Page != 2 || settings.listInput.PageSize != 20 || len(listed.Items) != 2 || listed.Items[0].ID != "command-1" || listed.PageInfo.Total != 22 {
		t.Fatalf("QuickCommands() = %#v", listed)
	}
	created, err := resolver.Mutation().CreateQuickCommand(context.Background(), model.CreateQuickCommandInput{Content: "总结变更"})
	if err != nil {
		t.Fatalf("CreateQuickCommand() error = %v", err)
	}
	if settings.createInput.Content != "总结变更" || created.ID != "command-3" {
		t.Fatalf("create input=%#v result=%#v", settings.createInput, created)
	}
	deleted, err := resolver.Mutation().DeleteQuickCommand(context.Background(), "command-1")
	if err != nil {
		t.Fatalf("DeleteQuickCommand() error = %v", err)
	}
	if !deleted || settings.deleteInput.ID != settingdomain.QuickCommandID("command-1") {
		t.Fatalf("delete result=%t input=%#v", deleted, settings.deleteInput)
	}
}

func TestAppearanceSettingsResolversForwardSettingsUseCase(t *testing.T) {
	settings := &fakeSettingUseCase{
		appearanceResult: settingapp.AppearanceSettingsDTO{
			WallpaperColorScheme: settingdomain.WallpaperColorSchemeRainbow,
		},
	}
	resolver := NewResolver(UseCases{Settings: settings})

	got, err := resolver.Query().AppearanceSettings(context.Background())
	if err != nil {
		t.Fatalf("AppearanceSettings() error = %v", err)
	}
	if got.WallpaperColorScheme != model.WallpaperColorSchemeRainbow {
		t.Fatalf("AppearanceSettings() = %#v", got)
	}

	updated, err := resolver.Mutation().UpdateAppearanceSettings(context.Background(), model.UpdateAppearanceSettingsInput{
		WallpaperColorScheme: model.WallpaperColorSchemeFruitSalad,
	})
	if err != nil {
		t.Fatalf("UpdateAppearanceSettings() error = %v", err)
	}
	if settings.appearanceInput.WallpaperColorScheme != settingdomain.WallpaperColorSchemeFruitSalad || updated.WallpaperColorScheme != model.WallpaperColorSchemeRainbow {
		t.Fatalf("update input=%#v result=%#v", settings.appearanceInput, updated)
	}
}

func TestWebPushResolversForwardPrincipalAndSubscription(t *testing.T) {
	notifications := &fakeNotificationUseCase{
		config:       notificationapp.ConfigDTO{Enabled: true, PublicKey: "public", ProxyURL: "http://old-proxy.example:8080"},
		registration: notificationapp.SubscriptionDTO{ID: "subscription-1"},
	}
	resolver := NewResolver(UseCases{Notifications: notifications})
	ctx := WithPrincipal(context.Background(), authdomain.AccessPrincipal{KeyHash: "principal", Kind: "test"})

	config, err := resolver.Query().WebPushConfig(ctx)
	if err != nil || !config.Enabled || config.PublicKey != "public" || config.ProxyURL != "http://old-proxy.example:8080" {
		t.Fatalf("WebPushConfig() = %#v, %v", config, err)
	}
	updated, err := resolver.Mutation().UpdateWebPushProxy(ctx, "socks5://proxy.example:1080")
	if err != nil || notifications.updateProxyInput.ProxyURL != "socks5://proxy.example:1080" || updated.ProxyURL != "http://old-proxy.example:8080" {
		t.Fatalf("UpdateWebPushProxy() = %#v, input = %#v, error = %v", updated, notifications.updateProxyInput, err)
	}
	registered, err := resolver.Mutation().RegisterPushSubscription(ctx, model.RegisterPushSubscriptionInput{
		Endpoint: "https://push.example/1", P256dh: "p256dh", Auth: "auth",
	})
	if err != nil || registered.ID != "subscription-1" {
		t.Fatalf("RegisterPushSubscription() = %#v, %v", registered, err)
	}
	if notifications.registerInput.PrincipalKeyHash != "principal" || notifications.registerInput.Endpoint != "https://push.example/1" {
		t.Fatalf("register input = %#v", notifications.registerInput)
	}
	unregistered, err := resolver.Mutation().UnregisterPushSubscription(ctx, "subscription-1")
	if err != nil || !unregistered || notifications.unregisterInput.PrincipalKeyHash != "principal" || notifications.unregisterInput.ID != "subscription-1" {
		t.Fatalf("UnregisterPushSubscription() = %t, input = %#v, error = %v", unregistered, notifications.unregisterInput, err)
	}
}

func TestQueryProjectsForwardsUseCase(t *testing.T) {
	now := time.Unix(10, 0).UTC()
	projects := &fakeProjectUseCase{
		listResult: []projectapp.DTO{
			{
				ID:    "project-1",
				Name:  "AnyCode",
				Path:  "/workspace/anycode",
				IsGit: true,
				GitState: projectdomain.GitState{
					IsRepository:  true,
					CurrentBranch: "main",
					Branches:      []projectdomain.GitBranch{{Name: "main", IsCurrent: true}},
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	resolver := NewResolver(UseCases{Projects: projects}).Query()

	got, err := resolver.Projects(context.Background())
	if err != nil {
		t.Fatalf("Projects() error = %v", err)
	}
	if projects.listCalls != 1 {
		t.Fatalf("ListProjects calls = %d", projects.listCalls)
	}
	if len(got) != 1 || got[0].ID != "project-1" || got[0].GitState.CurrentBranch != "main" {
		t.Fatalf("Projects() = %#v", got)
	}
}

func TestQueryBrowseDirectoryForwardsUseCase(t *testing.T) {
	projects := &fakeProjectUseCase{
		browseResult: projectapp.DirectoryPageDTO{
			Path:   "/workspace",
			Parent: "/",
			Entries: []projectdomain.DirectoryEntry{
				{Name: "anycode", Path: "/workspace/anycode", IsDir: true, IsGit: true, CanRead: true},
				{Name: "private", Path: "/workspace/private", IsDir: true, CanRead: false, ErrorCode: "permission_denied"},
			},
		},
	}
	resolver := NewResolver(UseCases{Projects: projects}).Query()

	got, err := resolver.BrowseDirectory(context.Background(), model.BrowseDirectoryInput{Path: "/workspace"})
	if err != nil {
		t.Fatalf("BrowseDirectory() error = %v", err)
	}
	if projects.browseInput.Path != "/workspace" {
		t.Fatalf("BrowseDirectory input = %#v", projects.browseInput)
	}
	if got.Path != "/workspace" || len(got.Entries) != 2 || !got.Entries[0].IsGit || got.Entries[1].ErrorCode != "permission_denied" {
		t.Fatalf("BrowseDirectory() = %#v", got)
	}
}

func TestQueryProjectGitStateForwardsRefresh(t *testing.T) {
	projects := &fakeProjectUseCase{
		gitStateResult: projectdomain.GitState{
			IsRepository:  true,
			CurrentBranch: "main",
			Branches:      []projectdomain.GitBranch{{Name: "main", IsCurrent: true}},
		},
	}
	resolver := NewResolver(UseCases{Projects: projects}).Query()

	got, err := resolver.ProjectGitState(context.Background(), "project-1", true)
	if err != nil {
		t.Fatalf("ProjectGitState() error = %v", err)
	}
	if projects.gitStateInput.ProjectID != "project-1" || !projects.gitStateInput.Refresh {
		t.Fatalf("ProjectGitState input = %#v", projects.gitStateInput)
	}
	if got.CurrentBranch != "main" || len(got.Branches) != 1 {
		t.Fatalf("ProjectGitState() = %#v", got)
	}
}

func TestMutationCreateProjectForwardsUseCase(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	projects := &fakeProjectUseCase{
		createResult: projectapp.DTO{
			ID:        "project-1",
			Name:      "AnyCode",
			Path:      "/workspace/anycode",
			IsGit:     true,
			GitState:  projectdomain.GitState{IsRepository: true, CurrentBranch: "main"},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	resolver := NewResolver(UseCases{Projects: projects}).Mutation()

	got, err := resolver.CreateProject(context.Background(), model.CreateProjectInput{
		Path: "/workspace/anycode",
		Name: "AnyCode",
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	if projects.createInput.Path != "/workspace/anycode" || projects.createInput.Name != "AnyCode" {
		t.Fatalf("CreateProject input = %#v", projects.createInput)
	}
	if got.ID != "project-1" || !got.IsGit || !got.GitState.IsRepository {
		t.Fatalf("CreateProject() = %#v", got)
	}
}

func TestMutationUpdateProjectSettingsForwardsRawCommand(t *testing.T) {
	command := "  echo first\necho second\n\n"
	projects := &fakeProjectUseCase{
		updateSettingsResult: projectapp.DTO{
			ID:                  "project-1",
			Name:                "AnyCode",
			WorktreeInitCommand: command,
		},
	}
	resolver := NewResolver(UseCases{Projects: projects}).Mutation()

	got, err := resolver.UpdateProjectSettings(context.Background(), model.UpdateProjectSettingsInput{
		ProjectID:           "project-1",
		WorktreeInitCommand: command,
	})
	if err != nil {
		t.Fatalf("UpdateProjectSettings() error = %v", err)
	}
	if projects.updateSettingsInput.ProjectID != "project-1" || projects.updateSettingsInput.WorktreeInitCommand != command {
		t.Fatalf("UpdateProjectSettings input = %#v", projects.updateSettingsInput)
	}
	if got.WorktreeInitCommand != command {
		t.Fatalf("UpdateProjectSettings() = %#v", got)
	}
}

func TestMutationRemoveProjectStopsSessionsAndForwardsUseCase(t *testing.T) {
	projects := &fakeProjectUseCase{}
	sessions := &fakeSessionUseCase{}
	resolver := NewResolver(UseCases{Projects: projects, Sessions: sessions}).Mutation()

	got, err := resolver.RemoveProject(context.Background(), "project-1")
	if err != nil {
		t.Fatalf("RemoveProject() error = %v", err)
	}
	if !got {
		t.Fatal("RemoveProject() = false")
	}
	if sessions.stopProjectID != "project-1" {
		t.Fatalf("stopProjectID = %q", sessions.stopProjectID)
	}
	if projects.removeCalls != 1 || projects.removeInput.ProjectID != "project-1" {
		t.Fatalf("remove input = %#v calls=%d", projects.removeInput, projects.removeCalls)
	}
}

func TestQueryWorkflowDefinitionForwardsUseCase(t *testing.T) {
	workflows := &fakeWorkflowUseCase{
		getResult: workflowapp.DefinitionDTO{
			ID:        "workflow-1",
			ProjectID: "project-1",
			Name:      "Default",
			Version:   2,
			Active:    true,
			Graph: workflowdomain.Graph{
				Nodes: []workflowdomain.Node{{ID: "node-1", Type: "codex", Title: "实现"}},
				Edges: []workflowdomain.Edge{{From: "node-1", To: "node-2", Priority: 3}},
			},
		},
	}
	resolver := NewResolver(UseCases{Workflows: workflows}).Query()

	got, err := resolver.WorkflowDefinition(context.Background(), "workflow-1")
	if err != nil {
		t.Fatalf("WorkflowDefinition() error = %v", err)
	}
	if workflows.gotGetID != "workflow-1" {
		t.Fatalf("workflow definition id = %q", workflows.gotGetID)
	}
	if got.ID != "workflow-1" || got.ProjectID != "project-1" || got.Version != 2 || !got.Active {
		t.Fatalf("WorkflowDefinition() = %#v", got)
	}
	if len(got.Graph.Nodes) != 1 || got.Graph.Nodes[0].ID != "node-1" || got.Graph.Nodes[0].Title != "实现" {
		t.Fatalf("WorkflowDefinition() nodes = %#v", got.Graph.Nodes)
	}
	if len(got.Graph.Edges) != 1 || got.Graph.Edges[0].Priority != 3 {
		t.Fatalf("WorkflowDefinition() edges = %#v", got.Graph.Edges)
	}
}

func TestQuerySessionCardForwardsUseCase(t *testing.T) {
	sessions := &fakeSessionUseCase{getCardResult: sessionapp.CardDTO{
		DTO:         sessionapp.DTO{ID: "session-1", ProjectID: "project-1", Status: sessiondomain.StatusRunning},
		ProjectName: "AnyCode",
	}}
	got, err := NewResolver(UseCases{Sessions: sessions}).Query().SessionCard(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if sessions.gotGetCardID != "session-1" || got.ID != "session-1" || got.ProjectName != "AnyCode" {
		t.Fatalf("session card = %#v, requested = %q", got, sessions.gotGetCardID)
	}
}

func TestSubscriptionSessionEventsStartsWithTranscriptEvent(t *testing.T) {
	source := make(chan timelineapp.DTO, 1)
	source <- timelineapp.DTO{
		ID: "event-1", OrderKey: "order-1", Phase: processdomain.CodexPhaseStandalone,
		Content:    processdomain.CodexMessageContent{Role: "assistant", Text: "hello", Format: processdomain.CodexTextMarkdown},
		OccurredAt: "2026-07-02T01:02:03Z",
	}
	close(source)
	sessionEvents := &fakeSessionEventUseCase{events: source}
	ch, err := NewResolver(UseCases{SessionEvents: sessionEvents}).Subscription().SessionEvents(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := <-ch
	if !ok || got.ID != "event-1" || got.OrderKey != "order-1" || !got.OccurredAt.Equal(time.Date(2026, 7, 2, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("first transcript event = %#v", got)
	}
	content, ok := got.Content.(*model.TranscriptMessageContent)
	if !ok || content.Text != "hello" {
		t.Fatalf("content = %#v", got.Content)
	}
	if _, ok := <-ch; ok {
		t.Fatal("transcript channel stayed open after source closed")
	}
}

func TestMapSessionUpdateEventUsesEventSpecificFields(t *testing.T) {
	artifactCount := 2
	filesChanged := 4
	statusAt := time.Date(2026, 7, 2, 1, 2, 3, 0, time.UTC)
	metadataAt := statusAt.Add(time.Minute)
	status := sessionapp.CardStatusDTO{
		Status: sessiondomain.StatusRunning, CurrentNodeTitle: "Implement",
		AvailableActions: []string{"stop"}, UpdatedAt: statusAt,
	}
	todo := sessiondomain.TodoList{Items: []sessiondomain.TodoItem{{Text: "Implement", Completed: true}}}
	usage := sessiondomain.TokenUsage{InputTokens: 10, TotalTokens: 12}
	priority := sessiondomain.PriorityHigh
	config := sessiondomain.Config{CodexModel: "gpt-5.4", ReasoningEffort: "high", PermissionMode: "workspace-write", FastMode: true}
	cleanup := sessionapp.WorktreeCleanupDTO{Status: sessiondomain.WorktreeCleanupFailed, Attempts: 2}
	actions := []string{"retry_worktree_cleanup"}

	tests := []struct {
		name    string
		input   sessioneventapp.UpdateDTO
		present map[string]bool
	}{
		{name: "status", input: sessioneventapp.UpdateDTO{Type: sessioneventapp.TypeStatus, SessionID: "session-1", Status: &status}, present: map[string]bool{"status": true}},
		{name: "todo", input: sessioneventapp.UpdateDTO{Type: "session.todo_list_updated", SessionID: "session-1", TodoList: &todo}, present: map[string]bool{"todo": true}},
		{name: "usage", input: sessioneventapp.UpdateDTO{Type: sessioneventapp.TypeUsage, SessionID: "session-1", Usage: &usage}, present: map[string]bool{"usage": true}},
		{name: "diff", input: sessioneventapp.UpdateDTO{Type: "session.diff_changed", SessionID: "session-1", FilesChanged: &filesChanged}, present: map[string]bool{"diff": true}},
		{name: "artifacts", input: sessioneventapp.UpdateDTO{Type: "session.artifacts_updated", SessionID: "session-1", ArtifactCount: &artifactCount}, present: map[string]bool{"artifacts": true}},
		{name: "priority", input: sessioneventapp.UpdateDTO{Type: "session.priority_changed", SessionID: "session-1", Priority: &priority, UpdatedAt: &metadataAt}, present: map[string]bool{"priority": true, "updatedAt": true}},
		{name: "config", input: sessioneventapp.UpdateDTO{Type: "session.config_changed", SessionID: "session-1", Config: &config, UpdatedAt: &metadataAt}, present: map[string]bool{"config": true, "updatedAt": true}},
		{name: "worktree", input: sessioneventapp.UpdateDTO{Type: "session.worktree_cleanup_failed", SessionID: "session-1", WorktreeCleanup: &cleanup, AvailableActions: actions, UpdatedAt: &metadataAt}, present: map[string]bool{"worktree": true, "actions": true, "updatedAt": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapSessionUpdateEvent(tt.input)
			present := map[string]bool{}
			if got.Status != nil {
				present["status"] = true
			}
			if got.TodoList != nil {
				present["todo"] = true
			}
			if got.Usage != nil {
				present["usage"] = true
			}
			if got.FilesChanged != nil {
				present["diff"] = true
			}
			if got.ArtifactCount != nil {
				present["artifacts"] = true
			}
			if got.Priority != nil {
				present["priority"] = true
			}
			if got.Config != nil {
				present["config"] = true
			}
			if got.WorktreeCleanup != nil {
				present["worktree"] = true
			}
			if got.AvailableActions != nil {
				present["actions"] = true
			}
			if got.UpdatedAt != nil {
				present["updatedAt"] = true
			}
			if got.EventType != tt.input.Type || got.SessionID != "session-1" || !reflect.DeepEqual(present, tt.present) {
				t.Fatalf("mapped update = %#v, present = %#v", got, present)
			}
		})
	}
}

func TestSubscriptionSessionEventsStopsBlockedSendAfterCancel(t *testing.T) {
	source := make(chan timelineapp.DTO, 1)
	sessionEvents := &fakeSessionEventUseCase{events: source}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := NewResolver(UseCases{SessionEvents: sessionEvents}).Subscription().SessionEvents(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	source <- timelineapp.DTO{ID: "event-1", Content: processdomain.CodexMessageContent{Role: "assistant", Text: "blocked"}}
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("transcript subscription delivered after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("transcript subscription did not close after cancellation")
	}
}

func TestQuerySessionTranscriptForwardsBeforeCursorAndLimit(t *testing.T) {
	beforeCursor := "0:40"
	messageRole := "assistant"
	limit := 50
	timeline := &fakeTimelineUseCase{}
	resolver := NewResolver(UseCases{Timeline: timeline}).Query()

	_, err := resolver.SessionTranscript(context.Background(), model.ListTranscriptEventsInput{
		SessionID:    "session-1",
		BeforeCursor: &beforeCursor,
		MessageRole:  &messageRole,
		Limit:        &limit,
	})
	if err != nil {
		t.Fatalf("SessionTranscript() error = %v", err)
	}

	want := timelineapp.ListSessionEventsInput{
		SessionID:    "session-1",
		BeforeCursor: beforeCursor,
		MessageRole:  messageRole,
		Limit:        limit,
	}
	if !reflect.DeepEqual(timeline.gotListSessionEventsInput, want) {
		t.Fatalf("ListSessionEvents() input = %#v, want %#v", timeline.gotListSessionEventsInput, want)
	}
}

func TestQueryPendingQuestionRequestsForwardsUseCase(t *testing.T) {
	questions := &fakeQuestionUseCase{
		pending: []questionapp.RequestDTO{
			{
				ID:        "request-1",
				SessionID: "session-1",
				Status:    questiondomain.RequestPending,
				Questions: []questiondomain.Question{
					{
						ID:      "question-1",
						Body:    "Choose",
						Options: []questiondomain.Option{{ID: "option-1", Label: "Continue"}},
					},
				},
			},
		},
	}
	resolver := NewResolver(UseCases{Questions: questions}).Query()

	got, err := resolver.PendingQuestionRequests(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("PendingQuestionRequests() error = %v", err)
	}
	if questions.gotPendingSessionID != "session-1" {
		t.Fatalf("pending session id = %q", questions.gotPendingSessionID)
	}
	if len(got) != 1 || got[0].ID != "request-1" || len(got[0].Questions) != 1 {
		t.Fatalf("pending requests = %#v", got)
	}
	if got[0].Questions[0].Answer == nil || got[0].Questions[0].Options[0].Payload == nil {
		t.Fatalf("pending question JSON maps should be non-nil: %#v", got[0].Questions[0])
	}
}

func TestMutationResumeSessionForwardsUseCase(t *testing.T) {
	now := time.Unix(30, 0).UTC()
	sessions := &fakeSessionUseCase{
		executeResult: sessionapp.DTO{
			ID:             "session-1",
			ProjectID:      "project-1",
			Requirement:    "resume work",
			Mode:           "chat",
			Status:         "running",
			CodexSessionID: "codex-session-1",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	got, err := resolver.ResumeSession(context.Background(), "session-1", nil)
	if err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if sessions.gotExecuteID != "session-1" {
		t.Fatalf("ResumeSession() id = %q", sessions.gotExecuteID)
	}
	if got.ID != "session-1" || got.Status != "running" || got.CodexSessionID != "codex-session-1" {
		t.Fatalf("ResumeSession() = %#v", got)
	}
}

func TestMutationRetrySessionWorktreeCleanupForwardsUseCase(t *testing.T) {
	now := time.Unix(30, 0).UTC()
	sessions := &fakeSessionUseCase{
		retryCleanupResult: sessionapp.DTO{
			ID:        "session-1",
			ProjectID: "project-1",
			Status:    sessiondomain.StatusClosed,
			WorktreeCleanup: sessionapp.WorktreeCleanupDTO{
				Status:   sessiondomain.WorktreeCleanupPending,
				Attempts: 2,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	got, err := resolver.RetrySessionWorktreeCleanup(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("RetrySessionWorktreeCleanup() error = %v", err)
	}
	if sessions.gotRetryCleanupID != "session-1" || got.WorktreeCleanup == nil || got.WorktreeCleanup.Status != "pending" || got.WorktreeCleanup.Attempts != 2 {
		t.Fatalf("RetrySessionWorktreeCleanup() = %#v id=%q", got, sessions.gotRetryCleanupID)
	}
}

func TestMutationRetrySessionInitializationForwardsUseCase(t *testing.T) {
	now := time.Unix(30, 0).UTC()
	sessions := &fakeSessionUseCase{
		retryInitializationResult: sessionapp.DTO{
			ID:        "session-1",
			ProjectID: "project-1",
			Status:    sessiondomain.StatusInitializing,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	got, err := resolver.RetrySessionInitialization(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("RetrySessionInitialization() error = %v", err)
	}
	if sessions.gotRetryInitializationID != "session-1" || got.Status != string(sessiondomain.StatusInitializing) {
		t.Fatalf("RetrySessionInitialization() = %#v id=%q", got, sessions.gotRetryInitializationID)
	}
}

func TestMutationStartSessionForwardsUnifiedExecutionUseCase(t *testing.T) {
	now := time.Unix(30, 0).UTC()
	sessions := &fakeSessionUseCase{
		executeResult: sessionapp.DTO{
			ID:          "session-1",
			ProjectID:   "project-1",
			Requirement: "start work",
			Mode:        "chat",
			Status:      "queued",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()
	force := true

	got, err := resolver.StartSession(context.Background(), "session-1", &force)
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if sessions.gotExecuteID != "session-1" || !sessions.gotExecuteForce {
		t.Fatalf("StartSession() input = id=%q force=%v", sessions.gotExecuteID, sessions.gotExecuteForce)
	}
	if got.ID != "session-1" || got.Status != "queued" {
		t.Fatalf("StartSession() = %#v", got)
	}
}

func TestMutationExecuteSessionForwardsUseCase(t *testing.T) {
	now := time.Unix(31, 0).UTC()
	sessions := &fakeSessionUseCase{
		executeResult: sessionapp.DTO{
			ID:             "session-1",
			ProjectID:      "project-1",
			Requirement:    "continue work",
			Mode:           "chat",
			Status:         "queued",
			CodexSessionID: "codex-session-1",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()
	force := true

	got, err := resolver.ExecuteSession(context.Background(), "session-1", &force)
	if err != nil {
		t.Fatalf("ExecuteSession() error = %v", err)
	}
	if sessions.gotExecuteID != "session-1" || !sessions.gotExecuteForce {
		t.Fatalf("ExecuteSession() input = id=%q force=%v", sessions.gotExecuteID, sessions.gotExecuteForce)
	}
	if got.ID != "session-1" || got.Status != "queued" || got.CodexSessionID != "codex-session-1" {
		t.Fatalf("ExecuteSession() = %#v", got)
	}
}

func TestMutationUpdateSessionConfigForwardsUseCase(t *testing.T) {
	now := time.Unix(32, 0).UTC()
	fastMode := true
	sessions := &fakeSessionUseCase{
		updateConfigResult: sessionapp.DTO{
			ID:          "session-1",
			ProjectID:   "project-1",
			Requirement: "resume work",
			Mode:        "chat",
			Status:      "stopped",
			Config: sessiondomain.Config{
				CodexModel:      "gpt-5.4-mini",
				ReasoningEffort: "high",
				PermissionMode:  "workspace-write",
				FastMode:        true,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	got, err := resolver.UpdateSessionConfig(context.Background(), model.UpdateSessionConfigInput{
		SessionID: "session-1",
		Config: &model.SessionConfigInput{
			CodexModel:      strPtr("gpt-5.4-mini"),
			ReasoningEffort: strPtr("high"),
			PermissionMode:  strPtr("workspace-write"),
			FastMode:        &fastMode,
		},
	})
	if err != nil {
		t.Fatalf("UpdateSessionConfig() error = %v", err)
	}
	if sessions.gotUpdateConfig.SessionID != "session-1" {
		t.Fatalf("UpdateSessionConfig() input = %#v", sessions.gotUpdateConfig)
	}
	if sessions.gotUpdateConfig.Config.CodexModel != "gpt-5.4-mini" || sessions.gotUpdateConfig.Config.ReasoningEffort != "high" || sessions.gotUpdateConfig.Config.PermissionMode != "workspace-write" || sessions.gotUpdateConfig.Config.FastMode == nil || !*sessions.gotUpdateConfig.Config.FastMode {
		t.Fatalf("UpdateSessionConfig() config = %#v", sessions.gotUpdateConfig.Config)
	}
	if got.ID != "session-1" || got.Config.CodexModel != "gpt-5.4-mini" || !got.Config.FastMode {
		t.Fatalf("UpdateSessionConfig() = %#v", got)
	}

	_, err = resolver.UpdateSessionConfig(context.Background(), model.UpdateSessionConfigInput{
		SessionID: "session-1",
		Config: &model.SessionConfigInput{
			CodexModel:      strPtr("gpt-5.4-mini"),
			ReasoningEffort: strPtr("high"),
			PermissionMode:  strPtr("workspace-write"),
		},
	})
	if err != nil || sessions.gotUpdateConfig.Config.FastMode != nil {
		t.Fatalf("UpdateSessionConfig() omitted FastMode = %#v, %v", sessions.gotUpdateConfig.Config.FastMode, err)
	}
}

func TestMutationCreateSessionPreservesNullableFastMode(t *testing.T) {
	now := time.Unix(33, 0).UTC()
	fastMode := false
	sessions := &fakeSessionUseCase{
		createResult: sessionapp.DTO{
			ID:          "session-1",
			ProjectID:   "project-1",
			Requirement: "implement fast mode",
			Mode:        "chat",
			Status:      "queued",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	_, err := resolver.CreateSession(context.Background(), model.CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement fast mode",
		Mode:        "chat",
		Config: &model.SessionConfigInput{
			CodexModel:      strPtr("gpt-5.4"),
			ReasoningEffort: strPtr("high"),
			PermissionMode:  strPtr("workspace-write"),
			FastMode:        &fastMode,
		},
		Mentions: []*model.PromptMentionInput{{Path: "src/main.go"}},
	})
	if err != nil || sessions.gotCreate.Config.FastMode == nil || *sessions.gotCreate.Config.FastMode || !reflect.DeepEqual(sessions.gotCreate.Mentions, []sessiondomain.PromptMention{{Path: "src/main.go"}}) {
		t.Fatalf("CreateSession() input = %#v, %v", sessions.gotCreate, err)
	}

	_, err = resolver.CreateSession(context.Background(), model.CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement fast mode",
		Mode:        "chat",
		Config: &model.SessionConfigInput{
			CodexModel:      strPtr("gpt-5.4"),
			ReasoningEffort: strPtr("high"),
			PermissionMode:  strPtr("workspace-write"),
		},
	})
	if err != nil || sessions.gotCreate.Config.FastMode != nil {
		t.Fatalf("CreateSession() omitted FastMode = %#v, %v", sessions.gotCreate.Config.FastMode, err)
	}
}

func TestMutationAppendPromptForwardsFileIDs(t *testing.T) {
	sessions := &fakeSessionUseCase{
		appendResult: sessionapp.PromptAppendDTO{
			ID:        "append-1",
			SessionID: "session-1",
			Body:      "continue",
			CreatedAt: time.Unix(33, 0).UTC(),
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	got, err := resolver.AppendPrompt(context.Background(), model.AppendPromptInput{
		SessionID:           "session-1",
		Body:                "continue",
		StagedAttachmentIds: []string{"staged-1", "staged-2"},
		ArtifactIds:         []string{"artifact-1", "artifact-2"},
		Mentions:            []*model.PromptMentionInput{{Path: "src/main.go"}},
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if got.ID != "append-1" {
		t.Fatalf("AppendPrompt() = %#v", got)
	}
	if len(sessions.gotAppend.StagedAttachmentIDs) != 2 || sessions.gotAppend.StagedAttachmentIDs[0] != "staged-1" || sessions.gotAppend.StagedAttachmentIDs[1] != "staged-2" {
		t.Fatalf("AppendPrompt() staged ids = %#v", sessions.gotAppend.StagedAttachmentIDs)
	}
	if len(sessions.gotAppend.ArtifactIDs) != 2 || sessions.gotAppend.ArtifactIDs[0] != "artifact-1" || sessions.gotAppend.ArtifactIDs[1] != "artifact-2" {
		t.Fatalf("AppendPrompt() artifact ids = %#v", sessions.gotAppend.ArtifactIDs)
	}
	if !reflect.DeepEqual(sessions.gotAppend.Mentions, []sessiondomain.PromptMention{{Path: "src/main.go"}}) {
		t.Fatalf("AppendPrompt() mentions = %#v", sessions.gotAppend.Mentions)
	}
}

func TestMutationUpdatePromptAppendForwardsTargetAndReturnsDTO(t *testing.T) {
	sessions := &fakeSessionUseCase{
		updateAppendResult: sessionapp.PromptAppendDTO{
			ID:        "append-1",
			SessionID: "session-1",
			Body:      "after",
			CreatedAt: time.Unix(34, 0).UTC(),
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	got, err := resolver.UpdatePromptAppend(context.Background(), model.UpdatePromptAppendInput{
		SessionID:      "session-1",
		PromptAppendID: "append-1",
		Body:           "after",
	})
	if err != nil {
		t.Fatalf("UpdatePromptAppend() error = %v", err)
	}
	if sessions.gotUpdateAppend.SessionID != "session-1" || sessions.gotUpdateAppend.PromptAppendID != "append-1" || sessions.gotUpdateAppend.Body != "after" {
		t.Fatalf("UpdatePromptAppend() input = %#v", sessions.gotUpdateAppend)
	}
	if got.ID != "append-1" || got.Body != "after" {
		t.Fatalf("UpdatePromptAppend() = %#v", got)
	}
}

func TestMutationUpdatePromptAppendPresentsStartedErrorExtensions(t *testing.T) {
	sessions := &fakeSessionUseCase{err: apperror.New(
		apperror.CodePromptEditAfterStart,
		apperror.CategoryValidationError,
		"流程已开始运行，无法编辑追加提示",
	).WithDetails(map[string]any{
		"sessionId":      "session-1",
		"promptAppendId": "append-1",
	}).WithRetryable(false).WithUserAction("review_session")}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	_, err := resolver.UpdatePromptAppend(context.Background(), model.UpdatePromptAppendInput{
		SessionID:      "session-1",
		PromptAppendID: "append-1",
		Body:           "after",
	})
	if err == nil {
		t.Fatal("UpdatePromptAppend() expected error")
	}
	presented := ErrorPresenter(context.Background(), err)
	if presented.Message != "流程已开始运行，无法编辑追加提示" {
		t.Fatalf("message = %q", presented.Message)
	}
	if presented.Extensions["code"] != apperror.CodePromptEditAfterStart || presented.Extensions["category"] != string(apperror.CategoryValidationError) {
		t.Fatalf("extensions = %#v", presented.Extensions)
	}
	if presented.Extensions["retryable"] != false || presented.Extensions["userAction"] != "review_session" {
		t.Fatalf("extensions = %#v", presented.Extensions)
	}
}

func TestSubmitQuestionRequestNotifiesSessionUseCase(t *testing.T) {
	optionID := "retry_merge"
	sessions := &fakeSessionUseCase{
		submitQuestionResult: questionapp.RequestDTO{
			ID:        "request-1",
			SessionID: "session-1",
			Status:    questiondomain.RequestAnswered,
			Questions: []questiondomain.Question{
				{
					ID:   "question-1",
					Type: "merge_failure_action",
					SelectedOptionID: func() *questiondomain.OptionID {
						id := questiondomain.OptionID(optionID)
						return &id
					}(),
					Answer: map[string]any{"action": "retry_merge"},
				},
			},
		},
	}
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()

	got, err := resolver.SubmitQuestionRequest(context.Background(), model.SubmitQuestionRequestInput{
		RequestID: "request-1",
		Answers: []*model.QuestionAnswerInput{
			{
				QuestionID:       "question-1",
				SelectedOptionID: &optionID,
				Payload:          map[string]any{"action": "retry_merge"},
			},
		},
	})
	if err != nil {
		t.Fatalf("SubmitQuestionRequest() error = %v", err)
	}
	if got.ID != "request-1" || got.Status != string(questiondomain.RequestAnswered) {
		t.Fatalf("SubmitQuestionRequest() = %#v", got)
	}
	if sessions.gotSubmitQuestion.RequestID != "request-1" || len(sessions.gotSubmitQuestion.Answers) != 1 {
		t.Fatalf("question submit input = %#v", sessions.gotSubmitQuestion)
	}
}

func TestSubmitQuestionRequestCanBeRetriedWhenSessionHandlingFails(t *testing.T) {
	optionID := "retry_merge"
	sessions := &fakeSessionUseCase{
		submitQuestionResult: questionapp.RequestDTO{
			ID:        "request-1",
			SessionID: "session-1",
			Status:    questiondomain.RequestAnswered,
			Questions: []questiondomain.Question{
				{
					ID:   "question-1",
					Type: "merge_failure_action",
					SelectedOptionID: func() *questiondomain.OptionID {
						id := questiondomain.OptionID(optionID)
						return &id
					}(),
					Answer: map[string]any{"action": "retry_merge"},
				},
			},
		},
	}
	sessions.err = errors.New("merge retry failed")
	resolver := NewResolver(UseCases{Sessions: sessions}).Mutation()
	input := model.SubmitQuestionRequestInput{
		RequestID: "request-1",
		Answers: []*model.QuestionAnswerInput{
			{
				QuestionID:       "question-1",
				SelectedOptionID: &optionID,
				Payload:          map[string]any{"action": "retry_merge"},
			},
		},
	}

	if _, err := resolver.SubmitQuestionRequest(context.Background(), input); err == nil {
		t.Fatal("first SubmitQuestionRequest() expected session handling error")
	}
	sessions.err = nil
	got, err := resolver.SubmitQuestionRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("second SubmitQuestionRequest() error = %v", err)
	}
	if got.ID != "request-1" || sessions.submitQuestionCalls != 2 {
		t.Fatalf("retry result=%#v sessionCalls=%d", got, sessions.submitQuestionCalls)
	}
}

func TestErrorPresenterAddsApplicationErrorExtensions(t *testing.T) {
	err := apperror.Wrap(errors.New("git diff failed at /home/nzlov/workspaces/github/anycode token=secret"), apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "").
		WithDetails(map[string]any{"sessionId": "session-1", "worktreePath": "/home/nzlov/workspaces/github/anycode", "accessKey": "secret"}).
		WithRetryable(true).
		WithUserAction("retry")

	got := ErrorPresenter(context.Background(), err)

	if got.Message != "git diff failed at /home/nzlov/workspaces/github/anycode token=secret" {
		t.Fatalf("message = %q", got.Message)
	}
	if got.Extensions["code"] != apperror.CodeDiffUnavailable || got.Extensions["category"] != string(apperror.CategoryInfraError) {
		t.Fatalf("extensions = %#v", got.Extensions)
	}
	if got.Extensions["retryable"] != true || got.Extensions["userAction"] != "retry" {
		t.Fatalf("extensions = %#v", got.Extensions)
	}
	details, ok := got.Extensions["details"].(map[string]any)
	if !ok || details["sessionId"] != "session-1" || details["worktreePath"] != "/home/nzlov/workspaces/github/anycode" || details["accessKey"] != "secret" {
		t.Fatalf("details = %#v", got.Extensions["details"])
	}
}

func TestDiffFieldSelectedReadsGraphQLSelection(t *testing.T) {
	ctx := graphql.WithFieldContext(context.Background(), &graphql.FieldContext{
		Field: graphql.CollectedField{
			Field: &ast.Field{Name: "sessionDiff"},
			Selections: ast.SelectionSet{
				&ast.Field{Name: "mode"},
				&ast.Field{Name: "files"},
			},
		},
	})

	if diffFieldSelected(ctx, "fileDiff") {
		t.Fatal("fileDiff selected = true")
	}
	if diffFieldSelected(ctx, "allDiff") {
		t.Fatal("allDiff selected = true")
	}
	if !diffFieldSelected(context.Background(), "fileDiff") {
		t.Fatal("missing field context should preserve conservative include behavior")
	}
}

func TestDiffFieldSelectedReadsNamedFragments(t *testing.T) {
	ctx := graphql.WithOperationContext(context.Background(), &graphql.OperationContext{
		Doc: &ast.QueryDocument{
			Fragments: ast.FragmentDefinitionList{
				&ast.FragmentDefinition{
					Name: "DiffFields",
					SelectionSet: ast.SelectionSet{
						&ast.Field{Name: "fileDiff"},
					},
				},
			},
		},
	})
	ctx = graphql.WithFieldContext(ctx, &graphql.FieldContext{
		Field: graphql.CollectedField{
			Field: &ast.Field{Name: "sessionDiff"},
			Selections: ast.SelectionSet{
				&ast.Field{Name: "mode"},
				&ast.FragmentSpread{Name: "DiffFields"},
			},
		},
	})

	if !diffFieldSelected(ctx, "fileDiff") {
		t.Fatal("fileDiff selected through named fragment = false")
	}
	if diffFieldSelected(ctx, "allDiff") {
		t.Fatal("allDiff selected through named fragment = true")
	}
}

type fakeDiffUseCase struct {
	diffapp.UseCase
}

type fakeTimelineUseCase struct {
	timelineapp.UseCase
	sessionEvents             <-chan timelineapp.DTO
	gotListSessionEventsInput timelineapp.ListSessionEventsInput
	gotSessionEventsInput     timelineapp.SessionEventsInput
}

type fakeSessionEventUseCase struct {
	events    <-chan timelineapp.DTO
	updates   <-chan sessioneventapp.UpdateDTO
	sessionID sessiondomain.ID
}

type fakeWorkflowUseCase struct {
	workflowapp.UseCase
	gotGetID  workflowdomain.DefinitionID
	getResult workflowapp.DefinitionDTO
}

type fakeProjectUseCase struct {
	projectapp.UseCase
	createInput          projectapp.CreateProjectInput
	createResult         projectapp.DTO
	removeInput          projectapp.RemoveProjectInput
	removeCalls          int
	listResult           []projectapp.DTO
	listCalls            int
	gitStateInput        projectapp.ProjectGitStateInput
	gitStateResult       projectdomain.GitState
	browseInput          projectapp.BrowseDirectoryInput
	browseResult         projectapp.DirectoryPageDTO
	updateSettingsInput  projectapp.UpdateProjectSettingsInput
	updateSettingsResult projectapp.DTO
}

func (f *fakeProjectUseCase) UpdateProjectSettings(_ context.Context, input projectapp.UpdateProjectSettingsInput) (projectapp.DTO, error) {
	f.updateSettingsInput = input
	return f.updateSettingsResult, nil
}

type fakeSettingUseCase struct {
	settingapp.UseCase
	appearanceInput  settingapp.UpdateAppearanceSettingsInput
	appearanceResult settingapp.AppearanceSettingsDTO
	listInput        settingapp.ListQuickCommandsInput
	listResult       port.Page[settingapp.QuickCommandDTO]
	createInput      settingapp.CreateQuickCommandInput
	createResult     settingapp.QuickCommandDTO
	deleteInput      settingapp.DeleteQuickCommandInput
}

type fakeNotificationUseCase struct {
	notificationapp.UseCase
	config           notificationapp.ConfigDTO
	registration     notificationapp.SubscriptionDTO
	updateProxyInput notificationapp.UpdateProxyInput
	registerInput    notificationapp.RegisterSubscriptionInput
	unregisterInput  notificationapp.UnregisterSubscriptionInput
}

func (f *fakeNotificationUseCase) GetConfig(context.Context) (notificationapp.ConfigDTO, error) {
	return f.config, nil
}

func (f *fakeNotificationUseCase) UpdateProxy(_ context.Context, input notificationapp.UpdateProxyInput) (notificationapp.ConfigDTO, error) {
	f.updateProxyInput = input
	return f.config, nil
}

func (f *fakeNotificationUseCase) RegisterSubscription(_ context.Context, input notificationapp.RegisterSubscriptionInput) (notificationapp.SubscriptionDTO, error) {
	f.registerInput = input
	return f.registration, nil
}

func (f *fakeNotificationUseCase) UnregisterSubscription(_ context.Context, input notificationapp.UnregisterSubscriptionInput) error {
	f.unregisterInput = input
	return nil
}

func (f *fakeSettingUseCase) GetAppearanceSettings(context.Context) (settingapp.AppearanceSettingsDTO, error) {
	return f.appearanceResult, nil
}

func (f *fakeSettingUseCase) UpdateAppearanceSettings(_ context.Context, input settingapp.UpdateAppearanceSettingsInput) (settingapp.AppearanceSettingsDTO, error) {
	f.appearanceInput = input
	return f.appearanceResult, nil
}

func (f *fakeSettingUseCase) ListQuickCommands(_ context.Context, input settingapp.ListQuickCommandsInput) (port.Page[settingapp.QuickCommandDTO], error) {
	f.listInput = input
	return f.listResult, nil
}

func (f *fakeSettingUseCase) CreateQuickCommand(_ context.Context, input settingapp.CreateQuickCommandInput) (settingapp.QuickCommandDTO, error) {
	f.createInput = input
	return f.createResult, nil
}

func (f *fakeSettingUseCase) DeleteQuickCommand(_ context.Context, input settingapp.DeleteQuickCommandInput) error {
	f.deleteInput = input
	return nil
}

func (f *fakeProjectUseCase) CreateProject(_ context.Context, input projectapp.CreateProjectInput) (projectapp.DTO, error) {
	f.createInput = input
	return f.createResult, nil
}

func (f *fakeProjectUseCase) RemoveProject(_ context.Context, input projectapp.RemoveProjectInput) error {
	f.removeInput = input
	f.removeCalls++
	return nil
}

func (f *fakeProjectUseCase) ListProjects(context.Context) ([]projectapp.DTO, error) {
	f.listCalls++
	return f.listResult, nil
}

func (f *fakeProjectUseCase) ProjectGitState(_ context.Context, input projectapp.ProjectGitStateInput) (projectdomain.GitState, error) {
	f.gitStateInput = input
	return f.gitStateResult, nil
}

func (f *fakeProjectUseCase) BrowseDirectory(_ context.Context, input projectapp.BrowseDirectoryInput) (projectapp.DirectoryPageDTO, error) {
	f.browseInput = input
	return f.browseResult, nil
}

func (f *fakeTimelineUseCase) ListSessionEvents(_ context.Context, input timelineapp.ListSessionEventsInput) (timelineapp.Page, error) {
	f.gotListSessionEventsInput = input
	return timelineapp.Page{}, nil
}

func (f *fakeTimelineUseCase) SessionEvents(_ context.Context, input timelineapp.SessionEventsInput) (<-chan timelineapp.DTO, error) {
	f.gotSessionEventsInput = input
	return f.sessionEvents, nil
}

func (f *fakeSessionEventUseCase) SessionEvents(_ context.Context, sessionID sessiondomain.ID) (<-chan timelineapp.DTO, error) {
	f.sessionID = sessionID
	return f.events, nil
}

func (f *fakeSessionEventUseCase) SessionUpdates(context.Context) (<-chan sessioneventapp.UpdateDTO, error) {
	return f.updates, nil
}

func (f *fakeWorkflowUseCase) GetDefinition(_ context.Context, id workflowdomain.DefinitionID) (workflowapp.DefinitionDTO, error) {
	f.gotGetID = id
	return f.getResult, nil
}

type fakeQuestionUseCase struct {
	questionapp.UseCase
	pending             []questionapp.RequestDTO
	updateSource        <-chan questionapp.RequestDTO
	submitResult        questionapp.RequestDTO
	gotSubmit           questionapp.SubmitRequestInput
	submitCalls         int
	gotPendingSessionID questiondomain.SessionID
	gotUpdateSessionID  questiondomain.SessionID
}

func (f *fakeQuestionUseCase) SubmitBatch(_ context.Context, input questionapp.SubmitRequestInput) (questionapp.RequestDTO, error) {
	f.gotSubmit = input
	f.submitCalls++
	return f.submitResult, nil
}

func (f *fakeQuestionUseCase) ListPendingRequestsBySession(_ context.Context, sessionID questiondomain.SessionID) ([]questionapp.RequestDTO, error) {
	f.gotPendingSessionID = sessionID
	return f.pending, nil
}

func (f *fakeQuestionUseCase) QuestionRequestUpdates(_ context.Context, sessionID questiondomain.SessionID) (<-chan questionapp.RequestDTO, error) {
	f.gotUpdateSessionID = sessionID
	return f.updateSource, nil
}

type fakeSessionUseCase struct {
	sessionapp.UseCase
	gotAnswered               questionapp.RequestDTO
	answeredCalls             int
	gotSubmitQuestion         questionapp.SubmitRequestInput
	submitQuestionResult      questionapp.RequestDTO
	submitQuestionCalls       int
	err                       error
	gotGetCardID              sessiondomain.ID
	getCardResult             sessionapp.CardDTO
	gotGetID                  sessiondomain.ID
	getResult                 sessionapp.DetailDTO
	gotCreate                 sessionapp.CreateSessionInput
	createResult              sessionapp.DTO
	gotResumeID               sessiondomain.ID
	resumeResult              sessionapp.DTO
	gotExecuteID              sessiondomain.ID
	gotExecuteForce           bool
	executeResult             sessionapp.DTO
	stopProjectID             sessiondomain.ProjectID
	gotUpdateConfig           sessionapp.UpdateSessionConfigInput
	updateConfigResult        sessionapp.DTO
	gotAppend                 sessionapp.AppendPromptInput
	appendResult              sessionapp.PromptAppendDTO
	gotUpdateAppend           sessionapp.UpdatePromptAppendInput
	updateAppendResult        sessionapp.PromptAppendDTO
	gotRetryCleanupID         sessiondomain.ID
	retryCleanupResult        sessionapp.DTO
	gotRetryInitializationID  sessiondomain.ID
	retryInitializationResult sessionapp.DTO
	gotDeleteSessionFileID    sessiondomain.SessionFileID
	gotCleanup                sessionapp.CleanupSessionsInput
	cleanupResult             int
}

func (f *fakeSessionUseCase) DeleteSessionFile(_ context.Context, id sessiondomain.SessionFileID) error {
	f.gotDeleteSessionFileID = id
	return f.err
}

func (f *fakeSessionUseCase) CleanupSessions(_ context.Context, input sessionapp.CleanupSessionsInput) (int, error) {
	f.gotCleanup = input
	return f.cleanupResult, f.err
}

func (f *fakeSessionUseCase) CreateSession(_ context.Context, input sessionapp.CreateSessionInput) (sessionapp.DTO, error) {
	f.gotCreate = input
	return f.createResult, f.err
}

func (f *fakeSessionUseCase) SubmitQuestionRequest(_ context.Context, input questionapp.SubmitRequestInput) (questionapp.RequestDTO, error) {
	f.gotSubmitQuestion = input
	f.submitQuestionCalls++
	return f.submitQuestionResult, f.err
}

func (f *fakeSessionUseCase) ExecuteSession(_ context.Context, id sessiondomain.ID) (sessionapp.DTO, error) {
	f.gotExecuteID = id
	return f.executeResult, f.err
}

func (f *fakeSessionUseCase) ExecuteSessionWithOptions(_ context.Context, id sessiondomain.ID, options sessionapp.StartSessionOptions) (sessionapp.DTO, error) {
	f.gotExecuteID = id
	f.gotExecuteForce = options.Force
	return f.executeResult, f.err
}

func (f *fakeSessionUseCase) GetSession(_ context.Context, id sessiondomain.ID) (sessionapp.DetailDTO, error) {
	f.gotGetID = id
	return f.getResult, f.err
}

func (f *fakeSessionUseCase) HandleQuestionRequestAnswered(_ context.Context, request questionapp.RequestDTO) error {
	f.gotAnswered = request
	f.answeredCalls++
	return f.err
}

func (f *fakeSessionUseCase) GetSessionCard(_ context.Context, id sessiondomain.ID) (sessionapp.CardDTO, error) {
	f.gotGetCardID = id
	return f.getCardResult, f.err
}

func (f *fakeSessionUseCase) ResumeSession(_ context.Context, id sessiondomain.ID) (sessionapp.DTO, error) {
	f.gotResumeID = id
	return f.resumeResult, f.err
}

func (f *fakeSessionUseCase) ResumeSessionWithOptions(_ context.Context, id sessiondomain.ID, _ sessionapp.StartSessionOptions) (sessionapp.DTO, error) {
	f.gotResumeID = id
	return f.resumeResult, f.err
}

func (f *fakeSessionUseCase) StopProjectSessions(_ context.Context, projectID sessiondomain.ProjectID) (int, error) {
	f.stopProjectID = projectID
	return 1, f.err
}

func (f *fakeSessionUseCase) AppendPrompt(_ context.Context, input sessionapp.AppendPromptInput) (sessionapp.PromptAppendDTO, error) {
	f.gotAppend = input
	return f.appendResult, f.err
}

func (f *fakeSessionUseCase) UpdatePromptAppend(_ context.Context, input sessionapp.UpdatePromptAppendInput) (sessionapp.PromptAppendDTO, error) {
	f.gotUpdateAppend = input
	return f.updateAppendResult, f.err
}

func (f *fakeSessionUseCase) UpdateSessionConfig(_ context.Context, input sessionapp.UpdateSessionConfigInput) (sessionapp.DTO, error) {
	f.gotUpdateConfig = input
	return f.updateConfigResult, f.err
}

func (f *fakeSessionUseCase) RetryWorktreeCleanup(_ context.Context, id sessiondomain.ID) (sessionapp.DTO, error) {
	f.gotRetryCleanupID = id
	return f.retryCleanupResult, f.err
}

func (f *fakeSessionUseCase) RetrySessionInitialization(_ context.Context, id sessiondomain.ID) (sessionapp.DTO, error) {
	f.gotRetryInitializationID = id
	return f.retryInitializationResult, f.err
}

func strPtr(value string) *string {
	return &value
}
