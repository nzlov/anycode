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
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/nzlov/anycode/internal/domain/process"
)

type appServerRunInput struct {
	processRunID    process.RunID
	sessionID       process.SessionID
	codexSessionID  string
	transcript      process.CodexTranscriptSource
	workdir         string
	artifactDir     string
	prompt          string
	model           string
	reasoningEffort string
	permissionMode  string
	fastMode        bool
	imagePaths      []string
}

type appServerConnection struct {
	reader  *bufio.Reader
	writer  io.Writer
	nextID  int
	pending [][]byte
}

type appServerEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

type appServerTerminal string

const (
	appServerTurnTerminal    appServerTerminal = "turn/completed"
	appServerCompactTerminal appServerTerminal = "thread/compacted"
	maxFileMatches                             = 100
)

func (c *Client) SlashCommands() []process.CodexSlashCommand {
	return []process.CodexSlashCommand{
		{Name: "/review", Description: "审查当前工作区变更", AcceptsArgs: true},
		{Name: "/compact", Description: "压缩当前会话上下文", RequiresThread: true},
		{Name: "/goal", Description: "设置当前会话目标：/goal <目标>", AcceptsArgs: true, RequiresThread: true},
		{Name: "/plan", Description: "以计划模式处理：/plan <任务>", AcceptsArgs: true},
	}
}

func (c *Client) SearchFiles(ctx context.Context, root string, query string) ([]process.CodexFileMatch, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("file search root is required")
	}
	cmd := exec.CommandContext(ctx, c.Bin(), "app-server", "--stdio")
	cmd.Dir = root
	env := os.Environ()
	if codexHome := c.CodexHome(); codexHome != "" {
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
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	connection := &appServerConnection{reader: bufio.NewReader(stdout), writer: stdin}
	finish := func(requestErr error) error {
		_ = stdin.Close()
		_, _ = io.Copy(io.Discard, connection.reader)
		waitErr := cmd.Wait()
		if requestErr != nil {
			return requestErr
		}
		if waitErr != nil {
			return commandError(waitErr, stderr.String())
		}
		return nil
	}
	if err := connection.request("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "anycode", "title": "AnyCode", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true},
	}, nil); err != nil {
		return nil, finish(fmt.Errorf("initialize codex file search: %w", err))
	}
	if err := connection.notify("initialized", nil); err != nil {
		return nil, finish(err)
	}
	var response struct {
		Files []struct {
			Path      string   `json:"path"`
			MatchType string   `json:"match_type"`
			Score     uint32   `json:"score"`
			Indices   []uint32 `json:"indices"`
		} `json:"files"`
	}
	if err := connection.request("fuzzyFileSearch", map[string]any{
		"query": query, "roots": []string{root},
	}, &response); err != nil {
		return nil, finish(fmt.Errorf("search codex project files: %w", err))
	}
	if err := finish(nil); err != nil {
		return nil, err
	}
	matches := make([]process.CodexFileMatch, 0, min(len(response.Files), maxFileMatches))
	for _, match := range response.Files {
		if match.MatchType != "file" || filepath.IsAbs(match.Path) {
			continue
		}
		clean := filepath.Clean(match.Path)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			continue
		}
		matches = append(matches, process.CodexFileMatch{
			Path: filepath.ToSlash(clean), Score: match.Score, Indices: append([]uint32(nil), match.Indices...),
		})
		if len(matches) == maxFileMatches {
			break
		}
	}
	return matches, nil
}

