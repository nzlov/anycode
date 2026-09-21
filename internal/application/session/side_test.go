package session

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

func TestStartSidePersistsTranscriptAndRestoresIt(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = sideTestSession()
	sides := newFakeSideRepository()
	events := make(chan processdomain.CodexEvent, 3)
	events <- sideMessageEvent("message-1", 1, "answer")
	events <- sideExitEvent(2, processdomain.ExitResult{})
	close(events)
	codex := &fakeCodexProcess{
		forkHandle: processdomain.CodexHandle{CodexSessionID: "side-thread", TurnID: "turn-1"},
		events:     events,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithSessionSides(sides))
	defer service.Close()
	service.generateID = func() (domain.ID, error) { return "side-run-1", nil }

	run, err := service.StartSide(context.Background(), SideInput{SessionID: "session-1", Prompt: "  inspect the parser  "})
	if err != nil {
		t.Fatal(err)
	}
	if run.CodexSessionID != "side-thread" || run.ProcessRunID != "side-run-1" || run.TurnID != "turn-1" {
		t.Fatalf("side run = %#v", run)
	}
	input := codex.forkInput
	if !codex.forkCalled || !input.Ephemeral || input.SourceCodexSessionID != "source-thread" {
		t.Fatalf("side fork = %#v", input)
	}
	if input.PermissionMode != "read-only" || input.Workdir != "/workspace/session-1" || len(input.Input) != 1 || input.Input[0].Text != "inspect the parser" {
		t.Fatalf("side start input = %#v", input.CodexStartInput)
	}
	waitForSideStatus(t, sides, "side-thread", domain.SideStatusCompleted)

	restored, err := service.ListSides(context.Background(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || restored[0].Prompt != "inspect the parser" || restored[0].Status != domain.SideStatusCompleted {
		t.Fatalf("restored Sides = %#v", restored)
	}
	if len(restored[0].Events) != 1 || restored[0].Events[0].Content.(processdomain.CodexMessageContent).Text != "answer" {
		t.Fatalf("restored Side events = %#v", restored[0].Events)
	}
	if len(repo.saved) != 0 {
		t.Fatalf("Side mutated source session: saved=%d", len(repo.saved))
	}
}

func TestSideSubscriptionDisconnectKeepsRunAndReplaysStoredEvents(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = sideTestSession()
	sides := newFakeSideRepository()
	events := make(chan processdomain.CodexEvent, 4)
	codex := &fakeCodexProcess{
		forkHandle: processdomain.CodexHandle{CodexSessionID: "side-thread", TurnID: "turn-1"},
		events:     events,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithSessionSides(sides))
	defer service.Close()
	service.generateID = func() (domain.ID, error) { return "side-run-1", nil }
	run, err := service.StartSide(context.Background(), SideInput{SessionID: "session-1", Prompt: "inspect"})
	if err != nil {
		t.Fatal(err)
	}

	firstCtx, cancel := context.WithCancel(context.Background())
	first, err := service.SideEvents(firstCtx, run.ProcessRunID)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("disconnected Side subscription did not close")
	}
	if codex.stoppedID != "" {
		t.Fatalf("subscription disconnect stopped Side run %q", codex.stoppedID)
	}

	events <- sideMessageEvent("message-1", 1, "after refresh")
	waitForSideEventCount(t, sides, "side-thread", 1)
	second, err := service.SideEvents(context.Background(), run.ProcessRunID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-second:
		if event.Content.(processdomain.CodexMessageContent).Text != "after refresh" {
			t.Fatalf("replayed event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("stored Side event was not replayed")
	}
	events <- sideExitEvent(2, processdomain.ExitResult{})
	close(events)
}

func TestRecoverInterruptedSidesMarksRunningRecordsFailed(t *testing.T) {
	repo := newFakeRepository()
	sides := newFakeSideRepository()
	sides.sides["side-thread"] = domain.Side{
		ID: "side-thread", SessionID: "session-1", ProcessRunID: "side-run-1", Prompt: "inspect",
		Status: domain.SideStatusRunning, TurnIndex: 1,
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithSessionSides(sides))
	service.now = func() time.Time { return time.Unix(100, 0).UTC() }

	if _, err := service.RecoverInterruptedSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := sides.FindSide(context.Background(), "side-thread")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != domain.SideStatusFailed || recovered.Error != "Side 运行因服务重启而中断" {
		t.Fatalf("recovered Side = %#v", recovered)
	}
}

func TestContinueSidePersistsFollowUpAndUsesLoadedThread(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["session-1"] = sideTestSession()
	sides := newFakeSideRepository()
	sides.sides["side-thread"] = domain.Side{
		ID: "side-thread", SessionID: "session-1", ProcessRunID: "side-run-1", TurnID: "turn-1",
		Prompt: "first", Status: domain.SideStatusCompleted, TurnIndex: 1,
	}
	events := make(chan processdomain.CodexEvent, 1)
	events <- sideExitEvent(1, processdomain.ExitResult{})
	close(events)
	codex := &fakeCodexProcess{loadedHandle: processdomain.CodexHandle{TurnID: "turn-2"}, events: events}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithSessionSides(sides))
	defer service.Close()
	service.generateID = func() (domain.ID, error) { return "side-run-2", nil }

	run, err := service.ContinueSide(context.Background(), SideInput{
		SessionID: "session-1", CodexSessionID: "side-thread", Prompt: "follow up",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !codex.loadedCalled || codex.resumeCalled || codex.loadedInput.CodexSessionID != "side-thread" || codex.loadedInput.PermissionMode != "read-only" {
		t.Fatalf("loaded continuation = %#v", codex.loadedInput)
	}
	if run.ProcessRunID != "side-run-2" || run.TurnID != "turn-2" || !reflect.DeepEqual(run.FollowUps, []string{"follow up"}) {
		t.Fatalf("continued Side = %#v", run)
	}
	waitForSideStatus(t, sides, "side-thread", domain.SideStatusCompleted)
}

func TestSideOverridesAndAttachmentsDoNotChangeSourceSession(t *testing.T) {
	for _, continuing := range []bool{false, true} {
		t.Run(map[bool]string{false: "start", true: "continue"}[continuing], func(t *testing.T) {
			repo := newFakeRepository()
			repo.sessions["session-1"] = sideTestSession()
			sides := newFakeSideRepository()
			if continuing {
				sides.sides["side-thread"] = domain.Side{ID: "side-thread", SessionID: "session-1", ProcessRunID: "old-run", Prompt: "first", Status: domain.SideStatusCompleted, TurnIndex: 1}
			}
			events := make(chan processdomain.CodexEvent, 1)
			events <- sideExitEvent(1, processdomain.ExitResult{})
			close(events)
			codex := &fakeCodexProcess{events: events}
			service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithSessionSides(sides))
			defer service.Close()
			input := SideInput{
				SessionID: "session-1", CodexSessionID: "side-thread", Prompt: "inspect attachments",
				Config: &SideConfigInput{CodexModel: " other-model ", ReasoningEffort: " low ", FastMode: false},
				Files: []SideFileInput{
					{Filename: "screenshot.png", MimeType: "image/png", Reader: strings.NewReader("png")},
					{Filename: "notes.txt", MimeType: "text/plain", Reader: strings.NewReader("notes")},
					{Filename: "notes.txt", Reader: strings.NewReader("")},
				},
			}
			var items []processdomain.CodexInputItem
			if continuing {
				if _, err := service.ContinueSide(context.Background(), input); err != nil {
					t.Fatal(err)
				}
				got := codex.loadedInput
				if got.Model != "other-model" || got.ReasoningEffort != "low" || got.FastMode || got.PermissionMode != "read-only" {
					t.Fatalf("continued config = %#v", got)
				}
				items = got.Input
			} else {
				if _, err := service.StartSide(context.Background(), input); err != nil {
					t.Fatal(err)
				}
				got := codex.forkInput
				if got.Model != "other-model" || got.ReasoningEffort != "low" || got.FastMode || got.PermissionMode != "read-only" {
					t.Fatalf("fork config = %#v", got)
				}
				items = got.Input
			}
			if len(items) != 4 || items[1].Type != "localImage" || string(items[1].Data) != "png" || items[2].Type != "mention" || string(items[2].Data) != "notes" || items[3].Data == nil {
				t.Fatalf("Side inputs = %#v", items)
			}
			if len(repo.saved) != 0 || repo.sessions["session-1"].Config != sideTestSession().Config {
				t.Fatal("Side mutated the source config")
			}
		})
	}
}

func TestSideAllowsAttachmentOnlyAndRejectsInvalidInputBeforeStarting(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     SideInput
		wantError bool
	}{
		{name: "attachment only", input: SideInput{Prompt: "attachment", Files: []SideFileInput{{Filename: "empty.txt", Reader: strings.NewReader("")}}}},
		{name: "empty", wantError: true},
		{name: "incomplete config", input: SideInput{Prompt: "hello", Config: &SideConfigInput{CodexModel: "test"}}, wantError: true},
		{name: "missing reader", input: SideInput{Files: []SideFileInput{{Filename: "a.txt"}}}, wantError: true},
		{name: "failed read", input: SideInput{Files: []SideFileInput{{Filename: "a.txt", Reader: failingSideReader{}}}}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepository()
			repo.sessions["session-1"] = sideTestSession()
			codex := &fakeCodexProcess{}
			service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithSessionSides(newFakeSideRepository()))
			defer service.Close()
			tc.input.SessionID = "session-1"
			_, err := service.StartSide(context.Background(), tc.input)
			if (err != nil) != tc.wantError || codex.forkCalled == tc.wantError {
				t.Fatalf("error = %v, started = %v", err, codex.forkCalled)
			}
		})
	}
}

