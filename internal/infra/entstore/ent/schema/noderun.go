package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type NodeRun struct {
	ent.Schema
}

func (NodeRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("workflow_run_id").NotEmpty(),
		field.String("node_id").NotEmpty(),
		field.String("status").NotEmpty(),
		field.Int("attempt").Default(1),
		field.String("process_run_id").Optional().Nillable(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.JSON("output", map[string]any{}).Default(map[string]any{}),
	}
}

func (NodeRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("workflow_run_id", "node_id"),
		index.Fields("workflow_run_id", "status"),
	}
}
