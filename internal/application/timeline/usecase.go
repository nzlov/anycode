package timeline

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	SessionID     sessiondomain.ID
	BeforeEventID eventdomain.ID
	MessageRole   string
	Limit         int
}

type SessionEventsInput struct {
	SessionID sessiondomain.ID
}

type SessionRepository interface {
	Find(ctx context.Context, id sessiondomain.ID) (sessiondomain.Session, error)
}

type CodexTranscriptSource interface {
	SessionEvents(ctx context.Context, input processdomain.CodexTranscriptInput) ([]processdomain.CodexEvent, error)
}

type CodexTranscriptIndex interface {
	TranscriptSources(ctx context.Context, sessionID processdomain.SessionID) ([]processdomain.CodexTranscriptSource, error)
}

type CodexTranscriptRunIndex interface {
	TranscriptRuns(ctx context.Context, sessionID processdomain.SessionID) ([]processdomain.Run, error)
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
	events, usage, processUsage, nodeUsage, err := s.sessionHistoryEvents(ctx, input.SessionID)
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
		Items:        pageEvents,
		Page:         1,
		PageSize:     limit,
		Total:        total,
		NextCursor:   nextCursor,
		Usage:        usage,
		ProcessUsage: processUsage,
		NodeUsage:    nodeUsage,
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

func (s *Service) sessionHistoryEvents(ctx context.Context, sessionID sessiondomain.ID) ([]DTO, *TokenUsageDTO, []UsageAttributionDTO, []UsageAttributionDTO, error) {
	if s.sessions == nil {
		return nil, nil, nil, nil, nil
	}
	current, err := s.sessions.Find(ctx, sessionID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	events, usage, processUsage, nodeUsage, err := s.codexTranscriptEvents(ctx, current)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	statuses, err := s.statusHistoryEvents(ctx, current)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	events = append(events, statuses...)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].OrderKey < events[j].OrderKey
	})
	return groupTimelineEvents(events), usage, processUsage, nodeUsage, nil
}

type orderedTranscriptEvent struct {
	event        DTO
	createdAt    time.Time
	sessionIndex int
	eventIndex   int
}

