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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

const (
	defaultHistoryPageLimit = 50
	maxHistoryPageLimit     = 500
	historyProjectionWindow = 128
	reverseReadBlock        = 64 * 1024
	sessionPollInterval     = 50 * time.Millisecond
)

type sessionLogLine struct {
	offset int64
	raw    []byte
}

func (c *Client) HistoryPage(ctx context.Context, input process.CodexHistoryPageInput) (process.CodexHistoryPage, error) {
	threadID := strings.TrimSpace(input.ThreadID)
	if threadID == "" {
		return process.CodexHistoryPage{}, process.ErrThreadUnavailable
	}
	path, err := sessionLogByID(c.CodexHome(), threadID)
	if err != nil {
		return process.CodexHistoryPage{}, err
	}
	if path == "" {
		return process.CodexHistoryPage{}, process.ErrThreadUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return process.CodexHistoryPage{}, fmt.Errorf("open codex session log: %w", err)
	}
	defer file.Close()

	beforeOffset := int64(0)
	if input.Cursor != "" {
		beforeOffset, err = strconv.ParseInt(input.Cursor, 10, 64)
		if err != nil || beforeOffset < 0 {
			return process.CodexHistoryPage{}, errors.New("invalid codex history cursor")
		}
	}
	limit := input.Limit
	if limit < 1 {
		limit = defaultHistoryPageLimit
	}
	if limit > maxHistoryPageLimit {
		limit = maxHistoryPageLimit
	}
	lines, _, err := readSessionLinesBackward(ctx, file, beforeOffset, limit+historyProjectionWindow)
	if err != nil {
		return process.CodexHistoryPage{}, err
	}
	if len(lines) == 0 {
		return process.CodexHistoryPage{}, nil
	}
	pageStart := len(lines) - limit
	if pageStart < 0 {
		pageStart = 0
	}
	startOffset := lines[pageStart].offset
	sessionCWD := sessionLogCWD(path)
	sourceID := filepath.Base(path)
	projector := newCodexTranscriptProjector()
	events := make([]process.CodexEvent, 0, limit)
	for _, line := range lines {
		for _, rawEvent := range projector.project(parseSessionLogLine(line.raw, sessionCWD, sourceID, line.offset)) {
			if rawEvent.SourceOffset < startOffset || rawEvent.Type == "thread.started" {
				continue
			}
			event := canonicalCodexEvent(rawEvent)
			event.CodexSessionID = threadID
			events = append(events, event)
		}
	}
	for _, rawEvent := range projector.flushPending() {
		if rawEvent.SourceOffset < startOffset || rawEvent.Type == "thread.started" {
			continue
		}
		event := canonicalCodexEvent(rawEvent)
		event.CodexSessionID = threadID
		events = append(events, event)
	}
	nextCursor := ""
	if startOffset > 0 {
		nextCursor = strconv.FormatInt(startOffset, 10)
	}
	return process.CodexHistoryPage{Events: events, NextCursor: nextCursor}, nil
}

