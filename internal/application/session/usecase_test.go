package session

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/application/port"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	gitdiffdomain "github.com/nzlov/anycode/internal/domain/gitdiff"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	domain "github.com/nzlov/anycode/internal/domain/session"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
)

func TestCreateSessionDefaultsModeAndSavesRequestedConfig(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "  implement app session  ",
		Config: domain.Config{
			CodexModel:      "gpt-5.4-mini",
			ReasoningEffort: "medium",
			PermissionMode:  "workspace-write",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.ID != "session-1" || got.ProjectID != "project-1" || got.Requirement != "implement app session" {
		t.Fatalf("CreateSession() DTO = %#v", got)
	}
	if got.Mode != domain.ModeChat || got.Status != domain.StatusQueued {
		t.Fatalf("CreateSession() mode/status = %q/%q", got.Mode, got.Status)
	}
	if len(repo.saved) != 2 {
		t.Fatalf("saved sessions = %d", len(repo.saved))
	}
	saved := repo.saved[len(repo.saved)-1]
	if saved.Status != domain.StatusQueued || saved.Mode != domain.ModeChat || saved.Queue.Kind != domain.QueueKindStart {
		t.Fatalf("saved session status/mode = %q/%q", saved.Status, saved.Mode)
	}
	if !reflect.DeepEqual(saved.Config, got.Config) {
		t.Fatalf("saved config = %#v, want %#v", saved.Config, got.Config)
	}
	if saved.LastRunAt == nil || saved.CodexSessionID != "" || saved.WorktreePath != "" {
		t.Fatalf("CreateSession() should queue without starting codex: %#v", saved)
	}
}

func TestCreateSessionFillsMissingConfigFromPreviousProjectSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.lastConfig = domain.Config{
		CodexModel:      "gpt-5.4",
		ReasoningEffort: "high",
		PermissionMode:  "workspace-write",
	}
	repo.hasLastConfig = true
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
		Config: domain.Config{
			CodexModel: "gpt-5.5",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	want := domain.Config{
		CodexModel:      "gpt-5.5",
		ReasoningEffort: "high",
		PermissionMode:  "workspace-write",
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("Config = %#v, want %#v", got.Config, want)
	}
	if repo.lastConfigProjectID != "project-1" {
		t.Fatalf("LastConfigForProject project = %q", repo.lastConfigProjectID)
	}
}

func TestCreateSessionUsesProjectScopedShortIDForDefaultGeneratedID(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["existing"] = domain.Session{ID: "existing", ProjectID: "018f61095330c21e109ba56787a2e09f"}
	projects := newFakeProjectRepository()
	projects.projects["018f61095330c21e109ba56787a2e09f"] = projectdomain.Project{
		ID:   "018f61095330c21e109ba56787a2e09f",
		Name: "workspaces",
	}
	service := New(repo, projects)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "0123456789abcdef0123456789abcdef", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "018f61095330c21e109ba56787a2e09f",
		Requirement: "implement app session",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.ID != "p018f6109-c2" {
		t.Fatalf("CreateSession() ID = %q, want p018f6109-c2", got.ID)
	}
	if _, ok := repo.sessions["p018f6109-c2"]; !ok {
		t.Fatalf("saved sessions = %#v", repo.sessions)
	}
}

func TestCreateSessionRetriesProjectShortIDWhenCandidateExists(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["old-1"] = domain.Session{ID: "old-1", ProjectID: "018f61095330c21e109ba56787a2e09f"}
	repo.sessions["old-2"] = domain.Session{ID: "old-2", ProjectID: "018f61095330c21e109ba56787a2e09f"}
	repo.sessions["p018f6109-c3"] = domain.Session{ID: "p018f6109-c3", ProjectID: "other", Requirement: "keep me"}
	projects := newFakeProjectRepository()
	projects.projects["018f61095330c21e109ba56787a2e09f"] = projectdomain.Project{
		ID:   "018f61095330c21e109ba56787a2e09f",
		Name: "workspaces",
	}
	service := New(repo, projects)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "0123456789abcdef0123456789abcdef", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "018f61095330c21e109ba56787a2e09f",
		Requirement: "implement app session",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.ID != "p018f6109-c4" {
		t.Fatalf("CreateSession() ID = %q, want p018f6109-c4", got.ID)
	}
	if repo.sessions["p018f6109-c3"].Requirement != "keep me" {
		t.Fatalf("existing session was overwritten: %#v", repo.sessions["p018f6109-c3"])
	}
}

func TestCreateSessionUsesDistinctProjectCodesForSimilarProjectIDs(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Name: "project-1"}
	projects.projects["project-2"] = projectdomain.Project{ID: "project-2", Name: "project-2"}
	service := New(repo, projects)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "0123456789abcdef0123456789abcdef", nil }

	first, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement first session",
	})
	if err != nil {
		t.Fatalf("CreateSession(project-1) error = %v", err)
	}
	second, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-2",
		Requirement: "implement second session",
	})
	if err != nil {
		t.Fatalf("CreateSession(project-2) error = %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("CreateSession() IDs should differ for similar project IDs, both got %q", first.ID)
	}
	if first.ID != "pproject1-c1" || second.ID != "pproject2-c1" {
		t.Fatalf("CreateSession() IDs = %q/%q, want pproject1-c1/pproject2-c1", first.ID, second.ID)
	}
}

func TestCreateSessionValidatesProjectAndRequirement(t *testing.T) {
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"))

	if _, err := service.CreateSession(context.Background(), CreateSessionInput{Requirement: "body"}); err == nil {
		t.Fatal("CreateSession() expected project id error")
	}
	if _, err := service.CreateSession(context.Background(), CreateSessionInput{ProjectID: "project-1", Requirement: "   "}); err == nil {
		t.Fatal("CreateSession() expected requirement error")
	}
	if _, err := service.CreateSession(context.Background(), CreateSessionInput{ProjectID: "missing", Requirement: "body"}); err == nil {
		t.Fatal("CreateSession() expected missing project error")
	}
}

func TestCreateSessionCreatesWorktreeForGitProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1"}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
		BaseBranch:  " main ",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.WorktreePath != "/data/worktrees/project-1/session-1" {
		t.Fatalf("WorktreePath = %q", got.WorktreePath)
	}
	if got.WorktreeBranch != "session-1" {
		t.Fatalf("WorktreeBranch = %q, want %q", got.WorktreeBranch, "session-1")
	}
	if worktrees.createProjectPath != "/workspace/project-1" || worktrees.createProjectID != "project-1" || worktrees.createSessionID != "session-1" || worktrees.createBaseBranch != "main" {
		t.Fatalf("Create() args = path %q project %q session %q branch %q", worktrees.createProjectPath, worktrees.createProjectID, worktrees.createSessionID, worktrees.createBaseBranch)
	}
	if len(repo.saved) != 2 || repo.saved[len(repo.saved)-1].WorktreePath != got.WorktreePath {
		t.Fatalf("saved sessions = %#v", repo.saved)
	}
}

func TestCreateSessionStoresWorktreeBaseCommit(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1", headCommit: "base"}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
		BaseBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.WorktreeBaseCommit != "base" {
		t.Fatalf("WorktreeBaseCommit = %q", got.WorktreeBaseCommit)
	}
	if worktrees.headCommitPath != "/data/worktrees/project-1/session-1" || worktrees.headCommitRef != "" {
		t.Fatalf("HeadCommit() = path %q ref %q", worktrees.headCommitPath, worktrees.headCommitRef)
	}
	if repo.sessions["session-1"].WorktreeBaseCommit != "base" {
		t.Fatalf("saved WorktreeBaseCommit = %q", repo.sessions["session-1"].WorktreeBaseCommit)
	}
}

func TestCreateSessionUsesShortIDForGitWorktreeBranch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["018f61095330c21e109ba56787a2e09f"] = projectdomain.Project{
		ID:    "018f61095330c21e109ba56787a2e09f",
		Name:  "workspaces",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{path: "/data/worktrees/018f61095330c21e109ba56787a2e09f/p018f6109-c1"}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "0123456789abcdef0123456789abcdef", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "018f61095330c21e109ba56787a2e09f",
		Requirement: "implement app session",
		BaseBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.ID != "p018f6109-c1" || got.WorktreeBranch != "p018f6109-c1" {
		t.Fatalf("CreateSession() id/worktree branch = %q/%q", got.ID, got.WorktreeBranch)
	}
	if worktrees.createSessionID != "p018f6109-c1" {
		t.Fatalf("Create() session = %q, want p018f6109-c1", worktrees.createSessionID)
	}
}

func TestCreateSessionRequiresBaseBranchForGitProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1"}
	service := New(repo, projects, WithWorktrees(worktrees))

	_, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
	})
	if err == nil {
		t.Fatal("CreateSession() expected base branch error")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeValidationFailed || appErr.UserAction != "select_base_branch" {
		t.Fatalf("CreateSession() error = %#v", err)
	}
	if worktrees.createCalled {
		t.Fatal("Create() should not be called without base branch")
	}
	if len(repo.saved) != 0 {
		t.Fatalf("saved sessions = %#v", repo.saved)
	}
}

func TestCreateSessionUsesProjectPathForNonGitProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: false,
	}
	worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1"}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.WorktreePath != "/workspace/project-1" {
		t.Fatalf("WorktreePath = %q", got.WorktreePath)
	}
	if worktrees.createCalled {
		t.Fatal("Create() should not be called for non-git project")
	}
}

func TestCreateWorkflowSessionStartsFirstNodeCodexWithNodeRun(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		Path:              projectdomain.ProjectPath{Value: "/workspace/project-1"},
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-1")
	workflows := &fakeWorkflowStarter{start: domain.WorkflowStart{
		WorkflowRunID:    "workflow-run-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Validate build",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-1"}}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex))
	ids := []domain.ID{"session-1", "process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "ship feature",
		Mode:        domain.ModeWorkflow,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued || got.CodexSessionID != "" {
		t.Fatalf("CreateSession() = %#v", got)
	}
	if workflows.input.WorkflowDefinitionID != "workflow-1" || workflows.input.Requirement != "ship feature" {
		t.Fatalf("workflow input = %#v", workflows.input)
	}
	if len(processes.created) != 0 {
		t.Fatalf("process runs = %#v", processes.created)
	}
	saved := repo.sessions["session-1"]
	if codex.startCalled || saved.Queue.WorkflowRunID != "workflow-run-1" || saved.Queue.NodeRunID == nil || *saved.Queue.NodeRunID != "node-run-1" || saved.Queue.Prompt != "Validate build" {
		t.Fatalf("queued workflow session = %#v codexCalled=%v", saved, codex.startCalled)
	}
}

func TestCreateWorkflowSessionClosesWhenWorkflowStartsAtCloseNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		Path:              projectdomain.ProjectPath{Value: "/workspace/project-1"},
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-close")
	workflows := &fakeWorkflowStarter{start: domain.WorkflowStart{
		WorkflowRunID:    "workflow-run-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "close",
		CurrentNodeTitle: "Close",
		Status:           "completed",
		Close:            true,
	}}
	service := New(repo, projects, WithWorkflows(workflows))
	service.generateID = func() (domain.ID, error) { return "session-1", nil }
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "ship feature",
		Mode:        domain.ModeWorkflow,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CreateSession() = %#v", got)
	}
	if saved := repo.sessions["session-1"]; saved.Status != domain.StatusClosed || saved.CloseReason == nil || *saved.CloseReason != domain.CloseReasonWorkflowClosed {
		t.Fatalf("saved session = %#v", saved)
	}
}

func TestStartWorkflowSessionUsesWorkflowStarterInsteadOfPlainCodex(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/project-1",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-1")
	workflows := &fakeWorkflowStarter{start: domain.WorkflowStart{
		WorkflowRunID:    "workflow-run-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Run workflow node",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-1"}}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex))
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued || workflows.input.WorkflowDefinitionID != "workflow-1" {
		t.Fatalf("StartSession() = %#v workflow input=%#v", got, workflows.input)
	}
	if codex.startCalled {
		t.Fatalf("codex should not start before queue drain: %#v", codex.startInput)
	}
	if saved := repo.sessions["session-1"]; saved.Queue.NodeRunID == nil || *saved.Queue.NodeRunID != "node-run-1" || saved.Queue.Prompt != "Run workflow node" {
		t.Fatalf("queued session = %#v", saved)
	}
}

func TestWorkflowNodePromptMentionsAnswerUserGuidance(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/project-1",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-1")
	workflows := &fakeWorkflowStarter{start: domain.WorkflowStart{
		WorkflowRunID:    "workflow-run-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Run workflow node",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-1"}}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex))
	ids := []domain.ID{"event-queued", "process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	if _, err := service.StartSession(ctx, "session-1"); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSessionWithOptions() error = %v", err)
	}
	prompt := codex.startInput.Prompt
	if !strings.Contains(prompt, "Run workflow node") {
		t.Fatalf("prompt missing workflow node prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "answer_user") {
		t.Fatalf("workflow prompt missing answer_user guidance: %q", prompt)
	}
}

func TestStartWorkflowResumeFailedSessionRerunsCurrentNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusResumeFailed,
		WorktreePath: "/workspace/project-1",
	}
	nodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{rerunAdvance: domain.WorkflowAdvance{
		WorkflowRunID:    "workflow-run-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Run current node again",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-2"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithProcesses(processes, codex))
	service.generateID = func() (domain.ID, error) { return "process-run-2", nil }
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued || workflows.rerunInput.SessionID != "session-1" {
		t.Fatalf("StartSession() = %#v rerun input=%#v", got, workflows.rerunInput)
	}
	if codex.startCalled {
		t.Fatalf("codex should not start before queue drain: %#v", codex.startInput)
	}
	if saved := repo.sessions["session-1"]; saved.Queue.NodeRunID == nil || *saved.Queue.NodeRunID != "node-run-2" {
		t.Fatalf("queued session = %#v", saved)
	} else {
		for _, want := range []string{
			"无法复用已有 Codex 会话",
			"原始需求：\nship feature",
			"当前流程节点提示词：\nRun current node again",
		} {
			if !strings.Contains(saved.Queue.Prompt, want) {
				t.Fatalf("queued prompt missing %q: %q", want, saved.Queue.Prompt)
			}
		}
	}
}

func TestWorkflowRerunRebuildsPromptWithAppendsAndNodePrompt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusResumeFailed,
		WorktreePath: "/workspace/project-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-1", SessionID: "session-1", Body: "preserve manual fix", CreatedAt: time.Unix(10, 0).UTC()},
	}
	nodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{rerunAdvance: domain.WorkflowAdvance{
		WorkflowRunID:    "workflow-run-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Run current node again",
	}}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows))
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued || workflows.rerunInput.SessionID != "session-1" {
		t.Fatalf("StartSession() = %#v rerun input=%#v", got, workflows.rerunInput)
	}
	prompt := repo.sessions["session-1"].Queue.Prompt
	for _, want := range []string{
		"无法复用已有 Codex 会话",
		"复查",
		"原始需求：\nship feature",
		"追加描述：\npreserve manual fix",
		"当前流程节点提示词：\nRun current node again",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("queued prompt missing %q: %q", want, prompt)
		}
	}
}

func TestWorkflowRerunResumeFailedWithCodexSessionStartsNewProcess(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "ship feature",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusResumeFailed,
		CodexSessionID: "codex-session-failed",
		WorktreePath:   "/workspace/project-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-1", SessionID: "session-1", Body: "preserve manual fix", CreatedAt: time.Unix(10, 0).UTC()},
	}
	nodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{rerunAdvance: domain.WorkflowAdvance{
		WorkflowRunID:    "workflow-run-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Run current node again",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{
		startHandle:  processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-new"},
		resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-failed"},
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithProcesses(processes, codex))
	ids := []domain.ID{"process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued || repo.sessions["session-1"].Queue.Kind != domain.QueueKindStart {
		t.Fatalf("queued session = %#v", repo.sessions["session-1"])
	}
	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if codex.resumeCalled {
		t.Fatalf("workflow rerun should start new codex process, got resume input %#v", codex.resumeInput)
	}
	if !codex.startCalled {
		t.Fatal("workflow rerun should call codex start")
	}
	prompt := codex.startInput.Prompt
	if strings.Count(prompt, "无法复用已有 Codex 会话") != 1 {
		t.Fatalf("codex prompt should contain one review notice: %q", prompt)
	}
	for _, want := range []string{
		"原始需求：\nship feature",
		"追加描述：\npreserve manual fix",
		"当前流程节点提示词：\nRun current node again",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("codex prompt missing %q: %q", want, prompt)
		}
	}
}

func TestWorkflowMergeNodeRecordsMergeAndClosesWhenCompleted(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		IsGit:             true,
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-merge")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &nodeRunID,
			CurrentNodeID:    "merge",
			CurrentNodeTitle: "Merge",
			Status:           "running",
			Merge:            &domain.WorkflowMerge{Strategy: "merge"},
		},
		advance: domain.WorkflowAdvance{
			WorkflowRunID: "workflow-run-1",
			Status:        "completed",
			Completed:     true,
		},
	}
	merge := &fakeMergePort{result: gitdiffdomain.MergeResult{
		Strategy:       "merge",
		BaseBranch:     "main",
		WorktreeBranch: "feature/session-1",
		BaseCommit:     "base",
		HeadCommit:     "head",
		MergeCommit:    "merge",
		Status:         "merged",
	}}
	worktrees := &fakeWorktreeManager{}
	service := New(repo, projects, WithWorkflows(workflows), WithMergePort(merge), WithWorktrees(worktrees))
	service.generateID = func() (domain.ID, error) { return "merge-record-1", nil }
	service.now = func() time.Time { return time.Unix(60, 0).UTC() }

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("StartSession() status = %q", got.Status)
	}
	saved := repo.sessions["session-1"]
	if saved.CloseReason == nil || *saved.CloseReason != domain.CloseReasonMergedClosed {
		t.Fatalf("CloseReason = %#v", saved.CloseReason)
	}
	if !merge.mergeCalled || merge.mergeInput.WorktreePath != "/data/worktrees/project-1/session-1" || merge.mergeInput.BaseBranch != "main" {
		t.Fatalf("merge input = %#v called=%v", merge.mergeInput, merge.mergeCalled)
	}
	if len(repo.mergeRecords) != 1 {
		t.Fatalf("merge records = %#v", repo.mergeRecords)
	}
	record := repo.mergeRecords[0]
	if record.NodeRunID == nil || *record.NodeRunID != "node-run-merge" || record.Status != "merged" || record.MergeCommit != "merge" {
		t.Fatalf("merge record = %#v", record)
	}
	if record.MergedAt == nil || !record.MergedAt.Equal(time.Unix(60, 0).UTC()) {
		t.Fatalf("MergedAt = %#v", record.MergedAt)
	}
	if workflows.completeInput.NodeRunID != "node-run-merge" {
		t.Fatalf("complete input = %#v", workflows.completeInput)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	if got.WorktreePath != "" {
		t.Fatalf("closed session worktree path = %q, want empty", got.WorktreePath)
	}
	if repo.sessions["session-1"].WorktreePath != "" {
		t.Fatalf("saved worktree path = %q, want empty", repo.sessions["session-1"].WorktreePath)
	}
}

func TestWorkflowMergeNodeFailureRecordsAndFailsCurrentNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		IsGit:             true,
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-merge")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &nodeRunID,
			CurrentNodeID:    "merge",
			CurrentNodeTitle: "Merge",
			Status:           "running",
			Merge:            &domain.WorkflowMerge{Strategy: "rebase"},
		},
		failAdvance: domain.WorkflowAdvance{
			WorkflowRunID:    "workflow-run-1",
			Status:           "blocked",
			Blocked:          true,
			BlockedReason:    "merge failed",
			RequiresCodex:    false,
			CurrentNodeID:    "merge",
			CurrentNodeTitle: "Merge",
		},
	}
	merge := &fakeMergePort{result: gitdiffdomain.MergeResult{
		Strategy:      "rebase",
		BaseBranch:    "main",
		Status:        "failed",
		FailureCode:   "dirty_worktree",
		FailureReason: "worktree has uncommitted changes",
	}}
	service := New(repo, projects, WithWorkflows(workflows), WithMergePort(merge))
	service.generateID = func() (domain.ID, error) { return "merge-record-1", nil }
	service.now = func() time.Time { return time.Unix(60, 0).UTC() }

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusBlocked {
		t.Fatalf("StartSession() status = %q", got.Status)
	}
	if !merge.rebaseCalled || merge.rebaseInput.WorktreePath != "/data/worktrees/project-1/session-1" || merge.rebaseInput.BaseBranch != "main" {
		t.Fatalf("rebase input = %#v called=%v", merge.rebaseInput, merge.rebaseCalled)
	}
	if workflows.failInput.NodeRunID != "node-run-merge" || workflows.failInput.Code != "dirty_worktree" {
		t.Fatalf("fail input = %#v", workflows.failInput)
	}
	mergeOutput, ok := workflows.failInput.Output["merge"].(map[string]any)
	if !ok || mergeOutput["status"] != "failed" || mergeOutput["failureCode"] != "dirty_worktree" || mergeOutput["failureReason"] != "worktree has uncommitted changes" {
		t.Fatalf("merge output = %#v", workflows.failInput.Output)
	}
	if len(repo.mergeRecords) != 1 || repo.mergeRecords[0].Status != "failed" || repo.mergeRecords[0].FailureCode != "dirty_worktree" {
		t.Fatalf("merge records = %#v", repo.mergeRecords)
	}
}