func (s *Service) codexTranscriptEvents(ctx context.Context, current sessiondomain.Session) ([]DTO, *TokenUsageDTO, []UsageAttributionDTO, []UsageAttributionDTO, error) {
	if s.transcript == nil {
		return nil, nil, nil, nil, nil
	}
	sources, err := s.codexTranscriptSources(ctx, current)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if len(sources) == 0 {
		return nil, nil, nil, nil, nil
	}
	ordered := []orderedTranscriptEvent(nil)
	var latestUsage *TokenUsageDTO
	var latestUsageAt time.Time
	compactionCount := 0
	usageSamples := map[string][]usageSample{}
	compactions := map[string][]time.Time{}
	for sessionIndex, source := range sources {
		codexSessionID := source.CodexSessionID
		events, err := s.transcript.SessionEvents(ctx, processdomain.CodexTranscriptInput{
			Source: source,
		})
		if err != nil {
			return nil, nil, nil, nil, err
		}
		for index, event := range events {
			eventTime := event.CreatedAt
			if eventTime.IsZero() {
				eventTime = current.UpdatedAt
			}
			if status, ok := event.Content.(processdomain.CodexStatusContent); ok && status.Code == "context.compacted" {
				compactionCount++
				compactions[codexSessionID] = append(compactions[codexSessionID], eventTime)
			}
			if event.Type == "" || event.Content == nil {
				continue
			}
			if usage, ok := event.Content.(processdomain.CodexUsageContent); ok {
				if latestUsage == nil || !eventTime.Before(latestUsageAt) {
					latestUsage = usageDTO(usage)
					latestUsageAt = eventTime
				}
				usageSamples[codexSessionID] = append(usageSamples[codexSessionID], usageSample{at: eventTime, usage: usage})
				continue
			}
			if !visibleCodexTimelineEvent(event) {
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
					Type:          event.Type,
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
	if latestUsage != nil {
		latestUsage.CompactionCount = compactionCount
	}
	for codexSessionID := range usageSamples {
		sort.SliceStable(usageSamples[codexSessionID], func(i, j int) bool {
			return usageSamples[codexSessionID][i].at.Before(usageSamples[codexSessionID][j].at)
		})
		sort.SliceStable(compactions[codexSessionID], func(i, j int) bool {
			return compactions[codexSessionID][i].Before(compactions[codexSessionID][j])
		})
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
	processUsage, nodeUsage, err := s.usageAttributions(ctx, current, usageSamples, compactions)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return result, latestUsage, processUsage, nodeUsage, nil
}

type usageSample struct {
	at    time.Time
	usage processdomain.CodexUsageContent
}

func (s *Service) usageAttributions(ctx context.Context, current sessiondomain.Session, samples map[string][]usageSample, compactions map[string][]time.Time) ([]UsageAttributionDTO, []UsageAttributionDTO, error) {
	index, ok := s.index.(CodexTranscriptRunIndex)
	if !ok {
		return nil, nil, nil
	}
	runs, err := index.TranscriptRuns(ctx, processdomain.SessionID(current.ID))
	if err != nil {
		return nil, nil, err
	}
	processUsage := make([]UsageAttributionDTO, 0, len(runs))
	nodeIndexes := map[string]int{}
	nodeUsage := []UsageAttributionDTO(nil)
	for _, run := range runs {
		runSamples := samples[run.CodexSessionID]
		start := cumulativeUsageBefore(runSamples, run.StartedAt)
		endAt := time.Time{}
		if run.FinishedAt != nil {
			endAt = *run.FinishedAt
		}
		end, currentTurn := cumulativeUsageAt(runSamples, endAt)
		usage := usageDelta(start, end, currentTurn)
		usage.CompactionCount = countTimesInWindow(compactions[run.CodexSessionID], run.StartedAt, endAt)
		attribution := UsageAttributionDTO{ProcessRunID: string(run.ID), Usage: usage}
		if run.NodeRunID != nil {
			attribution.NodeRunID = string(*run.NodeRunID)
		}
		processUsage = append(processUsage, attribution)
		if attribution.NodeRunID == "" {
			continue
		}
		if nodeIndex, found := nodeIndexes[attribution.NodeRunID]; found {
			nodeUsage[nodeIndex].Usage = addUsage(nodeUsage[nodeIndex].Usage, usage)
		} else {
			nodeIndexes[attribution.NodeRunID] = len(nodeUsage)
			nodeUsage = append(nodeUsage, UsageAttributionDTO{NodeRunID: attribution.NodeRunID, Usage: usage})
		}
	}
	return processUsage, nodeUsage, nil
}

func cumulativeUsageBefore(samples []usageSample, before time.Time) processdomain.CodexUsageContent {
	var result processdomain.CodexUsageContent
	if before.IsZero() {
		return result
	}
	for _, sample := range samples {
		if sample.at.After(before) {
			break
		}
		result = sample.usage
	}
	return result
}

func cumulativeUsageAt(samples []usageSample, at time.Time) (processdomain.CodexUsageContent, processdomain.CodexUsageContent) {
	var result processdomain.CodexUsageContent
	var current processdomain.CodexUsageContent
	for _, sample := range samples {
		if !at.IsZero() && sample.at.After(at) {
			break
		}
		result = sample.usage
		current = sample.usage
	}
	return result, current
}

func usageDelta(start, end, current processdomain.CodexUsageContent) TokenUsageDTO {
	return TokenUsageDTO{
		InputTokens:                  nonNegative(end.InputTokens - start.InputTokens),
		CachedInputTokens:            nonNegative(end.CachedInputTokens - start.CachedInputTokens),
		OutputTokens:                 nonNegative(end.OutputTokens - start.OutputTokens),
		ReasoningOutputTokens:        nonNegative(end.ReasoningOutputTokens - start.ReasoningOutputTokens),
		TotalTokens:                  nonNegative(end.TotalTokens - start.TotalTokens),
		ContextWindow:                end.ContextWindow,
		CurrentInputTokens:           current.CurrentInputTokens,
		CurrentCachedInputTokens:     current.CurrentCachedInputTokens,
		CurrentOutputTokens:          current.CurrentOutputTokens,
		CurrentReasoningOutputTokens: current.CurrentReasoningOutputTokens,
		CurrentTotalTokens:           current.CurrentTotalTokens,
	}
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func countTimesInWindow(times []time.Time, start, end time.Time) int {
	count := 0
	for _, value := range times {
		if (!start.IsZero() && !value.After(start)) || (!end.IsZero() && value.After(end)) {
			continue
		}
		count++
	}
	return count
}

func addUsage(left, right TokenUsageDTO) TokenUsageDTO {
	left.InputTokens += right.InputTokens
	left.CachedInputTokens += right.CachedInputTokens
	left.OutputTokens += right.OutputTokens
	left.ReasoningOutputTokens += right.ReasoningOutputTokens
	left.TotalTokens += right.TotalTokens
	left.CompactionCount += right.CompactionCount
	left.CurrentInputTokens = right.CurrentInputTokens
	left.CurrentCachedInputTokens = right.CurrentCachedInputTokens
	left.CurrentOutputTokens = right.CurrentOutputTokens
	left.CurrentReasoningOutputTokens = right.CurrentReasoningOutputTokens
	left.CurrentTotalTokens = right.CurrentTotalTokens
	left.ContextWindow = right.ContextWindow
	return left
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
	if usage, ok := event.Content.(processdomain.CodexUsageContent); ok {
		return DTO{ID: eventdomain.ID(canonicalID), Type: event.Type, Usage: usageDTO(usage), OccurredAt: event.CreatedAt.UTC().Format(time.RFC3339Nano)}, true
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
	if eventType == "session.todo_list_updated" {
		return false
	}
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
