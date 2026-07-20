package timeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	ListSessionEvents(ctx context.Context, input ListSessionEventsInput) (Page, error)
	SessionEvents(ctx context.Context, input SessionEventsInput) (<-chan DTO, error)
}

type ListSessionEventsInput struct {
	SessionID    sessiondomain.ID
	BeforeCursor string
	MessageRole  string
	Limit        int
}

type SessionEventsInput struct {
	SessionID sessiondomain.ID
}

type SessionRepository interface {
	Find(ctx context.Context, id sessiondomain.ID) (sessiondomain.Session, error)
}

type CodexTranscriptSource interface {
	SessionEventPage(ctx context.Context, input processdomain.CodexTranscriptPageInput) (processdomain.CodexTranscriptPage, error)
}

type CodexTranscriptIndex interface {
	TranscriptSources(ctx context.Context, sessionID processdomain.SessionID) ([]processdomain.CodexTranscriptSource, error)
}

type LiveEventSource interface {
	LiveCodexEvents(ctx context.Context, sessionID processdomain.SessionID) (<-chan processdomain.CodexEvent, error)
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

type Service struct {
	live       LiveEventSource
	sessions   SessionRepository
	transcript CodexTranscriptSource
	index      CodexTranscriptIndex
	history    eventdomain.Store
}

type Option func(*Service)

func WithHistory(history eventdomain.Store) Option {
	return func(service *Service) {
		service.history = history
	}
}

func New(live LiveEventSource, sessions SessionRepository, transcript CodexTranscriptSource, index CodexTranscriptIndex, options ...Option) *Service {
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
	current, err := s.sessions.Find(ctx, input.SessionID)
	if err != nil {
		return Page{}, fmt.Errorf("find session for timeline: %w", err)
	}
	sources, err := s.codexTranscriptSources(ctx, current)
	if err != nil {
		return Page{}, fmt.Errorf("list session transcript sources: %w", err)
	}
	if len(sources) == 0 || s.transcript == nil {
		return s.listStoredSessionEvents(ctx, current, input, limit)
	}
	events, nextCursor, err := s.listTranscriptPage(ctx, sources, input.BeforeCursor, input.MessageRole, limit)
	if err != nil {
		return Page{}, err
	}
	if input.BeforeCursor == "" {
		statuses, statusErr := s.statusHistoryEvents(ctx, current)
		if statusErr != nil {
			return Page{}, statusErr
		}
		events = append(events, statuses...)
		sort.SliceStable(events, func(i, j int) bool { return events[i].OrderKey < events[j].OrderKey })
	}
	events = groupTimelineEvents(events)
	return Page{
		Items:      events,
		Page:       1,
		PageSize:   limit,
		Total:      len(events),
		NextCursor: nextCursor,
		Usage:      tokenUsageFromSession(current.Usage),
	}, nil
}

func (s *Service) listStoredSessionEvents(ctx context.Context, current sessiondomain.Session, input ListSessionEventsInput, limit int) (Page, error) {
	events, err := s.statusHistoryEvents(ctx, current)
	if err != nil {
		return Page{}, err
	}
	events = groupTimelineEvents(events)
	pageEvents, total, hasMore := pageEventsBefore(events, eventdomain.ID(input.BeforeCursor), limit)
	nextCursor := ""
	if hasMore && len(pageEvents) > 0 {
		nextCursor = string(pageEvents[0].ID)
	}
	return Page{
		Items: pageEvents, Page: 1, PageSize: limit, Total: total, NextCursor: nextCursor,
		Usage: tokenUsageFromSession(current.Usage),
	}, nil
}

func (s *Service) listTranscriptPage(ctx context.Context, sources []processdomain.CodexTranscriptSource, cursor string, messageRole string, limit int) ([]DTO, string, error) {
	sourceIndex, beforeOffset, err := decodeTranscriptCursor(cursor, len(sources))
	if err != nil {
		return nil, "", err
	}
	collected := []DTO(nil)
	nextCursor := ""
	for sourceIndex >= 0 && len(collected) < limit {
		page, err := s.transcript.SessionEventPage(ctx, processdomain.CodexTranscriptPageInput{
			Source: sources[sourceIndex], BeforeOffset: beforeOffset, Limit: limit,
		})
		if err != nil {
			return nil, "", fmt.Errorf("read session transcript page: %w", err)
		}
		pageEvents := transcriptPageEvents(page.Events, sourceIndex+1, messageRole)
		collected = append(pageEvents, collected...)
		if page.HasMore {
			if page.StartOffset <= 0 || (beforeOffset > 0 && page.StartOffset >= beforeOffset) {
				return nil, "", errors.New("read session transcript page: cursor did not advance")
			}
			beforeOffset = page.StartOffset
			nextCursor = encodeTranscriptCursor(sourceIndex, beforeOffset)
			continue
		}
		sourceIndex--
		beforeOffset = 0
		if sourceIndex >= 0 {
			nextCursor = encodeTranscriptCursor(sourceIndex, 0)
		} else {
			nextCursor = ""
		}
	}
	return collected, nextCursor, nil
}

func transcriptPageEvents(events []processdomain.CodexEvent, sourceGroup int, messageRole string) []DTO {
	result := make([]DTO, 0, len(events))
	for _, event := range events {
		if _, ok := event.Content.(processdomain.CodexUsageContent); ok {
			continue
		}
		item, ok := fromCodexEvent(event, sourceGroup)
		if !ok {
			continue
		}
		if messageRole != "" {
			message, ok := item.Content.(processdomain.CodexMessageContent)
			if !ok || message.Role != messageRole {
				continue
			}
		}
		result = append(result, item)
	}
	return result
}

func encodeTranscriptCursor(sourceIndex int, offset int64) string {
	return fmt.Sprintf("%d:%d", sourceIndex, offset)
}

func decodeTranscriptCursor(cursor string, sourceCount int) (int, int64, error) {
	if sourceCount < 1 {
		return -1, 0, nil
	}
	if strings.TrimSpace(cursor) == "" {
		return sourceCount - 1, 0, nil
	}
	parts := strings.Split(cursor, ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("invalid transcript cursor")
	}
	sourceIndex, err := strconv.Atoi(parts[0])
	if err != nil || sourceIndex < 0 || sourceIndex >= sourceCount {
		return 0, 0, errors.New("invalid transcript cursor source")
	}
	offset, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || offset < 0 {
		return 0, 0, errors.New("invalid transcript cursor offset")
	}
	return sourceIndex, offset, nil
}

func tokenUsageFromSession(usage sessiondomain.TokenUsage) *TokenUsageDTO {
	if usage.IsZero() {
		return nil
	}
	return &TokenUsageDTO{
		InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens,
		OutputTokens: usage.OutputTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens,
		TotalTokens: usage.TotalTokens, ContextWindow: usage.ContextWindow,
		CurrentInputTokens: usage.CurrentInputTokens, CurrentCachedInputTokens: usage.CurrentCachedInputTokens,
		CurrentOutputTokens: usage.CurrentOutputTokens, CurrentReasoningOutputTokens: usage.CurrentReasoningOutputTokens,
		CurrentTotalTokens: usage.CurrentTotalTokens, CompactionCount: usage.CompactionCount,
	}
}

func (s *Service) SessionEvents(ctx context.Context, input SessionEventsInput) (<-chan DTO, error) {
	if s == nil {
		return nil, errors.New("timeline usecase: nil service")
	}
	if s.live == nil {
		return nil, errors.New("live event source is required")
	}
	if input.SessionID == "" {
		return nil, errors.New("session id is required")
	}
	eventSessionID := eventdomain.SessionID(input.SessionID)
	sourceGroups, err := s.codexSourceGroups(ctx, eventdomain.Scope{SessionID: &eventSessionID})
	if err != nil {
		return nil, err
	}
	out := make(chan DTO, 16)
	liveCtx, cancelLive := context.WithCancel(ctx)
	live, err := s.live.LiveCodexEvents(liveCtx, processdomain.SessionID(input.SessionID))
	if err != nil {
		cancelLive()
		close(out)
		return nil, err
	}
	go func() {
		defer close(out)
		defer cancelLive()
		for {
			select {
			case codexEvent, ok := <-live:
				if !ok {
					return
				}
				if _, ok := codexEvent.Content.(processdomain.CodexUsageContent); ok {
					continue
				}
				sourceGroup := sourceGroups[codexEvent.CodexSessionID]
				if sourceGroup == 0 && codexEvent.CodexSessionID != "" {
					sourceGroup = len(sourceGroups) + 1
					sourceGroups[codexEvent.CodexSessionID] = sourceGroup
				}
				item, ok := fromCodexEvent(codexEvent, sourceGroup)
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
		content, visible := storedEventContent(item.Type, item.Payload)
		if !visible {
			continue
		}
		result = append(result, DTO{
			ID:         item.ID,
			OrderKey:   timelineOrderKey(item.CreatedAt, 0, 0, 0, string(item.ID)),
			Phase:      processdomain.CodexPhaseStandalone,
			Content:    content,
			OccurredAt: item.CreatedAt.UTC().Format(time.RFC3339Nano),
			Causality:  item.Causality,
		})
	}
	return result, nil
}

func (s *Service) codexTranscriptSources(ctx context.Context, current sessiondomain.Session) ([]processdomain.CodexTranscriptSource, error) {
	if s.index == nil {
		return nil, nil
	}
	return s.index.TranscriptSources(ctx, processdomain.SessionID(current.ID))
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
	sources, err := s.codexTranscriptSources(ctx, current)
	if err != nil {
		return nil, err
	}
	for index, source := range sources {
		groups[source.CodexSessionID] = index + 1
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

func fromCodexEvent(event processdomain.CodexEvent, sourceGroup int) (DTO, bool) {
	if !visibleCodexTimelineEvent(event) {
		return DTO{}, false
	}
	canonicalID := processdomain.CanonicalCodexEventID(event.CodexSessionID, event.EventID)
	if canonicalID == "" {
		canonicalID = event.EventID
	}
	return DTO{
		ID:            eventdomain.ID(canonicalID),
		Type:          event.Type,
		OrderKey:      timelineOrderKey(event.CreatedAt, sourceGroup, event.SourceOffset, event.SourceIndex, canonicalID),
		CorrelationID: canonicalCorrelationID(event.CodexSessionID, event.CorrelationID),
		Phase:         normalizedPhase(event.Phase),
		Content:       event.Content,
		OccurredAt:    event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, true
}

func visibleCodexTimelineEvent(event processdomain.CodexEvent) bool {
	if event.Content == nil {
		return false
	}
	switch event.Type {
	case processdomain.CodexEventPlan, processdomain.CodexEventTranscriptBound, processdomain.CodexEventProcessExit:
		return false
	default:
		return true
	}
}

func groupTimelineEvents(events []DTO) []DTO {
	grouped := make([]DTO, 0, len(events))
	indexes := map[eventdomain.ID]int{}
	for _, item := range events {
		groupID, kind, label, ok := routineGroup(item)
		if !ok {
			grouped = append(grouped, item)
			continue
		}
		if index, found := indexes[groupID]; found {
			grouped[index].Group.Members = append(grouped[index].Group.Members, item)
			continue
		}
		indexes[groupID] = len(grouped)
		grouped = append(grouped, newTimelineGroup(groupID, kind, label, item))
	}
	return grouped
}

func newTimelineGroup(id eventdomain.ID, kind, label string, first DTO) DTO {
	return DTO{
		ID:         id,
		OrderKey:   first.OrderKey,
		Phase:      processdomain.CodexPhaseStandalone,
		OccurredAt: first.OccurredAt,
		Causality:  first.Causality,
		Content: processdomain.CodexStatusContent{
			Code: "group." + kind, Level: "info", Message: label,
		},
		Group: &GroupDTO{Kind: kind, Label: label, Members: []DTO{first}},
	}
}

func routineGroup(item DTO) (eventdomain.ID, string, string, bool) {
	key := firstNonEmpty(item.Causality.ProcessRunID, item.Causality.NodeRunID, "session")
	switch content := item.Content.(type) {
	case processdomain.CodexStatusContent:
		code := content.Code
		if content.Level == "error" || strings.Contains(code, "waiting_user") || strings.Contains(code, "waiting_approval") || failedProcessExit(code, content.Details) {
			return "", "", "", false
		}
		if isLifecycleEvent(code) {
			return eventdomain.ID("group:lifecycle:" + key), "lifecycle", "Lifecycle", true
		}
	case processdomain.CodexUnknownContent:
		if strings.HasPrefix(content.RawType, "artifact.") && !strings.Contains(content.RawType, "failed") {
			artifactKey := strings.TrimSpace(item.Causality.CorrelationID)
			if nodeRunID := strings.TrimSpace(item.Causality.NodeRunID); nodeRunID != "" {
				if artifactKey != "" {
					artifactKey = nodeRunID + ":" + artifactKey
				} else {
					artifactKey = nodeRunID
				}
			}
			artifactKey = firstNonEmpty(artifactKey, item.Causality.ProcessRunID, "session")
			return eventdomain.ID("group:artifact:" + artifactKey), "artifact", "Artifacts", true
		}
	}
	return "", "", "", false
}

func isLifecycleEvent(code string) bool {
	switch code {
	case "session.queued", "session.starting", "process.transcript_bound", "session.running", "session.stopping", "session.stopped", "process.exited":
		return true
	default:
		return false
	}
}

func failedProcessExit(code string, details map[string]any) bool {
	if code != "process.exited" {
		return false
	}
	if reason, _ := details["failureReason"].(string); strings.TrimSpace(reason) != "" {
		return true
	}
	switch exitCode := details["exitCode"].(type) {
	case int:
		return exitCode != 0
	case int32:
		return exitCode != 0
	case int64:
		return exitCode != 0
	case float32:
		return exitCode != 0
	case float64:
		return exitCode != 0
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func storedEventContent(eventType string, payload map[string]any) (processdomain.CodexEventContent, bool) {
	if strings.HasPrefix(eventType, "artifact.") {
		return processdomain.CodexUnknownContent{RawType: eventType, Payload: payload}, true
	}
	if isVisibleStatusEvent(eventType) {
		return statusContent(eventType, payload), true
	}
	return nil, false
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
	switch eventType {
	case "session.answer_resume_queued",
		"session.artifact_archive_failed",
		"session.blocked",
		"session.close_failed",
		"session.closed",
		"session.closing",
		"session.completed",
		"session.execution_already_active",
		"session.failed",
		"session.prompt_append_cancelled",
		"session.queued",
		"session.recovery_waiting_user",
		"session.resume_failed",
		"session.running",
		"session.starting",
		"session.stopped",
		"session.stopping",
		"session.waiting_approval",
		"session.waiting_user",
		"session.worktree_cleanup_failed",
		"session.worktree_init_failed",
		"workflow.approval_submitted",
		"workflow.failed",
		"workflow.merge",
		"workflow.resume_action_failed",
		"process.exited":
		return true
	default:
		return false
	}
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
