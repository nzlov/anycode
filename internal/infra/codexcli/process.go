package codexcli

import (
	"bufio"
	"bytes"
	"context"
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

var (
	ErrProcessNotFound      = errors.New("codex process run is not active")
	errAmbiguousSessionLogs = errors.New("multiple active codex session logs")
)

type activeProcess struct {
	cmd            *exec.Cmd
	stdout         io.ReadCloser
	stderr         *bytes.Buffer
	home           string
	workdir        string
	codexSessionID string
	baseline       map[string]int64
}

var processRegistry sync.Map

func (c *Client) Start(ctx context.Context, input process.CodexStartInput) (process.CodexHandle, error) {
	args := c.buildStartArgs(input)
	return c.start(ctx, input.ProcessRunID, args, input.Workdir, "")
}

func (c *Client) Resume(ctx context.Context, input process.CodexResumeInput) (process.CodexHandle, error) {
	args := c.buildResumeArgs(input)
	return c.start(ctx, input.ProcessRunID, args, input.Workdir, input.CodexSessionID)
}

func (c *Client) start(ctx context.Context, runID process.RunID, args []string, workdir string, codexSessionID string) (process.CodexHandle, error) {
	if runID == "" {
		return process.CodexHandle{}, errors.New("process run id is required")
	}
	if err := ctx.Err(); err != nil {
		return process.CodexHandle{}, err
	}
	codexHome := c.CodexHome()
	baseline := sessionLogOffsets(codexHome)
	cmd := exec.CommandContext(context.Background(), c.Bin(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	env := os.Environ()
	if codexHome != "" {
		env = upsertEnv(env, "CODEX_HOME", codexHome)
	}
	if c.mcpAuthToken != "" {
		env = upsertEnv(env, "ANYCODE_MCP_TOKEN", c.mcpAuthToken)
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
	processRegistry.Store(runID, &activeProcess{
		cmd:            cmd,
		stdout:         stdout,
		stderr:         &stderr,
		home:           codexHome,
		workdir:        workdir,
		codexSessionID: codexSessionID,
		baseline:       baseline,
	})
	return handle, nil
}

func (c *Client) Stop(_ context.Context, processRunID process.RunID) error {
	value, ok := processRegistry.Load(processRunID)
	if !ok {
		return ErrProcessNotFound
	}
	active := value.(*activeProcess)
	if active.cmd.Process == nil {
		processRegistry.Delete(processRunID)
		return ErrProcessNotFound
	}
	processRegistry.Delete(processRunID)
	pid := active.cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (c *Client) Events(ctx context.Context, handle process.CodexHandle) (<-chan process.CodexEvent, error) {
	value, ok := processRegistry.Load(handle.ProcessRunID)
	if !ok {
		return nil, ErrProcessNotFound
	}
	active := value.(*activeProcess)
	events := make(chan process.CodexEvent, 16)
	go func() {
		defer close(events)
		defer processRegistry.Delete(handle.ProcessRunID)
		defer active.stdout.Close()

		stdoutSessionIDs, drained := observeStdout(active.stdout)
		exited := make(chan process.ExitResult, 1)
		go func() {
			<-drained
			exited <- waitProcess(active.cmd, active.stderr)
		}()
		exitResult, readErr := tailSessionLog(ctx, active, events, exited, stdoutSessionIDs)
		sendProcessExit(ctx, events, exitResult, readErr)
	}()
	return events, nil
}

func observeStdout(stdout io.Reader) (<-chan string, <-chan struct{}) {
	sessionIDs := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(sessionIDs)
		defer close(done)
		reader := bufio.NewReader(stdout)
		identified := false
		for {
			line, err := reader.ReadBytes('\n')
			if !identified && len(line) > 0 {
				if sessionID := stdoutSessionID(bytes.TrimSpace(line)); sessionID != "" {
					sessionIDs <- sessionID
					identified = true
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return sessionIDs, done
}

func stdoutSessionID(raw []byte) string {
	var event map[string]any
	if json.Unmarshal(raw, &event) != nil || stringValue(event, "type") != "thread.started" {
		return ""
	}
	return stringValue(event, "thread_id", "session_id")
}

func tailSessionLog(ctx context.Context, active *activeProcess, events chan<- process.CodexEvent, exited <-chan process.ExitResult, stdoutSessionIDs <-chan string) (process.ExitResult, error) {
	path, err := waitForActiveSessionLog(ctx, active, stdoutSessionIDs)
	if err != nil {
		return failUnreadableProcess(active, exited, err), err
	}
	file, err := os.Open(path)
	if err != nil {
		err = fmt.Errorf("open codex session log: %w", err)
		return failUnreadableProcess(active, exited, err), err
	}
	defer file.Close()
	sessionCWD := active.workdir
	sourceID := filepath.Base(path)
	semanticState := newCodexSemanticState()
	skipLeadingLineTerminator := false
	if offset := active.baseline[path]; offset > 0 {
		var resumeOffset int64
		sessionCWD, resumeOffset, skipLeadingLineTerminator, err = primeCodexSemanticState(path, offset, sessionCWD, sourceID, semanticState)
		if err != nil {
			err = fmt.Errorf("prime codex session state: %w", err)
			return failUnreadableProcess(active, exited, err), err
		}
		if _, err := file.Seek(resumeOffset, io.SeekStart); err != nil {
			err = fmt.Errorf("seek codex session log: %w", err)
			return failUnreadableProcess(active, exited, err), err
		}
	}
	reader := bufio.NewReader(file)
	offset, _ := file.Seek(0, io.SeekCurrent)
	var exitResult process.ExitResult
	processExited := false
	var pending []byte
	var pendingOffset int64
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
			semanticState.apply(parsed)
			for _, event := range parsed {
				select {
				case events <- event:
				case <-ctx.Done():
					if !processExited {
						exitResult = waitForExit(exited)
					}
					return exitResult, ctx.Err()
				}
			}
			pending = nil
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
			return exitResult, nil
		}
		select {
		case <-ctx.Done():
			exitResult = waitForExit(exited)
			return exitResult, ctx.Err()
		case exitResult = <-exited:
			processExited = true
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func primeCodexSemanticState(path string, limit int64, sessionCWD string, sourceID string, state *codexSemanticState) (string, int64, bool, error) {
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
			state.apply(parsed)
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

func waitForActiveSessionLog(ctx context.Context, active *activeProcess, stdoutSessionIDs <-chan string) (string, error) {
	deadline := time.Now().Add(5 * time.Second)
	var last string
	allowWorkdirFallback := active.codexSessionID != ""
	for {
		if active.codexSessionID != "" || allowWorkdirFallback {
			path, err := activeSessionLog(active)
			if err == nil && path != "" {
				return path, nil
			}
			if err != nil {
				if errors.Is(err, errAmbiguousSessionLogs) {
					return "", fmt.Errorf("find codex session log: %w", err)
				}
				last = err.Error()
			}
		}
		if time.Now().After(deadline) {
			if last != "" {
				return "", fmt.Errorf("find codex session log: %s", last)
			}
			return "", errors.New("codex session log was not created")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case sessionID, ok := <-stdoutSessionIDs:
			if !ok {
				allowWorkdirFallback = true
				stdoutSessionIDs = nil
				continue
			}
			if sessionID != "" {
				active.codexSessionID = sessionID
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func activeSessionLog(active *activeProcess) (string, error) {
	if active.codexSessionID != "" {
		path, err := sessionLogByID(active.home, active.codexSessionID)
		if err != nil || path == "" {
			return path, err
		}
		if sessionLogAdvanced(path, active.baseline) {
			return path, nil
		}
		return "", nil
	}
	return activeSessionLogByWorkdir(active.home, active.workdir, active.baseline)
}

func latestSessionLog(codexHome string, workdir string) (string, error) {
	root := filepath.Join(codexHome, "sessions")
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		if workdir == "" {
			matches = append(matches, path)
			return nil
		}
		if sessionLogMatchesWorkdir(path, workdir) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	sortSessionLogsByModTime(matches)
	return matches[0], nil
}

func activeSessionLogByWorkdir(codexHome string, workdir string, baseline map[string]int64) (string, error) {
	root := filepath.Join(codexHome, "sessions")
	matches := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		if !sessionLogAdvanced(path, baseline) {
			return nil
		}
		if workdir == "" || sessionLogMatchesWorkdir(path, workdir) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("%w for workdir %q", errAmbiguousSessionLogs, workdir)
	}
	return matches[0], nil
}

func sortSessionLogsByModTime(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		left, leftErr := os.Stat(paths[i])
		right, rightErr := os.Stat(paths[j])
		if leftErr != nil || rightErr != nil {
			return paths[i] > paths[j]
		}
		if !left.ModTime().Equal(right.ModTime()) {
			return left.ModTime().After(right.ModTime())
		}
		return paths[i] > paths[j]
	})
}

func sessionLogOffsets(codexHome string) map[string]int64 {
	offsets := map[string]int64{}
	root := filepath.Join(codexHome, "sessions")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		offsets[path] = info.Size()
		return nil
	})
	return offsets
}

func sessionLogAdvanced(path string, baseline map[string]int64) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	offset, ok := baseline[path]
	return !ok || info.Size() > offset
}

func (c *Client) SessionEvents(ctx context.Context, input process.CodexTranscriptInput) ([]process.CodexEvent, error) {
	path, err := c.sessionLogPath(input)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open codex session log: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	events := []process.CodexEvent(nil)
	sessionCWD := input.Workdir
	sourceID := filepath.Base(path)
	semanticState := newCodexSemanticState()
	var offset int64
	for {
		if err := ctx.Err(); err != nil {
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
				semanticState.apply(parsed)
				events = append(events, parsed...)
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return nil, fmt.Errorf("read codex session log: %w", err)
	}
	return events, nil
}

func (c *Client) sessionLogPath(input process.CodexTranscriptInput) (string, error) {
	if input.CodexSessionID != "" {
		path, err := sessionLogByID(c.CodexHome(), input.CodexSessionID)
		if err != nil {
			return "", err
		}
		if path == "" {
			return "", fmt.Errorf("codex session log %q was not found", input.CodexSessionID)
		}
		return path, nil
	}
	if input.Workdir == "" {
		return "", errors.New("codex session id or workdir is required")
	}
	path, err := latestSessionLog(c.CodexHome(), input.Workdir)
	if err != nil {
		return "", fmt.Errorf("find codex session log: %w", err)
	}
	if path == "" {
		return "", errors.New("codex session log was not found")
	}
	return path, nil
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

func sessionLogMatchesWorkdir(path string, workdir string) bool {
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
		if cwd, ok := record.Payload["cwd"].(string); ok && cwd == workdir {
			return true
		}
		if err != nil {
			return false
		}
	}
	return false
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

type codexSemanticState struct {
	commands       map[string]process.CodexCommandContent
	lastOccurredAt time.Time
}

func newCodexSemanticState() *codexSemanticState {
	return &codexSemanticState{commands: map[string]process.CodexCommandContent{}}
}

func (s *codexSemanticState) apply(events []process.CodexEvent) {
	if s == nil {
		return
	}
	for index := range events {
		event := &events[index]
		if event.CreatedAt.IsZero() {
			if s.lastOccurredAt.IsZero() {
				event.CreatedAt = time.Unix(0, event.SourceOffset+int64(event.SourceIndex)+1).UTC()
			} else {
				event.CreatedAt = s.lastOccurredAt
			}
		}
		s.lastOccurredAt = event.CreatedAt
		if event.CorrelationID == "" {
			continue
		}
		if command, ok := event.Content.(process.CodexCommandContent); ok {
			if previous, exists := s.commands[event.CorrelationID]; exists && command.Command == "" {
				command.Command = previous.Command
			}
			event.Content = command
			if event.Phase == process.CodexPhaseStarted || event.Phase == process.CodexPhaseProgress {
				s.commands[event.CorrelationID] = command
			} else {
				delete(s.commands, event.CorrelationID)
			}
			continue
		}
		if tool, ok := event.Content.(process.CodexToolContent); ok && isTerminalCodexPhase(event.Phase) {
			if command, exists := s.commands[event.CorrelationID]; exists {
				item := mapValue(event.Payload["item"])
				normalized := mapValue(event.Payload["normalizedItem"])
				command.Output = normalizeANSIText(tool.Output.Text)
				command.ExitCode = intPointer(normalized["exitCode"], item["exit_code"], item["exitCode"])
				command.DurationMS = intPointer(normalized["durationMs"], item["duration_ms"], item["durationMs"])
				if command.ExitCode != nil && *command.ExitCode != 0 && event.Phase == process.CodexPhaseCompleted {
					event.Phase = process.CodexPhaseFailed
				}
				event.Content = command
				delete(s.commands, event.CorrelationID)
			}
		}
	}
}

func isTerminalCodexPhase(phase process.CodexPhase) bool {
	return phase == process.CodexPhaseCompleted || phase == process.CodexPhaseFailed || phase == process.CodexPhaseCancelled
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

	if event.Type == "token_count" {
		event.Content = codexUsageContent(event.Payload)
		return
	}
	if event.Type == "item.started" || event.Type == "item.completed" {
		applyCodexItemSemantic(event, itemType, item, normalized)
		return
	}
	if event.Type == "mcp_tool_call_end" {
		event.Phase = mcpToolPhase(event.Payload["result"])
		invocation := mapValue(event.Payload["invocation"])
		event.Content = process.CodexToolContent{
			QualifiedName: qualifiedInvocationName(invocation),
			Category:      "mcp",
			Output: process.CodexStructuredText{
				Format: process.CodexTextJSON,
				Text:   jsonText(event.Payload["result"]),
			},
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
		content := process.CodexCommandContent{Command: command, Output: normalizeANSIText(output), ExitCode: intPointer(normalized["exitCode"], item["exit_code"], item["exitCode"]), DurationMS: intPointer(normalized["durationMs"], item["duration_ms"], item["durationMs"])}
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
			if stringValue(entry, "type") != "input_image" {
				continue
			}
			source := stringValue(entry, "image_url")
			if source == "" {
				continue
			}
			images = append(images, process.CodexImage{Source: source, Detail: stringValue(entry, "detail")})
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
	return process.CodexUsageContent{
		InputTokens:           intValue(total["input_tokens"]),
		CachedInputTokens:     intValue(total["cached_input_tokens"]),
		OutputTokens:          intValue(total["output_tokens"]),
		ReasoningOutputTokens: intValue(total["reasoning_output_tokens"]),
		TotalTokens:           intValue(total["total_tokens"]),
		ContextWindow:         intValue(info["model_context_window"]),
	}
}

func isCodexStatusType(eventType string) bool {
	switch eventType {
	case "thread.started", "task.started", "task.completed", "turn.started", "turn.completed", "turn.aborted", "context.compacted", "turn.context", "world.state", "process.exit", "error", "invalid_json":
		return true
	default:
		return false
	}
}

func codexStatusContent(code string, payload map[string]any) process.CodexStatusContent {
	level := "info"
	if code == "error" || code == "invalid_json" || (code == "process.exit" && intValue(payload["exitCode"]) != 0) {
		level = "error"
	} else if code == "turn.aborted" || code == "context.compacted" {
		level = "warning"
	}
	return process.CodexStatusContent{
		Code:    code,
		Level:   level,
		Message: firstString(payload["message"], payload["reason"], payload["failureReason"]),
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
		if command != "" {
			itemType = "command_execution"
		}
		normalized := normalizedItem(itemType, "in_progress")
		normalized["qualifiedName"] = qualifiedToolName(payload)
		normalized["input"] = stringOrJSON(payload["arguments"])
		normalized["command"] = command
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
		normalized := normalizedItem("custom_tool_call", "in_progress")
		normalized["qualifiedName"] = stringValue(payload, "name")
		normalized["input"] = stringOrJSON(payload["input"])
		return []process.CodexEvent{{
			EventID:   eventID(timestamp, "item.started", callID),
			Type:      "item.started",
			Payload:   itemEventPayload(payload, normalized),
			CreatedAt: createdAt,
		}}
	case "custom_tool_call_output":
		callID := stringValue(payload, "call_id")
		normalized := normalizedItem("custom_tool_call", "completed")
		normalized["output"] = textFromValue(payload["output"])
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
	case "user_message", "agent_message", "context_compacted", "web_search_end":
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

func commandFromFunctionArguments(payload map[string]any) string {
	arguments := stringValue(payload, "arguments")
	if arguments == "" {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(arguments), &parsed); err != nil {
		return arguments
	}
	return stringValue(parsed, "cmd", "command")
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
	args = c.appendMCPArgs(args, input.SessionID)
	args = c.appendConfigArgs(args, input.Model, input.ReasoningEffort, input.PermissionMode)
	for _, path := range input.ImagePaths {
		if path != "" {
			args = append(args, "-i", path)
		}
	}
	if input.Prompt != "" {
		args = append(args, input.Prompt)
	}
	return args
}

func (c *Client) buildResumeArgs(input process.CodexResumeInput) []string {
	args := []string{"exec", "resume", "--json", "--skip-git-repo-check"}
	args = c.appendMCPArgs(args, input.SessionID)
	args = appendResumeConfigArgs(args, input.Model, input.ReasoningEffort)
	if input.CodexSessionID != "" {
		args = append(args, input.CodexSessionID)
	}
	if input.Prompt != "" {
		args = append(args, input.Prompt)
	}
	return args
}

func appendResumeConfigArgs(args []string, model string, reasoningEffort string) []string {
	if model != "" {
		args = append(args, "-m", model)
	}
	if reasoningEffort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", reasoningEffort))
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

func (c *Client) appendConfigArgs(args []string, model string, reasoningEffort string, permissionMode string) []string {
	if model != "" {
		args = append(args, "-m", model)
	}
	if reasoningEffort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", reasoningEffort))
	}
	if permissionMode != "" {
		if c != nil && c.mcpStdioSocket != "" {
			if profile, ok := mcpPermissionProfile(permissionMode); ok {
				return appendMCPPermissionProfileArgs(args, profile, c.mcpStdioSocket)
			}
		}
		args = append(args, "--sandbox", permissionMode)
	}
	return args
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