func TestCloseSessionDeletesSides(t *testing.T) {
	repo := newFakeRepository()
	session := sideTestSession()
	session.Status = domain.StatusCreated
	session.CodexSessionID = ""
	repo.sessions[session.ID] = session
	sides := newFakeSideRepository()
	sides.sides["side-thread"] = domain.Side{
		ID: "side-thread", SessionID: session.ID, ProcessRunID: "side-run-1", Prompt: "inspect",
		Status: domain.SideStatusCompleted, TurnIndex: 1,
	}
	codex := &fakeCodexProcess{}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithSessionSides(sides))
	defer service.Close()

	closed, err := service.CloseSession(context.Background(), CloseSessionInput{SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != domain.StatusClosed || len(sides.snapshotSides()) != 0 || codex.stoppedID != "side-run-1" {
		t.Fatalf("closed Side cleanup = status:%q sides:%#v stopped:%q", closed.Status, sides.snapshotSides(), codex.stoppedID)
	}
}

func TestCloseSessionStopsRunningSideBeforeDeletingIt(t *testing.T) {
	repo := newFakeRepository()
	session := sideTestSession()
	session.Status = domain.StatusCreated
	repo.sessions[session.ID] = session
	sides := newFakeSideRepository()
	events := make(chan processdomain.CodexEvent, 1)
	var stopOnce sync.Once
	codex := &fakeCodexProcess{
		forkHandle: processdomain.CodexHandle{CodexSessionID: "side-thread", TurnID: "turn-1"},
		events:     events,
		stopHook: func(context.Context, processdomain.RunID) error {
			stopOnce.Do(func() {
				events <- sideExitEvent(1, processdomain.ExitResult{FailureCode: "turn_interrupted"})
				close(events)
			})
			return nil
		},
	}
	service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex), WithSessionSides(sides))
	defer service.Close()
	service.generateID = func() (domain.ID, error) { return "side-run-1", nil }

	if _, err := service.StartSide(context.Background(), SideInput{SessionID: session.ID, Prompt: "inspect"}); err != nil {
		t.Fatal(err)
	}
	closed, err := service.CloseSession(context.Background(), CloseSessionInput{SessionID: session.ID})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != domain.StatusClosed || len(sides.snapshotSides()) != 0 || codex.stoppedID != "side-run-1" {
		t.Fatalf("closed running Side cleanup = status:%q sides:%#v stopped:%q", closed.Status, sides.snapshotSides(), codex.stoppedID)
	}
}

