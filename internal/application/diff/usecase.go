package diff

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/gitdiff"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	GetSessionDiff(ctx context.Context, input SessionDiffInput) (SessionDiffDTO, error)
	GetSessionDiffSummaries(ctx context.Context, input SessionDiffSummariesInput) ([]SessionDiffSummaryDTO, error)
	GetBranchDiff(ctx context.Context, input BranchDiffInput) (SessionDiffDTO, error)
	GetCommitHistory(ctx context.Context, input CommitHistoryInput) (CommitHistoryDTO, error)
}

type SessionDiffInput struct {
	SessionID       session.ID
	Mode            string
	FilePath        string
	IncludeFileDiff bool
	IncludeAllDiff  bool
	ContextBefore   int
	ContextAfter    int
}

type BranchDiffInput struct {
	ProjectID       projectdomain.ID
	Branch          string
	Mode            string
	FilePath        string
	IncludeFileDiff bool
	IncludeAllDiff  bool
	ContextBefore   int
	ContextAfter    int
}

type SessionDiffSummariesInput struct {
	SessionIDs []session.ID
}

type SessionDiffSummaryState string

const (
	SessionDiffSummaryChanged     SessionDiffSummaryState = "changed"
	SessionDiffSummaryClean       SessionDiffSummaryState = "clean"
	SessionDiffSummaryUnavailable SessionDiffSummaryState = "unavailable"
	SessionDiffSummaryError       SessionDiffSummaryState = "error"
)

type SessionDiffSummaryDTO struct {
	SessionID    session.ID
	State        SessionDiffSummaryState
	FilesChanged int
}

type SessionDiffDTO struct {
	Mode      string
	FilePath  string
	Files     []gitdiff.DiffFile
	FileDiff  *gitdiff.FileDiff
	AllDiff   []gitdiff.FileDiff
	Available bool
}

type CommitHistoryInput struct {
	SessionID session.ID
	Page      int
	PageSize  int
}

type CommitHistoryDTO struct {
	Commits   port.Page[gitdiff.CommitRecord]
	Available bool
}

const (
	defaultPage        = 1
	defaultPageSize    = 20
	maxPageSize        = 100
	modeSingle         = "single"
	modeAll            = "all"
	summaryConcurrency = 8
)

type Service struct {
	sessions session.Repository
	projects projectdomain.Repository
	diff     gitdiff.DiffPort
}

func New(sessions session.Repository, projects projectdomain.Repository, diff gitdiff.DiffPort) *Service {
	return &Service{sessions: sessions, projects: projects, diff: diff}
}

