package artifact_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
)

type failingArtifactPublisher struct{}

func (failingArtifactPublisher) PublishAfterCommit(context.Context, eventdomain.DomainEvent) error {
	return errors.New("subscriber unavailable")
}

type blockingArtifactRepository struct {
	session.ArtifactRepository
	mu               sync.Mutex
	sumCalls         int
	firstSumEntered  chan struct{}
	secondSumEntered chan struct{}
	releaseFirstSum  chan struct{}
}

func TestResolveArtifactsNormalizesAndBoundsLogicalPaths(t *testing.T) {
	ctx := context.Background()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := database.Attachments()
	now := time.Now().UTC()
	for _, artifact := range []session.SessionFile{
		{ID: "first", SessionID: "session-1", Role: session.FileRoleArtifact, SourceKey: "first", LogicalPath: "reports/result.txt", Filename: "result.txt", CreatedAt: now},
		{ID: "second", SessionID: "session-1", Role: session.FileRoleArtifact, SourceKey: "second", LogicalPath: "image.png", Filename: "image.png", CreatedAt: now.Add(time.Second)},
	} {
		if err := repo.SaveSessionAttachment(ctx, artifact); err != nil {
			t.Fatal(err)
		}
	}
	service := artifactapp.New(repo, nil, nil)

	got, err := service.Resolve(ctx, "session-1", []string{" image.png ", `reports\result.txt`, "image.png", "missing.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "second" || got[1].ID != "first" {
		t.Fatalf("Resolve() = %#v", got)
	}

	for _, invalid := range [][]string{{""}, {"/absolute.txt"}, {"../escape.txt"}, {"a/../escape.txt"}} {
		if _, err := service.Resolve(ctx, "session-1", invalid); err == nil {
			t.Fatalf("Resolve(%q) accepted invalid path", invalid)
		}
	}
	tooMany := make([]string, artifactapp.MaxResolveArtifactPaths+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("artifact-%d.txt", index)
	}
	if _, err := service.Resolve(ctx, "session-1", tooMany); err == nil {
		t.Fatal("Resolve() accepted too many paths")
	}
}

func (r *blockingArtifactRepository) SumSessionArtifactSize(ctx context.Context, sessionID session.ID) (int64, error) {
	r.mu.Lock()
	r.sumCalls++
	call := r.sumCalls
	r.mu.Unlock()
	switch call {
	case 1:
		close(r.firstSumEntered)
		<-r.releaseFirstSum
	case 2:
		close(r.secondSumEntered)
	}
	return r.ArtifactRepository.SumSessionArtifactSize(ctx, sessionID)
}

func TestPublishSerializesSessionQuotaChecks(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := &blockingArtifactRepository{
		ArtifactRepository: database.Attachments(),
		firstSumEntered:    make(chan struct{}), secondSumEntered: make(chan struct{}), releaseFirstSum: make(chan struct{}),
	}
	files := filestore.New(dataDir)
	service := artifactapp.New(repo, files, files)
	service.SetLimits(artifactapp.Limits{MaxFileBytes: 8, MaxSessionBytes: 10})

	results := make(chan error, 2)
	publish := func(key string) {
		_, err := service.PublishInlineArtifact(ctx, session.InlineArtifactRequest{
			SessionID: "session-1", Data: []byte("123456"), Filename: key + ".txt", MimeType: "text/plain", SourceKey: key,
		})
		results <- err
	}
	go publish("first")
	<-repo.firstSumEntered
	go publish("second")
	select {
	case <-repo.secondSumEntered:
		t.Fatal("second quota check entered before the first publish completed")
	case <-time.After(100 * time.Millisecond):
	}
	close(repo.releaseFirstSum)

	var succeeded, limited int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case strings.Contains(err.Error(), "session limit"):
			limited++
		default:
			t.Fatalf("unexpected publish error: %v", err)
		}
	}
	if succeeded != 1 || limited != 1 {
		t.Fatalf("publish results succeeded=%d limited=%d", succeeded, limited)
	}
}

