package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

type UseCase interface {
	CreateSession(ctx context.Context, input CreateSessionInput) (DTO, error)
	StartSession(ctx context.Context, id domain.ID) (DTO, error)
	StopSession(ctx context.Context, id domain.ID) (DTO, error)
	ResumeSession(ctx context.Context, id domain.ID) (DTO, error)
	CloseSession(ctx context.Context, input CloseSessionInput) (DTO, error)
	AppendPrompt(ctx context.Context, input AppendPromptInput) (PromptAppendDTO, error)
	GetSession(ctx context.Context, id domain.ID) (DetailDTO, error)
	ListSessions(ctx context.Context, input ListSessionsInput) (port.Page[CardDTO], error)
}

type CreateSessionInput struct {
	ProjectID           domain.ProjectID
	Requirement         string
	Mode                domain.Mode
	BaseBranch          string
	Config              domain.Config
	StagedAttachmentIDs []domain.StagedAttachmentID
}

type CloseSessionInput struct {
	SessionID domain.ID
	Reason    domain.CloseReason
}

type AppendPromptInput struct {
	SessionID domain.ID
	Body      string
}

type ListSessionsInput struct {
	ProjectID *domain.ProjectID
	Scope     string
	Range     string
	Page      int
	PageSize  int
	Filter    string
	Sort      string
}

type DTO struct {
	ID             domain.ID
	ProjectID      domain.ProjectID
	Requirement    string
	Mode           domain.Mode
	Status         domain.Status
	BaseBranch     string
	WorktreePath   string
	CodexSessionID string
	Config         domain.Config
	LastRunAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CardDTO struct {
	DTO
	ProjectName        string
	RequirementSummary string
	CurrentNodeTitle   string
	PendingQuestion    bool
	AvailableActions   []string
}

type DetailDTO struct {
	DTO
	CloseReason      *domain.CloseReason
	Attachments      []domain.SessionAttachment
	PromptAppends    []PromptAppendDTO
	AvailableActions []string
	CanResume        bool
}

type PromptAppendDTO struct {
	ID        string
	SessionID domain.ID
	Body      string
	CreatedAt time.Time
}

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

var ErrProcessLifecycleNotWired = errors.New("session process lifecycle is not wired")

type Service struct {
	repo       domain.Repository
	projects   projectdomain.Repository
	now        func() time.Time
	generateID func() (domain.ID, error)
}

func New(repo domain.Repository, projects projectdomain.Repository) *Service {
	return &Service{
		repo:       repo,
		projects:   projects,
		now:        time.Now,
		generateID: generateID,
	}
}

func (s *Service) CreateSession(ctx context.Context, input CreateSessionInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	if input.ProjectID == "" {
		return DTO{}, errors.New("project id is required")
	}
	if s.projects == nil {
		return DTO{}, errors.New("project repository is required")
	}
	if _, err := s.projects.Find(ctx, projectdomain.ID(input.ProjectID)); err != nil {
		return DTO{}, fmt.Errorf("find project: %w", err)
	}
	requirement := strings.TrimSpace(input.Requirement)
	if requirement == "" {
		return DTO{}, errors.New("session requirement is required")
	}
	mode := input.Mode
	if mode == "" {
		mode = domain.ModeChat
	}
	if mode != domain.ModeChat && mode != domain.ModeWorkflow {
		return DTO{}, fmt.Errorf("unsupported session mode %q", mode)
	}
	id, err := s.generateID()
	if err != nil {
		return DTO{}, fmt.Errorf("generate session id: %w", err)
	}
	now := s.now()
	session := domain.Session{
		ID:          id,
		ProjectID:   input.ProjectID,
		Requirement: requirement,
		Mode:        mode,
		Status:      domain.StatusCreated,
		BaseBranch:  strings.TrimSpace(input.BaseBranch),
		Config:      input.Config,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Save(ctx, session); err != nil {
		return DTO{}, fmt.Errorf("save session: %w", err)
	}
	return toDTO(session), nil
}

func (s *Service) StartSession(ctx context.Context, id domain.ID) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	switch session.Status {
	case domain.StatusCreated, domain.StatusStopped, domain.StatusFailed, domain.StatusCompleted:
	default:
		return DTO{}, fmt.Errorf("session cannot start from status %q", session.Status)
	}
	return DTO{}, ErrProcessLifecycleNotWired
}

func (s *Service) StopSession(ctx context.Context, id domain.ID) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	switch session.Status {
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping:
	default:
		return DTO{}, fmt.Errorf("session cannot stop from status %q", session.Status)
	}
	return DTO{}, ErrProcessLifecycleNotWired
}

func (s *Service) ResumeSession(ctx context.Context, id domain.ID) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	switch session.Status {
	case domain.StatusStopped, domain.StatusResumeFailed:
	default:
		return DTO{}, fmt.Errorf("session cannot resume from status %q", session.Status)
	}
	if strings.TrimSpace(session.CodexSessionID) == "" {
		return DTO{}, errors.New("session cannot resume without codex session id")
	}
	return DTO{}, ErrProcessLifecycleNotWired
}

