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
		"session.priority_changed",
		"session.todo_list_updated",
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
