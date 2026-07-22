package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type NotificationDelivery struct {
	ent.Schema
}

func (NotificationDelivery) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("event_id").NotEmpty(),
		field.String("subscription_id").NotEmpty(),
		field.Bytes("payload").NotEmpty().Sensitive(),
		field.String("status").NotEmpty(),
		field.Int("attempts").Default(0),
		field.Time("next_attempt_at").Default(time.Now),
		field.String("last_error").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (NotificationDelivery) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("event_id", "subscription_id").Unique(),
		index.Fields("status", "next_attempt_at"),
		index.Fields("subscription_id", "status"),
	}
}
