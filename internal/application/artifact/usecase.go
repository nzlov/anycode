package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/session"
)

const (
	DefaultMaxFileBytes    int64 = 512 << 20
	DefaultMaxSessionBytes int64 = 10 << 30
	OrphanOutputMaxAge           = 24 * time.Hour
)

var ErrArtifactDeleted = errors.New("artifact version was deleted and cannot be republished")
var errArtifactCommitted = errors.New("artifact metadata was committed")

type UseCase interface {
	Publish(ctx context.Context, input PublishInput) (session.SessionFile, error)
	Scan(ctx context.Context, input ScanInput) ([]session.SessionFile, error)
	List(ctx context.Context, query session.ArtifactQuery) ([]session.SessionFile, error)
	Resolve(ctx context.Context, sessionID session.ID, logicalPaths []string) ([]session.SessionFile, error)
	Delete(ctx context.Context, id session.SessionFileID) (session.SessionFile, error)
	UseAsInput(ctx context.Context, id session.SessionFileID) (session.SessionFile, error)
	ReadMCPContent(ctx context.Context, id session.SessionFileID) (MCPContent, bool, error)
	ReconcileQuarantines(ctx context.Context) (int, error)
	ReconcileOutputs(ctx context.Context) (int, error)
	ReconcileDeletedArtifacts(ctx context.Context) (int, error)
}

const MaxInlineArtifactBytes int64 = 25 << 20
const MaxResolveArtifactPaths = 100

type PublishInput struct {
	SessionID     session.ID
	Path          string
	LogicalPath   string
	SourceType    session.AttachmentSourceType
	SourceID      string
	SourceKey     string
	ProcessRunID  string
	NodeRunID     string
	CorrelationID string
}

type MCPContent struct {
	Type     string
	MIMEType string
	Data     []byte
}

type ScanInput = session.ArtifactScanRequest

type Service struct {
	repo            session.ArtifactRepository
	store           session.ArtifactStore
	attachments     session.AttachmentStore
	maxFileBytes    int64
	maxSessionBytes int64
	now             func() time.Time
	sessions        session.Repository
	events          eventdomain.Store
	publisher       eventdomain.Publisher
	lockMu          sync.Mutex
	sessionLocks    map[session.ID]*sessionArtifactLock
}

type sessionArtifactLock struct {
	mu   sync.Mutex
	refs int
}

type Limits struct {
	MaxFileBytes    int64
	MaxSessionBytes int64
}

type artifactEventCommitter interface {
	SaveArtifactWithEvent(ctx context.Context, artifact session.SessionFile, event eventdomain.DomainEvent) error
	DeleteArtifactWithEvent(ctx context.Context, artifact session.SessionFile, event eventdomain.DomainEvent) error
}

func New(repo session.ArtifactRepository, store session.ArtifactStore, attachments session.AttachmentStore, sessions ...session.Repository) *Service {
	service := &Service{
		repo:            repo,
		store:           store,
		attachments:     attachments,
		maxFileBytes:    DefaultMaxFileBytes,
		maxSessionBytes: DefaultMaxSessionBytes,
		now:             time.Now,
	}
	if len(sessions) > 0 {
		service.sessions = sessions[0]
	}
	return service
}

func (s *Service) SetLimits(limits Limits) {
	if limits.MaxFileBytes > 0 {
		s.maxFileBytes = limits.MaxFileBytes
	}
	if limits.MaxSessionBytes > 0 {
		s.maxSessionBytes = limits.MaxSessionBytes
	}
}

func (s *Service) SetEvents(events eventdomain.Store, publisher eventdomain.Publisher) {
	s.events = events
	s.publisher = publisher
}

func (s *Service) ReconcileQuarantines(ctx context.Context) (int, error) {
	if s == nil || s.store == nil || s.sessions == nil {
		return 0, errors.New("artifact reconciliation is not configured")
	}
	quarantines, err := s.store.ListArtifactQuarantines(ctx)
	if err != nil {
		return 0, err
	}
	processed := 0
	var reconcileErrs []error
	for _, quarantine := range quarantines {
		card, err := s.sessions.Find(ctx, quarantine.SessionID)
		switch {
		case errors.Is(err, session.ErrSessionNotFound):
			if s.now().Sub(quarantine.ModifiedAt) < OrphanOutputMaxAge {
				continue
			}
			err = s.store.DeleteQuarantine(ctx, quarantine.Path)
		case err != nil:
		case card.Status == session.StatusClosed:
			err = s.store.DeleteQuarantine(ctx, quarantine.Path)
		default:
			err = s.store.RestoreArtifactDir(ctx, quarantine.SessionID, quarantine.Path)
		}
		if err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("reconcile artifact quarantine for session %s: %w", quarantine.SessionID, err))
			continue
		}
		processed++
	}
	return processed, errors.Join(reconcileErrs...)
}

