package entstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/predicate"
	entsessionside "github.com/nzlov/anycode/internal/infra/entstore/ent/sessionside"
	entsessionsideevent "github.com/nzlov/anycode/internal/infra/entstore/ent/sessionsideevent"
)

var _ session.SideRepository = (*SideRepository)(nil)

type SideRepository struct {
	client *ent.Client
}

func NewSideRepository(client *ent.Client) *SideRepository {
	return &SideRepository{client: client}
}

func (r *SideRepository) CreateSide(ctx context.Context, side session.Side) error {
	create := r.client.SessionSide.Create().
		SetID(side.ID).
		SetSessionID(string(side.SessionID)).
		SetProcessRunID(side.ProcessRunID).
		SetTurnID(side.TurnID).
		SetPrompt(side.Prompt).
		SetFollowUps(nonNilStrings(side.FollowUps)).
		SetStatus(string(side.Status)).
		SetError(side.Error).
		SetTurnIndex(side.TurnIndex)
	if !side.CreatedAt.IsZero() {
		create.SetCreatedAt(side.CreatedAt)
	}
	if !side.UpdatedAt.IsZero() {
		create.SetUpdatedAt(side.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create session Side: %w", err)
	}
	return nil
}

func (r *SideRepository) FindSide(ctx context.Context, id string) (session.Side, error) {
	row, err := r.client.SessionSide.Get(ctx, id)
	if err != nil {
		return session.Side{}, fmt.Errorf("find session Side: %w", err)
	}
	return toDomainSide(row), nil
}

func (r *SideRepository) FindSideByProcessRun(ctx context.Context, processRunID string) (session.Side, error) {
	row, err := r.client.SessionSide.Query().
		Where(entsessionside.ProcessRunIDEQ(processRunID)).
		Only(ctx)
	if err != nil {
		return session.Side{}, fmt.Errorf("find session Side by process run: %w", err)
	}
	return toDomainSide(row), nil
}

func (r *SideRepository) ListSides(ctx context.Context, sessionID session.ID) ([]session.Side, error) {
	rows, err := r.client.SessionSide.Query().
		Where(entsessionside.SessionIDEQ(string(sessionID))).
		Order(ent.Asc(entsessionside.FieldCreatedAt), ent.Asc(entsessionside.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list session Sides: %w", err)
	}
	result := make([]session.Side, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDomainSide(row))
	}
	return result, nil
}

func (r *SideRepository) ListRunningSides(ctx context.Context) ([]session.Side, error) {
	rows, err := r.client.SessionSide.Query().
		Where(entsessionside.StatusEQ(string(session.SideStatusRunning))).
		Order(ent.Asc(entsessionside.FieldCreatedAt), ent.Asc(entsessionside.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running session Sides: %w", err)
	}
	result := make([]session.Side, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDomainSide(row))
	}
	return result, nil
}

func (r *SideRepository) BeginSideTurn(ctx context.Context, id string, processRunID string, turnID string, followUp string, now time.Time) (session.Side, error) {
	row, err := r.client.SessionSide.Get(ctx, id)
	if err != nil {
		return session.Side{}, fmt.Errorf("find session Side for continuation: %w", err)
	}
	followUps := append(append([]string(nil), row.FollowUps...), followUp)
	updated, err := r.client.SessionSide.UpdateOneID(id).
		SetProcessRunID(processRunID).
		SetTurnID(turnID).
		SetFollowUps(followUps).
		SetStatus(string(session.SideStatusRunning)).
		SetError("").
		SetTurnIndex(row.TurnIndex + 1).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return session.Side{}, fmt.Errorf("begin session Side turn: %w", err)
	}
	return toDomainSide(updated), nil
}

func (r *SideRepository) CompleteSideTurn(ctx context.Context, id string, processRunID string, status session.SideStatus, failure string, now time.Time) error {
	_, err := r.client.SessionSide.Update().
		Where(
			entsessionside.IDEQ(id),
			entsessionside.ProcessRunIDEQ(processRunID),
			entsessionside.StatusEQ(string(session.SideStatusRunning)),
		).
		SetStatus(string(status)).
		SetError(failure).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("complete session Side turn: %w", err)
	}
	return nil
}

