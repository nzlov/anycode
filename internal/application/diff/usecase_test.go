package diff

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/domain/gitdiff"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestCountSessionChangedFilesUsesSessionDiffSource(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		filesByWorktreePath: map[string][]gitdiff.DiffFile{
			"/worktrees/changed": {
				{Path: "a.go", Status: "modified"},
				{Path: "b.go", Status: "added"},
			},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "changed", ProjectID: "git-project", WorktreePath: "/worktrees/changed", WorktreeBaseCommit: "base"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "git-project", IsGit: true}},
		diffPort,
	)

	got, err := service.CountSessionChangedFiles(ctx, "changed")
	if err != nil {
		t.Fatalf("CountSessionChangedFiles() error = %v", err)
	}
	if got != 2 {
		t.Fatalf("CountSessionChangedFiles() = %d, want 2", got)
	}
	if calls := diffPort.changedFileCallCount("/worktrees/changed"); calls != 1 {
		t.Fatalf("ChangedFiles changed calls = %d, want 1", calls)
	}
}

func TestCountSessionChangedFilesRejectsUnavailableDiff(t *testing.T) {
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: false}},
		&fakeDiffPort{},
	)

	_, err := service.CountSessionChangedFiles(context.Background(), "session-1")
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeDiffUnavailable || !appErr.Retryable {
		t.Fatalf("CountSessionChangedFiles() error = %#v", err)
	}
}

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
	if len(got.Files) != 0 {
		t.Fatalf("GetSessionDiff() files = %#v", got.Files)
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

func TestGetSessionDiffReturnsAllChangedFiles(t *testing.T) {
	files := make([]gitdiff.DiffFile, 0, 101)
	for index := range 101 {
		files = append(files, gitdiff.DiffFile{Path: fmt.Sprintf("file-%03d.go", index)})
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", BaseBranch: "main", WorktreeBaseCommit: "base"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		&fakeDiffPort{files: files},
	)

	got, err := service.GetSessionDiff(context.Background(), SessionDiffInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if len(got.Files) != len(files) || got.Files[100].Path != "file-100.go" {
		t.Fatalf("GetSessionDiff() returned %d files, want all %d", len(got.Files), len(files))
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
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", BaseBranch: "main", WorktreeBaseCommit: "base"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{
		SessionID:       "session-1",
		Mode:            "single",
		FilePath:        "b.go",
		IncludeFileDiff: true,
	})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if !got.Available || got.FileDiff == nil || got.FileDiff.File.Path != "b.go" {
		t.Fatalf("GetSessionDiff() file diff = %#v", got.FileDiff)
	}
	if len(got.Files) != 2 {
		t.Fatalf("GetSessionDiff() files = %#v", got.Files)
	}
	if diffPort.lastBaseRef != "base" || diffPort.lastWorktreePath != "/repo" {
		t.Fatalf("diff input path/base = %q/%q", diffPort.lastWorktreePath, diffPort.lastBaseRef)
	}
}

func TestGetSessionDiffUsesResolvedMergeLogRangeWhenWorktreeWasCleaned(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		resolveConfigured: true,
		resolveFound:      true,
		resolvedInput:     gitdiff.DiffInput{WorktreePath: "/repo", BaseRef: "merge-parent", HeadRef: "merge"},
		files:             []gitdiff.DiffFile{{Path: "a.go", Status: "modified", Additions: 1}},
		fileDiffs: map[string]gitdiff.FileDiff{
			"a.go": {File: gitdiff.DiffFile{Path: "a.go", Status: "modified"}},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{
			ID:                 "session-1",
			ProjectID:          "project-1",
			Status:             sessiondomain.StatusClosed,
			BaseBranch:         "main",
			WorktreePath:       "/missing-worktree",
			WorktreeBaseCommit: "cutout",
		}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/repo"}, IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{
		SessionID:       "session-1",
		IncludeFileDiff: true,
	})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if !got.Available || got.FileDiff == nil || got.FileDiff.File.Path != "a.go" {
		t.Fatalf("GetSessionDiff() = %#v", got)
	}
	if diffPort.lastWorktreePath != "/repo" || diffPort.lastBaseRef != "merge-parent" || diffPort.lastHeadRef != "merge" {
		t.Fatalf("diff input path/base/head = %q/%q/%q", diffPort.lastWorktreePath, diffPort.lastBaseRef, diffPort.lastHeadRef)
	}
	if diffPort.lastResolveInput.ProjectPath != "/repo" || diffPort.lastResolveInput.WorktreePath != "/missing-worktree" || diffPort.lastResolveInput.WorktreeBranch != "session-1" || diffPort.lastResolveInput.WorktreeBaseCommit != "cutout" {
		t.Fatalf("resolve input = %#v", diffPort.lastResolveInput)
	}
}

