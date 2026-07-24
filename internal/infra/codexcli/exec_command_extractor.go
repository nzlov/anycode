package codexcli

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/nzlov/anycode/internal/domain/process"
)

const execWrapperPrefix = "async function __anycode_exec__(){\n"

type execToolCall struct {
	name string
	call *ast.CallExpression
}

type parsedExecSource struct {
	calls    []execToolCall
	bindings map[string][]ast.Expression
}

// GLUE: Codex rollout files persist command calls inside outer exec JavaScript; remove this parser when rollouts expose structured command items.
func extractExecCommandInvocations(source string) ([]process.CodexCommandInvocation, bool) {
	calls, ok := parseExecToolCalls(source)
	if !ok {
		return nil, false
	}
	commands := make([]process.CodexCommandInvocation, 0, len(calls))
	found := false
	for _, call := range calls {
		if call.name != "tools.exec_command" {
			continue
		}
		found = true
		command, ok := commandFromExecToolCall(call.call)
		if !ok {
			return nil, false
		}
		commands = append(commands, command)
	}
	return commands, found
}

func extractExecApplyPatch(source string, sessionCWD string) ([]process.CodexFileChange, bool) {
	parsed, ok := parseExecSource(source)
	if !ok {
		return nil, false
	}
	for _, call := range parsed.calls {
		if call.name != "tools.apply_patch" || call.call == nil || len(call.call.ArgumentList) != 1 {
			continue
		}
		patch, ok := staticExecString(call.call.ArgumentList[0], parsed.bindings, map[string]struct{}{})
		if !ok {
			return nil, false
		}
		return fileChangesFromApplyPatch(patch, sessionCWD)
	}
	return nil, false
}

func extractExecToolName(source string) string {
	calls, ok := parseExecToolCalls(source)
	if !ok || len(calls) == 0 {
		return ""
	}
	return calls[0].name
}

func extractExecTransportID(source string) string {
	calls, ok := parseExecToolCalls(source)
	if !ok {
		return ""
	}
	for _, call := range calls {
		if !isCommandTransportTool(call.name) || call.call == nil || len(call.call.ArgumentList) != 1 {
			continue
		}
		value, ok := staticPlanValue(call.call.ArgumentList[0])
		if !ok {
			return ""
		}
		arguments, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		return transportValue(arguments["cell_id"], arguments["cellId"], arguments["session_id"], arguments["sessionId"])
	}
	return ""
}

func extractUpdatePlanInvocation(source string) (map[string]any, bool) {
	calls, ok := parseExecToolCalls(source)
	if !ok {
		return nil, false
	}
	for _, call := range calls {
		if call.name != "tools.update_plan" || call.call == nil || len(call.call.ArgumentList) != 1 {
			continue
		}
		value, ok := staticPlanValue(call.call.ArgumentList[0])
		if !ok {
			return nil, false
		}
		arguments, ok := value.(map[string]any)
		return arguments, ok
	}
	return nil, false
}

func staticPlanValue(expression ast.Expression) (any, bool) {
	switch value := expression.(type) {
	case *ast.StringLiteral:
		return value.Value.String(), true
	case *ast.BooleanLiteral:
		return value.Value, true
	case *ast.ArrayLiteral:
		items := make([]any, 0, len(value.Value))
		for _, item := range value.Value {
			parsed, ok := staticPlanValue(item)
			if !ok {
				return nil, false
			}
			items = append(items, parsed)
		}
		return items, true
	case *ast.ObjectLiteral:
		result := make(map[string]any, len(value.Value))
		for _, property := range value.Value {
			keyed, ok := property.(*ast.PropertyKeyed)
			if !ok || keyed.Computed || keyed.Kind != ast.PropertyKindValue {
				return nil, false
			}
			key, ok := staticPropertyName(keyed.Key)
			if !ok {
				return nil, false
			}
			result[key], ok = staticPlanValue(keyed.Value)
			if !ok {
				return nil, false
			}
		}
		return result, true
	default:
		return nil, false
	}
}

func parseExecToolCalls(source string) ([]execToolCall, bool) {
	parsed, ok := parseExecSource(source)
	if !ok {
		return nil, false
	}
	return parsed.calls, true
}

