//go:build ignore

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const questionsTool = "questions"

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type incomingMessage struct {
	message rpcMessage
	err     error
}

type probeState struct {
	questionsCalled bool
	turnCompleted   bool
	turnStatus      string
}

type appServerClient struct {
	encoder  *json.Encoder
	incoming <-chan incomingMessage
	nextID   int
	answer   string
	state    probeState
}

func main() {
	var (
		codexBin = flag.String("codex", "codex", "path to the Codex CLI")
		cwd      = flag.String("cwd", ".", "working directory for the probe thread")
		answer   = flag.String("answer", "continue", "simulated selected option id returned to Codex")
		resume   = flag.String("resume-thread", "", "resume an existing probe thread instead of starting one")
		timeout  = flag.Duration("timeout", 2*time.Minute, "maximum probe duration")
	)
	flag.Parse()

	absCWD, err := absolutePath(*cwd)
	if err != nil {
		fatalf("resolve cwd: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := runProbe(ctx, *codexBin, absCWD, *answer, *resume); err != nil {
		fatalf("probe failed: %v", err)
	}
}

func runProbe(ctx context.Context, codexBin string, cwd string, answer string, resumeThread string) error {
	cmd := exec.CommandContext(ctx, codexBin, "app-server", "--stdio")
	cmd.Dir = cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open app-server stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open app-server stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start app-server: %w", err)
	}

	go printStderr(stderr)
	incoming := readMessages(stdout)
	client := &appServerClient{
		encoder:  json.NewEncoder(stdin),
		incoming: incoming,
		answer:   answer,
	}

	if err := client.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "anycode_questions_probe",
			"title":   "AnyCode questions probe",
			"version": "1",
		},
		"capabilities": map[string]any{"experimentalApi": true},
	}, nil); err != nil {
		return finish(cmd, stdin, err)
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		return finish(cmd, stdin, err)
	}

	var threadResponse struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	method := "thread/start"
	params := map[string]any{
		"cwd":            cwd,
		"approvalPolicy": "never",
		"sandbox":        "read-only",
		"dynamicTools":   []any{questionsSpec()},
	}
	if resumeThread == "" {
		params["ephemeral"] = false
	} else {
		method = "thread/resume"
		params["threadId"] = resumeThread
	}
	if err := client.request(ctx, method, params, &threadResponse); err != nil {
		return finish(cmd, stdin, err)
	}
	threadID := strings.TrimSpace(threadResponse.Thread.ID)
	if threadID == "" {
		return finish(cmd, stdin, errors.New("thread/start returned an empty thread id"))
	}

	prompt := "Call questions exactly once with one question whose title is Probe, " +
		"body is Continue the protocol probe?, type is choice, and options are " +
		"{id: continue, label: Continue} and {id: stop, label: Stop}. " +
		"Do not answer the question yourself and do not call another tool. " +
		"After questions returns, reply exactly QUESTIONS_OK followed by the selected answer."
	if err := client.request(ctx, "turn/start", map[string]any{
		"threadId": threadID,
		"input":    []any{map[string]any{"type": "text", "text": prompt}},
	}, nil); err != nil {
		return finish(cmd, stdin, err)
	}
	if err := client.waitForTurn(ctx); err != nil {
		return finish(cmd, stdin, err)
	}

	if !client.state.questionsCalled {
		return finish(cmd, stdin, errors.New("turn completed without item/tool/call for questions"))
	}
	if client.state.turnStatus != "completed" {
		return finish(cmd, stdin, fmt.Errorf("turn status is %q", client.state.turnStatus))
	}

	fmt.Printf("PASS questions dynamic tool call completed; thread=%s simulated answer=%q\n", threadID, answer)
	return finish(cmd, stdin, nil)
}

func questionsSpec() map[string]any {
	optionSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"label"},
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"label":       map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"payload":     map[string]any{"type": "object"},
		},
	}
	questionSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"body"},
		"properties": map[string]any{
			"body": map[string]any{"type": "string"},
			"type": map[string]any{"type": "string"},
			"options": map[string]any{
				"type":  "array",
				"items": optionSchema,
			},
		},
	}
	return map[string]any{
		"type":        "function",
		"name":        questionsTool,
		"description": "Ask the user one or more questions and wait for their answers. Each question requires a body; options are optional.",
		"inputSchema": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"questions"},
			"properties": map[string]any{
				"questions": map[string]any{
					"type":     "array",
					"minItems": 1,
					"items":    questionSchema,
				},
			},
		},
	}
}

func (c *appServerClient) request(ctx context.Context, method string, params any, result any) error {
	c.nextID++
	id := c.nextID
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}
	fmt.Printf("-> request %s id=%d\n", method, id)

	for {
		message, err := c.receive(ctx)
		if err != nil {
			return fmt.Errorf("wait for %s response: %w", method, err)
		}
		if err := c.handle(message); err != nil {
			return err
		}
		if rawIDEqual(message.ID, id) {
			if message.Error != nil {
				return fmt.Errorf("%s error %d: %s", method, message.Error.Code, message.Error.Message)
			}
			if result != nil && len(message.Result) > 0 {
				if err := json.Unmarshal(message.Result, result); err != nil {
					return fmt.Errorf("decode %s response: %w", method, err)
				}
			}
			return nil
		}
	}
}

func (c *appServerClient) notify(method string, params any) error {
	fmt.Printf("-> notification %s\n", method)
	return c.send(map[string]any{"method": method, "params": params})
}

