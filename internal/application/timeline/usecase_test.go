package timeline

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/event"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestListSessionEventsCombinesCodexTranscriptAndPersistedStatus(t *testing.T) {
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
		events: []process.CodexEvent{
			{
				EventID:       "codex-event-1",
				Type:          "item.completed",
				CorrelationID: "call-1",
				Phase:         process.CodexPhaseStandalone,
				Content:       process.CodexFileChangeContent{Changes: []process.CodexFileChange{{Kind: "modified", Path: "a.txt"}}},
				CreatedAt:     time.Unix(20, 0).UTC(),
			},
			{
				EventID:   "usage-1",
				Type:      "token_count",
				Content:   process.CodexUsageContent{InputTokens: 10, OutputTokens: 4, TotalTokens: 14},
				CreatedAt: time.Unix(25, 0).UTC(),
			},
		},
	}
	history := &fakeEventHistory{events: []eventdomain.DomainEvent{
		{
			ID:        "status-1",
			Scope:     eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "session.running",
			CreatedAt: time.Unix(10, 0).UTC(),
		},
		{
			ID:        "internal-1",
			Scope:     eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "attachment.archived",
			CreatedAt: time.Unix(15, 0).UTC(),
		},
	}}
	service := New(&fakeLiveSource{}, sessions, transcript, nil, WithHistory(history))

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, []eventdomain.ID{"status-1", "codex:codex-session-1:codex-event-1"}) {
		t.Fatalf("items = %#v", gotIDs)
	}
	if got.Total != 2 || got.Usage == nil || got.Usage.TotalTokens != 14 {
		t.Fatalf("page metadata = total %d, usage %#v", got.Total, got.Usage)
	}
	status, ok := got.Items[0].Content.(process.CodexStatusContent)
	if !ok || status.Code != "session.running" {
		t.Fatalf("status content = %#v", got.Items[0].Content)
	}
	codex := got.Items[1]
	if codex.Phase != process.CodexPhaseStandalone || codex.CorrelationID != "codex:codex-session-1:call-1" || codex.OccurredAt != "1970-01-01T00:00:20Z" {
		t.Fatalf("codex event = %#v", codex)
	}
	if content, ok := codex.Content.(process.CodexFileChangeContent); !ok || len(content.Changes) != 1 || content.Changes[0].Path != "a.txt" {
		t.Fatalf("codex content = %#v", codex.Content)
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
				Content:   process.CodexMessageContent{Role: "assistant", Text: "first run", Format: process.CodexTextMarkdown},
				CreatedAt: time.Unix(10, 0).UTC(),
			}},
			"codex-session-2": {{
				EventID:   "shared-event",
				Type:      "item.completed",
				Content:   process.CodexMessageContent{Role: "assistant", Text: "second run", Format: process.CodexTextMarkdown},
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
		{EventID: "z-started", Type: "item.started", CorrelationID: "call-1", Phase: process.CodexPhaseStarted, Content: process.CodexToolContent{}, SourceOffset: 10, CreatedAt: createdAt},
		{EventID: "a-completed", Type: "item.completed", CorrelationID: "call-1", Phase: process.CodexPhaseCompleted, Content: process.CodexToolContent{}, SourceOffset: 20, CreatedAt: createdAt},
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
	if got.Items[0].CorrelationID != "codex:codex-session-1:call-1" || got.Items[1].CorrelationID != got.Items[0].CorrelationID {
		t.Fatalf("correlation ids = %q, %q", got.Items[0].CorrelationID, got.Items[1].CorrelationID)
	}
}

func TestListSessionEventsPreservesCodexSessionOrderForEqualSourcePositions(t *testing.T) {
	sessions := &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {ID: "session-1", ProjectID: "project-1", CodexSessionID: "a-new"},
	}}
	createdAt := time.Unix(20, 0).UTC()
	transcript := &fakeTranscriptSource{eventsByID: map[string][]process.CodexEvent{
		"z-old": {{EventID: "event", Type: "item.completed", Content: process.CodexMessageContent{Role: "assistant", Text: "old"}, SourceOffset: 10, CreatedAt: createdAt}},
		"a-new": {{EventID: "event", Type: "item.completed", Content: process.CodexMessageContent{Role: "assistant", Text: "new"}, SourceOffset: 10, CreatedAt: createdAt}},
	}}
	service := New(&fakeLiveSource{}, sessions, transcript, &fakeCodexSessionIndex{ids: []string{"z-old", "a-new"}})

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	want := []eventdomain.ID{"codex:z-old:event", "codex:a-new:event"}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("items = %#v, want %#v", gotIDs, want)
	}
}

