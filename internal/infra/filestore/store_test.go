package filestore

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nzlov/anycode/internal/domain/session"
)

func TestArchiveArtifactCleansOversizedImage(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(outputDir, "invalid.png")
	if err := os.WriteFile(source, oversizedPNGHeader(10_000, 10_000), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = store.ArchiveArtifact(ctx, session.ArchiveArtifactInput{
		SessionID: "session-1", SourcePath: source, LogicalPath: "invalid.png", MaxBytes: 1024,
	})
	if err == nil {
		t.Fatal("ArchiveArtifact() accepted an oversized image")
	}
	archiveRoot := filepath.Join(store.attachmentsRoot(), "sessions", "session-1")
	entries, readErr := os.ReadDir(archiveRoot)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized image left archive directories: %#v", entries)
	}
}

func oversizedPNGHeader(width, height uint32) []byte {
	header := make([]byte, 33)
	copy(header[0:8], "\x89PNG\r\n\x1a\n")
	binary.BigEndian.PutUint32(header[8:12], 13)
	copy(header[12:16], "IHDR")
	binary.BigEndian.PutUint32(header[16:20], width)
	binary.BigEndian.PutUint32(header[20:24], height)
	header[24], header[25] = 8, 2
	binary.BigEndian.PutUint32(header[29:33], crc32.ChecksumIEEE(header[12:29]))
	return header
}

func TestValidateWebPDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.webp")
	if err := os.WriteFile(path, webPVP8XHeader(100, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateImageDimensions(path, "image/webp"); err != nil {
		t.Fatalf("validateImageDimensions() small WebP error = %v", err)
	}
	if err := os.WriteFile(path, webPVP8XHeader(10_000, 10_000), 0o644); err != nil {
		t.Fatal(err)
	}
	err := validateImageDimensions(path, "image/webp")
	var storeErr *Error
	if !errors.As(err, &storeErr) || storeErr.Code != "image_too_large" {
		t.Fatalf("validateImageDimensions() large WebP error = %v", err)
	}
}

func webPVP8XHeader(width, height int) []byte {
	header := make([]byte, 30)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(header)-8))
	copy(header[8:12], "WEBP")
	copy(header[12:16], "VP8X")
	binary.LittleEndian.PutUint32(header[16:20], 10)
	width--
	height--
	header[24], header[25], header[26] = byte(width), byte(width>>8), byte(width>>16)
	header[27], header[28], header[29] = byte(height), byte(height>>8), byte(height>>16)
	return header
}

func TestPreviewable(t *testing.T) {
	cases := []struct {
		mime string
		want bool
	}{
		{"image/png", true},
		{"video/mp4", true},
		{"application/pdf", true},
		{"audio/mpeg", true},
		{"text/plain", true},
		{"image/svg+xml", false},
	}
	for _, tc := range cases {
		if got := Previewable(tc.mime); got != tc.want {
			t.Fatalf("Previewable(%q) = %v", tc.mime, got)
		}
	}
}

func TestDetectMimeTypeDoesNotTrustPreviewableExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "forged.pdf")
	if err := os.WriteFile(path, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectMimeType(path, filepath.Base(path)); got != "text/plain; charset=utf-8" {
		t.Fatalf("detectMimeType() = %q", got)
	}
}

func TestClassifyArtifactCoversSupportedKinds(t *testing.T) {
	tests := []struct {
		mime    string
		kind    session.ArtifactKind
		preview session.PreviewKind
	}{
		{"image/png", session.ArtifactKindImage, session.PreviewKindImage},
		{"application/pdf", session.ArtifactKindPDF, session.PreviewKindPDF},
		{"video/mp4", session.ArtifactKindVideo, session.PreviewKindVideo},
		{"audio/mpeg", session.ArtifactKindAudio, session.PreviewKindAudio},
		{"application/zip", session.ArtifactKindArchive, session.PreviewKindNone},
		{"application/json", session.ArtifactKindText, session.PreviewKindText},
		{"application/octet-stream", session.ArtifactKindFile, session.PreviewKindNone},
		{"image/svg+xml", session.ArtifactKindImage, session.PreviewKindNone},
		{"image/bmp", session.ArtifactKindImage, session.PreviewKindNone},
	}
	for _, test := range tests {
		kind, preview := classifyArtifact(test.mime)
		if kind != test.kind || preview != test.preview {
			t.Fatalf("classifyArtifact(%q) = %q/%q, want %q/%q", test.mime, kind, preview, test.kind, test.preview)
		}
	}
}

func TestArchiveArtifactKeepsOutputAndCreatesImmutableCopy(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := outputDir + "/report.txt"
	if err := os.WriteFile(sourcePath, []byte("first version"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact, err := store.ArchiveArtifact(ctx, session.ArchiveArtifactInput{
		SessionID:  "session-1",
		SourcePath: sourcePath,
		SourceType: session.AttachmentSourceCodex,
		SourceKey:  "report.txt:13",
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Role != session.FileRoleArtifact || artifact.LogicalPath != "report.txt" || artifact.ArtifactKind != session.ArtifactKindText || artifact.PreviewKind != session.PreviewKindText {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	if artifact.SHA256 != "80d8f975e768eecac59d22a788bf8e811e51ca85e309ee47f1e821e3e58280f2" {
		t.Fatalf("SHA256 = %q", artifact.SHA256)
	}
	if err := os.WriteFile(sourcePath, []byte("second version"), 0o644); err != nil {
		t.Fatal(err)
	}
	archived, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(archived) != "first version" {
		t.Fatalf("archived body = %q", archived)
	}
	if matches, _ := filepath.Glob(artifact.Path + ".partial"); len(matches) != 0 {
		t.Fatalf("partial files = %#v", matches)
	}
}

func TestArchiveArtifactRejectsSymlinkAndOutsidePath(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveArtifact(ctx, session.ArchiveArtifactInput{SessionID: "session-1", SourcePath: outside}); err == nil {
		t.Fatal("expected outside path rejection")
	}
	link := filepath.Join(outputDir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveArtifact(ctx, session.ArchiveArtifactInput{SessionID: "session-1", SourcePath: link}); err == nil {
		t.Fatal("expected symlink rejection")
	}
	directoryLink := filepath.Join(outputDir, "linked-directory")
	if err := os.Symlink(filepath.Dir(outside), directoryLink); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ArchiveArtifact(ctx, session.ArchiveArtifactInput{
		SessionID: "session-1", SourcePath: filepath.Join(directoryLink, filepath.Base(outside)),
	}); err == nil {
		t.Fatal("expected intermediate directory symlink rejection")
	}
}

func TestQuarantineRestoreAndDeleteArtifactDir(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "result.bin"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	quarantine, err := store.QuarantineArtifactDir(ctx, "session-1", "close-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outputDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output directory still exists: %v", err)
	}
	if err := store.RestoreArtifactDir(ctx, "session-1", quarantine); err != nil {
		t.Fatal(err)
	}
	quarantine, err = store.QuarantineArtifactDir(ctx, "session-1", "close-token-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteQuarantine(ctx, quarantine); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(quarantine); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quarantine still exists: %v", err)
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
