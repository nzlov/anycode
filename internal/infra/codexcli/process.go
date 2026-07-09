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

var ErrProcessNotFound = errors.New("codex process run is not active")

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

		drained := drainStdout(active.stdout)
		exited := make(chan process.ExitResult, 1)
		go func() {
			<-drained
			exited <- waitProcess(active.cmd, active.stderr)
		}()
		exitResult, readErr := tailSessionLog(ctx, active, events, exited)
		sendProcessExit(ctx, events, exitResult, readErr)
	}()
	return events, nil
}

func drainStdout(stdout io.Reader) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, stdout)
	}()
	return done
}

func tailSessionLog(ctx context.Context, active *activeProcess, events chan<- process.CodexEvent, exited <-chan process.ExitResult) (process.ExitResult, error) {
	path, err := waitForActiveSessionLog(ctx, active)
	if err != nil {
		return failUnreadableProcess(active, exited, err), err
	}
	file, err := os.Open(path)
	if err != nil {
		err = fmt.Errorf("open codex session log: %w", err)
		return failUnreadableProcess(active, exited, err), err
	}
	defer file.Close()
	if offset := active.baseline[path]; offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			err = fmt.Errorf("seek codex session log: %w", err)
			return failUnreadableProcess(active, exited, err), err
		}
	}
	reader := bufio.NewReader(file)
	sessionCWD := active.workdir
	sourceID := filepath.Base(path)
	offset, _ := file.Seek(0, io.SeekCurrent)
	var exitResult process.ExitResult
	processExited := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineOffset := offset
			offset += int64(len(line))
			raw := bytes.TrimRight(line, "\r\n")
			raw = append([]byte(nil), raw...)
			if cwd := sessionCWDFromMeta(raw); cwd != "" {
				sessionCWD = cwd
			}
			for _, event := range parseSessionLogLine(raw, sessionCWD, sourceID, lineOffset) {
				select {
				case events <- event:
				case <-ctx.Done():
					if !processExited {
						exitResult = waitForExit(exited)
					}
					return exitResult, ctx.Err()
				}
			}
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

func waitForActiveSessionLog(ctx context.Context, active *activeProcess) (string, error) {
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for {
		path, err := activeSessionLog(active)
		if err == nil && path != "" {
			return path, nil
		}
		if err != nil {
			last = err.Error()
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
	sortSessionLogsByModTimeAscending(matches)
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

func sortSessionLogsByModTimeAscending(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		left, leftErr := os.Stat(paths[i])
		right, rightErr := os.Stat(paths[j])
		if leftErr != nil || rightErr != nil {
			return paths[i] < paths[j]
		}
		if !left.ModTime().Equal(right.ModTime()) {
			return left.ModTime().Before(right.ModTime())
		}
		return paths[i] < paths[j]
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
			if cwd := sessionCWDFromMeta(raw); cwd != "" {
				sessionCWD = cwd
			}
			events = append(events, parseSessionLogLine(raw, sessionCWD, sourceID, lineOffset)...)
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
	select {
	case events <- process.CodexEvent{Type: "process.exit", Payload: payload}:
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
		return []process.CodexEvent{{
			EventID: sourceEventID("invalid_json", sourceID, offset),
			Type:    "invalid_json",
			Payload: map[string]any{"error": err.Error(), "byteCount": len(raw)},
		}}
	}
	payload := payloadOrEmpty(record.Payload)
	createdAt := parseSessionTimestamp(record.Timestamp)
	var events []process.CodexEvent
	switch record.Type {
	case "session_meta":
		threadID := stringValue(payload, "session_id", "id")
		events = []process.CodexEvent{{
			EventID: eventID(record.Timestamp, "thread.started", threadID),
			Type:    "thread.started",
			Payload: map[string]any{
				"thread_id":  threadID,
				"session_id": threadID,
			},
			CreatedAt: createdAt,
		}}
	case "response_item":
		events = codexEventsFromResponseItem(record.Timestamp, payload, createdAt)
	case "event_msg":
		events = codexEventsFromEventMessage(record.Timestamp, payload, createdAt, sessionCWD)
	}
	for index := range events {
		if events[index].EventID == "" {
			events[index].EventID = sourceEventID(events[index].Type, sourceID, offset)
		}
	}
	return events
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
	switch stringValue(payload, "type") {
	case "function_call":
		callID := stringValue(payload, "call_id")
		return []process.CodexEvent{{
			EventID: eventID(timestamp, "item.started", callID),
			Type:    "item.started",
			Payload: map[string]any{
				"item": map[string]any{
					"id":      callID,
					"type":    "command_execution",
					"command": commandFromFunctionArguments(payload),
					"status":  "in_progress",
				},
			},
			CreatedAt: createdAt,
		}}
	case "function_call_output":
		callID := stringValue(payload, "call_id")
		return []process.CodexEvent{{
			EventID: eventID(timestamp, "item.completed", callID),
			Type:    "item.completed",
			Payload: map[string]any{
				"item": map[string]any{
					"id":                callID,
					"type":              "command_execution",
					"aggregated_output": stringValue(payload, "output"),
					"status":            "completed",
				},
			},
			CreatedAt: createdAt,
		}}
	case "message":
		if stringValue(payload, "role") == "assistant" {
			id := stringValue(payload, "id")
			return []process.CodexEvent{{
				EventID: eventID(timestamp, "item.completed", id),
				Type:    "item.completed",
				Payload: map[string]any{
					"item": map[string]any{
						"id":                id,
						"type":              "agent_message",
						"aggregated_output": messageText(payload),
						"status":            "completed",
					},
				},
				CreatedAt: createdAt,
			}}
		}
	case "reasoning":
		return []process.CodexEvent{{
			EventID: eventID(timestamp, "item.completed", stringValue(payload, "id")),
			Type:    "item.completed",
			Payload: map[string]any{
				"item": map[string]any{
					"id":                stringValue(payload, "id"),
					"type":              "reasoning",
					"aggregated_output": reasoningText(payload),
					"status":            "completed",
				},
			},
			CreatedAt: createdAt,
		}}
	}
	return nil
}

func codexEventsFromEventMessage(timestamp string, payload map[string]any, createdAt time.Time, sessionCWD string) []process.CodexEvent {
	switch stringValue(payload, "type") {
	case "patch_apply_end":
		callID := stringValue(payload, "call_id")
		return []process.CodexEvent{{
			EventID: eventID(timestamp, "item.completed", callID),
			Type:    "item.completed",
			Payload: map[string]any{
				"item": map[string]any{
					"id":      callID,
					"type":    "file_change",
					"changes": fileChangesFromPatch(payload, sessionCWD),
					"status":  stringValue(payload, "status"),
				},
			},
			CreatedAt: createdAt,
		}}
	case "agent_message":
		return []process.CodexEvent{{
			Type: "item.completed",
			Payload: map[string]any{
				"item": map[string]any{
					"type":              "agent_message",
					"aggregated_output": stringValue(payload, "message"),
					"status":            "completed",
				},
			},
			CreatedAt: createdAt,
		}}
	}
	return nil
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
	return filepath.Base(path)
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
	args := []string{"exec", "--skip-git-repo-check"}
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
	args := []string{"exec", "resume", "--skip-git-repo-check"}
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
