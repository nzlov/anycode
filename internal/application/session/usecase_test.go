package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	"github.com/nzlov/anycode/internal/application/port"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	gitdiffdomain "github.com/nzlov/anycode/internal/domain/gitdiff"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	domain "github.com/nzlov/anycode/internal/domain/session"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
)

func TestPromptWithArtifactGuidanceDoesNotExposeArtifactPath(t *testing.T) {
	got := promptWithArtifactGuidance("ship it", "/data/attachments/outputs/session-1")
	if !strings.Contains(got, "ANYCODE_ARTIFACT_DIR") {
		t.Fatalf("artifact guidance = %q", got)
	}
	if !strings.Contains(got, "临时文件") || strings.Contains(got, "产物") {
		t.Fatalf("artifact guidance label = %q", got)
	}
	if strings.Contains(got, "/data/attachments/outputs/session-1") {
		t.Fatalf("artifact guidance exposed disk path: %q", got)
	}
}

func TestSanitizeCodexPayloadValueRemovesRawArtifactSources(t *testing.T) {
	payload := sanitizeCodexPayloadValue(map[string]any{
		"item": map[string]any{
			"type": "tool_result",
			"content": []any{
				map[string]any{"type": "output_image", "image_url": "data:image/png;base64,secret", "path": "/private/image.png", "detail": "high"},
				map[string]any{"type": "resource", "resource": map[string]any{"blob": "secret-blob", "uri": "file:///private/report.pdf", "mimeType": "application/pdf"}},
			},
		},
		"large": strings.Repeat("x", maxPersistedCodexStringBytes+1),
	}, false).(map[string]any)
	item := payload["item"].(map[string]any)
	content := item["content"].([]any)
	image := content[0].(map[string]any)
	if image["image_url"] != nil || image["path"] != nil || image["detail"] != "high" {
		t.Fatalf("sanitized image payload = %#v", image)
	}
	resource := content[1].(map[string]any)["resource"].(map[string]any)
	if resource["blob"] != nil || resource["uri"] != nil || resource["mimeType"] != "application/pdf" {
		t.Fatalf("sanitized resource payload = %#v", resource)
	}
	if payload["large"] != "[omitted large value]" {
		t.Fatalf("sanitized large payload = %#v", payload["large"])
	}
}

func TestArchiveCodexEventImagesSanitizesUnknownContent(t *testing.T) {
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"))
	event := processdomain.CodexEvent{
		EventID: "unknown-1",
		Content: processdomain.CodexUnknownContent{RawType: "vendor.output", Payload: map[string]any{
			"content": []any{map[string]any{"type": "audio", "data": "YXVkaW8=", "mimeType": "audio/mpeg"}},
		}},
	}
	if failures := service.archiveCodexEventImages(context.Background(), domain.Session{ID: "session-1"}, processdomain.CodexHandle{}, &event); len(failures) != 0 {
		t.Fatalf("archive failures = %#v", failures)
	}
	content := event.Content.(processdomain.CodexUnknownContent)
	audio := content.Payload["content"].([]any)[0].(map[string]any)
	if audio["data"] != nil || audio["mimeType"] != "audio/mpeg" {
		t.Fatalf("unknown content was not sanitized = %#v", content)
	}
}

func TestCreateSessionDefaultsModeAndSavesRequestedConfig(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }
	fastMode := true

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "  implement app session  ",
		Config: ConfigInput{
			CodexModel:      "gpt-5.4-mini",
			ReasoningEffort: "medium",
			PermissionMode:  "workspace-write",
			FastMode:        &fastMode,
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
	if saved.Status != domain.StatusQueued || saved.Mode != domain.ModeChat || saved.Queue.Kind != domain.QueueKindStart || !saved.Queue.InitialStart {
		t.Fatalf("saved session status/mode = %q/%q", saved.Status, saved.Mode)
	}
	if !reflect.DeepEqual(saved.Config, got.Config) {
		t.Fatalf("saved config = %#v, want %#v", saved.Config, got.Config)
	}
	if saved.LastRunAt == nil || saved.CodexSessionID != "" || saved.WorktreePath != "" {
		t.Fatalf("CreateSession() should queue without starting codex: %#v", saved)
	}
}

func TestCreateSessionDoesNotInheritMissingConfigFromProjectSessions(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["previous"] = domain.Session{
		ID:        "previous",
		ProjectID: "project-1",
		Config: domain.Config{
			CodexModel:      "gpt-5.4",
			ReasoningEffort: "high",
			PermissionMode:  "workspace-write",
			FastMode:        true,
		},
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
		Config: ConfigInput{
			CodexModel: "gpt-5.5",
		},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	want := domain.Config{
		CodexModel: "gpt-5.5",
	}
	if !reflect.DeepEqual(got.Config, want) {
		t.Fatalf("Config = %#v, want %#v", got.Config, want)
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

func TestCreateSessionIgnoresBaseBranchForNonGitProject(t *testing.T) {
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Name:  "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: false,
	}
	service := New(repo, projects)
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(context.Background(), CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
		BaseBranch:  "main",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.BaseBranch != "" || got.WorktreeBranch != "" || got.WorktreePath != "/workspace/project-1" {
		t.Fatalf("CreateSession() = %#v", got)
	}
	if saved := repo.sessions["session-1"]; saved.BaseBranch != "" {
		t.Fatalf("saved non-git base branch = %q", saved.BaseBranch)
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
	if len(repo.saved) != 3 || repo.saved[0].WorktreeCleanup.Status != domain.WorktreeCleanupProvisioning || repo.saved[1].WorktreeCleanup.Status != domain.WorktreeCleanupActive || repo.saved[len(repo.saved)-1].WorktreePath != got.WorktreePath {
		t.Fatalf("saved sessions = %#v", repo.saved)
	}
}

func TestCreateSessionPersistsCleanupAfterRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := newFakeRepository()
	repo.rejectCanceledContext = true
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	noOwnership := domain.WorktreeOwnership{}
	worktrees := &fakeWorktreeManager{
		createErr: errors.New("worktree creation canceled"),
		onCreate:  cancel,
		ownership: &noOwnership,
	}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	_, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
		BaseBranch:  "main",
	})
	if err == nil || !strings.Contains(err.Error(), "worktree creation canceled") {
		t.Fatalf("CreateSession() error = %v", err)
	}
	saved := repo.sessions["session-1"]
	if saved.Status != domain.StatusFailed || saved.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("saved canceled session = %#v", saved)
	}
	if _, err := service.DrainWorktreeCleanup(context.Background()); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if cleaned := repo.sessions["session-1"]; cleaned.WorktreeCleanup.Status != domain.WorktreeCleanupCleaned {
		t.Fatalf("cleaned canceled session = %#v", cleaned)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("unconfirmed resources were deleted: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
}

func TestCreateSessionHoldsSessionLockUntilInitialQueueIsPersisted(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	createStarted := make(chan struct{})
	releaseCreate := make(chan struct{})
	worktrees := &fakeWorktreeManager{createStarted: createStarted, releaseCreate: releaseCreate}
	service := New(repo, projects, WithWorktrees(worktrees), WithSessionLocker(NewMemorySessionLocker()))
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	createResult := make(chan error, 1)
	go func() {
		_, err := service.CreateSession(ctx, CreateSessionInput{
			ProjectID:   "project-1",
			Requirement: "implement app session",
			BaseBranch:  "main",
		})
		createResult <- err
	}()
	<-createStarted

	type closeResult struct {
		dto DTO
		err error
	}
	closeStarted := make(chan struct{})
	closed := make(chan closeResult, 1)
	go func() {
		close(closeStarted)
		dto, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
		closed <- closeResult{dto: dto, err: err}
	}()
	<-closeStarted
	select {
	case result := <-closed:
		t.Fatalf("CloseSession() completed during creation: dto=%#v err=%v", result.dto, result.err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseCreate)
	if err := <-createResult; err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	result := <-closed
	if result.err != nil {
		t.Fatalf("CloseSession() error = %v", result.err)
	}
	if result.dto.Status != domain.StatusClosed || result.dto.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", result.dto)
	}
	if saved := repo.sessions["session-1"]; saved.Status != domain.StatusClosed || saved.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("final session = %#v", saved)
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

func TestCreateSessionRunsWorktreeInitBeforeArchivingAttachments(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{ID: "staged-1", Filename: "note.txt"}
	projects := newFakeProjectRepository()
	command := "  echo first\necho second\n"
	projects.projects["project-1"] = projectdomain.Project{
		ID:                  "project-1",
		Path:                projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit:               true,
		WorktreeInitCommand: command,
	}
	files := newFakeAttachmentStore()
	initializer := &fakeWorktreeInitializer{
		result: domain.WorktreeInitResult{Success: true},
		onRun: func() {
			if _, ok := repo.stagedAttachments["staged-1"]; !ok {
				t.Fatal("worktree init ran after staged attachment metadata was deleted")
			}
			if files.promoted["staged-1"] {
				t.Fatal("worktree init ran after attachment promotion")
			}
		},
	}
	worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1", headCommit: "base"}
	service := New(repo, projects, WithAttachments(repo, files), WithWorktrees(worktrees), WithWorktreeInitializer(initializer))
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	got, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:           "project-1",
		Requirement:         "use attachment",
		BaseBranch:          "main",
		StagedAttachmentIDs: []domain.StagedAttachmentID{"staged-1"},
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if !initializer.called || initializer.worktreePath != worktrees.path || initializer.script != command {
		t.Fatalf("worktree initializer call = called:%t path:%q script:%q", initializer.called, initializer.worktreePath, initializer.script)
	}
	if !files.promoted["staged-1"] || got.Status != domain.StatusQueued {
		t.Fatalf("CreateSession() result = promoted:%t session:%#v", files.promoted["staged-1"], got)
	}
}

func TestCreateSessionRecordsWorktreeInitFailureAndContinues(t *testing.T) {
	tests := []struct {
		name   string
		result domain.WorktreeInitResult
		err    error
	}{
		{name: "nonzero exit", result: domain.WorktreeInitResult{ExitCode: intPointer(7), Output: "setup failed\n"}},
		{name: "start failure", err: errors.New("start failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repo := newFakeRepository()
			projects := newFakeProjectRepository()
			projects.projects["project-1"] = projectdomain.Project{
				ID:                  "project-1",
				Path:                projectdomain.ProjectPath{Value: "/workspace/project-1"},
				IsGit:               true,
				WorktreeInitCommand: "./setup.sh",
			}
			worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1", headCommit: "base"}
			initializer := &fakeWorktreeInitializer{result: tt.result, err: tt.err}
			events := &fakeEventStore{}
			service := New(repo, projects, WithWorktrees(worktrees), WithWorktreeInitializer(initializer), WithEvents(events))
			service.now = func() time.Time { return time.Unix(10, 0).UTC() }
			ids := []domain.ID{"session-1", "event-init", "event-queued"}
			service.generateID = func() (domain.ID, error) {
				id := ids[0]
				ids = ids[1:]
				return id, nil
			}

			got, err := service.CreateSession(ctx, CreateSessionInput{
				ProjectID:   "project-1",
				Requirement: "create card",
				BaseBranch:  "main",
			})
			if err != nil {
				t.Fatalf("CreateSession() error = %v", err)
			}
			if got.Status != domain.StatusQueued {
				t.Fatalf("session status = %q", got.Status)
			}
			if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
				t.Fatalf("worktree was cleaned after init failure: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
			}
			event := waitForEventType(t, events, "session.worktree_init_failed")
			if event.Payload["output"] != tt.result.Output || event.Payload["outputTruncated"] != tt.result.OutputTruncated {
				t.Fatalf("failure event payload = %#v", event.Payload)
			}
			if tt.result.ExitCode != nil && event.Payload["exitCode"] != *tt.result.ExitCode {
				t.Fatalf("failure event exitCode = %#v", event.Payload["exitCode"])
			}
			if tt.err != nil && event.Payload["error"] != tt.err.Error() {
				t.Fatalf("failure event error = %#v", event.Payload["error"])
			}
		})
	}
}

func TestCreateSessionContinuesWhenWorktreeInitFailureEventCannotBeRecorded(t *testing.T) {
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                  "project-1",
		Path:                projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit:               true,
		WorktreeInitCommand: "exit 1",
	}
	worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1"}
	events := &fakeEventStore{appendErrs: []error{errors.New("event store failed"), nil}}
	service := New(repo, projects,
		WithWorktrees(worktrees),
		WithWorktreeInitializer(&fakeWorktreeInitializer{result: domain.WorktreeInitResult{ExitCode: intPointer(1)}}),
		WithEvents(events),
	)
	ids := []domain.ID{"session-1", "event-init", "event-queued"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.CreateSession(context.Background(), CreateSessionInput{ProjectID: "project-1", Requirement: "create card", BaseBranch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued || len(worktrees.removed) != 0 || repo.sessions["session-1"].Status != domain.StatusQueued {
		t.Fatalf("worktree/session changed after event persistence failure: removed=%#v session=%#v", worktrees.removed, repo.sessions["session-1"])
	}
}

func TestCreateSessionSkipsWorktreeInitForBlankCommandAndNonGitProject(t *testing.T) {
	tests := []struct {
		name    string
		project projectdomain.Project
		branch  string
	}{
		{name: "blank command", project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true, WorktreeInitCommand: " \n\t"}, branch: "main"},
		{name: "non git", project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, WorktreeInitCommand: "echo should-not-run"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			projects := newFakeProjectRepository()
			projects.projects["project-1"] = tt.project
			worktrees := &fakeWorktreeManager{path: "/data/worktrees/project-1/session-1"}
			initializer := &fakeWorktreeInitializer{result: domain.WorktreeInitResult{Success: true}}
			service := New(repo, projects, WithWorktrees(worktrees), WithWorktreeInitializer(initializer))
			service.generateID = func() (domain.ID, error) { return "session-1", nil }

			if _, err := service.CreateSession(context.Background(), CreateSessionInput{ProjectID: "project-1", Requirement: "create card", BaseBranch: tt.branch}); err != nil {
				t.Fatalf("CreateSession() error = %v", err)
			}
			if initializer.called {
				t.Fatal("worktree initializer should not be called")
			}
		})
	}
}

func TestCreateSessionStopsWhenRequestIsCancelledDuringWorktreeInit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                  "project-1",
		Path:                projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit:               true,
		WorktreeInitCommand: "sleep 30",
	}
	initializer := &fakeWorktreeInitializer{err: context.Canceled, onRun: cancel}
	service := New(repo, projects, WithWorktrees(&fakeWorktreeManager{path: "/data/worktrees/project-1/session-1"}), WithWorktreeInitializer(initializer))
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	_, err := service.CreateSession(ctx, CreateSessionInput{ProjectID: "project-1", Requirement: "create card", BaseBranch: "main"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateSession() error = %v, want context.Canceled", err)
	}
	if saved := repo.sessions["session-1"]; saved.Status != domain.StatusFailed || saved.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("session after cancelled initialization = %#v", saved)
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
		SessionID:        "session-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Validate build",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-1"}}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes}}))
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
	if codex.startCalled || !saved.Queue.InitialStart || saved.ID != "session-1" || saved.Queue.NodeRunID == nil || *saved.Queue.NodeRunID != "node-run-1" || saved.Queue.Prompt != "Validate build" {
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
		SessionID:        "session-1",
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

func TestWorkflowAdvanceClosesRunningSessionWithoutActiveProcess(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning,
	}
	repo.sessions[session.ID] = session
	events := &fakeEventStore{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}),
		WithEvents(events),
	)
	ids := []domain.ID{"event-stopped", "event-closed"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }

	got, err := service.applyWorkflowAdvance(ctx, session, domain.WorkflowAdvance{
		SessionID: "session-1",
		Close:     true,
	}, workflowAdvanceOptions{})
	if err != nil {
		t.Fatalf("applyWorkflowAdvance() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("session status = %q", got.Status)
	}
	saved := repo.sessions[session.ID]
	if saved.CloseReason == nil || *saved.CloseReason != domain.CloseReasonWorkflowClosed {
		t.Fatalf("close reason = %#v", saved.CloseReason)
	}
	gotEvents := events.snapshot()
	requireSessionEventTypes(t, gotEvents, "session.closing", sessionStatusUpdatedEvent, "session.closed", sessionStatusUpdatedEvent)
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
		SessionID:        "session-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Run workflow node",
	}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-1"}}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes}}))
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
	if saved := repo.sessions["session-1"]; saved.Queue.InitialStart || saved.Queue.NodeRunID == nil || *saved.Queue.NodeRunID != "node-run-1" || saved.Queue.Prompt != "Run workflow node" {
		t.Fatalf("queued session = %#v", saved)
	}
}

func TestRestartedWorkflowNodePromptIncludesUnifiedAnyCodeGuidance(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "ship feature",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		BaseBranch:   "main",
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
		SessionID:        "session-1",
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
	for _, want := range []string{"`answer_user`", "`update_plan`", "不得删除、移动、重建或清理当前工作树"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("restarted workflow prompt missing unified AnyCode guidance %q: %q", want, prompt)
		}
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
		SessionID:        "session-1",
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
	} else if saved.Queue.Prompt != "Run current node again" || !saved.Queue.ReviewAfterReuseFailure {
		t.Fatalf("queued execution intent = %#v", saved.Queue)
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
		SessionID:        "session-1",
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
	queue := repo.sessions["session-1"].Queue
	if queue.Prompt != "Run current node again" || !queue.ReviewAfterReuseFailure {
		t.Fatalf("queued execution intent = %#v", queue)
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
		SessionID:        "session-1",
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
			SessionID:        "session-1",
			NodeRunID:        &nodeRunID,
			CurrentNodeID:    "merge",
			CurrentNodeTitle: "Merge",
			Status:           "running",
			Merge:            &domain.WorkflowMerge{Strategy: "merge"},
		},
		advance: domain.WorkflowAdvance{
			SessionID: "session-1",
			Status:    "completed",
			Completed: true,
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
	if len(worktrees.removed) != 0 {
		t.Fatalf("merge close performed synchronous cleanup = %#v", worktrees.removed)
	}
	if got.WorktreePath == "" || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("closed session cleanup = %#v", got)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreePath == "" || saved.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("saved cleanup = %#v", saved)
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
			SessionID:        "session-1",
			NodeRunID:        &nodeRunID,
			CurrentNodeID:    "merge",
			CurrentNodeTitle: "Merge",
			Status:           "running",
			Merge:            &domain.WorkflowMerge{Strategy: "rebase"},
		},
		failAdvance: domain.WorkflowAdvance{
			SessionID:        "session-1",
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
	results, ok := workflows.failInput.Output["results"].(map[string]any)
	if !ok {
		t.Fatalf("result output = %#v", workflows.failInput.Output)
	}
	data, ok := results["data"].(map[string]any)
	if !ok {
		t.Fatalf("result data = %#v", results)
	}
	mergeOutput, ok := data["merge"].(map[string]any)
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
			SessionID:        "session-1",
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
	if questions.created.SessionID != "session-1" {
		t.Fatalf("created question batch input = %#v", questions.created)
	}
	if len(questions.created.Questions) != 1 {
		t.Fatalf("created questions = %#v", questions.created.Questions)
	}
	question := questions.created.Questions[0]
	if question.Type != "merge_failure_action" || question.Metadata["nodeRunId"] != "node-run-merge" {
		t.Fatalf("merge failure question = %#v", question)
	}
	if _, ok := question.Metadata["sessionId"]; ok {
		t.Fatalf("merge failure question contains duplicate session id: %#v", question.Metadata)
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
			SessionID:        "session-1",
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
			SessionID: "session-1",
			Status:    "completed",
			Completed: true,
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
	data, dataOK := results["data"].(map[string]any)
	if !ok || !dataOK || data["status"] != "passed" || data["count"] != float64(2) {
		t.Fatalf("expr results = %#v", workflows.completeInput.Output)
	}
	if workflows.completeInput.NodeRunID != "node-run-expr" {
		t.Fatalf("complete input = %#v", workflows.completeInput)
	}
}

func TestRestartedWorkflowExprDoesNotMarkNextCodexInitial(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		DefaultWorkflowID: &workflowID,
	}
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusStopped,
		BaseBranch:   "main",
		WorktreePath: "/workspace/project-1",
	}
	exprNodeRunID := domain.NodeRunID("node-run-expr")
	nextNodeRunID := domain.NodeRunID("node-run-build")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			SessionID:     "session-2",
			NodeRunID:     &exprNodeRunID,
			CurrentNodeID: "derive",
			Status:        "running",
			Expr: &domain.WorkflowExpr{
				Script: `{status: "ready"}`,
			},
		},
		advance: domain.WorkflowAdvance{
			SessionID:     "session-2",
			NodeRunID:     &nextNodeRunID,
			CurrentNodeID: "build",
			Status:        "running",
			RequiresCodex: true,
			Prompt:        "Build after expr",
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-2"}}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes}}))
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	if _, err := service.StartSession(ctx, "session-1"); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	queued := repo.sessions["session-1"]
	if queued.Status != domain.StatusQueued || queued.Queue.InitialStart {
		t.Fatalf("restarted expr queued session = %#v", queued)
	}
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if codex.startInput.Prompt != promptWithAnyCodeGuidance("Build after expr", repo.sessions["session-1"]) {
		t.Fatalf("restarted expr prompt = %q", codex.startInput.Prompt)
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
			SessionID: "session-1",
			Status:    "completed",
			Completed: true,
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
					"sessionId":   "forged-session",
					"nodeRunId":   string(nodeRunID),
					"strategy":    "merge",
					"failureCode": "merge_failed",
				},
				SelectedOptionID: &optionID,
				Options: []questiondomain.Option{
					{ID: "retry_merge", Payload: map[string]any{"action": "retry_merge"}},
				},
				Answer: map[string]any{
					"action":    "fail_node",
					"sessionId": "forged-workflow-run",
					"nodeRunId": "forged-node-run",
					"strategy":  "rebase",
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
	if workflows.completeInput.SessionID != "session-1" || workflows.completeInput.NodeRunID != nodeRunID {
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
			SessionID:        "session-1",
			CurrentNodeID:    "approve",
			CurrentNodeTitle: "Approve merge failure",
			Status:           "running",
			RequiresCodex:    false,
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
	if workflows.failInput.SessionID != "session-1" || workflows.failInput.NodeRunID != nodeRunID || workflows.failInput.Code != "dirty_worktree" {
		t.Fatalf("fail input = %#v", workflows.failInput)
	}
	results, ok := workflows.failInput.Output["results"].(map[string]any)
	if !ok {
		t.Fatalf("result output = %#v", workflows.failInput.Output)
	}
	data, ok := results["data"].(map[string]any)
	if !ok {
		t.Fatalf("result data = %#v", results)
	}
	mergeOutput, ok := data["merge"].(map[string]any)
	if !ok || mergeOutput["status"] != "failed" || mergeOutput["failureCode"] != "dirty_worktree" || mergeOutput["failureReason"] != "worktree has uncommitted changes" {
		t.Fatalf("merge output = %#v", workflows.failInput.Output)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusWaitingApproval {
		t.Fatalf("session status = %q", got)
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
					"nodeRunId": "node-run-merge",
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
		SessionID: "session-1",
		NodeRunID: &nodeRunID,
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

func TestAskMergeFailurePublishesWaitingUserStatusUpdate(t *testing.T) {
	repo := newFakeRepository()
	questions := &fakeQuestionCanceller{batch: questionapp.BatchDTO{ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchPending, Created: true}}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithQuestions(questions), WithEvents(events))
	nodeRunID := domain.NodeRunID("node-run-merge")

	got, err := service.askMergeFailure(context.Background(), domain.Session{
		ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning,
	}, domain.WorkflowAdvance{
		SessionID: "session-1", NodeRunID: &nodeRunID,
	}, gitdiffdomain.MergeResult{Strategy: "merge", Status: "failed", FailureReason: "conflict"}, "merge_conflict")
	if err != nil {
		t.Fatalf("askMergeFailure() error = %v", err)
	}
	if got.Status != domain.StatusWaitingUser || repo.sessions["session-1"].Status != domain.StatusWaitingUser {
		t.Fatalf("waiting session = %#v persisted=%#v", got, repo.sessions["session-1"])
	}
	requireSessionEventTypes(t, events.snapshot(), "workflow.merge_waiting_user", sessionStatusUpdatedEvent)
}

func TestAskMergeFailureKeepsStableBatchWhenSessionSaveFails(t *testing.T) {
	repo := newFakeRepository()
	repo.saveErr = errors.New("database unavailable")
	questions := &fakeQuestionCanceller{}
	service := New(repo, newFakeProjectRepository("project-1"), WithQuestions(questions))
	nodeRunID := domain.NodeRunID("node-run-merge")

	_, err := service.askMergeFailure(context.Background(), domain.Session{
		ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning,
	}, domain.WorkflowAdvance{
		SessionID: "session-1", NodeRunID: &nodeRunID, CommandID: "command-merge-1",
	}, gitdiffdomain.MergeResult{Strategy: "merge", Status: "failed", FailureReason: "conflict"}, "merge_conflict")
	if err == nil || !strings.Contains(err.Error(), "save session") {
		t.Fatalf("askMergeFailure() error = %v", err)
	}
	if questions.created.BatchID != "merge-failure-command-merge-1" {
		t.Fatalf("merge failure batch id = %q", questions.created.BatchID)
	}
	if questions.cancelledSessionID != "" {
		t.Fatalf("stable merge failure batch was cancelled for session %q", questions.cancelledSessionID)
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
			SessionID:        "session-1",
			NodeRunID:        &firstNodeRunID,
			CurrentNodeID:    "build",
			CurrentNodeTitle: "Build",
			Status:           "running",
			RequiresCodex:    true,
			Prompt:           "Build",
		},
		advance: domain.WorkflowAdvance{
			SessionID:        "session-1",
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
	closedEvents := make(chan processdomain.CodexEvent, 1)
	closedEvents <- transcriptReadyEvent("codex-session-1")
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
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() initial error = %v", err)
	}
	waitForEventTypeAfter(t, events, "workflow.exit_pending", "session.queued")
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() next error = %v", err)
	}
	if len(processes.created) < 2 {
		t.Fatalf("expected second process run, got %#v", processes.created)
	}
	if processes.created[1].NodeRunID == nil || *processes.created[1].NodeRunID != "node-run-2" {
		t.Fatalf("second process run = %#v", processes.created[1])
	}
	if !codex.resumeCalled || codex.resumeInput.CodexSessionID != "codex-session-1" || codex.resumeInput.Prompt != "Verify" {
		t.Fatalf("codex resume input = %#v", codex.resumeInput)
	}
	if workflows.completeInput.NodeRunID != "node-run-1" {
		t.Fatalf("complete input = %#v", workflows.completeInput)
	}
}

func TestWorkflowSessionMarksRunFailedWhenResultCorrectionStillInvalid(t *testing.T) {
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
			SessionID:          "session-1",
			NodeRunID:          &nodeRunID,
			CurrentNodeID:      "build",
			CurrentNodeTitle:   "Build",
			Status:             "running",
			RequiresCodex:      true,
			RequireResultRetry: true,
			Prompt:             "ANYCODE_WORKFLOW_RESULT_RETRY",
		},
		completeErr:  apperror.New(apperror.CodeWorkflowResultInvalid, apperror.CategoryWorkflowError, "workflow node result is invalid"),
		markFailErrs: []error{errors.New("temporary workflow persistence failure"), nil},
	}
	firstMarkStatus := domain.Status("")
	workflows.markFailHook = func(call int) {
		if call == 1 {
			firstMarkStatus = repo.sessions["session-1"].Status
		}
	}
	closedEvents := make(chan processdomain.CodexEvent)
	close(closedEvents)
	codex := &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 123},
		events:      closedEvents,
	}
	events := &fakeEventStore{}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(newFakeProcessRepository(), codex), WithEvents(events))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		if nextID == 1 {
			return "process-run-1", nil
		}
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	service.processExitDelay = func(int) time.Duration { return 0 }

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSessionWithOptions() error = %v", err)
	}
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	waitForEventTypeAfter(t, events, "workflow.exit_pending", "workflow.failed")
	if workflows.failedInput.SessionID != "session-1" || workflows.failedInput.NodeRunID == nil || *workflows.failedInput.NodeRunID != "node-run-1" {
		t.Fatalf("failed input = %#v", workflows.failedInput)
	}
	if workflows.failedInput.Code != apperror.CodeWorkflowResultInvalid {
		t.Fatalf("failed input = %#v", workflows.failedInput)
	}
	if repo.sessions["session-1"].Status != domain.StatusFailed {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
	if workflows.markFailCalls != 2 || firstMarkStatus == domain.StatusFailed {
		t.Fatalf("MarkStartFailed calls = %d, first failure session status = %q", workflows.markFailCalls, firstMarkStatus)
	}
}

func TestWorkflowAdvanceAfterProcessExitReturnsMarkStartFailedError(t *testing.T) {
	nodeRunID := processdomain.NodeRunID("node-run-1")
	resultErr := apperror.New(apperror.CodeWorkflowResultInvalid, apperror.CategoryWorkflowError, "invalid result")
	markErr := errors.New("persist workflow failure")
	workflows := &fakeWorkflowStarter{recoverErr: resultErr, markFailErr: markErr}
	service := New(newFakeRepository(), newFakeProjectRepository(), WithWorkflows(workflows))

	_, err := service.workflowAdvanceAfterProcessExit(context.Background(), processdomain.CodexHandle{ProcessRunID: "process-run-1"}, codexStartOptions{
		sessionID: "session-1",
		nodeRunID: &nodeRunID,
	}, processdomain.ExitResult{}, nil)
	if !errors.Is(err, markErr) || !errors.Is(err, resultErr) {
		t.Fatalf("workflowAdvanceAfterProcessExit() error = %v", err)
	}
	if workflows.failedInput.NodeRunID == nil || string(*workflows.failedInput.NodeRunID) != string(nodeRunID) {
		t.Fatalf("failed input = %#v", workflows.failedInput)
	}
}

func TestPendingApprovalFromEventIncludesPersistedResult(t *testing.T) {
	event := eventdomain.DomainEvent{
		Type: "session.waiting_approval",
		Payload: map[string]any{
			"sessionId":        "session-1",
			"nodeRunId":        "node-run-1",
			"currentNodeId":    "build",
			"currentNodeTitle": "Build",
			"approvalPhase":    "after_run",
			"result":           map[string]any{"version": float64(1), "outcome": "success", "summary": "done", "data": map[string]any{}},
		},
	}
	approval := pendingApprovalFromEvent(event)
	if approval == nil || approval.Phase != "after_run" || approval.Result["outcome"] != "success" {
		t.Fatalf("pendingApprovalFromEvent() = %#v", approval)
	}
}

func TestPendingApprovalFromEventKeepsBeforeRunResultNil(t *testing.T) {
	event := eventdomain.DomainEvent{
		Type: "session.waiting_approval",
		Payload: map[string]any{
			"sessionId":     "session-1",
			"nodeRunId":     "node-run-1",
			"currentNodeId": "build",
			"approvalPhase": "before_run",
			"result":        nil,
		},
	}
	approval := pendingApprovalFromEvent(event)
	if approval == nil || approval.Result != nil {
		t.Fatalf("pendingApprovalFromEvent() = %#v", approval)
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
			SessionID:        "session-1",
			NodeRunID:        &firstNodeRunID,
			CurrentNodeID:    "build",
			CurrentNodeTitle: "Build",
			Status:           "running",
			RequiresCodex:    true,
			Prompt:           "Build",
		},
		failAdvance: domain.WorkflowAdvance{
			SessionID:        "session-1",
			NodeRunID:        &secondNodeRunID,
			CurrentNodeID:    "build",
			CurrentNodeTitle: "Build",
			Status:           "running",
			RequiresCodex:    true,
			Prompt:           "Build retry",
		},
	}
	processes := newFakeProcessRepository()
	events := &fakeEventStore{}
	failedEvents := make(chan processdomain.CodexEvent, 1)
	exitCode := 2
	failedEvents <- processdomain.CodexEvent{Type: processdomain.CodexEventProcessExit, Content: processdomain.ExitResult{
		ExitCode:      &exitCode,
		FailureReason: `configuration error: invalid value "priority" for service_tier`,
	}}
	close(failedEvents)
	codex := &fakeCodexProcess{
		startHandle:  processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-1"},
		eventStreams: []<-chan processdomain.CodexEvent{failedEvents},
	}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex), WithEvents(events))
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
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() initial error = %v", err)
	}
	waitForEventTypeAfter(t, events, "workflow.exit_pending", "session.queued")
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() retry error = %v", err)
	}
	if len(processes.created) < 2 {
		t.Fatalf("expected retry process run, got %#v", processes.created)
	}
	if workflows.completeInput.NodeRunID != "" {
		t.Fatalf("complete should not be called on failed exit: %#v", workflows.completeInput)
	}
	if workflows.failInput.NodeRunID != "node-run-1" || workflows.failInput.Code != "codex_param_rejected" {
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

	got = codexProcessFailureCode(processdomain.ExitResult{
		FailureReason: `configuration error: invalid value "priority" for service_tier`,
	})
	if got != "codex_param_rejected" {
		t.Fatalf("codexProcessFailureCode(service_tier) = %q", got)
	}

	got = codexProcessFailureCode(processdomain.ExitResult{FailureReason: "exit status 7"})
	if got != "codex_process_failed" {
		t.Fatalf("codexProcessFailureCode() = %q", got)
	}
}

func TestWorkflowResultsFromTextExtractsJSONResults(t *testing.T) {
	got, ok := workflowResultsFromText(`{"results":{"status":"passed","count":2}}`)
	if !ok || got["status"] != "passed" || got["count"] != float64(2) {
		t.Fatalf("workflowResultsFromText() = %#v, %v", got, ok)
	}
}

func TestWorkflowResultsFromTextRejectsNonCanonicalEnvelope(t *testing.T) {
	tests := []string{
		"summary\n{\"results\":{\"status\":\"passed\"}}",
		"```json\n{\"results\":{\"status\":\"passed\"}}\n```",
		`{"results":{"status":"passed"},"approval":{"approved":true}}`,
		`{"status":"passed"}`,
	}
	for _, input := range tests {
		if got, ok := workflowResultsFromText(input); ok {
			t.Fatalf("workflowResultsFromText(%q) = %#v, true", input, got)
		}
	}
}

func TestWorkflowResultsAfterEventUsesOnlyLatestAssistantOutput(t *testing.T) {
	valid := completedAssistantEvent(`{"results":{"status":"passed"}}`)
	finalProse := completedAssistantEvent("Should I proceed?")
	nonAssistant := processdomain.CodexEvent{Type: processdomain.CodexEventCommand, Content: processdomain.CodexCommandContent{
		Commands: []processdomain.CodexCommandInvocation{{Command: "test", HasOutput: true, Output: "done"}},
	}}

	results := workflowResultsAfterEvent(nil, valid)
	if results["status"] != "passed" {
		t.Fatalf("valid assistant results = %#v", results)
	}
	results = workflowResultsAfterEvent(results, nonAssistant)
	if results["status"] != "passed" {
		t.Fatalf("non-assistant event replaced results = %#v", results)
	}
	if results = workflowResultsAfterEvent(results, finalProse); results != nil {
		t.Fatalf("final invalid assistant output retained stale results = %#v", results)
	}
	results = workflowResultsAfterEvent(nil, valid)
	if results = workflowResultsAfterEvent(results, completedAssistantEvent("  \n")); results != nil {
		t.Fatalf("final blank assistant output retained stale results = %#v", results)
	}
}

func TestWorkflowResultsFromEventExtractsCodexAssistantItem(t *testing.T) {
	got, ok := workflowResultsFromEvent(completedAssistantEvent(`{"results":{"status":"passed"}}`))
	if !ok || got["status"] != "passed" {
		t.Fatalf("workflowResultsFromEvent() = %#v, %v", got, ok)
	}
}

func TestWorkflowResultsFromEventRejectsNonCanonicalAssistantLifecycleEvents(t *testing.T) {
	result := `{"results":{"status":"passed"}}`
	tests := []processdomain.CodexEvent{
		{Type: processdomain.CodexEventStatus, Content: processdomain.CodexStatusContent{Code: "agent_message", Message: result}},
		{Type: processdomain.CodexEventReasoning, Content: processdomain.CodexReasoningContent{Text: result}},
		{Type: processdomain.CodexEventMessage, Content: processdomain.CodexMessageContent{Role: "user", Text: result}},
		{Type: processdomain.CodexEventMessage, Content: processdomain.CodexMessageContent{Role: "assistant", Text: ""}},
		{Type: processdomain.CodexEventCommand, Content: processdomain.CodexCommandContent{Commands: []processdomain.CodexCommandInvocation{{Output: result}}}},
	}
	for _, event := range tests {
		if got, ok := workflowResultsFromEvent(event); ok {
			t.Fatalf("workflowResultsFromEvent(%#v) = %#v, true", event, got)
		}
	}
}

func completedAssistantEvent(output string) processdomain.CodexEvent {
	return processdomain.CodexEvent{
		Type: processdomain.CodexEventMessage,
		Content: processdomain.CodexMessageContent{
			Role: "assistant", Text: output, Format: processdomain.CodexTextMarkdown,
		},
	}
}

func TestWorkflowResultsFromEventIgnoresCommandAggregatedOutput(t *testing.T) {
	_, ok := workflowResultsFromEvent(processdomain.CodexEvent{
		Type: processdomain.CodexEventCommand,
		Content: processdomain.CodexCommandContent{
			Commands: []processdomain.CodexCommandInvocation{{HasOutput: true, Output: `{"results":{"status":"passed"}}`}},
		},
	})
	if ok {
		t.Fatal("workflowResultsFromEvent() should ignore command aggregated output")
	}
}

func TestWorkflowResultsFromEventIgnoresUserPromptJSON(t *testing.T) {
	_, ok := workflowResultsFromEvent(processdomain.CodexEvent{
		Type: processdomain.CodexEventMessage,
		Content: processdomain.CodexMessageContent{
			Role: "user", Text: `Workflow input params JSON: {"requirement":"ship"}`,
		},
	})
	if ok {
		t.Fatal("workflowResultsFromEvent() should ignore user prompt JSON")
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
		Queue:        domain.QueueIntent{InitialStart: true},
	}
	nextNodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{
		approvalResult: domain.WorkflowApprovalResult{
			Run: domain.WorkflowRunSnapshot{
				SessionID:     "session-1",
				Status:        "running",
				CurrentNodeID: "verify",
				Context:       map[string]any{"last": map[string]any{"status": "succeeded"}},
			},
			Advance: domain.WorkflowAdvance{
				SessionID:        "session-1",
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
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithProcesses(processes, codex), WithEvents(events), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events}}))
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
		SessionID: "session-1",
		NodeID:    "approve",
		Approved:  true,
		Comment:   "looks good",
	})
	if err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	if got.SessionID != "session-1" || got.Status != "running" || got.CurrentNodeID != "verify" {
		t.Fatalf("SubmitWorkflowApproval() = %#v", got)
	}
	if workflows.approvalInput.SessionID != "session-1" || workflows.approvalInput.NodeID != "approve" || !workflows.approvalInput.Approved {
		t.Fatalf("approval input = %#v", workflows.approvalInput)
	}
	if repo.sessions["session-1"].Status != domain.StatusQueued || repo.sessions["session-1"].CodexSessionID != "" {
		t.Fatalf("session after approval = %#v", repo.sessions["session-1"])
	}
	if len(processes.created) != 0 {
		t.Fatalf("process runs = %#v", processes.created)
	}
	if codex.startCalled || !repo.sessions["session-1"].Queue.InitialStart || repo.sessions["session-1"].Queue.NodeRunID == nil || *repo.sessions["session-1"].Queue.NodeRunID != "node-run-2" || repo.sessions["session-1"].Queue.Prompt != "Verify build" {
		t.Fatalf("queued session = %#v codexCalled=%v", repo.sessions["session-1"], codex.startCalled)
	}
}

