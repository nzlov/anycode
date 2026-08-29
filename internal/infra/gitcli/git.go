package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/domain/session"
)

const defaultGitBin = "git"
const worktreeOwnershipMarkerSuffix = ".anycode-owner"

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
	if dataDir == "" {
		dataDir = "."
	}
	if absolute, err := filepath.Abs(dataDir); err == nil {
		dataDir = absolute
	}
	return &Client{gitBin: defaultGitBin, dataDir: dataDir}
}

func (c *Client) Clone(ctx context.Context, parentPath string, repositoryURL string) (string, error) {
	parentPath = strings.TrimSpace(parentPath)
	repositoryURL = strings.TrimSpace(repositoryURL)
	if parentPath == "" {
		return "", errors.New("clone parent path is required")
	}
	if repositoryURL == "" {
		return "", errors.New("clone repository URL is required")
	}
	absoluteParent, err := filepath.Abs(parentPath)
	if err != nil {
		return "", fmt.Errorf("resolve clone parent path: %w", err)
	}
	info, err := os.Stat(absoluteParent)
	if err != nil {
		return "", fmt.Errorf("inspect clone parent path: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("clone parent path is not a directory")
	}
	directoryName, err := cloneDirectoryName(repositoryURL)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(absoluteParent, directoryName)
	if _, err := c.run(ctx, absoluteParent, "clone", "--", repositoryURL, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func cloneDirectoryName(repositoryURL string) (string, error) {
	candidate := strings.TrimSpace(repositoryURL)
	if parsed, err := url.Parse(candidate); err == nil && parsed.Scheme != "" && parsed.Path != "" {
		candidate = parsed.Path
	}
	candidate = strings.TrimRight(candidate, "/\\")
	if index := strings.LastIndexAny(candidate, "/\\:"); index >= 0 {
		candidate = candidate[index+1:]
	}
	candidate = strings.TrimSuffix(candidate, ".git")
	if candidate == "" || candidate == "." || candidate == ".." || filepath.Base(candidate) != candidate {
		return "", errors.New("clone repository URL has no valid project directory name")
	}
	return candidate, nil
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

func (c *Client) BaseBranchExists(ctx context.Context, projectPath string, branch string) (bool, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false, nil
	}
	hasCommits, err := c.hasCommits(ctx, projectPath)
	if err != nil {
		return false, err
	}
	if !hasCommits {
		// Worktree creation uses an orphan branch when the repository has no commits.
		return true, nil
	}
	refs := []string{"refs/heads/" + branch, "refs/remotes/" + branch}
	out, err := c.run(ctx, projectPath, "for-each-ref", "--format=%(refname)", refs[0], refs[1])
	if err != nil {
		return false, err
	}
	for _, ref := range strings.Split(strings.TrimSpace(out), "\n") {
		ref = strings.TrimSpace(ref)
		if ref == refs[0] || ref == refs[1] {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) RetainCommit(ctx context.Context, projectPath string, sessionID session.ID, commit string) error {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return nil
	}
	ref := "refs/anycode/sessions/" + strings.TrimSpace(string(sessionID))
	if _, err := c.run(ctx, projectPath, "check-ref-format", ref); err != nil {
		return err
	}
	_, err := c.run(ctx, projectPath, "update-ref", ref, commit)
	return err
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
	path := filepath.Join(base, "worktrees", string(projectID), string(sessionID))
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return filepath.Clean(path)
}

func (c *Client) Create(ctx context.Context, projectPath string, projectID session.ProjectID, sessionID session.ID, branch string, baseBranch string, ownershipToken string) (string, error) {
	path := c.PathForSession(projectID, sessionID)
	ref := strings.TrimSpace(baseBranch)
	if ref == "" {
		ref = "HEAD"
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", errors.New("worktree branch is required")
	}
	ownershipToken = strings.TrimSpace(ownershipToken)
	if ownershipToken == "" {
		return "", errors.New("worktree ownership token is required")
	}
	preflight, err := c.InspectOwnership(ctx, projectPath, path, branch, ownershipToken)
	if err != nil {
		return "", err
	}
	if preflight.PathExists || preflight.BranchExists || preflight.Registered || preflight.MarkerExists {
		return "", errors.New("worktree ownership namespace is not empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("prepare worktree parent: %w", err)
	}
	if err := claimOwnershipMarker(path, ownershipToken); err != nil {
		return "", fmt.Errorf("claim worktree ownership marker: %w", err)
	}
	args := []string{"worktree", "add", "-b", branch, path, ref}
	hasCommits, err := c.hasCommits(ctx, projectPath)
	if err != nil {
		return "", err
	}
	if !hasCommits {
		args = []string{"worktree", "add", "--orphan", "-b", branch, path}
	}
	if _, err := c.run(ctx, projectPath, args...); err != nil {
		return "", err
	}
	linked, _, err := c.inspectWorktreeLink(ctx, projectPath, path, branch)
	if err != nil {
		return "", err
	}
	if !linked {
		return "", errors.New("created worktree is not linked to the target project and branch")
	}
	return path, nil
}

func (c *Client) SnapshotChanges(ctx context.Context, sourcePath string, targetPath string) error {
	sourcePath = absolutePath(strings.TrimSpace(sourcePath))
	targetPath = absolutePath(strings.TrimSpace(targetPath))
	if sourcePath == targetPath {
		return errors.New("source and target worktrees must differ")
	}
	sourceCommonDir, err := c.commonGitDir(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source worktree repository: %w", err)
	}
	targetCommonDir, err := c.commonGitDir(ctx, targetPath)
	if err != nil {
		return fmt.Errorf("resolve target worktree repository: %w", err)
	}
	if sourceCommonDir != targetCommonDir {
		return errors.New("source and target worktrees belong to different repositories")
	}
	targetHead, err := c.HeadCommit(ctx, targetPath, "")
	if err != nil {
		return err
	}
	if targetHead != "" {
		if _, err := c.run(ctx, targetPath, "reset", "--hard", "--quiet", "HEAD"); err != nil {
			return err
		}
	}
	if _, err := c.run(ctx, targetPath, "clean", "-fdx"); err != nil {
		return err
	}
	if targetHead != "" {
		patch, err := c.run(ctx, sourcePath, "diff", "--binary", "--full-index", "--no-renames", targetHead, "--")
		if err != nil {
			return err
		}
		if patch != "" {
			if _, err := c.runInput(ctx, targetPath, []byte(patch), "apply", "--binary", "--whitespace=nowarn", "-"); err != nil {
				return err
			}
		}
	}
	untracked, err := c.run(ctx, sourcePath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	if targetHead == "" {
		tracked, err := c.run(ctx, sourcePath, "ls-files", "--cached", "-z")
		if err != nil {
			return err
		}
		untracked += tracked
	}
	sourceRoot, err := os.OpenRoot(sourcePath)
	if err != nil {
		return err
	}
	defer sourceRoot.Close()
	targetRoot, err := os.OpenRoot(targetPath)
	if err != nil {
		return err
	}
	defer targetRoot.Close()
	seen := map[string]bool{}
	for _, path := range strings.Split(untracked, "\x00") {
		path = filepath.FromSlash(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if err := copySnapshotEntry(ctx, sourceRoot, targetRoot, path); err != nil {
			return fmt.Errorf("copy untracked worktree file %q: %w", filepath.ToSlash(path), err)
		}
	}
	return nil
}

func copySnapshotEntry(ctx context.Context, source *os.Root, target *os.Root, path string) error {
	if !filepath.IsLocal(path) || path == "." {
		return errors.New("worktree snapshot path is not local")
	}
	info, err := source.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := target.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := target.RemoveAll(path); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := source.Readlink(path)
		if err != nil {
			return err
		}
		return target.Symlink(link, path)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	input, err := source.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	partialPath := path + ".anycode-snapshot-partial"
	_ = target.Remove(partialPath)
	output, err := target.OpenFile(partialPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		_ = input.Close()
		return err
	}
	_, copyErr := copyFileWithContext(ctx, output, input)
	inputCloseErr := input.Close()
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || inputCloseErr != nil || syncErr != nil || closeErr != nil {
		_ = target.Remove(partialPath)
		return errors.Join(copyErr, inputCloseErr, syncErr, closeErr)
	}
	if err := target.Rename(partialPath, path); err != nil {
		_ = target.Remove(partialPath)
		return err
	}
	if err := target.Chmod(path, info.Mode().Perm()); err != nil {
		return err
	}
	return target.Chtimes(path, info.ModTime(), info.ModTime())
}

func copyFileWithContext(ctx context.Context, target io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			count, writeErr := target.Write(buffer[:read])
			written += int64(count)
			if writeErr != nil {
				return written, writeErr
			}
			if count != read {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func (c *Client) InspectOwnership(ctx context.Context, projectPath string, path string, branch string, ownershipToken string) (session.WorktreeOwnership, error) {
	path = absolutePath(strings.TrimSpace(path))
	branch = strings.TrimSpace(branch)
	ownershipToken = strings.TrimSpace(ownershipToken)
	linked, ownership, err := c.inspectWorktreeLink(ctx, projectPath, path, branch)
	if err != nil {
		return ownership, err
	}
	markerPath := ownershipMarkerPath(path)
	content, err := os.ReadFile(markerPath)
	if err == nil {
		ownership.MarkerExists = true
		ownership.TokenMatches = ownershipToken != "" && strings.TrimSpace(string(content)) == ownershipToken
	} else if !errors.Is(err, os.ErrNotExist) {
		return ownership, err
	}
	ownership.Matches = linked && ownership.TokenMatches
	return ownership, nil
}

func (c *Client) inspectWorktreeLink(ctx context.Context, projectPath string, path string, branch string) (bool, session.WorktreeOwnership, error) {
	path = absolutePath(strings.TrimSpace(path))
	branch = strings.TrimSpace(branch)
	ownership := session.WorktreeOwnership{}
	info, err := os.Lstat(path)
	if err == nil {
		ownership.PathExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, ownership, err
	}

	out, err := c.run(ctx, projectPath, "branch", "--list", "--format=%(refname:short)", branch)
	if err != nil {
		return false, ownership, err
	}
	ownership.BranchExists = strings.TrimSpace(out) == branch

	out, err = c.run(ctx, projectPath, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, ownership, err
	}
	registeredBranch, registered := registeredWorktreeBranch(out, path)
	ownership.Registered = registered
	if !registered || registeredBranch != branch || !ownership.PathExists || info == nil || !info.IsDir() {
		return false, ownership, nil
	}
	marker, err := os.Lstat(filepath.Join(path, ".git"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, ownership, nil
		}
		return false, ownership, err
	}
	if !marker.Mode().IsRegular() {
		return false, ownership, nil
	}
	content, err := os.ReadFile(filepath.Join(path, ".git"))
	if err != nil {
		return false, ownership, err
	}
	gitDirValue, ok := strings.CutPrefix(strings.TrimSpace(string(content)), "gitdir: ")
	if !ok {
		return false, ownership, nil
	}
	gitDir := canonicalPath(absolutePathFrom(path, gitDirValue))
	commonDir, err := c.commonGitDir(ctx, projectPath)
	if err != nil {
		return false, ownership, err
	}
	if !pathWithin(commonDir, gitDir) {
		return false, ownership, nil
	}
	reverse, err := os.ReadFile(filepath.Join(gitDir, "gitdir"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, ownership, nil
		}
		return false, ownership, err
	}
	if canonicalPath(absolutePathFrom(gitDir, strings.TrimSpace(string(reverse)))) != canonicalPath(filepath.Join(path, ".git")) {
		return false, ownership, nil
	}
	return true, ownership, nil
}

func registeredWorktreeBranch(output string, path string) (string, bool) {
	path = canonicalPath(path)
	for _, record := range strings.Split(output, "\x00\x00") {
		registeredPath := ""
		branch := ""
		for _, field := range strings.Split(record, "\x00") {
			switch {
			case strings.HasPrefix(field, "worktree "):
				registeredPath = canonicalPath(strings.TrimPrefix(field, "worktree "))
			case strings.HasPrefix(field, "branch refs/heads/"):
				branch = strings.TrimPrefix(field, "branch refs/heads/")
			}
		}
		if registeredPath == path {
			return branch, true
		}
	}
	return "", false
}

func (c *Client) commonGitDir(ctx context.Context, projectPath string) (string, error) {
	out, err := c.run(ctx, projectPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return canonicalPath(absolutePathFrom(projectPath, strings.TrimSpace(out))), nil
}

func (c *Client) ReleaseOwnership(_ context.Context, path string, ownershipToken string) error {
	markerPath := ownershipMarkerPath(absolutePath(path))
	content, err := os.ReadFile(markerPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(string(content)) != strings.TrimSpace(ownershipToken) {
		return errors.New("worktree ownership marker does not match")
	}
	return os.Remove(markerPath)
}

func claimOwnershipMarker(path string, ownershipToken string) error {
	markerPath := ownershipMarkerPath(path)
	temporary, err := os.CreateTemp(filepath.Dir(markerPath), ".anycode-owner-claim-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(strings.TrimSpace(ownershipToken) + "\n"); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Link(temporaryPath, markerPath)
}

func ownershipMarkerPath(path string) string {
	return absolutePath(path) + worktreeOwnershipMarkerSuffix
}

func absolutePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return filepath.Clean(absolute)
	}
	return filepath.Clean(path)
}

func canonicalPath(path string) string {
	path = absolutePath(path)
	if evaluated, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(evaluated)
	}
	return path
}

func absolutePathFrom(base string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return absolutePath(filepath.Join(base, path))
}

func pathWithin(parent string, child string) bool {
	relative, err := filepath.Rel(canonicalPath(parent), canonicalPath(child))
	if err != nil || relative == "." {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
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
		if ctx.Err() != nil {
			return err
		}
		if removeErr := os.RemoveAll(path); removeErr != nil {
			return fmt.Errorf("%w; remove path: %v", err, removeErr)
		}
		return nil
	}
	return nil
}

func (c *Client) DeleteBranch(ctx context.Context, projectPath string, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil
	}
	_, _ = c.run(ctx, projectPath, "worktree", "prune")
	out, err := c.run(ctx, projectPath, "branch", "--list", "--format=%(refname:short)", branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	_, err = c.run(ctx, projectPath, "branch", "-D", branch)
	return err
}

func (c *Client) run(ctx context.Context, path string, args ...string) (string, error) {
	return c.runInput(ctx, path, nil, args...)
}

func (c *Client) runInput(ctx context.Context, path string, input []byte, args ...string) (string, error) {
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
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
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
