package event

import (
	"context"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/event"
)

func TestLiveSessionEventsStreamsOnlyPublishedEvents(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{ProjectID: "project-1"},
	})
	if err != nil {
		t.Fatalf("LiveSessionEvents() error = %v", err)
	}
	select {
	case event := <-ch:
		t.Fatalf("LiveSessionEvents() replayed history event = %#v", event)
	default:
	}

	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "live-event",
		Scope:     domain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		SessionID: &sessionID,
		Type:      "session.running",
		CreatedAt: time.Unix(4, 0).UTC(),
	}); err != nil {
		t.Fatalf("PublishAfterCommit() error = %v", err)
	}
	if event := <-ch; event.ID != "live-event" || event.Type != "session.running" {
		t.Fatalf("LiveSessionEvents() event = %#v", event)
	}
}

func TestLiveSessionEventsFiltersPublishedEventsByScope(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	otherSessionID := domain.SessionID("session-2")
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{SessionID: &sessionID},
	})
	if err != nil {
		t.Fatalf("LiveSessionEvents() error = %v", err)
	}
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-ignored",
		Scope:     domain.Scope{SessionID: &otherSessionID},
		SessionID: &otherSessionID,
		Type:      "session.stopped",
	}); err != nil {
		t.Fatalf("PublishAfterCommit() ignored error = %v", err)
	}
	select {
	case event := <-ch:
		t.Fatalf("LiveSessionEvents() received out-of-scope event = %#v", event)
	default:
	}
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-match",
		Scope:     domain.Scope{SessionID: &sessionID},
		SessionID: &sessionID,
		Type:      "session.running",
	}); err != nil {
		t.Fatalf("PublishAfterCommit() match error = %v", err)
	}
	if event := <-ch; event.ID != "event-match" {
		t.Fatalf("LiveSessionEvents() event = %#v", event)
	}
}

func TestLiveSessionEventsEmptyScopeReceivesAllPublishedEvents(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	otherSessionID := domain.SessionID("session-2")
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{})
	if err != nil {
		t.Fatalf("LiveSessionEvents() error = %v", err)
	}
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-1",
		Scope:     domain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		SessionID: &sessionID,
		Type:      "session.running",
	}); err != nil {
		t.Fatalf("PublishAfterCommit() first error = %v", err)
	}
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-2",
		Scope:     domain.Scope{SessionID: &otherSessionID, ProjectID: "project-2"},
		SessionID: &otherSessionID,
		Type:      "session.stopped",
	}); err != nil {
		t.Fatalf("PublishAfterCommit() second error = %v", err)
	}
	got := []DTO{<-ch, <-ch}
	if got[0].ID != "event-1" || got[1].ID != "event-2" {
		t.Fatalf("events = %#v", got)
	}
}

func TestPublishAfterCommitReturnsNilForNilPayload(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{SessionID: &sessionID},
	})
	if err != nil {
		t.Fatalf("LiveSessionEvents() error = %v", err)
	}
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-1",
		Scope:     domain.Scope{SessionID: &sessionID},
		SessionID: &sessionID,
		Type:      "process.status_changed",
		CreatedAt: time.Unix(1, 0).UTC(),
	}); err != nil {
		t.Fatalf("PublishAfterCommit() error = %v", err)
	}
	got := <-ch
	if got.Payload == nil {
		t.Fatal("Payload is nil, want empty map")
	}
	if len(got.Payload) != 0 {
		t.Fatalf("Payload = %#v, want empty map", got.Payload)
	}
}

func TestPublishAfterCommitUnblocksWhenSubscriberContextCancels(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{SessionID: &sessionID},
	})
	if err != nil {
		t.Fatalf("LiveSessionEvents() error = %v", err)
	}
	for i := 0; i < cap(ch); i++ {
		if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
			ID:        domain.ID("event-fill-" + string(rune('a'+i))),
			Scope:     domain.Scope{SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "fill",
			CreatedAt: time.Unix(int64(i), 0).UTC(),
		}); err != nil {
			t.Fatalf("PublishAfterCommit() fill error = %v", err)
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- service.PublishAfterCommit(context.Background(), domain.DomainEvent{
			ID:        "event-blocked",
			Scope:     domain.Scope{SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "blocked",
			CreatedAt: time.Unix(20, 0).UTC(),
		})
	}()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PublishAfterCommit() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PublishAfterCommit() stayed blocked after subscriber context cancel")
	}
}

func TestPublishAfterCommitBackpressuresWhenSubscriberIsFull(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{SessionID: &sessionID},
	})
	if err != nil {
		t.Fatalf("LiveSessionEvents() error = %v", err)
	}
	for i := 0; i < cap(ch); i++ {
		if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
			ID:        domain.ID("event-fill-" + string(rune('a'+i))),
			Scope:     domain.Scope{SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "fill",
		}); err != nil {
			t.Fatalf("PublishAfterCommit() fill error = %v", err)
		}
	}
	done := make(chan error, 1)
	go func() {
		done <- service.PublishAfterCommit(context.Background(), domain.DomainEvent{
			ID:        "event-after-full",
			Scope:     domain.Scope{SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "after_full",
		})
	}()
	select {
	case err := <-done:
		t.Fatalf("PublishAfterCommit() finished before subscriber consumed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	<-ch
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PublishAfterCommit() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PublishAfterCommit() stayed blocked after subscriber consumed")
	}
}

func TestPublishAfterCommitDoesNotPanicAfterLiveSubscriberCancels(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{SessionID: &sessionID},
	}); err != nil {
		t.Fatalf("LiveSessionEvents() error = %v", err)
	}
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- service.PublishAfterCommit(context.Background(), domain.DomainEvent{
			ID:        "event-after-cancel",
			Scope:     domain.Scope{SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "after_cancel",
			CreatedAt: time.Unix(30, 0).UTC(),
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PublishAfterCommit() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("PublishAfterCommit() blocked after live subscriber cancel")
	}
}