func TestSubmitWorkflowApprovalPersistsNextApprovalProjection(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusWaitingApproval,
	}
	nextNodeRunID := domain.NodeRunID("node-run-2")
	result := map[string]any{"version": float64(1), "outcome": "success", "summary": "done", "data": map[string]any{}}
	workflows := &fakeWorkflowStarter{approvalResult: domain.WorkflowApprovalResult{
		Run: domain.WorkflowRunSnapshot{SessionID: "session-1", Status: "waiting_approval", CurrentNodeID: "review"},
		Advance: domain.WorkflowAdvance{
			SessionID: "session-1", NodeRunID: &nextNodeRunID, CurrentNodeID: "review", CurrentNodeTitle: "Review",
			Status: "waiting_approval", RequiresCodex: false, ApprovalPhase: "after_run", Result: result,
		},
	}}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithEvents(events), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events}}))
	sequence := 0
	service.generateID = func() (domain.ID, error) {
		sequence++
		return domain.ID(fmt.Sprintf("event-%d", sequence)), nil
	}

	if _, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{SessionID: "session-1", NodeID: "approve", Approved: true}); err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	var waiting eventdomain.DomainEvent
	for _, event := range events.snapshot() {
		if event.Type == "session.waiting_approval" {
			waiting = event
		}
	}
	if waiting.Type == "" || waiting.Payload["approvalPhase"] != "after_run" {
		t.Fatalf("waiting approval event = %#v", waiting)
	}
	gotResult, _ := waiting.Payload["result"].(map[string]any)
	if gotResult["outcome"] != "success" {
		t.Fatalf("waiting approval result = %#v", gotResult)
	}
}

func TestSubmitWorkflowApprovalAcknowledgesPreviouslyAppliedSystemCommand(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusWaitingApproval,
		AppliedSystemCommands: map[string]bool{"old-command": true},
	}
	workflows := &fakeWorkflowStarter{approvalResult: domain.WorkflowApprovalResult{
		Run:      domain.WorkflowRunSnapshot{SessionID: "session-1", Status: "blocked", CurrentNodeID: "review"},
		Advance:  domain.WorkflowAdvance{SessionID: "run-1", Blocked: true, BlockedReason: "rejected"},
		Rejected: true,
	}}
	eventSessionID := eventdomain.SessionID("session-1")
	events := &fakeEventStore{events: []eventdomain.DomainEvent{{
		ID: "old-command", SessionID: &eventSessionID, Type: workflowSystemAdvancePendingEvent,
		Payload: workflowAdvancePendingPayload(domain.WorkflowAdvance{SessionID: "run-1", NodeRunID: nodeRunIDPtr("old-node"), Close: true}),
	}}}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithEvents(events), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events}}))
	sequence := 0
	service.generateID = func() (domain.ID, error) {
		sequence++
		return domain.ID(fmt.Sprintf("event-%d", sequence)), nil
	}

	if _, err := service.SubmitWorkflowApproval(context.Background(), SubmitWorkflowApprovalInput{
		SessionID: "run-1", NodeID: "review", Approved: false, Comment: "stop",
	}); err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	foundAck := false
	for _, event := range events.snapshot() {
		foundAck = foundAck || event.Type == workflowSystemAdvanceCompletedEvent && event.Payload["commandEventId"] == "old-command"
	}
	if !foundAck {
		t.Fatalf("old command was not acknowledged before new approval: %#v", events.snapshot())
	}
	if repo.sessions["session-1"].AppliedSystemCommands["old-command"] {
		t.Fatalf("old command remains in applied ledger: %#v", repo.sessions["session-1"].AppliedSystemCommands)
	}
	if len(repo.appends) != 1 || repo.appends[0].Body != "stop" || repo.appends[0].Status != domain.PromptAppendPending {
		t.Fatalf("rejection prompt appends = %#v", repo.appends)
	}
}

func TestRestartedWorkflowApprovalDoesNotMarkNextCodexInitial(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "ship feature",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusStopped,
		BaseBranch:     "main",
		WorktreePath:   "/workspace/project-1",
		CodexSessionID: "codex-session-old",
	}
	workflowID := projectdomain.WorkflowDefinitionID("workflow-1")
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:                "project-1",
		DefaultWorkflowID: &workflowID,
	}
	approvalNodeRunID := domain.NodeRunID("node-run-approval")
	nextNodeRunID := domain.NodeRunID("node-run-verify")
	workflows := &fakeWorkflowStarter{
		start: domain.WorkflowStart{
			SessionID:        "session-2",
			NodeRunID:        &approvalNodeRunID,
			CurrentNodeID:    "approve",
			CurrentNodeTitle: "Approve",
			Status:           "waiting_approval",
			RequiresCodex:    false,
		},
		approvalResult: domain.WorkflowApprovalResult{
			Run: domain.WorkflowRunSnapshot{
				SessionID:     "session-1",
				Status:        "running",
				CurrentNodeID: "verify",
			},
			Advance: domain.WorkflowAdvance{
				SessionID:        "session-2",
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
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-2"}, events: stream}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes}}
	service := New(repo, projects, WithWorkflows(workflows), WithProcesses(processes, codex), WithUnitOfWork(uow))
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	if _, err := service.StartSession(ctx, "session-1"); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	waiting := repo.sessions["session-1"]
	if waiting.Status != domain.StatusWaitingApproval || waiting.Queue.InitialStart {
		t.Fatalf("restarted workflow waiting session = %#v", waiting)
	}
	if _, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		SessionID: "session-2",
		NodeID:    "approve",
		Approved:  true,
	}); err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	queued := repo.sessions["session-1"]
	if queued.Status != domain.StatusQueued || queued.Queue.InitialStart || queued.Queue.Kind != domain.QueueKindStart {
		t.Fatalf("restarted workflow queued session = %#v", queued)
	}
	if queued.CodexSessionID != "" || queued.Queue.ResumeCodexSessionID != "" {
		t.Fatalf("restarted workflow reused stale Codex session = %#v", queued)
	}
	if _, err := service.DrainQueuedSessions(ctx); err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if codex.startInput.Prompt != promptWithAnyCodeGuidance("Verify build", repo.sessions["session-1"]) {
		t.Fatalf("restarted workflow prompt = %q", codex.startInput.Prompt)
	}
	if _, ok := service.processConsumerDone(codex.startInput.ProcessRunID); !ok {
		t.Fatal("restarted workflow consumer was not registered")
	}
}

func TestSubmitWorkflowApprovalResumesExistingCodexSessionForNextNode(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "ship feature",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusWaitingApproval,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/project-1",
	}
	nextNodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{
		approvalResult: domain.WorkflowApprovalResult{
			Run: domain.WorkflowRunSnapshot{
				SessionID:     "session-1",
				Status:        "running",
				CurrentNodeID: "verify",
			},
			Advance: domain.WorkflowAdvance{
				SessionID:        "session-1",
				NodeRunID:        &nextNodeRunID,
				CurrentNodeID:    "verify",
				CurrentNodeTitle: "Verify",
				Status:           "running",
				RequiresCodex:    true,
				Prompt:           "Verify build",
			},
		},
	}
	events := &fakeEventStore{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithWorkflows(workflows),
		WithEvents(events),
		WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events}}),
	)
	service.now = func() time.Time { return time.Unix(51, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if _, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		SessionID: "session-1",
		NodeID:    "plan",
		Approved:  true,
	}); err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	queued := repo.sessions["session-1"]
	if queued.Status != domain.StatusQueued || queued.Queue.Kind != domain.QueueKindResume {
		t.Fatalf("queued session = %#v", queued)
	}
	if queued.Queue.ResumeCodexSessionID != "codex-session-1" || queued.Queue.Prompt != "Verify build" {
		t.Fatalf("queued resume = %#v", queued.Queue)
	}
	if queued.Queue.NodeRunID == nil || *queued.Queue.NodeRunID != "node-run-2" {
		t.Fatalf("queued node run = %#v", queued.Queue.NodeRunID)
	}

	previousRunID := processdomain.RunID("process-run-1")
	processes := newFakeProcessRepository()
	processes.created = []processdomain.Run{{
		ID:             previousRunID,
		SessionID:      "session-1",
		Status:         processdomain.StatusExited,
		CodexSessionID: "codex-session-1",
	}}
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"},
		events:       stream,
	}
	executor := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	executor.now = func() time.Time { return time.Unix(52, 0).UTC() }
	executor.generateID = func() (domain.ID, error) { return "process-run-2", nil }

	started, err := executor.startQueuedSession(ctx, queued, true)
	if err != nil {
		t.Fatalf("startQueuedSession() error = %v", err)
	}
	if started.Status != domain.StatusStarting || !codex.resumeCalled || codex.startCalled {
		t.Fatalf("started session = %#v start=%v resume=%v", started, codex.startCalled, codex.resumeCalled)
	}
	stream <- transcriptReadyEvent("codex-session-1")
	waitForSessionStatus(t, repo, "session-1", domain.StatusRunning)
	if codex.resumeInput.CodexSessionID != "codex-session-1" || codex.resumeInput.Prompt != "Verify build" {
		t.Fatalf("Codex Resume input = %#v", codex.resumeInput)
	}
	if len(processes.created) != 2 || processes.created[1].ResumeOf == nil || *processes.created[1].ResumeOf != previousRunID {
		t.Fatalf("process runs = %#v", processes.created)
	}
	consumerDone, ok := executor.processConsumerDone("process-run-2")
	if !ok {
		t.Fatal("workflow resume consumer was not registered")
	}
	stopping := repo.sessions["session-1"]
	stopping.Status = domain.StatusStopping
	repo.sessions["session-1"] = stopping
	close(stream)
	select {
	case <-consumerDone:
	case <-time.After(time.Second):
		t.Fatal("workflow resume consumer did not stop")
	}
}

func TestWorkflowResumeExitWithoutPromptAcknowledgementWaitsForResumeAction(t *testing.T) {
	nodeRunID := processdomain.NodeRunID("node-run-2")
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusRunning,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/project-1",
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID:             "process-run-2",
		SessionID:      "session-1",
		NodeRunID:      &nodeRunID,
		Status:         processdomain.StatusRunning,
		CodexSessionID: "codex-session-1",
	}
	processes.hasActive = true
	workflows := &fakeWorkflowStarter{resumeSnapshot: domain.WorkflowRunSnapshot{
		SessionID: "session-1", Status: "waiting_resume_action", CurrentNodeID: "verify",
	}}
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithWorkflows(workflows), WithEvents(events), WithUnitOfWork(uow))
	service.now = func() time.Time { return time.Unix(53, 0).UTC() }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	exitCode := 0

	service.handleCodexProcessExit(
		repo.sessions["session-1"],
		processdomain.CodexHandle{ProcessRunID: "process-run-2", CodexSessionID: "codex-session-1"},
		codexStartOptions{
			sessionID:            "session-1",
			nodeRunID:            &nodeRunID,
			resumeCodexSessionID: "codex-session-1",
		},
		processdomain.ExitResult{ExitCode: &exitCode, FinishedAt: time.Unix(53, 0).UTC()},
		nil,
	)

	if got := repo.sessions["session-1"].Status; got != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", got)
	}
	if workflows.resumeInput.SessionID != "session-1" || workflows.resumeInput.Code != "resume_failed" {
		t.Fatalf("workflow resume failure = %#v", workflows.resumeInput)
	}
	if workflows.failInput.NodeRunID != "" || workflows.recoverInput.NodeRunID != "" {
		t.Fatalf("workflow node was advanced: fail=%#v recover=%#v", workflows.failInput, workflows.recoverInput)
	}
	if processes.exitedID != "process-run-2" {
		t.Fatalf("exited process = %q", processes.exitedID)
	}
	if processes.exitedResult.FailureReason != "Codex resume exited before acknowledging the workflow node prompt" {
		t.Fatalf("process failure reason = %q", processes.exitedResult.FailureReason)
	}
	if uow.calls == 0 {
		t.Fatal("workflow resume failure did not use the unit of work")
	}
	waitForEventType(t, events, "process.resume_failed")
	waitForEventType(t, events, "session.resume_failed")
}

func TestWorkflowResumeStoppedBeforePromptAcknowledgementStaysStopped(t *testing.T) {
	nodeRunID := processdomain.NodeRunID("node-run-2")
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusStopping,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/project-1",
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-2", SessionID: "session-1", NodeRunID: &nodeRunID, Status: processdomain.StatusStopping}
	processes.hasActive = true
	workflows := &fakeWorkflowStarter{}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithWorkflows(workflows), WithEvents(events))
	service.now = func() time.Time { return time.Unix(55, 0).UTC() }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	exitCode := 143

	service.handleCodexProcessExit(
		repo.sessions["session-1"],
		processdomain.CodexHandle{ProcessRunID: "process-run-2", CodexSessionID: "codex-session-1"},
		codexStartOptions{sessionID: "session-1", nodeRunID: &nodeRunID, resumeCodexSessionID: "codex-session-1"},
		processdomain.ExitResult{ExitCode: &exitCode, FailureReason: "terminated", FinishedAt: time.Unix(55, 0).UTC()},
		nil,
	)

	if got := repo.sessions["session-1"].Status; got != domain.StatusStopped {
		t.Fatalf("session status = %q", got)
	}
	if workflows.resumeInput.SessionID != "" || workflows.failInput.NodeRunID != "" || workflows.recoverInput.NodeRunID != "" {
		t.Fatalf("workflow was advanced after stop: resume=%#v fail=%#v recover=%#v", workflows.resumeInput, workflows.failInput, workflows.recoverInput)
	}
	waitForEventType(t, events, "session.stopped")
}

func TestWorkflowResumeFailureAfterPromptAcknowledgementFailsNode(t *testing.T) {
	nodeRunID := processdomain.NodeRunID("node-run-2")
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/project-1",
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-2", SessionID: "session-1", NodeRunID: &nodeRunID, Status: processdomain.StatusRunning}
	processes.hasActive = true
	workflows := &fakeWorkflowStarter{failAdvance: domain.WorkflowAdvance{
		SessionID: "session-1", Blocked: true, BlockedReason: "node failed after start",
	}}
	eventStream := make(chan processdomain.CodexEvent, 2)
	eventStream <- processdomain.CodexEvent{EventID: "turn-started", Type: processdomain.CodexEventStatus, Content: processdomain.CodexStatusContent{Code: "turn.started"}}
	eventStream <- processdomain.CodexEvent{
		EventID: "process-exit",
		Type:    processdomain.CodexEventProcessExit,
		Content: processdomain.ExitResult{ExitCode: intPointer(1), FailureReason: "node command failed"},
	}
	codex := &fakeCodexProcess{events: eventStream}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithWorkflows(workflows), WithEvents(&fakeEventStore{}))
	service.now = func() time.Time { return time.Unix(54, 0).UTC() }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	handle := processdomain.CodexHandle{ProcessRunID: "process-run-2", CodexSessionID: "codex-session-1"}
	service.consumeCodexEvents(
		handle,
		repo.sessions["session-1"],
		codexStartOptions{
			sessionID:            "session-1",
			nodeRunID:            &nodeRunID,
			resumeCodexSessionID: "codex-session-1",
		},
		"/workspace/project-1",
	)
	consumerDone, ok := service.processConsumerDone(handle.ProcessRunID)
	if !ok {
		t.Fatal("workflow resume consumer was not registered")
	}
	close(eventStream)
	select {
	case <-consumerDone:
	case <-time.After(time.Second):
		t.Fatal("workflow resume consumer did not stop")
	}

	if got := repo.sessions["session-1"].Status; got != domain.StatusBlocked {
		t.Fatalf("session status = %q", got)
	}
	if workflows.failInput.NodeRunID != domain.NodeRunID(nodeRunID) || workflows.resumeInput.SessionID != "" {
		t.Fatalf("workflow failure routing: fail=%#v resume=%#v", workflows.failInput, workflows.resumeInput)
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
				SessionID:     "session-1",
				Status:        "blocked",
				CurrentNodeID: "approve",
				Context:       map[string]any{"blockedReason": "approval rejected"},
			},
			Advance: domain.WorkflowAdvance{
				SessionID:     "session-1",
				Status:        "blocked",
				Blocked:       true,
				BlockedReason: "approval rejected",
			},
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithProcesses(processes, codex), WithEvents(events), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events}}))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	service.now = func() time.Time { return time.Unix(51, 0).UTC() }

	got, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		SessionID: "session-1",
		NodeID:    "approve",
		Approved:  false,
		Comment:   "needs changes",
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

func TestSubmitWorkflowApprovalRejectsAfterRunWithPromptAppend(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "ship feature",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusWaitingApproval,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/project-1",
	}
	nodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{
		approvalResult: domain.WorkflowApprovalResult{
			Run: domain.WorkflowRunSnapshot{
				SessionID:     "session-1",
				Status:        "running",
				CurrentNodeID: "build",
			},
			Advance: domain.WorkflowAdvance{
				SessionID:        "session-1",
				NodeRunID:        &nodeRunID,
				CurrentNodeID:    "build",
				CurrentNodeTitle: "Build",
				Status:           "running",
				RequiresCodex:    true,
				Prompt:           "Build",
			},
			RejectedAfterRun: true,
			Rejected:         true,
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 123, CodexSessionID: "codex-session-2"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithProcesses(processes, codex), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo}}))
	ids := []domain.ID{"append-1", "process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.now = func() time.Time { return time.Unix(52, 0).UTC() }

	got, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		SessionID: "session-1",
		NodeID:    "build",
		Approved:  false,
		Comment:   "fix failing tests",
	})
	if err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	if got.Status != "running" || workflows.approvalInput.Comment != "fix failing tests" {
		t.Fatalf("approval result=%#v input=%#v", got, workflows.approvalInput)
	}
	if len(repo.appends) != 1 || repo.appends[0].Body != "fix failing tests" || repo.appends[0].Status != domain.PromptAppendPending {
		t.Fatalf("prompt appends = %#v", repo.appends)
	}
	queued := repo.sessions["session-1"]
	if queued.Status != domain.StatusQueued || queued.Queue.NodeRunID == nil || *queued.Queue.NodeRunID != "node-run-2" {
		t.Fatalf("queued session = %#v", queued)
	}
	if queued.Queue.Prompt != "Build" {
		t.Fatalf("queued prompt = %q", queued.Queue.Prompt)
	}
	if queued.Queue.Kind != domain.QueueKindResume || queued.Queue.ResumeCodexSessionID != "codex-session-1" {
		t.Fatalf("queued resume = %#v", queued.Queue)
	}
	if len(processes.created) != 0 || codex.startCalled {
		t.Fatalf("process should be queued, created=%#v start=%v", processes.created, codex.startCalled)
	}
}

func TestSubmitWorkflowApprovalRequiresTransactionalRunner(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      domain.ModeWorkflow,
		Status:    domain.StatusWaitingApproval,
	}
	workflows := &fakeWorkflowStarter{}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows))

	_, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		SessionID: "session-1",
		NodeID:    "approve",
		Approved:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "requires transactional workflow repository runner") {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	if workflows.approvalInput.SessionID != "" {
		t.Fatalf("workflow approval should not be submitted without transaction: %#v", workflows.approvalInput)
	}
}

func TestSubmitWorkflowApprovalExecutesPostCommitExprAdvance(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{
		DatabaseURL: filepath.Join(t.TempDir(), "anycode.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	session := domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      domain.ModeWorkflow,
		Status:    domain.StatusWaitingApproval,
	}
	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	definition := workflowdomain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Version:   1,
		Graph: workflowdomain.Graph{
			Nodes: []workflowdomain.Node{
				{ID: "build", Type: "codex", Title: "Build", Approval: workflowdomain.ApprovalConfig{AfterRun: true}},
				{ID: "expr", Type: "expr", Title: "Expr", Prompt: `{status: "ready"}`},
				{ID: "verify", Type: "codex", Title: "Verify", Prompt: "Verify result"},
			},
			Edges: []workflowdomain.Edge{
				{From: "build", To: "expr"},
				{From: "expr", To: "verify"},
			},
		},
	}
	if err := store.Workflows().SaveDefinition(ctx, definition); err != nil {
		t.Fatalf("save definition: %v", err)
	}
	now := time.Unix(10, 0).UTC()
	run := workflowdomain.Run{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               workflowdomain.RunWaitingApproval,
		CurrentNodeID:        "build",
		Context:              workflowdomain.Context{Values: map[string]any{}},
		PendingApproval:      &workflowdomain.PendingApproval{Phase: workflowdomain.ApprovalAfterRun, NodeID: "build", Attempt: 1},
		StartedAt:            &now,
	}
	nodeRun := workflowdomain.NodeRun{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    workflowdomain.NodeWaitingApproval,
		Attempt:   1,
		Result:    &workflowdomain.Result{Version: workflowdomain.ResultVersion, Outcome: workflowdomain.ResultSuccess, Summary: "passed", Data: map[string]any{"status": "passed"}},
		StartedAt: &now,
	}
	if err := store.Workflows().CreateInitialRun(ctx, run, nodeRun); err != nil {
		t.Fatalf("create workflow run: %v", err)
	}
	workflowService := workflowapp.New(store.Workflows(), workflowapp.WithEvents(store.Events()))
	service := New(store.Sessions(), newFakeProjectRepository("project-1"), WithWorkflows(workflowService), WithUnitOfWork(store), WithEvents(store.Events()))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("session-event-%d", nextID)), nil
	}
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }

	if _, err := service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		SessionID: "session-1",
		NodeID:    "build",
		Approved:  true,
	}); err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	gotSession, err := store.Sessions().Find(ctx, "session-1")
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if gotSession.Status != domain.StatusQueued || gotSession.Queue.NodeRunID == nil || gotSession.Queue.Prompt == "" {
		t.Fatalf("session after expr advance = %#v", gotSession)
	}
	gotRun, err := store.Workflows().FindRun(ctx, "session-1")
	if err != nil {
		t.Fatalf("find workflow run: %v", err)
	}
	if gotRun.CurrentNodeID != "verify" || gotRun.Status != workflowdomain.RunRunning {
		t.Fatalf("workflow run after expr advance = %#v", gotRun)
	}
	exprRun, err := store.Workflows().FindLatestNodeRun(ctx, "session-1", "expr")
	if err != nil {
		t.Fatalf("find expr node run: %v", err)
	}
	if exprRun.Status != workflowdomain.NodeSucceeded {
		t.Fatalf("expr node run = %#v", exprRun)
	}
}

func TestSubmitWorkflowApprovalRejectsAfterRunSystemNodeWithoutStartingCodex(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusWaitingApproval}
	nodeRunID := domain.NodeRunID("node-run-expr-2")
	workflows := &fakeWorkflowStarter{
		approvalResult: domain.WorkflowApprovalResult{
			Run: domain.WorkflowRunSnapshot{SessionID: "session-1", Status: "running", CurrentNodeID: "expr"},
			Advance: domain.WorkflowAdvance{
				SessionID: "session-1", NodeRunID: &nodeRunID, CurrentNodeID: "expr", CurrentNodeTitle: "Expr",
				Expr: &domain.WorkflowExpr{Script: `{status: "ready"}`},
			},
			RejectedAfterRun: true,
			Rejected:         true,
		},
		advance: domain.WorkflowAdvance{
			SessionID: "session-1", NodeRunID: &nodeRunID, CurrentNodeID: "expr", CurrentNodeTitle: "Expr",
			Status: "waiting_approval", ApprovalPhase: "after_run", Result: map[string]any{"outcome": "success"},
		},
	}
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithEvents(events), WithUnitOfWork(uow))
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}

	if _, err := service.SubmitWorkflowApproval(context.Background(), SubmitWorkflowApprovalInput{
		SessionID: "session-1", NodeID: "expr", Approved: false, Comment: "run again",
	}); err != nil {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	if workflows.completeCalls != 1 {
		t.Fatalf("CompleteNode calls = %d", workflows.completeCalls)
	}
	if len(repo.appends) != 1 || repo.appends[0].Status != domain.PromptAppendPending || repo.sessions["session-1"].Status != domain.StatusWaitingApproval || repo.sessions["session-1"].Queue.Kind != "" {
		t.Fatalf("session=%#v appends=%#v", repo.sessions["session-1"], repo.appends)
	}
	var pending, completed, runningStatus bool
	for _, event := range events.snapshot() {
		pending = pending || event.Type == workflowSystemAdvancePendingEvent
		completed = completed || event.Type == workflowSystemAdvanceCompletedEvent
		runningStatus = runningStatus || event.Type == sessionStatusUpdatedEvent && event.Causality.SessionStatus == string(domain.StatusRunning)
	}
	if !pending || !completed || !runningStatus {
		t.Fatalf("system advance events = %#v", events.snapshot())
	}
}

func TestSubmitWorkflowApprovalRejectsAfterRunRollsBackWhenPromptAppendFails(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{
		DatabaseURL: filepath.Join(t.TempDir(), "anycode.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	session := domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      domain.ModeWorkflow,
		Status:    domain.StatusWaitingApproval,
	}
	if err := store.Sessions().Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	definition := workflowdomain.Definition{
		ID:        "workflow-1",
		ProjectID: "project-1",
		Name:      "default",
		Version:   1,
		Graph: workflowdomain.Graph{
			Nodes: []workflowdomain.Node{{
				ID:       "build",
				Type:     "codex",
				Title:    "Build",
				Approval: workflowdomain.ApprovalConfig{AfterRun: true},
			}},
		},
	}
	if err := store.Workflows().SaveDefinition(ctx, definition); err != nil {
		t.Fatalf("save definition: %v", err)
	}
	now := time.Unix(10, 0).UTC()
	run := workflowdomain.Run{
		SessionID:            "session-1",
		WorkflowDefinitionID: "workflow-1",
		Status:               workflowdomain.RunWaitingApproval,
		CurrentNodeID:        "build",
		Context:              workflowdomain.Context{Values: map[string]any{}},
		PendingApproval:      &workflowdomain.PendingApproval{Phase: workflowdomain.ApprovalAfterRun, NodeID: "build", Attempt: 1},
		StartedAt:            &now,
	}
	nodeRun := workflowdomain.NodeRun{
		ID:        "node-run-1",
		SessionID: "session-1",
		NodeID:    "build",
		Status:    workflowdomain.NodeWaitingApproval,
		Attempt:   1,
		Result:    &workflowdomain.Result{Version: workflowdomain.ResultVersion, Outcome: workflowdomain.ResultSuccess, Summary: "review required", Data: map[string]any{"status": "failed"}},
		StartedAt: &now,
	}
	if err := store.Workflows().CreateInitialRun(ctx, run, nodeRun); err != nil {
		t.Fatalf("create workflow run: %v", err)
	}
	workflowService := workflowapp.New(store.Workflows(), workflowapp.WithEvents(store.Events()))
	service := New(store.Sessions(), newFakeProjectRepository("project-1"), WithWorkflows(workflowService), WithUnitOfWork(store), WithEvents(store.Events()))
	idCalls := 0
	service.generateID = func() (domain.ID, error) {
		idCalls++
		if idCalls == 2 {
			return "", errors.New("generate append id failed")
		}
		return domain.ID(fmt.Sprintf("event-%d", idCalls)), nil
	}

	_, err = service.SubmitWorkflowApproval(ctx, SubmitWorkflowApprovalInput{
		SessionID: "session-1",
		NodeID:    "build",
		Approved:  false,
		Comment:   "fix it",
	})
	if err == nil || !strings.Contains(err.Error(), "generate prompt append id") {
		t.Fatalf("SubmitWorkflowApproval() error = %v", err)
	}
	gotRun, err := store.Workflows().FindRun(ctx, "session-1")
	if err != nil {
		t.Fatalf("find workflow run: %v", err)
	}
	if gotRun.Status != workflowdomain.RunWaitingApproval {
		t.Fatalf("workflow run status = %q", gotRun.Status)
	}
	gotNodeRun, err := store.Workflows().FindLatestNodeRun(ctx, "session-1", "build")
	if err != nil {
		t.Fatalf("find node run: %v", err)
	}
	if gotNodeRun.Status != workflowdomain.NodeWaitingApproval || gotNodeRun.Attempt != 1 {
		t.Fatalf("node run = %#v", gotNodeRun)
	}
	gotSession, err := store.Sessions().Find(ctx, "session-1")
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	if gotSession.Status != domain.StatusWaitingApproval {
		t.Fatalf("session status = %q", gotSession.Status)
	}
	appends, err := store.Sessions().ListPromptAppends(ctx, "session-1")
	if err != nil {
		t.Fatalf("list prompt appends: %v", err)
	}
	if len(appends) != 0 {
		t.Fatalf("prompt appends = %#v", appends)
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
		SessionID:     "session-1",
		NodeRunID:     &nodeRunID,
		Status:        "running",
		RequiresCodex: true,
		Prompt:        "Run workflow node",
	}, failAdvance: domain.WorkflowAdvance{
		SessionID:     "session-1",
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
	if workflows.failInput.SessionID != "session-1" || workflows.failInput.NodeRunID != "node-run-1" {
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
			SessionID:     "session-1",
			NodeRunID:     &firstNodeRunID,
			Status:        "running",
			RequiresCodex: true,
			Prompt:        "Run workflow node",
		},
		failAdvance: domain.WorkflowAdvance{
			SessionID:        "session-1",
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
	if repo.sessions["session-1"].Status != domain.StatusStarting || repo.sessions["session-1"].CodexSessionID != "" {
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
	if len(codex.startInputs) != 2 || codex.startInputs[1].Prompt != promptWithAnyCodeGuidance("Retry workflow node", repo.sessions["session-1"]) {
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
	attachment, ok := files.sessionAttachments["staged-1"]
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

func TestCreateSessionRequestsCleanupWhenAttachmentArchiveFails(t *testing.T) {
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
	if got.Status != domain.StatusFailed || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("session status after archive failure = %q", got.Status)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("create failure performed synchronous cleanup: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
}

func TestCreateSessionCleanupFailureIsPersistedByCoordinator(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "disk failed") {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := service.DrainWorktreeCleanup(ctx); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	if !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1"}) {
		t.Fatalf("deleted branches = %#v", worktrees.deletedBranches)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || saved.WorktreeCleanup.ErrorCode != "worktree_branch_delete_failed" {
		t.Fatalf("saved cleanup failure = %#v", saved)
	}
}

func TestCreateSessionDoesNotDeleteUnconfirmedExistingBranch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	ownership := domain.WorktreeOwnership{
		PathExists:   true,
		BranchExists: true,
		Registered:   true,
	}
	worktrees := &fakeWorktreeManager{
		createErr: errors.New("branch already exists"),
		ownership: &ownership,
	}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.generateID = func() (domain.ID, error) { return "session-1", nil }

	if _, err := service.CreateSession(ctx, CreateSessionInput{
		ProjectID:   "project-1",
		Requirement: "implement app session",
		BaseBranch:  "main",
	}); err == nil {
		t.Fatal("CreateSession() expected branch collision error")
	}
	if _, err := service.DrainWorktreeCleanup(ctx); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("unconfirmed existing branch was deleted: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || saved.WorktreeCleanup.Retryable || saved.WorktreeCleanup.ErrorCode != "worktree_ownership_unconfirmed" {
		t.Fatalf("cleanup ownership failure = %#v", saved)
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
	files := newFakeAttachmentStore()
	files.sessionAttachments["attachment-1"] = domain.SessionAttachment{
		ID:        "attachment-1",
		SessionID: "created",
		Role:      domain.FileRoleInput,
		Filename:  "note.txt",
	}
	projects := newFakeProjectRepository("project-1")
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Name: "Project One"}
	service := New(repo, projects, WithAttachments(repo, files))

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
	if !slices.Equal(got.Items[0].AvailableActions, []string{"execute", "close"}) {
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
		TodoList: domain.TodoList{Items: []domain.TodoItem{
			{Text: "inspect", Completed: true},
			{Text: "verify"},
		}},
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-1", SessionID: "session-1", Body: "extra context", CreatedAt: time.Unix(11, 0).UTC()},
	}
	files := newFakeAttachmentStore()
	files.sessionAttachments["attachment-1"] = domain.SessionAttachment{
		ID:        "attachment-1",
		SessionID: "session-1",
		Role:      domain.FileRoleInput,
		Filename:  "note.txt",
		MimeType:  "text/plain",
		Size:      5,
	}
	projects := newFakeProjectRepository("project-1")
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Name: "Project One"}
	service := New(repo, projects, WithAttachments(repo, files))

	got, err := service.GetSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.ProjectName != "Project One" {
		t.Fatalf("GetSession() ProjectName = %q", got.ProjectName)
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
	if !slices.Equal(got.AvailableActions, []string{"execute", "close"}) {
		t.Fatalf("GetSession() actions = %#v", got.AvailableActions)
	}
	if !slices.Equal(got.TodoList.Items, repo.sessions["session-1"].TodoList.Items) {
		t.Fatalf("GetSession() todo list = %#v", got.TodoList)
	}
}

func TestGetSessionCardStatusReturnsOnlyLiveStatusFields(t *testing.T) {
	ctx := context.Background()
	updatedAt := time.Unix(12, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat,
		Status: domain.StatusStopped, UpdatedAt: updatedAt,
	}
	service := New(repo, newFakeProjectRepository("project-1"))

	got, err := service.GetSessionCardStatus(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetSessionCardStatus() error = %v", err)
	}
	if got.Status != domain.StatusStopped || got.CurrentNodeTitle != "" || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("status = %#v", got)
	}
	if !slices.Equal(got.AvailableActions, []string{"execute", "close"}) {
		t.Fatalf("actions = %#v", got.AvailableActions)
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
	if len(gotEvents) != 1 || gotEvents[0].Type != "session.priority_changed" || gotEvents[0].Payload["priority"] != domain.PriorityHigh || gotEvents[0].Payload["updatedAt"] != time.Unix(40, 0).UTC() {
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

func TestAppendPromptPersistsOrderedArtifactIDsWithoutCopying(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusWaitingApproval}
	files := newFakeAttachmentStore()
	files.sessionAttachments["artifact-1"] = domain.SessionFile{ID: "artifact-1", SessionID: "session-1", Role: domain.FileRoleArtifact, Path: "/archive/first.png", MimeType: "image/png"}
	files.sessionAttachments["artifact-2"] = domain.SessionFile{ID: "artifact-2", SessionID: "session-1", Role: domain.FileRoleArtifact, Path: "/archive/second.txt", MimeType: "text/plain"}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))
	service.generateID = func() (domain.ID, error) { return "append-1", nil }

	got, err := service.AppendPrompt(ctx, AppendPromptInput{
		SessionID: "session-1",
		ArtifactIDs: []domain.SessionFileID{
			"artifact-2", "artifact-1", "artifact-2",
		},
	})
	if err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if got.Body != "" || !slices.Equal(repo.appends[0].ArtifactIDs, []domain.SessionFileID{"artifact-2", "artifact-1"}) {
		t.Fatalf("persisted append = %#v", repo.appends[0])
	}
	if len(got.Artifacts) != 2 || got.Artifacts[0].ID != "artifact-2" || got.Artifacts[1].ID != "artifact-1" {
		t.Fatalf("AppendPrompt() artifacts = %#v", got.Artifacts)
	}
	if len(files.sessionAttachments) != 2 {
		t.Fatalf("artifact files were copied: %#v", files.sessionAttachments)
	}
}

func TestAppendPromptRejectsUnavailableArtifact(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusWaitingApproval}
	files := newFakeAttachmentStore()
	files.sessionAttachments["other-session"] = domain.SessionFile{ID: "other-session", SessionID: "session-2", Role: domain.FileRoleArtifact}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))

	for _, id := range []domain.SessionFileID{"missing", "other-session"} {
		_, err := service.AppendPrompt(context.Background(), AppendPromptInput{SessionID: "session-1", ArtifactIDs: []domain.SessionFileID{id}})
		if err == nil {
			t.Fatalf("AppendPrompt(%s) expected validation error", id)
		}
		var appErr *apperror.Error
		if !errors.As(err, &appErr) || appErr.Code != apperror.CodeValidationFailed {
			t.Fatalf("AppendPrompt(%s) error = %v, want validation error", id, err)
		}
	}
}

func TestPendingPromptInputUsesLiveArtifactsAndCancelsEmptyStaleAppend(t *testing.T) {
	repo := newFakeRepository()
	files := newFakeAttachmentStore()
	files.sessionAttachments["image"] = domain.SessionFile{ID: "image", SessionID: "session-1", Role: domain.FileRoleArtifact, Path: "/archive/image.png", MimeType: "image/png"}
	files.sessionAttachments["note"] = domain.SessionFile{ID: "note", SessionID: "session-1", Role: domain.FileRoleArtifact, Path: "/archive/note.txt", MimeType: "text/plain"}
	appends := []domain.PromptAppend{
		{ID: "append-live", SessionID: "session-1", Body: "inspect", Status: domain.PromptAppendPending, ArtifactIDs: []domain.SessionFileID{"image", "note"}},
		{ID: "append-stale", SessionID: "session-1", Status: domain.PromptAppendPending, ArtifactIDs: []domain.SessionFileID{"missing"}},
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files))

	prompt, ids, paths, images, cancelled, err := service.pendingPromptInput(context.Background(), "session-1", appends)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "/archive/image.png") || !strings.Contains(prompt, "/archive/note.txt") {
		t.Fatalf("prompt = %q", prompt)
	}
	if !slices.Equal(ids, []string{"append-live"}) || !slices.Equal(paths, []string{"/archive/image.png", "/archive/note.txt"}) || !slices.Equal(images, []string{"/archive/image.png"}) || !slices.Equal(cancelled, []string{"append-stale"}) {
		t.Fatalf("ids=%#v paths=%#v images=%#v cancelled=%#v", ids, paths, images, cancelled)
	}
}

func TestStartCodexNowCancelsUnavailableAttachmentOnlyPromptWithoutProcess(t *testing.T) {
	repo := newFakeRepository()
	queued := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusQueued,
		WorktreePath: "/workspace/session-1",
		Queue:        domain.QueueIntent{Kind: domain.QueueKindPromptAppend, Priority: domain.QueuePriorityMedium},
	}
	repo.sessions[queued.ID] = queued
	repo.appends = []domain.PromptAppend{{
		ID: "append-stale", SessionID: queued.ID, Status: domain.PromptAppendPending,
		ArtifactIDs: []domain.SessionFileID{"missing"},
	}}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, newFakeAttachmentStore()), WithEvents(events))
	service.now = func() time.Time { return time.Unix(50, 0).UTC() }
	drainObservedReleasedWorkdir := false
	service.queueDrainScheduler = func(service *Service) {
		drainObservedReleasedWorkdir = service.reserveWorkdir(queued.WorktreePath, "session-2")
		service.releaseWorkdir(queued.WorktreePath, "session-2")
	}

	got, err := service.startCodexWithWorkdirReservation(context.Background(), queued, codexStartOptions{queueKind: domain.QueueKindPromptAppend}, 1)
	if err != nil {
		t.Fatalf("startCodexWithWorkdirReservation() error = %v", err)
	}
	if got.Status != domain.StatusStopped || repo.sessions[queued.ID].Status != domain.StatusStopped {
		t.Fatalf("settled session = %#v persisted=%#v", got, repo.sessions[queued.ID])
	}
	if !slices.Equal(repo.deletedAppends, []string{"append-stale"}) || len(repo.appends) != 0 {
		t.Fatalf("deleted appends=%#v remaining=%#v", repo.deletedAppends, repo.appends)
	}
	if !drainObservedReleasedWorkdir {
		t.Fatal("queue drain ran before the settled prompt queue released its workdir")
	}
	if !service.reserveWorkdir(queued.WorktreePath, "session-2") {
		t.Fatal("workdir remained reserved after prompt queue settled without a process")
	}
	requireSessionEventTypes(t, events.snapshot(), "session.prompt_append_cancelled", sessionStatusUpdatedEvent)
}

