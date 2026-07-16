package codexcli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

var ErrProcessNotFound = process.ErrProcessNotFound

const (
	processRunOwnerEnv    = "ANYCODE_PROCESS_RUN_ID"
	artifactDirEnv        = "ANYCODE_ARTIFACT_DIR"
	mcpToolTimeoutSeconds = 86400
)

type detachedProcessOps struct {
	groupAlive   func(int) (bool, error)
	groupOwnedBy func(int, process.RunID) (bool, error)
	signalGroup  func(int, syscall.Signal) error
	waitExit     func(context.Context, int, time.Duration) (bool, error)
}

func defaultDetachedProcessOps() detachedProcessOps {
	return detachedProcessOps{
		groupAlive:   detachedProcessGroupAlive,
		groupOwnedBy: detachedProcessGroupOwnedBy,
		signalGroup: func(processGroupID int, signal syscall.Signal) error {
			return syscall.Kill(-processGroupID, signal)
		},
		waitExit: waitForDetachedProcessGroupExit,
	}
}

type activeProcess struct {
	cmd                    *exec.Cmd
	stderr                 *bytes.Buffer
	home                   string
	workdir                string
	codexSessionID         string
	transcriptPath         string
	transcriptRelativePath string
	baselineOffset         int64
	stdoutSessionIDs       <-chan string
	stdoutPlanUpdates      <-chan process.CodexEvent
	done                   chan struct{}
	mu                     sync.Mutex
	exitResult             process.ExitResult
	eventsStarted          bool
	observer               Observer
}

var processRegistry sync.Map

func (c *Client) Start(ctx context.Context, input process.CodexStartInput) (process.CodexHandle, error) {
	args := c.buildStartArgs(input)
	return c.start(ctx, input.ProcessRunID, args, input.Prompt, input.Workdir, input.ArtifactDir, "", process.CodexTranscriptSource{})
}

func (c *Client) Resume(ctx context.Context, input process.CodexResumeInput) (process.CodexHandle, error) {
	args := c.buildResumeArgs(input)
	return c.start(ctx, input.ProcessRunID, args, input.Prompt, input.Workdir, input.ArtifactDir, input.CodexSessionID, input.Transcript)
}

func (c *Client) start(ctx context.Context, runID process.RunID, args []string, prompt string, workdir string, artifactDir string, codexSessionID string, source process.CodexTranscriptSource) (process.CodexHandle, error) {
	if runID == "" {
		return process.CodexHandle{}, errors.New("process run id is required")
	}
	if err := ctx.Err(); err != nil {
		return process.CodexHandle{}, err
	}
	codexHome := c.CodexHome()
	transcriptPath := ""
	transcriptRelativePath := ""
	baselineOffset := int64(0)
	if codexSessionID != "" {
		if source.CodexSessionID != codexSessionID {
			return process.CodexHandle{}, fmt.Errorf("%w: transcript source does not match resume session", process.ErrTranscriptUnavailable)
		}
		var err error
		transcriptPath, transcriptRelativePath, err = resolveTranscriptPath(codexHome, source)
		if err != nil {
			return process.CodexHandle{}, err
		}
		info, err := os.Stat(transcriptPath)
		if err != nil {
			return process.CodexHandle{}, fmt.Errorf("%w: open resume transcript", process.ErrTranscriptUnavailable)
		}
		baselineOffset = info.Size()
	}
	cmd := exec.CommandContext(context.Background(), c.Bin(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if prompt != "" {
		cmd.Stdin = strings.NewReader(prompt)
	}
	env := os.Environ()
	if codexHome != "" {
		env = upsertEnv(env, "CODEX_HOME", codexHome)
	}
	if c.mcpAuthToken != "" {
		env = upsertEnv(env, "ANYCODE_MCP_TOKEN", c.mcpAuthToken)
	}
	env = upsertEnv(env, processRunOwnerEnv, string(runID))
	if artifactDir != "" {
		env = upsertEnv(env, artifactDirEnv, artifactDir)
	}
	cmd.Env = env
	if workdir != "" {
		cmd.Dir = workdir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return process.CodexHandle{}, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return process.CodexHandle{}, err
	}

	handle := process.CodexHandle{
		ProcessRunID:   runID,
		PID:            cmd.Process.Pid,
		CodexSessionID: codexSessionID,
	}
	stdoutSessionIDs, stdoutPlanUpdates := observeStdout(stdout, codexSessionID != "")
	active := &activeProcess{
		cmd:                    cmd,
		stderr:                 &stderr,
		home:                   codexHome,
		workdir:                workdir,
		codexSessionID:         codexSessionID,
		transcriptPath:         transcriptPath,
		transcriptRelativePath: transcriptRelativePath,
		baselineOffset:         baselineOffset,
		stdoutSessionIDs:       stdoutSessionIDs,
		stdoutPlanUpdates:      stdoutPlanUpdates,
		observer:               c.observer,
		done:                   make(chan struct{}),
	}
	processRegistry.Store(runID, active)
	go active.wait()
	return handle, nil
}

func (c *Client) Stop(ctx context.Context, processRunID process.RunID) error {
	value, ok := processRegistry.Load(processRunID)
	if !ok {
		return ErrProcessNotFound
	}
	active := value.(*activeProcess)
	if active.cmd.Process == nil {
		processRegistry.Delete(processRunID)
		return ErrProcessNotFound
	}
	if active.exited() {
		active.cleanupWithoutEventConsumer(processRunID)
		return nil
	}
	pid := active.cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	exited, err := waitForProcessDone(ctx, active.done, 500*time.Millisecond)
	if err != nil {
		return err
	}
	if exited {
		active.cleanupWithoutEventConsumer(processRunID)
		return nil
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	exited, err = waitForProcessDone(ctx, active.done, 2*time.Second)
	if err != nil {
		return err
	}
	if !exited {
		return errors.New("codex process did not exit after SIGKILL")
	}
	active.cleanupWithoutEventConsumer(processRunID)
	return nil
}

func (c *Client) StopDetached(ctx context.Context, detached process.DetachedProcess) error {
	if detached.ProcessRunID == "" || detached.PID <= 0 {
		return fmt.Errorf("%w: process run id and pid are required", process.ErrProcessOwnershipUnverified)
	}
	ops := c.detached
	if ops.groupAlive == nil || ops.groupOwnedBy == nil || ops.signalGroup == nil || ops.waitExit == nil {
		ops = defaultDetachedProcessOps()
	}
	alive, err := ops.groupAlive(detached.PID)
	if err != nil {
		return err
	}
	if !alive {
		return nil
	}
	owned, err := ops.groupOwnedBy(detached.PID, detached.ProcessRunID)
	if err != nil {
		return err
	}
	if !owned {
		alive, aliveErr := ops.groupAlive(detached.PID)
		if aliveErr != nil {
			return aliveErr
		}
		if !alive {
			return nil
		}
		return fmt.Errorf("%w: pid %d is not owned by process run %s", process.ErrProcessOwnershipUnverified, detached.PID, detached.ProcessRunID)
	}
	if err := ops.signalGroup(detached.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("terminate detached codex process group %d: %w", detached.PID, err)
	}
	exited, err := ops.waitExit(ctx, detached.PID, 500*time.Millisecond)
	if err != nil {
		return err
	}
	if !exited {
		if err := ops.signalGroup(detached.PID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("kill detached codex process group %d: %w", detached.PID, err)
		}
		exited, err = ops.waitExit(ctx, detached.PID, 2*time.Second)
		if err != nil {
			return err
		}
		if !exited {
			return errors.New("detached codex process did not exit after SIGKILL")
		}
	}
	if value, ok := processRegistry.Load(detached.ProcessRunID); ok {
		active := value.(*activeProcess)
		if active.cmd.Process != nil && active.cmd.Process.Pid == detached.PID {
			reaped, err := waitForProcessDone(ctx, active.done, 2*time.Second)
			if err != nil {
				return err
			}
			if !reaped {
				return errors.New("detached codex process was not reaped")
			}
			active.cleanupWithoutEventConsumer(detached.ProcessRunID)
		}
	}
	return nil
}

func detachedProcessGroupOwnedBy(processGroupID int, runID process.RunID) (bool, error) {
	members, err := detachedProcessGroupMembers(processGroupID)
	if err != nil {
		return false, err
	}
	if len(members) == 0 {
		return false, nil
	}
	want := processRunOwnerEnv + "=" + string(runID)
	verified := 0
	for _, pid := range members {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, fmt.Errorf("%w: read pid %d environment: %v", process.ErrProcessOwnershipUnverified, pid, err)
		}
		matched := false
		for _, item := range bytes.Split(raw, []byte{0}) {
			if string(item) == want {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
		verified++
	}
	return verified > 0, nil
}

func waitForDetachedProcessGroupExit(ctx context.Context, processGroupID int, timeout time.Duration) (bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		alive, err := detachedProcessGroupAlive(processGroupID)
		if err != nil {
			return false, err
		}
		if !alive {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return false, nil
		case <-ticker.C:
		}
	}
}

func detachedProcessGroupAlive(processGroupID int) (bool, error) {
	if err := syscall.Kill(-processGroupID, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		if !errors.Is(err, syscall.EPERM) {
			return false, fmt.Errorf("check detached codex process group %d: %w", processGroupID, err)
		}
	}
	members, err := detachedProcessGroupMembers(processGroupID)
	if err != nil {
		return false, err
	}
	return len(members) > 0, nil
}

func detachedProcessGroupMembers(processGroupID int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("%w: list proc processes: %v", process.ErrProcessOwnershipUnverified, err)
	}
	members := make([]int, 0, 1)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		groupID, state, err := procProcessGroup(pid)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if groupID == processGroupID && state != 'Z' {
			members = append(members, pid)
		}
	}
	return members, nil
}

func procProcessGroup(pid int) (int, byte, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, err
	}
	closing := bytes.LastIndexByte(raw, ')')
	if closing < 0 || closing+2 >= len(raw) {
		return 0, 0, fmt.Errorf("read detached codex process %d state: invalid proc stat", pid)
	}
	fields := strings.Fields(string(raw[closing+2:]))
	if len(fields) < 3 || len(fields[0]) != 1 {
		return 0, 0, fmt.Errorf("read detached codex process %d group: invalid proc stat", pid)
	}
	processGroupID, err := strconv.Atoi(fields[2])
	if err != nil {
		return 0, 0, fmt.Errorf("read detached codex process %d group: %w", pid, err)
	}
	return processGroupID, fields[0][0], nil
}

