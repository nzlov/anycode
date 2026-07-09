package timeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nzlov/anycode/internal/application/event"
	"github.com/nzlov/anycode/internal/application/port"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	ListSessionEvents(ctx context.Context, input ListSessionEventsInput) (port.Page[DTO], error)
	SessionEvents(ctx context.Context, input SessionEventsInput) (<-chan DTO, error)
}

type ListSessionEventsInput struct {
	SessionID     sessiondomain.ID
	BeforeEventID eventdomain.ID
	Limit         int
}

type SessionEventsInput struct {
	Scope        eventdomain.Scope
	AfterEventID eventdomain.ID
}

type SessionRepository interface {
	Find(ctx context.Context, id sessiondomain.ID) (sessiondomain.Session, error)
}

type CodexTranscriptSource interface {
	SessionEvents(ctx context.Context, input processdomain.CodexTranscriptInput) ([]processdomain.CodexEvent, error)
}

type CodexSessionIndex interface {
	CodexSessionIDs(ctx context.Context, sessionID processdomain.SessionID) ([]string, error)
}

type LiveEventSource interface {
	LiveSessionEvents(ctx context.Context, input event.LiveSessionEventsInput) (<-chan event.DTO, error)
}

type DTO struct {
	ID        eventdomain.ID
	Scope     eventdomain.Scope
	SessionID *eventdomain.SessionID
	Type      string
	Payload   map[string]any
	CreatedAt string
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

type Service struct {
	store      eventdomain.Store
	live       LiveEventSource
	sessions   SessionRepository
	transcript CodexTranscriptSource
	index      CodexSessionIndex
}

func New(store eventdomain.Store, live LiveEventSource, sessions SessionRepository, transcript CodexTranscriptSource, index CodexSessionIndex) *Service {
	return &Service{store: store, live: live, sessions: sessions, transcript: transcript, index: index}
}

func (s *Service) ListSessionEvents(ctx context.Context, input ListSessionEventsInput) (port.Page[DTO], error) {
	if s == nil {
		return port.Page[DTO]{}, errors.New("timeline usecase: nil service")
	}
	if s.store == nil {
		return port.Page[DTO]{}, errors.New("event store is required")
	}
	if input.SessionID == "" {
		return port.Page[DTO]{}, errors.New("session id is required")
	}
	limit := normalizeLimit(input.Limit)
	sessionID := eventdomain.SessionID(input.SessionID)
	events, err := s.sessionHistoryEvents(ctx, input.SessionID, eventdomain.Scope{SessionID: &sessionID})
	if err != nil {
		return port.Page[DTO]{}, fmt.Errorf("list session events: %w", err)
	}
	pageEvents, total, hasMore := pageEventsBefore(events, input.BeforeEventID, limit)
	items := toDTOs(pageEvents)
	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = string(items[0].ID)
	}
	return port.Page[DTO]{
		Items:      items,
		Page:       1,
		PageSize:   limit,
		Total:      total,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) SessionEvents(ctx context.Context, input SessionEventsInput) (<-chan DTO, error) {
	if s == nil {
		return nil, errors.New("timeline usecase: nil service")
	}
	if s.store == nil {
		return nil, errors.New("event store is required")
	}
	if s.live == nil {
		return nil, errors.New("live event source is required")
	}
	out := make(chan DTO, 16)
	liveCtx, cancelLive := context.WithCancel(ctx)
	live, err := s.live.LiveSessionEvents(liveCtx, event.LiveSessionEventsInput{Scope: input.Scope})
	if err != nil {
		cancelLive()
		close(out)
		return nil, err
	}
	events, err := s.eventsAfter(ctx, input.Scope, input.AfterEventID)
	if err != nil {
		cancelLive()
		close(out)
		return nil, fmt.Errorf("list session events: %w", err)
	}
	go func() {
		defer close(out)
		defer cancelLive()
		seen := map[eventdomain.ID]struct{}{}
		for _, event := range events {
			seen[event.ID] = struct{}{}
			select {
			case out <- toDTO(event):
			case <-ctx.Done():
				return
			}
		}
		for {
			select {
			case eventDTO, ok := <-live:
				if !ok {
					return
				}
				if _, ok := seen[eventdomain.ID(eventDTO.ID)]; ok {
					continue
				}
				seen[eventdomain.ID(eventDTO.ID)] = struct{}{}
				select {
				case out <- fromEventDTO(eventDTO):
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (s *Service) eventsAfter(ctx context.Context, scope eventdomain.Scope, after eventdomain.ID) ([]eventdomain.DomainEvent, error) {
	if scope.SessionID == nil || s.transcript == nil || s.sessions == nil {
		return s.store.After(ctx, scope, after)
	}
	events, err := s.sessionHistoryEvents(ctx, sessiondomain.ID(*scope.SessionID), scope)
	if err != nil {
		return nil, err
	}
	index := -1
	if after != "" {
		for i, event := range events {
			if event.ID == after {
				index = i
				break
			}
		}
		if index < 0 {
			return nil, fmt.Errorf("find after event: %s", after)
		}
	}
	if index+1 >= len(events) {
		return nil, nil
	}
	return events[index+1:], nil
}

func (s *Service) sessionHistoryEvents(ctx context.Context, sessionID sessiondomain.ID, scope eventdomain.Scope) ([]eventdomain.DomainEvent, error) {
	stored, err := s.store.List(ctx, scope)
	if err != nil {
		return nil, err
	}
	events := nonCodexSessionEvents(stored)
	transcript, err := s.codexTranscriptEvents(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	events = append(events, transcript...)
	sortEvents(events)
	return events, nil
}

func nonCodexSessionEvents(events []eventdomain.DomainEvent) []eventdomain.DomainEvent {
	filtered := make([]eventdomain.DomainEvent, 0, len(events))
	for _, event := range events {
		if event.Type == "process.codex_event" {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func (s *Service) codexTranscriptEvents(ctx context.Context, sessionID sessiondomain.ID) ([]eventdomain.DomainEvent, error) {
	if s.transcript == nil || s.sessions == nil {
		return nil, nil
	}
	current, err := s.sessions.Find(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	codexSessionIDs, err := s.codexSessionIDs(ctx, current)
	if err != nil {
		return nil, err
	}
	if len(codexSessionIDs) == 0 {
		return nil, nil
	}
	sessionIDForEvent := eventdomain.SessionID(current.ID)
	result := []eventdomain.DomainEvent(nil)
	for _, codexSessionID := range codexSessionIDs {
		events, err := s.transcript.SessionEvents(ctx, processdomain.CodexTranscriptInput{
			CodexSessionID: codexSessionID,
		})
		if err != nil {
			return nil, err
		}
		for index, event := range events {
			if event.Type == "" {
				continue
			}
			id := event.EventID
			if id == "" {
				id = fmt.Sprintf("%s:line-%d", codexSessionID, index)
			}
			createdAt := event.CreatedAt
			if createdAt.IsZero() {
				createdAt = current.UpdatedAt
			}
			result = append(result, eventdomain.DomainEvent{
				ID:        eventdomain.ID("codex:" + id),
				Scope:     eventdomain.Scope{ProjectID: string(current.ProjectID), SessionID: &sessionIDForEvent},
				SessionID: &sessionIDForEvent,
				Type:      "process.codex_event",
				Payload:   codexSessionEventPayload(event),
				CreatedAt: createdAt,
			})
		}
	}
	return result, nil
}

func (s *Service) codexSessionIDs(ctx context.Context, current sessiondomain.Session) ([]string, error) {
	seen := map[string]struct{}{}
	ids := []string(nil)
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if s.index != nil {
		indexed, err := s.index.CodexSessionIDs(ctx, processdomain.SessionID(current.ID))
		if err != nil {
			return nil, err
		}
		for _, id := range indexed {
			add(id)
		}
	}
	add(current.CodexSessionID)
	return ids, nil
}

func codexSessionEventPayload(event processdomain.CodexEvent) map[string]any {
	payload := make(map[string]any, len(event.Payload)+2)
	for key, value := range event.Payload {
		payload[key] = value
	}
	payload["codexType"] = event.Type
	if event.EventID != "" {
		payload["codexEventId"] = event.EventID
	}
	return payload
}

func pageEventsBefore(events []eventdomain.DomainEvent, before eventdomain.ID, limit int) ([]eventdomain.DomainEvent, int, bool) {
	end := len(events)
	if before != "" {
		end = -1
		for i, event := range events {
			if event.ID == before {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, len(events), false
		}
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return events[start:end], len(events), start > 0
}

func sortEvents(events []eventdomain.DomainEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		left := events[i]
		right := events[j]
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.ID < right.ID
	})
}

func normalizeLimit(limit int) int {
	if limit < 1 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit
}

func toDTOs(events []eventdomain.DomainEvent) []DTO {
	items := make([]DTO, 0, len(events))
	for _, event := range events {
		items = append(items, toDTO(event))
	}
	return items
}

func toDTO(event eventdomain.DomainEvent) DTO {
	return DTO{
		ID:        event.ID,
		Scope:     event.Scope,
		SessionID: event.SessionID,
		Type:      event.Type,
		Payload:   payloadOrEmpty(event.Payload),
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func fromEventDTO(dto event.DTO) DTO {
	return DTO{
		ID:        dto.ID,
		Scope:     dto.Scope,
		SessionID: dto.SessionID,
		Type:      dto.Type,
		Payload:   payloadOrEmpty(dto.Payload),
		CreatedAt: dto.CreatedAt,
	}
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}
