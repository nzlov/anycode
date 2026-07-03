package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ProcessEvent struct {
	ent.Schema
}

func (ProcessEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.String("process_run_id").Optional().Nillable(),
		field.String("event_id").Default(""),
		field.String("type").NotEmpty(),
		field.JSON("payload", map[string]any{}).Default(map[string]any{}),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (ProcessEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "created_at", "id"),
		index.Fields("process_run_id", "created_at", "id"),
		index.Fields("event_id"),
	}
}
