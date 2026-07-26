package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuickCommand struct {
	ent.Schema
}

func (QuickCommand) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("project_id").Optional().Nillable().Immutable(),
		field.String("content").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (QuickCommand) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "created_at"),
	}
}
