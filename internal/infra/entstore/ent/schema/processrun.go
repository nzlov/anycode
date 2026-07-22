package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ProcessRun struct {
	ent.Schema
}

func (ProcessRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.String("node_run_id").Optional().Nillable(),
		field.String("status").NotEmpty(),
		field.String("codex_session_id").Default(""),
		field.String("resume_of").Optional().Nillable(),
		field.Int("exit_code").Optional().Nillable(),
		field.Text("failure_reason").Default(""),
		field.Time("started_at").Default(time.Now).Immutable(),
		field.Time("finished_at").Optional().Nillable(),
	}
}

func (ProcessRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "status"),
		index.Fields("session_id", "started_at"),
		index.Fields("session_id", "codex_session_id"),
	}
}
