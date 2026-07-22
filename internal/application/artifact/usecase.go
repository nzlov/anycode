package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
)

const (
	DefaultMaxFileBytes     int64 = 512 << 20
	DefaultMaxSessionBytes  int64 = 10 << 30
	MaxInlineArtifactBytes  int64 = 25 << 20
	MaxResolveArtifactPaths       = 100
	OrphanOutputMaxAge            = 24 * time.Hour
)

type UseCase interface {
	Publish(ctx context.Context, input PublishInput) (session.SessionFile, error)
	List(ctx context.Context, query session.ArtifactQuery) ([]session.SessionFile, error)
	Resolve(ctx context.Context, sessionID session.ID, logicalPaths []string) ([]session.SessionFile, error)
	ReadToolContent(ctx context.Context, id session.SessionFileID) (ToolContent, bool, error)
	ReconcileQuarantines(ctx context.Context) (int, error)
	ReconcileOutputs(ctx context.Context) (int, error)
}

type PublishInput struct {
	SessionID session.ID
	Path      string
}

type ToolContent struct {
	Type     string
	MIMEType string
	Data     []byte
}

type Limits struct {
	MaxFileBytes    int64
	MaxSessionBytes int64
}

type Service struct {
	store           session.ArtifactStore
	maxFileBytes    int64
	maxSessionBytes int64
	now             func() time.Time
	sessions        session.Repository
	lockMu          sync.Mutex
	sessionLocks    map[session.ID]*sessionArtifactLock
}

type sessionArtifactLock struct {
	mu   sync.Mutex
	refs int
}

