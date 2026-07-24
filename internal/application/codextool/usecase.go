package codextool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	tunnelapp "github.com/nzlov/anycode/internal/application/tunnel"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	tunneldomain "github.com/nzlov/anycode/internal/domain/tunnel"
)

const (
	questionsTool       = "questions"
	publishArtifactTool = "publish_artifact"
	tunnelCreateTool    = "tunnel_create"
	tunnelListTool      = "tunnel_list"
	tunnelCloseTool     = "tunnel_close"
)

type SessionUseCase interface {
	RequestQuestions(ctx context.Context, input sessionapp.RequestQuestionsInput) (questionapp.RequestDTO, error)
}

type ArtifactUseCase interface {
	Publish(ctx context.Context, input artifactapp.PublishInput) (sessiondomain.SessionFile, error)
	ReadToolContent(ctx context.Context, id sessiondomain.SessionFileID) (artifactapp.ToolContent, bool, error)
}

type TunnelUseCase interface {
	Create(ctx context.Context, input tunnelapp.CreateInput) (tunnelapp.CreateResult, error)
	List(ctx context.Context) ([]tunnelapp.DTO, error)
	CloseOwned(ctx context.Context, sessionID tunneldomain.SessionID, id tunneldomain.ID) error
}

type Service struct {
	sessions  SessionUseCase
	artifacts ArtifactUseCase
	tunnels   TunnelUseCase
}

type Option func(*Service)

func WithTunnels(tunnels TunnelUseCase) Option {
	return func(s *Service) { s.tunnels = tunnels }
}

func New(sessions SessionUseCase, artifacts ArtifactUseCase, options ...Option) *Service {
	service := &Service{sessions: sessions, artifacts: artifacts}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) HandleDynamicTool(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	switch call.Tool {
	case questionsTool:
		return s.questions(ctx, call)
	case publishArtifactTool:
		return s.publishArtifact(ctx, call)
	case tunnelCreateTool:
		return s.createTunnel(ctx, call)
	case tunnelListTool:
		return s.listTunnels(ctx, call)
	case tunnelCloseTool:
		return s.closeTunnel(ctx, call)
	default:
		return processdomain.DynamicToolResult{}, fmt.Errorf("unknown dynamic tool %q", call.Tool)
	}
}

func (s *Service) createTunnel(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.tunnels == nil {
		return processdomain.DynamicToolResult{}, errors.New("tunnel service is unavailable")
	}
	var input struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode tunnel_create arguments: %w", err)
	}
	created, err := s.tunnels.Create(ctx, tunnelapp.CreateInput{
		SessionID: tunneldomain.SessionID(call.SessionID), Name: input.Name, Port: input.Port,
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"id": created.Tunnel.ID, "name": created.Tunnel.Name, "url": created.AccessURL, "publicUrl": created.Tunnel.URL,
		"hostname": created.Tunnel.Hostname, "port": created.Tunnel.Port, "status": created.Tunnel.Status,
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode tunnel_create result: %w", err)
	}
	return textResult(payload), nil
}

func (s *Service) listTunnels(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.tunnels == nil {
		return processdomain.DynamicToolResult{}, errors.New("tunnel service is unavailable")
	}
	items, err := s.tunnels.List(ctx)
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.SessionID != tunneldomain.SessionID(call.SessionID) {
			continue
		}
		result = append(result, map[string]any{
			"id": item.ID, "name": item.Name, "url": item.AccessURL, "publicUrl": item.URL, "hostname": item.Hostname,
			"port": item.Port, "status": item.Status, "createdAt": item.CreatedAt,
		})
	}
	payload, err := json.Marshal(map[string]any{"tunnels": result})
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode tunnel_list result: %w", err)
	}
	return textResult(payload), nil
}

func (s *Service) closeTunnel(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.tunnels == nil {
		return processdomain.DynamicToolResult{}, errors.New("tunnel service is unavailable")
	}
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode tunnel_close arguments: %w", err)
	}
	id := tunneldomain.ID(strings.TrimSpace(input.ID))
	if id == "" {
		return processdomain.DynamicToolResult{}, errors.New("tunnel id is required")
	}
	if err := s.tunnels.CloseOwned(ctx, tunneldomain.SessionID(call.SessionID), id); err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	payload, _ := json.Marshal(map[string]any{"id": id, "closed": true})
	return textResult(payload), nil
}

