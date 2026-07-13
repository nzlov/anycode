package timeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/event"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	ListSessionEvents(ctx context.Context, input ListSessionEventsInput) (Page, error)
	SessionEvents(ctx context.Context, input SessionEventsInput) (<-chan DTO, error)
}

type ListSessionEventsInput struct {
	SessionID     sessiondomain.ID
	BeforeEventID eventdomain.ID
	MessageRole   string
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

const (
	defaultLimit = 50
	maxLimit     = 200
)

type Service struct {
	live       LiveEventSource
	sessions   SessionRepository
	transcript CodexTranscriptSource
	index      CodexSessionIndex
	history    eventdomain.Store
}

type Option func(*Service)

func WithHistory(history eventdomain.Store) Option {
	return func(service *Service) {
		service.history = history
	}
}

func New(live LiveEventSource, sessions SessionRepository, transcript CodexTranscriptSource, index CodexSessionIndex, options ...Option) *Service {
	service := &Service{live: live, sessions: sessions, transcript: transcript, index: index}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) ListSessionEvents(ctx context.Context, input ListSessionEventsInput) (Page, error) {
	if s == nil {
		return Page{}, errors.New("timeline usecase: nil service")
	}
	if input.SessionID == "" {
		return Page{}, errors.New("session id is required")
	}
	limit := normalizeLimit(input.Limit)
	events, usage, err := s.sessionHistoryEvents(ctx, input.SessionID)
	if err != nil {
		return Page{}, fmt.Errorf("list session events: %w", err)
	}
	events = filterEventsByMessageRole(events, input.MessageRole)
	pageEvents, total, hasMore := pageEventsBefore(events, input.BeforeEventID, limit)
	nextCursor := ""
	if hasMore && len(pageEvents) > 0 {
		nextCursor = string(pageEvents[0].ID)
	}
	return Page{
		Items:      pageEvents,
		Page:       1,
		PageSize:   limit,
		Total:      total,
		NextCursor: nextCursor,
		Usage:      usage,
	}, nil
}

func filterEventsByMessageRole(events []DTO, role string) []DTO {
	role = strings.TrimSpace(role)
	if role == "" {
		return events
	}
	filtered := make([]DTO, 0, len(events))
	for _, item := range events {
		content, ok := item.Content.(processdomain.CodexMessageContent)
		if ok && content.Role == role {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *Service) SessionEvents(ctx context.Context, input SessionEventsInput) (<-chan DTO, error) {
	if s == nil {
		return nil, errors.New("timeline usecase: nil service")
	}
	if s.live == nil {
		return nil, errors.New("live event source is required")
	}
	sourceGroups, err := s.codexSourceGroups(ctx, input.Scope)
	if err != nil {
		return nil, err
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
				if _, ok := seen[eventdomain.ID(eventDTO.ID)]; ok {
					continue
				}
				seen[eventdomain.ID(eventDTO.ID)] = struct{}{}
				sourceGroup := 0
				if eventDTO.Type == "process.codex_event" {
					codexSessionID, _ := eventDTO.Payload["codexSessionId"].(string)
					sourceGroup = sourceGroups[codexSessionID]
					if sourceGroup == 0 && codexSessionID != "" {
						sourceGroup = len(sourceGroups) + 1
						sourceGroups[codexSessionID] = sourceGroup
					}
				}
				item, ok := fromEventDTO(eventDTO, sourceGroup)
				if !ok {
					continue
				}
				select {
				case out <- item:
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

func (s *Service) sessionHistoryEvents(ctx context.Context, sessionID sessiondomain.ID) ([]DTO, *TokenUsageDTO, error) {
	if s.sessions == nil {
		return nil, nil, nil
	}
	current, err := s.sessions.Find(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	events, usage, err := s.codexTranscriptEvents(ctx, current)
	if err != nil {
		return nil, nil, err
	}
	statuses, err := s.statusHistoryEvents(ctx, current)
	if err != nil {
		return nil, nil, err
	}
	events = append(events, statuses...)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].OrderKey < events[j].OrderKey
	})
	return events, usage, nil
}

type orderedTranscriptEvent struct {
	event        DTO
	createdAt    time.Time
	sessionIndex int
	eventIndex   int
}

func (s *Service) codexTranscriptEvents(ctx context.Context, current sessiondomain.Session) ([]DTO, *TokenUsageDTO, error) {
	if s.transcript == nil {
		return nil, nil, nil
	}
	codexSessionIDs, err := s.codexSessionIDs(ctx, current)
	if err != nil {
		return nil, nil, err
	}
	if len(codexSessionIDs) == 0 {
		return nil, nil, nil
	}
	ordered := []orderedTranscriptEvent(nil)
	var latestUsage *TokenUsageDTO
	for sessionIndex, codexSessionID := range codexSessionIDs {
		events, err := s.transcript.SessionEvents(ctx, processdomain.CodexTranscriptInput{
			CodexSessionID: codexSessionID,
		})
		if err != nil {
			return nil, nil, err
		}
		for index, event := range events {
			if event.Type == "" || event.Content == nil {
				continue
			}
			if usage, ok := event.Content.(processdomain.CodexUsageContent); ok {
				latestUsage = usageDTO(usage)
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
			canonicalID := processdomain.CanonicalCodexEventID(codexSessionID, eventID)
			ordered = append(ordered, orderedTranscriptEvent{
				event: DTO{
					ID:            eventdomain.ID(canonicalID),
					OrderKey:      timelineOrderKey(createdAt, sessionIndex+1, event.SourceOffset, event.SourceIndex, canonicalID),
					CorrelationID: canonicalCorrelationID(codexSessionID, event.CorrelationID),
					Phase:         normalizedPhase(event.Phase),
					Content:       event.Content,
					OccurredAt:    createdAt.UTC().Format(time.RFC3339Nano),
				},
				createdAt:    createdAt,
				sessionIndex: sessionIndex,
				eventIndex:   index,
			})
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		left := ordered[i]
		right := ordered[j]
		if !left.createdAt.Equal(right.createdAt) {
			return left.createdAt.Before(right.createdAt)
		}
		if left.sessionIndex != right.sessionIndex {
			return left.sessionIndex < right.sessionIndex
		}
		return left.eventIndex < right.eventIndex
	})
	result := make([]DTO, 0, len(ordered))
	for _, item := range ordered {
		result = append(result, item.event)
	}
	return result, latestUsage, nil
}

func (s *Service) statusHistoryEvents(ctx context.Context, current sessiondomain.Session) ([]DTO, error) {
	if s.history == nil {
		return nil, nil
	}
	sessionID := eventdomain.SessionID(current.ID)
	events, err := s.history.List(ctx, eventdomain.Scope{ProjectID: string(current.ProjectID), SessionID: &sessionID})
	if err != nil {
		return nil, err
	}
	result := make([]DTO, 0, len(events))
	for _, item := range events {
		if !isVisibleStatusEvent(item.Type) {
			continue
		}
		result = append(result, DTO{
			ID:         item.ID,
			OrderKey:   timelineOrderKey(item.CreatedAt, 0, 0, 0, string(item.ID)),
			Phase:      processdomain.CodexPhaseStandalone,
			Content:    statusContent(item.Type, item.Payload),
			OccurredAt: item.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
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

func (s *Service) codexSourceGroups(ctx context.Context, scope eventdomain.Scope) (map[string]int, error) {
	groups := map[string]int{}
	if s.sessions == nil || scope.SessionID == nil {
		return groups, nil
	}
	current, err := s.sessions.Find(ctx, sessiondomain.ID(*scope.SessionID))
	if err != nil {
		return nil, err
	}
	ids, err := s.codexSessionIDs(ctx, current)
	if err != nil {
		return nil, err
	}
	for index, id := range ids {
		groups[id] = index + 1
	}
	return groups, nil
}

func pageEventsBefore(events []DTO, before eventdomain.ID, limit int) ([]DTO, int, bool) {
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

func fromEventDTO(dto event.DTO, sourceGroup int) (DTO, bool) {
	createdAt, _ := time.Parse(time.RFC3339Nano, dto.CreatedAt)
	if dto.Type == "process.codex_event" {
		content, ok := dto.Payload["codexContent"].(processdomain.CodexEventContent)
		if !ok || content == nil {
			return DTO{}, false
		}
		if usage, ok := content.(processdomain.CodexUsageContent); ok {
			return DTO{Usage: usageDTO(usage)}, true
		}
		codexSessionID, _ := dto.Payload["codexSessionId"].(string)
		correlationID, _ := dto.Payload["codexCorrelationId"].(string)
		phase, _ := dto.Payload["codexPhase"].(string)
		sourceOffset, _ := dto.Payload["codexSourceOffset"].(int64)
		sourceIndex, _ := dto.Payload["codexSourceIndex"].(int)
		return DTO{
			ID:            dto.ID,
			OrderKey:      timelineOrderKey(createdAt, sourceGroup, sourceOffset, sourceIndex, string(dto.ID)),
			CorrelationID: canonicalCorrelationID(codexSessionID, correlationID),
			Phase:         normalizedPhase(processdomain.CodexPhase(phase)),
			Content:       content,
			OccurredAt:    dto.CreatedAt,
		}, true
	}
	if !isVisibleStatusEvent(dto.Type) {
		return DTO{}, false
	}
	return DTO{
		ID:         dto.ID,
		OrderKey:   timelineOrderKey(createdAt, 0, 0, 0, string(dto.ID)),
		Phase:      processdomain.CodexPhaseStandalone,
		Content:    statusContent(dto.Type, dto.Payload),
		OccurredAt: dto.CreatedAt,
	}, true
}

func canonicalCorrelationID(codexSessionID string, correlationID string) string {
	if strings.TrimSpace(correlationID) == "" {
		return ""
	}
	return "codex:" + codexSessionID + ":" + correlationID
}

func normalizedPhase(phase processdomain.CodexPhase) processdomain.CodexPhase {
	if phase == "" {
		return processdomain.CodexPhaseStandalone
	}
	return phase
}

func isVisibleStatusEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "session.") || strings.HasPrefix(eventType, "workflow.") || eventType == "process.exited"
}

func statusContent(code string, payload map[string]any) processdomain.CodexStatusContent {
	level := "info"
	if strings.Contains(code, "failed") || strings.Contains(code, "blocked") {
		level = "error"
	} else if strings.Contains(code, "waiting") || strings.Contains(code, "stopping") {
		level = "warning"
	}
	message := ""
	for _, key := range []string{"message", "reason", "failureReason", "blockedReason"} {
		if value, ok := payload[key].(string); ok && value != "" {
			message = value
			break
		}
	}
	return processdomain.CodexStatusContent{Code: code, Level: level, Message: message, Details: payload}
}
