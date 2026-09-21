package entstore

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
)

func TestSideRepositoryPersistsTurnsEventsAndDeletesWithSession(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	if err := store.Sessions().Save(ctx, session.Session{
		ID: "session-1", ProjectID: "project-1", Mode: session.ModeChat, Status: session.StatusRunning,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	repo := store.SessionSides()
	side := session.Side{
		ID: "side-1", SessionID: "session-1", ProcessRunID: "run-1", TurnID: "turn-1",
		Prompt: "inspect", FollowUps: []string{}, Status: session.SideStatusRunning, TurnIndex: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateSide(ctx, side); err != nil {
		t.Fatal(err)
	}
	running, err := repo.ListRunningSides(ctx)
	if err != nil || len(running) != 1 || running[0].ID != side.ID {
		t.Fatalf("running Sides = %#v, %v", running, err)
	}
	event := session.SideEvent{
		ID: "run-1:1", SideID: side.ID, SessionID: side.SessionID, ProcessRunID: side.ProcessRunID,
		EventID: "message-1", Type: "message", Phase: "standalone", ContentKind: "message",
		Content:   map[string]any{"Role": "assistant", "Text": "answer", "Format": "markdown", "Images": []any{}},
		TurnIndex: 1, Sequence: 1, CreatedAt: now.Add(time.Second),
	}
	if err := repo.AppendSideEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	continued, err := repo.BeginSideTurn(ctx, side.ID, "run-2", "turn-2", "follow up", now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if continued.ProcessRunID != "run-2" || continued.TurnIndex != 2 || !reflect.DeepEqual(continued.FollowUps, []string{"follow up"}) {
		t.Fatalf("continued Side = %#v", continued)
	}
	if err := repo.CompleteSideTurn(ctx, side.ID, "run-2", session.SideStatusCompleted, "", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	listed, err := repo.ListSides(ctx, side.SessionID)
	if err != nil || len(listed) != 1 || listed[0].Status != session.SideStatusCompleted {
		t.Fatalf("listed Sides = %#v, %v", listed, err)
	}
	events, err := repo.ListSideEvents(ctx, side.ID)
	if err != nil || len(events) != 1 || events[0].Content["Text"] != "answer" {
		t.Fatalf("listed Side events = %#v, %v", events, err)
	}
	if err := repo.DeleteSidesBySession(ctx, side.SessionID); err != nil {
		t.Fatal(err)
	}
	if listed, err := repo.ListSides(ctx, side.SessionID); err != nil || len(listed) != 0 {
		t.Fatalf("Sides after delete = %#v, %v", listed, err)
	}
	if events, err := repo.ListSideEvents(ctx, side.ID); err != nil || len(events) != 0 {
		t.Fatalf("Side events after delete = %#v, %v", events, err)
	}
}