func TestGetSessionDiffReturnsUnavailableWhenNoSourceCanBeResolved(t *testing.T) {
	diffPort := &fakeDiffPort{resolveConfigured: true}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{
			ID:                 "session-1",
			ProjectID:          "project-1",
			BaseBranch:         "main",
			WorktreePath:       "/missing-worktree",
			WorktreeBaseCommit: "cutout",
		}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/repo"}, IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(context.Background(), SessionDiffInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if got.Available || len(got.Files) != 0 {
		t.Fatalf("GetSessionDiff() = %#v", got)
	}
	if len(diffPort.changedFileCalls) != 0 {
		t.Fatalf("ChangedFiles() calls = %#v", diffPort.changedFileCalls)
	}
}

func TestGetSessionDiffReturnsStructuredAmbiguousMergeError(t *testing.T) {
	diffPort := &fakeDiffPort{
		resolveConfigured: true,
		resolveErr:        gitdiff.ErrAmbiguousSessionMerge,
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{
			ID:                 "session-1",
			ProjectID:          "project-1",
			BaseBranch:         "main",
			WorktreePath:       "/missing-worktree",
			WorktreeBaseCommit: "cutout",
		}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/repo"}, IsGit: true}},
		diffPort,
	)

	_, err := service.GetSessionDiff(context.Background(), SessionDiffInput{SessionID: "session-1"})
	if err == nil {
		t.Fatal("GetSessionDiff() expected ambiguous merge error")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeDiffUnavailable || appErr.Retryable || appErr.UserAction != "inspect_git_history" {
		t.Fatalf("GetSessionDiff() error = %#v", err)
	}
	if appErr.Details["sessionId"] != "session-1" || appErr.Details["worktreeBranch"] != "session-1" {
		t.Fatalf("GetSessionDiff() details = %#v", appErr.Details)
	}
}

func TestGetSessionDiffReturnsStructuredInvariantError(t *testing.T) {
	diffPort := &fakeDiffPort{
		resolveConfigured: true,
		resolveErr:        gitdiff.ErrSessionDiffInvariant,
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{
			ID:           "session-1",
			ProjectID:    "project-1",
			BaseBranch:   "main",
			WorktreePath: "/missing-worktree",
		}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/repo"}, IsGit: true}},
		diffPort,
	)

	_, err := service.GetSessionDiff(context.Background(), SessionDiffInput{SessionID: "session-1"})
	if err == nil {
		t.Fatal("GetSessionDiff() expected invariant error")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeDiffUnavailable || appErr.Retryable || appErr.UserAction != "" {
		t.Fatalf("GetSessionDiff() error = %#v", err)
	}
	if appErr.Details["sessionId"] != "session-1" {
		t.Fatalf("GetSessionDiff() details = %#v", appErr.Details)
	}
}

func TestGetSessionDiffAddsSourceDetailsToRetryableResolutionError(t *testing.T) {
	diffPort := &fakeDiffPort{
		resolveConfigured: true,
		resolveErr:        errors.New("revision not found"),
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{
			ID:                 "session-1",
			ProjectID:          "project-1",
			BaseBranch:         "main",
			WorktreePath:       "/missing-worktree",
			WorktreeBaseCommit: "cutout",
		}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/repo"}, IsGit: true}},
		diffPort,
	)

	_, err := service.GetSessionDiff(context.Background(), SessionDiffInput{SessionID: "session-1"})
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeDiffUnavailable || !appErr.Retryable {
		t.Fatalf("GetSessionDiff() error = %#v", err)
	}
	if appErr.Details["sessionId"] != "session-1" || appErr.Details["baseBranch"] != "main" || appErr.Details["worktreeBaseCommit"] != "cutout" {
		t.Fatalf("GetSessionDiff() details = %#v", appErr.Details)
	}
}

func TestGetSessionDiffUsesStoredBaseCommitForLiveWorktree(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		filesByWorktreePath: map[string][]gitdiff.DiffFile{
			"/live-worktree": {{Path: "live.go", Status: "modified", Additions: 1}},
			"/repo":          {{Path: "stored.go", Status: "modified", Additions: 1}},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{
			ID:                 "session-1",
			ProjectID:          "project-1",
			BaseBranch:         "main",
			WorktreePath:       "/live-worktree",
			WorktreeBaseCommit: "stored-base",
		}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/repo"}, IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if !got.Available || len(got.Files) != 1 || got.Files[0].Path != "live.go" {
		t.Fatalf("GetSessionDiff() = %#v", got)
	}
	if diffPort.lastWorktreePath != "/live-worktree" || diffPort.lastBaseRef != "stored-base" || diffPort.lastHeadRef != "" {
		t.Fatalf("diff input path/base/head = %q/%q/%q", diffPort.lastWorktreePath, diffPort.lastBaseRef, diffPort.lastHeadRef)
	}
}

func TestGetSessionDiffPassesContextExpansion(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{{Path: "a.go", Status: "modified"}},
		fileDiffs: map[string]gitdiff.FileDiff{
			"a.go": {File: gitdiff.DiffFile{Path: "a.go", Status: "modified"}},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", BaseBranch: "main", WorktreeBaseCommit: "base"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	_, err := service.GetSessionDiff(ctx, SessionDiffInput{
		SessionID:       "session-1",
		Mode:            "single",
		FilePath:        "a.go",
		IncludeFileDiff: true,
		ContextBefore:   30,
		ContextAfter:    10,
	})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if diffPort.lastContextBefore != 30 || diffPort.lastContextAfter != 10 {
		t.Fatalf("context = before %d after %d, want 30/10", diffPort.lastContextBefore, diffPort.lastContextAfter)
	}
}

func TestGetSessionDiffSkipsFileContentWhenNotRequested(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{
			{Path: "a.go", Status: "modified", Additions: 1},
			{Path: "b.go", Status: "added", Additions: 2},
		},
		fileDiffs: map[string]gitdiff.FileDiff{
			"a.go": {File: gitdiff.DiffFile{Path: "a.go", Status: "modified"}},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", BaseBranch: "main", WorktreeBaseCommit: "base"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{
		SessionID: "session-1",
		Mode:      "single",
	})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if !got.Available || got.FileDiff != nil || len(got.AllDiff) != 0 {
		t.Fatalf("GetSessionDiff() should return only files, got %#v", got)
	}
	if len(got.Files) != 2 {
		t.Fatalf("GetSessionDiff() files = %#v", got.Files)
	}
	if len(diffPort.fileDiffCalls) != 0 {
		t.Fatalf("FileDiff should not be called, got %#v", diffPort.fileDiffCalls)
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
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", WorktreeBaseCommit: "base"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1", FilePath: "missing.go", IncludeFileDiff: true})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if got.FilePath != "a.go" || got.FileDiff == nil || got.FileDiff.File.Path != "a.go" {
		t.Fatalf("GetSessionDiff() fallback = filePath %q diff %#v", got.FilePath, got.FileDiff)
	}
	if diffPort.lastBaseRef != "base" {
		t.Fatalf("session base commit used %q, want base", diffPort.lastBaseRef)
	}
}