func TestStartCodexNowSettlesPromptQueueAfterCancelledAppendWasAlreadyDeleted(t *testing.T) {
	repo := newFakeRepository()
	queued := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusQueued,
		Queue: domain.QueueIntent{Kind: domain.QueueKindPromptAppend, Priority: domain.QueuePriorityMedium},
	}
	repo.sessions[queued.ID] = queued
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, newFakeAttachmentStore()))

	got, err := service.startCodexNow(context.Background(), queued, codexStartOptions{queueKind: domain.QueueKindPromptAppend}, "/workspace", 1)
	if err != nil {
		t.Fatalf("startCodexNow() error = %v", err)
	}
	if got.Status != domain.StatusStopped || repo.sessions[queued.ID].Status != domain.StatusStopped {
		t.Fatalf("settled session = %#v persisted=%#v", got, repo.sessions[queued.ID])
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
	if got.Body != "" || repo.appends[0].Body != "" {
		t.Fatalf("attachment-only append body = DTO %q persisted %q", got.Body, repo.appends[0].Body)
	}
	if !files.promoted["staged-1"] {
		t.Fatalf("staged attachment was not promoted")
	}
	if _, ok := repo.stagedAttachments["staged-1"]; ok {
		t.Fatalf("staged attachment was not deleted")
	}
	attachment, ok := files.sessionAttachments["staged-1"]
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
	attachment, ok := files.sessionAttachments["staged-1"]
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

func TestUpdatePromptAppendAllowsPendingAppendBeforeFirstProcessRun(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusQueued,
	}
	createdAt := time.Unix(20, 0).UTC()
	repo.appends = []domain.PromptAppend{{
		ID:        "append-1",
		SessionID: "session-1",
		Body:      "before",
		Status:    domain.PromptAppendPending,
		CreatedAt: createdAt,
	}}
	files := newFakeAttachmentStore()
	files.sessionAttachments["attachment-1"] = domain.SessionAttachment{
		ID:         "attachment-1",
		SessionID:  "session-1",
		Role:       domain.FileRoleInput,
		SourceType: domain.AttachmentSourcePromptAppend,
		SourceID:   "append-1",
		Filename:   "notes.txt",
	}
	processes := newFakeProcessRepository()
	locker := &fakeSessionLocker{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithAttachments(repo, files),
		WithProcesses(processes, nil),
		WithUnitOfWork(uow),
		WithSessionLocker(locker),
	)

	got, err := service.UpdatePromptAppend(ctx, UpdatePromptAppendInput{
		SessionID:      "session-1",
		PromptAppendID: "append-1",
		Body:           "  after  ",
	})
	if err != nil {
		t.Fatalf("UpdatePromptAppend() error = %v", err)
	}
	if got.Body != "after" || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("UpdatePromptAppend() = %#v", got)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].ID != "attachment-1" {
		t.Fatalf("UpdatePromptAppend() attachments = %#v", got.Attachments)
	}
	if !uow.called || !reflect.DeepEqual(locker.ids, []domain.ID{"session-1"}) {
		t.Fatalf("uow/locker = called:%v ids:%#v", uow.called, locker.ids)
	}
	if updated := repo.appends[0]; updated.Body != "after" || updated.Status != domain.PromptAppendPending || !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("updated append = %#v", updated)
	}
}

func TestUpdatePromptAppendRejectsAnyHistoricalProcessRun(t *testing.T) {
	for _, status := range []domain.Status{
		domain.StatusQueued,
		domain.StatusStopped,
		domain.StatusFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			repo := newFakeRepository()
			repo.sessions["session-1"] = domain.Session{ID: "session-1", Status: status}
			repo.appends = []domain.PromptAppend{{
				ID:        "append-1",
				SessionID: "session-1",
				Body:      "before",
				Status:    domain.PromptAppendPending,
			}}
			processes := newFakeProcessRepository()
			processes.created = []processdomain.Run{{
				ID:        "process-1",
				SessionID: "session-1",
				Status:    processdomain.StatusExited,
			}}
			service := New(
				repo,
				newFakeProjectRepository("project-1"),
				WithProcesses(processes, nil),
				WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes}}),
				WithSessionLocker(NewMemorySessionLocker()),
			)

			_, err := service.UpdatePromptAppend(ctx, UpdatePromptAppendInput{
				SessionID:      "session-1",
				PromptAppendID: "append-1",
				Body:           "after",
			})
			appErr, ok := apperror.From(err)
			if !ok || appErr.Code != apperror.CodePromptEditAfterStart || appErr.Category != apperror.CategoryValidationError {
				t.Fatalf("UpdatePromptAppend() error = %#v", err)
			}
			if appErr.Message != "流程已开始运行，无法编辑追加提示" || appErr.Retryable || appErr.UserAction != "review_session" {
				t.Fatalf("UpdatePromptAppend() structured error = %#v", appErr)
			}
			if appErr.Details["sessionId"] != "session-1" || appErr.Details["promptAppendId"] != "append-1" {
				t.Fatalf("UpdatePromptAppend() details = %#v", appErr.Details)
			}
			if repo.appends[0].Body != "before" {
				t.Fatalf("append was modified: %#v", repo.appends[0])
			}
		})
	}
}

func TestUpdatePromptAppendRejectsInvalidTargetWithoutModification(t *testing.T) {
	tests := []struct {
		name          string
		input         UpdatePromptAppendInput
		append        domain.PromptAppend
		sessionStatus domain.Status
		wantCode      string
	}{
		{
			name:     "blank body",
			input:    UpdatePromptAppendInput{SessionID: "session-1", PromptAppendID: "append-1", Body: "  "},
			append:   domain.PromptAppend{ID: "append-1", SessionID: "session-1", Body: "before", Status: domain.PromptAppendPending},
			wantCode: apperror.CodeValidationFailed,
		},
		{
			name:     "wrong session",
			input:    UpdatePromptAppendInput{SessionID: "session-1", PromptAppendID: "append-1", Body: "after"},
			append:   domain.PromptAppend{ID: "append-1", SessionID: "session-2", Body: "before", Status: domain.PromptAppendPending},
			wantCode: apperror.CodeNotFound,
		},
		{
			name:     "not pending",
			input:    UpdatePromptAppendInput{SessionID: "session-1", PromptAppendID: "append-1", Body: "after"},
			append:   domain.PromptAppend{ID: "append-1", SessionID: "session-1", Body: "before", Status: domain.PromptAppendDispatched},
			wantCode: apperror.CodeNotFound,
		},
		{
			name:          "closed session",
			input:         UpdatePromptAppendInput{SessionID: "session-1", PromptAppendID: "append-1", Body: "after"},
			append:        domain.PromptAppend{ID: "append-1", SessionID: "session-1", Body: "before", Status: domain.PromptAppendPending},
			sessionStatus: domain.StatusClosed,
			wantCode:      apperror.CodeValidationFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeRepository()
			status := test.sessionStatus
			if status == "" {
				status = domain.StatusQueued
			}
			repo.sessions["session-1"] = domain.Session{ID: "session-1", Status: status}
			repo.appends = []domain.PromptAppend{test.append}
			processes := newFakeProcessRepository()
			service := New(
				repo,
				newFakeProjectRepository("project-1"),
				WithProcesses(processes, nil),
				WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes}}),
				WithSessionLocker(NewMemorySessionLocker()),
			)

			_, err := service.UpdatePromptAppend(context.Background(), test.input)
			appErr, ok := apperror.From(err)
			if !ok || appErr.Code != test.wantCode {
				t.Fatalf("UpdatePromptAppend() error = %#v", err)
			}
			if repo.appends[0].Body != "before" {
				t.Fatalf("append was modified: %#v", repo.appends[0])
			}
		})
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
	if got.Body != "" {
		t.Fatalf("AppendPrompt() body = %q", got.Body)
	}
	attachment, ok := files.sessionAttachments["staged-1"]
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
	if _, ok := files.sessionAttachments["staged-1"]; ok {
		t.Fatal("session attachment file was not rolled back")
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
	if _, ok := files.sessionAttachments["staged-1"]; ok {
		t.Fatal("session attachment file was not rolled back")
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
	if _, ok := files.sessionAttachments["staged-1"]; ok {
		t.Fatal("session attachment file was not rolled back")
	}
	if !files.deletedSessions["staged-1"] {
		t.Fatal("session attachment file was not rolled back")
	}
}

func TestAppendPromptPreservesPendingContentWhenAutoQueueFails(t *testing.T) {
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
	if _, ok := files.sessionAttachments["staged-1"]; !ok {
		t.Fatal("session attachment file was not preserved")
	}
	if files.deletedSessions["staged-1"] {
		t.Fatal("session attachment file was deleted")
	}
	if len(repo.appends) != 1 || repo.appends[0].Status != domain.PromptAppendPending {
		t.Fatalf("pending append was not preserved: %#v", repo.appends)
	}
	if len(repo.deletedAppends) != 0 {
		t.Fatalf("deleted appends = %#v", repo.deletedAppends)
	}
}

func TestAppendPromptQueuesStoppedChatInOneUnitOfWork(t *testing.T) {
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
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: newFakeProcessRepository(), events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithUnitOfWork(uow))
	ids := []domain.ID{"append-1", "event-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "continue"}); err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if uow.calls != 1 {
		t.Fatalf("unit of work calls = %d", uow.calls)
	}
	if len(repo.appends) != 1 || repo.appends[0].Status != domain.PromptAppendPending {
		t.Fatalf("prompt appends = %#v", repo.appends)
	}
	queued := repo.sessions["session-1"]
	if queued.Status != domain.StatusQueued || queued.Queue.Kind != domain.QueueKindPromptAppend || queued.Queue.ResumeCodexSessionID != "codex-session-1" {
		t.Fatalf("queued session = %#v", queued)
	}
	requireSessionEventTypes(t, events.events, "session.queued", sessionStatusUpdatedEvent)
}

func TestAnswerDeliveryWorkdirContentionKeepsAnswerQueueKind(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusResumeFailed,
		WorktreePath: "/workspace/shared", Priority: domain.PriorityMedium,
	}
	repo.sessions[session.ID] = session
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}))
	if !service.reserveWorkdir(session.WorktreePath, "session-2") {
		t.Fatal("reserve shared workdir for competing session")
	}

	queued, err := service.startCodex(ctx, session, codexStartOptions{
		resumeCodexSessionID: "codex-session-1",
		resumeOfProcessRunID: "process-run-1",
		answerBatchID:        "batch-1",
		prompt:               "deliver answer",
	}, true)
	if err != nil {
		t.Fatalf("startCodex() error = %v", err)
	}
	saved := repo.sessions[session.ID]
	if queued.Status != domain.StatusQueued || saved.Queue.Kind != domain.QueueKindAnswerUser || saved.Queue.Priority != domain.QueuePriorityHigh || saved.Queue.AnswerBatchID != "batch-1" {
		t.Fatalf("answer delivery queue = %#v", saved)
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
			FastMode:        true,
		},
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events))
	service.now = func() time.Time { return time.Unix(25, 0).UTC() }
	ids := []domain.ID{"event-config-1", "event-config-2"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.UpdateSessionConfig(ctx, UpdateSessionConfigInput{
		SessionID: "session-1",
		Config: ConfigInput{
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
		FastMode:        true,
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
	fastMode := false
	got, err = service.UpdateSessionConfig(ctx, UpdateSessionConfigInput{
		SessionID: "session-1",
		Config: ConfigInput{
			CodexModel:      "gpt-5.4-mini",
			ReasoningEffort: "high",
			PermissionMode:  "workspace-write",
			FastMode:        &fastMode,
		},
	})
	if err != nil || got.Config.FastMode || repo.sessions["session-1"].Config.FastMode {
		t.Fatalf("explicit FastMode false was not persisted: got=%#v saved=%#v err=%v", got.Config, repo.sessions["session-1"].Config, err)
	}
	gotEvents := events.snapshot()
	if len(gotEvents) != 2 {
		t.Fatalf("session.config_changed events = %#v", gotEvents)
	}
	firstConfig, firstOK := gotEvents[0].Payload["config"].(domain.Config)
	secondConfig, secondOK := gotEvents[1].Payload["config"].(domain.Config)
	if !firstOK || !secondOK || !firstConfig.FastMode || secondConfig.FastMode || gotEvents[0].Payload["updatedAt"] != time.Unix(25, 0).UTC() || gotEvents[1].Payload["updatedAt"] != time.Unix(25, 0).UTC() {
		t.Fatalf("session.config_changed events = %#v", gotEvents)
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
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, newFakeAttachmentStore()), WithProcesses(processes, codex))
	ids := []domain.ID{"append-1", "process-run-1", "event-starting", "event-transcript-bound", "event-running"}
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
	if repo.sessions["session-1"].Queue.Prompt != "" {
		t.Fatalf("queue should keep only execution intent: %#v", repo.sessions["session-1"].Queue)
	}
}

func TestAppendPromptQueuesStoppedChatSession(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	lastRunAt := time.Unix(5, 0).UTC()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "original requirement",
		Mode:           domain.ModeChat,
		Status:         domain.StatusStopped,
		BaseBranch:     "main",
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
		LastRunAt:      &lastRunAt,
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-0", SessionID: "session-1", Body: "old context", CreatedAt: time.Unix(10, 0).UTC()},
	}
	repo.stagedAttachments["staged-1"] = domain.StagedAttachment{
		ID:       "staged-1",
		Filename: "new-note.md",
		Path:     "/attachments/staged/staged-1/new-note.md",
		MimeType: "text/markdown",
	}
	processes := newFakeProcessRepository()
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}, events: stream}
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
	if saved.Status != domain.StatusQueued || saved.Queue.Kind != domain.QueueKindPromptAppend {
		t.Fatalf("queued session = %#v", saved)
	}
	if saved.Queue.ResumeCodexSessionID != "codex-session-1" {
		t.Fatalf("resume codex session id = %q", saved.Queue.ResumeCodexSessionID)
	}
	if saved.Queue.InitialStart {
		t.Fatalf("resume queue should not be marked as initial start: %#v", saved.Queue)
	}
	newPath := "/attachments/sessions/session-1/staged-1/new-note.md"
	if saved.Queue.Prompt != "" {
		t.Fatalf("resume queue should resolve pending prompts at launch: %#v", saved.Queue)
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
	if codex.resumeInput.Prompt != "only this new instruction\n\nAttached files available on disk:\n- "+newPath {
		t.Fatalf("codex resume prompt = %q", codex.resumeInput.Prompt)
	}
	if strings.Contains(codex.resumeInput.Prompt, anyCodePromptGuidance) || strings.Contains(codex.resumeInput.Prompt, managedWorktreePromptGuidance) {
		t.Fatalf("codex resume prompt should not repeat session guidance: %q", codex.resumeInput.Prompt)
	}
	if strings.Contains(codex.resumeInput.Prompt, "/data/attachments/sessions/session-1/notes.md") {
		t.Fatalf("codex resume prompt should not include old attachment path: %q", codex.resumeInput.Prompt)
	}
	if files.lastPromptAppendAttachmentSessionID != "session-1" || files.lastPromptAppendAttachmentID != "append-1" {
		t.Fatalf("prompt append attachment query session=%q append=%q", files.lastPromptAppendAttachmentSessionID, files.lastPromptAppendAttachmentID)
	}
	if len(repo.appends) != 2 || repo.appends[1].Status != domain.PromptAppendInflight || repo.appends[1].DispatchedProcessRunID != "process-run-1" {
		t.Fatalf("prompt append delivery state = %#v", repo.appends)
	}
}

func TestAppendPromptResumesWorkflowWithOnlyPendingContent(t *testing.T) {
	for _, test := range []struct {
		name                        string
		missingTranscript           bool
		resumeTranscriptUnavailable bool
	}{
		{name: "resume existing Codex session"},
		{name: "start full context when transcript is unavailable", missingTranscript: true},
		{name: "start full context when resume reports transcript unavailable", resumeTranscriptUnavailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repo := newFakeRepository()
			repo.sessions["session-1"] = domain.Session{
				ID:             "session-1",
				ProjectID:      "project-1",
				Requirement:    "original requirement",
				Mode:           domain.ModeWorkflow,
				Status:         domain.StatusStopped,
				BaseBranch:     "main",
				CodexSessionID: "codex-session-1",
				WorktreePath:   "/workspace/session-1",
			}
			nodeRunID := domain.NodeRunID("node-run-1")
			workflows := &fakeWorkflowStarter{resumeNodeAdvance: domain.WorkflowAdvance{
				SessionID:     "session-1",
				NodeRunID:     &nodeRunID,
				RequiresCodex: true,
				Prompt:        "Workflow node\n\nWorkflow input params JSON:\n{}\n\nWorkflow result contract",
			}}
			processes := newFakeProcessRepository()
			processes.transcriptMissing = test.missingTranscript
			events := make(chan processdomain.CodexEvent)
			close(events)
			codex := &fakeCodexProcess{
				resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"},
				events:       events,
			}
			if test.resumeTranscriptUnavailable {
				codex.resumeErr = processdomain.ErrTranscriptUnavailable
			}
			service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithWorkflows(workflows))
			service.artifacts = &fakeSessionArtifactStore{}
			nextID := 0
			service.generateID = func() (domain.ID, error) {
				nextID++
				return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
			}

			if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "only this instruction"}); err != nil {
				t.Fatalf("AppendPrompt() error = %v", err)
			}
			queued := repo.sessions["session-1"]
			if queued.Queue.Kind != domain.QueueKindPromptAppend {
				t.Fatalf("queue kind = %q", queued.Queue.Kind)
			}
			if started, err := service.DrainQueuedSessions(ctx); err != nil || started != 1 {
				t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
			}
			if processes.transcriptLookupSessionID != "session-1" {
				t.Fatalf("transcript lookup session = %q", processes.transcriptLookupSessionID)
			}
			if !test.missingTranscript && !test.resumeTranscriptUnavailable {
				if !codex.resumeCalled || codex.startCalled || codex.resumeInput.Prompt != "only this instruction" {
					t.Fatalf("codex resume=%v start=%v prompt=%q", codex.resumeCalled, codex.startCalled, codex.resumeInput.Prompt)
				}
				return
			}
			if !codex.startCalled || (codex.resumeCalled != test.resumeTranscriptUnavailable) {
				t.Fatalf("codex calls start=%v resume=%v", codex.startCalled, codex.resumeCalled)
			}
			prompt := codex.startInput.Prompt
			for _, want := range []string{"original requirement", "Workflow node", artifactPromptGuidance, anyCodePromptGuidance} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("fallback prompt missing %q: %q", want, prompt)
				}
			}
			if strings.Count(prompt, "only this instruction") != 1 {
				t.Fatalf("fallback prompt append count = %d: %q", strings.Count(prompt, "only this instruction"), prompt)
			}
		})
	}
}

func TestQueuedResumeIncludesAppendsAddedWhileQueued(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	processes.activeCount = 1
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1))
	ids := []domain.ID{"append-1", "append-2", "process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "first"}); err != nil {
		t.Fatalf("first AppendPrompt() error = %v", err)
	}
	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "second"}); err != nil {
		t.Fatalf("second AppendPrompt() error = %v", err)
	}
	processes.activeCount = 0
	if started, err := service.DrainQueuedSessions(ctx); err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	if codex.resumeInput.Prompt != "first\n\nsecond" {
		t.Fatalf("resume prompt = %q", codex.resumeInput.Prompt)
	}
	for _, promptAppend := range repo.appends {
		if promptAppend.Status != domain.PromptAppendInflight || promptAppend.DispatchedProcessRunID != "process-run-1" {
			t.Fatalf("prompt append was not marked inflight: %#v", repo.appends)
		}
	}
}

func TestQueuedStartIncludesAppendsAddedWhileQueued(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Requirement: "original", Mode: domain.ModeChat,
		Status: domain.StatusStopped, WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	processes.activeCount = 1
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-new"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1))
	ids := []domain.ID{"append-1", "append-2", "process-run-1"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "first"}); err != nil {
		t.Fatalf("first AppendPrompt() error = %v", err)
	}
	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "second"}); err != nil {
		t.Fatalf("second AppendPrompt() error = %v", err)
	}
	processes.activeCount = 0
	if started, err := service.DrainQueuedSessions(ctx); err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	for _, want := range []string{"原始需求：\noriginal", "追加描述：\nfirst", "追加描述：\nsecond"} {
		if !strings.Contains(codex.startInput.Prompt, want) {
			t.Fatalf("start prompt missing %q: %q", want, codex.startInput.Prompt)
		}
	}
	for _, promptAppend := range repo.appends {
		if promptAppend.Status != domain.PromptAppendInflight {
			t.Fatalf("prompt append was not marked inflight: %#v", repo.appends)
		}
	}
}

func TestRunningAppendRemainsPendingAfterProcessExit(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Requirement: "original", Mode: domain.ModeChat,
		Status: domain.StatusCreated, WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"},
		events:      stream,
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithSessionLocker(NewMemorySessionLocker()))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	stream <- transcriptReadyEvent("codex-session-1")
	waitForSessionStatus(t, repo, "session-1", domain.StatusRunning)
	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "review the result"}); err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if got := repo.sessions["session-1"]; got.Status != domain.StatusRunning || len(repo.appends) != 1 || repo.appends[0].Status != domain.PromptAppendPending {
		t.Fatalf("running append state = session=%#v appends=%#v", got, repo.appends)
	}
	close(stream)
	waitForEventType(t, events, "session.stopped")
	stopped := repo.sessions["session-1"]
	if stopped.Status != domain.StatusStopped || stopped.Queue != (domain.QueueIntent{}) {
		t.Fatalf("post-exit session = %#v", stopped)
	}
	if repo.appends[0].Status != domain.PromptAppendPending {
		t.Fatalf("append should remain pending until explicit resume: %#v", repo.appends[0])
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
	if saved.Queue.Kind != domain.QueueKindPromptAppend || saved.Queue.ResumeCodexSessionID != "codex-session-current" {
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

	queue := repo.sessions["session-1"].Queue
	if queue.Prompt != "" || queue.ReviewAfterReuseFailure {
		t.Fatalf("queued start intent = %#v", queue)
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
		BaseBranch:   "main",
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
	if !strings.Contains(prompt, anyCodePromptGuidance) {
		t.Fatalf("chat rebuilt start prompt missing TODO guidance: %q", prompt)
	}
	if !strings.Contains(prompt, managedWorktreePromptGuidance) {
		t.Fatalf("git chat rebuilt prompt missing worktree guidance: %q", prompt)
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
	if saved.Status != domain.StatusQueued || saved.Queue.Kind != domain.QueueKindPromptAppend || saved.Queue.ResumeCodexSessionID != "" {
		t.Fatalf("queued session = %#v", saved)
	}
	if saved.Queue.Prompt != "" || !saved.Queue.ReviewAfterReuseFailure {
		t.Fatalf("queued rebuild intent = %#v", saved.Queue)
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

func TestCloseSessionQuarantinesAndDeletesArtifactOutput(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusCreated}
	artifacts := &fakeSessionArtifactStore{}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.artifacts = artifacts
	service.generateID = func() (domain.ID, error) { return "close-token", nil }
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if artifacts.quarantinedToken != "close-token" || artifacts.deletedQuarantine != "/trash/session-1/close-token" || artifacts.restoredQuarantine != "" {
		t.Fatalf("artifact lifecycle = %#v", artifacts)
	}
}

func TestCloseSessionDeletesOutputDirectoryAndPreservesInput(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(30, 0).UTC()
	if err := store.Sessions().Create(ctx, domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusCreated,
		CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	files := filestore.New(dataDir)
	artifacts := artifactapp.New(files, store.Sessions())
	outputDir, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(outputDir, "result.txt")
	if err := os.WriteFile(source, []byte("result"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact, err := artifacts.Publish(ctx, artifactapp.PublishInput{SessionID: "session-1", Path: source})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Sessions().UpdateArtifactCount(ctx, "session-1", 1); err != nil {
		t.Fatal(err)
	}
	staged, err := files.Stage(ctx, domain.StageAttachmentInput{Filename: "input.txt", Reader: strings.NewReader("input")})
	if err != nil {
		t.Fatal(err)
	}
	input, err := files.Promote(ctx, domain.PromoteAttachmentInput{
		Staged: staged, SessionID: "session-1", SourceType: domain.AttachmentSourceRequirement, SourceID: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	service := New(store.Sessions(), newFakeProjectRepository("project-1"),
		WithAttachments(store.Attachments(), files),
		WithEvents(store.Events()),
		WithUnitOfWork(store),
	)
	service.now = func() time.Time { return now }
	closed, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if closed.Status != domain.StatusClosed || closed.ArtifactCount != 0 {
		t.Fatalf("closed session = %#v", closed)
	}
	if _, err := files.FindArtifact(ctx, artifact.ID); !errors.Is(err, domain.ErrSessionFileNotFound) {
		t.Fatalf("artifact after close error = %v", err)
	}
	inputs, err := files.ListSessionAttachments(ctx, "session-1")
	if err != nil || len(inputs) != 1 || inputs[0].ID != input.ID {
		t.Fatalf("inputs after close = %#v err=%v", inputs, err)
	}
	if _, err := os.Stat(inputs[0].Path); err != nil {
		t.Fatalf("input was deleted: %v", err)
	}
	if _, err := os.Stat(outputDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact output still exists: %v", err)
	}
	persisted, err := store.Sessions().Find(ctx, "session-1")
	if err != nil || persisted.ArtifactCount != 0 {
		t.Fatalf("persisted artifact count = %d, err=%v", persisted.ArtifactCount, err)
	}
	sessionID := eventdomain.SessionID("session-1")
	events, err := store.Events().List(ctx, eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	var closedEvent, artifactEvent, statusEvent bool
	for _, event := range events {
		closedEvent = closedEvent || event.Type == "session.closed"
		artifactEvent = artifactEvent || event.Type == "session.artifacts_updated" && fmt.Sprint(event.Payload["artifactCount"]) == "0"
		statusEvent = statusEvent || event.Type == sessionStatusUpdatedEvent && event.Causality.SessionStatus == string(domain.StatusClosed)
	}
	if !closedEvent || !artifactEvent || !statusEvent {
		t.Fatalf("close events = %#v", events)
	}
}

func TestArchiveCodexEventImagesReplacesInlineDataWithStoredReference(t *testing.T) {
	publisher := &fakeInlineArtifactPublisher{}
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"), WithArtifactPublisher(publisher))
	event := processdomain.CodexEvent{
		EventID: "event-1", CorrelationID: "call-1",
		Content: processdomain.CodexToolContent{Images: []processdomain.CodexImage{
			{Source: "data:image/png;base64,cG5n", SourceKind: "inline", Detail: "high"},
			{Source: "https://example.invalid/image.png", SourceKind: "remote"},
		}},
	}
	if failures := service.archiveCodexEventImages(context.Background(), domain.Session{ID: "session-1"}, processdomain.CodexHandle{ProcessRunID: "process-1"}, &event); len(failures) != 0 {
		t.Fatalf("archive failures = %#v", failures)
	}
	if string(publisher.input.Data) != "png" || publisher.input.SourceKey != "process-1:event-1:0" {
		t.Fatalf("inline artifact input = %#v", publisher.input)
	}
	content := event.Content.(processdomain.CodexToolContent)
	if len(content.Images) != 1 || content.Images[0].Source != "/files/artifact-1/preview" || content.Images[0].SourceKind != "stored" {
		t.Fatalf("stored images = %#v", content.Images)
	}
}

func TestArchiveCodexEventImagesArchivesAudioWithoutExposingItAsImage(t *testing.T) {
	publisher := &fakeInlineArtifactPublisher{previewKind: domain.PreviewKindAudio}
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"), WithArtifactPublisher(publisher))
	event := processdomain.CodexEvent{
		EventID: "event-audio",
		Content: processdomain.CodexToolContent{Images: []processdomain.CodexImage{
			{Source: "YXVkaW8=", SourceKind: "inline_base64", MimeType: "audio/mpeg"},
			{Source: "https://example.invalid/audio.mp3", SourceKind: "remote", MimeType: "audio/mpeg"},
		}},
	}
	if failures := service.archiveCodexEventImages(context.Background(), domain.Session{ID: "session-1"}, processdomain.CodexHandle{ProcessRunID: "process-1"}, &event); len(failures) != 0 {
		t.Fatalf("archive failures = %#v", failures)
	}
	if string(publisher.input.Data) != "audio" || publisher.input.Filename != "event-audio-1.mp3" {
		t.Fatalf("audio artifact input = %#v", publisher.input)
	}
	if images := event.Content.(processdomain.CodexToolContent).Images; len(images) != 0 {
		t.Fatalf("non-image candidate leaked into transcript images = %#v", images)
	}
}

func TestArchiveCodexEventImagesIsolatesCandidateFailure(t *testing.T) {
	publisher := &fakeInlineArtifactPublisher{err: errors.New("storage unavailable at /private/archive")}
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"), WithArtifactPublisher(publisher))
	event := processdomain.CodexEvent{
		EventID: "event-failed",
		Content: processdomain.CodexToolContent{Images: []processdomain.CodexImage{
			{Source: "data:image/png;base64,cG5n", SourceKind: "inline", MimeType: "image/png"},
		}},
	}
	failures := service.archiveCodexEventImages(context.Background(), domain.Session{ID: "session-1"}, processdomain.CodexHandle{ProcessRunID: "process-1"}, &event)
	if len(failures) != 1 || failures[0].eventType != "session.artifact_archive_failed" {
		t.Fatalf("archive failures = %#v", failures)
	}
	if payload := failures[0].payload; payload["mimeType"] != "image/png" || strings.Contains(fmt.Sprint(payload), "/private/archive") || strings.Contains(fmt.Sprint(payload), "cG5n") {
		t.Fatalf("failure payload leaked source data = %#v", payload)
	}
	if message := fmt.Sprint(failures[0].payload["message"]); !strings.Contains(message, "临时文件") || strings.Contains(message, "产物") {
		t.Fatalf("failure message = %q", message)
	}
	if images := event.Content.(processdomain.CodexToolContent).Images; len(images) != 0 {
		t.Fatalf("failed candidate remained in event = %#v", images)
	}
}

func TestArchiveCodexEventImagesDoesNotRepublishPublishArtifactResult(t *testing.T) {
	publisher := &fakeInlineArtifactPublisher{}
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"), WithArtifactPublisher(publisher))
	event := processdomain.CodexEvent{
		EventID: "event-publish",
		Content: processdomain.CodexToolContent{
			QualifiedName: "mcp__anycode.publish_artifact",
			Output:        processdomain.CodexStructuredText{Format: processdomain.CodexTextJSON, Text: `{"content":[{"type":"image","data":"cG5n","mimeType":"image/png"}]}`},
			Images:        []processdomain.CodexImage{{Source: "data:image/png;base64,cG5n", SourceKind: "inline"}},
		},
	}
	if failures := service.archiveCodexEventImages(context.Background(), domain.Session{ID: "session-1"}, processdomain.CodexHandle{ProcessRunID: "process-1"}, &event); len(failures) != 0 {
		t.Fatalf("archive failures = %#v", failures)
	}
	content := event.Content.(processdomain.CodexToolContent)
	if len(publisher.input.Data) != 0 || len(content.Images) != 0 || strings.Contains(content.Output.Text, "cG5n") {
		t.Fatalf("publish_artifact result was re-archived: input=%#v event=%#v", publisher.input, event.Content)
	}
}

func TestCloseSessionRestoresArtifactOutputWhenFinalSaveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusCreated}
	repo.saveHook = func(value domain.Session) error {
		if value.Status == domain.StatusClosed {
			return errors.New("save closed failed")
		}
		return nil
	}
	artifacts := &fakeSessionArtifactStore{}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.artifacts = artifacts
	service.generateID = func() (domain.ID, error) { return "close-token", nil }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected error")
	}
	if artifacts.restoredQuarantine != "/trash/session-1/close-token" || artifacts.deletedQuarantine != "" {
		t.Fatalf("artifact rollback = %#v", artifacts)
	}
	if repo.sessions["session-1"].Status != domain.StatusStopped {
		t.Fatalf("session status = %q", repo.sessions["session-1"].Status)
	}
}

func TestCloseSessionPersistsCleanupRequestWithoutGitSideEffects(t *testing.T) {
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
	worktrees.headCommit = "closed-head"
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed {
		t.Fatalf("CloseSession() status = %q", got.Status)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("close performed git cleanup: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreeHeadCommit != "closed-head" {
		t.Fatalf("captured worktree head = %q", saved.WorktreeHeadCommit)
	}
	if worktrees.headCommitPath != "/data/worktrees/project-1/session-1" || worktrees.headCommitRef != "" {
		t.Fatalf("HeadCommit() = path %q ref %q", worktrees.headCommitPath, worktrees.headCommitRef)
	}
	if got.WorktreePath != "/data/worktrees/project-1/session-1" || got.WorktreeBaseCommit != "base" || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("closed session worktree fields = %#v", got)
	}
}

func TestDrainWorktreeCleanupRemovesPersistedResources(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	confirmedAt := time.Unix(34, 0).UTC()
	repo.sessions["session-1"] = domain.Session{
		ID:                 "session-1",
		ProjectID:          "project-1",
		Status:             domain.StatusClosed,
		BaseBranch:         "main",
		WorktreePath:       "/data/worktrees/project-1/session-1",
		WorktreeBranch:     "session-1",
		WorktreeHeadCommit: "closed-head",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status:               domain.WorktreeCleanupPending,
			OwnershipToken:       "test-owner-token",
			OwnershipConfirmedAt: &confirmedAt,
		},
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true}
	worktrees := newFakeWorktreeManager()
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(35, 0).UTC() }

	processed, err := service.DrainWorktreeCleanup(ctx)
	if err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if processed != 1 {
		t.Fatalf("DrainWorktreeCleanup() = %d", processed)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) || !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1"}) {
		t.Fatalf("cleanup calls = removed:%#v branches:%#v", worktrees.removed, worktrees.deletedBranches)
	}
	if !slices.Equal(worktrees.retainedCommits, []string{"/workspace/project-1:session-1:closed-head"}) {
		t.Fatalf("retained commits = %#v", worktrees.retainedCommits)
	}
	if !slices.Equal(worktrees.operations, []string{"retain", "remove", "delete_branch", "release_ownership"}) {
		t.Fatalf("cleanup operation order = %#v", worktrees.operations)
	}
	if !slices.Equal(worktrees.releasedOwnership, []string{"/data/worktrees/project-1/session-1:test-owner-token"}) {
		t.Fatalf("released ownership = %#v", worktrees.releasedOwnership)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreeCleanup.Status != domain.WorktreeCleanupCleaned || saved.WorktreeCleanup.CompletedAt == nil || saved.WorktreePath == "" {
		t.Fatalf("cleaned session = %#v", saved)
	}
}

func TestDrainWorktreeCleanupKeepsResourcesWhenHeadRetentionFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	confirmedAt := time.Unix(34, 0).UTC()
	repo.sessions["session-1"] = domain.Session{
		ID:                 "session-1",
		ProjectID:          "project-1",
		Status:             domain.StatusClosed,
		BaseBranch:         "main",
		WorktreePath:       "/data/worktrees/project-1/session-1",
		WorktreeBranch:     "session-1",
		WorktreeHeadCommit: "closed-head",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status:               domain.WorktreeCleanupPending,
			OwnershipToken:       "test-owner-token",
			OwnershipConfirmedAt: &confirmedAt,
		},
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true}
	worktrees := newFakeWorktreeManager()
	worktrees.retainCommitErr = errors.New("update ref failed")
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(35, 0).UTC() }

	processed, err := service.DrainWorktreeCleanup(ctx)
	if err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if processed != 1 {
		t.Fatalf("DrainWorktreeCleanup() = %d", processed)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 || len(worktrees.releasedOwnership) != 0 {
		t.Fatalf("cleanup continued after retain failure: %#v", worktrees.operations)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || saved.WorktreeCleanup.ErrorCode != "worktree_head_retain_failed" || !saved.WorktreeCleanup.Retryable {
		t.Fatalf("retention failure = %#v", saved.WorktreeCleanup)
	}
}

func TestReconcileWorktreeCleanupConvertsProvisioningToPending(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusCreated,
		BaseBranch:     "main",
		WorktreePath:   "/data/worktrees/project-1/session-1",
		WorktreeBranch: "session-1",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status: domain.WorktreeCleanupProvisioning,
		},
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(36, 0).UTC() }

	reconciled, err := service.ReconcileWorktreeCleanup(ctx)
	if err != nil {
		t.Fatalf("ReconcileWorktreeCleanup() error = %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("ReconcileWorktreeCleanup() = %d", reconciled)
	}
	if saved := repo.sessions["session-1"]; saved.Status != domain.StatusFailed || saved.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("reconciled session = %#v", saved)
	}
}

func TestReconciledProvisioningDoesNotDeleteOrdinaryDirectory(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusCreated,
		BaseBranch:     "main",
		WorktreePath:   "/data/worktrees/project-1/session-1",
		WorktreeBranch: "session-1",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status: domain.WorktreeCleanupProvisioning,
		},
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true}
	ownership := domain.WorktreeOwnership{PathExists: true}
	worktrees := &fakeWorktreeManager{ownership: &ownership}
	service := New(repo, projects, WithWorktrees(worktrees))

	if _, err := service.ReconcileWorktreeCleanup(ctx); err != nil {
		t.Fatalf("ReconcileWorktreeCleanup() error = %v", err)
	}
	if _, err := service.DrainWorktreeCleanup(ctx); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("ordinary directory was deleted: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || saved.WorktreeCleanup.ErrorCode != "worktree_ownership_unconfirmed" {
		t.Fatalf("reconciled ownership failure = %#v", saved)
	}
}

func TestDrainWorktreeCleanupRecoversClaimedProvisioningWorktree(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusFailed,
		BaseBranch:     "main",
		WorktreePath:   "/data/worktrees/project-1/session-1",
		WorktreeBranch: "session-1",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status:         domain.WorktreeCleanupPending,
			OwnershipToken: "test-owner-token",
		},
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true}
	worktrees := newFakeWorktreeManager()
	service := New(repo, projects, WithWorktrees(worktrees))

	if _, err := service.DrainWorktreeCleanup(ctx); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) || !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1"}) {
		t.Fatalf("claimed provisioning cleanup = removed:%#v branches:%#v", worktrees.removed, worktrees.deletedBranches)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreeCleanup.Status != domain.WorktreeCleanupCleaned || saved.WorktreeCleanup.OwnershipConfirmedAt == nil {
		t.Fatalf("claimed provisioning session = %#v", saved)
	}
}