func (s *Service) ReconcileOutputs(ctx context.Context) (int, error) {
	if s == nil || s.store == nil || s.sessions == nil {
		return 0, errors.New("artifact reconciliation is not configured")
	}
	outputs, err := s.store.ListArtifactOutputDirectories(ctx)
	if err != nil {
		return 0, err
	}
	processed := 0
	var reconcileErrs []error
	for _, output := range outputs {
		if err := ctx.Err(); err != nil {
			return processed, errors.Join(append(reconcileErrs, err)...)
		}
		card, err := s.sessions.Find(ctx, output.SessionID)
		switch {
		case errors.Is(err, session.ErrSessionNotFound):
			if s.now().Sub(output.ModifiedAt) < OrphanOutputMaxAge {
				continue
			}
			err = s.store.DeleteArtifactOutputDirectory(ctx, output.SessionID)
		case err != nil:
		case card.Status == session.StatusClosed:
			err = s.store.DeleteArtifactOutputDirectory(ctx, output.SessionID)
		default:
			_, err = s.Scan(ctx, ScanInput{SessionID: output.SessionID, SourceType: session.AttachmentSourceReconciled})
		}
		if err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("reconcile artifact output %s: %w", output.SessionID, err))
			continue
		}
		processed++
	}
	return processed, errors.Join(reconcileErrs...)
}

func (s *Service) ReconcileDeletedArtifacts(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil || s.attachments == nil {
		return 0, errors.New("artifact deletion reconciliation is not configured")
	}
	artifacts, err := s.repo.ListArtifactsPendingPhysicalDelete(ctx, 100)
	if err != nil {
		return 0, err
	}
	processed := 0
	var cleanupErrs []error
	for _, artifact := range artifacts {
		if err := s.attachments.DeleteSession(ctx, artifact.ID); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("delete artifact %s: %w", artifact.ID, err))
			continue
		}
		if err := s.repo.MarkArtifactPhysicalDeleted(ctx, artifact.ID); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("confirm artifact deletion %s: %w", artifact.ID, err))
			continue
		}
		processed++
	}
	return processed, errors.Join(cleanupErrs...)
}

func (s *Service) Publish(ctx context.Context, input PublishInput) (session.SessionFile, error) {
	return s.publish(ctx, input, false)
}

