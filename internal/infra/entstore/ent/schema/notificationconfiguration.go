package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type NotificationConfiguration struct {
	ent.Schema
}

func (NotificationConfiguration) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("vapid_public_key").NotEmpty(),
		field.String("vapid_private_key").NotEmpty().Sensitive(),
		field.String("vapid_subject").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
