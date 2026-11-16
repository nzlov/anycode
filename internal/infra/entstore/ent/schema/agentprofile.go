package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/nzlov/anycode/internal/domain/agentprofile"
)

type AgentProfile struct{ ent.Schema }

func (AgentProfile) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.JSON("configuration", agentprofile.Configuration{}),
		field.JSON("analysis", agentprofile.Analysis{}),
	}
}
