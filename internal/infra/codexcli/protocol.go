package codexcli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

type appServerTurn struct {
	ID          string         `json:"id"`
	Status      string         `json:"status"`
	CompletedAt *int64         `json:"completedAt"`
	Error       map[string]any `json:"error"`
}

func (r *appServerRuntime) handleNotification(method string, raw json.RawMessage) {
	var identity struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
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
	if method == "turn/completed" {
		r.completeTurn(route, raw)
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
	result := process.ExitResult{FinishedAt: finishedAt}
	switch params.Turn.Status {
	case "failed":
		result.FailureCode = "turn_failed"
		result.FailureReason = stringValue(params.Turn.Error, "message")
		if result.FailureReason == "" {
			result.FailureReason = "Codex turn failed"
		}
	case "interrupted":
		result.FailureCode = "turn_interrupted"
	}
	route.finish(result)
}

func stringValue(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := value[key].(string); ok {
			return text
		}
	}
	return ""
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
