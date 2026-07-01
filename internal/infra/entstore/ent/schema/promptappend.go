package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PromptAppend struct {
	ent.Schema
}

func (PromptAppend) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.Text("body").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (PromptAppend) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "created_at"),
	}
}
