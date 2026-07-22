package codexcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
)

const defaultBin = "codex"

type Client struct {
	bin       string
	codexHome string
	observer  Observer

	mu          sync.Mutex
	runtime     *appServerRuntime
	toolHandler process.DynamicToolHandler
}

type Observation struct {
	Name     string
	Outcome  string
	Reason   string
	Duration time.Duration
	Bytes    int64
}

type Observer interface {
	Observe(Observation)
}

type Option func(*Client)

type ProbeError struct {
	Code string
	Bin  string
	Args []string
	Err  error
}

func (e *ProbeError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("codex probe %s for %q: %v", e.Code, e.Bin, e.Err)
}

func (e *ProbeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func WithCodexHome(path string) Option {
	return func(c *Client) { c.codexHome = strings.TrimSpace(path) }
}

func WithObserver(observer Observer) Option {
	return func(c *Client) { c.observer = observer }
}

func New(bin string, options ...Option) *Client {
	if strings.TrimSpace(bin) == "" {
		bin = strings.TrimSpace(os.Getenv("CODEX_BIN"))
	}
	if bin == "" {
		bin = defaultBin
	}
	client := &Client{bin: bin}
	for _, option := range options {
		option(client)
	}
	return client
}

func (c *Client) Bin() string {
	if c == nil || c.bin == "" {
		return defaultBin
	}
	return c.bin
}

func (c *Client) CodexHome() string {
	if c != nil && c.codexHome != "" {
		return c.codexHome
	}
	if value := strings.TrimSpace(os.Getenv("CODEX_HOME")); value != "" {
		return value
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home + string(os.PathSeparator) + ".codex"
	}
	return ".codex"
}

func (c *Client) SetDynamicToolHandler(handler process.DynamicToolHandler) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.toolHandler = handler
	c.mu.Unlock()
}

func (c *Client) dynamicToolHandler() process.DynamicToolHandler {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.toolHandler
}

func (c *Client) appServer(ctx context.Context) (*appServerRuntime, error) {
	if c == nil {
		return nil, errors.New("codex client is nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runtime != nil && c.runtime.alive() {
		return c.runtime, nil
	}
	runtime, err := startAppServerRuntime(ctx, c)
	if err != nil {
		return nil, err
	}
	c.runtime = runtime
	return runtime, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	runtime := c.runtime
	c.runtime = nil
	c.mu.Unlock()
	if runtime == nil {
		return nil
	}
	return runtime.close()
}

func IsProbeError(err error) bool {
	var probeErr *ProbeError
	return errors.As(err, &probeErr)
}

func observe(observer Observer, observation Observation) {
	if observer != nil {
		observer.Observe(observation)
	}
}
