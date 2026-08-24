package mindmap

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/mindmap"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
)

func TestAsyncQueueUsesIndependentGlobalConcurrencyAndConfiguredModel(t *testing.T) {
	ctx := context.Background()
	store := openMindMapTestStore(t)
	settings := &testMindMapSettings{configuration: settingdomain.MindMapConfiguration{
		Enabled: false, Mode: settingdomain.MindMapModeAsync, Model: "mind-map-model", ReasoningEffort: "xhigh", MaxConcurrent: 2,
	}}
	project, first := saveMindMapTestProjectAndSession(t, store, "session-1")
	first.CodexSessionID = "existing-thread"
	if err := store.Sessions().Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	sessions := []sessiondomain.Session{first}
	for _, id := range []sessiondomain.ID{"session-2", "session-3"} {
		closedAt := now
		session := sessiondomain.Session{
			ID: id, ProjectID: sessiondomain.ProjectID(project.ID), Requirement: "异步整理 " + string(id),
			Mode: sessiondomain.ModeChat, Status: sessiondomain.StatusClosed, ClosedAt: &closedAt,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := store.Sessions().Save(ctx, session); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, session)
	}
	for index, session := range sessions {
		createdAt := now.Add(time.Duration(index) * time.Second)
		if err := store.MindMaps().SaveTask(ctx, domain.Task{
			ID: domain.TaskID("task-" + string(rune('1'+index))), ProjectID: domain.ProjectID(project.ID), SessionID: domain.SessionID(session.ID),
			Status: domain.TaskQueued, CreatedAt: createdAt, UpdatedAt: createdAt,
		}); err != nil {
			t.Fatal(err)
		}
	}
	codex := newBlockingCodex()
	codex.resumeErr = processdomain.ErrThreadUnavailable
	queue := NewQueue(store.MindMaps(), store, store.Sessions(), store.Projects(), settings, store.Processes(), codex)
	queue.Start()
	t.Cleanup(queue.Close)

	time.Sleep(50 * time.Millisecond)
	if running, err := store.MindMaps().CountRunningTasks(ctx); err != nil || running != 0 {
		t.Fatalf("disabled queue running = %d, err = %v", running, err)
	}

	settings.configuration.Enabled = true
	queue.Schedule()
	waitForMindMapCondition(t, func() bool {
		running, _ := store.MindMaps().CountRunningTasks(ctx)
		return running == 2 && codex.startCount() == 2
	})
	queued, err := store.MindMaps().ListQueuedTasks(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 {
		t.Fatalf("queued tasks = %#v", queued)
	}
	for _, input := range codex.inputs() {
		if input.Model != "mind-map-model" || input.ReasoningEffort != "xhigh" || input.PermissionMode != "read-only" {
			t.Fatalf("start input = %#v", input)
		}
		if !strings.Contains(input.DeveloperInstructions, "禁止创建仅用于记录错误") {
			t.Fatalf("developer instructions = %q", input.DeveloperInstructions)
		}
		if !strings.Contains(input.DeveloperInstructions, "禁止将文件列表更新到节点内容里") {
			t.Fatalf("developer instructions allow file lists in node content = %q", input.DeveloperInstructions)
		}
		if !strings.Contains(input.DeveloperInstructions, "完整的 Tag 名称列表") || !strings.Contains(input.DeveloperInstructions, "清理孤儿 tag") {
			t.Fatalf("developer instructions lack managed tag or code location rules = %q", input.DeveloperInstructions)
		}
		if len(input.DynamicTools) != 3 || input.DynamicTools[0] != processdomain.DynamicToolMindMapSearch || input.DynamicTools[1] != processdomain.DynamicToolMindMapTags || input.DynamicTools[2] != processdomain.DynamicToolMindMapUpdate {
			t.Fatalf("dynamic tools = %#v", input.DynamicTools)
		}
	}
	if codex.resumeAttemptCount() != 1 {
		t.Fatalf("resume attempts = %d", codex.resumeAttemptCount())
	}
}

type blockingCodex struct {
	mu        sync.Mutex
	channels  map[processdomain.RunID]chan processdomain.CodexEvent
	starts    []processdomain.CodexStartInput
	resumeErr error
	resumes   int
}

func newBlockingCodex() *blockingCodex {
	return &blockingCodex{channels: make(map[processdomain.RunID]chan processdomain.CodexEvent)}
}

func (c *blockingCodex) Probe(context.Context) (processdomain.CodexCapabilities, error) {
	return processdomain.CodexCapabilities{}, nil
}

func (c *blockingCodex) Start(_ context.Context, input processdomain.CodexStartInput) (processdomain.CodexHandle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts = append(c.starts, input)
	c.channels[input.ProcessRunID] = make(chan processdomain.CodexEvent)
	return processdomain.CodexHandle{ProcessRunID: input.ProcessRunID, CodexSessionID: "thread-" + string(input.ProcessRunID)}, nil
}

func (c *blockingCodex) Resume(ctx context.Context, input processdomain.CodexResumeInput) (processdomain.CodexHandle, error) {
	c.mu.Lock()
	c.resumes++
	err := c.resumeErr
	c.mu.Unlock()
	if err != nil {
		return processdomain.CodexHandle{}, err
	}
	return c.Start(ctx, processdomain.CodexStartInput{
		ProcessRunID: input.ProcessRunID, SessionID: input.SessionID, Workdir: input.Workdir, Input: input.Input,
		Action: input.Action, DeveloperInstructions: input.DeveloperInstructions, Model: input.Model,
		ReasoningEffort: input.ReasoningEffort, PermissionMode: input.PermissionMode, DynamicTools: input.DynamicTools,
	})
}

func (c *blockingCodex) Fork(ctx context.Context, input processdomain.CodexForkInput) (processdomain.CodexHandle, error) {
	return c.Start(ctx, input.CodexStartInput)
}

func (c *blockingCodex) resumeAttemptCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resumes
}

func (c *blockingCodex) Steer(context.Context, processdomain.CodexSteerInput) error { return nil }

func (c *blockingCodex) Stop(_ context.Context, runID processdomain.RunID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	events, ok := c.channels[runID]
	if !ok {
		return processdomain.ErrProcessNotFound
	}
	close(events)
	delete(c.channels, runID)
	return nil
}

func (c *blockingCodex) Events(_ context.Context, handle processdomain.CodexHandle) (<-chan processdomain.CodexEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	events, ok := c.channels[handle.ProcessRunID]
	if !ok {
		return nil, errors.New("events not found")
	}
	return events, nil
}

func (c *blockingCodex) startCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.starts)
}

func (c *blockingCodex) inputs() []processdomain.CodexStartInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]processdomain.CodexStartInput(nil), c.starts...)
}

func waitForMindMapCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for mind map queue")
}

var _ processdomain.CodexProcess = (*blockingCodex)(nil)