type failingSideReader struct{}

func (failingSideReader) Read([]byte) (int, error) { return 0, errors.New("upload read failed") }

func sideTestSession() domain.Session {
	return domain.Session{
		ID: "session-1", ProjectID: "project-1", Mode: domain.ModeChat, Status: domain.StatusRunning,
		BaseBranch: "main", WorktreePath: "/workspace/session-1", CodexSessionID: "source-thread",
		Config: domain.Config{CodexModel: "gpt-test", ReasoningEffort: "high", PermissionMode: "workspace-write", FastMode: true},
	}
}

func sideMessageEvent(id string, sequence int64, text string) processdomain.CodexEvent {
	return processdomain.CodexEvent{
		EventID: id, Type: processdomain.CodexEventMessage, Sequence: sequence, CreatedAt: time.Unix(sequence, 0).UTC(),
		Content: processdomain.CodexMessageContent{Role: "assistant", Text: text, Format: processdomain.CodexTextMarkdown},
	}
}

func sideExitEvent(sequence int64, result processdomain.ExitResult) processdomain.CodexEvent {
	return processdomain.CodexEvent{
		EventID: "exit", Type: processdomain.CodexEventProcessExit, Sequence: sequence, CreatedAt: time.Unix(sequence, 0).UTC(), Content: result,
	}
}

