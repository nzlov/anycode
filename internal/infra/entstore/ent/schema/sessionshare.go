package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SessionShare struct{ ent.Schema }

func (SessionShare) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(), field.String("session_id"),
		field.String("token_hash"), field.String("guest_hash").Default(""),
		field.Time("created_at").Immutable(), field.Time("claimed_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}
func (SessionShare) Indexes() []ent.Index { return []ent.Index{index.Fields("session_id")} }
