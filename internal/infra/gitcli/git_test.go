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

func TestHeadCommitReturnsEmptyForUnbornHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")

	commit, err := New("").HeadCommit(context.Background(), dir, "")
	if err != nil {
		t.Fatalf("HeadCommit() error = %v", err)
	}
	if commit != "" {
		t.Fatalf("HeadCommit() = %q, want empty", commit)
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
	got, err := client.Create(ctx, repo, session.ProjectID("project-1"), session.ID("session-1"), "session-1", "feature/base", "owner-token")
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
	ownership, err := client.InspectOwnership(ctx, repo, got, "session-1", "owner-token")
	if err != nil {
		t.Fatalf("InspectOwnership() error = %v", err)
	}
	if !ownership.PathExists || !ownership.BranchExists || !ownership.Registered || !ownership.MarkerExists || !ownership.TokenMatches || !ownership.Matches {
		t.Fatalf("InspectOwnership() = %#v", ownership)
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
	if err := client.ReleaseOwnership(ctx, got, "owner-token"); err != nil {
		t.Fatalf("ReleaseOwnership() error = %v", err)
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

func TestCreateFailurePreservesExistingBranchAndDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	ctx := context.Background()
	repo := t.TempDir()
	dataDir := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	runGit(t, repo, "branch", "session-1")
	client := NewWorktrees(dataDir)
	worktreePath := client.PathForSession("project-1", "session-1")
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("create existing directory: %v", err)
	}
	sentinel := filepath.Join(worktreePath, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if _, err := client.Create(ctx, repo, "project-1", "session-1", "session-1", "main", "owner-token"); err == nil {
		t.Fatal("Create() expected existing branch error")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("existing directory content was removed: %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, repo, "branch", "--list", "session-1")); got != "session-1" {
		t.Fatalf("existing branch = %q, want session-1", got)
	}
}

func TestCreateFailurePreservesExistingUnmarkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	ctx := context.Background()
	repo := t.TempDir()
	dataDir := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	client := NewWorktrees(dataDir)
	worktreePath := client.PathForSession("project-1", "session-1")
	runGit(t, repo, "worktree", "add", "-b", "session-1", worktreePath, "main")

	if _, err := client.Create(ctx, repo, "project-1", "session-1", "session-1", "main", "owner-token"); err == nil {
		t.Fatal("Create() expected existing worktree conflict")
	}
	ownership, err := client.InspectOwnership(ctx, repo, worktreePath, "session-1", "owner-token")
	if err != nil {
		t.Fatalf("InspectOwnership() error = %v", err)
	}
	if !ownership.PathExists || !ownership.BranchExists || !ownership.Registered || ownership.MarkerExists || ownership.TokenMatches || ownership.Matches {
		t.Fatalf("unmarked ownership = %#v", ownership)
	}
}

func TestRelativeDataDirCreatesInspectableManagedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	root := t.TempDir()
	t.Chdir(root)
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	client := NewWorktrees("./relative-data")

	worktreePath, err := client.Create(context.Background(), repo, "project-1", "session-1", "session-1", "main", "owner-token")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	wantPath := filepath.Join(root, "relative-data", "worktrees", "project-1", "session-1")
	if worktreePath != wantPath {
		t.Fatalf("Create() path = %q, want %q", worktreePath, wantPath)
	}
	ownership, err := client.InspectOwnership(context.Background(), repo, worktreePath, "session-1", "owner-token")
	if err != nil {
		t.Fatalf("InspectOwnership() error = %v", err)
	}
	if !ownership.Matches {
		t.Fatalf("InspectOwnership() = %#v", ownership)
	}
	if err := client.Remove(context.Background(), worktreePath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := client.DeleteBranch(context.Background(), repo, "session-1"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if err := client.ReleaseOwnership(context.Background(), worktreePath, "owner-token"); err != nil {
		t.Fatalf("ReleaseOwnership() error = %v", err)
	}
}

func TestInspectOwnershipRejectsWorktreeFromAnotherRepositoryAtStalePath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	ctx := context.Background()
	root := t.TempDir()
	repoA := filepath.Join(root, "repo-a")
	repoB := filepath.Join(root, "repo-b")
	dataDir := filepath.Join(root, "data")
	for _, repo := range []string{repoA, repoB} {
		if err := os.Mkdir(repo, 0o755); err != nil {
			t.Fatalf("create repo: %v", err)
		}
		runGit(t, repo, "init", "-b", "main")
		runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	}
	client := NewWorktrees(dataDir)
	worktreePath, err := client.Create(ctx, repoA, "project-1", "session-1", "session-1", "main", "owner-token")
	if err != nil {
		t.Fatalf("Create(repo A) error = %v", err)
	}
	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatalf("remove repo A worktree directory: %v", err)
	}
	runGit(t, repoB, "worktree", "add", "-b", "other-session", worktreePath, "main")

	ownership, err := client.InspectOwnership(ctx, repoA, worktreePath, "session-1", "owner-token")
	if err != nil {
		t.Fatalf("InspectOwnership() error = %v", err)
	}
	if !ownership.PathExists || !ownership.BranchExists || !ownership.Registered || !ownership.TokenMatches || ownership.Matches {
		t.Fatalf("cross-repository ownership = %#v", ownership)
	}
}

func TestCreateClaimsMarkerBeforeGitFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	repo := t.TempDir()
	dataDir := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	client := NewWorktrees(dataDir)
	worktreePath := client.PathForSession("project-1", "session-1")

	if _, err := client.Create(context.Background(), repo, "project-1", "session-1", "session-1", "missing-base", "owner-token"); err == nil {
		t.Fatal("Create() expected invalid base error")
	}
	ownership, err := client.InspectOwnership(context.Background(), repo, worktreePath, "session-1", "owner-token")
	if err != nil {
		t.Fatalf("InspectOwnership() error = %v", err)
	}
	if !ownership.MarkerExists || !ownership.TokenMatches {
		t.Fatalf("claimed ownership after Git failure = %#v", ownership)
	}
}

