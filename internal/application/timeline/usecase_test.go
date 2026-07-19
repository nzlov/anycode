package timeline

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

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
				EventID: "plan-1",
				Type:    process.CodexEventPlan,
				Content: process.PlanUpdate{Items: []process.PlanItem{{Step: "Implement", Status: process.PlanItemInProgress}}},
			},
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
			ID:        "artifact-1",
			Scope:     eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "artifact.published",
			Payload:   map[string]any{"id": "file-1", "filename": "result.png", "downloadUrl": "/files/file-1/download"},
			CreatedAt: time.Unix(12, 0).UTC(),
		},
		{
			ID:        "internal-1",
			Scope:     eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID},
			SessionID: &sessionID,
			Type:      "attachment.archived",
			CreatedAt: time.Unix(15, 0).UTC(),
		},
	}}
	service := New(&fakeLiveSource{}, sessions, transcript, transcriptIndex("codex-session-1"), WithHistory(history))

	got, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if gotIDs := dtoIDs(got.Items); !reflect.DeepEqual(gotIDs, []eventdomain.ID{"group:lifecycle:session", "group:artifact:session", "codex:codex-session-1:codex-event-1"}) {
		t.Fatalf("items = %#v", gotIDs)
	}
	if got.Total != 3 || got.Usage == nil || got.Usage.TotalTokens != 14 {
		t.Fatalf("page metadata = total %d, usage %#v", got.Total, got.Usage)
	}
	if got.Items[0].Group == nil || len(got.Items[0].Group.Members) != 1 {
		t.Fatalf("status group = %#v", got.Items[0])
	}
	artifact, ok := got.Items[1].Group.Members[0].Content.(process.CodexUnknownContent)
	if !ok || artifact.RawType != "artifact.published" || artifact.Payload["filename"] != "result.png" {
		t.Fatalf("artifact group = %#v", got.Items[1])
	}
	codex := got.Items[2]
	if codex.Phase != process.CodexPhaseStandalone || codex.CorrelationID != "codex:codex-session-1:call-1" || codex.OccurredAt != "1970-01-01T00:00:20Z" {
		t.Fatalf("codex event = %#v", codex)
	}
	if content, ok := codex.Content.(process.CodexFileChangeContent); !ok || len(content.Changes) != 1 || content.Changes[0].Path != "a.txt" {
		t.Fatalf("codex content = %#v", codex.Content)
	}
	if transcript.input.Source.CodexSessionID != "codex-session-1" {
		t.Fatalf("transcript input = %#v", transcript.input)
	}
}

