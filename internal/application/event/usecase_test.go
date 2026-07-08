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
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if got.Page != 1 || got.PageSize != 2 || got.Total != 3 || got.NextCursor != "event-2" {
		t.Fatalf("page mismatch: %#v", got)
	}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, []domain.ID{"event-2", "event-3"}) {
		t.Fatalf("items mismatch: %#v", got.Items)
	}
	if got.Items[0].CreatedAt != "1970-01-01T00:00:11.000000001Z" {
		t.Fatalf("CreatedAt = %q", got.Items[0].CreatedAt)
	}
	if !reflect.DeepEqual(store.lastScope.SessionID, &sessionID) {
		t.Fatalf("scope session = %#v", store.lastScope.SessionID)
	}
}

func TestListSessionEventsLatestFirstPagesNewestEventsBeforeCursorInAscendingDisplayOrder(t *testing.T) {
	ctx := context.Background()
	sessionID := domain.SessionID("session-1")
	store := &fakeStore{
		events: []domain.DomainEvent{
			{ID: "event-1", Scope: domain.Scope{SessionID: &sessionID}, SessionID: &sessionID, Type: "one", CreatedAt: time.Unix(1, 0).UTC()},
			{ID: "event-2", Scope: domain.Scope{SessionID: &sessionID}, SessionID: &sessionID, Type: "two", CreatedAt: time.Unix(2, 0).UTC()},
			{ID: "event-3", Scope: domain.Scope{SessionID: &sessionID}, SessionID: &sessionID, Type: "three", CreatedAt: time.Unix(3, 0).UTC()},
			{ID: "event-4", Scope: domain.Scope{SessionID: &sessionID}, SessionID: &sessionID, Type: "four", CreatedAt: time.Unix(4, 0).UTC()},
			{ID: "event-5", Scope: domain.Scope{SessionID: &sessionID}, SessionID: &sessionID, Type: "five", CreatedAt: time.Unix(5, 0).UTC()},
		},
	}
	service := New(store)

	latest, err := service.ListSessionEvents(ctx, ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() latest page error = %v", err)
	}
	if latest.Page != 1 || latest.PageSize != 2 || latest.Total != 5 || latest.NextCursor != "event-4" {
		t.Fatalf("latest page info = %#v", latest)
	}
	if gotIDs := dtoIDs(latest.Items); !reflect.DeepEqual(gotIDs, []domain.ID{"event-4", "event-5"}) {
		t.Fatalf("latest page ids = %#v", gotIDs)
	}

	older, err := service.ListSessionEvents(ctx, ListSessionEventsInput{
		SessionID:     "session-1",
		BeforeEventID: "event-4",
		Limit:         2,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() older page error = %v", err)
	}
	if older.NextCursor != "event-2" {
		t.Fatalf("older next cursor = %q", older.NextCursor)
	}
	if gotIDs := dtoIDs(older.Items); !reflect.DeepEqual(gotIDs, []domain.ID{"event-2", "event-3"}) {
		t.Fatalf("older page ids = %#v", gotIDs)
	}

	oldest, err := service.ListSessionEvents(ctx, ListSessionEventsInput{
		SessionID:     "session-1",
		BeforeEventID: "event-2",
		Limit:         2,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() oldest page error = %v", err)
	}
	if oldest.NextCursor != "" {
		t.Fatalf("oldest next cursor = %q", oldest.NextCursor)
	}
	if gotIDs := dtoIDs(oldest.Items); !reflect.DeepEqual(gotIDs, []domain.ID{"event-1"}) {
		t.Fatalf("oldest page ids = %#v", gotIDs)
	}
}

func TestListSessionEventsDefaultsAndCapsPageSize(t *testing.T) {
	store := &fakeStore{}
	service := New(store)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     500,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if got.Page != 1 || got.PageSize != 200 {
		t.Fatalf("page defaults = %#v", got)
	}
}

func dtoIDs(items []DTO) []domain.ID {
	ids := make([]domain.ID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestListSessionEventsPreservesCodexDisplayPayloadShapes(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	store := &fakeStore{
		events: []domain.DomainEvent{
			{
				ID:        "event-output",
				Scope:     domain.Scope{ProjectID: "project-1", SessionID: &sessionID},
				SessionID: &sessionID,
				Type:      "process.codex_event",
				Payload: map[string]any{
					"eventId":      "codex-event-output",
					"processRunId": "process-run-1",
					"codexType":    "assistant_message",
					"message": map[string]any{
						"role": "assistant",
						"content": []any{
							map[string]any{"type": "output_text", "text": "hello"},
						},
					},
					"raw": `{"type":"assistant_message"}`,
				},
				CreatedAt: time.Unix(20, 0).UTC(),
			},
			{
				ID:        "event-tool",
				Scope:     domain.Scope{ProjectID: "project-1", SessionID: &sessionID},
				SessionID: &sessionID,
				Type:      "process.codex_event",
				Payload: map[string]any{
					"eventId":      "codex-event-tool",
					"processRunId": "process-run-1",
					"codexType":    "tool_call",
					"tool": map[string]any{
						"name":      "shell",
						"callId":    "call-1",
						"arguments": map[string]any{"cmd": "go test ./..."},
					},
				},
				CreatedAt: time.Unix(21, 0).UTC(),
			},
			{
				ID:        "event-status",
				Scope:     domain.Scope{ProjectID: "project-1", SessionID: &sessionID},
				SessionID: &sessionID,
				Type:      "process.status_changed",
				Payload: map[string]any{
					"processRunId":   "process-run-1",
					"status":         "running",
					"pid":            float64(1234),
					"codexSessionId": "codex-session-1",
				},
				CreatedAt: time.Unix(22, 0).UTC(),
			},
		},
	}
	service := New(store)

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("items length = %d, want 3: %#v", len(got.Items), got.Items)
	}
	outputPayload := got.Items[0].Payload
	if outputPayload["eventId"] != "codex-event-output" || outputPayload["processRunId"] != "process-run-1" || outputPayload["codexType"] != "assistant_message" {
		t.Fatalf("output payload identifiers mismatch: %#v", outputPayload)
	}
	message, ok := outputPayload["message"].(map[string]any)
	if !ok {
		t.Fatalf("output message payload missing: %#v", outputPayload)
	}
	content, ok := message["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("output content payload mismatch: %#v", message["content"])
	}
	firstContent, ok := content[0].(map[string]any)
	if !ok || firstContent["type"] != "output_text" || firstContent["text"] != "hello" {
		t.Fatalf("output content item mismatch: %#v", content[0])
	}
	if outputPayload["raw"] != `{"type":"assistant_message"}` {
		t.Fatalf("raw payload mismatch: %#v", outputPayload["raw"])
	}

	toolPayload := got.Items[1].Payload
	tool, ok := toolPayload["tool"].(map[string]any)
	if !ok || tool["name"] != "shell" || tool["callId"] != "call-1" {
		t.Fatalf("tool payload mismatch: %#v", toolPayload)
	}
	arguments, ok := tool["arguments"].(map[string]any)
	if !ok || arguments["cmd"] != "go test ./..." {
		t.Fatalf("tool arguments mismatch: %#v", tool)
	}

	statusPayload := got.Items[2].Payload
	if statusPayload["processRunId"] != "process-run-1" || statusPayload["status"] != "running" || statusPayload["pid"] != float64(1234) || statusPayload["codexSessionId"] != "codex-session-1" {
		t.Fatalf("status payload mismatch: %#v", statusPayload)
	}
}

func TestSessionEventsSendsHistoryThenStreamsPublishedEvents(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	store := &fakeStore{
		events: []domain.DomainEvent{
			{ID: "event-1", Scope: domain.Scope{SessionID: &sessionID, ProjectID: "project-1"}, SessionID: &sessionID, Type: "one", CreatedAt: time.Unix(1, 0).UTC()},
			{ID: "event-2", Scope: domain.Scope{SessionID: &sessionID, ProjectID: "project-1"}, SessionID: &sessionID, Type: "two", CreatedAt: time.Unix(2, 0).UTC()},
		},
	}
	service := New(store)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := service.SessionEvents(ctx, SessionEventsInput{
		Scope:        domain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		AfterEventID: "event-0",
	})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	got := []DTO{<-ch, <-ch}
	if len(got) != 2 || got[0].ID != "event-1" || got[1].ID != "event-2" {
		t.Fatalf("history events = %#v", got)
	}
	if store.lastAfter != "event-0" {
		t.Fatalf("after id = %q", store.lastAfter)
	}

	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-3",
		Scope:     domain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		SessionID: &sessionID,
		Type:      "three",
		CreatedAt: time.Unix(3, 0).UTC(),
	}); err != nil {
		t.Fatalf("PublishAfterCommit() error = %v", err)
	}
	if event := <-ch; event.ID != "event-3" || event.Type != "three" {
		t.Fatalf("published event = %#v", event)
	}
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("SessionEvents() channel stayed open after context cancel")
	}
}

