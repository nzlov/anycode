package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
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
	Filename   string
	MimeType   string
	Size       int64
	ETag       string
	ModifiedAt time.Time
	Reader     io.ReadCloser
	Seeker     io.ReadSeeker
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
		return AttachmentDTO{}, apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "stage attachment file failed").WithRetryable(true)
	}
	if err := s.repo.SaveStagedAttachment(ctx, staged); err != nil {
		_ = s.store.DeleteStaged(ctx, staged.ID)
		return AttachmentDTO{}, apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "save staged attachment failed").WithRetryable(true)
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
		return apperror.Wrap(fmt.Errorf("%w: %v", ErrAttachmentNotFound, err), apperror.CodeNotFound, apperror.CategoryValidationError, "attachment not found")
	}
	if err := s.store.DeleteStaged(ctx, id); err != nil {
		return apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "delete staged attachment file failed").WithRetryable(true)
	}
	if err := s.repo.DeleteStagedAttachment(ctx, id); err != nil {
		return apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "delete staged attachment failed").WithRetryable(true)
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
		return apperror.Wrap(fmt.Errorf("%w: %v", ErrAttachmentNotFound, err), apperror.CodeNotFound, apperror.CategoryValidationError, "attachment not found")
	}
	if err := s.store.DeleteSession(ctx, id); err != nil {
		return apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "delete session attachment file failed").WithRetryable(true)
	}
	if err := s.repo.DeleteSessionAttachment(ctx, id); err != nil {
		return apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "delete session attachment failed").WithRetryable(true)
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
		return Stream{}, apperror.Wrap(fmt.Errorf("%w: %v", ErrAttachmentNotFound, err), apperror.CodeNotFound, apperror.CategoryValidationError, "attachment not found")
	}
	if attachment.DeletedAt != nil {
		return Stream{}, apperror.Wrap(ErrAttachmentNotFound, apperror.CodeNotFound, apperror.CategoryValidationError, "attachment not found")
	}
	if mode == OpenPreview && !attachment.Previewable {
		return Stream{}, apperror.Wrap(ErrNotPreviewable, apperror.CodeAttachmentFailed, apperror.CategoryValidationError, "attachment is not previewable")
	}
	stream, err := s.store.Open(ctx, attachment.Path)
	if err != nil {
		return Stream{}, apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "open attachment file failed").WithRetryable(true)
	}
	return Stream{
		Filename:   attachment.Filename,
		MimeType:   attachment.MimeType,
		Size:       attachment.Size,
		ETag:       attachment.SHA256,
		ModifiedAt: attachment.CreatedAt,
		Reader:     stream.Reader,
		Seeker:     stream.Seeker,
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