func (c *Client) Events(ctx context.Context, handle process.CodexHandle) (<-chan process.CodexEvent, error) {
	value, ok := processRegistry.Load(handle.ProcessRunID)
	if !ok {
		return nil, ErrProcessNotFound
	}
	active := value.(*activeProcess)
	if !active.startEvents() {
		return nil, errors.New("codex process events already started")
	}
	events := make(chan process.CodexEvent, 16)
	go func() {
		defer close(events)
		defer processRegistry.Delete(handle.ProcessRunID)
		exited := make(chan process.ExitResult, 1)
		go func() {
			exited <- active.waitResult()
		}()
		exitResult, readErr := tailSessionLog(ctx, active, events, exited, active.stdoutSessionIDs, active.stdoutPlanUpdates)
		sendProcessExit(ctx, events, exitResult, readErr)
	}()
	return events, nil
}

func observeStdout(stdout io.Reader, waitForTurn bool) (<-chan string, <-chan process.CodexEvent) {
	sessionIDs := make(chan string, 1)
	planUpdates := make(chan process.CodexEvent, 64)
	go func() {
		defer close(sessionIDs)
		defer close(planUpdates)
		reader := bufio.NewReader(stdout)
		identified := false
		turnStarted := !waitForTurn
		for {
			line, err := reader.ReadBytes('\n')
			raw := bytes.TrimSpace(line)
			if !identified && len(raw) > 0 {
				if sessionID := stdoutSessionID(raw); sessionID != "" {
					sessionIDs <- sessionID
					identified = true
				}
			}
			if stdoutEventType(raw) == "turn.started" {
				turnStarted = true
			}
			if turnStarted && len(raw) > 0 {
				if event, ok := stdoutPlanUpdate(raw); ok {
					sendLatestPlanUpdate(planUpdates, event)
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return sessionIDs, planUpdates
}

func stdoutEventType(raw []byte) string {
	var event map[string]any
	if json.Unmarshal(raw, &event) != nil {
		return ""
	}
	return stringValue(event, "type")
}

func sendLatestPlanUpdate(updates chan process.CodexEvent, event process.CodexEvent) {
	select {
	case updates <- event:
		return
	default:
	}
	select {
	case <-updates:
	default:
	}
	select {
	case updates <- event:
	default:
	}
}

func (a *activeProcess) wait() {
	result := waitProcess(a.cmd, a.stderr)
	a.mu.Lock()
	a.exitResult = result
	close(a.done)
	a.mu.Unlock()
}

func (a *activeProcess) waitResult() process.ExitResult {
	<-a.done
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exitResult
}

func (a *activeProcess) exited() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

func (a *activeProcess) startEvents() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.eventsStarted {
		return false
	}
	a.eventsStarted = true
	return true
}

func (a *activeProcess) cleanupWithoutEventConsumer(runID process.RunID) {
	a.mu.Lock()
	eventsStarted := a.eventsStarted
	a.mu.Unlock()
	if !eventsStarted {
		processRegistry.CompareAndDelete(runID, a)
	}
}

func waitForProcessDone(ctx context.Context, done <-chan struct{}, timeout time.Duration) (bool, error) {
	select {
	case <-done:
		return true, nil
	default:
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
		return false, nil
	}
}

func stdoutSessionID(raw []byte) string {
	var event map[string]any
	if json.Unmarshal(raw, &event) != nil || stringValue(event, "type") != "thread.started" {
		return ""
	}
	return stringValue(event, "thread_id", "session_id")
}

func stdoutPlanUpdate(raw []byte) (process.CodexEvent, bool) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return process.CodexEvent{}, false
	}
	eventType := stringValue(payload, "type")
	update, correlationID, ok := planUpdateFromEvent(eventType, payload)
	if !ok {
		return process.CodexEvent{}, false
	}
	update.EventID = stablePlanUpdateEventID(update)
	event := process.CodexEvent{
		Type:          eventType,
		Payload:       payload,
		PlanUpdate:    &update,
		RealtimeOnly:  true,
		CorrelationID: correlationID,
		EventID:       update.EventID,
		CreatedAt:     parseSessionTimestamp(stringValue(payload, "timestamp")),
	}
	applyCodexSemantic(&event)
	return event, true
}

func planUpdateFromEvent(eventType string, payload map[string]any) (process.PlanUpdate, string, bool) {
	normalizedType := strings.ToLower(strings.TrimSpace(eventType))
	if normalizedType == "plan_update" || normalizedType == "turn/plan/updated" || normalizedType == "turn.plan.updated" || normalizedType == "plan.updated" {
		if update, ok := planUpdateFromPayload(payload); ok {
			return update, planUpdateCorrelationID(payload), true
		}
	}
	item := mapValue(payload["item"])
	if (normalizedType == "item.started" || normalizedType == "item.updated") && strings.EqualFold(strings.TrimSpace(stringValue(item, "type")), "todo_list") {
		if update, ok := planUpdateFromPayload(item); ok {
			return update, planUpdateCorrelationID(payload), true
		}
	}
	if update, correlationID, ok := planUpdateFromToolPayload(payload); ok {
		return update, correlationID, true
	}
	return process.PlanUpdate{}, "", false
}

func planUpdateFromToolPayload(payload map[string]any) (process.PlanUpdate, string, bool) {
	if isUpdatePlanTool(payload) {
		for _, key := range []string{"arguments", "input"} {
			switch value := payload[key].(type) {
			case string:
				var arguments map[string]any
				if json.Unmarshal([]byte(value), &arguments) == nil {
					if update, ok := planUpdateFromPayload(arguments); ok {
						return update, planUpdateCorrelationID(payload), true
					}
				}
			case map[string]any:
				if update, ok := planUpdateFromPayload(value); ok {
					return update, planUpdateCorrelationID(payload), true
				}
			}
		}
		if update, ok := planUpdateFromPayload(payload); ok {
			return update, planUpdateCorrelationID(payload), true
		}
	}
	for _, key := range []string{"item", "msg", "message", "params"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if update, correlationID, found := planUpdateFromToolPayload(nested); found {
				if correlationID == "" {
					correlationID = planUpdateCorrelationID(payload)
				}
				return update, correlationID, true
			}
		}
	}
	return process.PlanUpdate{}, "", false
}

func isUpdatePlanTool(payload map[string]any) bool {
	name := stringValue(payload, "name", "tool", "tool_name", "toolName", "function_name", "functionName")
	if name == "" {
		name = stringValue(mapValue(payload["function"]), "name")
	}
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "update_plan" || strings.HasSuffix(name, ".update_plan")
}

func planUpdateFromPayload(payload map[string]any) (process.PlanUpdate, bool) {
	for _, key := range []string{"plan", "todoList", "todo_list", "todos", "items"} {
		if items, ok := payload[key].([]any); ok {
			return planUpdateFromItems(items), true
		}
	}
	for _, key := range []string{"item", "msg", "message", "params"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if update, found := planUpdateFromPayload(nested); found {
				return update, true
			}
		}
	}
	return process.PlanUpdate{}, false
}

func planUpdateFromItems(items []any) process.PlanUpdate {
	update := process.PlanUpdate{Items: make([]process.PlanItem, 0, len(items))}
	for _, value := range items {
		item := mapValue(value)
		step := strings.TrimSpace(stringValue(item, "step", "text", "title", "content"))
		if step == "" {
			continue
		}
		update.Items = append(update.Items, process.PlanItem{
			Step:   step,
			Status: planItemStatus(item),
		})
	}
	return update
}

func planItemStatus(item map[string]any) process.PlanItemStatus {
	if completed, ok := item["completed"].(bool); ok {
		if completed {
			return process.PlanItemCompleted
		}
		return process.PlanItemPending
	}
	switch strings.ToLower(strings.TrimSpace(stringValue(item, "status"))) {
	case "complete", "completed", "done", "success", "succeeded":
		return process.PlanItemCompleted
	case "in_progress", "in-progress", "progress", "running", "started":
		return process.PlanItemInProgress
	default:
		return process.PlanItemPending
	}
}

func planUpdateCorrelationID(payload map[string]any) string {
	item := mapValue(payload["item"])
	return firstString(
		item["call_id"], item["callId"], item["id"], item["item_id"], item["itemId"],
		payload["call_id"], payload["callId"], payload["id"], payload["item_id"], payload["itemId"],
	)
}