func TestGetSessionDiffAllModeUsesAllFiles(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{
			{Path: "a.go", Status: "modified"},
			{Path: "b.go", Status: "added"},
			{Path: "c.go", Status: "deleted"},
		},
		fileDiffs: map[string]gitdiff.FileDiff{
			"a.go": {File: gitdiff.DiffFile{Path: "a.go", Status: "modified"}},
			"b.go": {File: gitdiff.DiffFile{Path: "b.go", Status: "added"}},
			"c.go": {File: gitdiff.DiffFile{Path: "c.go", Status: "deleted"}},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", BaseBranch: "main", WorktreeBaseCommit: "base"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1", Mode: "all", IncludeAllDiff: true})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if got.Mode != "all" || len(got.AllDiff) != 3 || got.AllDiff[2].File.Path != "c.go" {
		t.Fatalf("GetSessionDiff() all diff = %#v", got)
	}
	if !reflect.DeepEqual(diffPort.fileDiffCalls, []string{"a.go", "b.go", "c.go"}) {
		t.Fatalf("file diff calls = %#v", diffPort.fileDiffCalls)
	}
}

func TestGetSessionDiffUsesMergeRecordFileDiffWhenWorktreeWasCleaned(t *testing.T) {
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

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1", FilePath: "b.go", IncludeFileDiff: true})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if !got.Available || got.FileDiff == nil || got.FileDiff.File.Path != "b.go" {
		t.Fatalf("GetSessionDiff() = %#v", got)
	}
	if diffPort.lastRangeRepoPath != "" {
		t.Fatalf("RangeDiff should not be called, got repo %q", diffPort.lastRangeRepoPath)
	}
	if diffPort.lastWorktreePath != "/repo" || diffPort.lastBaseRef != "base" || diffPort.lastHeadRef != "merge" {
		t.Fatalf("diff input = path %q base %q head %q", diffPort.lastWorktreePath, diffPort.lastBaseRef, diffPort.lastHeadRef)
	}
	if !reflect.DeepEqual(diffPort.fileDiffCalls, []string{"b.go"}) {
		t.Fatalf("file diff calls = %#v", diffPort.fileDiffCalls)
	}
}