func (s *Service) publish(ctx context.Context, input PublishInput, allowClosing bool) (session.SessionFile, error) {
	if s == nil || s.repo == nil || s.store == nil || s.attachments == nil {
		return session.SessionFile{}, errors.New("artifact usecase is not configured")
	}
	if input.SessionID == "" {
		return session.SessionFile{}, errors.New("session id is required")
	}
	unlock := s.lockSession(input.SessionID)
	defer unlock()
	if err := s.validateSessionWritable(ctx, input.SessionID, allowClosing); err != nil {
		return session.SessionFile{}, err
	}
	root, err := s.store.EnsureArtifactDir(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	sourcePath := strings.TrimSpace(input.Path)
	if sourcePath == "" {
		sourcePath = strings.TrimSpace(input.LogicalPath)
	}
	if sourcePath == "" {
		return session.SessionFile{}, errors.New("artifact path is required")
	}
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(root, filepath.FromSlash(sourcePath))
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return session.SessionFile{}, fmt.Errorf("inspect artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return session.SessionFile{}, errors.New("artifact must be a regular file")
	}
	logicalPath := strings.TrimSpace(input.LogicalPath)
	if logicalPath == "" {
		logicalPath, err = filepath.Rel(root, sourcePath)
		if err != nil {
			return session.SessionFile{}, fmt.Errorf("resolve artifact path: %w", err)
		}
	}
	if filepath.IsAbs(logicalPath) || logicalPath == ".." || strings.HasPrefix(logicalPath, ".."+string(filepath.Separator)) {
		return session.SessionFile{}, errors.New("artifact logical path must stay inside the output directory")
	}
	logicalPath = filepath.ToSlash(filepath.Clean(logicalPath))
	sourceKey := strings.TrimSpace(input.SourceKey)
	if sourceKey == "" {
		sourceKey = sourceVersionKey(logicalPath, info)
	}
	if existing, ok, err := s.repo.FindArtifactBySourceKey(ctx, input.SessionID, sourceKey); err != nil {
		return session.SessionFile{}, err
	} else if ok {
		if existing.DeletedAt != nil {
			return session.SessionFile{}, ErrArtifactDeleted
		}
		return existing, nil
	}
	used, err := s.repo.SumSessionArtifactSize(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	if info.Size() > s.maxFileBytes {
		return session.SessionFile{}, fmt.Errorf("artifact exceeds %d byte file limit", s.maxFileBytes)
	}
	if used > s.maxSessionBytes-info.Size() {
		return session.SessionFile{}, fmt.Errorf("artifact exceeds %d byte session limit", s.maxSessionBytes)
	}
	sourceType := input.SourceType
	if sourceType == "" {
		sourceType = session.AttachmentSourcePublished
	}
	artifact, err := s.store.ArchiveArtifact(ctx, session.ArchiveArtifactInput{
		SessionID:     input.SessionID,
		SourcePath:    sourcePath,
		LogicalPath:   logicalPath,
		SourceType:    sourceType,
		SourceID:      input.SourceID,
		SourceKey:     sourceKey,
		ProcessRunID:  input.ProcessRunID,
		NodeRunID:     input.NodeRunID,
		CorrelationID: input.CorrelationID,
		MaxBytes:      s.maxFileBytes,
	})
	if err != nil {
		return session.SessionFile{}, err
	}
	after, statErr := os.Lstat(sourcePath)
	if statErr != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		cleanupErr := s.attachments.DeleteSession(context.WithoutCancel(ctx), artifact.ID)
		if statErr != nil {
			return session.SessionFile{}, errors.Join(fmt.Errorf("recheck artifact stability: %w", statErr), cleanupErr)
		}
		return session.SessionFile{}, errors.Join(errors.New("artifact changed while it was being archived"), cleanupErr)
	}
	if err := s.commitArtifact(ctx, artifact); err != nil {
		var cleanupErr error
		if !errors.Is(err, errArtifactCommitted) {
			cleanupErr = s.attachments.DeleteSession(context.WithoutCancel(ctx), artifact.ID)
		}
		if cleanupErr != nil {
			return session.SessionFile{}, errors.Join(err, fmt.Errorf("remove uncommitted artifact: %w", cleanupErr))
		}
		return session.SessionFile{}, err
	}
	return artifact, nil
}

func (s *Service) Scan(ctx context.Context, input ScanInput) ([]session.SessionFile, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("artifact usecase is not configured")
	}
	root, err := s.store.EnsureArtifactDir(ctx, input.SessionID)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".partial") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan artifact directory: %w", err)
	}
	sort.Strings(paths)
	published := make([]session.SessionFile, 0, len(paths))
	var scanErrs []error
	for _, path := range paths {
		sourceType := input.SourceType
		if relative, relErr := filepath.Rel(root, path); relErr == nil {
			parts := strings.Split(filepath.ToSlash(relative), "/")
			if len(parts) > 1 && parts[0] == "browser" {
				sourceType = session.AttachmentSourcePlaywright
			}
		}
		artifact, err := s.publish(ctx, PublishInput{
			SessionID:    input.SessionID,
			Path:         path,
			SourceType:   sourceType,
			SourceID:     input.SourceID,
			ProcessRunID: input.ProcessRunID,
			NodeRunID:    input.NodeRunID,
		}, true)
		if errors.Is(err, ErrArtifactDeleted) {
			continue
		}
		if err != nil {
			scanErrs = append(scanErrs, fmt.Errorf("publish %s: %w", filepath.Base(path), err))
			continue
		}
		published = append(published, artifact)
	}
	return published, errors.Join(scanErrs...)
}

func (s *Service) List(ctx context.Context, query session.ArtifactQuery) ([]session.SessionFile, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("artifact usecase is not configured")
	}
	return s.repo.ListSessionArtifacts(ctx, query)
}

