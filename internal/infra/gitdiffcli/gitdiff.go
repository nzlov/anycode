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
const defaultDiffContextLines = 10
const diffContextExpandStep = 20

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

func (c *Client) CurrentBranch(ctx context.Context, path string) (string, error) {
	return c.currentBranch(ctx, path)
}

func (c *Client) ResolveSessionDiffSource(ctx context.Context, input gitdiff.ResolveSessionDiffInput) (gitdiff.DiffInput, bool, error) {
	projectPath := strings.TrimSpace(input.ProjectPath)
	worktreePath := strings.TrimSpace(input.WorktreePath)
	baseBranch := strings.TrimSpace(input.BaseBranch)
	worktreeBranch := strings.TrimSpace(input.WorktreeBranch)
	baseCommit := strings.TrimSpace(input.WorktreeBaseCommit)
	required := []struct {
		name  string
		value string
	}{
		{name: "project path", value: projectPath},
		{name: "base branch", value: baseBranch},
		{name: "worktree branch", value: worktreeBranch},
		{name: "worktree base commit", value: baseCommit},
	}
	for _, field := range required {
		if field.value == "" {
			return gitdiff.DiffInput{}, false, fmt.Errorf("%w: %s is required", gitdiff.ErrSessionDiffInvariant, field.name)
		}
	}
	if worktreePath != "" {
		if info, err := os.Stat(worktreePath); err == nil {
			if info.IsDir() {
				usable, err := c.isUsableProjectWorktree(ctx, projectPath, worktreePath)
				if err != nil {
					return gitdiff.DiffInput{}, false, err
				}
				if usable {
					return gitdiff.DiffInput{WorktreePath: worktreePath, BaseRef: baseCommit}, true, nil
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return gitdiff.DiffInput{}, false, err
		}
	}

	mergeSubject := "Merge branch '" + worktreeBranch + "'"
	out, err := c.run(ctx, projectPath, "log", "--first-parent", "--merges", "--fixed-strings", "--grep="+mergeSubject, "--format=%H%x00%P%x00%s", baseBranch)
	if err != nil {
		return gitdiff.DiffInput{}, false, err
	}
	type candidate struct {
		commit      string
		firstParent string
	}
	var found *candidate
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		commit, rest, ok := strings.Cut(line, "\x00")
		if !ok {
			continue
		}
		parents, subject, ok := strings.Cut(rest, "\x00")
		if !ok {
			continue
		}
		subject = strings.TrimSpace(subject)
		if subject != mergeSubject && !strings.HasPrefix(subject, mergeSubject+" into ") {
			continue
		}
		parentCommits := strings.Fields(parents)
		if len(parentCommits) != 2 {
			continue
		}
		mergeBase, err := c.run(ctx, projectPath, "merge-base", baseCommit, parentCommits[1])
		if err != nil {
			return gitdiff.DiffInput{}, false, err
		}
		if strings.TrimSpace(mergeBase) != baseCommit {
			continue
		}
		if found != nil {
			return gitdiff.DiffInput{}, false, fmt.Errorf("%w: branch %q has multiple merge commits", gitdiff.ErrAmbiguousSessionMerge, worktreeBranch)
		}
		found = &candidate{commit: strings.TrimSpace(commit), firstParent: parentCommits[0]}
	}
	if found == nil {
		return gitdiff.DiffInput{}, false, nil
	}
	return gitdiff.DiffInput{
		WorktreePath: projectPath,
		BaseRef:      found.firstParent,
		HeadRef:      found.commit,
	}, true, nil
}

func (c *Client) isUsableProjectWorktree(ctx context.Context, projectPath string, worktreePath string) (bool, error) {
	topLevel, err := c.run(ctx, worktreePath, "rev-parse", "--show-toplevel")
	if err != nil {
		var gitErr *Error
		if errors.As(err, &gitErr) && gitErr.Code == "not_git_repository" {
			return false, nil
		}
		return false, err
	}
	wantTopLevel, err := canonicalGitPath("", worktreePath)
	if err != nil {
		return false, err
	}
	gotTopLevel, err := canonicalGitPath(worktreePath, strings.TrimSpace(topLevel))
	if err != nil {
		return false, err
	}
	if gotTopLevel != wantTopLevel {
		return false, nil
	}
	worktreeCommonDir, err := c.run(ctx, worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return false, err
	}
	projectCommonDir, err := c.run(ctx, projectPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return false, err
	}
	worktreeCommonPath, err := canonicalGitPath(worktreePath, strings.TrimSpace(worktreeCommonDir))
	if err != nil {
		return false, err
	}
	projectCommonPath, err := canonicalGitPath(projectPath, strings.TrimSpace(projectCommonDir))
	if err != nil {
		return false, err
	}
	return worktreeCommonPath == projectCommonPath, nil
}

func canonicalGitPath(basePath string, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(basePath, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		absPath = resolvedPath
	}
	return filepath.Clean(absPath), nil
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
	refs, err := c.diffRefs(ctx, input)
	if err != nil {
		return nil, err
	}
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
	refs, err := c.diffRefs(ctx, input.DiffInput)
	if err != nil {
		return gitdiff.FileDiff{}, err
	}
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
		contextBefore, contextAfter := normalizeDiffContext(input.ContextBefore, input.ContextAfter)
		args := append([]string{"diff", fmt.Sprintf("--unified=%d", probeDiffContext(contextBefore, contextAfter))}, refs...)
		args = append(args, "--", input.FilePath)
		out, err := c.run(ctx, input.WorktreePath, args...)
		if err != nil {
			return gitdiff.FileDiff{}, err
		}
		return gitdiff.FileDiff{File: stats, Hunks: trimDiffHunks(parseUnifiedDiff(out), contextBefore, contextAfter)}, nil
	}
	contextBefore, contextAfter := normalizeDiffContext(input.ContextBefore, input.ContextAfter)
	args := append([]string{"diff", fmt.Sprintf("--unified=%d", probeDiffContext(contextBefore, contextAfter))}, refs...)
	args = append(args, "--", input.FilePath)
	out, err := c.run(ctx, input.WorktreePath, args...)
	if err != nil {
		return gitdiff.FileDiff{}, err
	}
	return gitdiff.FileDiff{File: gitdiff.DiffFile{Path: input.FilePath}, Hunks: trimDiffHunks(parseUnifiedDiff(out), contextBefore, contextAfter)}, nil
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
		fileDiff, err := c.FileDiff(ctx, gitdiff.FileDiffInput{
			DiffInput:     diffInput,
			FilePath:      file.Path,
			ContextBefore: input.ContextBefore,
			ContextAfter:  input.ContextAfter,
		})
		if err != nil {
			return gitdiff.SessionDiff{}, err
		}
		hunks = append(hunks, fileDiff)
	}
	return gitdiff.SessionDiff{Files: files, Hunks: hunks}, nil
}

