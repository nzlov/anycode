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
	return c.start(ctx, input.ProcessRunID, input.SessionID, "", "", input.Workdir, input.ArtifactDir, input.Input, input.Action, input.ActionArgument, input.DeveloperInstructions, input.Model, input.ReasoningEffort, input.PermissionMode, input.WritableRoots, input.FastMode, input.DynamicTools, false)
}

func (c *Client) Resume(ctx context.Context, input process.CodexResumeInput) (process.CodexHandle, error) {
	if strings.TrimSpace(input.CodexSessionID) == "" {
		return process.CodexHandle{}, process.ErrThreadUnavailable
	}
	return c.start(ctx, input.ProcessRunID, input.SessionID, input.CodexSessionID, "", input.Workdir, input.ArtifactDir, input.Input, input.Action, input.ActionArgument, input.DeveloperInstructions, input.Model, input.ReasoningEffort, input.PermissionMode, input.WritableRoots, input.FastMode, input.DynamicTools, false)
}

func (c *Client) Fork(ctx context.Context, input process.CodexForkInput) (process.CodexHandle, error) {
	if strings.TrimSpace(input.SourceCodexSessionID) == "" {
		return process.CodexHandle{}, process.ErrThreadUnavailable
	}
	return c.start(ctx, input.ProcessRunID, input.SessionID, "", input.SourceCodexSessionID, input.Workdir, input.ArtifactDir, input.Input, input.Action, input.ActionArgument, input.DeveloperInstructions, input.Model, input.ReasoningEffort, input.PermissionMode, input.WritableRoots, input.FastMode, input.DynamicTools, input.Ephemeral)
}

func (c *Client) ContinueLoaded(ctx context.Context, input process.CodexResumeInput) (process.CodexHandle, error) {
	threadID := strings.TrimSpace(input.CodexSessionID)
	if threadID == "" {
		return process.CodexHandle{}, process.ErrThreadUnavailable
	}
	if input.ProcessRunID == "" || input.SessionID == "" {
		return process.CodexHandle{}, errors.New("process run id and session id are required")
	}
	runtime, err := c.appServer(ctx)
	if err != nil {
		return process.CodexHandle{}, err
	}
	return runtime.beginTurn(ctx, input.ProcessRunID, input.SessionID, threadID, input.Workdir, input.Input, input.Action, input.ActionArgument, input.DeveloperInstructions, input.Model, input.ReasoningEffort, input.PermissionMode, newWorkspaceWriteSettings(input.PermissionMode, input.WritableRoots, input.ArtifactDir), true, "", 0)
}