func TestGetSessionDiffMergeRecordSkipsRangeHunksWhenNotRequested(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{{Path: "a.go", Status: "modified", Additions: 1}},
		rangeDiff: gitdiff.SessionDiff{
			Files: []gitdiff.DiffFile{{Path: "a.go", Status: "modified", Additions: 1}},
			Hunks: []gitdiff.FileDiff{{File: gitdiff.DiffFile{Path: "a.go", Status: "modified"}}},
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

	got, err := service.GetSessionDiff(ctx, SessionDiffInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetSessionDiff() error = %v", err)
	}
	if !got.Available || len(got.Files) != 1 || got.FileDiff != nil || len(got.AllDiff) != 0 {
		t.Fatalf("GetSessionDiff() = %#v", got)
	}
	if diffPort.lastRangeRepoPath != "" {
		t.Fatalf("RangeDiff should not be called, got repo %q", diffPort.lastRangeRepoPath)
	}
	if diffPort.lastWorktreePath != "/repo" || diffPort.lastBaseRef != "base" || diffPort.lastHeadRef != "merge" {
		t.Fatalf("ChangedFiles input = path %q base %q head %q", diffPort.lastWorktreePath, diffPort.lastBaseRef, diffPort.lastHeadRef)
	}
}

func TestGetBranchDiffAggregatesSessionWorktreesForProjectBranch(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		filesByWorktreePath: map[string][]gitdiff.DiffFile{
			"/worktrees/session-1": {{Path: "a.go", Status: "modified", Additions: 1}},
			"/worktrees/session-2": {{Path: "b.go", Status: "added", Additions: 2}},
			"/worktrees/session-3": {{Path: "ignored.go", Status: "added", Additions: 1}},
		},
		fileDiffs: map[string]gitdiff.FileDiff{
			"a.go": {File: gitdiff.DiffFile{Path: "a.go", Status: "modified"}},
			"b.go": {File: gitdiff.DiffFile{Path: "b.go", Status: "added"}},
		},
	}
	service := New(
		&fakeSessionRepository{
			sessions: []sessiondomain.Session{
				{ID: "session-1", ProjectID: "project-1", BaseBranch: "main", WorktreePath: "/worktrees/session-1", WorktreeBaseCommit: "base-1"},
				{ID: "session-2", ProjectID: "project-1", BaseBranch: "main", WorktreePath: "/worktrees/session-2", WorktreeBaseCommit: "base-2"},
				{ID: "session-3", ProjectID: "project-1", BaseBranch: "feature", WorktreePath: "/worktrees/session-3", WorktreeBaseCommit: "base-3"},
			},
		},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true, Path: projectdomain.ProjectPath{Value: "/repo"}}},
		diffPort,
	)

	got, err := service.GetBranchDiff(ctx, BranchDiffInput{ProjectID: "project-1", Branch: "main", Mode: "all", IncludeAllDiff: true})
	if err != nil {
		t.Fatalf("GetBranchDiff() error = %v", err)
	}
	if !got.Available || len(got.Files) != 2 || len(got.AllDiff) != 2 {
		t.Fatalf("GetBranchDiff() = %#v", got)
	}
	if got.Files[0].Path != "session-1: a.go" || got.Files[1].Path != "session-2: b.go" {
		t.Fatalf("prefixed files = %#v", got.Files)
	}
	if !reflect.DeepEqual(diffPort.changedFileCalls, []string{"/worktrees/session-1", "/worktrees/session-2"}) {
		t.Fatalf("changed file calls = %#v", diffPort.changedFileCalls)
	}
}

