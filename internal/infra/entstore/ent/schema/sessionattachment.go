package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SessionAttachment struct {
	ent.Schema
}

func (SessionAttachment) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.String("role").Default("input"),
		field.String("source_type").NotEmpty(),
		field.String("source_id").NotEmpty(),
		field.String("source_key").Default(""),
		field.String("kind").Default("file"),
		field.String("artifact_kind").Default(""),
		field.String("logical_path").Default(""),
		field.Time("source_modified_at").Optional().Nillable(),
		field.String("filename").NotEmpty(),
		field.String("path").NotEmpty(),
		field.String("mime_type").Default("application/octet-stream"),
		field.Int64("size").Default(0),
		field.String("sha256").Default(""),
		field.Bool("previewable").Default(false),
		field.String("preview_kind").Default("none"),
		field.String("process_run_id").Default(""),
		field.String("node_run_id").Default(""),
		field.String("correlation_id").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("deleted_at").Optional().Nillable(),
	}
}

func (SessionAttachment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id"),
		index.Fields("session_id", "created_at"),
		index.Fields("session_id", "source_type", "source_id"),
		index.Fields("session_id", "role", "deleted_at", "created_at"),
	}
}
