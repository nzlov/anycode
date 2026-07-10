package timeline

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/event"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestListSessionEventsUsesOnlyCodexTranscript(t *testing.T) {
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
	service := New(&fakeLiveSource{}, sessions, transcript, nil)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, []eventdomain.ID{"codex:codex-session-1:codex-event-1"}) {
		t.Fatalf("items = %#v", gotIDs)
	}
	codex := got.Items[0]
	if codex.Type != "process.codex_event" || codex.Payload["codexType"] != "item.completed" || codex.Payload["codexEventId"] != "codex-event-1" || codex.Payload["codexSessionId"] != "codex-session-1" {
		t.Fatalf("codex event = %#v", codex)
	}
	if transcript.input.CodexSessionID != "codex-session-1" || transcript.input.Workdir != "" {
		t.Fatalf("transcript input = %#v", transcript.input)
	}
}

func TestListSessionEventsReadsAllIndexedCodexSessions(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
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
				EventID:   "shared-event",
				Type:      "item.completed",
				Payload:   map[string]any{"item": map[string]any{"type": "agent_message", "aggregated_output": "first run"}},
				CreatedAt: time.Unix(10, 0).UTC(),
			}},
			"codex-session-2": {{
				EventID:   "shared-event",
				Type:      "item.completed",
				Payload:   map[string]any{"item": map[string]any{"type": "agent_message", "aggregated_output": "second run"}},
				CreatedAt: time.Unix(20, 0).UTC(),
			}},
		},
	}
	index := &fakeCodexSessionIndex{ids: []string{"codex-session-1", "codex-session-2"}}
	service := New(&fakeLiveSource{}, sessions, transcript, index)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	wantIDs := []eventdomain.ID{"codex:codex-session-1:shared-event", "codex:codex-session-2:shared-event"}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("items = %#v", gotIDs)
	}
	older, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID:     "session-1",
		BeforeEventID: wantIDs[1],
		Limit:         1,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents(before) error = %v", err)
	}
	if gotIDs := dtoIDs(older.Items); !reflect.DeepEqual(gotIDs, wantIDs[:1]) {
		t.Fatalf("older items = %#v", gotIDs)
	}
	wantInputs := []process.CodexTranscriptInput{
		{CodexSessionID: "codex-session-1"},
		{CodexSessionID: "codex-session-2"},
		{CodexSessionID: "codex-session-1"},
		{CodexSessionID: "codex-session-2"},
	}
	if !reflect.DeepEqual(transcript.inputs, wantInputs) {
		t.Fatalf("transcript inputs = %#v", transcript.inputs)
	}
	if index.input != process.SessionID(sessionID) {
		t.Fatalf("index input = %q", index.input)
	}
}

func TestListSessionEventsPreservesTranscriptOrderForEqualTimestamps(t *testing.T) {
	sessions := &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {
			ID:             "session-1",
			ProjectID:      "project-1",
			CodexSessionID: "codex-session-1",
			UpdatedAt:      time.Unix(30, 0).UTC(),
		},
	}}
	createdAt := time.Unix(20, 0).UTC()
	transcript := &fakeTranscriptSource{events: []process.CodexEvent{
		{EventID: "z-started", Type: "item.started", CreatedAt: createdAt},
		{EventID: "a-completed", Type: "item.completed", CreatedAt: createdAt},
	}}
	service := New(&fakeLiveSource{}, sessions, transcript, nil)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	want := []eventdomain.ID{
		"codex:codex-session-1:z-started",
		"codex:codex-session-1:a-completed",
	}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("items = %#v, want %#v", gotIDs, want)
	}
}

func TestListSessionEventsPreservesHistoricalCodexPayload(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
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
	service := New(&fakeLiveSource{}, sessions, transcript, nil)

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

func TestSessionEventsForwardsOnlyLiveEvents(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	liveSource := &fakeLiveSource{ch: make(chan event.DTO, 2)}
	transcript := &fakeTranscriptSource{err: context.Canceled}
	service := New(liveSource, nil, transcript, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.SessionEvents(ctx, SessionEventsInput{
		Scope: eventdomain.Scope{SessionID: &sessionID},
	})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	liveSource.ch <- event.DTO{
		ID:        "event-status",
		Scope:     eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		SessionID: &sessionID,
		Type:      "session.running",
		CreatedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano),
	}
	liveSource.ch <- event.DTO{
		ID:        "event-2",
		Scope:     eventdomain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		SessionID: &sessionID,
		Type:      "process.codex_event",
		CreatedAt: time.Unix(2, 0).UTC().Format(time.RFC3339Nano),
	}
	if got := <-ch; got.ID != "event-2" {
		t.Fatalf("live event = %#v", got)
	}
	if len(transcript.inputs) != 0 {
		t.Fatalf("subscription read transcript: %#v", transcript.inputs)
	}
}

func dtoIDs(items []DTO) []eventdomain.ID {
	ids := make([]eventdomain.ID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
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
