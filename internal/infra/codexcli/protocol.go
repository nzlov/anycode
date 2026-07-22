package codexcli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

type appServerTurn struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	Items       []json.RawMessage `json:"items"`
	StartedAt   *int64            `json:"startedAt"`
	CompletedAt *int64            `json:"completedAt"`
	Error       map[string]any    `json:"error"`
}

func (r *appServerRuntime) handleNotification(method string, raw json.RawMessage) {
	var identity struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
	}
	if json.Unmarshal(raw, &identity) != nil || identity.ThreadID == "" {
		return
	}
	route := r.routeForThread(identity.ThreadID)
	if route == nil {
		return
	}
	if route.activeTurnID() == "" && identity.TurnID != "" {
		route.setTurnID(identity.TurnID)
	}
	switch method {
	case "item/started", "item/completed":
		var params struct {
			Item          json.RawMessage `json:"item"`
			StartedAtMS   int64           `json:"startedAtMs"`
			CompletedAtMS int64           `json:"completedAtMs"`
			TurnID        string          `json:"turnId"`
		}
		if json.Unmarshal(raw, &params) != nil {
			return
		}
		phase := process.CodexPhaseStarted
		timestamp := params.StartedAtMS
		if method == "item/completed" {
			phase = process.CodexPhaseCompleted
			timestamp = params.CompletedAtMS
		}
		if event, ok := eventFromThreadItem(identity.ThreadID, params.TurnID, params.Item, phase, millisTime(timestamp)); ok {
			route.emit(event)
		}
	case "item/agentMessage/delta":
		route.emit(deltaEvent(raw, identity, process.CodexEventMessage, process.CodexMessageContent{Role: "assistant", Format: process.CodexTextMarkdown}))
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		route.emit(deltaEvent(raw, identity, process.CodexEventReasoning, process.CodexReasoningContent{}))
	case "item/commandExecution/outputDelta":
		event := deltaEvent(raw, identity, process.CodexEventCommand, process.CodexCommandContent{Kind: process.CodexCommandExec})
		if content, ok := event.Content.(process.CodexCommandContent); ok {
			var params struct {
				Delta string `json:"delta"`
			}
			_ = json.Unmarshal(raw, &params)
			content.Commands = []process.CodexCommandInvocation{{HasOutput: true, Output: params.Delta}}
			event.Content = content
		}
		route.emit(event)
	case "item/fileChange/patchUpdated":
		var params struct {
			ItemID  string           `json:"itemId"`
			TurnID  string           `json:"turnId"`
			Changes []map[string]any `json:"changes"`
		}
		if json.Unmarshal(raw, &params) == nil {
			route.emit(process.CodexEvent{
				EventID: params.ItemID, Type: process.CodexEventFileChange, TurnID: params.TurnID, Phase: process.CodexPhaseProgress,
				Content: process.CodexFileChangeContent{Changes: fileChanges(params.Changes)}, CreatedAt: time.Now(),
			})
		}
	case "turn/plan/updated":
		var params struct {
			TurnID string `json:"turnId"`
			Plan   []struct {
				Step   string `json:"step"`
				Status string `json:"status"`
			} `json:"plan"`
		}
		if json.Unmarshal(raw, &params) == nil {
			items := make([]process.PlanItem, 0, len(params.Plan))
			for _, item := range params.Plan {
				items = append(items, process.PlanItem{Step: item.Step, Status: process.PlanItemStatus(item.Status)})
			}
			route.emit(process.CodexEvent{
				EventID: "plan:" + params.TurnID, Type: process.CodexEventPlan, TurnID: params.TurnID,
				Phase: process.CodexPhaseProgress, Content: process.PlanUpdate{EventID: "plan:" + params.TurnID, Items: items}, CreatedAt: time.Now(),
			})
		}
	case "thread/tokenUsage/updated":
		if event, ok := usageEvent(raw, identity); ok {
			route.emit(event)
		}
	case "thread/compacted":
		route.emit(process.CodexEvent{
			EventID: "compacted:" + identity.ThreadID, Type: process.CodexEventStatus, TurnID: identity.TurnID,
			Phase: process.CodexPhaseCompleted, Content: process.CodexStatusContent{Code: "context.compacted", Level: "info", Message: "Context compacted"}, CreatedAt: time.Now(),
		})
	case "turn/completed":
		r.completeTurn(route, raw)
	case "error":
		var params struct {
			Message   string `json:"message"`
			WillRetry bool   `json:"willRetry"`
		}
		if json.Unmarshal(raw, &params) == nil {
			route.emit(process.CodexEvent{
				EventID: "error:" + identity.TurnID, Type: process.CodexEventStatus, TurnID: identity.TurnID,
				Phase: process.CodexPhaseFailed, Content: process.CodexStatusContent{Code: "codex.error", Level: "error", Message: params.Message, Details: map[string]any{"willRetry": params.WillRetry}}, CreatedAt: time.Now(),
			})
		}
	}
}

