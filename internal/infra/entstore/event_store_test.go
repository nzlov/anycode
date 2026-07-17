package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/event"
)

func TestEventStoreAppendAfterAndScopeFilters(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{
		DatabaseURL: filepath.Join(t.TempDir(), "anycode.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	events := store.Events()
	session1 := event.SessionID("session-1")
	session2 := event.SessionID("session-2")
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	appendEvents(t, ctx, events,
		event.DomainEvent{
			ID:        "event-1",
			Scope:     event.Scope{ProjectID: "project-1", SessionID: &session1},
			SessionID: &session1,
			Type:      "session.started",
			Payload:   map[string]any{"message": "started"},
			CreatedAt: base,
		},
		event.DomainEvent{
			ID:        "event-2",
			Scope:     event.Scope{ProjectID: "project-1", SessionID: &session1},
			SessionID: &session1,
			Type:      "session.output",
			Payload:   map[string]any{"tokens": 12},
			CreatedAt: base,
		},
		event.DomainEvent{
			ID:        "event-3",
			Scope:     event.Scope{ProjectID: "project-1", SessionID: &session2},
			SessionID: &session2,
			Type:      "session.started",
			Payload:   map[string]any{"other": true},
			CreatedAt: base.Add(time.Second),
		},
		event.DomainEvent{
			ID:        "event-4",
			Scope:     event.Scope{ProjectID: "project-2", SessionID: &session1},
			SessionID: &session1,
			Type:      "session.started",
			Payload:   map[string]any{"project": "two"},
			CreatedAt: base.Add(2 * time.Second),
		},
	)

	got, err := events.After(ctx, event.Scope{ProjectID: "project-1", SessionID: &session1}, "")
	if err != nil {
		t.Fatalf("After() error = %v", err)
	}
	assertEventIDs(t, got, []event.ID{"event-1", "event-2"})
	if got[0].Payload["message"] != "started" {
		t.Fatalf("payload mismatch: %#v", got[0].Payload)
	}

	got, err = events.After(ctx, event.Scope{ProjectID: "project-1", SessionID: &session1}, "event-1")
	if err != nil {
		t.Fatalf("After(event-1) error = %v", err)
	}
	assertEventIDs(t, got, []event.ID{"event-2"})

	got, err = events.After(ctx, event.Scope{ProjectID: "project-1"}, "")
	if err != nil {
		t.Fatalf("After(project) error = %v", err)
	}
	assertEventIDs(t, got, []event.ID{"event-1", "event-2", "event-3"})

	got, err = events.After(ctx, event.Scope{SessionID: &session1}, "")
	if err != nil {
		t.Fatalf("After(session) error = %v", err)
	}
	assertEventIDs(t, got, []event.ID{"event-1", "event-2", "event-4"})
}

func TestEventStoreBeforeReturnsNewestWindowBeforeCursor(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{
		DatabaseURL: filepath.Join(t.TempDir(), "anycode.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	events := store.Events()
	sessionID := event.SessionID("session-1")
	base := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	appendEvents(t, ctx, events,
		event.DomainEvent{ID: "event-1", Scope: event.Scope{ProjectID: "project-1", SessionID: &sessionID}, SessionID: &sessionID, Type: "one", CreatedAt: base},
		event.DomainEvent{ID: "event-2", Scope: event.Scope{ProjectID: "project-1", SessionID: &sessionID}, SessionID: &sessionID, Type: "two", CreatedAt: base},
		event.DomainEvent{ID: "event-3", Scope: event.Scope{ProjectID: "project-1", SessionID: &sessionID}, SessionID: &sessionID, Type: "three", CreatedAt: base.Add(time.Second)},
		event.DomainEvent{ID: "event-4", Scope: event.Scope{ProjectID: "project-1", SessionID: &sessionID}, SessionID: &sessionID, Type: "four", CreatedAt: base.Add(2 * time.Second)},
	)

	got, total, hasMore, err := events.Before(ctx, event.Scope{ProjectID: "project-1", SessionID: &sessionID}, "", 2)
	if err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	if total != 4 || !hasMore {
		t.Fatalf("Before() total=%d hasMore=%v", total, hasMore)
	}
	assertEventIDs(t, got, []event.ID{"event-3", "event-4"})

	got, total, hasMore, err = events.Before(ctx, event.Scope{ProjectID: "project-1", SessionID: &sessionID}, "event-3", 2)
	if err != nil {
		t.Fatalf("Before(event-3) error = %v", err)
	}
	if total != 4 || hasMore {
		t.Fatalf("Before(event-3) total=%d hasMore=%v", total, hasMore)
	}
	assertEventIDs(t, got, []event.ID{"event-1", "event-2"})
}

func TestEventStorePreservesPayload(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{
		DatabaseURL: filepath.Join(t.TempDir(), "anycode.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	events := store.Events()
	sessionID := event.SessionID("session-1")
	appendEvents(t, ctx, events, event.DomainEvent{
		ID:        "event-secret",
		Scope:     event.Scope{ProjectID: "project-1", SessionID: &sessionID},
		SessionID: &sessionID,
		Type:      "session.failed",
		Payload: map[string]any{
			"accessKey":    "secret",
			"worktreePath": "/home/nzlov/workspaces/github/project",
		},
		Causality: event.Causality{ProcessRunID: "process-1", NodeRunID: "node-1", CorrelationID: "correlation-1", SessionStatus: "running"},
		CreatedAt: time.Now(),
	})

	got, err := events.After(ctx, event.Scope{ProjectID: "project-1", SessionID: &sessionID}, "")
	if err != nil {
		t.Fatalf("After() error = %v", err)
	}
	if got[0].Payload["accessKey"] != "secret" || got[0].Payload["worktreePath"] != "/home/nzlov/workspaces/github/project" {
		t.Fatalf("payload changed: %#v", got[0].Payload)
	}
	if got[0].Causality.ProcessRunID != "process-1" || got[0].Causality.NodeRunID != "node-1" || got[0].Causality.CorrelationID != "correlation-1" || got[0].Causality.SessionStatus != "running" {
		t.Fatalf("causality changed: %#v", got[0].Causality)
	}
}

func TestEventStoreRejectsCodexSessionContent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	for _, domainEvent := range []event.DomainEvent{
		{ID: "codex-event", Type: "process.codex_event", Payload: map[string]any{"message": "secret"}},
		{ID: "usage-event", Type: "session.running", Payload: map[string]any{"tokenUsage": map[string]any{"input": 10}}},
		{ID: "transcript-event", Type: "session.running", Payload: map[string]any{"transcript": []any{"secret"}}},
	} {
		if err := store.Events().Append(ctx, domainEvent); err == nil {
			t.Fatalf("event %q was accepted", domainEvent.ID)
		}
	}
	count, err := store.client.EventRecord.Query().Count(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("persisted Codex content events = %d", count)
	}
}

func appendEvents(t *testing.T, ctx context.Context, store *EventStore, events ...event.DomainEvent) {
	t.Helper()
	for _, event := range events {
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("append event %s: %v", event.ID, err)
		}
	}
}

func assertEventIDs(t *testing.T, got []event.DomainEvent, want []event.ID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("event[%d] = %q, want %q: %#v", i, got[i].ID, want[i], got)
		}
	}
}
