package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type EventRecord struct {
	ent.Schema
}

func (EventRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").Optional().Nillable(),
		field.String("project_id").NotEmpty(),
		field.String("type").NotEmpty(),
		field.JSON("payload", map[string]any{}).Default(map[string]any{}),
		field.String("process_run_id").Default(""),
		field.String("node_run_id").Default(""),
		field.String("correlation_id").Default(""),
		field.String("session_status").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (EventRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "created_at", "id"),
		index.Fields("session_id", "created_at", "id"),
		index.Fields("created_at", "id"),
	}
}