func (r *appServerRuntime) completeTurn(route *appServerRun, raw json.RawMessage) {
	var params struct {
		Turn appServerTurn `json:"turn"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return
	}
	finishedAt := time.Now()
	if params.Turn.CompletedAt != nil {
		finishedAt = time.Unix(*params.Turn.CompletedAt, 0)
	}
	phase := process.CodexPhaseCompleted
	level := "info"
	failureCode := ""
	failureReason := ""
	switch params.Turn.Status {
	case "failed":
		phase = process.CodexPhaseFailed
		level = "error"
		failureCode = "turn_failed"
		failureReason = stringValue(params.Turn.Error, "message")
		if failureReason == "" {
			failureReason = "Codex turn failed"
		}
	case "interrupted":
		phase = process.CodexPhaseCancelled
	}
	route.emit(process.CodexEvent{
		EventID: "turn:" + params.Turn.ID, Type: process.CodexEventStatus, TurnID: params.Turn.ID, Phase: phase,
		Content: process.CodexStatusContent{Code: "turn.completed", Level: level, Message: params.Turn.Status}, CreatedAt: finishedAt,
	})
	result := process.ExitResult{FailureCode: failureCode, FailureReason: failureReason, FinishedAt: finishedAt}
	route.emit(process.CodexEvent{EventID: "exit:" + params.Turn.ID, Type: process.CodexEventProcessExit, TurnID: params.Turn.ID, Content: result, CreatedAt: finishedAt})
	r.completeRoute(route)
}

func deltaEvent(raw json.RawMessage, identity struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
}, eventType process.CodexEventType, content process.CodexEventContent) process.CodexEvent {
	var params struct {
		Delta string `json:"delta"`
	}
	_ = json.Unmarshal(raw, &params)
	switch value := content.(type) {
	case process.CodexMessageContent:
		value.Text = params.Delta
		content = value
	case process.CodexReasoningContent:
		value.Text = params.Delta
		content = value
	}
	return process.CodexEvent{EventID: identity.ItemID, Type: eventType, TurnID: identity.TurnID, Phase: process.CodexPhaseProgress, Content: content, CreatedAt: time.Now()}
}

func usageEvent(raw json.RawMessage, identity struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
}) (process.CodexEvent, bool) {
	var params struct {
		TokenUsage struct {
			Total              usageBreakdown `json:"total"`
			Last               usageBreakdown `json:"last"`
			ModelContextWindow *int           `json:"modelContextWindow"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return process.CodexEvent{}, false
	}
	contextWindow := 0
	if params.TokenUsage.ModelContextWindow != nil {
		contextWindow = *params.TokenUsage.ModelContextWindow
	}
	return process.CodexEvent{
		EventID: "usage:" + identity.TurnID, Type: process.CodexEventUsage, TurnID: identity.TurnID, Phase: process.CodexPhaseProgress,
		Content: process.CodexUsageContent{
			InputTokens: params.TokenUsage.Total.InputTokens, CachedInputTokens: params.TokenUsage.Total.CachedInputTokens,
			OutputTokens: params.TokenUsage.Total.OutputTokens, ReasoningOutputTokens: params.TokenUsage.Total.ReasoningOutputTokens,
			TotalTokens: params.TokenUsage.Total.TotalTokens, ContextWindow: contextWindow,
			CurrentInputTokens: params.TokenUsage.Last.InputTokens, CurrentCachedInputTokens: params.TokenUsage.Last.CachedInputTokens,
			CurrentOutputTokens: params.TokenUsage.Last.OutputTokens, CurrentReasoningOutputTokens: params.TokenUsage.Last.ReasoningOutputTokens,
			CurrentTotalTokens: params.TokenUsage.Last.TotalTokens,
		}, CreatedAt: time.Now(),
	}, true
}

