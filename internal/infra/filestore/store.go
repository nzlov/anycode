package filestore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
)

type Store struct {
	dataDir string
}

type StageInput struct {
	OwnerKeyHash string
	Filename     string
	MimeType     string
	Size         int64
	Reader       io.Reader
}

type Stream struct {
	Filename string
	MimeType string
	Size     int64
	Reader   io.ReadCloser
}

type Error struct {
	Code string
	Path string
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("filestore %s at %s", e.Code, e.Path)
	}
	return fmt.Sprintf("filestore %s at %s: %v", e.Code, e.Path, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(dataDir string) *Store {
	if dataDir == "" {
		dataDir = os.Getenv("ANYCODE_DATA_DIR")
	}
	if dataDir == "" {
		dataDir = "."
	}
	return &Store{dataDir: dataDir}
}

func (s *Store) Stage(ctx context.Context, input StageInput) (session.StagedAttachment, error) {
	if input.Reader == nil {
		return session.StagedAttachment{}, &Error{Code: "missing_reader"}
	}
	id, err := newID()
	if err != nil {
		return session.StagedAttachment{}, &Error{Code: "id_failed", Err: err}
	}
	filename := cleanFilename(input.Filename)
	mimeType := resolveMimeType(filename, input.MimeType)
	dir := s.stagedDir(session.StagedAttachmentID(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return session.StagedAttachment{}, &Error{Code: classify(err), Path: dir, Err: err}
	}
	path := filepath.Join(dir, filename)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return session.StagedAttachment{}, &Error{Code: classify(err), Path: path, Err: err}
	}
	written, copyErr := io.Copy(file, readerWithContext{ctx: ctx, reader: input.Reader})
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return session.StagedAttachment{}, &Error{Code: classify(copyErr), Path: path, Err: copyErr}
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return session.StagedAttachment{}, &Error{Code: classify(closeErr), Path: path, Err: closeErr}
	}
	return session.StagedAttachment{
		ID:           session.StagedAttachmentID(id),
		OwnerKeyHash: input.OwnerKeyHash,
		Filename:     filename,
		Path:         path,
		MimeType:     mimeType,
		Size:         written,
		Previewable:  Previewable(mimeType),
		CreatedAt:    time.Now().UTC(),
	}, nil
}

func (s *Store) Promote(ctx context.Context, staged session.StagedAttachment, sessionID session.ID) (session.SessionAttachment, error) {
	if err := ctx.Err(); err != nil {
		return session.SessionAttachment{}, &Error{Code: "canceled", Path: staged.Path, Err: err}
	}
	if staged.ID == "" {
		return session.SessionAttachment{}, &Error{Code: "missing_staged_id", Path: staged.Path}
	}
	id := session.SessionAttachmentID(staged.ID)
	dir := s.sessionDir(sessionID, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return session.SessionAttachment{}, &Error{Code: classify(err), Path: dir, Err: err}
	}
	filename := cleanFilename(staged.Filename)
	target := filepath.Join(dir, filename)
	if err := os.Rename(staged.Path, target); err != nil {
		return session.SessionAttachment{}, &Error{Code: classify(err), Path: target, Err: err}
	}
	_ = os.Remove(filepath.Dir(staged.Path))
	return session.SessionAttachment{
		ID:          id,
		SessionID:   sessionID,
		Filename:    filename,
		Path:        target,
		MimeType:    staged.MimeType,
		Size:        staged.Size,
		Previewable: staged.Previewable,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (s *Store) DeleteStaged(ctx context.Context, id session.StagedAttachmentID) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "canceled", Err: err}
	}
	return removeDir(s.stagedDir(id))
}

func (s *Store) DeleteSession(ctx context.Context, id session.SessionAttachmentID) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "canceled", Err: err}
	}
	root := filepath.Join(s.attachmentsRoot(), "sessions")
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !entry.IsDir() || entry.Name() != string(id) {
			return nil
		}
		found = path
		return filepath.SkipAll
	})
	if err != nil {
		return &Error{Code: classify(err), Path: root, Err: err}
	}
	if found == "" {
		return &Error{Code: "not_found", Path: root, Err: os.ErrNotExist}
	}
	return removeDir(found)
}

func (s *Store) Open(ctx context.Context, path string) (Stream, error) {
	if err := ctx.Err(); err != nil {
		return Stream{}, &Error{Code: "canceled", Path: path, Err: err}
	}
	if !s.underAttachments(path) {
		return Stream{}, &Error{Code: "outside_attachment_root", Path: path}
	}
	file, err := os.Open(path)
	if err != nil {
		return Stream{}, &Error{Code: classify(err), Path: path, Err: err}
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return Stream{}, &Error{Code: classify(err), Path: path, Err: err}
	}
	return Stream{
		Filename: filepath.Base(path),
		MimeType: resolveMimeType(path, ""),
		Size:     info.Size(),
		Reader:   file,
	}, nil
}

func Previewable(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "video/")
}

func (s *Store) StagedPath(id session.StagedAttachmentID, filename string) string {
	return filepath.Join(s.stagedDir(id), cleanFilename(filename))
}

func (s *Store) SessionPath(sessionID session.ID, id session.SessionAttachmentID, filename string) string {
	return filepath.Join(s.sessionDir(sessionID, id), cleanFilename(filename))
}

func (s *Store) attachmentsRoot() string {
	return filepath.Join(s.dataDir, "attachments")
}

func (s *Store) stagedDir(id session.StagedAttachmentID) string {
	return filepath.Join(s.attachmentsRoot(), "staged", string(id))
}

func (s *Store) sessionDir(sessionID session.ID, id session.SessionAttachmentID) string {
	return filepath.Join(s.attachmentsRoot(), "sessions", string(sessionID), string(id))
}

func (s *Store) underAttachments(path string) bool {
	root, err := filepath.Abs(s.attachmentsRoot())
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, abs)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func removeDir(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return &Error{Code: classify(err), Path: path, Err: err}
	}
	return nil
}

func cleanFilename(filename string) string {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		return "attachment"
	}
	return filename
}

func resolveMimeType(filename string, provided string) string {
	if provided != "" {
		return provided
	}
	if value := mime.TypeByExtension(filepath.Ext(filename)); value != "" {
		return value
	}
	return "application/octet-stream"
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func classify(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	case errors.Is(err, os.ErrExist):
		return "already_exists"
	default:
		return "io_failed"
	}
}

type readerWithContext struct {
	ctx    context.Context
	reader io.Reader
}

func (r readerWithContext) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