func TestDrainWorktreeCleanupDoesNotDeleteUnconfirmedBranchAfterMarkerClaim(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusFailed,
		BaseBranch:     "main",
		WorktreePath:   "/data/worktrees/project-1/session-1",
		WorktreeBranch: "session-1",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status:         domain.WorktreeCleanupPending,
			OwnershipToken: "test-owner-token",
		},
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true}
	ownership := domain.WorktreeOwnership{BranchExists: true, MarkerExists: true, TokenMatches: true}
	worktrees := &fakeWorktreeManager{ownership: &ownership}
	service := New(repo, projects, WithWorktrees(worktrees))

	if _, err := service.DrainWorktreeCleanup(ctx); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 || len(worktrees.releasedOwnership) != 0 {
		t.Fatalf("unconfirmed branch was changed: removed=%#v branches=%#v released=%#v", worktrees.removed, worktrees.deletedBranches, worktrees.releasedOwnership)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || saved.WorktreeCleanup.ErrorCode != "worktree_ownership_unconfirmed" {
		t.Fatalf("unconfirmed branch cleanup = %#v", saved)
	}
}

func TestDrainWorktreeCleanupRejectsOwnershipMismatch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusClosed,
		BaseBranch:     "main",
		WorktreePath:   "/tmp/not-owned",
		WorktreeBranch: "session-1",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status: domain.WorktreeCleanupPending,
		},
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true}
	worktrees := newFakeWorktreeManager()
	service := New(repo, projects, WithWorktrees(worktrees))

	if _, err := service.DrainWorktreeCleanup(ctx); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("ownership mismatch deleted resources: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || saved.WorktreeCleanup.Retryable || saved.WorktreeCleanup.ErrorCode != "worktree_path_mismatch" {
		t.Fatalf("ownership failure = %#v", saved)
	}
}

func TestDrainWorktreeCleanupRejectsLiveSessionState(t *testing.T) {
	ctx := context.Background()
	confirmedAt := time.Unix(35, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusCreated,
		BaseBranch:     "main",
		WorktreePath:   "/data/worktrees/project-1/session-1",
		WorktreeBranch: "session-1",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status:               domain.WorktreeCleanupPending,
			OwnershipToken:       "test-owner-token",
			OwnershipConfirmedAt: &confirmedAt,
		},
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true}
	worktrees := newFakeWorktreeManager()
	service := New(repo, projects, WithWorktrees(worktrees))

	if _, err := service.DrainWorktreeCleanup(ctx); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("live session resources were deleted: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || saved.WorktreeCleanup.ErrorCode != "worktree_cleanup_state_invalid" {
		t.Fatalf("live session cleanup failure = %#v", saved)
	}
}

func TestRetryWorktreeCleanupQueuesFailedCleanup(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusClosed,
		WorktreeCleanup: domain.WorktreeCleanup{
			Status:    domain.WorktreeCleanupFailed,
			Retryable: true,
		},
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events))
	service.now = func() time.Time { return time.Unix(37, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-cleanup", nil }

	got, err := service.RetryWorktreeCleanup(ctx, "session-1")
	if err != nil {
		t.Fatalf("RetryWorktreeCleanup() error = %v", err)
	}
	if got.WorktreeCleanup.Status != domain.WorktreeCleanupPending || repo.sessions["session-1"].WorktreeCleanup.NextAt == nil {
		t.Fatalf("retried cleanup = %#v", got)
	}
	gotEvents := events.snapshot()
	if len(gotEvents) != 1 || gotEvents[0].Type != "session.worktree_cleanup_requested" {
		t.Fatalf("cleanup events = %#v", gotEvents)
	}
	cleanup, cleanupOK := gotEvents[0].Payload["worktreeCleanup"].(WorktreeCleanupDTO)
	actions, actionsOK := gotEvents[0].Payload["availableActions"].([]string)
	if !cleanupOK || cleanup.Status != domain.WorktreeCleanupPending || !actionsOK || len(actions) != 0 || gotEvents[0].Payload["updatedAt"] != time.Unix(37, 0).UTC() {
		t.Fatalf("cleanup event payload = %#v", gotEvents[0].Payload)
	}
}

func TestStartSessionRejectsPendingWorktreeCleanup(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusFailed,
		BaseBranch:     "main",
		WorktreePath:   "/data/worktrees/project-1/session-1",
		WorktreeBranch: "session-1",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status: domain.WorktreeCleanupPending,
		},
	}
	service := New(repo, newFakeProjectRepository("project-1"))

	_, err := service.ExecuteSession(context.Background(), "session-1")
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeWorktreeFailed {
		t.Fatalf("ExecuteSession() error = %#v", err)
	}
	if actions := availableActions(repo.sessions["session-1"]); !slices.Equal(actions, []string{"close"}) {
		t.Fatalf("availableActions() = %#v", actions)
	}
}

func TestCloseSessionRejectsExecutionClaimBeforeCleanupRequestCommit(t *testing.T) {
	ctx := context.Background()
	databaseURL := filepath.Join(t.TempDir(), "anycode.db")
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	contender, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("open contender store: %v", err)
	}
	defer contender.Close()

	now := time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC)
	confirmedAt := now
	saved := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		BaseBranch: "main", WorktreePath: "/data/worktrees/project-1/session-1", WorktreeBranch: "session-1",
		WorktreeCleanup: domain.WorktreeCleanup{
			Status:               domain.WorktreeCleanupActive,
			OwnershipToken:       "test-owner-token",
			OwnershipConfirmedAt: &confirmedAt,
		},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Sessions().Save(ctx, saved); err != nil {
		t.Fatalf("save session: %v", err)
	}
	expected, err := store.Sessions().Find(ctx, saved.ID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}
	starting := expected
	if err := starting.TransitionTo(domain.StatusStarting, now.Add(time.Second)); err != nil {
		t.Fatalf("transition starting: %v", err)
	}

	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID: "project-1", Path: projectdomain.ProjectPath{Value: "/workspace/project-1"}, IsGit: true,
	}
	worktrees := newFakeWorktreeManager()
	var claim port.ExecutionClaimResult
	var claimErr error
	questions := &fakeQuestionCanceller{onCancel: func() {
		claim, claimErr = contender.ClaimExecution(ctx, port.ExecutionClaimInput{
			ExpectedSession: expected,
			StartingSession: starting,
			Run: processdomain.Run{
				ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusStarting, StartedAt: now.Add(time.Second),
			},
		})
	}}
	service := New(store.Sessions(), projects,
		WithProcesses(store.Processes(), &fakeCodexProcess{}),
		WithEvents(store.Events()),
		WithQuestions(questions),
		WithUnitOfWork(store),
		WithWorktrees(worktrees),
	)
	service.now = func() time.Time { return now.Add(2 * time.Second) }

	closed, err := service.CloseSession(ctx, CloseSessionInput{SessionID: saved.ID})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if claimErr != nil {
		t.Fatalf("ClaimExecution() error = %v", claimErr)
	}
	if claim.Status != port.ExecutionStale {
		t.Fatalf("ClaimExecution() status = %q, want %q", claim.Status, port.ExecutionStale)
	}
	if _, found, err := store.Processes().FindActiveBySession(ctx, "session-1"); err != nil || found {
		t.Fatalf("active process after close = %v, %v", found, err)
	}
	if closed.Status != domain.StatusClosed || closed.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("closed session = %#v", closed)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("close performed git cleanup: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
}

func TestCloseSessionReleasesPreparationWhenCleanupRequestSaveFails(t *testing.T) {
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
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusClosed {
			if session.WorktreePath == "" || session.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
				t.Fatalf("final save session = %#v", session)
			}
			return errors.New("save cleanup request failed")
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
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected final save error")
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("git cleanup ran before persistence: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreePath != "/data/worktrees/project-1/session-1" || saved.Status != domain.StatusStopped || saved.CloseReason != nil {
		t.Fatalf("session should release the close preparation after persistence failure: %#v", saved)
	}
}

func TestCloseSessionKeepsCommittedCleanupRequestWhenEventAppendFails(t *testing.T) {
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
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := newFakeWorktreeManager()
	events := &fakeEventStore{appendErrs: []error{nil, nil, errors.New("append closed event failed")}}
	service := New(repo, projects, WithWorktrees(worktrees), WithEvents(events))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected event append error")
	}
	saved := repo.sessions["session-1"]
	if saved.Status != domain.StatusClosed || saved.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("committed close was released = %#v", saved)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("close performed git cleanup: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}

	processed, err := service.DrainWorktreeCleanup(ctx)
	if err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if processed != 1 || repo.sessions["session-1"].WorktreeCleanup.Status != domain.WorktreeCleanupCleaned {
		t.Fatalf("cleanup result = processed:%d session:%#v", processed, repo.sessions["session-1"])
	}
}

func TestCloseSessionRetryPersistsSingleCleanupRequestAfterSaveFailure(t *testing.T) {
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
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusClosed {
			return errors.New("save closed session failed")
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
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected first final save error")
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
	if got.WorktreePath == "" || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() retry = %#v", got)
	}
	if len(worktrees.removed) != 0 {
		t.Fatalf("retry should not remove missing worktree again: %#v", worktrees.removed)
	}
	if len(worktrees.deletedBranches) != 0 {
		t.Fatalf("retry performed branch cleanup = %#v", worktrees.deletedBranches)
	}
	savedAfterRetry := repo.sessions["session-1"]
	if savedAfterRetry.Status != domain.StatusClosed {
		t.Fatalf("saved status after retry = %q", savedAfterRetry.Status)
	}
	if savedAfterRetry.WorktreePath == "" || savedAfterRetry.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("saved cleanup after retry = %#v", savedAfterRetry)
	}
}

func TestCloseSessionClosesWhenWorktreeIsMissing(t *testing.T) {
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
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	worktrees := newFakeWorktreeManager()
	worktrees.setMissing("/data/worktrees/project-1/session-1", true)
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreePath == "" || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", got)
	}
	if len(worktrees.removed) != 0 {
		t.Fatalf("missing worktree should not remove again: %#v", worktrees.removed)
	}
	if len(worktrees.deletedBranches) != 0 {
		t.Fatalf("close deleted missing worktree branch = %#v", worktrees.deletedBranches)
	}
	saved := repo.sessions["session-1"]
	if saved.WorktreePath == "" || saved.WorktreeBaseCommit != "base" || saved.Status != domain.StatusClosed || saved.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("saved session = %#v", saved)
	}
}

func TestCloseSessionClosesWhenMissingWorktreeHasNoBaseCommit(t *testing.T) {
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
	worktrees := newFakeWorktreeManager()
	worktrees.setMissing("/data/worktrees/project-1/session-1", true)
	service := New(repo, projects, WithWorktrees(worktrees))

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreePath == "" || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", got)
	}
}

func TestCloseSessionDoesNotRunFailingWorktreeRemoval(t *testing.T) {
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
	worktrees := &fakeWorktreeManager{removeErr: errors.New("remove failed")}
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", got)
	}
	if len(worktrees.removed) != 0 {
		t.Fatalf("close attempted removal = %#v", worktrees.removed)
	}
}

func TestCloseSessionRecordsBranchDeleteFailureAndStillCloses(t *testing.T) {
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
	events := &fakeEventStore{}
	service := New(repo, projects, WithWorktrees(worktrees), WithEvents(events))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", got)
	}
	if _, err := service.DrainWorktreeCleanup(ctx); err != nil {
		t.Fatalf("DrainWorktreeCleanup() error = %v", err)
	}
	if !slices.Equal(worktrees.removed, []string{"/data/worktrees/project-1/session-1"}) {
		t.Fatalf("removed worktrees = %#v", worktrees.removed)
	}
	if !slices.Equal(worktrees.deletedBranches, []string{"/workspace/project-1:session-1"}) {
		t.Fatalf("deleted branches = %#v", worktrees.deletedBranches)
	}
	gotEvents := events.snapshot()
	requireSessionEventTypes(t, gotEvents,
		"session.closing", sessionStatusUpdatedEvent,
		"session.closed", sessionStatusUpdatedEvent,
		"session.worktree_cleanup_requested", "session.worktree_cleanup_failed",
	)
	if gotEvents[5].Payload["code"] != "worktree_branch_delete_failed" {
		t.Fatalf("branch cleanup error = %#v", gotEvents[5].Payload)
	}
	if saved := repo.sessions["session-1"]; saved.WorktreeCleanup.Status != domain.WorktreeCleanupFailed || !saved.WorktreeCleanup.Retryable {
		t.Fatalf("saved cleanup failure = %#v", saved)
	}
}

func TestCloseSessionDoesNotRequireBaseCommit(t *testing.T) {
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
	worktrees := newFakeWorktreeManager()
	service := New(repo, projects, WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreePath == "" || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", got)
	}
}

func TestCloseSessionPersistsCleanupWithoutWorktreeManager(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusCreated,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", got)
	}
}

func TestCloseSessionCancelsPendingQuestionsBeforeSavingClosed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingUser,
	}
	questions := &fakeQuestionCanceller{}
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusClosed && questions.cancelledSessionID != "session-1" {
			t.Fatalf("Save() called before pending questions were cancelled")
		}
		return nil
	}
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
		t.Fatalf("saved session = %#v", repo.sessions["session-1"])
	}
}

func TestCloseSessionStopsBeforeCleanupWhenQuestionCancellationFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusWaitingUser,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{
		ID:    "project-1",
		Path:  projectdomain.ProjectPath{Value: "/workspace/project-1"},
		IsGit: true,
	}
	questions := &fakeQuestionCanceller{cancelErr: errors.New("cancel failed")}
	worktrees := newFakeWorktreeManager()
	service := New(repo, projects, WithQuestions(questions), WithWorktrees(worktrees))

	_, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeAnswerUserCancelled || !appErr.Retryable {
		t.Fatalf("CloseSession() error = %#v", err)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("cleanup ran after cancellation failure: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
	if len(repo.saved) != 2 || repo.sessions["session-1"].Status != domain.StatusStopped {
		t.Fatalf("session should remain stopped after cancellation failure: %#v", repo.sessions["session-1"])
	}
}

func TestCloseSessionClosesWaitingSessionBeforeCleanupRuns(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusWaitingUser,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", IsGit: true}
	questions := &fakeQuestionCanceller{}
	worktrees := &fakeWorktreeManager{removeErr: errors.New("remove failed")}
	service := New(repo, projects, WithQuestions(questions), WithWorktrees(worktrees))

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if questions.cancelledSessionID != "session-1" {
		t.Fatalf("pending questions were not cancelled: %#v", questions)
	}
	if got.Status != domain.StatusClosed || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending || len(worktrees.removed) != 0 {
		t.Fatalf("session before asynchronous cleanup = %#v removed=%#v", got, worktrees.removed)
	}
}

func TestCloseSessionKeepsWaitingSessionStoppedWhenFinalSaveFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusWaitingUser,
	}
	saveCalls := 0
	repo.saveHook = func(domain.Session) error {
		saveCalls++
		if saveCalls == 2 {
			return errors.New("save closed failed")
		}
		return nil
	}
	questions := &fakeQuestionCanceller{}
	service := New(repo, newFakeProjectRepository("project-1"), WithQuestions(questions))

	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err == nil {
		t.Fatal("CloseSession() expected final save error")
	}
	if questions.cancelledSessionID != "session-1" {
		t.Fatalf("pending questions were not cancelled: %#v", questions)
	}
	if saved := repo.sessions["session-1"]; saved.Status != domain.StatusStopped {
		t.Fatalf("session after final save failure = %#v", saved)
	}
}

func TestCloseSessionWritesClosedAndCleanupRequestedEvents(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Status:       domain.StatusCompleted,
		BaseBranch:   "main",
		WorktreePath: "/data/worktrees/project-1/session-1",
	}
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", IsGit: true}
	worktrees := &fakeWorktreeManager{}
	events := &fakeEventStore{}
	service := New(repo, projects, WithWorktrees(worktrees), WithEvents(events))
	service.now = func() time.Time { return time.Unix(30, 0).UTC() }
	ids := []domain.ID{"event-closing", "event-closed", "event-cleanup-requested"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1", Reason: domain.CloseReasonMergedClosed})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreePath == "" || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", got)
	}
	if len(worktrees.removed) != 0 {
		t.Fatalf("close removed worktree = %#v", worktrees.removed)
	}
	gotEvents := events.snapshot()
	requireSessionEventTypes(t, gotEvents,
		"session.closing", sessionStatusUpdatedEvent,
		"session.closed", sessionStatusUpdatedEvent,
		"session.worktree_cleanup_requested",
	)
	if gotEvents[2].Payload["reason"] != string(domain.CloseReasonMergedClosed) {
		t.Fatalf("closed event = %#v", gotEvents[2])
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

func TestCloseSessionStopsActiveProcessFromFailedStateBeforeCleanup(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Status: domain.StatusFailed,
		BaseBranch: "main", WorktreePath: "/data/worktrees/project-1/session-1",
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	projects := newFakeProjectRepository()
	projects.projects["project-1"] = projectdomain.Project{ID: "project-1", IsGit: true}
	worktrees := newFakeWorktreeManager()
	processes.markExitedDoneHook = func() {
		if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
			t.Fatalf("worktree cleanup ran before process exit: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
		}
	}
	service := New(repo, projects, WithProcesses(processes, &fakeCodexProcess{}), WithWorktrees(worktrees))
	service.now = func() time.Time { return time.Unix(31, 0).UTC() }

	got, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if got.Status != domain.StatusClosed || got.WorktreeCleanup.Status != domain.WorktreeCleanupPending {
		t.Fatalf("CloseSession() = %#v", got)
	}
	if processes.exitedID != "process-run-1" {
		t.Fatalf("process exit id = %q", processes.exitedID)
	}
	if len(worktrees.removed) != 0 || len(worktrees.deletedBranches) != 0 {
		t.Fatalf("close performed git cleanup: removed=%#v branches=%#v", worktrees.removed, worktrees.deletedBranches)
	}
}

func TestCloseRequiresStopForActiveProcessRegardlessOfSessionStatus(t *testing.T) {
	ctx := context.Background()
	for _, status := range []domain.Status{
		domain.StatusFailed,
		domain.StatusBlocked,
		domain.StatusQueued,
		domain.StatusRunning,
	} {
		t.Run(string(status), func(t *testing.T) {
			processes := newFakeProcessRepository()
			processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
			processes.hasActive = true
			service := New(newFakeRepository(), newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}))

			requiresStop, err := closeRequiresStop(ctx, service, domain.Session{ID: "session-1", Status: status})
			if err != nil {
				t.Fatalf("closeRequiresStop() error = %v", err)
			}
			if !requiresStop {
				t.Fatal("closeRequiresStop() = false, want true")
			}
		})
	}
}

func TestCloseFailureAfterStopDoesNotRedeliverAcknowledgedPrompt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		BaseBranch: "main", WorktreePath: "/workspace/session-1", CodexSessionID: "codex-session-1",
	}
	repo.appends = []domain.PromptAppend{{
		ID: "append-old", SessionID: "session-1", Body: "already delivered", Status: domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusClosed {
			return errors.New("save closed failed")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	pid := 1234
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: &pid}
	processes.hasActive = true
	stream := make(chan processdomain.CodexEvent, 1)
	resumedStream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		eventStreams: []<-chan processdomain.CodexEvent{stream, resumedStream},
		resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"},
	}
	events := &fakeEventStore{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, codex),
		WithEvents(events),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}
	service.queueDrainScheduler = func(*Service) {}
	service.consumeCodexEvents(
		processdomain.CodexHandle{ProcessRunID: "process-run-1", PID: pid, CodexSessionID: "codex-session-1"},
		repo.sessions["session-1"],
		codexStartOptions{},
		"/workspace/session-1",
	)
	consumerDone, ok := service.processConsumerDone("process-run-1")
	if !ok {
		t.Fatal("process consumer was not registered")
	}

	closeDone := make(chan error, 1)
	go func() {
		_, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"})
		closeDone <- err
	}()
	waitForEventType(t, events, "session.stopping")
	stream <- processdomain.CodexEvent{Type: processdomain.CodexEventStatus, Content: processdomain.CodexStatusContent{Code: "task.started"}, CreatedAt: time.Unix(41, 0).UTC()}
	close(stream)
	if err := <-closeDone; err == nil {
		t.Fatal("CloseSession() error = nil, want missing worktree manager error")
	}
	select {
	case <-consumerDone:
	case <-time.After(time.Second):
		t.Fatal("process consumer did not finish")
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusStopped {
		t.Fatalf("session status = %q", got)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendDispatched {
		t.Fatalf("old prompt append = %#v", promptAppend)
	}
	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "new instruction"}); err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if started, err := service.DrainQueuedSessions(ctx); err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	resumedConsumerDone, ok := service.processConsumerDone(codex.resumeInput.ProcessRunID)
	if !ok {
		t.Fatal("resumed process consumer was not registered")
	}
	if codex.resumeInput.Prompt != "new instruction" {
		t.Fatalf("resume prompt = %q", codex.resumeInput.Prompt)
	}
	close(resumedStream)
	select {
	case <-resumedConsumerDone:
	case <-time.After(time.Second):
		t.Fatal("resumed process consumer did not finish")
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

func TestStartSessionCreatesProcessRunAndKeepsStartingUntilTranscript(t *testing.T) {
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
			FastMode:        true,
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
	if got.Status != domain.StatusStarting {
		t.Fatalf("StartSession() status = %q", got.Status)
	}
	if len(processes.created) != 1 || processes.created[0].ID != "process-run-1" || processes.created[0].Status != processdomain.StatusStarting {
		t.Fatalf("created process runs = %#v", processes.created)
	}
	if processes.active.Status != processdomain.StatusStarting || processes.active.PID == nil || *processes.active.PID != 1234 {
		t.Fatalf("starting process = %#v", processes.active)
	}
	if codex.startInput.ProcessRunID != "process-run-1" || codex.startInput.Workdir != "/workspace/session-1" || codex.startInput.Prompt != promptWithAnyCodeGuidance("implement session", repo.sessions["session-1"]) || !codex.startInput.FastMode {
		t.Fatalf("codex start input = %#v", codex.startInput)
	}
	if repo.sessions["session-1"].Status != domain.StatusStarting {
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
	if !strings.Contains(prompt, "`update_plan`") {
		t.Fatalf("prompt missing structured TODO tool guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "不要只在回复中输出 Markdown checklist") {
		t.Fatalf("prompt should distinguish structured TODO updates from Markdown: %q", prompt)
	}
	if strings.Contains(prompt, managedWorktreePromptGuidance) {
		t.Fatalf("non-git session prompt should not include worktree guidance: %q", prompt)
	}
}

func TestStartSessionPromptProtectsManagedWorktree(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "Implement the requested change",
		Mode:         domain.ModeChat,
		Status:       domain.StatusCreated,
		BaseBranch:   "main",
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
	if !strings.Contains(prompt, "不得删除、移动、重建或清理当前工作树") {
		t.Fatalf("prompt missing worktree lifecycle guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "卡片关闭时由 AnyCode") {
		t.Fatalf("prompt should assign cleanup ownership to AnyCode: %q", prompt)
	}
	if !strings.Contains(prompt, "当前卡片分支名执行非 fast-forward merge") || !strings.Contains(prompt, "保留 Git 默认合并提交信息") {
		t.Fatalf("prompt missing recoverable manual merge guidance: %q", prompt)
	}
	if strings.Contains(prompt, "保存 Diff 快照") {
		t.Fatalf("prompt should not promise Diff snapshots: %q", prompt)
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
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"}, events: stream}
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithEventPublisher(publisher))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{"process-run-1", "event-starting", "event-transcript-bound", "event-running"}
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
	requireSessionEventTypes(t, got, "session.starting", sessionStatusUpdatedEvent)
	if got[0].ID != "event-starting" || got[0].Type != "session.starting" || got[0].Scope.ProjectID != "project-1" {
		t.Fatalf("starting event = %#v", got[0])
	}
	if got[0].SessionID == nil || *got[0].SessionID != "session-1" || got[0].Payload["processRunId"] != "process-run-1" || got[0].Causality.ProcessRunID != "process-run-1" || got[0].Causality.SessionStatus != "starting" {
		t.Fatalf("starting payload/scope = %#v", got[0])
	}
	stream <- transcriptReadyEvent("codex-session-1")
	waitForEventType(t, events, "session.running")
	got = events.snapshot()
	if len(got) != 5 || got[2].ID != "event-transcript-bound" || got[2].Type != "process.transcript_bound" || got[3].ID != "event-running" || got[3].Type != "session.running" || got[4].Type != sessionStatusUpdatedEvent {
		t.Fatalf("bound/running events = %#v", got)
	}
	if got[3].Payload["pid"] != 1234 || got[3].Payload["codexSessionId"] != "codex-session-1" || got[3].Causality.SessionStatus != "running" {
		t.Fatalf("running payload = %#v", got[3].Payload)
	}
	published := publisher.snapshot()
	if len(published) != 5 || published[0].ID != "event-starting" || published[2].ID != "event-transcript-bound" || published[3].ID != "event-running" {
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
	files := newFakeAttachmentStore()
	files.sessionAttachments["attachment-image"] = domain.SessionAttachment{
		ID:        "attachment-image",
		SessionID: "session-1",
		Role:      domain.FileRoleInput,
		Path:      "/data/attachments/sessions/session-1/screenshot.png",
		MimeType:  "image/png",
	}
	files.sessionAttachments["attachment-note"] = domain.SessionAttachment{
		ID:        "attachment-note",
		SessionID: "session-1",
		Role:      domain.FileRoleInput,
		Path:      "/data/attachments/sessions/session-1/notes.md",
		MimeType:  "text/markdown",
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files), WithProcesses(processes, codex))
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
	if first.Status != domain.StatusStarting {
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
	if first.Status != domain.StatusStarting {
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
		Type:      processdomain.CodexEventUnknown,
		CreatedAt: time.Unix(39, 0).UTC(),
		Content: processdomain.CodexUnknownContent{RawType: "assistant_message", Payload: map[string]any{
			"authorization": "Bearer secret",
			"message":       map[string]any{"role": "assistant", "content": "hello"},
			"workdir":       "/home/nzlov/workspaces/github/project",
		}},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"}, events: source}
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithEventPublisher(publisher))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-transcript-bound",
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
	got := waitForPublishedCodexEventType(t, publisher, processdomain.CodexEventUnknown)
	if got.EventID != "codex-event-1" || got.SessionID != "session-1" || got.ProcessRunID != "process-run-1" || got.CodexSessionID != "codex-session-1" {
		t.Fatalf("codex event identity = %#v", got)
	}
	content := eventContent[processdomain.CodexUnknownContent](t, got)
	if content.RawType != "assistant_message" {
		t.Fatalf("codex event content = %#v", content)
	}
	if !got.CreatedAt.Equal(time.Unix(39, 0).UTC()) {
		t.Fatalf("codex event created at = %s", got.CreatedAt)
	}
	if content.Payload["authorization"] != "Bearer secret" || content.Payload["workdir"] != "/home/nzlov/workspaces/github/project" {
		t.Fatalf("codex event payload was changed: %#v", content.Payload)
	}
	message, ok := content.Payload["message"].(map[string]any)
	if !ok || message["role"] != "assistant" || message["content"] != "hello" {
		t.Fatalf("message payload = %#v", content.Payload["message"])
	}
	for _, event := range events.snapshot() {
		if event.Type == "process.codex_event" {
			t.Fatalf("codex transcript event should not be stored: %#v", event)
		}
	}
}

func TestTranscriptBindFailureSettlesAfterStopFailure(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Requirement: "implement session",
		Status: domain.StatusCreated, WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	processes.bindErr = errors.New("bind unavailable")
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{EventID: "event-1", Type: "assistant_message", Content: processdomain.CodexMessageContent{Role: "assistant", Text: "done"}}
	close(source)
	codex := &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"},
		events:      source,
		stopErr:     errors.New("stop unavailable"),
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	sequence := 0
	service.generateID = func() (domain.ID, error) {
		sequence++
		return domain.ID(fmt.Sprintf("generated-%d", sequence)), nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	failed := waitForEventType(t, events, "session.failed")
	if repo.sessions["session-1"].Status != domain.StatusFailed || processes.hasActive {
		t.Fatalf("unsettled bind failure: session=%#v process=%#v", repo.sessions["session-1"], processes.active)
	}
	if codex.stoppedID == "" || failed.Payload["failureCode"] != "codex_transcript_unavailable" {
		t.Fatalf("bind failure event = %#v stopped=%q", failed, codex.stoppedID)
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
		EventID:        "codex-thread-new",
		Type:           processdomain.CodexEventTranscriptBound,
		CodexSessionID: "codex-session-new",
		Content: processdomain.CodexTranscriptSource{
			CodexSessionID: "codex-session-new", RelativePath: "test/codex-session-new.jsonl",
		},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	publishedAfterSave := make(chan bool, 1)
	publisher := &fakeEventPublisher{
		onPublish: func(event eventdomain.DomainEvent) {
			if event.Type == "session.running" {
				publishedAfterSave <- repo.sessions["session-1"].CodexSessionID == "codex-session-new"
			}
		},
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithEventPublisher(publisher))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{"process-run-1", "event-starting", "event-transcript-bound", "event-running", "event-process-exited", "event-stopped"}
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
	select {
	case ok := <-publishedAfterSave:
		if !ok {
			t.Fatal("codex event published before CodexSessionID was saved")
		}
	case <-time.After(time.Second):
		t.Fatal("session.running event was not published")
	}
	waitForEventType(t, events, "session.stopped")
	if got := repo.sessions["session-1"].CodexSessionID; got != "codex-session-new" {
		t.Fatalf("CodexSessionID = %q, want new thread id", got)
	}
	if processes.runningCodex != "codex-session-new" {
		t.Fatalf("running codex session id = %q", processes.runningCodex)
	}
}

func TestLateCodexEventAfterStopDoesNotRestoreRunningState(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-session-old", WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	pid := 1234
	processes.active = processdomain.Run{
		ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning,
		PID: &pid, CodexSessionID: "codex-session-old",
	}
	processes.hasActive = true
	stream := make(chan processdomain.CodexEvent, 1)
	codex := &fakeCodexProcess{events: stream}
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, codex),
		WithEvents(events),
		WithEventPublisher(publisher),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	service.consumeCodexEvents(
		processdomain.CodexHandle{ProcessRunID: "process-run-1", PID: pid, CodexSessionID: "codex-session-old"},
		repo.sessions["session-1"],
		codexStartOptions{},
		"/workspace/session-1",
	)

	stopDone := make(chan struct {
		dto DTO
		err error
	}, 1)
	go func() {
		dto, err := service.StopSession(ctx, "session-1")
		stopDone <- struct {
			dto DTO
			err error
		}{dto: dto, err: err}
	}()
	waitForEventType(t, events, "session.stopping")
	stream <- processdomain.CodexEvent{
		EventID:        "late-thread",
		Type:           processdomain.CodexEventTranscriptBound,
		CodexSessionID: "codex-session-new",
		Content:        processdomain.CodexTranscriptSource{CodexSessionID: "codex-session-new", RelativePath: "test/codex-session-new.jsonl"},
	}
	close(stream)
	stopped := <-stopDone
	if stopped.err != nil || stopped.dto.Status != domain.StatusStopped {
		t.Fatalf("StopSession() = %#v, %v", stopped.dto, stopped.err)
	}
	waitForEventType(t, events, "session.stopped")
	if got := repo.sessions["session-1"]; got.Status != domain.StatusStopped || got.CodexSessionID != "codex-session-old" {
		t.Fatalf("session after late event = %#v", got)
	}
	if processes.hasActive || processes.runningCodex == "codex-session-new" {
		t.Fatalf("process after late event active=%v codexSessionID=%q", processes.hasActive, processes.runningCodex)
	}
}

func TestStopWorkflowSessionWaitsForConsumerSettlement(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning,
		WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: "session-1", Body: "not acknowledged", Status: domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	processes := newFakeProcessRepository()
	pid := 1234
	processes.active = processdomain.Run{
		ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: &pid,
	}
	processes.hasActive = true
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{events: stream}
	events := &fakeEventStore{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, codex),
		WithEvents(events),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	nodeRunID := processdomain.NodeRunID("node-run-1")
	service.consumeCodexEvents(
		processdomain.CodexHandle{ProcessRunID: "process-run-1", PID: pid},
		repo.sessions["session-1"],
		codexStartOptions{sessionID: "session-1", nodeRunID: &nodeRunID},
		"/workspace/session-1",
	)

	stopDone := make(chan struct {
		dto DTO
		err error
	}, 1)
	go func() {
		dto, err := service.StopSession(ctx, "session-1")
		stopDone <- struct {
			dto DTO
			err error
		}{dto: dto, err: err}
	}()
	waitForEventType(t, events, "session.stopping")
	close(stream)
	stopped := <-stopDone
	if stopped.err != nil || stopped.dto.Status != domain.StatusStopped {
		t.Fatalf("StopSession() = %#v, %v", stopped.dto, stopped.err)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendPending || promptAppend.DispatchedProcessRunID != "" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
	waitForEventType(t, events, "session.stopped")
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
		Type:    processdomain.CodexEventPlan,
		Content: processdomain.PlanUpdate{Items: []processdomain.PlanItem{
			{Step: "梳理需求", Status: processdomain.PlanItemCompleted},
			{Step: "实现卡片展示", Status: processdomain.PlanItemInProgress},
		}},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-transcript-bound",
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

type fakeSessionDiffCounter struct {
	count int
	err   error
	calls int
}

func (f *fakeSessionDiffCounter) CountSessionChangedFiles(context.Context, domain.ID) (int, error) {
	f.calls++
	return f.count, f.err
}

func TestHasCodexFileChanges(t *testing.T) {
	fileChange := processdomain.CodexFileChangeContent{
		Changes: []processdomain.CodexFileChange{{Kind: "update", Path: "main.go"}},
	}
	tests := []struct {
		name  string
		event processdomain.CodexEvent
		want  bool
	}{
		{name: "standalone file change", event: processdomain.CodexEvent{Type: processdomain.CodexEventFileChange, Phase: processdomain.CodexPhaseStandalone, Content: fileChange}, want: true},
		{name: "completed file change", event: processdomain.CodexEvent{Type: processdomain.CodexEventFileChange, Phase: processdomain.CodexPhaseCompleted, Content: fileChange}, want: true},
		{name: "empty file change", event: processdomain.CodexEvent{Type: processdomain.CodexEventFileChange, Content: processdomain.CodexFileChangeContent{}}},
		{name: "command with file change content", event: processdomain.CodexEvent{Type: processdomain.CodexEventCommand, Phase: processdomain.CodexPhaseCompleted, Content: fileChange}},
		{name: "file change with command content", event: processdomain.CodexEvent{Type: processdomain.CodexEventFileChange, Content: processdomain.CodexCommandContent{}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasCodexFileChanges(test.event); got != test.want {
				t.Fatalf("hasCodexFileChanges() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCodexTaskCompletionReconcilesShellDiffCount(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	counter := &fakeSessionDiffCounter{count: 1}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{}),
		WithEvents(events),
		WithDiffCounter(counter),
	)

	err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{ProcessRunID: "process-run-1"}, processdomain.CodexEvent{
		EventID: "task-completed-1",
		Type:    processdomain.CodexEventStatus,
		Content: processdomain.CodexStatusContent{Code: "task.completed"},
	})
	if err != nil {
		t.Fatalf("handleCodexEvent() error = %v", err)
	}
	if counter.calls != 1 || repo.sessions["session-1"].FilesChanged != 1 {
		t.Fatalf("diff counter calls=%d session=%#v", counter.calls, repo.sessions["session-1"])
	}
	event := waitForEventType(t, events, "session.diff_changed")
	if event.Payload["filesChanged"] != 1 {
		t.Fatalf("session.diff_changed = %#v", event)
	}
}

func TestCodexUsagePersistsBeforePublishingSessionUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	publishedAfterPersist := make(chan bool, 2)
	publisher := &fakeEventPublisher{onPublish: func(event eventdomain.DomainEvent) {
		if event.Type == "session.usage_updated" {
			publishedAfterPersist <- repo.sessions["session-1"].Usage.TotalTokens == 14
		}
	}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{}),
		WithEvents(events),
		WithUnitOfWork(uow),
		WithEventPublisher(publisher),
	)

	if err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{ProcessRunID: "process-run-1"}, processdomain.CodexEvent{
		EventID: "usage-1", Type: processdomain.CodexEventUsage,
		Content: processdomain.CodexUsageContent{
			InputTokens: 10, CachedInputTokens: 4, OutputTokens: 4, TotalTokens: 14,
			CurrentInputTokens: 6, CurrentTotalTokens: 8, ContextWindow: 200,
		},
	}); err != nil {
		t.Fatalf("handleCodexEvent() error = %v", err)
	}
	if got := repo.sessions["session-1"].Usage; got.TotalTokens != 14 || got.CurrentInputTokens != 6 || got.ContextWindow != 200 {
		t.Fatalf("persisted usage = %#v", got)
	}
	select {
	case ok := <-publishedAfterPersist:
		if !ok {
			t.Fatal("usage event published before persistence")
		}
	case <-time.After(time.Second):
		t.Fatal("usage update was not published")
	}

	if err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{ProcessRunID: "process-run-1"}, processdomain.CodexEvent{
		EventID: "compact-1", Type: processdomain.CodexEventStatus,
		Content: processdomain.CodexStatusContent{Code: "context.compacted"},
	}); err != nil {
		t.Fatalf("handle compaction: %v", err)
	}
	if got := repo.sessions["session-1"].Usage.CompactionCount; got != 1 {
		t.Fatalf("compaction count = %d", got)
	}
}

func TestHandleCodexProcessExitReconcilesFinalDiffCount(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	counter := &fakeSessionDiffCounter{count: 1}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, nil),
		WithEvents(events),
		WithDiffCounter(counter),
	)
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.queueDrainScheduler = func(*Service) {}
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}

	service.handleCodexProcessExit(
		repo.sessions["session-1"],
		processdomain.CodexHandle{ProcessRunID: "process-run-1"},
		codexStartOptions{},
		processdomain.ExitResult{FinishedAt: time.Unix(100, 0).UTC()},
		nil,
	)

	if counter.calls != 1 || repo.sessions["session-1"].FilesChanged != 1 {
		t.Fatalf("diff counter calls=%d session=%#v", counter.calls, repo.sessions["session-1"])
	}
	event := waitForEventType(t, events, "session.diff_changed")
	if event.Payload["filesChanged"] != 1 {
		t.Fatalf("session.diff_changed = %#v", event)
	}
}

func TestArtifactDirectoryUpdateCachesCountAndEmitsForEveryActiveChange(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID: "run-1", SessionID: "session-1", Status: processdomain.StatusRunning,
	}
	processes.hasActive = true
	artifacts := &fakeSessionArtifactStore{artifactCount: 2}
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(repo, newFakeProjectRepository("project-1"),
		WithProcesses(processes, nil),
		WithEvents(events),
		WithEventPublisher(publisher),
	)
	service.artifacts = artifacts

	for range 2 {
		if err := service.recordArtifactDirectoryUpdate(ctx, "session-1", "run-1"); err != nil {
			t.Fatalf("recordArtifactDirectoryUpdate() error = %v", err)
		}
	}
	if got := repo.sessions["session-1"].ArtifactCount; got != 2 {
		t.Fatalf("artifact count = %d, want 2", got)
	}
	if repo.updateArtifactCountCalls != 2 {
		t.Fatalf("artifact count updates = %d, want 2", repo.updateArtifactCountCalls)
	}
	stored := events.snapshot()
	if len(stored) != 2 {
		t.Fatalf("artifact events = %#v", stored)
	}
	for _, event := range stored {
		if event.Type != "session.artifacts_updated" || event.Payload["artifactCount"] != 2 || event.Payload["processRunId"] != "run-1" {
			t.Fatalf("artifact event = %#v", event)
		}
	}
	if published := publisher.snapshot(); len(published) != 2 {
		t.Fatalf("published artifact events = %#v", published)
	}
}

func TestArtifactWatcherStartsOnlyForMatchingActiveProcess(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	artifacts := &fakeSessionArtifactStore{artifactCount: 1}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, nil))
	service.artifacts = artifacts

	service.startArtifactWatcher("session-1", "run-1")()
	if artifacts.watchCalls != 0 {
		t.Fatalf("inactive process watcher calls = %d", artifacts.watchCalls)
	}

	processes.active = processdomain.Run{
		ID: "run-2", SessionID: "session-1", Status: processdomain.StatusRunning,
	}
	processes.hasActive = true
	service.startArtifactWatcher("session-1", "run-1")()
	if artifacts.watchCalls != 0 {
		t.Fatalf("stale process watcher calls = %d", artifacts.watchCalls)
	}

	stop := service.startArtifactWatcher("session-1", "run-2")
	if artifacts.watchCalls != 1 {
		t.Fatalf("active process watcher calls = %d, want 1", artifacts.watchCalls)
	}
	stop()
	if repo.sessions["session-1"].ArtifactCount != 1 {
		t.Fatalf("flushed artifact count = %d", repo.sessions["session-1"].ArtifactCount)
	}
}

func TestArtifactWatcherCanStartWhileSessionLockIsHeld(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, nil), WithSessionLocker(NewMemorySessionLocker()))
	service.artifacts = &fakeSessionArtifactStore{artifactCount: 1}

	stopReady := make(chan func())
	go func() {
		_ = service.withSessionLock(context.Background(), "session-1", func(context.Context) error {
			stopReady <- service.startArtifactWatcher("session-1", "run-1")
			return nil
		})
	}()
	var stop func()
	select {
	case stop = <-stopReady:
	case <-time.After(time.Second):
		t.Fatal("artifact watcher re-entered the held session lock")
	}
	stop()
}