func (r *appServerRuntime) followSessionLog(route *appServerRun, path string, startOffset int64) {
	var finished *process.ExitResult
	finishedChannel := route.finished
	if path == "" {
		var err error
		path, finished, err = waitForRouteSessionLog(route, r.client.CodexHome())
		if err != nil {
			result := process.ExitResult{FailureCode: "codex_transcript_unavailable", FailureReason: err.Error(), FinishedAt: time.Now()}
			if finished != nil {
				result = *finished
				result.FailureCode = "codex_transcript_unavailable"
				result.FailureReason = err.Error()
			}
			r.finishRouteFromTranscript(route, result)
			return
		}
		if finished != nil {
			finishedChannel = nil
		}
	}
	file, err := os.Open(path)
	if err != nil {
		r.finishRouteFromTranscript(route, process.ExitResult{
			FailureCode: "codex_transcript_unavailable", FailureReason: "open codex session log: " + err.Error(), FinishedAt: time.Now(),
		})
		return
	}
	defer file.Close()
	if _, err := file.Seek(startOffset, io.SeekStart); err != nil {
		r.finishRouteFromTranscript(route, process.ExitResult{
			FailureCode: "codex_transcript_unavailable", FailureReason: "seek codex session log: " + err.Error(), FinishedAt: time.Now(),
		})
		return
	}

	reader := bufio.NewReader(file)
	offset := startOffset
	skipLeadingLineTerminator := false
	if startOffset > 0 {
		var previous [1]byte
		if _, err := file.ReadAt(previous[:], startOffset-1); err == nil {
			skipLeadingLineTerminator = previous[0] != '\n'
		}
	}
	sessionCWD := sessionLogCWD(path)
	sourceID := filepath.Base(path)
	projector := newCodexTranscriptProjector()
	var pending []byte
	var pendingOffset int64
	stableEOF := 0

	emitProjected := func(projected []codexLogEvent) {
		for _, rawEvent := range projected {
			if rawEvent.Type == "thread.started" {
				continue
			}
			route.emit(canonicalCodexEvent(rawEvent))
			if finished == nil && (rawEvent.Type == "task.completed" || rawEvent.Type == "turn.aborted") {
				result := process.ExitResult{FinishedAt: rawEvent.CreatedAt}
				if result.FinishedAt.IsZero() {
					result.FinishedAt = time.Now()
				}
				if rawEvent.Type == "turn.aborted" {
					result.FailureCode = "turn_interrupted"
				}
				finished = &result
			}
		}
	}
	for {
		line, readErr := reader.ReadBytes('\n')
		if skipLeadingLineTerminator && (bytes.Equal(line, []byte{'\n'}) || bytes.Equal(line, []byte{'\r', '\n'})) {
			offset += int64(len(line))
			skipLeadingLineTerminator = false
			continue
		}
		skipLeadingLineTerminator = false
		if len(line) > 0 {
			stableEOF = 0
			if len(pending) == 0 {
				pendingOffset = offset
			}
			offset += int64(len(line))
			pending = append(pending, line...)
		}
		if len(pending) > 0 && (readErr == nil || finished != nil && errors.Is(readErr, io.EOF) && json.Valid(bytes.TrimSpace(pending))) {
			raw := bytes.TrimRight(pending, "\r\n")
			if cwd := sessionCWDFromMeta(raw); cwd != "" {
				sessionCWD = cwd
			}
			emitProjected(projector.project(parseSessionLogLine(raw, sessionCWD, sourceID, pendingOffset)))
			pending = nil
		}
		if readErr == nil {
			continue
		}
		if !errors.Is(readErr, io.EOF) {
			r.finishRouteFromTranscript(route, process.ExitResult{
				FailureCode: "codex_transcript_read_failed", FailureReason: "read codex session log: " + readErr.Error(), FinishedAt: time.Now(),
			})
			return
		}
		emitProjected(projector.flushExpiredPending(time.Now()))
		if finished != nil {
			stableEOF++
			if stableEOF >= 3 {
				emitProjected(projector.flushPending())
				r.finishRouteFromTranscript(route, *finished)
				return
			}
		}
		timer := time.NewTimer(sessionPollInterval)
		select {
		case <-route.ctx.Done():
			timer.Stop()
			return
		case result := <-finishedChannel:
			timer.Stop()
			finished = &result
			finishedChannel = nil
		case <-timer.C:
		}
	}
}

func waitForRouteSessionLog(route *appServerRun, codexHome string) (string, *process.ExitResult, error) {
	var finished *process.ExitResult
	for {
		path, err := sessionLogByID(codexHome, route.handle.CodexSessionID)
		if err != nil {
			return "", finished, err
		}
		if path != "" {
			return path, finished, nil
		}
		if finished != nil {
			return "", finished, errors.New("codex session log was not created")
		}
		timer := time.NewTimer(sessionPollInterval)
		select {
		case <-route.ctx.Done():
			timer.Stop()
			return "", finished, route.ctx.Err()
		case result := <-route.finished:
			timer.Stop()
			finished = &result
		case <-timer.C:
		}
	}
}

func (r *appServerRuntime) finishRouteFromTranscript(route *appServerRun, result process.ExitResult) {
	if result.FinishedAt.IsZero() {
		result.FinishedAt = time.Now()
	}
	route.emit(process.CodexEvent{
		EventID: "exit:" + route.activeTurnID(), Type: process.CodexEventProcessExit,
		Content: result, CreatedAt: result.FinishedAt,
	})
	r.completeRoute(route)
}

