package fsbrowser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestListDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := New().List(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != root {
		t.Fatalf("Path = %q", got.Path)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("Entries = %+v", got.Entries)
	}
	if got.Entries[0].Name != "repo" || !got.Entries[0].IsDir || !got.Entries[0].IsGit {
		t.Fatalf("first entry = %+v", got.Entries[0])
	}
	if got.Entries[1].Name != "file.txt" || got.Entries[1].IsDir || !got.Entries[1].CanRead {
		t.Fatalf("second entry = %+v", got.Entries[1])
	}
}

func TestListNotDirectoryReturnsStructuredError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := New().List(context.Background(), path)
	if err == nil {
		t.Fatal("expected error")
	}
	var browseErr *Error
	if !errors.As(err, &browseErr) {
		t.Fatalf("expected Error, got %T", err)
	}
	if browseErr.Code != "not_directory" {
		t.Fatalf("Code = %q", browseErr.Code)
	}
}
