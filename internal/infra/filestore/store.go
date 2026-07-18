package filestore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
)

const DefaultArtifactMaxBytes int64 = 512 << 20
const maxPreviewImagePixels int64 = 40_000_000
const defaultWatchInterval = 500 * time.Millisecond

type Store struct {
	dataDir       string
	watchInterval time.Duration
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
	return &Store{dataDir: dataDir, watchInterval: defaultWatchInterval}
}

func (s *Store) Stage(ctx context.Context, input session.StageAttachmentInput) (session.StagedAttachment, error) {
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

func (s *Store) Promote(ctx context.Context, input session.PromoteAttachmentInput) (session.SessionFile, error) {
	staged := input.Staged
	if err := ctx.Err(); err != nil {
		return session.SessionFile{}, &Error{Code: "canceled", Path: staged.Path, Err: err}
	}
	if staged.ID == "" || input.SessionID == "" || input.SourceType == "" || input.SourceID == "" {
		return session.SessionFile{}, &Error{Code: "missing_staged_id", Path: staged.Path}
	}
	id := session.SessionFileID(staged.ID)
	dir := s.sessionInputDir(input.SessionID, input.SourceType, input.SourceID, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return session.SessionFile{}, &Error{Code: classify(err), Path: dir, Err: err}
	}
	filename := cleanFilename(staged.Filename)
	target := filepath.Join(dir, filename)
	if err := os.Rename(staged.Path, target); err != nil {
		info, targetErr := os.Lstat(target)
		if !errors.Is(err, os.ErrNotExist) || targetErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return session.SessionFile{}, &Error{Code: classify(err), Path: target, Err: err}
		}
	}
	_ = os.Remove(filepath.Dir(staged.Path))
	return session.SessionFile{
		ID:          id,
		SessionID:   input.SessionID,
		Role:        session.FileRoleInput,
		SourceType:  input.SourceType,
		SourceID:    input.SourceID,
		Kind:        "file",
		Filename:    filename,
		Path:        target,
		MimeType:    staged.MimeType,
		Size:        staged.Size,
		Previewable: staged.Previewable,
		PreviewKind: previewKind(staged.MimeType),
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (s *Store) DeleteStaged(ctx context.Context, id session.StagedAttachmentID) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "canceled", Err: err}
	}
	return removeDir(s.stagedDir(id))
}

func (s *Store) DeleteSession(ctx context.Context, id session.SessionFileID) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "canceled", Err: err}
	}
	file, err := s.FindSessionFile(ctx, id)
	if errors.Is(err, session.ErrSessionFileNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if file.Role == session.FileRoleArtifact {
		_, err := s.DeleteArtifact(ctx, id)
		return err
	}
	return removeDir(filepath.Dir(file.Path))
}

func (s *Store) Open(ctx context.Context, path string) (session.AttachmentStream, error) {
	if err := ctx.Err(); err != nil {
		return session.AttachmentStream{}, &Error{Code: "canceled", Path: path, Err: err}
	}
	if !s.underAttachments(path) {
		return session.AttachmentStream{}, &Error{Code: "outside_attachment_root", Path: path}
	}
	file, err := os.Open(path)
	if err != nil {
		return session.AttachmentStream{}, &Error{Code: classify(err), Path: path, Err: err}
	}
	return session.AttachmentStream{
		Filename: filepath.Base(path),
		MimeType: resolveMimeType(path, ""),
		Reader:   file,
		Seeker:   file,
	}, nil
}

func (s *Store) EnsureArtifactDir(ctx context.Context, sessionID session.ID) (string, error) {
	root, err := s.createArtifactRoot(ctx, sessionID)
	if err != nil {
		return "", err
	}
	defer root.Close()
	path := s.ArtifactDir(sessionID)
	if err := validateOpenedRootPath(root, path); err != nil {
		return "", &Error{Code: "symlink_rejected", Path: path, Err: err}
	}
	return path, nil
}

func (s *Store) ArtifactDir(sessionID session.ID) string {
	return filepath.Join(s.attachmentsRoot(), "outputs", safeSessionPathComponent(sessionID))
}

func validateImageDimensions(reader io.Reader, mimeType string, path string) error {
	width, height := 0, 0
	var err error
	if mimeType == "image/webp" {
		width, height, err = decodeWebPDimensions(reader)
	} else {
		var config image.Config
		config, _, err = image.DecodeConfig(reader)
		width, height = config.Width, config.Height
	}
	if err != nil {
		return &Error{Code: "invalid_image", Path: path, Err: err}
	}
	if width <= 0 || height <= 0 || int64(width) > maxPreviewImagePixels/int64(height) {
		return &Error{Code: "image_too_large", Path: path}
	}
	return nil
}

