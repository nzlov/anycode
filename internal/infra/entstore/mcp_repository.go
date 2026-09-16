package entstore

import (
	"context"
	"crypto/sha256"
	"fmt"

	domain "github.com/nzlov/anycode/internal/domain/mcp"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	row "github.com/nzlov/anycode/internal/infra/entstore/ent/mcpentry"
)

type MCPRepository struct{ client *ent.Client }

func (s *Store) MCP() *MCPRepository { return &MCPRepository{client: s.client} }
func entryID(scope domain.Scope, name string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(scope.Kind+"\x00"+scope.ID+"\x00"+name)))
}
func (r *MCPRepository) List(ctx context.Context, scope domain.Scope) ([]domain.Entry, error) {
	rows, err := r.client.MCPEntry.Query().Where(row.ScopeKindEQ(scope.Kind), row.ScopeIDEQ(scope.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]domain.Entry, 0, len(rows))
	for _, v := range rows {
		result = append(result, domain.Entry{Scope: scope, Name: v.Name, Definition: v.Definition, Enabled: v.Enabled})
	}
	return result, nil
}
func (r *MCPRepository) Save(ctx context.Context, e domain.Entry) error {
	id := entryID(e.Scope, e.Name)
	update := func() error {
		q := r.client.MCPEntry.UpdateOneID(id)
		if e.Definition != nil {
			q.SetDefinition(e.Definition)
		} else {
			q.ClearDefinition()
		}
		if e.Enabled != nil {
			q.SetEnabled(*e.Enabled)
		} else {
			q.ClearEnabled()
		}
		return q.Exec(ctx)
	}
	if err := update(); !ent.IsNotFound(err) {
		return err
	}
	q := r.client.MCPEntry.Create().SetID(id).SetScopeKind(e.Scope.Kind).SetScopeID(e.Scope.ID).SetName(e.Name)
	if e.Definition != nil {
		q.SetDefinition(e.Definition)
	}
	if e.Enabled != nil {
		q.SetEnabled(*e.Enabled)
	}
	if err := q.Exec(ctx); ent.IsConstraintError(err) {
		return update()
	} else {
		return err
	}
}
func (r *MCPRepository) Delete(ctx context.Context, scope domain.Scope, name string) error {
	err := r.client.MCPEntry.DeleteOneID(entryID(scope, name)).Exec(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	return err
}
