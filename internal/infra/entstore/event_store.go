package entstore

import (
	"context"
	"fmt"
	"slices"

	"github.com/nzlov/anycode/internal/domain/event"
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
	if err := validatePersistedEvent(domainEvent); err != nil {
		return err
	}
	create := s.client.EventRecord.Create().
		SetID(string(domainEvent.ID)).
		SetProjectID(domainEvent.Scope.ProjectID).
		SetType(domainEvent.Type).
		SetPayload(payloadOrEmpty(domainEvent.Payload)).
		SetProcessRunID(domainEvent.Causality.ProcessRunID).
		SetWorkflowRunID(domainEvent.Causality.WorkflowRunID).
		SetNodeRunID(domainEvent.Causality.NodeRunID).
		SetCorrelationID(domainEvent.Causality.CorrelationID).
		SetSessionStatus(domainEvent.Causality.SessionStatus)
	if domainEvent.SessionID != nil {
		create.SetSessionID(string(*domainEvent.SessionID))
	} else if domainEvent.Scope.SessionID != nil {
		create.SetSessionID(string(*domainEvent.Scope.SessionID))
	}
	if !domainEvent.CreatedAt.IsZero() {
		create.SetCreatedAt(domainEvent.CreatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			existing, findErr := s.client.EventRecord.Get(ctx, string(domainEvent.ID))
			if findErr == nil && existing.Type == domainEvent.Type {
				return nil
			}
		}
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func validatePersistedEvent(domainEvent event.DomainEvent) error {
	if domainEvent.Type == "process.codex_event" || domainEvent.Type == "codex.transcript" || domainEvent.Type == "codex.usage" {
		return fmt.Errorf("event type %q contains Codex session content and cannot be persisted", domainEvent.Type)
	}
	for _, key := range []string{"codexContent", "codexPayload", "transcript", "tokenUsage", "compaction"} {
		if _, ok := domainEvent.Payload[key]; ok {
			return fmt.Errorf("event payload field %q contains Codex session content and cannot be persisted", key)
		}
	}
	return nil
}

func (s *EventStore) List(ctx context.Context, scope event.Scope) ([]event.DomainEvent, error) {
	rows, err := applyEventScope(s.client.EventRecord.Query(), scope).
		Order(ent.Asc(enteventrecord.FieldCreatedAt), ent.Asc(enteventrecord.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	events := make([]event.DomainEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, toDomainEvent(row))
	}
	return events, nil
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

func (s *EventStore) Before(ctx context.Context, scope event.Scope, before event.ID, limit int) ([]event.DomainEvent, int, bool, error) {
	if limit < 1 {
		limit = 1
	}
	total, err := applyEventScope(s.client.EventRecord.Query(), scope).Count(ctx)
	if err != nil {
		return nil, 0, false, fmt.Errorf("count events before: %w", err)
	}
	query := applyEventScope(s.client.EventRecord.Query(), scope)
	if before != "" {
		beforeRecord, err := applyEventScope(s.client.EventRecord.Query(), scope).
			Where(enteventrecord.IDEQ(string(before))).
			Only(ctx)
		if err != nil {
			return nil, 0, false, fmt.Errorf("find before event: %w", err)
		}
		query.Where(
			enteventrecord.Or(
				enteventrecord.CreatedAtLT(beforeRecord.CreatedAt),
				enteventrecord.And(
					enteventrecord.CreatedAtEQ(beforeRecord.CreatedAt),
					enteventrecord.IDLT(beforeRecord.ID),
				),
			),
		)
	}
	rows, err := query.
		Order(ent.Desc(enteventrecord.FieldCreatedAt), ent.Desc(enteventrecord.FieldID)).
		Limit(limit + 1).
		All(ctx)
	if err != nil {
		return nil, 0, false, fmt.Errorf("list events before: %w", err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	slices.Reverse(rows)
	events := make([]event.DomainEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, toDomainEvent(row))
	}
	return events, total, hasMore, nil
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
		Causality: event.Causality{
			ProcessRunID:  row.ProcessRunID,
			WorkflowRunID: row.WorkflowRunID,
			NodeRunID:     row.NodeRunID,
			CorrelationID: row.CorrelationID,
			SessionStatus: row.SessionStatus,
		},
		CreatedAt: row.CreatedAt,
	}
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}
