package codexcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

const maxFileMatches = 100

func (c *Client) SlashCommands() []process.CodexSlashCommand {
	return []process.CodexSlashCommand{
		{Name: "/review", Description: "审查当前工作区变更", AcceptsArgs: true},
		{Name: "/compact", Description: "压缩当前会话上下文", RequiresThread: true},
		{Name: "/goal", Description: "设置当前会话目标：/goal <目标>", AcceptsArgs: true, RequiresThread: true},
		{Name: "/plan", Description: "以计划模式处理：/plan <任务>", AcceptsArgs: true},
	}
}

func (c *Client) Start(ctx context.Context, input process.CodexStartInput) (process.CodexHandle, error) {
	return c.start(ctx, input.ProcessRunID, input.SessionID, "", input.Workdir, input.ArtifactDir, input.Input, input.Action, input.ActionArgument, input.DeveloperInstructions, input.Model, input.ReasoningEffort, input.PermissionMode, input.FastMode)
}

func (c *Client) Resume(ctx context.Context, input process.CodexResumeInput) (process.CodexHandle, error) {
	if strings.TrimSpace(input.CodexSessionID) == "" {
		return process.CodexHandle{}, process.ErrThreadUnavailable
	}
	return c.start(ctx, input.ProcessRunID, input.SessionID, input.CodexSessionID, input.Workdir, input.ArtifactDir, input.Input, input.Action, input.ActionArgument, input.DeveloperInstructions, input.Model, input.ReasoningEffort, input.PermissionMode, input.FastMode)
}

func (c *Client) start(
	ctx context.Context,
	runID process.RunID,
	sessionID process.SessionID,
	threadID string,
	workdir string,
	artifactDir string,
	input []process.CodexInputItem,
	action process.CodexAction,
	actionArgument string,
	developerInstructions string,
	model string,
	reasoningEffort string,
	permissionMode string,
	fastMode bool,
) (process.CodexHandle, error) {
	if runID == "" || sessionID == "" {
		return process.CodexHandle{}, errors.New("process run id and session id are required")
	}
	runtime, err := c.appServer(ctx)
	if err != nil {
		return process.CodexHandle{}, err
	}
	resuming := threadID != ""
	params := appServerThreadParams(workdir, artifactDir, developerInstructions, model, permissionMode, fastMode)
	params["dynamicTools"] = anyCodeDynamicTools()
	if threadID == "" {
		params["ephemeral"] = false
		params["historyMode"] = "paginated"
		var response struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := runtime.request(ctx, "thread/start", params, &response); err != nil {
			return process.CodexHandle{}, fmt.Errorf("start codex thread: %w", err)
		}
		threadID = strings.TrimSpace(response.Thread.ID)
	} else {
		params["threadId"] = threadID
		var response struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := runtime.request(ctx, "thread/resume", params, &response); err != nil {
			return process.CodexHandle{}, fmt.Errorf("resume codex thread: %w", err)
		}
		if resumed := strings.TrimSpace(response.Thread.ID); resumed != "" {
			threadID = resumed
		}
	}
	if threadID == "" {
		return process.CodexHandle{}, errors.New("codex app-server returned an empty thread id")
	}
	transcriptPath := ""
	transcriptOffset := int64(0)
	if resuming {
		transcriptPath, err = waitForSessionLog(ctx, c.CodexHome(), threadID)
		if err != nil {
			return process.CodexHandle{}, fmt.Errorf("find codex session log for resume: %w", err)
		}
		info, statErr := os.Stat(transcriptPath)
		if statErr != nil {
			return process.CodexHandle{}, fmt.Errorf("stat codex session log for resume: %w", statErr)
		}
		transcriptOffset = info.Size()
	}
	handle := process.CodexHandle{ProcessRunID: runID, CodexSessionID: threadID}
	routeCtx, routeCancel := context.WithCancel(context.Background())
	route := &appServerRun{
		handle: handle, sessionID: sessionID, workdir: workdir, ctx: routeCtx, cancel: routeCancel,
		events: make(chan process.CodexEvent, 1024), closed: make(chan struct{}), finished: make(chan process.ExitResult, 1),
	}
	runtime.register(route)
	go runtime.followSessionLog(route, transcriptPath, transcriptOffset)
	turnID, active, err := runtime.startInput(ctx, threadID, workdir, artifactDir, input, action, actionArgument, developerInstructions, model, reasoningEffort, permissionMode)
	if err != nil {
		runtime.removeRoute(route)
		return process.CodexHandle{}, err
	}
	route.setTurnID(turnID)
	handle.TurnID = turnID
	if !active {
		finished := process.ExitResult{FinishedAt: nowUTC()}
		route.emit(process.CodexEvent{Type: process.CodexEventProcessExit, Content: finished, CreatedAt: finished.FinishedAt})
		runtime.completeRoute(route)
	}
	return handle, nil
}

