package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/nzlov/anycode/internal/application/apperror"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

const sideDeveloperInstructions = "This is a temporary Side question. Inspect the current workspace without modifying files or invoking state-changing dynamic tools."

type SideUseCase interface {
	StartSide(ctx context.Context, input SideInput) (SideRunDTO, error)
	ContinueSide(ctx context.Context, input SideInput) (SideRunDTO, error)
	StopSide(ctx context.Context, processRunID processdomain.RunID) error
	SideEvents(ctx context.Context, processRunID processdomain.RunID) (<-chan processdomain.CodexEvent, error)
}

type SideInput struct {
	SessionID      domain.ID
	CodexSessionID string
	Prompt         string
	Config         *SideConfigInput
	Files          []SideFileInput
}

type SideConfigInput struct {
	CodexModel      string
	ReasoningEffort string
	FastMode        bool
}

type SideFileInput struct {
	Filename string
	MimeType string
	Reader   io.Reader
}

type SideRunDTO struct {
	CodexSessionID string
	ProcessRunID   processdomain.RunID
	TurnID         string
}

func (s *Service) StartSide(ctx context.Context, input SideInput) (SideRunDTO, error) {
	session, start, err := s.prepareSideTurn(ctx, input)
	if err != nil {
		return SideRunDTO{}, err
	}
	if strings.TrimSpace(session.CodexSessionID) == "" {
		return SideRunDTO{}, sideValidationError(session.ID, "当前卡片还没有可用的 Codex 会话")
	}
	handle, err := s.codex.Fork(ctx, processdomain.CodexForkInput{
		SourceCodexSessionID: session.CodexSessionID,
		Ephemeral:            true,
		CodexStartInput:      start,
	})
	if err != nil {
		return SideRunDTO{}, fmt.Errorf("start temporary Side question: %w", err)
	}
	return sideRunDTO(handle), nil
}

func (s *Service) ContinueSide(ctx context.Context, input SideInput) (SideRunDTO, error) {
	session, start, err := s.prepareSideTurn(ctx, input)
	if err != nil {
		return SideRunDTO{}, err
	}
	threadID := strings.TrimSpace(input.CodexSessionID)
	if threadID == "" {
		return SideRunDTO{}, sideValidationError(session.ID, "Side 会话 ID 不能为空")
	}
	ephemeral, ok := s.codex.(processdomain.CodexEphemeralThread)
	if !ok {
		return SideRunDTO{}, errors.New("Codex runtime does not support temporary Side continuation")
	}
	// GLUE: Start and loaded-thread continuation use parallel Codex input structs; remove this mapping when the process port shares one turn input.
	handle, err := ephemeral.ContinueLoaded(ctx, processdomain.CodexResumeInput{
		ProcessRunID: start.ProcessRunID, SessionID: start.SessionID, CodexSessionID: threadID,
		Workdir: start.Workdir, Input: start.Input, DeveloperInstructions: start.DeveloperInstructions,
		Model: start.Model, ReasoningEffort: start.ReasoningEffort, PermissionMode: start.PermissionMode,
		FastMode: start.FastMode,
	})
	if err != nil {
		return SideRunDTO{}, fmt.Errorf("continue temporary Side question: %w", err)
	}
	return sideRunDTO(handle), nil
}

func (s *Service) StopSide(ctx context.Context, processRunID processdomain.RunID) error {
	if s == nil || s.codex == nil {
		return errors.New("session side usecase: Codex process is required")
	}
	if processRunID == "" {
		return nil
	}
	ephemeral, ok := s.codex.(processdomain.CodexEphemeralThread)
	if !ok {
		return errors.New("Codex runtime does not support temporary Side questions")
	}
	if err := ephemeral.StopEphemeral(ctx, processRunID); err != nil && !errors.Is(err, processdomain.ErrProcessNotFound) {
		return fmt.Errorf("stop temporary Side question: %w", err)
	}
	return nil
}