func TestSessionEventsFiltersPublishedEventsByScope(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	otherSessionID := domain.SessionID("session-2")
	service := New(&fakeStore{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.SessionEvents(ctx, SessionEventsInput{
		Scope: domain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
	})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-other-session",
		Scope:     domain.Scope{SessionID: &otherSessionID, ProjectID: "project-1"},
		SessionID: &otherSessionID,
		Type:      "other",
	}); err != nil {
		t.Fatalf("PublishAfterCommit() other session error = %v", err)
	}
	select {
	case event := <-ch:
		t.Fatalf("received mismatched event = %#v", event)
	default:
	}
	if err := service.PublishAfterCommit(context.Background(), domain.DomainEvent{
		ID:        "event-matching",
		Scope:     domain.Scope{SessionID: &sessionID, ProjectID: "project-1"},
		SessionID: &sessionID,
		Type:      "matching",
	}); err != nil {
		t.Fatalf("PublishAfterCommit() matching error = %v", err)
	}
	if event := <-ch; event.ID != "event-matching" {
		t.Fatalf("matching event = %#v", event)
	}
}

func TestSessionEventsEmptyScopeReceivesAllPublishedEvents(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	otherSessionID := domain.SessionID("session-2")
	service := New(&fakeStore{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.SessionEvents(ctx, SessionEventsInput{})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
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

func TestSessionEventsReturnsEmptyPayloadForNilDomainPayload(t *testing.T) {
	sessionID := domain.SessionID("session-1")
	store := &fakeStore{
		events: []domain.DomainEvent{
			{ID: "event-1", Scope: domain.Scope{SessionID: &sessionID}, SessionID: &sessionID, Type: "process.status_changed", CreatedAt: time.Unix(1, 0).UTC()},
		},
	}
	service := New(store)

	ch, err := service.SessionEvents(context.Background(), SessionEventsInput{
		Scope: domain.Scope{SessionID: &sessionID},
	})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	got := <-ch
	if got.Payload == nil {
		t.Fatal("Payload is nil, want empty map")
	}
	if len(got.Payload) != 0 {
		t.Fatalf("Payload = %#v, want empty map", got.Payload)
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
	events     []domain.DomainEvent
	err        error
	lastScope  domain.Scope
	lastAfter  domain.ID
	lastBefore domain.ID
	lastLimit  int
}

func (s *fakeStore) Append(context.Context, domain.DomainEvent) error {
	return errors.New("unexpected Append call")
}

func (s *fakeStore) After(_ context.Context, scope domain.Scope, after domain.ID) ([]domain.DomainEvent, error) {
	s.lastScope = scope
	s.lastAfter = after
	return s.events, s.err
}

func (s *fakeStore) Before(_ context.Context, scope domain.Scope, before domain.ID, limit int) ([]domain.DomainEvent, int, bool, error) {
	s.lastScope = scope
	s.lastBefore = before
	s.lastLimit = limit
	if s.err != nil {
		return nil, 0, false, s.err
	}
	end := len(s.events)
	if before != "" {
		end = -1
		for index, event := range s.events {
			if event.ID == before {
				end = index
				break
			}
		}
		if end < 0 {
			return nil, 0, false, errors.New("before event not found")
		}
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	return s.events[start:end], len(s.events), start > 0, nil
}
