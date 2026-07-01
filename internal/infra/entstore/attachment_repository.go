package entstore

import (
	"context"
	"fmt"

	domainsession "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entsessionattachment "github.com/nzlov/anycode/internal/infra/entstore/ent/sessionattachment"
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

func (r *AttachmentRepository) SaveSessionAttachment(ctx context.Context, attachment domainsession.SessionAttachment) error {
	kind := attachment.Kind
	if kind == "" {
		kind = "file"
	}
	create := r.client.SessionAttachment.Create().
		SetID(string(attachment.ID)).
		SetSessionID(string(attachment.SessionID)).
		SetKind(kind).
		SetFilename(attachment.Filename).
		SetPath(attachment.Path).
		SetMimeType(attachment.MimeType).
		SetSize(attachment.Size).
		SetPreviewable(attachment.Previewable)
	if !attachment.CreatedAt.IsZero() {
		create.SetCreatedAt(attachment.CreatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("save session attachment: %w", err)
	}
	return nil
}

func (r *AttachmentRepository) FindSessionAttachment(ctx context.Context, id domainsession.SessionAttachmentID) (domainsession.SessionAttachment, error) {
	row, err := r.client.SessionAttachment.Get(ctx, string(id))
	if err != nil {
		return domainsession.SessionAttachment{}, fmt.Errorf("find session attachment: %w", err)
	}
	return toDomainSessionAttachment(row), nil
}

func (r *AttachmentRepository) ListSessionAttachments(ctx context.Context, sessionID domainsession.ID) ([]domainsession.SessionAttachment, error) {
	rows, err := r.client.SessionAttachment.Query().
		Where(entsessionattachment.SessionIDEQ(string(sessionID))).
		Order(ent.Asc(entsessionattachment.FieldCreatedAt), ent.Asc(entsessionattachment.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list session attachments: %w", err)
	}
	attachments := make([]domainsession.SessionAttachment, 0, len(rows))
	for _, row := range rows {
		attachments = append(attachments, toDomainSessionAttachment(row))
	}
	return attachments, nil
}

func (r *AttachmentRepository) DeleteSessionAttachment(ctx context.Context, id domainsession.SessionAttachmentID) error {
	if err := r.client.SessionAttachment.DeleteOneID(string(id)).Exec(ctx); err != nil {
		return fmt.Errorf("delete session attachment: %w", err)
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

func toDomainSessionAttachment(row *ent.SessionAttachment) domainsession.SessionAttachment {
	return domainsession.SessionAttachment{
		ID:          domainsession.SessionAttachmentID(row.ID),
		SessionID:   domainsession.ID(row.SessionID),
		Kind:        row.Kind,
		Filename:    row.Filename,
		Path:        row.Path,
		MimeType:    row.MimeType,
		Size:        row.Size,
		Previewable: row.Previewable,
		CreatedAt:   row.CreatedAt,
	}
}
