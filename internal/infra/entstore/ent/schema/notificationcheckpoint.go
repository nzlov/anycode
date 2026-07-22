package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type NotificationCheckpoint struct {
	ent.Schema
}

func (NotificationCheckpoint) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("last_event_id").Default(""),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