func appServerThreadParams(workdir string, artifactDir string, developerInstructions string, model string, permissionMode string, fastMode bool) map[string]any {
	params := map[string]any{}
	if workdir != "" {
		params["cwd"] = workdir
	}
	if model != "" {
		params["model"] = model
	}
	if developerInstructions != "" {
		params["developerInstructions"] = developerInstructions
	}
	if fastMode {
		params["serviceTier"] = "priority"
	}
	if permissionMode != "" {
		params["sandbox"] = permissionMode
	}
	if artifactDir != "" {
		config := map[string]any{
			"shell_environment_policy": map[string]any{"set": map[string]string{"ANYCODE_ARTIFACT_DIR": artifactDir}},
		}
		if permissionMode == "workspace-write" {
			config["sandbox_workspace_write"] = map[string]any{"writable_roots": []string{artifactDir}}
		}
		params["config"] = config
	}
	return params
}

func appServerSandboxPolicy(permissionMode string, artifactDir string) map[string]any {
	switch strings.TrimSpace(permissionMode) {
	case "read-only":
		return map[string]any{"type": "readOnly"}
	case "workspace-write":
		policy := map[string]any{"type": "workspaceWrite"}
		if artifactDir = strings.TrimSpace(artifactDir); artifactDir != "" {
			policy["writableRoots"] = []string{artifactDir}
		}
		return policy
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	default:
		return nil
	}
}

func anyCodeDynamicTools() []map[string]any {
	optionSchema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"label"},
		"properties": map[string]any{
			"id": map[string]any{"type": "string"}, "label": map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"}, "payload": map[string]any{"type": "object"},
		},
	}
	questionSchema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"body"},
		"properties": map[string]any{
			"body": map[string]any{"type": "string"}, "type": map[string]any{"type": "string"},
			"options": map[string]any{"type": "array", "items": optionSchema},
		},
	}
	return []map[string]any{
		{
			"type": "function", "name": "questions",
			"description": "Ask the user one or more questions and wait for their answers. Each question requires a body; options are optional.",
			"inputSchema": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"questions"},
				"properties": map[string]any{"questions": map[string]any{
					"type": "array", "minItems": 1, "items": questionSchema,
				}},
			},
		},
		{
			"type": "function", "name": "publish_artifact",
			"description": "Inspect a file in this card's ANYCODE_ARTIFACT_DIR and return its stable metadata and preview content.",
			"inputSchema": map[string]any{
				"type": "object", "required": []string{"path"},
				"properties": map[string]any{"path": map[string]any{"type": "string", "description": "Path relative to ANYCODE_ARTIFACT_DIR."}},
			},
		},
	}
}

func (r *appServerRuntime) startInput(ctx context.Context, threadID string, workdir string, artifactDir string, input []process.CodexInputItem, action process.CodexAction, actionArgument string, developerInstructions string, model string, reasoningEffort string, permissionMode string) (string, bool, error) {
	switch action {
	case process.CodexActionCompact:
		if err := r.request(ctx, "thread/compact/start", map[string]any{"threadId": threadID}, nil); err != nil {
			return "", false, fmt.Errorf("compact codex thread: %w", err)
		}
		return "", false, nil
	case process.CodexActionReview:
		target := map[string]any{"type": "uncommittedChanges"}
		if actionArgument != "" {
			target = map[string]any{"type": "custom", "instructions": actionArgument}
		}
		var response struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if err := r.request(ctx, "review/start", map[string]any{"threadId": threadID, "delivery": "inline", "target": target}, &response); err != nil {
			return "", false, fmt.Errorf("start codex review: %w", err)
		}
		return response.Turn.ID, true, nil
	case process.CodexActionGoal:
		if actionArgument == "" {
			return "", false, errors.New("codex goal objective is required")
		}
		if err := r.request(ctx, "thread/goal/set", map[string]any{"threadId": threadID, "objective": actionArgument, "status": "active"}, nil); err != nil {
			return "", false, fmt.Errorf("set codex thread goal: %w", err)
		}
		return "", false, nil
	}
	var collaborationMode map[string]any
	if action == process.CodexActionPlan {
		task := firstTextInput(input)
		if task == "" {
			return "", false, errors.New("codex plan task is required")
		}
		if model == "" {
			return "", false, errors.New("codex plan model is required")
		}
		settings := map[string]any{"model": model, "developer_instructions": developerInstructions}
		if reasoningEffort != "" {
			settings["reasoning_effort"] = reasoningEffort
		}
		collaborationMode = map[string]any{"mode": "plan", "settings": settings}
	}
	items, err := appServerInput(input, workdir)
	if err != nil {
		return "", false, err
	}
	if len(items) == 0 {
		return "", false, nil
	}
	params := map[string]any{"threadId": threadID, "input": items}
	if collaborationMode != nil {
		params["collaborationMode"] = collaborationMode
	}
	if sandboxPolicy := appServerSandboxPolicy(permissionMode, artifactDir); sandboxPolicy != nil {
		params["sandboxPolicy"] = sandboxPolicy
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := r.request(ctx, "turn/start", params, &response); err != nil {
		return "", false, fmt.Errorf("start codex turn: %w", err)
	}
	return response.Turn.ID, true, nil
}

func appServerInput(input []process.CodexInputItem, workdir string) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(input))
	for _, item := range input {
		switch item.Type {
		case "text":
			if item.Text == "" {
				continue
			}
			result = append(result, map[string]any{"type": "text", "text": item.Text})
		case "localImage", "localAudio":
			if item.Path != "" {
				result = append(result, map[string]any{"type": item.Type, "path": item.Path})
			}
		case "mention":
			if item.Path != "" && item.Name != "" {
				path, err := appServerMentionPath(item.Path, workdir)
				if err != nil {
					return nil, err
				}
				result = append(result, map[string]any{"type": "mention", "path": path, "name": item.Name})
			}
		}
	}
	return result, nil
}

