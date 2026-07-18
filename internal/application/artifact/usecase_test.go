package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/filestore"
)

func TestArtifactDirectoryIsTheSourceOfTruth(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	files := filestore.New(dataDir)
	service := New(files)
	root, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "reports", "result.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := service.Publish(ctx, PublishInput{SessionID: "session-1", Path: "reports/result.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != path || first.LogicalPath != "reports/result.txt" || first.Role != session.FileRoleArtifact {
		t.Fatalf("published artifact = %#v", first)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "attachments", "sessions", "session-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact copy exists: %v", err)
	}

	listed, err := service.List(ctx, session.ArtifactQuery{SessionID: "session-1"})
	if err != nil || len(listed) != 1 || listed[0].ID != first.ID {
		t.Fatalf("listed artifacts = %#v, %v", listed, err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := service.Publish(ctx, PublishInput{SessionID: "session-1", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Size != int64(len("second")) {
		t.Fatalf("path identity/content metadata = first:%#v second:%#v", first, second)
	}

	deleted, err := files.DeleteArtifact(ctx, first.ID)
	if err != nil || deleted.ID != first.ID {
		t.Fatalf("deleted artifact = %#v, %v", deleted, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact still exists: %v", err)
	}
}

func TestResolveArtifactsNormalizesAndBoundsLogicalPaths(t *testing.T) {
	ctx := context.Background()
	files := filestore.New(t.TempDir())
	root, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "reports", "result.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("result"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(files)

	resolved, err := service.Resolve(ctx, "session-1", []string{"reports/result.txt", `reports\result.txt`, "missing.txt"})
	if err != nil || len(resolved) != 1 || resolved[0].LogicalPath != "reports/result.txt" {
		t.Fatalf("resolved artifacts = %#v, %v", resolved, err)
	}
	for _, invalid := range []string{"", "/absolute.txt", "../escape.txt", "reports/../escape.txt"} {
		if _, err := service.Resolve(ctx, "session-1", []string{invalid}); err == nil {
			t.Fatalf("Resolve(%q) expected error", invalid)
		}
	}
	tooMany := make([]string, MaxResolveArtifactPaths+1)
	for index := range tooMany {
		tooMany[index] = "artifact.txt"
	}
	if _, err := service.Resolve(ctx, "session-1", tooMany); err == nil {
		t.Fatal("Resolve() accepted too many paths")
	}
}

func TestPublishRejectsSessionQuotaOverflow(t *testing.T) {
	ctx := context.Background()
	files := filestore.New(t.TempDir())
	root, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(files)
	service.SetLimits(Limits{MaxFileBytes: 10, MaxSessionBytes: 4})
	if _, err := service.Publish(ctx, PublishInput{SessionID: "session-1", Path: "result.txt"}); err == nil || !strings.Contains(err.Error(), "session limit") {
		t.Fatalf("quota error = %v", err)
	}
}

func TestPublishInlineArtifactWritesIntoOutputAndDeduplicatesBySourceKey(t *testing.T) {
	ctx := context.Background()
	files := filestore.New(t.TempDir())
	service := New(files)
	input := session.InlineArtifactRequest{
		SessionID: "session-1",
		Data:      []byte("image bytes"),
		Filename:  "image.bin",
		SourceKey: "inline-1",
	}
	first, err := service.PublishInlineArtifact(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.PublishInlineArtifact(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Path != second.Path || !strings.HasPrefix(first.Path, files.ArtifactDir("session-1")) {
		t.Fatalf("inline artifacts = first:%#v second:%#v", first, second)
	}
	listed, err := service.List(ctx, session.ArtifactQuery{SessionID: "session-1"})
	if err != nil || len(listed) != 1 {
		t.Fatalf("inline artifact list = %#v, %v", listed, err)
	}
}

func TestListIgnoresPartialAndSymlinkFiles(t *testing.T) {
	ctx := context.Background()
	files := filestore.New(t.TempDir())
	root, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.partial"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "kept.txt"), filepath.Join(root, "ignored-link.txt")); err != nil {
		t.Fatal(err)
	}
	service := New(files)
	got, err := service.List(ctx, session.ArtifactQuery{SessionID: "session-1", Sort: "created_at_asc"})
	if err != nil || len(got) != 1 || got[0].Filename != "kept.txt" {
		t.Fatalf("List() = %#v, %v", got, err)
	}
}

func TestListRefreshesCachedCountFromTheDirectory(t *testing.T) {
	ctx := context.Background()
	files := filestore.New(t.TempDir())
	root, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first.txt", "second.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo := &artifactCountRepository{}
	service := New(files, repo)
	listed, err := service.List(ctx, session.ArtifactQuery{SessionID: "session-1", Filter: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 || repo.sessionID != "session-1" || repo.count != 2 {
		t.Fatalf("listed = %#v, cached session/count = %q/%d", listed, repo.sessionID, repo.count)
	}
}

type artifactCountRepository struct {
	session.Repository
	sessionID session.ID
	count     int
}

func (r *artifactCountRepository) UpdateArtifactCount(_ context.Context, sessionID session.ID, count int) error {
	r.sessionID = sessionID
	r.count = count
	return nil
}
