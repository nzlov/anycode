package codexcli

import (
	"encoding/json"
	"strings"

	"github.com/nzlov/anycode/internal/domain/process"
)

func extractExecCommandInvocations(source string) ([]process.CodexCommandInvocation, bool) {
	commands := make([]process.CodexCommandInvocation, 0, 1)
	found := false
	regexAllowed := true
	memberReceiver := false
	for cursor := 0; cursor < len(source); {
		if isJSWhitespace(source[cursor]) {
			cursor++
			continue
		}
		switch source[cursor] {
		case '\'', '"', '`':
			next, ok := skipJSString(source, cursor)
			if !ok {
				return nil, false
			}
			cursor = next
			regexAllowed = false
			memberReceiver = false
			continue
		case '/':
			if next, matched, ok := skipJSComment(source, cursor); matched {
				if !ok {
					return nil, false
				}
				cursor = next
				continue
			}
			if regexAllowed {
				next, ok := skipJSRegex(source, cursor)
				if !ok {
					return nil, false
				}
				cursor = next
				regexAllowed = false
				memberReceiver = false
				continue
			}
			cursor++
			regexAllowed = true
			memberReceiver = false
			continue
		}
		if source[cursor] >= '0' && source[cursor] <= '9' {
			cursor++
			for cursor < len(source) && isJSIdentifierPart(source[cursor]) {
				cursor++
			}
			regexAllowed = false
			memberReceiver = false
			continue
		}
		if !isJSIdentifierStart(source[cursor]) {
			memberReceiver = source[cursor] == '.'
			regexAllowed = jsPunctuationAllowsRegex(source[cursor])
			cursor++
			continue
		}
		start := cursor
		identifier, next := readJSIdentifier(source, cursor)
		cursor = next
		wasMemberReceiver := memberReceiver
		memberReceiver = false
		regexAllowed = jsIdentifierAllowsRegex(identifier)
		if identifier != "tools" || wasMemberReceiver {
			continue
		}
		command, _, matched, ok := parseExecCommandCall(source, start)
		if !matched {
			continue
		}
		found = true
		if !ok {
			return nil, false
		}
		commands = append(commands, command)
	}
	return commands, found
}

func parseExecCommandCall(source string, start int) (process.CodexCommandInvocation, int, bool, bool) {
	_, cursor := readJSIdentifier(source, start)
	cursor, ok := skipJSTrivia(source, cursor)
	if !ok || cursor >= len(source) || source[cursor] != '.' {
		return process.CodexCommandInvocation{}, start, false, true
	}
	cursor, ok = skipJSTrivia(source, cursor+1)
	if !ok || cursor >= len(source) || !isJSIdentifierStart(source[cursor]) {
		return process.CodexCommandInvocation{}, start, false, true
	}
	identifier, cursor := readJSIdentifier(source, cursor)
	if identifier != "exec_command" {
		return process.CodexCommandInvocation{}, start, false, true
	}
	cursor, ok = skipJSTrivia(source, cursor)
	if !ok || cursor >= len(source) || source[cursor] != '(' {
		return process.CodexCommandInvocation{}, start, false, true
	}
	cursor, ok = skipJSTrivia(source, cursor+1)
	if !ok || cursor >= len(source) || source[cursor] != '{' {
		return process.CodexCommandInvocation{}, start, true, false
	}
	command, cursor, ok := parseExecCommandObject(source, cursor)
	if !ok {
		return process.CodexCommandInvocation{}, start, true, false
	}
	cursor, ok = skipJSTrivia(source, cursor)
	if !ok {
		return process.CodexCommandInvocation{}, start, true, false
	}
	if cursor < len(source) && source[cursor] == ',' {
		cursor, ok = skipJSTrivia(source, cursor+1)
		if !ok {
			return process.CodexCommandInvocation{}, start, true, false
		}
	}
	if cursor >= len(source) || source[cursor] != ')' {
		return process.CodexCommandInvocation{}, start, true, false
	}
	return command, cursor + 1, true, true
}

func parseExecCommandObject(source string, start int) (process.CodexCommandInvocation, int, bool) {
	var command process.CodexCommandInvocation
	seenCommand := false
	seenWorkdir := false
	cursor := start + 1
	for {
		var ok bool
		cursor, ok = skipJSTrivia(source, cursor)
		if !ok || cursor >= len(source) {
			return process.CodexCommandInvocation{}, start, false
		}
		if source[cursor] == '}' {
			return command, cursor + 1, seenCommand && command.Command != ""
		}

		key := ""
		if source[cursor] == '"' {
			key, cursor, ok = parseJSONString(source, cursor)
		} else if isJSIdentifierStart(source[cursor]) {
			key, cursor = readJSIdentifier(source, cursor)
			ok = true
		}
		if !ok || key == "" {
			return process.CodexCommandInvocation{}, start, false
		}
		cursor, ok = skipJSTrivia(source, cursor)
		if !ok || cursor >= len(source) || source[cursor] != ':' {
			return process.CodexCommandInvocation{}, start, false
		}
		cursor, ok = skipJSTrivia(source, cursor+1)
		if !ok || cursor >= len(source) {
			return process.CodexCommandInvocation{}, start, false
		}

		switch key {
		case "cmd":
			if seenCommand || source[cursor] != '"' {
				return process.CodexCommandInvocation{}, start, false
			}
			command.Command, cursor, ok = parseJSONString(source, cursor)
			seenCommand = true
		case "workdir":
			if seenWorkdir || source[cursor] != '"' {
				return process.CodexCommandInvocation{}, start, false
			}
			command.Workdir, cursor, ok = parseJSONString(source, cursor)
			seenWorkdir = true
		default:
			cursor, ok = skipJSPropertyValue(source, cursor)
		}
		if !ok {
			return process.CodexCommandInvocation{}, start, false
		}
		cursor, ok = skipJSTrivia(source, cursor)
		if !ok || cursor >= len(source) {
			return process.CodexCommandInvocation{}, start, false
		}
		switch source[cursor] {
		case ',':
			cursor++
		case '}':
			return command, cursor + 1, seenCommand && command.Command != ""
		default:
			return process.CodexCommandInvocation{}, start, false
		}
	}
}