func (s *Service) Resolve(ctx context.Context, sessionID session.ID, logicalPaths []string) ([]session.SessionFile, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("artifact usecase is not configured")
	}
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	if len(logicalPaths) > MaxResolveArtifactPaths {
		return nil, fmt.Errorf("artifact reference count exceeds %d", MaxResolveArtifactPaths)
	}
	normalized := make([]string, 0, len(logicalPaths))
	seen := make(map[string]struct{}, len(logicalPaths))
	for _, logicalPath := range logicalPaths {
		value, err := normalizeLogicalPathReference(logicalPath)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	if len(normalized) == 0 {
		return []session.SessionFile{}, nil
	}
	resolved, err := s.repo.ResolveLatestSessionArtifactsByLogicalPaths(ctx, sessionID, normalized)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]session.SessionFile, len(resolved))
	for _, artifact := range resolved {
		byPath[artifact.LogicalPath] = artifact
	}
	ordered := make([]session.SessionFile, 0, len(resolved))
	for _, logicalPath := range normalized {
		if artifact, ok := byPath[logicalPath]; ok {
			ordered = append(ordered, artifact)
		}
	}
	return ordered, nil
}

func normalizeLogicalPathReference(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if value == "" || pathpkg.IsAbs(value) || (len(value) >= 2 && value[1] == ':') {
		return "", errors.New("artifact reference must be a relative logical path")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("artifact reference cannot contain dot segments")
		}
	}
	value = pathpkg.Clean(value)
	if value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return "", errors.New("artifact reference must stay inside the output directory")
	}
	return value, nil
}

func (s *Service) Delete(ctx context.Context, id session.SessionFileID) (session.SessionFile, error) {
	if s == nil || s.repo == nil || s.store == nil || s.attachments == nil {
		return session.SessionFile{}, errors.New("artifact usecase is not configured")
	}
	artifact, err := s.repo.FindSessionAttachment(ctx, id)
	if err != nil {
		return session.SessionFile{}, err
	}
	unlock := s.lockSession(artifact.SessionID)
	defer unlock()
	artifact, err = s.repo.FindSessionAttachment(ctx, id)
	if err != nil {
		return session.SessionFile{}, err
	}
	if artifact.Role != session.FileRoleArtifact || artifact.DeletedAt != nil {
		return session.SessionFile{}, errors.New("artifact is unavailable")
	}
	deletedAt := s.now().UTC()
	artifact.DeletedAt = &deletedAt
	var eventErr error
	if s.events != nil {
		event, err := s.artifactEvent(ctx, artifact, "artifact.deleted")
		if err != nil {
			return session.SessionFile{}, err
		}
		if committer, ok := s.repo.(artifactEventCommitter); ok {
			eventErr = committer.DeleteArtifactWithEvent(ctx, artifact, event)
			if eventErr == nil {
				s.publishCommittedEvent(ctx, event)
			}
		} else {
			artifact, eventErr = s.repo.SoftDeleteArtifact(ctx, id, deletedAt)
			if eventErr == nil {
				eventErr = s.publishEvent(ctx, artifact, "artifact.deleted")
				if eventErr != nil && !errors.Is(eventErr, errArtifactCommitted) {
					eventErr = fmt.Errorf("%w: %v", errArtifactCommitted, eventErr)
				}
			}
		}
	} else {
		artifact, eventErr = s.repo.SoftDeleteArtifact(ctx, id, deletedAt)
	}
	if eventErr != nil && !errors.Is(eventErr, errArtifactCommitted) {
		return artifact, eventErr
	}
	deleteErr := s.attachments.DeleteSession(context.WithoutCancel(ctx), id)
	if deleteErr != nil {
		deleteErr = fmt.Errorf("delete archived artifact: %w", deleteErr)
	} else if err := s.repo.MarkArtifactPhysicalDeleted(context.WithoutCancel(ctx), id); err != nil {
		deleteErr = fmt.Errorf("confirm archived artifact deletion: %w", err)
	}
	return artifact, errors.Join(eventErr, deleteErr)
}

func (s *Service) publishEvent(ctx context.Context, artifact session.SessionFile, eventType string) error {
	if s.events == nil {
		return nil
	}
	event, err := s.artifactEvent(ctx, artifact, eventType)
	if err != nil {
		return err
	}
	if err := s.events.Append(ctx, event); err != nil {
		return fmt.Errorf("append artifact event: %w", err)
	}
	s.publishCommittedEvent(ctx, event)
	return nil
}

