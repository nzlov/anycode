package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nzlov/anycode/internal/application/apperror"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

const sideDeveloperInstructions = "This is a temporary Side question. Inspect the current workspace without modifying files or invoking state-changing dynamic tools."

type SideUseCase interface {
	StartSide(ctx context.Context, input StartSideInput) (SideRunDTO, error)
	ContinueSide(ctx context.Context, input ContinueSideInput) (SideRunDTO, error)
	StopSide(ctx context.Context, processRunID processdomain.RunID) error
	SideEvents(ctx context.Context, processRunID processdomain.RunID) (<-chan processdomain.CodexEvent, error)
}

type StartSideInput struct {
	SessionID domain.ID
	Prompt    string
}

type ContinueSideInput struct {
	SessionID      domain.ID
	CodexSessionID string
	Prompt         string
}

type SideRunDTO struct {
	CodexSessionID string
	ProcessRunID   processdomain.RunID
	TurnID         string
}

func (s *Service) StartSide(ctx context.Context, input StartSideInput) (SideRunDTO, error) {
	session, workdir, runID, prompt, err := s.prepareSideTurn(ctx, input.SessionID, input.Prompt)
	if err != nil {
		return SideRunDTO{}, err
	}
	if strings.TrimSpace(session.CodexSessionID) == "" {
		return SideRunDTO{}, sideValidationError(session.ID, "当前卡片还没有可用的 Codex 会话")
	}
	handle, err := s.codex.Fork(ctx, processdomain.CodexForkInput{
		SourceCodexSessionID: session.CodexSessionID,
		Ephemeral:            true,
		CodexStartInput:      sideStartInput(session, runID, workdir, prompt),
	})
	if err != nil {
		return SideRunDTO{}, fmt.Errorf("start temporary Side question: %w", err)
	}
	return sideRunDTO(handle), nil
}

func (s *Service) ContinueSide(ctx context.Context, input ContinueSideInput) (SideRunDTO, error) {
	session, workdir, runID, prompt, err := s.prepareSideTurn(ctx, input.SessionID, input.Prompt)
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
	start := sideStartInput(session, runID, workdir, prompt)
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

func (s *Service) prepareSideTurn(ctx context.Context, sessionID domain.ID, promptValue string) (domain.Session, string, processdomain.RunID, string, error) {
	if s == nil || s.repo == nil || s.codex == nil {
		return domain.Session{}, "", "", "", errors.New("session side usecase: repositories and Codex process are required")
	}
	prompt := strings.TrimSpace(promptValue)
	if prompt == "" {
		return domain.Session{}, "", "", "", sideValidationError(sessionID, "Side 提示词不能为空")
	}
	session, err := s.repo.Find(ctx, sessionID)
	if err != nil {
		return domain.Session{}, "", "", "", fmt.Errorf("find Side source session: %w", err)
	}
	if session.Mode != domain.ModeChat || session.Status == domain.StatusClosed {
		return domain.Session{}, "", "", "", sideValidationError(session.ID, "当前卡片不能发起 Side 提问")
	}
	workdir, err := s.sessionWorkdir(ctx, session)
	if err != nil {
		return domain.Session{}, "", "", "", err
	}
	generated, err := s.generateID()
	if err != nil {
		return domain.Session{}, "", "", "", fmt.Errorf("generate Side process run id: %w", err)
	}
	return session, workdir, processdomain.RunID(generated), prompt, nil
}

func sideStartInput(session domain.Session, runID processdomain.RunID, workdir string, prompt string) processdomain.CodexStartInput {
	return processdomain.CodexStartInput{
		ProcessRunID: runID, SessionID: processdomain.SessionID(session.ID), Workdir: workdir,
		Input:                 []processdomain.CodexInputItem{{Type: "text", Text: prompt}},
		DeveloperInstructions: sideDeveloperInstructions,
		Model:                 strings.TrimSpace(session.Config.CodexModel), ReasoningEffort: strings.TrimSpace(session.Config.ReasoningEffort),
		PermissionMode: "read-only", FastMode: session.Config.FastMode,
	}
}

func sideRunDTO(handle processdomain.CodexHandle) SideRunDTO {
	return SideRunDTO{CodexSessionID: handle.CodexSessionID, ProcessRunID: handle.ProcessRunID, TurnID: handle.TurnID}
}

func sideValidationError(sessionID domain.ID, message string) error {
	return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, message).WithDetails(map[string]any{"sessionId": string(sessionID)})
}