func TestListSessionEventsGroupsRoutineEventsBeforePaginationWithoutLosingAuditEvents(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	sessions := &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {ID: "session-1", ProjectID: "project-1", UpdatedAt: time.Unix(20, 0).UTC()},
	}}
	processCausality := eventdomain.Causality{ProcessRunID: "process-1", NodeRunID: "node-1"}
	artifactCausality := eventdomain.Causality{NodeRunID: "node-1", CorrelationID: "publish-1"}
	history := &fakeEventHistory{events: []eventdomain.DomainEvent{
		{ID: "queued", Type: "session.queued", Causality: processCausality, CreatedAt: time.Unix(1, 0).UTC()},
		{ID: "starting", Type: "session.starting", Causality: processCausality, CreatedAt: time.Unix(2, 0).UTC()},
		{ID: "running", Type: "session.running", Causality: processCausality, CreatedAt: time.Unix(3, 0).UTC()},
		{ID: "todo-1", Type: "session.todo_list_updated", Causality: processCausality, CreatedAt: time.Unix(4, 0).UTC()},
		{ID: "todo-2", Type: "session.todo_list_updated", Causality: processCausality, CreatedAt: time.Unix(5, 0).UTC()},
		{ID: "artifact-1", Type: "artifact.published", Causality: artifactCausality, CreatedAt: time.Unix(6, 0).UTC()},
		{ID: "artifact-2", Type: "artifact.published", Causality: artifactCausality, CreatedAt: time.Unix(7, 0).UTC()},
		{ID: "artifact-3", Type: "artifact.published", Causality: eventdomain.Causality{NodeRunID: "node-1", CorrelationID: "publish-2"}, CreatedAt: time.Unix(7, int64(time.Millisecond)).UTC()},
		{ID: "waiting", Type: "session.waiting_user", Causality: processCausality, CreatedAt: time.Unix(8, 0).UTC()},
		{ID: "failed", Type: "session.failed", Causality: processCausality, Payload: map[string]any{"reason": "boom"}, CreatedAt: time.Unix(9, 0).UTC()},
		{ID: "failed-exit", Type: "process.exited", Causality: processCausality, Payload: map[string]any{"exitCode": float64(1)}, CreatedAt: time.Unix(10, 0).UTC()},
	}}
	for index := range history.events {
		history.events[index].Scope = eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID}
		history.events[index].SessionID = &sessionID
	}
	service := New(&fakeLiveSource{}, sessions, nil, nil, WithHistory(history))

	page, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 6 || len(page.Items) != 4 || page.NextCursor == "" {
		t.Fatalf("page = %#v", page)
	}
	all, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var auditIDs []eventdomain.ID
	for _, item := range all.Items {
		if item.Group == nil {
			auditIDs = append(auditIDs, item.ID)
			continue
		}
		for _, member := range item.Group.Members {
			auditIDs = append(auditIDs, member.ID)
		}
	}
	want := []eventdomain.ID{"queued", "starting", "running", "artifact-1", "artifact-2", "artifact-3", "waiting", "failed", "failed-exit"}
	if !reflect.DeepEqual(auditIDs, want) {
		t.Fatalf("expanded ids = %#v, want %#v", auditIDs, want)
	}
	if all.Items[3].ID != "waiting" || all.Items[4].ID != "failed" || all.Items[5].ID != "failed-exit" {
		t.Fatalf("waiting/error events were grouped: %#v", dtoIDs(all.Items))
	}
}

func TestListSessionEventsHidesCardUpdatesAndMirroredWorkflowWait(t *testing.T) {
	sessionID := eventdomain.SessionID("session-1")
	sessions := &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {ID: "session-1", ProjectID: "project-1", UpdatedAt: time.Unix(20, 0).UTC()},
	}}
	events := []eventdomain.DomainEvent{
		{ID: "status", Type: "session.status_updated"},
		{ID: "todo", Type: "session.todo_list_updated"},
		{ID: "diff", Type: "session.diff_changed"},
		{ID: "artifacts", Type: "session.artifacts_updated"},
		{ID: "priority", Type: "session.priority_changed"},
		{ID: "config", Type: "session.config_changed"},
		{ID: "cleanup", Type: "session.worktree_cleanup_completed"},
		{ID: "cleanup-failed", Type: "session.worktree_cleanup_failed"},
		{ID: "workflow-wait", Type: "workflow.waiting_approval"},
		{ID: "session-wait", Type: "session.waiting_approval"},
	}
	for index := range events {
		events[index].Scope = eventdomain.Scope{ProjectID: "project-1", SessionID: &sessionID}
		events[index].SessionID = &sessionID
		events[index].CreatedAt = time.Unix(int64(index+1), 0).UTC()
	}
	service := New(
		&fakeLiveSource{},
		sessions,
		nil,
		nil,
		WithHistory(&fakeEventHistory{events: events}),
	)

	page, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{
		SessionID: "session-1",
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if got := dtoIDs(page.Items); !reflect.DeepEqual(got, []eventdomain.ID{"cleanup-failed", "session-wait"}) {
		t.Fatalf("visible status events = %#v", got)
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
	index := transcriptIndex("codex-session-1", "codex-session-2")
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
		{Source: transcriptSource("codex-session-1")},
		{Source: transcriptSource("codex-session-2")},
		{Source: transcriptSource("codex-session-1")},
		{Source: transcriptSource("codex-session-2")},
	}
	if !reflect.DeepEqual(transcript.inputs, wantInputs) {
		t.Fatalf("transcript inputs = %#v", transcript.inputs)
	}
	if index.input != process.SessionID(sessionID) {
		t.Fatalf("index input = %q", index.input)
	}
}

