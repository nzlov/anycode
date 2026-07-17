package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	domainsession "github.com/nzlov/anycode/internal/domain/session"
)

type Session struct {
	ent.Schema
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("project_id").NotEmpty(),
		field.String("requirement").Default(""),
		field.String("mode").NotEmpty(),
		field.String("status").NotEmpty(),
		field.String("priority").Default("medium"),
		field.String("close_reason").Optional().Nillable(),
		field.String("base_branch").Default(""),
		field.String("worktree_path").Default(""),
		field.String("worktree_branch").Default(""),
		field.String("worktree_base_commit").Default(""),
		field.String("worktree_cleanup_status").Default(string(domainsession.WorktreeCleanupNotApplicable)),
		field.Int("worktree_cleanup_attempts").Default(0),
		field.String("worktree_ownership_token").Default(""),
		field.Time("worktree_ownership_confirmed_at").Optional().Nillable(),
		field.Time("worktree_cleanup_requested_at").Optional().Nillable(),
		field.Time("worktree_cleanup_last_at").Optional().Nillable(),
		field.Time("worktree_cleanup_next_at").Optional().Nillable(),
		field.Time("worktree_cleanup_completed_at").Optional().Nillable(),
		field.String("worktree_cleanup_error_code").Default(""),
		field.String("worktree_cleanup_error").Default(""),
		field.Bool("worktree_cleanup_retryable").Default(false),
		field.String("codex_session_id").Default(""),
		field.String("codex_model").Default(""),
		field.String("reasoning_effort").Default(""),
		field.String("permission_mode").Default(""),
		field.Bool("fast_mode").Default(false),
		field.JSON("todo_list", domainsession.TodoList{}).Optional(),
		field.Int("artifact_count").Default(0).NonNegative(),
		field.Int("files_changed").Default(0).NonNegative(),
		field.Time("queued_at").Optional().Nillable(),
		field.String("queue_kind").Default(""),
		field.String("queue_priority").Default("medium"),
		field.Bool("queue_initial_start").Optional().Nillable(),
		field.Bool("queue_review_after_reuse_failure").Default(false),
		field.String("queue_node_run_id").Default(""),
		field.String("queue_prompt").Default(""),
		field.String("queue_resume_codex_session_id").Default(""),
		field.String("queue_resume_of_process_run_id").Default(""),
		field.String("queue_answer_batch_id").Default(""),
		field.String("workflow_definition_id").Default(""),
		field.String("workflow_status").Default(""),
		field.String("workflow_current_node_id").Default(""),
		field.JSON("workflow_context", map[string]any{}).Default(map[string]any{}),
		field.JSON("workflow_pending_approval", map[string]any{}).Default(map[string]any{}),
		field.Time("workflow_started_at").Optional().Nillable(),
		field.Time("workflow_stopped_at").Optional().Nillable(),
		field.JSON("applied_system_commands", map[string]bool{}).Optional(),
		field.Time("last_run_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("closed_at").Optional().Nillable(),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id"),
		index.Fields("project_id", "updated_at"),
		index.Fields("project_id", "last_run_at"),
		index.Fields("status"),
		index.Fields("status", "queue_priority", "priority", "queued_at"),
		index.Fields("worktree_cleanup_status", "worktree_cleanup_next_at", "updated_at"),
	}
}
