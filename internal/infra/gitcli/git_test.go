package gitcli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
	if err := client.Remove(ctx, got); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(got); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree still exists after Remove(): %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