func (c *appServerClient) waitForTurn(ctx context.Context) error {
	for !c.state.turnCompleted {
		message, err := c.receive(ctx)
		if err != nil {
			return fmt.Errorf("wait for turn/completed: %w", err)
		}
		if err := c.handle(message); err != nil {
			return err
		}
	}
	return nil
}

func (c *appServerClient) handle(message rpcMessage) error {
	if message.Method == "" {
		fmt.Printf("<- response id=%s\n", compactJSON(message.ID))
		return nil
	}
	if hasID(message.ID) {
		fmt.Printf("<- server request %s id=%s\n", message.Method, compactJSON(message.ID))
		return c.handleServerRequest(message)
	}

	fmt.Printf("<- notification %s%s\n", message.Method, notificationSummary(message))
	if message.Method == "turn/completed" {
		var params struct {
			Turn struct {
				Status string `json:"status"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return fmt.Errorf("decode turn/completed: %w", err)
		}
		c.state.turnCompleted = true
		c.state.turnStatus = params.Turn.Status
	}
	return nil
}

func (c *appServerClient) handleServerRequest(message rpcMessage) error {
	if message.Method != "item/tool/call" {
		return c.send(map[string]any{
			"id": message.ID,
			"error": map[string]any{
				"code":    -32601,
				"message": "unsupported probe server request: " + message.Method,
			},
		})
	}

	var params struct {
		Tool      string          `json:"tool"`
		CallID    string          `json:"callId"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return fmt.Errorf("decode item/tool/call: %w", err)
	}
	if params.Tool != questionsTool {
		return c.send(map[string]any{
			"id": message.ID,
			"result": map[string]any{
				"success":      false,
				"contentItems": []any{map[string]any{"type": "inputText", "text": "unknown tool"}},
			},
		})
	}

	questionCount, err := decodeQuestionCount(params.Arguments)
	if err != nil {
		return fmt.Errorf("decode questions arguments: %w", err)
	}
	if strings.TrimSpace(params.CallID) == "" {
		return errors.New("questions call id must not be empty")
	}
	answers := make([]map[string]any, 0, questionCount)
	for index := 0; index < questionCount; index++ {
		answers = append(answers, map[string]any{
			"questionId": fmt.Sprintf("%s:%d", params.CallID, index), "selectedOptionId": c.answer,
			"customAnswer": "", "payload": map[string]any{},
		})
	}
	encoded, err := json.Marshal(map[string]any{"requestId": params.CallID, "answers": answers})
	if err != nil {
		return fmt.Errorf("encode questions result: %w", err)
	}
	c.state.questionsCalled = true
	fmt.Printf("   questions=%d answer=%q\n", questionCount, c.answer)
	return c.send(map[string]any{
		"id": message.ID,
		"result": map[string]any{
			"success": true,
			"contentItems": []any{
				map[string]any{"type": "inputText", "text": string(encoded)},
			},
		},
	})
}

func decodeQuestionCount(arguments json.RawMessage) (int, error) {
	var input struct {
		Questions []struct {
			Body string `json:"body"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return 0, err
	}
	if len(input.Questions) == 0 {
		return 0, errors.New("questions must not be empty")
	}
	for _, question := range input.Questions {
		if strings.TrimSpace(question.Body) == "" {
			return 0, errors.New("question body must not be empty")
		}
	}
	return len(input.Questions), nil
}

func (c *appServerClient) send(value any) error {
	return c.encoder.Encode(value)
}

func (c *appServerClient) receive(ctx context.Context) (rpcMessage, error) {
	select {
	case <-ctx.Done():
		return rpcMessage{}, ctx.Err()
	case incoming, ok := <-c.incoming:
		if !ok {
			return rpcMessage{}, io.EOF
		}
		return incoming.message, incoming.err
	}
}

func readMessages(reader io.Reader) <-chan incomingMessage {
	messages := make(chan incomingMessage, 256)
	go func() {
		defer close(messages)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var message rpcMessage
			if err := json.Unmarshal(line, &message); err != nil {
				messages <- incomingMessage{err: fmt.Errorf("decode app-server message: %w", err)}
				return
			}
			messages <- incomingMessage{message: message}
		}
		if err := scanner.Err(); err != nil {
			messages <- incomingMessage{err: err}
		}
	}()
	return messages
}

func printStderr(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fmt.Fprintf(os.Stderr, "app-server: %s\n", scanner.Text())
	}
}

func finish(cmd *exec.Cmd, stdin io.Closer, probeErr error) error {
	_ = stdin.Close()
	waitErr := cmd.Wait()
	if probeErr != nil {
		return probeErr
	}
	if waitErr != nil {
		return fmt.Errorf("app-server exit: %w", waitErr)
	}
	return nil
}

func rawIDEqual(raw json.RawMessage, id int) bool {
	var got int
	return json.Unmarshal(raw, &got) == nil && got == id
}

func hasID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "null"
	}
	return string(bytes.TrimSpace(raw))
}

func notificationSummary(message rpcMessage) string {
	if message.Method != "item/started" && message.Method != "item/completed" {
		return ""
	}
	var params struct {
		Item struct {
			Type   string `json:"type"`
			Tool   string `json:"tool"`
			Status string `json:"status"`
		} `json:"item"`
	}
	if json.Unmarshal(message.Params, &params) != nil {
		return ""
	}
	return fmt.Sprintf(" type=%s tool=%s status=%s", params.Item.Type, params.Item.Tool, params.Item.Status)
}

func absolutePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("cwd is required")
	}
	return filepath.Abs(path)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