func (s *Service) GetSessionDiff(ctx context.Context, input SessionDiffInput) (SessionDiffDTO, error) {
	if s == nil {
		return SessionDiffDTO{}, errors.New("diff usecase: nil service")
	}
	if input.SessionID == "" {
		return SessionDiffDTO{}, errors.New("session id is required")
	}
	if s.sessions == nil {
		return SessionDiffDTO{}, errors.New("session repository is required")
	}
	if s.projects == nil {
		return SessionDiffDTO{}, errors.New("project repository is required")
	}
	if s.diff == nil {
		return SessionDiffDTO{}, errors.New("diff port is required")
	}
	mode := normalizeMode(input.Mode)
	dto := SessionDiffDTO{
		Mode:      mode,
		FilePath:  strings.TrimSpace(input.FilePath),
		Files:     []gitdiff.DiffFile{},
		Available: false,
	}

	sess, err := s.sessions.Find(ctx, input.SessionID)
	if err != nil {
		return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "session not found").WithDetails(map[string]any{"sessionId": string(input.SessionID)})
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(sess.ProjectID))
	if err != nil {
		return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "project not found").WithDetails(map[string]any{"projectId": string(sess.ProjectID)})
	}
	if !project.IsGit {
		return dto, nil
	}

	diffInput, ok, err := s.resolveSessionDiffInput(ctx, sess, project)
	if err != nil {
		return SessionDiffDTO{}, err
	}
	if !ok {
		return dto, nil
	}
	files, err := s.diff.ChangedFiles(ctx, diffInput)
	if err != nil {
		return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "list session diff files failed").WithRetryable(true)
	}
	dto.Files = files
	dto.Available = true
	if len(files) == 0 {
		return dto, nil
	}
	if !input.IncludeFileDiff && !input.IncludeAllDiff {
		return dto, nil
	}
	switch mode {
	case modeAll:
		if !input.IncludeAllDiff {
			return dto, nil
		}
		dto.AllDiff = make([]gitdiff.FileDiff, 0, len(dto.Files))
		for _, file := range dto.Files {
			fileDiff, err := s.diff.FileDiff(ctx, gitdiff.FileDiffInput{
				DiffInput:     diffInput,
				FilePath:      file.Path,
				ContextBefore: input.ContextBefore,
				ContextAfter:  input.ContextAfter,
			})
			if err != nil {
				return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "read session file diff failed").WithDetails(map[string]any{"filePath": file.Path}).WithRetryable(true)
			}
			dto.AllDiff = append(dto.AllDiff, fileDiff)
		}
	default:
		if !input.IncludeFileDiff {
			return dto, nil
		}
		filePath := dto.FilePath
		if filePath == "" || !hasFile(files, filePath) {
			filePath = files[0].Path
		}
		fileDiff, err := s.diff.FileDiff(ctx, gitdiff.FileDiffInput{
			DiffInput:     diffInput,
			FilePath:      filePath,
			ContextBefore: input.ContextBefore,
			ContextAfter:  input.ContextAfter,
		})
		if err != nil {
			return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "read session file diff failed").WithDetails(map[string]any{"filePath": filePath}).WithRetryable(true)
		}
		dto.FilePath = filePath
		dto.FileDiff = &fileDiff
	}
	return dto, nil
}

func (s *Service) GetSessionDiffSummaries(ctx context.Context, input SessionDiffSummariesInput) ([]SessionDiffSummaryDTO, error) {
	if s == nil {
		return nil, errors.New("diff usecase: nil service")
	}
	ids := uniqueSessionIDs(input.SessionIDs)
	if len(ids) == 0 {
		return []SessionDiffSummaryDTO{}, nil
	}

	type summaryJob struct {
		index     int
		sessionID session.ID
	}
	jobs := make(chan summaryJob)
	results := make([]SessionDiffSummaryDTO, len(ids))
	workerCount := min(summaryConcurrency, len(ids))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for job := range jobs {
				results[job.index] = s.getSessionDiffSummary(ctx, job.sessionID)
			}
		}()
	}

	interrupted := false
sendJobs:
	for index, sessionID := range ids {
		select {
		case jobs <- summaryJob{index: index, sessionID: sessionID}:
		case <-ctx.Done():
			interrupted = true
			break sendJobs
		}
	}
	close(jobs)
	workers.Wait()
	if interrupted || ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return results, nil
}

func (s *Service) getSessionDiffSummary(ctx context.Context, sessionID session.ID) SessionDiffSummaryDTO {
	summary := SessionDiffSummaryDTO{SessionID: sessionID, State: SessionDiffSummaryError}
	dto, err := s.GetSessionDiff(ctx, SessionDiffInput{SessionID: sessionID})
	if err != nil {
		return summary
	}
	if !dto.Available {
		summary.State = SessionDiffSummaryUnavailable
		return summary
	}
	if len(dto.Files) == 0 {
		summary.State = SessionDiffSummaryClean
		return summary
	}
	summary.State = SessionDiffSummaryChanged
	summary.FilesChanged = len(dto.Files)
	return summary
}

func uniqueSessionIDs(input []session.ID) []session.ID {
	seen := make(map[session.ID]struct{}, len(input))
	result := make([]session.ID, 0, len(input))
	for _, sessionID := range input {
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		result = append(result, sessionID)
	}
	return result
}

