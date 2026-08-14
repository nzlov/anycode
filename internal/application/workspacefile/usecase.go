package workspacefile

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	OpenSessionWorkspaceFile(ctx context.Context, input OpenInput) (sessiondomain.WorkspaceFileStream, error)
}

type OpenInput struct {
	SessionID sessiondomain.ID
	Path      string
}

type Service struct {
	sessions sessiondomain.Repository
	files    sessiondomain.WorkspaceFileReader
}

func New(sessions sessiondomain.Repository, files sessiondomain.WorkspaceFileReader) *Service {
	return &Service{sessions: sessions, files: files}
}

func (s *Service) OpenSessionWorkspaceFile(ctx context.Context, input OpenInput) (sessiondomain.WorkspaceFileStream, error) {
	if s == nil || s.sessions == nil || s.files == nil || input.SessionID == "" {
		return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
	}
	current, err := s.sessions.Find(ctx, input.SessionID)
	if err != nil {
		return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
	}
	root := filepath.Clean(strings.TrimSpace(current.WorktreePath))
	path := strings.TrimSpace(input.Path)
	if root == "." || !filepath.IsAbs(root) || path == "" {
		return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
	}
	relativePath := filepath.Clean(path)
	if filepath.IsAbs(relativePath) {
		relativePath, err = filepath.Rel(root, relativePath)
		if err != nil {
			return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
		}
	}
	if relativePath == "." || filepath.IsAbs(relativePath) || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
	}
	stream, err := s.files.OpenWorkspaceFile(ctx, root, relativePath)
	if err != nil {
		if errors.Is(err, sessiondomain.ErrWorkspaceFileNotFound) {
			return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
		}
		return sessiondomain.WorkspaceFileStream{}, err
	}
	return stream, nil
}
