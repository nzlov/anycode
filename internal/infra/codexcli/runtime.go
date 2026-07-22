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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

type appServerEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *appServerError `json:"error"`
}

type appServerError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type appServerRuntime struct {
	client *Client
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *bytes.Buffer
	done   chan struct{}

	writeMu sync.Mutex
	nextID  atomic.Int64

	pendingMu sync.Mutex
	pending   map[string]chan appServerEnvelope

	routesMu sync.Mutex
	routes   map[process.RunID]*appServerRun
	threads  map[string]*appServerRun

	userAgent string
	exitErr   error
	closeOnce sync.Once
}

type appServerRun struct {
	handle    process.CodexHandle
	sessionID process.SessionID
	workdir   string
	ctx       context.Context
	cancel    context.CancelFunc
	events    chan process.CodexEvent
	sequence  atomic.Int64
	turnMu    sync.RWMutex
	turnID    string
	claimed   bool
	closed    chan struct{}
	closeOnce sync.Once
}

func startAppServerRuntime(ctx context.Context, client *Client) (*appServerRuntime, error) {
	cmd := exec.Command(client.Bin(), "app-server", "--stdio")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	env := os.Environ()
	if codexHome := client.CodexHome(); codexHome != "" {
		env = upsertEnv(env, "CODEX_HOME", codexHome)
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	runtime := &appServerRuntime{
		client: client, cmd: cmd, stdin: stdin, stderr: stderr, done: make(chan struct{}),
		pending: map[string]chan appServerEnvelope{}, routes: map[process.RunID]*appServerRun{}, threads: map[string]*appServerRun{},
	}
	go runtime.readLoop(stdout)
	var initialized struct {
		UserAgent string `json:"userAgent"`
	}
	if err := runtime.request(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "anycode", "title": "AnyCode", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true},
	}, &initialized); err != nil {
		_ = runtime.close()
		return nil, fmt.Errorf("initialize codex app-server: %w", err)
	}
	runtime.userAgent = initialized.UserAgent
	if err := runtime.notify("initialized", nil); err != nil {
		_ = runtime.close()
		return nil, fmt.Errorf("acknowledge codex app-server initialization: %w", err)
	}
	return runtime, nil
}

func (r *appServerRuntime) alive() bool {
	if r == nil {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

func (r *appServerRuntime) request(ctx context.Context, method string, params any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := r.nextID.Add(1)
	key := strconv.FormatInt(id, 10)
	response := make(chan appServerEnvelope, 1)
	r.pendingMu.Lock()
	r.pending[key] = response
	r.pendingMu.Unlock()
	defer func() {
		r.pendingMu.Lock()
		delete(r.pending, key)
		r.pendingMu.Unlock()
	}()
	if err := r.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case envelope := <-response:
		if envelope.Error != nil {
			return fmt.Errorf("app-server %s failed (%d): %s", method, envelope.Error.Code, envelope.Error.Message)
		}
		if result == nil || len(envelope.Result) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Result), []byte("null")) {
			return nil
		}
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode app-server %s response: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		return r.runtimeError()
	}
}

func (r *appServerRuntime) notify(method string, params any) error {
	return r.write(map[string]any{"method": method, "params": params})
}

func (r *appServerRuntime) write(message any) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	select {
	case <-r.done:
		return r.runtimeError()
	default:
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = r.stdin.Write(encoded)
	return err
}

func (r *appServerRuntime) readLoop(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var envelope appServerEnvelope
			if decodeErr := json.Unmarshal(line, &envelope); decodeErr != nil {
				r.finish(fmt.Errorf("decode app-server message: %w", decodeErr))
				return
			}
			r.dispatch(envelope)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				r.finish(err)
			} else {
				r.finish(nil)
			}
			return
		}
	}
}

func (r *appServerRuntime) dispatch(envelope appServerEnvelope) {
	if len(envelope.ID) > 0 && envelope.Method == "" {
		key := string(bytes.TrimSpace(envelope.ID))
		r.pendingMu.Lock()
		response := r.pending[key]
		r.pendingMu.Unlock()
		if response != nil {
			response <- envelope
		}
		return
	}
	if len(envelope.ID) > 0 {
		go r.handleServerRequest(envelope)
		return
	}
	r.handleNotification(envelope.Method, envelope.Params)
}

func (r *appServerRuntime) handleServerRequest(envelope appServerEnvelope) {
	if envelope.Method != "item/tool/call" {
		_ = r.write(map[string]any{"id": envelope.ID, "error": map[string]any{"code": -32601, "message": "method not supported"}})
		return
	}
	var params struct {
		Arguments json.RawMessage `json:"arguments"`
		CallID    string          `json:"callId"`
		ThreadID  string          `json:"threadId"`
		Tool      string          `json:"tool"`
		TurnID    string          `json:"turnId"`
	}
	if err := json.Unmarshal(envelope.Params, &params); err != nil {
		_ = r.write(map[string]any{"id": envelope.ID, "error": map[string]any{"code": -32602, "message": "invalid dynamic tool request"}})
		return
	}
	route := r.routeForThread(params.ThreadID)
	if route == nil {
		_ = r.write(map[string]any{"id": envelope.ID, "error": map[string]any{"code": -32000, "message": "dynamic tool thread is not active"}})
		return
	}
	handler := r.client.dynamicToolHandler()
	if handler == nil {
		_ = r.write(map[string]any{"id": envelope.ID, "result": dynamicToolFailure("dynamic tool handler is unavailable")})
		return
	}
	result, err := handler.HandleDynamicTool(route.ctx, process.DynamicToolCall{
		ProcessRunID: route.handle.ProcessRunID, SessionID: route.sessionID, ThreadID: params.ThreadID,
		TurnID: params.TurnID, CallID: params.CallID, Tool: params.Tool, Arguments: params.Arguments,
	})
	if err != nil {
		result = process.DynamicToolResult{Success: false, Content: []process.DynamicToolContent{{Type: "inputText", Text: err.Error()}}}
	}
	_ = r.write(map[string]any{"id": envelope.ID, "result": dynamicToolResponse(result)})
}

