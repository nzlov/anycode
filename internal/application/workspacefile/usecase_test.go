package workspacefile

import (
	"context"
	"io"
	"strings"
	"testing"

	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestOpenSessionWorkspaceFileResolvesPathsInsideWorktree(t *testing.T) {
	repository := &fakeSessionRepository{current: sessiondomain.Session{ID: "session-1", WorktreePath: "/workspace/session-1"}}
	reader := &fakeWorkspaceFileReader{}
	service := New(repository, reader)

	for _, path := range []string{
		"src/App.java",
		"/workspace/session-1/src/App.java",
	} {
		stream, err := service.OpenSessionWorkspaceFile(context.Background(), OpenInput{SessionID: "session-1", Path: path})
		if err != nil {
			t.Fatalf("OpenSessionWorkspaceFile(%q) error = %v", path, err)
		}
		stream.Reader.Close()
		if reader.root != "/workspace/session-1" || reader.relativePath != "src/App.java" {
			t.Fatalf("reader input = root:%q path:%q", reader.root, reader.relativePath)
		}
	}
}

func TestOpenSessionWorkspaceFileRejectsPathsOutsideWorktree(t *testing.T) {
	repository := &fakeSessionRepository{current: sessiondomain.Session{ID: "session-1", WorktreePath: "/workspace/session-1"}}
	reader := &fakeWorkspaceFileReader{}
	service := New(repository, reader)

	for _, path := range []string{"../secret.txt", "/workspace/secret.txt", "."} {
		_, err := service.OpenSessionWorkspaceFile(context.Background(), OpenInput{SessionID: "session-1", Path: path})
		if err != sessiondomain.ErrWorkspaceFileNotFound {
			t.Fatalf("OpenSessionWorkspaceFile(%q) error = %v", path, err)
		}
	}
	if reader.calls != 0 {
		t.Fatalf("reader calls = %d, want 0", reader.calls)
	}
}

type fakeSessionRepository struct {
	sessiondomain.Repository
	current sessiondomain.Session
}

func (r *fakeSessionRepository) Find(context.Context, sessiondomain.ID) (sessiondomain.Session, error) {
	return r.current, nil
}

type fakeWorkspaceFileReader struct {
	root         string
	relativePath string
	calls        int
}

func (r *fakeWorkspaceFileReader) OpenWorkspaceFile(_ context.Context, root string, relativePath string) (sessiondomain.WorkspaceFileStream, error) {
	r.calls++
	r.root = root
	r.relativePath = relativePath
	return sessiondomain.WorkspaceFileStream{Reader: io.NopCloser(strings.NewReader("content"))}, nil
}
