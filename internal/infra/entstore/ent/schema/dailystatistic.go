package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DailyStatistic struct {
	ent.Schema
}

func (DailyStatistic) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty().Immutable(),
		field.String("project_id").NotEmpty().Immutable(),
		field.String("project_name").NotEmpty(),
		field.String("day").NotEmpty().Immutable(),
		field.String("month").NotEmpty().Immutable(),
		field.Int("created_cards").Default(0),
		field.Int("closed_cards").Default(0),
		field.Int("files_changed").Default(0),
		field.Int64("total_tokens").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (DailyStatistic) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("day"),
		index.Fields("month"),
		index.Fields("project_id"),
	}
}
