package gitdiffcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nzlov/anycode/internal/domain/gitdiff"
)

func TestChangedFilesAndFileDiff(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	writeFile(t, repo, "main.go", "package main\n\nfunc main() {}\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	writeFile(t, repo, "main.go", "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n")
	writeFile(t, repo, "README.md", "# Demo\n")

	client := New("")
	files, err := client.ChangedFiles(ctx, gitdiff.DiffInput{WorktreePath: repo, BaseRef: "HEAD"})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ChangedFiles() len = %d, files = %#v", len(files), files)
	}
	paths := []string{files[0].Path, files[1].Path}
	if !slices.Contains(paths, "main.go") || !slices.Contains(paths, "README.md") {
		t.Fatalf("ChangedFiles() paths = %#v", paths)
	}
	mainFile := findFile(files, "main.go")
	if mainFile.Status != "modified" || mainFile.Additions == 0 || mainFile.Deletions == 0 {
		t.Fatalf("main.go diff file = %#v", mainFile)
	}
	readmeFile := findFile(files, "README.md")
	if readmeFile.Status != "added" || readmeFile.Additions == 0 {
		t.Fatalf("README.md diff file = %#v", readmeFile)
	}

	fileDiff, err := client.FileDiff(ctx, gitdiff.FileDiffInput{
		DiffInput: gitdiff.DiffInput{WorktreePath: repo, BaseRef: "HEAD"},
		FilePath:  "main.go",
	})
	if err != nil {
		t.Fatalf("FileDiff() error = %v", err)
	}
	if fileDiff.File.Path != "main.go" || fileDiff.File.Status != "modified" {
		t.Fatalf("FileDiff() file = %#v", fileDiff.File)
	}
	if len(fileDiff.Hunks) != 1 || fileDiff.Hunks[0].OldStart != 1 || fileDiff.Hunks[0].NewStart != 1 {
		t.Fatalf("FileDiff() hunks = %#v", fileDiff.Hunks)
	}
	if !hasKind(fileDiff.Hunks[0].Lines, "add") || !hasKind(fileDiff.Hunks[0].Lines, "delete") {
		t.Fatalf("FileDiff() lines = %#v", fileDiff.Hunks[0].Lines)
	}

	untrackedDiff, err := client.FileDiff(ctx, gitdiff.FileDiffInput{
		DiffInput: gitdiff.DiffInput{WorktreePath: repo, BaseRef: "HEAD"},
		FilePath:  "README.md",
	})
	if err != nil {
		t.Fatalf("FileDiff(untracked) error = %v", err)
	}
	if untrackedDiff.File.Status != "added" || len(untrackedDiff.Hunks) != 1 || !hasKind(untrackedDiff.Hunks[0].Lines, "add") {
		t.Fatalf("FileDiff(untracked) = %#v", untrackedDiff)
	}
}