func dynamicToolFailure(message string) map[string]any {
	return map[string]any{"success": false, "contentItems": []map[string]any{{"type": "inputText", "text": message}}}
}

func dynamicToolResponse(result process.DynamicToolResult) map[string]any {
	items := make([]map[string]any, 0, len(result.Content))
	for _, item := range result.Content {
		switch item.Type {
		case "inputImage":
			items = append(items, map[string]any{"type": item.Type, "imageUrl": item.ImageURL})
		case "inputAudio":
			items = append(items, map[string]any{"type": item.Type, "audioUrl": item.AudioURL})
		default:
			items = append(items, map[string]any{"type": "inputText", "text": item.Text})
		}
	}
	return map[string]any{"success": result.Success, "contentItems": items}
}

func (r *appServerRuntime) finish(readErr error) {
	r.closeOnce.Do(func() {
		waitErr := r.cmd.Wait()
		if readErr == nil && waitErr != nil {
			readErr = waitErr
		}
		if message := strings.TrimSpace(r.stderr.String()); message != "" && readErr != nil {
			readErr = fmt.Errorf("%w: %s", readErr, message)
		}
		r.exitErr = readErr
		close(r.done)
		r.failRoutes(readErr)
	})
}

func (r *appServerRuntime) failRoutes(cause error) {
	r.routesMu.Lock()
	routes := make([]*appServerRun, 0, len(r.threads))
	for _, route := range r.threads {
		routes = append(routes, route)
	}
	r.threads = map[string]*appServerRun{}
	r.routesMu.Unlock()
	for _, route := range routes {
		reason := "codex app-server exited"
		if cause != nil {
			reason += ": " + cause.Error()
		}
		route.emit(process.CodexEvent{Type: process.CodexEventProcessExit, Content: process.ExitResult{
			FailureCode: "app_server_exited", FailureReason: reason, FinishedAt: time.Now(),
		}, CreatedAt: time.Now()})
		route.close()
	}
}

func (r *appServerRuntime) runtimeError() error {
	if r.exitErr != nil {
		return r.exitErr
	}
	return errors.New("codex app-server is not running")
}

func (r *appServerRuntime) close() error {
	if r == nil {
		return nil
	}
	if r.alive() {
		_ = r.stdin.Close()
		select {
		case <-r.done:
		case <-time.After(2 * time.Second):
			if r.cmd.Process != nil {
				_ = syscall.Kill(-r.cmd.Process.Pid, syscall.SIGKILL)
			}
			<-r.done
		}
	}
	return r.exitErr
}

func (r *appServerRuntime) register(route *appServerRun) {
	r.routesMu.Lock()
	r.routes[route.handle.ProcessRunID] = route
	r.threads[route.handle.CodexSessionID] = route
	r.routesMu.Unlock()
}

func (r *appServerRuntime) routeForThread(threadID string) *appServerRun {
	r.routesMu.Lock()
	defer r.routesMu.Unlock()
	return r.threads[threadID]
}

func (r *appServerRuntime) routeForRun(runID process.RunID) *appServerRun {
	r.routesMu.Lock()
	defer r.routesMu.Unlock()
	route := r.routes[runID]
	if route == nil || route.isClosed() {
		return nil
	}
	return route
}

func (r *appServerRuntime) completeRoute(route *appServerRun) {
	route.close()
	r.routesMu.Lock()
	if r.threads[route.handle.CodexSessionID] == route {
		delete(r.threads, route.handle.CodexSessionID)
	}
	if route.claimed {
		delete(r.routes, route.handle.ProcessRunID)
	}
	r.routesMu.Unlock()
}

func (r *appServerRuntime) claimEvents(runID process.RunID) (<-chan process.CodexEvent, bool) {
	r.routesMu.Lock()
	route := r.routes[runID]
	if route == nil || route.claimed {
		r.routesMu.Unlock()
		return nil, false
	}
	route.claimed = true
	if route.isClosed() {
		delete(r.routes, runID)
	}
	r.routesMu.Unlock()
	return route.events, true
}

func (r *appServerRun) emit(event process.CodexEvent) {
	event.SessionID = r.sessionID
	event.ProcessRunID = r.handle.ProcessRunID
	event.CodexSessionID = r.handle.CodexSessionID
	if event.TurnID == "" {
		event.TurnID = r.activeTurnID()
	}
	if event.Sequence == 0 {
		event.Sequence = r.sequence.Add(1)
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	select {
	case r.events <- event:
	case <-r.closed:
	}
}

func (r *appServerRun) close() {
	r.closeOnce.Do(func() {
		r.cancel()
		close(r.closed)
		close(r.events)
	})
}

func (r *appServerRun) setTurnID(turnID string) {
	r.turnMu.Lock()
	r.turnID = turnID
	r.turnMu.Unlock()
}

func (r *appServerRun) activeTurnID() string {
	r.turnMu.RLock()
	defer r.turnMu.RUnlock()
	return r.turnID
}

func (r *appServerRun) isClosed() bool {
	select {
	case <-r.closed:
		return true
	default:
		return false
	}
}

func upsertEnv(env []string, key string, value string) []string {
	prefix := key + "="
	for index, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[index] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