func New(store session.ArtifactStore, sessions ...session.Repository) *Service {
	service := &Service{
		store:           store,
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

func (s *Service) Publish(ctx context.Context, input PublishInput) (session.SessionFile, error) {
	if s == nil || s.store == nil {
		return session.SessionFile{}, errors.New("artifact usecase is not configured")
	}
	if input.SessionID == "" {
		return session.SessionFile{}, errors.New("session id is required")
	}
	unlock := s.lockSession(input.SessionID)
	defer unlock()
	if err := s.validateSessionWritable(ctx, input.SessionID); err != nil {
		return session.SessionFile{}, err
	}
	root, err := s.store.EnsureArtifactDir(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	sourcePath := strings.TrimSpace(input.Path)
	if sourcePath == "" {
		return session.SessionFile{}, errors.New("artifact path is required")
	}
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(root, filepath.FromSlash(sourcePath))
	}
	artifact, err := s.store.InspectArtifact(ctx, session.InspectArtifactInput{
		SessionID:  input.SessionID,
		SourcePath: sourcePath,
		MaxBytes:   s.maxFileBytes,
	})
	if err != nil {
		return session.SessionFile{}, err
	}
	used, err := s.store.SumArtifactSize(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	if used > s.maxSessionBytes {
		return session.SessionFile{}, fmt.Errorf("artifact exceeds %d byte session limit", s.maxSessionBytes)
	}
	return artifact, nil
}

func (s *Service) List(ctx context.Context, query session.ArtifactQuery) ([]session.SessionFile, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("artifact usecase is not configured")
	}
	artifacts, err := s.store.ListArtifacts(ctx, query)
	if err != nil {
		return nil, err
	}
	if s.sessions != nil && query.SessionID != "" {
		count, err := s.store.CountArtifacts(ctx, query.SessionID)
		if err != nil {
			return nil, err
		}
		if err := s.sessions.UpdateArtifactCount(ctx, query.SessionID, count); err != nil {
			return nil, err
		}
	}
	return artifacts, nil
}

func (s *Service) Resolve(ctx context.Context, sessionID session.ID, logicalPaths []string) ([]session.SessionFile, error) {
	if s == nil || s.store == nil {
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
	return s.store.ResolveArtifacts(ctx, sessionID, normalized)
}

func (s *Service) ReadToolContent(ctx context.Context, id session.SessionFileID) (ToolContent, bool, error) {
	if s == nil || s.store == nil {
		return ToolContent{}, false, errors.New("artifact usecase is not configured")
	}
	artifact, err := s.store.FindArtifact(ctx, id)
	if err != nil {
		return ToolContent{}, false, err
	}
	contentType := ""
	switch artifact.ArtifactKind {
	case session.ArtifactKindImage:
		contentType = "image"
	case session.ArtifactKindAudio:
		contentType = "audio"
	}
	if contentType == "" || artifact.Size > MaxInlineArtifactBytes {
		return ToolContent{}, false, nil
	}
	stream, err := s.store.OpenArtifact(ctx, id)
	if err != nil {
		return ToolContent{}, false, err
	}
	defer stream.Reader.Close()
	data, err := io.ReadAll(io.LimitReader(stream.Reader, MaxInlineArtifactBytes+1))
	if err != nil {
		return ToolContent{}, false, err
	}
	if int64(len(data)) > MaxInlineArtifactBytes {
		return ToolContent{}, false, nil
	}
	return ToolContent{Type: contentType, MIMEType: artifact.MimeType, Data: data}, true, nil
}

func (s *Service) PublishInlineArtifact(ctx context.Context, input session.InlineArtifactRequest) (session.SessionFile, error) {
	if s == nil || s.store == nil {
		return session.SessionFile{}, errors.New("artifact usecase is not configured")
	}
	if input.SessionID == "" || len(input.Data) == 0 || strings.TrimSpace(input.SourceKey) == "" {
		return session.SessionFile{}, errors.New("inline artifact session, data, and source key are required")
	}
	maxBytes := min(MaxInlineArtifactBytes, s.maxFileBytes)
	if int64(len(input.Data)) > maxBytes {
		return session.SessionFile{}, fmt.Errorf("inline artifact exceeds %d byte limit", maxBytes)
	}
	unlock := s.lockSession(input.SessionID)
	defer unlock()
	if err := s.validateSessionWritable(ctx, input.SessionID); err != nil {
		return session.SessionFile{}, err
	}
	used, err := s.store.SumArtifactSize(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	if used > s.maxSessionBytes-int64(len(input.Data)) {
		return session.SessionFile{}, fmt.Errorf("inline artifact exceeds %d byte session limit", s.maxSessionBytes)
	}
	return s.store.WriteInlineArtifact(ctx, session.WriteInlineArtifactInput{
		SessionID: input.SessionID,
		Data:      input.Data,
		Filename:  input.Filename,
		SourceKey: input.SourceKey,
		MaxBytes:  maxBytes,
	})
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
		card, err := s.sessions.Find(ctx, output.SessionID)
		remove := false
		switch {
		case errors.Is(err, session.ErrSessionNotFound):
			remove = s.now().Sub(output.ModifiedAt) >= OrphanOutputMaxAge
		case err != nil:
		case card.Status == session.StatusClosed:
			remove = true
		}
		if err != nil && !errors.Is(err, session.ErrSessionNotFound) {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("reconcile artifact output %s: %w", output.SessionID, err))
			continue
		}
		if !remove {
			continue
		}
		if err := s.store.DeleteArtifactOutputDirectory(ctx, output.SessionID); err != nil {
			reconcileErrs = append(reconcileErrs, fmt.Errorf("reconcile artifact output %s: %w", output.SessionID, err))
			continue
		}
		processed++
	}
	return processed, errors.Join(reconcileErrs...)
}

func (s *Service) validateSessionWritable(ctx context.Context, sessionID session.ID) error {
	if s.sessions == nil {
		return nil
	}
	card, err := s.sessions.Find(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("find artifact session: %w", err)
	}
	if card.Status == session.StatusClosed || card.CloseReason != nil {
		return errors.New("session does not accept artifact publication")
	}
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

func normalizeLogicalPathReference(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if value == "" || path.IsAbs(value) || (len(value) >= 2 && value[1] == ':') {
		return "", errors.New("artifact reference must be a relative logical path")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("artifact reference cannot contain dot segments")
		}
	}
	value = path.Clean(value)
	if value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return "", errors.New("artifact reference must stay inside the output directory")
	}
	return value, nil
}