func (s *Service) GetBranchDiff(ctx context.Context, input BranchDiffInput) (SessionDiffDTO, error) {
	if s == nil {
		return SessionDiffDTO{}, errors.New("diff usecase: nil service")
	}
	if input.ProjectID == "" {
		return SessionDiffDTO{}, errors.New("project id is required")
	}
	if s.projects == nil {
		return SessionDiffDTO{}, errors.New("project repository is required")
	}
	if s.sessions == nil {
		return SessionDiffDTO{}, errors.New("session repository is required")
	}
	if s.diff == nil {
		return SessionDiffDTO{}, errors.New("diff port is required")
	}
	mode := normalizeMode(input.Mode)
	dto := SessionDiffDTO{
		Mode:      mode,
		FilePath:  strings.TrimSpace(input.FilePath),
		Files:     []gitdiff.DiffFile{},
		Available: false,
	}

	project, err := s.projects.Find(ctx, input.ProjectID)
	if err != nil {
		return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "project not found").WithDetails(map[string]any{"projectId": string(input.ProjectID)})
	}
	if !project.IsGit || project.RemovedAt != nil {
		return dto, nil
	}
	branch := strings.TrimSpace(input.Branch)

	sessions, err := s.listBranchSessions(ctx, input.ProjectID, branch)
	if err != nil {
		return SessionDiffDTO{}, err
	}

	files := []gitdiff.DiffFile{}
	sources := map[string]branchDiffSource{}
	for _, sess := range sessions {
		sessionFiles, sessionSources, err := s.branchSessionDiffSources(ctx, sess, project)
		if err != nil {
			return SessionDiffDTO{}, err
		}
		files = append(files, sessionFiles...)
		for path, source := range sessionSources {
			sources[path] = source
		}
	}

	dto.Files = files
	dto.Available = true
	if len(files) == 0 {
		return dto, nil
	}
	if !input.IncludeFileDiff && !input.IncludeAllDiff {
		return dto, nil
	}
	switch mode {
	case modeAll:
		if !input.IncludeAllDiff {
			return dto, nil
		}
		dto.AllDiff = make([]gitdiff.FileDiff, 0, len(dto.Files))
		for _, file := range dto.Files {
			fileDiff, err := s.branchFileDiff(ctx, sources[file.Path], input.ContextBefore, input.ContextAfter)
			if err != nil {
				return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "read branch file diff failed").WithDetails(map[string]any{"filePath": file.Path}).WithRetryable(true)
			}
			dto.AllDiff = append(dto.AllDiff, fileDiff)
		}
	default:
		if !input.IncludeFileDiff {
			return dto, nil
		}
		filePath := dto.FilePath
		if filePath == "" || !hasFile(files, filePath) {
			filePath = files[0].Path
		}
		fileDiff, err := s.branchFileDiff(ctx, sources[filePath], input.ContextBefore, input.ContextAfter)
		if err != nil {
			return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "read branch file diff failed").WithDetails(map[string]any{"filePath": filePath}).WithRetryable(true)
		}
		dto.FilePath = filePath
		dto.FileDiff = &fileDiff
	}
	return dto, nil
}

type branchDiffSource struct {
	FilePath    string
	DisplayPath string
	Hunk        *gitdiff.FileDiff
	Input       *gitdiff.DiffInput
}

func (s *Service) listBranchSessions(ctx context.Context, projectID projectdomain.ID, branch string) ([]session.Session, error) {
	projectSessionID := session.ProjectID(projectID)
	seen := map[session.ID]bool{}
	result := []session.Session{}
	for page := 1; ; page++ {
		rows, total, err := s.sessions.ListCards(ctx, session.ListQuery{
			ProjectID: &projectSessionID,
			Page:      page,
			PageSize:  maxPageSize,
			Sort:      "updated_at desc",
		})
		if err != nil {
			return nil, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "list branch sessions failed").WithRetryable(true)
		}
		for _, row := range rows {
			if seen[row.ID] {
				continue
			}
			if branch != "" && strings.TrimSpace(row.BaseBranch) != branch {
				continue
			}
			seen[row.ID] = true
			result = append(result, row)
		}
		if page*maxPageSize >= total || len(rows) == 0 {
			break
		}
	}
	return result, nil
}

