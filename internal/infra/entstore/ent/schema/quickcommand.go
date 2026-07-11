package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type QuickCommand struct {
	ent.Schema
}

func (QuickCommand) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("content").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