func parseExecSource(source string) (parsedExecSource, bool) {
	program, err := parser.ParseFile(
		nil,
		"",
		execWrapperPrefix+source+"\n}",
		parser.IgnoreRegExpErrors,
		parser.WithDisableSourceMaps,
	)
	if err != nil {
		return parsedExecSource{}, false
	}

	callNodes, bindings := collectExecNodes(program)
	calls := make([]execToolCall, 0, len(callNodes))
	for _, call := range callNodes {
		dot, ok := call.Callee.(*ast.DotExpression)
		if !ok {
			continue
		}
		receiver, ok := dot.Left.(*ast.Identifier)
		if !ok || receiver.Name.String() != "tools" {
			continue
		}
		calls = append(calls, execToolCall{
			name: "tools." + dot.Identifier.Name.String(),
			call: call,
		})
	}
	sort.Slice(calls, func(i, j int) bool {
		return calls[i].call.Idx0() < calls[j].call.Idx0()
	})
	staticBindings := make(map[string][]ast.Expression)
	for _, binding := range bindings {
		identifier, ok := binding.Target.(*ast.Identifier)
		if !ok || binding.Initializer == nil {
			continue
		}
		name := identifier.Name.String()
		staticBindings[name] = append(staticBindings[name], binding.Initializer)
	}
	return parsedExecSource{calls: calls, bindings: staticBindings}, true
}

func collectExecNodes(root ast.Node) ([]*ast.CallExpression, []*ast.Binding) {
	calls := make([]*ast.CallExpression, 0, 1)
	bindings := make([]*ast.Binding, 0, 1)
	seen := make(map[uintptr]struct{})
	var walk func(reflect.Value)
	walk = func(value reflect.Value) {
		if !value.IsValid() {
			return
		}
		for value.Kind() == reflect.Interface {
			if value.IsNil() {
				return
			}
			value = value.Elem()
		}
		switch value.Kind() {
		case reflect.Pointer:
			if value.IsNil() {
				return
			}
			pointer := value.Pointer()
			if _, exists := seen[pointer]; exists {
				return
			}
			seen[pointer] = struct{}{}
			if call, ok := value.Interface().(*ast.CallExpression); ok {
				calls = append(calls, call)
			}
			if binding, ok := value.Interface().(*ast.Binding); ok {
				bindings = append(bindings, binding)
			}
			walk(value.Elem())
		case reflect.Struct:
			if value.Type().PkgPath() != reflect.TypeOf(ast.Identifier{}).PkgPath() {
				return
			}
			for index := 0; index < value.NumField(); index++ {
				field := value.Field(index)
				if field.CanInterface() {
					walk(field)
				}
			}
		case reflect.Slice:
			for index := 0; index < value.Len(); index++ {
				walk(value.Index(index))
			}
		}
	}
	walk(reflect.ValueOf(root))
	return calls, bindings
}

func staticExecString(expression ast.Expression, bindings map[string][]ast.Expression, resolving map[string]struct{}) (string, bool) {
	switch value := expression.(type) {
	case *ast.StringLiteral:
		return value.Value.String(), true
	case *ast.TemplateLiteral:
		if len(value.Expressions) != 0 || (value.Tag != nil && !isStringRawTag(value.Tag)) {
			return "", false
		}
		var result strings.Builder
		for _, element := range value.Elements {
			if value.Tag == nil {
				result.WriteString(element.Parsed.String())
			} else {
				result.WriteString(element.Literal)
			}
		}
		return result.String(), true
	case *ast.Identifier:
		name := value.Name.String()
		initializers := bindings[name]
		if len(initializers) != 1 {
			return "", false
		}
		if _, cyclic := resolving[name]; cyclic {
			return "", false
		}
		resolving[name] = struct{}{}
		result, ok := staticExecString(initializers[0], bindings, resolving)
		delete(resolving, name)
		return result, ok
	default:
		return "", false
	}
}

