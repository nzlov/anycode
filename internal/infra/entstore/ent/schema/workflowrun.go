package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type WorkflowRun struct {
	ent.Schema
}

func (WorkflowRun) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.String("workflow_definition_id").NotEmpty(),
		field.String("status").NotEmpty(),
		field.String("current_node_id").Default(""),
		field.JSON("context", map[string]any{}).Default(map[string]any{}),
		field.JSON("pending_approval", map[string]any{}).Default(map[string]any{}),
		field.Time("started_at").Optional().Nillable(),
		field.Time("stopped_at").Optional().Nillable(),
	}
}

func (WorkflowRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id"),
		index.Fields("workflow_definition_id", "status"),
	}
}
