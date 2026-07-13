package question

import (
	"context"
	"time"
)

type BatchID string
type QuestionID string
type SessionID string
type WorkflowRunID string
type OptionID string

type BatchStatus string
type DeliveryStatus string

const (
	BatchPending   BatchStatus = "pending"
	BatchAnswered  BatchStatus = "answered"
	BatchCancelled BatchStatus = "cancelled"
)

const (
	DeliveryPending          DeliveryStatus = "pending"
	DeliveryRecoveryRequired DeliveryStatus = "recovery_required"
	DeliveryRecoveryQueued   DeliveryStatus = "recovery_queued"
	DeliveryDelivered        DeliveryStatus = "delivered"
)

type Batch struct {
	ID            BatchID
	SessionID     SessionID
	WorkflowRunID *WorkflowRunID
	Status        BatchStatus
	Delivery      DeliveryStatus
	Questions     []Question
	CreatedAt     time.Time
	AnsweredAt    *time.Time
}

type Question struct {
	ID               QuestionID
	BatchID          BatchID
	Title            string
	Body             string
	Type             string
	Options          []Option
	AllowCustom      bool
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

type RecoveryRepository interface {
	Repository
	FindLatestBySession(ctx context.Context, sessionID SessionID) (Batch, bool, error)
	SetDeliveryStatus(ctx context.Context, id BatchID, status DeliveryStatus) (Batch, bool, error)
}

type AnswerWaiter interface {
	Prepare(ctx context.Context, batchID BatchID) error
	Wait(ctx context.Context, batchID BatchID) ([]Answer, error)
	Resume(ctx context.Context, batchID BatchID, answers []Answer) error
	Cancel(ctx context.Context, batchID BatchID, reason string) error
	Forget(batchID BatchID)
}