func (c *Client) startAppServer(ctx context.Context, input appServerRunInput) (process.CodexHandle, error) {
	if input.processRunID == "" {
		return process.CodexHandle{}, errors.New("process run id is required")
	}
	if err := ctx.Err(); err != nil {
		return process.CodexHandle{}, err
	}

	codexHome := c.CodexHome()
	transcriptPath := ""
	transcriptRelativePath := ""
	baselineOffset := int64(0)
	if input.codexSessionID != "" {
		if input.transcript.CodexSessionID != input.codexSessionID {
			return process.CodexHandle{}, fmt.Errorf("%w: transcript source does not match resume session", process.ErrTranscriptUnavailable)
		}
		var err error
		transcriptPath, transcriptRelativePath, err = resolveTranscriptPath(codexHome, input.transcript)
		if err != nil {
			return process.CodexHandle{}, err
		}
		info, err := os.Stat(transcriptPath)
		if err != nil {
			return process.CodexHandle{}, fmt.Errorf("%w: open resume transcript", process.ErrTranscriptUnavailable)
		}
		baselineOffset = info.Size()
	}

	cmd := exec.CommandContext(context.Background(), c.Bin(), c.buildAppServerArgs(input)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	env := os.Environ()
	if codexHome != "" {
		env = upsertEnv(env, "CODEX_HOME", codexHome)
	}
	if c.mcpAuthToken != "" {
		env = upsertEnv(env, "ANYCODE_MCP_TOKEN", c.mcpAuthToken)
	}
	env = upsertEnv(env, processRunOwnerEnv, string(input.processRunID))
	if input.artifactDir != "" {
		env = upsertEnv(env, artifactDirEnv, input.artifactDir)
	}
	cmd.Env = env
	if input.workdir != "" {
		cmd.Dir = input.workdir
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return process.CodexHandle{}, err
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

	connection := &appServerConnection{reader: bufio.NewReader(stdout), writer: stdin}
	failStart := func(err error) (process.CodexHandle, error) {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		_ = cmd.Wait()
		if message := strings.TrimSpace(stderr.String()); message != "" {
			err = fmt.Errorf("%w: %s", err, message)
		}
		return process.CodexHandle{}, err
	}
	if err := connection.request("initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "anycode", "title": "AnyCode", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true},
	}, nil); err != nil {
		return failStart(fmt.Errorf("initialize codex app-server: %w", err))
	}
	if err := connection.notify("initialized", nil); err != nil {
		return failStart(fmt.Errorf("acknowledge codex app-server initialization: %w", err))
	}

	threadID := input.codexSessionID
	if threadID == "" {
		var response struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := connection.request("thread/start", c.appServerThreadParams(input, false), &response); err != nil {
			return failStart(fmt.Errorf("start codex app-server thread: %w", err))
		}
		threadID = strings.TrimSpace(response.Thread.ID)
	} else {
		var response struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		params := c.appServerThreadParams(input, true)
		params["threadId"] = threadID
		if err := connection.request("thread/resume", params, &response); err != nil {
			return failStart(fmt.Errorf("resume codex app-server thread: %w", err))
		}
		if resumed := strings.TrimSpace(response.Thread.ID); resumed != "" {
			threadID = resumed
		}
	}
	if threadID == "" {
		return failStart(errors.New("codex app-server returned an empty thread id"))
	}

	terminal, err := connection.startInput(threadID, input)
	if err != nil {
		return failStart(err)
	}
	sessionIDs := make(chan string, 1)
	sessionIDs <- threadID
	close(sessionIDs)
	planUpdates := make(chan process.CodexEvent, 64)
	active := &activeProcess{
		cmd: cmd, stdin: stdin, stderr: &stderr, home: codexHome, workdir: input.workdir,
		processRunID: input.processRunID, sessionID: input.sessionID, codexSessionID: threadID,
		transcriptPath: transcriptPath, transcriptRelativePath: transcriptRelativePath,
		baselineOffset: baselineOffset, stdoutSessionIDs: sessionIDs,
		stdoutPlanUpdates: planUpdates, observer: c.observer, done: make(chan struct{}),
	}
	processRegistry.Store(input.processRunID, active)
	if terminal == "" {
		_ = stdin.Close()
		close(planUpdates)
	} else {
		go monitorAppServer(connection, active, terminal, threadID, planUpdates)
	}
	go active.wait()
	return process.CodexHandle{ProcessRunID: input.processRunID, PID: cmd.Process.Pid, CodexSessionID: threadID}, nil
}

func (c *Client) buildAppServerArgs(input appServerRunInput) []string {
	args := []string{"app-server", "--stdio"}
	args = c.appendMCPArgs(args, input.sessionID)
	args = c.appendPlaywrightMCPArgs(args, input.processRunID, input.artifactDir)
	args = c.appendRuntimeConfigArgs(args, "", input.reasoningEffort, input.permissionMode, input.fastMode, false)
	if input.artifactDir != "" && input.permissionMode == "workspace-write" {
		args = append(args, "-c", fmt.Sprintf("sandbox_workspace_write.writable_roots=[%q]", input.artifactDir))
	}
	return args
}

func (c *Client) appServerThreadParams(input appServerRunInput, resume bool) map[string]any {
	params := map[string]any{"approvalPolicy": "never"}
	if input.workdir != "" {
		params["cwd"] = input.workdir
	}
	if input.model != "" {
		params["model"] = input.model
	}
	if input.fastMode {
		params["serviceTier"] = "priority"
	}
	if _, profile := mcpPermissionProfile(input.permissionMode); c.mcpStdioSocket != "" && profile {
		params["permissions"] = "anycode-mcp"
	} else if input.permissionMode != "" {
		params["sandbox"] = input.permissionMode
	}
	if !resume {
		params["ephemeral"] = false
	}
	return params
}

func (c *appServerConnection) startInput(threadID string, input appServerRunInput) (appServerTerminal, error) {
	prompt := strings.TrimSpace(input.prompt)
	if prompt == "/compact" {
		if err := c.request("thread/compact/start", map[string]any{"threadId": threadID}, nil); err != nil {
			return "", fmt.Errorf("compact codex thread: %w", err)
		}
		return appServerCompactTerminal, nil
	}
	if instructions, ok := slashCommandArgs(prompt, "/review"); ok {
		target := map[string]any{"type": "uncommittedChanges"}
		if instructions != "" {
			target = map[string]any{"type": "custom", "instructions": instructions}
		}
		if err := c.request("review/start", map[string]any{
			"threadId": threadID, "delivery": "inline", "target": target,
		}, nil); err != nil {
			return "", fmt.Errorf("start codex review: %w", err)
		}
		return appServerTurnTerminal, nil
	}
	if objective, ok := slashCommandArgs(prompt, "/goal"); ok {
		if objective == "" {
			return "", errors.New("codex goal objective is required")
		}
		if err := c.request("thread/goal/set", map[string]any{
			"threadId": threadID, "objective": objective, "status": "active",
		}, nil); err != nil {
			return "", fmt.Errorf("set codex thread goal: %w", err)
		}
		return "", nil
	}
	turnPrompt := input.prompt
	var collaborationMode map[string]any
	if task, ok := slashCommandArgs(prompt, "/plan"); ok {
		if task == "" {
			return "", errors.New("codex plan task is required")
		}
		if strings.TrimSpace(input.model) == "" {
			return "", errors.New("codex plan model is required")
		}
		turnPrompt = task
		settings := map[string]any{"model": input.model, "developer_instructions": nil}
		if input.reasoningEffort != "" {
			settings["reasoning_effort"] = input.reasoningEffort
		}
		collaborationMode = map[string]any{"mode": "plan", "settings": settings}
	}
	inputs := make([]map[string]any, 0, 1+len(input.imagePaths))
	if turnPrompt != "" {
		inputs = append(inputs, map[string]any{"type": "text", "text": turnPrompt})
		inputs = append(inputs, appServerFileMentions(turnPrompt, input.workdir)...)
	}
	for _, path := range input.imagePaths {
		if strings.TrimSpace(path) != "" {
			inputs = append(inputs, map[string]any{"type": "localImage", "path": path})
		}
	}
	if len(inputs) == 0 {
		return "", nil
	}
	params := map[string]any{"threadId": threadID, "input": inputs}
	if collaborationMode != nil {
		params["collaborationMode"] = collaborationMode
	} else if input.reasoningEffort != "" {
		params["effort"] = input.reasoningEffort
	}
	if err := c.request("turn/start", params, nil); err != nil {
		return "", fmt.Errorf("start codex turn: %w", err)
	}
	return appServerTurnTerminal, nil
}

func slashCommandArgs(prompt string, command string) (string, bool) {
	if prompt == command {
		return "", true
	}
	if !strings.HasPrefix(prompt, command) || len(prompt) == len(command) {
		return "", false
	}
	next, _ := utf8.DecodeRuneInString(prompt[len(command):])
	if !unicode.IsSpace(next) {
		return "", false
	}
	return strings.TrimSpace(prompt[len(command):]), true
}

func appServerFileMentions(prompt string, workdir string) []map[string]any {
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		return nil
	}
	root, err := filepath.Abs(workdir)
	if err != nil || root == "" {
		return nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil
	}
	mentions := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for index := 0; index < len(prompt); {
		if prompt[index] != '@' || !isMentionBoundary(prompt, index) {
			_, size := utf8.DecodeRuneInString(prompt[index:])
			index += size
			continue
		}
		name, end := mentionName(prompt, index+1)
		index = max(end, index+1)
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || filepath.IsAbs(clean) {
			continue
		}
		path := filepath.Join(root, clean)
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		resolvedPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		resolvedRelative, err := filepath.Rel(resolvedRoot, resolvedPath)
		if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
			continue
		}
		info, err := os.Stat(resolvedPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name = filepath.ToSlash(relative)
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		mentions = append(mentions, map[string]any{"type": "mention", "name": name, "path": path})
	}
	return mentions
}

