package sessionevent

import (
	"context"
	"errors"
	"time"

	eventapp "github.com/nzlov/anycode/internal/application/event"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

const (
	TypeStatus      = "session.status_updated"
	TypeUsage       = "usage.updated"
	domainTypeUsage = "session.usage_updated"
)

type UpdateDTO struct {
	ID               string
	Type             string
	SessionID        sessiondomain.ID
	OccurredAt       string
	Status           *sessionapp.CardStatusDTO
	TodoList         *sessiondomain.TodoList
	Usage            *sessiondomain.TokenUsage
	ArtifactCount    *int
	FilesChanged     *int
	Priority         *sessiondomain.Priority
	Config           *sessiondomain.Config
	WorktreeCleanup  *sessionapp.WorktreeCleanupDTO
	AvailableActions []string
	UpdatedAt        *time.Time
}

type UseCase interface {
	SessionEvents(ctx context.Context, sessionID sessiondomain.ID) (<-chan timelineapp.DTO, error)
	SessionUpdates(ctx context.Context) (<-chan UpdateDTO, error)
}

type TimelineSource interface {
	SessionEvents(ctx context.Context, input timelineapp.SessionEventsInput) (<-chan timelineapp.DTO, error)
}

type DomainEventSource interface {
	LiveSessionEvents(ctx context.Context, input eventapp.LiveSessionEventsInput) (<-chan eventapp.DTO, error)
}

type SessionStatusSource interface {
	GetSessionCardStatus(ctx context.Context, id sessiondomain.ID) (sessionapp.CardStatusDTO, error)
}

type Service struct {
	timeline TimelineSource
	events   DomainEventSource
	sessions SessionStatusSource
}

func New(timeline TimelineSource, events DomainEventSource, sessions SessionStatusSource) *Service {
	return &Service{timeline: timeline, events: events, sessions: sessions}
}

func (s *Service) SessionEvents(ctx context.Context, sessionID sessiondomain.ID) (<-chan timelineapp.DTO, error) {
	if s == nil || s.timeline == nil {
		return nil, errors.New("session event usecase is not configured")
	}
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	return s.timeline.SessionEvents(ctx, timelineapp.SessionEventsInput{SessionID: sessionID})
}

func (s *Service) SessionUpdates(ctx context.Context) (<-chan UpdateDTO, error) {
	if s == nil || s.timeline == nil || s.events == nil || s.sessions == nil {
		return nil, errors.New("session update usecase is not fully configured")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	domainEvents, err := s.events.LiveSessionEvents(streamCtx, eventapp.LiveSessionEventsInput{})
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan UpdateDTO)
	go func() {
		defer close(out)
		defer cancel()
		for {
			select {
			case <-streamCtx.Done():
				return
			case item, ok := <-domainEvents:
				if !ok {
					return
				}
				update, ok, err := s.fromDomainEvent(streamCtx, item)
				if err != nil {
					return
				}
				if ok && !send(streamCtx, out, update) {
					return
				}
			}
		}
	}()
	return out, nil
}

func (s *Service) fromDomainEvent(ctx context.Context, item eventapp.DTO) (UpdateDTO, bool, error) {
	if item.SessionID == nil {
		return UpdateDTO{}, false, nil
	}
	update := UpdateDTO{
		ID:         string(item.ID),
		Type:       item.Type,
		SessionID:  sessiondomain.ID(*item.SessionID),
		OccurredAt: item.CreatedAt,
	}
	switch {
	case item.Type == TypeStatus:
		status, err := s.sessions.GetSessionCardStatus(ctx, update.SessionID)
		if err != nil {
			return UpdateDTO{}, false, err
		}
		update.Status = &status
	case item.Type == "session.todo_list_updated":
		todo, ok := item.Payload["todoList"].(sessiondomain.TodoList)
		if !ok {
			return UpdateDTO{}, false, nil
		}
		update.TodoList = &todo
	case item.Type == "session.diff_changed":
		value, ok := eventInt(item.Payload, "filesChanged")
		if !ok {
			return UpdateDTO{}, false, nil
		}
		update.FilesChanged = &value
	case item.Type == "session.artifacts_updated":
		value, ok := eventInt(item.Payload, "artifactCount")
		if !ok {
			return UpdateDTO{}, false, nil
		}
		update.ArtifactCount = &value
	case item.Type == domainTypeUsage:
		usage, ok := item.Payload["usage"].(sessiondomain.TokenUsage)
		if !ok {
			return UpdateDTO{}, false, nil
		}
		update.Type = TypeUsage
		update.Usage = &usage
	case item.Type == "session.priority_changed":
		priority, priorityOK := item.Payload["priority"].(sessiondomain.Priority)
		updatedAt, updatedAtOK := item.Payload["updatedAt"].(time.Time)
		if !priorityOK || !updatedAtOK {
			return UpdateDTO{}, false, nil
		}
		update.Priority = &priority
		update.UpdatedAt = &updatedAt
	case item.Type == "session.config_changed":
		config, configOK := item.Payload["config"].(sessiondomain.Config)
		updatedAt, updatedAtOK := item.Payload["updatedAt"].(time.Time)
		if !configOK || !updatedAtOK {
			return UpdateDTO{}, false, nil
		}
		update.Config = &config
		update.UpdatedAt = &updatedAt
	case item.Type == "session.worktree_cleanup_requested",
		item.Type == "session.worktree_cleanup_completed",
		item.Type == "session.worktree_cleanup_failed",
		item.Type == "session.worktree_ownership_confirmed":
		cleanup, cleanupOK := item.Payload["worktreeCleanup"].(sessionapp.WorktreeCleanupDTO)
		actions, actionsOK := item.Payload["availableActions"].([]string)
		updatedAt, updatedAtOK := item.Payload["updatedAt"].(time.Time)
		if !cleanupOK || !actionsOK || !updatedAtOK {
			return UpdateDTO{}, false, nil
		}
		update.WorktreeCleanup = &cleanup
		update.AvailableActions = actions
		update.UpdatedAt = &updatedAt
	default:
		return UpdateDTO{}, false, nil
	}
	return update, true, nil
}

// GLUE: live domain events still expose map payloads; remove this conversion when their payloads are typed.
func eventInt(payload map[string]any, key string) (int, bool) {
	value, ok := payload[key].(int)
	return value, ok
}

func send(ctx context.Context, out chan<- UpdateDTO, event UpdateDTO) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- event:
		return true
	}
}
