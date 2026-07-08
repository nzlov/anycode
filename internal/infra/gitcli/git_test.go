package gitcli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/domain/session"
)

func TestDetectBranchesAndHeadCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	runGit(t, dir, "checkout", "-b", "feature/test")

	client := New("")
	state, err := client.Detect(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !state.IsRepository {
		t.Fatalf("expected repository: %+v", state)
	}
	if state.CurrentBranch != "feature/test" {
		t.Fatalf("CurrentBranch = %q", state.CurrentBranch)
	}
	if len(state.Branches) != 2 {
		t.Fatalf("Branches = %+v", state.Branches)
	}

	commit, err := client.HeadCommit(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(commit) != 40 {
		t.Fatalf("commit = %q", commit)
	}
}

func TestBranchesFetchesAndIncludesRemoteBranches(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	ctx := context.Background()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	repo := filepath.Join(base, "repo")
	other := filepath.Join(base, "other")
	runGit(t, base, "init", "--bare", remote)
	runGit(t, base, "clone", remote, repo)
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	runGit(t, repo, "branch", "-M", "main")
	runGit(t, repo, "push", "-u", "origin", "main")
	runGit(t, repo, "remote", "rename", "origin", "upstream")
	runGit(t, base, "clone", remote, other)
	runGit(t, other, "checkout", "-b", "feature/remote-only")
	runGit(t, other, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "remote-only")
	runGit(t, other, "push", "origin", "feature/remote-only")

	branches, err := New("").Branches(ctx, repo)
	if err != nil {
		t.Fatalf("Branches() error = %v", err)
	}
	if !hasBranch(branches, "main") {
		t.Fatalf("Branches() missing local main: %+v", branches)
	}
	if !hasBranch(branches, "upstream/feature/remote-only") {
		t.Fatalf("Branches() missing fetched remote branch with prefix: %+v", branches)
	}
	if hasBranch(branches, "upstream/HEAD") {
		t.Fatalf("Branches() should not include remote HEAD: %+v", branches)
	}
}

func TestDetectNonRepositoryReturnsState(t *testing.T) {
	state, err := New("").Detect(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if state.IsRepository {
		t.Fatalf("expected non-repository: %+v", state)
	}
	if state.ErrorCode != "not_git_repository" {
		t.Fatalf("ErrorCode = %q", state.ErrorCode)
	}
}

func TestHeadCommitReturnsStructuredError(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")
	_, err := New("").HeadCommit(context.Background(), dir, "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("expected Error, got %T", err)
	}
}

func TestPathForSession(t *testing.T) {
	got := (&Client{dataDir: "/data"}).PathForSession(session.ProjectID("project-1"), session.ID("session-1"))
	want := filepath.Join("/data", "worktrees", "project-1", "session-1")
	if got != want {
		t.Fatalf("PathForSession = %q, want %q", got, want)
	}
}

func TestPathForSessionUsesANYCODEDataDir(t *testing.T) {
	t.Setenv("ANYCODE_DATA_DIR", "/env-data")
	got := NewWorktrees("").PathForSession(session.ProjectID("project-1"), session.ID("session-1"))
	want := filepath.Join("/env-data", "worktrees", "project-1", "session-1")
	if got != want {
		t.Fatalf("PathForSession = %q, want %q", got, want)
	}
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	ctx := context.Background()
	repo := t.TempDir()
	dataDir := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	runGit(t, repo, "checkout", "-b", "feature/base")

	client := NewWorktrees(dataDir)
	got, err := client.Create(ctx, repo, session.ProjectID("project-1"), session.ID("session-1"), "feature/base")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	want := filepath.Join(dataDir, "worktrees", "project-1", "session-1")
	if got != want {
		t.Fatalf("Create() path = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(got, ".git")); err != nil {
		t.Fatalf("worktree .git not found: %v", err)
	}
	head, err := client.HeadCommit(ctx, got, "")
	if err != nil {
		t.Fatalf("HeadCommit(worktree) error = %v", err)
	}
	base, err := client.HeadCommit(ctx, repo, "feature/base")
	if err != nil {
		t.Fatalf("HeadCommit(base) error = %v", err)
	}
	if head != base {
		t.Fatalf("worktree head = %q, want %q", head, base)
	}
	branch, err := client.CurrentBranch(ctx, got)
	if err != nil {
		t.Fatalf("CurrentBranch(worktree) error = %v", err)
	}
	if branch != "session-1" {
		t.Fatalf("worktree branch = %q, want %q", branch, "session-1")
	}
	if err := client.Remove(ctx, got); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(got); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree still exists after Remove(): %v", err)
	}
	if err := client.Remove(ctx, got); err != nil {
		t.Fatalf("Remove() second call error = %v", err)
	}
	if err := client.DeleteBranch(ctx, repo, "session-1"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	branches, err := client.Branches(ctx, repo)
	if err != nil {
		t.Fatalf("Branches() error = %v", err)
	}
	for _, branch := range branches {
		if branch.Name == "session-1" {
			t.Fatalf("session branch still exists after DeleteBranch(): %+v", branches)
		}
	}
}

func TestCreateWorktreeFetchesRemoteBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	ctx := context.Background()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	repo := filepath.Join(base, "repo")
	other := filepath.Join(base, "other")
	dataDir := filepath.Join(base, "data")
	runGit(t, base, "init", "--bare", remote)
	runGit(t, base, "clone", remote, repo)
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	runGit(t, repo, "branch", "-M", "main")
	runGit(t, repo, "push", "-u", "origin", "main")
	runGit(t, repo, "remote", "rename", "origin", "upstream")
	runGit(t, base, "clone", remote, other)
	runGit(t, other, "checkout", "-b", "feature/remote-only")
	runGit(t, other, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "remote-only")
	runGit(t, other, "push", "origin", "feature/remote-only")
	wantHead := gitOutput(t, other, "rev-parse", "HEAD")

	client := NewWorktrees(dataDir)
	got, err := client.Create(ctx, repo, session.ProjectID("project-1"), session.ID("session-1"), "upstream/feature/remote-only")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	head, err := client.HeadCommit(ctx, got, "")
	if err != nil {
		t.Fatalf("HeadCommit(worktree) error = %v", err)
	}
	if head != wantHead {
		t.Fatalf("worktree head = %q, want %q", head, wantHead)
	}
}

func TestCreateWorktreeFromEmptyRepositoryCreatesOrphanBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	ctx := context.Background()
	repo := t.TempDir()
	dataDir := t.TempDir()
	runGit(t, repo, "init")

	client := NewWorktrees(dataDir)
	got, err := client.Create(ctx, repo, session.ProjectID("project-1"), session.ID("session-1"), "main")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	want := filepath.Join(dataDir, "worktrees", "project-1", "session-1")
	if got != want {
		t.Fatalf("Create() path = %q, want %q", got, want)
	}
	branch, err := client.CurrentBranch(ctx, got)
	if err != nil {
		t.Fatalf("CurrentBranch(worktree) error = %v", err)
	}
	if branch != "session-1" {
		t.Fatalf("worktree branch = %q, want %q", branch, "session-1")
	}
}

func hasBranch(branches []project.GitBranch, name string) bool {
	for _, branch := range branches {
		if branch.Name == name {
			return true
		}
	}
	return false
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
