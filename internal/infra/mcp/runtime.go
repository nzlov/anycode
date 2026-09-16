package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	domain "github.com/nzlov/anycode/internal/domain/mcp"
)

// Runtime owns live connections only. Configuration is resolved by the application on every request.
type Runtime struct {
	mu          sync.Mutex
	connections map[string]*connection
	done        chan struct{}
	closed      bool
}
type connection struct {
	mu          sync.Mutex
	session     *sdk.ClientSession
	fingerprint [32]byte
	used        time.Time
	users       int
	cleanup     func()
}

func New() *Runtime {
	r := &Runtime{connections: map[string]*connection{}, done: make(chan struct{})}
	go r.reap()
	return r
}
func (r *Runtime) reap() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.mu.Lock()
			var idle []*connection
			for key, c := range r.connections {
				if c.users == 0 && time.Since(c.used) > 5*time.Minute {
					delete(r.connections, key)
					idle = append(idle, c)
				}
			}
			r.mu.Unlock()
			for _, c := range idle {
				c.mu.Lock()
				c.close()
				c.mu.Unlock()
			}
		}
	}
}
func (r *Runtime) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.done)
	all := r.connections
	r.connections = map[string]*connection{}
	r.mu.Unlock()
	for _, c := range all {
		c.mu.Lock()
		c.close()
		c.mu.Unlock()
	}
}
func (r *Runtime) acquire(ctx context.Context, key, name string, d domain.Definition, workdir string) (*connection, error) {
	key = key + "/" + name
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, errors.New("MCP runtime closed")
	}
	c := r.connections[key]
	if c == nil {
		c = &connection{used: time.Now()}
		r.connections[key] = c
	}
	c.users++
	r.mu.Unlock()
	c.mu.Lock()
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed || ctx.Err() != nil {
		r.release(c)
		return nil, errors.New("MCP 请求已取消")
	}
	payload, _ := json.Marshal(struct {
		Definition domain.Definition
		Workdir    string
	}{d, workdir})
	fingerprint := sha256.Sum256(payload)
	if c.session != nil && fingerprint != c.fingerprint {
		c.close()
	}
	if c.session == nil {
		var transport sdk.Transport
		if d.Transport == "stdio" {
			cmd := exec.Command(d.Command, d.Args...)
			cmd.Dir = workdir
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			c.cleanup = func() {
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
			}
			env := map[string]string{}
			for _, v := range os.Environ() {
				k, val, ok := strings.Cut(v, "=")
				if ok {
					env[k] = val
				}
			}
			for k, v := range d.Env {
				env[k] = v
			}
			for k, v := range env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			transport = &sdk.CommandTransport{Command: cmd, TerminateDuration: time.Second}
		} else {
			transport = &sdk.StreamableClientTransport{Endpoint: d.URL, HTTPClient: &http.Client{Transport: headerTransport{headers: d.Headers}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, MaxRetries: -1}
		}
		client := sdk.NewClient(&sdk.Implementation{Name: "AnyCode", Version: "1"}, nil)
		session, err := client.Connect(ctx, transport, nil)
		if err != nil {
			c.close()
			r.release(c)
			return nil, errors.New("MCP 连接失败，请检查服务地址、启动命令和认证配置")
		}
		c.session = session
		c.fingerprint = fingerprint
	}
	return c, nil
}

func (c *connection) close() {
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
	if c.cleanup != nil {
		c.cleanup()
		c.cleanup = nil
	}
}
func (r *Runtime) release(c *connection) {
	c.mu.Unlock()
	r.mu.Lock()
	c.users--
	c.used = time.Now()
	r.mu.Unlock()
}

type headerTransport struct{ headers map[string]string }

func (t headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(req)
}
func (r *Runtime) Discover(ctx context.Context, key, name string, d domain.Definition, workdir string) ([]domain.Tool, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, err := r.acquire(ctx, key, name, d, workdir)
	if err != nil {
		return nil, "", err
	}
	defer r.release(c)
	tools := []domain.Tool{}
	for tool, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			c.close()
			return nil, "", errors.New("MCP 工具发现失败")
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, "", err
		}
		tools = append(tools, domain.Tool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
		if len(tools) > 1000 {
			return nil, "", errors.New("MCP 工具数量超过限制")
		}
	}
	return tools, c.session.InitializeResult().Instructions, nil
}
func (r *Runtime) Call(ctx context.Context, key, name string, d domain.Definition, workdir, tool string, args json.RawMessage) (domain.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	c, err := r.acquire(ctx, key, name, d, workdir)
	if err != nil {
		return domain.Result{}, err
	}
	defer r.release(c)
	result, err := c.session.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		c.close()
		return domain.Result{}, errors.New("MCP 调用失败或超时")
	}
	output := domain.Result{IsError: result.IsError}
	for _, content := range result.Content {
		switch v := content.(type) {
		case *sdk.TextContent:
			output.Content = append(output.Content, domain.Content{Type: "text", Text: v.Text})
		case *sdk.ImageContent:
			output.Content = append(output.Content, domain.Content{Type: "image", Data: v.Data, MIMEType: v.MIMEType})
		case *sdk.AudioContent:
			output.Content = append(output.Content, domain.Content{Type: "audio", Data: v.Data, MIMEType: v.MIMEType})
		default:
			data, e := json.Marshal(content)
			if e != nil {
				return domain.Result{}, fmt.Errorf("encode MCP content: %w", e)
			}
			output.Content = append(output.Content, domain.Content{Type: "text", Text: string(data)})
		}
	}
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return domain.Result{}, err
		}
		output.Content = append(output.Content, domain.Content{Type: "text", Text: string(data)})
	}
	return output, nil
}
