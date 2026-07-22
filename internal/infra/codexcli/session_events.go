package codexcli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

func planUpdateFromEvent(eventType string, payload map[string]any) (process.PlanUpdate, string, bool) {
	normalizedType := strings.ToLower(strings.TrimSpace(eventType))
	if normalizedType == "plan_update" || normalizedType == "turn/plan/updated" || normalizedType == "turn.plan.updated" || normalizedType == "plan.updated" {
		if update, ok := planUpdateFromPayload(payload); ok {
			return update, planUpdateCorrelationID(payload), true
		}
	}
	item := mapValue(payload["item"])
	if (normalizedType == "item.started" || normalizedType == "item.updated") && strings.EqualFold(strings.TrimSpace(stringValue(item, "type")), "todo_list") {
		if update, ok := planUpdateFromPayload(item); ok {
			return update, planUpdateCorrelationID(payload), true
		}
	}
	if update, correlationID, ok := planUpdateFromToolPayload(payload); ok {
		return update, correlationID, true
	}
	return process.PlanUpdate{}, "", false
}

func planUpdateFromToolPayload(payload map[string]any) (process.PlanUpdate, string, bool) {
	if isUpdatePlanTool(payload) {
		for _, key := range []string{"arguments", "input"} {
			switch value := payload[key].(type) {
			case string:
				var arguments map[string]any
				if json.Unmarshal([]byte(value), &arguments) == nil {
					if update, ok := planUpdateFromPayload(arguments); ok {
						return update, planUpdateCorrelationID(payload), true
					}
				}
			case map[string]any:
				if update, ok := planUpdateFromPayload(value); ok {
					return update, planUpdateCorrelationID(payload), true
				}
			}
		}
		if update, ok := planUpdateFromPayload(payload); ok {
			return update, planUpdateCorrelationID(payload), true
		}
	}
	for _, key := range []string{"item", "msg", "message", "params"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if update, correlationID, found := planUpdateFromToolPayload(nested); found {
				if correlationID == "" {
					correlationID = planUpdateCorrelationID(payload)
				}
				return update, correlationID, true
			}
		}
	}
	return process.PlanUpdate{}, "", false
}

func isUpdatePlanTool(payload map[string]any) bool {
	name := stringValue(payload, "name", "tool", "tool_name", "toolName", "function_name", "functionName")
	if name == "" {
		name = stringValue(mapValue(payload["function"]), "name")
	}
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "update_plan" || strings.HasSuffix(name, ".update_plan")
}

func planUpdateFromPayload(payload map[string]any) (process.PlanUpdate, bool) {
	for _, key := range []string{"plan", "todoList", "todo_list", "todos", "items"} {
		if items, ok := payload[key].([]any); ok {
			return planUpdateFromItems(items), true
		}
	}
	for _, key := range []string{"item", "msg", "message", "params"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if update, found := planUpdateFromPayload(nested); found {
				return update, true
			}
		}
	}
	return process.PlanUpdate{}, false
}

func planUpdateFromItems(items []any) process.PlanUpdate {
	update := process.PlanUpdate{Items: make([]process.PlanItem, 0, len(items))}
	for _, value := range items {
		item := mapValue(value)
		step := strings.TrimSpace(stringValue(item, "step", "text", "title", "content"))
		if step == "" {
			continue
		}
		update.Items = append(update.Items, process.PlanItem{
			Step:   step,
			Status: planItemStatus(item),
		})
	}
	return update
}

func planItemStatus(item map[string]any) process.PlanItemStatus {
	if completed, ok := item["completed"].(bool); ok {
		if completed {
			return process.PlanItemCompleted
		}
		return process.PlanItemPending
	}
	switch strings.ToLower(strings.TrimSpace(stringValue(item, "status"))) {
	case "complete", "completed", "done", "success", "succeeded":
		return process.PlanItemCompleted
	case "in_progress", "in-progress", "progress", "running", "started":
		return process.PlanItemInProgress
	default:
		return process.PlanItemPending
	}
}

func planUpdateCorrelationID(payload map[string]any) string {
	item := mapValue(payload["item"])
	return firstString(
		item["call_id"], item["callId"], item["id"], item["item_id"], item["itemId"],
		payload["call_id"], payload["callId"], payload["id"], payload["item_id"], payload["itemId"],
	)
}