func (s *Service) artifactEvent(ctx context.Context, artifact session.SessionFile, eventType string) (eventdomain.DomainEvent, error) {
	sessionID := eventdomain.SessionID(artifact.SessionID)
	projectID := ""
	if s.sessions != nil {
		card, err := s.sessions.Find(ctx, artifact.SessionID)
		if err != nil {
			return eventdomain.DomainEvent{}, fmt.Errorf("find artifact event session: %w", err)
		}
		projectID = string(card.ProjectID)
	}
	status := "available"
	if artifact.DeletedAt != nil || eventType == "artifact.deleted" {
		status = "deleted"
	}
	payload := map[string]any{
		"id":            string(artifact.ID),
		"artifactKind":  string(artifact.ArtifactKind),
		"logicalPath":   artifact.LogicalPath,
		"filename":      artifact.Filename,
		"mimeType":      artifact.MimeType,
		"size":          artifact.Size,
		"sha256":        artifact.SHA256,
		"previewKind":   string(artifact.PreviewKind),
		"status":        status,
		"previewUrl":    artifactPreviewURL(artifact),
		"downloadUrl":   "/files/" + string(artifact.ID) + "/download",
		"correlationId": artifact.CorrelationID,
	}
	event := eventdomain.DomainEvent{
		ID:        eventdomain.ID(eventType + ":" + string(artifact.ID)),
		Scope:     eventdomain.Scope{SessionID: &sessionID, ProjectID: projectID},
		SessionID: &sessionID,
		Type:      eventType,
		Payload:   payload,
		Causality: eventdomain.Causality{
			ProcessRunID:  artifact.ProcessRunID,
			NodeRunID:     artifact.NodeRunID,
			CorrelationID: artifact.CorrelationID,
		},
		CreatedAt: artifact.CreatedAt,
	}
	if eventType == "artifact.deleted" {
		event.CreatedAt = s.now().UTC()
	}
	return event, nil
}

func (s *Service) publishCommittedEvent(ctx context.Context, event eventdomain.DomainEvent) {
	if s.publisher != nil {
		if err := s.publisher.PublishAfterCommit(ctx, event); err != nil {
			log.Printf("publish committed artifact event %s (%s): %v", event.ID, event.Type, err)
		}
	}
}

func (s *Service) commitArtifact(ctx context.Context, artifact session.SessionFile) error {
	if s.events == nil {
		return s.repo.SaveSessionAttachment(ctx, artifact)
	}
	event, err := s.artifactEvent(ctx, artifact, "artifact.published")
	if err != nil {
		return err
	}
	if committer, ok := s.repo.(artifactEventCommitter); ok {
		if err := committer.SaveArtifactWithEvent(ctx, artifact, event); err != nil {
			return err
		}
	} else {
		if err := s.repo.SaveSessionAttachment(ctx, artifact); err != nil {
			return err
		}
		if err := s.events.Append(ctx, event); err != nil {
			return fmt.Errorf("%w: append artifact event: %v", errArtifactCommitted, err)
		}
	}
	s.publishCommittedEvent(ctx, event)
	return nil
}

func (s *Service) lockSession(sessionID session.ID) func() {
	s.lockMu.Lock()
	if s.sessionLocks == nil {
		s.sessionLocks = make(map[session.ID]*sessionArtifactLock)
	}
	entry := s.sessionLocks[sessionID]
	if entry == nil {
		entry = &sessionArtifactLock{}
		s.sessionLocks[sessionID] = entry
	}
	entry.refs++
	s.lockMu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		s.lockMu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(s.sessionLocks, sessionID)
		}
		s.lockMu.Unlock()
	}
}

func (s *Service) validateSessionWritable(ctx context.Context, sessionID session.ID, allowClosing bool) error {
	if s.sessions == nil {
		return nil
	}
	card, err := s.sessions.Find(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("find artifact session: %w", err)
	}
	if card.Status == session.StatusClosed || (!allowClosing && card.CloseReason != nil) {
		return errors.New("session does not accept artifact publication")
	}
	return nil
}

func artifactPreviewURL(artifact session.SessionFile) string {
	if artifact.PreviewKind == session.PreviewKindNone || artifact.DeletedAt != nil {
		return ""
	}
	return "/files/" + string(artifact.ID) + "/preview"
}

func (s *Service) UseAsInput(ctx context.Context, id session.SessionFileID) (session.SessionFile, error) {
	if s == nil || s.repo == nil || s.store == nil || s.attachments == nil {
		return session.SessionFile{}, errors.New("artifact usecase is not configured")
	}
	artifact, err := s.repo.FindSessionAttachment(ctx, id)
	if err != nil {
		return session.SessionFile{}, fmt.Errorf("find artifact: %w", err)
	}
	if artifact.Role != session.FileRoleArtifact || artifact.DeletedAt != nil {
		return session.SessionFile{}, errors.New("artifact is unavailable")
	}
	input, err := s.store.CopyArtifactToInput(ctx, artifact)
	if err != nil {
		return session.SessionFile{}, fmt.Errorf("copy artifact as input: %w", err)
	}
	if err := s.repo.SaveSessionAttachment(ctx, input); err != nil {
		cleanupErr := s.attachments.DeleteSession(context.WithoutCancel(ctx), input.ID)
		return session.SessionFile{}, errors.Join(err, cleanupErr)
	}
	return input, nil
}