func TestWorkflowMergeNodeFailureAsksUserBeforeFailingNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		IsGit:             true,
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-merge")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &nodeRunID,
			CurrentNodeID:    "merge",
			CurrentNodeTitle: "Merge",
			Status:           "running",
			Merge:            &domain.WorkflowMerge{Strategy: "rebase"},
		},
	}
	merge := &fakeMergePort{result: gitdiffdomain.MergeResult{
		Strategy:      "rebase",
		BaseBranch:    "main",
		Status:        "failed",
		FailureCode:   "dirty_worktree",
		FailureReason: "worktree has uncommitted changes",
	}}
	questions := &fakeQuestionCanceller{}
	service := New(repo, projects, WithWorkflows(workflows), WithMergePort(merge), WithQuestions(questions))
	service.generateID = func() (domain.ID, error) { return "merge-record-1", nil }
	service.now = func() time.Time { return time.Unix(60, 0).UTC() }

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusWaitingUser {
		t.Fatalf("StartSession() status = %q", got.Status)
	}
	if workflows.failInput.NodeRunID != "" {
		t.Fatalf("FailNode should not be called before user answer: %#v", workflows.failInput)
	}
	if questions.created.SessionID != "session-1" || questions.created.WorkflowRunID == nil || *questions.created.WorkflowRunID != "workflow-run-1" {
		t.Fatalf("created question batch input = %#v", questions.created)
	}
	if len(questions.created.Questions) != 1 {
		t.Fatalf("created questions = %#v", questions.created.Questions)
	}
	question := questions.created.Questions[0]
	if question.Type != "merge_failure_action" || question.Metadata["nodeRunId"] != "node-run-merge" {
		t.Fatalf("merge failure question = %#v", question)
	}
	if len(question.Options) != 3 || question.Options[0].Payload["action"] != "retry_merge" {
		t.Fatalf("merge failure options = %#v", question.Options)
	}
}

func TestWorkflowExprNodeCompletesWithResults(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		DefaultWorkflowID: &workflowID,
	}
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      domain.ModeWorkflow,
		Status:    domain.StatusStopped,
	}
	nodeRunID := domain.NodeRunID("node-run-expr")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &nodeRunID,
			CurrentNodeID:    "derive",
			CurrentNodeTitle: "Derive",
			Status:           "running",
			Expr: &domain.WorkflowExpr{
				Script: `{status: params.ok ? "passed" : "failed", count: params.count + 1}`,
				Params: map[string]any{"ok": true, "count": 1},
			},
		},
		advance: domain.WorkflowAdvance{
			WorkflowRunID: "workflow-run-1",
			Status:        "completed",
			Completed:     true,
		},
	}
	service := New(repo, projects, WithWorkflows(workflows))
	service.now = func() time.Time { return time.Unix(70, 0).UTC() }

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusCompleted {
		t.Fatalf("StartSession() status = %q", got.Status)
	}
	results, ok := workflows.completeInput.Output["results"].(map[string]any)
	if !ok || results["status"] != "passed" || results["count"] != 2 {
		t.Fatalf("expr results = %#v", workflows.completeInput.Output)
	}
	if workflows.completeInput.NodeRunID != "node-run-expr" {
		t.Fatalf("complete input = %#v", workflows.completeInput)
	}
}

func TestHandleQuestionBatchAnsweredRetriesMerge(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusWaitingUser,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Name: "project-1", IsGit: true}
	nodeRunID := domain.NodeRunID("node-run-merge")
	workflows := &fakeWorkflowStarter{
		advance: domain.WorkflowAdvance{
			WorkflowRunID: "workflow-run-1",
			Status:        "completed",
			Completed:     true,
		},
	}
	merge := &fakeMergePort{result: gitdiffdomain.MergeResult{
		Strategy:       "merge",
		BaseBranch:     "main",
		WorktreeBranch: "feature/session-1",
		BaseCommit:     "base",
		HeadCommit:     "head",
		MergeCommit:    "merge",
		Status:         "merged",
	}}
	worktrees := &fakeWorktreeManager{}
	service := New(repo, projects, WithWorkflows(workflows), WithMergePort(merge), WithWorktrees(worktrees))
	service.generateID = func() (domain.ID, error) { return "merge-record-2", nil }
	service.now = func() time.Time { return time.Unix(90, 0).UTC() }
	optionID := questiondomain.OptionID("retry_merge")

	err := service.HandleQuestionBatchAnswered(ctx, questionapp.BatchDTO{
		ID:        "batch-1",
		SessionID: "session-1",
		Status:    questiondomain.BatchAnswered,
		Questions: []questiondomain.Question{
			{
				ID:   "question-1",
				Type: "merge_failure_action",
				Metadata: map[string]any{
					"workflowRunId": "workflow-run-1",
					"nodeRunId":     string(nodeRunID),
					"strategy":      "merge",
					"failureCode":   "merge_failed",
				},
				SelectedOptionID: &optionID,
				Options: []questiondomain.Option{
					{ID: "retry_merge", Payload: map[string]any{"action": "retry_merge"}},
				},
				Answer: map[string]any{
					"action":        "fail_node",
					"workflowRunId": "forged-workflow-run",
					"nodeRunId":     "forged-node-run",
					"strategy":      "rebase",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleQuestionBatchAnswered() error = %v", err)
	}
	if !merge.mergeCalled || merge.mergeInput.BaseBranch != "main" {
		t.Fatalf("merge input = %#v called=%v", merge.mergeInput, merge.mergeCalled)
	}
	if merge.rebaseCalled {
		t.Fatalf("client payload should not override server metadata: rebase input = %#v", merge.rebaseInput)
	}
	if workflows.completeInput.NodeRunID != nodeRunID {
		t.Fatalf("complete input = %#v", workflows.completeInput)
	}
	if repo.sessions["session-1"].Status != domain.StatusClosed {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
}

func TestHandleQuestionBatchAnsweredDoesNotMarkNormalAnswerRunning(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingUser,
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events))
	service.generateID = func() (domain.ID, error) { return "event-running", nil }

	err := service.HandleQuestionBatchAnswered(ctx, questionapp.BatchDTO{
		ID:        "batch-1",
		SessionID: "session-1",
		Status:    questiondomain.BatchAnswered,
		Questions: []questiondomain.Question{
			{
				ID:   "question-1",
				Type: "choice",
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleQuestionBatchAnswered() error = %v", err)
	}
	if repo.sessions["session-1"].Status != domain.StatusWaitingUser {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
	got := events.snapshot()
	if len(got) != 0 {
		t.Fatalf("events = %#v", got)
	}
}

func TestHandleQuestionBatchAnsweredFailsMergeNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:         "session-1",
		ProjectID:  "project-1",
		Mode:       domain.ModeWorkflow,
		Status:     domain.StatusWaitingUser,
		BaseBranch: "main",
	}
	nodeRunID := domain.NodeRunID("node-run-merge")
	workflows := &fakeWorkflowStarter{
		failAdvance: domain.WorkflowAdvance{
			WorkflowRunID: "workflow-run-1",
			Status:        "blocked",
			Blocked:       true,
			BlockedReason: "merge failed",
		},
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows))
	optionID := questiondomain.OptionID("fail_node")

	err := service.HandleQuestionBatchAnswered(ctx, questionapp.BatchDTO{
		ID:        "batch-1",
		SessionID: "session-1",
		Status:    questiondomain.BatchAnswered,
		Questions: []questiondomain.Question{
			{
				ID:   "question-1",
				Type: "merge_failure_action",
				Metadata: map[string]any{
					"workflowRunId": "workflow-run-1",
					"nodeRunId":     string(nodeRunID),
					"failureCode":   "dirty_worktree",
					"failureReason": "worktree has uncommitted changes",
				},
				SelectedOptionID: &optionID,
				Options: []questiondomain.Option{
					{ID: "fail_node", Payload: map[string]any{"action": "fail_node"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleQuestionBatchAnswered() error = %v", err)
	}
	if workflows.failInput.NodeRunID != nodeRunID || workflows.failInput.Code != "dirty_worktree" {
		t.Fatalf("fail input = %#v", workflows.failInput)
	}
	mergeOutput, ok := workflows.failInput.Output["merge"].(map[string]any)
	if !ok || mergeOutput["status"] != "failed" || mergeOutput["failureCode"] != "dirty_worktree" || mergeOutput["failureReason"] != "worktree has uncommitted changes" {
		t.Fatalf("merge output = %#v", workflows.failInput.Output)
	}
}

func TestHandleQuestionBatchAnsweredRejectsUnsupportedMergeAction(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusWaitingUser}
	service := New(repo, newFakeProjectRepository("project-1"))
	optionID := questiondomain.OptionID("unknown")

	err := service.HandleQuestionBatchAnswered(ctx, questionapp.BatchDTO{
		ID:        "batch-1",
		SessionID: "session-1",
		Status:    questiondomain.BatchAnswered,
		Questions: []questiondomain.Question{
			{
				ID:   "question-1",
				Type: "merge_failure_action",
				Metadata: map[string]any{
					"workflowRunId": "workflow-run-1",
					"nodeRunId":     "node-run-merge",
				},
				SelectedOptionID: &optionID,
				Options: []questiondomain.Option{
					{ID: "unknown", Payload: map[string]any{"action": "delete"}},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported merge failure action") {
		t.Fatalf("HandleQuestionBatchAnswered() error = %v", err)
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeValidationFailed || appErr.Details["action"] != "delete" {
		t.Fatalf("HandleQuestionBatchAnswered() app error = %#v", err)
	}
}

func TestAskMergeFailureCancelsQuestionWhenSessionSaveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.saveErr = errors.New("database unavailable")
	questions := &fakeQuestionCanceller{}
	service := New(repo, newFakeProjectRepository("project-1"), WithQuestions(questions))
	nodeRunID := domain.NodeRunID("node-run-merge")

	_, err := service.askMergeFailure(ctx, domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusStopped,
	}, domain.WorkflowAdvance{
		WorkflowRunID: "workflow-run-1",
		NodeRunID:     &nodeRunID,
	}, gitdiffdomain.MergeResult{
		Strategy:      "merge",
		Status:        "failed",
		FailureReason: "conflict",
	}, "merge_conflict")
	if err == nil || !strings.Contains(err.Error(), "save session") {
		t.Fatalf("askMergeFailure() error = %v", err)
	}
	if questions.cancelledSessionID != "session-1" || questions.cancelReason != "merge failure question abandoned" {
		t.Fatalf("cancelled questions = %q %q", questions.cancelledSessionID, questions.cancelReason)
	}
}

func TestWorkflowSessionStartsNextNodeAfterProcessExit(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/project-1",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		DefaultWorkflowID: &workflowID,
	}
	firstNodeRunID := domain.NodeRunID("node-run-1")
	secondNodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &firstNodeRunID,
			CurrentNodeID:    "build",
			CurrentNodeTitle: "Build",
			Status:           "running",
			RequiresCodex:    true,
			Prompt:           "Build",
		},
		advance: domain.WorkflowAdvance{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &secondNodeRunID,
			CurrentNodeID:    "verify",
			CurrentNodeTitle: "Verify",
			Status:           "running",
			RequiresCodex:    true,
			Prompt:           "Verify",
		},
	}
	processes := newFakeProcessRepository()
	events := &fakeEventStore{}
	closedEvents := make(chan processdomain.CodexEvent)
	close(closedEvents)
	blockedEvents := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		startHandle:  processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-1"},
		eventStreams: []<-chan processdomain.CodexEvent{closedEvents, blockedEvents},
	}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex), WithEvents(events))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		if nextID == 1 {
			return "process-run-1", nil
		}
		if nextID == 5 {
			return "process-run-2", nil
		}
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if repo.sessions["session-1"].Status == domain.StatusQueued {
			if _, err := service.DrainQueuedSessions(ctx); err != nil {
				t.Fatalf("DrainQueuedSessions() error = %v", err)
			}
		}
		if len(processes.created) >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(processes.created) < 2 {
		t.Fatalf("expected second process run, got %#v", processes.created)
	}
	if processes.created[1].NodeRunID == nil || *processes.created[1].NodeRunID != "node-run-2" {
		t.Fatalf("second process run = %#v", processes.created[1])
	}
	if !codex.resumeCalled || codex.resumeInput.CodexSessionID != "codex-session-1" || codex.resumeInput.Prompt != promptWithAnswerUserGuidance("Verify") {
		t.Fatalf("codex resume input = %#v", codex.resumeInput)
	}
	if workflows.completeInput.NodeRunID != "node-run-1" {
		t.Fatalf("complete input = %#v", workflows.completeInput)
	}
}

func TestWorkflowSessionMarksRunFailedWhenJSONRetryStillMissingResults(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/project-1",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-1")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &nodeRunID,
			CurrentNodeID:    "build",
			CurrentNodeTitle: "Build",
			Status:           "running",
			RequiresCodex:    true,
			RequireJSONRetry: true,
			Prompt:           "ANYCODE_WORKFLOW_JSON_RETRY",
		},
		completeErr: apperror.New(apperror.CodeWorkflowJSONRequired, apperror.CategoryWorkflowError, "workflow node output JSON is required"),
	}
	closedEvents := make(chan processdomain.CodexEvent)
	close(closedEvents)
	codex := &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 123},
		events:      closedEvents,
	}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(newFakeProcessRepository(), codex), WithEvents(&fakeEventStore{}))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		if nextID == 1 {
			return "process-run-1", nil
		}
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSessionWithOptions() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if repo.sessions["session-1"].Status == domain.StatusQueued {
			if _, err := service.DrainQueuedSessions(ctx); err != nil {
				t.Fatalf("DrainQueuedSessions() error = %v", err)
			}
		}
		if workflows.failedInput.Code == apperror.CodeWorkflowJSONRequired {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if workflows.failedInput.WorkflowRunID != "workflow-run-1" || workflows.failedInput.NodeRunID == nil || *workflows.failedInput.NodeRunID != "node-run-1" {
		t.Fatalf("failed input = %#v", workflows.failedInput)
	}
	if workflows.failedInput.Code != apperror.CodeWorkflowJSONRequired {
		t.Fatalf("failed input = %#v", workflows.failedInput)
	}
	if repo.sessions["session-1"].Status != domain.StatusFailed {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
}

func TestWorkflowSessionFailsCurrentNodeOnAbnormalProcessExit(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/project-1",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		DefaultWorkflowID: &workflowID,
	}
	firstNodeRunID := domain.NodeRunID("node-run-1")
	secondNodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &firstNodeRunID,
			CurrentNodeID:    "build",
			CurrentNodeTitle: "Build",
			Status:           "running",
			RequiresCodex:    true,
			Prompt:           "Build",
		},
		failAdvance: domain.WorkflowAdvance{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &secondNodeRunID,
			CurrentNodeID:    "build",
			CurrentNodeTitle: "Build",
			Status:           "running",
			RequiresCodex:    true,
			Prompt:           "Build retry",
		},
	}
	processes := newFakeProcessRepository()
	failedEvents := make(chan processdomain.CodexEvent, 1)
	failedEvents <- processdomain.CodexEvent{Type: "process.exit", Payload: map[string]any{"exitCode": 7, "failureReason": "exit status 7"}}
	close(failedEvents)
	codex := &fakeCodexProcess{
		startHandle:  processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-1"},
		eventStreams: []<-chan processdomain.CodexEvent{failedEvents},
	}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		switch nextID {
		case 1:
			return "process-run-1", nil
		case 3:
			return "process-run-2", nil
		default:
			return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
		}
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if repo.sessions["session-1"].Status == domain.StatusQueued {
			if _, err := service.DrainQueuedSessions(ctx); err != nil {
				t.Fatalf("DrainQueuedSessions() error = %v", err)
			}
		}
		if len(processes.created) >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(processes.created) < 2 {
		t.Fatalf("expected retry process run, got %#v", processes.created)
	}
	if workflows.completeInput.NodeRunID != "" {
		t.Fatalf("complete should not be called on failed exit: %#v", workflows.completeInput)
	}
	if workflows.failInput.NodeRunID != "node-run-1" || workflows.failInput.Code != "codex_process_failed" {
		t.Fatalf("fail input = %#v", workflows.failInput)
	}
	if processes.created[1].NodeRunID == nil || *processes.created[1].NodeRunID != "node-run-2" {
		t.Fatalf("retry process run = %#v", processes.created[1])
	}
}

func TestCodexProcessFailureCodeClassifiesParameterRejection(t *testing.T) {
	got := codexProcessFailureCode(processdomain.ExitResult{
		FailureReason: `exit status 2: invalid value "readonly" for '--sandbox'`,
	})
	if got != "codex_param_rejected" {
		t.Fatalf("codexProcessFailureCode() = %q", got)
	}

	got = codexProcessFailureCode(processdomain.ExitResult{FailureReason: "exit status 7"})
	if got != "codex_process_failed" {
		t.Fatalf("codexProcessFailureCode() = %q", got)
	}
}

func TestWorkflowResultsFromTextExtractsJSONResults(t *testing.T) {
	got, ok := workflowResultsFromText("summary\n```json\n{\"results\":{\"status\":\"passed\",\"count\":2}}\n```")
	if !ok || got["status"] != "passed" || got["count"] != float64(2) {
		t.Fatalf("workflowResultsFromText() = %#v, %v", got, ok)
	}
}

func TestWorkflowResultsFromEventExtractsCodexAssistantItem(t *testing.T) {
	got, ok := workflowResultsFromEvent(processdomain.CodexEvent{
		Type: "item.completed",
		Payload: map[string]any{
			"item": map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": `{"results":{"status":"passed"}}`},
				},
			},
		},
	})
	if !ok || got["status"] != "passed" {
		t.Fatalf("workflowResultsFromEvent() = %#v, %v", got, ok)
	}
}

func TestWorkflowResultsFromEventExtractsAggregatedOutput(t *testing.T) {
	got, ok := workflowResultsFromEvent(processdomain.CodexEvent{
		Type: "item.completed",
		Payload: map[string]any{
			"item": map[string]any{
				"type":              "agent_message",
				"aggregated_output": `{"results":{"status":"passed"}}`,
				"status":            "completed",
			},
		},
	})
	if !ok || got["status"] != "passed" {
		t.Fatalf("workflowResultsFromEvent() = %#v, %v", got, ok)
	}
}

func TestWorkflowResultsFromEventIgnoresCommandAggregatedOutput(t *testing.T) {
	_, ok := workflowResultsFromEvent(processdomain.CodexEvent{
		Type: "item.completed",
		Payload: map[string]any{
			"item": map[string]any{
				"type":              "command_execution",
				"aggregated_output": `{"results":{"status":"passed"}}`,
				"status":            "completed",
			},
		},
	})
	if ok {
		t.Fatal("workflowResultsFromEvent() should ignore command aggregated output")
	}
}

func TestWorkflowResultsFromEventIgnoresUserPromptJSON(t *testing.T) {
	_, ok := workflowResultsFromEvent(processdomain.CodexEvent{
		Type: "item.completed",
		Payload: map[string]any{
			"item": map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": `Workflow input params JSON: {"requirement":"ship"}`},
				},
			},
		},
	})
	if ok {
		t.Fatal("workflowResultsFromEvent() should ignore user prompt JSON")
	}
}

func TestCodexSessionIDFromEventReadsThreadID(t *testing.T) {
	got := codexSessionIDFromEvent(processdomain.CodexEvent{
		Type:    "thread.started",
		Payload: map[string]any{"thread_id": "codex-thread-1"},
	})
	if got != "codex-thread-1" {
		t.Fatalf("codexSessionIDFromEvent() = %q", got)
	}
}

func TestRunWorkflowExprRequiresObjectResult(t *testing.T) {
	got, err := runWorkflowExpr(`{status: params.ok ? "passed" : "failed"}`, map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("runWorkflowExpr() error = %v", err)
	}
	if got["status"] != "passed" {
		t.Fatalf("runWorkflowExpr() = %#v", got)
	}
	if _, err := runWorkflowExpr(`params.ok`, map[string]any{"ok": true}); err == nil {
		t.Fatal("runWorkflowExpr() expected object result error")
	}
}

