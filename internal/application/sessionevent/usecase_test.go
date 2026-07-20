package sessionevent

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	eventapp "github.com/nzlov/anycode/internal/application/event"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

func TestSessionEventsForwardsTranscriptOnly(t *testing.T) {
	transcript := make(chan timelineapp.DTO, 1)
	timeline := &fakeTimelineSource{events: transcript}
	service := New(timeline, &fakeDomainEventSource{}, &fakeSessionStatusSource{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := service.SessionEvents(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if timeline.input.SessionID != "session-1" {
		t.Fatalf("session input = %#v", timeline.input)
	}
	want := timelineapp.DTO{
		ID: "message-1", Type: processdomain.CodexEventMessage, OccurredAt: "2026-07-17T10:00:00Z",
		Content: processdomain.CodexMessageContent{Role: "assistant", Text: "done"},
	}
	transcript <- want
	if got := <-stream; !reflect.DeepEqual(got, want) {
		t.Fatalf("transcript event = %#v, want %#v", got, want)
	}
}

func TestSessionUpdatesMapsGlobalCardEvents(t *testing.T) {
	domainEvents := make(chan eventapp.DTO, 10)
	timeline := &fakeTimelineSource{}
	events := &fakeDomainEventSource{events: domainEvents}
	status := sessionapp.CardStatusDTO{
		Status: sessiondomain.StatusRunning, CurrentNodeTitle: "Implement",
		AvailableActions: []string{"stop"}, UpdatedAt: time.Unix(2, 0).UTC(),
	}
	statuses := &fakeSessionStatusSource{status: status}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := New(timeline, events, statuses).SessionUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if events.input.Scope != (eventdomain.Scope{}) {
		t.Fatalf("domain event scope = %#v", events.input.Scope)
	}
	if cap(stream) != 0 {
		t.Fatalf("session update mapper buffer = %d, want 0", cap(stream))
	}

	sessionID := eventdomain.SessionID("session-1")
	domainEvents <- eventapp.DTO{
		ID: "status-1", SessionID: &sessionID, Type: TypeStatus, CreatedAt: "2026-07-17T10:00:01Z",
	}
	state := <-stream
	if state.Type != TypeStatus || state.SessionID != "session-1" || state.Status == nil || !reflect.DeepEqual(*state.Status, status) {
		t.Fatalf("status event = %#v", state)
	}
	if statuses.sessionID != "session-1" {
		t.Fatalf("status session id = %q", statuses.sessionID)
	}

	todo := sessiondomain.TodoList{Items: []sessiondomain.TodoItem{{Text: "Implement", Completed: true}}}
	domainEvents <- eventapp.DTO{
		ID: "todo-1", SessionID: &sessionID, Type: "session.todo_list_updated",
		Payload: map[string]any{"todoList": todo},
	}
	todoEvent := <-stream
	if todoEvent.TodoList == nil || !reflect.DeepEqual(*todoEvent.TodoList, todo) {
		t.Fatalf("todo event = %#v", todoEvent)
	}

	domainEvents <- eventapp.DTO{
		ID: "diff-1", SessionID: &sessionID, Type: "session.diff_changed",
		Payload: map[string]any{"filesChanged": 0},
	}
	diff := <-stream
	if diff.FilesChanged == nil || *diff.FilesChanged != 0 {
		t.Fatalf("diff event = %#v", diff)
	}

	domainEvents <- eventapp.DTO{
		ID: "artifact-1", SessionID: &sessionID, Type: "session.artifacts_updated",
		Payload: map[string]any{"artifactCount": 3},
	}
	artifact := <-stream
	if artifact.ArtifactCount == nil || *artifact.ArtifactCount != 3 {
		t.Fatalf("artifact event = %#v", artifact)
	}

	metadataAt := time.Unix(3, 0).UTC()
	domainEvents <- eventapp.DTO{
		ID: "priority-1", SessionID: &sessionID, Type: "session.priority_changed",
		Payload: map[string]any{"priority": sessiondomain.PriorityHigh, "updatedAt": metadataAt},
	}
	priority := <-stream
	if priority.Type != "session.priority_changed" || priority.Priority == nil || *priority.Priority != sessiondomain.PriorityHigh || priority.UpdatedAt == nil || !priority.UpdatedAt.Equal(metadataAt) {
		t.Fatalf("priority event = %#v", priority)
	}

	config := sessiondomain.Config{CodexModel: "gpt-5.4", ReasoningEffort: "high", PermissionMode: "workspace-write", FastMode: true}
	domainEvents <- eventapp.DTO{
		ID: "config-1", SessionID: &sessionID, Type: "session.config_changed",
		Payload: map[string]any{"config": config, "updatedAt": metadataAt},
	}
	configEvent := <-stream
	if configEvent.Config == nil || !reflect.DeepEqual(*configEvent.Config, config) || configEvent.UpdatedAt == nil || !configEvent.UpdatedAt.Equal(metadataAt) {
		t.Fatalf("config event = %#v", configEvent)
	}

	cleanup := sessionapp.WorktreeCleanupDTO{Status: sessiondomain.WorktreeCleanupFailed, Attempts: 2}
	actions := []string{"retry_worktree_cleanup"}
	domainEvents <- eventapp.DTO{
		ID: "cleanup-1", SessionID: &sessionID, Type: "session.worktree_cleanup_failed",
		Payload: map[string]any{"worktreeCleanup": cleanup, "availableActions": actions, "updatedAt": metadataAt},
	}
	cleanupEvent := <-stream
	if cleanupEvent.WorktreeCleanup == nil || !reflect.DeepEqual(*cleanupEvent.WorktreeCleanup, cleanup) || !reflect.DeepEqual(cleanupEvent.AvailableActions, actions) || cleanupEvent.UpdatedAt == nil || !cleanupEvent.UpdatedAt.Equal(metadataAt) {
		t.Fatalf("worktree event = %#v", cleanupEvent)
	}

	usage := sessiondomain.TokenUsage{InputTokens: 10, TotalTokens: 12}
	usageSessionID := eventdomain.SessionID("session-2")
	domainEvents <- eventapp.DTO{
		ID: "usage-1", SessionID: &usageSessionID, Type: "session.usage_updated", CreatedAt: "2026-07-17T10:00:02Z",
		Payload: map[string]any{"usage": usage},
	}
	usageEvent := <-stream
	if usageEvent.Type != TypeUsage || usageEvent.SessionID != "session-2" || usageEvent.Usage == nil || !reflect.DeepEqual(*usageEvent.Usage, usage) {
		t.Fatalf("usage event = %#v", usageEvent)
	}
}

func TestSessionUpdatesIgnoresBusinessAndRawArtifactEvents(t *testing.T) {
	domainEvents := make(chan eventapp.DTO, 7)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := New(
		&fakeTimelineSource{},
		&fakeDomainEventSource{events: domainEvents},
		&fakeSessionStatusSource{},
	).SessionUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := eventdomain.SessionID("session-1")
	for _, eventType := range []string{
		"question.pending",
		"workflow.approval_submitted",
		"workflow.failed",
		"workflow.system_advance_pending",
		"session.prompt_append_cancelled",
		"session.running",
		"artifact.published",
	} {
		domainEvents <- eventapp.DTO{SessionID: &sessionID, Type: eventType}
	}
	select {
	case got := <-stream:
		t.Fatalf("ignored event reached card stream: %#v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSessionUpdatesClosesWhenRequiredSourceCloses(t *testing.T) {
	domainEvents := make(chan eventapp.DTO)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := New(
		&fakeTimelineSource{},
		&fakeDomainEventSource{events: domainEvents},
		&fakeSessionStatusSource{},
	).SessionUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	close(domainEvents)
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("card stream emitted after source closed")
		}
	case <-time.After(time.Second):
		t.Fatal("card stream stayed open after source closed")
	}
}

func TestSessionUpdatesClosesWhenStatusReadFails(t *testing.T) {
	domainEvents := make(chan eventapp.DTO, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := New(
		&fakeTimelineSource{},
		&fakeDomainEventSource{events: domainEvents},
		&fakeSessionStatusSource{err: errors.New("temporary read failure")},
	).SessionUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := eventdomain.SessionID("session-1")
	domainEvents <- eventapp.DTO{SessionID: &sessionID, Type: TypeStatus}
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("card stream emitted without status data")
		}
	case <-time.After(time.Second):
		t.Fatal("card stream stayed open after status read failed")
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

type fakeSessionStatusSource struct {
	status    sessionapp.CardStatusDTO
	err       error
	sessionID sessiondomain.ID
}

func (f *fakeSessionStatusSource) GetSessionCardStatus(_ context.Context, sessionID sessiondomain.ID) (sessionapp.CardStatusDTO, error) {
	f.sessionID = sessionID
	return f.status, f.err
}
