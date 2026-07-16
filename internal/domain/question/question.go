package question

import (
	"context"
	"time"
)

type BatchID string
type QuestionID string
type SessionID string
type WorkflowRunID string
type ProcessRunID string
type OptionID string

type BatchStatus string
type DeliveryStatus string

const (
	BatchPending   BatchStatus = "pending"
	BatchAnswered  BatchStatus = "answered"
	BatchCancelled BatchStatus = "cancelled"
)

const (
	DeliveryNone           DeliveryStatus = "none"
	DeliveryAwaitingResume DeliveryStatus = "awaiting_resume"
	DeliveryInflight       DeliveryStatus = "inflight"
	DeliveryDelivered      DeliveryStatus = "delivered"
)

type Batch struct {
	ID                   BatchID
	SessionID            SessionID
	WorkflowRunID        *WorkflowRunID
	OriginProcessRunID   *ProcessRunID
	Status               BatchStatus
	DeliveryStatus       DeliveryStatus
	DeliveryProcessRunID *ProcessRunID
	Questions            []Question
	CreatedAt            time.Time
	AnsweredAt           *time.Time
	DeliveredAt          *time.Time
}

type Question struct {
	ID               QuestionID
	BatchID          BatchID
	Title            string
	Body             string
	Type             string
	Options          []Option
	Metadata         map[string]any
	SelectedOptionID *OptionID
	CustomAnswer     string
	Answer           map[string]any
	Status           string
}

type Option struct {
	ID          OptionID
	Label       string
	Description string
	Payload     map[string]any
}

type Answer struct {
	QuestionID       QuestionID
	SelectedOptionID *OptionID
	CustomAnswer     string
	Payload          map[string]any
}

type Policy interface {
	CanSubmit(batch Batch, answers []Answer) error
	ApplyAnswers(batch Batch, answers []Answer) (Batch, error)
	Cancel(batch Batch, reason string) (Batch, error)
}

type Repository interface {
	CreateBatch(ctx context.Context, batch Batch) error
	FindBatch(ctx context.Context, id BatchID) (Batch, error)
	ListPendingBySession(ctx context.Context, sessionID SessionID) ([]Batch, error)
	SubmitAnswers(ctx context.Context, id BatchID, answers []Answer) (Batch, bool, error)
	CancelPendingBySession(ctx context.Context, sessionID SessionID, reason string) ([]Batch, error)
}

type AgentRepository interface {
	Repository
	CancelPendingBatch(ctx context.Context, id BatchID, reason string) (Batch, bool, error)
	FindLatestBySession(ctx context.Context, sessionID SessionID) (Batch, bool, error)
	FindPendingByOriginProcessRun(ctx context.Context, processRunID ProcessRunID) (Batch, bool, error)
	FindInflightByDeliveryProcessRun(ctx context.Context, processRunID ProcessRunID) (Batch, bool, error)
	FindAwaitingDeliveryBySession(ctx context.Context, sessionID SessionID) (Batch, bool, error)
	ListAgentBatchesForRecovery(ctx context.Context) ([]Batch, error)
	SetOriginProcessRun(ctx context.Context, id BatchID, processRunID ProcessRunID) error
	MarkDeliveryAwaitingResume(ctx context.Context, id BatchID) error
	MarkDeliveryInflight(ctx context.Context, id BatchID, processRunID ProcessRunID) error
	MarkDeliveryDelivered(ctx context.Context, id BatchID, processRunID ProcessRunID, deliveredAt time.Time) (Batch, bool, error)
	MarkDeliveryDeliveredByProcessRun(ctx context.Context, processRunID ProcessRunID, deliveredAt time.Time) ([]Batch, error)
	ResetDeliveryAwaitingResume(ctx context.Context, id BatchID) error
	ResetDeliveryAwaitingResumeByProcessRun(ctx context.Context, processRunID ProcessRunID) ([]Batch, error)
	CancelUndeliveredBySession(ctx context.Context, sessionID SessionID) ([]Batch, error)
}
