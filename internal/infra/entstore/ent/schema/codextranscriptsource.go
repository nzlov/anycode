package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type CodexTranscriptSource struct {
	ent.Schema
}

func (CodexTranscriptSource) Fields() []ent.Field {
	return []ent.Field{
		field.String("codex_session_id").Immutable().Unique(),
		field.String("relative_path").NotEmpty(),
		field.Time("bound_at").Default(time.Now).Immutable(),
	}
}
