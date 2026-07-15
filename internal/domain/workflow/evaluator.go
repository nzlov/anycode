package workflow

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/expr-lang/expr"
)

type DefaultConditionEvaluator struct{}

func (DefaultConditionEvaluator) Evaluate(condition Condition, context Context) (bool, error) {
	return evalCondition(condition, context)
}

func ValidateCondition(condition Condition) error {
	return validateCondition(condition)
}

type DefaultPlanner struct {
	Evaluator ConditionEvaluator
}

func (p DefaultPlanner) NextNode(def Definition, run Run, context Context) (NodeDecision, error) {
	evaluator := p.Evaluator
	if evaluator == nil {
		evaluator = DefaultConditionEvaluator{}
	}

	edges := make([]Edge, 0, len(def.Graph.Edges))
	for _, edge := range def.Graph.Edges {
		if edge.From == run.CurrentNodeID {
			edges = append(edges, edge)
		}
	}
	if len(edges) == 0 {
		return NodeDecision{}, nil
	}

	sort.SliceStable(edges, func(i, j int) bool {
		return edges[i].Priority < edges[j].Priority
	})
	for _, edge := range edges {
		matched, err := evaluator.Evaluate(edge.Condition, context)
		if err != nil {
			return NodeDecision{}, err
		}
		if matched {
			return NodeDecision{NextNodeID: edge.To}, nil
		}
	}
	return NodeDecision{
		Blocked: true,
		Reason:  "no workflow edge condition matched",
	}, nil
}

func (DefaultPlanner) ShouldRetry(node Node, attempts int, _ NodeFailure) bool {
	return node.Retry.MaxAttempts > 0 && attempts < node.Retry.MaxAttempts
}

func evalCondition(condition Condition, context Context) (bool, error) {
	if isExprCondition(condition) {
		return evalExprCondition(condition, context)
	}
	if len(condition.All) > 0 {
		for _, child := range condition.All {
			ok, err := evalCondition(child, context)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	}
	if len(condition.Any) > 0 {
		for _, child := range condition.Any {
			ok, err := evalCondition(child, context)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if condition.Not != nil {
		ok, err := evalCondition(*condition.Not, context)
		return !ok, err
	}
	if condition.Field == "" && condition.Op == "" {
		return true, nil
	}

	actual, exists := lookup(context.Values, condition.Field)
	switch condition.Op {
	case "exists":
		return exists && actual != nil, nil
	case "eq":
		return exists && equalValue(actual, condition.Value), nil
	case "ne":
		return !exists || !equalValue(actual, condition.Value), nil
	case "contains":
		if !exists || actual == nil {
			return false, nil
		}
		return containsValue(actual, condition.Value)
	case "gt", "gte", "lt", "lte":
		if !exists || actual == nil {
			return false, nil
		}
		return compareNumber(condition.Op, actual, condition.Value)
	default:
		return false, fmt.Errorf("unsupported workflow condition op %q", condition.Op)
	}
}

func validateCondition(condition Condition) error {
	if isExprCondition(condition) {
		if strings.TrimSpace(condition.Expr) == "" {
			return errors.New("workflow expr condition is required")
		}
		if len(condition.All) > 0 || len(condition.Any) > 0 || condition.Not != nil || condition.Field != "" || condition.Op != "" {
			return errors.New("workflow expr condition cannot mix with all, any, not, or field/op")
		}
		return nil
	}
	branches := 0
	if len(condition.All) > 0 {
		branches++
	}
	if len(condition.Any) > 0 {
		branches++
	}
	if condition.Not != nil {
		branches++
	}
	if condition.Field != "" || condition.Op != "" {
		branches++
	}
	if branches == 0 {
		return nil
	}
	if branches > 1 {
		return errors.New("workflow condition must use exactly one of all, any, not, or field/op")
	}
	for _, child := range condition.All {
		if err := validateCondition(child); err != nil {
			return err
		}
	}
	if len(condition.All) > 0 {
		return nil
	}
	for _, child := range condition.Any {
		if err := validateCondition(child); err != nil {
			return err
		}
	}
	if len(condition.Any) > 0 {
		return nil
	}
	if condition.Not != nil {
		return validateCondition(*condition.Not)
	}
	if condition.Field == "" {
		return errors.New("workflow condition field is required")
	}
	switch condition.Op {
	case "exists", "eq", "ne", "contains", "gt", "gte", "lt", "lte":
		return nil
	default:
		return fmt.Errorf("unsupported workflow condition op %q", condition.Op)
	}
}

func isExprCondition(condition Condition) bool {
	return strings.TrimSpace(condition.Mode) == "expr" || strings.TrimSpace(condition.Expr) != ""
}

func evalExprCondition(condition Condition, context Context) (bool, error) {
	env := exprEnv(context)
	program, err := expr.Compile(strings.TrimSpace(condition.Expr), expr.Env(env))
	if err != nil {
		return false, fmt.Errorf("compile workflow expr condition: %w", err)
	}
	output, err := expr.Run(program, env)
	if err != nil {
		return false, fmt.Errorf("run workflow expr condition: %w", err)
	}
	value, ok := output.(bool)
	if !ok {
		return false, fmt.Errorf("workflow expr condition must return bool, got %T", output)
	}
	return value, nil
}

func exprEnv(context Context) map[string]any {
	values := context.Values
	return map[string]any{
		"results":  mapValue(values, "results"),
		"params":   mapValue(values, "params"),
		"approval": mapValue(values, "approval"),
	}
}

func mapValue(values map[string]any, key string) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	if mapped, ok := values[key].(map[string]any); ok && mapped != nil {
		return mapped
	}
	return map[string]any{}
}

func lookup(values map[string]any, field string) (any, bool) {
	if field == "" || values == nil {
		return nil, false
	}
	var current any = values
	for _, part := range strings.Split(field, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func equalValue(left any, right any) bool {
	if lf, ok := toFloat(left); ok {
		if rf, ok := toFloat(right); ok {
			return lf == rf
		}
	}
	return reflect.DeepEqual(left, right)
}

func containsValue(actual any, expected any) (bool, error) {
	switch value := actual.(type) {
	case string:
		return strings.Contains(value, fmt.Sprint(expected)), nil
	case []string:
		expectedString := fmt.Sprint(expected)
		for _, item := range value {
			if item == expectedString {
				return true, nil
			}
		}
		return false, nil
	case []any:
		for _, item := range value {
			if equalValue(item, expected) {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, errors.New("contains workflow condition requires string or array value")
	}
}

func compareNumber(op string, left any, right any) (bool, error) {
	leftNumber, ok := toFloat(left)
	if !ok {
		return false, errors.New("workflow condition left value is not numeric")
	}
	rightNumber, ok := toFloat(right)
	if !ok {
		return false, errors.New("workflow condition right value is not numeric")
	}
	switch op {
	case "gt":
		return leftNumber > rightNumber, nil
	case "gte":
		return leftNumber >= rightNumber, nil
	case "lt":
		return leftNumber < rightNumber, nil
	case "lte":
		return leftNumber <= rightNumber, nil
	default:
		return false, fmt.Errorf("unsupported numeric workflow condition op %q", op)
	}
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