func (r *SideRepository) AppendSideEvent(ctx context.Context, event session.SideEvent) error {
	content := event.Content
	if content == nil {
		content = map[string]any{}
	}
	err := r.client.SessionSideEvent.Create().
		SetID(event.ID).
		SetSideID(event.SideID).
		SetSessionID(string(event.SessionID)).
		SetProcessRunID(event.ProcessRunID).
		SetEventID(event.EventID).
		SetType(event.Type).
		SetCorrelationID(event.CorrelationID).
		SetTurnID(event.TurnID).
		SetPhase(event.Phase).
		SetContentKind(event.ContentKind).
		SetContent(content).
		SetTurnIndex(event.TurnIndex).
		SetSequence(event.Sequence).
		SetCreatedAt(event.CreatedAt).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("append session Side event: %w", err)
	}
	return nil
}

func (r *SideRepository) ListSideEvents(ctx context.Context, sideID string) ([]session.SideEvent, error) {
	rows, err := r.client.SessionSideEvent.Query().
		Where(entsessionsideevent.SideIDEQ(sideID)).
		Order(ent.Asc(entsessionsideevent.FieldTurnIndex), ent.Asc(entsessionsideevent.FieldSequence)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list session Side events: %w", err)
	}
	return toDomainSideEvents(rows), nil
}

func (r *SideRepository) ListSideRunEvents(ctx context.Context, processRunID string) ([]session.SideEvent, error) {
	rows, err := r.client.SessionSideEvent.Query().
		Where(entsessionsideevent.ProcessRunIDEQ(processRunID)).
		Order(ent.Asc(entsessionsideevent.FieldSequence)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list session Side run events: %w", err)
	}
	return toDomainSideEvents(rows), nil
}

func (r *SideRepository) DeleteSide(ctx context.Context, id string) error {
	return r.delete(ctx, entsessionsideevent.SideIDEQ(id), entsessionside.IDEQ(id))
}

func (r *SideRepository) DeleteSidesBySession(ctx context.Context, sessionID session.ID) error {
	value := string(sessionID)
	return r.delete(ctx, entsessionsideevent.SessionIDEQ(value), entsessionside.SessionIDEQ(value))
}

func (r *SideRepository) delete(ctx context.Context, eventPredicate predicate.SessionSideEvent, sidePredicate predicate.SessionSide) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin session Side deletion: %w", err)
	}
	if _, err = tx.SessionSideEvent.Delete().Where(eventPredicate).Exec(ctx); err == nil {
		_, err = tx.SessionSide.Delete().Where(sidePredicate).Exec(ctx)
	}
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return errors.Join(fmt.Errorf("delete session Side: %w", err), rollbackErr)
		}
		return fmt.Errorf("delete session Side: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session Side deletion: %w", err)
	}
	return nil
}

func toDomainSide(row *ent.SessionSide) session.Side {
	return session.Side{
		ID: row.ID, SessionID: session.ID(row.SessionID), ProcessRunID: row.ProcessRunID,
		TurnID: row.TurnID, Prompt: row.Prompt, FollowUps: nonNilStrings(row.FollowUps),
		Status: session.SideStatus(row.Status), Error: row.Error, TurnIndex: row.TurnIndex,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func toDomainSideEvents(rows []*ent.SessionSideEvent) []session.SideEvent {
	result := make([]session.SideEvent, 0, len(rows))
	for _, row := range rows {
		result = append(result, session.SideEvent{
			ID: row.ID, SideID: row.SideID, SessionID: session.ID(row.SessionID), ProcessRunID: row.ProcessRunID,
			EventID: row.EventID, Type: row.Type, CorrelationID: row.CorrelationID, TurnID: row.TurnID,
			Phase: row.Phase, ContentKind: row.ContentKind, Content: row.Content,
			TurnIndex: row.TurnIndex, Sequence: row.Sequence, CreatedAt: row.CreatedAt,
		})
	}
	return result
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
