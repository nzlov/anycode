package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MindMapTask struct {
	ent.Schema
}

func (MindMapTask) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("project_id").NotEmpty(),
		field.String("session_id").NotEmpty(),
		field.String("status").NotEmpty(),
		field.String("process_run_id").Default(""),
		field.Int("attempts").Default(0),
		field.Text("error").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (MindMapTask) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id").Unique(),
		index.Fields("status", "created_at"),
		index.Fields("project_id", "created_at"),
	}
}
