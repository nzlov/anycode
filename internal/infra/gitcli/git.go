package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/domain/session"
)

const defaultGitBin = "git"
const emptyTreeCommit = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

type Client struct {
	gitBin  string
	dataDir string
}

type Error struct {
	Code   string
	Path   string
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	msg := fmt.Sprintf("git %s", e.Code)
	if e.Path != "" {
		msg += " at " + e.Path
	}
	if len(e.Args) > 0 {
		msg += ": git " + strings.Join(e.Args, " ")
	}
	if e.Stderr != "" {
		return msg + ": " + e.Stderr
	}
	if e.Err != nil {
		return msg + ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(gitBin string) *Client {
	if gitBin == "" {
		gitBin = defaultGitBin
	}
	return &Client{gitBin: gitBin}
}

func NewWorktrees(dataDir string) *Client {
	if dataDir == "" {
		dataDir = os.Getenv("ANYCODE_DATA_DIR")
	}
	return &Client{gitBin: defaultGitBin, dataDir: dataDir}
}

func (c *Client) Detect(ctx context.Context, path string) (project.GitState, error) {
	out, err := c.run(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return project.GitState{
			IsRepository: false,
			ErrorCode:    "not_git_repository",
			ErrorMessage: err.Error(),
		}, nil
	}
	if strings.TrimSpace(out) != "true" {
		return project.GitState{IsRepository: false, ErrorCode: "not_git_repository"}, nil
	}

	branches, branchErr := c.Branches(ctx, path)
	current := currentBranch(branches)
	state := project.GitState{
		IsRepository:  true,
		CurrentBranch: current,
		Branches:      branches,
	}
	if branchErr != nil {
		state.ErrorCode = gitErrorCode(branchErr)
		state.ErrorMessage = branchErr.Error()
	}
	return state, nil
}

func (c *Client) Branches(ctx context.Context, path string) ([]project.GitBranch, error) {
	if err := c.fetchRemotes(ctx, path); err != nil {
		return nil, err
	}
	out, err := c.run(ctx, path, "branch", "--all", "--format=%(refname)%00%(refname:short)%00%(HEAD)")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	branches := make([]project.GitBranch, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		refname, rest, ok := strings.Cut(line, "\x00")
		name := refname
		marker := ""
		if ok {
			name, marker, _ = strings.Cut(rest, "\x00")
		}
		if strings.HasPrefix(refname, "refs/remotes/") && strings.HasSuffix(refname, "/HEAD") {
			continue
		}
		branches = append(branches, project.GitBranch{
			Name:      strings.TrimSpace(name),
			IsCurrent: strings.TrimSpace(marker) == "*",
		})
	}
	return branches, nil
}

func (c *Client) HeadCommit(ctx context.Context, path string, branch string) (string, error) {
	useHead := branch == ""
	if branch == "" {
		branch = "HEAD"
	}
	out, err := c.run(ctx, path, "rev-parse", branch)
	if err != nil {
		if useHead && gitErrorCode(err) == "revision_not_found" {
			hasCommits, hasCommitsErr := c.hasCommits(ctx, path)
			if hasCommitsErr != nil {
				return "", err
			}
			if !hasCommits {
				return "", nil
			}
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Client) SnapshotCommit(ctx context.Context, path string, branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	currentBranch, err := c.CurrentBranch(ctx, path)
	if err != nil {
		return "", err
	}
	if branch == "" || currentBranch != branch {
		return "", &Error{
			Code:   "unexpected_worktree_branch",
			Path:   path,
			Args:   []string{"branch", "--show-current"},
			Stderr: fmt.Sprintf("current branch %q does not match expected branch %q", currentBranch, branch),
		}
	}
	if _, err := c.run(ctx, path, "add", "-A", "--"); err != nil {
		return "", err
	}
	if _, err := c.run(ctx, path,
		"-c", "user.name=AnyCode",
		"-c", "user.email=anycode@localhost",
		"commit", "--allow-empty", "--no-verify", "-m", "anycode session diff snapshot",
	); err != nil {
		return "", err
	}
	commit, err := c.HeadCommit(ctx, path, "")
	if err != nil {
		return "", err
	}
	refName := snapshotRefName(branch)
	if _, err := c.run(ctx, path, "update-ref", refName, commit); err != nil {
		return "", err
	}
	return commit, nil
}

func snapshotRefName(branch string) string {
	branch = sanitizeRefPath(branch)
	if branch == "" {
		branch = "snapshot"
	}
	return "refs/anycode/session-snapshots/" + branch
}

func sanitizeRefPath(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	lastSlash := false
	for _, r := range value {
		valid := r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '-' || r == '_' || r == '.' || r == '/'
		if !valid {
			r = '-'
		}
		if r == '/' {
			if builder.Len() == 0 || lastSlash {
				continue
			}
			lastSlash = true
			builder.WriteRune(r)
			continue
		}
		lastSlash = false
		builder.WriteRune(r)
	}
	return strings.Trim(builder.String(), "./")
}

func (c *Client) Exists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (c *Client) MergeBase(ctx context.Context, worktreePath string, baseRef string) (string, error) {
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		baseRef = "HEAD"
	}
	hasCommits, err := c.hasCommits(ctx, worktreePath)
	if err != nil {
		return "", err
	}
	if !hasCommits {
		return emptyTreeCommit, nil
	}
	out, err := c.run(ctx, worktreePath, "merge-base", baseRef, "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Client) CurrentBranch(ctx context.Context, path string) (string, error) {
	out, err := c.run(ctx, path, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Client) PathForSession(projectID session.ProjectID, sessionID session.ID) string {
	base := c.dataDir
	if base == "" {
		base = os.Getenv("ANYCODE_DATA_DIR")
	}
	if base == "" {
		base = "."
	}
	return filepath.Join(base, "worktrees", string(projectID), string(sessionID))
}

func (c *Client) Create(ctx context.Context, projectPath string, projectID session.ProjectID, sessionID session.ID, baseBranch string) (string, error) {
	path := c.PathForSession(projectID, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("prepare worktree parent: %w", err)
	}
	ref := strings.TrimSpace(baseBranch)
	if ref == "" {
		ref = "HEAD"
	}
	branch := strings.TrimSpace(string(sessionID))
	args := []string{"worktree", "add", "-b", branch, path, ref}
	if err := c.fetchRemotes(ctx, projectPath); err != nil {
		_ = os.RemoveAll(path)
		return "", err
	}
	hasCommits, err := c.hasCommits(ctx, projectPath)
	if err != nil {
		_ = os.RemoveAll(path)
		return "", err
	}
	if !hasCommits {
		args = []string{"worktree", "add", "--orphan", "-b", branch, path}
	}
	if _, err := c.run(ctx, projectPath, args...); err != nil {
		_ = os.RemoveAll(path)
		return "", err
	}
	return path, nil
}

func (c *Client) fetchRemotes(ctx context.Context, path string) error {
	out, err := c.run(ctx, path, "remote")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	_, err = c.run(ctx, path, "fetch", "--prune", "--all")
	return err
}

func (c *Client) hasCommits(ctx context.Context, projectPath string) (bool, error) {
	out, err := c.run(ctx, projectPath, "rev-list", "--max-count=1", "--all")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (c *Client) Remove(ctx context.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, err := c.run(ctx, path, "worktree", "remove", "--force", path); err != nil {
		if removeErr := os.RemoveAll(path); removeErr != nil {
			return fmt.Errorf("%w; remove path: %v", err, removeErr)
		}
		return err
	}
	return nil
}

func (c *Client) DeleteBranch(ctx context.Context, projectPath string, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil
	}
	_, _ = c.run(ctx, projectPath, "worktree", "prune")
	branches, err := c.Branches(ctx, projectPath)
	if err != nil {
		return err
	}
	for _, item := range branches {
		if item.Name != branch {
			continue
		}
		if _, err := c.run(ctx, projectPath, "branch", "-D", branch); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (c *Client) run(ctx context.Context, path string, args ...string) (string, error) {
	gitBin := c.gitBin
	if gitBin == "" {
		gitBin = defaultGitBin
	}
	allArgs := make([]string, 0, len(args)+2)
	if path != "" {
		allArgs = append(allArgs, "-C", path)
	}
	allArgs = append(allArgs, args...)
	cmd := exec.CommandContext(ctx, gitBin, allArgs...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &Error{
			Code:   classify(err, stderr.String()),
			Path:   path,
			Args:   allArgs,
			Stderr: strings.TrimSpace(stderr.String()),
			Err:    err,
		}
	}
	return stdout.String(), nil
}

func currentBranch(branches []project.GitBranch) string {
	for _, branch := range branches {
		if branch.IsCurrent {
			return branch.Name
		}
	}
	return ""
}

func classify(err error, stderr string) string {
	stderr = strings.ToLower(stderr)
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "canceled"
	case strings.Contains(stderr, "not a git repository"):
		return "not_git_repository"
	case strings.Contains(stderr, "unknown revision"), strings.Contains(stderr, "ambiguous argument"):
		return "revision_not_found"
	case strings.Contains(stderr, "permission denied"):
		return "permission_denied"
	case errors.Is(err, os.ErrNotExist):
		return "git_not_found"
	default:
		return "command_failed"
	}
}

func gitErrorCode(err error) string {
	var gitErr *Error
	if errors.As(err, &gitErr) {
		return gitErr.Code
	}
	return "command_failed"
}