func stablePlanUpdateEventID(update process.PlanUpdate) string {
	type planItemIdentity struct {
		Step      string
		Completed bool
	}
	identity := make([]planItemIdentity, 0, len(update.Items))
	for _, item := range update.Items {
		identity = append(identity, planItemIdentity{
			Step:      item.Step,
			Completed: item.Status == process.PlanItemCompleted,
		})
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("plan:%x", digest[:16])
}

func tailSessionLog(ctx context.Context, active *activeProcess, events chan<- process.CodexEvent, exited <-chan process.ExitResult, stdoutSessionIDs <-chan string, stdoutPlanUpdates <-chan process.CodexEvent) (process.ExitResult, error) {
	bindStarted := time.Now()
	unmatchedStdoutPlans := []string(nil)
	unmatchedSessionPlans := []string(nil)
	var transcriptSource *process.CodexTranscriptSource
	transcriptAnnounced := false
	emit := func(event process.CodexEvent) error {
		if event.PlanUpdate != nil {
			planEventID := event.PlanUpdate.EventID
			if event.RealtimeOnly {
				var matched bool
				unmatchedSessionPlans, matched = consumePlanMatch(unmatchedSessionPlans, planEventID)
				if matched {
					return nil
				}
				unmatchedStdoutPlans = append(unmatchedStdoutPlans, planEventID)
			} else {
				var matched bool
				unmatchedStdoutPlans, matched = consumePlanMatch(unmatchedStdoutPlans, planEventID)
				if matched {
					event.PlanUpdate = nil
				} else {
					unmatchedSessionPlans = append(unmatchedSessionPlans, planEventID)
				}
			}
		}
		if transcriptSource != nil && !event.RealtimeOnly && !transcriptAnnounced {
			source := *transcriptSource
			event.Transcript = &source
			transcriptAnnounced = true
		}
		select {
		case events <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	path, remainingStdoutPlans, initialExitResult, processExited, err := waitForActiveSessionLog(ctx, active, exited, stdoutSessionIDs, stdoutPlanUpdates, emit)
	stdoutPlanUpdates = remainingStdoutPlans
	if err != nil {
		observe(active.observer, Observation{Name: "transcript.bind", Outcome: "failed", Reason: transcriptFailureReason(err), Duration: time.Since(bindStarted)})
		if processExited {
			if initialExitResult.FailureReason == "" {
				initialExitResult.FailureReason = err.Error()
			}
			if errors.Is(err, process.ErrTranscriptUnavailable) {
				initialExitResult.FailureCode = "codex_transcript_unavailable"
			}
			return initialExitResult, err
		}
		return failUnreadableProcess(active, exited, err), err
	}
	source, err := transcriptSourceForPath(active.home, active.codexSessionID, path)
	if err != nil {
		observe(active.observer, Observation{Name: "transcript.bind", Outcome: "failed", Reason: transcriptFailureReason(err), Duration: time.Since(bindStarted)})
		return failUnreadableProcess(active, exited, err), err
	}
	transcriptSource = &source
	file, err := os.Open(path)
	if err != nil {
		err = fmt.Errorf("open codex session log: %w", err)
		observe(active.observer, Observation{Name: "transcript.bind", Outcome: "failed", Reason: "unavailable", Duration: time.Since(bindStarted)})
		return failUnreadableProcess(active, exited, err), err
	}
	defer file.Close()
	sessionCWD := active.workdir
	sourceID := filepath.Base(path)
	projector := newCodexTranscriptProjector()
	skipLeadingLineTerminator := false
	if offset := active.baselineOffset; offset > 0 {
		var resumeOffset int64
		sessionCWD, resumeOffset, skipLeadingLineTerminator, err = primeCodexTranscriptProjector(path, offset, sessionCWD, sourceID, projector)
		if err != nil {
			err = fmt.Errorf("prime codex session state: %w", err)
			observe(active.observer, Observation{Name: "transcript.bind", Outcome: "failed", Reason: "read_failed", Duration: time.Since(bindStarted)})
			return failUnreadableProcess(active, exited, err), err
		}
		if _, err := file.Seek(resumeOffset, io.SeekStart); err != nil {
			err = fmt.Errorf("seek codex session log: %w", err)
			observe(active.observer, Observation{Name: "transcript.bind", Outcome: "failed", Reason: "read_failed", Duration: time.Since(bindStarted)})
			return failUnreadableProcess(active, exited, err), err
		}
	}
	observe(active.observer, Observation{Name: "transcript.bind", Outcome: "success", Duration: time.Since(bindStarted)})
	reader := bufio.NewReader(file)
	offset, _ := file.Seek(0, io.SeekCurrent)
	exitResult := initialExitResult
	var pending []byte
	var pendingOffset int64
	drainStdoutPlans := func() error {
		for stdoutPlanUpdates != nil {
			select {
			case event, ok := <-stdoutPlanUpdates:
				if !ok {
					stdoutPlanUpdates = nil
					continue
				}
				if err := emit(event); err != nil {
					return err
				}
			default:
				return nil
			}
		}
		return nil
	}
	flushTranscriptMessages := func(projected []process.CodexEvent) error {
		for _, event := range projected {
			if err := emit(event); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		line, err := reader.ReadBytes('\n')
		if skipLeadingLineTerminator && len(line) > 0 {
			if bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n")) {
				offset += int64(len(line))
				skipLeadingLineTerminator = false
				continue
			}
			if errors.Is(err, io.EOF) && bytes.Equal(line, []byte("\r")) {
				offset++
				continue
			}
			skipLeadingLineTerminator = false
		}
		if len(line) > 0 {
			if len(pending) == 0 {
				pendingOffset = offset
			}
			offset += int64(len(line))
			pending = append(pending, line...)
		}
		if len(pending) > 0 && (err == nil || processExited && errors.Is(err, io.EOF)) {
			raw := bytes.TrimRight(pending, "\r\n")
			raw = append([]byte(nil), raw...)
			if cwd := sessionCWDFromMeta(raw); cwd != "" {
				sessionCWD = cwd
			}
			parsed := parseSessionLogLine(raw, sessionCWD, sourceID, pendingOffset)
			parsed = projector.project(parsed)
			for _, event := range parsed {
				if err := emit(event); err != nil {
					if !processExited {
						exitResult = waitForExit(exited)
					}
					return exitResult, err
				}
			}
			pending = nil
		}
		if err := drainStdoutPlans(); err != nil {
			if !processExited {
				exitResult = waitForExit(exited)
			}
			return exitResult, err
		}
		if err := flushTranscriptMessages(projector.flushExpiredPending(time.Now())); err != nil {
			if !processExited {
				exitResult = waitForExit(exited)
			}
			return exitResult, err
		}
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) {
			if !processExited {
				exitResult = waitForExit(exited)
			}
			return exitResult, fmt.Errorf("read codex session log: %w", err)
		}
		if processExited {
			for stdoutPlanUpdates != nil {
				select {
				case event, ok := <-stdoutPlanUpdates:
					if !ok {
						stdoutPlanUpdates = nil
						continue
					}
					if err := emit(event); err != nil {
						return exitResult, err
					}
				case <-ctx.Done():
					return exitResult, ctx.Err()
				}
			}
			if err := flushTranscriptMessages(projector.flushPending()); err != nil {
				return exitResult, err
			}
			return exitResult, nil
		}
		select {
		case <-ctx.Done():
			exitResult = waitForExit(exited)
			return exitResult, ctx.Err()
		case event, ok := <-stdoutPlanUpdates:
			if !ok {
				stdoutPlanUpdates = nil
				continue
			}
			if err := emit(event); err != nil {
				exitResult = waitForExit(exited)
				return exitResult, err
			}
		case exitResult = <-exited:
			processExited = true
		case <-time.After(50 * time.Millisecond):
		}
		if err := flushTranscriptMessages(projector.flushExpiredPending(time.Now())); err != nil {
			if !processExited {
				exitResult = waitForExit(exited)
			}
			return exitResult, err
		}
	}
}

func consumePlanMatch(pending []string, eventID string) ([]string, bool) {
	for index, candidate := range pending {
		if candidate == eventID {
			return pending[index+1:], true
		}
	}
	return pending[:0], false
}

func primeCodexTranscriptProjector(path string, limit int64, sessionCWD string, sourceID string, projector *codexTranscriptProjector) (string, int64, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return sessionCWD, 0, false, err
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, limit))
	var offset int64
	resumeOffset := limit
	skipLeadingLineTerminator := false
	for offset < limit {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 {
			if readErr == nil {
				continue
			}
			break
		}
		lineOffset := offset
		offset += int64(len(line))
		if offset > limit {
			break
		}
		raw := bytes.TrimRight(line, "\r\n")
		if errors.Is(readErr, io.EOF) && !json.Valid(raw) {
			resumeOffset = lineOffset
		} else {
			if errors.Is(readErr, io.EOF) {
				skipLeadingLineTerminator = true
			}
			if cwd := sessionCWDFromMeta(raw); cwd != "" {
				sessionCWD = cwd
			}
			parsed := parseSessionLogLine(raw, sessionCWD, sourceID, lineOffset)
			projector.prime(parsed)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return sessionCWD, 0, false, readErr
		}
	}
	return sessionCWD, resumeOffset, skipLeadingLineTerminator, nil
}

func failUnreadableProcess(active *activeProcess, exited <-chan process.ExitResult, err error) process.ExitResult {
	terminateProcessGroup(active.cmd)
	result := waitForExit(exited)
	if result.FailureReason == "" {
		result.FailureReason = err.Error()
	}
	if errors.Is(err, process.ErrTranscriptUnavailable) {
		result.FailureCode = "codex_transcript_unavailable"
	}
	return result
}

