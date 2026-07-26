package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type MindMapGraph struct {
	ent.Schema
}

func (MindMapGraph) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.JSON("nodes", []map[string]any{}).Default([]map[string]any{}),
		field.JSON("edges", []map[string]any{}).Default([]map[string]any{}),
		field.JSON("history", []map[string]any{}).Default([]map[string]any{}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