func TestCreateDoesNotOverwriteConcurrentOwnershipMarker(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	repo := t.TempDir()
	dataDir := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	client := NewWorktrees(dataDir)
	worktreePath := client.PathForSession("project-1", "session-1")
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("create marker parent: %v", err)
	}
	markerPath := ownershipMarkerPath(worktreePath)
	if err := os.WriteFile(markerPath, []byte("other-owner\n"), 0o600); err != nil {
		t.Fatalf("write concurrent marker: %v", err)
	}

	if _, err := client.Create(context.Background(), repo, "project-1", "session-1", "session-1", "main", "owner-token"); err == nil {
		t.Fatal("Create() expected ownership marker conflict")
	}
	content, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read concurrent marker: %v", err)
	}
	if strings.TrimSpace(string(content)) != "other-owner" {
		t.Fatalf("concurrent marker was overwritten: %q", content)
	}
}

func TestCreateSupportsSymlinkedProjectPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	repoLink := filepath.Join(root, "repo-link")
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "-c", "user.name=AnyCode", "-c", "user.email=anycode@example.test", "commit", "--allow-empty", "-m", "init")
	if err := os.Symlink(repo, repoLink); err != nil {
		t.Fatalf("create project symlink: %v", err)
	}
	client := NewWorktrees(dataDir)
	worktreePath, err := client.Create(context.Background(), repoLink, "project-1", "session-1", "session-1", "main", "owner-token")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	ownership, err := client.InspectOwnership(context.Background(), repoLink, worktreePath, "session-1", "owner-token")
	if err != nil {
		t.Fatalf("InspectOwnership() error = %v", err)
	}
	if !ownership.Matches {
		t.Fatalf("symlinked project ownership = %#v", ownership)
	}
}

func TestRemoveFallsBackForDamagedWorktreeDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "damaged-worktree")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("create damaged worktree directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "leftover.txt"), []byte("leftover\n"), 0o644); err != nil {
		t.Fatalf("write leftover file: %v", err)
	}

	if err := New("").Remove(context.Background(), path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("damaged worktree directory still exists: %v", err)
	}
}

func TestRemoveDoesNotFallbackAfterContextCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancelled-worktree")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("create worktree directory: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := New("").Remove(ctx, path); err == nil {
		t.Fatal("Remove() expected canceled error")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cancelled worktree directory was removed: %v", err)
	}
}

func TestDeleteBranchDoesNotFetchRemotes(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Tester")
	runGit(t, repo, "config", "user.email", "tester@example.com")
	runGit(t, repo, "commit", "--allow-empty", "-m", "base")
	runGit(t, repo, "branch", "session-1")
	runGit(t, repo, "remote", "add", "origin", "https://127.0.0.1:1/unreachable.git")

	if err := New("").DeleteBranch(context.Background(), repo, "session-1"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, repo, "branch", "--list", "session-1")); got != "" {
		t.Fatalf("session branch still exists: %q", got)
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
	got, err := client.Create(ctx, repo, session.ProjectID("project-1"), session.ID("session-1"), "session-1", "upstream/feature/remote-only", "owner-token")
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
	got, err := client.Create(ctx, repo, session.ProjectID("project-1"), session.ID("session-1"), "session-1", "main", "owner-token")
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

func TestDeleteBranchPrunesMissingWorktreeMetadata(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}

	ctx := context.Background()
	repo := t.TempDir()
	dataDir := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Tester")
	runGit(t, repo, "config", "user.email", "tester@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "base")

	client := NewWorktrees(dataDir)
	worktreePath, err := client.Create(ctx, repo, session.ProjectID("project-1"), session.ID("session-1"), "session-1", "main", "owner-token")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := os.RemoveAll(worktreePath); err != nil {
		t.Fatalf("remove worktree dir: %v", err)
	}
	if err := client.DeleteBranch(ctx, repo, "session-1"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if branches, err := client.Branches(ctx, repo); err != nil {
		t.Fatalf("Branches() error = %v", err)
	} else if hasBranch(branches, "session-1") {
		t.Fatalf("session branch still exists after DeleteBranch(): %+v", branches)
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
