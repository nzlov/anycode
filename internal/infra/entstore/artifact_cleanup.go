package entstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/nzlov/anycode/internal/application/port"
	domainsession "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/predicate"
	entsessionattachment "github.com/nzlov/anycode/internal/infra/entstore/ent/sessionattachment"
)

func (t transaction) DeleteSessionArtifacts(ctx context.Context, input port.DeleteSessionArtifactsInput) (port.DeleteSessionArtifactsResult, error) {
	if input.SessionID == "" || input.DeletedAt.IsZero() {
		return port.DeleteSessionArtifactsResult{}, errors.New("delete session artifacts: invalid input")
	}
	predicates := []predicate.SessionAttachment{
		entsessionattachment.SessionIDEQ(string(input.SessionID)),
		entsessionattachment.RoleEQ(string(domainsession.FileRoleArtifact)),
		entsessionattachment.DeletedAtIsNil(),
	}
	rows, err := t.client.SessionAttachment.Query().
		Where(predicates...).
		Order(ent.Asc(entsessionattachment.FieldCreatedAt), ent.Asc(entsessionattachment.FieldID)).
		All(ctx)
	if err != nil {
		return port.DeleteSessionArtifactsResult{}, fmt.Errorf("list session artifacts for delete: %w", err)
	}
	if len(rows) > 0 {
		changed, err := t.client.SessionAttachment.Update().Where(predicates...).SetDeletedAt(input.DeletedAt).Save(ctx)
		if err != nil {
			return port.DeleteSessionArtifactsResult{}, fmt.Errorf("delete session artifacts: %w", err)
		}
		if changed != len(rows) {
			return port.DeleteSessionArtifactsResult{}, fmt.Errorf("delete session artifacts: updated %d of %d rows", changed, len(rows))
		}
	}
	if err := t.client.Session.UpdateOneID(string(input.SessionID)).SetArtifactCount(0).Exec(ctx); err != nil {
		return port.DeleteSessionArtifactsResult{}, fmt.Errorf("reset session artifact count: %w", err)
	}
	artifacts := make([]domainsession.SessionFile, 0, len(rows))
	for _, row := range rows {
		artifact := toDomainSessionAttachment(row)
		artifact.DeletedAt = &input.DeletedAt
		artifacts = append(artifacts, artifact)
	}
	return port.DeleteSessionArtifactsResult{Artifacts: artifacts}, nil
}
