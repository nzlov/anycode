package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nzlov/anycode/internal/application/apperror"
	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

type mcpHandler struct {
	sessions  sessionapp.UseCase
	artifacts artifactapp.UseCase
}

const mcpTransportHeader = "X-AnyCode-MCP-Transport"

func newMCPHandler(sessions sessionapp.UseCase, artifacts ...artifactapp.UseCase) http.Handler {
	handler := mcpHandler{sessions: sessions}
	if len(artifacts) > 0 {
		handler.artifacts = artifacts[0]
	}
	return handler
}

func (h mcpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if batchID := strings.TrimSpace(r.PathValue("batchID")); batchID != "" {
		switch r.PathValue("action") {
		case "fail":
			h.failDelivery(w, r, batchID)
		case "", "ack":
			h.acknowledgeDelivery(w, r, batchID)
		default:
			http.NotFound(w, r)
		}
		return
	}
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
		_ = writeMCPResult(w, req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "anycode", "version": "0.1.0"},
		})
	case "tools/list":
		_ = writeMCPResult(w, req.ID, map[string]any{"tools": []map[string]any{answerUserTool(), publishArtifactTool()}})
	case "tools/call":
		result, batchID, directDelivery, err := h.callTool(r.Context(), r.PathValue("sessionID"), req.Params)
		if err != nil {
			writeMCPApplicationError(w, req.ID, -32602, err)
			return
		}
		if err := writeMCPResult(w, req.ID, result); err != nil {
			if directDelivery {
				h.failDirectDelivery(r, batchID)
			}
			return
		}
		if directDelivery && r.Header.Get(mcpTransportHeader) != "stdio" {
			if err := http.NewResponseController(w).Flush(); err != nil {
				h.failDirectDelivery(r, batchID)
				return
			}
			if err := h.sessions.AcknowledgeUserAnswerDelivery(context.WithoutCancel(r.Context()), sessionapp.AcknowledgeUserAnswerDeliveryInput{
				SessionID: sessiondomain.ID(strings.TrimSpace(r.PathValue("sessionID"))),
				BatchID:   questiondomain.BatchID(batchID),
			}); err != nil {
				log.Printf("acknowledge direct MCP answer_user delivery: %v", err)
			}
		}
	default:
		writeMCPError(w, req.ID, -32601, "method not found")
	}
}

func (h mcpHandler) failDelivery(w http.ResponseWriter, r *http.Request, batchID string) {
	if h.sessions == nil {
		http.Error(w, "session service unavailable", http.StatusServiceUnavailable)
		return
	}
	err := h.sessions.FailUserAnswerDelivery(context.WithoutCancel(r.Context()), sessionapp.FailUserAnswerDeliveryInput{
		SessionID: sessiondomain.ID(strings.TrimSpace(r.PathValue("sessionID"))),
		BatchID:   questiondomain.BatchID(batchID),
		Kind:      sessionapp.UserAnswerDeliveryTransportClosed,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h mcpHandler) failDirectDelivery(r *http.Request, batchID questiondomain.BatchID) {
	err := h.sessions.FailUserAnswerDelivery(context.WithoutCancel(r.Context()), sessionapp.FailUserAnswerDeliveryInput{
		SessionID: sessiondomain.ID(strings.TrimSpace(r.PathValue("sessionID"))),
		BatchID:   batchID,
		Kind:      sessionapp.UserAnswerDeliveryTransportClosed,
	})
	if err != nil {
		log.Printf("fail direct MCP answer_user delivery: %v", err)
	}
}

func (h mcpHandler) callTool(ctx context.Context, sessionID string, raw json.RawMessage) (map[string]any, questiondomain.BatchID, bool, error) {
	var params mcpToolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, "", false, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "invalid tool call params")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, "", false, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "session id is required")
	}
	switch params.Name {
	case "answer_user":
		return h.callAnswerUser(ctx, sessionID, params.Arguments)
	case "publish_artifact":
		result, err := h.callPublishArtifact(ctx, sessionID, params.Arguments)
		return result, "", false, err
	default:
		return nil, "", false, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "unknown tool").WithDetails(map[string]any{"tool": params.Name})
	}
}

