package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SessionSideEvent struct {
	ent.Schema
}

func (SessionSideEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("side_id").NotEmpty(),
		field.String("session_id").NotEmpty(),
		field.String("process_run_id").NotEmpty(),
		field.String("event_id").NotEmpty(),
		field.String("type").NotEmpty(),
		field.String("correlation_id").Default(""),
		field.String("turn_id").Default(""),
		field.String("phase").Default(""),
		field.String("content_kind").NotEmpty(),
		field.JSON("content", map[string]any{}).Default(map[string]any{}),
		field.Int("turn_index").Positive(),
		field.Int64("sequence").Positive(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (SessionSideEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("side_id", "turn_index", "sequence"),
		index.Fields("process_run_id", "sequence"),
		index.Fields("session_id"),
	}
}
