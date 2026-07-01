package fsbrowser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/nzlov/anycode/internal/domain/project"
)

type Browser struct{}

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
		return fmt.Sprintf("browse %s at %s", e.Code, e.Path)
	}
	return fmt.Sprintf("browse %s at %s: %v", e.Code, e.Path, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New() *Browser {
	return &Browser{}
}

func (b *Browser) List(ctx context.Context, path string) (project.DirectoryListing, error) {
	if err := ctx.Err(); err != nil {
		return project.DirectoryListing{}, &Error{Code: "canceled", Path: path, Err: err}
	}
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return project.DirectoryListing{}, &Error{Code: "invalid_path", Path: path, Err: err}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return project.DirectoryListing{}, &Error{Code: classify(err), Path: abs, Err: err}
	}
	if !info.IsDir() {
		return project.DirectoryListing{}, &Error{Code: "not_directory", Path: abs, Err: err}
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return project.DirectoryListing{}, &Error{Code: classify(err), Path: abs, Err: err}
	}
	listing := project.DirectoryListing{
		Path:    abs,
		Parent:  filepath.Dir(abs),
		Entries: make([]project.DirectoryEntry, 0, len(entries)),
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return project.DirectoryListing{}, &Error{Code: "canceled", Path: abs, Err: err}
		}
		entryPath := filepath.Join(abs, entry.Name())
		isDir := entry.IsDir()
		canRead := canReadPath(entryPath, isDir)
		item := project.DirectoryEntry{
			Name:    entry.Name(),
			Path:    entryPath,
			IsDir:   isDir,
			IsGit:   isDir && hasDotGit(entryPath),
			CanRead: canRead,
		}
		if !canRead {
			item.ErrorCode = "permission_denied"
		}
		listing.Entries = append(listing.Entries, item)
	}
	sort.Slice(listing.Entries, func(i, j int) bool {
		left := listing.Entries[i]
		right := listing.Entries[j]
		if left.IsDir != right.IsDir {
			return left.IsDir
		}
		return left.Name < right.Name
	})
	return listing, nil
}

func hasDotGit(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func canReadPath(path string, isDir bool) bool {
	if isDir {
		file, err := os.Open(path)
		if err != nil {
			return false
		}
		defer file.Close()
		_, err = file.Readdirnames(1)
		return err == nil || errors.Is(err, io.EOF)
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

func classify(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	case errors.Is(err, os.ErrNotExist):
		return "not_found"
	default:
		return "read_failed"
	}
}
