package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type QuestionBatch struct {
	ent.Schema
}

func (QuestionBatch) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.String("workflow_run_id").Optional().Nillable(),
		field.String("status").NotEmpty(),
		field.JSON("questions", []map[string]any{}).Default([]map[string]any{}),
		field.JSON("answers", []map[string]any{}).Default([]map[string]any{}),
		field.Text("cancel_reason").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("answered_at").Optional().Nillable(),
	}
}

func (QuestionBatch) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "status"),
		index.Fields("session_id", "created_at"),
	}
}
