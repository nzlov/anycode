package session

import (
	"context"
	"errors"
	"time"
)

var ErrShareAccess = errors.New("share access denied")
var ErrShareConflict = errors.New("share or message has already been used")
var ErrShareSessionBusy = errors.New("session is busy; approved message remains queued")

func (s Session) CanAcceptSharedMessage() bool {
	switch s.Status {
	case StatusStopped, StatusFailed, StatusCompleted, StatusResumeFailed:
		return true
	default:
		return false
	}
}

type ShareLink struct {
	ID        string     `json:"id"`
	SessionID ID         `json:"sessionId"`
	TokenHash string     `json:"-"`
	GuestHash string     `json:"-"`
	CreatedAt time.Time  `json:"createdAt"`
	ClaimedAt *time.Time `json:"claimedAt"`
	RevokedAt *time.Time `json:"revokedAt"`
}

type SharedMessageContent struct {
	Body        string                    `json:"body"`
	Config      Config                    `json:"config"`
	Attachments []SharedMessageAttachment `json:"attachments"`
}

type SharedMessageAttachment struct {
	ID       StagedAttachmentID `json:"id"`
	Filename string             `json:"filename"`
	MimeType string             `json:"mimeType"`
	Size     int64              `json:"size"`
}

type SharedMessage struct {
	ID            string               `json:"id"`
	ShareID       string               `json:"shareId"`
	SessionID     ID                   `json:"sessionId"`
	Content       SharedMessageContent `json:"content"`
	Status        string               `json:"status"`
	ApprovalOrder int64                `json:"approvalOrder"`
	CreatedAt     time.Time            `json:"createdAt"`
	ReviewedAt    *time.Time           `json:"reviewedAt"`
}

// ShareRepository owns durable single-use claims and review transitions.
type ShareRepository interface {
	CreateShare(context.Context, ShareLink) error
	FindShare(context.Context, string) (ShareLink, error)
	ListShares(context.Context, ID) ([]ShareLink, error)
	ClaimShare(context.Context, string, string, string, time.Time) (bool, error)
	RevokeShare(context.Context, string, time.Time) error
	CreateSharedMessage(context.Context, SharedMessage) error
	ListSharedMessages(context.Context, ID) ([]SharedMessage, error)
	FindSharedMessage(context.Context, string) (SharedMessage, error)
	ListApprovedSharedMessages(context.Context) ([]SharedMessage, error)
	TransitionSharedMessage(context.Context, string, string, string, int64, time.Time) (bool, error)
}