func (c *Client) CommitHistory(ctx context.Context, input gitdiff.CommitHistoryInput) ([]gitdiff.CommitRecord, error) {
	baseRef := strings.TrimSpace(input.BaseRef)
	headRef := strings.TrimSpace(input.HeadRef)
	if headRef == "" {
		headRef = "HEAD"
	}
	refRange := headRef
	if baseRef != "" {
		refRange = baseRef + ".." + headRef
	}
	out, err := c.run(ctx, strings.TrimSpace(input.WorktreePath), "log", "--date=iso-strict", "--format=%H%x1f%h%x1f%an%x1f%ae%x1f%aI%x1f%s", refRange)
	if err != nil {
		return nil, err
	}
	return parseCommitHistory(out), nil
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

func normalizeDiffContext(before int, after int) (int, int) {
	if before < 1 {
		before = defaultDiffContextLines
	}
	if after < 1 {
		after = defaultDiffContextLines
	}
	return before, after
}

func probeDiffContext(before int, after int) int {
	if before > after {
		return before + diffContextExpandStep
	}
	return after + diffContextExpandStep
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

func parseCommitHistory(out string) []gitdiff.CommitRecord {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return []gitdiff.CommitRecord{}
	}
	commits := []gitdiff.CommitRecord{}
	for _, line := range strings.Split(trimmed, "\n") {
		parts := strings.SplitN(line, "\x1f", 6)
		if len(parts) != 6 {
			continue
		}
		commits = append(commits, gitdiff.CommitRecord{
			Hash:        parts[0],
			ShortHash:   parts[1],
			AuthorName:  parts[2],
			AuthorEmail: parts[3],
			CreatedAt:   parts[4],
			Subject:     parts[5],
		})
	}
	return commits
}

func parseUnifiedDiff(out string) []gitdiff.DiffHunk {
	hunks := []gitdiff.DiffHunk{}
	var current *gitdiff.DiffHunk
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
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

func trimDiffHunks(hunks []gitdiff.DiffHunk, contextBefore int, contextAfter int) []gitdiff.DiffHunk {
	trimmed := make([]gitdiff.DiffHunk, 0, len(hunks))
	for _, hunk := range hunks {
		changeStart := -1
		changeEnd := -1
		for i, line := range hunk.Lines {
			if line.Kind == "add" || line.Kind == "delete" {
				if changeStart == -1 {
					changeStart = i
					changeEnd = i
					continue
				}
				currentEnd := changeEnd + contextAfter + 1
				nextStart := i - contextBefore
				if nextStart < 0 {
					nextStart = 0
				}
				if nextStart <= currentEnd {
					changeEnd = i
					continue
				}
				trimmed = append(trimmed, trimHunkWindow(hunk, changeStart, changeEnd, contextBefore, contextAfter))
				changeStart = i
				changeEnd = i
			}
		}
		if changeStart == -1 {
			trimmed = append(trimmed, hunk)
			continue
		}
		trimmed = append(trimmed, trimHunkWindow(hunk, changeStart, changeEnd, contextBefore, contextAfter))
	}
	return trimmed
}

func trimHunkWindow(hunk gitdiff.DiffHunk, firstChange int, lastChange int, contextBefore int, contextAfter int) gitdiff.DiffHunk {
	originalLen := len(hunk.Lines)
	start := firstChange - contextBefore
	if start < 0 {
		start = 0
	}
	end := lastChange + contextAfter + 1
	if end > len(hunk.Lines) {
		end = len(hunk.Lines)
	}
	oldStart, newStart := lineStartsAt(hunk, start)
	lines := append([]gitdiff.DiffLine(nil), hunk.Lines[start:end]...)
	hunk.Lines = lines
	hunk.OldStart = oldStart
	hunk.NewStart = newStart
	oldCount, newCount := diffLineCounts(lines)
	hunk.Header = fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount)
	hunk.CanExpandBefore = start > 0
	hunk.CanExpandAfter = end < originalLen
	return hunk
}

func lineStartsAt(hunk gitdiff.DiffHunk, target int) (int, int) {
	oldLine := hunk.OldStart
	newLine := hunk.NewStart
	for i, line := range hunk.Lines {
		if i == target {
			return oldLine, newLine
		}
		switch line.Kind {
		case "add":
			newLine++
		case "delete":
			oldLine++
		default:
			oldLine++
			newLine++
		}
	}
	return oldLine, newLine
}

func diffLineCounts(lines []gitdiff.DiffLine) (int, int) {
	oldCount := 0
	newCount := 0
	for _, line := range lines {
		switch line.Kind {
		case "add":
			newCount++
		case "delete":
			oldCount++
		default:
			oldCount++
			newCount++
		}
	}
	return oldCount, newCount
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

func (c *Client) diffRefs(ctx context.Context, input gitdiff.DiffInput) ([]string, error) {
	baseRef, err := c.diffBaseRef(ctx, input)
	if err != nil {
		return nil, err
	}
	refs := []string{baseRef}
	if headRef := strings.TrimSpace(input.HeadRef); headRef != "" {
		refs = append(refs, headRef)
	}
	return refs, nil
}

func (c *Client) diffBaseRef(ctx context.Context, input gitdiff.DiffInput) (string, error) {
	baseRef := normalizedBaseRef(input.BaseRef)
	if !strings.HasSuffix(baseRef, "...") {
		return baseRef, nil
	}
	baseRef = strings.TrimSpace(strings.TrimSuffix(baseRef, "..."))
	if baseRef == "" {
		baseRef = "HEAD"
	}
	headRef := strings.TrimSpace(input.HeadRef)
	if headRef == "" {
		headRef = "HEAD"
	}
	out, err := c.run(ctx, input.WorktreePath, "merge-base", baseRef, headRef)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
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
