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
	ListSides(ctx context.Context, sessionID domain.ID) ([]SideRunDTO, error)
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
	Prompt         string
	FollowUps      []string
	Status         domain.SideStatus
	Error          string
	Events         []processdomain.CodexEvent
}

func (s *Service) ListSides(ctx context.Context, sessionID domain.ID) ([]SideRunDTO, error) {
	if s == nil || s.repo == nil || s.sides == nil {
		return nil, errors.New("session side usecase: repositories are required")
	}
	if _, err := s.repo.Find(ctx, sessionID); err != nil {
		return nil, fmt.Errorf("find Side source session: %w", err)
	}
	items, err := s.sides.ListSides(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	result := make([]SideRunDTO, 0, len(items))
	for index := range items {
		dto, err := s.sideDTO(ctx, items[index])
		if err != nil {
			return nil, err
		}
		result = append(result, dto)
	}
	return result, nil
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
	source, run, err := s.claimSideRun(ctx, handle)
	if err != nil {
		s.discardSideRun(handle.ProcessRunID)
		return SideRunDTO{}, s.stopSideAfterStartFailure(ctx, handle.ProcessRunID, err)
	}
	now := s.now()
	side := domain.Side{
		ID: handle.CodexSessionID, SessionID: session.ID, ProcessRunID: string(handle.ProcessRunID), TurnID: handle.TurnID,
		Prompt: sidePrompt(input), FollowUps: []string{}, Status: domain.SideStatusRunning,
		TurnIndex: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.sides.CreateSide(ctx, side); err != nil {
		s.discardSideRun(handle.ProcessRunID)
		return SideRunDTO{}, s.stopSideAfterStartFailure(ctx, handle.ProcessRunID, err)
	}
	s.collectSideRun(side, source, run)
	return sideRunDTO(side, nil), nil
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
	side, err := s.sides.FindSide(ctx, threadID)
	if err != nil {
		return SideRunDTO{}, err
	}
	if side.SessionID != input.SessionID {
		return SideRunDTO{}, sideValidationError(session.ID, "Side 不属于当前卡片")
	}
	if side.Status == domain.SideStatusRunning {
		return SideRunDTO{}, sideValidationError(session.ID, "Side 仍在运行")
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
	source, run, err := s.claimSideRun(ctx, handle)
	if err != nil {
		s.discardSideRun(handle.ProcessRunID)
		return SideRunDTO{}, s.stopSideAfterStartFailure(ctx, handle.ProcessRunID, err)
	}
	side, err = s.sides.BeginSideTurn(ctx, side.ID, string(handle.ProcessRunID), handle.TurnID, sidePrompt(input), s.now())
	if err != nil {
		s.discardSideRun(handle.ProcessRunID)
		return SideRunDTO{}, s.stopSideAfterStartFailure(ctx, handle.ProcessRunID, err)
	}
	s.collectSideRun(side, source, run)
	dto, err := s.sideDTO(ctx, side)
	if err != nil {
		return SideRunDTO{}, err
	}
	return dto, nil
}

func (s *Service) StopSide(ctx context.Context, processRunID processdomain.RunID) error {
	if s == nil || s.codex == nil || s.sides == nil {
		return errors.New("session side usecase: Codex process and repository are required")
	}
	if processRunID == "" {
		return nil
	}
	side, err := s.sides.FindSideByProcessRun(ctx, string(processRunID))
	if err != nil {
		return err
	}
	if err := s.stopEphemeralSide(ctx, processRunID); err != nil {
		return err
	}
	if err := s.waitForSideRun(ctx, processRunID); err != nil {
		return err
	}
	if err := s.sides.DeleteSide(ctx, side.ID); err != nil {
		return fmt.Errorf("delete session Side: %w", err)
	}
	return nil
}

func (s *Service) SideEvents(ctx context.Context, processRunID processdomain.RunID) (<-chan processdomain.CodexEvent, error) {
	if s == nil || s.sides == nil {
		return nil, errors.New("session side usecase: Side repository is required")
	}
	if processRunID == "" {
		return nil, sideValidationError("", "Side 运行 ID 不能为空")
	}
	live, unsubscribe := s.subscribeSideRun(processRunID)
	stored, err := s.sides.ListSideRunEvents(ctx, string(processRunID))
	if err != nil {
		unsubscribe()
		return nil, err
	}
	backlog, err := decodeSideEvents(stored)
	if err != nil {
		unsubscribe()
		return nil, err
	}
	out := make(chan processdomain.CodexEvent)
	go forwardSideEvents(ctx, out, backlog, live, unsubscribe)
	return out, nil
}

func (s *Service) prepareSideTurn(ctx context.Context, input SideInput) (domain.Session, processdomain.CodexStartInput, error) {
	var start processdomain.CodexStartInput
	if s == nil || s.repo == nil || s.sides == nil || s.codex == nil {
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
	if session.Mode != domain.ModeChat || session.Status == domain.StatusClosed || session.Status == domain.StatusStopping {
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

func sideRunDTO(side domain.Side, events []processdomain.CodexEvent) SideRunDTO {
	return SideRunDTO{
		CodexSessionID: side.ID, ProcessRunID: processdomain.RunID(side.ProcessRunID), TurnID: side.TurnID,
		Prompt: side.Prompt, FollowUps: append([]string(nil), side.FollowUps...), Status: side.Status,
		Error: side.Error, Events: events,
	}
}

func sidePrompt(input SideInput) string {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt != "" || len(input.Files) == 0 {
		return prompt
	}
	names := make([]string, 0, len(input.Files))
	for _, file := range input.Files {
		if name := strings.TrimSpace(file.Filename); name != "" {
			names = append(names, name)
		}
	}
	return "请查看附件：" + strings.Join(names, "、")
}

func sideValidationError(sessionID domain.ID, message string) error {
	return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, message).WithDetails(map[string]any{"sessionId": string(sessionID)})
}