func waitForSideStatus(t *testing.T, repo *fakeSideRepository, id string, status domain.SideStatus) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		side, err := repo.FindSide(context.Background(), id)
		if err == nil && side.Status == status {
			return
		}
		time.Sleep(time.Millisecond)
	}
	side, _ := repo.FindSide(context.Background(), id)
	t.Fatalf("Side status = %q, want %q", side.Status, status)
}

func waitForSideEventCount(t *testing.T, repo *fakeSideRepository, id string, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events, _ := repo.ListSideEvents(context.Background(), id)
		if len(events) == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	events, _ := repo.ListSideEvents(context.Background(), id)
	t.Fatalf("Side event count = %d, want %d", len(events), count)
}

type fakeSideRepository struct {
	mu     sync.Mutex
	sides  map[string]domain.Side
	events map[string][]domain.SideEvent
}

func newFakeSideRepository() *fakeSideRepository {
	return &fakeSideRepository{sides: map[string]domain.Side{}, events: map[string][]domain.SideEvent{}}
}

func (r *fakeSideRepository) CreateSide(_ context.Context, side domain.Side) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sides[side.ID] = cloneSide(side)
	return nil
}

func (r *fakeSideRepository) FindSide(_ context.Context, id string) (domain.Side, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	side, ok := r.sides[id]
	if !ok {
		return domain.Side{}, errors.New("side not found")
	}
	return cloneSide(side), nil
}

func (r *fakeSideRepository) FindSideByProcessRun(_ context.Context, processRunID string) (domain.Side, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, side := range r.sides {
		if side.ProcessRunID == processRunID {
			return cloneSide(side), nil
		}
	}
	return domain.Side{}, errors.New("side not found")
}

func (r *fakeSideRepository) ListSides(_ context.Context, sessionID domain.ID) ([]domain.Side, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]domain.Side, 0)
	for _, side := range r.sides {
		if side.SessionID == sessionID {
			result = append(result, cloneSide(side))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (r *fakeSideRepository) ListRunningSides(context.Context) ([]domain.Side, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]domain.Side, 0)
	for _, side := range r.sides {
		if side.Status == domain.SideStatusRunning {
			result = append(result, cloneSide(side))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (r *fakeSideRepository) BeginSideTurn(_ context.Context, id string, processRunID string, turnID string, followUp string, now time.Time) (domain.Side, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	side, ok := r.sides[id]
	if !ok {
		return domain.Side{}, errors.New("side not found")
	}
	side.ProcessRunID = processRunID
	side.TurnID = turnID
	side.FollowUps = append(side.FollowUps, followUp)
	side.Status = domain.SideStatusRunning
	side.Error = ""
	side.TurnIndex++
	side.UpdatedAt = now
	r.sides[id] = side
	return cloneSide(side), nil
}

func (r *fakeSideRepository) CompleteSideTurn(_ context.Context, id string, processRunID string, status domain.SideStatus, failure string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	side, ok := r.sides[id]
	if ok && side.ProcessRunID == processRunID && side.Status == domain.SideStatusRunning {
		side.Status = status
		side.Error = failure
		side.UpdatedAt = now
		r.sides[id] = side
	}
	return nil
}

func (r *fakeSideRepository) AppendSideEvent(_ context.Context, event domain.SideEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[event.SideID] = append(r.events[event.SideID], event)
	return nil
}

func (r *fakeSideRepository) ListSideEvents(_ context.Context, sideID string) ([]domain.SideEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.SideEvent(nil), r.events[sideID]...), nil
}

func (r *fakeSideRepository) ListSideRunEvents(_ context.Context, processRunID string) ([]domain.SideEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]domain.SideEvent, 0)
	for _, events := range r.events {
		for _, event := range events {
			if event.ProcessRunID == processRunID {
				result = append(result, event)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result, nil
}

func (r *fakeSideRepository) DeleteSide(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sides, id)
	delete(r.events, id)
	return nil
}

func (r *fakeSideRepository) DeleteSidesBySession(_ context.Context, sessionID domain.ID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, side := range r.sides {
		if side.SessionID == sessionID {
			delete(r.sides, id)
			delete(r.events, id)
		}
	}
	return nil
}

func (r *fakeSideRepository) snapshotSides() []domain.Side {
	result, _ := r.ListSides(context.Background(), "session-1")
	return result
}

func cloneSide(side domain.Side) domain.Side {
	side.FollowUps = append([]string(nil), side.FollowUps...)
	return side
}