func TestSubmitWorkflowApprovalStartsNextCodexNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusWaitingApproval,
		WorktreePath: "/workspace/project-1",
	}
	nextNodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{
		approvalResult: domain.WorkflowApprovalResult{
			Run: domain.WorkflowRunSnapshot{
				ID:            "workflow-run-1",
				SessionID:     "session-1",
				Status:        "running",
				CurrentNodeID: "verify",
				Context:       map[string]any{"last": map[string]any{"status": "succeeded"}},
			},
			Advance: domain.WorkflowAdvance{
				WorkflowRunID:    "workflow-run-1",
				NodeRunID:        &nextNodeRunID,
				CurrentNodeID:    "verify",
				CurrentNodeTitle: "Verify",
				Status:           "running",
				RequiresCodex:    true,
				Prompt:           "Verify build",
			},
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-2"}}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithProcesses(processes, codex), WithEvents(events))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		if nextID == 3 {
			return "process-run-2", nil
		}
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	service.now = func() time.Time { return time.Unix(50, 0).UTC() }

	got, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		WorkflowRunID: "workflow-run-1",
		NodeID:        "approve",
		Approved:      true,
		Comment:       "looks good",
	})
	if err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	if got.ID != "workflow-run-1" || got.Status != "running" || got.CurrentNodeID != "verify" {
		t.Fatalf("SubmitWorkflowApproval() = %#v", got)
	}
	if workflows.approvalInput.WorkflowRunID != "workflow-run-1" || workflows.approvalInput.NodeID != "approve" || !workflows.approvalInput.Approved {
		t.Fatalf("approval input = %#v", workflows.approvalInput)
	}
	if repo.sessions["session-1"].Status != domain.StatusQueued || repo.sessions["session-1"].CodexSessionID != "" {
		t.Fatalf("session after approval = %#v", repo.sessions["session-1"])
	}
	if len(processes.created) != 0 {
		t.Fatalf("process runs = %#v", processes.created)
	}
	if codex.startCalled || repo.sessions["session-1"].Queue.NodeRunID == nil || *repo.sessions["session-1"].Queue.NodeRunID != "node-run-2" || repo.sessions["session-1"].Queue.Prompt != "Verify build" {
		t.Fatalf("queued session = %#v codexCalled=%v", repo.sessions["session-1"], codex.startCalled)
	}
}

func TestSubmitWorkflowApprovalRejectBlocksSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      domain.ModeWorkflow,
		Status:    domain.StatusWaitingApproval,
	}
	workflows := &fakeWorkflowStarter{
		approvalResult: domain.WorkflowApprovalResult{
			Run: domain.WorkflowRunSnapshot{
				ID:            "workflow-run-1",
				SessionID:     "session-1",
				Status:        "blocked",
				CurrentNodeID: "approve",
				Context:       map[string]any{"blockedReason": "approval rejected"},
			},
			Advance: domain.WorkflowAdvance{
				WorkflowRunID: "workflow-run-1",
				Status:        "blocked",
				Blocked:       true,
				BlockedReason: "approval rejected",
			},
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithProcesses(processes, codex), WithEvents(events))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	service.now = func() time.Time { return time.Unix(51, 0).UTC() }

	got, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		WorkflowRunID: "workflow-run-1",
		NodeID:        "approve",
		Approved:      false,
		Comment:       "needs changes",
	})
	if err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	if got.Status != "blocked" || repo.sessions["session-1"].Status != domain.StatusBlocked {
		t.Fatalf("approval result = %#v session=%#v", got, repo.sessions["session-1"])
	}
	if codex.startCalled || len(processes.created) != 0 {
		t.Fatalf("codex/process should not start: start=%v runs=%#v", codex.startCalled, processes.created)
	}
}

func TestCreateWorkflowSessionAppliesWorkflowFailureWhenCodexStartFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		Path:              projectdomain.ProjectPath{Value: "/workspace/project-1"},
		DefaultWorkflowID: &workflowID,
	}
	nodeRunID := domain.NodeRunID("node-run-1")
	workflows := &fakeWorkflowStarter{start: domain.WorkflowStart{
		WorkflowRunID: "workflow-run-1",
		NodeRunID:     &nodeRunID,
		Status:        "running",
		RequiresCodex: true,
		Prompt:        "Run workflow node",
	}, failAdvance: domain.WorkflowAdvance{
		WorkflowRunID: "workflow-run-1",
		Status:        "blocked",
		Blocked:       true,
		BlockedReason: "workflow node failed",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startErr: errors.New("codex rejected params")}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex))
	ids := []domain.ID{"session-1", "process-run-1", "event-1", "event-2", "event-3"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "ship feature",
		Mode:        domain.ModeWorkflow,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued {
		t.Fatalf("CreateSession() status = %q", got.Status)
	}
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if repo.sessions["session-1"].Status != domain.StatusBlocked {
		t.Fatalf("session status after drain = %q", repo.sessions["session-1"].Status)
	}
	if workflows.failInput.WorkflowRunID != "workflow-run-1" || workflows.failInput.NodeRunID != "node-run-1" {
		t.Fatalf("workflow fail input = %#v", workflows.failInput)
	}
	if workflows.failInput.Code != "codex_start_failed" || !strings.Contains(workflows.failInput.Message, "codex rejected params") {
		t.Fatalf("workflow fail input = %#v", workflows.failInput)
	}
}

func TestCreateWorkflowSessionRetriesWhenCodexStartFailsBeforeMaxAttempts(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		Name:              "project-1",
		Path:              projectdomain.ProjectPath{Value: "/workspace/project-1"},
		DefaultWorkflowID: &workflowID,
	}
	firstNodeRunID := domain.NodeRunID("node-run-1")
	secondNodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			WorkflowRunID: "workflow-run-1",
			NodeRunID:     &firstNodeRunID,
			Status:        "running",
			RequiresCodex: true,
			Prompt:        "Run workflow node",
		},
		failAdvance: domain.WorkflowAdvance{
			WorkflowRunID:    "workflow-run-1",
			NodeRunID:        &secondNodeRunID,
			CurrentNodeID:    "build",
			CurrentNodeTitle: "Build",
			Status:           "running",
			RequiresCodex:    true,
			Prompt:           "Retry workflow node",
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{
		startErrs: []error{errors.New("temporary codex failure"), nil},
		startHandles: []processdomain.CodexHandle{
			{PID: 222, CodexSessionID: "codex-session-2"},
		},
	}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex))
	ids := []domain.ID{"session-1", "process-run-1", "process-event-1", "process-run-2"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "ship feature",
		Mode:        domain.ModeWorkflow,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued {
		t.Fatalf("CreateSession() = %#v", got)
	}
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if repo.sessions["session-1"].Status != domain.StatusRunning || repo.sessions["session-1"].CodexSessionID != "codex-session-2" {
		t.Fatalf("session after retry = %#v", repo.sessions["session-1"])
	}
	if workflows.failInput.NodeRunID != "node-run-1" || workflows.failInput.Code != "codex_start_failed" {
		t.Fatalf("workflow fail input = %#v", workflows.failInput)
	}
	if len(processes.created) != 2 {
		t.Fatalf("process runs = %#v", processes.created)
	}
	if processes.created[0].NodeRunID == nil || *processes.created[0].NodeRunID != "node-run-1" {
		t.Fatalf("first process run = %#v", processes.created[0])
	}
	if processes.created[1].NodeRunID == nil || *processes.created[1].NodeRunID != "node-run-2" {
		t.Fatalf("second process run = %#v", processes.created[1])
	}
	if len(codex.startInputs) != 2 || codex.startInputs[1].Prompt != promptWithAnswerUserGuidance("Retry workflow node") {
		t.Fatalf("codex start inputs = %#v", codex.startInputs)
	}
}

func TestGetSessionDerivesCurrentNodeTitleFromLatestWorkflowEvent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      domain.ModeWorkflow,
		Status:    domain.StatusWaitingApproval,
	}
	events := &fakeEventStore{}
	sessionID := eventdomain.SessionID("session-1")
	if err := events.Append(ctx, eventdomain.DomainEvent{
		ID:        "event-1",
		Scope:     eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID},
		SessionID: &sessionID,
		Type:      "workflow.started",
		Payload:   map[string]any{"currentNodeTitle": "Codex 执行"},
		CreatedAt: time.Unix(10, 0).UTC(),
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	if err := events.Append(ctx, eventdomain.DomainEvent{
		ID:        "event-2",
		Scope:     eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID},
		SessionID: &sessionID,
		Type:      "workflow.waiting_approval",
		Payload:   map[string]any{"currentNodeTitle": "验证构建结果"},
		CreatedAt: time.Unix(11, 0).UTC(),
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events))

	got, err := service.GetSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.CurrentNodeTitle != "验证构建结果" {
		t.Fatalf("CurrentNodeTitle = %q", got.CurrentNodeTitle)
	}
}

func TestCreateSessionMarksSessionFailedWhenWorktreeCreateFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{createErr: errors.New("worktree failed")}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	if _, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
		BaseBranch:  "main",
	}); err == nil {
		t.Fatal("CreateSession() expected worktree error")
	}
	got := repo.sessions["session-1"]
	if got.Status != domain.StatusFailed {
		t.Fatalf("session status after worktree failure = %q, saved=%#v", got.Status, repo.saved)
	}
}

func TestCreateSessionArchivesStagedAttachments(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:        "staged-1",
		Filename:  "note.txt",
		Path:      "/attachments/staged/staged-1/note.txt",
		MimeType:  "text/plain",
		Size:      5,
		CreatedAt: time.Unix(9, 0).UTC(),
	}
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	if _, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:           "project-1",
		Requirement:         "use attachment",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, ok := repo.stagedAttachments["staged-1"]; ok {
		t.Fatal("staged attachment metadata was not deleted")
	}
	attachment, ok := repo.sessionAttachments["staged-1"]
	if !ok {
		t.Fatal("session attachment metadata was not saved")
	}
	if attachment.SessionID != "session-1" || attachment.Filename != "note.txt" || attachment.SourceType != domain.AttachmentSourceRequirement || attachment.SourceID != "session-1" {
		t.Fatalf("session attachment = %#v", attachment)
	}
	if !files.promoted["staged-1"] {
		t.Fatal("staged attachment file was not promoted")
	}
}

func TestCreateSessionMarksFailedWhenAttachmentArchiveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
	}
	files := newFakeAttachmentStore()
	files.promoteErr = errors.New("disk failed")
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	if _, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:           "project-1",
		Requirement:         "use attachment",
		BaseBranch:          "main",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err == nil {
		t.Fatal("CreateSession() expected attachment archive error")
	}
	got, err := repo.Find(ctx, "session-1")
	if err != nil {
		t.Fatalf("Find() session after archive failure: %v", err)
	}
	if got.Status != domain.StatusFailed {
		t.Fatalf("session status after archive failure = %q", got.Status)
	}
}

func TestCreateSessionRemovesCreatedWorktreeWhenAttachmentArchiveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	files := newFakeAttachmentStore()
	files.promoteErr = errors.New("disk failed")
	worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1"}
	service := New(repo, projects, WithAttachments(repo, files), WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	if _, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:           "project-1",
		Requirement:         "use attachment",
		BaseBranch:          "main",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err == nil {
		t.Fatal("CreateSession() expected attachment archive error")
	}
	got, err := repo.Find(ctx, "session-1")
	if err != nil {
		t.Fatalf("Find() session after archive failure: %v", err)
	}
	if got.Status != domain.StatusFailed {
		t.Fatalf("session status after archive failure = %q", got.Status)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	if !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1"}) {
		t.Fatalf("deleted branches = %#v", worktrees.deletedBranches)
	}
}

func TestCreateSessionReturnsWorktreeCleanupErrorWhenRollbackBranchDeleteFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	files := newFakeAttachmentStore()
	files.promoteErr = errors.New("disk failed")
	worktrees := &fakeWorktreeManager{
		path:            "/data/worktrees/project-1/session-1",
		deleteBranchErr: errors.New("delete branch failed"),
	}
	service := New(repo, projects, WithAttachments(repo, files), WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	_, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:           "project-1",
		Requirement:         "use attachment",
		BaseBranch:          "main",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	})
	if err == nil {
		t.Fatal("CreateSession() expected attachment archive and cleanup error")
	}
	if !strings.Contains(err.Error(), "disk failed") || !strings.Contains(err.Error(), "cleanup created worktree") || !strings.Contains(err.Error(), "delete branch failed") {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	if !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1"}) {
		t.Fatalf("deleted branches = %#v", worktrees.deletedBranches)
	}
}

func TestListSessionsReturnsCardsPage(t *testing.T) {
	ctx := context.Background()
	projectID := domain.ProjectID("project-1")
	repo := newFakeRepository()
	repo.listSessions = []domain.Session{
		{ID: "created", ProjectID: projectID, Requirement: "created card", Status: domain.StatusCreated},
		{ID: "running", ProjectID: projectID, Requirement: "running card", Status: domain.StatusRunning},
	}
	repo.listTotal = 7
	repo.sessionAttachments["attachment-1"] = domain.SessionAttachment{
		ID:        "attachment-1",
		SessionID: "created",
		Filename:  "note.txt",
	}
	projects := newFakeProjectRepository("project-1")
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Name: "Project One"}
	service := New(repo, projects, WithAttachments(repo, newFakeAttachmentStore()))

	got, err := service.ListSessions(ctx, ListSessionsInput{
		ProjectID: &projectID,
		Filter:    "card",
		Sort:      "updated_desc",
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if got.Page != 1 || got.PageSize != 20 || got.Total != 7 {
		t.Fatalf("ListSessions() page = %#v", got)
	}
	if repo.lastListQuery.ProjectID == nil || *repo.lastListQuery.ProjectID != projectID {
		t.Fatalf("ListSessions() query project = %#v", repo.lastListQuery.ProjectID)
	}
	if got.Items[0].RequirementSummary != "created card" {
		t.Fatalf("RequirementSummary = %q", got.Items[0].RequirementSummary)
	}
	if got.Items[0].ProjectName != "Project One" {
		t.Fatalf("ProjectName = %q", got.Items[0].ProjectName)
	}
	if len(got.Items[0].Attachments) != 1 || got.Items[0].Attachments[0].Filename != "note.txt" {
		t.Fatalf("ListSessions() attachments = %#v", got.Items[0].Attachments)
	}
	if !slices.Equal(got.Items[0].AvailableActions, []string{"run", "close"}) {
		t.Fatalf("created actions = %#v", got.Items[0].AvailableActions)
	}
	if !slices.Equal(got.Items[1].AvailableActions, []string{"stop"}) {
		t.Fatalf("running actions = %#v", got.Items[1].AvailableActions)
	}
}

func TestGetSessionReturnsDetailWithResumeAction(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "resume this",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-1", SessionID: "session-1", Body: "extra context", CreatedAt: time.Unix(11, 0).UTC()},
	}
	repo.sessionAttachments["attachment-1"] = domain.SessionAttachment{
		ID:        "attachment-1",
		SessionID: "session-1",
		Filename:  "note.txt",
		MimeType:  "text/plain",
		Size:      5,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, newFakeAttachmentStore()))

	got, err := service.GetSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if !got.CanResume {
		t.Fatal("GetSession() CanResume = false")
	}
	if len(got.Attachments) != 1 || len(got.PromptAppends) != 1 {
		t.Fatalf("GetSession() detail collections, got attachments=%d appends=%d", len(got.Attachments), len(got.PromptAppends))
	}
	if got.Attachments[0].Filename != "note.txt" {
		t.Fatalf("GetSession() attachments = %#v", got.Attachments)
	}
	if got.PromptAppends[0].Body != "extra context" {
		t.Fatalf("GetSession() prompt appends = %#v", got.PromptAppends)
	}
	if !slices.Equal(got.AvailableActions, []string{"run", "resume", "close"}) {
		t.Fatalf("GetSession() actions = %#v", got.AvailableActions)
	}
}

func TestSetSessionPriorityNormalizesAndSaves(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusCreated,
		Priority:  domain.PriorityLow,
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-priority", nil }

	got, err := service.SetSessionPriority(ctx, SetSessionPriorityInput{
		SessionID: "session-1",
		Priority:  "high",
	})
	if err != nil {
		t.Fatalf("SetSessionPriority() error = %v", err)
	}
	if got.Priority != domain.PriorityHigh {
		t.Fatalf("Priority = %q, want %q", got.Priority, domain.PriorityHigh)
	}
	if repo.sessions["session-1"].Priority != domain.PriorityHigh {
		t.Fatalf("saved priority = %q", repo.sessions["session-1"].Priority)
	}
	gotEvents := events.snapshot()
	if len(gotEvents) != 1 || gotEvents[0].Type != "session.priority_changed" || gotEvents[0].Payload["priority"] != string(domain.PriorityHigh) {
		t.Fatalf("events = %#v", gotEvents)
	}
}

func TestAppendPromptValidatesAndPersists(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingApproval,
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	got, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID: "session-1",
		Body:      "  continue with tests  ",
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if got.ID != "append-1" || got.SessionID != "session-1" || got.Body != "continue with tests" {
		t.Fatalf("AppendPrompt() DTO = %#v", got)
	}
	if len(repo.appends) != 1 {
		t.Fatalf("appends = %d", len(repo.appends))
	}
	if repo.appends[0].Body != "continue with tests" {
		t.Fatalf("persisted append = %#v", repo.appends[0])
	}

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "   "}); err == nil {
		t.Fatal("AppendPrompt() expected content error")
	}
}

func TestAppendPromptArchivesStagedAttachmentsWithBody(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingApproval,
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:          "staged-1",
		Filename:    "notes.txt",
		Path:        "/attachments/staged/staged-1/notes.txt",
		MimeType:    "text/plain",
		Size:        12,
		Previewable: false,
	}
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	got, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID:           "session-1",
		Body:                "   ",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if got.Body != "追加附件" || repo.appends[0].Body != "追加附件" {
		t.Fatalf("attachment-only append body = DTO %q persisted %q", got.Body, repo.appends[0].Body)
	}
	if !files.promoted["staged-1"] {
		t.Fatalf("staged attachment was not promoted")
	}
	if _, ok := repo.stagedAttachments["staged-1"]; ok {
		t.Fatalf("staged attachment was not deleted")
	}
	attachment, ok := repo.sessionAttachments["staged-1"]
	if !ok {
		t.Fatalf("session attachment was not saved")
	}
	if attachment.SessionID != "session-1" || attachment.Filename != "notes.txt" {
		t.Fatalf("session attachment = %#v", attachment)
	}
}

func TestAppendPromptArchivesStagedAttachments(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingApproval,
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:        "staged-1",
		Filename:  "note.txt",
		Path:      "/attachments/staged/staged-1/note.txt",
		MimeType:  "text/plain",
		Size:      5,
		CreatedAt: time.Unix(9, 0).UTC(),
	}
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	got, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID:           "session-1",
		Body:                "continue with attachment",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].ID != "staged-1" {
		t.Fatalf("AppendPrompt() attachments = %#v", got.Attachments)
	}
	if _, ok := repo.stagedAttachments["staged-1"]; ok {
		t.Fatal("staged attachment metadata was not deleted")
	}
	attachment, ok := repo.sessionAttachments["staged-1"]
	if !ok {
		t.Fatal("session attachment metadata was not saved")
	}
	if attachment.SessionID != "session-1" || attachment.Filename != "note.txt" {
		t.Fatalf("session attachment = %#v", attachment)
	}
	if !files.promoted["staged-1"] {
		t.Fatal("staged attachment file was not promoted")
	}
}

func TestAppendPromptAllowsAttachmentOnlyAppend(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingApproval,
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
		Path:     "/attachments/staged/staged-1/note.txt",
		MimeType: "text/plain",
		Size:     5,
	}
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	got, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID:           "session-1",
		Body:                "   ",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if got.Body != "追加附件" {
		t.Fatalf("AppendPrompt() body = %q", got.Body)
	}
	attachment, ok := repo.sessionAttachments["staged-1"]
	if !ok {
		t.Fatal("session attachment metadata was not saved")
	}
	if attachment.SourceType != domain.AttachmentSourcePromptAppend || attachment.SourceID != "append-1" {
		t.Fatalf("session attachment = %#v", attachment)
	}
	if !files.promoted["staged-1"] {
		t.Fatal("staged attachment file was not promoted")
	}
}

