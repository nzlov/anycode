package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type SystemConfiguration struct {
	ent.Schema
}

func (SystemConfiguration) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("wallpaper_color_scheme").NotEmpty(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