func (c *Client) start(
	ctx context.Context,
	runID process.RunID,
	sessionID process.SessionID,
	threadID string,
	forkFromThreadID string,
	workdir string,
	artifactDir string,
	input []process.CodexInputItem,
	action process.CodexAction,
	actionArgument string,
	developerInstructions string,
	model string,
	reasoningEffort string,
	permissionMode string,
	writableRoots []string,
	fastMode bool,
	dynamicTools []process.DynamicToolName,
	ephemeral bool,
) (process.CodexHandle, error) {
	if runID == "" || sessionID == "" {
		return process.CodexHandle{}, errors.New("process run id and session id are required")
	}
	runtime, err := c.appServer(ctx)
	if err != nil {
		return process.CodexHandle{}, err
	}
	workspaceWrite := newWorkspaceWriteSettings(permissionMode, writableRoots, artifactDir)
	resuming := threadID != ""
	forking := strings.TrimSpace(forkFromThreadID) != ""
	params := appServerThreadParams(workdir, artifactDir, developerInstructions, model, permissionMode, fastMode, workspaceWrite)
	if !forking {
		params["dynamicTools"] = anyCodeDynamicTools(dynamicTools...)
	}
	if forking {
		params["threadId"] = strings.TrimSpace(forkFromThreadID)
		params["ephemeral"] = ephemeral
		if ephemeral {
			params["excludeTurns"] = true
		}
		var response struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := runtime.request(ctx, "thread/fork", params, &response); err != nil {
			if isUnavailableThreadResumeError(err) {
				return process.CodexHandle{}, fmt.Errorf("fork codex thread: %w: %v", process.ErrThreadUnavailable, err)
			}
			return process.CodexHandle{}, fmt.Errorf("fork codex thread: %w", err)
		}
		threadID = strings.TrimSpace(response.Thread.ID)
	} else if threadID == "" {
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
			if isUnavailableThreadResumeError(err) {
				return process.CodexHandle{}, fmt.Errorf("resume codex thread: %w: %v", process.ErrThreadUnavailable, err)
			}
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
	if !ephemeral && (resuming || forking) {
		transcriptPath, err = waitForSessionLog(ctx, c.CodexHome(), threadID)
		if err != nil {
			return process.CodexHandle{}, fmt.Errorf("find codex session log: %w", err)
		}
		info, statErr := os.Stat(transcriptPath)
		if statErr != nil {
			return process.CodexHandle{}, fmt.Errorf("stat codex session log: %w", statErr)
		}
		transcriptOffset = info.Size()
	}
	return runtime.beginTurn(ctx, runID, sessionID, threadID, workdir, input, action, actionArgument, developerInstructions, model, reasoningEffort, permissionMode, workspaceWrite, ephemeral, transcriptPath, transcriptOffset)
}

func (r *appServerRuntime) beginTurn(
	ctx context.Context,
	runID process.RunID,
	sessionID process.SessionID,
	threadID string,
	workdir string,
	input []process.CodexInputItem,
	action process.CodexAction,
	actionArgument string,
	developerInstructions string,
	model string,
	reasoningEffort string,
	permissionMode string,
	workspaceWrite *workspaceWriteSettings,
	directEvents bool,
	transcriptPath string,
	transcriptOffset int64,
) (process.CodexHandle, error) {
	handle := process.CodexHandle{ProcessRunID: runID, CodexSessionID: threadID}
	routeCtx, routeCancel := context.WithCancel(context.Background())
	route := &appServerRun{
		handle: handle, sessionID: sessionID, workdir: workdir, ctx: routeCtx, cancel: routeCancel,
		directEvents: directEvents, events: make(chan process.CodexEvent, 1024), closed: make(chan struct{}), finished: make(chan process.ExitResult, 1),
	}
	r.register(route)
	if !directEvents {
		go r.followSessionLog(route, transcriptPath, transcriptOffset)
	}
	turnID, active, err := r.startInput(ctx, threadID, workdir, input, action, actionArgument, developerInstructions, model, reasoningEffort, permissionMode, workspaceWrite, route.retainInputCleanup)
	if err != nil {
		r.removeRoute(route)
		return process.CodexHandle{}, err
	}
	route.setTurnID(turnID)
	handle.TurnID = turnID
	if !active {
		finished := process.ExitResult{FinishedAt: nowUTC()}
		route.emit(process.CodexEvent{Type: process.CodexEventProcessExit, Content: finished, CreatedAt: finished.FinishedAt})
		r.completeRoute(route)
	}
	return handle, nil
}

func isUnavailableThreadResumeError(err error) bool {
	var requestErr *appServerRequestError
	return errors.As(err, &requestErr) && requestErr.code == -32600 && strings.HasPrefix(strings.ToLower(strings.TrimSpace(requestErr.message)), "no rollout found for thread id")
}

type workspaceWriteSettings struct {
	WritableRoots []string `json:"writable_roots"`
}

func newWorkspaceWriteSettings(permissionMode string, configuredRoots []string, artifactDir string) *workspaceWriteSettings {
	if strings.TrimSpace(permissionMode) != "workspace-write" {
		return nil
	}
	settings := workspaceWriteSettings{WritableRoots: make([]string, 0, len(configuredRoots)+1)}
	seen := make(map[string]struct{}, len(configuredRoots)+1)
	for _, root := range append(append([]string(nil), configuredRoots...), artifactDir) {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if _, exists := seen[root]; exists {
			continue
		}
		seen[root] = struct{}{}
		settings.WritableRoots = append(settings.WritableRoots, root)
	}
	return &settings
}

func appServerThreadParams(workdir string, artifactDir string, developerInstructions string, model string, permissionMode string, fastMode bool, workspaceWrite *workspaceWriteSettings) map[string]any {
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
	} else {
		params["serviceTier"] = "default"
	}
	if permissionMode != "" {
		params["sandbox"] = permissionMode
	}
	config := map[string]any{}
	if artifactDir != "" {
		config["shell_environment_policy"] = map[string]any{"set": map[string]string{"ANYCODE_ARTIFACT_DIR": artifactDir}}
	}
	if workspaceWrite != nil {
		// GLUE: Thread config and turn policy are separate App Server fields; remove this duplicate mapping when one field can own both phases.
		config["sandbox_workspace_write"] = workspaceWrite
	}
	if len(config) > 0 {
		params["config"] = config
	}
	return params
}