type usageBreakdown struct {
	InputTokens           int `json:"inputTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	OutputTokens          int `json:"outputTokens"`
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
	TotalTokens           int `json:"totalTokens"`
}

func eventsFromTurns(threadID string, turns []appServerTurn) []process.CodexEvent {
	events := make([]process.CodexEvent, 0)
	var sequence int64
	for turnIndex := len(turns) - 1; turnIndex >= 0; turnIndex-- {
		turn := turns[turnIndex]
		createdAt := time.Time{}
		if turn.StartedAt != nil {
			createdAt = time.Unix(*turn.StartedAt, 0)
		}
		for itemIndex, raw := range turn.Items {
			sequence++
			itemTime := createdAt
			if !itemTime.IsZero() {
				itemTime = itemTime.Add(time.Duration(itemIndex) * time.Microsecond)
			}
			if event, ok := eventFromThreadItem(threadID, turn.ID, raw, process.CodexPhaseCompleted, itemTime); ok {
				event.Sequence = sequence
				events = append(events, event)
			}
		}
	}
	return events
}

func eventFromThreadItem(threadID string, turnID string, raw json.RawMessage, fallbackPhase process.CodexPhase, createdAt time.Time) (process.CodexEvent, bool) {
	var item map[string]any
	if json.Unmarshal(raw, &item) != nil {
		return process.CodexEvent{}, false
	}
	id := stringValue(item, "id")
	typeName := stringValue(item, "type")
	event := process.CodexEvent{EventID: id, CodexSessionID: threadID, TurnID: turnID, Phase: itemPhase(item, fallbackPhase), CreatedAt: createdAt}
	switch typeName {
	case "userMessage":
		event.Type = process.CodexEventMessage
		event.Content = process.CodexMessageContent{Role: "user", Text: userInputText(item["content"]), Format: process.CodexTextMarkdown}
	case "agentMessage":
		event.Type = process.CodexEventMessage
		event.Content = process.CodexMessageContent{Role: "assistant", Text: stringValue(item, "text"), Format: process.CodexTextMarkdown}
	case "reasoning":
		event.Type = process.CodexEventReasoning
		text := strings.Join(stringSlice(item["summary"]), "\n")
		if text == "" {
			text = strings.Join(stringSlice(item["content"]), "\n")
		}
		event.Content = process.CodexReasoningContent{Text: text}
	case "commandExecution":
		event.Type = process.CodexEventCommand
		exitCode := optionalInt(item["exitCode"])
		duration := optionalInt(item["durationMs"])
		output := stringValue(item, "aggregatedOutput")
		event.Content = process.CodexCommandContent{Kind: process.CodexCommandExec, DurationMS: duration, Commands: []process.CodexCommandInvocation{{
			Command: stringValue(item, "command"), Workdir: stringValue(item, "cwd"), HasOutput: output != "", Output: output, ExitCode: exitCode, DurationMS: duration,
		}}}
	case "fileChange":
		event.Type = process.CodexEventFileChange
		event.Content = process.CodexFileChangeContent{Changes: fileChanges(mapSlice(item["changes"]))}
	case "dynamicToolCall":
		event.Type = process.CodexEventTool
		event.CorrelationID = id
		event.Content = process.CodexToolContent{
			QualifiedName: stringValue(item, "tool"), Category: "dynamic",
			Input:  process.CodexStructuredText{Format: process.CodexTextJSON, Text: jsonText(item["arguments"])},
			Output: process.CodexStructuredText{Format: process.CodexTextJSON, Text: jsonText(item["contentItems"])},
		}
	case "mcpToolCall":
		event.Type = process.CodexEventTool
		event.CorrelationID = id
		event.Content = process.CodexToolContent{
			QualifiedName: strings.Trim(strings.Join([]string{stringValue(item, "server"), stringValue(item, "tool")}, "/"), "/"), Category: "mcp",
			Input:  process.CodexStructuredText{Format: process.CodexTextJSON, Text: jsonText(item["arguments"])},
			Output: process.CodexStructuredText{Format: process.CodexTextJSON, Text: jsonText(item["result"])},
		}
	case "plan":
		event.Type = process.CodexEventPlan
		event.Content = process.PlanUpdate{EventID: id}
	case "contextCompaction":
		event.Type = process.CodexEventStatus
		event.Content = process.CodexStatusContent{Code: "context.compacted", Level: "info", Message: "Context compacted"}
	case "webSearch", "imageView", "imageGeneration", "collabAgentToolCall", "subAgentActivity", "sleep":
		event.Type = process.CodexEventTool
		event.CorrelationID = id
		event.Content = process.CodexToolContent{
			QualifiedName: typeName, Category: typeName,
			Input:  process.CodexStructuredText{Format: process.CodexTextJSON, Text: jsonText(item)},
			Output: process.CodexStructuredText{Format: process.CodexTextPlain, Text: firstNonEmpty(stringValue(item, "result"), stringValue(item, "path"))},
		}
	case "enteredReviewMode", "exitedReviewMode", "hookPrompt":
		event.Type = process.CodexEventStatus
		event.Content = process.CodexStatusContent{Code: "codex." + typeName, Level: "info", Message: firstNonEmpty(stringValue(item, "review"), typeName)}
	default:
		event.Type = process.CodexEventUnknown
		event.Content = process.CodexUnknownContent{RawType: typeName, Payload: item}
	}
	return event, event.Content != nil
}

func itemPhase(item map[string]any, fallback process.CodexPhase) process.CodexPhase {
	switch strings.ToLower(stringValue(item, "status")) {
	case "failed", "error":
		return process.CodexPhaseFailed
	case "declined", "cancelled", "canceled", "interrupted":
		return process.CodexPhaseCancelled
	case "inprogress", "in_progress", "running":
		if fallback == process.CodexPhaseCompleted {
			return process.CodexPhaseProgress
		}
		return fallback
	case "completed", "success":
		return process.CodexPhaseCompleted
	default:
		return fallback
	}
}

func fileChanges(values []map[string]any) []process.CodexFileChange {
	result := make([]process.CodexFileChange, 0, len(values))
	for _, value := range values {
		result = append(result, process.CodexFileChange{Kind: stringValue(value, "kind"), Path: stringValue(value, "path"), UnifiedDiff: stringValue(value, "diff")})
	}
	return result
}

func userInputText(value any) string {
	var parts []string
	for _, item := range mapSlice(value) {
		if text := stringValue(item, "text"); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func mapSlice(value any) []map[string]any {
	items, _ := value.([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped, ok := item.(map[string]any); ok {
			result = append(result, mapped)
		}
	}
	return result
}

func stringSlice(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func stringValue(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}

func optionalInt(value any) *int {
	if value == nil {
		return nil
	}
	var result int
	switch number := value.(type) {
	case float64:
		result = int(number)
	case int:
		result = number
	default:
		return nil
	}
	return &result
}

func jsonText(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func millisTime(value int64) time.Time {
	if value <= 0 {
		return time.Now()
	}
	return time.UnixMilli(value)
}
