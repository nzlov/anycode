package diff

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/domain/gitdiff"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestGetSessionDiffReturnsUnavailableForNonGitProject(t *testing.T) {
	ctx := context.Background()
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: false}},
		&fakeDiffPort{},
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if got.Available {
		t.Fatal("GetSessionDiff() Available = true")
	}
	if got.Files.Page != 1 || got.Files.PageSize != 20 || got.Files.Total != 0 {
		t.Fatalf("GetSessionDiff() files page = %#v", got.Files)
	}
}

func TestGetSessionDiffReturnsStructuredNotFound(t *testing.T) {
	service := New(&fakeSessionRepository{}, &fakeProjectRepository{}, &fakeDiffPort{})

	_, err := service.GetSessionDiff(context.Background(), SessionDiffInput{SessionID: "missing"})
	if err == nil {
		t.Fatal("GetSessionDiff() expected error")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeNotFound || appErr.Details["sessionId"] != "missing" {
		t.Fatalf("GetSessionDiff() app error = %#v", err)
	}
}

func TestGetSessionDiffReadsSelectedFile(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{
			{Path: "a.go", Status: "modified", Additions: 1},
			{Path: "b.go", Status: "added", Additions: 2},
		},
		fileDiffs: map[string]gitdiff.FileDiff{
			"b.go": {File: gitdiff.DiffFile{Path: "b.go", Status: "added"}, Hunks: []gitdiff.DiffHunk{{Header: "@@"}}},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", BaseBranch: "main"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{
		SessionID: "session-1",
		Mode:      "single",
		FilePath:  "b.go",
		Page:      1,
		PageSize:  1,
	})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if !got.Available || got.FileDiff == nil || got.FileDiff.File.Path != "b.go" {
		t.Fatalf("GetSessionDiff() file diff = %#v", got.FileDiff)
	}
	if got.Files.Total != 2 || len(got.Files.Items) != 1 || got.Files.NextCursor != "2" {
		t.Fatalf("GetSessionDiff() files page = %#v", got.Files)
	}
	if diffPort.lastBaseRef != "main" || diffPort.lastWorktreePath != "/repo" {
		t.Fatalf("diff input path/base = %q/%q", diffPort.lastWorktreePath, diffPort.lastBaseRef)
	}
}

func TestGetSessionDiffFallsBackToFirstFileWhenSelectedFileIsMissing(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{{Path: "a.go", Status: "modified"}},
		fileDiffs: map[string]gitdiff.FileDiff{
			"a.go": {File: gitdiff.DiffFile{Path: "a.go", Status: "modified"}},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1", FilePath: "missing.go"})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if got.FilePath != "a.go" || got.FileDiff == nil || got.FileDiff.File.Path != "a.go" {
		t.Fatalf("GetSessionDiff() fallback = filePath %q diff %#v", got.FilePath, got.FileDiff)
	}
	if diffPort.lastBaseRef != "HEAD" {
		t.Fatalf("empty session base branch used %q, want HEAD", diffPort.lastBaseRef)
	}
}

func TestGetSessionDiffAllModeUsesCurrentPageFiles(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{
			{Path: "a.go", Status: "modified"},
			{Path: "b.go", Status: "added"},
			{Path: "c.go", Status: "deleted"},
		},
		fileDiffs: map[string]gitdiff.FileDiff{
			"c.go": {File: gitdiff.DiffFile{Path: "c.go", Status: "deleted"}},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", BaseBranch: "main"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1", Mode: "all", Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if got.Mode != "all" || len(got.AllDiff) != 1 || got.AllDiff[0].File.Path != "c.go" {
		t.Fatalf("GetSessionDiff() all diff = %#v", got)
	}
	if !reflect.DeepEqual(diffPort.fileDiffCalls, []string{"c.go"}) {
		t.Fatalf("file diff calls = %#v", diffPort.fileDiffCalls)
	}
}

func TestGetSessionDiffUsesMergeRecordRangeWhenWorktreeWasCleaned(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		rangeDiff: gitdiff.SessionDiff{
			Files: []gitdiff.DiffFile{
				{Path: "a.go", Status: "modified", Additions: 1},
				{Path: "b.go", Status: "added", Additions: 2},
			},
			Hunks: []gitdiff.FileDiff{
				{File: gitdiff.DiffFile{Path: "b.go", Status: "added"}, Hunks: []gitdiff.DiffHunk{{Header: "@@"}}},
			},
		},
	}
	service := New(
		&fakeSessionRepository{
			session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/removed", BaseBranch: "main"},
			mergeRecord: sessiondomain.MergeRecord{
				SessionID:   "session-1",
				Status:      "merged",
				BaseCommit:  "base",
				HeadCommit:  "head",
				MergeCommit: "merge",
			},
			hasMergeRecord: true,
		},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true, Path: projectdomain.ProjectPath{Value: "/repo"}}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1", FilePath: "b.go"})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if !got.Available || got.FileDiff == nil || got.FileDiff.File.Path != "b.go" {
		t.Fatalf("GetSessionDiff() = %#v", got)
	}
	if diffPort.lastRangeRepoPath != "/repo" || diffPort.lastRangeBaseRef != "base" || diffPort.lastRangeHeadRef != "head" {
		t.Fatalf("range input = path %q base %q head %q", diffPort.lastRangeRepoPath, diffPort.lastRangeBaseRef, diffPort.lastRangeHeadRef)
	}
	if diffPort.lastWorktreePath != "" {
		t.Fatalf("worktree diff should not be used, got %q", diffPort.lastWorktreePath)
	}
}