func waitForExit(exited <-chan process.ExitResult) process.ExitResult {
	select {
	case result := <-exited:
		return result
	case <-time.After(2 * time.Second):
		return process.ExitResult{FailureReason: "codex process did not exit after transcript reader failure"}
	}
}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return
	}
	time.Sleep(500 * time.Millisecond)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func waitForActiveSessionLog(ctx context.Context, active *activeProcess, exited <-chan process.ExitResult, stdoutSessionIDs <-chan string, stdoutPlanUpdates <-chan process.CodexEvent, emit func(process.CodexEvent) error) (string, <-chan process.CodexEvent, process.ExitResult, bool, error) {
	var last string
	var exitResult process.ExitResult
	processExited := false
	for {
		if stdoutSessionIDs != nil {
			select {
			case sessionID, ok := <-stdoutSessionIDs:
				if !ok {
					stdoutSessionIDs = nil
				} else if sessionID != "" {
					active.codexSessionID = sessionID
				}
			default:
			}
		}
		if active.transcriptPath != "" || active.codexSessionID != "" {
			path, err := activeSessionLog(active)
			if err == nil && path != "" {
				return path, stdoutPlanUpdates, exitResult, processExited, nil
			}
			if err != nil {
				last = err.Error()
			}
		}
		if processExited && stdoutSessionIDs == nil {
			if last != "" {
				return "", stdoutPlanUpdates, exitResult, true, fmt.Errorf("%w: %s", process.ErrTranscriptUnavailable, last)
			}
			return "", stdoutPlanUpdates, exitResult, true, process.ErrTranscriptUnavailable
		}
		select {
		case <-ctx.Done():
			return "", stdoutPlanUpdates, exitResult, processExited, ctx.Err()
		case exitResult = <-exited:
			processExited = true
			exited = nil
		case sessionID, ok := <-stdoutSessionIDs:
			if !ok {
				stdoutSessionIDs = nil
				continue
			}
			if sessionID != "" {
				active.codexSessionID = sessionID
			}
		case event, ok := <-stdoutPlanUpdates:
			if !ok {
				stdoutPlanUpdates = nil
				continue
			}
			if err := emit(event); err != nil {
				return "", stdoutPlanUpdates, exitResult, processExited, err
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func activeSessionLog(active *activeProcess) (string, error) {
	if active.transcriptPath != "" {
		info, err := os.Stat(active.transcriptPath)
		if err != nil {
			return "", fmt.Errorf("%w: resume transcript is not readable", process.ErrTranscriptUnavailable)
		}
		if info.Size() > active.baselineOffset {
			return active.transcriptPath, nil
		}
		return "", nil
	}
	if active.codexSessionID != "" {
		path, err := sessionLogByID(active.home, active.codexSessionID)
		if err != nil || path == "" {
			return path, err
		}
		return path, nil
	}
	return "", nil
}

func (c *Client) SessionEvents(ctx context.Context, input process.CodexTranscriptInput) ([]process.CodexEvent, error) {
	startedAt := time.Now()
	var bytesRead int64
	observeRead := func(outcome string, reason string) {
		observe(c.observer, Observation{Name: "transcript.read", Outcome: outcome, Reason: reason, Duration: time.Since(startedAt), Bytes: bytesRead})
	}
	path, _, err := resolveTranscriptPath(c.CodexHome(), input.Source)
	if err != nil {
		observeRead("failed", transcriptFailureReason(err))
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		observeRead("failed", "unavailable")
		return nil, fmt.Errorf("%w: transcript source is not readable", process.ErrTranscriptUnavailable)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	events := []process.CodexEvent(nil)
	sessionCWD := ""
	sourceID := filepath.Base(path)
	projector := newCodexTranscriptProjector()
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
			bytesRead = offset
			observeRead("failed", transcriptFailureReason(err))
			return nil, err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineOffset := offset
			offset += int64(len(line))
			raw := bytes.TrimRight(line, "\r\n")
			raw = append([]byte(nil), raw...)
			if !errors.Is(err, io.EOF) || json.Valid(raw) {
				if cwd := sessionCWDFromMeta(raw); cwd != "" {
					sessionCWD = cwd
				}
				parsed := parseSessionLogLine(raw, sessionCWD, sourceID, lineOffset)
				parsed = projector.project(parsed)
				events = append(events, parsed...)
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		bytesRead = offset
		observeRead("failed", "read_failed")
		return nil, fmt.Errorf("read codex session log: %w", err)
	}
	events = append(events, projector.flushPending()...)
	bytesRead = offset
	observeRead("success", "")
	return events, nil
}

func observe(observer Observer, observation Observation) {
	if observer != nil {
		observer.Observe(observation)
	}
}

func transcriptFailureReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, process.ErrTranscriptUnavailable):
		return "unavailable"
	default:
		return "read_failed"
	}
}

func resolveTranscriptPath(codexHome string, source process.CodexTranscriptSource) (string, string, error) {
	if strings.TrimSpace(source.CodexSessionID) == "" || strings.TrimSpace(source.RelativePath) == "" {
		return "", "", fmt.Errorf("%w: transcript source is required", process.ErrTranscriptUnavailable)
	}
	relative := filepath.Clean(filepath.FromSlash(source.RelativePath))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: invalid transcript source path", process.ErrTranscriptUnavailable)
	}
	root := filepath.Join(codexHome, "sessions")
	path := filepath.Join(root, relative)
	checked, err := filepath.Rel(root, path)
	if err != nil || checked == ".." || strings.HasPrefix(checked, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: transcript source is outside codex sessions", process.ErrTranscriptUnavailable)
	}
	return path, filepath.ToSlash(checked), nil
}

func transcriptSourceForPath(codexHome string, codexSessionID string, path string) (process.CodexTranscriptSource, error) {
	root := filepath.Join(codexHome, "sessions")
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return process.CodexTranscriptSource{}, fmt.Errorf("%w: discovered transcript is outside codex sessions", process.ErrTranscriptUnavailable)
	}
	if strings.TrimSpace(codexSessionID) == "" {
		return process.CodexTranscriptSource{}, fmt.Errorf("%w: discovered transcript has no codex session id", process.ErrTranscriptUnavailable)
	}
	return process.CodexTranscriptSource{
		CodexSessionID: codexSessionID,
		RelativePath:   filepath.ToSlash(relative),
		BoundAt:        time.Now().UTC(),
	}, nil
}

func sessionLogByID(codexHome string, codexSessionID string) (string, error) {
	root := filepath.Join(codexHome, "sessions")
	var matched string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		if sessionLogMatchesSessionID(path, codexSessionID) {
			matched = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("find codex session log: %w", err)
	}
	return matched, nil
}

func sessionLogMatchesSessionID(path string, codexSessionID string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for i := 0; i < 20; i++ {
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return false
		}
		raw := bytes.TrimRight(line, "\r\n")
		var record struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if json.Unmarshal(raw, &record) != nil || record.Type != "session_meta" {
			if err != nil {
				return false
			}
			continue
		}
		return stringValue(payloadOrEmpty(record.Payload), "session_id", "id") == codexSessionID
	}
	return false
}

func waitProcess(cmd *exec.Cmd, stderr *bytes.Buffer) process.ExitResult {
	err := cmd.Wait()
	stderrText := ""
	if stderr != nil {
		stderrText = stderr.String()
	}
	result := process.ExitResult{}
	if err == nil {
		code := 0
		result.ExitCode = &code
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		result.ExitCode = &code
	}
	result.FailureReason = commandError(err, stderrText).Error()
	return result
}

func sendProcessExit(ctx context.Context, events chan<- process.CodexEvent, result process.ExitResult, readErr error) {
	payload := map[string]any{}
	if result.ExitCode != nil {
		payload["exitCode"] = *result.ExitCode
	}
	if result.FailureReason != "" {
		payload["failureReason"] = result.FailureReason
	}
	if result.FailureCode != "" {
		payload["failureCode"] = result.FailureCode
	}
	if readErr != nil {
		payload["readError"] = readErr.Error()
		if result.FailureReason == "" {
			payload["failureReason"] = readErr.Error()
		}
	}
	event := process.CodexEvent{Type: "process.exit", Payload: payload}
	applyCodexSemantic(&event)
	select {
	case events <- event:
	case <-ctx.Done():
	}
}

