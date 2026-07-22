package timeline

import (
	"context"
	"errors"
	"testing"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestListSessionEventsUsesAppServerHistory(t *testing.T) {
	sessionEventID := eventdomain.SessionID("session-1")
	sessions := &fakeSessionRepository{session: sessiondomain.Session{
		ID: "session-1", ProjectID: "project-1", CodexSessionID: "thread-1",
		Usage: sessiondomain.TokenUsage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14},
	}}
	codex := &fakeCodexHistory{page: processdomain.CodexHistoryPage{
		Events: []processdomain.CodexEvent{
			{EventID: "assistant-1", CodexSessionID: "thread-1", Type: processdomain.CodexEventMessage, Content: processdomain.CodexMessageContent{Role: "assistant", Text: "done"}, CreatedAt: time.Unix(20, 0).UTC()},
			{EventID: "usage-1", CodexSessionID: "thread-1", Type: processdomain.CodexEventUsage, Content: processdomain.CodexUsageContent{TotalTokens: 14}, CreatedAt: time.Unix(21, 0).UTC()},
		},
		NextCursor: "next-turn",
	}}
	history := &fakeEventHistory{events: []eventdomain.DomainEvent{{
		ID: "status-1", Scope: eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionEventID},
		SessionID: &sessionEventID, Type: "session.running", CreatedAt: time.Unix(10, 0).UTC(),
	}}}
	service := New(&fakeLiveSource{}, sessions, codex, WithHistory(history))

	page, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[1].ID != "codex:thread-1:assistant-1" {
		t.Fatalf("items = %#v", page.Items)
	}
	if page.NextCursor != "next-turn" || page.Usage == nil || page.Usage.TotalTokens != 14 {
		t.Fatalf("page = %#v", page)
	}
	if codex.input.ThreadID != "thread-1" || codex.input.Limit != 25 {
		t.Fatalf("history input = %#v", codex.input)
	}
}

func TestListSessionEventsPassesCursorAndFiltersMessageRole(t *testing.T) {
	sessions := &fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", CodexSessionID: "thread-1"}}
	codex := &fakeCodexHistory{page: processdomain.CodexHistoryPage{Events: []processdomain.CodexEvent{
		{EventID: "user-1", CodexSessionID: "thread-1", Type: processdomain.CodexEventMessage, Content: processdomain.CodexMessageContent{Role: "user", Text: "question"}},
		{EventID: "assistant-1", CodexSessionID: "thread-1", Type: processdomain.CodexEventMessage, Content: processdomain.CodexMessageContent{Role: "assistant", Text: "answer"}},
	}}}
	service := New(&fakeLiveSource{}, sessions, codex)
	page, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1", BeforeCursor: "cursor-1", MessageRole: "assistant", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "codex:thread-1:assistant-1" {
		t.Fatalf("items = %#v", page.Items)
	}
	if codex.input.Cursor != "cursor-1" {
		t.Fatalf("cursor = %q", codex.input.Cursor)
	}
}

func TestListSessionEventsFallsBackToStoredEventsWithoutThread(t *testing.T) {
	sessionEventID := eventdomain.SessionID("session-1")
	sessions := &fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", ProjectID: "project-1"}}
	history := &fakeEventHistory{events: []eventdomain.DomainEvent{{
		ID: "status-1", Scope: eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionEventID},
		SessionID: &sessionEventID, Type: "session.completed", CreatedAt: time.Unix(10, 0).UTC(),
	}}}
	service := New(&fakeLiveSource{}, sessions, nil, WithHistory(history))
	page, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "status-1" {
		t.Fatalf("items = %#v", page.Items)
	}
}

func TestListSessionEventsReturnsAppServerHistoryFailure(t *testing.T) {
	sessions := &fakeSessionRepository{session: sessiondomain.Session{ID: "session-1", CodexSessionID: "thread-1"}}
	service := New(&fakeLiveSource{}, sessions, &fakeCodexHistory{err: errors.New("app-server unavailable")})
	if _, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1"}); err == nil {
		t.Fatal("expected history failure")
	}
}

func TestSessionEventsForwardsLiveCodexEvents(t *testing.T) {
	live := &fakeLiveSource{events: make(chan processdomain.CodexEvent, 1)}
	service := New(live, &fakeSessionRepository{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := service.SessionEvents(ctx, SessionEventsInput{SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	live.events <- processdomain.CodexEvent{
		EventID: "message-1", CodexSessionID: "thread-1", Type: processdomain.CodexEventMessage,
		Content: processdomain.CodexMessageContent{Role: "assistant", Text: "done"},
	}
	select {
	case event := <-stream:
		if event.ID != "codex:thread-1:message-1" {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("live event was not forwarded")
	}
}

type fakeSessionRepository struct {
	session sessiondomain.Session
	err     error
}

func (r *fakeSessionRepository) Find(context.Context, sessiondomain.ID) (sessiondomain.Session, error) {
	return r.session, r.err
}

type fakeCodexHistory struct {
	input processdomain.CodexHistoryPageInput
	page  processdomain.CodexHistoryPage
	err   error
}

func (h *fakeCodexHistory) HistoryPage(_ context.Context, input processdomain.CodexHistoryPageInput) (processdomain.CodexHistoryPage, error) {
	h.input = input
	return h.page, h.err
}

type fakeLiveSource struct {
	input  processdomain.SessionID
	events chan processdomain.CodexEvent
}

func (s *fakeLiveSource) LiveCodexEvents(_ context.Context, sessionID processdomain.SessionID) (<-chan processdomain.CodexEvent, error) {
	s.input = sessionID
	if s.events == nil {
		s.events = make(chan processdomain.CodexEvent)
	}
	return s.events, nil
}

type fakeEventHistory struct {
	events []eventdomain.DomainEvent
	err    error
}

func (s *fakeEventHistory) Append(context.Context, eventdomain.DomainEvent) error { return nil }
func (s *fakeEventHistory) List(context.Context, eventdomain.Scope) ([]eventdomain.DomainEvent, error) {
	return append([]eventdomain.DomainEvent(nil), s.events...), s.err
}
func (s *fakeEventHistory) After(context.Context, eventdomain.Scope, eventdomain.ID) ([]eventdomain.DomainEvent, error) {
	return nil, nil
}
func (s *fakeEventHistory) Before(context.Context, eventdomain.Scope, eventdomain.ID, int) ([]eventdomain.DomainEvent, int, bool, error) {
	return nil, 0, false, nil
}
