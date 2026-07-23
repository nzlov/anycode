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
		field.String("background_type").NotEmpty().Default("bing"),
		field.String("solid_theme").NotEmpty().Default("vermilion"),
		field.Int("background_mask").Default(0),
		field.String("wallpaper_id").Default(""),
		field.String("wallpaper_filename").Default(""),
		field.String("wallpaper_mime_type").Default(""),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
