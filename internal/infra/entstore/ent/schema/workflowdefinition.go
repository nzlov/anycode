package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type WorkflowDefinition struct {
	ent.Schema
}

func (WorkflowDefinition) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("project_id").NotEmpty(),
		field.String("name").NotEmpty(),
		field.Int("version").Default(1),
		field.JSON("graph", map[string]any{}).Default(map[string]any{}),
		field.Bool("active").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (WorkflowDefinition) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "active"),
	}
}