func (s *Service) CloseSession(ctx context.Context, input CloseSessionInput) (DTO, error) {
	if s == nil {
		return DTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, input.SessionID)
	if err != nil {
		return DTO{}, fmt.Errorf("find session: %w", err)
	}
	if session.Status == domain.StatusClosed {
		return toDTO(session), nil
	}
	reason := input.Reason
	if reason == "" {
		reason = domain.CloseReasonUserClosed
	}
	if reason != domain.CloseReasonUserClosed && reason != domain.CloseReasonMergedClosed {
		return DTO{}, fmt.Errorf("unsupported close reason %q", reason)
	}
	now := s.now()
	session.Status = domain.StatusClosed
	session.CloseReason = &reason
	session.ClosedAt = &now
	session.UpdatedAt = now
	if err := s.repo.Save(ctx, session); err != nil {
		return DTO{}, fmt.Errorf("save session: %w", err)
	}
	return toDTO(session), nil
}

func (s *Service) AppendPrompt(ctx context.Context, input AppendPromptInput) (PromptAppendDTO, error) {
	if s == nil {
		return PromptAppendDTO{}, errors.New("session usecase: nil service")
	}
	if input.SessionID == "" {
		return PromptAppendDTO{}, errors.New("session id is required")
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return PromptAppendDTO{}, errors.New("prompt append body is required")
	}
	id, err := s.generateID()
	if err != nil {
		return PromptAppendDTO{}, fmt.Errorf("generate prompt append id: %w", err)
	}
	append := domain.PromptAppend{
		ID:        string(id),
		SessionID: input.SessionID,
		Body:      body,
		CreatedAt: s.now(),
	}
	if err := s.repo.AppendPrompt(ctx, append); err != nil {
		return PromptAppendDTO{}, fmt.Errorf("append prompt: %w", err)
	}
	return toPromptAppendDTO(append), nil
}

func (s *Service) GetSession(ctx context.Context, id domain.ID) (DetailDTO, error) {
	if s == nil {
		return DetailDTO{}, errors.New("session usecase: nil service")
	}
	session, err := s.repo.Find(ctx, id)
	if err != nil {
		return DetailDTO{}, fmt.Errorf("find session: %w", err)
	}
	appends, err := s.repo.ListPromptAppends(ctx, id)
	if err != nil {
		return DetailDTO{}, fmt.Errorf("list prompt appends: %w", err)
	}
	return toDetailDTO(session, appends), nil
}

func (s *Service) ListSessions(ctx context.Context, input ListSessionsInput) (port.Page[CardDTO], error) {
	if s == nil {
		return port.Page[CardDTO]{}, errors.New("session usecase: nil service")
	}
	page, pageSize := normalizePage(input.Page, input.PageSize)
	query := domain.ListQuery{
		ProjectID: input.ProjectID,
		Scope:     input.Scope,
		Range:     input.Range,
		Page:      page,
		PageSize:  pageSize,
		Filter:    input.Filter,
		Sort:      input.Sort,
	}
	sessions, total, err := s.repo.ListCards(ctx, query)
	if err != nil {
		return port.Page[CardDTO]{}, fmt.Errorf("list session cards: %w", err)
	}
	items := make([]CardDTO, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, toCardDTO(session))
	}
	return port.Page[CardDTO]{
		Items:    items,
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    total,
	}, nil
}

func toDTO(session domain.Session) DTO {
	return DTO{
		ID:             session.ID,
		ProjectID:      session.ProjectID,
		Requirement:    session.Requirement,
		Mode:           session.Mode,
		Status:         session.Status,
		BaseBranch:     session.BaseBranch,
		WorktreePath:   session.WorktreePath,
		CodexSessionID: session.CodexSessionID,
		Config:         session.Config,
		LastRunAt:      session.LastRunAt,
		CreatedAt:      session.CreatedAt,
		UpdatedAt:      session.UpdatedAt,
	}
}

func toCardDTO(session domain.Session) CardDTO {
	return CardDTO{
		DTO:                toDTO(session),
		RequirementSummary: session.Requirement,
		AvailableActions:   availableActions(session),
	}
}

func toDetailDTO(session domain.Session, appends []domain.PromptAppend) DetailDTO {
	promptAppends := make([]PromptAppendDTO, 0, len(appends))
	for _, promptAppend := range appends {
		promptAppends = append(promptAppends, toPromptAppendDTO(promptAppend))
	}
	return DetailDTO{
		DTO:              toDTO(session),
		CloseReason:      session.CloseReason,
		Attachments:      []domain.SessionAttachment{},
		PromptAppends:    promptAppends,
		AvailableActions: availableActions(session),
		CanResume:        canResume(session),
	}
}

func toPromptAppendDTO(append domain.PromptAppend) PromptAppendDTO {
	return PromptAppendDTO{
		ID:        append.ID,
		SessionID: append.SessionID,
		Body:      append.Body,
		CreatedAt: append.CreatedAt,
	}
}

func availableActions(session domain.Session) []string {
	switch session.Status {
	case domain.StatusCreated, domain.StatusStopped, domain.StatusFailed, domain.StatusCompleted:
		actions := []string{"run", "close"}
		if canResume(session) {
			actions = []string{"run", "resume", "close"}
		}
		return actions
	case domain.StatusStarting, domain.StatusRunning, domain.StatusWaitingUser, domain.StatusStopping:
		return []string{"stop"}
	case domain.StatusResumeFailed:
		return []string{"run", "resume", "stop", "close"}
	case domain.StatusClosed:
		return []string{}
	default:
		return []string{"close"}
	}
}

func canResume(session domain.Session) bool {
	return strings.TrimSpace(session.CodexSessionID) != "" &&
		(session.Status == domain.StatusStopped || session.Status == domain.StatusResumeFailed)
}

func generateID() (domain.ID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return domain.ID(hex.EncodeToString(b[:])), nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = defaultPage
	}
	if pageSize < 1 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return page, pageSize
}