func TestListSessionEventsFiltersMessageRoleBeforePaging(t *testing.T) {
	sessions := &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {
			ID:             "session-1",
			ProjectID:      "project-1",
			CodexSessionID: "codex-session-1",
		},
	}}
	events := make([]process.CodexEvent, 0, 24)
	for index := 1; index <= 12; index++ {
		createdAt := time.Unix(int64(index*2), 0).UTC()
		events = append(events,
			process.CodexEvent{
				EventID:   fmt.Sprintf("assistant-%02d", index),
				Type:      "item.completed",
				Content:   process.CodexMessageContent{Role: "assistant", Text: fmt.Sprintf("answer %d", index)},
				CreatedAt: createdAt,
			},
			process.CodexEvent{
				EventID:   fmt.Sprintf("user-%02d", index),
				Type:      "item.completed",
				Content:   process.CodexMessageContent{Role: "user", Text: fmt.Sprintf("question %d", index)},
				CreatedAt: createdAt.Add(time.Second),
			},
		)
	}
	service := New(&fakeLiveSource{}, sessions, &fakeTranscriptSource{events: events}, nil)

	latest, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID:   "session-1",
		Limit:       10,
		MessageRole: "assistant",
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	wantLatest := make([]eventdomain.ID, 0, 10)
	for index := 3; index <= 12; index++ {
		wantLatest = append(wantLatest, eventdomain.ID(fmt.Sprintf("codex:codex-session-1:assistant-%02d", index)))
	}
	if gotIDs := dtoIDs(latest.Items); !reflect.DeepEqual(gotIDs, wantLatest) {
		t.Fatalf("latest items = %#v, want %#v", gotIDs, wantLatest)
	}
	if latest.Total != 12 || latest.NextCursor != "codex:codex-session-1:assistant-03" {
		t.Fatalf("latest page metadata = total %d, next cursor %q", latest.Total, latest.NextCursor)
	}

	older, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID:     "session-1",
		BeforeEventID: "codex:codex-session-1:assistant-03",
		Limit:         10,
		MessageRole:   "assistant",
	})
	if err != nil {
		t.Fatalf("ListSessionEvents(before) error = %v", err)
	}
	wantOlder := []eventdomain.ID{
		"codex:codex-session-1:assistant-01",
		"codex:codex-session-1:assistant-02",
	}
	if gotIDs := dtoIDs(older.Items); !reflect.DeepEqual(gotIDs, wantOlder) {
		t.Fatalf("older items = %#v, want %#v", gotIDs, wantOlder)
	}
	if older.Total != 12 || older.NextCursor != "" {
		t.Fatalf("older page metadata = total %d, next cursor %q", older.Total, older.NextCursor)
	}

	unfiltered, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents(unfiltered) error = %v", err)
	}
	if unfiltered.Total != 24 || len(unfiltered.Items) != 10 {
		t.Fatalf("unfiltered page metadata = total %d, items %d", unfiltered.Total, len(unfiltered.Items))
	}
}