func TestAppendPromptRollsBackArchivedAttachmentsWhenAppendSaveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingApproval,
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
		Path:     "/attachments/staged/staged-1/note.txt",
		MimeType: "text/plain",
		Size:     5,
	}
	repo.appendPromptErr = errors.New("append save failed")
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID:           "session-1",
		Body:                "continue with attachment",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err == nil {
		t.Fatal("AppendPrompt() expected append save error")
	}
	if _, ok := repo.sessionAttachments["staged-1"]; ok {
		t.Fatal("session attachment metadata was not rolled back")
	}
	if !files.deletedSessions["staged-1"] {
		t.Fatal("session attachment file was not rolled back")
	}
	if len(repo.appends) != 0 {
		t.Fatalf("appends = %#v, want none", repo.appends)
	}
}

func TestAppendPromptRollsBackArchivedAttachmentsWhenStagedDeleteFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingApproval,
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
		Path:     "/attachments/staged/staged-1/note.txt",
		MimeType: "text/plain",
		Size:     5,
	}
	repo.deleteStagedAttachmentErr = errors.New("delete staged failed")
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID:           "session-1",
		Body:                "continue with attachment",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err == nil {
		t.Fatal("AppendPrompt() expected staged delete error")
	}
	if _, ok := repo.sessionAttachments["staged-1"]; ok {
		t.Fatal("session attachment metadata was not rolled back")
	}
	if !files.deletedSessions["staged-1"] {
		t.Fatal("session attachment file was not rolled back")
	}
}

func TestAppendPromptRollbackIgnoresRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingApproval,
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
		Path:     "/attachments/staged/staged-1/note.txt",
		MimeType: "text/plain",
		Size:     5,
	}
	repo.appendPromptHook = cancel
	repo.appendPromptErr = errors.New("append save failed")
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID:           "session-1",
		Body:                "continue with attachment",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err == nil {
		t.Fatal("AppendPrompt() expected append save error")
	}
	if _, ok := repo.sessionAttachments["staged-1"]; ok {
		t.Fatal("session attachment metadata was not rolled back")
	}
	if !files.deletedSessions["staged-1"] {
		t.Fatal("session attachment file was not rolled back")
	}
}

func TestAppendPromptRollsBackArchivedAttachmentsWhenAutoResumeFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeChat,
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "note.txt",
		Path:     "/attachments/staged/staged-1/note.txt",
		MimeType: "text/plain",
		Size:     5,
	}
	repo.saveErr = errors.New("queue save failed")
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID:           "session-1",
		Body:                "continue with attachment",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	}); err == nil {
		t.Fatal("AppendPrompt() expected auto resume error")
	}
	if _, ok := repo.sessionAttachments["staged-1"]; ok {
		t.Fatal("session attachment metadata was not rolled back")
	}
	if !files.deletedSessions["staged-1"] {
		t.Fatal("session attachment file was not rolled back")
	}
	if len(repo.appends) != 0 {
		t.Fatalf("appends = %#v, want none", repo.appends)
	}
	if len(repo.deletedAppends) != 1 || repo.deletedAppends[0] != "append-1" {
		t.Fatalf("deleted appends = %#v", repo.deletedAppends)
	}
}

func TestAppendPromptRejectsClosedSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusClosed,
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "continue"}); err == nil {
		t.Fatal("AppendPrompt() expected closed session error")
	}
	if len(repo.appends) != 0 {
		t.Fatalf("closed session appends = %d, want 0", len(repo.appends))
	}
}

func TestUpdateSessionConfigPersistsTrimmedConfig(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusStopped,
		Config: domain.Config{
			CodexModel:      "gpt-old",
			ReasoningEffort: "low",
			PermissionMode:  "read-only",
		},
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(25, 0).UTC() }

	got, err := service.UpdateSessionConfig(ctx, UpdateSessionConfigInput{
		SessionID: "session-1",
		Config: domain.Config{
			CodexModel:      " gpt-5.4-mini ",
			ReasoningEffort: " high ",
			PermissionMode:  " workspace-write ",
		},
	})
	if err != nil {
		t.Fatalf("UpdateSessionConfig() error = %v", err)
	}
	want := domain.Config{
		CodexModel:      "gpt-5.4-mini",
		ReasoningEffort: "high",
		PermissionMode:  "workspace-write",
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("Config = %#v, want %#v", got.Config, want)
	}
	if !reflect.DeepEqual(repo.sessions["session-1"].Config, want) {
		t.Fatalf("saved config = %#v, want %#v", repo.sessions["session-1"].Config, want)
	}
	if !repo.sessions["session-1"].UpdatedAt.Equal(time.Unix(25, 0).UTC()) {
		t.Fatalf("UpdatedAt = %v", repo.sessions["session-1"].UpdatedAt)
	}
}

func TestAppendPromptAutoStartsStoppedChatSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Mode:         domain.ModeChat,
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/session-1",
		Config: domain.Config{
			CodexModel:      "gpt-test",
			ReasoningEffort: "medium",
			PermissionMode:  "workspace-write",
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, nil), WithProcesses(processes, codex))
	ids := []domain.ID{"append-1", "process-run-1", "event-starting", "event-running"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID: "session-1",
		Body:      "  continue with tests  ",
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if got.ID != "append-1" || got.Body != "continue with tests" {
		t.Fatalf("AppendPrompt() DTO = %#v", got)
	}
	if repo.sessions["session-1"].Status != domain.StatusQueued {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
	if len(processes.created) != 0 {
		t.Fatalf("process runs = %#v", processes.created)
	}
	if !strings.Contains(repo.sessions["session-1"].Queue.Prompt, "implement session") || !strings.Contains(repo.sessions["session-1"].Queue.Prompt, "追加描述：\ncontinue with tests") {
		t.Fatalf("queued prompt = %q", repo.sessions["session-1"].Queue.Prompt)
	}
}

func TestAppendPromptResumesStoppedChatSessionWithOnlyNewBody(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "original requirement",
		Mode:           domain.ModeChat,
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-0", SessionID: "session-1", Body: "old context", CreatedAt: time.Unix(10, 0).UTC()},
	}
	repo.sessionAttachments["attachment-1"] = domain.SessionAttachment{
		ID:        "attachment-1",
		SessionID: "session-1",
		Filename:  "notes.md",
		Path:      "/data/attachments/sessions/session-1/notes.md",
		MimeType:  "text/markdown",
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "new-note.md",
		Path:     "/attachments/staged/staged-1/new-note.md",
		MimeType: "text/markdown",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files), WithProcesses(processes, codex))
	ids := []domain.ID{"append-1", "process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	_, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID:           "session-1",
		Body:                "  only this new instruction  ",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}

	saved := repo.sessions["session-1"]
	if saved.Status != domain.StatusQueued || saved.Queue.Kind != domain.QueueKindResume {
		t.Fatalf("queued session = %#v", saved)
	}
	if saved.Queue.ResumeCodexSessionID != "codex-session-1" {
		t.Fatalf("resume codex session id = %q", saved.Queue.ResumeCodexSessionID)
	}
	newPath := "/attachments/sessions/session-1/staged-1/new-note.md"
	if saved.Queue.Prompt != "only this new instruction\n\nAttached files available on disk:\n- "+newPath {
		t.Fatalf("resume prompt = %q", saved.Queue.Prompt)
	}
	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if !codex.resumeCalled || codex.startCalled {
		t.Fatalf("codex calls start=%v resume=%v", codex.startCalled, codex.resumeCalled)
	}
	if !files.promoted["staged-1"] {
		t.Fatal("staged attachment file was not promoted")
	}
	if codex.resumeInput.Prompt != promptWithAnswerUserGuidance("only this new instruction\n\nAttached files available on disk:\n- "+newPath) {
		t.Fatalf("codex resume prompt = %q", codex.resumeInput.Prompt)
	}
	if strings.Contains(codex.resumeInput.Prompt, "/data/attachments/sessions/session-1/notes.md") {
		t.Fatalf("codex resume prompt should not include old attachment path: %q", codex.resumeInput.Prompt)
	}
	if repo.lastPromptAppendAttachmentSessionID != "session-1" || repo.lastPromptAppendAttachmentID != "append-1" {
		t.Fatalf("prompt append attachment query session=%q append=%q", repo.lastPromptAppendAttachmentSessionID, repo.lastPromptAppendAttachmentID)
	}
}

func TestAppendPromptResumesStoredCodexSessionID(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "original requirement",
		Mode:           domain.ModeChat,
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-current",
		WorktreePath:   "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-current"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	ids := []domain.ID{"append-1", "process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	_, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID: "session-1",
		Body:      "use latest thread",
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}

	saved := repo.sessions["session-1"]
	if saved.CodexSessionID != "codex-session-current" {
		t.Fatalf("CodexSessionID = %q", saved.CodexSessionID)
	}
	if saved.Queue.Kind != domain.QueueKindResume || saved.Queue.ResumeCodexSessionID != "codex-session-current" {
		t.Fatalf("queued session = %#v", saved)
	}
	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if !codex.resumeCalled || codex.resumeInput.CodexSessionID != "codex-session-current" {
		t.Fatalf("resume input = %#v", codex.resumeInput)
	}
}

func TestAppendPromptRebuildsPromptWithReviewNoticeWhenNotResuming(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "original requirement",
		Mode:         domain.ModeChat,
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-0", SessionID: "session-1", Body: "old context", CreatedAt: time.Unix(10, 0).UTC()},
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	_, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID: "session-1",
		Body:      "new instruction",
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}

	prompt := repo.sessions["session-1"].Queue.Prompt
	for _, want := range []string{
		"无法复用已有 Codex 会话",
		"复查",
		"原始需求：\noriginal requirement",
		"追加描述：\nold context",
		"追加描述：\nnew instruction",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("queued prompt missing %q: %q", want, prompt)
		}
	}
}

func TestAppendPromptRebuiltStartSendsPromptToCodexOnce(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "original requirement",
		Mode:         domain.ModeChat,
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-0", SessionID: "session-1", Body: "old context", CreatedAt: time.Unix(10, 0).UTC()},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-new"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	ids := []domain.ID{"append-1", "process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	_, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID: "session-1",
		Body:      "new instruction",
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}

	prompt := codex.startInput.Prompt
	if strings.Count(prompt, "无法复用已有 Codex 会话") != 1 {
		t.Fatalf("codex prompt should contain one review notice: %q", prompt)
	}
	if strings.Contains(prompt, "当前流程节点提示词") {
		t.Fatalf("chat rebuilt prompt should not wrap itself as node prompt: %q", prompt)
	}
	for _, want := range []string{
		"原始需求：\noriginal requirement",
		"追加描述：\nold context",
		"追加描述：\nnew instruction",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("codex prompt missing %q: %q", want, prompt)
		}
	}
}

func TestAppendPromptRebuildsResumeFailedChatSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "original requirement",
		Mode:           domain.ModeChat,
		Status:         domain.StatusResumeFailed,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-0", SessionID: "session-1", Body: "old context", CreatedAt: time.Unix(10, 0).UTC()},
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	_, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID: "session-1",
		Body:      "new instruction",
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}

	saved := repo.sessions["session-1"]
	if saved.Status != domain.StatusQueued || saved.Queue.Kind != domain.QueueKindStart || saved.Queue.ResumeCodexSessionID != "" {
		t.Fatalf("queued session = %#v", saved)
	}
	for _, want := range []string{
		"无法复用已有 Codex 会话",
		"原始需求：\noriginal requirement",
		"追加描述：\nold context",
		"追加描述：\nnew instruction",
	} {
		if !strings.Contains(saved.Queue.Prompt, want) {
			t.Fatalf("queued prompt missing %q: %q", want, saved.Queue.Prompt)
		}
	}
}

func TestCloseSessionMarksClosedAndDefaultsReason(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusCreated,
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() status = %q", got.Status)
	}
	saved := repo.sessions["session-1"]
	if saved.CloseReason == nil || *saved.CloseReason != domain.CloseReasonUserClosed {
		t.Fatalf("CloseSession() reason = %#v", saved.CloseReason)
	}
	if saved.ClosedAt == nil || !saved.ClosedAt.Equal(time.Unix(30, 0).UTC()) {
		t.Fatalf("CloseSession() ClosedAt = %#v", saved.ClosedAt)
	}
}

func TestCloseSessionRemovesWorktreeBeforeSavingClosed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:                 "session-1",
		ProjectID:          "project-1",
		Status:             domain.StatusCreated,
		BaseBranch:         "main",
		WorktreePath:       "/data/worktrees/project-1/session-1",
		WorktreeBaseCommit: "base",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{headCommit: "head"}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() status = %q", got.Status)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	if !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1"}) {
		t.Fatalf("deleted branches = %#v", worktrees.deletedBranches)
	}
	if worktrees.snapshotPath != "/data/worktrees/project-1/session-1" {
		t.Fatalf("SnapshotCommit() path = %q", worktrees.snapshotPath)
	}
	if worktrees.snapshotBranch != "session-1" {
		t.Fatalf("SnapshotCommit() branch = %q", worktrees.snapshotBranch)
	}
	if got.WorktreeBaseCommit != "base" || got.WorktreeHeadCommit != "head" {
		t.Fatalf("closed session commits = base %q head %q", got.WorktreeBaseCommit, got.WorktreeHeadCommit)
	}
}

func TestCloseSessionDoesNotRemoveWorktreeWhenSnapshotSaveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:                 "session-1",
		ProjectID:          "project-1",
		Status:             domain.StatusCreated,
		BaseBranch:         "main",
		WorktreePath:       "/data/worktrees/project-1/session-1",
		WorktreeBaseCommit: "base",
	}
	repo.saveHook = func(_ domain.Session) error {
		return errors.New("snapshot save failed")
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{headCommit: "head"}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected snapshot save error")
	}
	if len(worktrees.removed) != 0 {
		t.Fatalf("Remove() should not be called, got %#v", worktrees.removed)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreeHeadCommit != "" || saved.Status == domain.StatusClosed {
		t.Fatalf("session should remain unchanged after snapshot save failure: %#v", saved)
	}
}

func TestCloseSessionKeepsStoredSnapshotWhenFinalSaveFailsAfterWorktreeCleanup(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:                 "session-1",
		ProjectID:          "project-1",
		Status:             domain.StatusCreated,
		BaseBranch:         "main",
		WorktreePath:       "/data/worktrees/project-1/session-1",
		WorktreeBaseCommit: "base",
	}
	saveCalls := 0
	repo.saveHook = func(session domain.Session) error {
		saveCalls++
		if saveCalls == 2 {
			if session.WorktreePath != "" {
				t.Fatalf("second save WorktreePath = %q, want empty", session.WorktreePath)
			}
			if session.Status == domain.StatusClosed {
				t.Fatalf("second save should clear worktree before closed status: %#v", session)
			}
			return errors.New("clear worktree path failed")
		}
		return nil
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{headCommit: "head"}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected clear path save error")
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreeBaseCommit != "base" || saved.WorktreeHeadCommit != "head" {
		t.Fatalf("snapshot should remain stored after clear path save failure: %#v", saved)
	}
	if saved.WorktreePath != "/data/worktrees/project-1/session-1" {
		t.Fatalf("worktree path should remain stale until retry clears it: %#v", saved)
	}
	if saved.Status == domain.StatusClosed {
		t.Fatalf("session should not be marked closed when clear path save fails: %#v", saved)
	}
	if len(repo.saved) != 1 {
		t.Fatalf("Save() calls = %#v", repo.saved)
	}
	if repo.saved[0].WorktreePath != "/data/worktrees/project-1/session-1" {
		t.Fatalf("saved worktree path = %q", repo.saved[0].WorktreePath)
	}
}

func TestCloseSessionCanRetryAfterWorktreeRemovedAndSnapshotStored(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:                 "session-1",
		ProjectID:          "project-1",
		Status:             domain.StatusCreated,
		BaseBranch:         "main",
		WorktreePath:       "/data/worktrees/project-1/session-1",
		WorktreeBaseCommit: "base",
	}
	saveCalls := 0
	repo.saveHook = func(session domain.Session) error {
		saveCalls++
		if saveCalls == 2 {
			return errors.New("clear worktree path failed")
		}
		return nil
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := newFakeWorktreeManager()
	worktrees.headCommit = "head"
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected first clear path save error")
	}
	savedAfterFirst := repo.sessions["session-1"]
	if savedAfterFirst.WorktreePath != "/data/worktrees/project-1/session-1" {
		t.Fatalf("saved worktree path after first failure = %q", savedAfterFirst.WorktreePath)
	}
	if savedAfterFirst.WorktreeBaseCommit != "base" || savedAfterFirst.WorktreeHeadCommit != "head" {
		t.Fatalf("saved snapshot after first failure = %#v", savedAfterFirst)
	}

	repo.saveHook = nil
	worktrees.resetCallState()
	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() retry error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() retry status = %q", got.Status)
	}
	if got.WorktreePath != "" {
		t.Fatalf("CloseSession() retry WorktreePath = %q, want empty", got.WorktreePath)
	}
	if worktrees.headCommitPath != "" || worktrees.mergeBasePath != "" {
		t.Fatalf("retry should not read commits from missing worktree: head=%q mergeBase=%q", worktrees.headCommitPath, worktrees.mergeBasePath)
	}
	if len(worktrees.removed) != 0 {
		t.Fatalf("retry should not remove missing worktree again: %#v", worktrees.removed)
	}
	if !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1"}) {
		t.Fatalf("deleted branches on retry = %#v", worktrees.deletedBranches)
	}
	savedAfterRetry := repo.sessions["session-1"]
	if savedAfterRetry.Status != domain.StatusClosed {
		t.Fatalf("saved status after retry = %q", savedAfterRetry.Status)
	}
	if savedAfterRetry.WorktreePath != "" {
		t.Fatalf("saved WorktreePath after retry = %q", savedAfterRetry.WorktreePath)
	}
}

func TestCloseSessionFailsWhenMissingWorktreeSnapshotIncomplete(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:                 "session-1",
		ProjectID:          "project-1",
		Status:             domain.StatusCreated,
		BaseBranch:         "main",
		WorktreePath:       "/data/worktrees/project-1/session-1",
		WorktreeBaseCommit: "base",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := newFakeWorktreeManager()
	worktrees.setMissing("/data/worktrees/project-1/session-1", true)
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected missing worktree snapshot error")
	} else {
		appErr, ok := apperror.From(err)
		if !ok || appErr.Code != apperror.CodeCloseFailed {
			t.Fatalf("CloseSession() error = %#v", err)
		}
	}
	if worktrees.headCommitPath != "" || worktrees.mergeBasePath != "" {
		t.Fatalf("missing worktree should fail before rereading commits: head=%q mergeBase=%q", worktrees.headCommitPath, worktrees.mergeBasePath)
	}
	if len(worktrees.removed) != 0 {
		t.Fatalf("missing worktree should not remove again: %#v", worktrees.removed)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreePath != "/data/worktrees/project-1/session-1" || saved.Status == domain.StatusClosed {
		t.Fatalf("session should remain open with original path after incomplete snapshot failure: %#v", saved)
	}
}

func TestCloseSessionDoesNotSaveClosedWhenWorktreeRemoveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusCreated,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{headCommit: "head", mergeBase: "base", removeErr: errors.New("remove failed")}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected worktree remove error")
	}
	if repo.sessions["session-1"].Status == domain.StatusClosed {
		t.Fatalf("session should not be closed after worktree remove failure: %#v", repo.sessions["session-1"])
	}
	if len(repo.saved) != 1 {
		t.Fatalf("Save() calls = %#v", repo.saved)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreeBaseCommit != "base" || saved.WorktreeHeadCommit != "head" {
		t.Fatalf("snapshot should be stored before remove failure: %#v", saved)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
}

func TestCloseSessionCanRetryAfterBranchDeleteFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusCreated,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{deleteBranchErr: errors.New("delete branch failed")}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected delete branch error")
	}
	if repo.sessions["session-1"].Status != domain.StatusClosed {
		t.Fatalf("session should be closed after worktree removal: %#v", repo.sessions["session-1"])
	}
	if repo.sessions["session-1"].WorktreePath != "" {
		t.Fatalf("closed session worktree path = %q, want empty", repo.sessions["session-1"].WorktreePath)
	}
	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "continue"}); err == nil {
		t.Fatal("AppendPrompt() expected closed session error after cleanup failure")
	}

	worktrees.deleteBranchErr = nil
	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() retry error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() retry status = %q", got.Status)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	if !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1", "/workspace/project-1:session-1"}) {
		t.Fatalf("deleted branches = %#v", worktrees.deletedBranches)
	}
}