func (s *Service) branchSessionDiffSources(ctx context.Context, sess session.Session, project projectdomain.Project) ([]gitdiff.DiffFile, map[string]branchDiffSource, error) {
	diffInput, ok, err := s.resolveSessionDiffInput(ctx, sess, project)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, map[string]branchDiffSource{}, nil
	}
	files, err := s.diff.ChangedFiles(ctx, diffInput)
	if err != nil {
		return nil, nil, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "list branch session changed files failed").WithRetryable(true)
	}
	prefixed, sources := prefixBranchDiff(sess.ID, files, nil, &diffInput)
	return prefixed, sources, nil
}

func mergeRecordDiffInput(project projectdomain.Project, mergeRecord session.MergeRecord) gitdiff.DiffInput {
	return gitdiff.DiffInput{
		WorktreePath: strings.TrimSpace(project.Path.Value),
		BaseRef:      strings.TrimSpace(mergeRecord.BaseCommit),
		HeadRef:      strings.TrimSpace(mergeRecord.MergeCommit),
	}
}

func (s *Service) resolveSessionDiffInput(ctx context.Context, sess session.Session, project projectdomain.Project) (gitdiff.DiffInput, bool, error) {
	mergeRecord, hasMergeRecord, err := s.sessions.LatestSuccessfulMergeRecord(ctx, sess.ID)
	if err != nil {
		return gitdiff.DiffInput{}, false, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "latest merge record unavailable").WithRetryable(true)
	}
	if hasMergeRecord && strings.TrimSpace(mergeRecord.BaseCommit) != "" && strings.TrimSpace(mergeRecord.MergeCommit) != "" {
		return mergeRecordDiffInput(project, mergeRecord), true, nil
	}
	diffInput, ok, err := s.diff.ResolveSessionDiffSource(ctx, gitdiff.ResolveSessionDiffInput{
		ProjectPath:        strings.TrimSpace(project.Path.Value),
		WorktreePath:       strings.TrimSpace(sess.WorktreePath),
		BaseBranch:         strings.TrimSpace(sess.BaseBranch),
		WorktreeBranch:     strings.TrimSpace(string(sess.ID)),
		WorktreeBaseCommit: strings.TrimSpace(sess.WorktreeBaseCommit),
	})
	if err == nil {
		return diffInput, ok, nil
	}
	appErr := apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "resolve session diff source failed")
	if errors.Is(err, gitdiff.ErrAmbiguousSessionMerge) {
		return gitdiff.DiffInput{}, false, appErr.WithDetails(map[string]any{
			"sessionId":      string(sess.ID),
			"worktreeBranch": string(sess.ID),
		}).WithUserAction("inspect_git_history")
	}
	if errors.Is(err, gitdiff.ErrSessionDiffInvariant) {
		return gitdiff.DiffInput{}, false, appErr.WithDetails(map[string]any{
			"sessionId": string(sess.ID),
		})
	}
	return gitdiff.DiffInput{}, false, appErr.WithDetails(map[string]any{
		"sessionId":          string(sess.ID),
		"baseBranch":         strings.TrimSpace(sess.BaseBranch),
		"worktreeBaseCommit": strings.TrimSpace(sess.WorktreeBaseCommit),
	}).WithRetryable(true)
}

func prefixBranchDiff(sessionID session.ID, files []gitdiff.DiffFile, hunks []gitdiff.FileDiff, input *gitdiff.DiffInput) ([]gitdiff.DiffFile, map[string]branchDiffSource) {
	prefix := shortSessionID(sessionID) + ": "
	prefixed := make([]gitdiff.DiffFile, 0, len(files))
	sources := make(map[string]branchDiffSource, len(files))
	hunksByPath := map[string]gitdiff.FileDiff{}
	for _, hunk := range hunks {
		hunksByPath[hunk.File.Path] = hunk
	}
	for _, file := range files {
		rawPath := file.Path
		file.Path = prefix + rawPath
		prefixed = append(prefixed, file)
		source := branchDiffSource{FilePath: rawPath, DisplayPath: file.Path, Input: input}
		if hunk, ok := hunksByPath[rawPath]; ok {
			hunk.File.Path = file.Path
			source.Hunk = &hunk
		}
		sources[file.Path] = source
	}
	return prefixed, sources
}