func TestListSessionEventsProjectsCurrentAndCumulativeUsageWithCompactionCount(t *testing.T) {
	sessions := &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {ID: "session-1", ProjectID: "project-1", UpdatedAt: time.Unix(3, 0).UTC()},
	}}
	transcript := &fakeTranscriptSource{events: []process.CodexEvent{
		{Type: "token_count", Content: process.CodexUsageContent{
			InputTokens: 46_200_000, CachedInputTokens: 45_000_000, OutputTokens: 200_000, TotalTokens: 46_400_000,
			CurrentInputTokens: 314_000, CurrentCachedInputTokens: 300_000, CurrentOutputTokens: 2_000, CurrentTotalTokens: 316_000, ContextWindow: 353_000,
		}},
		{EventID: "compact-1", Type: "context.compacted", Content: process.CodexStatusContent{Code: "context.compacted", Level: "warning"}, CreatedAt: time.Unix(2, 0).UTC()},
	}}
	service := New(&fakeLiveSource{}, sessions, transcript, transcriptIndex("codex-session-1"))

	page, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Usage == nil || page.Usage.InputTokens != 46_200_000 || page.Usage.CurrentInputTokens != 314_000 || page.Usage.CurrentCachedInputTokens != 300_000 || page.Usage.ContextWindow != 353_000 || page.Usage.CompactionCount != 1 {
		t.Fatalf("usage = %#v", page.Usage)
	}
}

func TestListSessionEventsSelectsLatestUsageByTimestampAcrossSources(t *testing.T) {
	sessions := &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {ID: "session-1", ProjectID: "project-1", UpdatedAt: time.Unix(30, 0).UTC()},
	}}
	transcript := &fakeTranscriptSource{eventsByID: map[string][]process.CodexEvent{
		"codex-session-1": {{Type: "token_count", Content: process.CodexUsageContent{InputTokens: 200}, CreatedAt: time.Unix(20, 0).UTC()}},
		"codex-session-2": {{Type: "token_count", Content: process.CodexUsageContent{InputTokens: 100}, CreatedAt: time.Unix(10, 0).UTC()}},
	}}
	service := New(&fakeLiveSource{}, sessions, transcript, transcriptIndex("codex-session-1", "codex-session-2"))
	page, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Usage == nil || page.Usage.InputTokens != 200 {
		t.Fatalf("latest usage = %#v", page.Usage)
	}
}

func TestListSessionEventsAttributesNonNegativeUsageDeltasToProcessAndNodeRuns(t *testing.T) {
	finishedOne := time.Unix(20, 0).UTC()
	finishedTwo := time.Unix(30, 0).UTC()
	index := transcriptIndex("codex-session-1")
	index.runs = []process.Run{
		{ID: "process-1", SessionID: "session-1", NodeRunID: nodeRunID("node-1"), CodexSessionID: "codex-session-1", StartedAt: time.Unix(0, 0).UTC(), FinishedAt: &finishedOne},
		{ID: "process-2", SessionID: "session-1", NodeRunID: nodeRunID("node-2"), CodexSessionID: "codex-session-1", StartedAt: finishedOne, FinishedAt: &finishedTwo},
	}
	transcript := &fakeTranscriptSource{events: []process.CodexEvent{
		{Type: "token_count", Content: process.CodexUsageContent{InputTokens: 100, TotalTokens: 110}, CreatedAt: time.Unix(5, 0).UTC()},
		{Type: "token_count", Content: process.CodexUsageContent{InputTokens: 160, TotalTokens: 180}, CreatedAt: time.Unix(15, 0).UTC()},
		{EventID: "compact-boundary", Type: "context.compacted", Content: process.CodexStatusContent{Code: "context.compacted", Level: "warning"}, CreatedAt: finishedOne},
		{Type: "token_count", Content: process.CodexUsageContent{InputTokens: 250, TotalTokens: 280}, CreatedAt: time.Unix(25, 0).UTC()},
	}}
	service := New(&fakeLiveSource{}, &fakeSessionRepository{sessions: map[sessiondomain.ID]sessiondomain.Session{
		"session-1": {ID: "session-1", ProjectID: "project-1", UpdatedAt: finishedTwo},
	}}, transcript, index)

	page, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.ProcessUsage) != 2 || page.ProcessUsage[0].Usage.InputTokens != 160 || page.ProcessUsage[1].Usage.InputTokens != 90 || len(page.NodeUsage) != 2 || page.NodeUsage[1].Usage.TotalTokens != 100 {
		t.Fatalf("process usage = %#v node usage = %#v", page.ProcessUsage, page.NodeUsage)
	}
	if page.ProcessUsage[0].Usage.CompactionCount != 1 || page.ProcessUsage[1].Usage.CompactionCount != 0 {
		t.Fatalf("boundary compactions = %#v", page.ProcessUsage)
	}
}