func isMentionBoundary(prompt string, index int) bool {
	if index == 0 {
		return true
	}
	previous, _ := utf8.DecodeLastRuneInString(prompt[:index])
	return unicode.IsSpace(previous)
}

func mentionName(prompt string, start int) (string, int) {
	if start >= len(prompt) {
		return "", start
	}
	if prompt[start] == '"' {
		for end := start + 1; end < len(prompt); end++ {
			if prompt[end] != '"' || prompt[end-1] == '\\' {
				continue
			}
			quoted := prompt[start : end+1]
			name, err := strconv.Unquote(quoted)
			if err != nil {
				return "", end + 1
			}
			return name, end + 1
		}
		return "", len(prompt)
	}
	end := start
	for end < len(prompt) {
		r, size := utf8.DecodeRuneInString(prompt[end:])
		if unicode.IsSpace(r) {
			break
		}
		end += size
	}
	return prompt[start:end], end
}

func (c *appServerConnection) request(method string, params any, result any) error {
	c.nextID++
	id := c.nextID
	request := map[string]any{"id": id, "method": method, "params": params}
	if err := json.NewEncoder(c.writer).Encode(request); err != nil {
		return err
	}
	for {
		line, err := c.reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			raw := bytes.TrimSpace(line)
			var envelope appServerEnvelope
			if json.Unmarshal(raw, &envelope) != nil {
				return fmt.Errorf("invalid app-server response: %s", raw)
			}
			var responseID int
			if len(envelope.ID) > 0 && json.Unmarshal(envelope.ID, &responseID) == nil && responseID == id {
				if envelope.Error != nil {
					return fmt.Errorf("app-server error %d: %s", envelope.Error.Code, envelope.Error.Message)
				}
				if result != nil && len(envelope.Result) > 0 {
					if err := json.Unmarshal(envelope.Result, result); err != nil {
						return fmt.Errorf("decode app-server %s response: %w", method, err)
					}
				}
				return nil
			}
			c.pending = append(c.pending, append([]byte(nil), raw...))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("app-server closed before responding")
			}
			return err
		}
	}
}

