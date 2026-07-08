package redaction

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	Redacted     = "[redacted]"
	RedactedPath = "[redacted_path]"
)

var textTokenPattern = regexp.MustCompile(`\s+|\S+`)

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
	parts := textTokenPattern.FindAllString(input, -1)
	if len(parts) == 0 {
		return input
	}
	var output strings.Builder
	redactNext := false
	redactNextValue := false
	for _, part := range parts {
		if redactNext {
			if strings.TrimSpace(part) == "" {
				continue
			}
			redactNext = false
			continue
		}
		if redactNextValue {
			if strings.TrimSpace(part) == "" {
				output.WriteString(part)
				continue
			}
			token := strings.Trim(part, `"'(),;:`)
			output.WriteString(Redacted)
			redactNext = isAuthorizationScheme(token)
			redactNextValue = false
			continue
		}
		if strings.TrimSpace(part) == "" {
			output.WriteString(part)
			continue
		}
		trimmed := strings.Trim(part, `"'(),;:`)
		switch {
		case isSensitiveAssignment(trimmed):
			output.WriteString(redactAssignment(part))
			redactNext = assignmentValueIsBearer(trimmed)
		case isAuthorizationColon(part):
			redacted, needsValue, skipNext := redactAuthorizationColon(part)
			output.WriteString(redacted)
			redactNextValue = needsValue
			redactNext = skipNext
		case isAbsolutePath(trimmed):
			output.WriteString(strings.Replace(part, trimmed, RedactedPath, 1))
		case strings.EqualFold(trimmed, "bearer"):
			output.WriteString(Redacted)
			redactNext = true
		default:
			output.WriteString(part)
		}
	}
	return output.String()
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

func isAuthorizationColon(value string) bool {
	core := strings.Trim(value, `"'(),;`)
	return strings.EqualFold(core, "authorization:") ||
		strings.HasPrefix(strings.ToLower(core), "authorization:")
}

func redactAuthorizationColon(value string) (string, bool, bool) {
	leftTrimmed := strings.TrimLeft(value, `"'(),;`)
	prefixLen := len(value) - len(leftTrimmed)
	rightTrimmed := strings.TrimRight(leftTrimmed, `"'(),;`)
	suffix := leftTrimmed[len(rightTrimmed):]
	core := rightTrimmed
	index := strings.Index(core, ":")
	if index < 0 {
		return value, false, false
	}
	prefix := value[:prefixLen] + core[:index+1]
	authValue := core[index+1:]
	if authValue == "" {
		return prefix + suffix, true, false
	}
	return prefix + Redacted + suffix, false, isAuthorizationScheme(authValue)
}

func isAuthorizationScheme(value string) bool {
	return strings.EqualFold(value, "bearer") || strings.EqualFold(value, "basic")
}

func isAbsolutePath(value string) bool {
	return strings.HasPrefix(value, "/")
}