func TestCloseSessionUsesMergeBaseWhenStoredBaseCommitIsMissing(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusCreated,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := &fakeWorktreeManager{headCommit: "head", mergeBase: "base"}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.WorktreeBaseCommit != "base" || got.WorktreeHeadCommit != "head" {
		t.Fatalf("CloseSession() commits = base %q head %q", got.WorktreeBaseCommit, got.WorktreeHeadCommit)
	}
	if worktrees.mergeBasePath != "/data/worktrees/project-1/session-1" || worktrees.mergeBaseRef != "main" {
		t.Fatalf("MergeBase() path/ref = %q/%q", worktrees.mergeBasePath, worktrees.mergeBaseRef)
	}
	if worktrees.snapshotPath != "/data/worktrees/project-1/session-1" {
		t.Fatalf("SnapshotCommit() path = %q", worktrees.snapshotPath)
	}
	if worktrees.snapshotBranch != "session-1" {
		t.Fatalf("SnapshotCommit() branch = %q", worktrees.snapshotBranch)
	}
}

func TestCloseSessionWithoutWorktreeManagerStillCloses(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusCreated,
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() status = %q", got.Status)
	}
}

func TestCloseSessionCancelsPendingQuestionsAfterSavingClosed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingUser,
	}
	questions := &fakeQuestionCanceller{}
	service := New(repo, newFakeProjectRepository("project-1"), WithQuestions(questions))

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() status = %q", got.Status)
	}
	if questions.cancelledSessionID != "session-1" || questions.cancelReason != "session closed" {
		t.Fatalf("cancelled questions = %q %q", questions.cancelledSessionID, questions.cancelReason)
	}
	if repo.sessions["session-1"].Status != domain.StatusClosed {
		t.Fatalf("saved session before cancel = %#v", repo.sessions["session-1"])
	}
}

func TestCloseSessionWritesClosedEventAndClearsRemovedWorktree(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusCompleted,
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", IsGit: true}
	worktrees := &fakeWorktreeManager{}
	events := &fakeEventStore{}
	service := New(repo, projects, WithWorktrees(worktrees), WithEvents(events))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-closed", nil }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1", Reason: domain.CloseReasonMergedClosed})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreePath != "" {
		t.Fatalf("CloseSession() = %#v", got)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	gotEvents := events.snapshot()
	if len(gotEvents) != 1 || gotEvents[0].Type != "session.closed" || gotEvents[0].Payload["reason"] != string(domain.CloseReasonMergedClosed) {
		t.Fatalf("events = %#v", gotEvents)
	}
}

func TestCloseSessionStopsActiveProcessBeforeClosing(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	questions := &fakeQuestionCanceller{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithQuestions(questions))
	service.now = func() time.Time { return time.Unix(31, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() status = %q", got.Status)
	}
	if processes.exitedID != "process-run-1" || processes.exitedResult.FailureReason != "stopped by user" {
		t.Fatalf("process exit = %q %#v", processes.exitedID, processes.exitedResult)
	}
	if questions.cancelledSessionID != "session-1" || questions.cancelReason != "session closed" {
		t.Fatalf("cancelled questions = %q %q", questions.cancelledSessionID, questions.cancelReason)
	}
	saved := repo.sessions["session-1"]
	if saved.CloseReason == nil || *saved.CloseReason != domain.CloseReasonUserClosed {
		t.Fatalf("CloseReason = %#v", saved.CloseReason)
	}
}

func TestCloseQueuedAnswerUserStopsActiveProcessBeforeClosing(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(30, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusQueued,
		QueuedAt:  &queuedAt,
		Queue: domain.QueueIntent{
			Kind:     domain.QueueKindAnswerUser,
			Priority: domain.QueuePriorityImmediate,
		},
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusWaitingUser}
	processes.hasActive = true
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(31, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() status = %q", got.Status)
	}
	if codex.stoppedID != "process-run-1" {
		t.Fatalf("codex stopped process = %q", codex.stoppedID)
	}
	if processes.exitedID != "process-run-1" || processes.exitedResult.FailureReason != "stopped by user" {
		t.Fatalf("process exit = %q %#v", processes.exitedID, processes.exitedResult)
	}
	saved := repo.sessions["session-1"]
	if saved.Queue.Kind != "" || saved.QueuedAt != nil {
		t.Fatalf("closed session should clear queue: %#v", saved)
	}
}

func TestStartSessionCreatesProcessRunAndMarksRunning(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
		Config: domain.Config{
			CodexModel:      "gpt-test",
			ReasoningEffort: "medium",
			PermissionMode:  "workspace-write",
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	got, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusRunning {
		t.Fatalf("StartSession() status = %q", got.Status)
	}
	if len(processes.created) != 1 || processes.created[0].ID != "process-run-1" || processes.created[0].Status != processdomain.StatusStarting {
		t.Fatalf("created process runs = %#v", processes.created)
	}
	if processes.runningID != "process-run-1" || processes.runningPID != 1234 {
		t.Fatalf("running process = id %q pid %d", processes.runningID, processes.runningPID)
	}
	if codex.startInput.ProcessRunID != "process-run-1" || codex.startInput.Workdir != "/workspace/session-1" || codex.startInput.Prompt != promptWithAnswerUserGuidance("implement session") {
		t.Fatalf("codex start input = %#v", codex.startInput)
	}
	if repo.sessions["session-1"].Status != domain.StatusRunning {
		t.Fatalf("saved session = %#v", repo.sessions["session-1"])
	}
}

func TestStartSessionPromptMentionsAnswerUserGuidance(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "Implement the requested change",
		Mode:         domain.ModeChat,
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/project",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSessionWithOptions() error = %v", err)
	}
	prompt := codex.startInput.Prompt
	if !strings.Contains(prompt, "answer_user") {
		t.Fatalf("prompt missing answer_user guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "不确定") {
		t.Fatalf("prompt should tell Codex to ask when uncertain: %q", prompt)
	}
	if !strings.Contains(prompt, "request_user_input") {
		t.Fatalf("prompt should distinguish request_user_input from answer_user: %q", prompt)
	}
	if !strings.Contains(prompt, "不要使用 `request_user_input`") {
		t.Fatalf("prompt should tell Codex not to use request_user_input for AnyCode questions: %q", prompt)
	}
}

func TestStartSessionAppendsLifecycleEvents(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"}}
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithEventPublisher(publisher))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{"process-run-1", "event-starting", "event-running"}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	got := events.snapshot()
	if len(got) != 2 {
		t.Fatalf("events length = %d, want 2: %#v", len(got), got)
	}
	if got[0].ID != "event-starting" || got[0].Type != "session.starting" || got[0].Scope.ProjectID != "project-1" {
		t.Fatalf("starting event = %#v", got[0])
	}
	if got[0].SessionID == nil || *got[0].SessionID != "session-1" || got[0].Payload["processRunId"] != "process-run-1" || got[0].Payload["status"] != "starting" {
		t.Fatalf("starting payload/scope = %#v", got[0])
	}
	if got[1].ID != "event-running" || got[1].Type != "session.running" {
		t.Fatalf("running event = %#v", got[1])
	}
	if got[1].Payload["pid"] != 1234 || got[1].Payload["codexSessionId"] != "codex-session-1" || got[1].Payload["status"] != "running" {
		t.Fatalf("running payload = %#v", got[1].Payload)
	}
	published := publisher.snapshot()
	if len(published) != 2 || published[0].ID != "event-starting" || published[1].ID != "event-running" {
		t.Fatalf("published events = %#v", published)
	}
}

func TestStartSessionPassesArchivedAttachmentsToCodex(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	repo.sessionAttachments["attachment-image"] = domain.SessionAttachment{
		ID:        "attachment-image",
		SessionID: "session-1",
		Path:      "/data/attachments/sessions/session-1/screenshot.png",
		MimeType:  "image/png",
	}
	repo.sessionAttachments["attachment-note"] = domain.SessionAttachment{
		ID:        "attachment-note",
		SessionID: "session-1",
		Path:      "/data/attachments/sessions/session-1/notes.md",
		MimeType:  "text/markdown",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, nil), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if !slices.Contains(codex.startInput.AttachmentPaths, "/data/attachments/sessions/session-1/screenshot.png") ||
		!slices.Contains(codex.startInput.AttachmentPaths, "/data/attachments/sessions/session-1/notes.md") {
		t.Fatalf("AttachmentPaths = %#v", codex.startInput.AttachmentPaths)
	}
	if !slices.Equal(codex.startInput.ImagePaths, []string{"/data/attachments/sessions/session-1/screenshot.png"}) {
		t.Fatalf("ImagePaths = %#v", codex.startInput.ImagePaths)
	}
	if !strings.Contains(codex.startInput.Prompt, "/data/attachments/sessions/session-1/screenshot.png") ||
		!strings.Contains(codex.startInput.Prompt, "/data/attachments/sessions/session-1/notes.md") {
		t.Fatalf("Prompt missing attachment paths: %q", codex.startInput.Prompt)
	}
}

func TestStartSessionQueuesWhenSameWorkdirAlreadyRunning(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "first",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/shared",
	}
	repo.sessions["session-2"] = domain.Session{
		ID:           "session-2",
		ProjectID:    "project-1",
		Requirement:  "second",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/shared",
	}
	processes := newFakeProcessRepository()
	events := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 1234},
		events:      events,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{"process-run-1"}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	first, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("first StartSessionWithOptions() error = %v", err)
	}
	if first.Status != domain.StatusRunning {
		t.Fatalf("first status = %q", first.Status)
	}
	second, err := service.StartSessionWithOptions(ctx, "session-2", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("second StartSessionWithOptions() error = %v", err)
	}
	if second.Status != domain.StatusQueued || repo.sessions["session-2"].Status != domain.StatusQueued {
		t.Fatalf("second session = %#v saved=%#v", second, repo.sessions["session-2"])
	}
	if len(codex.startInputs) != 1 || codex.startInputs[0].SessionID != "session-1" {
		t.Fatalf("codex start inputs = %#v", codex.startInputs)
	}
	close(events)
}

func TestStartSessionQueuesWhenSameProjectPathAlreadyRunning(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:          "session-1",
		ProjectID:   "project-1",
		Requirement: "first",
		Status:      domain.StatusCreated,
	}
	repo.sessions["session-2"] = domain.Session{
		ID:          "session-2",
		ProjectID:   "project-1",
		Requirement: "second",
		Status:      domain.StatusCreated,
	}
	projects := newFakeProjectRepository("project-1")
	project := projects.projects["project-1"]
	project.Path = projectdomain.ProjectPath{Value: "/workspace/project-1"}
	projects.projects["project-1"] = project
	processes := newFakeProcessRepository()
	events := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 1234},
		events:      events,
	}
	service := New(repo, projects, WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{"process-run-1"}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	first, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("first StartSessionWithOptions() error = %v", err)
	}
	if first.Status != domain.StatusRunning {
		t.Fatalf("first status = %q", first.Status)
	}
	second, err := service.StartSessionWithOptions(ctx, "session-2", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("second StartSessionWithOptions() error = %v", err)
	}
	if second.Status != domain.StatusQueued || repo.sessions["session-2"].Status != domain.StatusQueued {
		t.Fatalf("second session = %#v saved=%#v", second, repo.sessions["session-2"])
	}
	if len(codex.startInputs) != 1 || codex.startInputs[0].Workdir != "/workspace/project-1" {
		t.Fatalf("codex start inputs = %#v", codex.startInputs)
	}
	close(events)
}

func TestStartSessionPublishesCodexEventsWithoutStoringTranscript(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{
		EventID:   "codex-event-1",
		Type:      "assistant_message",
		CreatedAt: time.Unix(39, 0).UTC(),
		Payload: map[string]any{
			"message": map[string]any{"role": "assistant", "content": "hello"},
		},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithEventPublisher(publisher))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-running",
		"event-codex",
		"event-process-exited",
		"event-stopped",
	}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	got := waitForPublishedEventType(t, publisher, "process.codex_event")
	if got.Payload["codexEventId"] != "codex-event-1" || got.Payload["codexType"] != "assistant_message" {
		t.Fatalf("codex event payload identifiers = %#v", got.Payload)
	}
	if !got.CreatedAt.Equal(time.Unix(39, 0).UTC()) {
		t.Fatalf("codex event created at = %s", got.CreatedAt)
	}
	if _, ok := got.Payload["processRunId"]; ok {
		t.Fatalf("codex event should not include processRunId: %#v", got.Payload)
	}
	message, ok := got.Payload["message"].(map[string]any)
	if !ok || message["role"] != "assistant" || message["content"] != "hello" {
		t.Fatalf("message payload = %#v", got.Payload["message"])
	}
	for _, event := range events.snapshot() {
		if event.Type == "process.codex_event" {
			t.Fatalf("codex transcript event should not be stored: %#v", event)
		}
	}
}

func TestStartSessionReplacesStaleCodexSessionIDFromThreadStarted(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "implement session",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-old",
		WorktreePath:   "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{
		EventID: "codex-thread-new",
		Type:    "thread.started",
		Payload: map[string]any{
			"thread_id": "codex-session-new",
		},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	publishedAfterSave := make(chan bool, 1)
	publisher := &fakeEventPublisher{
		onPublish: func(event eventdomain.DomainEvent) {
			if event.Type == "process.codex_event" {
				publishedAfterSave <- repo.sessions["session-1"].CodexSessionID == "codex-session-new"
			}
		},
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(&fakeEventStore{}), WithEventPublisher(publisher))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{"process-run-1", "event-starting", "event-running", "event-codex", "event-process-exited", "event-stopped"}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && repo.sessions["session-1"].CodexSessionID != "codex-session-new" {
		time.Sleep(10 * time.Millisecond)
	}
	if got := repo.sessions["session-1"].CodexSessionID; got != "codex-session-new" {
		t.Fatalf("CodexSessionID = %q, want new thread id", got)
	}
	if processes.runningCodex != "codex-session-new" {
		t.Fatalf("running codex session id = %q", processes.runningCodex)
	}
	select {
	case ok := <-publishedAfterSave:
		if !ok {
			t.Fatal("codex event published before CodexSessionID was saved")
		}
	case <-time.After(time.Second):
		t.Fatal("thread.started event was not published")
	}
}

func TestStartSessionPersistsTodoListFromPlanUpdateEvent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{
		EventID: "codex-event-plan",
		Type:    "plan_update",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"step": "梳理需求", "status": "completed"},
				map[string]any{"step": "实现卡片展示", "status": "in_progress"},
			},
		},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-running",
		"event-todo-list",
		"event-process-exited",
		"event-stopped",
	}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	waitForEventType(t, events, "session.todo_list_updated")

	got := repo.sessions["session-1"].TodoList
	if len(got.Items) != 2 || got.Items[0].Text != "梳理需求" || !got.Items[0].Completed || got.Items[1].Completed {
		t.Fatalf("todo list = %#v", got)
	}
}

func TestStartSessionUpdatesTodoListFromStartedAndUpdatedItems(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	source := make(chan processdomain.CodexEvent, 2)
	source <- processdomain.CodexEvent{
		EventID: "codex-event-todo-started",
		Type:    "item.started",
		Payload: map[string]any{
			"type": "item.started",
			"item": map[string]any{
				"id":   "item_3",
				"type": "todo_list",
				"items": []any{
					map[string]any{"text": "Explore project context for Git/worktree creation", "completed": false},
					map[string]any{"text": "Clarify intended conflict policy", "completed": false},
					map[string]any{"text": "Compare approaches and recommend design", "completed": false},
				},
			},
		},
	}
	source <- processdomain.CodexEvent{
		EventID: "codex-event-todo-updated",
		Type:    "item.updated",
		Payload: map[string]any{
			"type": "item.updated",
			"item": map[string]any{
				"id":   "item_3",
				"type": "todo_list",
				"items": []any{
					map[string]any{"text": "Explore project context for Git/worktree creation", "completed": true},
					map[string]any{"text": "Clarify intended conflict policy", "completed": false},
					map[string]any{"text": "Compare approaches and recommend design", "completed": false},
					map[string]any{"text": "Implement chosen design", "completed": false},
				},
			},
		},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-running",
		"process-event-todo-started",
		"event-codex-started",
		"event-todo-list-started",
		"process-event-todo-updated",
		"event-codex-updated",
		"event-todo-list-updated",
		"event-process-exited",
		"event-stopped",
	}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	waitForEventType(t, events, "process.exited")

	todoEvents := make([]eventdomain.DomainEvent, 0, 2)
	for _, event := range events.snapshot() {
		if event.Type == "session.todo_list_updated" {
			todoEvents = append(todoEvents, event)
		}
	}
	if len(todoEvents) != 2 {
		t.Fatalf("todo list event count = %d, want 2: %#v", len(todoEvents), todoEvents)
	}
	if todoEvents[0].Payload["completed"] != 0 || todoEvents[0].Payload["total"] != 3 {
		t.Fatalf("started todo list event payload = %#v, want completed=0 total=3", todoEvents[0].Payload)
	}
	if todoEvents[1].Payload["completed"] != 1 || todoEvents[1].Payload["total"] != 4 {
		t.Fatalf("updated todo list event payload = %#v, want completed=1 total=4", todoEvents[1].Payload)
	}

	got := repo.sessions["session-1"].TodoList
	if got.Total() != 4 || got.Completed() != 1 {
		t.Fatalf("todo list counts = %d/%d, want 1/4: %#v", got.Completed(), got.Total(), got)
	}
	if got.Items[0].Text != "Explore project context for Git/worktree creation" || !got.Items[0].Completed {
		t.Fatalf("first todo item = %#v", got.Items[0])
	}
	if got.Items[3].Text != "Implement chosen design" || got.Items[3].Completed {
		t.Fatalf("updated todo item = %#v", got.Items[3])
	}
}

func TestTodoListFromCodexEventParsesPlanUpdateShapes(t *testing.T) {
	tests := []struct {
		name  string
		event processdomain.CodexEvent
	}{
		{
			name: "params plan",
			event: processdomain.CodexEvent{
				Type: "turn/plan/updated",
				Payload: map[string]any{
					"params": map[string]any{
						"plan": []any{
							map[string]any{"step": "梳理事件流", "status": "completed"},
							map[string]any{"step": "落库 TODO", "status": "in_progress"},
						},
					},
				},
			},
		},
		{
			name: "tool call arguments",
			event: processdomain.CodexEvent{
				Type: "function_call",
				Payload: map[string]any{
					"item": map[string]any{
						"name":      "update_plan",
						"arguments": `{"plan":[{"step":"解析 arguments","status":"done"},{"step":"刷新卡片","status":"pending"}]}`,
					},
				},
			},
		},
		{
			name: "updated todo list item",
			event: processdomain.CodexEvent{
				Type: "item.updated",
				Payload: map[string]any{
					"type": "item.updated",
					"item": map[string]any{
						"id":   "item_5",
						"type": "todo_list",
						"items": []any{
							map[string]any{"text": "定位事件解析", "status": "completed"},
							map[string]any{"text": "刷新卡片列表", "status": "pending"},
						},
					},
				},
			},
		},
		{
			name: "started todo list item",
			event: processdomain.CodexEvent{
				Type: "item.started",
				Payload: map[string]any{
					"type": "item.started",
					"item": map[string]any{
						"id":   "item_3",
						"type": "todo_list",
						"items": []any{
							map[string]any{"text": "Explore project context for Git/worktree creation", "completed": false},
							map[string]any{"text": "Clarify intended conflict policy", "completed": true},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := todoListFromCodexEvent(tt.event)
			if !ok {
				t.Fatal("todoListFromCodexEvent() did not find plan")
			}
			if got.Total() != 2 || got.Completed() != 1 {
				t.Fatalf("todo list counts = %d/%d, want 1/2: %#v", got.Completed(), got.Total(), got)
			}
		})
	}
}

func TestTodoListFromCodexEventIgnoresUnrelatedPlanPayloads(t *testing.T) {
	tests := []processdomain.CodexEvent{
		{
			Type: "assistant_message",
			Payload: map[string]any{
				"plan": []any{
					map[string]any{"step": "不应覆盖", "status": "completed"},
				},
			},
		},
		{
			Type: "function_call",
			Payload: map[string]any{
				"item": map[string]any{
					"name":      "write_file",
					"arguments": `{"plan":[]}`,
				},
			},
		},
		{
			Type: "assistant_message",
			Payload: map[string]any{
				"item": map[string]any{
					"type": "todo_list",
					"items": []any{
						map[string]any{"text": "不应覆盖", "completed": true},
					},
				},
			},
		},
	}
	for _, event := range tests {
		if got, ok := todoListFromCodexEvent(event); ok {
			t.Fatalf("todoListFromCodexEvent() = %#v, true; want false", got)
		}
	}
}

func TestStartSessionClearsTodoListFromEmptyPlanUpdateEvent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
		TodoList: domain.TodoList{Items: []domain.TodoItem{
			{Text: "旧任务", Completed: true},
		}},
	}
	processes := newFakeProcessRepository()
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{
		EventID: "codex-event-empty-plan",
		Type:    "turn/plan/updated",
		Payload: map[string]any{
			"params": map[string]any{
				"plan": []any{},
			},
		},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-running",
		"process-event-empty-plan",
		"event-codex",
		"event-todo-list",
		"event-process-exited",
		"event-stopped",
	}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	gotEvent := waitForEventType(t, events, "session.todo_list_updated")

	got := repo.sessions["session-1"].TodoList
	if got.Total() != 0 {
		t.Fatalf("todo list = %#v, want empty", got)
	}
	if gotEvent.Payload["total"] != 0 {
		t.Fatalf("todo list event payload = %#v", gotEvent.Payload)
	}
}

func TestSessionModeMarksParamRejectedExitFailed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Mode:         domain.ModeChat,
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{
		Type: "process.exit",
		Payload: map[string]any{
			"exitCode":      2,
			"failureReason": `exit status 2: invalid value "readonly" for '--sandbox'`,
		},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-running",
		"process-event-exit",
		"event-codex",
		"event-process-exited",
		"event-failed",
	}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	got := waitForEventType(t, events, "session.failed")
	if repo.sessions["session-1"].Status != domain.StatusFailed {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
	if got.Payload["failureCode"] != "codex_param_rejected" {
		t.Fatalf("session failed payload = %#v", got.Payload)
	}
	if processes.exitedResult.FailureReason != `exit status 2: invalid value "readonly" for '--sandbox'` {
		t.Fatalf("exited result = %#v", processes.exitedResult)
	}
}

func TestStartSessionReturnsCurrentSessionWhenProcessIsAlreadyActive(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))

	got, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusRunning {
		t.Fatalf("StartSession() status = %q", got.Status)
	}
	if codex.startCalled {
		t.Fatal("codex Start should not be called when active process exists")
	}
}

