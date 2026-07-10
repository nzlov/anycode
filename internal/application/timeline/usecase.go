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
	Scope eventdomain.Scope
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
	live       LiveEventSource
	sessions   SessionRepository
	transcript CodexTranscriptSource
	index      CodexSessionIndex
}

func New(live LiveEventSource, sessions SessionRepository, transcript CodexTranscriptSource, index CodexSessionIndex) *Service {
	return &Service{live: live, sessions: sessions, transcript: transcript, index: index}
}

func (s *Service) ListSessionEvents(ctx context.Context, input ListSessionEventsInput) (port.Page[DTO], error) {
	if s == nil {
		return port.Page[DTO]{}, errors.New("timeline usecase: nil service")
	}
	if input.SessionID == "" {
		return port.Page[DTO]{}, errors.New("session id is required")
	}
	limit := normalizeLimit(input.Limit)
	events, err := s.sessionHistoryEvents(ctx, input.SessionID)
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
	go func() {
		defer close(out)
		defer cancelLive()
		seen := map[eventdomain.ID]struct{}{}
		for {
			select {
			case eventDTO, ok := <-live:
				if !ok {
					return
				}
				if eventDTO.Type != "process.codex_event" {
					continue
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

func (s *Service) sessionHistoryEvents(ctx context.Context, sessionID sessiondomain.ID) ([]eventdomain.DomainEvent, error) {
	events, err := s.codexTranscriptEvents(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return events, nil
}

type orderedTranscriptEvent struct {
	event        eventdomain.DomainEvent
	sessionIndex int
	eventIndex   int
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
	ordered := []orderedTranscriptEvent(nil)
	for sessionIndex, codexSessionID := range codexSessionIDs {
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
			eventID := event.EventID
			if eventID == "" {
				eventID = fmt.Sprintf("line-%d", index)
			}
			createdAt := event.CreatedAt
			if createdAt.IsZero() {
				createdAt = current.UpdatedAt
			}
			ordered = append(ordered, orderedTranscriptEvent{
				event: eventdomain.DomainEvent{
					ID:        eventdomain.ID(processdomain.CanonicalCodexEventID(codexSessionID, eventID)),
					Scope:     eventdomain.Scope{ProjectID: string(current.ProjectID), SessionID: &sessionIDForEvent},
					SessionID: &sessionIDForEvent,
					Type:      "process.codex_event",
					Payload:   codexSessionEventPayload(codexSessionID, event),
					CreatedAt: createdAt,
				},
				sessionIndex: sessionIndex,
				eventIndex:   index,
			})
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left := ordered[i]
		right := ordered[j]
		if !left.event.CreatedAt.Equal(right.event.CreatedAt) {
			return left.event.CreatedAt.Before(right.event.CreatedAt)
		}
		if left.sessionIndex != right.sessionIndex {
			return left.sessionIndex < right.sessionIndex
		}
		return left.eventIndex < right.eventIndex
	})
	result := make([]eventdomain.DomainEvent, 0, len(ordered))
	for _, item := range ordered {
		result = append(result, item.event)
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

func codexSessionEventPayload(codexSessionID string, event processdomain.CodexEvent) map[string]any {
	payload := make(map[string]any, len(event.Payload)+3)
	for key, value := range event.Payload {
		payload[key] = value
	}
	payload["codexType"] = event.Type
	payload["codexSessionId"] = codexSessionID
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