func decodeWebPDimensions(reader io.Reader) (int, int, error) {
	var header [30]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return 0, 0, err
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WEBP" {
		return 0, 0, errors.New("invalid WebP container")
	}
	switch string(header[12:16]) {
	case "VP8X":
		width := 1 + int(header[24]) + int(header[25])<<8 + int(header[26])<<16
		height := 1 + int(header[27]) + int(header[28])<<8 + int(header[29])<<16
		return width, height, nil
	case "VP8L":
		if header[20] != 0x2f {
			return 0, 0, errors.New("invalid VP8L signature")
		}
		bits := binary.LittleEndian.Uint32(header[21:25])
		return 1 + int(bits&0x3fff), 1 + int((bits>>14)&0x3fff), nil
	case "VP8 ":
		if string(header[23:26]) != "\x9d\x01\x2a" {
			return 0, 0, errors.New("invalid VP8 frame header")
		}
		width := int(binary.LittleEndian.Uint16(header[26:28]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(header[28:30]) & 0x3fff)
		return width, height, nil
	default:
		return 0, 0, errors.New("unsupported WebP bitstream")
	}
}

func (s *Store) QuarantineArtifactDir(ctx context.Context, sessionID session.ID, token string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", &Error{Code: "canceled", Err: err}
	}
	token = cleanPathComponent(token)
	if token == "" {
		return "", &Error{Code: "missing_quarantine_token"}
	}
	source := s.ArtifactDir(sessionID)
	target := filepath.Join(s.attachmentsRoot(), "output-trash", safeSessionPathComponent(sessionID), token)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", &Error{Code: classify(err), Path: target, Err: err}
	}
	if err := os.Rename(source, target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", &Error{Code: classify(err), Path: target, Err: err}
	}
	return target, nil
}

func (s *Store) RestoreArtifactDir(ctx context.Context, sessionID session.ID, quarantinePath string) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "canceled", Path: quarantinePath, Err: err}
	}
	if quarantinePath == "" {
		return nil
	}
	trashRoot := filepath.Join(s.attachmentsRoot(), "output-trash")
	if !pathWithin(trashRoot, quarantinePath) {
		return &Error{Code: "outside_quarantine_root", Path: quarantinePath}
	}
	target := s.ArtifactDir(sessionID)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return &Error{Code: classify(err), Path: target, Err: err}
	}
	if entries, readErr := os.ReadDir(target); readErr == nil && len(entries) == 0 {
		if err := os.Remove(target); err != nil {
			return &Error{Code: classify(err), Path: target, Err: err}
		}
	}
	if err := os.Rename(quarantinePath, target); err != nil {
		return &Error{Code: classify(err), Path: target, Err: err}
	}
	return nil
}

func (s *Store) ListArtifactQuarantines(ctx context.Context) ([]session.ArtifactQuarantine, error) {
	root := filepath.Join(s.attachmentsRoot(), "output-trash")
	sessionEntries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []session.ArtifactQuarantine{}, nil
	}
	if err != nil {
		return nil, &Error{Code: classify(err), Path: root, Err: err}
	}
	quarantines := make([]session.ArtifactQuarantine, 0)
	for _, sessionEntry := range sessionEntries {
		if err := ctx.Err(); err != nil {
			return nil, &Error{Code: "canceled", Path: root, Err: err}
		}
		if !sessionEntry.IsDir() || sessionEntry.Type()&os.ModeSymlink != 0 {
			continue
		}
		sessionID := session.ID(sessionEntry.Name())
		sessionDir := filepath.Join(root, sessionEntry.Name())
		tokenEntries, err := os.ReadDir(sessionDir)
		if err != nil {
			return nil, &Error{Code: classify(err), Path: sessionDir, Err: err}
		}
		for _, tokenEntry := range tokenEntries {
			if tokenEntry.IsDir() && tokenEntry.Type()&os.ModeSymlink == 0 {
				info, err := tokenEntry.Info()
				if err != nil {
					return nil, &Error{Code: classify(err), Path: filepath.Join(sessionDir, tokenEntry.Name()), Err: err}
				}
				quarantines = append(quarantines, session.ArtifactQuarantine{
					SessionID:  sessionID,
					Path:       filepath.Join(sessionDir, tokenEntry.Name()),
					ModifiedAt: info.ModTime(),
				})
			}
		}
	}
	return quarantines, nil
}

