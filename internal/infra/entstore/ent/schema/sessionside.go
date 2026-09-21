package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type SessionSide struct {
	ent.Schema
}

func (SessionSide) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.String("process_run_id").NotEmpty(),
		field.String("turn_id").Default(""),
		field.Text("prompt").NotEmpty(),
		field.JSON("follow_ups", []string{}).Default([]string{}),
		field.String("status").NotEmpty(),
		field.Text("error").Default(""),
		field.Int("turn_index").Default(1).Positive(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (SessionSide) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "created_at"),
		index.Fields("process_run_id").Unique(),
	}
}
