package graph

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/nzlov/anycode/internal/application/apperror"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	"github.com/nzlov/anycode/internal/application/port"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
	"github.com/vektah/gqlparser/v2/ast"
)

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

func TestSubscriptionSessionEventsForwardsUseCaseEvents(t *testing.T) {
	sessionID := "session-1"
	projectID := "project-1"
	eventSessionID := eventdomain.SessionID(sessionID)
	source := make(chan timelineapp.DTO, 1)
	source <- timelineapp.DTO{
		ID:        "event-1",
		Scope:     eventdomain.Scope{SessionID: &eventSessionID, ProjectID: projectID},
		SessionID: &eventSessionID,
		Type:      "process.output",
		Payload:   map[string]any{"text": "hello"},
		CreatedAt: "2026-07-02T01:02:03Z",
	}
	close(source)

	timeline := &fakeTimelineUseCase{sessionEvents: source}
	resolver := NewResolver(UseCases{Timeline: timeline}).Subscription()
	ch, err := resolver.SessionEvents(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}

	wantInput := timelineapp.SessionEventsInput{
		Scope: eventdomain.Scope{SessionID: &eventSessionID},
	}
	if !reflect.DeepEqual(timeline.gotSessionEventsInput, wantInput) {
		t.Fatalf("SessionEvents() input = %#v, want %#v", timeline.gotSessionEventsInput, wantInput)
	}

	got, ok := <-ch
	if !ok {
		t.Fatal("SessionEvents() channel closed before ready item")
	}
	if !got.Ready || got.Event != nil {
		t.Fatalf("SessionEvents() ready item = %#v", got)
	}
	got, ok = <-ch
	if !ok || got.Event == nil {
		t.Fatal("SessionEvents() channel closed before event item")
	}
	event := got.Event
	if got.Ready || event.ID != "event-1" || event.Type != "process.output" || event.CreatedAt != "2026-07-02T01:02:03Z" {
		t.Fatalf("SessionEvents() event = %#v", got)
	}
	if event.Scope == nil || event.Scope.SessionID == nil || *event.Scope.SessionID != sessionID || event.Scope.ProjectID != projectID {
		t.Fatalf("SessionEvents() scope = %#v", event.Scope)
	}
	if event.SessionID == nil || *event.SessionID != sessionID {
		t.Fatalf("SessionEvents() sessionID = %#v", event.SessionID)
	}
	if event.Payload["text"] != "hello" {
		t.Fatalf("SessionEvents() payload = %#v", event.Payload)
	}
	if _, ok := <-ch; ok {
		t.Fatal("SessionEvents() channel stayed open after source closed")
	}
}

