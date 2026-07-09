package timeline

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/event"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestListSessionEventsUsesStoredSessionEventsAndCodexTranscript(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	store := &fakeStore{
		events: []eventdomain.DomainEvent{
			{
				ID:        "stored-codex-event",
				Scope:     eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID},
				SessionID: &sessionID,
				Type:      "process.codex_event",
				Payload:   map[string]any{"codexType": "item.completed"},
				CreatedAt: time.Unix(5, 0).UTC(),
			},
			{
				ID:        "event-running",
				Scope:     eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID},
				SessionID: &sessionID,
				Type:      "session.running",
				Payload:   map[string]any{"processRunId": "process-run-1"},
				CreatedAt: time.Unix(10, 0).UTC(),
			},
		},
	}
	sessions := &fakeSessionRepository{
		sessions: map[sessiondomain.ID]sessiondomain.Session{
			"session-1": {
				ID:             "session-1",
				ProjectID:      "project-1",
				CodexSessionID: "codex-session-1",
				UpdatedAt:      time.Unix(30, 0).UTC(),
			},
		},
	}
	transcript := &fakeTranscriptSource{
		events: []process.CodexEvent{{
			EventID:   "codex-event-1",
			Type:      "item.completed",
			Payload:   map[string]any{"item": map[string]any{"type": "file_change"}},
			CreatedAt: time.Unix(20, 0).UTC(),
		}},
	}
	service := New(store, &fakeLiveSource{}, sessions, transcript, nil)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, []eventdomain.ID{"event-running", "codex:codex-event-1"}) {
		t.Fatalf("items = %#v", gotIDs)
	}
	codex := got.Items[1]
	if codex.Type != "process.codex_event" || codex.Payload["codexType"] != "item.completed" || codex.Payload["codexEventId"] != "codex-event-1" {
		t.Fatalf("codex event = %#v", codex)
	}
	if transcript.input.CodexSessionID != "codex-session-1" || transcript.input.Workdir != "" {
		t.Fatalf("transcript input = %#v", transcript.input)
	}
}

func TestListSessionEventsReadsAllIndexedCodexSessions(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	store := &fakeStore{}
	sessions := &fakeSessionRepository{
		sessions: map[sessiondomain.ID]sessiondomain.Session{
			"session-1": {
				ID:             "session-1",
				ProjectID:      "project-1",
				CodexSessionID: "codex-session-2",
				UpdatedAt:      time.Unix(30, 0).UTC(),
			},
		},
	}
	transcript := &fakeTranscriptSource{
		eventsByID: map[string][]process.CodexEvent{
			"codex-session-1": {{
				EventID:   "codex-event-1",
				Type:      "item.completed",
				Payload:   map[string]any{"item": map[string]any{"type": "agent_message", "aggregated_output": "first run"}},
				CreatedAt: time.Unix(10, 0).UTC(),
			}},
			"codex-session-2": {{
				EventID:   "codex-event-2",
				Type:      "item.completed",
				Payload:   map[string]any{"item": map[string]any{"type": "agent_message", "aggregated_output": "second run"}},
				CreatedAt: time.Unix(20, 0).UTC(),
			}},
		},
	}
	index := &fakeCodexSessionIndex{ids: []string{"codex-session-1", "codex-session-2"}}
	service := New(store, &fakeLiveSource{}, sessions, transcript, index)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, []eventdomain.ID{"codex:codex-event-1", "codex:codex-event-2"}) {
		t.Fatalf("items = %#v", gotIDs)
	}
	if !reflect.DeepEqual(transcript.inputs, []process.CodexTranscriptInput{{CodexSessionID: "codex-session-1"}, {CodexSessionID: "codex-session-2"}}) {
		t.Fatalf("transcript inputs = %#v", transcript.inputs)
	}
	if index.input != process.SessionID(sessionID) {
		t.Fatalf("index input = %q", index.input)
	}
}

func TestListSessionEventsPreservesHistoricalCodexPayload(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	store := &fakeStore{}
	sessions := &fakeSessionRepository{
		sessions: map[sessiondomain.ID]sessiondomain.Session{
			"session-1": {
				ID:             "session-1",
				ProjectID:      "project-1",
				CodexSessionID: "codex-session-1",
				UpdatedAt:      time.Unix(30, 0).UTC(),
			},
		},
	}
	transcript := &fakeTranscriptSource{
		events: []process.CodexEvent{{
			EventID: "codex-event-1",
			Type:    "item.completed",
			Payload: map[string]any{
				"workdir":       "/home/nzlov/workspaces/github/project",
				"authorization": "Bearer secret",
			},
			CreatedAt: time.Unix(20, 0).UTC(),
		}},
	}
	service := New(store, &fakeLiveSource{}, sessions, transcript, nil)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: sessiondomain.ID(sessionID),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	payload := got.Items[0].Payload
	if payload["workdir"] != "/home/nzlov/workspaces/github/project" || payload["authorization"] != "Bearer secret" {
		t.Fatalf("payload was changed: %#v", payload)
	}
}