func TestStartSessionQueuesWhenAgentLimitReached(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
		Priority:     domain.PriorityHigh,
	}
	processes := newFakeProcessRepository()
	processes.activeCount = 1
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1))
	service.now = func() time.Time { return time.Unix(42, 0).UTC() }

	got, err := service.StartSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued {
		t.Fatalf("StartSession() status = %q", got.Status)
	}
	saved := repo.sessions["session-1"]
	if saved.Status != domain.StatusQueued || saved.QueuedAt == nil || saved.Queue.Kind != domain.QueueKindStart {
		t.Fatalf("queued session = %#v", saved)
	}
	if codex.startCalled || len(processes.created) != 0 {
		t.Fatalf("queued start should not launch codex: called=%v created=%#v", codex.startCalled, processes.created)
	}
}

func TestForceStartQueuedSessionBypassesAgentLimit(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusQueued,
		WorktreePath: "/workspace/session-1",
		QueuedAt:     &queuedAt,
		Queue:        domain.QueueIntent{Kind: domain.QueueKindStart, Prompt: "implement session"},
	}
	processes := newFakeProcessRepository()
	processes.activeCount = 1
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1))
	service.now = func() time.Time { return time.Unix(43, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-force", nil }

	got, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("StartSessionWithOptions() error = %v", err)
	}
	if got.Status != domain.StatusRunning {
		t.Fatalf("StartSessionWithOptions() status = %q", got.Status)
	}
	saved := repo.sessions["session-1"]
	if saved.QueuedAt != nil || saved.Queue.Kind != "" {
		t.Fatalf("force start should clear queue fields: %#v", saved)
	}
	if !codex.startCalled || len(processes.created) != 1 || processes.created[0].ID != "process-run-force" {
		t.Fatalf("force start did not launch process: called=%v created=%#v", codex.startCalled, processes.created)
	}
}

func TestForceStartQueuedAnswerUserReleasesWaitingProcess(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	pid := 1234
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusQueued,
		QueuedAt:  &queuedAt,
		Queue: domain.QueueIntent{
			Kind:     domain.QueueKindAnswerUser,
			Priority: domain.QueuePriorityImmediate,
		},
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID:             "process-run-1",
		SessionID:      "session-1",
		Status:         processdomain.StatusWaitingUser,
		PID:            &pid,
		CodexSessionID: "codex-session-1",
	}
	processes.hasActive = true
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1))
	service.now = func() time.Time { return time.Unix(43, 0).UTC() }

	got, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("StartSessionWithOptions() error = %v", err)
	}
	if got.Status != domain.StatusRunning {
		t.Fatalf("StartSessionWithOptions() status = %q", got.Status)
	}
	if codex.startCalled {
		t.Fatal("queued answer_user force start should release the existing process instead of starting codex")
	}
	if processes.runningID != "process-run-1" || processes.runningCodex != "codex-session-1" {
		t.Fatalf("process running = %q %q", processes.runningID, processes.runningCodex)
	}
	saved := repo.sessions["session-1"]
	if saved.Queue.Kind != "" || saved.QueuedAt != nil {
		t.Fatalf("released answer_user should clear queue: %#v", saved)
	}
}

func TestDrainQueuedSessionsStartsHighestPriorityFirst(t *testing.T) {
	ctx := context.Background()
	lowQueuedAt := time.Unix(40, 0).UTC()
	highQueuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["low-session"] = domain.Session{
		ID:           "low-session",
		ProjectID:    "project-1",
		Requirement:  "low priority",
		Status:       domain.StatusQueued,
		Priority:     domain.PriorityLow,
		WorktreePath: "/workspace/low-session",
		QueuedAt:     &lowQueuedAt,
		Queue:        domain.QueueIntent{Kind: domain.QueueKindStart, Prompt: "low priority"},
	}
	repo.sessions["high-session"] = domain.Session{
		ID:           "high-session",
		ProjectID:    "project-1",
		Requirement:  "high priority",
		Status:       domain.StatusQueued,
		Priority:     domain.PriorityHigh,
		WorktreePath: "/workspace/high-session",
		QueuedAt:     &highQueuedAt,
		Queue:        domain.QueueIntent{Kind: domain.QueueKindStart, Prompt: "high priority"},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1))
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-high", nil }

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}

	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if codex.startInput.SessionID != "high-session" {
		t.Fatalf("started session = %q, want high-session", codex.startInput.SessionID)
	}
	if repo.sessions["high-session"].Status != domain.StatusRunning || repo.sessions["low-session"].Status != domain.StatusQueued {
		t.Fatalf("session statuses: high=%q low=%q", repo.sessions["high-session"].Status, repo.sessions["low-session"].Status)
	}
}

func TestDrainQueuedSessionsSkipsSessionStoppedAfterSelection(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusQueued,
		Priority:     domain.PriorityHigh,
		WorktreePath: "/workspace/session-1",
		QueuedAt:     &queuedAt,
		Queue:        domain.QueueIntent{Kind: domain.QueueKindStart, Prompt: "implement session"},
	}
	repo.listQueuedHook = func() {
		session := repo.sessions["session-1"]
		session.Status = domain.StatusStopped
		session.QueuedAt = nil
		session.Queue = domain.QueueIntent{}
		repo.sessions["session-1"] = session
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1), WithSessionLocker(NewMemorySessionLocker()))

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}

	if started != 0 {
		t.Fatalf("DrainQueuedSessions() = %d, want 0", started)
	}
	if codex.startCalled || len(processes.created) != 0 {
		t.Fatalf("stopped queued session should not launch codex: called=%v created=%#v", codex.startCalled, processes.created)
	}
	if repo.sessions["session-1"].Status != domain.StatusStopped {
		t.Fatalf("session status = %q, want stopped", repo.sessions["session-1"].Status)
	}
}

func TestDrainQueuedSessionsStopsStaleAnswerUserAndContinues(t *testing.T) {
	ctx := context.Background()
	answerQueuedAt := time.Unix(40, 0).UTC()
	startQueuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["answer-session"] = domain.Session{
		ID:        "answer-session",
		ProjectID: "project-1",
		Status:    domain.StatusQueued,
		QueuedAt:  &answerQueuedAt,
		Queue: domain.QueueIntent{
			Kind:     domain.QueueKindAnswerUser,
			Priority: domain.QueuePriorityImmediate,
		},
	}
	repo.sessions["start-session"] = domain.Session{
		ID:           "start-session",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusQueued,
		WorktreePath: "/workspace/start-session",
		QueuedAt:     &startQueuedAt,
		Queue:        domain.QueueIntent{Kind: domain.QueueKindStart, Prompt: "implement session"},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithSessionLocker(NewMemorySessionLocker()))
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-start", nil }

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 2 {
		t.Fatalf("DrainQueuedSessions() = %d, want 2", started)
	}
	if got := repo.sessions["answer-session"]; got.Status != domain.StatusStopped || got.Queue.Kind != "" || got.QueuedAt != nil {
		t.Fatalf("stale answer_user session = %#v", got)
	}
	if got := repo.sessions["start-session"]; got.Status != domain.StatusRunning {
		t.Fatalf("start session status = %q", got.Status)
	}
	if !codex.startCalled || codex.startInput.SessionID != "start-session" {
		t.Fatalf("codex start input = %#v", codex.startInput)
	}
}

func TestDrainQueuedSessionsTreatsPersistedStartFailureAsProcessed(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusQueued,
		WorktreePath: "/workspace/session-1",
		QueuedAt:     &queuedAt,
		Queue:        domain.QueueIntent{Kind: domain.QueueKindStart, Prompt: "implement session"},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startErr: errors.New("codex rejected args")}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithSessionLocker(NewMemorySessionLocker()))
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-start", nil }

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if got := repo.sessions["session-1"]; got.Status != domain.StatusFailed || got.Queue.Kind != "" || got.QueuedAt != nil {
		t.Fatalf("failed queued session = %#v", got)
	}
}

func TestDrainQueuedWorkflowStartFailureReturnsWorkflowFailureError(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
	failErr := errors.New("workflow fail node unavailable")
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusQueued,
		WorktreePath: "/workspace/session-1",
		QueuedAt:     &queuedAt,
		Queue: domain.QueueIntent{
			Kind:          domain.QueueKindStart,
			Prompt:        "implement session",
			WorkflowRunID: "workflow-run-1",
			NodeRunID:     &nodeRunID,
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startErr: errors.New("codex rejected args")}
	workflows := &fakeWorkflowStarter{failErr: failErr}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithWorkflows(workflows), WithSessionLocker(NewMemorySessionLocker()))
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-start", nil }

	started, err := service.DrainQueuedSessions(ctx)
	if !errors.Is(err, failErr) {
		t.Fatalf("DrainQueuedSessions() error = %v, want %v", err, failErr)
	}
	if started != 0 {
		t.Fatalf("DrainQueuedSessions() = %d, want 0", started)
	}
	if workflows.failInput.WorkflowRunID != "workflow-run-1" || workflows.failInput.NodeRunID != nodeRunID {
		t.Fatalf("workflow fail input = %#v", workflows.failInput)
	}
	if workflows.failedInput.Code != "codex_start_failed" {
		t.Fatalf("workflow start failed input = %#v", workflows.failedInput)
	}
}

func TestStopProjectSessionsStopsQueuedSessions(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	queued := domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusQueued,
		QueuedAt:  &queuedAt,
		Queue:     domain.QueueIntent{Kind: domain.QueueKindStart, Prompt: "implement session"},
	}
	repo.sessions["session-1"] = queued
	repo.listSessions = []domain.Session{queued}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}))

	stopped, err := service.StopProjectSessions(ctx, "project-1")
	if err != nil {
		t.Fatalf("StopProjectSessions() error = %v", err)
	}
	if stopped != 1 {
		t.Fatalf("stopped count = %d", stopped)
	}
	saved := repo.sessions["session-1"]
	if saved.Status != domain.StatusStopped || saved.QueuedAt != nil || saved.Queue.Kind != "" {
		t.Fatalf("queued project session was not stopped cleanly: %#v", saved)
	}
}

func TestResumeSessionStartsCodexResume(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
		Config: domain.Config{
			CodexModel:      "gpt-5.4",
			ReasoningEffort: "high",
			PermissionMode:  "workspace-write",
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(41, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-2", nil }

	got, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if got.Status != domain.StatusRunning {
		t.Fatalf("ResumeSession() status = %q", got.Status)
	}
	if !codex.resumeCalled || codex.resumeInput.CodexSessionID != "codex-session-1" || codex.resumeInput.ProcessRunID != "process-run-2" {
		t.Fatalf("codex resume input = %#v", codex.resumeInput)
	}
	if codex.resumeInput.Model != "gpt-5.4" || codex.resumeInput.ReasoningEffort != "high" || codex.resumeInput.PermissionMode != "workspace-write" {
		t.Fatalf("codex resume config = %#v", codex.resumeInput)
	}
}

func TestResumeWorkflowSessionBindsCurrentNodeRun(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusResumeFailed,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	nodeRunID := domain.NodeRunID("node-run-1")
	workflows := &fakeWorkflowStarter{resumeNodeAdvance: domain.WorkflowAdvance{
		WorkflowRunID:    "workflow-run-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Build",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithWorkflows(workflows))
	service.now = func() time.Time { return time.Unix(41, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-2", nil }

	got, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if got.Status != domain.StatusRunning || workflows.resumeNodeInput.SessionID != "session-1" {
		t.Fatalf("ResumeSession() = %#v workflow input=%#v", got, workflows.resumeNodeInput)
	}
	if len(processes.created) != 1 || processes.created[0].NodeRunID == nil || *processes.created[0].NodeRunID != "node-run-1" {
		t.Fatalf("process runs = %#v", processes.created)
	}
	if !codex.resumeCalled || codex.resumeInput.CodexSessionID != "codex-session-1" || codex.resumeInput.ProcessRunID != "process-run-2" {
		t.Fatalf("codex resume input = %#v", codex.resumeInput)
	}
}

func TestResumeSessionFailureMarksResumeFailed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{resumeErr: errors.New("resume unavailable")}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(43, 0).UTC() }
	ids := []domain.ID{"process-run-2"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err == nil {
		t.Fatal("ResumeSession() expected error")
	}
	if repo.sessions["session-1"].Status != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
	if processes.exitedID != "process-run-2" || processes.exitedResult.FailureReason != "resume unavailable" {
		t.Fatalf("exited process = %q %#v", processes.exitedID, processes.exitedResult)
	}
	if !codex.resumeCalled {
		t.Fatal("codex Resume should be called")
	}
}

func TestResumeWorkflowSessionFailureMarksWorkflowWaitingResumeAction(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{resumeErr: errors.New("resume unavailable")}
	workflows := &fakeWorkflowStarter{
		resumeSnapshot: domain.WorkflowRunSnapshot{
			ID:            "workflow-run-1",
			SessionID:     "session-1",
			Status:        "waiting_resume_action",
			CurrentNodeID: "build",
		},
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithWorkflows(workflows), WithEvents(events))
	service.now = func() time.Time { return time.Unix(43, 0).UTC() }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		switch nextID {
		case 1:
			return "process-run-2", nil
		case 2:
			return "process-event-1", nil
		default:
			return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
		}
	}

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err == nil {
		t.Fatal("ResumeSession() expected error")
	}
	if repo.sessions["session-1"].Status != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
	if workflows.resumeInput.SessionID != "session-1" || workflows.resumeInput.Code != "resume_failed" {
		t.Fatalf("workflow resume input = %#v", workflows.resumeInput)
	}
	got := events.snapshot()
	found := false
	for _, event := range got {
		if event.Type == "workflow.waiting_resume_action" && event.Payload["workflowRunId"] == "workflow-run-1" && event.Payload["currentNodeId"] == "build" {
			found = true
		}
	}
	if !found {
		t.Fatalf("workflow waiting resume event not found: %#v", got)
	}
}

func TestStopSessionStopsActiveProcessAndMarksStopped(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(42, 0).UTC() }

	got, err := service.StopSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if got.Status != domain.StatusStopped {
		t.Fatalf("StopSession() status = %q", got.Status)
	}
	if codex.stoppedID != "process-run-1" {
		t.Fatalf("stopped process id = %q", codex.stoppedID)
	}
	if processes.exitedID != "process-run-1" || processes.exitedResult.FailureReason != "stopped by user" {
		t.Fatalf("exited process = %q %#v", processes.exitedID, processes.exitedResult)
	}
	if repo.sessions["session-1"].Status != domain.StatusStopped {
		t.Fatalf("saved session = %#v", repo.sessions["session-1"])
	}
}

func TestStopSessionFromResumeFailedMarksStoppedWithoutActiveProcess(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusResumeFailed,
	}
	processes := newFakeProcessRepository()
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}))

	got, err := service.StopSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if got.Status != domain.StatusStopped || repo.sessions["session-1"].Status != domain.StatusStopped {
		t.Fatalf("StopSession() status = %q saved=%q", got.Status, repo.sessions["session-1"].Status)
	}
}

func TestStopSessionCancelsPendingQuestions(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingUser,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	questions := &fakeQuestionCanceller{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithQuestions(questions))

	if _, err := service.StopSession(ctx, "session-1"); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if questions.cancelledSessionID != "session-1" || questions.cancelReason != "session stopped" {
		t.Fatalf("cancelled questions = %q %q", questions.cancelledSessionID, questions.cancelReason)
	}
}

func TestStopSessionUsesUnitOfWorkForProcessExitAndStoppedEvent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	txRepo := repo
	txProcesses := newFakeProcessRepository()
	txEvents := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	uow := &fakeUnitOfWork{
		tx: fakeTx{
			sessions:  txRepo,
			processes: txProcesses,
			events:    txEvents,
		},
		publisher: publisher,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithEvents(&fakeEventStore{}), WithEventPublisher(publisher), WithUnitOfWork(uow))
	service.now = func() time.Time { return time.Unix(47, 0).UTC() }
	ids := []domain.ID{"event-stopping", "event-stopped"}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.StopSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if got.Status != domain.StatusStopped {
		t.Fatalf("status = %q", got.Status)
	}
	if !uow.called || uow.publishedDuringCall {
		t.Fatalf("uow called=%v publishedDuringCall=%v", uow.called, uow.publishedDuringCall)
	}
	if txProcesses.exitedID != "process-run-1" || txProcesses.exitedResult.FailureReason != "stopped by user" {
		t.Fatalf("exited process = %q %#v", txProcesses.exitedID, txProcesses.exitedResult)
	}
	if txRepo.sessions["session-1"].Status != domain.StatusStopped {
		t.Fatalf("tx session = %#v", txRepo.sessions["session-1"])
	}
	if events := txEvents.snapshot(); len(events) != 2 || events[0].Type != "session.stopping" || events[1].Type != "session.stopped" {
		t.Fatalf("tx events = %#v", events)
	}
}

func TestMarkWaitingUserAndRunningAfterUserWait(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusRunning,
	}
	events := &fakeEventStore{}
	pid := 1234
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID:             "process-run-1",
		SessionID:      "session-1",
		Status:         processdomain.StatusRunning,
		PID:            &pid,
		CodexSessionID: "codex-session-1",
	}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithProcesses(processes, nil))
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	ids := []domain.ID{"event-waiting", "event-queued", "event-running"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	waiting, err := service.MarkWaitingUser(ctx, "session-1")
	if err != nil {
		t.Fatalf("MarkWaitingUser() error = %v", err)
	}
	if waiting.Status != domain.StatusWaitingUser || repo.sessions["session-1"].Status != domain.StatusWaitingUser {
		t.Fatalf("waiting status = %q saved=%q", waiting.Status, repo.sessions["session-1"].Status)
	}
	running, err := service.MarkRunningAfterUserWait(ctx, "session-1")
	if err != nil {
		t.Fatalf("MarkRunningAfterUserWait() error = %v", err)
	}
	if running.Status != domain.StatusRunning || repo.sessions["session-1"].Status != domain.StatusRunning {
		t.Fatalf("running status = %q saved=%q", running.Status, repo.sessions["session-1"].Status)
	}
	if repo.sessions["session-1"].QueuedAt != nil || repo.sessions["session-1"].Queue.Kind != "" {
		t.Fatalf("answer_user queue was not cleared: %#v", repo.sessions["session-1"])
	}
	got := events.snapshot()
	if len(got) != 3 || got[0].Type != "session.waiting_user" || got[1].Type != "session.queued" || got[2].Type != "session.running" {
		t.Fatalf("events = %#v", got)
	}
}

func TestMarkWaitingUserUsesUnitOfWorkForProcessAndSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	txRepo := repo
	txProcesses := newFakeProcessRepository()
	txEvents := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	uow := &fakeUnitOfWork{
		tx: fakeTx{
			sessions:  txRepo,
			processes: txProcesses,
			events:    txEvents,
		},
		publisher: publisher,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(&fakeEventStore{}), WithEventPublisher(publisher), WithUnitOfWork(uow), WithProcesses(processes, nil))
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-waiting", nil }

	got, err := service.MarkWaitingUser(ctx, "session-1")
	if err != nil {
		t.Fatalf("MarkWaitingUser() error = %v", err)
	}
	if got.Status != domain.StatusWaitingUser {
		t.Fatalf("status = %q", got.Status)
	}
	if !uow.called || uow.publishedDuringCall {
		t.Fatalf("uow called=%v publishedDuringCall=%v", uow.called, uow.publishedDuringCall)
	}
	if txProcesses.active.Status != processdomain.StatusWaitingUser || txProcesses.active.ID != "process-run-1" {
		t.Fatalf("tx process = %#v", txProcesses.active)
	}
	if txRepo.sessions["session-1"].Status != domain.StatusWaitingUser {
		t.Fatalf("tx saved session = %#v", txRepo.sessions["session-1"])
	}
	if events := txEvents.snapshot(); len(events) != 1 || events[0].Type != "session.waiting_user" {
		t.Fatalf("tx events = %#v", events)
	}
}