func appServerMentionPath(path string, workdir string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if strings.TrimSpace(workdir) == "" {
		return "", errors.New("codex mention workdir is required")
	}
	root, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", fmt.Errorf("resolve codex mention workdir: %w", err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return "", fmt.Errorf("resolve codex mention %q: %w", path, err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("codex mention %q escapes workdir", path)
	}
	return target, nil
}

func (c *Client) Steer(ctx context.Context, input process.CodexSteerInput) error {
	c.mu.Lock()
	runtime := c.runtime
	c.mu.Unlock()
	if runtime == nil || !runtime.alive() {
		return process.ErrProcessNotFound
	}
	route := runtime.routeForRun(input.ProcessRunID)
	if route == nil {
		return process.ErrProcessNotFound
	}
	turnID := route.activeTurnID()
	if turnID == "" {
		return process.ErrProcessNotFound
	}
	items, err := appServerInput(input.Input, route.workdir)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("codex steer input is required")
	}
	var response struct {
		TurnID string `json:"turnId"`
	}
	if err := runtime.request(ctx, "turn/steer", map[string]any{
		"threadId": route.handle.CodexSessionID, "expectedTurnId": turnID, "input": items,
	}, &response); err != nil {
		return fmt.Errorf("steer codex turn: %w", err)
	}
	if response.TurnID != "" && response.TurnID != turnID {
		return fmt.Errorf("steer codex turn returned unexpected turn id %q", response.TurnID)
	}
	return nil
}

func firstTextInput(input []process.CodexInputItem) string {
	for _, item := range input {
		if item.Type == "text" {
			return item.Text
		}
	}
	return ""
}

func (c *Client) Events(ctx context.Context, handle process.CodexHandle) (<-chan process.CodexEvent, error) {
	c.mu.Lock()
	runtime := c.runtime
	c.mu.Unlock()
	if runtime == nil {
		return nil, process.ErrProcessNotFound
	}
	events, ok := runtime.claimEvents(handle.ProcessRunID)
	if !ok {
		return nil, process.ErrProcessNotFound
	}
	return events, nil
}

func (c *Client) Stop(ctx context.Context, runID process.RunID) error {
	c.mu.Lock()
	runtime := c.runtime
	c.mu.Unlock()
	if runtime == nil || !runtime.alive() {
		return process.ErrProcessNotFound
	}
	route := runtime.routeForRun(runID)
	if route == nil {
		return process.ErrProcessNotFound
	}
	turnID := route.activeTurnID()
	if turnID == "" {
		return process.ErrProcessNotFound
	}
	if err := runtime.request(ctx, "turn/interrupt", map[string]any{"threadId": route.handle.CodexSessionID, "turnId": turnID}, nil); err != nil {
		return fmt.Errorf("interrupt codex turn: %w", err)
	}
	return nil
}

func (c *Client) DeleteThread(ctx context.Context, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	runtime, err := c.appServer(ctx)
	if err != nil {
		return err
	}
	if err := runtime.request(ctx, "thread/delete", map[string]any{"threadId": threadID}, nil); err != nil {
		return fmt.Errorf("delete codex thread: %w", err)
	}
	return nil
}

func (c *Client) SearchFiles(ctx context.Context, root string, query string) ([]process.CodexFileMatch, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("file search root is required")
	}
	runtime, err := c.appServer(ctx)
	if err != nil {
		return nil, err
	}
	var response struct {
		Files []struct {
			Path      string   `json:"path"`
			MatchType string   `json:"match_type"`
			Score     uint32   `json:"score"`
			Indices   []uint32 `json:"indices"`
		} `json:"files"`
	}
	if err := runtime.request(ctx, "fuzzyFileSearch", map[string]any{"query": query, "roots": []string{root}}, &response); err != nil {
		return nil, fmt.Errorf("search codex project files: %w", err)
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
		matches = append(matches, process.CodexFileMatch{Path: filepath.ToSlash(clean), Score: match.Score, Indices: append([]uint32(nil), match.Indices...)})
		if len(matches) == maxFileMatches {
			break
		}
	}
	return matches, nil
}

func (r *appServerRuntime) removeRoute(route *appServerRun) {
	r.routesMu.Lock()
	delete(r.routes, route.handle.ProcessRunID)
	if r.threads[route.handle.CodexSessionID] == route {
		delete(r.threads, route.handle.CodexSessionID)
	}
	r.routesMu.Unlock()
	route.close()
}

func nowUTC() time.Time { return time.Now().UTC() }
