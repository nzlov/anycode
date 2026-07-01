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
		field.String("kind").Default("file"),
		field.String("filename").NotEmpty(),
		field.String("path").NotEmpty(),
		field.String("mime_type").Default("application/octet-stream"),
		field.Int64("size").Default(0),
		field.Bool("previewable").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (SessionAttachment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id"),
		index.Fields("session_id", "created_at"),
	}
}