func TestDeleteSessionFileUpdatesCountAndPublishesCanonicalEvent(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Status: domain.StatusStopped, ArtifactCount: 1,
	}
	artifacts := &fakeSessionArtifactStore{
		artifactCount: 1,
		artifacts: map[domain.SessionFileID]domain.SessionFile{
			"artifact-1": {ID: "artifact-1", SessionID: "session-1", Role: domain.FileRoleArtifact},
		},
	}
	artifacts.beforeDelete = func() {
		if repo.sessions["session-1"].ArtifactCount != 1 {
			t.Fatalf("artifact count changed before deletion = %d", repo.sessions["session-1"].ArtifactCount)
		}
	}
	service := New(repo, newFakeProjectRepository("project-1"))
	service.artifacts = artifacts
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service.events = events
	service.publisher = publisher

	if err := service.DeleteSessionFile(context.Background(), "artifact-1"); err != nil {
		t.Fatal(err)
	}
	if repo.sessions["session-1"].ArtifactCount != 0 || repo.updateArtifactCountCalls != 1 {
		t.Fatalf("session artifact count = %d, updates = %d", repo.sessions["session-1"].ArtifactCount, repo.updateArtifactCountCalls)
	}
	if _, exists := artifacts.artifacts["artifact-1"]; exists {
		t.Fatal("artifact was not deleted")
	}
	stored := events.snapshot()
	if len(stored) != 1 || stored[0].Type != "session.artifacts_updated" || stored[0].Payload["artifactCount"] != 0 {
		t.Fatalf("stored artifact event = %#v", stored)
	}
	if published := publisher.snapshot(); len(published) != 1 || published[0].Type != "session.artifacts_updated" {
		t.Fatalf("published artifact event = %#v", published)
	}
}

func TestDeleteSessionFileFailureKeepsCountAndPublishesNothing(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Status: domain.StatusStopped, ArtifactCount: 1,
	}
	deleteErr := errors.New("delete artifact failed")
	artifacts := &fakeSessionArtifactStore{
		artifactCount: 1,
		artifacts: map[domain.SessionFileID]domain.SessionFile{
			"artifact-1": {ID: "artifact-1", SessionID: "session-1", Role: domain.FileRoleArtifact},
		},
		deleteErr: deleteErr,
	}
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithEventPublisher(publisher))
	service.artifacts = artifacts

	if err := service.DeleteSessionFile(context.Background(), "artifact-1"); !errors.Is(err, deleteErr) {
		t.Fatalf("DeleteSessionFile() error = %v, want %v", err, deleteErr)
	}
	if repo.sessions["session-1"].ArtifactCount != 1 || repo.updateArtifactCountCalls != 0 {
		t.Fatalf("session artifact count changed after failed deletion: %#v", repo.sessions["session-1"])
	}
	if _, exists := artifacts.artifacts["artifact-1"]; !exists {
		t.Fatal("artifact was removed after failed deletion")
	}
	if len(events.snapshot()) != 0 || len(publisher.snapshot()) != 0 {
		t.Fatalf("failed deletion published events: stored=%#v published=%#v", events.snapshot(), publisher.snapshot())
	}
}

func TestPersistCodexFileChangeUpdatesDiffCount(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning, FilesChanged: 1,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	counter := &fakeSessionDiffCounter{count: 3}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{}),
		WithEvents(events),
		WithDiffCounter(counter),
		WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events}}),
	)

	err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{ProcessRunID: "process-run-1"}, processdomain.CodexEvent{
		EventID: "file-change-1", Type: processdomain.CodexEventFileChange, Phase: processdomain.CodexPhaseStandalone,
		Content:   processdomain.CodexFileChangeContent{Changes: []processdomain.CodexFileChange{{Kind: "update", Path: "main.go"}}},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("handleCodexEvent() error = %v", err)
	}
	if counter.calls != 1 || repo.updateFilesChangedCalls != 1 || repo.sessions["session-1"].FilesChanged != 3 {
		t.Fatalf("diff counter calls=%d update calls=%d session=%#v", counter.calls, repo.updateFilesChangedCalls, repo.sessions["session-1"])
	}
	if event := waitForEventType(t, events, "session.diff_changed"); event.Payload["filesChanged"] != 3 {
		t.Fatalf("session.diff_changed = %#v", event)
	}
}

func TestPersistCodexFileChangeKeepsDiffCountOnFailure(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning, FilesChanged: 2,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	counter := &fakeSessionDiffCounter{err: errors.New("diff unavailable")}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithEvents(events), WithDiffCounter(counter))

	err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{ProcessRunID: "process-run-1"}, processdomain.CodexEvent{
		EventID: "file-change-1", Type: processdomain.CodexEventFileChange, Phase: processdomain.CodexPhaseStandalone,
		Content:   processdomain.CodexFileChangeContent{Changes: []processdomain.CodexFileChange{{Kind: "update", Path: "main.go"}}},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("handleCodexEvent() error = %v", err)
	}
	if counter.calls != 1 || repo.sessions["session-1"].FilesChanged != 2 {
		t.Fatalf("diff counter calls=%d session=%#v", counter.calls, repo.sessions["session-1"])
	}
	for _, event := range events.snapshot() {
		if event.Type == "session.diff_changed" {
			t.Fatalf("unexpected diff event = %#v", event)
		}
	}
}

func TestPersistCodexFileChangeRejectsNegativeDiffCount(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning, FilesChanged: 2,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	counter := &fakeSessionDiffCounter{count: -1}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithEvents(events), WithDiffCounter(counter))

	err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{ProcessRunID: "process-run-1"}, processdomain.CodexEvent{
		EventID: "file-change-1", Type: processdomain.CodexEventFileChange, Phase: processdomain.CodexPhaseStandalone,
		Content:   processdomain.CodexFileChangeContent{Changes: []processdomain.CodexFileChange{{Kind: "update", Path: "main.go"}}},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("handleCodexEvent() error = %v", err)
	}
	if counter.calls != 1 || repo.updateFilesChangedCalls != 0 || repo.sessions["session-1"].FilesChanged != 2 {
		t.Fatalf("diff counter calls=%d update calls=%d session=%#v", counter.calls, repo.updateFilesChangedCalls, repo.sessions["session-1"])
	}
	for _, event := range events.snapshot() {
		if event.Type == "session.diff_changed" {
			t.Fatalf("unexpected diff event = %#v", event)
		}
	}
}

func TestStartSessionIgnoresDuplicateTypedPlanUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
	}
	update := processdomain.PlanUpdate{Items: []processdomain.PlanItem{
		{Step: "Persist TODO", Status: processdomain.PlanItemInProgress},
	}}
	source := make(chan processdomain.CodexEvent, 2)
	source <- processdomain.CodexEvent{EventID: "plan-1", Type: processdomain.CodexEventPlan, Content: update}
	source <- processdomain.CodexEvent{EventID: "plan-1", Type: processdomain.CodexEventPlan, Content: update}
	close(source)
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 1234},
		events:      source,
	}), WithEvents(events), WithEventPublisher(publisher))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{"process-run-1", "event-starting", "event-transcript-bound", "event-running", "event-todo", "event-exited", "event-stopped"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	waitForEventType(t, events, "process.exited")
	count := 0
	for _, event := range events.snapshot() {
		if event.Type == "session.todo_list_updated" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("todo list event count = %d, want 1: %#v", count, events.snapshot())
	}
	publishedPlans := 0
	for _, event := range publisher.codexSnapshot() {
		if event.Type == processdomain.CodexEventPlan {
			publishedPlans++
		}
	}
	if publishedPlans != 0 {
		t.Fatalf("plan updates leaked into runtime transcript = %#v", publisher.codexSnapshot())
	}
}

func TestStartSessionIgnoresPlanUpdateMatchingPersistedTodoList(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusCreated,
		WorktreePath: "/workspace/session-1",
		TodoList: domain.TodoList{Items: []domain.TodoItem{
			{Text: "Persist TODO", Completed: false},
		}},
	}
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{
		EventID: "plan-resume",
		Type:    processdomain.CodexEventPlan,
		Content: processdomain.PlanUpdate{Items: []processdomain.PlanItem{
			{Step: "Persist TODO", Status: processdomain.PlanItemInProgress},
		}},
	}
	close(source)
	events := &fakeEventStore{}
	publisher := &fakeEventPublisher{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"},
		events:      source,
	}), WithEvents(events), WithEventPublisher(publisher))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{"process-run-1", "event-starting", "event-transcript-bound", "event-running", "event-exited", "event-stopped"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	waitForEventType(t, events, "process.exited")
	for _, event := range events.snapshot() {
		if event.Type == "session.todo_list_updated" {
			t.Fatalf("matching persisted TODO was published again: %#v", events.snapshot())
		}
	}
	for _, event := range publisher.codexSnapshot() {
		if event.Type == processdomain.CodexEventPlan {
			t.Fatalf("matching TODO leaked into runtime transcript: %#v", event)
		}
	}
}

func TestConsumeCodexFileChangePublishesRuntimeBeforeDiffCountChange(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning, FilesChanged: 1,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	counter := &fakeSessionDiffCounter{count: 3}
	events := &fakeEventStore{}
	sequence := make(chan string, 2)
	publisher := &fakeEventPublisher{
		onPublishCodex: func(event processdomain.CodexEvent) {
			if event.Type == processdomain.CodexEventFileChange {
				sequence <- fmt.Sprintf("runtime:%d", counter.calls)
			}
		},
		onPublish: func(event eventdomain.DomainEvent) {
			if event.Type == "session.diff_changed" {
				sequence <- fmt.Sprintf("diff:%d", counter.calls)
			}
		},
	}
	source := make(chan processdomain.CodexEvent, 1)
	source <- processdomain.CodexEvent{
		EventID: "file-change-1", Type: processdomain.CodexEventFileChange,
		Phase: processdomain.CodexPhaseStandalone,
		Content: processdomain.CodexFileChangeContent{
			Changes: []processdomain.CodexFileChange{{Kind: "modified", Path: "main.go"}},
		},
	}
	codex := &fakeCodexProcess{events: source}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, codex),
		WithEvents(events),
		WithEventPublisher(publisher),
		WithDiffCounter(counter),
	)
	service.consumeCodexEvents(
		processdomain.CodexHandle{ProcessRunID: "process-run-1"},
		repo.sessions["session-1"],
		codexStartOptions{},
		"",
	)

	for index, want := range []string{"runtime:0", "diff:1"} {
		select {
		case got := <-sequence:
			if got != want {
				t.Fatalf("notification[%d] = %q, want %q", index, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("notification[%d] = timeout, want %q; events=%#v", index, want, events.snapshot())
		}
	}
	if got := repo.sessions["session-1"].FilesChanged; got != 3 {
		t.Fatalf("FilesChanged = %d, want 3", got)
	}
	close(source)
	if done, ok := service.processConsumerDone("process-run-1"); ok {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Codex event consumer did not stop")
		}
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
		Type:    processdomain.CodexEventPlan,
		Content: processdomain.PlanUpdate{Items: []processdomain.PlanItem{
			{Step: "Explore project context for Git/worktree creation", Status: processdomain.PlanItemPending},
			{Step: "Clarify intended conflict policy", Status: processdomain.PlanItemPending},
			{Step: "Compare approaches and recommend design", Status: processdomain.PlanItemPending},
		}},
	}
	source <- processdomain.CodexEvent{
		EventID: "codex-event-todo-updated",
		Type:    processdomain.CodexEventPlan,
		Content: processdomain.PlanUpdate{Items: []processdomain.PlanItem{
			{Step: "Explore project context for Git/worktree creation", Status: processdomain.PlanItemCompleted},
			{Step: "Clarify intended conflict policy", Status: processdomain.PlanItemPending},
			{Step: "Compare approaches and recommend design", Status: processdomain.PlanItemPending},
			{Step: "Implement chosen design", Status: processdomain.PlanItemPending},
		}},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-transcript-bound",
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
	startedTodo, ok := todoEvents[0].Payload["todoList"].(domain.TodoList)
	if !ok || startedTodo.Total() != 3 || startedTodo.Items[0].Text != "Explore project context for Git/worktree creation" {
		t.Fatalf("started todo list payload = %#v", todoEvents[0].Payload["todoList"])
	}
	updatedTodo, ok := todoEvents[1].Payload["todoList"].(domain.TodoList)
	if !ok || updatedTodo.Total() != 4 || updatedTodo.Completed() != 1 {
		t.Fatalf("updated todo list payload = %#v", todoEvents[1].Payload["todoList"])
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

func TestTodoListFromCodexEventMapsTypedPlanUpdate(t *testing.T) {
	got, ok := todoListFromCodexEvent(processdomain.CodexEvent{Type: processdomain.CodexEventPlan, Content: processdomain.PlanUpdate{Items: []processdomain.PlanItem{
		{Step: "梳理事件流", Status: processdomain.PlanItemCompleted},
		{Step: "落库 TODO", Status: processdomain.PlanItemInProgress},
	}}})
	if !ok {
		t.Fatal("todoListFromCodexEvent() did not find typed plan")
	}
	if got.Total() != 2 || got.Completed() != 1 {
		t.Fatalf("todo list counts = %d/%d, want 1/2: %#v", got.Completed(), got.Total(), got)
	}
}

func TestTodoListFromCodexEventIgnoresUnrelatedPlanPayloads(t *testing.T) {
	tests := []processdomain.CodexEvent{
		{
			Type: processdomain.CodexEventUnknown,
			Content: processdomain.CodexUnknownContent{RawType: "assistant_message", Payload: map[string]any{
				"plan": []any{
					map[string]any{"step": "不应覆盖", "status": "completed"},
				},
			}},
		},
		{
			Type:    processdomain.CodexEventTool,
			Content: processdomain.CodexToolContent{QualifiedName: "write_file", Input: processdomain.CodexStructuredText{Text: `{"plan":[]}`}},
		},
		{
			Type:    processdomain.CodexEventMessage,
			Content: processdomain.CodexMessageContent{Role: "assistant", Text: "todo_list"},
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
		Type:    processdomain.CodexEventPlan,
		Content: processdomain.PlanUpdate{Items: []processdomain.PlanItem{}},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234}, events: source}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	service.now = func() time.Time { return time.Unix(40, 0).UTC() }
	ids := []domain.ID{
		"process-run-1",
		"event-starting",
		"event-transcript-bound",
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
	if todo, ok := gotEvent.Payload["todoList"].(domain.TodoList); !ok || todo.Total() != 0 {
		t.Fatalf("todo list event full payload = %#v", gotEvent.Payload["todoList"])
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
		Type: processdomain.CodexEventProcessExit,
		Content: processdomain.ExitResult{
			ExitCode: intPointer(2), FailureReason: `configuration error: invalid value "priority" for service_tier`,
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
		"event-transcript-bound",
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
	if processes.exitedResult.FailureReason != `configuration error: invalid value "priority" for service_tier` {
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
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1), WithUnitOfWork(uow))
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

func TestExecuteSessionResumesWithoutPendingPrompt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "implement session",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	processes.activeCount = 1
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, codex),
		WithMaxConcurrentAgents(1),
	)
	service.now = func() time.Time { return time.Unix(42, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	got, err := service.ExecuteSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("ExecuteSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued {
		t.Fatalf("ExecuteSession() status = %q", got.Status)
	}
	saved := repo.sessions["session-1"]
	if saved.Queue.Kind != domain.QueueKindResume || saved.Queue.ResumeCodexSessionID != "codex-session-1" || saved.Queue.Prompt != "" {
		t.Fatalf("queued execution = %#v", saved.Queue)
	}

	processes.activeCount = 0
	started, err := service.DrainQueuedSessions(ctx)
	if err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	if !codex.resumeCalled || codex.resumeInput.Prompt != "implement session" {
		t.Fatalf("codex resume input = %#v", codex.resumeInput)
	}
}

func TestDrainQueuedRecoveredChatResumeWithoutPendingPrompt(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Requirement:    "implement session",
		Status:         domain.StatusQueued,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
		QueuedAt:       &queuedAt,
		Queue: domain.QueueIntent{
			Kind:                 domain.QueueKindResume,
			Prompt:               restartRecoveryPrompt,
			ResumeCodexSessionID: "codex-session-1",
		},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	if !codex.resumeCalled || codex.resumeInput.Prompt != restartRecoveryPrompt {
		t.Fatalf("codex resume input = %#v", codex.resumeInput)
	}
}

func TestExecuteSessionQueuesResumeWhenPendingPromptExists(t *testing.T) {
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
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending,
	}}
	processes := newFakeProcessRepository()
	processes.activeCount = 1
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithMaxConcurrentAgents(1))

	got, err := service.ExecuteSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("ExecuteSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued {
		t.Fatalf("ExecuteSession() status = %q", got.Status)
	}
	if saved := repo.sessions["session-1"]; saved.Queue.Kind != domain.QueueKindResume || saved.Queue.ResumeCodexSessionID != "codex-session-1" || saved.Queue.Prompt != "" {
		t.Fatalf("queued execution = %#v", saved.Queue)
	}
}

func TestExecuteSessionQueuesStartWhenResumeIsUnavailable(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Requirement:  "implement session",
		Status:       domain.StatusStopped,
		WorktreePath: "/workspace/session-1",
	}
	processes := newFakeProcessRepository()
	processes.activeCount = 1
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{}),
		WithMaxConcurrentAgents(1),
	)
	service.now = func() time.Time { return time.Unix(42, 0).UTC() }

	got, err := service.ExecuteSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("ExecuteSession() error = %v", err)
	}
	if got.Status != domain.StatusQueued {
		t.Fatalf("ExecuteSession() status = %q", got.Status)
	}
	if saved := repo.sessions["session-1"]; saved.Queue.Kind != domain.QueueKindStart {
		t.Fatalf("queued execution = %#v", saved.Queue)
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
	if got.Status != domain.StatusStarting {
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
		BaseBranch:   "main",
		WorktreePath: "/workspace/high-session",
		QueuedAt:     &highQueuedAt,
		Queue:        domain.QueueIntent{Kind: domain.QueueKindStart, InitialStart: true, Prompt: "high priority"},
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
	if codex.startInput.Prompt != promptWithAnyCodeGuidance("high priority", repo.sessions["high-session"]) {
		t.Fatalf("initial queued prompt = %q", codex.startInput.Prompt)
	}
	if repo.sessions["high-session"].Status != domain.StatusStarting || repo.sessions["low-session"].Status != domain.StatusQueued {
		t.Fatalf("session statuses: high=%q low=%q", repo.sessions["high-session"].Status, repo.sessions["low-session"].Status)
	}
}

func TestDrainQueuedSessionsConvergesQueueWhenProcessAlreadyActive(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusQueued,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1", QueuedAt: &queuedAt,
		Queue: domain.QueueIntent{
			Kind: domain.QueueKindResume, Prompt: "resume current workflow node",
			ResumeCodexSessionID: "codex-session-1", ResumeOfProcessRunID: "process-old",
		},
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{
		ID: "process-new", SessionID: "session-1", Status: processdomain.StatusRunning, CodexSessionID: "codex-session-1",
	}
	processes.hasActive = true
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithMaxConcurrentAgents(1))
	service.now = func() time.Time { return time.Unix(42, 0).UTC() }

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 0 {
		t.Fatalf("DrainQueuedSessions() = %d, want 0", started)
	}
	got := repo.sessions["session-1"]
	if got.Status != domain.StatusRunning || got.Queue != (domain.QueueIntent{}) || got.QueuedAt != nil {
		t.Fatalf("converged session = %#v", got)
	}
	if codex.resumeCalled || len(processes.created) != 0 {
		t.Fatalf("duplicate execution launched: resume=%v created=%#v", codex.resumeCalled, processes.created)
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
			Kind:      domain.QueueKindStart,
			Prompt:    "implement session",
			NodeRunID: &nodeRunID,
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
	if workflows.failInput.SessionID != "session-1" || workflows.failInput.NodeRunID != nodeRunID {
		t.Fatalf("workflow fail input = %#v", workflows.failInput)
	}
	if workflows.failedInput.Code != "codex_start_failed" {
		t.Fatalf("workflow start failed input = %#v", workflows.failedInput)
	}
}

func TestDrainQueuedWorkflowStartFailureAdvancesFromPersistedFailureState(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
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
			Kind:      domain.QueueKindStart,
			Prompt:    "implement session",
			NodeRunID: &nodeRunID,
		},
	}
	workflows := &fakeWorkflowStarter{failAdvance: domain.WorkflowAdvance{
		SessionID:        "session-1",
		CurrentNodeID:    "approve",
		CurrentNodeTitle: "Approve failure",
		Status:           "running",
		RequiresCodex:    false,
	}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{startErr: errors.New("codex rejected args")}),
		WithWorkflows(workflows),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-start", nil }

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusWaitingApproval {
		t.Fatalf("session status = %q", got)
	}
}

func TestDrainQueuedWorkflowStartFailureUsesFallbackEventIDsToExitProcess(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusQueued,
		WorktreePath: "/workspace/session-1",
		QueuedAt:     &queuedAt,
		Queue: domain.QueueIntent{
			Kind:      domain.QueueKindStart,
			Prompt:    "Run workflow node",
			NodeRunID: &nodeRunID,
		},
	}
	processes := newFakeProcessRepository()
	events := &fakeEventStore{}
	workflows := &fakeWorkflowStarter{failAdvance: domain.WorkflowAdvance{
		SessionID:        "session-1",
		CurrentNodeID:    "approve",
		CurrentNodeTitle: "Approve failure",
		Status:           "running",
		RequiresCodex:    false,
	}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{startErr: errors.New("codex rejected args")}),
		WithWorkflows(workflows),
		WithEvents(events),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	generateCalls := 0
	service.generateID = func() (domain.ID, error) {
		generateCalls++
		switch generateCalls {
		case 1:
			return "process-run-1", nil
		case 2:
			return "event-starting", nil
		default:
			return "", errors.New("random source unavailable")
		}
	}

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusWaitingApproval {
		t.Fatalf("session status = %q", got)
	}
	if processes.exitedID != "process-run-1" || processes.hasActive {
		t.Fatalf("process exit = %q active=%v", processes.exitedID, processes.hasActive)
	}
	gotEvents := events.snapshot()
	requireSessionEventTypes(t, gotEvents,
		"session.starting", sessionStatusUpdatedEvent,
		"process.start_failed", "session.failed", sessionStatusUpdatedEvent,
		"session.waiting_approval", sessionStatusUpdatedEvent,
	)
}

func TestDrainQueuedWorkflowPreStartFailureNormalizesStateBeforeAdvance(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
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
			Kind:      domain.QueueKindStart,
			Prompt:    "implement session",
			NodeRunID: &nodeRunID,
		},
	}
	workflows := &fakeWorkflowStarter{failAdvance: domain.WorkflowAdvance{
		SessionID:        "session-1",
		CurrentNodeID:    "approve",
		CurrentNodeTitle: "Approve failure",
		Status:           "running",
		RequiresCodex:    false,
	}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}),
		WithWorkflows(workflows),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	generateCalls := 0
	service.generateID = func() (domain.ID, error) {
		generateCalls++
		if generateCalls == 1 {
			return "", errors.New("random source unavailable")
		}
		return domain.ID(fmt.Sprintf("event-%d", generateCalls)), nil
	}

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusWaitingApproval {
		t.Fatalf("session status = %q", got)
	}
	if workflows.failInput.Code != "codex_start_failed" {
		t.Fatalf("workflow fail input = %#v", workflows.failInput)
	}
}

func TestDrainQueuedWorkflowPreStartAndWorkflowFailurePersistsFailedSession(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
	failErr := errors.New("workflow fail node unavailable")
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:           "session-1",
		ProjectID:    "project-1",
		Mode:         domain.ModeWorkflow,
		Status:       domain.StatusQueued,
		WorktreePath: "/workspace/session-1",
		QueuedAt:     &queuedAt,
		Queue: domain.QueueIntent{
			Kind:      domain.QueueKindStart,
			NodeRunID: &nodeRunID,
		},
	}
	workflows := &fakeWorkflowStarter{failErr: failErr}
	events := &fakeEventStore{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{}),
		WithWorkflows(workflows),
		WithEvents(events),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	service.generateID = func() (domain.ID, error) { return "", errors.New("random source unavailable") }

	started, err := service.DrainQueuedSessions(ctx)
	if !errors.Is(err, failErr) {
		t.Fatalf("DrainQueuedSessions() error = %v, want %v", err, failErr)
	}
	if started != 0 {
		t.Fatalf("DrainQueuedSessions() = %d, want 0", started)
	}
	if got := repo.sessions["session-1"]; got.Status != domain.StatusFailed || got.Queue != (domain.QueueIntent{}) || got.QueuedAt != nil {
		t.Fatalf("failed session = %#v", got)
	}
	requireSessionEventTypes(t, events.snapshot(), "session.failed", sessionStatusUpdatedEvent)
}

func TestDrainQueuedWorkflowKeepsActiveProcessWhenRunningPersistenceAndStopFail(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	queuedAt := time.Unix(40, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusQueued,
		WorktreePath: "/workspace/session-1", QueuedAt: &queuedAt,
		Queue: domain.QueueIntent{
			Kind: domain.QueueKindStart, Prompt: "Run build", NodeRunID: &nodeRunID,
		},
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "extra context", Status: domain.PromptAppendPending}}
	runningSaveFailed := false
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusRunning && !runningSaveFailed {
			runningSaveFailed = true
			return errors.New("running save failed")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"},
		stopErr:     errors.New("stop unavailable"),
		events:      stream,
	}
	workflows := &fakeWorkflowStarter{}
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, codex),
		WithWorkflows(workflows),
		WithEvents(events),
		WithUnitOfWork(uow),
	)
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}

	started, err := service.DrainQueuedSessions(ctx)
	if started != 1 || err != nil {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	stream <- transcriptReadyEvent("codex-session-1")
	waitForProcessStop(t, codex, "id-1")
	if workflows.failInput.NodeRunID != "" {
		t.Fatalf("workflow failure was advanced: %#v", workflows.failInput)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusStarting {
		t.Fatalf("session status = %q", got)
	}
	if !processes.hasActive || processes.exitedID != "" {
		t.Fatalf("process active=%v exited=%q", processes.hasActive, processes.exitedID)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendInflight || promptAppend.DispatchedProcessRunID != "id-1" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
	if service.reserveWorkdir("/workspace/session-1", "session-2") {
		t.Fatal("workdir reservation was released while workflow process may still be running")
	}
}

func TestDrainQueuedWorkflowResumeMarksWaitingActionWhenRunningPersistenceFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	queuedAt := time.Unix(40, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusQueued,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1", QueuedAt: &queuedAt,
		Queue: domain.QueueIntent{
			Kind: domain.QueueKindResume, Prompt: "Resume build", ResumeCodexSessionID: "session-1", NodeRunID: &nodeRunID,
		},
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "extra context", Status: domain.PromptAppendPending}}
	runningSaveFailed := false
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusRunning && !runningSaveFailed {
			runningSaveFailed = true
			return errors.New("running save failed")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}, events: stream}
	workflows := &fakeWorkflowStarter{resumeSnapshot: domain.WorkflowRunSnapshot{
		SessionID: "session-1", Status: "waiting_resume_action", CurrentNodeID: "build",
	}}
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, codex),
		WithWorkflows(workflows),
		WithEvents(events),
		WithUnitOfWork(uow),
	)
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	stream <- transcriptReadyEvent("codex-session-1")
	waitForEventType(t, events, "session.resume_failed")
	if got := repo.sessions["session-1"].Status; got != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", got)
	}
	if processes.hasActive || processes.exitedID != "id-1" {
		t.Fatalf("process active=%v exited=%q", processes.hasActive, processes.exitedID)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendPending || promptAppend.DispatchedProcessRunID != "" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
}

func TestDrainQueuedWorkflowResumeFailureWaitsForResumeAction(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusQueued,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
		QueuedAt:       &queuedAt,
		Queue: domain.QueueIntent{
			Kind:                 domain.QueueKindResume,
			Prompt:               "Resume build",
			NodeRunID:            &nodeRunID,
			ResumeCodexSessionID: "codex-session-1",
		},
	}
	workflows := &fakeWorkflowStarter{resumeSnapshot: domain.WorkflowRunSnapshot{
		SessionID:     "session-1",
		Status:        "waiting_resume_action",
		CurrentNodeID: "build",
	}}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(newFakeProcessRepository(), &fakeCodexProcess{resumeErr: errors.New("resume unavailable")}),
		WithWorkflows(workflows),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	service.now = func() time.Time { return time.Unix(44, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-resume", nil }

	started, err := service.DrainQueuedSessions(ctx)
	if err != nil {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, want 1", started)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", got)
	}
	if workflows.resumeInput.SessionID != "session-1" || workflows.failInput.NodeRunID != "" {
		t.Fatalf("workflow resume=%#v fail=%#v", workflows.resumeInput, workflows.failInput)
	}
}

func TestDrainQueuedWorkflowResumeFailureReturnsWorkflowStateError(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	nodeRunID := domain.NodeRunID("node-run-1")
	workflowErr := errors.New("workflow store unavailable")
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusQueued,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
		QueuedAt:       &queuedAt,
		Queue: domain.QueueIntent{
			Kind:                 domain.QueueKindResume,
			Prompt:               "Resume build",
			NodeRunID:            &nodeRunID,
			ResumeCodexSessionID: "codex-session-1",
		},
	}
	workflows := &fakeWorkflowStarter{resumeErr: workflowErr}
	codex := &fakeCodexProcess{resumeErr: errors.New("resume unavailable")}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(newFakeProcessRepository(), codex),
		WithWorkflows(workflows),
		WithSessionLocker(NewMemorySessionLocker()),
	)
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("process-run-%d", nextID)), nil
	}

	started, err := service.DrainQueuedSessions(ctx)
	if !errors.Is(err, workflowErr) || !errors.Is(err, errWorkflowResumeStateNotPersisted) {
		t.Fatalf("DrainQueuedSessions() error = %v", err)
	}
	if started != 0 {
		t.Fatalf("DrainQueuedSessions() = %d, want 0", started)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", got)
	}

	workflows.resumeErr = nil
	workflows.resumeNodeAdvance = domain.WorkflowAdvance{
		SessionID:        "session-1",
		NodeRunID:        &nodeRunID,
		CurrentNodeID:    "build",
		CurrentNodeTitle: "Build",
		Status:           "running",
		RequiresCodex:    true,
		Prompt:           "Resume build",
	}
	codex.resumeErr = nil
	codex.resumeHandle = processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"}

	got, err := service.ExecuteSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("ExecuteSession() error = %v", err)
	}
	if got.Status != domain.StatusStarting || workflows.resumeNodeInput.SessionID != "session-1" {
		t.Fatalf("ExecuteSession() = %#v workflow=%#v", got, workflows.resumeNodeInput)
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
			FastMode:        true,
		},
	}
	files := newFakeAttachmentStore()
	files.sessionAttachments["artifact-image"] = domain.SessionFile{
		ID: "artifact-image", SessionID: "session-1", Role: domain.FileRoleArtifact,
		Path: "/archive/image.png", MimeType: "image/png",
	}
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: "session-1", Body: "continue work", Status: domain.PromptAppendPending,
		ArtifactIDs: []domain.SessionFileID{"artifact-image"},
	}}
	processes := newFakeProcessRepository()
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}, events: stream}
	service := New(repo, newFakeProjectRepository("project-1"), WithAttachments(repo, files), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(41, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "process-run-2", nil }

	got, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if got.Status != domain.StatusStarting {
		t.Fatalf("ResumeSession() status = %q", got.Status)
	}
	if !codex.resumeCalled || codex.resumeInput.CodexSessionID != "codex-session-1" || codex.resumeInput.ProcessRunID != "process-run-2" {
		t.Fatalf("codex resume input = %#v", codex.resumeInput)
	}
	if codex.resumeInput.Prompt != "continue work\n\nAttached files available on disk:\n- /archive/image.png" || repo.appends[0].Status != domain.PromptAppendInflight {
		t.Fatalf("resume prompt delivery = %#v appends=%#v", codex.resumeInput, repo.appends)
	}
	if !slices.Equal(codex.resumeInput.ImagePaths, []string{"/archive/image.png"}) {
		t.Fatalf("resume image paths = %#v", codex.resumeInput.ImagePaths)
	}
	if codex.resumeInput.Model != "gpt-5.4" || codex.resumeInput.ReasoningEffort != "high" || codex.resumeInput.PermissionMode != "workspace-write" || !codex.resumeInput.FastMode {
		t.Fatalf("codex resume config = %#v", codex.resumeInput)
	}
}

func TestResumeSessionMergesPendingPromptAppends(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{
		{ID: "append-old", SessionID: "session-1", Body: "already sent", Status: domain.PromptAppendDispatched},
		{ID: "append-1", SessionID: "session-1", Body: "first pending", Status: domain.PromptAppendPending},
		{ID: "append-2", SessionID: "session-1", Body: "second pending", Status: domain.PromptAppendPending},
	}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.generateID = func() (domain.ID, error) { return "process-run-2", nil }

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if codex.resumeInput.Prompt != "first pending\n\nsecond pending" {
		t.Fatalf("resume prompt = %q", codex.resumeInput.Prompt)
	}
	if repo.appends[0].Status != domain.PromptAppendDispatched || repo.appends[0].DispatchedProcessRunID != "" {
		t.Fatalf("historical append changed: %#v", repo.appends[0])
	}
	for _, promptAppend := range repo.appends[1:] {
		if promptAppend.Status != domain.PromptAppendInflight || promptAppend.DispatchedProcessRunID != "process-run-2" {
			t.Fatalf("pending append was not marked inflight: %#v", repo.appends)
		}
	}
}

func TestResumeProcessSuccessCompletesInflightPromptAppends(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	stream := make(chan processdomain.CodexEvent, 1)
	codex := &fakeCodexProcess{
		resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"},
		events:       stream,
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithEvents(events))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if repo.appends[0].Status != domain.PromptAppendInflight {
		t.Fatalf("append before process exit = %#v", repo.appends[0])
	}
	stream <- processdomain.CodexEvent{Type: processdomain.CodexEventProcessExit, Content: processdomain.ExitResult{ExitCode: intPointer(0)}}
	close(stream)
	waitForEventType(t, events, "session.stopped")
	if repo.appends[0].Status != domain.PromptAppendDispatched || repo.appends[0].DispatchedAt == nil || repo.appends[0].DispatchedProcessRunID != "id-1" {
		t.Fatalf("completed append = %#v", repo.appends[0])
	}
}

func TestAsyncResumeFailureReleasesPromptAndMarksResumeFailed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	stream := make(chan processdomain.CodexEvent, 1)
	codex := &fakeCodexProcess{
		resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"},
		events:       stream,
	}
	events := &fakeEventStore{}
	processes := newFakeProcessRepository()
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	stream <- processdomain.CodexEvent{Type: processdomain.CodexEventProcessExit, Content: processdomain.ExitResult{ExitCode: intPointer(1), FailureReason: "thread not found"}}
	close(stream)
	waitForEventType(t, events, "session.resume_failed")
	if got := repo.sessions["session-1"].Status; got != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", got)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendPending || promptAppend.DispatchedAt != nil || promptAppend.DispatchedProcessRunID != "" {
		t.Fatalf("released append = %#v", promptAppend)
	}
	queued, err := service.ExecuteSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("ExecuteSession() after resume failure error = %v", err)
	}
	if queued.Status != domain.StatusQueued || repo.sessions["session-1"].Queue.Kind != domain.QueueKindResume {
		t.Fatalf("retry queue = %#v", repo.sessions["session-1"])
	}
}

func TestRunningPersistenceFailureStopsStartedProcessAndKeepsPromptPending(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	runningSaveFailed := false
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusRunning && !runningSaveFailed {
			runningSaveFailed = true
			return errors.New("running save failed")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}, events: stream}
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithUnitOfWork(uow))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	stream <- transcriptReadyEvent("codex-session-1")
	waitForEventType(t, events, "session.resume_failed")
	if codex.stoppedID != "id-1" || !codex.eventsCalled {
		t.Fatalf("codex cleanup stopped=%q eventsCalled=%v", codex.stoppedID, codex.eventsCalled)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", got)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendPending || promptAppend.DispatchedProcessRunID != "" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
	if processes.hasActive {
		t.Fatalf("process remained active: %#v", processes.active)
	}
}

func TestRunningPersistenceFailureKeepsActiveProcessAndWorkdirWhenStopFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	runningSaveFailed := false
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusRunning && !runningSaveFailed {
			runningSaveFailed = true
			return errors.New("running save failed")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	stream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"},
		stopErr:      errors.New("stop unavailable"),
		events:       stream,
	}
	events := &fakeEventStore{}
	uow := &fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events), WithUnitOfWork(uow))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}

	_, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true})
	if err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	stream <- transcriptReadyEvent("codex-session-1")
	waitForProcessStop(t, codex, "id-1")
	if codex.stoppedID != "id-1" || !processes.hasActive || processes.exitedID != "" {
		t.Fatalf("process cleanup stopped=%q active=%v exited=%q", codex.stoppedID, processes.hasActive, processes.exitedID)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusStarting {
		t.Fatalf("session status = %q", got)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendInflight || promptAppend.DispatchedProcessRunID != "id-1" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
	if service.reserveWorkdir("/workspace/session-1", "session-2") {
		t.Fatal("workdir reservation was released while process may still be running")
	}
}

func TestCodexEventStreamFailureStopsProcessAndReleasesPrompt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	processes := newFakeProcessRepository()
	codex := &fakeCodexProcess{
		resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"},
		eventsErr:    errors.New("events unavailable"),
	}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("id-%d", nextID)), nil
	}

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	waitForEventType(t, events, "session.resume_failed")
	if codex.stoppedID != "id-1" || processes.hasActive {
		t.Fatalf("process cleanup stopped=%q active=%v", codex.stoppedID, processes.hasActive)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", got)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendPending || promptAppend.DispatchedProcessRunID != "" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
}

