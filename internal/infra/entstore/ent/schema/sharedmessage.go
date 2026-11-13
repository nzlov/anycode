package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/nzlov/anycode/internal/domain/session"
)

type SharedMessage struct{ ent.Schema }

func (SharedMessage) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(), field.String("share_id").Immutable(), field.String("session_id").Immutable(),
		field.JSON("content", session.SharedMessageContent{}).Immutable(),
		field.String("status").Default("pending"), field.Time("created_at").Immutable(),
		field.Int64("approval_order").Default(0),
		field.Time("reviewed_at").Optional().Nillable(),
	}
}
func (SharedMessage) Indexes() []ent.Index {
	return []ent.Index{index.Fields("session_id", "created_at"), index.Fields("share_id")}
}
