package attachment

import (
	"context"
	"io"

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
