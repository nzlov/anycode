package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MergeRecord struct {
	ent.Schema
}

func (MergeRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.String("node_run_id").Optional().Nillable(),
		field.String("strategy").Default(""),
		field.String("base_branch").Default(""),
		field.String("worktree_branch").Default(""),
		field.String("base_commit").Default(""),
		field.String("head_commit").Default(""),
		field.String("merge_commit").Default(""),
		field.String("status").Default(""),
		field.String("failure_code").Default(""),
		field.Text("failure_reason").Default(""),
		field.Time("merged_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (MergeRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "created_at"),
	}
}