func TestHistoryAndLiveUseTheSameOrderKeyForTheSameEvent(t *testing.T) {
	createdAt := time.Unix(20, 0).UTC()
	sessionID := eventdomain.SessionID("session-1")
	sessions := &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {
			ID:             "session-1",
			ProjectID:      "project-1",
			CodexSessionID: "codex-session-1",
		},
	}}
	content := process.CodexToolContent{Output: process.CodexStructuredText{Format: process.CodexTextPlain, Text: "done"}}
	transcript := &fakeTranscriptSource{events: []process.CodexEvent{{
		EventID:       "event-1",
		Type:          "item.completed",
		CorrelationID: "call-1",
		Phase:         process.CodexPhaseCompleted,
		Content:       content,
		SourceOffset:  42,
		SourceIndex:   1,
		CreatedAt:     createdAt,
	}}}
	live := &fakeLiveSource{ch: make(chan event.DTO, 1)}
	service := New(live, sessions, transcript, nil)
	history, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := service.SessionEvents(ctx, SessionEventsInput{Scope: eventdomain.Scope{SessionID: &sessionID}})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	live.ch <- event.DTO{
		ID:        "codex:codex-session-1:event-1",
		SessionID: &sessionID,
		Type:      "process.codex_event",
		Payload: map[string]any{
			"codexSessionId":     "codex-session-1",
			"codexCorrelationId": "call-1",
			"codexPhase":         string(process.CodexPhaseCompleted),
			"codexContent":       content,
			"codexSourceOffset":  int64(42),
			"codexSourceIndex":   1,
		},
		CreatedAt: createdAt.Format(time.RFC3339Nano),
	}
	liveEvent := <-stream
	if history.Items[0].OrderKey != liveEvent.OrderKey {
		t.Fatalf("history/live order keys = %q/%q", history.Items[0].OrderKey, liveEvent.OrderKey)
	}
}

func TestListSessionEventsPreservesUnknownCodexPayload(t *testing.T) {
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
			Content: process.CodexUnknownContent{RawType: "future_event", Payload: map[string]any{
				"workdir":       "/home/nzlov/workspaces/github/project",
				"authorization": "Bearer secret",
			}},
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
	unknown, ok := got.Items[0].Content.(process.CodexUnknownContent)
	if !ok || unknown.Payload["workdir"] != "/home/nzlov/workspaces/github/project" || unknown.Payload["authorization"] != "Bearer secret" {
		t.Fatalf("unknown content was changed: %#v", got.Items[0].Content)
	}
}

func TestSessionEventsForwardsTypedLiveEventsInArrivalOrder(t *testing.T) {
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
		Payload: map[string]any{
			"codexSessionId":     "codex-session-1",
			"codexCorrelationId": "call-1",
			"codexPhase":         string(process.CodexPhaseCompleted),
			"codexContent":       process.CodexToolContent{Output: process.CodexStructuredText{Format: process.CodexTextPlain, Text: "ok"}},
		},
		CreatedAt: time.Unix(2, 0).UTC().Format(time.RFC3339Nano),
	}
	if got := <-ch; got.ID != "event-status" {
		t.Fatalf("status event = %#v", got)
	}
	if got := <-ch; got.ID != "event-2" || got.CorrelationID != "codex:codex-session-1:call-1" || got.Phase != process.CodexPhaseCompleted {
		t.Fatalf("codex event = %#v", got)
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

type fakeEventHistory struct {
	events []eventdomain.DomainEvent
	err    error
}

func (s *fakeEventHistory) Append(context.Context, eventdomain.DomainEvent) error {
	return nil
}

func (s *fakeEventHistory) List(context.Context, eventdomain.Scope) ([]eventdomain.DomainEvent, error) {
	return append([]eventdomain.DomainEvent(nil), s.events...), s.err
}

func (s *fakeEventHistory) After(context.Context, eventdomain.Scope, eventdomain.ID) ([]eventdomain.DomainEvent, error) {
	return nil, nil
}

func (s *fakeEventHistory) Before(context.Context, eventdomain.Scope, eventdomain.ID, int) ([]eventdomain.DomainEvent, int, bool, error) {
	return nil, 0, false, nil
}

func (i *fakeCodexSessionIndex) CodexSessionIDs(_ context.Context, sessionID process.SessionID) ([]string, error) {
	i.input = sessionID
	return i.ids, i.err
}
