package event

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/event"
)

func TestListSessionEventsReturnsPageAndDTOs(t *testing.T) {
	ctx := context.Background()
	sessionID := domain.SessionID("session-1")
	store := &fakeStore{
		events: []domain.DomainEvent{
			{
				ID:        "event-1",
				Scope:     domain.Scope{ProjectID: "project-1", SessionID: &sessionID},
				SessionID: &sessionID,
				Type:      "session.started",
				Payload:   map[string]any{"message": "started"},
				CreatedAt: time.Unix(10, 0).UTC(),
			},
			{
				ID:        "event-2",
				Scope:     domain.Scope{ProjectID: "project-1", SessionID: &sessionID},
				SessionID: &sessionID,
				Type:      "session.output",
				Payload:   map[string]any{"tokens": float64(12)},
				CreatedAt: time.Unix(11, 1).UTC(),
			},
			{
				ID:        "event-3",
				Scope:     domain.Scope{ProjectID: "project-1", SessionID: &sessionID},
				SessionID: &sessionID,
				Type:      "session.finished",
				Payload:   map[string]any{"ok": true},
				CreatedAt: time.Unix(12, 0).UTC(),
			},
		},
	}
	service := New(store)

	got, err := service.ListSessionEvents(ctx, ListSessionEventsInput{
		SessionID: "session-1",
		Page:      2,
		PageSize:  1,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if got.Page != 2 || got.PageSize != 1 || got.Total != 3 || got.NextCursor != "event-2" {
		t.Fatalf("page mismatch: %#v", got)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "event-2" {
		t.Fatalf("items mismatch: %#v", got.Items)
	}
	if got.Items[0].CreatedAt != "1970-01-01T00:00:11.000000001Z" {
		t.Fatalf("CreatedAt = %q", got.Items[0].CreatedAt)
	}
	if !reflect.DeepEqual(store.lastScope.SessionID, &sessionID) {
		t.Fatalf("scope session = %#v", store.lastScope.SessionID)
	}
}

func TestListSessionEventsDefaultsAndCapsPageSize(t *testing.T) {
	store := &fakeStore{}
	service := New(store)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Page:      -1,
		PageSize:  500,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if got.Page != 1 || got.PageSize != 200 {
		t.Fatalf("page defaults = %#v", got)
	}
}

func TestSessionEventsSendsHistoryThenCloses(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	store := &fakeStore{
		events: []domain.DomainEvent{
			{ID: "event-1", Scope: domain.Scope{SessionID: &sessionID, ProjectID: "project-1"}, SessionID: &sessionID, Type: "one", CreatedAt: time.Unix(1, 0).UTC()},
			{ID: "event-2", Scope: domain.Scope{SessionID: &sessionID, ProjectID: "project-1"}, SessionID: &sessionID, Type: "two", CreatedAt: time.Unix(2, 0).UTC()},
		},
	}
	service := New(store)

	ch, err := service.SessionEvents(context.Background(), SessionEventsInput{
		Scope:        domain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		AfterEventID: "event-0",
	})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	var got []DTO
	for event := range ch {
		got = append(got, event)
	}
	if len(got) != 2 || got[0].ID != "event-1" || got[1].ID != "event-2" {
		t.Fatalf("history events = %#v", got)
	}
	if store.lastAfter != "event-0" {
		t.Fatalf("after id = %q", store.lastAfter)
	}
}

func TestListSessionEventsValidatesInput(t *testing.T) {
	service := New(&fakeStore{})
	if _, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{}); err == nil {
		t.Fatal("ListSessionEvents() expected session id error")
	}
	var nilService *Service
	if _, err := nilService.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1"}); err == nil {
		t.Fatal("ListSessionEvents() expected nil service error")
	}
	service = New(&fakeStore{err: errors.New("boom")})
	if _, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1"}); err == nil {
		t.Fatal("ListSessionEvents() expected store error")
	}
}

type fakeStore struct {
	events    []domain.DomainEvent
	err       error
	lastScope domain.Scope
	lastAfter domain.ID
}

func (s *fakeStore) Append(context.Context, domain.DomainEvent) error {
	return errors.New("unexpected Append call")
}

func (s *fakeStore) After(_ context.Context, scope domain.Scope, after domain.ID) ([]domain.DomainEvent, error) {
	s.lastScope = scope
	s.lastAfter = after
	return s.events, s.err
}