func (s *Service) SideEvents(ctx context.Context, processRunID processdomain.RunID) (<-chan processdomain.CodexEvent, error) {
	if s == nil || s.codex == nil {
		return nil, errors.New("session side usecase: Codex process is required")
	}
	if processRunID == "" {
		return nil, sideValidationError("", "Side 运行 ID 不能为空")
	}
	ephemeral, ok := s.codex.(processdomain.CodexEphemeralThread)
	if !ok {
		return nil, errors.New("Codex runtime does not support temporary Side questions")
	}
	return ephemeral.EphemeralEvents(ctx, processRunID)
}

func (s *Service) prepareSideTurn(ctx context.Context, input SideInput) (domain.Session, processdomain.CodexStartInput, error) {
	var start processdomain.CodexStartInput
	if s == nil || s.repo == nil || s.codex == nil {
		return domain.Session{}, start, errors.New("session side usecase: repositories and Codex process are required")
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" && len(input.Files) == 0 {
		return domain.Session{}, start, sideValidationError(input.SessionID, "请输入 Side 提示词或添加附件")
	}
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return domain.Session{}, start, fmt.Errorf("find Side source session: %w", err)
	}
	if session.Mode != domain.ModeChat || session.Status == domain.StatusClosed {
		return domain.Session{}, start, sideValidationError(session.ID, "当前卡片不能发起 Side 提问")
	}
	workdir, err := s.sessionWorkdir(ctx, session)
	if err != nil {
		return domain.Session{}, start, err
	}
	generated, err := s.generateID()
	if err != nil {
		return domain.Session{}, start, fmt.Errorf("generate Side process run id: %w", err)
	}
	config := session.Config
	if input.Config != nil {
		config.CodexModel = strings.TrimSpace(input.Config.CodexModel)
		config.ReasoningEffort = strings.TrimSpace(input.Config.ReasoningEffort)
		config.FastMode = input.Config.FastMode
		if config.CodexModel == "" || config.ReasoningEffort == "" {
			return domain.Session{}, start, sideValidationError(session.ID, "Side 模型和思考强度不能为空")
		}
	}
	files := make([]domain.SessionFile, 0, len(input.Files))
	for index, file := range input.Files {
		if file.Reader == nil || strings.TrimSpace(file.Filename) == "" {
			return domain.Session{}, start, sideValidationError(session.ID, "Side 附件无效")
		}
		if err := ctx.Err(); err != nil {
			return domain.Session{}, start, err
		}
		data, err := io.ReadAll(file.Reader)
		if err != nil {
			return domain.Session{}, start, fmt.Errorf("read Side attachment %q: %w", file.Filename, err)
		}
		mimeType := file.MimeType
		if mimeType == "" {
			mimeType = mime.TypeByExtension(filepath.Ext(file.Filename))
		}
		files = append(files, domain.SessionFile{
			ID:       domain.SessionFileID(fmt.Sprintf("side-upload-%d", index)),
			Filename: file.Filename, MimeType: mimeType, InlineData: data,
		})
	}
	start = processdomain.CodexStartInput{
		ProcessRunID: processdomain.RunID(generated), SessionID: processdomain.SessionID(session.ID), Workdir: workdir,
		Input:                 codexInput(prompt, files, nil),
		DeveloperInstructions: sideDeveloperInstructions,
		Model:                 strings.TrimSpace(config.CodexModel), ReasoningEffort: strings.TrimSpace(config.ReasoningEffort),
		PermissionMode: "read-only", FastMode: config.FastMode,
	}
	return session, start, nil
}

func sideRunDTO(handle processdomain.CodexHandle) SideRunDTO {
	return SideRunDTO{CodexSessionID: handle.CodexSessionID, ProcessRunID: handle.ProcessRunID, TurnID: handle.TurnID}
}

func sideValidationError(sessionID domain.ID, message string) error {
	return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, message).WithDetails(map[string]any{"sessionId": string(sessionID)})
}
