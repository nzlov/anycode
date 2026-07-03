package entstore

import (
	"context"
	"fmt"

	"github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/redaction"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	enteventrecord "github.com/nzlov/anycode/internal/infra/entstore/ent/eventrecord"
)

type EventStore struct {
	client *ent.Client
}

func NewEventStore(client *ent.Client) *EventStore {
	return &EventStore{client: client}
}

func (s *EventStore) Append(ctx context.Context, domainEvent event.DomainEvent) error {
	create := s.client.EventRecord.Create().
		SetID(string(domainEvent.ID)).
		SetProjectID(domainEvent.Scope.ProjectID).
		SetType(domainEvent.Type).
		SetPayload(redaction.Map(payloadOrEmpty(domainEvent.Payload)))
	if domainEvent.SessionID != nil {
		create.SetSessionID(string(*domainEvent.SessionID))
	} else if domainEvent.Scope.SessionID != nil {
		create.SetSessionID(string(*domainEvent.Scope.SessionID))
	}
	if !domainEvent.CreatedAt.IsZero() {
		create.SetCreatedAt(domainEvent.CreatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *EventStore) After(ctx context.Context, scope event.Scope, after event.ID) ([]event.DomainEvent, error) {
	query := applyEventScope(s.client.EventRecord.Query(), scope)
	if after != "" {
		afterRecord, err := applyEventScope(s.client.EventRecord.Query(), scope).
			Where(enteventrecord.IDEQ(string(after))).
			Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("find after event: %w", err)
		}
		query.Where(
			enteventrecord.Or(
				enteventrecord.CreatedAtGT(afterRecord.CreatedAt),
				enteventrecord.And(
					enteventrecord.CreatedAtEQ(afterRecord.CreatedAt),
					enteventrecord.IDGT(afterRecord.ID),
				),
			),
		)
	}
	rows, err := query.
		Order(ent.Asc(enteventrecord.FieldCreatedAt), ent.Asc(enteventrecord.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events after: %w", err)
	}
	events := make([]event.DomainEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, toDomainEvent(row))
	}
	return events, nil
}

func applyEventScope(query *ent.EventRecordQuery, scope event.Scope) *ent.EventRecordQuery {
	if scope.ProjectID != "" {
		query.Where(enteventrecord.ProjectIDEQ(scope.ProjectID))
	}
	if scope.SessionID != nil {
		query.Where(enteventrecord.SessionIDEQ(string(*scope.SessionID)))
	}
	return query
}

func toDomainEvent(row *ent.EventRecord) event.DomainEvent {
	var sessionID *event.SessionID
	if row.SessionID != nil {
		value := event.SessionID(*row.SessionID)
		sessionID = &value
	}
	return event.DomainEvent{
		ID: event.ID(row.ID),
		Scope: event.Scope{
			SessionID: sessionID,
			ProjectID: row.ProjectID,
		},
		SessionID: sessionID,
		Type:      row.Type,
		Payload:   payloadOrEmpty(row.Payload),
		CreatedAt: row.CreatedAt,
	}
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}
