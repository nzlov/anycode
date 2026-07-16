package entstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	domainsession "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entsessionattachment "github.com/nzlov/anycode/internal/infra/entstore/ent/sessionattachment"
)

type AttachmentRepository struct {
	client *ent.Client
	db     *sql.DB
}

func NewAttachmentRepository(client *ent.Client, db ...*sql.DB) *AttachmentRepository {
	repository := &AttachmentRepository{client: client}
	if len(db) > 0 {
		repository.db = db[0]
	}
	return repository
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

func (r *AttachmentRepository) SaveSessionAttachment(ctx context.Context, attachment domainsession.SessionFile) error {
	if attachment.Role == domainsession.FileRoleArtifact {
		return r.saveArtifact(ctx, attachment)
	}
	kind := attachment.Kind
	if kind == "" {
		kind = "file"
	}
	create := r.client.SessionAttachment.Create().
		SetID(string(attachment.ID)).
		SetSessionID(string(attachment.SessionID)).
		SetRole(string(domainsession.FileRoleInput)).
		SetSourceType(string(attachment.SourceType)).
		SetSourceID(attachment.SourceID).
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

func (r *AttachmentRepository) FindSessionAttachment(ctx context.Context, id domainsession.SessionFileID) (domainsession.SessionFile, error) {
	if r.db != nil {
		row := r.db.QueryRowContext(ctx, sessionFileSelect+` WHERE id = ?`, string(id))
		attachment, err := scanSessionFile(row)
		if err != nil {
			return domainsession.SessionFile{}, fmt.Errorf("find session attachment: %w", err)
		}
		return attachment, nil
	}
	row, err := r.client.SessionAttachment.Get(ctx, string(id))
	if err != nil {
		return domainsession.SessionFile{}, fmt.Errorf("find session attachment: %w", err)
	}
	return toDomainSessionAttachment(row), nil
}

func (r *AttachmentRepository) ListSessionAttachments(ctx context.Context, sessionID domainsession.ID) ([]domainsession.SessionFile, error) {
	rows, err := r.client.SessionAttachment.Query().
		Where(
			entsessionattachment.SessionIDEQ(string(sessionID)),
			entsessionattachment.RoleEQ(string(domainsession.FileRoleInput)),
			entsessionattachment.DeletedAtIsNil(),
		).
		Order(ent.Asc(entsessionattachment.FieldCreatedAt), ent.Asc(entsessionattachment.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list session attachments: %w", err)
	}
	attachments := make([]domainsession.SessionFile, 0, len(rows))
	for _, row := range rows {
		attachments = append(attachments, toDomainSessionAttachment(row))
	}
	return attachments, nil
}

const sessionFileSelect = `SELECT id, session_id, role, source_type, source_id, source_key, kind,
	artifact_kind, logical_path, source_modified_at, filename, path, mime_type, size, sha256,
	previewable, preview_kind, process_run_id, node_run_id, correlation_id, created_at, deleted_at
	FROM session_attachments`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSessionFile(row rowScanner) (domainsession.SessionFile, error) {
	var attachment domainsession.SessionFile
	var id, sessionID, role, sourceType string
	var artifactKind, previewKind string
	var sourceModifiedAt, deletedAt sql.NullTime
	err := row.Scan(
		&id, &sessionID, &role, &sourceType, &attachment.SourceID, &attachment.SourceKey, &attachment.Kind,
		&artifactKind, &attachment.LogicalPath, &sourceModifiedAt, &attachment.Filename, &attachment.Path,
		&attachment.MimeType, &attachment.Size, &attachment.SHA256, &attachment.Previewable, &previewKind,
		&attachment.ProcessRunID, &attachment.NodeRunID, &attachment.CorrelationID, &attachment.CreatedAt, &deletedAt,
	)
	if err != nil {
		return domainsession.SessionFile{}, err
	}
	attachment.ID = domainsession.SessionFileID(id)
	attachment.SessionID = domainsession.ID(sessionID)
	attachment.Role = domainsession.FileRole(role)
	attachment.SourceType = domainsession.AttachmentSourceType(sourceType)
	attachment.ArtifactKind = domainsession.ArtifactKind(artifactKind)
	attachment.PreviewKind = domainsession.PreviewKind(previewKind)
	if sourceModifiedAt.Valid {
		attachment.SourceModifiedAt = &sourceModifiedAt.Time
	}
	if deletedAt.Valid {
		attachment.DeletedAt = &deletedAt.Time
	}
	return attachment, nil
}

func (r *AttachmentRepository) saveArtifact(ctx context.Context, attachment domainsession.SessionFile) error {
	if r.db == nil {
		return fmt.Errorf("save session artifact: database connection unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session artifact transaction: %w", err)
	}
	defer tx.Rollback()
	if err := insertArtifact(ctx, tx, attachment); err != nil {
		return err
	}
	if err := refreshSessionArtifactCount(ctx, tx, attachment.SessionID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session artifact transaction: %w", err)
	}
	return nil
}

func (r *AttachmentRepository) SaveArtifactWithEvent(ctx context.Context, artifact domainsession.SessionFile, event eventdomain.DomainEvent) error {
	if r.db == nil {
		return fmt.Errorf("save session artifact transaction: database connection unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session artifact transaction: %w", err)
	}
	defer tx.Rollback()
	if err := insertArtifact(ctx, tx, artifact); err != nil {
		return err
	}
	if err := refreshSessionArtifactCount(ctx, tx, artifact.SessionID); err != nil {
		return err
	}
	if err := insertArtifactEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session artifact transaction: %w", err)
	}
	return nil
}

func (r *AttachmentRepository) DeleteArtifactWithEvent(ctx context.Context, artifact domainsession.SessionFile, event eventdomain.DomainEvent) error {
	if r.db == nil || artifact.DeletedAt == nil {
		return fmt.Errorf("delete session artifact transaction: invalid input")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete session artifact transaction: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE session_attachments SET deleted_at = ? WHERE id = ? AND role = 'artifact' AND deleted_at IS NULL`, *artifact.DeletedAt, string(artifact.ID))
	if err != nil {
		return fmt.Errorf("delete session artifact: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete session artifact rows: %w", err)
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	if err := refreshSessionArtifactCount(ctx, tx, artifact.SessionID); err != nil {
		return err
	}
	if err := insertArtifactEvent(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete session artifact transaction: %w", err)
	}
	return nil
}

type artifactExecQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func refreshSessionArtifactCount(ctx context.Context, query artifactExecQuerier, sessionID domainsession.ID) error {
	var count int
	if err := query.QueryRowContext(ctx, `SELECT COUNT(DISTINCT logical_path) FROM session_attachments WHERE session_id = ? AND role = 'artifact' AND deleted_at IS NULL`, string(sessionID)).Scan(&count); err != nil {
		return fmt.Errorf("count current session artifacts: %w", err)
	}
	result, err := query.ExecContext(ctx, `UPDATE sessions SET artifact_count = ? WHERE id = ?`, count, string(sessionID))
	if err != nil {
		return fmt.Errorf("update session artifact count: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session artifact count rows: %w", err)
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func insertArtifact(ctx context.Context, executor artifactExecQuerier, attachment domainsession.SessionFile) error {
	attachment = normalizeArtifact(attachment)
	_, err := executor.ExecContext(ctx, `INSERT INTO session_attachments (
		id, session_id, role, source_type, source_id, source_key, kind, artifact_kind, logical_path,
		source_modified_at, filename, path, mime_type, size, sha256, previewable, preview_kind,
		process_run_id, node_run_id, correlation_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(attachment.ID), string(attachment.SessionID), string(attachment.Role), string(attachment.SourceType),
		attachment.SourceID, attachment.SourceKey, attachment.Kind, string(attachment.ArtifactKind), attachment.LogicalPath,
		attachment.SourceModifiedAt, attachment.Filename, attachment.Path, attachment.MimeType, attachment.Size,
		attachment.SHA256, attachment.Previewable, string(attachment.PreviewKind), attachment.ProcessRunID,
		attachment.NodeRunID, attachment.CorrelationID, attachment.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("save session artifact: %w", err)
	}
	return nil
}

func insertArtifactEvent(ctx context.Context, executor artifactExecQuerier, event eventdomain.DomainEvent) error {
	payload, err := json.Marshal(payloadOrEmpty(event.Payload))
	if err != nil {
		return fmt.Errorf("marshal session artifact event: %w", err)
	}
	var sessionID any
	if event.SessionID != nil {
		sessionID = string(*event.SessionID)
	} else if event.Scope.SessionID != nil {
		sessionID = string(*event.Scope.SessionID)
	}
	result, err := executor.ExecContext(ctx, `INSERT INTO event_records (id, session_id, project_id, type, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`, string(event.ID), sessionID, event.Scope.ProjectID, event.Type, string(payload), event.CreatedAt)
	if err != nil {
		return fmt.Errorf("save session artifact event: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("save session artifact event rows: %w", err)
	}
	if changed == 0 {
		var existingType string
		if err := executor.QueryRowContext(ctx, `SELECT type FROM event_records WHERE id = ?`, string(event.ID)).Scan(&existingType); err != nil {
			return fmt.Errorf("find conflicting session artifact event: %w", err)
		}
		if existingType != event.Type {
			return fmt.Errorf("session artifact event id %s conflicts with type %s", event.ID, existingType)
		}
	}
	return nil
}

func normalizeArtifact(attachment domainsession.SessionFile) domainsession.SessionFile {
	if attachment.Kind == "" {
		attachment.Kind = "file"
	}
	if attachment.ArtifactKind == "" {
		attachment.ArtifactKind = domainsession.ArtifactKindFile
	}
	if attachment.PreviewKind == "" {
		attachment.PreviewKind = domainsession.PreviewKindNone
	}
	if attachment.MimeType == "" {
		attachment.MimeType = "application/octet-stream"
	}
	if attachment.CreatedAt.IsZero() {
		attachment.CreatedAt = time.Now().UTC()
	}
	return attachment
}

func (r *AttachmentRepository) FindArtifactBySourceKey(ctx context.Context, sessionID domainsession.ID, sourceKey string) (domainsession.SessionFile, bool, error) {
	if r.db == nil {
		return domainsession.SessionFile{}, false, fmt.Errorf("find session artifact: database connection unavailable")
	}
	row := r.db.QueryRowContext(ctx, sessionFileSelect+` WHERE session_id = ? AND role = 'artifact' AND source_key = ?`, string(sessionID), sourceKey)
	attachment, err := scanSessionFile(row)
	if err == sql.ErrNoRows {
		return domainsession.SessionFile{}, false, nil
	}
	if err != nil {
		return domainsession.SessionFile{}, false, fmt.Errorf("find session artifact: %w", err)
	}
	return attachment, true, nil
}

func (r *AttachmentRepository) ListSessionArtifacts(ctx context.Context, query domainsession.ArtifactQuery) ([]domainsession.SessionFile, error) {
	if r.db == nil {
		return nil, fmt.Errorf("list session artifacts: database connection unavailable")
	}
	where := []string{"current.session_id = ?"}
	args := []any{string(query.SessionID)}
	if query.Kind != "" {
		where = append(where, "current.artifact_kind = ?")
		args = append(args, string(query.Kind))
	}
	if query.Source != "" {
		where = append(where, "current.source_type = ?")
		args = append(args, string(query.Source))
	}
	if filter := strings.TrimSpace(query.Filter); filter != "" {
		where = append(where, "(LOWER(current.filename) LIKE ? OR LOWER(current.logical_path) LIKE ?)")
		pattern := "%" + strings.ToLower(filter) + "%"
		args = append(args, pattern, pattern)
	}
	clause := strings.Join(where, " AND ")
	order := "current.created_at DESC, current.id DESC"
	switch query.Sort {
	case "created_at_asc":
		order = "current.created_at ASC, current.id ASC"
	case "filename_asc":
		order = "current.filename ASC, current.id ASC"
	case "size_desc":
		order = "current.size DESC, current.id DESC"
	}
	querySQL := strings.Replace(sessionFileSelect, "FROM session_attachments", "FROM session_attachments AS current", 1) + ` WHERE ` + clause + `
		AND current.role = 'artifact'
		AND current.deleted_at IS NULL
		AND current.id = (
			SELECT latest.id FROM session_attachments AS latest
			WHERE latest.session_id = current.session_id
				AND latest.role = 'artifact'
				AND latest.deleted_at IS NULL
				AND latest.logical_path = current.logical_path
			ORDER BY latest.created_at DESC, latest.id DESC
			LIMIT 1
		) ORDER BY ` + order
	rows, err := r.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("list session artifacts: %w", err)
	}
	defer rows.Close()
	artifacts := make([]domainsession.SessionFile, 0)
	for rows.Next() {
		artifact, err := scanSessionFile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session artifact: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list session artifacts: %w", err)
	}
	return artifacts, nil
}

func (r *AttachmentRepository) ResolveLatestSessionArtifactsByLogicalPaths(ctx context.Context, sessionID domainsession.ID, logicalPaths []string) ([]domainsession.SessionFile, error) {
	if r.db == nil {
		return nil, fmt.Errorf("resolve session artifacts: database connection unavailable")
	}
	if len(logicalPaths) == 0 {
		return []domainsession.SessionFile{}, nil
	}
	placeholders := make([]string, len(logicalPaths))
	args := make([]any, 0, len(logicalPaths)+1)
	args = append(args, string(sessionID))
	for index, logicalPath := range logicalPaths {
		placeholders[index] = "?"
		args = append(args, logicalPath)
	}
	rows, err := r.db.QueryContext(ctx, sessionFileSelect+` WHERE session_id = ? AND role = 'artifact' AND deleted_at IS NULL AND logical_path IN (`+strings.Join(placeholders, ",")+`) AND id = (
		SELECT latest.id FROM session_attachments AS latest
		WHERE latest.session_id = session_attachments.session_id
			AND latest.role = 'artifact'
			AND latest.deleted_at IS NULL
			AND latest.logical_path = session_attachments.logical_path
		ORDER BY latest.created_at DESC, latest.id DESC
		LIMIT 1
	) ORDER BY logical_path ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("resolve session artifacts: %w", err)
	}
	defer rows.Close()
	resolved := make([]domainsession.SessionFile, 0, len(logicalPaths))
	for rows.Next() {
		artifact, err := scanSessionFile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan resolved session artifact: %w", err)
		}
		resolved = append(resolved, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve session artifacts rows: %w", err)
	}
	return resolved, nil
}

func (r *AttachmentRepository) SumSessionArtifactSize(ctx context.Context, sessionID domainsession.ID) (int64, error) {
	if r.db == nil {
		return 0, fmt.Errorf("sum session artifacts: database connection unavailable")
	}
	var size int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0) FROM session_attachments WHERE session_id = ? AND role = 'artifact' AND deleted_at IS NULL`, string(sessionID)).Scan(&size); err != nil {
		return 0, fmt.Errorf("sum session artifacts: %w", err)
	}
	return size, nil
}

func (r *AttachmentRepository) SoftDeleteArtifact(ctx context.Context, id domainsession.SessionFileID, deletedAt time.Time) (domainsession.SessionFile, error) {
	if r.db == nil {
		return domainsession.SessionFile{}, fmt.Errorf("delete session artifact: database connection unavailable")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domainsession.SessionFile{}, fmt.Errorf("begin delete session artifact transaction: %w", err)
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, sessionFileSelect+` WHERE id = ?`, string(id))
	artifact, err := scanSessionFile(row)
	if err != nil {
		return domainsession.SessionFile{}, fmt.Errorf("find session artifact for delete: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE session_attachments SET deleted_at = ? WHERE id = ? AND role = 'artifact' AND deleted_at IS NULL`, deletedAt, string(id))
	if err != nil {
		return domainsession.SessionFile{}, fmt.Errorf("delete session artifact: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return domainsession.SessionFile{}, fmt.Errorf("delete session artifact: %w", err)
	}
	if changed == 0 {
		return domainsession.SessionFile{}, sql.ErrNoRows
	}
	if err := refreshSessionArtifactCount(ctx, tx, artifact.SessionID); err != nil {
		return domainsession.SessionFile{}, err
	}
	if err := tx.Commit(); err != nil {
		return domainsession.SessionFile{}, fmt.Errorf("commit delete session artifact transaction: %w", err)
	}
	artifact.DeletedAt = &deletedAt
	return artifact, nil
}

func (r *AttachmentRepository) ListArtifactsPendingPhysicalDelete(ctx context.Context, limit int) ([]domainsession.SessionFile, error) {
	if r.db == nil {
		return nil, fmt.Errorf("list pending artifact deletions: database connection unavailable")
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, sessionFileSelect+` WHERE role = 'artifact' AND deleted_at IS NOT NULL AND path <> '' ORDER BY deleted_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending artifact deletions: %w", err)
	}
	defer rows.Close()
	artifacts := make([]domainsession.SessionFile, 0)
	for rows.Next() {
		artifact, err := scanSessionFile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending artifact deletion: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending artifact deletions: %w", err)
	}
	return artifacts, nil
}

func (r *AttachmentRepository) MarkArtifactPhysicalDeleted(ctx context.Context, id domainsession.SessionFileID) error {
	if r.db == nil {
		return fmt.Errorf("confirm artifact deletion: database connection unavailable")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE session_attachments SET path = '' WHERE id = ? AND role = 'artifact' AND deleted_at IS NOT NULL`, string(id))
	if err != nil {
		return fmt.Errorf("confirm artifact deletion: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm artifact deletion rows: %w", err)
	}
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *AttachmentRepository) ListPromptAppendAttachments(ctx context.Context, sessionID domainsession.ID, appendID string) ([]domainsession.SessionFile, error) {
	rows, err := r.client.SessionAttachment.Query().
		Where(
			entsessionattachment.SessionIDEQ(string(sessionID)),
			entsessionattachment.SourceTypeEQ(string(domainsession.AttachmentSourcePromptAppend)),
			entsessionattachment.SourceIDEQ(appendID),
		).
		Order(ent.Asc(entsessionattachment.FieldCreatedAt), ent.Asc(entsessionattachment.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list prompt append attachments: %w", err)
	}
	attachments := make([]domainsession.SessionFile, 0, len(rows))
	for _, row := range rows {
		attachments = append(attachments, toDomainSessionAttachment(row))
	}
	return attachments, nil
}

func (r *AttachmentRepository) DeleteSessionAttachment(ctx context.Context, id domainsession.SessionFileID) error {
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

func toDomainSessionAttachment(row *ent.SessionAttachment) domainsession.SessionFile {
	return domainsession.SessionFile{
		ID:               domainsession.SessionFileID(row.ID),
		SessionID:        domainsession.ID(row.SessionID),
		Role:             domainsession.FileRole(row.Role),
		SourceType:       domainsession.AttachmentSourceType(row.SourceType),
		SourceID:         row.SourceID,
		SourceKey:        row.SourceKey,
		Kind:             row.Kind,
		ArtifactKind:     domainsession.ArtifactKind(row.ArtifactKind),
		LogicalPath:      row.LogicalPath,
		SourceModifiedAt: row.SourceModifiedAt,
		Filename:         row.Filename,
		Path:             row.Path,
		MimeType:         row.MimeType,
		Size:             row.Size,
		SHA256:           row.Sha256,
		Previewable:      row.Previewable,
		PreviewKind:      domainsession.PreviewKind(row.PreviewKind),
		ProcessRunID:     row.ProcessRunID,
		NodeRunID:        row.NodeRunID,
		CorrelationID:    row.CorrelationID,
		CreatedAt:        row.CreatedAt,
		DeletedAt:        row.DeletedAt,
	}
}
