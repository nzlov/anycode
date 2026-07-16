package graph

import eventapp "github.com/nzlov/anycode/internal/application/event"

func sessionCardChangeEvent(eventDTO eventapp.DTO, projectID *string) bool {
	if projectID != nil && eventDTO.Scope.ProjectID != *projectID {
		return false
	}
	switch eventDTO.Type {
	case "session.queued",
		"session.starting",
		"session.running",
		"session.waiting_user",
		"session.waiting_approval",
		"session.stopping",
		"session.stopped",
		"session.recoverable",
		"session.resume_failed",
		"session.failed",
		"session.blocked",
		"session.completed",
		"session.closed",
		"session.worktree_cleanup_requested",
		"session.worktree_cleanup_completed",
		"session.worktree_cleanup_failed",
		"session.config_changed",
		"session.priority_changed",
		"session.todo_list_updated",
		"session.diff_changed",
		"artifact.published",
		"artifact.deleted",
		"workflow.waiting_resume_action",
		"workflow.resume_action_failed",
		"workflow.failed",
		"workflow.blocked",
		"workflow.completed":
		return true
	default:
		return false
	}
}