func parseSessionLogLine(raw []byte, sessionCWD string, sourceID string, offset int64) []process.CodexEvent {
	var record struct {
		Timestamp string         `json:"timestamp"`
		Type      string         `json:"type"`
		Payload   map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return finalizeCodexEvents([]process.CodexEvent{{
			EventID: sourceEventID("invalid_json", sourceID, offset),
			Type:    "invalid_json",
			Payload: map[string]any{"error": err.Error(), "byteCount": len(raw)},
		}}, sourceID, offset)
	}
	payload := payloadOrEmpty(record.Payload)
	_, hadEncryptedContent := payload["encrypted_content"]
	delete(payload, "encrypted_content")
	createdAt := parseSessionTimestamp(record.Timestamp)
	var events []process.CodexEvent
	switch record.Type {
	case "session_meta":
		threadID := stringValue(payload, "session_id", "id")
		events = []process.CodexEvent{{
			EventID:   eventID(record.Timestamp, "thread.started", threadID),
			Type:      "thread.started",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "response_item":
		if hadEncryptedContent && stringValue(payload, "type") == "reasoning" && reasoningText(payload) == "" {
			return nil
		}
		events = codexEventsFromResponseItem(record.Timestamp, payload, createdAt)
	case "event_msg":
		events = codexEventsFromEventMessage(record.Timestamp, payload, createdAt, sessionCWD)
	case "agent_message":
		normalized := normalizedItem("agent_message", "completed")
		normalized["output"] = firstString(payload["message"], payload["text"], payload["output"], messageText(payload))
		events = []process.CodexEvent{{
			EventID:   eventID(record.Timestamp, "item.completed", stringValue(payload, "id", "event_id")),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "compacted":
		events = []process.CodexEvent{{
			Type:      "context.compacted",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "turn_context":
		events = []process.CodexEvent{{
			Type:      "turn.context",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "world_state":
		events = []process.CodexEvent{{
			Type:      "world.state",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	default:
		if record.Type == "" {
			return nil
		}
		events = []process.CodexEvent{{
			Type:      record.Type,
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	}
	return finalizeCodexEvents(events, sourceID, offset)
}

func finalizeCodexEvents(events []process.CodexEvent, sourceID string, offset int64) []process.CodexEvent {
	for index := range events {
		if events[index].EventID == "" {
			events[index].EventID = sourceEventID(events[index].Type, sourceID, offset)
		}
		events[index].SourceOffset = offset
		events[index].SourceIndex = index
		applyCodexSemantic(&events[index])
	}
	return events
}

type codexTranscriptProjector struct {
	commands        map[string]process.CodexCommandContent
	recentCanonical []transcriptCanonicalMessage
	pendingMessages []pendingTranscriptMessage
	bufferedEvents  []process.CodexEvent
	visibility      standardTranscriptVisibility
	lastOccurred    time.Time
}

const transcriptMessageMirrorWindow = 100 * time.Millisecond

type transcriptCanonicalMessage struct {
	signature  string
	occurredAt time.Time
}

type pendingTranscriptMessage struct {
	event      process.CodexEvent
	signature  string
	historical bool
	pendingAt  time.Time
}

func newCodexTranscriptProjector() *codexTranscriptProjector {
	return &codexTranscriptProjector{
		commands:   map[string]process.CodexCommandContent{},
		visibility: newStandardTranscriptVisibility(),
	}
}

func (p *codexTranscriptProjector) project(events []process.CodexEvent) []process.CodexEvent {
	return p.projectEvents(events, false)
}

func (p *codexTranscriptProjector) prime(events []process.CodexEvent) {
	p.projectEvents(events, true)
}

func (p *codexTranscriptProjector) projectEvents(events []process.CodexEvent, historical bool) []process.CodexEvent {
	if p == nil {
		if historical {
			return nil
		}
		return events
	}
	projected := make([]process.CodexEvent, 0, len(events))
	for index := range events {
		event := &events[index]
		p.fillOccurredAt(event)
		p.mergeCommandState(event)
		p.pruneCanonicalMessages(event.CreatedAt)
		projected = append(projected, p.expirePendingMessages(event.CreatedAt)...)
		visible := p.visibility.visible(*event)
		if !visible {
			continue
		}
		canonicalSignature, canonical := canonicalMessageSignature(*event)
		if canonical {
			if pending, matched := p.consumePendingMessage(canonicalSignature, event.CreatedAt); matched {
				if !historical && !pending.historical {
					p.removeBufferedEvent(pending.event.EventID)
					p.bufferedEvents = append(p.bufferedEvents, *event)
				}
				projected = append(projected, p.releaseReadyBuffered()...)
				continue
			}
			p.recentCanonical = append(p.recentCanonical, transcriptCanonicalMessage{
				signature:  canonicalSignature,
				occurredAt: event.CreatedAt,
			})
			projected = append(projected, p.queueVisibleEvent(*event, historical)...)
			continue
		}
		if signature, mirror := eventMessageMirrorSignature(*event); mirror {
			if p.consumeCanonicalMessage(signature, event.CreatedAt) {
				continue
			}
			pendingAt := time.Time{}
			if !historical {
				pendingAt = time.Now()
			}
			p.pendingMessages = append(p.pendingMessages, pendingTranscriptMessage{
				event:      *event,
				signature:  signature,
				historical: historical,
				pendingAt:  pendingAt,
			})
			projected = append(projected, p.queueVisibleEvent(*event, historical)...)
			continue
		}
		projected = append(projected, p.queueVisibleEvent(*event, historical)...)
	}
	return projected
}

func (p *codexTranscriptProjector) fillOccurredAt(event *process.CodexEvent) {
	if event.CreatedAt.IsZero() {
		if p.lastOccurred.IsZero() {
			event.CreatedAt = time.Unix(0, event.SourceOffset+int64(event.SourceIndex)+1).UTC()
		} else {
			event.CreatedAt = p.lastOccurred
		}
	}
	p.lastOccurred = event.CreatedAt
}

func (p *codexTranscriptProjector) mergeCommandState(event *process.CodexEvent) {
	if event.CorrelationID == "" {
		return
	}
	if command, ok := event.Content.(process.CodexCommandContent); ok {
		if previous, exists := p.commands[event.CorrelationID]; exists && len(command.Commands) == 0 {
			command.Commands = previous.Commands
		}
		event.Content = command
		if event.Phase == process.CodexPhaseStarted || event.Phase == process.CodexPhaseProgress {
			p.commands[event.CorrelationID] = command
		} else {
			delete(p.commands, event.CorrelationID)
		}
		return
	}
	if tool, ok := event.Content.(process.CodexToolContent); ok {
		if command, exists := p.commands[event.CorrelationID]; exists {
			item := mapValue(event.Payload["item"])
			normalized := mapValue(event.Payload["normalizedItem"])
			command.Output = normalizeANSIText(tool.Output.Text)
			command.ExitCode = intPointer(normalized["exitCode"], item["exit_code"], item["exitCode"])
			command.DurationMS = intPointer(normalized["durationMs"], item["duration_ms"], item["durationMs"])
			if command.ExitCode != nil && *command.ExitCode != 0 && event.Phase == process.CodexPhaseCompleted {
				event.Phase = process.CodexPhaseFailed
			}
			event.Content = command
			if isTerminalCodexPhase(event.Phase) {
				delete(p.commands, event.CorrelationID)
			} else {
				p.commands[event.CorrelationID] = command
			}
		}
	}
}

func (p *codexTranscriptProjector) flushExpiredPending(now time.Time) []process.CodexEvent {
	if p == nil || len(p.pendingMessages) == 0 {
		return nil
	}
	pending := p.pendingMessages[:0]
	for _, message := range p.pendingMessages {
		if message.historical || message.pendingAt.IsZero() || now.Sub(message.pendingAt) < transcriptMessageMirrorWindow {
			pending = append(pending, message)
		}
	}
	p.pendingMessages = pending
	return p.releaseReadyBuffered()
}

func (p *codexTranscriptProjector) flushPending() []process.CodexEvent {
	if p == nil {
		return nil
	}
	p.pendingMessages = nil
	buffered := p.bufferedEvents
	p.bufferedEvents = nil
	return buffered
}

func (p *codexTranscriptProjector) expirePendingMessages(occurredAt time.Time) []process.CodexEvent {
	pending := p.pendingMessages[:0]
	for _, message := range p.pendingMessages {
		if occurredAt.Sub(message.event.CreatedAt) <= transcriptMessageMirrorWindow {
			pending = append(pending, message)
		}
	}
	p.pendingMessages = pending
	return p.releaseReadyBuffered()
}

func (p *codexTranscriptProjector) consumePendingMessage(signature string, occurredAt time.Time) (pendingTranscriptMessage, bool) {
	matchedIndex := -1
	matchedDelta := time.Duration(0)
	for index, pending := range p.pendingMessages {
		if pending.signature != signature {
			continue
		}
		delta := occurredAt.Sub(pending.event.CreatedAt)
		if delta < 0 || delta > transcriptMessageMirrorWindow {
			continue
		}
		if matchedIndex == -1 || delta < matchedDelta {
			matchedIndex = index
			matchedDelta = delta
		}
	}
	if matchedIndex == -1 {
		return pendingTranscriptMessage{}, false
	}
	matched := p.pendingMessages[matchedIndex]
	p.pendingMessages = append(p.pendingMessages[:matchedIndex], p.pendingMessages[matchedIndex+1:]...)
	return matched, true
}

func (p *codexTranscriptProjector) queueVisibleEvent(event process.CodexEvent, historical bool) []process.CodexEvent {
	if historical {
		return nil
	}
	p.bufferedEvents = append(p.bufferedEvents, event)
	return p.releaseReadyBuffered()
}

func (p *codexTranscriptProjector) releaseReadyBuffered() []process.CodexEvent {
	if len(p.bufferedEvents) == 0 {
		return nil
	}
	pendingEventIDs := make(map[string]struct{}, len(p.pendingMessages))
	for _, pending := range p.pendingMessages {
		if !pending.historical {
			pendingEventIDs[pending.event.EventID] = struct{}{}
		}
	}
	releaseCount := len(p.bufferedEvents)
	for index, event := range p.bufferedEvents {
		if _, pending := pendingEventIDs[event.EventID]; pending {
			releaseCount = index
			break
		}
	}
	if releaseCount == 0 {
		return nil
	}
	ready := append([]process.CodexEvent(nil), p.bufferedEvents[:releaseCount]...)
	p.bufferedEvents = append(p.bufferedEvents[:0], p.bufferedEvents[releaseCount:]...)
	return ready
}

func (p *codexTranscriptProjector) removeBufferedEvent(eventID string) {
	for index, event := range p.bufferedEvents {
		if event.EventID == eventID {
			p.bufferedEvents = append(p.bufferedEvents[:index], p.bufferedEvents[index+1:]...)
			return
		}
	}
}

func (p *codexTranscriptProjector) consumeCanonicalMessage(signature string, occurredAt time.Time) bool {
	for index, canonical := range p.recentCanonical {
		if canonical.signature != signature {
			continue
		}
		delta := occurredAt.Sub(canonical.occurredAt)
		if delta < 0 || delta > transcriptMessageMirrorWindow {
			continue
		}
		p.recentCanonical = append(p.recentCanonical[:index], p.recentCanonical[index+1:]...)
		return true
	}
	return false
}

func (p *codexTranscriptProjector) pruneCanonicalMessages(occurredAt time.Time) {
	recent := p.recentCanonical[:0]
	for _, canonical := range p.recentCanonical {
		delta := occurredAt.Sub(canonical.occurredAt)
		if delta < 0 || delta <= transcriptMessageMirrorWindow {
			recent = append(recent, canonical)
		}
	}
	p.recentCanonical = recent
}

func messageSignature(event process.CodexEvent) (string, bool) {
	message, ok := event.Content.(process.CodexMessageContent)
	if !ok || message.Text == "" {
		return "", false
	}
	return message.Role + "\x00" + message.Text, true
}

func eventMessageMirrorSignature(event process.CodexEvent) (string, bool) {
	if !eventMessageMirrorCandidate(event) {
		return "", false
	}
	return messageSignature(event)
}

func canonicalMessageSignature(event process.CodexEvent) (string, bool) {
	item := mapValue(event.Payload["item"])
	if event.Type != "item.completed" || stringValue(item, "type") != "message" || stringValue(item, "role") != "assistant" {
		return "", false
	}
	return messageSignature(event)
}

func eventMessageMirrorCandidate(event process.CodexEvent) bool {
	item := mapValue(event.Payload["item"])
	normalized := mapValue(event.Payload["normalizedItem"])
	return event.Type == "item.completed" &&
		stringValue(normalized, "type") == "agent_message" &&
		stringValue(item, "type") == "agent_message" &&
		stringValue(item, "message") != ""
}

func isTerminalCodexPhase(phase process.CodexPhase) bool {
	return phase == process.CodexPhaseCompleted || phase == process.CodexPhaseFailed || phase == process.CodexPhaseCancelled
}

type standardTranscriptVisibility struct {
	hiddenToolCalls map[string]struct{}
}

func newStandardTranscriptVisibility() standardTranscriptVisibility {
	return standardTranscriptVisibility{hiddenToolCalls: map[string]struct{}{}}
}

func (v standardTranscriptVisibility) visible(event process.CodexEvent) bool {
	if event.CorrelationID == "" {
		return true
	}
	tool, ok := event.Content.(process.CodexToolContent)
	if !ok {
		return true
	}
	if isInternalTranscriptTool(tool.QualifiedName) {
		if event.Phase == process.CodexPhaseStarted || event.Phase == process.CodexPhaseProgress {
			v.hiddenToolCalls[event.CorrelationID] = struct{}{}
		}
		if isTerminalCodexPhase(event.Phase) {
			delete(v.hiddenToolCalls, event.CorrelationID)
		}
		return false
	}
	if _, ok := v.hiddenToolCalls[event.CorrelationID]; ok {
		if isTerminalCodexPhase(event.Phase) {
			delete(v.hiddenToolCalls, event.CorrelationID)
		}
		return false
	}
	return true
}

func isInternalTranscriptTool(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return normalized == "apply_patch" || strings.HasSuffix(normalized, ".apply_patch")
}

func applyCodexSemantic(event *process.CodexEvent) {
	if event == nil {
		return
	}
	event.Phase = process.CodexPhaseStandalone
	item := mapValue(event.Payload["item"])
	normalized := mapValue(event.Payload["normalizedItem"])
	itemType := stringValue(normalized, "type")
	if itemType == "" {
		itemType = stringValue(item, "type")
	}
	event.CorrelationID = codexCorrelationID(item, event.Payload)
	if update, correlationID, ok := planUpdateFromEvent(event.Type, event.Payload); ok {
		update.EventID = stablePlanUpdateEventID(update)
		event.PlanUpdate = &update
		if correlationID != "" {
			event.CorrelationID = correlationID
		}
	}

	if event.Type == "token_count" {
		event.Content = codexUsageContent(event.Payload)
		return
	}
	if event.Type == "item.started" || event.Type == "item.completed" {
		applyCodexItemSemantic(event, itemType, item, normalized)
		return
	}
	if event.Type == "mcp_tool_call_end" {
		result := mapValue(event.Payload["result"])
		okResult := mapValue(result["Ok"])
		event.Phase = mcpToolPhase(result)
		invocation := mapValue(event.Payload["invocation"])
		event.Content = process.CodexToolContent{
			QualifiedName: qualifiedInvocationName(invocation),
			Category:      "mcp",
			Output: process.CodexStructuredText{
				Format: process.CodexTextJSON,
				Text:   jsonText(event.Payload["result"]),
			},
			Images: codexImages(okResult),
		}
		return
	}
	if isCodexStatusType(event.Type) {
		event.Content = codexStatusContent(event.Type, event.Payload)
		return
	}
	event.Content = process.CodexUnknownContent{RawType: event.Type, Payload: cloneMap(event.Payload)}
}

func applyCodexItemSemantic(event *process.CodexEvent, itemType string, item map[string]any, normalized map[string]any) {
	phase := codexItemPhase(event.Type, stringValue(normalized, "status"))
	output := firstString(normalized["output"], item["aggregated_output"], item["output"], item["text"])
	command := normalizeDisplayCommand(firstString(normalized["command"], item["command"]))
	name := firstString(normalized["qualifiedName"], item["name"])
	input := firstString(normalized["input"], item["input"])
	if name == "exec" {
		if nestedName := extractExecToolName(input); nestedName != "" {
			name = nestedName
		}
	}

	switch itemType {
	case "agent_message", "assistant_message":
		event.Phase = process.CodexPhaseStandalone
		event.Content = process.CodexMessageContent{Role: "assistant", Text: output, Format: process.CodexTextMarkdown, Images: codexImages(item)}
	case "user_message":
		event.Phase = process.CodexPhaseStandalone
		event.Content = process.CodexMessageContent{Role: "user", Text: output, Format: process.CodexTextPlain, Images: codexImages(item)}
	case "reasoning":
		event.Phase = process.CodexPhaseStandalone
		event.Content = process.CodexReasoningContent{Text: output}
	case "command_execution":
		event.Phase = phase
		commands := commandInvocationsFromValue(normalized["commands"])
		if len(commands) == 0 && command != "" {
			commands = []process.CodexCommandInvocation{{Command: command}}
		}
		for index := range commands {
			commands[index].Command = normalizeDisplayCommand(commands[index].Command)
		}
		content := process.CodexCommandContent{Commands: commands, Output: normalizeANSIText(output), ExitCode: intPointer(normalized["exitCode"], item["exit_code"], item["exitCode"]), DurationMS: intPointer(normalized["durationMs"], item["duration_ms"], item["durationMs"])}
		if content.ExitCode != nil && *content.ExitCode != 0 && event.Phase == process.CodexPhaseCompleted {
			event.Phase = process.CodexPhaseFailed
		}
		event.Content = content
	case "file_change":
		event.Phase = process.CodexPhaseStandalone
		event.Content = process.CodexFileChangeContent{Changes: codexFileChanges(normalized["changes"])}
	case "tool_call", "tool_result", "custom_tool_call", "tool_search", "web_search", "mcp_tool_call":
		event.Phase = phase
		event.Content = process.CodexToolContent{
			QualifiedName: name,
			Category:      codexToolCategory(itemType, name),
			Input:         structuredText(input, process.CodexTextPlain),
			Output:        structuredText(output, process.CodexTextPlain),
			Images:        codexImages(item),
		}
	default:
		event.Phase = phase
		event.Content = process.CodexUnknownContent{RawType: itemType, Payload: cloneMap(event.Payload)}
	}
}

func codexItemPhase(eventType string, status string) process.CodexPhase {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error":
		return process.CodexPhaseFailed
	case "cancelled", "canceled", "aborted":
		return process.CodexPhaseCancelled
	case "progress", "running":
		return process.CodexPhaseProgress
	case "in_progress", "started":
		return process.CodexPhaseStarted
	case "completed", "complete", "success", "succeeded":
		return process.CodexPhaseCompleted
	}
	if eventType == "item.started" {
		return process.CodexPhaseStarted
	}
	return process.CodexPhaseCompleted
}

func mcpToolPhase(value any) process.CodexPhase {
	result := mapValue(value)
	if result["Err"] != nil || result["err"] != nil {
		return process.CodexPhaseFailed
	}
	ok := mapValue(result["Ok"])
	if isError, _ := ok["isError"].(bool); isError {
		return process.CodexPhaseFailed
	}
	return process.CodexPhaseCompleted
}

func codexCorrelationID(item map[string]any, payload map[string]any) string {
	return firstString(
		item["call_id"], item["callId"], item["id"], item["item_id"], item["itemId"],
		payload["call_id"], payload["callId"], payload["id"], payload["item_id"], payload["itemId"],
	)
}

func codexToolCategory(itemType string, name string) string {
	switch itemType {
	case "web_search":
		return "web_search"
	case "tool_search":
		return "tool_search"
	case "custom_tool_call":
		return "custom"
	case "mcp_tool_call":
		return "mcp"
	}
	if strings.HasPrefix(name, "mcp__") || strings.Contains(name, ".mcp__") {
		return "mcp"
	}
	return "generic"
}

func codexImages(item map[string]any) []process.CodexImage {
	images := []process.CodexImage(nil)
	for _, field := range []string{"content", "output"} {
		parts, ok := item[field].([]any)
		if !ok {
			continue
		}
		for _, part := range parts {
			entry := mapValue(part)
			contentType := stringValue(entry, "type")
			resource := mapValue(entry["resource"])
			var source string
			var mimeType string
			inlineBlob := false
			sourceKind := "remote"
			switch contentType {
			case "input_image", "output_image", "image":
				source = firstString(entry["image_url"], entry["url"], entry["data"])
				mimeType = stringValue(entry, "mime_type", "mimeType")
			case "input_audio", "audio":
				source = firstString(entry["data"], entry["audio"])
				inlineBlob = source != ""
				if source == "" {
					source = firstString(entry["url"])
				}
				mimeType = stringValue(entry, "mime_type", "mimeType")
			case "resource", "embedded_resource":
				source = firstString(resource["blob"], entry["blob"])
				inlineBlob = source != ""
				if source == "" {
					source = firstString(resource["url"], resource["uri"])
				}
				mimeType = firstString(resource["mimeType"], resource["mime_type"], entry["mimeType"], entry["mime_type"])
			default:
				continue
			}
			if source == "" {
				continue
			}
			if strings.HasPrefix(source, "data:") {
				sourceKind = "inline"
			} else if inlineBlob {
				// GLUE: The transcript image carrier also transports inline MCP blobs until GraphQL exposes a generic artifact candidate.
				sourceKind = "inline_base64"
			} else if strings.HasPrefix(source, "/") {
				sourceKind = "managed_file"
			}
			images = append(images, process.CodexImage{Source: source, Detail: stringValue(entry, "detail"), SourceKind: sourceKind, MimeType: mimeType})
		}
	}
	return images
}

func codexFileChanges(value any) []process.CodexFileChange {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	changes := make([]process.CodexFileChange, 0, len(entries))
	for _, value := range entries {
		entry := mapValue(value)
		path := stringValue(entry, "path")
		if path == "" {
			continue
		}
		changes = append(changes, process.CodexFileChange{
			Kind:        normalizeFileChangeKind(stringValue(entry, "kind")),
			Path:        path,
			MovePath:    stringValue(entry, "movePath", "move_path"),
			UnifiedDiff: stringValue(entry, "unifiedDiff", "unified_diff"),
		})
	}
	return changes
}

func normalizeFileChangeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "add", "added", "create", "created":
		return "added"
	case "delete", "deleted", "remove", "removed":
		return "deleted"
	case "rename", "renamed", "move", "moved":
		return "renamed"
	default:
		return "modified"
	}
}

func codexUsageContent(payload map[string]any) process.CodexUsageContent {
	info := mapValue(payload["info"])
	total := mapValue(info["total_token_usage"])
	current := mapValue(info["last_token_usage"])
	return process.CodexUsageContent{
		InputTokens:                  intValue(total["input_tokens"]),
		CachedInputTokens:            intValue(total["cached_input_tokens"]),
		OutputTokens:                 intValue(total["output_tokens"]),
		ReasoningOutputTokens:        intValue(total["reasoning_output_tokens"]),
		TotalTokens:                  intValue(total["total_tokens"]),
		ContextWindow:                intValue(info["model_context_window"]),
		CurrentInputTokens:           intValue(current["input_tokens"]),
		CurrentCachedInputTokens:     intValue(current["cached_input_tokens"]),
		CurrentOutputTokens:          intValue(current["output_tokens"]),
		CurrentReasoningOutputTokens: intValue(current["reasoning_output_tokens"]),
		CurrentTotalTokens:           intValue(current["total_tokens"]),
	}
}

func isCodexStatusType(eventType string) bool {
	switch eventType {
	case "thread.started", "task.started", "task.completed", "turn.started", "turn.completed", "turn.aborted", "context.compacted", "turn.context", "world.state", "process.exit", "error", "invalid_json", "inter_agent_communication_metadata", "sub_agent_activity", "thread_settings_applied":
		return true
	default:
		return false
	}
}

func codexStatusContent(code string, payload map[string]any) process.CodexStatusContent {
	level := "info"
	if code == "error" || code == "invalid_json" || (code == "process.exit" && intValue(payload["exitCode"]) != 0) {
		level = "error"
	} else if code == "turn.aborted" || code == "context.compacted" || (code == "sub_agent_activity" && stringValue(payload, "kind") == "interrupted") {
		level = "warning"
	}
	message := firstString(payload["message"], payload["reason"], payload["failureReason"])
	if message == "" {
		switch code {
		case "inter_agent_communication_metadata":
			message = "Inter-agent communication metadata"
		case "sub_agent_activity":
			message = strings.TrimSpace("Sub-agent " + firstString(payload["kind"]) + " " + firstString(payload["agent_path"]))
		case "thread_settings_applied":
			message = "Thread settings applied"
		}
	}
	return process.CodexStatusContent{
		Code:    code,
		Level:   level,
		Message: message,
		Details: cloneMap(payload),
	}
}

func structuredText(text string, fallback process.CodexTextFormat) process.CodexStructuredText {
	if text == "" {
		return process.CodexStructuredText{Format: fallback}
	}
	format := fallback
	if json.Valid([]byte(text)) {
		format = process.CodexTextJSON
	}
	return process.CodexStructuredText{Format: format, Text: text}
}

func normalizeANSIText(value string) string {
	return strings.ReplaceAll(value, "␛[", "\x1b[")
}

func normalizeDisplayCommand(value string) string {
	command := strings.TrimSpace(value)
	for _, prefix := range []string{"/bin/bash -lc ", "bash -lc ", "/bin/sh -lc ", "sh -lc ", "/bin/zsh -lc ", "zsh -lc "} {
		if strings.HasPrefix(command, prefix) {
			return unquoteShellArgument(strings.TrimSpace(strings.TrimPrefix(command, prefix)))
		}
	}
	return command
}

func unquoteShellArgument(value string) string {
	if len(value) < 2 || value[0] != value[len(value)-1] || (value[0] != '\'' && value[0] != '"') {
		return value
	}
	inner := value[1 : len(value)-1]
	if value[0] == '\'' {
		return strings.ReplaceAll(inner, `'\''`, `'`)
	}
	replacer := strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`")
	return replacer.Replace(inner)
}

func qualifiedInvocationName(invocation map[string]any) string {
	server := stringValue(invocation, "server")
	tool := stringValue(invocation, "tool")
	if server == "" {
		return tool
	}
	if tool == "" {
		return server
	}
	return server + "." + tool
}

func intPointer(values ...any) *int {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			result := typed
			return &result
		case int64:
			result := int(typed)
			return &result
		case float64:
			result := int(typed)
			return &result
		}
	}
	return nil
}

func intValue(value any) int {
	if result := intPointer(value); result != nil {
		return *result
	}
	return 0
}

func firstString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return ""
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func cloneMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func sessionCWDFromMeta(raw []byte) string {
	var record struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if json.Unmarshal(raw, &record) != nil || record.Type != "session_meta" {
		return ""
	}
	return stringValue(payloadOrEmpty(record.Payload), "cwd")
}

func parseSessionTimestamp(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func codexEventsFromResponseItem(timestamp string, payload map[string]any, createdAt time.Time) []process.CodexEvent {
	itemType := stringValue(payload, "type")
	switch itemType {
	case "function_call":
		callID := stringValue(payload, "call_id")
		command := commandFromFunctionArguments(payload)
		itemType := "tool_call"
		if command.Command != "" {
			itemType = "command_execution"
		}
		normalized := normalizedItem(itemType, "in_progress")
		normalized["qualifiedName"] = qualifiedToolName(payload)
		normalized["input"] = stringOrJSON(payload["arguments"])
		normalized["command"] = command.Command
		if command.Command != "" {
			normalized["commands"] = commandInvocationValues([]process.CodexCommandInvocation{command})
		}
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.started", callID),
			Type:      "item.started",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "function_call_output":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("tool_result", stringValue(payload, "status"))
		if normalized["status"] == "" {
			normalized["status"] = "completed"
		}
		normalized["output"] = textFromValue(payload["output"])
		normalized["exitCode"] = firstValue(payload["exit_code"], payload["exitCode"])
		normalized["durationMs"] = firstValue(payload["duration_ms"], payload["durationMs"])
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.completed", callID),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "custom_tool_call":
		callID := stringValue(payload, "call_id")
		name := stringValue(payload, "name")
		input := stringOrJSON(payload["input"])
		itemType := "custom_tool_call"
		commands, extracted := extractExecCommandInvocations(input)
		nestedName := extractExecToolName(input)
		if name == "exec" && (extracted || isCommandTransportTool(nestedName)) {
			itemType = "command_execution"
		}
		normalized := normalizedItem(itemType, "in_progress")
		normalized["qualifiedName"] = name
		normalized["input"] = input
		if itemType == "command_execution" {
			normalized["commands"] = commandInvocationValues(commands)
		}
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.started", callID),
			Type:      "item.started",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "custom_tool_call_output":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("custom_tool_call", "completed")
		result := normalizeCustomToolOutput(payload["output"])
		normalized["output"] = result.output
		if result.exitCode != nil {
			normalized["exitCode"] = *result.exitCode
		}
		if result.durationMS != nil {
			normalized["durationMs"] = *result.durationMS
		}
		if result.status != "" {
			normalized["status"] = result.status
		}
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.completed", callID),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "tool_search_call":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("tool_search", "in_progress")
		normalized["qualifiedName"] = "tool_search"
		normalized["input"] = stringOrJSON(payload["arguments"])
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.started", callID),
			Type:      "item.started",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "tool_search_output":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("tool_search", "completed")
		normalized["qualifiedName"] = "tool_search"
		normalized["output"] = jsonText(payload["tools"])
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.completed", callID),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "web_search_call":
		callID := stringValue(payload, "id", "call_id")
		status := strings.ToLower(strings.TrimSpace(stringValue(payload, "status")))
		if status == "" {
			status = "completed"
		}
		eventType := "item.completed"
		if status == "in_progress" {
			eventType = "item.started"
		}
		normalized := normalizedItem("web_search", status)
		normalized["output"] = stringOrJSON(payload["action"])
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, eventType, callID),
			Type:      eventType,
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "agent_message", "assistant_message", "user_message":
		normalizedType := itemType
		if normalizedType == "assistant_message" {
			normalizedType = "agent_message"
		}
		normalized := normalizedItem(normalizedType, "completed")
		normalized["output"] = firstString(payload["message"], payload["text"], payload["output"], messageText(payload))
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.completed", stringValue(payload, "id", "event_id")),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "message":
		itemType := ""
		switch stringValue(payload, "role") {
		case "user":
			itemType = "user_message"
		case "assistant":
			itemType = "agent_message"
		}
		if itemType == "" {
			return nil
		}
		id := stringValue(payload, "id")
		normalized := normalizedItem(itemType, "completed")
		normalized["output"] = messageText(payload)
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.completed", id),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "reasoning":
		normalized := normalizedItem("reasoning", "completed")
		normalized["output"] = reasoningText(payload)
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.completed", stringValue(payload, "id")),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	}
	if itemType == "" {
		return nil
	}
	return []process.CodexEvent{{
		Type:      itemType,
		Payload:   payload,
		CreatedAt: createdAt,
	}}
}

func normalizedItem(itemType string, status string) map[string]any {
	return map[string]any{"type": itemType, "status": status}
}

func hasAnyKey(value map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := value[key]; ok {
			return true
		}
	}
	return false
}

func firstValue(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func itemEventPayload(item map[string]any, normalized map[string]any) map[string]any {
	return map[string]any{"item": item, "normalizedItem": normalized}
}

func stringOrJSON(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return jsonText(value)
}

func codexEventsFromEventMessage(timestamp string, payload map[string]any, createdAt time.Time, sessionCWD string) []process.CodexEvent {
	eventType := stringValue(payload, "type")
	switch eventType {
	case "patch_apply_end":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("file_change", stringValue(payload, "status"))
		normalized["changes"] = fileChangesFromPatch(payload, sessionCWD)
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.completed", callID),
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "agent_message":
		normalized := normalizedItem("agent_message", "completed")
		normalized["output"] = firstString(payload["message"], payload["text"], payload["output"], messageText(payload))
		return []process.CodexEvent{{
			Type:      "item.completed",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "user_message", "context_compacted", "web_search_end":
		// Richer canonical records are emitted as response_item or compacted entries.
		return nil
	case "task_started":
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "task.started", stringValue(payload, "turn_id")),
			Type:      "task.started",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "task_complete":
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "task.completed", stringValue(payload, "turn_id")),
			Type:      "task.completed",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "token_count":
		return []process.CodexEvent{{
			Type:      "token_count",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	case "turn_aborted":
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "turn.aborted", stringValue(payload, "turn_id")),
			Type:      "turn.aborted",
			Payload:   payload,
			CreatedAt: createdAt,
		}}
	}
	if eventType == "" {
		return nil
	}
	return []process.CodexEvent{{
		Type:      eventType,
		Payload:   payload,
		CreatedAt: createdAt,
	}}
}

func qualifiedToolName(payload map[string]any) string {
	name := stringValue(payload, "name")
	namespace := stringValue(payload, "namespace")
	if namespace == "" {
		return name
	}
	if name == "" {
		return namespace
	}
	return namespace + "." + name
}

func jsonText(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func commandFromFunctionArguments(payload map[string]any) process.CodexCommandInvocation {
	arguments := stringValue(payload, "arguments")
	if arguments == "" {
		return process.CodexCommandInvocation{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return process.CodexCommandInvocation{Command: arguments}
	}
	return process.CodexCommandInvocation{
		Command: stringValue(parsed, "cmd", "command"),
		Workdir: stringValue(parsed, "workdir"),
	}
}

func commandInvocationValues(commands []process.CodexCommandInvocation) []any {
	values := make([]any, 0, len(commands))
	for _, command := range commands {
		values = append(values, map[string]any{"command": command.Command, "workdir": command.Workdir})
	}
	return values
}

func commandInvocationsFromValue(value any) []process.CodexCommandInvocation {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}
	commands := make([]process.CodexCommandInvocation, 0, len(entries))
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			return nil
		}
		command := stringValue(item, "command")
		if command == "" {
			return nil
		}
		commands = append(commands, process.CodexCommandInvocation{Command: command, Workdir: stringValue(item, "workdir")})
	}
	return commands
}

func messageText(payload map[string]any) string {
	content, ok := payload["content"].([]any)
	if !ok {
		return stringValue(payload, "message")
	}
	var builder strings.Builder
	for _, item := range content {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text := stringValue(entry, "text"); text != "" {
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(text)
		}
	}
	return builder.String()
}

func reasoningText(payload map[string]any) string {
	for _, value := range []any{
		payload["summary"],
		payload["content"],
		payload["text"],
		payload["message"],
	} {
		if text := textFromValue(value); text != "" {
			return text
		}
	}
	return ""
}

func textFromValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := textFromValue(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "content", "summary"} {
			if text := textFromValue(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

type customToolOutput struct {
	output     string
	exitCode   *int
	durationMS *int
	status     string
}

func normalizeCustomToolOutput(value any) customToolOutput {
	items, ok := value.([]any)
	if !ok {
		return unwrapCustomToolEnvelope(textFromValue(value))
	}
	result := customToolOutput{}
	parts := make([]string, 0, len(items))
	for index, item := range items {
		text := textFromValue(item)
		if index == 0 {
			if status, durationMS, matched := parseScriptSummary(text); matched {
				result.status = status
				result.durationMS = durationMS
				continue
			}
		}
		part := unwrapCustomToolEnvelope(text)
		if part.output != "" {
			parts = append(parts, part.output)
		}
		if part.exitCode != nil {
			result.exitCode = part.exitCode
		}
		if part.durationMS != nil {
			result.durationMS = part.durationMS
		}
		if part.status != "" {
			result.status = part.status
		}
	}
	result.output = strings.Join(parts, "\n")
	return result
}

func parseScriptSummary(value string) (string, *int, bool) {
	lines := strings.Split(value, "\n")
	if len(lines) < 4 || lines[2] != "Output:" {
		return "", nil, false
	}
	for _, line := range lines[3:] {
		if line != "" {
			return "", nil, false
		}
	}
	status := ""
	switch {
	case lines[0] == "Script completed":
		status = "completed"
	case strings.HasPrefix(lines[0], "Script running with cell ID "):
		status = "running"
	case lines[0] == "Script failed":
		status = "failed"
	default:
		return "", nil, false
	}
	const wallTimePrefix = "Wall time "
	const wallTimeSuffix = " seconds"
	if !strings.HasPrefix(lines[1], wallTimePrefix) || !strings.HasSuffix(lines[1], wallTimeSuffix) {
		return "", nil, false
	}
	seconds, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(lines[1], wallTimePrefix), wallTimeSuffix), 64)
	if err != nil || seconds < 0 {
		return "", nil, false
	}
	durationMS := int(seconds*1000 + 0.5)
	return status, &durationMS, true
}

func unwrapCustomToolEnvelope(value string) customToolOutput {
	result := customToolOutput{output: value}
	var envelope map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(value)), &envelope) != nil {
		return result
	}
	output, ok := envelope["output"].(string)
	if !ok || !hasAnyKey(envelope, "chunk_id", "session_id", "exit_code", "wall_time_seconds", "original_token_count") {
		return result
	}
	result.output = output
	result.exitCode = intPointer(envelope["exit_code"], envelope["exitCode"])
	if seconds, ok := envelope["wall_time_seconds"].(float64); ok && seconds >= 0 {
		durationMS := int(seconds*1000 + 0.5)
		result.durationMS = &durationMS
	}
	if result.exitCode == nil && envelope["session_id"] != nil {
		result.status = "running"
	}
	return result
}

func isCommandTransportTool(name string) bool {
	switch name {
	case "tools.write_stdin", "tools.wait":
		return true
	default:
		return false
	}
}

func fileChangesFromPatch(payload map[string]any, sessionCWD string) []any {
	changes, ok := payload["changes"].(map[string]any)
	if !ok {
		return nil
	}
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]any, 0, len(paths))
	for _, path := range paths {
		entry, _ := changes[path].(map[string]any)
		result = append(result, map[string]any{
			"path":        normalizePatchPath(path, sessionCWD),
			"kind":        stringValue(entry, "type"),
			"unifiedDiff": stringValue(entry, "unified_diff", "unifiedDiff"),
			"movePath":    normalizePatchPath(stringValue(entry, "move_path", "movePath"), sessionCWD),
		})
	}
	return result
}

func normalizePatchPath(path string, sessionCWD string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(path)
	}
	if sessionCWD != "" {
		if rel, err := filepath.Rel(sessionCWD, path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func eventID(timestamp string, typ string, key string) string {
	if key == "" {
		return ""
	}
	parts := []string{timestamp, typ}
	parts = append(parts, key)
	return strings.Join(parts, ":")
}

func sourceEventID(typ string, sourceID string, offset int64) string {
	if sourceID == "" {
		sourceID = "session"
	}
	return fmt.Sprintf("source:%s:%d:%s", sourceID, offset, typ)
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}

func stringValue(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

func (c *Client) buildStartArgs(input process.CodexStartInput) []string {
	args := []string{"exec", "--json", "--skip-git-repo-check"}
	if input.Workdir != "" {
		args = append(args, "-C", input.Workdir)
	}
	if input.ArtifactDir != "" {
		args = append(args, "--add-dir", input.ArtifactDir)
	}
	args = c.appendMCPArgs(args, input.SessionID)
	args = c.appendPlaywrightMCPArgs(args, input.ProcessRunID, input.ArtifactDir)
	args = c.appendRuntimeConfigArgs(args, input.Model, input.ReasoningEffort, input.PermissionMode, input.FastMode, true)
	for _, path := range input.ImagePaths {
		if path != "" {
			args = append(args, "-i", path)
		}
	}
	if input.Prompt != "" {
		args = append(args, "-")
	}
	return args
}

func (c *Client) buildResumeArgs(input process.CodexResumeInput) []string {
	args := []string{"exec", "resume", "--json", "--skip-git-repo-check"}
	args = c.appendMCPArgs(args, input.SessionID)
	args = c.appendPlaywrightMCPArgs(args, input.ProcessRunID, input.ArtifactDir)
	args = c.appendRuntimeConfigArgs(args, input.Model, input.ReasoningEffort, input.PermissionMode, input.FastMode, false)
	if input.ArtifactDir != "" && input.PermissionMode == "workspace-write" {
		args = append(args, "-c", fmt.Sprintf("sandbox_workspace_write.writable_roots=[%q]", input.ArtifactDir))
	}
	for _, path := range input.ImagePaths {
		if path != "" {
			args = append(args, "-i", path)
		}
	}
	if input.CodexSessionID != "" {
		args = append(args, input.CodexSessionID)
	}
	if input.Prompt != "" {
		args = append(args, "-")
	}
	return args
}

func (c *Client) appendPlaywrightMCPArgs(args []string, runID process.RunID, artifactDir string) []string {
	if c == nil || c.playwrightBin == "" || runID == "" || artifactDir == "" {
		return args
	}
	outputDir := filepath.Join(artifactDir, "browser", string(runID))
	playwrightArgs := []string{"--headless", "--isolated", "--image-responses", "allow", "--output-dir", outputDir}
	if c.chromiumBin != "" {
		playwrightArgs = append(playwrightArgs, "--executable-path", c.chromiumBin)
	}
	args = append(args,
		"-c", `mcp_servers.playwright.type="stdio"`,
		"-c", fmt.Sprintf("mcp_servers.playwright.command=%q", c.playwrightBin),
		"-c", fmt.Sprintf("mcp_servers.playwright.args=%s", tomlStringArray(playwrightArgs)),
	)
	if c.mcpToolTimeout {
		args = append(args, "-c", fmt.Sprintf("mcp_servers.playwright.tool_timeout_sec=%d", mcpToolTimeoutSeconds))
	}
	return args
}

func (c *Client) appendRuntimeConfigArgs(args []string, model string, reasoningEffort string, permissionMode string, fastMode bool, allowSandboxFlag bool) []string {
	if model != "" {
		args = append(args, "-m", model)
	}
	if reasoningEffort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", reasoningEffort))
	}
	if fastMode {
		args = append(args, "-c", `service_tier="priority"`)
	}
	if permissionMode == "" {
		return args
	}
	if c != nil && c.mcpStdioSocket != "" {
		if profile, ok := mcpPermissionProfile(permissionMode); ok {
			return appendMCPPermissionProfileArgs(args, profile, c.mcpStdioSocket)
		}
	}
	if allowSandboxFlag {
		args = append(args, "--sandbox", permissionMode)
	}
	return args
}

func (c *Client) appendMCPArgs(args []string, sessionID process.SessionID) []string {
	if c == nil || sessionID == "" {
		return args
	}
	if c.mcpStdioCommand != "" && c.mcpStdioSocket != "" {
		mcpArgs := []string{"mcp-stdio", "--session-id", string(sessionID), "--socket", c.mcpStdioSocket}
		args = append(args,
			"-c", `mcp_servers.anycode.type="stdio"`,
			"-c", fmt.Sprintf("mcp_servers.anycode.command=%q", c.mcpStdioCommand),
			"-c", fmt.Sprintf("mcp_servers.anycode.args=%s", tomlStringArray(mcpArgs)),
		)
		if c.mcpToolTimeout {
			args = append(args, "-c", fmt.Sprintf("mcp_servers.anycode.tool_timeout_sec=%d", mcpToolTimeoutSeconds))
		}
		if c.mcpAuthToken != "" {
			args = append(args, "-c", `mcp_servers.anycode.env_vars=["ANYCODE_MCP_TOKEN"]`)
		}
		return args
	}
	if c.mcpBaseURL == "" {
		return args
	}
	url := c.mcpBaseURL + "/mcp/sessions/" + string(sessionID)
	args = append(args,
		"-c", `mcp_servers.anycode.type="streamable_http"`,
		"-c", fmt.Sprintf("mcp_servers.anycode.url=%q", url),
	)
	if c.mcpToolTimeout {
		args = append(args, "-c", fmt.Sprintf("mcp_servers.anycode.tool_timeout_sec=%d", mcpToolTimeoutSeconds))
	}
	if c.mcpAuthToken != "" {
		args = append(args, "-c", `mcp_servers.anycode.bearer_token_env_var="ANYCODE_MCP_TOKEN"`)
	}
	return args
}

func tomlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

func mcpPermissionProfile(permissionMode string) (string, bool) {
	switch strings.TrimSpace(permissionMode) {
	case "read-only":
		return ":read-only", true
	case "workspace-write":
		return ":workspace", true
	default:
		return "", false
	}
}

func appendMCPPermissionProfileArgs(args []string, extends string, socket string) []string {
	const profile = "anycode-mcp"
	return append(args,
		"-c", `features.network_proxy.enabled=true`,
		"-c", fmt.Sprintf("default_permissions=%q", profile),
		"-c", fmt.Sprintf("permissions.%s.extends=%q", profile, extends),
		"-c", fmt.Sprintf("permissions.%s.network.enabled=true", profile),
		"-c", fmt.Sprintf("permissions.%s.network.mode=%q", profile, "limited"),
		"-c", fmt.Sprintf("permissions.%s.network.unix_sockets=%s", profile, tomlStringMap(map[string]string{socket: "allow"})),
	)
}

func tomlStringMap(values map[string]string) string {
	pairs := make([]string, 0, len(values))
	for key, value := range values {
		pairs = append(pairs, strconv.Quote(key)+"="+strconv.Quote(value))
	}
	return "{" + strings.Join(pairs, ",") + "}"
}

func upsertEnv(env []string, key string, value string) []string {
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
