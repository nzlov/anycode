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
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

const (
	questionsTool       = "questions"
	publishArtifactTool = "publish_artifact"
)

type SessionUseCase interface {
	RequestQuestions(ctx context.Context, input sessionapp.RequestQuestionsInput) (questionapp.RequestDTO, error)
}

type ArtifactUseCase interface {
	Publish(ctx context.Context, input artifactapp.PublishInput) (sessiondomain.SessionFile, error)
	ReadToolContent(ctx context.Context, id sessiondomain.SessionFileID) (artifactapp.ToolContent, bool, error)
}

type Service struct {
	sessions  SessionUseCase
	artifacts ArtifactUseCase
}

func New(sessions SessionUseCase, artifacts ArtifactUseCase) *Service {
	return &Service{sessions: sessions, artifacts: artifacts}
}

func (s *Service) HandleDynamicTool(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	switch call.Tool {
	case questionsTool:
		return s.questions(ctx, call)
	case publishArtifactTool:
		return s.publishArtifact(ctx, call)
	default:
		return processdomain.DynamicToolResult{}, fmt.Errorf("unknown dynamic tool %q", call.Tool)
	}
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
	Title   string        `json:"title"`
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
		title := strings.TrimSpace(input.Title)
		body := strings.TrimSpace(input.Body)
		if title == "" && body == "" {
			return nil, errors.New("question title or body is required")
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
			Title: title, Body: body, Type: questionType, Options: options, Status: string(questiondomain.RequestPending),
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