func (c *appServerConnection) notify(method string, params any) error {
	notification := map[string]any{"method": method}
	if params != nil {
		notification["params"] = params
	}
	return json.NewEncoder(c.writer).Encode(notification)
}

func monitorAppServer(connection *appServerConnection, active *activeProcess, terminal appServerTerminal, threadID string, planUpdates chan process.CodexEvent) {
	defer close(planUpdates)
	completed := false
	handle := func(raw []byte) {
		if event, ok := appServerPlanUpdate(raw); ok {
			sendLatestPlanUpdate(planUpdates, event)
		}
		terminalMatch, failure := appServerTerminalState(raw, terminal, threadID)
		if terminalMatch {
			completed = true
			active.failProtocol(failure)
		}
	}
	for _, raw := range connection.pending {
		handle(raw)
	}
	connection.pending = nil
	for !completed {
		line, err := connection.reader.ReadBytes('\n')
		if raw := bytes.TrimSpace(line); len(raw) > 0 {
			handle(raw)
		}
		if err != nil {
			if !completed {
				active.failProtocol("codex app-server exited before the active operation completed")
			}
			return
		}
	}
	_ = active.stdin.Close()
	_, _ = io.Copy(io.Discard, connection.reader)
}

func appServerTerminalState(raw []byte, terminal appServerTerminal, threadID string) (bool, string) {
	var envelope appServerEnvelope
	if json.Unmarshal(raw, &envelope) != nil || envelope.Method != string(terminal) {
		return false, ""
	}
	var params map[string]any
	if json.Unmarshal(envelope.Params, &params) != nil || stringValue(params, "threadId", "thread_id") != threadID {
		return false, ""
	}
	if terminal != appServerTurnTerminal {
		return true, ""
	}
	turn := mapValue(params["turn"])
	status := strings.TrimSpace(stringValue(turn, "status"))
	if status == "completed" {
		return true, ""
	}
	message := strings.TrimSpace(stringValue(mapValue(turn["error"]), "message"))
	if message == "" {
		message = "codex turn " + status
	}
	return true, message
}

func appServerPlanUpdate(raw []byte) (process.CodexEvent, bool) {
	var envelope appServerEnvelope
	if json.Unmarshal(raw, &envelope) != nil || envelope.Method == "" {
		return process.CodexEvent{}, false
	}
	var params map[string]any
	if json.Unmarshal(envelope.Params, &params) != nil {
		return process.CodexEvent{}, false
	}
	update, correlationID, ok := planUpdateFromEvent(strings.ReplaceAll(envelope.Method, "/", "."), params)
	if !ok {
		return process.CodexEvent{}, false
	}
	update.EventID = stablePlanUpdateEventID(update)
	return process.CodexEvent{
		Type: process.CodexEventPlan, Phase: process.CodexPhaseStandalone,
		Content: update, CorrelationID: correlationID, EventID: update.EventID,
	}, true
}