func appServerSandboxPolicy(permissionMode string, workspaceWrite *workspaceWriteSettings) map[string]any {
	switch strings.TrimSpace(permissionMode) {
	case "read-only":
		return map[string]any{"type": "readOnly"}
	case "workspace-write":
		policy := map[string]any{"type": "workspaceWrite"}
		if workspaceWrite != nil {
			policy["writableRoots"] = workspaceWrite.WritableRoots
		}
		return policy
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	default:
		return nil
	}
}

func anyCodeDynamicTools(enabled ...process.DynamicToolName) []map[string]any {
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
			"files": map[string]any{
				"type": "array", "maxItems": 100, "items": map[string]any{"type": "string"},
				"description": "Optional published file IDs returned by publish_artifact. Files are shown below this question.",
			},
			"options": map[string]any{"type": "array", "items": optionSchema},
		},
	}
	tools := []map[string]any{
		{
			"type": "function", "name": "questions",
			"description": "Ask the user one or more questions and wait for their answers. Keep the containing exec call open for 300000 ms. If the current turn is still active when questions completes, continue that turn even if exec has already yielded. If the turn exits before questions completes, later answers resume through durable storage. Each question requires a body; options are optional.",
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
		{
			"type": "function", "name": "tunnel_create",
			"description": "Create an authenticated temporary Cloudflare Quick Tunnel for an HTTP test program already listening on a localhost port.",
			"inputSchema": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"name", "port"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "Human-readable tunnel name shown in AnyCode."},
					"port": map[string]any{
						"type": "integer", "minimum": 1024, "maximum": 65535,
						"description": "Local HTTP port on 127.0.0.1. The test program must already be listening.",
					},
				},
			},
		},
		{
			"type": "function", "name": "tunnel_list",
			"description": "List active temporary tunnels created for this AnyCode session.",
			"inputSchema": map[string]any{"type": "object", "additionalProperties": false},
		},
		{
			"type": "function", "name": "tunnel_close",
			"description": "Close a temporary tunnel created for this AnyCode session.",
			"inputSchema": map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"id"},
				"properties": map[string]any{"id": map[string]any{"type": "string", "description": "Tunnel ID returned by tunnel_create or tunnel_list."}},
			},
		},
	}
	for _, name := range enabled {
		switch name {
		case process.DynamicToolMindMapSearch:
			tools = append(tools, mindMapSearchTool())
		case process.DynamicToolMindMapTags:
			tools = append(tools, mindMapTagsTool())
		case process.DynamicToolMindMapUpdate:
			tools = append(tools, mindMapUpdateTool())
		}
	}
	return tools
}

func mindMapTagsTool() map[string]any {
	return map[string]any{
		"type": "function", "name": string(process.DynamicToolMindMapTags),
		"description": "Refresh mind map tag metadata when no fresh mind_map_search result is available or an update reports a stale revision. Returns compact JSON text containing tagRevision and tag titles; parse the text before reading fields when composing tool calls in code.",
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false},
	}
}

func mindMapSearchTool() map[string]any {
	return map[string]any{
		"type": "function", "name": string(process.DynamicToolMindMapSearch),
		"description": "Search node IDs, titles, content, tags, and code locations in this card's current mind map. Returns budgeted Markdown with full matching nodes, summarized one-hop related nodes, non-tag relationships, and a tagRevision that can be passed directly to mind_map_update. Use one focused SQL-style boolean search before changing concepts so durable nodes are reused.",
		"inputSchema": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "minLength": 1, "maxLength": 500, "description": "SQL-style boolean expression of case-insensitive terms across node IDs, titles, content, tags, file paths, and methods. AND binds more tightly than OR; parentheses override precedence. Adjacent terms require an operator. Example: workflow AND (runner OR retry)."},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "description": "Maximum matching nodes to return. Defaults to 5; usually use 3-5."},
			},
		},
	}
}

