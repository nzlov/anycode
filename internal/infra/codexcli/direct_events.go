package codexcli

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

type directThreadItem struct {
	ID               string             `json:"id"`
	Type             string             `json:"type"`
	Text             string             `json:"text"`
	Content          json.RawMessage    `json:"content"`
	Summary          []string           `json:"summary"`
	Command          string             `json:"command"`
	CWD              string             `json:"cwd"`
	AggregatedOutput *string            `json:"aggregatedOutput"`
	ExitCode         *int               `json:"exitCode"`
	DurationMS       *int               `json:"durationMs"`
	Status           string             `json:"status"`
	Changes          []directFileChange `json:"changes"`
	Server           string             `json:"server"`
	Tool             string             `json:"tool"`
	Arguments        any                `json:"arguments"`
	Result           any                `json:"result"`
	Results          any                `json:"results"`
	Error            any                `json:"error"`
	Success          *bool              `json:"success"`
	ContentItems     any                `json:"contentItems"`
	Query            string             `json:"query"`
}

type directContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type directFileChange struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
	Kind struct {
		Type     string `json:"type"`
		MovePath string `json:"move_path"`
	} `json:"kind"`
}

func directCodexEvent(method string, raw json.RawMessage) (process.CodexEvent, bool) {
	if method != "item/started" && method != "item/completed" {
		return process.CodexEvent{}, false
	}
	var params struct {
		TurnID        string           `json:"turnId"`
		StartedAtMS   int64            `json:"startedAtMs"`
		CompletedAtMS int64            `json:"completedAtMs"`
		Item          directThreadItem `json:"item"`
	}
	if json.Unmarshal(raw, &params) != nil || params.Item.ID == "" {
		return process.CodexEvent{}, false
	}
	started := method == "item/started"
	if started && (params.Item.Type == "agentMessage" || params.Item.Type == "reasoning") {
		return process.CodexEvent{}, false
	}
	if !started && params.Item.Type == "userMessage" {
		return process.CodexEvent{}, false
	}
	content, eventType, ok := directItemContent(params.Item)
	if !ok {
		return process.CodexEvent{}, false
	}
	phase := process.CodexPhaseCompleted
	if started {
		phase = process.CodexPhaseStarted
	} else if params.Item.Status == "failed" {
		phase = process.CodexPhaseFailed
	} else if params.Item.Status == "declined" {
		phase = process.CodexPhaseCancelled
	}
	if eventType == process.CodexEventMessage || eventType == process.CodexEventReasoning || eventType == process.CodexEventUnknown {
		phase = process.CodexPhaseStandalone
	}
	createdAt := time.Now()
	if started && params.StartedAtMS > 0 {
		createdAt = time.UnixMilli(params.StartedAtMS)
	} else if !started && params.CompletedAtMS > 0 {
		createdAt = time.UnixMilli(params.CompletedAtMS)
	}
	return process.CodexEvent{
		EventID: method + ":" + params.Item.ID, Type: eventType, CorrelationID: params.Item.ID,
		TurnID: params.TurnID, Phase: phase, Content: content, CreatedAt: createdAt,
	}, true
}

func directItemContent(item directThreadItem) (process.CodexEventContent, process.CodexEventType, bool) {
	switch item.Type {
	case "userMessage":
		var inputs []directContentItem
		_ = json.Unmarshal(item.Content, &inputs)
		parts := make([]string, 0, len(inputs))
		for _, value := range inputs {
			if value.Type == "text" && strings.TrimSpace(value.Text) != "" {
				parts = append(parts, value.Text)
			}
		}
		return process.CodexMessageContent{Role: "user", Text: strings.Join(parts, "\n"), Format: process.CodexTextMarkdown}, process.CodexEventMessage, len(parts) > 0
	case "agentMessage":
		return process.CodexMessageContent{Role: "assistant", Text: item.Text, Format: process.CodexTextMarkdown}, process.CodexEventMessage, item.Text != ""
	case "reasoning":
		text := strings.Join(item.Summary, "\n")
		if text == "" {
			var content []string
			_ = json.Unmarshal(item.Content, &content)
			text = strings.Join(content, "\n")
		}
		return process.CodexReasoningContent{Text: text}, process.CodexEventReasoning, text != ""
	case "commandExecution":
		output := ""
		if item.AggregatedOutput != nil {
			output = *item.AggregatedOutput
		}
		command := process.CodexCommandInvocation{
			Command: item.Command, Workdir: item.CWD, HasOutput: output != "", Output: output,
			ExitCode: item.ExitCode, DurationMS: item.DurationMS,
		}
		return process.CodexCommandContent{Kind: process.CodexCommandShell, Commands: []process.CodexCommandInvocation{command}, DurationMS: item.DurationMS}, process.CodexEventCommand, true
	case "fileChange":
		changes := make([]process.CodexFileChange, 0, len(item.Changes))
		for _, value := range item.Changes {
			changes = append(changes, process.CodexFileChange{Kind: value.Kind.Type, Path: value.Path, MovePath: value.Kind.MovePath, UnifiedDiff: value.Diff})
		}
		return process.CodexFileChangeContent{Changes: changes}, process.CodexEventFileChange, len(changes) > 0
	case "mcpToolCall":
		output := item.Result
		if output == nil {
			output = item.Error
		}
		return directToolContent(item.Server+"/"+item.Tool, "mcp", item.Arguments, output), process.CodexEventTool, true
	case "dynamicToolCall":
		output := item.ContentItems
		if output == nil {
			output = map[string]any{"success": item.Success}
		}
		return directToolContent(item.Tool, "dynamic", item.Arguments, output), process.CodexEventTool, true
	case "webSearch":
		return directToolContent("web/search", "web", map[string]any{"query": item.Query}, item.Results), process.CodexEventTool, true
	case "hookPrompt", "plan", "contextCompaction":
		return nil, "", false
	default:
		payload := map[string]any{}
		encoded, _ := json.Marshal(item)
		_ = json.Unmarshal(encoded, &payload)
		return process.CodexUnknownContent{RawType: "codex." + item.Type, Payload: payload}, process.CodexEventUnknown, item.Type != ""
	}
}

func directToolContent(name string, category string, input any, output any) process.CodexToolContent {
	return process.CodexToolContent{
		QualifiedName: strings.Trim(name, "/"), Category: category,
		Input:  process.CodexStructuredText{Format: process.CodexTextJSON, Text: jsonText(input)},
		Output: process.CodexStructuredText{Format: process.CodexTextJSON, Text: jsonText(output)},
	}
}