func (s *Service) questions(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.sessions == nil {
		return processdomain.DynamicToolResult{}, errors.New("session service is unavailable")
	}
	if strings.TrimSpace(call.CallID) == "" || call.SessionID == "" {
		return processdomain.DynamicToolResult{}, errors.New("questions call id and session id are required")
	}
	var input questionsInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode questions arguments: %w", err)
	}
	questions, err := buildQuestions(input.Questions)
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	request, err := s.sessions.RequestQuestions(ctx, sessionapp.RequestQuestionsInput{
		RequestID: questiondomain.RequestID(call.CallID),
		SessionID: sessiondomain.ID(call.SessionID),
		Questions: questions,
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	payload, err := json.Marshal(questionResult(request))
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode questions result: %w", err)
	}
	return textResult(payload), nil
}

func (s *Service) publishArtifact(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.artifacts == nil {
		return processdomain.DynamicToolResult{}, errors.New("artifact service is unavailable")
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode publish_artifact arguments: %w", err)
	}
	path := strings.TrimSpace(input.Path)
	clean := filepath.Clean(path)
	if path == "" || filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return processdomain.DynamicToolResult{}, errors.New("artifact path must be relative to ANYCODE_ARTIFACT_DIR")
	}
	artifact, err := s.artifacts.Publish(ctx, artifactapp.PublishInput{SessionID: sessiondomain.ID(call.SessionID), Path: path})
	if err != nil {
		return processdomain.DynamicToolResult{}, err
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
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode publish_artifact result: %w", err)
	}
	result := textResult(payload)
	media, ok, err := s.artifacts.ReadToolContent(ctx, artifact.ID)
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	if !ok {
		return result, nil
	}
	dataURL := "data:" + media.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(media.Data)
	switch media.Type {
	case "image":
		result.Content = append(result.Content, processdomain.DynamicToolContent{Type: "inputImage", ImageURL: dataURL})
	case "audio":
		result.Content = append(result.Content, processdomain.DynamicToolContent{Type: "inputAudio", AudioURL: dataURL})
	}
	return result, nil
}

type questionsInput struct {
	Questions []questionInput `json:"questions"`
}

type questionInput struct {
	Body    string        `json:"body"`
	Type    string        `json:"type"`
	Options []optionInput `json:"options"`
}

type optionInput struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Payload     map[string]any `json:"payload"`
}

func buildQuestions(inputs []questionInput) ([]questiondomain.Question, error) {
	if len(inputs) == 0 {
		return nil, errors.New("questions are required")
	}
	questions := make([]questiondomain.Question, 0, len(inputs))
	for _, input := range inputs {
		body := strings.TrimSpace(input.Body)
		if body == "" {
			return nil, errors.New("question body is required")
		}
		questionType := strings.TrimSpace(input.Type)
		if questionType == "" {
			questionType = "choice"
		}
		options := make([]questiondomain.Option, 0, len(input.Options))
		for _, inputOption := range input.Options {
			id := strings.TrimSpace(inputOption.ID)
			if id == "" {
				id = strings.TrimSpace(inputOption.Label)
			}
			if id == "" {
				return nil, errors.New("question option id or label is required")
			}
			options = append(options, questiondomain.Option{
				ID:          questiondomain.OptionID(id),
				Label:       inputOption.Label,
				Description: inputOption.Description,
				Payload:     inputOption.Payload,
			})
		}
		questions = append(questions, questiondomain.Question{
			Body: body, Type: questionType, Options: options, Status: string(questiondomain.RequestPending),
		})
	}
	return questions, nil
}

func questionResult(request questionapp.RequestDTO) map[string]any {
	answers := make([]map[string]any, 0, len(request.Questions))
	for _, question := range request.Questions {
		answer := map[string]any{
			"questionId":   string(question.ID),
			"customAnswer": question.CustomAnswer,
			"payload":      question.Answer,
		}
		if question.SelectedOptionID != nil {
			answer["selectedOptionId"] = string(*question.SelectedOptionID)
		}
		answers = append(answers, answer)
	}
	return map[string]any{"requestId": string(request.ID), "answers": answers}
}

func textResult(payload []byte) processdomain.DynamicToolResult {
	return processdomain.DynamicToolResult{
		Success: true,
		Content: []processdomain.DynamicToolContent{{Type: "inputText", Text: string(payload)}},
	}
}