func mindMapUpdateTool() map[string]any {
	tags := map[string]any{
		"type": "array", "minItems": 1, "maxItems": 20, "uniqueItems": true,
		"description": "Desired tag names for this node. The server normalizes names, creates or reuses tags, reconciles relationships, and removes orphan tags.",
		"items":       map[string]any{"type": "string", "minLength": 1, "maxLength": 100},
	}
	nodeUpsert := mindMapOperationSchema("upsert_node", []string{"kind", "id", "tags"}, map[string]any{
		"title":   map[string]any{"type": "string", "minLength": 1, "maxLength": 500},
		"content": map[string]any{"type": "string", "maxLength": 20000},
		"tags":    tags,
		"files":   mindMapNodeFilesSchema(),
	})
	operation := map[string]any{"oneOf": []map[string]any{
		nodeUpsert,
		mindMapOperationSchema("delete_node", []string{"kind", "id"}, nil),
		mindMapOperationSchema("upsert_edge", []string{"kind", "id", "sourceId", "targetId"}, map[string]any{
			"sourceId": map[string]any{"type": "string", "minLength": 1},
			"targetId": map[string]any{"type": "string", "minLength": 1},
			"label":    map[string]any{"type": "string", "maxLength": 500},
		}),
		mindMapOperationSchema("delete_edge", []string{"kind", "id"}, nil),
	}}
	return map[string]any{
		"type": "function", "name": string(process.DynamicToolMindMapUpdate),
		"description": "Apply node and relationship changes to this card's isolated mind map. Pass the tagRevision from the most recent mind_map_search or mind_map_tags result. Every node upsert must provide its complete desired tag-name list. The server exclusively creates, reuses, links, and removes tag nodes; agents must not manipulate project-root or tag relationships. Nodes that reference implementation code must include a non-empty files list with file, method, startLine, and endLine. Search before reusing or changing concepts. Each node must express exactly one durable concept. Delete obsolete transient nodes only when they were touched by the current task. Deleting a node also removes its relationships.",
		"inputSchema": map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"tagRevision", "operations"},
			"properties": map[string]any{
				"tagRevision": map[string]any{"type": "string", "minLength": 1, "description": "Exact tagRevision returned by the most recent mind_map_search or mind_map_tags call."},
				"operations":  map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": operation},
			},
		},
	}
}

func mindMapNodeFilesSchema() map[string]any {
	return map[string]any{
		"type": "array", "maxItems": 100,
		"description": "Complete code location list. Required and non-empty when the concept references implementation code; pass an empty list only to clear previous locations. Store locations only, never source snapshots.",
		"items": map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"file", "method", "startLine", "endLine"},
			"properties": map[string]any{
				"file":      map[string]any{"type": "string", "minLength": 1, "maxLength": 2000},
				"method":    map[string]any{"type": "string", "minLength": 1, "maxLength": 500},
				"startLine": map[string]any{"type": "integer", "minimum": 1},
				"endLine":   map[string]any{"type": "integer", "minimum": 1},
			},
		},
	}
}

func mindMapOperationSchema(kind string, required []string, fields map[string]any) map[string]any {
	properties := map[string]any{
		"kind": map[string]any{"type": "string", "enum": []string{kind}},
		"id":   map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
	}
	for name, schema := range fields {
		properties[name] = schema
	}
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": required, "properties": properties,
	}
}

