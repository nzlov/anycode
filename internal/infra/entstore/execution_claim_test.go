package entstore

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/session"
)

func TestClaimExecutionConcurrentOnlyOneClaimed(t *testing.T) {
	ctx := context.Background()
	databaseURL := filepath.Join(t.TempDir(), "anycode.db")
	store, err := Open(ctx, OpenOptions{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	contender, err := Open(ctx, OpenOptions{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("open contender store: %v", err)
	}
	defer contender.Close()

	queuedAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	queued := session.Session{
		ID: "session-1", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusQueued,
		QueuedAt: &queuedAt, Queue: session.QueueIntent{Kind: session.QueueKindResume, Priority: session.QueuePriorityMedium, Prompt: "continue"},
		CreatedAt: queuedAt, UpdatedAt: queuedAt,
	}
	if err := store.Sessions().Save(ctx, queued); err != nil {
		t.Fatalf("save queued session: %v", err)
	}
	expected, err := store.Sessions().Find(ctx, queued.ID)
	if err != nil {
		t.Fatalf("find queued session: %v", err)
	}
	starting := expected
	if err := starting.TransitionTo(session.StatusStarting, queuedAt.Add(time.Second)); err != nil {
		t.Fatalf("transition starting: %v", err)
	}

	start := make(chan struct{})
	results := make(chan port.ExecutionClaimResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for index, runID := range []process.RunID{"process-1", "process-2"} {
		wg.Add(1)
		go func(index int, runID process.RunID) {
			defer wg.Done()
			<-start
			claimStore := store
			if index == 1 {
				claimStore = contender
			}
			result, err := claimStore.ClaimExecution(ctx, port.ExecutionClaimInput{
				ExpectedSession: expected,
				StartingSession: starting,
				Run: process.Run{
					ID: runID, SessionID: process.SessionID(expected.ID), Status: process.StatusStarting, StartedAt: queuedAt.Add(time.Second),
				},
				MaxActive: 1,
			})
			results <- result
			errs <- err
		}(index, runID)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ClaimExecution() error = %v", err)
		}
	}
	counts := map[port.ExecutionClaimStatus]int{}
	for result := range results {
		counts[result.Status]++
	}
	if counts[port.ExecutionClaimed] != 1 || counts[port.ExecutionAlreadyActive] != 1 {
		t.Fatalf("claim results = %#v", counts)
	}
	active, found, err := store.Processes().FindActiveBySession(ctx, process.SessionID(expected.ID))
	if err != nil || !found || active.ID == "" {
		t.Fatalf("active run = %#v, %v, %v", active, found, err)
	}
	current, err := store.Sessions().Find(ctx, expected.ID)
	if err != nil {
		t.Fatalf("find claimed session: %v", err)
	}
	if current.Status != session.StatusStarting || current.Queue.Kind != "" || current.Queue.Prompt != "" || current.QueuedAt != nil {
		t.Fatalf("claimed session = %#v", current)
	}
}

func TestClaimExecutionRejectsStaleQueue(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	now := time.Date(2026, 7, 14, 11, 0, 0, 0, time.UTC)
	queued := session.Session{
		ID: "session-1", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusQueued,
		Queue:    session.QueueIntent{Kind: session.QueueKindStart, Priority: session.QueuePriorityMedium, Prompt: "old"},
		QueuedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Sessions().Save(ctx, queued); err != nil {
		t.Fatalf("save queued session: %v", err)
	}
	expected, _ := store.Sessions().Find(ctx, queued.ID)
	changed := expected
	changed.Queue.Prompt = "new"
	changed.UpdatedAt = now.Add(time.Second)
	if err := store.Sessions().Save(ctx, changed); err != nil {
		t.Fatalf("save changed queue: %v", err)
	}
	starting := expected
	if err := starting.TransitionTo(session.StatusStarting, now.Add(2*time.Second)); err != nil {
		t.Fatalf("transition starting: %v", err)
	}

	result, err := store.ClaimExecution(ctx, port.ExecutionClaimInput{
		ExpectedSession: expected,
		StartingSession: starting,
		Run:             process.Run{ID: "process-1", SessionID: "session-1", Status: process.StatusStarting, StartedAt: now.Add(2 * time.Second)},
	})
	if err != nil {
		t.Fatalf("ClaimExecution() error = %v", err)
	}
	if result.Status != port.ExecutionStale || result.Session.Queue.Prompt != "new" {
		t.Fatalf("ClaimExecution() = %#v", result)
	}
	if _, found, err := store.Processes().FindActiveBySession(ctx, "session-1"); err != nil || found {
		t.Fatalf("stale claim active run = %v, %v", found, err)
	}
}

func TestClaimExecutionReturnsAtCapacityWithoutCreatingRun(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	activeSession := session.Session{ID: "active", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusRunning, CreatedAt: now, UpdatedAt: now}
	if err := store.Sessions().Save(ctx, activeSession); err != nil {
		t.Fatalf("save active session: %v", err)
	}
	if err := store.Processes().CreateRun(ctx, process.Run{ID: "active-run", SessionID: "active", Status: process.StatusRunning, StartedAt: now}); err != nil {
		t.Fatalf("save active run: %v", err)
	}
	queuedAt := now.Add(time.Second)
	queued := session.Session{
		ID: "queued", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusQueued,
		Queue:    session.QueueIntent{Kind: session.QueueKindStart, Priority: session.QueuePriorityMedium, Prompt: "start"},
		QueuedAt: &queuedAt, CreatedAt: queuedAt, UpdatedAt: queuedAt,
	}
	if err := store.Sessions().Save(ctx, queued); err != nil {
		t.Fatalf("save queued session: %v", err)
	}
	expected, _ := store.Sessions().Find(ctx, queued.ID)
	starting := expected
	if err := starting.TransitionTo(session.StatusStarting, now.Add(2*time.Second)); err != nil {
		t.Fatalf("transition starting: %v", err)
	}

	result, err := store.ClaimExecution(ctx, port.ExecutionClaimInput{
		ExpectedSession: expected,
		StartingSession: starting,
		Run:             process.Run{ID: "queued-run", SessionID: "queued", Status: process.StatusStarting, StartedAt: now.Add(2 * time.Second)},
		MaxActive:       1,
	})
	if err != nil {
		t.Fatalf("ClaimExecution() error = %v", err)
	}
	if result.Status != port.ExecutionAtCapacity {
		t.Fatalf("ClaimExecution() = %#v", result)
	}
	if _, found, err := store.Processes().FindActiveBySession(ctx, "queued"); err != nil || found {
		t.Fatalf("capacity claim active run = %v, %v", found, err)
	}
}

func TestClaimExecutionConcurrentDifferentSessionsHonorsCapacity(t *testing.T) {
	ctx := context.Background()
	databaseURL := filepath.Join(t.TempDir(), "anycode.db")
	store, err := Open(ctx, OpenOptions{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	contender, err := Open(ctx, OpenOptions{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("open contender store: %v", err)
	}
	defer contender.Close()

	now := time.Date(2026, 7, 14, 13, 30, 0, 0, time.UTC)
	expected := make([]session.Session, 2)
	starting := make([]session.Session, 2)
	for index, sessionID := range []session.ID{"session-1", "session-2"} {
		queuedAt := now.Add(time.Duration(index) * time.Second)
		queued := session.Session{
			ID: sessionID, ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusQueued,
			Queue:    session.QueueIntent{Kind: session.QueueKindStart, Priority: session.QueuePriorityMedium, Prompt: string(sessionID)},
			QueuedAt: &queuedAt, CreatedAt: queuedAt, UpdatedAt: queuedAt,
		}
		if err := store.Sessions().Save(ctx, queued); err != nil {
			t.Fatalf("save queued session: %v", err)
		}
		expected[index], err = store.Sessions().Find(ctx, sessionID)
		if err != nil {
			t.Fatalf("find queued session: %v", err)
		}
		starting[index] = expected[index]
		if err := starting[index].TransitionTo(session.StatusStarting, now.Add(2*time.Second)); err != nil {
			t.Fatalf("transition starting: %v", err)
		}
	}

	start := make(chan struct{})
	results := make(chan port.ExecutionClaimResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	stores := []*Store{store, contender}
	runIDs := []process.RunID{"process-1", "process-2"}
	for index := range stores {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			result, err := stores[index].ClaimExecution(ctx, port.ExecutionClaimInput{
				ExpectedSession: expected[index],
				StartingSession: starting[index],
				Run: process.Run{
					ID: runIDs[index], SessionID: process.SessionID(expected[index].ID),
					Status: process.StatusStarting, StartedAt: now.Add(2 * time.Second),
				},
				MaxActive: 1,
			})
			results <- result
			errs <- err
		}(index)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ClaimExecution() error = %v", err)
		}
	}
	counts := map[port.ExecutionClaimStatus]int{}
	for result := range results {
		counts[result.Status]++
	}
	if counts[port.ExecutionClaimed] != 1 || counts[port.ExecutionAtCapacity] != 1 {
		t.Fatalf("claim results = %#v", counts)
	}
	active, err := store.Processes().CountActive(ctx)
	if err != nil {
		t.Fatalf("CountActive() error = %v", err)
	}
	if active != 1 {
		t.Fatalf("active process count = %d, want 1", active)
	}
}
