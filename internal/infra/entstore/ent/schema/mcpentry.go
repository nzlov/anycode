package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/nzlov/anycode/internal/domain/mcp"
)

type MCPEntry struct{ ent.Schema }

func (MCPEntry) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("scope_kind").Immutable(),
		field.String("scope_id").Immutable(),
		field.String("name").Immutable(),
		field.JSON("definition", &mcp.Definition{}).Optional(),
		field.Bool("enabled").Optional().Nillable(),
	}
}
func (MCPEntry) Indexes() []ent.Index {
	return []ent.Index{index.Fields("scope_kind", "scope_id", "name").Unique()}
}