func TestSessionStatusEventPublishesAfterUnitOfWorkReturns(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingUser,
	}
	pid := 1234
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID:             "process-run-1",
		SessionID:      "session-1",
		Status:         processdomain.StatusWaitingUser,
		PID:            &pid,
		CodexSessionID: "codex-session-1",
	}
	processes.hasActive = true
	txRepo := repo
	txProcesses := newFakeProcessRepository()
	txEvents := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	uow := &fakeUnitOfWork{
		tx: fakeTx{
			sessions:  txRepo,
			processes: txProcesses,
			events:    txEvents,
		},
		publisher: publisher,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(&fakeEventStore{}), WithEventPublisher(publisher), WithUnitOfWork(uow), WithProcesses(processes, nil))
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	ids := []domain.ID{"event-queued", "event-running"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.MarkRunningAfterUserWait(ctx, "session-1")
	if err != nil {
		t.Fatalf("MarkRunningAfterUserWait() error = %v", err)
	}
	if got.Status != domain.StatusRunning {
		t.Fatalf("status = %q", got.Status)
	}
	if !uow.called || uow.publishedDuringCall {
		t.Fatalf("uow called=%v publishedDuringCall=%v", uow.called, uow.publishedDuringCall)
	}
	if txRepo.sessions["session-1"].Status != domain.StatusRunning {
		t.Fatalf("tx saved session = %#v", txRepo.sessions["session-1"])
	}
	if txProcesses.active.Status != processdomain.StatusRunning || txProcesses.active.ID != "process-run-1" {
		t.Fatalf("tx process = %#v", txProcesses.active)
	}
	if events := txEvents.snapshot(); len(events) != 2 || events[0].Type != "session.queued" || events[1].Type != "session.running" {
		t.Fatalf("tx events = %#v", events)
	}
	if published := publisher.snapshot(); len(published) != 2 || published[0].ID != "event-queued" || published[1].ID != "event-running" {
		t.Fatalf("published events = %#v", published)
	}
}

func TestLifecycleActionsUseSessionLocker(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	locker := &fakeSessionLocker{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithSessionLocker(locker))
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if !slices.Equal(locker.ids, []domain.ID{"session-1"}) {
		t.Fatalf("locked ids after start = %#v", locker.ids)
	}

	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	codex.resumeHandle = processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}
	service.generateID = func() (domain.ID, error) { return "process-run-2", nil }
	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if !slices.Equal(locker.ids, []domain.ID{"session-1", "session-1"}) {
		t.Fatalf("locked ids after resume = %#v", locker.ids)
	}

	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning}
	processes.active = processdomain.Run{ID: "process-run-2", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	if _, err := service.StopSession(ctx, "session-1"); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if !slices.Equal(locker.ids, []domain.ID{"session-1", "session-1", "session-1", "session-1"}) {
		t.Fatalf("locked ids = %#v", locker.ids)
	}
}

func TestStartSessionUsesUnitOfWorkForProcessLifecycleEvents(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	txRepo := newFakeRepository()
	txProcesses := newFakeProcessRepository()
	txEvents := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	uow := &fakeUnitOfWork{
		tx: fakeTx{
			sessions:  txRepo,
			processes: txProcesses,
			events:    txEvents,
		},
		publisher: publisher,
	}
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{
		EventID: "codex-event-1",
		Type:    "assistant_message",
		Payload: map[string]any{"content": "hello"},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"}, events: source}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithEvents(&fakeEventStore{}), WithEventPublisher(publisher), WithUnitOfWork(uow))
	service.now = func() time.Time { return time.Unix(45, 0).UTC() }
	ids := []domain.ID{"process-run-1", "event-starting", "event-running", "event-codex", "event-process-exited"}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if got.Status != domain.StatusRunning {
		t.Fatalf("status = %q", got.Status)
	}
	waitForPublishedEventType(t, publisher, "process.codex_event")
	waitForEventType(t, txEvents, "process.exited")
	if !uow.called || uow.publishedDuringCall {
		t.Fatalf("uow called=%v publishedDuringCall=%v", uow.called, uow.publishedDuringCall)
	}
	if len(txProcesses.created) != 1 || txProcesses.created[0].ID != "process-run-1" {
		t.Fatalf("created process runs = %#v", txProcesses.created)
	}
	if txProcesses.runningID != "process-run-1" || txProcesses.runningCodex != "codex-session-1" {
		t.Fatalf("running process = %q %q", txProcesses.runningID, txProcesses.runningCodex)
	}
	if txProcesses.exitedID != "process-run-1" {
		t.Fatalf("exited process = %q", txProcesses.exitedID)
	}
	if txRepo.sessions["session-1"].Status != domain.StatusRunning {
		t.Fatalf("tx session = %#v", txRepo.sessions["session-1"])
	}
}

func TestSessionHasDifferentActiveRun(t *testing.T) {
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID:        "process-run-new",
		SessionID: "session-1",
		Status:    processdomain.StatusRunning,
	}
	processes.hasActive = true
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"), WithProcesses(processes, nil))

	if !service.sessionHasDifferentActiveRun(context.Background(), "session-1", "process-run-old") {
		t.Fatal("expected different active run")
	}
	if service.sessionHasDifferentActiveRun(context.Background(), "session-1", "process-run-new") {
		t.Fatal("expected same active run")
	}
}

func TestResumeFailureUsesUnitOfWorkForProcessEventAndSessionEvents(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	txRepo := newFakeRepository()
	txProcesses := newFakeProcessRepository()
	txEvents := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	uow := &fakeUnitOfWork{
		tx: fakeTx{
			sessions:  txRepo,
			processes: txProcesses,
			events:    txEvents,
		},
		publisher: publisher,
	}
	codex := &fakeCodexProcess{resumeErr: errors.New("resume unavailable")}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithEvents(&fakeEventStore{}), WithEventPublisher(publisher), WithUnitOfWork(uow))
	service.now = func() time.Time { return time.Unix(46, 0).UTC() }
	ids := []domain.ID{"process-run-2", "event-starting", "event-process-resume-failed", "event-session-resume-failed"}
	service.generateID = func() (domain.ID, error) {
		if len(ids) == 0 {
			t.Fatal("generateID called more than expected")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err == nil {
		t.Fatal("ResumeSession() expected error")
	}
	if !uow.called || uow.publishedDuringCall {
		t.Fatalf("uow called=%v publishedDuringCall=%v", uow.called, uow.publishedDuringCall)
	}
	if txProcesses.exitedID != "process-run-2" || txProcesses.exitedResult.FailureReason != "resume unavailable" {
		t.Fatalf("exited process = %q %#v", txProcesses.exitedID, txProcesses.exitedResult)
	}
	if txRepo.sessions["session-1"].Status != domain.StatusResumeFailed {
		t.Fatalf("tx session = %#v", txRepo.sessions["session-1"])
	}
	if got := txEvents.snapshot(); len(got) != 3 || got[1].Type != "process.resume_failed" || got[2].Type != "session.resume_failed" {
		t.Fatalf("tx events = %#v", got)
	}
}

func TestLifecycleActionsDoNotMutateBeforeProcessAdapterIsWired(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusCreated,
	}
	service := New(repo, newFakeProjectRepository("project-1"))

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); !errors.Is(err, ErrProcessLifecycleNotWired) {
		t.Fatalf("StartSession() error = %v, want ErrProcessLifecycleNotWired", err)
	}
	if repo.sessions["session-1"].Status != domain.StatusCreated {
		t.Fatalf("StartSession() mutated status to %q", repo.sessions["session-1"].Status)
	}
}

func TestAvailableActionsByStatus(t *testing.T) {
	tests := []struct {
		name    string
		session domain.Session
		want    []string
	}{
		{
			name:    "created",
			session: domain.Session{Status: domain.StatusCreated},
			want:    []string{"run", "close"},
		},
		{
			name:    "running",
			session: domain.Session{Status: domain.StatusRunning},
			want:    []string{"stop"},
		},
		{
			name:    "waiting approval",
			session: domain.Session{Status: domain.StatusWaitingApproval},
			want:    []string{"close"},
		},
		{
			name:    "stopped resumable",
			session: domain.Session{Status: domain.StatusStopped, CodexSessionID: "codex-1"},
			want:    []string{"run", "resume", "close"},
		},
		{
			name:    "resume failed",
			session: domain.Session{Status: domain.StatusResumeFailed, CodexSessionID: "codex-1"},
			want:    []string{"run", "resume", "stop", "close"},
		},
		{
			name:    "resume failed without codex session id",
			session: domain.Session{Status: domain.StatusResumeFailed},
			want:    []string{"run", "stop", "close"},
		},
		{
			name:    "queued",
			session: domain.Session{Status: domain.StatusQueued},
			want:    []string{"run", "stop", "close"},
		},
		{
			name:    "closed",
			session: domain.Session{Status: domain.StatusClosed},
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := availableActions(tt.session)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("availableActions() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestMarkInterruptedSessionsRecoverableStopsResumableSessions(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	queuedAt := time.Unix(4, 0).UTC()
	repo.interruptedSessions = []domain.Session{
		{
			ID:             "session-1",
			ProjectID:      "project-1",
			Status:         domain.StatusRunning,
			CodexSessionID: "codex-1",
			UpdatedAt:      time.Unix(1, 0).UTC(),
		},
		{
			ID:             "session-2",
			ProjectID:      "project-1",
			Status:         domain.StatusWaitingUser,
			CodexSessionID: "codex-2",
			UpdatedAt:      time.Unix(2, 0).UTC(),
		},
		{
			ID:             "session-4",
			ProjectID:      "project-1",
			Status:         domain.StatusQueued,
			CodexSessionID: "codex-4",
			Queue:          domain.QueueIntent{Kind: domain.QueueKindAnswerUser},
			QueuedAt:       &queuedAt,
			UpdatedAt:      time.Unix(4, 0).UTC(),
		},
	}
	repo.listSessions = []domain.Session{
		{
			ID:        "session-3",
			ProjectID: "project-1",
			Status:    domain.StatusRunning,
			UpdatedAt: time.Unix(3, 0).UTC(),
		},
	}
	repo.listTotal = 1
	events := &fakeEventStore{}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID:        "process-1",
		SessionID: "session-1",
		Status:    processdomain.StatusRunning,
		StartedAt: time.Unix(10, 0).UTC(),
	}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithProcesses(processes, nil))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}

	count, err := service.MarkInterruptedSessionsRecoverable(ctx)
	if err != nil {
		t.Fatalf("MarkInterruptedSessionsRecoverable() error = %v", err)
	}
	if count != 4 {
		t.Fatalf("recoverable count = %d", count)
	}
	for _, id := range []domain.ID{"session-1", "session-2", "session-4"} {
		got := repo.sessions[id]
		if got.Status != domain.StatusStopped {
			t.Fatalf("session %s status = %q", id, got.Status)
		}
		if got.UpdatedAt != time.Unix(100, 0).UTC() {
			t.Fatalf("session %s updatedAt = %s", id, got.UpdatedAt)
		}
		if !slices.Contains(availableActions(got), "resume") {
			t.Fatalf("session %s actions = %#v", id, availableActions(got))
		}
	}
	if got := repo.sessions["session-4"]; got.Queue.Kind != "" || got.QueuedAt != nil {
		t.Fatalf("session-4 queue was not cleared: %#v", got)
	}
	if got := repo.sessions["session-3"]; got.Status != domain.StatusResumeFailed {
		t.Fatalf("session-3 status = %q", got.Status)
	}
	if processes.exitedID != "process-1" || processes.exitedResult.FailureReason != "service_restarted" || !processes.exitedResult.FinishedAt.Equal(time.Unix(100, 0).UTC()) {
		t.Fatalf("interrupted process exit = %q %#v", processes.exitedID, processes.exitedResult)
	}
	gotEvents := events.snapshot()
	if len(gotEvents) != 4 || gotEvents[0].Type != "session.recoverable" || gotEvents[0].Payload["previousStatus"] != "running" || gotEvents[3].Type != "session.resume_failed" {
		t.Fatalf("events = %#v", gotEvents)
	}
}

type fakeRepository struct {
	saved                               []domain.Session
	sessions                            map[domain.ID]domain.Session
	createErr                           error
	saveErr                             error
	saveHook                            func(domain.Session) error
	listSessions                        []domain.Session
	interruptedSessions                 []domain.Session
	listQueuedHook                      func()
	listTotal                           int
	lastListQuery                       domain.ListQuery
	lastConfig                          domain.Config
	hasLastConfig                       bool
	lastConfigProjectID                 domain.ProjectID
	appends                             []domain.PromptAppend
	deletedAppends                      []string
	appendPromptHook                    func()
	appendPromptErr                     error
	mergeRecords                        []domain.MergeRecord
	addMergeRecordErr                   error
	stagedAttachments                   map[domain.StagedAttachmentID]domain.StagedAttachment
	sessionAttachments                  map[domain.SessionAttachmentID]domain.SessionAttachment
	deleteStagedAttachmentErr           error
	lastPromptAppendAttachmentSessionID domain.ID
	lastPromptAppendAttachmentID        string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		sessions:           map[domain.ID]domain.Session{},
		stagedAttachments:  map[domain.StagedAttachmentID]domain.StagedAttachment{},
		sessionAttachments: map[domain.SessionAttachmentID]domain.SessionAttachment{},
	}
}

func (r *fakeRepository) Create(_ context.Context, session domain.Session) error {
	if r.createErr != nil {
		return r.createErr
	}
	if _, exists := r.sessions[session.ID]; exists {
		return fmt.Errorf("%w: %s", domain.ErrSessionAlreadyExists, session.ID)
	}
	r.saved = append(r.saved, session)
	r.sessions[session.ID] = session
	return nil
}

func (r *fakeRepository) Save(_ context.Context, session domain.Session) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	if r.saveHook != nil {
		if err := r.saveHook(session); err != nil {
			return err
		}
	}
	r.saved = append(r.saved, session)
	r.sessions[session.ID] = session
	return nil
}

func (r *fakeRepository) Find(_ context.Context, id domain.ID) (domain.Session, error) {
	session, ok := r.sessions[id]
	if !ok {
		return domain.Session{}, errors.New("not found")
	}
	return session, nil
}

func (r *fakeRepository) ListCards(_ context.Context, query domain.ListQuery) ([]domain.Session, int, error) {
	r.lastListQuery = query
	filtered := make([]domain.Session, 0, len(r.listSessions))
	for _, session := range r.listSessions {
		if query.Scope != "" && string(session.Status) != query.Scope {
			continue
		}
		filtered = append(filtered, session)
	}
	total := r.listTotal
	if total == 0 {
		total = len(filtered)
	}
	return append([]domain.Session(nil), filtered...), total, nil
}

func (r *fakeRepository) ListQueued(context.Context) ([]domain.Session, error) {
	queued := make([]domain.Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		if session.Status == domain.StatusQueued {
			queued = append(queued, session)
		}
	}
	if r.listQueuedHook != nil {
		r.listQueuedHook()
		r.listQueuedHook = nil
	}
	return queued, nil
}

func (r *fakeRepository) ListInterruptedWithCodexSession(context.Context) ([]domain.Session, error) {
	return append([]domain.Session(nil), r.interruptedSessions...), nil
}

func (r *fakeRepository) CountByProject(_ context.Context, projectID domain.ProjectID) (int, error) {
	count := 0
	for _, session := range r.sessions {
		if session.ProjectID == projectID {
			count++
		}
	}
	return count, nil
}

func (r *fakeRepository) LastConfigForProject(_ context.Context, projectID domain.ProjectID) (domain.Config, bool, error) {
	r.lastConfigProjectID = projectID
	return r.lastConfig, r.hasLastConfig, nil
}

func (r *fakeRepository) AppendPrompt(_ context.Context, promptAppend domain.PromptAppend) error {
	if r.appendPromptHook != nil {
		r.appendPromptHook()
	}
	if r.appendPromptErr != nil {
		return r.appendPromptErr
	}
	r.appends = append(r.appends, promptAppend)
	return nil
}

func (r *fakeRepository) DeletePromptAppend(_ context.Context, id string) error {
	r.deletedAppends = append(r.deletedAppends, id)
	for index, promptAppend := range r.appends {
		if promptAppend.ID == id {
			r.appends = append(r.appends[:index], r.appends[index+1:]...)
			return nil
		}
	}
	return nil
}

func (r *fakeRepository) ListPromptAppends(_ context.Context, sessionID domain.ID) ([]domain.PromptAppend, error) {
	appends := make([]domain.PromptAppend, 0, len(r.appends))
	for _, promptAppend := range r.appends {
		if promptAppend.SessionID == sessionID {
			appends = append(appends, promptAppend)
		}
	}
	return appends, nil
}

func (r *fakeRepository) AddMergeRecord(_ context.Context, record domain.MergeRecord) error {
	if r.addMergeRecordErr != nil {
		return r.addMergeRecordErr
	}
	r.mergeRecords = append(r.mergeRecords, record)
	return nil
}

func (r *fakeRepository) LatestSuccessfulMergeRecord(context.Context, domain.ID) (domain.MergeRecord, bool, error) {
	return domain.MergeRecord{}, false, nil
}

func (r *fakeRepository) SaveStagedAttachment(_ context.Context, attachment domain.StagedAttachment) error {
	r.stagedAttachments[attachment.ID] = attachment
	return nil
}

func (r *fakeRepository) FindStagedAttachment(_ context.Context, id domain.StagedAttachmentID) (domain.StagedAttachment, error) {
	attachment, ok := r.stagedAttachments[id]
	if !ok {
		return domain.StagedAttachment{}, errors.New("not found")
	}
	return attachment, nil
}

func (r *fakeRepository) DeleteStagedAttachment(_ context.Context, id domain.StagedAttachmentID) error {
	if r.deleteStagedAttachmentErr != nil {
		return r.deleteStagedAttachmentErr
	}
	delete(r.stagedAttachments, id)
	return nil
}

func (r *fakeRepository) SaveSessionAttachment(_ context.Context, attachment domain.SessionAttachment) error {
	r.sessionAttachments[attachment.ID] = attachment
	return nil
}

func (r *fakeRepository) FindSessionAttachment(_ context.Context, id domain.SessionAttachmentID) (domain.SessionAttachment, error) {
	attachment, ok := r.sessionAttachments[id]
	if !ok {
		return domain.SessionAttachment{}, errors.New("not found")
	}
	return attachment, nil
}

func (r *fakeRepository) ListSessionAttachments(_ context.Context, sessionID domain.ID) ([]domain.SessionAttachment, error) {
	attachments := make([]domain.SessionAttachment, 0, len(r.sessionAttachments))
	for _, attachment := range r.sessionAttachments {
		if attachment.SessionID == sessionID {
			attachments = append(attachments, attachment)
		}
	}
	return attachments, nil
}

func (r *fakeRepository) ListPromptAppendAttachments(_ context.Context, sessionID domain.ID, appendID string) ([]domain.SessionAttachment, error) {
	r.lastPromptAppendAttachmentSessionID = sessionID
	r.lastPromptAppendAttachmentID = appendID
	attachments := make([]domain.SessionAttachment, 0, len(r.sessionAttachments))
	for _, attachment := range r.sessionAttachments {
		if attachment.SessionID == sessionID && attachment.SourceType == domain.AttachmentSourcePromptAppend && attachment.SourceID == appendID {
			attachments = append(attachments, attachment)
		}
	}
	return attachments, nil
}

func (r *fakeRepository) DeleteSessionAttachment(_ context.Context, id domain.SessionAttachmentID) error {
	delete(r.sessionAttachments, id)
	return nil
}

type fakeAttachmentStore struct {
	promoted        map[domain.StagedAttachmentID]bool
	deletedSessions map[domain.SessionAttachmentID]bool
	promoteErr      error
}

func newFakeAttachmentStore() *fakeAttachmentStore {
	return &fakeAttachmentStore{
		promoted:        map[domain.StagedAttachmentID]bool{},
		deletedSessions: map[domain.SessionAttachmentID]bool{},
	}
}