func TestSubscriptionSessionCardChangedUsesLiveEventsWithoutHistoryReplay(t *testing.T) {
	sessionID := "session-1"
	projectID := "project-1"
	otherProjectID := "project-2"
	eventSessionID := eventdomain.SessionID(sessionID)
	source := make(chan eventapp.DTO, 3)
	source <- eventapp.DTO{
		ID:        "ignored-process",
		Scope:     eventdomain.Scope{SessionID: &eventSessionID, ProjectID: projectID},
		SessionID: &eventSessionID,
		Type:      "process.codex_event",
		CreatedAt: "2026-07-02T01:02:03Z",
	}
	source <- eventapp.DTO{
		ID:        "ignored-project",
		Scope:     eventdomain.Scope{SessionID: &eventSessionID, ProjectID: otherProjectID},
		SessionID: &eventSessionID,
		Type:      "session.running",
		CreatedAt: "2026-07-02T01:02:04Z",
	}
	source <- eventapp.DTO{
		ID:        "card-change",
		Scope:     eventdomain.Scope{SessionID: &eventSessionID, ProjectID: projectID},
		SessionID: &eventSessionID,
		Type:      "session.priority_changed",
		CreatedAt: "2026-07-02T01:02:05Z",
	}
	close(source)

	events := &fakeEventUseCase{liveSessionEvents: source}
	sessions := &fakeSessionUseCase{
		getCardResult: sessionapp.CardDTO{
			DTO: sessionapp.DTO{
				ID:        sessiondomain.ID(sessionID),
				ProjectID: sessiondomain.ProjectID(projectID),
				Status:    sessiondomain.StatusRunning,
				Priority:  sessiondomain.PriorityHigh,
			},
			ProjectName: "AnyCode",
		},
	}
	resolver := NewResolver(UseCases{Events: events, Sessions: sessions}).Subscription()
	ch, err := resolver.SessionCardChanged(context.Background(), &projectID)
	if err != nil {
		t.Fatalf("SessionCardChanged() error = %v", err)
	}

	wantInput := eventapp.LiveSessionEventsInput{
		Scope: eventdomain.Scope{ProjectID: projectID},
	}
	if !reflect.DeepEqual(events.gotLiveSessionEventsInput, wantInput) {
		t.Fatalf("LiveSessionEvents() input = %#v, want %#v", events.gotLiveSessionEventsInput, wantInput)
	}

	got, ok := <-ch
	if !ok {
		t.Fatal("SessionCardChanged() channel closed before card")
	}
	if got.ID != sessionID || got.ProjectID != projectID || got.ProjectName != "AnyCode" {
		t.Fatalf("SessionCardChanged() card = %#v", got)
	}
	if sessions.gotGetCardID != sessiondomain.ID(sessionID) {
		t.Fatalf("GetSessionCard() id = %q, want %q", sessions.gotGetCardID, sessionID)
	}
	if _, ok := <-ch; ok {
		t.Fatal("SessionCardChanged() emitted more than matching card changes")
	}
}