func TestCodexEventStreamFailureKeepsProcessAndPromptInflightWhenStopFails(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStopped,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	processes := newFakeProcessRepository()
	stopCalled := make(chan struct{})
	codex := &fakeCodexProcess{
		resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"},
		eventsErr:    errors.New("events unavailable"),
		stopHook: func(context.Context, processdomain.RunID) error {
			close(stopCalled)
			return errors.New("stop unavailable")
		},
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	select {
	case <-stopCalled:
	case <-time.After(time.Second):
		t.Fatal("codex Stop was not called")
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusStarting {
		t.Fatalf("session status = %q", got)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendInflight || promptAppend.DispatchedProcessRunID != "process-run-1" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
	if !processes.hasActive {
		t.Fatal("process was falsely marked exited")
	}
	if service.reserveWorkdir("/workspace/session-1", "session-2") {
		t.Fatal("workdir reservation was released while process may still be running")
	}
}

func TestInterruptedSessionRecoveryReleasesInflightPrompt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: session.ID, Body: "continue", Status: domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: intPointer(1234)}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithEvents(&fakeEventStore{}))
	service.generateID = func() (domain.ID, error) { return "event-1", nil }

	if count, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil || count != 1 {
		t.Fatalf("MarkInterruptedSessionsRecoverable() = %d, %v", count, err)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusQueued {
		t.Fatalf("session status = %q", got)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendPending || promptAppend.DispatchedProcessRunID != "" {
		t.Fatalf("recovered append = %#v", promptAppend)
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
		SessionID:        "session-1",
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
	if got.Status != domain.StatusStarting || workflows.resumeNodeInput.SessionID != "session-1" {
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
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "retry work", Status: domain.PromptAppendPending}}
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
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendPending || promptAppend.DispatchedProcessRunID != "" {
		t.Fatalf("prompt append after start failure = %#v", promptAppend)
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
		resumeNodeAdvance: domain.WorkflowAdvance{
			SessionID:     "session-1",
			NodeRunID:     func() *domain.NodeRunID { value := domain.NodeRunID("node-run-1"); return &value }(),
			RequiresCodex: true,
			Prompt:        "Resume build",
		},
		resumeSnapshot: domain.WorkflowRunSnapshot{
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
		if event.Type == "workflow.waiting_resume_action" && event.Payload["sessionId"] == "session-1" && event.Payload["currentNodeId"] == "build" {
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
	if processes.stoppingID != "process-run-1" {
		t.Fatalf("stopping process id = %q", processes.stoppingID)
	}
	if processes.exitedID != "process-run-1" || processes.exitedResult.FailureReason != "stopped by user" {
		t.Fatalf("exited process = %q %#v", processes.exitedID, processes.exitedResult)
	}
	if repo.sessions["session-1"].Status != domain.StatusStopped {
		t.Fatalf("saved session = %#v", repo.sessions["session-1"])
	}
}

func TestAcknowledgedPromptIsNotRedeliveredAfterStop(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeChat,
		Status:         domain.StatusRunning,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{
		ID:                     "append-old",
		SessionID:              "session-1",
		Body:                   "already delivered",
		Status:                 domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	service.now = func() time.Time { return time.Unix(42, 0).UTC() }
	ids := []domain.ID{"append-new", "process-run-2"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.queueDrainScheduler = func(*Service) {}
	if err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{
		ProcessRunID: "process-run-1",
	}, processdomain.CodexEvent{
		Type:      processdomain.CodexEventStatus,
		Content:   processdomain.CodexStatusContent{Code: "task.started"},
		CreatedAt: time.Unix(41, 0).UTC(),
	}); err != nil {
		t.Fatalf("handleCodexEvent() error = %v", err)
	}

	if _, err := service.StopSession(ctx, "session-1"); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "new instruction"}); err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if started, err := service.DrainQueuedSessions(ctx); err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}

	if codex.resumeInput.Prompt != "new instruction" {
		t.Fatalf("resume prompt = %q", codex.resumeInput.Prompt)
	}
	if repo.appends[0].Status != domain.PromptAppendDispatched {
		t.Fatalf("old prompt append = %#v", repo.appends[0])
	}
}

func TestPromptAcknowledgementCompletesInflightAppendWithUnitOfWork(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
	}
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: "session-1", Body: "delivered", Status: domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	uow := &fakeUnitOfWork{tx: fakeTx{
		sessions:  repo,
		processes: processes,
		events:    &fakeEventStore{},
	}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}), WithUnitOfWork(uow))
	deliveredAt := time.Unix(41, 0).UTC()

	if err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{
		ProcessRunID: "process-run-1",
	}, processdomain.CodexEvent{
		Type:      processdomain.CodexEventStatus,
		Content:   processdomain.CodexStatusContent{Code: "turn.started"},
		CreatedAt: deliveredAt,
	}); err != nil {
		t.Fatalf("handleCodexEvent() error = %v", err)
	}

	if !uow.called {
		t.Fatal("unit of work was not used")
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendDispatched || promptAppend.DispatchedAt == nil || !promptAppend.DispatchedAt.Equal(deliveredAt) {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
}

func TestStopBeforePromptAcknowledgementDefersPromptSettlement(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusStarting,
	}
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: "session-1", Body: "not delivered", Status: domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusStarting}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}))
	service.processConsumers.Store(processdomain.RunID("process-run-1"), struct{}{})

	got, err := service.stopSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if got.Status != domain.StatusStopping {
		t.Fatalf("StopSession() status = %q", got.Status)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendInflight || promptAppend.DispatchedProcessRunID != "process-run-1" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
}

func TestLatePromptAcknowledgementWhileStoppingPreventsRedelivery(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: "session-1", Body: "delivered before stop", Status: domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))
	ids := []domain.ID{"append-new", "process-run-2"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.queueDrainScheduler = func(*Service) {}
	service.processConsumers.Store(processdomain.RunID("process-run-1"), struct{}{})

	if got, err := service.stopSession(ctx, "session-1"); err != nil || got.Status != domain.StatusStopping {
		t.Fatalf("StopSession() = %#v, %v", got, err)
	}
	if err := service.handleCodexEvent(ctx, "session-1", processdomain.CodexHandle{
		ProcessRunID: "process-run-1",
	}, processdomain.CodexEvent{
		Type:      processdomain.CodexEventStatus,
		Content:   processdomain.CodexStatusContent{Code: "task.started"},
		CreatedAt: time.Unix(41, 0).UTC(),
	}); err != nil {
		t.Fatalf("handleCodexEvent() error = %v", err)
	}
	if _, _, err := service.persistCodexProcessExit(ctx, repo.sessions["session-1"], processdomain.CodexHandle{
		ProcessRunID: "process-run-1",
	}, codexStartOptions{}, processdomain.ExitResult{
		FailureReason: "signal: terminated",
		FinishedAt:    time.Unix(42, 0).UTC(),
	}, nil); err != nil {
		t.Fatalf("persistCodexProcessExit() error = %v", err)
	}
	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "new instruction"}); err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if started, err := service.DrainQueuedSessions(ctx); err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	if codex.resumeInput.Prompt != "new instruction" {
		t.Fatalf("resume prompt = %q", codex.resumeInput.Prompt)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendDispatched || promptAppend.DispatchedProcessRunID != "process-run-1" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
}

func TestStoppedProcessExitReleasesUnacknowledgedPrompt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	original := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		CodexSessionID: "codex-session-1", WorktreePath: "/workspace/session-1",
	}
	repo.sessions[original.ID] = original
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: original.ID, Body: "not delivered", Status: domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	codex := &fakeCodexProcess{resumeHandle: processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(events))
	ids := []domain.ID{"event-stopping", "event-exited", "event-stopped", "append-new", "event-queued", "process-run-2", "event-starting", "event-transcript-bound", "event-running"}
	service.generateID = func() (domain.ID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	service.queueDrainScheduler = func(*Service) {}
	service.processConsumers.Store(processdomain.RunID("process-run-1"), struct{}{})

	if got, err := service.stopSession(ctx, "session-1"); err != nil || got.Status != domain.StatusStopping {
		t.Fatalf("StopSession() = %#v, %v", got, err)
	}
	if _, _, err := service.persistCodexProcessExit(ctx, original, processdomain.CodexHandle{
		ProcessRunID: "process-run-1",
	}, codexStartOptions{}, processdomain.ExitResult{
		FailureReason: "signal: terminated",
		FinishedAt:    time.Unix(42, 0).UTC(),
	}, nil); err != nil {
		t.Fatalf("persistCodexProcessExit() error = %v", err)
	}

	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendPending || promptAppend.DispatchedProcessRunID != "" {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
	if _, err := service.AppendPrompt(ctx, AppendPromptInput{SessionID: "session-1", Body: "new instruction"}); err != nil {
		t.Fatalf("AppendPrompt() error = %v", err)
	}
	if started, err := service.DrainQueuedSessions(ctx); err != nil || started != 1 {
		t.Fatalf("DrainQueuedSessions() = %d, %v", started, err)
	}
	if codex.resumeInput.Prompt != "not delivered\n\nnew instruction" {
		t.Fatalf("resume prompt = %q", codex.resumeInput.Prompt)
	}
}

func TestPromptAcknowledgementRetriesPersistenceFailure(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
	}
	repo.appends = []domain.PromptAppend{{
		ID: "append-1", SessionID: "session-1", Body: "delivered", Status: domain.PromptAppendInflight,
		DispatchedProcessRunID: "process-run-1",
	}}
	repo.completePromptAppendsErrs = []error{errors.New("temporary write failure"), nil}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, &fakeCodexProcess{}))
	service.processExitDelay = func(int) time.Duration { return 0 }
	locker := &fakeSessionLocker{}
	service.locker = locker

	service.handleCodexEventWithRetry("session-1", processdomain.CodexHandle{
		ProcessRunID: "process-run-1",
	}, processdomain.CodexEvent{Type: processdomain.CodexEventStatus, Content: processdomain.CodexStatusContent{Code: "turn.started"}})

	if repo.completePromptAppendsCalls != 2 {
		t.Fatalf("complete prompt append calls = %d", repo.completePromptAppendsCalls)
	}
	if promptAppend := repo.appends[0]; promptAppend.Status != domain.PromptAppendDispatched {
		t.Fatalf("prompt append = %#v", promptAppend)
	}
	if !slices.Equal(locker.ids, []domain.ID{"session-1", "session-1"}) {
		t.Fatalf("session lock attempts = %#v", locker.ids)
	}
}

func TestPlanUpdateRetriesPersistenceFailure(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
	}
	saveCalls := 0
	repo.saveHook = func(domain.Session) error {
		saveCalls++
		if saveCalls == 1 {
			return errors.New("temporary write failure")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	events := &fakeEventStore{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{}),
		WithEvents(events),
	)
	service.processExitDelay = func(int) time.Duration { return 0 }

	err := service.handleCodexEventWithRetry("session-1", processdomain.CodexHandle{
		ProcessRunID: "process-run-1",
	}, processdomain.CodexEvent{
		EventID: "plan-1", Type: processdomain.CodexEventPlan,
		Content: processdomain.PlanUpdate{Items: []processdomain.PlanItem{{Step: "Persist TODO", Status: processdomain.PlanItemInProgress}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saveCalls != 2 || len(repo.sessions["session-1"].TodoList.Items) != 1 {
		t.Fatalf("save calls = %d, todo = %#v", saveCalls, repo.sessions["session-1"].TodoList)
	}
	count := 0
	for _, event := range events.snapshot() {
		if event.Type == "session.todo_list_updated" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("todo update event count = %d, want 1", count)
	}
}

func TestStopSessionCompletesAfterRequestContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	publisher := &fakeEventPublisher{}
	publisher.onPublish = func(event eventdomain.DomainEvent) {
		if event.Type == "session.stopping" {
			cancel()
		}
	}
	codex := &fakeCodexProcess{}
	codex.stopHook = func(stopCtx context.Context, _ processdomain.RunID) error {
		if err := stopCtx.Err(); err != nil {
			t.Fatalf("detached stop context error = %v", err)
		}
		return nil
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithEvents(&fakeEventStore{}), WithEventPublisher(publisher))

	got, err := service.StopSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if got.Status != domain.StatusStopped || repo.sessions["session-1"].Status != domain.StatusStopped {
		t.Fatalf("session status = %q saved=%q", got.Status, repo.sessions["session-1"].Status)
	}
}

func TestStopSessionReconcilesMissingLocalProcess(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusStopping,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusStopping, PID: intPointer(1234)}
	processes.hasActive = true
	codex := &fakeCodexProcess{stopErr: processdomain.ErrProcessNotFound}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex))

	got, err := service.StopSession(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if got.Status != domain.StatusStopped || processes.exitedID != "process-run-1" {
		t.Fatalf("session status = %q exited process = %q", got.Status, processes.exitedID)
	}
}

func TestStopSessionIsIdempotentAfterStopped(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusStopped}
	service := New(repo, newFakeProjectRepository("project-1"))

	got, err := service.StopSession(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if got.Status != domain.StatusStopped {
		t.Fatalf("session status = %q", got.Status)
	}
}

func TestStopSessionRetriesPendingQuestionCleanupAfterStopped(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusStopped}
	questions := &fakeQuestionCanceller{cancelErr: errors.New("cancel failed")}
	service := New(repo, newFakeProjectRepository("project-1"), WithQuestions(questions))

	if _, err := service.StopSession(context.Background(), "session-1"); err == nil {
		t.Fatal("StopSession() error = nil, want pending question cleanup failure")
	}
	questions.cancelErr = nil
	if _, err := service.StopSession(context.Background(), "session-1"); err != nil {
		t.Fatalf("StopSession() retry error = %v", err)
	}
	if questions.cancelledSessionID != "session-1" || questions.cancelReason != "session stopped" {
		t.Fatalf("cancelled questions = %q %q", questions.cancelledSessionID, questions.cancelReason)
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

func TestStopSessionFromQueuedCancelsQueue(t *testing.T) {
	ctx := context.Background()
	queuedAt := time.Unix(41, 0).UTC()
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusQueued,
		QueuedAt:  &queuedAt,
		Queue: domain.QueueIntent{
			Kind:     domain.QueueKindStart,
			Priority: domain.QueuePriorityMedium,
			Prompt:   "queued prompt",
		},
	}
	events := &fakeEventStore{}
	questions := &fakeQuestionCanceller{cancelErr: errors.New("question store unavailable")}
	queueDrained := make(chan struct{}, 1)
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithEvents(events),
		WithQuestions(questions),
	)
	service.queueDrainScheduler = func(*Service) { queueDrained <- struct{}{} }
	service.now = func() time.Time { return time.Unix(42, 0).UTC() }

	got, err := service.StopSession(ctx, "session-1")
	if err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if got.Status != domain.StatusStopped {
		t.Fatalf("StopSession() status = %q", got.Status)
	}
	saved := repo.sessions["session-1"]
	if saved.QueuedAt != nil || saved.Queue != (domain.QueueIntent{}) {
		t.Fatalf("saved queue = %#v queuedAt=%v", saved.Queue, saved.QueuedAt)
	}
	gotEvents := events.snapshot()
	requireSessionEventTypes(t, gotEvents, "session.stopped", sessionStatusUpdatedEvent)
	if gotEvents[0].Payload["reason"] != "queue_cancelled" {
		t.Fatalf("events = %#v", gotEvents)
	}
	if questions.cancelledSessionID != "" {
		t.Fatalf("ordinary queued cancellation touched questions: %q", questions.cancelledSessionID)
	}
	select {
	case <-queueDrained:
	default:
		t.Fatal("queued cancellation did not schedule queue drain")
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
	requireSessionEventTypes(t, txEvents.snapshot(),
		"session.stopping", sessionStatusUpdatedEvent,
		"session.stopped", sessionStatusUpdatedEvent,
	)
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
	startStream := make(chan processdomain.CodexEvent)
	resumeStream := make(chan processdomain.CodexEvent)
	codex := &fakeCodexProcess{
		startHandle:  processdomain.CodexHandle{PID: 1234},
		eventStreams: []<-chan processdomain.CodexEvent{startStream, resumeStream},
	}
	locker := &fakeSessionLocker{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, codex), WithSessionLocker(locker))
	service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

	if _, err := service.StartSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if len(locker.ids) == 0 || locker.ids[len(locker.ids)-1] != "session-1" {
		t.Fatalf("locked ids after start = %#v", locker.ids)
	}
	close(startStream)
	if done, ok := service.processConsumerDone("process-run-1"); ok {
		<-done
	}

	repo.sessions["session-1"] = domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Status:         domain.StatusStopped,
		CodexSessionID: "codex-session-1",
		WorktreePath:   "/workspace/session-1",
	}
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "continue", Status: domain.PromptAppendPending}}
	codex.resumeHandle = processdomain.CodexHandle{PID: 2233, CodexSessionID: "codex-session-1"}
	service.generateID = func() (domain.ID, error) { return "process-run-2", nil }
	lockedBeforeResume := len(locker.ids)
	if _, err := service.ResumeSessionWithOptions(ctx, "session-1", StartSessionOptions{Force: true}); err != nil {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if len(locker.ids) <= lockedBeforeResume || locker.ids[len(locker.ids)-1] != "session-1" {
		t.Fatalf("locked ids after resume = %#v", locker.ids)
	}

	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning}
	processes.active = processdomain.Run{ID: "process-run-2", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	codex.stopHook = func(context.Context, processdomain.RunID) error {
		close(resumeStream)
		return nil
	}
	lockedBeforeStop := len(locker.ids)
	if _, err := service.StopSession(ctx, "session-1"); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	if len(locker.ids) <= lockedBeforeStop {
		t.Fatalf("StopSession() did not use locker: %#v", locker.ids)
	}
	lockedBeforeClose := len(locker.ids)
	if _, err := service.CloseSession(ctx, CloseSessionInput{SessionID: "session-1"}); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if len(locker.ids) <= lockedBeforeClose {
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
	txRepo.sessions["session-1"] = repo.sessions["session-1"]
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
		Type:    processdomain.CodexEventMessage,
		Content: processdomain.CodexMessageContent{Role: "assistant", Text: "hello"},
	}
	close(source)
	codex := &fakeCodexProcess{startHandle: processdomain.CodexHandle{PID: 1234, CodexSessionID: "codex-session-1"}, events: source}
	service := New(txRepo, newFakeProjectRepository("project-1"), WithProcesses(txProcesses, codex), WithEvents(&fakeEventStore{}), WithEventPublisher(publisher), WithUnitOfWork(uow))
	service.now = func() time.Time { return time.Unix(45, 0).UTC() }
	ids := []domain.ID{"process-run-1", "event-starting", "event-transcript-bound", "event-running", "event-codex", "event-process-exited"}
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
	if got.Status != domain.StatusStarting {
		t.Fatalf("status = %q", got.Status)
	}
	waitForPublishedCodexEventType(t, publisher, processdomain.CodexEventMessage)
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
	if txRepo.sessions["session-1"].Status != domain.StatusStopped || txRepo.sessions["session-1"].CodexSessionID != "codex-session-1" {
		t.Fatalf("tx session = %#v", txRepo.sessions["session-1"])
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
	repo.appends = []domain.PromptAppend{{ID: "append-1", SessionID: "session-1", Body: "retry", Status: domain.PromptAppendPending}}
	txRepo := newFakeRepository()
	txRepo.sessions["session-1"] = repo.sessions["session-1"]
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
	requireSessionEventTypes(t, txEvents.snapshot(),
		"session.starting", sessionStatusUpdatedEvent,
		"process.resume_failed", "session.resume_failed", sessionStatusUpdatedEvent,
	)
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
			want:    []string{"execute", "close"},
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
			want:    []string{"execute", "close"},
		},
		{
			name:    "resume failed",
			session: domain.Session{Status: domain.StatusResumeFailed, CodexSessionID: "codex-1"},
			want:    []string{"execute", "stop", "close"},
		},
		{
			name:    "resume failed without codex session id",
			session: domain.Session{Status: domain.StatusResumeFailed},
			want:    []string{"execute", "stop", "close"},
		},
		{
			name:    "queued",
			session: domain.Session{Status: domain.StatusQueued},
			want:    []string{"execute", "stop", "close"},
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

func TestMarkInterruptedSessionsRecoverableDelegatesToRecoveryCoordinator(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	running := domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning, CodexSessionID: "codex-1"}
	stopping := domain.Session{ID: "session-2", ProjectID: "project-1", Status: domain.StatusStopping, CodexSessionID: "codex-2"}
	missingID := domain.Session{ID: "session-3", ProjectID: "project-1", Status: domain.StatusRunning}
	for _, session := range []domain.Session{running, stopping, missingID} {
		repo.sessions[session.ID] = session
	}
	repo.interruptedSessions = []domain.Session{running, stopping}
	repo.listSessions = []domain.Session{missingID}
	repo.listTotal = 1
	events := &fakeEventStore{}
	processes := newFakeProcessRepository()
	processes.activeBySession = map[processdomain.SessionID]processdomain.Run{
		"session-1": {ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: intPointer(101)},
		"session-2": {ID: "process-2", SessionID: "session-2", Status: processdomain.StatusStopping, PID: intPointer(102)},
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithProcesses(processes, &fakeCodexProcess{}))
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
	if count != 3 {
		t.Fatalf("recoverable count = %d", count)
	}
	if got := repo.sessions["session-1"]; got.Status != domain.StatusQueued || got.Queue.Kind != domain.QueueKindResume {
		t.Fatalf("running session recovery = %#v", got)
	}
	if got := repo.sessions["session-2"]; got.Status != domain.StatusStopped {
		t.Fatalf("stopping session recovery = %#v", got)
	}
	if got := repo.sessions["session-3"]; got.Status != domain.StatusResumeFailed {
		t.Fatalf("missing Codex session recovery = %#v", got)
	}
}

func TestMarkInterruptedSessionsRecoverableCompletesPendingWorkflowExit(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Mode:      domain.ModeWorkflow,
		Status:    domain.StatusRunning,
	}
	repo.sessions[session.ID] = session
	repo.listSessions = []domain.Session{session}
	repo.listTotal = 1
	eventSessionID := eventdomain.SessionID(session.ID)
	events := &fakeEventStore{events: []eventdomain.DomainEvent{{
		ID:        "event-1",
		SessionID: &eventSessionID,
		Type:      "workflow.exit_pending",
		Payload: workflowProcessExitPayload(domain.WorkflowProcessExitInput{
			SessionID: "session-1",
			NodeRunID: "node-run-1",
			Output:    map[string]any{"processRunId": "process-run-1", "exit": "completed"},
		}),
	}}}
	nextNodeRunID := domain.NodeRunID("node-run-2")
	advance := domain.WorkflowAdvance{
		SessionID:     "session-1",
		NodeRunID:     &nextNodeRunID,
		CurrentNodeID: "verify",
		RequiresCodex: true,
		Prompt:        "verify",
	}
	workflows := &fakeWorkflowStarter{recoverAdvance: &advance}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), nil), WithEvents(events), WithWorkflows(workflows))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-2", nil }

	count, err := service.MarkInterruptedSessionsRecoverable(ctx)
	if err != nil {
		t.Fatalf("MarkInterruptedSessionsRecoverable() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("recoverable count = %d", count)
	}
	if got := repo.sessions[session.ID]; got.Status != domain.StatusQueued || got.ID != "session-1" || got.Queue.NodeRunID == nil || *got.Queue.NodeRunID != "node-run-2" {
		t.Fatalf("recovered session = %#v", got)
	}
	if workflows.recoverInput.SessionID != "session-1" || workflows.recoverInput.NodeRunID != "node-run-1" {
		t.Fatalf("recover input = %#v", workflows.recoverInput)
	}
	for _, event := range events.snapshot() {
		if event.Type == "session.recoverable" {
			t.Fatalf("pending workflow exit fell back to generic recovery: %#v", events.snapshot())
		}
	}
}

func TestMarkInterruptedSessionsRecoverableDefersPendingWorkflowSideEffect(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusRunning,
		CodexSessionID: "codex-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	eventSessionID := eventdomain.SessionID(session.ID)
	events := &fakeEventStore{events: []eventdomain.DomainEvent{{
		ID:        "event-1",
		SessionID: &eventSessionID,
		Type:      "workflow.exit_pending",
		Payload: workflowProcessExitPayload(domain.WorkflowProcessExitInput{
			SessionID: "session-1",
			NodeRunID: "node-run-1",
			Output:    map[string]any{"processRunId": "process-run-1", "exit": "completed"},
		}),
	}}}
	exprNodeRunID := domain.NodeRunID("node-run-expr")
	advance := domain.WorkflowAdvance{
		SessionID:     "session-1",
		NodeRunID:     &exprNodeRunID,
		CurrentNodeID: "expr",
		Expr:          &domain.WorkflowExpr{Script: `{status: "ready"}`},
	}
	workflows := &fakeWorkflowStarter{recoverAdvance: &advance}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), nil), WithEvents(events), WithWorkflows(workflows))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-2", nil }

	if _, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil {
		t.Fatalf("MarkInterruptedSessionsRecoverable() error = %v", err)
	}
	if got := repo.sessions[session.ID].Status; got != domain.StatusResumeFailed {
		t.Fatalf("session status = %q", got)
	}
	if got := repo.sessions[session.ID].CodexSessionID; got != "" {
		t.Fatalf("CodexSessionID = %q, want cleared", got)
	}
	if slices.Contains(availableActions(repo.sessions[session.ID]), "resume") {
		t.Fatalf("available actions = %#v", availableActions(repo.sessions[session.ID]))
	}
	if _, err := service.ResumeSession(ctx, session.ID); err == nil || !strings.Contains(err.Error(), "without codex session id") {
		t.Fatalf("ResumeSession() error = %v", err)
	}
	if workflows.resumeInput.SessionID != session.ID || workflows.resumeInput.Code != "workflow_advance_interrupted" {
		t.Fatalf("resume failure input = %#v", workflows.resumeInput)
	}
	if workflows.completeCalls != 0 {
		t.Fatalf("CompleteNode() calls = %d, want 0", workflows.completeCalls)
	}
}

func TestMarkInterruptedSessionsRecoverableExecutesPersistedSystemAdvances(t *testing.T) {
	cases := []struct {
		name    string
		advance domain.WorkflowAdvance
		setup   func(*Service, *fakeRepository)
		verify  func(*testing.T, *Service, *fakeRepository)
	}{
		{
			name: "expr",
			advance: domain.WorkflowAdvance{SessionID: "session-1", CurrentNodeID: "expr", Expr: &domain.WorkflowExpr{
				Script: `{status: "ready"}`, Params: map[string]any{"input": "value"},
			}},
			setup: func(service *Service, _ *fakeRepository) {
				service.workflows = &fakeWorkflowStarter{advance: domain.WorkflowAdvance{SessionID: "session-1", Completed: true}}
			},
			verify: func(t *testing.T, service *Service, _ *fakeRepository) {
				if service.workflows.(*fakeWorkflowStarter).completeCalls != 1 {
					t.Fatalf("CompleteNode calls = %d", service.workflows.(*fakeWorkflowStarter).completeCalls)
				}
			},
		},
		{
			name:    "close",
			advance: domain.WorkflowAdvance{SessionID: "session-1", CurrentNodeID: "close", Close: true},
		},
		{
			name: "merge",
			advance: domain.WorkflowAdvance{SessionID: "session-1", CurrentNodeID: "merge", Merge: &domain.WorkflowMerge{
				Strategy: "merge",
			}},
			setup: func(service *Service, repo *fakeRepository) {
				service.merge = &fakeMergePort{result: gitdiffdomain.MergeResult{Status: "merged", Strategy: "merge", BaseBranch: "master"}}
				service.workflows = &fakeWorkflowStarter{advance: domain.WorkflowAdvance{SessionID: "session-1", Completed: true}}
				session := repo.sessions["session-1"]
				session.BaseBranch = "master"
				session.WorktreePath = "/tmp/worktree"
				session.WorktreeBranch = "session-1"
				session.WorktreeCleanup = domain.WorktreeCleanup{Status: domain.WorktreeCleanupActive, OwnershipToken: "owner"}
				repo.sessions[session.ID] = session
			},
			verify: func(t *testing.T, service *Service, _ *fakeRepository) {
				if !service.merge.(*fakeMergePort).mergeCalled {
					t.Fatal("merge was not executed")
				}
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepository()
			session := domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning}
			repo.sessions[session.ID] = session
			repo.listSessions = []domain.Session{session}
			repo.listTotal = 1
			nodeRunID := domain.NodeRunID("node-run-1")
			tt.advance.NodeRunID = &nodeRunID
			eventSessionID := eventdomain.SessionID(session.ID)
			events := &fakeEventStore{events: []eventdomain.DomainEvent{{
				ID: "command-1", SessionID: &eventSessionID, Type: workflowSystemAdvancePendingEvent,
				Payload: workflowAdvancePendingPayload(tt.advance),
			}}}
			service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithProcesses(newFakeProcessRepository(), nil))
			service.now = func() time.Time { return time.Unix(100, 0).UTC() }
			service.generateID = func() (domain.ID, error) { return "completed-1", nil }
			if tt.setup != nil {
				tt.setup(service, repo)
			}

			count, err := service.MarkInterruptedSessionsRecoverable(context.Background())
			if err != nil {
				t.Fatalf("MarkInterruptedSessionsRecoverable() error = %v", err)
			}
			if count != 1 {
				t.Fatalf("recovered count = %d", count)
			}
			if tt.verify != nil {
				tt.verify(t, service, repo)
			}
			foundCompleted := false
			for _, event := range events.snapshot() {
				if event.Type == workflowSystemAdvanceCompletedEvent && event.Payload["commandEventId"] == "command-1" {
					foundCompleted = true
				}
			}
			if !foundCompleted {
				t.Fatalf("completion event missing: %#v", events.snapshot())
			}
		})
	}
}

func TestPendingSystemAdvancesReturnsEveryUnacknowledgedCommandInOrder(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	events := &fakeEventStore{events: []eventdomain.DomainEvent{
		{ID: "command-1", SessionID: &sessionID, Type: workflowSystemAdvancePendingEvent, Payload: workflowAdvancePendingPayload(domain.WorkflowAdvance{
			SessionID: "run-1", NodeRunID: nodeRunIDPtr("node-1"), CurrentNodeID: "expr-1", Expr: &domain.WorkflowExpr{Script: `{ok: true}`},
		})},
		{ID: "command-2", SessionID: &sessionID, Type: workflowSystemAdvancePendingEvent, Payload: workflowAdvancePendingPayload(domain.WorkflowAdvance{
			SessionID: "run-1", NodeRunID: nodeRunIDPtr("node-2"), CurrentNodeID: "close", Close: true,
		})},
		{ID: "completed-2", SessionID: &sessionID, Type: workflowSystemAdvanceCompletedEvent, Payload: map[string]any{"commandEventId": "command-2"}},
		{ID: "command-3", SessionID: &sessionID, Type: workflowSystemAdvancePendingEvent, Payload: workflowAdvancePendingPayload(domain.WorkflowAdvance{
			SessionID: "run-1", NodeRunID: nodeRunIDPtr("node-3"), CurrentNodeID: "expr-3", Expr: &domain.WorkflowExpr{Script: `{ok: true}`},
		})},
	}}
	service := New(newFakeRepository(), newFakeProjectRepository("project-1"), WithEvents(events))

	commands, err := service.pendingSystemAdvances(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("pendingSystemAdvances() error = %v", err)
	}
	if len(commands) != 2 || commands[0].commandEventID != "command-1" || commands[1].commandEventID != "command-3" {
		t.Fatalf("pendingSystemAdvances() = %#v", commands)
	}
}

func TestAppliedSystemCommandSkipsProjectionAfterAckFailure(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning}
	nodeRunID := domain.NodeRunID("node-1")
	workflows := &fakeWorkflowStarter{advance: domain.WorkflowAdvance{SessionID: "run-1", Completed: true}}
	events := &fakeEventStore{appendErrs: []error{nil, errors.New("ack unavailable")}}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithEvents(events))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	sequence := 0
	service.generateID = func() (domain.ID, error) {
		sequence++
		return domain.ID(fmt.Sprintf("event-%d", sequence)), nil
	}
	pending := workflowApprovalPostCommitAdvance{
		session: repo.sessions["session-1"], commandEventID: "command-1",
		advance: domain.WorkflowAdvance{SessionID: "run-1", NodeRunID: &nodeRunID, CurrentNodeID: "expr", Expr: &domain.WorkflowExpr{Script: `{ok: true}`}},
	}

	if err := service.executePendingSystemAdvance(context.Background(), pending); err == nil || !strings.Contains(err.Error(), "ack unavailable") {
		t.Fatalf("first executePendingSystemAdvance() error = %v", err)
	}
	if !repo.sessions["session-1"].AppliedSystemCommands["command-1"] || workflows.completeCalls != 1 {
		t.Fatalf("session=%#v completeCalls=%d", repo.sessions["session-1"], workflows.completeCalls)
	}
	session := repo.sessions["session-1"]
	session.Status = domain.StatusRunning
	repo.sessions[session.ID] = session
	pending.session = session
	if err := service.executePendingSystemAdvance(context.Background(), pending); err != nil {
		t.Fatalf("retry executePendingSystemAdvance() error = %v", err)
	}
	if workflows.completeCalls != 1 {
		t.Fatalf("applied command replayed projection: completeCalls=%d", workflows.completeCalls)
	}
	foundAck := false
	for _, event := range events.snapshot() {
		foundAck = foundAck || event.Type == workflowSystemAdvanceCompletedEvent && event.Payload["commandEventId"] == "command-1"
	}
	if !foundAck {
		t.Fatalf("completion ack missing: %#v", events.snapshot())
	}
}

func TestCloseSystemCommandPersistsLedgerOnlyWithFinalClosedProjection(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning}
	var stages []domain.Session
	repo.saveHook = func(session domain.Session) error {
		stages = append(stages, session)
		return nil
	}
	events := &fakeEventStore{}
	processes := newFakeProcessRepository()
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithProcesses(processes, nil), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}))
	sequence := 0
	service.generateID = func() (domain.ID, error) {
		sequence++
		return domain.ID(fmt.Sprintf("event-%d", sequence)), nil
	}

	if _, err := service.closeWorkflowSession(context.Background(), CloseSessionInput{SessionID: "session-1", Reason: domain.CloseReasonWorkflowClosed, appliedSystemCommandID: "command-close"}); err != nil {
		t.Fatalf("closeWorkflowSession() error = %v", err)
	}
	var stopping, closed *domain.Session
	for index := range stages {
		stage := &stages[index]
		if stage.Status == domain.StatusStopping {
			stopping = stage
		}
		if stage.Status == domain.StatusClosed {
			closed = stage
		}
	}
	if stopping == nil || stopping.AppliedSystemCommands["command-close"] {
		t.Fatalf("stopping stage contains applied ledger: %#v", stopping)
	}
	if closed == nil || !closed.AppliedSystemCommands["command-close"] {
		t.Fatalf("closed stage lacks applied ledger: %#v", closed)
	}
}

func TestRecoverCloseSystemCommandAfterPreparedStoppingCrash(t *testing.T) {
	repo := newFakeRepository()
	reason := domain.CloseReasonWorkflowClosed
	session := domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow,
		Status: domain.StatusStopping, CloseReason: &reason,
	}
	repo.sessions[session.ID] = session
	repo.listSessions = []domain.Session{session}
	eventSessionID := eventdomain.SessionID(session.ID)
	events := &fakeEventStore{events: []eventdomain.DomainEvent{{
		ID: "command-close", SessionID: &eventSessionID, Type: workflowSystemAdvancePendingEvent,
		Payload: workflowAdvancePendingPayload(domain.WorkflowAdvance{SessionID: "run-1", NodeRunID: nodeRunIDPtr("node-close"), Close: true}),
	}}}
	processes := newFakeProcessRepository()
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithProcesses(processes, nil), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, processes: processes, events: events}}))
	sequence := 0
	service.generateID = func() (domain.ID, error) {
		sequence++
		return domain.ID(fmt.Sprintf("event-%d", sequence)), nil
	}

	handled, err := service.recoverAllPendingSystemAdvances(context.Background())
	if err != nil {
		t.Fatalf("recoverAllPendingSystemAdvances() error = %v", err)
	}
	recovered := repo.sessions[session.ID]
	if !handled[session.ID] || recovered.Status != domain.StatusClosed || recovered.CloseReason == nil || *recovered.CloseReason != reason {
		t.Fatalf("handled=%#v session=%#v", handled, recovered)
	}
	if recovered.AppliedSystemCommands["command-close"] {
		t.Fatalf("acknowledged command remains in applied ledger: %#v", recovered.AppliedSystemCommands)
	}
	foundAck := false
	for _, event := range events.snapshot() {
		foundAck = foundAck || event.Type == workflowSystemAdvanceCompletedEvent && event.Payload["commandEventId"] == "command-close"
	}
	if !foundAck {
		t.Fatalf("completion ack missing: %#v", events.snapshot())
	}
}

func TestRecoverAllPendingSystemAdvancesUsesStableSnapshotOverPageSize(t *testing.T) {
	repo := newFakeRepository()
	events := &fakeEventStore{}
	for index := 0; index < maxPageSize+1; index++ {
		id := domain.ID(fmt.Sprintf("session-%03d", index))
		session := domain.Session{ID: id, ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusCompleted, AppliedSystemCommands: map[string]bool{fmt.Sprintf("command-%03d", index): true}}
		repo.sessions[id] = session
		repo.listSessions = append(repo.listSessions, session)
		eventSessionID := eventdomain.SessionID(id)
		events.events = append(events.events, eventdomain.DomainEvent{
			ID: eventdomain.ID(fmt.Sprintf("command-%03d", index)), SessionID: &eventSessionID, Type: workflowSystemAdvancePendingEvent,
			Payload: workflowAdvancePendingPayload(domain.WorkflowAdvance{SessionID: "run-1", NodeRunID: nodeRunIDPtr("node-1"), Close: true}),
		})
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithEvents(events), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events}}))
	sequence := 0
	service.generateID = func() (domain.ID, error) {
		sequence++
		return domain.ID(fmt.Sprintf("ack-%03d", sequence)), nil
	}

	handled, err := service.recoverAllPendingSystemAdvances(context.Background())
	if err != nil {
		t.Fatalf("recoverAllPendingSystemAdvances() error = %v", err)
	}
	if len(handled) != maxPageSize+1 {
		t.Fatalf("handled sessions = %d", len(handled))
	}
}