func TestGetBranchDiffUsesStoredBaseCommitForLiveWorktree(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{{Path: "a.go", Status: "modified", Additions: 1}},
	}
	service := New(
		&fakeSessionRepository{
			sessions: []sessiondomain.Session{{
				ID:                 "session-1",
				ProjectID:          "project-1",
				BaseBranch:         "main",
				WorktreePath:       "/worktrees/session-1",
				WorktreeBaseCommit: "base-commit",
			}},
		},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetBranchDiff(ctx, BranchDiffInput{ProjectID: "project-1", Branch: "main"})
	if err != nil {
		t.Fatalf("GetBranchDiff() error = %v", err)
	}
	if !got.Available || len(got.Files) != 1 {
		t.Fatalf("GetBranchDiff() = %#v", got)
	}
	if diffPort.lastWorktreePath != "/worktrees/session-1" || diffPort.lastBaseRef != "base-commit" || diffPort.lastHeadRef != "" {
		t.Fatalf("diff input path/base/head = %q/%q/%q", diffPort.lastWorktreePath, diffPort.lastBaseRef, diffPort.lastHeadRef)
	}
}

func TestGetBranchDiffUsesMergeRecordFileDiffLazily(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		files: []gitdiff.DiffFile{
			{Path: "a.go", Status: "modified", Additions: 1},
			{Path: "b.go", Status: "added", Additions: 2},
		},
		fileDiffs: map[string]gitdiff.FileDiff{
			"b.go": {File: gitdiff.DiffFile{Path: "b.go", Status: "added"}},
		},
	}
	service := New(
		&fakeSessionRepository{
			sessions: []sessiondomain.Session{
				{ID: "session-1", ProjectID: "project-1", BaseBranch: "main", WorktreePath: "/removed"},
			},
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

	got, err := service.GetBranchDiff(ctx, BranchDiffInput{
		ProjectID:       "project-1",
		Branch:          "main",
		Mode:            "single",
		FilePath:        "session-1: b.go",
		IncludeFileDiff: true,
	})
	if err != nil {
		t.Fatalf("GetBranchDiff() error = %v", err)
	}
	if !got.Available || got.FileDiff == nil || got.FileDiff.File.Path != "session-1: b.go" {
		t.Fatalf("GetBranchDiff() = %#v", got)
	}
	if diffPort.lastRangeRepoPath != "" {
		t.Fatalf("RangeDiff should not be called, got repo %q", diffPort.lastRangeRepoPath)
	}
	if diffPort.lastWorktreePath != "/repo" || diffPort.lastBaseRef != "base" || diffPort.lastHeadRef != "merge" {
		t.Fatalf("diff input = path %q base %q head %q", diffPort.lastWorktreePath, diffPort.lastBaseRef, diffPort.lastHeadRef)
	}
	if !reflect.DeepEqual(diffPort.fileDiffCalls, []string{"b.go"}) {
		t.Fatalf("file diff calls = %#v", diffPort.fileDiffCalls)
	}
}

func TestGetBranchDiffIgnoresOtherBranches(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		filesByWorktreePath: map[string][]gitdiff.DiffFile{
			"/worktrees/session-1": {{Path: "a.go", Status: "modified"}},
		},
	}
	service := New(
		&fakeSessionRepository{
			sessions: []sessiondomain.Session{
				{ID: "session-1", ProjectID: "project-1", BaseBranch: "feature", WorktreePath: "/worktrees/session-1"},
			},
		},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true, Path: projectdomain.ProjectPath{Value: "/repo"}}},
		diffPort,
	)

	got, err := service.GetBranchDiff(ctx, BranchDiffInput{ProjectID: "project-1", Branch: "main"})
	if err != nil {
		t.Fatalf("GetBranchDiff() error = %v", err)
	}
	if !got.Available || len(got.Files) != 0 {
		t.Fatalf("GetBranchDiff() = %#v", got)
	}
	if len(diffPort.changedFileCalls) != 0 {
		t.Fatalf("ChangedFiles should not be called, got paths %#v", diffPort.changedFileCalls)
	}
}

