package workspacefile

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/filetype"
)

type Reader struct{}

func New() *Reader {
	return &Reader{}
}

func (r *Reader) OpenWorkspaceFile(ctx context.Context, rootPath string, relativePath string) (sessiondomain.WorkspaceFileStream, error) {
	if err := ctx.Err(); err != nil {
		return sessiondomain.WorkspaceFileStream{}, err
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
	}
	defer root.Close()
	file, err := root.Open(relativePath)
	if err != nil {
		return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return sessiondomain.WorkspaceFileStream{}, sessiondomain.ErrWorkspaceFileNotFound
	}
	mimeType, err := detectMIMEType(file, info.Name())
	if err != nil {
		file.Close()
		return sessiondomain.WorkspaceFileStream{}, err
	}
	return sessiondomain.WorkspaceFileStream{
		Filename: info.Name(), MimeType: mimeType, Size: info.Size(), ModifiedAt: info.ModTime(),
		PreviewKind: previewKind(mimeType), Reader: file, Seeker: file,
	}, nil
}

func detectMIMEType(file *os.File, filename string) (string, error) {
	sample := make([]byte, 32<<10)
	read, err := file.Read(sample)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	sample = sample[:read]
	detected := http.DetectContentType(sample)
	detected = filetype.RefineBrowserMediaMIMEType(filename, detected, sample)
	if detected != "application/octet-stream" {
		return detected, nil
	}
	if value := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); value != "" {
		return value, nil
	}
	return detected, nil
}

func previewKind(mimeType string) sessiondomain.PreviewKind {
	if kind := sessiondomain.BrowserPreviewKind(mimeType); kind != sessiondomain.PreviewKindNone {
		return kind
	}
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if strings.HasPrefix(normalized, "text/") || normalized == "application/json" || normalized == "application/xml" || normalized == "application/yaml" || normalized == "application/toml" || strings.HasSuffix(normalized, "+json") || strings.HasSuffix(normalized, "+xml") {
		return sessiondomain.PreviewKindText
	}
	return sessiondomain.PreviewKindNone
}