func TestConsecutiveMergeNodesUseDistinctPendingCommands(t *testing.T) {
	repo := newFakeRepository()
	session := domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning, WorktreePath: "/tmp/worktree", BaseBranch: "main"}
	repo.sessions[session.ID] = session
	node1, node2 := domain.NodeRunID("node-1"), domain.NodeRunID("node-2")
	workflows := &fakeWorkflowStarter{completeAdvances: []domain.WorkflowAdvance{
		{SessionID: "run-1", NodeRunID: &node2, CurrentNodeID: "merge-2", Merge: &domain.WorkflowMerge{Strategy: "merge"}},
		{SessionID: "run-1", Completed: true},
	}}
	merge := &fakeMergePort{result: gitdiffdomain.MergeResult{Status: "merged", Strategy: "merge", BaseBranch: "main", MergeCommit: "merge"}}
	eventSessionID := eventdomain.SessionID(session.ID)
	events := &fakeEventStore{events: []eventdomain.DomainEvent{{
		ID: "command-1", SessionID: &eventSessionID, Type: workflowSystemAdvancePendingEvent,
		Payload: workflowAdvancePendingPayload(domain.WorkflowAdvance{SessionID: "run-1", NodeRunID: &node1, CurrentNodeID: "merge-1", Merge: &domain.WorkflowMerge{Strategy: "merge"}}),
	}}}
	service := New(repo, newFakeProjectRepository("project-1"), WithWorkflows(workflows), WithMergePort(merge), WithEvents(events), WithProcesses(newFakeProcessRepository(), nil), WithUnitOfWork(&fakeUnitOfWork{tx: fakeTx{sessions: repo, events: events, processes: newFakeProcessRepository()}}))
	sequence := 0
	service.generateID = func() (domain.ID, error) {
		sequence++
		return domain.ID(fmt.Sprintf("event-%d", sequence)), nil
	}

	if _, err := service.recoverPendingSystemAdvance(context.Background(), session); err != nil {
		t.Fatalf("recoverPendingSystemAdvance() error = %v", err)
	}
	if merge.mergeCalls != 2 || len(repo.mergeRecords) != 2 || repo.mergeRecords[0].ID == repo.mergeRecords[1].ID {
		t.Fatalf("mergeCalls=%d records=%#v", merge.mergeCalls, repo.mergeRecords)
	}
	pendingIDs := []eventdomain.ID{}
	for _, event := range events.snapshot() {
		if event.Type == workflowSystemAdvancePendingEvent {
			pendingIDs = append(pendingIDs, event.ID)
		}
	}
	if len(pendingIDs) != 2 || pendingIDs[0] == pendingIDs[1] {
		t.Fatalf("pending command IDs = %#v", pendingIDs)
	}
}

func nodeRunIDPtr(value domain.NodeRunID) *domain.NodeRunID { return &value }

func TestMarkInterruptedSessionsRecoverableAppliesPersistedWorkflowFailure(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	session := domain.Session{
		ID:             "session-1",
		ProjectID:      "project-1",
		Mode:           domain.ModeWorkflow,
		Status:         domain.StatusRunning,
		CodexSessionID: "codex-1",
	}
	repo.sessions[session.ID] = session
	repo.interruptedSessions = []domain.Session{session}
	eventSessionID := eventdomain.SessionID(session.ID)
	events := &fakeEventStore{events: []eventdomain.DomainEvent{{
		ID:        "event-1",
		SessionID: &eventSessionID,
		Type:      "workflow.exit_pending",
		Payload: workflowProcessExitPayload(domain.WorkflowProcessExitInput{
			SessionID: "session-1",
			NodeRunID: "node-run-1",
			Output:    map[string]any{"processRunId": "process-run-1", "exit": "completed"},
		}),
	}}}
	failedAdvance := domain.WorkflowAdvance{SessionID: "session-1", Status: "failed"}
	workflows := &fakeWorkflowStarter{recoverAdvance: &failedAdvance}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), nil), WithEvents(events), WithWorkflows(workflows))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	service.generateID = func() (domain.ID, error) { return "event-2", nil }

	if _, err := service.MarkInterruptedSessionsRecoverable(ctx); err != nil {
		t.Fatalf("MarkInterruptedSessionsRecoverable() error = %v", err)
	}
	if got := repo.sessions[session.ID].Status; got != domain.StatusFailed {
		t.Fatalf("session status = %q", got)
	}
	gotEvents := events.snapshot()
	if len(gotEvents) < 2 || gotEvents[len(gotEvents)-2].Type != "workflow.failed" || gotEvents[len(gotEvents)-1].Type != sessionStatusUpdatedEvent {
		t.Fatalf("failure events = %#v", gotEvents)
	}
}

func TestHandleCodexProcessExitRetriesOnlyFailedProcessRun(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:        "session-1",
		ProjectID: "project-1",
		Status:    domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	processes.markExitedFailures = 1
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, nil), WithEvents(events))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}

	service.handleCodexProcessExit(
		repo.sessions["session-1"],
		processdomain.CodexHandle{ProcessRunID: "process-run-1"},
		codexStartOptions{},
		processdomain.ExitResult{},
		nil,
	)

	if processes.markExitedCalls != 2 || processes.exitedID != "process-run-1" {
		t.Fatalf("mark exited calls=%d id=%q", processes.markExitedCalls, processes.exitedID)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusStopped {
		t.Fatalf("session status = %q", got)
	}
	gotEvents := events.snapshot()
	requireSessionEventTypes(t, gotEvents, "process.exited", "session.stopped", sessionStatusUpdatedEvent)
}

func TestHandleCodexProcessExitStopsRetryingWhenServiceCloses(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	processes.markExitedFailures = 100
	retryEntered := make(chan struct{})
	var retryOnce sync.Once
	processes.markExitedHook = func() {
		retryOnce.Do(func() { close(retryEntered) })
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, nil), WithEvents(&fakeEventStore{}))
	service.processExitDelay = func(int) time.Duration { return time.Hour }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		service.handleCodexProcessExit(
			repo.sessions["session-1"],
			processdomain.CodexHandle{ProcessRunID: "process-run-1"},
			codexStartOptions{},
			processdomain.ExitResult{},
			nil,
		)
	}()
	<-retryEntered
	service.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleCodexProcessExit() did not stop after service close")
	}
}

func TestWorkflowProcessExitRetriesApplyWithoutCompletingNodeAgain(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:          "session-1",
		ProjectID:   "project-1",
		Requirement: "continue workflow",
		Mode:        domain.ModeWorkflow,
		Status:      domain.StatusRunning,
	}
	failedBlockedSave := false
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusBlocked && !failedBlockedSave {
			failedBlockedSave = true
			return errors.New("save blocked session failed")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	workflows := &fakeWorkflowStarter{advance: domain.WorkflowAdvance{
		SessionID:     "session-1",
		Blocked:       true,
		BlockedReason: "blocked",
	}}
	events := &fakeEventStore{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, nil), WithEvents(events), WithWorkflows(workflows))
	service.processExitDelay = func(int) time.Duration { return 0 }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	currentNodeRunID := processdomain.NodeRunID("node-run-1")

	service.handleCodexProcessExit(
		repo.sessions["session-1"],
		processdomain.CodexHandle{ProcessRunID: "process-run-1"},
		codexStartOptions{sessionID: "session-1", nodeRunID: &currentNodeRunID},
		processdomain.ExitResult{},
		nil,
	)

	if workflows.completeCalls != 1 {
		t.Fatalf("CompleteNode() calls = %d, want 1", workflows.completeCalls)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusBlocked {
		t.Fatalf("session status = %q", got)
	}
	var checkpoint *eventdomain.DomainEvent
	for index := range events.events {
		if events.events[index].Type == "workflow.exit_pending" {
			checkpoint = &events.events[index]
			break
		}
	}
	if checkpoint == nil || checkpoint.Payload["sessionId"] != "session-1" || checkpoint.Payload["nodeRunId"] != "node-run-1" {
		t.Fatalf("workflow exit checkpoint = %#v", checkpoint)
	}
}

func TestWorkflowProcessExitRetriesFailureSaveWithoutCompletingNodeAgain(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning}
	failedStatusSave := false
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusFailed && !failedStatusSave {
			failedStatusSave = true
			return errors.New("save failed session failed")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	workflows := &fakeWorkflowStarter{completeErr: errors.New("complete node failed")}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, nil), WithEvents(&fakeEventStore{}), WithWorkflows(workflows))
	service.processExitDelay = func(int) time.Duration { return 0 }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	currentNodeRunID := processdomain.NodeRunID("node-run-1")

	service.handleCodexProcessExit(
		repo.sessions["session-1"],
		processdomain.CodexHandle{ProcessRunID: "process-run-1"},
		codexStartOptions{sessionID: "session-1", nodeRunID: &currentNodeRunID},
		processdomain.ExitResult{},
		nil,
	)

	if workflows.completeCalls != 1 {
		t.Fatalf("CompleteNode() calls = %d, want 1", workflows.completeCalls)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusFailed {
		t.Fatalf("session status = %q", got)
	}
}

func TestWorkflowProcessExitDoesNotReplayExprAdvanceAfterSideEffectFailure(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning}
	failedCompletedSave := false
	repo.saveHook = func(session domain.Session) error {
		if session.Status == domain.StatusCompleted && !failedCompletedSave {
			failedCompletedSave = true
			return errors.New("save completed session failed")
		}
		return nil
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	exprNodeRunID := domain.NodeRunID("node-run-expr")
	workflows := &fakeWorkflowStarter{}
	workflows.advance = domain.WorkflowAdvance{
		SessionID: "session-1",
		NodeRunID: &exprNodeRunID,
		Expr:      &domain.WorkflowExpr{Script: `{status: "ready"}`},
	}
	workflows.completeHook = func() {
		if workflows.completeCalls == 2 {
			workflows.advance = domain.WorkflowAdvance{SessionID: "session-1", Completed: true}
		}
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(processes, nil), WithEvents(&fakeEventStore{}), WithWorkflows(workflows))
	service.processExitDelay = func(int) time.Duration { return 0 }
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	currentNodeRunID := processdomain.NodeRunID("node-run-1")

	service.handleCodexProcessExit(
		repo.sessions["session-1"],
		processdomain.CodexHandle{ProcessRunID: "process-run-1"},
		codexStartOptions{sessionID: "session-1", nodeRunID: &currentNodeRunID},
		processdomain.ExitResult{},
		nil,
	)

	if workflows.completeCalls != 2 {
		t.Fatalf("CompleteNode() calls = %d, want 2", workflows.completeCalls)
	}
	if got := repo.sessions["session-1"].Status; got != domain.StatusFailed {
		t.Fatalf("session status = %q", got)
	}
}

func TestConsumeCodexEventsReleasesWorkdirBeforeExitPersistenceCompletes(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{ID: "session-1", ProjectID: "project-1", Status: domain.StatusRunning}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	retryEntered := make(chan struct{})
	releaseRetry := make(chan struct{})
	var retryOnce sync.Once
	processes.markExitedHook = func() {
		retryOnce.Do(func() {
			close(retryEntered)
			<-releaseRetry
		})
	}
	eventSource := make(chan processdomain.CodexEvent)
	close(eventSource)
	events := &fakeEventStore{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{events: eventSource}),
		WithEvents(events),
	)
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	if !service.reserveWorkdir("/workspace/session-1", "session-1") {
		t.Fatal("reserveWorkdir() = false")
	}

	service.consumeCodexEvents(
		processdomain.CodexHandle{ProcessRunID: "process-run-1"},
		repo.sessions["session-1"],
		codexStartOptions{},
		"/workspace/session-1",
	)
	<-retryEntered
	if !service.reserveWorkdir("/workspace/session-1", "session-2") {
		t.Fatal("workdir remained reserved while exit persistence was retrying")
	}
	service.releaseWorkdir("/workspace/session-1", "session-2")
	close(releaseRetry)
	waitForEventType(t, events, "session.stopped")
}

