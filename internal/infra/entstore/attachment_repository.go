package entstore

import (
	"context"
	"fmt"

	domainsession "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
)

type AttachmentRepository struct {
	client *ent.Client
}

func NewAttachmentRepository(client *ent.Client) *AttachmentRepository {
	return &AttachmentRepository{client: client}
}

func (r *AttachmentRepository) SaveStagedAttachment(ctx context.Context, attachment domainsession.StagedAttachment) error {
	create := r.client.StagedAttachment.Create().
		SetID(string(attachment.ID)).
		SetOwnerKeyHash(attachment.OwnerKeyHash).
		SetFilename(attachment.Filename).
		SetPath(attachment.Path).
		SetMimeType(attachment.MimeType).
		SetSize(attachment.Size).
		SetPreviewable(attachment.Previewable)
	if !attachment.CreatedAt.IsZero() {
		create.SetCreatedAt(attachment.CreatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("save staged attachment: %w", err)
	}
	return nil
}

func (r *AttachmentRepository) FindStagedAttachment(ctx context.Context, id domainsession.StagedAttachmentID) (domainsession.StagedAttachment, error) {
	row, err := r.client.StagedAttachment.Get(ctx, string(id))
	if err != nil {
		return domainsession.StagedAttachment{}, fmt.Errorf("find staged attachment: %w", err)
	}
	return toDomainStagedAttachment(row), nil
}

func (r *AttachmentRepository) DeleteStagedAttachment(ctx context.Context, id domainsession.StagedAttachmentID) error {
	if err := r.client.StagedAttachment.DeleteOneID(string(id)).Exec(ctx); err != nil {
		return fmt.Errorf("delete staged attachment: %w", err)
	}
	return nil
}

func toDomainStagedAttachment(row *ent.StagedAttachment) domainsession.StagedAttachment {
	return domainsession.StagedAttachment{
		ID:           domainsession.StagedAttachmentID(row.ID),
		OwnerKeyHash: row.OwnerKeyHash,
		Filename:     row.Filename,
		Path:         row.Path,
		MimeType:     row.MimeType,
		Size:         row.Size,
		Previewable:  row.Previewable,
		CreatedAt:    row.CreatedAt,
	}
}