func (s *Service) ReadMCPContent(ctx context.Context, id session.SessionFileID) (MCPContent, bool, error) {
	if s == nil || s.repo == nil || s.attachments == nil {
		return MCPContent{}, false, errors.New("artifact usecase is not configured")
	}
	artifact, err := s.repo.FindSessionAttachment(ctx, id)
	if err != nil {
		return MCPContent{}, false, err
	}
	contentType := ""
	switch artifact.ArtifactKind {
	case session.ArtifactKindImage:
		contentType = "image"
	case session.ArtifactKindAudio:
		contentType = "audio"
	}
	if artifact.Role != session.FileRoleArtifact || artifact.DeletedAt != nil || contentType == "" || artifact.Size > MaxInlineArtifactBytes {
		return MCPContent{}, false, nil
	}
	stream, err := s.attachments.Open(ctx, artifact.Path)
	if err != nil {
		return MCPContent{}, false, err
	}
	defer stream.Reader.Close()
	data, err := io.ReadAll(io.LimitReader(stream.Reader, MaxInlineArtifactBytes+1))
	if err != nil {
		return MCPContent{}, false, err
	}
	if int64(len(data)) > MaxInlineArtifactBytes {
		return MCPContent{}, false, nil
	}
	return MCPContent{Type: contentType, MIMEType: artifact.MimeType, Data: data}, true, nil
}

func (s *Service) PublishInlineArtifact(ctx context.Context, input session.InlineArtifactRequest) (session.SessionFile, error) {
	if s == nil || s.repo == nil || s.store == nil || s.attachments == nil {
		return session.SessionFile{}, errors.New("artifact usecase is not configured")
	}
	if input.SessionID == "" || len(input.Data) == 0 || strings.TrimSpace(input.SourceKey) == "" {
		return session.SessionFile{}, errors.New("inline artifact session, data, and source key are required")
	}
	if int64(len(input.Data)) > MaxInlineArtifactBytes || int64(len(input.Data)) > s.maxFileBytes {
		return session.SessionFile{}, fmt.Errorf("inline artifact exceeds %d byte limit", min(MaxInlineArtifactBytes, s.maxFileBytes))
	}
	unlock := s.lockSession(input.SessionID)
	defer unlock()
	if err := s.validateSessionWritable(ctx, input.SessionID, false); err != nil {
		return session.SessionFile{}, err
	}
	if existing, ok, err := s.repo.FindArtifactBySourceKey(ctx, input.SessionID, input.SourceKey); err != nil {
		return session.SessionFile{}, err
	} else if ok {
		if existing.DeletedAt != nil {
			return session.SessionFile{}, ErrArtifactDeleted
		}
		return existing, nil
	}
	used, err := s.repo.SumSessionArtifactSize(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	if used > s.maxSessionBytes-int64(len(input.Data)) {
		return session.SessionFile{}, fmt.Errorf("inline artifact exceeds %d byte session limit", s.maxSessionBytes)
	}
	artifact, err := s.store.ArchiveInlineArtifact(ctx, session.ArchiveInlineArtifactInput{
		SessionID: input.SessionID, Data: input.Data, Filename: input.Filename, DeclaredMIME: input.MimeType,
		SourceType: input.SourceType, SourceID: input.SourceID, SourceKey: input.SourceKey,
		ProcessRunID: input.ProcessRunID, NodeRunID: input.NodeRunID, CorrelationID: input.CorrelationID,
		MaxBytes: min(MaxInlineArtifactBytes, s.maxFileBytes),
	})
	if err != nil {
		return session.SessionFile{}, err
	}
	if err := s.commitArtifact(ctx, artifact); err != nil {
		var cleanupErr error
		if !errors.Is(err, errArtifactCommitted) {
			cleanupErr = s.attachments.DeleteSession(context.WithoutCancel(ctx), artifact.ID)
		}
		return session.SessionFile{}, errors.Join(err, cleanupErr)
	}
	return artifact, nil
}

func sourceVersionKey(logicalPath string, info os.FileInfo) string {
	return fmt.Sprintf("%s:%d:%d", filepath.ToSlash(logicalPath), info.Size(), info.ModTime().UnixNano())
}