func isStringRawTag(expression ast.Expression) bool {
	dot, ok := expression.(*ast.DotExpression)
	if !ok || dot.Identifier.Name.String() != "raw" {
		return false
	}
	receiver, ok := dot.Left.(*ast.Identifier)
	return ok && receiver.Name.String() == "String"
}

func fileChangesFromApplyPatch(patch string, sessionCWD string) ([]process.CodexFileChange, bool) {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "*** Begin Patch" {
		return nil, false
	}
	changes := make([]process.CodexFileChange, 0, 1)
	var current *process.CodexFileChange
	var diff []string
	flush := func() {
		if current == nil {
			return
		}
		current.UnifiedDiff = strings.TrimSuffix(strings.Join(diff, "\n"), "\n")
		changes = append(changes, *current)
		current = nil
		diff = nil
	}
	for _, line := range lines[1:] {
		kind, path, header := applyPatchFileHeader(line)
		if header {
			flush()
			path, ok := normalizeApplyPatchPath(path, sessionCWD)
			if !ok {
				return nil, false
			}
			current = &process.CodexFileChange{Kind: kind, Path: path}
			continue
		}
		if strings.HasPrefix(line, "*** Move to: ") {
			if current == nil || current.Kind != "modified" {
				return nil, false
			}
			current.Kind = "renamed"
			movePath, ok := normalizeApplyPatchPath(strings.TrimPrefix(line, "*** Move to: "), sessionCWD)
			if !ok {
				return nil, false
			}
			current.MovePath = movePath
			continue
		}
		if strings.TrimSpace(line) == "*** End Patch" {
			flush()
			return changes, len(changes) > 0
		}
		if current != nil {
			diff = append(diff, line)
		}
	}
	return nil, false
}

func normalizeApplyPatchPath(path string, sessionCWD string) (string, bool) {
	path = normalizePatchPath(path, sessionCWD)
	if path == "" || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, "../") {
		return "", false
	}
	return path, true
}

func applyPatchFileHeader(line string) (string, string, bool) {
	for _, header := range []struct {
		prefix string
		kind   string
	}{
		{prefix: "*** Update File: ", kind: "modified"},
		{prefix: "*** Add File: ", kind: "added"},
		{prefix: "*** Delete File: ", kind: "deleted"},
	} {
		if strings.HasPrefix(line, header.prefix) {
			return header.kind, strings.TrimSpace(strings.TrimPrefix(line, header.prefix)), true
		}
	}
	return "", "", false
}

func commandFromExecToolCall(call *ast.CallExpression) (process.CodexCommandInvocation, bool) {
	if call == nil || len(call.ArgumentList) != 1 {
		return process.CodexCommandInvocation{}, false
	}
	object, ok := call.ArgumentList[0].(*ast.ObjectLiteral)
	if !ok {
		return process.CodexCommandInvocation{}, false
	}
	var command process.CodexCommandInvocation
	seenCommand := false
	seenWorkdir := false
	for _, property := range object.Value {
		keyed, ok := property.(*ast.PropertyKeyed)
		if !ok || keyed.Computed || keyed.Kind != ast.PropertyKindValue {
			return process.CodexCommandInvocation{}, false
		}
		key, ok := staticPropertyName(keyed.Key)
		if !ok {
			return process.CodexCommandInvocation{}, false
		}
		switch key {
		case "cmd":
			if seenCommand {
				return process.CodexCommandInvocation{}, false
			}
			command.Command, ok = staticString(keyed.Value)
			seenCommand = true
		case "workdir":
			if seenWorkdir {
				return process.CodexCommandInvocation{}, false
			}
			command.Workdir, ok = staticString(keyed.Value)
			seenWorkdir = true
		}
		if !ok {
			return process.CodexCommandInvocation{}, false
		}
	}
	return command, seenCommand && command.Command != ""
}

func staticPropertyName(expression ast.Expression) (string, bool) {
	switch value := expression.(type) {
	case *ast.Identifier:
		return value.Name.String(), true
	case *ast.StringLiteral:
		return value.Value.String(), true
	default:
		return "", false
	}
}

func staticString(expression ast.Expression) (string, bool) {
	value, ok := expression.(*ast.StringLiteral)
	if !ok {
		return "", false
	}
	return value.Value.String(), true
}
