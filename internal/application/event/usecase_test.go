package event

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
)

func TestLiveCodexEventsRoutesTypedEventsBySession(t *testing.T) {
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := service.LiveCodexEvents(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishCodexEvent(context.Background(), processdomain.CodexEvent{
		EventID: "other", Type: processdomain.CodexEventCommand, SessionID: "session-2",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-stream:
		t.Fatalf("received another session's Codex event: %#v", event)
	default:
	}
	want := processdomain.CodexEvent{
		EventID: "command-1", Type: processdomain.CodexEventCommand, SessionID: "session-1",
		CorrelationID: "call-1", Phase: processdomain.CodexPhaseStarted,
		Content: processdomain.CodexCommandContent{Kind: processdomain.CodexCommandExec},
	}
	if err := service.PublishCodexEvent(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if got := <-stream; !reflect.DeepEqual(got, want) {
		t.Fatalf("Codex event = %#v, want %#v", got, want)
	}
}

func TestLiveCodexEventsClosesWhenSubscriberCancels(t *testing.T) {
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := service.LiveCodexEvents(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("Codex stream emitted after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("Codex stream stayed open after cancellation")
	}
}

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

func TestPublishAfterCommitRoutesThroughKeyedScopeBuckets(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	otherSessionID := domain.SessionID("session-2")
	service := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	global, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{})
	if err != nil {
		t.Fatalf("LiveSessionEvents() global error = %v", err)
	}
	project, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{ProjectID: "project-1"},
	})
	if err != nil {
		t.Fatalf("LiveSessionEvents() project error = %v", err)
	}
	session, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{ProjectID: "project-1", SessionID: &sessionID},
	})
	if err != nil {
		t.Fatalf("LiveSessionEvents() session error = %v", err)
	}
	otherSession, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{
		Scope: domain.Scope{SessionID: &otherSessionID},
	})
	if err != nil {
		t.Fatalf("LiveSessionEvents() other session error = %v", err)
	}

	want := domain.DomainEvent{
		ID:        "event-1",
		Scope:     domain.Scope{ProjectID: "project-1", SessionID: &sessionID},
		SessionID: &sessionID,
		Type:      "session.running",
	}
	if err := service.PublishAfterCommit(context.Background(), want); err != nil {
		t.Fatalf("PublishAfterCommit() error = %v", err)
	}
	for name, ch := range map[string]<-chan DTO{
		"global":  global,
		"project": project,
		"session": session,
	} {
		if got := <-ch; got.ID != want.ID {
			t.Fatalf("%s subscriber event = %#v", name, got)
		}
	}
	select {
	case got := <-otherSession:
		t.Fatalf("other session subscriber received event = %#v", got)
	default:
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

func TestSlowSubscriptionDisconnectsWhenMailboxIsFull(t *testing.T) {
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
	started := time.Now()
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-after-full",
		Scope:     domain.Scope{SessionID: &sessionID},
		SessionID: &sessionID,
		Type:      "after_full",
	}); err != nil {
		t.Fatalf("PublishAfterCommit() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("PublishAfterCommit() blocked for %s", elapsed)
	}
	for i := 0; i < cap(ch); i++ {
		if _, ok := <-ch; !ok {
			t.Fatalf("subscriber closed after %d buffered events", i)
		}
	}
	if _, ok := <-ch; ok {
		t.Fatal("slow subscriber remained open after mailbox overflow")
	}
}

func TestSubscriptionObserverRecordsOverflowWithoutEventContent(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	recorder := &eventObservationRecorder{}
	service := New(WithObserver(recorder))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.LiveSessionEvents(ctx, LiveSessionEventsInput{Scope: domain.Scope{SessionID: &sessionID}})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= cap(ch); index++ {
		if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
			ID: domain.ID(fmt.Sprintf("event-%d", index)), Scope: domain.Scope{SessionID: &sessionID},
			Payload: map[string]any{"message": "must-not-be-observed"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Contains(recorder.items, (Observation{Name: "subscription.delivery", Outcome: "overflow"})) {
		t.Fatalf("observations = %#v", recorder.items)
	}
	if strings.Contains(fmt.Sprintf("%#v", recorder.items), "must-not-be-observed") || strings.Contains(fmt.Sprintf("%#v", recorder.items), "session-1") {
		t.Fatalf("observation leaked event content: %#v", recorder.items)
	}
}

func TestSubscriptionObserverDoesNotReportCancellationAsOverflow(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	recorder := &eventObservationRecorder{}
	service := New(WithObserver(recorder))
	done := make(chan struct{})
	sub := service.subscribe(domain.Scope{SessionID: &sessionID}, make(chan DTO, 1), done)
	close(done)
	if result := sub.trySend(DTO{ID: "after-cancel"}); result != deliveryUnavailable {
		t.Fatalf("delivery result = %d", result)
	}
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{ID: "after-cancel", Scope: domain.Scope{SessionID: &sessionID}}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(recorder.items, (Observation{Name: "subscription.delivery", Outcome: "overflow"})) {
		t.Fatalf("cancellation observations = %#v", recorder.items)
	}
}

type eventObservationRecorder struct {
	items []Observation
}

func (r *eventObservationRecorder) Observe(observation Observation) {
	r.items = append(r.items, observation)
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