func (r *appServerRuntime) startInput(ctx context.Context, threadID string, workdir string, input []process.CodexInputItem, action process.CodexAction, actionArgument string, developerInstructions string, model string, reasoningEffort string, permissionMode string, workspaceWrite *workspaceWriteSettings, retainInputCleanup func(func())) (string, bool, error) {
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
	items, cleanup, err := appServerInput(input, workdir)
	if err != nil {
		return "", false, err
	}
	if len(items) == 0 {
		cleanup()
		return "", false, nil
	}
	params := map[string]any{"threadId": threadID, "input": items}
	if collaborationMode != nil {
		params["collaborationMode"] = collaborationMode
	} else if reasoningEffort != "" {
		params["effort"] = reasoningEffort
	}
	if sandboxPolicy := appServerSandboxPolicy(permissionMode, workspaceWrite); sandboxPolicy != nil {
		params["sandboxPolicy"] = sandboxPolicy
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := r.request(ctx, "turn/start", params, &response); err != nil {
		cleanup()
		return "", false, fmt.Errorf("start codex turn: %w", err)
	}
	retainInputCleanup(cleanup)
	return response.Turn.ID, true, nil
}

func appServerInput(input []process.CodexInputItem, workdir string) ([]map[string]any, func(), error) {
	result := make([]map[string]any, 0, len(input))
	temporaryPaths := make([]string, 0)
	cleanup := func() {
		for _, path := range temporaryPaths {
			_ = os.Remove(path)
		}
	}
	for _, item := range input {
		path := item.Path
		if len(item.Data) > 0 {
			extension := filepath.Ext(filepath.Base(item.Name))
			if len(extension) > 16 {
				extension = ""
			}
			file, err := os.CreateTemp("", "anycode-prompt-file-*"+extension)
			if err != nil {
				cleanup()
				return nil, func() {}, fmt.Errorf("create temporary prompt file: %w", err)
			}
			path = file.Name()
			temporaryPaths = append(temporaryPaths, path)
			if _, err := file.Write(item.Data); err != nil {
				file.Close()
				cleanup()
				return nil, func() {}, fmt.Errorf("write temporary prompt file: %w", err)
			}
			if err := file.Close(); err != nil {
				cleanup()
				return nil, func() {}, fmt.Errorf("close temporary prompt file: %w", err)
			}
		}
		switch item.Type {
		case "text":
			if item.Text == "" {
				continue
			}
			result = append(result, map[string]any{"type": "text", "text": item.Text})
		case "localImage", "localAudio":
			if path != "" {
				result = append(result, map[string]any{"type": item.Type, "path": path})
			}
		case "mention":
			if path != "" && item.Name != "" {
				path, err := appServerMentionPath(path, workdir)
				if err != nil {
					cleanup()
					return nil, func() {}, err
				}
				result = append(result, map[string]any{"type": "mention", "path": path, "name": item.Name})
			}
		}
	}
	return result, cleanup, nil
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
	items, cleanup, err := appServerInput(input.Input, route.workdir)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		cleanup()
		return errors.New("codex steer input is required")
	}
	var response struct {
		TurnID string `json:"turnId"`
	}
	if err := runtime.request(ctx, "turn/steer", map[string]any{
		"threadId": route.handle.CodexSessionID, "expectedTurnId": turnID, "input": items,
	}, &response); err != nil {
		cleanup()
		return fmt.Errorf("steer codex turn: %w", err)
	}
	if response.TurnID != "" && response.TurnID != turnID {
		cleanup()
		return fmt.Errorf("steer codex turn returned unexpected turn id %q", response.TurnID)
	}
	route.retainInputCleanup(cleanup)
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

func (c *Client) EphemeralEvents(_ context.Context, runID process.RunID) (<-chan process.CodexEvent, error) {
	c.mu.Lock()
	runtime := c.runtime
	c.mu.Unlock()
	if runtime == nil {
		return nil, process.ErrProcessNotFound
	}
	events, ok := runtime.claimEphemeralEvents(runID)
	if !ok {
		return nil, process.ErrProcessNotFound
	}
	return events, nil
}

func (c *Client) StopEphemeral(ctx context.Context, runID process.RunID) error {
	c.mu.Lock()
	runtime := c.runtime
	c.mu.Unlock()
	if runtime == nil || !runtime.alive() {
		return process.ErrProcessNotFound
	}
	route := runtime.routeForRun(runID)
	if route == nil || !route.directEvents {
		return process.ErrProcessNotFound
	}
	return c.Stop(ctx, runID)
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