func TestSessionEventsQueuesLiveEventsPublishedWhileLoadingSnapshot(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	afterStarted := make(chan struct{})
	releaseAfter := make(chan struct{})
	store := &fakeStore{
		events: []eventdomain.DomainEvent{
			{ID: "event-1", Scope: eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"}, SessionID: &sessionID, Type: "history", CreatedAt: time.Unix(1, 0).UTC()},
		},
		afterStarted: afterStarted,
		releaseAfter: releaseAfter,
	}
	liveSource := &fakeLiveSource{ch: make(chan event.DTO, 1)}
	service := New(store, liveSource, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan (<-chan DTO), 1)
	errs := make(chan error, 1)
	go func() {
		ch, err := service.SessionEvents(ctx, SessionEventsInput{Scope: eventdomain.Scope{SessionID: &sessionID}})
		if err != nil {
			errs <- err
			return
		}
		result <- ch
	}()
	<-afterStarted
	liveSource.ch <- event.DTO{
		ID:        "event-2",
		Scope:     eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		SessionID: &sessionID,
		Type:      "live",
		CreatedAt: time.Unix(2, 0).UTC().Format(time.RFC3339Nano),
	}
	close(releaseAfter)

	var ch <-chan DTO
	select {
	case err := <-errs:
		t.Fatalf("SessionEvents() error = %v", err)
	case ch = <-result:
	case <-time.After(time.Second):
		t.Fatal("SessionEvents() did not return")
	}
	if got := <-ch; got.ID != "event-1" {
		t.Fatalf("first event = %#v", got)
	}
	if got := <-ch; got.ID != "event-2" {
		t.Fatalf("second event = %#v", got)
	}
}

func TestSessionEventsCancelsLiveSubscriptionWhenSnapshotFails(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	store := &fakeStore{err: errors.New("snapshot failed")}
	liveSource := &fakeLiveSource{ch: make(chan event.DTO)}
	service := New(store, liveSource, nil, nil, nil)

	_, err := service.SessionEvents(context.Background(), SessionEventsInput{Scope: eventdomain.Scope{SessionID: &sessionID}})
	if err == nil {
		t.Fatal("SessionEvents() expected error")
	}
	select {
	case <-liveSource.done:
	case <-time.After(time.Second):
		t.Fatal("live subscription context was not canceled")
	}
}

func dtoIDs(items []DTO) []eventdomain.ID {
	ids := make([]eventdomain.ID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

type fakeStore struct {
	events       []eventdomain.DomainEvent
	err          error
	lastScope    eventdomain.Scope
	lastAfter    eventdomain.ID
	afterStarted chan struct{}
	releaseAfter chan struct{}
}

func (s *fakeStore) Append(context.Context, eventdomain.DomainEvent) error {
	return errors.New("unexpected Append call")
}

func (s *fakeStore) List(_ context.Context, scope eventdomain.Scope) ([]eventdomain.DomainEvent, error) {
	s.lastScope = scope
	return s.events, s.err
}

func (s *fakeStore) After(_ context.Context, scope eventdomain.Scope, after eventdomain.ID) ([]eventdomain.DomainEvent, error) {
	s.lastScope = scope
	s.lastAfter = after
	if s.afterStarted != nil {
		close(s.afterStarted)
	}
	if s.releaseAfter != nil {
		<-s.releaseAfter
	}
	return s.events, s.err
}

func (s *fakeStore) Before(context.Context, eventdomain.Scope, eventdomain.ID, int) ([]eventdomain.DomainEvent, int, bool, error) {
	return nil, 0, false, errors.New("unexpected Before call")
}

type fakeLiveSource struct {
	ch    chan event.DTO
	input event.LiveSessionEventsInput
	done  <-chan struct{}
}

func (s *fakeLiveSource) LiveSessionEvents(ctx context.Context, input event.LiveSessionEventsInput) (<-chan event.DTO, error) {
	s.input = input
	s.done = ctx.Done()
	if s.ch == nil {
		s.ch = make(chan event.DTO, 1)
	}
	return s.ch, nil
}

type fakeSessionRepository struct {
	sessions map[sessiondomain.ID]sessiondomain.Session
}

func (r *fakeSessionRepository) Find(_ context.Context, id sessiondomain.ID) (sessiondomain.Session, error) {
	session, ok := r.sessions[id]
	if !ok {
		return sessiondomain.Session{ID: id}, nil
	}
	return session, nil
}

type fakeTranscriptSource struct {
	input      process.CodexTranscriptInput
	inputs     []process.CodexTranscriptInput
	events     []process.CodexEvent
	eventsByID map[string][]process.CodexEvent
	err        error
}

func (s *fakeTranscriptSource) SessionEvents(_ context.Context, input process.CodexTranscriptInput) ([]process.CodexEvent, error) {
	s.input = input
	s.inputs = append(s.inputs, input)
	if s.eventsByID != nil {
		return s.eventsByID[input.CodexSessionID], s.err
	}
	return s.events, s.err
}

type fakeCodexSessionIndex struct {
	input process.SessionID
	ids   []string
	err   error
}

func (i *fakeCodexSessionIndex) CodexSessionIDs(_ context.Context, sessionID process.SessionID) ([]string, error) {
	i.input = sessionID
	return i.ids, i.err
}
