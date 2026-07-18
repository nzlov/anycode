package sessionevent

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	eventapp "github.com/nzlov/anycode/internal/application/event"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestSessionEventsCombinesTranscriptUsageStateAndQuestions(t *testing.T) {
	transcript := make(chan timelineapp.DTO, 2)
	domainEvents := make(chan eventapp.DTO, 1)
	questions := make(chan questionapp.BatchDTO, 1)
	timeline := &fakeTimelineSource{events: transcript}
	events := &fakeDomainEventSource{events: domainEvents}
	sessions := &fakeSessionSource{detail: sessionapp.DetailDTO{DTO: sessionapp.DTO{ID: "session-1", Status: sessiondomain.StatusRunning}}}
	questionSource := &fakeQuestionSource{events: questions}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := New(timeline, events, sessions, questionSource).SessionEvents(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if timeline.input.SessionID != "session-1" || events.input.Scope.SessionID == nil || *events.input.Scope.SessionID != "session-1" || questionSource.sessionID != "session-1" {
		t.Fatalf("subscription inputs: timeline=%#v events=%#v question=%q", timeline.input, events.input, questionSource.sessionID)
	}

	transcript <- timelineapp.DTO{
		ID: "message-1", Type: processdomain.CodexEventMessage, OccurredAt: "2026-07-17T10:00:00Z",
		Content: processdomain.CodexMessageContent{Role: "assistant", Text: "done"},
	}
	message := <-stream
	if message.Type != string(processdomain.CodexEventMessage) || message.Transcript == nil || message.Transcript.ID != "message-1" {
		t.Fatalf("transcript event = %#v", message)
	}

	usage := &timelineapp.TokenUsageDTO{InputTokens: 10, OutputTokens: 3, TotalTokens: 13}
	transcript <- timelineapp.DTO{ID: "usage-1", Type: processdomain.CodexEventUsage, Usage: usage}
	usageEvent := <-stream
	if usageEvent.Type != TypeUsage || !reflect.DeepEqual(usageEvent.Usage, usage) || usageEvent.Transcript != nil {
		t.Fatalf("usage event = %#v", usageEvent)
	}

	eventSessionID := eventdomain.SessionID("session-1")
	domainEvents <- eventapp.DTO{ID: "state-1", SessionID: &eventSessionID, Type: "session.running", CreatedAt: "2026-07-17T10:00:01Z"}
	stateEvent := <-stream
	if stateEvent.Type != "session.running" || stateEvent.Session == nil || stateEvent.Session.ID != "session-1" {
		t.Fatalf("state event = %#v", stateEvent)
	}

	questions <- questionapp.BatchDTO{ID: "batch-1", SessionID: "session-1", Status: questiondomain.BatchPending}
	questionEvent := <-stream
	if questionEvent.Type != TypeQuestion || questionEvent.QuestionBatch == nil || questionEvent.QuestionBatch.ID != "batch-1" || questionEvent.Session == nil {
		t.Fatalf("question event = %#v", questionEvent)
	}

	close(transcript)
	close(domainEvents)
	close(questions)
	if _, ok := <-stream; ok {
		t.Fatal("session event stream stayed open after all sources closed")
	}
}

func TestSessionEventsClosesWhenRequiredSourceCloses(t *testing.T) {
	transcript := make(chan timelineapp.DTO)
	domainEvents := make(chan eventapp.DTO)
	questions := make(chan questionapp.BatchDTO)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := New(
		&fakeTimelineSource{events: transcript},
		&fakeDomainEventSource{events: domainEvents},
		&fakeSessionSource{},
		&fakeQuestionSource{events: questions},
	).SessionEvents(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}

	close(transcript)
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("session event stream emitted after transcript source closed")
		}
	case <-time.After(time.Second):
		t.Fatal("session event stream stayed open after transcript source closed")
	}
}

func TestSessionEventsClosesWhenSessionProjectionFails(t *testing.T) {
	for _, source := range []string{"domain", "question"} {
		t.Run(source, func(t *testing.T) {
			transcript := make(chan timelineapp.DTO)
			domainEvents := make(chan eventapp.DTO, 1)
			questions := make(chan questionapp.BatchDTO, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stream, err := New(
				&fakeTimelineSource{events: transcript},
				&fakeDomainEventSource{events: domainEvents},
				&fakeSessionSource{err: errors.New("temporary read failure")},
				&fakeQuestionSource{events: questions},
			).SessionEvents(ctx, "session-1")
			if err != nil {
				t.Fatal(err)
			}
			if source == "domain" {
				domainEvents <- eventapp.DTO{ID: "state-1", Type: "session.todo_list_updated"}
			} else {
				questions <- questionapp.BatchDTO{ID: "batch-1", SessionID: "session-1"}
			}
			select {
			case _, ok := <-stream:
				if ok {
					t.Fatal("session event stream emitted an unprojected update")
				}
			case <-time.After(time.Second):
				t.Fatal("session event stream stayed open after projection failed")
			}
		})
	}
}

type fakeTimelineSource struct {
	events <-chan timelineapp.DTO
	input  timelineapp.SessionEventsInput
}

func (f *fakeTimelineSource) SessionEvents(_ context.Context, input timelineapp.SessionEventsInput) (<-chan timelineapp.DTO, error) {
	f.input = input
	return f.events, nil
}

type fakeDomainEventSource struct {
	events <-chan eventapp.DTO
	input  eventapp.LiveSessionEventsInput
}

func (f *fakeDomainEventSource) LiveSessionEvents(_ context.Context, input eventapp.LiveSessionEventsInput) (<-chan eventapp.DTO, error) {
	f.input = input
	return f.events, nil
}

type fakeSessionSource struct {
	detail sessionapp.DetailDTO
	err    error
}

func (f *fakeSessionSource) GetSession(context.Context, sessiondomain.ID) (sessionapp.DetailDTO, error) {
	return f.detail, f.err
}

type fakeQuestionSource struct {
	events    <-chan questionapp.BatchDTO
	sessionID questiondomain.SessionID
}

func (f *fakeQuestionSource) QuestionBatchUpdates(_ context.Context, sessionID questiondomain.SessionID) (<-chan questionapp.BatchDTO, error) {
	f.sessionID = sessionID
	return f.events, nil
}
