package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MindMapEdge struct {
	ent.Schema
}

func (MindMapEdge) Fields() []ent.Field {
	return []ent.Field{
		field.String("project_id").NotEmpty(),
		field.String("session_id").Default(""),
		field.String("edge_id").NotEmpty(),
		field.String("source_id").Optional().Nillable(),
		field.String("target_id").Optional().Nillable(),
		field.String("label").Optional().Nillable(),
		field.Time("source_updated_at").Optional().Nillable(),
		field.Time("target_updated_at").Optional().Nillable(),
		field.Time("label_updated_at").Optional().Nillable(),
		field.Time("deleted_at").Optional().Nillable(),
	}
}

func (MindMapEdge) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "session_id", "edge_id").Unique(),
		index.Fields("session_id"),
	}
}
