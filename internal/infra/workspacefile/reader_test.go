package workspacefile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestReaderOpensTextFileInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "App.java"), []byte("class App {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stream, err := New().OpenWorkspaceFile(context.Background(), root, filepath.Join("src", "App.java"))
	if err != nil {
		t.Fatalf("OpenWorkspaceFile() error = %v", err)
	}
	defer stream.Reader.Close()
	if stream.Filename != "App.java" || stream.Size == 0 || stream.PreviewKind != sessiondomain.PreviewKindText {
		t.Fatalf("stream = %#v", stream)
	}
}

func TestReaderRejectsSymlinkEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "secret.txt")); err != nil {
		t.Fatal(err)
	}

	_, err := New().OpenWorkspaceFile(context.Background(), root, "secret.txt")
	if !errors.Is(err, sessiondomain.ErrWorkspaceFileNotFound) {
		t.Fatalf("OpenWorkspaceFile() error = %v", err)
	}
}
