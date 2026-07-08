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
	"syscall"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

var ErrProcessNotFound = errors.New("codex process run is not active")

type activeProcess struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *bytes.Buffer
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
	cmd := exec.CommandContext(context.Background(), c.Bin(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if c.mcpAuthToken != "" {
		cmd.Env = append(os.Environ(), "ANYCODE_MCP_TOKEN="+c.mcpAuthToken)
	}
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
		cmd:    cmd,
		stdout: stdout,
		stderr: &stderr,
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

		reader := bufio.NewReader(active.stdout)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				raw := bytes.TrimRight(line, "\r\n")
				raw = append([]byte(nil), raw...)
				event := parseCodexEvent(raw)
				select {
				case events <- event:
				case <-ctx.Done():
					return
				}
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				sendProcessExit(ctx, events, waitProcess(active.cmd, active.stderr.String()), err)
				return
			}
		}
		sendProcessExit(ctx, events, waitProcess(active.cmd, active.stderr.String()), nil)
	}()
	return events, nil
}

func waitProcess(cmd *exec.Cmd, stderr string) process.ExitResult {
	err := cmd.Wait()
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
	result.FailureReason = commandError(err, stderr).Error()
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

func parseCodexEvent(raw []byte) process.CodexEvent {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return process.CodexEvent{
			Type:    "invalid_json",
			Payload: map[string]any{"error": err.Error()},
			Raw:     raw,
		}
	}

	return process.CodexEvent{
		EventID: stringField(payload, "id", "event_id"),
		Type:    eventType(payload),
		Payload: payload,
		Raw:     raw,
	}
}

func eventType(payload map[string]any) string {
	if typ := stringField(payload, "type"); typ != "" {
		return typ
	}
	if msg, ok := payload["msg"].(map[string]any); ok {
		return stringField(msg, "type")
	}
	return "unknown"
}

func stringField(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}