func TestSubscriptionSessionEventsStopsBlockedSendAfterCancel(t *testing.T) {
	sessionID := "session-1"
	eventSessionID := eventdomain.SessionID(sessionID)
	source := make(chan timelineapp.DTO, 1)
	timeline := &fakeTimelineUseCase{sessionEvents: source}
	resolver := NewResolver(UseCases{Timeline: timeline}).Subscription()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := resolver.SessionEvents(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	if ready := <-ch; !ready.Ready {
		t.Fatalf("ready item = %#v", ready)
	}
	source <- timelineapp.DTO{
		ID:        "event-1",
		Scope:     eventdomain.Scope{SessionID: &eventSessionID},
		SessionID: &eventSessionID,
		Type:      "process.codex_event",
		CreatedAt: "2026-07-02T01:02:03Z",
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("SessionEvents() delivered a blocked event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("SessionEvents() did not close after cancellation")
	}
}

func TestSubscriptionSessionCardChangedStopsBlockedSendAfterCancel(t *testing.T) {
	sessionID := "session-1"
	eventSessionID := eventdomain.SessionID(sessionID)
	source := make(chan eventapp.DTO, 1)
	events := &fakeEventUseCase{liveSessionEvents: source}
	sessions := &fakeSessionUseCase{getCardResult: sessionapp.CardDTO{DTO: sessionapp.DTO{
		ID:        sessiondomain.ID(sessionID),
		ProjectID: "project-1",
	}}}
	resolver := NewResolver(UseCases{Events: events, Sessions: sessions}).Subscription()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := resolver.SessionCardChanged(ctx, nil)
	if err != nil {
		t.Fatalf("SessionCardChanged() error = %v", err)
	}
	source <- eventapp.DTO{
		ID:        "card-change",
		Scope:     eventdomain.Scope{SessionID: &eventSessionID, ProjectID: "project-1"},
		SessionID: &eventSessionID,
		Type:      "session.running",
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("SessionCardChanged() delivered a blocked card after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("SessionCardChanged() did not close after cancellation")
	}
}

func TestSessionCardChangeEventIncludesCardStateChanges(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	tests := []string{
		"session.running",
		"session.stopped",
		"session.recoverable",
		"session.failed",
		"session.blocked",
		"session.completed",
		"session.closed",
		"session.config_changed",
		"session.priority_changed",
		"session.todo_list_updated",
		"workflow.blocked",
	}
	for _, eventType := range tests {
		t.Run(eventType, func(t *testing.T) {
			eventDTO := eventapp.DTO{
				Scope:     eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
				SessionID: &sessionID,
				Type:      eventType,
			}
			if !sessionCardChangeEvent(eventDTO, nil) {
				t.Fatalf("sessionCardChangeEvent(%q) = false, want true", eventType)
			}
		})
	}
}

func TestQuerySessionEventsForwardsBeforeCursorAndLimit(t *testing.T) {
	beforeEventID := "event-40"
	limit := 50
	timeline := &fakeTimelineUseCase{}
	resolver := NewResolver(UseCases{Timeline: timeline}).Query()

	_, err := resolver.SessionEvents(context.Background(), model.ListSessionEventsInput{
		SessionID:     "session-1",
		BeforeEventID: &beforeEventID,
		Limit:         &limit,
	})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}

	want := timelineapp.ListSessionEventsInput{
		SessionID:     "session-1",
		BeforeEventID: eventdomain.ID(beforeEventID),
		Limit:         limit,
	}
	if !reflect.DeepEqual(timeline.gotListSessionEventsInput, want) {
		t.Fatalf("ListSessionEvents() input = %#v, want %#v", timeline.gotListSessionEventsInput, want)
	}
}

func TestQueryPendingQuestionBatchesForwardsUseCase(t *testing.T) {
	questions := &fakeQuestionUseCase{
		pending: []questionapp.BatchDTO{
			{
				ID:        "batch-1",
				SessionID: "session-1",
				Status:    questiondomain.BatchPending,
				Questions: []questiondomain.Question{
					{
						ID:      "question-1",
						Title:   "Choose",
						Options: []questiondomain.Option{{ID: "option-1", Label: "Continue"}},
					},
				},
			},
		},
	}
	resolver := NewResolver(UseCases{Questions: questions}).Query()

	got, err := resolver.PendingQuestionBatches(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("PendingQuestionBatches() error = %v", err)
	}
	if questions.gotPendingSessionID != "session-1" {
		t.Fatalf("pending session id = %q", questions.gotPendingSessionID)
	}
	if len(got) != 1 || got[0].ID != "batch-1" || len(got[0].Questions) != 1 {
		t.Fatalf("pending batches = %#v", got)
	}
	if got[0].Questions[0].Answer == nil || got[0].Questions[0].Options[0].Payload == nil {
		t.Fatalf("pending question JSON maps should be non-nil: %#v", got[0].Questions[0])
	}
}

func TestMutationResumeSessionForwardsUseCase(t *testing.T) {
	now := time.Unix(30, 0).UTC()
	sessions := &fakeSessionUseCase{
		resumeResult: sessionapp.DTO{
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
	if sessions.gotResumeID != "session-1" {
		t.Fatalf("ResumeSession() id = %q", sessions.gotResumeID)
	}
	if got.ID != "session-1" || got.Status != "running" || got.CodexSessionID != "codex-session-1" {
		t.Fatalf("ResumeSession() = %#v", got)
	}
}

func TestMutationUpdateSessionConfigForwardsUseCase(t *testing.T) {
	now := time.Unix(32, 0).UTC()
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
		},
	})
	if err != nil {
		t.Fatalf("UpdateSessionConfig() error = %v", err)
	}
	if sessions.gotUpdateConfig.SessionID != "session-1" {
		t.Fatalf("UpdateSessionConfig() input = %#v", sessions.gotUpdateConfig)
	}
	if sessions.gotUpdateConfig.Config.CodexModel != "gpt-5.4-mini" || sessions.gotUpdateConfig.Config.ReasoningEffort != "high" || sessions.gotUpdateConfig.Config.PermissionMode != "workspace-write" {
		t.Fatalf("UpdateSessionConfig() config = %#v", sessions.gotUpdateConfig.Config)
	}
	if got.ID != "session-1" || got.Config.CodexModel != "gpt-5.4-mini" {
		t.Fatalf("UpdateSessionConfig() = %#v", got)
	}
}

func TestMutationAppendPromptForwardsStagedAttachmentIDs(t *testing.T) {
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
}

func TestSubscriptionSessionStateUpdatesRegistersBothSourcesBeforeReady(t *testing.T) {
	sessionID := "session-1"
	eventSessionID := eventdomain.SessionID(sessionID)
	eventSource := make(chan eventapp.DTO, 1)
	questionSource := make(chan questionapp.BatchDTO, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := &fakeEventUseCase{liveSessionEvents: eventSource}
	questions := &fakeQuestionUseCase{updateSource: questionSource}
	sessions := &fakeSessionUseCase{getResult: sessionapp.DetailDTO{DTO: sessionapp.DTO{
		ID:        sessiondomain.ID(sessionID),
		ProjectID: "project-1",
		Status:    sessiondomain.StatusRunning,
	}}}
	resolver := NewResolver(UseCases{Events: events, Questions: questions, Sessions: sessions}).Subscription()

	ch, err := resolver.SessionStateUpdates(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionStateUpdates() error = %v", err)
	}
	wantScope := eventdomain.Scope{SessionID: &eventSessionID}
	if !reflect.DeepEqual(events.gotLiveSessionEventsInput.Scope, wantScope) {
		t.Fatalf("event subscription scope = %#v, want %#v", events.gotLiveSessionEventsInput.Scope, wantScope)
	}
	if questions.gotUpdateSessionID != questiondomain.SessionID(sessionID) {
		t.Fatalf("question subscription session id = %q", questions.gotUpdateSessionID)
	}
	ready := <-ch
	if !ready.Ready || ready.Session != nil || ready.QuestionBatch != nil {
		t.Fatalf("ready item = %#v", ready)
	}

	eventSource <- eventapp.DTO{
		ID:        "state-1",
		Scope:     wantScope,
		SessionID: &eventSessionID,
		Type:      "session.running",
	}
	stateUpdate := <-ch
	if stateUpdate.Ready || stateUpdate.Session == nil || stateUpdate.Session.ID != sessionID || stateUpdate.QuestionBatch != nil {
		t.Fatalf("session state item = %#v", stateUpdate)
	}

	questionSource <- questionapp.BatchDTO{
		ID:        "batch-1",
		SessionID: questiondomain.SessionID(sessionID),
		Status:    questiondomain.BatchPending,
	}
	questionUpdate := <-ch
	if questionUpdate.Ready || questionUpdate.Session == nil || questionUpdate.QuestionBatch == nil {
		t.Fatalf("question state item = %#v", questionUpdate)
	}
	if questionUpdate.QuestionBatch.ID != "batch-1" || questionUpdate.Session.ID != sessionID {
		t.Fatalf("question state item = %#v", questionUpdate)
	}
}

func TestSubscriptionSessionStateUpdatesStopsBlockedSendAfterCancel(t *testing.T) {
	sessionID := "session-1"
	eventSessionID := eventdomain.SessionID(sessionID)
	eventSource := make(chan eventapp.DTO, 1)
	questionSource := make(chan questionapp.BatchDTO)
	events := &fakeEventUseCase{liveSessionEvents: eventSource}
	questions := &fakeQuestionUseCase{updateSource: questionSource}
	sessions := &fakeSessionUseCase{getResult: sessionapp.DetailDTO{DTO: sessionapp.DTO{ID: sessiondomain.ID(sessionID)}}}
	resolver := NewResolver(UseCases{Events: events, Questions: questions, Sessions: sessions}).Subscription()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := resolver.SessionStateUpdates(ctx, sessionID)
	if err != nil {
		t.Fatalf("SessionStateUpdates() error = %v", err)
	}
	if ready := <-ch; !ready.Ready {
		t.Fatalf("ready item = %#v", ready)
	}
	eventSource <- eventapp.DTO{
		ID:        "state-1",
		Scope:     eventdomain.Scope{SessionID: &eventSessionID},
		SessionID: &eventSessionID,
		Type:      "session.running",
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("SessionStateUpdates() delivered a blocked item after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("SessionStateUpdates() did not close after cancellation")
	}
}

func TestSubmitQuestionBatchNotifiesSessionUseCase(t *testing.T) {
	optionID := "retry_merge"
	questions := &fakeQuestionUseCase{
		submitResult: questionapp.BatchDTO{
			ID:        "batch-1",
			SessionID: "session-1",
			Status:    questiondomain.BatchAnswered,
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
	sessions := &fakeSessionUseCase{}
	resolver := NewResolver(UseCases{Questions: questions, Sessions: sessions}).Mutation()

	got, err := resolver.SubmitQuestionBatch(context.Background(), model.SubmitQuestionBatchInput{
		BatchID: "batch-1",
		Answers: []*model.QuestionAnswerInput{
			{
				QuestionID:       "question-1",
				SelectedOptionID: &optionID,
				Payload:          map[string]any{"action": "retry_merge"},
			},
		},
	})
	if err != nil {
		t.Fatalf("SubmitQuestionBatch() error = %v", err)
	}
	if got.ID != "batch-1" || got.Status != string(questiondomain.BatchAnswered) {
		t.Fatalf("SubmitQuestionBatch() = %#v", got)
	}
	if questions.gotSubmit.BatchID != "batch-1" || len(questions.gotSubmit.Answers) != 1 {
		t.Fatalf("question submit input = %#v", questions.gotSubmit)
	}
	if sessions.gotAnswered.ID != "batch-1" || sessions.gotAnswered.Status != questiondomain.BatchAnswered {
		t.Fatalf("session answered batch = %#v", sessions.gotAnswered)
	}
}

func TestSubmitQuestionBatchCanBeRetriedWhenSessionHandlingFails(t *testing.T) {
	optionID := "retry_merge"
	questions := &fakeQuestionUseCase{
		submitResult: questionapp.BatchDTO{
			ID:        "batch-1",
			SessionID: "session-1",
			Status:    questiondomain.BatchAnswered,
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
	sessions := &fakeSessionUseCase{err: errors.New("merge retry failed")}
	resolver := NewResolver(UseCases{Questions: questions, Sessions: sessions}).Mutation()
	input := model.SubmitQuestionBatchInput{
		BatchID: "batch-1",
		Answers: []*model.QuestionAnswerInput{
			{
				QuestionID:       "question-1",
				SelectedOptionID: &optionID,
				Payload:          map[string]any{"action": "retry_merge"},
			},
		},
	}

	if _, err := resolver.SubmitQuestionBatch(context.Background(), input); err == nil {
		t.Fatal("first SubmitQuestionBatch() expected session handling error")
	}
	sessions.err = nil
	got, err := resolver.SubmitQuestionBatch(context.Background(), input)
	if err != nil {
		t.Fatalf("second SubmitQuestionBatch() error = %v", err)
	}
	if got.ID != "batch-1" || questions.submitCalls != 2 || sessions.answeredCalls != 2 {
		t.Fatalf("retry result=%#v questionCalls=%d sessionCalls=%d", got, questions.submitCalls, sessions.answeredCalls)
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

type fakeEventUseCase struct {
	eventapp.UseCase
	liveSessionEvents         <-chan eventapp.DTO
	gotLiveSessionEventsInput eventapp.LiveSessionEventsInput
}

type fakeTimelineUseCase struct {
	timelineapp.UseCase
	sessionEvents             <-chan timelineapp.DTO
	gotListSessionEventsInput timelineapp.ListSessionEventsInput
	gotSessionEventsInput     timelineapp.SessionEventsInput
}

type fakeWorkflowUseCase struct {
	workflowapp.UseCase
	gotGetID  workflowdomain.DefinitionID
	getResult workflowapp.DefinitionDTO
}

type fakeProjectUseCase struct {
	projectapp.UseCase
	createInput    projectapp.CreateProjectInput
	createResult   projectapp.DTO
	removeInput    projectapp.RemoveProjectInput
	removeCalls    int
	listResult     []projectapp.DTO
	listCalls      int
	gitStateInput  projectapp.ProjectGitStateInput
	gitStateResult projectdomain.GitState
	browseInput    projectapp.BrowseDirectoryInput
	browseResult   projectapp.DirectoryPageDTO
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

func (f *fakeEventUseCase) LiveSessionEvents(_ context.Context, input eventapp.LiveSessionEventsInput) (<-chan eventapp.DTO, error) {
	f.gotLiveSessionEventsInput = input
	return f.liveSessionEvents, nil
}

func (f *fakeTimelineUseCase) ListSessionEvents(_ context.Context, input timelineapp.ListSessionEventsInput) (port.Page[timelineapp.DTO], error) {
	f.gotListSessionEventsInput = input
	return port.Page[timelineapp.DTO]{}, nil
}

func (f *fakeTimelineUseCase) SessionEvents(_ context.Context, input timelineapp.SessionEventsInput) (<-chan timelineapp.DTO, error) {
	f.gotSessionEventsInput = input
	return f.sessionEvents, nil
}

func (f *fakeWorkflowUseCase) GetDefinition(_ context.Context, id workflowdomain.DefinitionID) (workflowapp.DefinitionDTO, error) {
	f.gotGetID = id
	return f.getResult, nil
}

type fakeQuestionUseCase struct {
	questionapp.UseCase
	pending                  []questionapp.BatchDTO
	updateSource             <-chan questionapp.BatchDTO
	submitResult             questionapp.BatchDTO
	gotSubmit                questionapp.SubmitBatchInput
	submitCalls              int
	gotPendingSessionID      questiondomain.SessionID
	gotUpdateSessionID       questiondomain.SessionID
}

func (f *fakeQuestionUseCase) SubmitBatch(_ context.Context, input questionapp.SubmitBatchInput) (questionapp.BatchDTO, error) {
	f.gotSubmit = input
	f.submitCalls++
	return f.submitResult, nil
}

func (f *fakeQuestionUseCase) ListPendingBySession(_ context.Context, sessionID questiondomain.SessionID) ([]questionapp.BatchDTO, error) {
	f.gotPendingSessionID = sessionID
	return f.pending, nil
}

func (f *fakeQuestionUseCase) QuestionBatchUpdates(_ context.Context, sessionID questiondomain.SessionID) (<-chan questionapp.BatchDTO, error) {
	f.gotUpdateSessionID = sessionID
	return f.updateSource, nil
}

type fakeSessionUseCase struct {
	sessionapp.UseCase
	gotAnswered        questionapp.BatchDTO
	answeredCalls      int
	err                error
	gotGetCardID       sessiondomain.ID
	getCardResult      sessionapp.CardDTO
	gotGetID           sessiondomain.ID
	getResult          sessionapp.DetailDTO
	gotResumeID        sessiondomain.ID
	resumeResult       sessionapp.DTO
	stopProjectID      sessiondomain.ProjectID
	gotUpdateConfig    sessionapp.UpdateSessionConfigInput
	updateConfigResult sessionapp.DTO
	gotAppend          sessionapp.AppendPromptInput
	appendResult       sessionapp.PromptAppendDTO
}

func (f *fakeSessionUseCase) GetSession(_ context.Context, id sessiondomain.ID) (sessionapp.DetailDTO, error) {
	f.gotGetID = id
	return f.getResult, f.err
}

func (f *fakeSessionUseCase) HandleQuestionBatchAnswered(_ context.Context, batch questionapp.BatchDTO) error {
	f.gotAnswered = batch
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

func (f *fakeSessionUseCase) UpdateSessionConfig(_ context.Context, input sessionapp.UpdateSessionConfigInput) (sessionapp.DTO, error) {
	f.gotUpdateConfig = input
	return f.updateConfigResult, f.err
}

func strPtr(value string) *string {
	return &value
}
