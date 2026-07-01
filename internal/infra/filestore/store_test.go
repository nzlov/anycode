package filestore

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/nzlov/anycode/internal/domain/session"
)

func TestPreviewable(t *testing.T) {
	cases := []struct {
		mime string
		want bool
	}{
		{"image/png", true},
		{"video/mp4", true},
		{"application/pdf", false},
		{"text/plain", false},
	}
	for _, tc := range cases {
		if got := Previewable(tc.mime); got != tc.want {
			t.Fatalf("Previewable(%q) = %v", tc.mime, got)
		}
	}
}

func TestStageOpenPromoteAndDelete(t *testing.T) {
	store := New(t.TempDir())
	staged, err := store.Stage(context.Background(), session.StageAttachmentInput{
		OwnerKeyHash: "owner",
		Filename:     "../image.png",
		MimeType:     "image/png",
		Reader:       strings.NewReader("png-data"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if staged.Filename != "image.png" {
		t.Fatalf("Filename = %q", staged.Filename)
	}
	if !staged.Previewable {
		t.Fatalf("expected previewable attachment")
	}
	if _, err := os.Stat(staged.Path); err != nil {
		t.Fatal(err)
	}

	stream, err := store.Open(context.Background(), staged.Path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(stream.Reader)
	if closeErr := stream.Reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "png-data" {
		t.Fatalf("body = %q", body)
	}

	attachment, err := store.Promote(context.Background(), staged, session.ID("session-1"))
	if err != nil {
		t.Fatal(err)
	}
	if attachment.SessionID != "session-1" {
		t.Fatalf("SessionID = %q", attachment.SessionID)
	}
	if _, err := os.Stat(staged.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file still exists: %v", err)
	}
	if _, err := os.Stat(attachment.Path); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(context.Background(), attachment.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(attachment.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session file still exists: %v", err)
	}
}

func TestDeleteStaged(t *testing.T) {
	store := New(t.TempDir())
	staged, err := store.Stage(context.Background(), session.StageAttachmentInput{
		Filename: "note.txt",
		Reader:   strings.NewReader("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteStaged(context.Background(), staged.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file still exists: %v", err)
	}
}

func TestNewUsesANYCODEDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ANYCODE_DATA_DIR", dataDir)
	store := New("")
	got := store.StagedPath(session.StagedAttachmentID("staged-1"), "note.txt")
	if !strings.HasPrefix(got, dataDir) {
		t.Fatalf("StagedPath = %q, want prefix %q", got, dataDir)
	}
}

func TestOpenRejectsOutsideAttachmentRoot(t *testing.T) {
	path := t.TempDir() + "/file.txt"
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := New(t.TempDir()).Open(context.Background(), path)
	if err == nil {
		t.Fatal("expected error")
	}
	var storeErr *Error
	if !errors.As(err, &storeErr) {
		t.Fatalf("expected Error, got %T", err)
	}
	if storeErr.Code != "outside_attachment_root" {
		t.Fatalf("Code = %q", storeErr.Code)
	}
}