func skipJSPropertyValue(source string, start int) (int, bool) {
	stack := make([]byte, 0, 2)
	regexAllowed := true
	for cursor := start; cursor < len(source); {
		if isJSWhitespace(source[cursor]) {
			cursor++
			continue
		}
		switch source[cursor] {
		case '\'', '"', '`':
			next, ok := skipJSString(source, cursor)
			if !ok {
				return start, false
			}
			cursor = next
			regexAllowed = false
			continue
		case '/':
			if next, matched, ok := skipJSComment(source, cursor); matched {
				if !ok {
					return start, false
				}
				cursor = next
				continue
			}
			if regexAllowed {
				next, ok := skipJSRegex(source, cursor)
				if !ok {
					return start, false
				}
				cursor = next
				regexAllowed = false
				continue
			}
			cursor++
			regexAllowed = true
			continue
		case '(', '[', '{':
			stack = append(stack, source[cursor])
			regexAllowed = true
		case ')':
			if len(stack) == 0 || stack[len(stack)-1] != '(' {
				return start, false
			}
			stack = stack[:len(stack)-1]
			regexAllowed = false
		case ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return start, false
			}
			stack = stack[:len(stack)-1]
			regexAllowed = false
		case '}':
			if len(stack) == 0 {
				return cursor, cursor > start
			}
			if stack[len(stack)-1] != '{' {
				return start, false
			}
			stack = stack[:len(stack)-1]
			regexAllowed = false
		case ',':
			if len(stack) == 0 {
				return cursor, cursor > start
			}
			regexAllowed = true
		default:
			if isJSIdentifierStart(source[cursor]) {
				identifier, next := readJSIdentifier(source, cursor)
				cursor = next
				regexAllowed = jsIdentifierAllowsRegex(identifier)
				continue
			}
			if source[cursor] >= '0' && source[cursor] <= '9' {
				cursor++
				for cursor < len(source) && isJSIdentifierPart(source[cursor]) {
					cursor++
				}
				regexAllowed = false
				continue
			}
			regexAllowed = jsPunctuationAllowsRegex(source[cursor])
		}
		cursor++
	}
	return start, false
}

func parseJSONString(source string, start int) (string, int, bool) {
	end, ok := skipJSString(source, start)
	if !ok || source[start] != '"' {
		return "", start, false
	}
	var value string
	if json.Unmarshal([]byte(source[start:end]), &value) != nil {
		return "", start, false
	}
	return value, end, true
}

func skipJSString(source string, start int) (int, bool) {
	quote := source[start]
	for cursor := start + 1; cursor < len(source); cursor++ {
		if source[cursor] == '\\' {
			cursor++
			continue
		}
		if source[cursor] == quote {
			return cursor + 1, true
		}
		if (quote == '\'' || quote == '"') && (source[cursor] == '\n' || source[cursor] == '\r') {
			return start, false
		}
	}
	return start, false
}

func skipJSRegex(source string, start int) (int, bool) {
	inCharacterClass := false
	for cursor := start + 1; cursor < len(source); cursor++ {
		switch source[cursor] {
		case '\\':
			cursor++
		case '\n', '\r':
			return start, false
		case '[':
			inCharacterClass = true
		case ']':
			inCharacterClass = false
		case '/':
			if inCharacterClass {
				continue
			}
			cursor++
			for cursor < len(source) && isJSIdentifierPart(source[cursor]) {
				cursor++
			}
			return cursor, true
		}
	}
	return start, false
}

func skipJSComment(source string, start int) (int, bool, bool) {
	if start+1 >= len(source) || source[start] != '/' {
		return start, false, true
	}
	switch source[start+1] {
	case '/':
		if newline := strings.IndexByte(source[start+2:], '\n'); newline >= 0 {
			return start + 2 + newline + 1, true, true
		}
		return len(source), true, true
	case '*':
		if end := strings.Index(source[start+2:], "*/"); end >= 0 {
			return start + 2 + end + 2, true, true
		}
		return start, true, false
	default:
		return start, false, true
	}
}

func skipJSTrivia(source string, start int) (int, bool) {
	cursor := start
	for cursor < len(source) {
		if isJSWhitespace(source[cursor]) {
			cursor++
			continue
		}
		if source[cursor] == '/' {
			if next, matched, ok := skipJSComment(source, cursor); matched {
				if !ok {
					return start, false
				}
				cursor = next
				continue
			}
		}
		break
	}
	return cursor, true
}

func jsIdentifierAllowsRegex(identifier string) bool {
	switch identifier {
	case "await", "case", "delete", "do", "else", "in", "instanceof", "return", "throw", "typeof", "void", "yield":
		return true
	default:
		return false
	}
}

func jsPunctuationAllowsRegex(value byte) bool {
	switch value {
	case '(', '[', '{', '=', ',', ':', ';', '!', '?', '&', '|', '+', '-', '*', '%', '^', '~', '<', '>':
		return true
	default:
		return false
	}
}

func readJSIdentifier(source string, start int) (string, int) {
	cursor := start + 1
	for cursor < len(source) && isJSIdentifierPart(source[cursor]) {
		cursor++
	}
	return source[start:cursor], cursor
}

func isJSIdentifierStart(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isJSIdentifierPart(value byte) bool {
	return isJSIdentifierStart(value) || value >= '0' && value <= '9'
}

func isJSWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}
