package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	domain "github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	StageAttachment(ctx context.Context, input StageAttachmentInput) (AttachmentDTO, error)
	DeleteStagedAttachment(ctx context.Context, id domain.StagedAttachmentID) error
	DeleteSessionAttachment(ctx context.Context, id domain.SessionAttachmentID) error
	OpenAttachment(ctx context.Context, id domain.AttachmentID, mode OpenMode) (Stream, error)
}

type StageAttachmentInput struct {
	OwnerKeyHash string
	Filename     string
	MimeType     string
	Size         int64
	Reader       io.Reader
}

type OpenMode string

const (
	OpenPreview  OpenMode = "preview"
	OpenDownload OpenMode = "download"
)

var (
	ErrAttachmentNotFound = errors.New("attachment not found")
	ErrNotPreviewable     = errors.New("attachment is not previewable")
)

type AttachmentDTO struct {
	ID          string
	Filename    string
	MimeType    string
	Size        int64
	Previewable bool
}

type Stream struct {
	Filename string
	MimeType string
	Reader   io.ReadCloser
}

type Service struct {
	repo  domain.AttachmentRepository
	store domain.AttachmentStore
}

func New(repo domain.AttachmentRepository, store domain.AttachmentStore) *Service {
	return &Service{repo: repo, store: store}
}

func (s *Service) StageAttachment(ctx context.Context, input StageAttachmentInput) (AttachmentDTO, error) {
	if s == nil {
		return AttachmentDTO{}, errors.New("attachment usecase: nil service")
	}
	if s.repo == nil {
		return AttachmentDTO{}, errors.New("attachment repository is required")
	}
	if s.store == nil {
		return AttachmentDTO{}, errors.New("attachment store is required")
	}
	if input.Reader == nil {
		return AttachmentDTO{}, errors.New("attachment reader is required")
	}
	if strings.TrimSpace(input.Filename) == "" {
		return AttachmentDTO{}, errors.New("attachment filename is required")
	}
	staged, err := s.store.Stage(ctx, domain.StageAttachmentInput{
		OwnerKeyHash: input.OwnerKeyHash,
		Filename:     input.Filename,
		MimeType:     input.MimeType,
		Size:         input.Size,
		Reader:       input.Reader,
	})
	if err != nil {
		return AttachmentDTO{}, fmt.Errorf("stage attachment file: %w", err)
	}
	if err := s.repo.SaveStagedAttachment(ctx, staged); err != nil {
		_ = s.store.DeleteStaged(ctx, staged.ID)
		return AttachmentDTO{}, fmt.Errorf("save staged attachment: %w", err)
	}
	return toAttachmentDTO(staged), nil
}

func (s *Service) DeleteStagedAttachment(ctx context.Context, id domain.StagedAttachmentID) error {
	if s == nil {
		return errors.New("attachment usecase: nil service")
	}
	if s.repo == nil {
		return errors.New("attachment repository is required")
	}
	if s.store == nil {
		return errors.New("attachment store is required")
	}
	if id == "" {
		return errors.New("staged attachment id is required")
	}
	if _, err := s.repo.FindStagedAttachment(ctx, id); err != nil {
		return fmt.Errorf("%w: %v", ErrAttachmentNotFound, err)
	}
	if err := s.store.DeleteStaged(ctx, id); err != nil {
		return fmt.Errorf("delete staged attachment file: %w", err)
	}
	if err := s.repo.DeleteStagedAttachment(ctx, id); err != nil {
		return fmt.Errorf("delete staged attachment: %w", err)
	}
	return nil
}

func (s *Service) DeleteSessionAttachment(ctx context.Context, id domain.SessionAttachmentID) error {
	if s == nil {
		return errors.New("attachment usecase: nil service")
	}
	if s.repo == nil {
		return errors.New("attachment repository is required")
	}
	if s.store == nil {
		return errors.New("attachment store is required")
	}
	if id == "" {
		return errors.New("session attachment id is required")
	}
	if _, err := s.repo.FindSessionAttachment(ctx, id); err != nil {
		return fmt.Errorf("%w: %v", ErrAttachmentNotFound, err)
	}
	if err := s.store.DeleteSession(ctx, id); err != nil {
		return fmt.Errorf("delete session attachment file: %w", err)
	}
	if err := s.repo.DeleteSessionAttachment(ctx, id); err != nil {
		return fmt.Errorf("delete session attachment: %w", err)
	}
	return nil
}

func (s *Service) OpenAttachment(ctx context.Context, id domain.AttachmentID, mode OpenMode) (Stream, error) {
	if s == nil {
		return Stream{}, errors.New("attachment usecase: nil service")
	}
	if s.repo == nil {
		return Stream{}, errors.New("attachment repository is required")
	}
	if s.store == nil {
		return Stream{}, errors.New("attachment store is required")
	}
	if id == "" {
		return Stream{}, errors.New("attachment id is required")
	}
	attachment, err := s.repo.FindSessionAttachment(ctx, domain.SessionAttachmentID(id))
	if err != nil {
		return Stream{}, fmt.Errorf("%w: %v", ErrAttachmentNotFound, err)
	}
	if mode == OpenPreview && !attachment.Previewable {
		return Stream{}, ErrNotPreviewable
	}
	stream, err := s.store.Open(ctx, attachment.Path)
	if err != nil {
		return Stream{}, fmt.Errorf("open attachment file: %w", err)
	}
	return Stream{
		Filename: stream.Filename,
		MimeType: stream.MimeType,
		Reader:   stream.Reader,
	}, nil
}

func toAttachmentDTO(attachment domain.StagedAttachment) AttachmentDTO {
	return AttachmentDTO{
		ID:          string(attachment.ID),
		Filename:    attachment.Filename,
		MimeType:    attachment.MimeType,
		Size:        attachment.Size,
		Previewable: attachment.Previewable,
	}
}