func TestWorkflowProcessExitKeepsSessionLockThroughAdvance(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = domain.Session{
		ID:          "session-1",
		ProjectID:   "project-1",
		Requirement: "continue workflow",
		Mode:        domain.ModeWorkflow,
		Status:      domain.StatusRunning,
	}
	processes := newFakeProcessRepository()
	processes.active = processdomain.Run{ID: "process-run-1", SessionID: "session-1", Status: processdomain.StatusRunning}
	processes.hasActive = true
	completeEntered := make(chan struct{})
	releaseComplete := make(chan struct{})
	nextNodeRunID := domain.NodeRunID("node-run-2")
	workflows := &fakeWorkflowStarter{
		completeHook: func() {
			close(completeEntered)
			<-releaseComplete
		},
		advance: domain.WorkflowAdvance{
			SessionID:     "session-1",
			NodeRunID:     &nextNodeRunID,
			RequiresCodex: true,
			Prompt:        "continue",
		},
	}
	locker := &mutexSessionLocker{}
	service := New(
		repo,
		newFakeProjectRepository("project-1"),
		WithProcesses(processes, &fakeCodexProcess{}),
		WithEvents(&fakeEventStore{}),
		WithWorkflows(workflows),
		WithSessionLocker(locker),
	)
	nextID := 0
	service.generateID = func() (domain.ID, error) {
		nextID++
		return domain.ID(fmt.Sprintf("event-%d", nextID)), nil
	}
	currentNodeRunID := processdomain.NodeRunID("node-run-1")
	exitDone := make(chan struct{})
	go func() {
		defer close(exitDone)
		service.handleCodexProcessExit(
			repo.sessions["session-1"],
			processdomain.CodexHandle{ProcessRunID: "process-run-1"},
			codexStartOptions{sessionID: "session-1", nodeRunID: &currentNodeRunID},
			processdomain.ExitResult{},
			nil,
		)
	}()
	<-completeEntered
	stopDone := make(chan error, 1)
	go func() {
		_, err := service.StopSession(context.Background(), "session-1")
		stopDone <- err
	}()
	select {
	case err := <-stopDone:
		t.Fatalf("StopSession() returned before workflow advance released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseComplete)
	if err := <-stopDone; err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
	<-exitDone
	if got := repo.sessions["session-1"].Status; got != domain.StatusStopped {
		t.Fatalf("session status = %q", got)
	}
}

type fakeRepository struct {
	saved                        []domain.Session
	sessions                     map[domain.ID]domain.Session
	createErr                    error
	saveErr                      error
	saveHook                     func(domain.Session) error
	listSessions                 []domain.Session
	interruptedSessions          []domain.Session
	listQueuedHook               func()
	listTotal                    int
	lastListQuery                domain.ListQuery
	appends                      []domain.PromptAppend
	deletedAppends               []string
	appendPromptHook             func()
	appendPromptErr              error
	markPromptAppendsInflightErr error
	completePromptAppendsErrs    []error
	completePromptAppendsCalls   int
	mergeRecords                 []domain.MergeRecord
	addMergeRecordErr            error
	stagedAttachments            map[domain.StagedAttachmentID]domain.StagedAttachment
	deleteStagedAttachmentErr    error
	rejectCanceledContext        bool
	updateFilesChangedCalls      int
	updateArtifactCountCalls     int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		sessions:          map[domain.ID]domain.Session{},
		stagedAttachments: map[domain.StagedAttachmentID]domain.StagedAttachment{},
	}
}

func (r *fakeRepository) Create(ctx context.Context, session domain.Session) error {
	if r.rejectCanceledContext && ctx.Err() != nil {
		return ctx.Err()
	}
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

func (r *fakeRepository) Save(ctx context.Context, session domain.Session) error {
	if r.rejectCanceledContext && ctx.Err() != nil {
		return ctx.Err()
	}
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

func (r *fakeRepository) UpdateFilesChanged(_ context.Context, id domain.ID, filesChanged int) error {
	if filesChanged < 0 {
		return errors.New("files changed must be non-negative")
	}
	current, ok := r.sessions[id]
	if !ok {
		return errors.New("not found")
	}
	current.FilesChanged = filesChanged
	r.sessions[id] = current
	r.updateFilesChangedCalls++
	return nil
}

func (r *fakeRepository) UpdateUsage(_ context.Context, id domain.ID, usage domain.TokenUsage) error {
	session := r.sessions[id]
	session.Usage = usage
	r.sessions[id] = session
	return nil
}

func (r *fakeRepository) UpdateArtifactCount(_ context.Context, id domain.ID, artifactCount int) error {
	if artifactCount < 0 {
		return errors.New("artifact count must be non-negative")
	}
	current, ok := r.sessions[id]
	if !ok {
		return errors.New("not found")
	}
	current.ArtifactCount = artifactCount
	r.sessions[id] = current
	r.updateArtifactCountCalls++
	return nil
}

func (r *fakeRepository) Find(_ context.Context, id domain.ID) (domain.Session, error) {
	session, ok := r.sessions[id]
	if !ok {
		return domain.Session{}, errors.New("not found")
	}
	return normalizeFakeSessionWorktree(session), nil
}

func (r *fakeRepository) ListCards(_ context.Context, query domain.ListQuery) ([]domain.Session, int, error) {
	r.lastListQuery = query
	filtered := make([]domain.Session, 0, len(r.listSessions))
	for _, session := range r.listSessions {
		session = normalizeFakeSessionWorktree(session)
		if query.Scope != "" && string(session.Status) != query.Scope {
			continue
		}
		filtered = append(filtered, session)
	}
	total := r.listTotal
	if total == 0 {
		total = len(filtered)
	}
	page, pageSize := query.Page, query.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = len(filtered)
	}
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return []domain.Session{}, total, nil
	}
	end := min(start+pageSize, len(filtered))
	return append([]domain.Session(nil), filtered[start:end]...), total, nil
}

func (r *fakeRepository) ListQueued(context.Context) ([]domain.Session, error) {
	queued := make([]domain.Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		session = normalizeFakeSessionWorktree(session)
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

func (r *fakeRepository) ListProvisioningWorktrees(context.Context, int) ([]domain.Session, error) {
	result := make([]domain.Session, 0)
	for _, session := range r.sessions {
		if session.WorktreeCleanup.Status == domain.WorktreeCleanupProvisioning {
			result = append(result, session)
		}
	}
	slices.SortFunc(result, func(left, right domain.Session) int { return strings.Compare(string(left.ID), string(right.ID)) })
	return result, nil
}

func (r *fakeRepository) ListWorktreeCleanupDue(_ context.Context, now time.Time, _ int) ([]domain.Session, error) {
	result := make([]domain.Session, 0)
	for _, session := range r.sessions {
		if worktreeCleanupDue(session, now) {
			result = append(result, session)
		}
	}
	slices.SortFunc(result, func(left, right domain.Session) int { return strings.Compare(string(left.ID), string(right.ID)) })
	return result, nil
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

func (r *fakeRepository) UpdatePendingPromptAppendBody(_ context.Context, sessionID domain.ID, id string, body string) (domain.PromptAppend, bool, error) {
	for index := range r.appends {
		promptAppend := &r.appends[index]
		if promptAppend.ID != id || promptAppend.SessionID != sessionID || promptAppend.Status != domain.PromptAppendPending {
			continue
		}
		promptAppend.Body = body
		return *promptAppend, true, nil
	}
	return domain.PromptAppend{}, false, nil
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

func (r *fakeRepository) ListPendingPromptAppends(_ context.Context, sessionID domain.ID) ([]domain.PromptAppend, error) {
	appends := make([]domain.PromptAppend, 0, len(r.appends))
	for _, promptAppend := range r.appends {
		if promptAppend.SessionID == sessionID && promptAppend.Status == domain.PromptAppendPending {
			appends = append(appends, promptAppend)
		}
	}
	return appends, nil
}

func (r *fakeRepository) MarkPromptAppendsInflight(_ context.Context, ids []string, processRunID string) error {
	if r.markPromptAppendsInflightErr != nil {
		return r.markPromptAppendsInflightErr
	}
	selected := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		selected[id] = struct{}{}
	}
	for index := range r.appends {
		if _, ok := selected[r.appends[index].ID]; !ok || r.appends[index].Status != domain.PromptAppendPending {
			continue
		}
		r.appends[index].Status = domain.PromptAppendInflight
		r.appends[index].DispatchedProcessRunID = processRunID
	}
	return nil
}

func (r *fakeRepository) CompletePromptAppends(_ context.Context, processRunID string, dispatchedAt time.Time) error {
	r.completePromptAppendsCalls++
	if len(r.completePromptAppendsErrs) > 0 {
		err := r.completePromptAppendsErrs[0]
		r.completePromptAppendsErrs = r.completePromptAppendsErrs[1:]
		if err != nil {
			return err
		}
	}
	for index := range r.appends {
		if r.appends[index].Status != domain.PromptAppendInflight || r.appends[index].DispatchedProcessRunID != processRunID {
			continue
		}
		r.appends[index].Status = domain.PromptAppendDispatched
		value := dispatchedAt
		r.appends[index].DispatchedAt = &value
	}
	return nil
}

func (r *fakeRepository) ReleasePromptAppends(_ context.Context, processRunID string) error {
	for index := range r.appends {
		if r.appends[index].Status != domain.PromptAppendInflight || r.appends[index].DispatchedProcessRunID != processRunID {
			continue
		}
		r.appends[index].Status = domain.PromptAppendPending
		r.appends[index].DispatchedProcessRunID = ""
		r.appends[index].DispatchedAt = nil
	}
	return nil
}

func (r *fakeRepository) AddMergeRecord(_ context.Context, record domain.MergeRecord) error {
	if r.addMergeRecordErr != nil {
		return r.addMergeRecordErr
	}
	r.mergeRecords = append(r.mergeRecords, record)
	return nil
}

func (r *fakeRepository) FindMergeRecord(_ context.Context, id string) (domain.MergeRecord, bool, error) {
	for _, record := range r.mergeRecords {
		if record.ID == id {
			return record, true, nil
		}
	}
	return domain.MergeRecord{}, false, nil
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

type fakeAttachmentStore struct {
	promoted                            map[domain.StagedAttachmentID]bool
	deletedSessions                     map[domain.SessionAttachmentID]bool
	sessionAttachments                  map[domain.SessionAttachmentID]domain.SessionAttachment
	lastPromptAppendAttachmentSessionID domain.ID
	lastPromptAppendAttachmentID        string
	promoteErr                          error
}

type fakeInlineArtifactPublisher struct {
	input       domain.InlineArtifactRequest
	previewKind domain.PreviewKind
	err         error
}

func (p *fakeInlineArtifactPublisher) PublishInlineArtifact(_ context.Context, input domain.InlineArtifactRequest) (domain.SessionAttachment, error) {
	p.input = input
	if p.err != nil {
		return domain.SessionAttachment{}, p.err
	}
	previewKind := p.previewKind
	if previewKind == "" {
		previewKind = domain.PreviewKindImage
	}
	mimeType := "image/png"
	if previewKind == domain.PreviewKindAudio {
		mimeType = "audio/mpeg"
	}
	return domain.SessionAttachment{ID: "artifact-1", MimeType: mimeType, PreviewKind: previewKind}, nil
}

type fakeSessionArtifactStore struct {
	quarantinedToken   string
	restoredQuarantine string
	deletedQuarantine  string
	artifactCount      int
	watchCalls         int
	artifacts          map[domain.SessionFileID]domain.SessionFile
	beforeDelete       func()
	deleteErr          error
}

func (s *fakeSessionArtifactStore) EnsureArtifactDir(context.Context, domain.ID) (string, error) {
	return "/outputs/session-1", nil
}

func (s *fakeSessionArtifactStore) ArtifactDir(domain.ID) string {
	return "/outputs/session-1"
}

func (s *fakeSessionArtifactStore) InspectArtifact(context.Context, domain.InspectArtifactInput) (domain.SessionFile, error) {
	return domain.SessionFile{}, errors.New("unexpected InspectArtifact call")
}

func (s *fakeSessionArtifactStore) WriteInlineArtifact(context.Context, domain.WriteInlineArtifactInput) (domain.SessionFile, error) {
	return domain.SessionFile{}, errors.New("unexpected WriteInlineArtifact call")
}

func (s *fakeSessionArtifactStore) FindArtifact(_ context.Context, id domain.SessionFileID) (domain.SessionFile, error) {
	artifact, ok := s.artifacts[id]
	if !ok {
		return domain.SessionFile{}, domain.ErrSessionFileNotFound
	}
	return artifact, nil
}

func (s *fakeSessionArtifactStore) ListArtifacts(context.Context, domain.ArtifactQuery) ([]domain.SessionFile, error) {
	return nil, nil
}

func (s *fakeSessionArtifactStore) ResolveArtifacts(context.Context, domain.ID, []string) ([]domain.SessionFile, error) {
	return nil, nil
}

func (s *fakeSessionArtifactStore) SumArtifactSize(context.Context, domain.ID) (int64, error) {
	return 0, nil
}

func (s *fakeSessionArtifactStore) CountArtifacts(context.Context, domain.ID) (int, error) {
	return s.artifactCount, nil
}

func (s *fakeSessionArtifactStore) DeleteArtifact(_ context.Context, id domain.SessionFileID) (domain.SessionFile, error) {
	if s.beforeDelete != nil {
		s.beforeDelete()
	}
	if s.deleteErr != nil {
		return domain.SessionFile{}, s.deleteErr
	}
	artifact, ok := s.artifacts[id]
	if !ok {
		return domain.SessionFile{}, domain.ErrSessionFileNotFound
	}
	delete(s.artifacts, id)
	if s.artifactCount > 0 {
		s.artifactCount--
	}
	return artifact, nil
}

func (s *fakeSessionArtifactStore) OpenArtifact(context.Context, domain.SessionFileID) (domain.AttachmentStream, error) {
	return domain.AttachmentStream{}, domain.ErrSessionFileNotFound
}

func (s *fakeSessionArtifactStore) WatchArtifactDir(ctx context.Context, _ domain.ID) (<-chan struct{}, error) {
	s.watchCalls++
	changes := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(changes)
	}()
	return changes, nil
}

func (s *fakeSessionArtifactStore) QuarantineArtifactDir(_ context.Context, sessionID domain.ID, token string) (string, error) {
	s.quarantinedToken = token
	return "/trash/" + string(sessionID) + "/" + token, nil
}

func (s *fakeSessionArtifactStore) RestoreArtifactDir(_ context.Context, _ domain.ID, quarantinePath string) error {
	s.restoredQuarantine = quarantinePath
	return nil
}

func (s *fakeSessionArtifactStore) DeleteQuarantine(_ context.Context, quarantinePath string) error {
	s.deletedQuarantine = quarantinePath
	return nil
}

func (s *fakeSessionArtifactStore) ListArtifactQuarantines(context.Context) ([]domain.ArtifactQuarantine, error) {
	return nil, nil
}

func (s *fakeSessionArtifactStore) ListArtifactOutputDirectories(context.Context) ([]domain.ArtifactOutputDirectory, error) {
	return nil, nil
}

func (s *fakeSessionArtifactStore) DeleteArtifactOutputDirectory(context.Context, domain.ID) error {
	return nil
}

func newFakeAttachmentStore() *fakeAttachmentStore {
	return &fakeAttachmentStore{
		promoted:           map[domain.StagedAttachmentID]bool{},
		deletedSessions:    map[domain.SessionAttachmentID]bool{},
		sessionAttachments: map[domain.SessionAttachmentID]domain.SessionAttachment{},
	}
}

func (s *fakeAttachmentStore) Stage(context.Context, domain.StageAttachmentInput) (domain.StagedAttachment, error) {
	return domain.StagedAttachment{}, errors.New("unexpected Stage call")
}

func (s *fakeAttachmentStore) Promote(_ context.Context, input domain.PromoteAttachmentInput) (domain.SessionAttachment, error) {
	if s.promoteErr != nil {
		return domain.SessionAttachment{}, s.promoteErr
	}
	staged := input.Staged
	s.promoted[staged.ID] = true
	attachment := domain.SessionAttachment{
		ID:          domain.SessionAttachmentID(staged.ID),
		SessionID:   input.SessionID,
		Role:        domain.FileRoleInput,
		SourceType:  input.SourceType,
		SourceID:    input.SourceID,
		Kind:        "file",
		Filename:    staged.Filename,
		Path:        "/attachments/sessions/" + string(input.SessionID) + "/" + string(staged.ID) + "/" + staged.Filename,
		MimeType:    staged.MimeType,
		Size:        staged.Size,
		Previewable: staged.Previewable,
		CreatedAt:   time.Unix(11, 0).UTC(),
	}
	s.sessionAttachments[attachment.ID] = attachment
	return attachment, nil
}

func (s *fakeAttachmentStore) DeleteStaged(context.Context, domain.StagedAttachmentID) error {
	return errors.New("unexpected DeleteStaged call")
}

func (s *fakeAttachmentStore) DeleteSession(ctx context.Context, id domain.SessionAttachmentID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.deletedSessions[id] = true
	delete(s.sessionAttachments, id)
	return nil
}

func (s *fakeAttachmentStore) FindSessionFile(_ context.Context, id domain.SessionFileID) (domain.SessionFile, error) {
	attachment, ok := s.sessionAttachments[id]
	if !ok {
		return domain.SessionFile{}, domain.ErrSessionFileNotFound
	}
	return attachment, nil
}

func (s *fakeAttachmentStore) ListSessionAttachments(_ context.Context, sessionID domain.ID) ([]domain.SessionAttachment, error) {
	attachments := make([]domain.SessionAttachment, 0)
	for _, attachment := range s.sessionAttachments {
		if attachment.SessionID == sessionID && attachment.Role == domain.FileRoleInput {
			attachments = append(attachments, attachment)
		}
	}
	return attachments, nil
}

func (s *fakeAttachmentStore) ListPromptAppendAttachments(_ context.Context, sessionID domain.ID, appendID string) ([]domain.SessionAttachment, error) {
	s.lastPromptAppendAttachmentSessionID = sessionID
	s.lastPromptAppendAttachmentID = appendID
	attachments := make([]domain.SessionAttachment, 0)
	for _, attachment := range s.sessionAttachments {
		if attachment.SessionID == sessionID && attachment.SourceType == domain.AttachmentSourcePromptAppend && attachment.SourceID == appendID {
			attachments = append(attachments, attachment)
		}
	}
	return attachments, nil
}

func (s *fakeAttachmentStore) Open(context.Context, string) (domain.AttachmentStream, error) {
	return domain.AttachmentStream{}, errors.New("unexpected Open call")
}

type fakeWorktreeManager struct {
	path              string
	headCommit        string
	createErr         error
	headCommitErr     error
	removeErr         error
	deleteBranchErr   error
	retainCommitErr   error
	releaseOwnerErr   error
	statErr           error
	inspectErr        error
	ownership         *domain.WorktreeOwnership
	createCalled      bool
	createProjectPath string
	createProjectID   domain.ProjectID
	createSessionID   domain.ID
	createBaseBranch  string
	headCommitPath    string
	headCommitRef     string
	removed           []string
	deletedBranches   []string
	retainedCommits   []string
	releasedOwnership []string
	operations        []string
	missingPaths      map[string]bool
	onCreate          func()
	createStarted     chan struct{}
	releaseCreate     <-chan struct{}
}

type fakeWorktreeInitializer struct {
	called       bool
	worktreePath string
	script       string
	result       domain.WorktreeInitResult
	err          error
	onRun        func()
}

func (r *fakeWorktreeInitializer) Run(_ context.Context, worktreePath string, script string) (domain.WorktreeInitResult, error) {
	r.called = true
	r.worktreePath = worktreePath
	r.script = script
	if r.onRun != nil {
		r.onRun()
	}
	return r.result, r.err
}

func intPointer(value int) *int {
	return &value
}

func newFakeWorktreeManager() *fakeWorktreeManager {
	return &fakeWorktreeManager{missingPaths: map[string]bool{}}
}

func (m *fakeWorktreeManager) Create(ctx context.Context, projectPath string, projectID domain.ProjectID, sessionID domain.ID, branch string, baseBranch string, ownershipToken string) (string, error) {
	m.createCalled = true
	m.createProjectPath = projectPath
	m.createProjectID = projectID
	m.createSessionID = sessionID
	if branch != string(sessionID) {
		return "", fmt.Errorf("unexpected worktree branch %q", branch)
	}
	if strings.TrimSpace(ownershipToken) == "" {
		return "", errors.New("unexpected empty worktree ownership token")
	}
	m.createBaseBranch = baseBranch
	if m.onCreate != nil {
		m.onCreate()
	}
	if m.createStarted != nil {
		close(m.createStarted)
	}
	if m.releaseCreate != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-m.releaseCreate:
		}
	}
	if m.createErr != nil {
		return "", m.createErr
	}
	return m.PathForSession(projectID, sessionID), nil
}

func (m *fakeWorktreeManager) HeadCommit(_ context.Context, path string, ref string) (string, error) {
	m.headCommitPath = path
	m.headCommitRef = ref
	if m.headCommitErr != nil {
		return "", m.headCommitErr
	}
	return m.headCommit, nil
}

func (m *fakeWorktreeManager) InspectOwnership(_ context.Context, _ string, path string, _ string, _ string) (domain.WorktreeOwnership, error) {
	if m.inspectErr != nil {
		return domain.WorktreeOwnership{}, m.inspectErr
	}
	if m.statErr != nil {
		return domain.WorktreeOwnership{}, m.statErr
	}
	if m.ownership != nil {
		return *m.ownership, nil
	}
	exists := !m.missingPaths[path]
	return domain.WorktreeOwnership{
		PathExists:   exists,
		BranchExists: true,
		Registered:   exists,
		MarkerExists: true,
		TokenMatches: true,
		Matches:      exists,
	}, nil
}

func (m *fakeWorktreeManager) Remove(_ context.Context, path string) error {
	m.operations = append(m.operations, "remove")
	m.removed = append(m.removed, path)
	if m.removeErr != nil {
		return m.removeErr
	}
	m.setMissing(path, true)
	return nil
}

func (m *fakeWorktreeManager) DeleteBranch(_ context.Context, projectPath string, branch string) error {
	m.operations = append(m.operations, "delete_branch")
	m.deletedBranches = append(m.deletedBranches, projectPath+":"+branch)
	if m.deleteBranchErr != nil {
		return m.deleteBranchErr
	}
	return nil
}

func (m *fakeWorktreeManager) RetainCommit(_ context.Context, projectPath string, sessionID domain.ID, commit string) error {
	m.operations = append(m.operations, "retain")
	m.retainedCommits = append(m.retainedCommits, projectPath+":"+string(sessionID)+":"+commit)
	return m.retainCommitErr
}

func (m *fakeWorktreeManager) ReleaseOwnership(_ context.Context, path string, ownershipToken string) error {
	m.operations = append(m.operations, "release_ownership")
	m.releasedOwnership = append(m.releasedOwnership, path+":"+ownershipToken)
	return m.releaseOwnerErr
}

func (m *fakeWorktreeManager) PathForSession(projectID domain.ProjectID, sessionID domain.ID) string {
	if m.path == "" {
		return fmt.Sprintf("/data/worktrees/%s/%s", projectID, sessionID)
	}
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
	m.removed = nil
	m.deletedBranches = nil
	m.retainedCommits = nil
	m.releasedOwnership = nil
	m.operations = nil
}

func normalizeFakeSessionWorktree(session domain.Session) domain.Session {
	if strings.TrimSpace(session.BaseBranch) != "" && session.WorktreeCleanup.Status == "" {
		session.WorktreeBranch = string(session.ID)
		session.WorktreeCleanup.Status = domain.WorktreeCleanupActive
		session.WorktreeCleanup.OwnershipToken = "test-owner-token"
		confirmedAt := session.UpdatedAt
		session.WorktreeCleanup.OwnershipConfirmedAt = &confirmedAt
	}
	return session
}

type fakeMergePort struct {
	mergeInput   gitdiffdomain.MergeInput
	rebaseInput  gitdiffdomain.RebaseInput
	result       gitdiffdomain.MergeResult
	err          error
	mergeCalled  bool
	mergeCalls   int
	rebaseCalled bool
	abortPath    string
	abortErr     error
}

func (m *fakeMergePort) MergeToBase(_ context.Context, input gitdiffdomain.MergeInput) (gitdiffdomain.MergeResult, error) {
	m.mergeCalled = true
	m.mergeCalls++
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
	markFailCalls     int
	markFailErrs      []error
	markFailHook      func(int)
	completeInput     domain.WorkflowNodeCompleteInput
	advance           domain.WorkflowAdvance
	completeAdvances  []domain.WorkflowAdvance
	completeErr       error
	completeHook      func()
	completeCalls     int
	recoverInput      domain.WorkflowProcessExitInput
	recoverAdvance    *domain.WorkflowAdvance
	recoverErr        error
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
	s.markFailCalls++
	if s.markFailHook != nil {
		s.markFailHook(s.markFailCalls)
	}
	if len(s.markFailErrs) > 0 {
		err := s.markFailErrs[0]
		s.markFailErrs = s.markFailErrs[1:]
		return err
	}
	return s.markFailErr
}

func (s *fakeWorkflowStarter) MarkResumeFailedForSession(_ context.Context, input domain.WorkflowResumeFailureInput) (domain.WorkflowRunSnapshot, error) {
	s.resumeInput = input
	if s.resumeErr != nil {
		return domain.WorkflowRunSnapshot{}, s.resumeErr
	}
	return s.resumeSnapshot, nil
}

func (s *fakeWorkflowStarter) MarkResumeFailedForSessionWithRepositories(ctx context.Context, input domain.WorkflowResumeFailureInput, _ workflowdomain.Repository, _ eventdomain.Store) (domain.WorkflowRunSnapshot, []eventdomain.DomainEvent, error) {
	result, err := s.MarkResumeFailedForSession(ctx, input)
	return result, nil, err
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
	s.completeCalls++
	s.completeInput = input
	if s.completeHook != nil {
		s.completeHook()
	}
	if s.completeErr != nil {
		return domain.WorkflowAdvance{}, s.completeErr
	}
	if len(s.completeAdvances) > 0 {
		advance := s.completeAdvances[0]
		s.completeAdvances = s.completeAdvances[1:]
		return advance, nil
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

func (s *fakeWorkflowStarter) RecoverProcessExit(ctx context.Context, input domain.WorkflowProcessExitInput) (domain.WorkflowAdvance, error) {
	s.recoverInput = input
	if s.recoverErr != nil {
		return domain.WorkflowAdvance{}, s.recoverErr
	}
	if s.recoverAdvance != nil {
		return *s.recoverAdvance, nil
	}
	if input.Failed {
		return s.FailNode(ctx, domain.WorkflowNodeFailInput{
			SessionID: input.SessionID,
			NodeRunID: input.NodeRunID,
			Code:      input.FailureCode,
			Message:   input.FailureMessage,
			Output:    input.Output,
		})
	}
	return s.CompleteNode(ctx, domain.WorkflowNodeCompleteInput{
		SessionID: input.SessionID,
		NodeRunID: input.NodeRunID,
		Output:    input.Output,
	})
}

func (s *fakeWorkflowStarter) SubmitApprovalForSession(_ context.Context, input domain.WorkflowApprovalInput) (domain.WorkflowApprovalResult, error) {
	s.approvalInput = input
	if s.approvalErr != nil {
		return domain.WorkflowApprovalResult{}, s.approvalErr
	}
	return s.approvalResult, nil
}

func (s *fakeWorkflowStarter) SubmitApprovalForSessionWithRepositories(ctx context.Context, input domain.WorkflowApprovalInput, _ workflowdomain.Repository, _ eventdomain.Store) (domain.WorkflowApprovalResult, []eventdomain.DomainEvent, error) {
	result, err := s.SubmitApprovalForSession(ctx, input)
	return result, nil, err
}

type fakeProcessRepository struct {
	mu                        sync.RWMutex
	created                   []processdomain.Run
	active                    processdomain.Run
	activeBySession           map[processdomain.SessionID]processdomain.Run
	hasActive                 bool
	activeCount               int
	runningID                 processdomain.RunID
	runningPID                int
	runningCodex              string
	bindErr                   error
	stoppingID                processdomain.RunID
	exitedID                  processdomain.RunID
	exitedResult              processdomain.ExitResult
	markExitedCalls           int
	markExitedFailures        int
	markExitedHook            func()
	markExitedDoneHook        func()
	transcriptSources         map[string]processdomain.CodexTranscriptSource
	transcriptMissing         bool
	transcriptLookupSessionID processdomain.SessionID
}

func newFakeProcessRepository() *fakeProcessRepository {
	return &fakeProcessRepository{transcriptSources: map[string]processdomain.CodexTranscriptSource{}}
}

func (r *fakeProcessRepository) CreateRun(_ context.Context, run processdomain.Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, run)
	r.active = run
	r.hasActive = true
	return nil
}

func (r *fakeProcessRepository) HasAnyBySession(_ context.Context, sessionID processdomain.SessionID) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, run := range r.created {
		if run.SessionID == sessionID {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeProcessRepository) FindRun(_ context.Context, id processdomain.RunID) (processdomain.Run, error) {
	if r.active.ID == id {
		return r.active, nil
	}
	for _, run := range r.created {
		if run.ID == id {
			return run, nil
		}
	}
	return processdomain.Run{}, errors.New("process run not found")
}

func (r *fakeProcessRepository) FindActiveBySession(_ context.Context, sessionID processdomain.SessionID) (processdomain.Run, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if active, ok := r.activeBySession[sessionID]; ok {
		return active, true, nil
	}
	if r.hasActive && r.active.SessionID == sessionID {
		return r.active, true, nil
	}
	return processdomain.Run{}, false, nil
}

func (r *fakeProcessRepository) FindLatestBySession(_ context.Context, sessionID processdomain.SessionID) (processdomain.Run, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if active, ok := r.activeBySession[sessionID]; ok {
		return active, true, nil
	}
	for index := len(r.created) - 1; index >= 0; index-- {
		if r.created[index].SessionID == sessionID {
			return r.created[index], true, nil
		}
	}
	if r.active.SessionID == sessionID && r.active.ID != "" {
		return r.active, true, nil
	}
	return processdomain.Run{}, false, nil
}

func (r *fakeProcessRepository) CountActive(context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.activeCount > 0 {
		return r.activeCount, nil
	}
	if r.hasActive && (r.active.Status == processdomain.StatusStarting || r.active.Status == processdomain.StatusRunning) {
		return 1, nil
	}
	count := 0
	for _, active := range r.activeBySession {
		if active.Status == processdomain.StatusStarting || active.Status == processdomain.StatusRunning {
			count++
		}
	}
	if count > 0 {
		return count, nil
	}
	return 0, nil
}

func (r *fakeProcessRepository) MarkWaitingUser(_ context.Context, id processdomain.RunID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active.ID = id
	r.active.Status = processdomain.StatusWaitingUser
	r.hasActive = true
	return nil
}

func (r *fakeProcessRepository) MarkStarted(_ context.Context, id processdomain.RunID, pid int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active.ID = id
	r.active.PID = &pid
	r.active.Status = processdomain.StatusStarting
	r.hasActive = true
	return nil
}

func (r *fakeProcessRepository) BindTranscript(_ context.Context, id processdomain.RunID, pid int, source processdomain.CodexTranscriptSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bindErr != nil {
		return r.bindErr
	}
	r.runningID = id
	r.runningPID = pid
	r.runningCodex = source.CodexSessionID
	r.active.ID = id
	r.active.Status = processdomain.StatusRunning
	r.active.PID = &pid
	r.active.CodexSessionID = source.CodexSessionID
	r.hasActive = true
	if r.transcriptSources == nil {
		r.transcriptSources = map[string]processdomain.CodexTranscriptSource{}
	}
	r.transcriptSources[source.CodexSessionID] = source
	return nil
}

func (r *fakeProcessRepository) FindTranscriptSource(_ context.Context, sessionID processdomain.SessionID, codexSessionID string) (processdomain.CodexTranscriptSource, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transcriptLookupSessionID = sessionID
	if r.transcriptMissing {
		return processdomain.CodexTranscriptSource{}, false, nil
	}
	source, ok := r.transcriptSources[codexSessionID]
	if !ok && codexSessionID != "" {
		return processdomain.CodexTranscriptSource{
			CodexSessionID: codexSessionID,
			RelativePath:   "test/" + codexSessionID + ".jsonl",
			BoundAt:        time.Now().UTC(),
		}, true, nil
	}
	return source, ok, nil
}

func (r *fakeProcessRepository) TranscriptSources(_ context.Context, sessionID processdomain.SessionID) ([]processdomain.CodexTranscriptSource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]struct{}{}
	var sources []processdomain.CodexTranscriptSource
	for _, run := range r.created {
		if run.SessionID != sessionID || run.CodexSessionID == "" {
			continue
		}
		if _, ok := seen[run.CodexSessionID]; ok {
			continue
		}
		seen[run.CodexSessionID] = struct{}{}
		if source, ok := r.transcriptSources[run.CodexSessionID]; ok {
			sources = append(sources, source)
		} else {
			sources = append(sources, processdomain.CodexTranscriptSource{
				CodexSessionID: run.CodexSessionID,
				RelativePath:   "test/" + run.CodexSessionID + ".jsonl",
				BoundAt:        time.Now().UTC(),
			})
		}
	}
	return sources, nil
}

func (r *fakeProcessRepository) MarkRunning(_ context.Context, id processdomain.RunID, pid int, codexSessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *fakeProcessRepository) MarkStopping(_ context.Context, id processdomain.RunID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stoppingID = id
	r.active.ID = id
	r.active.Status = processdomain.StatusStopping
	r.hasActive = true
	return nil
}

func (r *fakeProcessRepository) MarkExited(_ context.Context, id processdomain.RunID, result processdomain.ExitResult) error {
	r.mu.Lock()
	r.markExitedCalls++
	hook := r.markExitedHook
	failed := false
	if r.markExitedFailures > 0 {
		r.markExitedFailures--
		failed = true
	}
	r.mu.Unlock()
	if hook != nil {
		hook()
	}
	if failed {
		return errors.New("mark process exited failed")
	}
	r.mu.Lock()
	r.exitedID = id
	r.exitedResult = result
	if r.active.ID == id {
		r.hasActive = false
	}
	for sessionID, active := range r.activeBySession {
		if active.ID == id {
			delete(r.activeBySession, sessionID)
		}
	}
	doneHook := r.markExitedDoneHook
	r.mu.Unlock()
	if doneHook != nil {
		doneHook()
	}
	return nil
}

type fakeQuestionCanceller struct {
	cancelledSessionID questiondomain.SessionID
	cancelReason       string
	cancelErr          error
	onCancel           func()
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
			SessionID: input.SessionID,
			Status:    questiondomain.BatchPending,
			Questions: input.Questions,
			Created:   true,
		}
	}
	return c.batch, nil
}

func (c *fakeQuestionCanceller) CancelPendingBySession(_ context.Context, sessionID questiondomain.SessionID, reason string) error {
	c.cancelledSessionID = sessionID
	c.cancelReason = reason
	if c.onCancel != nil {
		c.onCancel()
	}
	return c.cancelErr
}

func TestRequestUserAnswerReturnsDirectAnswerWithoutStoppingOrigin(t *testing.T) {
	store, service, codex := newAnswerUserWaitTestService(t)
	timeout := make(chan time.Time)
	service.answerUserTimer = func(duration time.Duration) (<-chan time.Time, func()) {
		if duration != answerUserWarmWaitTimeout {
			t.Fatalf("answer_user timeout = %s", duration)
		}
		return timeout, func() {}
	}

	result := make(chan questionapp.BatchDTO, 1)
	errs := make(chan error, 1)
	go func() {
		batch, err := service.RequestUserAnswer(context.Background(), RequestUserAnswerInput{
			SessionID: "session-1",
			Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
		})
		result <- batch
		errs <- err
	}()

	pending := waitForAnswerUserBatch(t, store)
	option := questiondomain.OptionID("yes")
	answered, err := service.SubmitQuestionBatch(context.Background(), questionapp.SubmitBatchInput{
		BatchID: pending.ID,
		Answers: []questiondomain.Answer{{QuestionID: pending.Questions[0].ID, SelectedOptionID: &option}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if answered.DeliveryStatus != questiondomain.DeliveryInflight {
		t.Fatalf("answered batch = %#v", answered)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	returned := <-result
	if returned.Status != questiondomain.BatchAnswered || returned.DeliveryStatus != questiondomain.DeliveryInflight {
		t.Fatalf("returned batch = %#v", returned)
	}
	if codex.stoppedID != "" || codex.resumeCalled {
		t.Fatalf("direct answer stopped or resumed origin: stop=%q resume=%v", codex.stoppedID, codex.resumeCalled)
	}
	if err := service.AcknowledgeUserAnswerDelivery(context.Background(), AcknowledgeUserAnswerDeliveryInput{SessionID: "session-1", BatchID: pending.ID}); err != nil {
		t.Fatal(err)
	}
	session, err := store.Sessions().Find(context.Background(), "session-1")
	if err != nil || session.Status != domain.StatusRunning {
		t.Fatalf("session = %#v, %v", session, err)
	}
	run, err := store.Processes().FindRun(context.Background(), "process-1")
	if err != nil || run.Status != processdomain.StatusRunning {
		t.Fatalf("process = %#v, %v", run, err)
	}
	delivered, err := store.Questions().FindBatch(context.Background(), pending.ID)
	if err != nil || delivered.DeliveryStatus != questiondomain.DeliveryDelivered {
		t.Fatalf("delivered batch = %#v, %v", delivered, err)
	}
}

func TestRequestUserAnswerTimeoutStopsOriginAndKeepsQuestionPending(t *testing.T) {
	store, service, codex := newAnswerUserWaitTestService(t)
	timeout := make(chan time.Time, 1)
	service.answerUserTimer = func(time.Duration) (<-chan time.Time, func()) { return timeout, func() {} }

	done := make(chan questionapp.BatchDTO, 1)
	errs := make(chan error, 1)
	go func() {
		batch, err := service.RequestUserAnswer(context.Background(), RequestUserAnswerInput{
			SessionID: "session-1",
			Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
		})
		done <- batch
		errs <- err
	}()
	pending := waitForAnswerUserBatch(t, store)
	timeout <- time.Now()
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if batch := <-done; batch.ID != pending.ID || batch.Status != questiondomain.BatchPending {
		t.Fatalf("timeout batch = %#v", batch)
	}
	if codex.stoppedID != "process-1" {
		t.Fatalf("stopped process = %q", codex.stoppedID)
	}
	run, err := store.Processes().FindRun(context.Background(), "process-1")
	if err != nil || run.Status != processdomain.StatusExited {
		t.Fatalf("process = %#v, %v", run, err)
	}
	stillPending, err := store.Questions().ListPendingBySession(context.Background(), "session-1")
	if err != nil || len(stillPending) != 1 {
		t.Fatalf("pending batches = %#v, %v", stillPending, err)
	}
	option := questiondomain.OptionID("yes")
	answered, err := service.SubmitQuestionBatch(context.Background(), questionapp.SubmitBatchInput{
		BatchID: pending.ID,
		Answers: []questiondomain.Answer{{QuestionID: pending.Questions[0].ID, SelectedOptionID: &option}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if answered.DeliveryStatus != questiondomain.DeliveryAwaitingResume {
		t.Fatalf("answered batch = %#v", answered)
	}
	session, err := store.Sessions().Find(context.Background(), "session-1")
	if err != nil || session.Status != domain.StatusQueued || session.Queue.Kind != domain.QueueKindAnswerUser || session.Queue.ResumeOfProcessRunID != "process-1" {
		t.Fatalf("queued session = %#v, %v", session, err)
	}
}

func TestRequestUserAnswerTimeoutReturnsAnswerCommittedBeforeTimeout(t *testing.T) {
	store, service, codex := newAnswerUserWaitTestService(t)
	timeout := make(chan time.Time, 1)
	service.answerUserTimer = func(time.Duration) (<-chan time.Time, func()) { return timeout, func() {} }
	quiet := &quietAnswerQuestions{Service: questionapp.New(store.Questions()), updates: make(chan questionapp.BatchDTO)}
	service.questions = quiet

	result := make(chan questionapp.BatchDTO, 1)
	errs := make(chan error, 1)
	go func() {
		batch, err := service.RequestUserAnswer(context.Background(), RequestUserAnswerInput{
			SessionID: "session-1",
			Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
		})
		result <- batch
		errs <- err
	}()

	pending := waitForAnswerUserBatch(t, store)
	option := questiondomain.OptionID("yes")
	if _, err := service.SubmitQuestionBatch(context.Background(), questionapp.SubmitBatchInput{
		BatchID: pending.ID,
		Answers: []questiondomain.Answer{{QuestionID: pending.Questions[0].ID, SelectedOptionID: &option}},
	}); err != nil {
		t.Fatal(err)
	}
	timeout <- time.Now()
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	returned := <-result
	if returned.Status != questiondomain.BatchAnswered || returned.DeliveryStatus != questiondomain.DeliveryInflight {
		t.Fatalf("timeout race returned batch = %#v", returned)
	}
	if codex.stoppedID != "" {
		t.Fatalf("answered origin was stopped: %q", codex.stoppedID)
	}
}

func TestSubmitQuestionBatchPublishesInflightBeforeFallbackSnapshot(t *testing.T) {
	store, service, _ := newAnswerUserWaitTestService(t)
	service.answerUserTimer = func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
	questions := &orderedAnswerQuestions{
		Service:         questionapp.New(store.Questions()),
		inflightEntered: make(chan struct{}),
		releaseInflight: make(chan struct{}),
	}
	service.questions = questions
	requestDone := make(chan error, 1)
	go func() {
		_, err := service.RequestUserAnswer(context.Background(), RequestUserAnswerInput{
			SessionID: "session-1",
			Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
		})
		requestDone <- err
	}()
	pending := waitForAnswerUserBatch(t, store)
	option := questiondomain.OptionID("yes")
	submitDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitQuestionBatch(context.Background(), questionapp.SubmitBatchInput{
			BatchID: pending.ID,
			Answers: []questiondomain.Answer{{QuestionID: pending.Questions[0].ID, SelectedOptionID: &option}},
		})
		submitDone <- err
	}()
	<-questions.inflightEntered
	fallbackDone := make(chan error, 1)
	go func() {
		fallbackDone <- service.fallbackUserAnswer(context.Background(), "session-1", pending.ID, answerUserFallbackTransport)
	}()
	close(questions.releaseInflight)
	if err := <-submitDone; err != nil {
		t.Fatal(err)
	}
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := <-fallbackDone; err != nil {
		t.Fatal(err)
	}
	statuses := questions.snapshot()
	inflightIndex := slices.Index(statuses, questiondomain.DeliveryInflight)
	awaitingIndex := slices.Index(statuses, questiondomain.DeliveryAwaitingResume)
	if inflightIndex < 0 || awaitingIndex < 0 || inflightIndex > awaitingIndex {
		t.Fatalf("delivery publication order = %#v", statuses)
	}
}

func TestWaitForUserAnswerReturnsWhenOriginAlreadyExited(t *testing.T) {
	store, service, _ := newAnswerUserWaitTestService(t)
	batch, origin, err := service.requestUserAnswer(context.Background(), RequestUserAnswerInput{
		SessionID: "session-1",
		Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Processes().MarkExited(context.Background(), origin.ID, processdomain.ExitResult{FinishedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	returned, err := service.waitForUserAnswer(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if returned.ID != batch.ID || time.Since(started) > time.Second {
		t.Fatalf("wait returned %#v after %s", returned, time.Since(started))
	}
}

func TestDirectAnswerTransportCloseFallsBackToResume(t *testing.T) {
	store, service, codex := newAnswerUserWaitTestService(t)
	service.answerUserTimer = func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
	done := make(chan error, 1)
	go func() {
		_, err := service.RequestUserAnswer(context.Background(), RequestUserAnswerInput{
			SessionID: "session-1",
			Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
		})
		done <- err
	}()
	pending := waitForAnswerUserBatch(t, store)
	option := questiondomain.OptionID("yes")
	if _, err := service.SubmitQuestionBatch(context.Background(), questionapp.SubmitBatchInput{
		BatchID: pending.ID,
		Answers: []questiondomain.Answer{{QuestionID: pending.Questions[0].ID, SelectedOptionID: &option}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stopEntered := make(chan struct{})
	releaseStop := make(chan struct{})
	codex.stopHook = func(context.Context, processdomain.RunID) error {
		close(stopEntered)
		<-releaseStop
		return nil
	}
	fallbackDone := make(chan error, 1)
	go func() {
		fallbackDone <- service.fallbackUserAnswer(context.Background(), "session-1", pending.ID, answerUserFallbackTransport)
	}()
	<-stopEntered
	if err := service.AcknowledgeUserAnswerDelivery(context.Background(), AcknowledgeUserAnswerDeliveryInput{SessionID: "session-1", BatchID: pending.ID}); err == nil {
		t.Fatal("late delivery ACK unexpectedly reversed fallback claim")
	}
	close(releaseStop)
	if err := <-fallbackDone; err != nil {
		t.Fatal(err)
	}
	if codex.stoppedID != "process-1" {
		t.Fatalf("stopped process = %q", codex.stoppedID)
	}
	batch, err := store.Questions().FindBatch(context.Background(), pending.ID)
	if err != nil || batch.DeliveryStatus != questiondomain.DeliveryAwaitingResume {
		t.Fatalf("fallback batch = %#v, %v", batch, err)
	}
	session, err := store.Sessions().Find(context.Background(), "session-1")
	if err != nil || session.Status != domain.StatusQueued || session.Queue.Kind != domain.QueueKindAnswerUser {
		t.Fatalf("fallback session = %#v, %v", session, err)
	}
}

func TestAnswerDeliveryCommandsRejectWrongSessionWithoutStoppingOrigin(t *testing.T) {
	store, service, codex := newAnswerUserWaitTestService(t)
	service.answerUserTimer = func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
	result := make(chan questionapp.BatchDTO, 1)
	errs := make(chan error, 1)
	go func() {
		batch, err := service.RequestUserAnswer(context.Background(), RequestUserAnswerInput{
			SessionID: "session-1",
			Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
		})
		result <- batch
		errs <- err
	}()
	pending := waitForAnswerUserBatch(t, store)
	option := questiondomain.OptionID("yes")
	if _, err := service.SubmitQuestionBatch(context.Background(), questionapp.SubmitBatchInput{
		BatchID: pending.ID,
		Answers: []questiondomain.Answer{{QuestionID: pending.Questions[0].ID, SelectedOptionID: &option}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	<-result
	if err := service.AcknowledgeUserAnswerDelivery(context.Background(), AcknowledgeUserAnswerDeliveryInput{SessionID: "wrong-session", BatchID: pending.ID}); err == nil {
		t.Fatal("wrong-session ACK unexpectedly succeeded")
	}
	if err := service.FailUserAnswerDelivery(context.Background(), FailUserAnswerDeliveryInput{SessionID: "wrong-session", BatchID: pending.ID, Kind: UserAnswerDeliveryTransportClosed}); err == nil {
		t.Fatal("wrong-session delivery failure unexpectedly succeeded")
	}
	if codex.stoppedID != "" {
		t.Fatalf("wrong-session command stopped origin %q", codex.stoppedID)
	}
	run, err := store.Processes().FindRun(context.Background(), "process-1")
	if err != nil || run.Status != processdomain.StatusWaitingUser {
		t.Fatalf("origin after wrong-session commands = %#v, %v", run, err)
	}
	batch, err := store.Questions().FindBatch(context.Background(), pending.ID)
	if err != nil || batch.DeliveryStatus != questiondomain.DeliveryInflight {
		t.Fatalf("delivery after wrong-session commands = %#v, %v", batch, err)
	}
}

func TestWorkflowAnswerStopFailureRollsBackSessionAndEventsWhenWorkflowUpdateFails(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	initial := domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusWaitingUser, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now}
	if err := store.Sessions().Save(ctx, initial); err != nil {
		t.Fatal(err)
	}
	workflows := &fakeWorkflowStarter{resumeErr: errors.New("workflow persistence failed")}
	service := New(store.Sessions(), newFakeProjectRepository("project-1"), WithEvents(store.Events()), WithUnitOfWork(store), WithWorkflows(workflows))
	err = service.persistAnswerFallbackStopFailure(ctx, initial.ID, "batch-1", "process-1", errors.New("stop unavailable"))
	if err == nil || !strings.Contains(err.Error(), "workflow persistence failed") {
		t.Fatalf("stop failure persistence error = %v", err)
	}
	saved, err := store.Sessions().Find(ctx, initial.ID)
	if err != nil || saved.Status != domain.StatusWaitingUser {
		t.Fatalf("session after workflow rollback = %#v, %v", saved, err)
	}
	count, err := store.Client().EventRecord.Query().Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("events after workflow rollback = %d, %v", count, err)
	}
}

func TestInflightAnswerProcessExitMarksResumeFailedWhenResumeCannotBeQueued(t *testing.T) {
	store, service, _ := newAnswerUserWaitTestService(t)
	service.answerUserTimer = func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
	result := make(chan questionapp.BatchDTO, 1)
	errs := make(chan error, 1)
	go func() {
		batch, err := service.RequestUserAnswer(context.Background(), RequestUserAnswerInput{
			SessionID: "session-1",
			Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}},
		})
		result <- batch
		errs <- err
	}()
	pending := waitForAnswerUserBatch(t, store)
	option := questiondomain.OptionID("yes")
	if _, err := service.SubmitQuestionBatch(context.Background(), questionapp.SubmitBatchInput{
		BatchID: pending.ID,
		Answers: []questiondomain.Answer{{QuestionID: pending.Questions[0].ID, SelectedOptionID: &option}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	<-result
	if _, err := store.Client().ProcessRun.UpdateOneID("process-1").SetCodexSessionID("").Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, err := service.persistCodexProcessExit(context.Background(), domain.Session{ID: "session-1"}, processdomain.CodexHandle{ProcessRunID: "process-1", PID: 1234}, codexStartOptions{}, processdomain.ExitResult{FinishedAt: time.Now().UTC()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	run, findErr := store.Processes().FindRun(context.Background(), "process-1")
	if findErr != nil || run.Status != processdomain.StatusExited {
		t.Fatalf("process after resume failure = %#v, %v", run, findErr)
	}
	batch, findErr := store.Questions().FindBatch(context.Background(), pending.ID)
	if findErr != nil || batch.DeliveryStatus != questiondomain.DeliveryAwaitingResume || batch.DeliveryProcessRunID != nil {
		t.Fatalf("delivery after resume failure = %#v, %v", batch, findErr)
	}
	session, findErr := store.Sessions().Find(context.Background(), "session-1")
	if findErr != nil || session.Status != domain.StatusResumeFailed {
		t.Fatalf("session after resume failure = %#v, %v", session, findErr)
	}
}

func TestWorkflowDirectAnswerAckRestoresSameNodeRun(t *testing.T) {
	ctx := context.Background()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeWorkflow, Status: domain.StatusRunning, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	definition := workflowdomain.Definition{
		ID: "workflow-1", ProjectID: "project-1", Name: "Default", Version: 1,
		Graph:     workflowdomain.Graph{Nodes: []workflowdomain.Node{{ID: "build", Type: "codex", Title: "Build", Prompt: "build"}}},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Workflows().SaveDefinition(ctx, definition); err != nil {
		t.Fatal(err)
	}
	workflowRun := workflowdomain.Run{SessionID: "session-1", WorkflowDefinitionID: definition.ID, Status: workflowdomain.RunRunning, CurrentNodeID: "build", StartedAt: &now}
	if err := store.Workflows().CreateRun(ctx, workflowRun); err != nil {
		t.Fatal(err)
	}
	workflowProcessID := workflowdomain.ProcessRunID("process-1")
	nodeRun := workflowdomain.NodeRun{ID: "node-run-1", SessionID: workflowRun.SessionID, NodeID: "build", Status: workflowdomain.NodeRunning, Attempt: 1, ProcessRunID: &workflowProcessID, StartedAt: &now}
	if err := store.Workflows().SaveNodeRun(ctx, nodeRun); err != nil {
		t.Fatal(err)
	}
	pid := 1234
	processNodeID := processdomain.NodeRunID(nodeRun.ID)
	if err := store.Processes().CreateRun(ctx, processdomain.Run{ID: "process-1", SessionID: "session-1", NodeRunID: &processNodeID, Status: processdomain.StatusRunning, PID: &pid, CodexSessionID: "codex-1", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	questions := questionapp.New(store.Questions())
	service := New(store.Sessions(), newFakeProjectRepository("project-1"),
		WithProcesses(store.Processes(), &fakeCodexProcess{}), WithEvents(store.Events()), WithQuestions(questions),
		WithUnitOfWork(store), WithSessionLocker(NewMemorySessionLocker()))
	service.answerUserTimer = func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} }

	result := make(chan questionapp.BatchDTO, 1)
	errs := make(chan error, 1)
	go func() {
		batch, err := service.RequestUserAnswer(ctx, RequestUserAnswerInput{SessionID: "session-1", Questions: []questiondomain.Question{{Title: "Continue?", Options: []questiondomain.Option{{ID: "yes", Label: "Yes"}}}}})
		result <- batch
		errs <- err
	}()
	pending := waitForAnswerUserBatch(t, store)
	option := questiondomain.OptionID("yes")
	if _, err := service.SubmitQuestionBatch(ctx, questionapp.SubmitBatchInput{BatchID: pending.ID, Answers: []questiondomain.Answer{{QuestionID: pending.Questions[0].ID, SelectedOptionID: &option}}}); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	answered := <-result
	if err := service.AcknowledgeUserAnswerDelivery(ctx, AcknowledgeUserAnswerDeliveryInput{SessionID: "session-1", BatchID: answered.ID}); err != nil {
		t.Fatal(err)
	}
	gotNode, err := store.Workflows().FindLatestNodeRun(ctx, workflowRun.SessionID, "build")
	if err != nil {
		t.Fatal(err)
	}
	if gotNode.ID != nodeRun.ID || gotNode.Attempt != 1 || gotNode.Status != workflowdomain.NodeRunning || gotNode.ProcessRunID == nil || *gotNode.ProcessRunID != workflowProcessID {
		t.Fatalf("node run after direct ACK = %#v", gotNode)
	}
	count, err := store.Client().ProcessRun.Query().Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("process run count = %d, %v", count, err)
	}
}

func newAnswerUserWaitTestService(t *testing.T) (*entstore.Store, *Service, *fakeCodexProcess) {
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
	now := time.Now().UTC()
	if err := store.Sessions().Save(ctx, domain.Session{ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning, CodexSessionID: "codex-1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	pid := 1234
	if err := store.Processes().CreateRun(ctx, processdomain.Run{ID: "process-1", SessionID: "session-1", Status: processdomain.StatusRunning, PID: &pid, CodexSessionID: "codex-1", StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	codex := &fakeCodexProcess{}
	questions := questionapp.New(store.Questions())
	service := New(store.Sessions(), newFakeProjectRepository("project-1"),
		WithProcesses(store.Processes(), codex), WithEvents(store.Events()), WithQuestions(questions),
		WithUnitOfWork(store), WithSessionLocker(NewMemorySessionLocker()))
	return store, service, codex
}

type quietAnswerQuestions struct {
	*questionapp.Service
	updates chan questionapp.BatchDTO
}

func (q *quietAnswerQuestions) QuestionBatchUpdates(context.Context, questiondomain.SessionID) (<-chan questionapp.BatchDTO, error) {
	return q.updates, nil
}

func (q *quietAnswerQuestions) PublishBatch(questionapp.BatchDTO) {}

type orderedAnswerQuestions struct {
	*questionapp.Service
	inflightEntered chan struct{}
	releaseInflight chan struct{}
	once            sync.Once
	mu              sync.Mutex
	statuses        []questiondomain.DeliveryStatus
}

func (q *orderedAnswerQuestions) PublishBatch(batch questionapp.BatchDTO) {
	if batch.DeliveryStatus == questiondomain.DeliveryInflight {
		q.once.Do(func() {
			close(q.inflightEntered)
			<-q.releaseInflight
		})
	}
	q.mu.Lock()
	q.statuses = append(q.statuses, batch.DeliveryStatus)
	q.mu.Unlock()
	q.Service.PublishBatch(batch)
}

func (q *orderedAnswerQuestions) snapshot() []questiondomain.DeliveryStatus {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]questiondomain.DeliveryStatus(nil), q.statuses...)
}

func waitForAnswerUserBatch(t *testing.T, store *entstore.Store) questiondomain.Batch {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		batches, err := store.Questions().ListPendingBySession(context.Background(), "session-1")
		if err != nil {
			t.Fatal(err)
		}
		if len(batches) == 1 {
			return batches[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("answer_user batch was not created")
	return questiondomain.Batch{}
}

type fakeCodexProcess struct {
	startCalled   bool
	startInput    processdomain.CodexStartInput
	startInputs   []processdomain.CodexStartInput
	startHandle   processdomain.CodexHandle
	startHandles  []processdomain.CodexHandle
	startErr      error
	startErrs     []error
	resumeCalled  bool
	resumeInput   processdomain.CodexResumeInput
	resumeHandle  processdomain.CodexHandle
	resumeErr     error
	stoppedID     processdomain.RunID
	stopErr       error
	stopHook      func(context.Context, processdomain.RunID) error
	detachedStops []processdomain.DetachedProcess
	detachedErr   error
	detachedHook  func(processdomain.DetachedProcess)
	eventsCalled  bool
	eventsErr     error
	events        <-chan processdomain.CodexEvent
	eventStreams  []<-chan processdomain.CodexEvent
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
	if handle.CodexSessionID == "" {
		handle.CodexSessionID = "codex-session-test"
	}
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
	if handle.CodexSessionID == "" {
		handle.CodexSessionID = input.CodexSessionID
	}
	return handle, nil
}

func (p *fakeCodexProcess) Stop(ctx context.Context, id processdomain.RunID) error {
	p.stoppedID = id
	if p.stopHook != nil {
		return p.stopHook(ctx, id)
	}
	return p.stopErr
}

func (p *fakeCodexProcess) StopDetached(_ context.Context, detached processdomain.DetachedProcess) error {
	p.detachedStops = append(p.detachedStops, detached)
	if p.detachedHook != nil {
		p.detachedHook(detached)
	}
	return p.detachedErr
}

func (p *fakeCodexProcess) Events(_ context.Context, handle processdomain.CodexHandle) (<-chan processdomain.CodexEvent, error) {
	p.eventsCalled = true
	if p.eventsErr != nil {
		return nil, p.eventsErr
	}
	var source <-chan processdomain.CodexEvent
	if len(p.eventStreams) > 0 {
		source = p.eventStreams[0]
		p.eventStreams = p.eventStreams[1:]
	} else if p.events != nil {
		source = p.events
	} else {
		source = make(chan processdomain.CodexEvent)
	}

	events := make(chan processdomain.CodexEvent)
	go func() {
		defer close(events)
		transcriptBound := false
		for event := range source {
			if event.Type == processdomain.CodexEventTranscriptBound {
				transcriptBound = true
			} else if !transcriptBound && event.Type != processdomain.CodexEventProcessExit && handle.CodexSessionID != "" {
				events <- transcriptReadyEvent(handle.CodexSessionID)
				transcriptBound = true
			}
			events <- event
		}
	}()
	return events, nil
}

type fakeUnitOfWork struct {
	called                bool
	calls                 int
	tx                    fakeTx
	err                   error
	publisher             *fakeEventPublisher
	publishedBeforeReturn int
	publishedDuringCall   bool
}

func (u *fakeUnitOfWork) Do(ctx context.Context, fn func(context.Context, port.Tx) error) error {
	u.called = true
	u.calls++
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
	ids  []domain.ID
	hook func()
}

type mutexSessionLocker struct {
	mu sync.Mutex
}

func (l *mutexSessionLocker) WithSessionLock(ctx context.Context, _ domain.ID, fn func(context.Context) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return fn(ctx)
}

func (l *fakeSessionLocker) WithSessionLock(ctx context.Context, id domain.ID, fn func(context.Context) error) error {
	l.ids = append(l.ids, id)
	if l.hook != nil {
		hook := l.hook
		l.hook = nil
		hook()
	}
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

func (tx fakeTx) ClaimExecution(ctx context.Context, input port.ExecutionClaimInput) (port.ExecutionClaimResult, error) {
	if tx.sessions == nil || tx.processes == nil {
		return port.ExecutionClaimResult{}, errors.New("execution claim repositories are required")
	}
	current, err := tx.sessions.Find(ctx, input.ExpectedSession.ID)
	if err != nil {
		return port.ExecutionClaimResult{}, err
	}
	active, found, err := tx.processes.FindActiveBySession(ctx, processdomain.SessionID(current.ID))
	if err != nil {
		return port.ExecutionClaimResult{}, err
	}
	if found {
		return port.ExecutionClaimResult{Status: port.ExecutionAlreadyActive, Session: current, ActiveRun: &active}, nil
	}
	if !current.MatchesLifecycleSnapshot(input.ExpectedSession) {
		return port.ExecutionClaimResult{Status: port.ExecutionStale, Session: current}, nil
	}
	if input.MaxActive > 0 {
		count, err := tx.processes.CountActive(ctx)
		if err != nil {
			return port.ExecutionClaimResult{}, err
		}
		if count >= input.MaxActive {
			return port.ExecutionClaimResult{Status: port.ExecutionAtCapacity, Session: current}, nil
		}
	}
	if err := tx.processes.CreateRun(ctx, input.Run); err != nil {
		return port.ExecutionClaimResult{}, err
	}
	if err := tx.sessions.Save(ctx, input.StartingSession); err != nil {
		return port.ExecutionClaimResult{}, err
	}
	return port.ExecutionClaimResult{Status: port.ExecutionClaimed, Session: input.StartingSession}, nil
}

func (tx fakeTx) PrepareClose(ctx context.Context, input port.ClosePreparationInput) (port.ClosePreparationResult, error) {
	if tx.sessions == nil || tx.processes == nil {
		return port.ClosePreparationResult{}, errors.New("close preparation repositories are required")
	}
	current, err := tx.sessions.Find(ctx, input.ExpectedSession.ID)
	if err != nil {
		return port.ClosePreparationResult{}, err
	}
	if current.Status == domain.StatusClosed {
		return port.ClosePreparationResult{Status: port.CloseAlreadyClosed, Session: current}, nil
	}
	active, found, err := tx.processes.FindActiveBySession(ctx, processdomain.SessionID(current.ID))
	if err != nil {
		return port.ClosePreparationResult{}, err
	}
	if found {
		return port.ClosePreparationResult{Status: port.CloseActive, Session: current, ActiveRun: &active}, nil
	}
	if !current.MatchesLifecycleSnapshot(input.ExpectedSession) {
		return port.ClosePreparationResult{Status: port.CloseStale, Session: current}, nil
	}
	if err := tx.sessions.Save(ctx, input.ClosingSession); err != nil {
		return port.ClosePreparationResult{}, err
	}
	return port.ClosePreparationResult{Status: port.ClosePrepared, Session: input.ClosingSession}, nil
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
	mu         sync.Mutex
	events     []eventdomain.DomainEvent
	appendErrs []error
}

type fakeEventPublisher struct {
	mu             sync.Mutex
	events         []eventdomain.DomainEvent
	codexEvents    []processdomain.CodexEvent
	onPublish      func(eventdomain.DomainEvent)
	onPublishCodex func(processdomain.CodexEvent)
}

func (p *fakeEventPublisher) PublishCodexEvent(_ context.Context, event processdomain.CodexEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.onPublishCodex != nil {
		p.onPublishCodex(event)
	}
	p.codexEvents = append(p.codexEvents, event)
	return nil
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

func (p *fakeEventPublisher) codexSnapshot() []processdomain.CodexEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	events := make([]processdomain.CodexEvent, len(p.codexEvents))
	copy(events, p.codexEvents)
	return events
}

func (s *fakeEventStore) Append(_ context.Context, event eventdomain.DomainEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.appendErrs) > 0 {
		err := s.appendErrs[0]
		s.appendErrs = s.appendErrs[1:]
		if err != nil {
			return err
		}
	}
	s.events = append(s.events, event)
	return nil
}

func (s *fakeEventStore) List(_ context.Context, scope eventdomain.Scope) ([]eventdomain.DomainEvent, error) {
	events := s.snapshot()
	filtered := make([]eventdomain.DomainEvent, 0, len(events))
	for _, event := range events {
		if scope.SessionID != nil && (event.SessionID == nil || *event.SessionID != *scope.SessionID) {
			continue
		}
		if scope.ProjectID != "" && event.Scope.ProjectID != scope.ProjectID {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered, nil
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

func requireSessionEventTypes(t *testing.T, events []eventdomain.DomainEvent, want ...string) {
	t.Helper()
	got := make([]string, len(events))
	for index, event := range events {
		got[index] = event.Type
	}
	if !slices.Equal(got, want) {
		t.Fatalf("event types = %#v, want %#v; events=%#v", got, want, events)
	}
}

func waitForSessionStatus(t *testing.T, repo *fakeRepository, sessionID domain.ID, status domain.Status) domain.Session {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session, err := repo.Find(context.Background(), sessionID)
		if err == nil && session.Status == status {
			return session
		}
		time.Sleep(time.Millisecond)
	}
	session, _ := repo.Find(context.Background(), sessionID)
	t.Fatalf("session %q status = %q, want %q", sessionID, session.Status, status)
	return domain.Session{}
}

func waitForProcessStop(t *testing.T, codex *fakeCodexProcess, processRunID processdomain.RunID) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if codex.stoppedID == processRunID {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("stopped process = %q, want %q", codex.stoppedID, processRunID)
}

func transcriptReadyEvent(codexSessionID string) processdomain.CodexEvent {
	return processdomain.CodexEvent{
		EventID:        "transcript:" + codexSessionID,
		Type:           processdomain.CodexEventTranscriptBound,
		CodexSessionID: codexSessionID,
		Content: processdomain.CodexTranscriptSource{
			CodexSessionID: codexSessionID,
			RelativePath:   "test/" + codexSessionID + ".jsonl",
			BoundAt:        time.Now().UTC(),
		},
	}
}

func waitForEventTypeAfter(t *testing.T, store *fakeEventStore, afterType string, eventType string) eventdomain.DomainEvent {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		seenAfter := false
		for _, event := range store.snapshot() {
			if seenAfter && event.Type == eventType {
				return event
			}
			if event.Type == afterType {
				seenAfter = true
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("event %q after %q was not published: %#v", eventType, afterType, store.snapshot())
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

func waitForPublishedCodexEventType(t *testing.T, publisher *fakeEventPublisher, eventType processdomain.CodexEventType) processdomain.CodexEvent {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, event := range publisher.codexSnapshot() {
			if event.Type == eventType {
				return event
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("published Codex event type %q not found in %#v", eventType, publisher.codexSnapshot())
	return processdomain.CodexEvent{}
}

func eventContent[T processdomain.CodexEventContent](t *testing.T, event processdomain.CodexEvent) T {
	t.Helper()
	content, ok := event.Content.(T)
	if !ok {
		var zero T
		t.Fatalf("event content = %T, want %T: %#v", event.Content, zero, event)
	}
	return content
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