func TestCumulativeUsageBeforeMissingStartUsesZeroBaseline(t *testing.T) {
	samples := []usageSample{{at: time.Unix(1, 0).UTC(), usage: process.CodexUsageContent{InputTokens: 10}}}
	if got := cumulativeUsageBefore(samples, time.Time{}); got.InputTokens != 0 {
		t.Fatalf("zero-time baseline = %#v", got)
	}
}

func nodeRunID(id string) *process.NodeRunID {
	value := process.NodeRunID(id)
	return &value
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
	service := New(&fakeLiveSource{}, sessions, transcript, transcriptIndex("codex-session-1"))

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
	service := New(&fakeLiveSource{}, sessions, transcript, transcriptIndex("z-old", "a-new"))

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
	service := New(&fakeLiveSource{}, sessions, &fakeTranscriptSource{events: events}, transcriptIndex("codex-session-1"))

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
	live := &fakeLiveSource{ch: make(chan process.CodexEvent, 1)}
	service := New(live, sessions, transcript, transcriptIndex("codex-session-1"))
	history, err := service.ListSessionEvents(context.Background(), ListSessionEventsInput{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := service.SessionEvents(ctx, SessionEventsInput{SessionID: sessiondomain.ID(sessionID)})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	live.ch <- process.CodexEvent{
		EventID:        "event-1",
		Type:           process.CodexEventTool,
		SessionID:      process.SessionID(sessionID),
		CodexSessionID: "codex-session-1",
		CorrelationID:  "call-1",
		Phase:          process.CodexPhaseCompleted,
		Content:        content,
		SourceOffset:   42,
		SourceIndex:    1,
		CreatedAt:      createdAt,
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
	service := New(&fakeLiveSource{}, sessions, transcript, transcriptIndex("codex-session-1"))

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
	liveSource := &fakeLiveSource{ch: make(chan process.CodexEvent, 2)}
	transcript := &fakeTranscriptSource{err: context.Canceled}
	service := New(liveSource, nil, transcript, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.SessionEvents(ctx, SessionEventsInput{SessionID: sessiondomain.ID(sessionID)})
	if err != nil {
		t.Fatalf("SessionEvents() error = %v", err)
	}
	liveSource.ch <- process.CodexEvent{
		EventID:        "event-1",
		Type:           process.CodexEventMessage,
		SessionID:      process.SessionID(sessionID),
		CodexSessionID: "codex-session-1",
		Content:        process.CodexMessageContent{Role: "assistant", Text: "first"},
		CreatedAt:      time.Unix(1, 0).UTC(),
	}
	liveSource.ch <- process.CodexEvent{
		EventID:        "event-2",
		Type:           process.CodexEventTool,
		SessionID:      process.SessionID(sessionID),
		CodexSessionID: "codex-session-1",
		CorrelationID:  "call-1",
		Phase:          process.CodexPhaseCompleted,
		Content:        process.CodexToolContent{Output: process.CodexStructuredText{Format: process.CodexTextPlain, Text: "ok"}},
		CreatedAt:      time.Unix(2, 0).UTC(),
	}
	if got := <-ch; got.ID != "codex:codex-session-1:event-1" {
		t.Fatalf("first event = %#v", got)
	}
	if got := <-ch; got.ID != "codex:codex-session-1:event-2" || got.CorrelationID != "codex:codex-session-1:call-1" || got.Phase != process.CodexPhaseCompleted {
		t.Fatalf("codex event = %#v", got)
	}
	if len(transcript.inputs) != 0 {
		t.Fatalf("subscription read transcript: %#v", transcript.inputs)
	}
}

func TestSessionEventsKeepsPlanUpdatesOutOfTranscript(t *testing.T) {
	liveSource := &fakeLiveSource{ch: make(chan process.CodexEvent, 1)}
	service := New(liveSource, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.SessionEvents(ctx, SessionEventsInput{SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	liveSource.ch <- process.CodexEvent{
		EventID: "plan-1", Type: process.CodexEventPlan,
		Content: process.PlanUpdate{Items: []process.PlanItem{{Step: "Implement", Status: process.PlanItemInProgress}}},
	}
	select {
	case got := <-ch:
		t.Fatalf("plan update leaked into transcript: %#v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSessionEventsKeepsUsageOutOfTranscript(t *testing.T) {
	liveSource := &fakeLiveSource{ch: make(chan process.CodexEvent, 2)}
	service := New(liveSource, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.SessionEvents(ctx, SessionEventsInput{SessionID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	liveSource.ch <- process.CodexEvent{
		EventID: "usage-1", Type: process.CodexEventUsage, SessionID: "session-1",
		Content: process.CodexUsageContent{InputTokens: 10},
	}
	liveSource.ch <- process.CodexEvent{
		EventID: "message-1", Type: process.CodexEventMessage, SessionID: "session-1",
		Content: process.CodexMessageContent{Role: "assistant", Text: "visible"},
	}
	if got := <-ch; got.ID != "message-1" || got.Type != process.CodexEventMessage {
		t.Fatalf("transcript event = %#v", got)
	}
}

func TestSessionUsageEventsMapsUsageAcrossSessions(t *testing.T) {
	liveSource := &fakeLiveSource{usageCh: make(chan process.CodexEvent, 2)}
	service := New(liveSource, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := service.SessionUsageEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	liveSource.usageCh <- process.CodexEvent{
		EventID: "usage-1", Type: process.CodexEventUsage, SessionID: "session-1",
		Content:   process.CodexUsageContent{InputTokens: 10, TotalTokens: 12},
		CreatedAt: time.Unix(3, 0).UTC(),
	}
	got := <-ch
	if got.SessionID != "session-1" || got.Usage.InputTokens != 10 || got.Usage.TotalTokens != 12 || got.OccurredAt != "1970-01-01T00:00:03Z" {
		t.Fatalf("usage update = %#v", got)
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
	ch      chan process.CodexEvent
	usageCh chan process.CodexEvent
	input   process.SessionID
	done    <-chan struct{}
}

func (s *fakeLiveSource) LiveCodexEvents(ctx context.Context, sessionID process.SessionID) (<-chan process.CodexEvent, error) {
	s.input = sessionID
	s.done = ctx.Done()
	if s.ch == nil {
		s.ch = make(chan process.CodexEvent, 1)
	}
	return s.ch, nil
}

func (s *fakeLiveSource) LiveCodexUsageEvents(ctx context.Context) (<-chan process.CodexEvent, error) {
	s.done = ctx.Done()
	if s.usageCh == nil {
		s.usageCh = make(chan process.CodexEvent, 1)
	}
	return s.usageCh, nil
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
		return s.eventsByID[input.Source.CodexSessionID], s.err
	}
	return s.events, s.err
}

type fakeCodexSessionIndex struct {
	input   process.SessionID
	sources []process.CodexTranscriptSource
	runs    []process.Run
	err     error
}

func transcriptSource(id string) process.CodexTranscriptSource {
	return process.CodexTranscriptSource{CodexSessionID: id, RelativePath: id + ".jsonl"}
}

func transcriptIndex(ids ...string) *fakeCodexSessionIndex {
	sources := make([]process.CodexTranscriptSource, 0, len(ids))
	for _, id := range ids {
		sources = append(sources, transcriptSource(id))
	}
	return &fakeCodexSessionIndex{sources: sources}
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

func (i *fakeCodexSessionIndex) TranscriptSources(_ context.Context, sessionID process.SessionID) ([]process.CodexTranscriptSource, error) {
	i.input = sessionID
	return i.sources, i.err
}

func (i *fakeCodexSessionIndex) TranscriptRuns(_ context.Context, sessionID process.SessionID) ([]process.Run, error) {
	i.input = sessionID
	return i.runs, i.err
}
