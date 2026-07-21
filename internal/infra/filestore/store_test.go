package filestore

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
)

func TestInspectArtifactKeepsOversizedImageDownloadableWithoutPreview(t *testing.T) {
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
	artifact, err := store.InspectArtifact(ctx, session.InspectArtifactInput{
		SessionID: "session-1", SourcePath: source, MaxBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.PreviewKind != session.PreviewKindNone || artifact.Path != source {
		t.Fatalf("oversized artifact = %#v", artifact)
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
	if err := validateImageDimensions(bytes.NewReader(webPVP8XHeader(100, 100)), "image/webp", "image.webp"); err != nil {
		t.Fatalf("validateImageDimensions() small WebP error = %v", err)
	}
	err := validateImageDimensions(bytes.NewReader(webPVP8XHeader(10_000, 10_000)), "image/webp", "image.webp")
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
		{"application/yaml", true},
		{"application/problem+json", true},
		{"image/svg+xml", false},
		{"application/octet-stream", false},
	}
	for _, tc := range cases {
		if got := Previewable(tc.mime); got != tc.want {
			t.Fatalf("Previewable(%q) = %v", tc.mime, got)
		}
	}
}

func TestDetectMimeTypeDoesNotTrustPreviewableExtension(t *testing.T) {
	if got := detectMimeType(strings.NewReader("plain text"), "forged.pdf"); got != "text/plain; charset=utf-8" {
		t.Fatalf("detectMimeType() = %q", got)
	}
	if got := detectMimeType(strings.NewReader("plain text"), "README"); got != "text/plain; charset=utf-8" {
		t.Fatalf("detectMimeType() extensionless text = %q", got)
	}
	if got := detectMimeType(strings.NewReader("\x00\x01\x02"), "forged.yml"); got != "application/octet-stream" {
		t.Fatalf("detectMimeType() binary YAML = %q", got)
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
		{"application/yaml", session.ArtifactKindText, session.PreviewKindText},
		{"application/toml", session.ArtifactKindText, session.PreviewKindText},
		{"application/xml", session.ArtifactKindText, session.PreviewKindText},
		{"application/problem+json", session.ArtifactKindText, session.PreviewKindText},
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

func TestInspectArtifactPreviewsTextFiles(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.yml", "settings.toml", "README"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(outputDir, name)
			if err := os.WriteFile(path, []byte("enabled: true\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			artifact, err := store.InspectArtifact(ctx, session.InspectArtifactInput{
				SessionID:  "session-1",
				SourcePath: path,
			})
			if err != nil {
				t.Fatal(err)
			}
			if artifact.ArtifactKind != session.ArtifactKindText || artifact.PreviewKind != session.PreviewKindText || !artifact.Previewable {
				t.Fatalf("InspectArtifact(%q) = kind %q, preview %q, previewable %v", name, artifact.ArtifactKind, artifact.PreviewKind, artifact.Previewable)
			}
		})
	}
}

func TestInspectArtifactUsesOutputFileWithoutCreatingCopy(t *testing.T) {
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
	artifact, err := store.InspectArtifact(ctx, session.InspectArtifactInput{
		SessionID:  "session-1",
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Role != session.FileRoleArtifact || artifact.LogicalPath != "report.txt" || artifact.ArtifactKind != session.ArtifactKindText || artifact.PreviewKind != session.PreviewKindText {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	if err := os.WriteFile(sourcePath, []byte("second version"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "second version" {
		t.Fatalf("current body = %q", current)
	}
	if artifact.Path != sourcePath {
		t.Fatalf("artifact path = %q, want %q", artifact.Path, sourcePath)
	}
	if _, err := os.Stat(filepath.Join(store.attachmentsRoot(), "sessions", "session-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact copy exists: %v", err)
	}
}

func TestInspectArtifactRejectsSymlinkAndOutsidePath(t *testing.T) {
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
	if _, err := store.InspectArtifact(ctx, session.InspectArtifactInput{SessionID: "session-1", SourcePath: outside}); err == nil {
		t.Fatal("expected outside path rejection")
	}
	link := filepath.Join(outputDir, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InspectArtifact(ctx, session.InspectArtifactInput{SessionID: "session-1", SourcePath: link}); err == nil {
		t.Fatal("expected symlink rejection")
	}
	directoryLink := filepath.Join(outputDir, "linked-directory")
	if err := os.Symlink(filepath.Dir(outside), directoryLink); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InspectArtifact(ctx, session.InspectArtifactInput{
		SessionID: "session-1", SourcePath: filepath.Join(directoryLink, filepath.Base(outside)),
	}); err == nil {
		t.Fatal("expected intermediate directory symlink rejection")
	}
}

func TestEnsureArtifactDirRejectsSymlinkedDirectories(t *testing.T) {
	for _, test := range []struct {
		name        string
		sessionLink bool
	}{
		{name: "outputs"},
		{name: "session", sessionLink: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := New(t.TempDir())
			attachments := store.attachmentsRoot()
			outputs := filepath.Join(attachments, "outputs")
			link := outputs
			if test.sessionLink {
				if err := os.MkdirAll(outputs, 0o755); err != nil {
					t.Fatal(err)
				}
				link = filepath.Join(outputs, "session-1")
			} else if err := os.MkdirAll(attachments, 0o755); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			if _, err := store.EnsureArtifactDir(context.Background(), "session-1"); err == nil {
				t.Fatal("created artifact directory through a symlink")
			} else {
				var storeErr *Error
				if !errors.As(err, &storeErr) || storeErr.Code != "symlink_rejected" {
					t.Fatalf("EnsureArtifactDir() error = %v", err)
				}
			}
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("artifact directory escaped output root: %#v", entries)
			}
		})
	}
}

func TestArtifactRootPathValidationRejectsParentReplacement(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	root, err := store.createArtifactRoot(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	outputs := filepath.Join(store.attachmentsRoot(), "outputs")
	movedOutputs := outputs + "-moved"
	outsideOutputs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outsideOutputs, "session-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outputs, movedOutputs); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideOutputs, outputs); err != nil {
		t.Fatal(err)
	}
	if err := validateOpenedRootPath(root, store.ArtifactDir("session-1")); err == nil {
		t.Fatal("validated an artifact path after its parent was replaced")
	}
}

func TestOpenAndDeleteArtifactUseRootedOutputPaths(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outputDir, "reports", "result.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("result"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact, err := store.InspectArtifact(ctx, session.InspectArtifactInput{SessionID: "session-1", SourcePath: path})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := store.OpenArtifact(ctx, artifact.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(stream.Reader)
	closeErr := stream.Reader.Close()
	if readErr != nil || closeErr != nil || string(body) != "result" {
		t.Fatalf("opened artifact = %q, read error = %v, close error = %v", body, readErr, closeErr)
	}
	if _, err := store.DeleteArtifact(ctx, artifact.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted artifact still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty artifact directory still exists: %v", err)
	}
}

func TestArtifactOutputRootStaysBoundAfterDirectoryReplacement(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	originalPath := filepath.Join(outputDir, "result.txt")
	if err := os.WriteFile(originalPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := store.openArtifactRoot(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	outputs := filepath.Dir(outputDir)
	movedOutputs := outputs + "-moved"
	outsideOutputs := t.TempDir()
	outsideSession := filepath.Join(outsideOutputs, "session-1")
	if err := os.MkdirAll(outsideSession, 0o755); err != nil {
		t.Fatal(err)
	}
	outsidePath := filepath.Join(outsideSession, "result.txt")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outputs, movedOutputs); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideOutputs, outputs); err != nil {
		t.Fatal(err)
	}

	file, err := root.Open("result.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(body) != "original" {
		t.Fatalf("rooted artifact = %q, read error = %v, close error = %v", body, readErr, closeErr)
	}
	if err := root.Remove("result.txt"); err != nil {
		t.Fatal(err)
	}
	outsideBody, err := os.ReadFile(outsidePath)
	if err != nil || string(outsideBody) != "outside" {
		t.Fatalf("outside artifact = %q, %v", outsideBody, err)
	}
	if _, err := store.FindArtifact(ctx, encodeArtifactID("session-1", "result.txt")); err == nil {
		t.Fatal("found artifact through a symlinked output root")
	} else {
		var storeErr *Error
		if !errors.As(err, &storeErr) || storeErr.Code != "symlink_rejected" {
			t.Fatalf("symlinked output root error = %v", err)
		}
	}
}

func TestArtifactMetadataRejectsSymlinkReplacement(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(outputDir, "target.txt")
	if err := os.WriteFile(targetPath, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(outputDir, "result.txt")
	if err := os.Symlink("target.txt", artifactPath); err != nil {
		t.Fatal(err)
	}
	root, err := store.openArtifactRoot(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := store.artifactFromFile(root, "session-1", "result.txt"); err == nil {
		t.Fatal("read artifact metadata through a symlink replacement")
	}
}

func TestWriteInlineArtifactRejectsSymlinkedDirectory(t *testing.T) {
	store := New(t.TempDir())
	ctx := context.Background()
	outputDir, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(outputDir, "inline")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteInlineArtifact(ctx, session.WriteInlineArtifactInput{
		SessionID: "session-1",
		Data:      []byte("image"),
		Filename:  "image.png",
		SourceKey: "event-1:0",
	}); err == nil {
		t.Fatal("expected symlinked inline directory rejection")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("inline artifact escaped output directory: %#v", entries)
	}
}

func TestSessionDirectoriesRejectPathLikeSessionIDs(t *testing.T) {
	store := New(t.TempDir())
	for _, sessionID := range []session.ID{"", "../escape", "nested/session", " padded "} {
		artifactDir := store.ArtifactDir(sessionID)
		outputRoot := filepath.Join(store.attachmentsRoot(), "outputs")
		if !pathWithin(outputRoot, artifactDir) || filepath.Dir(artifactDir) != outputRoot {
			t.Fatalf("ArtifactDir(%q) escaped output root: %q", sessionID, artifactDir)
		}
		inputDir := store.sessionInputDir(sessionID, session.AttachmentSourceRequirement, "source", "file-1")
		inputRoot := filepath.Join(store.attachmentsRoot(), "sessions")
		if !pathWithin(inputRoot, inputDir) {
			t.Fatalf("sessionInputDir(%q) escaped input root: %q", sessionID, inputDir)
		}
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

func TestWatchArtifactDirReportsOnlyCreateAndDelete(t *testing.T) {
	store := New(t.TempDir())
	store.watchInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := store.WatchArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.ArtifactDir("session-1"), "result.txt")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for artifact creation")
	}

	if err := os.WriteFile(path, []byte("second version"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
		t.Fatal("content modification emitted an artifact directory change")
	case <-time.After(75 * time.Millisecond):
	}

	secondPath := filepath.Join(store.ArtifactDir("session-1"), "second.txt")
	if err := os.WriteFile(secondPath, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second artifact creation")
	}
	if err := os.Remove(secondPath); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for artifact deletion")
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

	attachment, err := store.Promote(context.Background(), session.PromoteAttachmentInput{
		Staged: staged, SessionID: "session-1", SourceType: session.AttachmentSourcePromptAppend, SourceID: "append-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if attachment.SessionID != "session-1" {
		t.Fatalf("SessionID = %q", attachment.SessionID)
	}
	if attachment.SourceType != session.AttachmentSourcePromptAppend || attachment.SourceID != "append-1" {
		t.Fatalf("attachment source = %q/%q", attachment.SourceType, attachment.SourceID)
	}
	found, err := store.FindSessionFile(context.Background(), attachment.ID)
	if err != nil || found.Path != attachment.Path {
		t.Fatalf("FindSessionFile() = %#v, %v", found, err)
	}
	if _, err := os.Stat(staged.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged file still exists: %v", err)
	}
	if _, err := os.Stat(attachment.Path); err != nil {
		t.Fatal(err)
	}
	retried, err := store.Promote(context.Background(), session.PromoteAttachmentInput{
		Staged: staged, SessionID: "session-1", SourceType: session.AttachmentSourcePromptAppend, SourceID: "append-1",
	})
	if err != nil || retried.Path != attachment.Path {
		t.Fatalf("idempotent Promote() = %#v, %v", retried, err)
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
