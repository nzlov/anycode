package gitdiffcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nzlov/anycode/internal/domain/gitdiff"
)

const defaultGitBin = "git"

var hunkHeaderPattern = regexp.MustCompile(`^@@ -([0-9]+)(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@`)

type Client struct {
	gitBin string
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
	msg := fmt.Sprintf("git diff %s", e.Code)
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

func (c *Client) MergeToBase(ctx context.Context, input gitdiff.MergeInput) (gitdiff.MergeResult, error) {
	return c.mergeToBase(ctx, "merge", input.WorktreePath, input.BaseBranch)
}

func (c *Client) RebaseOntoBase(ctx context.Context, input gitdiff.RebaseInput) (gitdiff.MergeResult, error) {
	return c.mergeToBase(ctx, "rebase", input.WorktreePath, input.BaseBranch)
}

func (c *Client) Abort(ctx context.Context, worktreePath string) error {
	if _, err := c.run(ctx, worktreePath, "merge", "--abort"); err == nil {
		return nil
	}
	if _, err := c.run(ctx, worktreePath, "rebase", "--abort"); err == nil {
		return nil
	}
	return nil
}

func (c *Client) ChangedFiles(ctx context.Context, input gitdiff.DiffInput) ([]gitdiff.DiffFile, error) {
	refs := diffRefs(input)
	nameStatusArgs := append([]string{"diff", "--name-status"}, refs...)
	nameStatusArgs = append(nameStatusArgs, "--")
	nameStatus, err := c.run(ctx, input.WorktreePath, nameStatusArgs...)
	if err != nil {
		return nil, err
	}
	numstatArgs := append([]string{"diff", "--numstat"}, refs...)
	numstatArgs = append(numstatArgs, "--")
	numstat, err := c.run(ctx, input.WorktreePath, numstatArgs...)
	if err != nil {
		return nil, err
	}
	counts := parseNumstat(numstat)
	files := parseNameStatus(nameStatus)
	for i := range files {
		if count, ok := counts[files[i].Path]; ok {
			files[i].Additions = count.additions
			files[i].Deletions = count.deletions
		}
	}
	if strings.TrimSpace(input.HeadRef) == "" {
		untracked, err := c.untrackedFiles(ctx, input.WorktreePath)
		if err != nil {
			return nil, err
		}
		files = append(files, untracked...)
	}
	return files, nil
}

func (c *Client) mergeToBase(ctx context.Context, strategy string, worktreePath string, baseBranch string) (gitdiff.MergeResult, error) {
	baseBranch = normalizedBaseRef(baseBranch)
	result := gitdiff.MergeResult{Strategy: strategy, BaseBranch: baseBranch}
	worktreeBranch, err := c.currentBranch(ctx, worktreePath)
	if err != nil {
		return result, err
	}
	result.WorktreeBranch = worktreeBranch
	baseCommit, err := c.revParse(ctx, worktreePath, baseBranch)
	if err != nil {
		return result, err
	}
	headCommit, err := c.revParse(ctx, worktreePath, "HEAD")
	if err != nil {
		return result, err
	}
	result.BaseCommit = baseCommit
	result.HeadCommit = headCommit
	if dirty, err := c.isDirty(ctx, worktreePath); err != nil {
		return result, err
	} else if dirty {
		result.Status = "failed"
		result.FailureCode = "dirty_worktree"
		result.FailureReason = "worktree has uncommitted changes"
		return result, nil
	}
	switch strategy {
	case "rebase":
		return c.rebaseOntoBase(ctx, result, worktreePath, baseBranch)
	default:
		return c.mergeHeadToBase(ctx, result, worktreePath, baseBranch, baseCommit, headCommit, worktreeBranch)
	}
}

func (c *Client) mergeHeadToBase(ctx context.Context, result gitdiff.MergeResult, worktreePath string, baseBranch string, baseCommit string, headCommit string, worktreeBranch string) (gitdiff.MergeResult, error) {
	if _, err := c.run(ctx, worktreePath, "merge-base", "--is-ancestor", baseCommit, headCommit); err == nil {
		if err := c.updateBranch(ctx, worktreePath, baseBranch, headCommit); err != nil {
			result.Status = "failed"
			result.FailureCode = "merge_failed"
			result.FailureReason = err.Error()
			return result, nil
		}
		result.Status = "merged"
		result.MergeCommit = headCommit
		return result, nil
	}
	if _, err := c.run(ctx, worktreePath, "switch", "--detach", baseCommit); err != nil {
		return result, err
	}
	defer func() { _, _ = c.run(context.Background(), worktreePath, "switch", worktreeBranch) }()
	if _, err := c.run(ctx, worktreePath, "merge", "--no-ff", "--no-edit", headCommit); err != nil {
		_ = c.Abort(context.Background(), worktreePath)
		result.Status = statusFromGitError(err)
		result.FailureCode = result.Status
		result.FailureReason = err.Error()
		return result, nil
	}
	mergeCommit, err := c.revParse(ctx, worktreePath, "HEAD")
	if err != nil {
		return result, err
	}
	if err := c.updateBranch(ctx, worktreePath, baseBranch, mergeCommit); err != nil {
		result.Status = "failed"
		result.FailureCode = "merge_failed"
		result.FailureReason = err.Error()
		return result, nil
	}
	result.Status = "merged"
	result.MergeCommit = mergeCommit
	return result, nil
}

func (c *Client) rebaseOntoBase(ctx context.Context, result gitdiff.MergeResult, worktreePath string, baseBranch string) (gitdiff.MergeResult, error) {
	if _, err := c.run(ctx, worktreePath, "rebase", baseBranch); err != nil {
		_ = c.Abort(context.Background(), worktreePath)
		result.Status = statusFromGitError(err)
		result.FailureCode = result.Status
		result.FailureReason = err.Error()
		return result, nil
	}
	headCommit, err := c.revParse(ctx, worktreePath, "HEAD")
	if err != nil {
		return result, err
	}
	if err := c.updateBranch(ctx, worktreePath, baseBranch, headCommit); err != nil {
		result.Status = "failed"
		result.FailureCode = "merge_failed"
		result.FailureReason = err.Error()
		return result, nil
	}
	result.HeadCommit = headCommit
	result.MergeCommit = headCommit
	result.Status = "merged"
	return result, nil
}

func (c *Client) currentBranch(ctx context.Context, path string) (string, error) {
	out, err := c.run(ctx, path, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(out)
	if branch == "" {
		return "", errors.New("worktree is not on a branch")
	}
	return branch, nil
}

func (c *Client) revParse(ctx context.Context, path string, ref string) (string, error) {
	out, err := c.run(ctx, path, "rev-parse", strings.TrimSpace(ref))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Client) isDirty(ctx context.Context, path string) (bool, error) {
	out, err := c.run(ctx, path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func (c *Client) updateBranch(ctx context.Context, path string, branch string, commit string) error {
	_, err := c.run(ctx, path, "branch", "-f", branch, commit)
	return err
}

func (c *Client) FileDiff(ctx context.Context, input gitdiff.FileDiffInput) (gitdiff.FileDiff, error) {
	if strings.TrimSpace(input.FilePath) == "" {
		return gitdiff.FileDiff{}, errors.New("file path is required")
	}
	refs := diffRefs(input.DiffInput)
	if stats, ok, err := c.fileStats(ctx, input.DiffInput, input.FilePath); err != nil {
		return gitdiff.FileDiff{}, err
	} else if ok {
		if strings.TrimSpace(input.HeadRef) == "" && stats.Status == "added" && isUntracked(ctx, c, input.WorktreePath, input.FilePath) {
			hunk, err := untrackedFileDiff(input.WorktreePath, input.FilePath)
			if err != nil {
				return gitdiff.FileDiff{}, err
			}
			return gitdiff.FileDiff{File: stats, Hunks: hunk}, nil
		}
		args := append([]string{"diff", "--unified=80"}, refs...)
		args = append(args, "--", input.FilePath)
		out, err := c.run(ctx, input.WorktreePath, args...)
		if err != nil {
			return gitdiff.FileDiff{}, err
		}
		return gitdiff.FileDiff{File: stats, Hunks: parseUnifiedDiff(out)}, nil
	}
	args := append([]string{"diff", "--unified=80"}, refs...)
	args = append(args, "--", input.FilePath)
	out, err := c.run(ctx, input.WorktreePath, args...)
	if err != nil {
		return gitdiff.FileDiff{}, err
	}
	return gitdiff.FileDiff{File: gitdiff.DiffFile{Path: input.FilePath}, Hunks: parseUnifiedDiff(out)}, nil
}

func (c *Client) RangeDiff(ctx context.Context, input gitdiff.RangeDiffInput) (gitdiff.SessionDiff, error) {
	diffInput := gitdiff.DiffInput{
		WorktreePath: input.RepoPath,
		BaseRef:      input.BaseRef,
		HeadRef:      input.HeadRef,
	}
	files, err := c.ChangedFiles(ctx, diffInput)
	if err != nil {
		return gitdiff.SessionDiff{}, err
	}
	hunks := make([]gitdiff.FileDiff, 0, len(files))
	for _, file := range files {
		fileDiff, err := c.FileDiff(ctx, gitdiff.FileDiffInput{DiffInput: diffInput, FilePath: file.Path})
		if err != nil {
			return gitdiff.SessionDiff{}, err
		}
		hunks = append(hunks, fileDiff)
	}
	return gitdiff.SessionDiff{Files: files, Hunks: hunks}, nil
}

func (c *Client) fileStats(ctx context.Context, input gitdiff.DiffInput, filePath string) (gitdiff.DiffFile, bool, error) {
	files, err := c.ChangedFiles(ctx, gitdiff.DiffInput{WorktreePath: input.WorktreePath, BaseRef: input.BaseRef, HeadRef: input.HeadRef})
	if err != nil {
		return gitdiff.DiffFile{}, false, err
	}
	for _, file := range files {
		if file.Path == filePath {
			return file, true, nil
		}
	}
	return gitdiff.DiffFile{}, false, nil
}

func (c *Client) untrackedFiles(ctx context.Context, path string) ([]gitdiff.DiffFile, error) {
	out, err := c.run(ctx, path, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	files := []gitdiff.DiffFile{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		files = append(files, gitdiff.DiffFile{
			Path:      name,
			Status:    "added",
			Additions: countFileLines(filepath.Join(path, name)),
		})
	}
	return files, nil
}

func isUntracked(ctx context.Context, c *Client, worktreePath string, filePath string) bool {
	untracked, err := c.untrackedFiles(ctx, worktreePath)
	if err != nil {
		return false
	}
	for _, file := range untracked {
		if file.Path == filePath {
			return true
		}
	}
	return false
}

func untrackedFileDiff(worktreePath string, filePath string) ([]gitdiff.DiffHunk, error) {
	body, err := os.ReadFile(filepath.Join(worktreePath, filePath))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	diffLines := make([]gitdiff.DiffLine, 0, len(lines))
	for _, line := range lines {
		diffLines = append(diffLines, gitdiff.DiffLine{Kind: "add", Content: "+" + line})
	}
	return []gitdiff.DiffHunk{{
		Header:   fmt.Sprintf("@@ -0,0 +1,%d @@", len(lines)),
		OldStart: 0,
		NewStart: 1,
		Lines:    diffLines,
	}}, nil
}

func countFileLines(path string) int {
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 {
		return 0
	}
	return strings.Count(strings.TrimSuffix(string(body), "\n"), "\n") + 1
}

func (c *Client) run(ctx context.Context, path string, args ...string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("git diff path is required")
	}
	gitBin := c.gitBin
	if gitBin == "" {
		gitBin = defaultGitBin
	}
	allArgs := append([]string{"-C", path}, args...)
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

type lineCount struct {
	additions int
	deletions int
}

func parseNumstat(out string) map[string]lineCount {
	counts := make(map[string]lineCount)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			continue
		}
		counts[normalizeDiffPath(fields[len(fields)-1])] = lineCount{
			additions: parseCount(fields[0]),
			deletions: parseCount(fields[1]),
		}
	}
	return counts
}

func parseNameStatus(out string) []gitdiff.DiffFile {
	files := []gitdiff.DiffFile{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		files = append(files, gitdiff.DiffFile{
			Path:   normalizeDiffPath(fields[len(fields)-1]),
			Status: statusName(fields[0]),
		})
	}
	return files
}

func parseUnifiedDiff(out string) []gitdiff.DiffHunk {
	hunks := []gitdiff.DiffHunk{}
	var current *gitdiff.DiffHunk
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "@@") {
			if current != nil {
				hunks = append(hunks, *current)
			}
			oldStart, newStart := parseHunkStarts(line)
			current = &gitdiff.DiffHunk{Header: line, OldStart: oldStart, NewStart: newStart}
			continue
		}
		if current == nil {
			continue
		}
		current.Lines = append(current.Lines, gitdiff.DiffLine{
			Kind:    lineKind(line),
			Content: line,
		})
	}
	if current != nil {
		hunks = append(hunks, *current)
	}
	return hunks
}

func parseHunkStarts(header string) (int, int) {
	matches := hunkHeaderPattern.FindStringSubmatch(header)
	if len(matches) != 3 {
		return 0, 0
	}
	oldStart, _ := strconv.Atoi(matches[1])
	newStart, _ := strconv.Atoi(matches[2])
	return oldStart, newStart
}

func lineKind(line string) string {
	switch {
	case strings.HasPrefix(line, "+"):
		return "add"
	case strings.HasPrefix(line, "-"):
		return "delete"
	default:
		return "context"
	}
}

func statusName(status string) string {
	switch {
	case strings.HasPrefix(status, "A"):
		return "added"
	case strings.HasPrefix(status, "D"):
		return "deleted"
	case strings.HasPrefix(status, "R"):
		return "renamed"
	case strings.HasPrefix(status, "C"):
		return "copied"
	default:
		return "modified"
	}
}

func parseCount(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

func normalizeDiffPath(path string) string {
	return strings.TrimSpace(strings.Trim(path, "\""))
}

func normalizedBaseRef(baseRef string) string {
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		return "HEAD"
	}
	return baseRef
}

func diffRefs(input gitdiff.DiffInput) []string {
	refs := []string{normalizedBaseRef(input.BaseRef)}
	if headRef := strings.TrimSpace(input.HeadRef); headRef != "" {
		refs = append(refs, headRef)
	}
	return refs
}

func classify(err error, stderr string) string {
	stderr = strings.ToLower(stderr)
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "canceled"
	case strings.Contains(stderr, "conflict"), strings.Contains(stderr, "automatic merge failed"), strings.Contains(stderr, "fix conflicts"):
		return "merge_conflict"
	case strings.Contains(stderr, "not a git repository"):
		return "not_git_repository"
	case strings.Contains(stderr, "bad revision"), strings.Contains(stderr, "unknown revision"), strings.Contains(stderr, "ambiguous argument"):
		return "revision_not_found"
	case strings.Contains(stderr, "permission denied"):
		return "permission_denied"
	case errors.Is(err, os.ErrNotExist):
		return "git_not_found"
	default:
		return "command_failed"
	}
}

func statusFromGitError(err error) string {
	var gitErr *Error
	if errors.As(err, &gitErr) && gitErr.Code == "merge_conflict" {
		return "merge_conflict"
	}
	return "failed"
}
