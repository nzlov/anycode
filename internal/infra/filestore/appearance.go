package filestore

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var wallpaperIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (s *Store) SaveWallpaper(ctx context.Context, id string, reader io.Reader) error {
	if !wallpaperIDPattern.MatchString(id) || reader == nil {
		return &Error{Code: "invalid_wallpaper"}
	}
	dir := filepath.Join(s.dataDir, "settings", "wallpapers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &Error{Code: classify(err), Path: dir, Err: err}
	}
	path := filepath.Join(dir, id)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return &Error{Code: classify(err), Path: path, Err: err}
	}
	_, copyErr := io.Copy(file, readerWithContext{ctx: ctx, reader: reader})
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return &Error{Code: "write_failed", Path: path, Err: errors.Join(copyErr, closeErr)}
	}
	return nil
}

func (s *Store) OpenWallpaper(ctx context.Context, id string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, &Error{Code: "canceled", Err: err}
	}
	if !wallpaperIDPattern.MatchString(id) {
		return nil, &Error{Code: "invalid_wallpaper"}
	}
	path := filepath.Join(s.dataDir, "settings", "wallpapers", id)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, &Error{Code: classify(err), Path: path, Err: err}
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, &Error{Code: "symlink_rejected", Path: path}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, &Error{Code: classify(err), Path: path, Err: err}
	}
	return file, nil
}

func (s *Store) DeleteWallpaper(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return &Error{Code: "canceled", Err: err}
	}
	if !wallpaperIDPattern.MatchString(id) {
		return &Error{Code: "invalid_wallpaper"}
	}
	path := filepath.Join(s.dataDir, "settings", "wallpapers", id)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &Error{Code: classify(err), Path: path, Err: err}
	}
	return nil
}
