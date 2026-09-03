package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

type SystemConfiguration struct {
	ent.Schema
}

func (SystemConfiguration) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.Int("agent_max_concurrent").Default(2),
		field.JSON("agent_writable_roots", []string{}).
			Default([]string{}).
			Annotations(entsql.DefaultExpr("'[]'")),
		field.String("send_shortcut").NotEmpty().Default("shift_enter"),
		field.Int("codex_context_window").Default(0),
		field.Int("codex_auto_compact_token_limit").Default(0),
		field.Bool("mind_map_enabled").Default(false),
		field.String("mind_map_mode").NotEmpty().Default("realtime"),
		field.String("mind_map_layout").NotEmpty().Default("radial"),
		field.String("mind_map_model").Default(""),
		field.String("mind_map_reasoning_effort").Default(""),
		field.Int("mind_map_max_concurrent").Default(1),
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