func TestGetCommitHistoryReturnsPagedCommits(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		commits: []gitdiff.CommitRecord{
			{Hash: "commit-3", Subject: "third"},
			{Hash: "commit-2", Subject: "second"},
			{Hash: "commit-1", Subject: "first"},
		},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo", BaseBranch: "main", WorktreeBaseCommit: "base"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetCommitHistory(ctx, CommitHistoryInput{SessionID: "session-1", Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("GetCommitHistory() error = %v", err)
	}
	if !got.Available || got.Commits.Total != 3 || len(got.Commits.Items) != 1 || got.Commits.Items[0].Hash != "commit-1" {
		t.Fatalf("GetCommitHistory() = %#v", got)
	}
	if diffPort.lastCommitWorktreePath != "/repo" || diffPort.lastCommitBaseRef != "base" || diffPort.lastCommitHeadRef != "" {
		t.Fatalf("commit input = path %q base %q head %q", diffPort.lastCommitWorktreePath, diffPort.lastCommitBaseRef, diffPort.lastCommitHeadRef)
	}
}

func TestGetCommitHistoryUsesResolvedMergeLogRange(t *testing.T) {
	diffPort := &fakeDiffPort{
		resolveConfigured: true,
		resolveFound:      true,
		resolvedInput:     gitdiff.DiffInput{WorktreePath: "/repo", BaseRef: "merge-parent", HeadRef: "merge"},
		commits:           []gitdiff.CommitRecord{{Hash: "merge", Subject: "Merge branch 'session-1'"}},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{
			ID:                 "session-1",
			ProjectID:          "project-1",
			BaseBranch:         "main",
			WorktreePath:       "/missing-worktree",
			WorktreeBaseCommit: "cutout",
		}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", Path: projectdomain.ProjectPath{Value: "/repo"}, IsGit: true}},
		diffPort,
	)

	got, err := service.GetCommitHistory(context.Background(), CommitHistoryInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetCommitHistory() error = %v", err)
	}
	if !got.Available || got.Commits.Total != 1 {
		t.Fatalf("GetCommitHistory() = %#v", got)
	}
	if diffPort.lastCommitWorktreePath != "/repo" || diffPort.lastCommitBaseRef != "merge-parent" || diffPort.lastCommitHeadRef != "merge" {
		t.Fatalf("commit input = path %q base %q head %q", diffPort.lastCommitWorktreePath, diffPort.lastCommitBaseRef, diffPort.lastCommitHeadRef)
	}
}

func TestGetCommitHistoryUsesStoredBaseCommitForLiveWorktree(t *testing.T) {
	ctx := context.Background()
	diffPort := &fakeDiffPort{
		commits: []gitdiff.CommitRecord{{Hash: "commit-1", Subject: "change"}},
	}
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{
			ID:                 "session-1",
			ProjectID:          "project-1",
			BaseBranch:         "main",
			WorktreePath:       "/repo",
			WorktreeBaseCommit: "base-commit",
		}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: true}},
		diffPort,
	)

	got, err := service.GetCommitHistory(ctx, CommitHistoryInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetCommitHistory() error = %v", err)
	}
	if !got.Available || got.Commits.Total != 1 {
		t.Fatalf("GetCommitHistory() = %#v", got)
	}
	if diffPort.lastCommitWorktreePath != "/repo" || diffPort.lastCommitBaseRef != "base-commit" || diffPort.lastCommitHeadRef != "" {
		t.Fatalf("commit input = path %q base %q head %q", diffPort.lastCommitWorktreePath, diffPort.lastCommitBaseRef, diffPort.lastCommitHeadRef)
	}
}