type fakeSessionRepository struct {
	session        sessiondomain.Session
	mergeRecord    sessiondomain.MergeRecord
	hasMergeRecord bool
}

func (r *fakeSessionRepository) Save(context.Context, sessiondomain.Session) error { return nil }

func (r *fakeSessionRepository) Find(_ context.Context, id sessiondomain.ID) (sessiondomain.Session, error) {
	if r.session.ID != id {
		return sessiondomain.Session{}, errors.New("session not found")
	}
	return r.session, nil
}

func (r *fakeSessionRepository) ListCards(context.Context, sessiondomain.ListQuery) ([]sessiondomain.Session, int, error) {
	return nil, 0, nil
}

func (r *fakeSessionRepository) ListInterruptedWithCodexSession(context.Context) ([]sessiondomain.Session, error) {
	return nil, nil
}

func (r *fakeSessionRepository) LastConfigForProject(context.Context, sessiondomain.ProjectID) (sessiondomain.Config, bool, error) {
	return sessiondomain.Config{}, false, nil
}

func (r *fakeSessionRepository) AppendPrompt(context.Context, sessiondomain.PromptAppend) error {
	return nil
}

func (r *fakeSessionRepository) ListPromptAppends(context.Context, sessiondomain.ID) ([]sessiondomain.PromptAppend, error) {
	return nil, nil
}

func (r *fakeSessionRepository) AddMergeRecord(context.Context, sessiondomain.MergeRecord) error {
	return nil
}

func (r *fakeSessionRepository) LatestSuccessfulMergeRecord(context.Context, sessiondomain.ID) (sessiondomain.MergeRecord, bool, error) {
	return r.mergeRecord, r.hasMergeRecord, nil
}

type fakeProjectRepository struct {
	project projectdomain.Project
}

func (r *fakeProjectRepository) Save(context.Context, projectdomain.Project) error { return nil }

func (r *fakeProjectRepository) Find(_ context.Context, id projectdomain.ID) (projectdomain.Project, error) {
	if r.project.ID != id {
		return projectdomain.Project{}, errors.New("project not found")
	}
	return r.project, nil
}

func (r *fakeProjectRepository) List(context.Context) ([]projectdomain.Project, error) {
	return nil, nil
}

func (r *fakeProjectRepository) UpdateDefaultWorkflow(context.Context, projectdomain.ID, projectdomain.WorkflowDefinitionID) error {
	return nil
}

type fakeDiffPort struct {
	files             []gitdiff.DiffFile
	fileDiffs         map[string]gitdiff.FileDiff
	rangeDiff         gitdiff.SessionDiff
	fileDiffCalls     []string
	lastWorktreePath  string
	lastBaseRef       string
	lastRangeRepoPath string
	lastRangeBaseRef  string
	lastRangeHeadRef  string
}

func (p *fakeDiffPort) ChangedFiles(_ context.Context, input gitdiff.DiffInput) ([]gitdiff.DiffFile, error) {
	p.lastWorktreePath = input.WorktreePath
	p.lastBaseRef = input.BaseRef
	return p.files, nil
}

func (p *fakeDiffPort) FileDiff(_ context.Context, input gitdiff.FileDiffInput) (gitdiff.FileDiff, error) {
	p.fileDiffCalls = append(p.fileDiffCalls, input.FilePath)
	if diff, ok := p.fileDiffs[input.FilePath]; ok {
		return diff, nil
	}
	return gitdiff.FileDiff{}, errors.New("file diff not found")
}

func (p *fakeDiffPort) RangeDiff(_ context.Context, input gitdiff.RangeDiffInput) (gitdiff.SessionDiff, error) {
	p.lastRangeRepoPath = input.RepoPath
	p.lastRangeBaseRef = input.BaseRef
	p.lastRangeHeadRef = input.HeadRef
	return p.rangeDiff, nil
}