func waitForSessionLog(ctx context.Context, codexHome string, threadID string) (string, error) {
	for {
		path, err := sessionLogByID(codexHome, threadID)
		if err != nil {
			return "", err
		}
		if path != "" {
			return path, nil
		}
		timer := time.NewTimer(sessionPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

func sessionLogByID(codexHome string, threadID string) (string, error) {
	root := filepath.Join(codexHome, "sessions")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("stat codex sessions: %w", err)
	}
	var matched string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		if sessionLogMatchesID(path, threadID) {
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

func sessionLogMatchesID(path string, threadID string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for index := 0; index < 20; index++ {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && readErr != nil {
			return false
		}
		var record struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if json.Unmarshal(bytes.TrimSpace(line), &record) == nil && record.Type == "session_meta" {
			return stringValue(record.Payload, "session_id", "id") == threadID
		}
		if readErr != nil {
			return false
		}
	}
	return false
}

func sessionLogCWD(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for index := 0; index < 20; index++ {
		line, readErr := reader.ReadBytes('\n')
		if cwd := sessionCWDFromMeta(bytes.TrimSpace(line)); cwd != "" {
			return cwd
		}
		if readErr != nil {
			return ""
		}
	}
	return ""
}

func readSessionLinesBackward(ctx context.Context, file *os.File, beforeOffset int64, keep int) ([]sessionLogLine, int64, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("read codex session log metadata: %w", err)
	}
	endOffset := beforeOffset
	if endOffset <= 0 || endOffset > info.Size() {
		endOffset = info.Size()
	}
	endOffset, err = alignSessionOffset(file, endOffset, info.Size())
	if err != nil {
		return nil, 0, err
	}
	if endOffset == 0 || keep < 1 {
		return nil, endOffset, nil
	}

	position := endOffset
	newlineCount := 0
	chunks := make([][]byte, 0, 2)
	for position > 0 && newlineCount <= keep {
		if err := ctx.Err(); err != nil {
			return nil, endOffset, err
		}
		start := position - reverseReadBlock
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, position-start)
		read, readErr := file.ReadAt(chunk, start)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, endOffset, fmt.Errorf("read codex session log page: %w", readErr)
		}
		chunk = chunk[:read]
		newlineCount += bytes.Count(chunk, []byte{'\n'})
		chunks = append(chunks, chunk)
		position = start
	}
	windowSize := 0
	for _, chunk := range chunks {
		windowSize += len(chunk)
	}
	window := make([]byte, 0, windowSize)
	for index := len(chunks) - 1; index >= 0; index-- {
		window = append(window, chunks[index]...)
	}
	windowOffset := position
	if position > 0 {
		var previous [1]byte
		if _, err := file.ReadAt(previous[:], position-1); err != nil {
			return nil, endOffset, fmt.Errorf("align codex session log page start: %w", err)
		}
		if previous[0] != '\n' {
			newline := bytes.IndexByte(window, '\n')
			if newline < 0 {
				return nil, endOffset, nil
			}
			window = window[newline+1:]
			windowOffset += int64(newline + 1)
		}
	}
	lines := sessionLinesFromWindow(window, windowOffset)
	if len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	return lines, endOffset, nil
}

func alignSessionOffset(file *os.File, offset int64, size int64) (int64, error) {
	if offset <= 0 || offset >= size {
		return offset, nil
	}
	var previous [1]byte
	if _, err := file.ReadAt(previous[:], offset-1); err != nil {
		return 0, fmt.Errorf("align codex history cursor: %w", err)
	}
	if previous[0] == '\n' {
		return offset, nil
	}
	position := offset
	for position > 0 {
		start := position - reverseReadBlock
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, position-start)
		read, readErr := file.ReadAt(chunk, start)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return 0, fmt.Errorf("align codex history cursor: %w", readErr)
		}
		if newline := bytes.LastIndexByte(chunk[:read], '\n'); newline >= 0 {
			return start + int64(newline) + 1, nil
		}
		position = start
	}
	return 0, nil
}

func sessionLinesFromWindow(window []byte, windowOffset int64) []sessionLogLine {
	lines := make([]sessionLogLine, 0, bytes.Count(window, []byte{'\n'})+1)
	for len(window) > 0 {
		newline := bytes.IndexByte(window, '\n')
		if newline < 0 {
			raw := bytes.TrimSuffix(window, []byte{'\r'})
			if json.Valid(raw) {
				lines = append(lines, sessionLogLine{offset: windowOffset, raw: append([]byte(nil), raw...)})
			}
			break
		}
		raw := bytes.TrimSuffix(window[:newline], []byte{'\r'})
		lines = append(lines, sessionLogLine{offset: windowOffset, raw: append([]byte(nil), raw...)})
		window = window[newline+1:]
		windowOffset += int64(newline + 1)
	}
	return lines
}
