package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nzlov/anycode/internal/application/apperror"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

type mcpHandler struct {
	sessions sessionapp.UseCase
}

func newMCPHandler(sessions sessionapp.UseCase) http.Handler {
	return mcpHandler{sessions: sessions}
}

func (h mcpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMCPError(w, nil, -32700, "parse error")
		return
	}
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		writeMCPResult(w, req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "anycode", "version": "0.1.0"},
		})
	case "tools/list":
		writeMCPResult(w, req.ID, map[string]any{"tools": []map[string]any{answerUserTool()}})
	case "tools/call":
		result, err := h.callTool(r.Context(), r.PathValue("sessionID"), req.Params)
		if err != nil {
			writeMCPApplicationError(w, req.ID, -32602, err)
			return
		}
		writeMCPResult(w, req.ID, result)
	default:
		writeMCPError(w, req.ID, -32601, "method not found")
	}
}

func (h mcpHandler) callTool(ctx context.Context, sessionID string, raw json.RawMessage) (map[string]any, error) {
	if h.sessions == nil {
		return nil, apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "session service unavailable").WithRetryable(true)
	}
	var params mcpToolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "invalid tool call params")
	}
	if params.Name != "answer_user" {
		return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "unknown tool").WithDetails(map[string]any{"tool": params.Name})
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session id is required")
	}
	questions, err := buildQuestions(params.Arguments.Questions)
	if err != nil {
		return nil, err
	}
	batch, err := h.sessions.RequestUserAnswer(ctx, sessionapp.RequestUserAnswerInput{
		SessionID: sessiondomain.ID(sessionID),
		Questions: questions,
	})
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"batchId": string(batch.ID),
		"status":  "suspended",
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(payload)}},
		"isError": false,
	}, nil
}

func buildQuestions(inputs []mcpQuestionInput) ([]questiondomain.Question, error) {
	if len(inputs) == 0 {
		return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "questions are required")
	}
	questions := make([]questiondomain.Question, 0, len(inputs))
	for _, input := range inputs {
		title := strings.TrimSpace(input.Title)
		body := strings.TrimSpace(input.Body)
		if title == "" && body == "" {
			return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question title or body is required")
		}
		questionType := strings.TrimSpace(input.Type)
		if questionType == "" {
			questionType = "choice"
		}
		options := make([]questiondomain.Option, 0, len(input.Options))
		for _, option := range input.Options {
			id := strings.TrimSpace(option.ID)
			if id == "" {
				id = strings.TrimSpace(option.Label)
			}
			if id == "" {
				return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question option id or label is required")
			}
			options = append(options, questiondomain.Option{
				ID:          questiondomain.OptionID(id),
				Label:       option.Label,
				Description: option.Description,
				Payload:     option.Payload,
			})
		}
		questions = append(questions, questiondomain.Question{
			Title:       title,
			Body:        body,
			Type:        questionType,
			Options:     options,
			AllowCustom: input.AllowCustom,
			Status:      string(questiondomain.BatchPending),
		})
	}
	return questions, nil
}

func answerUserTool() map[string]any {
	return map[string]any{
		"name":        "answer_user",
		"description": "Ask the user one or more option questions and wait for their answers.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"title":       map[string]any{"type": "string"},
							"body":        map[string]any{"type": "string"},
							"type":        map[string]any{"type": "string"},
							"allowCustom": map[string]any{"type": "boolean"},
							"options": map[string]any{
								"type": "array",
								"items": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"id":          map[string]any{"type": "string"},
										"label":       map[string]any{"type": "string"},
										"description": map[string]any{"type": "string"},
										"payload":     map[string]any{"type": "object"},
									},
									"required": []string{"label"},
								},
							},
						},
					},
				},
			},
			"required": []string{"questions"},
		},
	}
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpToolCallParams struct {
	Name      string           `json:"name"`
	Arguments mcpToolArguments `json:"arguments"`
}

type mcpToolArguments struct {
	Questions []mcpQuestionInput `json:"questions"`
}

type mcpQuestionInput struct {
	Title       string           `json:"title"`
	Body        string           `json:"body"`
	Type        string           `json:"type"`
	Options     []mcpOptionInput `json:"options"`
	AllowCustom bool             `json:"allowCustom"`
}

type mcpOptionInput struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Payload     map[string]any `json:"payload"`
}

func writeMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeMCPResponse(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  result,
	})
}

func writeMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	writeMCPResponse(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func writeMCPApplicationError(w http.ResponseWriter, id json.RawMessage, code int, err error) {
	appErr, ok := apperror.From(err)
	if !ok {
		appErr = apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "request failed")
	}
	data := map[string]any{
		"code":       appErr.Code,
		"category":   string(appErr.Category),
		"retryable":  appErr.Retryable,
		"userAction": appErr.UserAction,
	}
	if details := appErr.PublicDetails(); len(details) > 0 {
		data["details"] = details
	}
	writeMCPResponse(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]any{
			"code":    code,
			"message": appErr.PublicMessage(),
			"data":    data,
		},
	})
}

func writeMCPResponse(w http.ResponseWriter, response map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
