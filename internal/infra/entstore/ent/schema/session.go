package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Session struct {
	ent.Schema
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("project_id").NotEmpty(),
		field.String("requirement").Default(""),
		field.String("mode").NotEmpty(),
		field.String("status").NotEmpty(),
		field.String("close_reason").Optional().Nillable(),
		field.String("base_branch").Default(""),
		field.String("worktree_path").Default(""),
		field.String("codex_session_id").Default(""),
		field.String("codex_model").Default(""),
		field.String("reasoning_effort").Default(""),
		field.String("permission_mode").Default(""),
		field.Time("last_run_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("closed_at").Optional().Nillable(),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id"),
		index.Fields("project_id", "updated_at"),
		index.Fields("project_id", "last_run_at"),
		index.Fields("status"),
	}
}