func (h mcpHandler) callAnswerUser(ctx context.Context, sessionID string, arguments mcpToolArguments) (map[string]any, questiondomain.BatchID, bool, error) {
	if h.sessions == nil {
		return nil, "", false, apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "session service unavailable").WithRetryable(true)
	}
	questions, err := buildQuestions(arguments.Questions)
	if err != nil {
		return nil, "", false, err
	}
	batch, err := h.sessions.RequestUserAnswer(ctx, sessionapp.RequestUserAnswerInput{
		SessionID: sessiondomain.ID(sessionID),
		Questions: questions,
	})
	if err != nil {
		logMCPToolFailure("request_user_answer", sessionID, err)
		return nil, "", false, err
	}
	directDelivery := batch.Status == questiondomain.BatchAnswered && batch.DeliveryStatus == questiondomain.DeliveryInflight
	resultPayload := map[string]any{"batchId": string(batch.ID), "status": "suspended"}
	if directDelivery {
		resultPayload["status"] = "answered"
		answers := make([]map[string]any, 0, len(batch.Questions))
		for _, question := range batch.Questions {
			answer := map[string]any{
				"questionId":   question.ID,
				"customAnswer": question.CustomAnswer,
				"payload":      question.Answer,
			}
			if question.SelectedOptionID != nil {
				answer["selectedOptionId"] = *question.SelectedOptionID
			}
			answers = append(answers, answer)
		}
		resultPayload["answers"] = answers
	}
	payload, err := json.Marshal(resultPayload)
	if err != nil {
		return nil, "", false, err
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(payload)}},
		"isError": false,
	}, batch.ID, directDelivery, nil
}

func logMCPToolFailure(stage string, sessionID string, err error) {
	appErr, ok := apperror.From(err)
	if !ok {
		appErr = apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "request failed")
	}
	log.Printf("mcp tool failed: pid=%d session_id=%s stage=%s code=%s category=%s retryable=%t", os.Getpid(), sessionID, stage, appErr.Code, appErr.Category, appErr.Retryable)
}

func (h mcpHandler) acknowledgeDelivery(w http.ResponseWriter, r *http.Request, batchID string) {
	if h.sessions == nil {
		http.Error(w, "session service unavailable", http.StatusServiceUnavailable)
		return
	}
	err := h.sessions.AcknowledgeUserAnswerDelivery(context.WithoutCancel(r.Context()), sessionapp.AcknowledgeUserAnswerDeliveryInput{
		SessionID: sessiondomain.ID(strings.TrimSpace(r.PathValue("sessionID"))),
		BatchID:   questiondomain.BatchID(batchID),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h mcpHandler) callPublishArtifact(ctx context.Context, sessionID string, arguments mcpToolArguments) (map[string]any, error) {
	if h.artifacts == nil {
		return nil, apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "artifact service unavailable").WithRetryable(true)
	}
	path := strings.TrimSpace(arguments.Path)
	if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator)) {
		return nil, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "artifact path must be relative to ANYCODE_ARTIFACT_DIR")
	}
	artifact, err := h.artifacts.Publish(ctx, artifactapp.PublishInput{
		SessionID: sessiondomain.ID(sessionID),
		Path:      path,
	})
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{
		"id":           string(artifact.ID),
		"logicalPath":  artifact.LogicalPath,
		"filename":     artifact.Filename,
		"mimeType":     artifact.MimeType,
		"artifactKind": string(artifact.ArtifactKind),
		"previewKind":  string(artifact.PreviewKind),
		"size":         artifact.Size,
	})
	if err != nil {
		return nil, err
	}
	content := []map[string]any{{"type": "text", "text": string(payload)}}
	if media, ok, readErr := h.artifacts.ReadMCPContent(ctx, artifact.ID); readErr == nil && ok {
		content = append(content, map[string]any{
			"type":     media.Type,
			"data":     base64.StdEncoding.EncodeToString(media.Data),
			"mimeType": media.MIMEType,
		})
	}
	return map[string]any{
		"content": content,
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
			Title:   title,
			Body:    body,
			Type:    questionType,
			Options: options,
			Status:  string(questiondomain.BatchPending),
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
							"title": map[string]any{"type": "string"},
							"body":  map[string]any{"type": "string"},
							"type":  map[string]any{"type": "string"},
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

func publishArtifactTool() map[string]any {
	return map[string]any{
		"name":        "publish_artifact",
		"description": "Inspect a file in this card's ANYCODE_ARTIFACT_DIR and return its stable path-based metadata and preview content.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Path relative to ANYCODE_ARTIFACT_DIR."},
			},
			"required": []string{"path"},
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
	Path      string             `json:"path"`
}

type mcpQuestionInput struct {
	Title   string           `json:"title"`
	Body    string           `json:"body"`
	Type    string           `json:"type"`
	Options []mcpOptionInput `json:"options"`
}

type mcpOptionInput struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Payload     map[string]any `json:"payload"`
}

func writeMCPResult(w http.ResponseWriter, id json.RawMessage, result any) error {
	return writeMCPResponse(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"result":  result,
	})
}

func writeMCPError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	_ = writeMCPResponse(w, map[string]any{
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
	_ = writeMCPResponse(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error": map[string]any{
			"code":    code,
			"message": appErr.PublicMessage(),
			"data":    data,
		},
	})
}

func writeMCPResponse(w http.ResponseWriter, response map[string]any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(response)
}
