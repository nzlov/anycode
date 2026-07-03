package diff

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/gitdiff"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	GetSessionDiff(ctx context.Context, input SessionDiffInput) (SessionDiffDTO, error)
}

type SessionDiffInput struct {
	SessionID session.ID
	Mode      string
	FilePath  string
	Page      int
	PageSize  int
}

type SessionDiffDTO struct {
	Mode      string
	FilePath  string
	Files     port.Page[gitdiff.DiffFile]
	FileDiff  *gitdiff.FileDiff
	AllDiff   []gitdiff.FileDiff
	Available bool
}

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
	modeSingle      = "single"
	modeAll         = "all"
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
	page, pageSize := normalizePage(input.Page, input.PageSize)
	dto := SessionDiffDTO{
		Mode:      mode,
		FilePath:  strings.TrimSpace(input.FilePath),
		Files:     emptyPage(page, pageSize),
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

	mergeRecord, hasMergeRecord, err := s.sessions.LatestSuccessfulMergeRecord(ctx, input.SessionID)
	if err != nil {
		return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "latest merge record unavailable").WithRetryable(true)
	}
	if hasMergeRecord && strings.TrimSpace(mergeRecord.BaseCommit) != "" && strings.TrimSpace(mergeHeadRef(mergeRecord)) != "" {
		rangeDiff, err := s.diff.RangeDiff(ctx, gitdiff.RangeDiffInput{
			RepoPath: strings.TrimSpace(project.Path.Value),
			BaseRef:  strings.TrimSpace(mergeRecord.BaseCommit),
			HeadRef:  strings.TrimSpace(mergeHeadRef(mergeRecord)),
		})
		if err != nil {
			return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "read merge range diff failed").WithRetryable(true)
		}
		return applyDiffResult(dto, rangeDiff.Files, rangeDiff.Hunks), nil
	}

	if strings.TrimSpace(sess.WorktreePath) == "" {
		return dto, nil
	}

	baseRef := strings.TrimSpace(sess.BaseBranch)
	if baseRef == "" {
		baseRef = "HEAD"
	}
	diffInput := gitdiff.DiffInput{
		WorktreePath: strings.TrimSpace(sess.WorktreePath),
		BaseRef:      baseRef,
	}
	files, err := s.diff.ChangedFiles(ctx, diffInput)
	if err != nil {
		return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "list changed files failed").WithRetryable(true)
	}
	dto = applyDiffFiles(dto, files)
	if len(files) == 0 {
		return dto, nil
	}
	switch mode {
	case modeAll:
		dto.AllDiff = make([]gitdiff.FileDiff, 0, len(dto.Files.Items))
		for _, file := range dto.Files.Items {
			fileDiff, err := s.diff.FileDiff(ctx, gitdiff.FileDiffInput{
				DiffInput: diffInput,
				FilePath:  file.Path,
			})
			if err != nil {
				return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "read file diff failed").WithDetails(map[string]any{"filePath": file.Path}).WithRetryable(true)
			}
			dto.AllDiff = append(dto.AllDiff, fileDiff)
		}
	default:
		filePath := dto.FilePath
		if filePath == "" || !hasFile(files, filePath) {
			filePath = files[0].Path
		}
		fileDiff, err := s.diff.FileDiff(ctx, gitdiff.FileDiffInput{
			DiffInput: diffInput,
			FilePath:  filePath,
		})
		if err != nil {
			return SessionDiffDTO{}, apperror.Wrap(err, apperror.CodeDiffUnavailable, apperror.CategoryInfraError, "read file diff failed").WithDetails(map[string]any{"filePath": filePath}).WithRetryable(true)
		}
		dto.FilePath = filePath
		dto.FileDiff = &fileDiff
	}
	return dto, nil
}

func applyDiffResult(dto SessionDiffDTO, files []gitdiff.DiffFile, hunks []gitdiff.FileDiff) SessionDiffDTO {
	dto = applyDiffFiles(dto, files)
	if len(files) == 0 {
		return dto
	}
	hunkByPath := map[string]gitdiff.FileDiff{}
	for _, hunk := range hunks {
		hunkByPath[hunk.File.Path] = hunk
	}
	switch dto.Mode {
	case modeAll:
		dto.AllDiff = make([]gitdiff.FileDiff, 0, len(dto.Files.Items))
		for _, file := range dto.Files.Items {
			if hunk, ok := hunkByPath[file.Path]; ok {
				dto.AllDiff = append(dto.AllDiff, hunk)
			} else {
				dto.AllDiff = append(dto.AllDiff, gitdiff.FileDiff{File: file})
			}
		}
	default:
		filePath := dto.FilePath
		if filePath == "" || !hasFile(files, filePath) {
			filePath = files[0].Path
		}
		fileDiff, ok := hunkByPath[filePath]
		if !ok {
			fileDiff = gitdiff.FileDiff{File: findFile(files, filePath)}
		}
		dto.FilePath = filePath
		dto.FileDiff = &fileDiff
	}
	return dto
}

func applyDiffFiles(dto SessionDiffDTO, files []gitdiff.DiffFile) SessionDiffDTO {
	pageItems := slicePage(files, dto.Files.Page, dto.Files.PageSize)
	dto.Files = port.Page[gitdiff.DiffFile]{
		Items:      pageItems,
		Page:       dto.Files.Page,
		PageSize:   dto.Files.PageSize,
		Total:      len(files),
		NextCursor: nextCursor(dto.Files.Page, dto.Files.PageSize, len(files)),
	}
	dto.Available = true
	return dto
}

func mergeHeadRef(record session.MergeRecord) string {
	if strings.TrimSpace(record.HeadCommit) != "" {
		return record.HeadCommit
	}
	return record.MergeCommit
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

func emptyPage(page int, pageSize int) port.Page[gitdiff.DiffFile] {
	return port.Page[gitdiff.DiffFile]{Items: []gitdiff.DiffFile{}, Page: page, PageSize: pageSize}
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
