package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type StagedAttachment struct {
	ent.Schema
}

func (StagedAttachment) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("owner_key_hash").Default(""),
		field.String("filename").NotEmpty(),
		field.String("path").NotEmpty(),
		field.String("mime_type").Default("application/octet-stream"),
		field.Int64("size").Default(0),
		field.Bool("previewable").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (StagedAttachment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("owner_key_hash"),
		index.Fields("created_at"),
	}
}
