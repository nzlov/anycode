package codexcli

import (
	"reflect"
	"sort"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/nzlov/anycode/internal/domain/process"
)

const execWrapperPrefix = "async function __anycode_exec__(){\n"

type execToolCall struct {
	name string
	call *ast.CallExpression
}

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

func extractExecToolName(source string) string {
	calls, ok := parseExecToolCalls(source)
	if !ok || len(calls) == 0 {
		return ""
	}
	return calls[0].name
}

func parseExecToolCalls(source string) ([]execToolCall, bool) {
	program, err := parser.ParseFile(
		nil,
		"",
		execWrapperPrefix+source+"\n}",
		parser.IgnoreRegExpErrors,
		parser.WithDisableSourceMaps,
	)
	if err != nil {
		return nil, false
	}

	callNodes := collectCallExpressions(program)
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
	return calls, true
}

func collectCallExpressions(root ast.Node) []*ast.CallExpression {
	calls := make([]*ast.CallExpression, 0, 1)
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
	return calls
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