func TestFileDiffDefaultsToTenContextLines(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	lines := make([]string, 0, 41)
	for i := 1; i <= 41; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	writeFile(t, repo, "story.txt", strings.Join(lines, "\n")+"\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	lines[20] = "line 21 changed"
	writeFile(t, repo, "story.txt", strings.Join(lines, "\n")+"\n")

	got, err := New("").FileDiff(ctx, gitdiff.FileDiffInput{
		DiffInput: gitdiff.DiffInput{WorktreePath: repo, BaseRef: "HEAD"},
		FilePath:  "story.txt",
	})
	if err != nil {
		t.Fatalf("FileDiff() error = %v", err)
	}
	if len(got.Hunks) != 1 {
		t.Fatalf("FileDiff() hunks = %#v", got.Hunks)
	}
	if got.Hunks[0].OldStart != 11 || got.Hunks[0].NewStart != 11 {
		t.Fatalf("FileDiff() starts = old %d new %d, want 11/11", got.Hunks[0].OldStart, got.Hunks[0].NewStart)
	}
	if len(got.Hunks[0].Lines) != 22 {
		t.Fatalf("FileDiff() line count = %d, want 22", len(got.Hunks[0].Lines))
	}
}

func TestFileDiffSupportsAsymmetricContextExpansion(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	lines := make([]string, 0, 81)
	for i := 1; i <= 81; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	writeFile(t, repo, "story.txt", strings.Join(lines, "\n")+"\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	lines[40] = "line 41 changed"
	writeFile(t, repo, "story.txt", strings.Join(lines, "\n")+"\n")

	got, err := New("").FileDiff(ctx, gitdiff.FileDiffInput{
		DiffInput:     gitdiff.DiffInput{WorktreePath: repo, BaseRef: "HEAD"},
		FilePath:      "story.txt",
		ContextBefore: 30,
		ContextAfter:  10,
	})
	if err != nil {
		t.Fatalf("FileDiff() error = %v", err)
	}
	if len(got.Hunks) != 1 {
		t.Fatalf("FileDiff() hunks = %#v", got.Hunks)
	}
	hunk := got.Hunks[0]
	if hunk.OldStart != 11 || hunk.NewStart != 11 {
		t.Fatalf("FileDiff() starts = old %d new %d, want 11/11", hunk.OldStart, hunk.NewStart)
	}
	if len(hunk.Lines) != 42 {
		t.Fatalf("FileDiff() line count = %d, want 42", len(hunk.Lines))
	}
	if !hunk.CanExpandBefore || !hunk.CanExpandAfter {
		t.Fatalf("FileDiff() expand flags = before %v after %v, want both true", hunk.CanExpandBefore, hunk.CanExpandAfter)
	}
}

func TestFileDiffSplitsMergedGitHunkIntoContextWindows(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	lines := make([]string, 0, 120)
	for i := 1; i <= 120; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	writeFile(t, repo, "story.txt", strings.Join(lines, "\n")+"\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	lines[40] = "line 41 changed"
	lines[74] = "line 75 changed"
	writeFile(t, repo, "story.txt", strings.Join(lines, "\n")+"\n")

	got, err := New("").FileDiff(ctx, gitdiff.FileDiffInput{
		DiffInput: gitdiff.DiffInput{WorktreePath: repo, BaseRef: "HEAD"},
		FilePath:  "story.txt",
	})
	if err != nil {
		t.Fatalf("FileDiff() error = %v", err)
	}
	if len(got.Hunks) != 2 {
		t.Fatalf("FileDiff() hunks = %#v", got.Hunks)
	}
	for i, hunk := range got.Hunks {
		if len(hunk.Lines) != 22 {
			t.Fatalf("hunk %d line count = %d, want 22", i, len(hunk.Lines))
		}
	}
	if !got.Hunks[0].CanExpandAfter || !got.Hunks[1].CanExpandBefore {
		t.Fatalf("expand flags = first after %v second before %v", got.Hunks[0].CanExpandAfter, got.Hunks[1].CanExpandBefore)
	}
}

func TestChangedFilesThreeDotUsesMergeBaseForLiveWorktree(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	writeFile(t, repo, "base.txt", "base\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	runGit(t, repo, "switch", "-c", "session-1")
	writeFile(t, repo, "work.txt", "work\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "work")
	runGit(t, repo, "switch", "main")
	writeFile(t, repo, "main-only.txt", "main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "main only")
	runGit(t, repo, "switch", "session-1")
	writeFile(t, repo, "scratch.txt", "scratch\n")

	files, err := New("").ChangedFiles(ctx, gitdiff.DiffInput{WorktreePath: repo, BaseRef: "main..."})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if findFile(files, "main-only.txt").Path != "" {
		t.Fatalf("ChangedFiles() included base branch only file: %#v", files)
	}
	if findFile(files, "work.txt").Status != "added" {
		t.Fatalf("work.txt missing from files: %#v", files)
	}
	if findFile(files, "scratch.txt").Status != "added" {
		t.Fatalf("scratch.txt missing from files: %#v", files)
	}
}

func TestChangedFilesThreeDotIncludesTrackedWorktreeChanges(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	writeFile(t, repo, "base.txt", "base\n")
	writeFile(t, repo, "tracked.txt", "old\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "base")
	runGit(t, repo, "switch", "-c", "session-1")
	runGit(t, repo, "switch", "main")
	writeFile(t, repo, "main-only.txt", "main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "main only")
	runGit(t, repo, "switch", "session-1")
	writeFile(t, repo, "tracked.txt", "new\n")

	files, err := New("").ChangedFiles(ctx, gitdiff.DiffInput{WorktreePath: repo, BaseRef: "main..."})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if findFile(files, "main-only.txt").Path != "" {
		t.Fatalf("ChangedFiles() included base branch only file: %#v", files)
	}
	if findFile(files, "tracked.txt").Status != "modified" {
		t.Fatalf("tracked worktree change missing from files: %#v", files)
	}
}

func TestMergeToBaseFastForwardsBaseBranch(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	writeFile(t, repo, "main.go", "package main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	baseCommit := gitOutput(t, repo, "rev-parse", "main")
	runGit(t, repo, "switch", "-c", "feature/card-1")
	writeFile(t, repo, "main.go", "package main\n\nfunc main() {}\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "feature")
	headCommit := gitOutput(t, repo, "rev-parse", "HEAD")

	got, err := New("").MergeToBase(ctx, gitdiff.MergeInput{WorktreePath: repo, BaseBranch: "main"})
	if err != nil {
		t.Fatalf("MergeToBase() error = %v", err)
	}
	if got.Status != "merged" || got.Strategy != "merge" || got.BaseCommit != baseCommit || got.HeadCommit != headCommit || got.MergeCommit != headCommit {
		t.Fatalf("MergeToBase() = %#v", got)
	}
	if mainHead := gitOutput(t, repo, "rev-parse", "main"); mainHead != headCommit {
		t.Fatalf("main head = %q, want %q", mainHead, headCommit)
	}
	if branch := gitOutput(t, repo, "branch", "--show-current"); branch != "feature/card-1" {
		t.Fatalf("current branch = %q", branch)
	}
}

func TestMergeToBaseRejectsDirtyWorktree(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	writeFile(t, repo, "main.go", "package main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	runGit(t, repo, "switch", "-c", "feature/card-1")
	writeFile(t, repo, "main.go", "package main\n\nfunc main() {}\n")

	got, err := New("").MergeToBase(ctx, gitdiff.MergeInput{WorktreePath: repo, BaseBranch: "main"})
	if err != nil {
		t.Fatalf("MergeToBase() error = %v", err)
	}
	if got.Status != "failed" || got.FailureCode != "dirty_worktree" {
		t.Fatalf("MergeToBase() = %#v", got)
	}
}

func TestRebaseOntoBaseUpdatesBaseBranch(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	writeFile(t, repo, "main.go", "package main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	runGit(t, repo, "switch", "-c", "feature/card-1")
	writeFile(t, repo, "feature.go", "package main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "feature")

	got, err := New("").RebaseOntoBase(ctx, gitdiff.RebaseInput{WorktreePath: repo, BaseBranch: "main"})
	if err != nil {
		t.Fatalf("RebaseOntoBase() error = %v", err)
	}
	if got.Status != "merged" || got.Strategy != "rebase" || got.MergeCommit == "" {
		t.Fatalf("RebaseOntoBase() = %#v", got)
	}
	if mainHead := gitOutput(t, repo, "rev-parse", "main"); mainHead != got.MergeCommit {
		t.Fatalf("main head = %q, want %q", mainHead, got.MergeCommit)
	}
}

func TestRangeDiffUsesCommitRefs(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	writeFile(t, repo, "main.go", "package main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	baseCommit := gitOutput(t, repo, "rev-parse", "HEAD")
	writeFile(t, repo, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, repo, "feature.go", "package main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "feature")
	headCommit := gitOutput(t, repo, "rev-parse", "HEAD")
	writeFile(t, repo, "scratch.txt", "uncommitted\n")

	got, err := New("").RangeDiff(ctx, gitdiff.RangeDiffInput{
		RepoPath: repo,
		BaseRef:  baseCommit,
		HeadRef:  headCommit,
	})
	if err != nil {
		t.Fatalf("RangeDiff() error = %v", err)
	}
	if len(got.Files) != 2 {
		t.Fatalf("RangeDiff() files = %#v", got.Files)
	}
	if findFile(got.Files, "scratch.txt").Path != "" {
		t.Fatalf("RangeDiff() included uncommitted file: %#v", got.Files)
	}
	if feature := findFile(got.Files, "feature.go"); feature.Status != "added" {
		t.Fatalf("feature.go = %#v", feature)
	}
	if len(got.Hunks) != 2 {
		t.Fatalf("RangeDiff() hunks = %#v", got.Hunks)
	}
}

func TestCommitHistoryUsesBaseRangeNewestFirst(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	writeFile(t, repo, "main.go", "package main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	baseCommit := gitOutput(t, repo, "rev-parse", "HEAD")
	writeFile(t, repo, "feature.go", "package main\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "feature one")
	firstCommit := gitOutput(t, repo, "rev-parse", "HEAD")
	writeFile(t, repo, "feature.go", "package main\n\nfunc feature() {}\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "feature two")

	got, err := New("").CommitHistory(ctx, gitdiff.CommitHistoryInput{
		WorktreePath: repo,
		BaseRef:      baseCommit,
		HeadRef:      "HEAD",
	})
	if err != nil {
		t.Fatalf("CommitHistory() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("CommitHistory() len = %d, commits = %#v", len(got), got)
	}
	if got[0].Subject != "feature two" || got[1].Subject != "feature one" {
		t.Fatalf("CommitHistory() order = %#v", got)
	}
	if got[1].Hash != firstCommit || got[1].ShortHash == "" || got[1].AuthorName == "" || got[1].CreatedAt == "" {
		t.Fatalf("CommitHistory() fields = %#v", got[1])
	}
}

func TestParseUnifiedDiff(t *testing.T) {
	hunks := parseUnifiedDiff(`diff --git a/a.go b/a.go
@@ -1,2 +1,3 @@
 package a
-var Name = "old"
+var Name = "new"
+var Added = true
`)
	if len(hunks) != 1 {
		t.Fatalf("parseUnifiedDiff() len = %d", len(hunks))
	}
	if hunks[0].OldStart != 1 || hunks[0].NewStart != 1 {
		t.Fatalf("parseUnifiedDiff() starts = %#v", hunks[0])
	}
	if got := []string{hunks[0].Lines[0].Kind, hunks[0].Lines[1].Kind, hunks[0].Lines[2].Kind}; !slices.Equal(got, []string{"context", "delete", "add"}) {
		t.Fatalf("parseUnifiedDiff() kinds = %#v", got)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "AnyCode Test")
	runGit(t, repo, "config", "user.email", "anycode@example.invalid")
	return repo
}

func writeFile(t *testing.T, repo string, name string, body string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}

func gitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func findFile(files []gitdiff.DiffFile, path string) gitdiff.DiffFile {
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	return gitdiff.DiffFile{}
}

func hasKind(lines []gitdiff.DiffLine, kind string) bool {
	for _, line := range lines {
		if line.Kind == kind {
			return true
		}
	}
	return false
}
