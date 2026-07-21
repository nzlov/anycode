package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type PushSubscription struct {
	ent.Schema
}

func (PushSubscription) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("principal_key_hash").NotEmpty().Sensitive(),
		field.String("endpoint_hash").NotEmpty().Sensitive(),
		field.String("endpoint").NotEmpty().Sensitive(),
		field.String("p256dh").NotEmpty().Sensitive(),
		field.String("auth").NotEmpty().Sensitive(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PushSubscription) Indexes() []ent.Index {
	return []ent.Index{index.Fields("endpoint_hash").Unique()}
}
