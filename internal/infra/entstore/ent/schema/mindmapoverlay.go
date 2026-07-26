package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MindMapOverlay struct {
	ent.Schema
}

func (MindMapOverlay) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("project_id").NotEmpty(),
		field.JSON("changes", []map[string]any{}).Default([]map[string]any{}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (MindMapOverlay) Indexes() []ent.Index {
	return []ent.Index{index.Fields("project_id", "updated_at")}
}