func (s *Store) ListArtifactOutputDirectories(ctx context.Context) ([]session.ArtifactOutputDirectory, error) {
	root := filepath.Join(s.attachmentsRoot(), "outputs")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []session.ArtifactOutputDirectory{}, nil
	}
	if err != nil {
		return nil, &Error{Code: classify(err), Path: root, Err: err}
	}
	outputs := make([]session.ArtifactOutputDirectory, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, &Error{Code: "canceled", Path: root, Err: err}
		}
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, &Error{Code: classify(err), Path: filepath.Join(root, entry.Name()), Err: err}
		}
		outputs = append(outputs, session.ArtifactOutputDirectory{
			SessionID:  session.ID(entry.Name()),
			ModifiedAt: info.ModTime(),
		})
	}
	return outputs, nil
}

func (s *Store) DeleteArtifactOutputDirectory(ctx context.Context, sessionID session.ID) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "canceled", Path: s.ArtifactDir(sessionID), Err: err}
	}
	path := s.ArtifactDir(sessionID)
	root := filepath.Join(s.attachmentsRoot(), "outputs")
	if filepath.Dir(path) != root || filepath.Base(path) != string(sessionID) {
		return &Error{Code: "outside_output_root", Path: path}
	}
	return removeDir(path)
}

func (s *Store) DeleteQuarantine(ctx context.Context, quarantinePath string) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "canceled", Path: quarantinePath, Err: err}
	}
	trashRoot := filepath.Join(s.attachmentsRoot(), "output-trash")
	if !pathWithin(trashRoot, quarantinePath) {
		return &Error{Code: "outside_quarantine_root", Path: quarantinePath}
	}
	return removeDir(quarantinePath)
}

func Previewable(mimeType string) bool {
	_, preview := classifyArtifact(mimeType)
	return preview != session.PreviewKindNone
}

func (s *Store) StagedPath(id session.StagedAttachmentID, filename string) string {
	return filepath.Join(s.stagedDir(id), cleanFilename(filename))
}

func (s *Store) SessionPath(sessionID session.ID, id session.SessionFileID, filename string) string {
	return filepath.Join(s.sessionInputDir(sessionID, session.AttachmentSourceRequirement, string(sessionID), id), cleanFilename(filename))
}

func (s *Store) attachmentsRoot() string {
	return filepath.Join(s.dataDir, "attachments")
}

func (s *Store) stagedDir(id session.StagedAttachmentID) string {
	return filepath.Join(s.attachmentsRoot(), "staged", string(id))
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

func pathWithin(root string, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func cleanPathComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value != filepath.Base(value) || value == "." || value == ".." {
		return ""
	}
	return value
}

func safeSessionPathComponent(sessionID session.ID) string {
	value := string(sessionID)
	if value != "" && cleanPathComponent(value) == value {
		return value
	}
	hash := sha256.Sum256([]byte(value))
	return ".invalid-" + hex.EncodeToString(hash[:])
}

func detectAttachmentMimeType(path string, filename string) string {
	file, err := os.Open(path)
	if err != nil {
		return detectMimeType(nil, filename)
	}
	defer file.Close()
	return detectMimeType(file, filename)
}

func detectMimeType(reader io.Reader, filename string) string {
	detected := "application/octet-stream"
	if reader != nil {
		buffer := make([]byte, 512)
		read, _ := reader.Read(buffer)
		if read > 0 {
			detected = http.DetectContentType(buffer[:read])
			if detected != "application/octet-stream" && detected != "text/plain; charset=utf-8" {
				return detected
			}
		}
	}
	byExtension := resolveMimeType(filename, "")
	_, previewKind := classifyArtifact(byExtension)
	if previewKind == session.PreviewKindImage || previewKind == session.PreviewKindPDF || previewKind == session.PreviewKindVideo || previewKind == session.PreviewKindAudio {
		return detected
	}
	return byExtension
}

func classifyArtifact(mimeType string) (session.ArtifactKind, session.PreviewKind) {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch {
	case mimeType == "application/pdf":
		return session.ArtifactKindPDF, session.PreviewKindPDF
	case strings.HasPrefix(mimeType, "image/"):
		switch mimeType {
		case "image/png", "image/jpeg", "image/webp", "image/gif":
			return session.ArtifactKindImage, session.PreviewKindImage
		default:
			return session.ArtifactKindImage, session.PreviewKindNone
		}
	case strings.HasPrefix(mimeType, "video/"):
		return session.ArtifactKindVideo, session.PreviewKindVideo
	case strings.HasPrefix(mimeType, "audio/"):
		return session.ArtifactKindAudio, session.PreviewKindAudio
	case strings.HasPrefix(mimeType, "text/") || mimeType == "application/json":
		return session.ArtifactKindText, session.PreviewKindText
	case mimeType == "application/zip" || mimeType == "application/x-tar" || mimeType == "application/gzip" || mimeType == "application/x-7z-compressed" || mimeType == "application/x-rar-compressed":
		return session.ArtifactKindArchive, session.PreviewKindNone
	default:
		return session.ArtifactKindFile, session.PreviewKindNone
	}
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