func TestCommittedArtifactIgnoresLivePublisherFailure(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dataDir)
	service := artifactapp.New(database.Attachments(), files, files)
	service.SetEvents(database.Events(), failingArtifactPublisher{})
	artifact, err := service.PublishInlineArtifact(ctx, session.InlineArtifactRequest{
		SessionID: "session-1", Data: []byte("result"), Filename: "result.txt", MimeType: "text/plain", SourceKey: "result-1",
	})
	if err != nil {
		t.Fatalf("PublishInlineArtifact() error = %v", err)
	}
	if _, err := service.Delete(ctx, artifact.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	sessionID := eventdomain.SessionID("session-1")
	events, err := database.Events().List(ctx, eventdomain.Scope{SessionID: &sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != "artifact.published" || events[1].Type != "artifact.deleted" {
		t.Fatalf("persisted events = %#v", events)
	}
}

func TestClosingSessionOnlyAcceptsFinalScan(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	reason := session.CloseReasonUserClosed
	card := session.Session{
		ID: "session-1", ProjectID: "project-1", Requirement: "closing", Mode: session.ModeChat,
		Status: session.StatusStopping, Priority: session.PriorityMedium, CloseReason: &reason, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Sessions().Create(ctx, card); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dataDir)
	service := artifactapp.New(database.Attachments(), files, files, database.Sessions())
	outputDir, err := files.EnsureArtifactDir(ctx, card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "final.txt"), []byte("final"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishInlineArtifact(ctx, session.InlineArtifactRequest{
		SessionID: card.ID, Data: []byte("late"), Filename: "late.txt", MimeType: "text/plain", SourceKey: "late",
	}); err == nil {
		t.Fatal("closing session accepted an inline artifact")
	}
	artifacts, err := service.Scan(ctx, artifactapp.ScanInput{SessionID: card.ID, SourceID: "session_close"})
	if err != nil || len(artifacts) != 1 || artifacts[0].LogicalPath != "final.txt" {
		t.Fatalf("final Scan() artifacts=%#v error=%v", artifacts, err)
	}
	card.Status = session.StatusClosed
	card.ClosedAt = &now
	if err := database.Sessions().Save(ctx, card); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Scan(ctx, artifactapp.ScanInput{SessionID: card.ID}); err == nil {
		t.Fatal("closed session accepted an artifact scan")
	}
}

func TestPublishScanVersionAndDelete(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dataDir)
	service := artifactapp.New(database.Attachments(), files, files)
	outputDir, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(outputDir, "reports", "home.txt")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	firstTime := time.Unix(100, 0).UTC()
	if err := os.WriteFile(source, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, firstTime, firstTime); err != nil {
		t.Fatal(err)
	}
	first, err := service.Publish(ctx, artifactapp.PublishInput{SessionID: "session-1", Path: source, SourceType: session.AttachmentSourceMCP})
	if err != nil {
		t.Fatal(err)
	}
	again, err := service.Publish(ctx, artifactapp.PublishInput{SessionID: "session-1", Path: source, SourceType: session.AttachmentSourceMCP})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("duplicate publish IDs = %q, %q", first.ID, again.ID)
	}
	secondTime := firstTime.Add(time.Second)
	if err := os.WriteFile(source, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, secondTime, secondTime); err != nil {
		t.Fatal(err)
	}
	published, err := service.Scan(ctx, artifactapp.ScanInput{SessionID: "session-1", SourceType: session.AttachmentSourceReconciled})
	if err != nil {
		t.Fatal(err)
	}
	if len(published) != 1 || published[0].ID == first.ID {
		t.Fatalf("scan published = %#v", published)
	}
	artifacts, total, err := service.List(ctx, session.ArtifactQuery{SessionID: "session-1", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(artifacts) != 2 {
		t.Fatalf("artifact listing total=%d rows=%#v", total, artifacts)
	}
	input, err := service.UseAsInput(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if input.ID == first.ID || input.Path == first.Path || input.Role != session.FileRoleInput || input.SourceType != session.AttachmentSourceArtifactCopy {
		t.Fatalf("copied input = %#v", input)
	}
	deleted, err := service.Delete(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.DeletedAt == nil {
		t.Fatal("deleted artifact is missing tombstone")
	}
	if _, err := os.Stat(first.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archived file still exists: %v", err)
	}
	if body, err := os.ReadFile(input.Path); err != nil || string(body) != "first" {
		t.Fatalf("copied input changed after artifact delete: body=%q err=%v", body, err)
	}
	_, total, err = service.List(ctx, session.ArtifactQuery{SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("artifact total after delete = %d", total)
	}
	if _, err := service.Scan(ctx, artifactapp.ScanInput{SessionID: "session-1"}); err != nil {
		t.Fatalf("scan resurrected deleted artifact: %v", err)
	}
}

func TestScanIgnoresPartialAndSymlink(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dataDir)
	service := artifactapp.New(database.Attachments(), files, files)
	outputDir, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "unfinished.partial"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(outputDir, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "browser", "process-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "browser", "process-1", "page.txt"), []byte("page"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifacts, err := service.Scan(ctx, artifactapp.ScanInput{SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].SourceType != session.AttachmentSourcePlaywright {
		t.Fatalf("scan artifacts = %#v", artifacts)
	}
}

func TestScanContinuesAfterIndividualPublishFailure(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dataDir)
	service := artifactapp.New(database.Attachments(), files, files)
	service.SetLimits(artifactapp.Limits{MaxFileBytes: 4, MaxSessionBytes: 100})
	outputDir, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "a-too-large.txt"), []byte("large"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "z-valid.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	published, err := service.Scan(ctx, artifactapp.ScanInput{SessionID: "session-1"})
	if err == nil || !strings.Contains(err.Error(), "a-too-large.txt") {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(published) != 1 || published[0].LogicalPath != "z-valid.txt" {
		t.Fatalf("Scan() published = %#v", published)
	}
}

func TestPublishInlineArtifactArchivesAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dataDir)
	service := artifactapp.New(database.Attachments(), files, files)
	imageData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	request := session.InlineArtifactRequest{
		SessionID: "session-1", Data: imageData, Filename: "generated.png", MimeType: "image/png",
		SourceType: session.AttachmentSourceCodex, SourceKey: "process-1:event-1:0",
	}
	first, err := service.PublishInlineArtifact(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.PublishInlineArtifact(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.ArtifactKind != session.ArtifactKindImage || first.PreviewKind != session.PreviewKindImage || first.SHA256 == "" {
		t.Fatalf("inline artifacts = %#v / %#v", first, second)
	}
	if body, err := os.ReadFile(first.Path); err != nil || !bytes.Equal(body, imageData) {
		t.Fatalf("inline archive bytes=%d err=%v", len(body), err)
	}
	media, ok, err := service.ReadMCPContent(ctx, first.ID)
	if err != nil || !ok || media.Type != "image" || media.MIMEType != "image/png" || !bytes.Equal(media.Data, imageData) {
		t.Fatalf("MCP media = %#v ok=%t err=%v", media, ok, err)
	}
	active, err := service.PublishInlineArtifact(ctx, session.InlineArtifactRequest{
		SessionID: "session-1", Data: []byte("<svg onload='alert(1)'></svg>"), Filename: "active.svg", MimeType: "image/svg+xml",
		SourceType: session.AttachmentSourceCodex, SourceKey: "process-1:event-1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if active.ArtifactKind != session.ArtifactKindText || active.PreviewKind != session.PreviewKindText || active.MimeType != "text/plain" {
		t.Fatalf("active content classification = %#v", active)
	}
	if _, err := service.PublishInlineArtifact(ctx, session.InlineArtifactRequest{SessionID: "session-1", Data: []byte("data")}); err == nil {
		t.Fatal("inline artifact without source key succeeded")
	}
}

func TestReconcileQuarantinesRestoresOpenSessionAndDeletesClosedSession(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, card := range []session.Session{
		{ID: "open-session", ProjectID: "project-1", Requirement: "open", Mode: session.ModeChat, Status: session.StatusStopped, Priority: session.PriorityMedium, CreatedAt: now, UpdatedAt: now},
		{ID: "closed-session", ProjectID: "project-1", Requirement: "closed", Mode: session.ModeChat, Status: session.StatusClosed, Priority: session.PriorityMedium, CreatedAt: now, UpdatedAt: now, ClosedAt: &now},
	} {
		if err := database.Sessions().Create(ctx, card); err != nil {
			t.Fatal(err)
		}
	}
	files := filestore.New(dataDir)
	for _, sessionID := range []session.ID{"open-session", "closed-session", "orphan-session"} {
		dir, err := files.EnsureArtifactDir(ctx, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "result.txt"), []byte(string(sessionID)), 0o644); err != nil {
			t.Fatal(err)
		}
		quarantine, err := files.QuarantineArtifactDir(ctx, sessionID, "token")
		if err != nil {
			t.Fatal(err)
		}
		if sessionID == "orphan-session" {
			old := now.Add(-25 * time.Hour)
			if err := os.Chtimes(quarantine, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	service := artifactapp.New(database.Attachments(), files, files, database.Sessions())
	count, err := service.ReconcileQuarantines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("reconciled count = %d", count)
	}
	if _, err := os.Stat(filepath.Join(files.ArtifactDir("open-session"), "result.txt")); err != nil {
		t.Fatalf("open session output was not restored: %v", err)
	}
	quarantines, err := files.ListArtifactQuarantines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantines) != 0 {
		t.Fatalf("remaining quarantines = %#v", quarantines)
	}
}

func TestReconcileOutputsScansOpenAndCleansClosedAndOldOrphans(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, card := range []session.Session{
		{ID: "open-session", ProjectID: "project-1", Requirement: "open", Mode: session.ModeChat, Status: session.StatusStopped, Priority: session.PriorityMedium, CreatedAt: now, UpdatedAt: now},
		{ID: "closed-session", ProjectID: "project-1", Requirement: "closed", Mode: session.ModeChat, Status: session.StatusClosed, Priority: session.PriorityMedium, CreatedAt: now, UpdatedAt: now, ClosedAt: &now},
	} {
		if err := database.Sessions().Create(ctx, card); err != nil {
			t.Fatal(err)
		}
	}
	files := filestore.New(dataDir)
	for _, sessionID := range []session.ID{"open-session", "closed-session", "old-orphan", "young-orphan"} {
		dir, err := files.EnsureArtifactDir(ctx, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "result.txt"), []byte(string(sessionID)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldOrphan := files.ArtifactDir("old-orphan")
	if err := os.Chtimes(oldOrphan, now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	service := artifactapp.New(database.Attachments(), files, files, database.Sessions())
	count, err := service.ReconcileOutputs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("reconciled count = %d", count)
	}
	artifacts, total, err := service.List(ctx, session.ArtifactQuery{SessionID: "open-session"})
	if err != nil || total != 1 || len(artifacts) != 1 {
		t.Fatalf("open session artifacts total=%d rows=%#v err=%v", total, artifacts, err)
	}
	for _, sessionID := range []session.ID{"closed-session", "old-orphan"} {
		if _, err := os.Stat(files.ArtifactDir(sessionID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("output %s still exists: %v", sessionID, err)
		}
	}
	if _, err := os.Stat(files.ArtifactDir("young-orphan")); err != nil {
		t.Fatalf("young orphan was removed: %v", err)
	}
}

func TestReconcileDeletedArtifactsFinishesPhysicalCleanup(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	files := filestore.New(dataDir)
	service := artifactapp.New(database.Attachments(), files, files)
	outputDir, err := files.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(outputDir, "result.txt")
	if err := os.WriteFile(source, []byte("result"), 0o644); err != nil {
		t.Fatal(err)
	}
	artifact, err := service.Publish(ctx, artifactapp.PublishInput{SessionID: "session-1", Path: source})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Attachments().SoftDeleteArtifact(ctx, artifact.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	count, err := service.ReconcileDeletedArtifacts(ctx)
	if err != nil || count != 1 {
		t.Fatalf("reconcile deleted count=%d err=%v", count, err)
	}
	if _, err := os.Stat(artifact.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact still exists: %v", err)
	}
	found, err := database.Attachments().FindSessionAttachment(ctx, artifact.ID)
	if err != nil || found.Path != "" {
		t.Fatalf("cleaned artifact = %#v err=%v", found, err)
	}
	if count, err := service.ReconcileDeletedArtifacts(ctx); err != nil || count != 0 {
		t.Fatalf("second reconciliation count=%d err=%v", count, err)
	}
}
