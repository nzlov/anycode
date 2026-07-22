package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuestionRequest struct {
	ent.Schema
}

func (QuestionRequest) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.String("origin_process_run_id").Default(""),
		field.String("status").NotEmpty(),
		field.JSON("questions", []map[string]any{}).Default([]map[string]any{}),
		field.JSON("answers", []map[string]any{}).Default([]map[string]any{}),
		field.Text("cancel_reason").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("answered_at").Optional().Nillable(),
	}
}

func (QuestionRequest) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "status"),
		index.Fields("session_id", "created_at"),
		index.Fields("origin_process_run_id", "status"),
	}
}
