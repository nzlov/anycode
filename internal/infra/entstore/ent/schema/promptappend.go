package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	domainsession "github.com/nzlov/anycode/internal/domain/session"
)

type PromptAppend struct {
	ent.Schema
}

func (PromptAppend) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("session_id").NotEmpty(),
		field.Text("body").Default(""),
		field.JSON("mentions", []domainsession.PromptMention{}).Optional(),
		field.JSON("artifact_ids", []string{}).Default([]string{}),
		field.String("status").Default(string(domainsession.PromptAppendPending)),
		field.Time("dispatched_at").Optional().Nillable(),
		field.String("dispatched_process_run_id").Default(""),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (PromptAppend) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("session_id", "created_at"),
		index.Fields("session_id", "status", "created_at"),
	}
}
