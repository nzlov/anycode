package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type MindMapNode struct {
	ent.Schema
}

type MindMapNodeFile struct {
	File      string `json:"file"`
	Method    string `json:"method"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
}

func (MindMapNode) Fields() []ent.Field {
	return []ent.Field{
		field.String("project_id").NotEmpty(),
		field.String("session_id").Default(""),
		field.String("node_id").NotEmpty(),
		field.String("title").Optional().Nillable(),
		field.String("content").Optional().Nillable(),
		field.JSON("files", []MindMapNodeFile{}).Optional(),
		field.Time("title_updated_at").Optional().Nillable(),
		field.Time("content_updated_at").Optional().Nillable(),
		field.Time("files_updated_at").Optional().Nillable(),
		field.Time("deleted_at").Optional().Nillable(),
	}
}

func (MindMapNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "session_id", "node_id").Unique(),
		index.Fields("session_id"),
	}
}
