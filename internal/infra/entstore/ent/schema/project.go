package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Project struct {
	ent.Schema
}

func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("name").NotEmpty(),
		field.String("path").NotEmpty(),
		field.Bool("is_git").Default(false),
		field.String("worktree_init_command").Default(""),
		field.String("default_workflow_id").Optional().Nillable(),
		field.Time("removed_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Project) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("path").Unique(),
	}
}
