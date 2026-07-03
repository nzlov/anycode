package redaction

import (
	"fmt"
	"strings"
)

const (
	Redacted     = "[redacted]"
	RedactedPath = "[redacted_path]"
)

func Map(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = valueForKey(key, value)
	}
	return output
}

func Value(input any) any {
	return valueForKey("", input)
}

func Text(input string) string {
	if input == "" {
		return ""
	}
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return input
	}
	output := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		trimmed := strings.Trim(part, `"'(),;:`)
		switch {
		case isSensitiveAssignment(trimmed):
			output = append(output, redactAssignment(part))
			if assignmentValueIsBearer(trimmed) && i+1 < len(parts) {
				i++
			}
		case isAbsolutePath(trimmed):
			output = append(output, strings.Replace(part, trimmed, RedactedPath, 1))
		case strings.EqualFold(trimmed, "bearer") && i+1 < len(parts):
			output = append(output, Redacted)
			i++
		default:
			output = append(output, part)
		}
	}
	return strings.Join(output, " ")
}

func valueForKey(key string, input any) any {
	if isSensitiveKey(key) {
		return Redacted
	}
	if isPathKey(key) {
		if text, ok := input.(string); ok && isAbsolutePath(text) {
			return RedactedPath
		}
	}
	switch value := input.(type) {
	case map[string]any:
		return Map(value)
	case []any:
		output := make([]any, len(value))
		for i, item := range value {
			output[i] = Value(item)
		}
		return output
	case []string:
		output := make([]any, len(value))
		for i, item := range value {
			output[i] = valueForKey(key, item)
		}
		return output
	case string:
		return Text(value)
	case fmt.Stringer:
		return Text(value.String())
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := normalizeKey(key)
	return strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "accesskey") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "password")
}

func isPathKey(key string) bool {
	normalized := normalizeKey(key)
	return normalized == "path" ||
		strings.HasSuffix(normalized, "path") ||
		strings.HasSuffix(normalized, "dir") ||
		strings.Contains(normalized, "worktree") ||
		strings.Contains(normalized, "workspace")
}

func normalizeKey(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return key
}

func isSensitiveAssignment(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "token=") ||
		strings.Contains(lower, "access_key=") ||
		strings.Contains(lower, "accesskey=") ||
		strings.Contains(lower, "authorization=") ||
		strings.Contains(lower, "bearer ")
}

func redactAssignment(value string) string {
	if index := strings.Index(value, "="); index >= 0 {
		return value[:index+1] + Redacted
	}
	return Redacted
}

func assignmentValueIsBearer(value string) bool {
	index := strings.Index(value, "=")
	return index >= 0 && strings.EqualFold(value[index+1:], "bearer")
}

func isAbsolutePath(value string) bool {
	return strings.HasPrefix(value, "/")
}