func stablePlanUpdateEventID(update process.PlanUpdate) string {
	type planItemIdentity struct {
		Step   string
		Status process.PlanItemStatus
	}
	identity := make([]planItemIdentity, 0, len(update.Items))
	for _, item := range update.Items {
		identity = append(identity, planItemIdentity{
			Step:   item.Step,
			Status: item.Status,
		})
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("plan:%x", digest[:16])
}

type codexLogEvent struct {
	EventID       string
	Type          string
	Payload       map[string]any
	PlanUpdate    *process.PlanUpdate
	CorrelationID string
	Phase         process.CodexPhase
	Content       process.CodexEventContent
	SourceOffset  int64
	SourceIndex   int
	CreatedAt     time.Time
}

func canonicalCodexEvent(event codexLogEvent) process.CodexEvent {
	content := event.Content
	eventType := process.CodexEventUnknown
	eventID := event.EventID
	if event.PlanUpdate != nil {
		content = *event.PlanUpdate
		eventType = process.CodexEventPlan
		eventID = event.PlanUpdate.EventID
	} else {
		switch content.(type) {
		case process.CodexMessageContent:
			eventType = process.CodexEventMessage
		case process.CodexReasoningContent:
			eventType = process.CodexEventReasoning
		case process.CodexCommandContent:
			eventType = process.CodexEventCommand
		case process.CodexToolContent:
			eventType = process.CodexEventTool
		case process.CodexFileChangeContent:
			eventType = process.CodexEventFileChange
		case process.CodexUsageContent:
			eventType = process.CodexEventUsage
		case process.CodexStatusContent:
			eventType = process.CodexEventStatus
		}
	}
	return process.CodexEvent{
		EventID: eventID, Type: eventType, CorrelationID: event.CorrelationID,
		Phase: event.Phase, Content: content, CreatedAt: event.CreatedAt,
	}
}

func parseSessionLogLine(raw []byte, sessionCWD string, sourceID string, offset int64) []codexLogEvent {
	var record struct {
		Timestamp string         `json:"timestamp"`
		Type      string         `json:"type"`
		Payload   map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return finalizeCodexEvents([]codexLogEvent{{
			EventID: sourceEventID("invalid_json", sourceID, offset),
			Type:    "invalid_json",
			Payload: map[string]any{"error": err.Error(), "byteCount": len(raw)},
		}}, sourceID, offset)
	}
	payload := payloadOrEmpty(record.Payload)
	_, hadEncryptedContent := payload["encrypted_content"]
	delete(payload, "encrypted_content")
	createdAt := parseSessionTimestamp(record.Timestamp)
	var events []codexLogEvent
	switch record.Type {
	case "session_meta":
		threadID := stringValue(payload, "session_id", "id")
		events = []codexLogEvent{{
			EventID:   eventID(record.Timestamp, "thread.started", threadID),
			Type:      "thread.started",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "response_item":
		if hadEncryptedContent && stringValue(payload, "type") == "reasoning" && reasoningText(payload) == "" {
			return nil
		}
		events = codexEventsFromResponseItem(record.Timestamp, payload, createdAt)
	case "event_msg":
		events = codexEventsFromEventMessage(record.Timestamp, payload, createdAt, sessionCWD)
	case "agent_message":
		normalized := normalizedItem("agent_message", "completed")
		normalized["output"] = firstString(payload["message"], payload["text"], payload["output"], messageText(payload))
		events = []codexLogEvent{{
			EventID:   eventID(record.Timestamp, "item.completed", stringValue(payload, "id", "event_id")),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "compacted":
		events = []codexLogEvent{{
			Type:      "context.compacted",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "turn_context":
		events = []codexLogEvent{{
			Type:      "turn.context",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "world_state":
		events = []codexLogEvent{{
			Type:      "world.state",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	default:
		if record.Type == "" {
			return nil
		}
		events = []codexLogEvent{{
			Type:      record.Type,
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	}
	return finalizeCodexEvents(events, sourceID, offset)
}

func finalizeCodexEvents(events []codexLogEvent, sourceID string, offset int64) []codexLogEvent {
	for index := range events {
		if events[index].EventID == "" {
			events[index].EventID = sourceEventID(events[index].Type, sourceID, offset)
		}
		events[index].SourceOffset = offset
		events[index].SourceIndex = index
		applyCodexSemantic(&events[index])
	}
	return events
}

type codexTranscriptProjector struct {
	commands        map[string]process.CodexCommandContent
	tools           map[string]process.CodexToolContent
	recentCanonical []transcriptCanonicalMessage
	pendingMessages []pendingTranscriptMessage
	bufferedEvents  []codexLogEvent
	visibility      standardTranscriptVisibility
	lastOccurred    time.Time
}

const transcriptMessageMirrorWindow = 100 * time.Millisecond

type transcriptCanonicalMessage struct {
	signature  string
	occurredAt time.Time
}

type pendingTranscriptMessage struct {
	event      codexLogEvent
	signature  string
	historical bool
	pendingAt  time.Time
}

func newCodexTranscriptProjector() *codexTranscriptProjector {
	return &codexTranscriptProjector{
		commands:   map[string]process.CodexCommandContent{},
		tools:      map[string]process.CodexToolContent{},
		visibility: newStandardTranscriptVisibility(),
	}
}

func (p *codexTranscriptProjector) project(events []codexLogEvent) []codexLogEvent {
	return p.projectEvents(events, false)
}

func (p *codexTranscriptProjector) prime(events []codexLogEvent) {
	p.projectEvents(events, true)
}

func (p *codexTranscriptProjector) projectEvents(events []codexLogEvent, historical bool) []codexLogEvent {
	if p == nil {
		if historical {
			return nil
		}
		return events
	}
	projected := make([]codexLogEvent, 0, len(events))
	for index := range events {
		event := &events[index]
		p.fillOccurredAt(event)
		p.mergeCommandState(event)
		p.pruneCanonicalMessages(event.CreatedAt)
		projected = append(projected, p.expirePendingMessages(event.CreatedAt)...)
		visible := p.visibility.visible(*event)
		if !visible {
			continue
		}
		canonicalSignature, canonical := canonicalMessageSignature(*event)
		if canonical {
			if pending, matched := p.consumePendingMessage(canonicalSignature, event.CreatedAt); matched {
				if !historical && !pending.historical {
					p.removeBufferedEvent(pending.event.EventID)
					p.bufferedEvents = append(p.bufferedEvents, *event)
				}
				projected = append(projected, p.releaseReadyBuffered()...)
				continue
			}
			p.recentCanonical = append(p.recentCanonical, transcriptCanonicalMessage{
				signature:  canonicalSignature,
				occurredAt: event.CreatedAt,
			})
			projected = append(projected, p.queueVisibleEvent(*event, historical)...)
			continue
		}
		if signature, mirror := eventMessageMirrorSignature(*event); mirror {
			if p.consumeCanonicalMessage(signature, event.CreatedAt) {
				continue
			}
			pendingAt := time.Time{}
			if !historical {
				pendingAt = time.Now()
			}
			p.pendingMessages = append(p.pendingMessages, pendingTranscriptMessage{
				event:      *event,
				signature:  signature,
				historical: historical,
				pendingAt:  pendingAt,
			})
			projected = append(projected, p.queueVisibleEvent(*event, historical)...)
			continue
		}
		projected = append(projected, p.queueVisibleEvent(*event, historical)...)
	}
	return projected
}

func (p *codexTranscriptProjector) fillOccurredAt(event *codexLogEvent) {
	if event.CreatedAt.IsZero() {
		if p.lastOccurred.IsZero() {
			event.CreatedAt = time.Unix(0, event.SourceOffset+int64(event.SourceIndex)+1).UTC()
		} else {
			event.CreatedAt = p.lastOccurred
		}
	}
	p.lastOccurred = event.CreatedAt
}

func (p *codexTranscriptProjector) mergeCommandState(event *codexLogEvent) {
	if event.CorrelationID == "" {
		return
	}
	if command, ok := event.Content.(process.CodexCommandContent); ok {
		if previous, exists := p.commands[event.CorrelationID]; exists && len(command.Commands) == 0 {
			command.Commands = append([]process.CodexCommandInvocation(nil), previous.Commands...)
		}
		event.Content = command
		if event.Phase == process.CodexPhaseStarted || event.Phase == process.CodexPhaseProgress {
			p.commands[event.CorrelationID] = cloneCommandContent(command)
		} else {
			delete(p.commands, event.CorrelationID)
		}
		return
	}
	if tool, ok := event.Content.(process.CodexToolContent); ok {
		if command, exists := p.commands[event.CorrelationID]; exists {
			command = cloneCommandContent(command)
			item := mapValue(event.Payload["item"])
			normalized := mapValue(event.Payload["normalizedItem"])
			command.DurationMS = intPointer(normalized["durationMs"], item["duration_ms"], item["durationMs"])
			results := commandOutputsFromValue(normalized["commandOutputs"])
			if len(results) == len(command.Commands) {
				for index := range command.Commands {
					applyCommandOutput(&command.Commands[index], results[index])
				}
				if len(command.Commands) == 1 && command.Commands[0].DurationMS == nil {
					command.Commands[0].DurationMS = command.DurationMS
				}
			} else if len(command.Commands) == 1 {
				command.Commands[0].HasOutput = true
				command.Commands[0].Output = normalizeANSIText(tool.Output.Text)
				command.Commands[0].ExitCode = intPointer(normalized["exitCode"], item["exit_code"], item["exitCode"])
				command.Commands[0].DurationMS = command.DurationMS
			}
			if commandInvocationFailed(command.Commands) && event.Phase == process.CodexPhaseCompleted {
				event.Phase = process.CodexPhaseFailed
			}
			event.Content = command
			if isTerminalCodexPhase(event.Phase) {
				delete(p.commands, event.CorrelationID)
			} else {
				p.commands[event.CorrelationID] = cloneCommandContent(command)
			}
			return
		}
		if previous, exists := p.tools[event.CorrelationID]; exists {
			if tool.QualifiedName == "" {
				tool.QualifiedName = previous.QualifiedName
			}
			if tool.Category == "" || tool.Category == "generic" {
				tool.Category = previous.Category
			}
			if tool.Input.Text == "" {
				tool.Input = previous.Input
			}
			if len(tool.Images) == 0 {
				tool.Images = previous.Images
			}
		}
		event.Content = tool
		if isTerminalCodexPhase(event.Phase) {
			delete(p.tools, event.CorrelationID)
		} else {
			p.tools[event.CorrelationID] = tool
		}
	}
}

func cloneCommandContent(command process.CodexCommandContent) process.CodexCommandContent {
	command.Commands = append([]process.CodexCommandInvocation(nil), command.Commands...)
	return command
}

func (p *codexTranscriptProjector) flushExpiredPending(now time.Time) []codexLogEvent {
	if p == nil || len(p.pendingMessages) == 0 {
		return nil
	}
	pending := p.pendingMessages[:0]
	for _, message := range p.pendingMessages {
		if message.historical || message.pendingAt.IsZero() || now.Sub(message.pendingAt) < transcriptMessageMirrorWindow {
			pending = append(pending, message)
		}
	}
	p.pendingMessages = pending
	return p.releaseReadyBuffered()
}

func (p *codexTranscriptProjector) flushPending() []codexLogEvent {
	if p == nil {
		return nil
	}
	p.pendingMessages = nil
	buffered := p.bufferedEvents
	p.bufferedEvents = nil
	return buffered
}

func (p *codexTranscriptProjector) expirePendingMessages(occurredAt time.Time) []codexLogEvent {
	pending := p.pendingMessages[:0]
	for _, message := range p.pendingMessages {
		if occurredAt.Sub(message.event.CreatedAt) <= transcriptMessageMirrorWindow {
			pending = append(pending, message)
		}
	}
	p.pendingMessages = pending
	return p.releaseReadyBuffered()
}

func (p *codexTranscriptProjector) consumePendingMessage(signature string, occurredAt time.Time) (pendingTranscriptMessage, bool) {
	matchedIndex := -1
	matchedDelta := time.Duration(0)
	for index, pending := range p.pendingMessages {
		if pending.signature != signature {
			continue
		}
		delta := occurredAt.Sub(pending.event.CreatedAt)
		if delta < 0 || delta > transcriptMessageMirrorWindow {
			continue
		}
		if matchedIndex == -1 || delta < matchedDelta {
			matchedIndex = index
			matchedDelta = delta
		}
	}
	if matchedIndex == -1 {
		return pendingTranscriptMessage{}, false
	}
	matched := p.pendingMessages[matchedIndex]
	p.pendingMessages = append(p.pendingMessages[:matchedIndex], p.pendingMessages[matchedIndex+1:]...)
	return matched, true
}

func (p *codexTranscriptProjector) queueVisibleEvent(event codexLogEvent, historical bool) []codexLogEvent {
	if historical {
		return nil
	}
	p.bufferedEvents = append(p.bufferedEvents, event)
	return p.releaseReadyBuffered()
}

func (p *codexTranscriptProjector) releaseReadyBuffered() []codexLogEvent {
	if len(p.bufferedEvents) == 0 {
		return nil
	}
	pendingEventIDs := make(map[string]struct{}, len(p.pendingMessages))
	for _, pending := range p.pendingMessages {
		if !pending.historical {
			pendingEventIDs[pending.event.EventID] = struct{}{}
		}
	}
	releaseCount := len(p.bufferedEvents)
	for index, event := range p.bufferedEvents {
		if _, pending := pendingEventIDs[event.EventID]; pending {
			releaseCount = index
			break
		}
	}
	if releaseCount == 0 {
		return nil
	}
	ready := append([]codexLogEvent(nil), p.bufferedEvents[:releaseCount]...)
	p.bufferedEvents = append(p.bufferedEvents[:0], p.bufferedEvents[releaseCount:]...)
	return ready
}

func (p *codexTranscriptProjector) removeBufferedEvent(eventID string) {
	for index, event := range p.bufferedEvents {
		if event.EventID == eventID {
			p.bufferedEvents = append(p.bufferedEvents[:index], p.bufferedEvents[index+1:]...)
			return
		}
	}
}

func (p *codexTranscriptProjector) consumeCanonicalMessage(signature string, occurredAt time.Time) bool {
	for index, canonical := range p.recentCanonical {
		if canonical.signature != signature {
			continue
		}
		delta := occurredAt.Sub(canonical.occurredAt)
		if delta < 0 || delta > transcriptMessageMirrorWindow {
			continue
		}
		p.recentCanonical = append(p.recentCanonical[:index], p.recentCanonical[index+1:]...)
		return true
	}
	return false
}

func (p *codexTranscriptProjector) pruneCanonicalMessages(occurredAt time.Time) {
	recent := p.recentCanonical[:0]
	for _, canonical := range p.recentCanonical {
		delta := occurredAt.Sub(canonical.occurredAt)
		if delta < 0 || delta <= transcriptMessageMirrorWindow {
			recent = append(recent, canonical)
		}
	}
	p.recentCanonical = recent
}

func messageSignature(event codexLogEvent) (string, bool) {
	message, ok := event.Content.(process.CodexMessageContent)
	if !ok || message.Text == "" {
		return "", false
	}
	return message.Role + "\x00" + message.Text, true
}

func eventMessageMirrorSignature(event codexLogEvent) (string, bool) {
	if !eventMessageMirrorCandidate(event) {
		return "", false
	}
	return messageSignature(event)
}

func canonicalMessageSignature(event codexLogEvent) (string, bool) {
	item := mapValue(event.Payload["item"])
	if event.Type != "item.completed" || stringValue(item, "type") != "message" || stringValue(item, "role") != "assistant" {
		return "", false
	}
	return messageSignature(event)
}

func eventMessageMirrorCandidate(event codexLogEvent) bool {
	item := mapValue(event.Payload["item"])
	normalized := mapValue(event.Payload["normalizedItem"])
	return event.Type == "item.completed" &&
		stringValue(normalized, "type") == "agent_message" &&
		stringValue(item, "type") == "agent_message" &&
		stringValue(item, "message") != ""
}

func isTerminalCodexPhase(phase process.CodexPhase) bool {
	return phase == process.CodexPhaseCompleted || phase == process.CodexPhaseFailed || phase == process.CodexPhaseCancelled
}

type standardTranscriptVisibility struct {
	hiddenToolCalls map[string]struct{}
}

func newStandardTranscriptVisibility() standardTranscriptVisibility {
	return standardTranscriptVisibility{hiddenToolCalls: map[string]struct{}{}}
}

func (v standardTranscriptVisibility) visible(event codexLogEvent) bool {
	if event.CorrelationID == "" {
		return true
	}
	tool, ok := event.Content.(process.CodexToolContent)
	if !ok {
		return true
	}
	if isInternalTranscriptTool(tool.QualifiedName) {
		if event.Phase == process.CodexPhaseStarted || event.Phase == process.CodexPhaseProgress {
			v.hiddenToolCalls[event.CorrelationID] = struct{}{}
		}
		if isTerminalCodexPhase(event.Phase) {
			delete(v.hiddenToolCalls, event.CorrelationID)
		}
		return false
	}
	if _, ok := v.hiddenToolCalls[event.CorrelationID]; ok {
		if isTerminalCodexPhase(event.Phase) {
			delete(v.hiddenToolCalls, event.CorrelationID)
		}
		return false
	}
	return true
}

func isInternalTranscriptTool(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return normalized == "apply_patch" || strings.HasSuffix(normalized, ".apply_patch")
}

func applyCodexSemantic(event *codexLogEvent) {
	if event == nil {
		return
	}
	event.Phase = process.CodexPhaseStandalone
	item := mapValue(event.Payload["item"])
	normalized := mapValue(event.Payload["normalizedItem"])
	itemType := stringValue(normalized, "type")
	if itemType == "" {
		itemType = stringValue(item, "type")
	}
	event.CorrelationID = codexCorrelationID(item, event.Payload)
	if update, correlationID, ok := planUpdateFromEvent(event.Type, event.Payload); ok {
		update.EventID = stablePlanUpdateEventID(update)
		event.PlanUpdate = &update
		if correlationID != "" {
			event.CorrelationID = correlationID
		}
	}

	if event.Type == "token_count" {
		event.Content = codexUsageContent(event.Payload)
		return
	}
	if event.Type == "item.started" || event.Type == "item.completed" {
		applyCodexItemSemantic(event, itemType, item, normalized)
		return
	}
	if event.Type == "mcp_tool_call_end" {
		result := mapValue(event.Payload["result"])
		okResult := mapValue(result["Ok"])
		event.Phase = mcpToolPhase(result)
		invocation := mapValue(event.Payload["invocation"])
		event.Content = process.CodexToolContent{
			QualifiedName: qualifiedInvocationName(invocation),
			Category:      "mcp",
			Output: process.CodexStructuredText{
				Format: process.CodexTextJSON,
				Text:   jsonText(event.Payload["result"]),
			},
			Images: codexImages(okResult),
		}
		return
	}
	if isCodexStatusType(event.Type) {
		event.Content = codexStatusContent(event.Type, event.Payload)
		return
	}
	event.Content = process.CodexUnknownContent{RawType: event.Type, Payload: cloneMap(event.Payload)}
}

func applyCodexItemSemantic(event *codexLogEvent, itemType string, item map[string]any, normalized map[string]any) {
	phase := codexItemPhase(event.Type, stringValue(normalized, "status"))
	output := firstString(normalized["output"], item["aggregated_output"], item["output"], item["text"])
	command := normalizeDisplayCommand(firstString(normalized["command"], item["command"]))
	name := firstString(normalized["qualifiedName"], item["name"])
	input := firstString(normalized["input"], item["input"])
	if name == "exec" {
		if nestedName := extractExecToolName(input); nestedName != "" {
			name = nestedName
		}
	}

	switch itemType {
	case "agent_message", "assistant_message":
		event.Phase = process.CodexPhaseStandalone
		event.Content = process.CodexMessageContent{Role: "assistant", Text: output, Format: process.CodexTextMarkdown, Images: codexImages(item)}
	case "user_message":
		event.Phase = process.CodexPhaseStandalone
		event.Content = process.CodexMessageContent{Role: "user", Text: output, Format: process.CodexTextPlain, Images: codexImages(item)}
	case "reasoning":
		event.Phase = process.CodexPhaseStandalone
		event.Content = process.CodexReasoningContent{Text: output}
	case "command_execution":
		event.Phase = phase
		kind := process.CodexCommandShell
		if stringValue(normalized, "commandKind") == string(process.CodexCommandExec) {
			kind = process.CodexCommandExec
		}
		commands := commandInvocationsFromValue(normalized["commands"])
		if len(commands) == 0 && command != "" {
			commands = []process.CodexCommandInvocation{{Command: command}}
		}
		for index := range commands {
			commands[index].Command = normalizeDisplayCommand(commands[index].Command)
		}
		durationMS := intPointer(normalized["durationMs"], item["duration_ms"], item["durationMs"])
		if len(commands) == 1 && event.Type == "item.completed" {
			commands[0].HasOutput = true
			commands[0].Output = normalizeANSIText(output)
			commands[0].ExitCode = intPointer(normalized["exitCode"], item["exit_code"], item["exitCode"])
			commands[0].DurationMS = durationMS
		}
		content := process.CodexCommandContent{Kind: kind, Commands: commands, DurationMS: durationMS}
		if commandInvocationFailed(commands) && event.Phase == process.CodexPhaseCompleted {
			event.Phase = process.CodexPhaseFailed
		}
		event.Content = content
	case "file_change":
		event.Phase = process.CodexPhaseStandalone
		event.Content = process.CodexFileChangeContent{Changes: codexFileChanges(normalized["changes"])}
	case "tool_call", "tool_result", "custom_tool_call", "tool_search", "web_search", "mcp_tool_call":
		event.Phase = phase
		event.Content = process.CodexToolContent{
			QualifiedName: name,
			Category:      codexToolCategory(itemType, name),
			Input:         structuredText(input, process.CodexTextPlain),
			Output:        structuredText(output, process.CodexTextPlain),
			Images:        codexImages(item),
		}
	default:
		event.Phase = phase
		event.Content = process.CodexUnknownContent{RawType: itemType, Payload: cloneMap(event.Payload)}
	}
}

func codexItemPhase(eventType string, status string) process.CodexPhase {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error":
		return process.CodexPhaseFailed
	case "cancelled", "canceled", "aborted":
		return process.CodexPhaseCancelled
	case "progress", "running":
		return process.CodexPhaseProgress
	case "in_progress", "started":
		return process.CodexPhaseStarted
	case "completed", "complete", "success", "succeeded":
		return process.CodexPhaseCompleted
	}
	if eventType == "item.started" {
		return process.CodexPhaseStarted
	}
	return process.CodexPhaseCompleted
}

func mcpToolPhase(value any) process.CodexPhase {
	result := mapValue(value)
	if result["Err"] != nil || result["err"] != nil {
		return process.CodexPhaseFailed
	}
	ok := mapValue(result["Ok"])
	if isError, _ := ok["isError"].(bool); isError {
		return process.CodexPhaseFailed
	}
	return process.CodexPhaseCompleted
}

func codexCorrelationID(item map[string]any, payload map[string]any) string {
	return firstString(
		item["call_id"], item["callId"], item["id"], item["item_id"], item["itemId"],
		payload["call_id"], payload["callId"], payload["id"], payload["item_id"], payload["itemId"],
	)
}

func codexToolCategory(itemType string, name string) string {
	switch itemType {
	case "web_search":
		return "web_search"
	case "tool_search":
		return "tool_search"
	case "custom_tool_call":
		return "custom"
	case "mcp_tool_call":
		return "mcp"
	}
	if strings.HasPrefix(name, "mcp__") || strings.Contains(name, ".mcp__") {
		return "mcp"
	}
	return "generic"
}

func codexImages(item map[string]any) []process.CodexImage {
	images := []process.CodexImage(nil)
	for _, field := range []string{"content", "output"} {
		parts, ok := item[field].([]any)
		if !ok {
			continue
		}
		for _, part := range parts {
			entry := mapValue(part)
			contentType := stringValue(entry, "type")
			resource := mapValue(entry["resource"])
			var source string
			var mimeType string
			inlineBlob := false
			sourceKind := "remote"
			switch contentType {
			case "input_image", "output_image", "image":
				source = firstString(entry["image_url"], entry["url"], entry["data"])
				mimeType = stringValue(entry, "mime_type", "mimeType")
			case "input_audio", "audio":
				source = firstString(entry["data"], entry["audio"])
				inlineBlob = source != ""
				if source == "" {
					source = firstString(entry["url"])
				}
				mimeType = stringValue(entry, "mime_type", "mimeType")
			case "resource", "embedded_resource":
				source = firstString(resource["blob"], entry["blob"])
				inlineBlob = source != ""
				if source == "" {
					source = firstString(resource["url"], resource["uri"])
				}
				mimeType = firstString(resource["mimeType"], resource["mime_type"], entry["mimeType"], entry["mime_type"])
			default:
				continue
			}
			if source == "" {
				continue
			}
			if strings.HasPrefix(source, "data:") {
				sourceKind = "inline"
			} else if inlineBlob {
				// GLUE: The transcript image carrier also transports inline MCP blobs until GraphQL exposes a generic artifact candidate.
				sourceKind = "inline_base64"
			} else if strings.HasPrefix(source, "/") {
				sourceKind = "managed_file"
			}
			images = append(images, process.CodexImage{Source: source, Detail: stringValue(entry, "detail"), SourceKind: sourceKind, MimeType: mimeType})
		}
	}
	return images
}

func codexFileChanges(value any) []process.CodexFileChange {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	changes := make([]process.CodexFileChange, 0, len(entries))
	for _, value := range entries {
		entry := mapValue(value)
		path := stringValue(entry, "path")
		if path == "" {
			continue
		}
		changes = append(changes, process.CodexFileChange{
			Kind:        normalizeFileChangeKind(stringValue(entry, "kind")),
			Path:        path,
			MovePath:    stringValue(entry, "movePath", "move_path"),
			UnifiedDiff: stringValue(entry, "unifiedDiff", "unified_diff"),
		})
	}
	return changes
}

func normalizeFileChangeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "add", "added", "create", "created":
		return "added"
	case "delete", "deleted", "remove", "removed":
		return "deleted"
	case "rename", "renamed", "move", "moved":
		return "renamed"
	default:
		return "modified"
	}
}

func codexUsageContent(payload map[string]any) process.CodexUsageContent {
	info := mapValue(payload["info"])
	total := mapValue(info["total_token_usage"])
	current := mapValue(info["last_token_usage"])
	return process.CodexUsageContent{
		InputTokens:                  intValue(total["input_tokens"]),
		CachedInputTokens:            intValue(total["cached_input_tokens"]),
		OutputTokens:                 intValue(total["output_tokens"]),
		ReasoningOutputTokens:        intValue(total["reasoning_output_tokens"]),
		TotalTokens:                  intValue(total["total_tokens"]),
		ContextWindow:                intValue(info["model_context_window"]),
		CurrentInputTokens:           intValue(current["input_tokens"]),
		CurrentCachedInputTokens:     intValue(current["cached_input_tokens"]),
		CurrentOutputTokens:          intValue(current["output_tokens"]),
		CurrentReasoningOutputTokens: intValue(current["reasoning_output_tokens"]),
		CurrentTotalTokens:           intValue(current["total_tokens"]),
	}
}

func isCodexStatusType(eventType string) bool {
	switch eventType {
	case "thread.started", "task.started", "task.completed", "turn.started", "turn.completed", "turn.aborted", "context.compacted", "turn.context", "world.state", "process.exit", "error", "invalid_json", "inter_agent_communication_metadata", "sub_agent_activity", "thread_settings_applied":
		return true
	default:
		return false
	}
}

func codexStatusContent(code string, payload map[string]any) process.CodexStatusContent {
	level := "info"
	if code == "error" || code == "invalid_json" || (code == "process.exit" && intValue(payload["exitCode"]) != 0) {
		level = "error"
	} else if code == "turn.aborted" || code == "context.compacted" || (code == "sub_agent_activity" && stringValue(payload, "kind") == "interrupted") {
		level = "warning"
	}
	message := firstString(payload["message"], payload["reason"], payload["failureReason"])
	if message == "" {
		switch code {
		case "inter_agent_communication_metadata":
			message = "Inter-agent communication metadata"
		case "sub_agent_activity":
			message = strings.TrimSpace("Sub-agent " + firstString(payload["kind"]) + " " + firstString(payload["agent_path"]))
		case "thread_settings_applied":
			message = "Thread settings applied"
		}
	}
	return process.CodexStatusContent{
		Code:    code,
		Level:   level,
		Message: message,
		Details: cloneMap(payload),
	}
}

func structuredText(text string, fallback process.CodexTextFormat) process.CodexStructuredText {
	if text == "" {
		return process.CodexStructuredText{Format: fallback}
	}
	format := fallback
	if json.Valid([]byte(text)) {
		format = process.CodexTextJSON
	}
	return process.CodexStructuredText{Format: format, Text: text}
}

func normalizeANSIText(value string) string {
	return strings.ReplaceAll(value, "␛[", "\x1b[")
}

func normalizeDisplayCommand(value string) string {
	command := strings.TrimSpace(value)
	for _, prefix := range []string{"/bin/bash -lc ", "bash -lc ", "/bin/sh -lc ", "sh -lc ", "/bin/zsh -lc ", "zsh -lc "} {
		if strings.HasPrefix(command, prefix) {
			return unquoteShellArgument(strings.TrimSpace(strings.TrimPrefix(command, prefix)))
		}
	}
	return command
}

func unquoteShellArgument(value string) string {
	if len(value) < 2 || value[0] != value[len(value)-1] || (value[0] != '\'' && value[0] != '"') {
		return value
	}
	inner := value[1 : len(value)-1]
	if value[0] == '\'' {
		return strings.ReplaceAll(inner, `'\''`, `'`)
	}
	replacer := strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`")
	return replacer.Replace(inner)
}

func qualifiedInvocationName(invocation map[string]any) string {
	server := stringValue(invocation, "server")
	tool := stringValue(invocation, "tool")
	if server == "" {
		return tool
	}
	if tool == "" {
		return server
	}
	return server + "." + tool
}

func intPointer(values ...any) *int {
	for _, value := range values {
		switch typed := value.(type) {
		case *int:
			if typed != nil {
				result := *typed
				return &result
			}
		case int:
			result := typed
			return &result
		case int64:
			result := int(typed)
			return &result
		case float64:
			result := int(typed)
			return &result
		}
	}
	return nil
}

func intValue(value any) int {
	if result := intPointer(value); result != nil {
		return *result
	}
	return 0
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func sessionCWDFromMeta(raw []byte) string {
	var record struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if json.Unmarshal(raw, &record) != nil || record.Type != "session_meta" {
		return ""
	}
	return stringValue(payloadOrEmpty(record.Payload), "cwd")
}

func parseSessionTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func codexEventsFromResponseItem(timestamp string, payload map[string]any, createdAt time.Time) []codexLogEvent {
	itemType := stringValue(payload, "type")
	switch itemType {
	case "function_call":
		callID := stringValue(payload, "call_id")
		command := commandFromFunctionArguments(payload)
		itemType := "tool_call"
		if command.Command != "" {
			itemType = "command_execution"
		}
		normalized := normalizedItem(itemType, "in_progress")
		normalized["qualifiedName"] = qualifiedToolName(payload)
		normalized["input"] = stringOrJSON(payload["arguments"])
		normalized["command"] = command.Command
		if command.Command != "" {
			normalized["commands"] = commandInvocationValues([]process.CodexCommandInvocation{command})
		}
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.started", callID),
			Type:      "item.started",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "function_call_output":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("tool_result", stringValue(payload, "status"))
		if normalized["status"] == "" {
			normalized["status"] = "completed"
		}
		normalized["output"] = textFromValue(payload["output"])
		normalized["exitCode"] = firstValue(payload["exit_code"], payload["exitCode"])
		normalized["durationMs"] = firstValue(payload["duration_ms"], payload["durationMs"])
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.completed", callID),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "custom_tool_call":
		callID := stringValue(payload, "call_id")
		name := stringValue(payload, "name")
		input := stringOrJSON(payload["input"])
		itemType := "custom_tool_call"
		commands, extracted := extractExecCommandInvocations(input)
		nestedName := extractExecToolName(input)
		if name == "exec" && (extracted || isCommandTransportTool(nestedName)) {
			itemType = "command_execution"
			if len(commands) == 0 {
				commands = []process.CodexCommandInvocation{{Command: nestedName}}
			}
		}
		normalized := normalizedItem(itemType, "in_progress")
		normalized["qualifiedName"] = name
		normalized["input"] = input
		if itemType == "command_execution" {
			normalized["commandKind"] = string(process.CodexCommandExec)
			normalized["commands"] = commandInvocationValues(commands)
		}
		toolEvent := codexLogEvent{
			EventID:   eventID(timestamp, "item.started", callID),
			Type:      "item.started",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}
		if arguments, ok := extractUpdatePlanInvocation(input); ok {
			if update, ok := planUpdateFromPayload(arguments); ok {
				planEvent := codexLogEvent{
					Type: "plan_update", Payload: arguments, PlanUpdate: &update,
					CorrelationID: callID, CreatedAt: createdAt,
				}
				if itemType == "custom_tool_call" {
					return []codexLogEvent{planEvent}
				}
				return []codexLogEvent{planEvent, toolEvent}
			}
		}
		return []codexLogEvent{toolEvent}
	case "custom_tool_call_output":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("custom_tool_call", "completed")
		result := normalizeCustomToolOutput(payload["output"])
		normalized["output"] = result.output
		if len(result.commandOutputs) > 0 {
			normalized["commandOutputs"] = commandOutputValues(result.commandOutputs)
		}
		if result.exitCode != nil {
			normalized["exitCode"] = *result.exitCode
		}
		if result.durationMS != nil {
			normalized["durationMs"] = *result.durationMS
		}
		if result.status != "" {
			normalized["status"] = result.status
		}
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.completed", callID),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "tool_search_call":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("tool_search", "in_progress")
		normalized["qualifiedName"] = "tool_search"
		normalized["input"] = stringOrJSON(payload["arguments"])
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.started", callID),
			Type:      "item.started",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "tool_search_output":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("tool_search", "completed")
		normalized["qualifiedName"] = "tool_search"
		normalized["output"] = jsonText(payload["tools"])
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.completed", callID),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "web_search_call":
		callID := stringValue(payload, "id", "call_id")
		status := strings.ToLower(strings.TrimSpace(stringValue(payload, "status")))
		if status == "" {
			status = "completed"
		}
		eventType := "item.completed"
		if status == "in_progress" {
			eventType = "item.started"
		}
		normalized := normalizedItem("web_search", status)
		normalized["output"] = stringOrJSON(payload["action"])
		return []codexLogEvent{{
			EventID:   eventID(timestamp, eventType, callID),
			Type:      eventType,
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "agent_message", "assistant_message", "user_message":
		normalizedType := itemType
		if normalizedType == "assistant_message" {
			normalizedType = "agent_message"
		}
		normalized := normalizedItem(normalizedType, "completed")
		normalized["output"] = firstString(payload["message"], payload["text"], payload["output"], messageText(payload))
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.completed", stringValue(payload, "id", "event_id")),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "message":
		itemType := ""
		switch stringValue(payload, "role") {
		case "user":
			itemType = "user_message"
		case "assistant":
			itemType = "agent_message"
		}
		if itemType == "" {
			return nil
		}
		id := stringValue(payload, "id")
		normalized := normalizedItem(itemType, "completed")
		normalized["output"] = messageText(payload)
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.completed", id),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "reasoning":
		normalized := normalizedItem("reasoning", "completed")
		normalized["output"] = reasoningText(payload)
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.completed", stringValue(payload, "id")),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	}
	if itemType == "" {
		return nil
	}
	return []codexLogEvent{{
		Type:      itemType,
		Payload:   payload,
		CreatedAt: createdAt,
	}}
}

func normalizedItem(itemType string, status string) map[string]any {
	return map[string]any{"type": itemType, "status": status}
}

func hasAnyKey(value map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := value[key]; ok {
			return true
		}
	}
	return false
}

func firstValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func itemEventPayload(item map[string]any, normalized map[string]any) map[string]any {
	return map[string]any{"item": item, "normalizedItem": normalized}
}

func stringOrJSON(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return jsonText(value)
}

func codexEventsFromEventMessage(timestamp string, payload map[string]any, createdAt time.Time, sessionCWD string) []codexLogEvent {
	eventType := stringValue(payload, "type")
	switch eventType {
	case "patch_apply_end":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("file_change", stringValue(payload, "status"))
		normalized["changes"] = fileChangesFromPatch(payload, sessionCWD)
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "item.completed", callID),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "agent_message":
		normalized := normalizedItem("agent_message", "completed")
		normalized["output"] = firstString(payload["message"], payload["text"], payload["output"], messageText(payload))
		return []codexLogEvent{{
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "user_message", "context_compacted", "web_search_end":
		// Richer canonical records are emitted as response_item or compacted entries.
		return nil
	case "task_started":
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "task.started", stringValue(payload, "turn_id")),
			Type:      "task.started",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "task_complete":
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "task.completed", stringValue(payload, "turn_id")),
			Type:      "task.completed",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "token_count":
		return []codexLogEvent{{
			Type:      "token_count",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "turn_aborted":
		return []codexLogEvent{{
			EventID:   eventID(timestamp, "turn.aborted", stringValue(payload, "turn_id")),
			Type:      "turn.aborted",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	}
	if eventType == "" {
		return nil
	}
	return []codexLogEvent{{
		Type:      eventType,
		Payload:   payload,
		CreatedAt: createdAt,
	}}
}

func qualifiedToolName(payload map[string]any) string {
	name := stringValue(payload, "name")
	namespace := stringValue(payload, "namespace")
	if namespace == "" {
		return name
	}
	if name == "" {
		return namespace
	}
	return namespace + "." + name
}

func commandFromFunctionArguments(payload map[string]any) process.CodexCommandInvocation {
	arguments := stringValue(payload, "arguments")
	if arguments == "" {
		return process.CodexCommandInvocation{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return process.CodexCommandInvocation{Command: arguments}
	}
	return process.CodexCommandInvocation{
		Command: stringValue(parsed, "cmd", "command"),
		Workdir: stringValue(parsed, "workdir"),
	}
}

func commandInvocationValues(commands []process.CodexCommandInvocation) []any {
	values := make([]any, 0, len(commands))
	for _, command := range commands {
		values = append(values, map[string]any{"command": command.Command, "workdir": command.Workdir})
	}
	return values
}

func commandInvocationsFromValue(value any) []process.CodexCommandInvocation {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	commands := make([]process.CodexCommandInvocation, 0, len(entries))
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			return nil
		}
		command := stringValue(item, "command")
		if command == "" {
			return nil
		}
		commands = append(commands, process.CodexCommandInvocation{Command: command, Workdir: stringValue(item, "workdir")})
	}
	return commands
}

func messageText(payload map[string]any) string {
	content, ok := payload["content"].([]any)
	if !ok {
		return stringValue(payload, "message")
	}
	var builder strings.Builder
	for _, item := range content {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text := stringValue(entry, "text"); text != "" {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(text)
		}
	}
	return builder.String()
}

func reasoningText(payload map[string]any) string {
	for _, value := range []any{
		payload["summary"],
		payload["content"],
		payload["text"],
		payload["message"],
	} {
		if text := textFromValue(value); text != "" {
			return text
		}
	}
	return ""
}

func textFromValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := textFromValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "content", "summary"} {
			if text := textFromValue(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

type customToolOutput struct {
	output         string
	exitCode       *int
	durationMS     *int
	status         string
	commandOutputs []customCommandOutput
}

type customCommandOutput struct {
	output     string
	exitCode   *int
	durationMS *int
	status     string
}

func normalizeCustomToolOutput(value any) customToolOutput {
	items, ok := value.([]any)
	if !ok {
		part, _ := unwrapCustomToolEnvelope(textFromValue(value))
		part.commandOutputs = []customCommandOutput{{output: part.output, exitCode: part.exitCode, durationMS: part.durationMS, status: part.status}}
		return part
	}
	result := customToolOutput{}
	parts := make([]string, 0, len(items))
	plainOutputs := make([]customCommandOutput, 0, len(items))
	for index, item := range items {
		text := textFromValue(item)
		if index == 0 {
			if status, durationMS, matched := parseScriptSummary(text); matched {
				result.status = status
				result.durationMS = durationMS
				continue
			}
		}
		part, structured := unwrapCustomToolEnvelope(text)
		commandOutput := customCommandOutput{
			output: part.output, exitCode: part.exitCode, durationMS: part.durationMS, status: part.status,
		}
		if structured {
			result.commandOutputs = append(result.commandOutputs, commandOutput)
		} else {
			plainOutputs = append(plainOutputs, commandOutput)
		}
		if part.output != "" {
			parts = append(parts, part.output)
		}
	}
	if len(result.commandOutputs) == 0 {
		result.commandOutputs = plainOutputs
	}
	result.output = strings.Join(parts, "\n")
	if len(result.commandOutputs) == 1 {
		part := result.commandOutputs[0]
		result.exitCode = part.exitCode
		if part.durationMS != nil {
			result.durationMS = part.durationMS
		}
		if part.status != "" {
			result.status = part.status
		}
	} else {
		running := false
		for _, part := range result.commandOutputs {
			if part.status == "failed" || (part.exitCode != nil && *part.exitCode != 0) {
				result.status = "failed"
				running = false
				break
			}
			if part.status == "running" {
				running = true
			}
		}
		if running {
			result.status = "running"
		}
	}
	return result
}

func commandOutputValues(outputs []customCommandOutput) []any {
	values := make([]any, 0, len(outputs))
	for _, output := range outputs {
		values = append(values, map[string]any{
			"output": output.output, "exitCode": output.exitCode, "durationMs": output.durationMS,
		})
	}
	return values
}

func commandOutputsFromValue(value any) []customCommandOutput {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	outputs := make([]customCommandOutput, 0, len(entries))
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			return nil
		}
		outputs = append(outputs, customCommandOutput{
			output: stringValue(item, "output"), exitCode: intPointer(item["exitCode"]), durationMS: intPointer(item["durationMs"]),
		})
	}
	return outputs
}

func applyCommandOutput(command *process.CodexCommandInvocation, output customCommandOutput) {
	command.HasOutput = true
	command.Output = normalizeANSIText(output.output)
	command.ExitCode = output.exitCode
	command.DurationMS = output.durationMS
}

func commandInvocationFailed(commands []process.CodexCommandInvocation) bool {
	for _, command := range commands {
		if command.ExitCode != nil && *command.ExitCode != 0 {
			return true
		}
	}
	return false
}

func parseScriptSummary(value string) (string, *int, bool) {
	lines := strings.Split(value, "\n")
	if len(lines) < 4 || lines[2] != "Output:" {
		return "", nil, false
	}
	for _, line := range lines[3:] {
		if line != "" {
			return "", nil, false
		}
	}
	status := ""
	switch {
	case lines[0] == "Script completed":
		status = "completed"
	case strings.HasPrefix(lines[0], "Script running with cell ID "):
		status = "running"
	case lines[0] == "Script failed":
		status = "failed"
	default:
		return "", nil, false
	}
	const wallTimePrefix = "Wall time "
	const wallTimeSuffix = " seconds"
	if !strings.HasPrefix(lines[1], wallTimePrefix) || !strings.HasSuffix(lines[1], wallTimeSuffix) {
		return "", nil, false
	}
	seconds, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(lines[1], wallTimePrefix), wallTimeSuffix), 64)
	if err != nil || seconds < 0 {
		return "", nil, false
	}
	durationMS := int(seconds*1000 + 0.5)
	return status, &durationMS, true
}

func unwrapCustomToolEnvelope(value string) (customToolOutput, bool) {
	result := customToolOutput{output: value}
	var envelope map[string]any
	matched := false
	for _, candidate := range customToolEnvelopeCandidates(value) {
		var parsed map[string]any
		if json.Unmarshal([]byte(candidate), &parsed) == nil {
			_, hasOutput := parsed["output"].(string)
			if hasOutput && hasAnyKey(parsed, "chunk_id", "session_id") && hasAnyKey(parsed, "wall_time_seconds", "original_token_count") {
				envelope = parsed
				matched = true
				break
			}
		}
	}
	if !matched {
		return result, false
	}
	output := envelope["output"].(string)
	result.output = output
	result.exitCode = intPointer(envelope["exit_code"], envelope["exitCode"])
	if seconds, ok := envelope["wall_time_seconds"].(float64); ok && seconds >= 0 {
		durationMS := int(seconds*1000 + 0.5)
		result.durationMS = &durationMS
	}
	if result.exitCode == nil && envelope["session_id"] != nil {
		result.status = "running"
	}
	return result, true
}

func customToolEnvelopeCandidates(value string) []string {
	trimmed := strings.TrimSpace(value)
	candidates := []string{trimmed}
	for offset := 0; offset < len(trimmed); {
		lineEnd := strings.IndexByte(trimmed[offset:], '\n')
		if lineEnd < 0 {
			break
		}
		offset += lineEnd + 1
		if candidate := strings.TrimSpace(trimmed[offset:]); candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func isCommandTransportTool(name string) bool {
	switch name {
	case "tools.write_stdin", "tools.wait":
		return true
	default:
		return false
	}
}

func fileChangesFromPatch(payload map[string]any, sessionCWD string) []any {
	changes, ok := payload["changes"].(map[string]any)
	if !ok {
		return nil
	}
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]any, 0, len(paths))
	for _, path := range paths {
		entry, _ := changes[path].(map[string]any)
		result = append(result, map[string]any{
			"path":        normalizePatchPath(path, sessionCWD),
			"kind":        stringValue(entry, "type"),
			"unifiedDiff": stringValue(entry, "unified_diff", "unifiedDiff"),
			"movePath":    normalizePatchPath(stringValue(entry, "move_path", "movePath"), sessionCWD),
		})
	}
	return result
}

func normalizePatchPath(path string, sessionCWD string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	if sessionCWD != "" {
		if rel, err := filepath.Rel(sessionCWD, path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func eventID(timestamp string, typ string, key string) string {
	if key == "" {
		return ""
	}
	parts := []string{timestamp, typ}
	parts = append(parts, key)
	return strings.Join(parts, ":")
}

func sourceEventID(typ string, sourceID string, offset int64) string {
	if sourceID == "" {
		sourceID = "session"
	}
	return fmt.Sprintf("source:%s:%d:%s", sourceID, offset, typ)
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}