func (s *Service) branchFileDiff(ctx context.Context, source branchDiffSource, contextBefore int, contextAfter int) (gitdiff.FileDiff, error) {
	if source.Hunk != nil {
		return *source.Hunk, nil
	}
	if source.Input == nil {
		return gitdiff.FileDiff{}, nil
	}
	fileDiff, err := s.diff.FileDiff(ctx, gitdiff.FileDiffInput{
		DiffInput:     *source.Input,
		FilePath:      source.FilePath,
		ContextBefore: contextBefore,
		ContextAfter:  contextAfter,
	})
	if err != nil {
		return gitdiff.FileDiff{}, err
	}
	fileDiff.File.Path = source.DisplayPath
	return fileDiff, nil
}

func shortSessionID(id session.ID) string {
	value := string(id)
	if len(value) <= 12 {
		return value
	}
	return value[:8]
}

func (s *Service) GetCommitHistory(ctx context.Context, input CommitHistoryInput) (CommitHistoryDTO, error) {
	if s == nil {
		return CommitHistoryDTO{}, errors.New("diff usecase: nil service")
	}
	if input.SessionID == "" {
		return CommitHistoryDTO{}, errors.New("session id is required")
	}
	if s.sessions == nil {
		return CommitHistoryDTO{}, errors.New("session repository is required")
	}
	if s.projects == nil {
		return CommitHistoryDTO{}, errors.New("project repository is required")
	}
	if s.diff == nil {
		return CommitHistoryDTO{}, errors.New("diff port is required")
	}
	page, pageSize := normalizePage(input.Page, input.PageSize)
	dto := CommitHistoryDTO{Commits: emptyCommitPage(page, pageSize)}

	sess, err := s.sessions.Find(ctx, input.SessionID)
	if err != nil {
		return CommitHistoryDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "session not found").WithDetails(map[string]any{"sessionId": string(input.SessionID)})
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(sess.ProjectID))
	if err != nil {
		return CommitHistoryDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "project not found").WithDetails(map[string]any{"projectId": string(sess.ProjectID)})
	}
	if !project.IsGit {
		return dto, nil
	}

	diffInput, ok, err := s.resolveSessionDiffInput(ctx, sess, project)
	if err != nil {
		return CommitHistoryDTO{}, err
	}
	if !ok {
		return dto, nil
	}
	historyInput := gitdiff.CommitHistoryInput{
		WorktreePath: diffInput.WorktreePath,
		BaseRef:      diffInput.BaseRef,
		HeadRef:      diffInput.HeadRef,
	}

	commits, err := s.diff.CommitHistory(ctx, historyInput)
	if err != nil {
		return CommitHistoryDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "read commit history failed").WithRetryable(true)
	}
	dto.Available = true
	dto.Commits = port.Page[gitdiff.CommitRecord]{
		Items:      slicePage(commits, page, pageSize),
		Page:       page,
		PageSize:   pageSize,
		Total:      len(commits),
		NextCursor: nextCursor(page, pageSize, len(commits)),
	}
	return dto, nil
}

func normalizeMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case modeAll:
		return modeAll
	default:
		return modeSingle
	}
}

func normalizePage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = defaultPage
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}

func emptyCommitPage(page int, pageSize int) port.Page[gitdiff.CommitRecord] {
	return port.Page[gitdiff.CommitRecord]{Items: []gitdiff.CommitRecord{}, Page: page, PageSize: pageSize}
}

func slicePage[T any](items []T, page int, pageSize int) []T {
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []T{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	out := make([]T, end-start)
	copy(out, items[start:end])
	return out
}

func nextCursor(page int, pageSize int, total int) string {
	if page*pageSize >= total {
		return ""
	}
	return fmt.Sprintf("%d", page+1)
}

func hasFile(files []gitdiff.DiffFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func findFile(files []gitdiff.DiffFile, path string) gitdiff.DiffFile {
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	return gitdiff.DiffFile{Path: path}
}
