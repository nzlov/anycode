package sharing

import (
	"context"
	"errors"
	"time"

	eventapp "github.com/nzlov/anycode/internal/application/event"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/session"
)

const (
	TypeReady                = "sharing.ready"
	TypeLinkCreated          = "sharing.link_created"
	TypeLinkClaimed          = "sharing.link_claimed"
	TypeLinkRevoked          = "sharing.link_revoked"
	TypeMessageSubmitted     = "sharing.message_submitted"
	TypeMessageStatusChanged = "sharing.message_status_changed"
)

type Events interface {
	eventdomain.Publisher
	LiveSessionEvents(context.Context, eventapp.LiveSessionEventsInput) (<-chan eventapp.DTO, error)
}

type Option func(*Service)

func WithEvents(events Events) Option {
	return func(s *Service) { s.events = events }
}

type Update struct {
	Type       string
	SessionID  session.ID
	ShareID    string
	MessageID  string
	Status     string
	OccurredAt time.Time
}

func (s *Service) Updates(ctx context.Context, id session.ID) (<-chan Update, error) {
	if s.events == nil {
		return nil, errors.New("sharing events are not configured")
	}
	sessionID := eventdomain.SessionID(id)
	scope := eventdomain.Scope{}
	if id != "" {
		scope.SessionID = &sessionID
	}
	source, err := s.events.LiveSessionEvents(ctx, eventapp.LiveSessionEventsInput{Scope: scope})
	if err != nil {
		return nil, err
	}
	out := make(chan Update)
	go func() {
		defer close(out)
		send := func(update Update) bool {
			select {
			case <-ctx.Done():
				return false
			case out <- update:
				return true
			}
		}
		// Query only after the listener is registered, including after reconnects.
		if !send(Update{Type: TypeReady, SessionID: id, OccurredAt: time.Now().UTC()}) {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case item, ok := <-source:
				if !ok {
					return
				}
				switch item.Type {
				case TypeLinkCreated, TypeLinkClaimed, TypeLinkRevoked, TypeMessageSubmitted, TypeMessageStatusChanged:
				default:
					continue
				}
				shareID, _ := item.Payload["shareId"].(string)
				messageID, _ := item.Payload["messageId"].(string)
				status, _ := item.Payload["status"].(string)
				occurredAt, _ := time.Parse(time.RFC3339Nano, item.CreatedAt)
				if item.SessionID == nil {
					continue
				}
				if !send(Update{Type: item.Type, SessionID: session.ID(*item.SessionID), ShareID: shareID, MessageID: messageID, Status: status, OccurredAt: occurredAt}) {
					return
				}
			}
		}
	}()
	return out, nil
}

func (s *Service) publish(ctx context.Context, kind string, id session.ID, shareID, messageID, status string) {
	if s.events == nil {
		return
	}
	sessionID := eventdomain.SessionID(id)
	payload := map[string]any{"shareId": shareID}
	if messageID != "" {
		payload["messageId"] = messageID
	}
	if status != "" {
		payload["status"] = status
	}
	// The repository write has committed; a notification failure must not replay it.
	_ = s.events.PublishAfterCommit(context.WithoutCancel(ctx), eventdomain.DomainEvent{
		ID: eventdomain.ID(secret()[:24]), Type: kind, Scope: eventdomain.Scope{SessionID: &sessionID},
		SessionID: &sessionID, Payload: payload, CreatedAt: time.Now().UTC(),
	})
}