func (s *fakeAttachmentStore) Stage(context.Context, domain.StageAttachmentInput) (domain.StagedAttachment, error) {
	return domain.StagedAttachment{}, errors.New("unexpected Stage call")
}

func (s *fakeAttachmentStore) Promote(_ context.Context, staged domain.StagedAttachment, sessionID domain.ID) (domain.SessionAttachment, error) {
	if s.promoteErr != nil {
		return domain.SessionAttachment{}, s.promoteErr
	}
	s.promoted[staged.ID] = true
	return domain.SessionAttachment{
		ID:          domain.SessionAttachmentID(staged.ID),
		SessionID:   sessionID,
		Kind:        "file",
		Filename:    staged.Filename,
		Path:        "/attachments/sessions/" + string(sessionID) + "/" + string(staged.ID) + "/" + staged.Filename,
		MimeType:    staged.MimeType,
		Size:        staged.Size,
		Previewable: staged.Previewable,
		CreatedAt:   time.Unix(11, 0).UTC(),
	}, nil
}

func (s *fakeAttachmentStore) DeleteStaged(context.Context, domain.StagedAttachmentID) error {
	return errors.New("unexpected DeleteStaged call")
}

func (s *fakeAttachmentStore) DeleteSession(ctx context.Context, id domain.SessionAttachmentID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.deletedSessions[id] = true
	return nil
}

func (s *fakeAttachmentStore) Open(context.Context, string) (domain.AttachmentStream, error) {
	return domain.AttachmentStream{}, errors.New("unexpected Open call")
}

type fakeWorktreeManager struct {
	path              string
	headCommit        string
	snapshotCommit    string
	mergeBase         string
	createErr         error
	headCommitErr     error
	snapshotErr       error
	mergeBaseErr      error
	removeErr         error
	deleteBranchErr   error
	statErr           error
	createCalled      bool
	createProjectPath string
	createProjectID   domain.ProjectID
	createSessionID   domain.ID
	createBaseBranch  string
	headCommitPath    string
	headCommitRef     string
	snapshotPath      string
	snapshotBranch    string
	mergeBasePath     string
	mergeBaseRef      string
	removed           []string
	deletedBranches   []string
	missingPaths      map[string]bool
}

func newFakeWorktreeManager() *fakeWorktreeManager {
	return &fakeWorktreeManager{missingPaths: map[string]bool{}}
}

func (m *fakeWorktreeManager) Create(_ context.Context, projectPath string, projectID domain.ProjectID, sessionID domain.ID, baseBranch string) (string, error) {
	m.createCalled = true
	m.createProjectPath = projectPath
	m.createProjectID = projectID
	m.createSessionID = sessionID
	m.createBaseBranch = baseBranch
	if m.createErr != nil {
		return "", m.createErr
	}
	return m.path, nil
}

func (m *fakeWorktreeManager) HeadCommit(_ context.Context, path string, ref string) (string, error) {
	m.headCommitPath = path
	m.headCommitRef = ref
	if m.headCommitErr != nil {
		return "", m.headCommitErr
	}
	return m.headCommit, nil
}

func (m *fakeWorktreeManager) SnapshotCommit(_ context.Context, path string, branch string) (string, error) {
	m.snapshotPath = path
	m.snapshotBranch = branch
	if m.snapshotErr != nil {
		return "", m.snapshotErr
	}
	if m.snapshotCommit != "" {
		return m.snapshotCommit, nil
	}
	return m.headCommit, nil
}

func (m *fakeWorktreeManager) MergeBase(_ context.Context, worktreePath string, baseRef string) (string, error) {
	m.mergeBasePath = worktreePath
	m.mergeBaseRef = baseRef
	if m.mergeBaseErr != nil {
		return "", m.mergeBaseErr
	}
	return m.mergeBase, nil
}

func (m *fakeWorktreeManager) Exists(_ context.Context, path string) (bool, error) {
	if m.statErr != nil {
		return false, m.statErr
	}
	return !m.missingPaths[path], nil
}

func (m *fakeWorktreeManager) Remove(_ context.Context, path string) error {
	m.removed = append(m.removed, path)
	if m.removeErr != nil {
		return m.removeErr
	}
	m.setMissing(path, true)
	return nil
}

func (m *fakeWorktreeManager) DeleteBranch(_ context.Context, projectPath string, branch string) error {
	m.deletedBranches = append(m.deletedBranches, projectPath+":"+branch)
	if m.deleteBranchErr != nil {
		return m.deleteBranchErr
	}
	return nil
}

func (m *fakeWorktreeManager) PathForSession(projectID domain.ProjectID, sessionID domain.ID) string {
	return m.path
}

func (m *fakeWorktreeManager) setMissing(path string, missing bool) {
	if m.missingPaths == nil {
		m.missingPaths = map[string]bool{}
	}
	m.missingPaths[path] = missing
}

func (m *fakeWorktreeManager) resetCallState() {
	m.headCommitPath = ""
	m.headCommitRef = ""
	m.snapshotPath = ""
	m.snapshotBranch = ""
	m.mergeBasePath = ""
	m.mergeBaseRef = ""
	m.removed = nil
	m.deletedBranches = nil
}

type fakeMergePort struct {
	mergeInput   gitdiffdomain.MergeInput
	rebaseInput  gitdiffdomain.RebaseInput
	result       gitdiffdomain.MergeResult
	err          error
	mergeCalled  bool
	rebaseCalled bool
	abortPath    string
	abortErr     error
}

func (m *fakeMergePort) MergeToBase(_ context.Context, input gitdiffdomain.MergeInput) (gitdiffdomain.MergeResult, error) {
	m.mergeCalled = true
	m.mergeInput = input
	return m.result, m.err
}

func (m *fakeMergePort) RebaseOntoBase(_ context.Context, input gitdiffdomain.RebaseInput) (gitdiffdomain.MergeResult, error) {
	m.rebaseCalled = true
	m.rebaseInput = input
	return m.result, m.err
}

func (m *fakeMergePort) Abort(_ context.Context, worktreePath string) error {
	m.abortPath = worktreePath
	return m.abortErr
}

type fakeWorkflowStarter struct {
	input             domain.WorkflowStartInput
	start             domain.WorkflowStart
	err               error
	failedInput       domain.WorkflowStartFailureInput
	markFailErr       error
	completeInput     domain.WorkflowNodeCompleteInput
	advance           domain.WorkflowAdvance
	completeErr       error
	failInput         domain.WorkflowNodeFailInput
	failAdvance       domain.WorkflowAdvance
	failErr           error
	resumeInput       domain.WorkflowResumeFailureInput
	resumeSnapshot    domain.WorkflowRunSnapshot
	resumeErr         error
	resumeNodeInput   domain.WorkflowResumeCurrentNodeInput
	resumeNodeAdvance domain.WorkflowAdvance
	resumeNodeErr     error
	rerunInput        domain.WorkflowRerunCurrentNodeInput
	rerunAdvance      domain.WorkflowAdvance
	rerunErr          error
	approvalInput     domain.WorkflowApprovalInput
	approvalResult    domain.WorkflowApprovalResult
	approvalErr       error
}

func (s *fakeWorkflowStarter) StartForSession(_ context.Context, input domain.WorkflowStartInput) (domain.WorkflowStart, error) {
	s.input = input
	if s.err != nil {
		return domain.WorkflowStart{}, s.err
	}
	return s.start, nil
}

func (s *fakeWorkflowStarter) MarkStartFailed(_ context.Context, input domain.WorkflowStartFailureInput) error {
	s.failedInput = input
	return s.markFailErr
}

func (s *fakeWorkflowStarter) MarkResumeFailedForSession(_ context.Context, input domain.WorkflowResumeFailureInput) (domain.WorkflowRunSnapshot, error) {
	s.resumeInput = input
	if s.resumeErr != nil {
		return domain.WorkflowRunSnapshot{}, s.resumeErr
	}
	return s.resumeSnapshot, nil
}

func (s *fakeWorkflowStarter) ResumeCurrentNodeForSession(_ context.Context, input domain.WorkflowResumeCurrentNodeInput) (domain.WorkflowAdvance, error) {
	s.resumeNodeInput = input
	if s.resumeNodeErr != nil {
		return domain.WorkflowAdvance{}, s.resumeNodeErr
	}
	return s.resumeNodeAdvance, nil
}

func (s *fakeWorkflowStarter) RerunCurrentNodeForSession(_ context.Context, input domain.WorkflowRerunCurrentNodeInput) (domain.WorkflowAdvance, error) {
	s.rerunInput = input
	if s.rerunErr != nil {
		return domain.WorkflowAdvance{}, s.rerunErr
	}
	return s.rerunAdvance, nil
}

func (s *fakeWorkflowStarter) CompleteNode(_ context.Context, input domain.WorkflowNodeCompleteInput) (domain.WorkflowAdvance, error) {
	s.completeInput = input
	if s.completeErr != nil {
		return domain.WorkflowAdvance{}, s.completeErr
	}
	return s.advance, nil
}

func (s *fakeWorkflowStarter) FailNode(_ context.Context, input domain.WorkflowNodeFailInput) (domain.WorkflowAdvance, error) {
	s.failInput = input
	if s.failErr != nil {
		return domain.WorkflowAdvance{}, s.failErr
	}
	return s.failAdvance, nil
}

func (s *fakeWorkflowStarter) SubmitApprovalForSession(_ context.Context, input domain.WorkflowApprovalInput) (domain.WorkflowApprovalResult, error) {
	s.approvalInput = input
	if s.approvalErr != nil {
		return domain.WorkflowApprovalResult{}, s.approvalErr
	}
	return s.approvalResult, nil
}

type fakeProcessRepository struct {
	created      []processdomain.Run
	active       processdomain.Run
	hasActive    bool
	activeCount  int
	runningID    processdomain.RunID
	runningPID   int
	runningCodex string
	exitedID     processdomain.RunID
	exitedResult processdomain.ExitResult
}

func newFakeProcessRepository() *fakeProcessRepository {
	return &fakeProcessRepository{}
}

func (r *fakeProcessRepository) CreateRun(_ context.Context, run processdomain.Run) error {
	r.created = append(r.created, run)
	r.active = run
	r.hasActive = true
	return nil
}

func (r *fakeProcessRepository) FindActiveBySession(_ context.Context, sessionID processdomain.SessionID) (processdomain.Run, bool, error) {
	if r.hasActive && r.active.SessionID == sessionID {
		return r.active, true, nil
	}
	return processdomain.Run{}, false, nil
}

func (r *fakeProcessRepository) CountActive(context.Context) (int, error) {
	if r.activeCount > 0 {
		return r.activeCount, nil
	}
	if r.hasActive && (r.active.Status == processdomain.StatusStarting || r.active.Status == processdomain.StatusRunning) {
		return 1, nil
	}
	return 0, nil
}

func (r *fakeProcessRepository) MarkWaitingUser(_ context.Context, id processdomain.RunID) error {
	r.active.ID = id
	r.active.Status = processdomain.StatusWaitingUser
	r.hasActive = true
	return nil
}

func (r *fakeProcessRepository) MarkRunning(_ context.Context, id processdomain.RunID, pid int, codexSessionID string) error {
	r.runningID = id
	r.runningPID = pid
	r.runningCodex = codexSessionID
	r.active.ID = id
	r.active.Status = processdomain.StatusRunning
	r.active.PID = &pid
	r.active.CodexSessionID = codexSessionID
	r.hasActive = true
	return nil
}

func (r *fakeProcessRepository) MarkExited(_ context.Context, id processdomain.RunID, result processdomain.ExitResult) error {
	r.exitedID = id
	r.exitedResult = result
	if r.active.ID == id {
		r.hasActive = false
	}
	return nil
}

type fakeQuestionCanceller struct {
	cancelledSessionID questiondomain.SessionID
	cancelReason       string
	cancelErr          error
	created            questionapp.CreateBatchInput
	batch              questionapp.BatchDTO
	createErr          error
}

func (c *fakeQuestionCanceller) CreateBatch(_ context.Context, input questionapp.CreateBatchInput) (questionapp.BatchDTO, error) {
	c.created = input
	if c.createErr != nil {
		return questionapp.BatchDTO{}, c.createErr
	}
	if c.batch.ID == "" {
		c.batch = questionapp.BatchDTO{
			ID:            "batch-1",
			SessionID:     input.SessionID,
			WorkflowRunID: input.WorkflowRunID,
			Status:        questiondomain.BatchPending,
			Questions:     input.Questions,
		}
	}
	return c.batch, nil
}

func (c *fakeQuestionCanceller) CancelPendingBySession(_ context.Context, sessionID questiondomain.SessionID, reason string) error {
	c.cancelledSessionID = sessionID
	c.cancelReason = reason
	return c.cancelErr
}

type fakeCodexProcess struct {
	startCalled  bool
	startInput   processdomain.CodexStartInput
	startInputs  []processdomain.CodexStartInput
	startHandle  processdomain.CodexHandle
	startHandles []processdomain.CodexHandle
	startErr     error
	startErrs    []error
	resumeCalled bool
	resumeInput  processdomain.CodexResumeInput
	resumeHandle processdomain.CodexHandle
	resumeErr    error
	stoppedID    processdomain.RunID
	stopErr      error
	eventsErr    error
	events       <-chan processdomain.CodexEvent
	eventStreams []<-chan processdomain.CodexEvent
}

func (p *fakeCodexProcess) Probe(context.Context) (processdomain.CodexCapabilities, error) {
	return processdomain.CodexCapabilities{}, nil
}

func (p *fakeCodexProcess) Start(_ context.Context, input processdomain.CodexStartInput) (processdomain.CodexHandle, error) {
	p.startCalled = true
	p.startInput = input
	p.startInputs = append(p.startInputs, input)
	if len(p.startErrs) > 0 {
		err := p.startErrs[0]
		p.startErrs = p.startErrs[1:]
		if err != nil {
			return processdomain.CodexHandle{}, err
		}
	} else if p.startErr != nil {
		return processdomain.CodexHandle{}, p.startErr
	}
	handle := p.startHandle
	if len(p.startHandles) > 0 {
		handle = p.startHandles[0]
		p.startHandles = p.startHandles[1:]
	}
	handle.ProcessRunID = input.ProcessRunID
	return handle, nil
}

func (p *fakeCodexProcess) Resume(_ context.Context, input processdomain.CodexResumeInput) (processdomain.CodexHandle, error) {
	p.resumeCalled = true
	p.resumeInput = input
	if p.resumeErr != nil {
		return processdomain.CodexHandle{}, p.resumeErr
	}
	handle := p.resumeHandle
	handle.ProcessRunID = input.ProcessRunID
	return handle, nil
}

func (p *fakeCodexProcess) Stop(_ context.Context, id processdomain.RunID) error {
	p.stoppedID = id
	return p.stopErr
}

func (p *fakeCodexProcess) Events(context.Context, processdomain.CodexHandle) (<-chan processdomain.CodexEvent, error) {
	if p.eventsErr != nil {
		return nil, p.eventsErr
	}
	if len(p.eventStreams) > 0 {
		events := p.eventStreams[0]
		p.eventStreams = p.eventStreams[1:]
		return events, nil
	}
	if p.events != nil {
		return p.events, nil
	}
	return nil, errors.New("events not configured")
}

type fakeUnitOfWork struct {
	called                bool
	tx                    fakeTx
	err                   error
	publisher             *fakeEventPublisher
	publishedBeforeReturn int
	publishedDuringCall   bool
}

func (u *fakeUnitOfWork) Do(ctx context.Context, fn func(context.Context, port.Tx) error) error {
	u.called = true
	publishedBefore := 0
	if u.publisher != nil {
		publishedBefore = len(u.publisher.snapshot())
	}
	if err := fn(ctx, u.tx); err != nil {
		return err
	}
	if u.publisher != nil {
		u.publishedBeforeReturn = len(u.publisher.snapshot())
		if u.publishedBeforeReturn != publishedBefore {
			u.publishedDuringCall = true
		}
	}
	return u.err
}

type fakeSessionLocker struct {
	ids []domain.ID
}

func (l *fakeSessionLocker) WithSessionLock(ctx context.Context, id domain.ID, fn func(context.Context) error) error {
	l.ids = append(l.ids, id)
	return fn(ctx)
}

type fakeTx struct {
	projects  projectdomain.Repository
	sessions  domain.Repository
	workflows workflowdomain.Repository
	questions questiondomain.Repository
	processes processdomain.Repository
	events    eventdomain.Store
}

func (tx fakeTx) Projects() projectdomain.Repository {
	return tx.projects
}

func (tx fakeTx) Sessions() domain.Repository {
	return tx.sessions
}

func (tx fakeTx) Workflows() workflowdomain.Repository {
	return tx.workflows
}

func (tx fakeTx) Questions() questiondomain.Repository {
	return tx.questions
}

func (tx fakeTx) Processes() processdomain.Repository {
	return tx.processes
}

func (tx fakeTx) Events() eventdomain.Store {
	return tx.events
}

type fakeEventStore struct {
	mu     sync.Mutex
	events []eventdomain.DomainEvent
}

type fakeEventPublisher struct {
	mu        sync.Mutex
	events    []eventdomain.DomainEvent
	onPublish func(eventdomain.DomainEvent)
}

func (p *fakeEventPublisher) PublishAfterCommit(_ context.Context, event eventdomain.DomainEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.onPublish != nil {
		p.onPublish(event)
	}
	p.events = append(p.events, event)
	return nil
}

func (p *fakeEventPublisher) snapshot() []eventdomain.DomainEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := make([]eventdomain.DomainEvent, len(p.events))
	copy(events, p.events)
	return events
}

func (s *fakeEventStore) Append(_ context.Context, event eventdomain.DomainEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *fakeEventStore) List(_ context.Context, _ eventdomain.Scope) ([]eventdomain.DomainEvent, error) {
	return s.snapshot(), nil
}

func (s *fakeEventStore) After(_ context.Context, _ eventdomain.Scope, _ eventdomain.ID) ([]eventdomain.DomainEvent, error) {
	return s.snapshot(), nil
}

func (s *fakeEventStore) Before(_ context.Context, _ eventdomain.Scope, before eventdomain.ID, limit int) ([]eventdomain.DomainEvent, int, bool, error) {
	events := s.snapshot()
	end := len(events)
	if before != "" {
		end = -1
		for index, event := range events {
			if event.ID == before {
				end = index
				break
			}
		}
		if end < 0 {
			return nil, 0, false, errors.New("before event not found")
		}
	}
	if limit < 1 {
		limit = 1
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return events[start:end], len(events), start > 0, nil
}

func (s *fakeEventStore) snapshot() []eventdomain.DomainEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]eventdomain.DomainEvent, len(s.events))
	copy(events, s.events)
	return events
}

func waitForEventType(t *testing.T, store *fakeEventStore, eventType string) eventdomain.DomainEvent {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, event := range store.snapshot() {
			if event.Type == eventType {
				return event
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("event type %q not found in %#v", eventType, store.snapshot())
	return eventdomain.DomainEvent{}
}

func waitForPublishedEventType(t *testing.T, publisher *fakeEventPublisher, eventType string) eventdomain.DomainEvent {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, event := range publisher.snapshot() {
			if event.Type == eventType {
				return event
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("published event type %q not found in %#v", eventType, publisher.snapshot())
	return eventdomain.DomainEvent{}
}

type fakeProjectRepository struct {
	projects map[projectdomain.ID]projectdomain.Project
}

func newFakeProjectRepository(ids ...projectdomain.ID) *fakeProjectRepository {
	repo := &fakeProjectRepository{projects: map[projectdomain.ID]projectdomain.Project{}}
	for _, id := range ids {
		repo.projects[id] = projectdomain.Project{ID: id, Name: string(id)}
	}
	return repo
}

func (r *fakeProjectRepository) Save(context.Context, projectdomain.Project) error {
	return errors.New("unexpected project Save call")
}

func (r *fakeProjectRepository) Find(_ context.Context, id projectdomain.ID) (projectdomain.Project, error) {
	project, ok := r.projects[id]
	if !ok {
		return projectdomain.Project{}, errors.New("not found")
	}
	return project, nil
}

func (r *fakeProjectRepository) FindByPath(context.Context, string) (projectdomain.Project, bool, error) {
	return projectdomain.Project{}, false, errors.New("unexpected project FindByPath call")
}

func (r *fakeProjectRepository) List(context.Context) ([]projectdomain.Project, error) {
	return nil, errors.New("unexpected project List call")
}

func (r *fakeProjectRepository) Remove(context.Context, projectdomain.ID, time.Time) error {
	return errors.New("unexpected project Remove call")
}

func (r *fakeProjectRepository) UpdateDefaultWorkflow(context.Context, projectdomain.ID, projectdomain.WorkflowDefinitionID) error {
	return errors.New("unexpected project UpdateDefaultWorkflow call")
}
