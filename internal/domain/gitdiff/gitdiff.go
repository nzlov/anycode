package gitdiff

import "context"

type SessionID string
type ProjectID string

type Worktree struct {
	SessionID      SessionID
	Path           string
	BaseBranch     string
	WorktreeBranch string
}

type DiffFile struct {
	Path      string
	Status    string
	Additions int
	Deletions int
}

type DiffHunk struct {
	Header   string
	OldStart int
	NewStart int
	Lines    []DiffLine
}

type DiffLine struct {
	Kind    string
	Content string
}

type FileDiff struct {
	File  DiffFile
	Hunks []DiffHunk
}

type SessionDiff struct {
	Files []DiffFile
	Hunks []FileDiff
}

type CommitRecord struct {
	Hash        string
	ShortHash   string
	Subject     string
	AuthorName  string
	AuthorEmail string
	CreatedAt   string
}

type MergeResult struct {
	Strategy       string
	BaseBranch     string
	WorktreeBranch string
	BaseCommit     string
	HeadCommit     string
	MergeCommit    string
	Status         string
	FailureCode    string
	FailureReason  string
}

type CreateWorktreeInput struct {
	ProjectID      ProjectID
	SessionID      SessionID
	ProjectPath    string
	BaseBranch     string
	WorktreePath   string
	WorktreeBranch string
}

type MergeInput struct {
	WorktreePath string
	BaseBranch   string
}

type RebaseInput struct {
	WorktreePath string
	BaseBranch   string
}

type DiffInput struct {
	WorktreePath string
	BaseRef      string
	HeadRef      string
}

type FileDiffInput struct {
	DiffInput
	FilePath string
}

type RangeDiffInput struct {
	RepoPath string
	BaseRef  string
	HeadRef  string
}

type CommitHistoryInput struct {
	WorktreePath string
	BaseRef      string
	HeadRef      string
}

type WorktreePort interface {
	CreateWorktree(ctx context.Context, input CreateWorktreeInput) (Worktree, error)
	RemoveWorktree(ctx context.Context, path string) error
	CurrentHead(ctx context.Context, path string) (string, error)
	BranchName(ctx context.Context, path string) (string, error)
}

type MergePort interface {
	MergeToBase(ctx context.Context, input MergeInput) (MergeResult, error)
	RebaseOntoBase(ctx context.Context, input RebaseInput) (MergeResult, error)
	Abort(ctx context.Context, worktreePath string) error
}

type DiffPort interface {
	CurrentBranch(ctx context.Context, path string) (string, error)
	ChangedFiles(ctx context.Context, input DiffInput) ([]DiffFile, error)
	FileDiff(ctx context.Context, input FileDiffInput) (FileDiff, error)
	RangeDiff(ctx context.Context, input RangeDiffInput) (SessionDiff, error)
	CommitHistory(ctx context.Context, input CommitHistoryInput) ([]CommitRecord, error)
}
