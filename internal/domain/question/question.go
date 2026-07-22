package question

import (
	"context"
	"time"
)

type RequestID string
type QuestionID string
type SessionID string
type ProcessRunID string
type OptionID string

type RequestStatus string

const (
	RequestPending   RequestStatus = "pending"
	RequestAnswered  RequestStatus = "answered"
	RequestCancelled RequestStatus = "cancelled"
)

type Request struct {
	ID                 RequestID
	SessionID          SessionID
	OriginProcessRunID *ProcessRunID
	Status             RequestStatus
	Questions          []Question
	CreatedAt          time.Time
	AnsweredAt         *time.Time
}

type Question struct {
	ID               QuestionID
	RequestID        RequestID
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
	CanSubmit(request Request, answers []Answer) error
	ApplyAnswers(request Request, answers []Answer) (Request, error)
	Cancel(request Request, reason string) (Request, error)
}

type Repository interface {
	CreateRequest(ctx context.Context, request Request) error
	FindRequest(ctx context.Context, id RequestID) (Request, error)
	ListPendingRequestsBySession(ctx context.Context, sessionID SessionID) ([]Request, error)
	SubmitAnswers(ctx context.Context, id RequestID, answers []Answer) (Request, bool, error)
	CancelPendingRequest(ctx context.Context, id RequestID, reason string) (Request, bool, error)
	CancelPendingRequestsBySession(ctx context.Context, sessionID SessionID, reason string) ([]Request, error)
	FindLatestRequestBySession(ctx context.Context, sessionID SessionID) (Request, bool, error)
	FindPendingRequestByOriginProcessRun(ctx context.Context, processRunID ProcessRunID) (Request, bool, error)
}