func TestGetCommitHistoryReturnsUnavailableForNonGitProject(t *testing.T) {
	ctx := context.Background()
	service := New(
		&fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1", WorktreePath: "/repo"}},
		&fakeProjectRepository{project: projectdomain.Project{ID: "project-1", IsGit: false}},
		&fakeDiffPort{},
	)

	got, err := service.GetCommitHistory(ctx, CommitHistoryInput{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("GetCommitHistory() error = %v", err)
	}
	if got.Available || got.Commits.Total != 0 {
		t.Fatalf("GetCommitHistory() = %#v", got)
	}
}

type fakeSessionRepository struct {
	session        sessiondomain.Session
	sessions       []sessiondomain.Session
	mergeRecord    sessiondomain.MergeRecord
	hasMergeRecord bool
}

func (r *fakeSessionRepository) Create(context.Context, sessiondomain.Session) error { return nil }

func (r *fakeSessionRepository) Save(context.Context, sessiondomain.Session) error { return nil }

func (r *fakeSessionRepository) UpdateFilesChanged(context.Context, sessiondomain.ID, int) error {
	return nil
}

func (r *fakeSessionRepository) Find(_ context.Context, id sessiondomain.ID) (sessiondomain.Session, error) {
	if r.session.ID == id {
		return r.session, nil
	}
	for _, item := range r.sessions {
		if item.ID == id {
			return item, nil
		}
	}
	return sessiondomain.Session{}, errors.New("session not found")
}

func (r *fakeSessionRepository) ListCards(_ context.Context, query sessiondomain.ListQuery) ([]sessiondomain.Session, int, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 {
		pageSize = len(r.sessions)
	}
	filtered := make([]sessiondomain.Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		if query.ProjectID != nil && session.ProjectID != *query.ProjectID {
			continue
		}
		filtered = append(filtered, session)
	}
	start := (page - 1) * pageSize
	if start >= len(filtered) {
		return []sessiondomain.Session{}, len(filtered), nil
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[start:end], len(filtered), nil
}

func (r *fakeSessionRepository) ListQueued(context.Context) ([]sessiondomain.Session, error) {
	return nil, nil
}

func (r *fakeSessionRepository) ListProvisioningWorktrees(context.Context, int) ([]sessiondomain.Session, error) {
	return nil, nil
}

func (r *fakeSessionRepository) ListWorktreeCleanupDue(context.Context, time.Time, int) ([]sessiondomain.Session, error) {
	return nil, nil
}

func (r *fakeSessionRepository) ListInterruptedWithCodexSession(context.Context) ([]sessiondomain.Session, error) {
	return nil, nil
}

func (r *fakeSessionRepository) CountByProject(context.Context, sessiondomain.ProjectID) (int, error) {
	return 0, nil
}

func (r *fakeSessionRepository) LastConfigForProject(context.Context, sessiondomain.ProjectID) (sessiondomain.Config, bool, error) {
	return sessiondomain.Config{}, false, nil
}

func (r *fakeSessionRepository) AppendPrompt(context.Context, sessiondomain.PromptAppend) error {
	return nil
}

func (r *fakeSessionRepository) UpdatePendingPromptAppendBody(context.Context, sessiondomain.ID, string, string) (sessiondomain.PromptAppend, bool, error) {
	return sessiondomain.PromptAppend{}, false, nil
}

func (r *fakeSessionRepository) DeletePromptAppend(context.Context, string) error {
	return nil
}

func (r *fakeSessionRepository) ListPromptAppends(context.Context, sessiondomain.ID) ([]sessiondomain.PromptAppend, error) {
	return nil, nil
}

func (r *fakeSessionRepository) ListPendingPromptAppends(context.Context, sessiondomain.ID) ([]sessiondomain.PromptAppend, error) {
	return nil, nil
}

func (r *fakeSessionRepository) MarkPromptAppendsInflight(context.Context, []string, string) error {
	return nil
}
func (r *fakeSessionRepository) CompletePromptAppends(context.Context, string, time.Time) error {
	return nil
}
func (r *fakeSessionRepository) ReleasePromptAppends(context.Context, string) error {
	return nil
}

func (r *fakeSessionRepository) ListPromptAppendAttachments(context.Context, sessiondomain.ID, string) ([]sessiondomain.SessionAttachment, error) {
	return nil, nil
}

func (r *fakeSessionRepository) AddMergeRecord(context.Context, sessiondomain.MergeRecord) error {
	return nil
}

func (r *fakeSessionRepository) LatestSuccessfulMergeRecord(context.Context, sessiondomain.ID) (sessiondomain.MergeRecord, bool, error) {
	return r.mergeRecord, r.hasMergeRecord, nil
}

type fakeProjectRepository struct {
	project  projectdomain.Project
	projects map[projectdomain.ID]projectdomain.Project
}

func (r *fakeProjectRepository) Save(context.Context, projectdomain.Project) error { return nil }

func (r *fakeProjectRepository) Find(_ context.Context, id projectdomain.ID) (projectdomain.Project, error) {
	if r.project.ID == id {
		return r.project, nil
	}
	if project, ok := r.projects[id]; ok {
		return project, nil
	}
	return projectdomain.Project{}, errors.New("project not found")
}

func (r *fakeProjectRepository) FindByPath(context.Context, string) (projectdomain.Project, bool, error) {
	return projectdomain.Project{}, false, nil
}

func (r *fakeProjectRepository) List(context.Context) ([]projectdomain.Project, error) {
	return nil, nil
}

func (r *fakeProjectRepository) Remove(context.Context, projectdomain.ID, time.Time) error {
	return nil
}

func (r *fakeProjectRepository) UpdateDefaultWorkflow(context.Context, projectdomain.ID, projectdomain.WorkflowDefinitionID) error {
	return nil
}

type fakeDiffPort struct {
	mu                      sync.Mutex
	resolveConfigured       bool
	resolveFound            bool
	resolvedInput           gitdiff.DiffInput
	resolveErr              error
	lastResolveInput        gitdiff.ResolveSessionDiffInput
	currentBranch           string
	files                   []gitdiff.DiffFile
	filesByWorktreePath     map[string][]gitdiff.DiffFile
	changedFileErrors       map[string]error
	changedFilesStarted     chan string
	changedFilesRelease     chan struct{}
	changedFilesInFlight    int
	maxChangedFilesInFlight int
	fileDiffs               map[string]gitdiff.FileDiff
	rangeDiff               gitdiff.SessionDiff
	commits                 []gitdiff.CommitRecord
	fileDiffCalls           []string
	changedFileCalls        []string
	lastWorktreePath        string
	lastBaseRef             string
	lastHeadRef             string
	lastRangeRepoPath       string
	lastRangeBaseRef        string
	lastRangeHeadRef        string
	lastCommitWorktreePath  string
	lastCommitBaseRef       string
	lastCommitHeadRef       string
	lastContextBefore       int
	lastContextAfter        int
}

func (p *fakeDiffPort) CurrentBranch(context.Context, string) (string, error) {
	if p.currentBranch == "" {
		return "main", nil
	}
	return p.currentBranch, nil
}

func (p *fakeDiffPort) ResolveSessionDiffSource(_ context.Context, input gitdiff.ResolveSessionDiffInput) (gitdiff.DiffInput, bool, error) {
	p.mu.Lock()
	p.lastResolveInput = input
	p.mu.Unlock()
	if p.resolveConfigured {
		return p.resolvedInput, p.resolveFound, p.resolveErr
	}
	if input.WorktreePath == "" {
		return gitdiff.DiffInput{}, false, nil
	}
	if input.WorktreeBaseCommit == "" {
		return gitdiff.DiffInput{}, false, gitdiff.ErrSessionDiffInvariant
	}
	return gitdiff.DiffInput{WorktreePath: input.WorktreePath, BaseRef: input.WorktreeBaseCommit}, true, nil
}

func (p *fakeDiffPort) ChangedFiles(_ context.Context, input gitdiff.DiffInput) ([]gitdiff.DiffFile, error) {
	p.mu.Lock()
	p.lastWorktreePath = input.WorktreePath
	p.lastBaseRef = input.BaseRef
	p.lastHeadRef = input.HeadRef
	p.changedFileCalls = append(p.changedFileCalls, input.WorktreePath)
	p.changedFilesInFlight++
	if p.changedFilesInFlight > p.maxChangedFilesInFlight {
		p.maxChangedFilesInFlight = p.changedFilesInFlight
	}
	started := p.changedFilesStarted
	release := p.changedFilesRelease
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.changedFilesInFlight--
		p.mu.Unlock()
	}()
	if started != nil {
		started <- input.WorktreePath
	}
	if release != nil {
		<-release
	}
	if err := p.changedFileErrors[input.WorktreePath]; err != nil {
		return nil, err
	}
	if p.filesByWorktreePath != nil {
		return p.filesByWorktreePath[input.WorktreePath], nil
	}
	return p.files, nil
}

func (p *fakeDiffPort) changedFileCallCount(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, call := range p.changedFileCalls {
		if call == path {
			count++
		}
	}
	return count
}

func (p *fakeDiffPort) FileDiff(_ context.Context, input gitdiff.FileDiffInput) (gitdiff.FileDiff, error) {
	p.fileDiffCalls = append(p.fileDiffCalls, input.FilePath)
	p.lastContextBefore = input.ContextBefore
	p.lastContextAfter = input.ContextAfter
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

func (p *fakeDiffPort) CommitHistory(_ context.Context, input gitdiff.CommitHistoryInput) ([]gitdiff.CommitRecord, error) {
	p.lastCommitWorktreePath = input.WorktreePath
	p.lastCommitBaseRef = input.BaseRef
	p.lastCommitHeadRef = input.HeadRef
	return p.commits, nil
}
