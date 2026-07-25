package process

import (
	"encoding/json"
	"strings"
)

const MaxInlineTranscriptBytes = 1 << 20

// PrepareCodexEventForTranscript enforces the public transcript data contract.
// Inline artifact bytes are never exposed; large textual content is represented
// by its source record offset until the caller explicitly requests that record.
func PrepareCodexEventForTranscript(event CodexEvent, deferLarge bool) CodexEvent {
	event.Content = sanitizeCodexEventContent(event.Content)
	if !deferLarge || event.SourceLength <= 0 {
		return event
	}
	content, deferred := deferLargeCodexContent(event.Content)
	if !deferred {
		return event
	}
	event.Content = content
	event.Deferred = &CodexContentReference{ByteOffset: event.SourceOffset, ByteLength: event.SourceLength}
	return event
}

func sanitizeCodexEventContent(content CodexEventContent) CodexEventContent {
	switch value := content.(type) {
	case CodexMessageContent:
		value.Text = sanitizeTranscriptString(value.Text, 0)
		value.Images = sanitizeCodexImages(value.Images)
		return value
	case CodexReasoningContent:
		value.Text = sanitizeTranscriptString(value.Text, 0)
		return value
	case CodexCommandContent:
		for index := range value.Commands {
			value.Commands[index].Command = sanitizeTranscriptString(value.Commands[index].Command, 0)
			value.Commands[index].Output = sanitizeTranscriptString(value.Commands[index].Output, 0)
		}
		return value
	case CodexToolContent:
		value.Input.Text = sanitizeTranscriptString(value.Input.Text, 0)
		value.Output.Text = sanitizeTranscriptString(value.Output.Text, 0)
		value.Images = sanitizeCodexImages(value.Images)
		return value
	case CodexFileChangeContent:
		for index := range value.Changes {
			value.Changes[index].UnifiedDiff = sanitizeTranscriptString(value.Changes[index].UnifiedDiff, 0)
		}
		return value
	case CodexStatusContent:
		value.Message = sanitizeTranscriptString(value.Message, 0)
		value.Details, _ = sanitizeTranscriptValue(value.Details, 0).(map[string]any)
		return value
	case CodexUnknownContent:
		value.Payload, _ = sanitizeTranscriptValue(value.Payload, 0).(map[string]any)
		return value
	default:
		return content
	}
}

func sanitizeCodexImages(images []CodexImage) []CodexImage {
	result := make([]CodexImage, 0, len(images))
	for _, image := range images {
		if inlineDataStart(image.Source) >= 0 || image.SourceKind == "inline" || image.SourceKind == "inline_base64" {
			continue
		}
		result = append(result, image)
	}
	return result
}

func sanitizeTranscriptValue(value any, depth int) any {
	if depth > 8 {
		return "[omitted nested value]"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if text, ok := child.(string); ok && inlineDataStart(text) >= 0 && isArtifactSourceKey(key) {
				continue
			}
			result[key] = sanitizeTranscriptValue(child, depth+1)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, child := range typed {
			result = append(result, sanitizeTranscriptValue(child, depth+1))
		}
		return result
	case string:
		return sanitizeTranscriptString(typed, depth+1)
	default:
		return value
	}
}

func sanitizeTranscriptString(value string, depth int) string {
	trimmed := strings.TrimSpace(value)
	if depth <= 8 && len(trimmed) > 1 && ((trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}') || (trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']')) {
		var decoded any
		if json.Unmarshal([]byte(trimmed), &decoded) == nil {
			if encoded, err := json.Marshal(sanitizeTranscriptValue(decoded, depth+1)); err == nil {
				return string(encoded)
			}
		}
	}
	return omitInlineData(value)
}

func omitInlineData(value string) string {
	for {
		start := inlineDataStart(value)
		if start < 0 {
			return value
		}
		end := inlineDataEnd(value, start)
		value = value[:start] + "[artifact source omitted]" + value[end:]
	}
}

func inlineDataStart(value string) int {
	lower := strings.ToLower(value)
	searchFrom := 0
	for {
		relative := strings.Index(lower[searchFrom:], "data:")
		if relative < 0 {
			return -1
		}
		start := searchFrom + relative
		headerEnd := strings.Index(lower[start:], ",")
		if headerEnd >= 0 {
			header := lower[start : start+headerEnd]
			header = strings.NewReplacer(`\/`, "/", `\u002f`, "/", `\u003b`, ";").Replace(header)
			if strings.Contains(header, ";base64") {
				return start
			}
		}
		searchFrom = start + len("data:")
	}
}

func inlineDataEnd(value string, start int) int {
	comma := strings.IndexByte(value[start:], ',')
	if comma < 0 {
		return len(value)
	}
	end := start + comma + 1
	for end < len(value) {
		char := value[end]
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '+' || char == '/' || char == '=' {
			end++
			continue
		}
		if char == '\\' && end+1 < len(value) && (value[end+1] == '/' || value[end+1] == '\\') {
			end += 2
			continue
		}
		break
	}
	return end
}

func isArtifactSourceKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "data", "blob", "image", "image_url", "imageurl", "audio", "source", "src":
		return true
	default:
		return false
	}
}

func deferLargeCodexContent(content CodexEventContent) (CodexEventContent, bool) {
	deferred := false
	switch value := content.(type) {
	case CodexMessageContent:
		if len(value.Text) > MaxInlineTranscriptBytes {
			value.Text = ""
			deferred = true
		}
		return value, deferred
	case CodexReasoningContent:
		if len(value.Text) > MaxInlineTranscriptBytes {
			value.Text = ""
			deferred = true
		}
		return value, deferred
	case CodexCommandContent:
		total := 0
		for _, command := range value.Commands {
			total += len(command.Output)
		}
		if total <= MaxInlineTranscriptBytes {
			return value, false
		}
		for index := range value.Commands {
			if value.Commands[index].Output != "" {
				value.Commands[index].Output = ""
				deferred = true
			}
		}
		return value, deferred
	case CodexToolContent:
		if len(value.Input.Text)+len(value.Output.Text) <= MaxInlineTranscriptBytes {
			return value, false
		}
		if value.Input.Text != "" {
			value.Input.Text = ""
			deferred = true
		}
		if value.Output.Text != "" {
			value.Output.Text = ""
			deferred = true
		}
		return value, deferred
	case CodexFileChangeContent:
		total := 0
		for _, change := range value.Changes {
			total += len(change.UnifiedDiff)
		}
		if total <= MaxInlineTranscriptBytes {
			return value, false
		}
		for index := range value.Changes {
			if value.Changes[index].UnifiedDiff != "" {
				value.Changes[index].UnifiedDiff = ""
				deferred = true
			}
		}
		return value, deferred
	case CodexStatusContent:
		if encoded, err := json.Marshal(value.Details); err == nil && len(encoded)+len(value.Message) > MaxInlineTranscriptBytes {
			value.Details = map[string]any{}
			value.Message = ""
			deferred = true
		}
		return value, deferred
	case CodexUnknownContent:
		if encoded, err := json.Marshal(value.Payload); err == nil && len(encoded) > MaxInlineTranscriptBytes {
			value.Payload = map[string]any{}
			deferred = true
		}
		return value, deferred
	default:
		return content, false
	}
}
