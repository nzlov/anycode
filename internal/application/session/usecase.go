package session

import (
	"context"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
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
