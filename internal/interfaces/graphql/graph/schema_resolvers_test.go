package graph

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	"github.com/nzlov/anycode/internal/application/port"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

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
	afterEventID := "event-0"
	eventSessionID := eventdomain.SessionID(sessionID)
	source := make(chan eventapp.DTO, 1)
	source <- eventapp.DTO{
		ID:        "event-1",
		Scope:     eventdomain.Scope{SessionID: &eventSessionID, ProjectID: projectID},
		SessionID: &eventSessionID,
		Type:      "process.output",
		Payload:   map[string]any{"text": "hello"},
		CreatedAt: "2026-07-02T01:02:03Z",
	}
	close(source)

	events := &fakeEventUseCase{sessionEvents: source}
	resolver := NewResolver(UseCases{Events: events}).Subscription()
	ch, err := resolver.SessionEvents(context.Background(), model.SessionEventsInput{
		SessionID:    &sessionID,
		ProjectID:    &projectID,
		AfterEventID: &afterEventID,
	})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}

	wantInput := eventapp.SessionEventsInput{
		Scope: eventdomain.Scope{
			SessionID: &eventSessionID,
			ProjectID: projectID,
		},
		AfterEventID: eventdomain.ID(afterEventID),
	}
	if !reflect.DeepEqual(events.gotSessionEventsInput, wantInput) {
		t.Fatalf("SessionEvents() input = %#v, want %#v", events.gotSessionEventsInput, wantInput)
	}

	got, ok := <-ch
	if !ok {
		t.Fatal("SessionEvents() channel closed before event")
	}
	if got.ID != "event-1" || got.Type != "process.output" || got.CreatedAt != "2026-07-02T01:02:03Z" {
		t.Fatalf("SessionEvents() event = %#v", got)
	}
	if got.Scope == nil || got.Scope.SessionID == nil || *got.Scope.SessionID != sessionID || got.Scope.ProjectID != projectID {
		t.Fatalf("SessionEvents() scope = %#v", got.Scope)
	}
	if got.SessionID == nil || *got.SessionID != sessionID {
		t.Fatalf("SessionEvents() sessionID = %#v", got.SessionID)
	}
	if got.Payload["text"] != "hello" {
		t.Fatalf("SessionEvents() payload = %#v", got.Payload)
	}
	if _, ok := <-ch; ok {
		t.Fatal("SessionEvents() channel stayed open after source closed")
	}
}

func TestQuerySessionEventsForwardsBeforeCursorAndLimit(t *testing.T) {
	beforeEventID := "event-40"
	limit := 50
	events := &fakeEventUseCase{}
	resolver := NewResolver(UseCases{Events: events}).Query()

	_, err := resolver.SessionEvents(context.Background(), model.ListSessionEventsInput{
		SessionID:     "session-1",
		BeforeEventID: &beforeEventID,
		Limit:         &limit,
	})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}

	want := eventapp.ListSessionEventsInput{
		SessionID:     "session-1",
		BeforeEventID: eventdomain.ID(beforeEventID),
		Limit:         limit,
	}
	if !reflect.DeepEqual(events.gotListSessionEventsInput, want) {
		t.Fatalf("ListSessionEvents() input = %#v, want %#v", events.gotListSessionEventsInput, want)
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

func TestSubscriptionPendingQuestionBatchesForwardsUseCase(t *testing.T) {
	source := make(chan questionapp.BatchDTO, 1)
	source <- questionapp.BatchDTO{
		ID:        "batch-1",
		SessionID: "session-1",
		Status:    questiondomain.BatchPending,
	}
	close(source)
	questions := &fakeQuestionUseCase{pendingSource: source}
	resolver := NewResolver(UseCases{Questions: questions}).Subscription()

	ch, err := resolver.PendingQuestionBatches(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("PendingQuestionBatches() error = %v", err)
	}
	if questions.gotSubscriptionSessionID != "session-1" {
		t.Fatalf("subscription session id = %q", questions.gotSubscriptionSessionID)
	}
	got, ok := <-ch
	if !ok {
		t.Fatal("PendingQuestionBatches() channel closed before batch")
	}
	if got.ID != "batch-1" || got.Status != string(questiondomain.BatchPending) {
		t.Fatalf("pending batch = %#v", got)
	}
	if _, ok := <-ch; ok {
		t.Fatal("PendingQuestionBatches() channel stayed open after source closed")
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

	if got.Message != "git diff failed at [redacted_path] token=[redacted]" {
		t.Fatalf("message = %q", got.Message)
	}
	if got.Extensions["code"] != apperror.CodeDiffUnavailable || got.Extensions["category"] != string(apperror.CategoryInfraError) {
		t.Fatalf("extensions = %#v", got.Extensions)
	}
	if got.Extensions["retryable"] != true || got.Extensions["userAction"] != "retry" {
		t.Fatalf("extensions = %#v", got.Extensions)
	}
	details, ok := got.Extensions["details"].(map[string]any)
	if !ok || details["sessionId"] != "session-1" || details["worktreePath"] != "[redacted_path]" || details["accessKey"] != "[redacted]" {
		t.Fatalf("details = %#v", got.Extensions["details"])
	}
}

type fakeEventUseCase struct {
	eventapp.UseCase
	sessionEvents             <-chan eventapp.DTO
	gotListSessionEventsInput eventapp.ListSessionEventsInput
	gotSessionEventsInput     eventapp.SessionEventsInput
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

func (f *fakeEventUseCase) ListSessionEvents(_ context.Context, input eventapp.ListSessionEventsInput) (port.Page[eventapp.DTO], error) {
	f.gotListSessionEventsInput = input
	return port.Page[eventapp.DTO]{}, nil
}

func (f *fakeEventUseCase) SessionEvents(_ context.Context, input eventapp.SessionEventsInput) (<-chan eventapp.DTO, error) {
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
	pendingSource            <-chan questionapp.BatchDTO
	submitResult             questionapp.BatchDTO
	gotSubmit                questionapp.SubmitBatchInput
	submitCalls              int
	gotPendingSessionID      questiondomain.SessionID
	gotSubscriptionSessionID questiondomain.SessionID
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

func (f *fakeQuestionUseCase) PendingQuestionBatches(_ context.Context, sessionID questiondomain.SessionID) (<-chan questionapp.BatchDTO, error) {
	f.gotSubscriptionSessionID = sessionID
	return f.pendingSource, nil
}

type fakeSessionUseCase struct {
	sessionapp.UseCase
	gotAnswered        questionapp.BatchDTO
	answeredCalls      int
	err                error
	gotResumeID        sessiondomain.ID
	resumeResult       sessionapp.DTO
	stopProjectID      sessiondomain.ProjectID
	gotUpdateConfig    sessionapp.UpdateSessionConfigInput
	updateConfigResult sessionapp.DTO
	gotAppend          sessionapp.AppendPromptInput
	appendResult       sessionapp.PromptAppendDTO
}

func (f *fakeSessionUseCase) HandleQuestionBatchAnswered(_ context.Context, batch questionapp.BatchDTO) error {
	f.gotAnswered = batch
	f.answeredCalls++
	return f.err
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
