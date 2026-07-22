package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	domainsession "github.com/nzlov/anycode/internal/domain/session"
)

func TestPurgeSessionsDeletesOwnedHistory(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, id := range []domainsession.ID{"session-1", "session-2"} {
		if err := store.Sessions().Save(ctx, domainsession.Session{
			ID: id, ProjectID: "project-1", Mode: domainsession.ModeChat, Status: domainsession.StatusClosed,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.client.EventRecord.Create().SetID("event-1").SetSessionID("session-1").SetProjectID("project-1").SetType("session.closed").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.client.NotificationDelivery.Create().SetID("delivery-1").SetEventID("event-1").SetSubscriptionID("subscription-1").SetPayload([]byte("payload")).SetStatus("pending").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.client.QuestionBatch.Create().SetID("batch-1").SetSessionID("session-1").SetStatus("answered").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.client.NodeRun.Create().SetID("node-1").SetSessionID("session-1").SetNodeID("build").SetStatus("succeeded").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.client.ProcessRun.Create().SetID("process-1").SetSessionID("session-1").SetStatus("exited").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.client.PromptAppend.Create().SetID("append-1").SetSessionID("session-1").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.client.MergeRecord.Create().SetID("merge-1").SetSessionID("session-1").Exec(ctx); err != nil {
		t.Fatal(err)
	}

	if err := store.PurgeSessions(ctx, []domainsession.ID{"session-1"}); err != nil {
		t.Fatalf("PurgeSessions() error = %v", err)
	}
	counts := map[string]int{}
	counts["sessions"] = mustCount(t, func() (int, error) { return store.client.Session.Query().Count(ctx) })
	counts["events"] = mustCount(t, func() (int, error) { return store.client.EventRecord.Query().Count(ctx) })
	counts["deliveries"] = mustCount(t, func() (int, error) { return store.client.NotificationDelivery.Query().Count(ctx) })
	counts["questions"] = mustCount(t, func() (int, error) { return store.client.QuestionBatch.Query().Count(ctx) })
	counts["nodes"] = mustCount(t, func() (int, error) { return store.client.NodeRun.Query().Count(ctx) })
	counts["processes"] = mustCount(t, func() (int, error) { return store.client.ProcessRun.Query().Count(ctx) })
	counts["appends"] = mustCount(t, func() (int, error) { return store.client.PromptAppend.Query().Count(ctx) })
	counts["merges"] = mustCount(t, func() (int, error) { return store.client.MergeRecord.Query().Count(ctx) })
	if counts["sessions"] != 1 {
		t.Fatalf("counts = %#v", counts)
	}
	for name, count := range counts {
		if name != "sessions" && count != 0 {
			t.Fatalf("counts = %#v", counts)
		}
	}
	if _, err := store.Sessions().Find(ctx, "session-2"); err != nil {
		t.Fatalf("unrelated session was deleted: %v", err)
	}
}

func mustCount(t *testing.T, countFn func() (int, error)) int {
	t.Helper()
	count, err := countFn()
	if err != nil {
		t.Fatal(err)
	}
	return count
}
