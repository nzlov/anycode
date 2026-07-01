package codexcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nzlov/anycode/internal/domain/process"
)

const defaultBin = "codex"

type Client struct {
	bin string
}

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
	if len(e.Args) == 0 {
		return fmt.Sprintf("codex probe %s for %q: %v", e.Code, e.Bin, e.Err)
	}
	return fmt.Sprintf("codex probe %s for %q %s: %v", e.Code, e.Bin, strings.Join(e.Args, " "), e.Err)
}

func (e *ProbeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(bin string) *Client {
	if bin == "" {
		bin = os.Getenv("CODEX_BIN")
	}
	if bin == "" {
		bin = defaultBin
	}
	return &Client{bin: bin}
}

func (c *Client) Bin() string {
	if c == nil || c.bin == "" {
		return defaultBin
	}
	return c.bin
}

func (c *Client) Probe(ctx context.Context) (process.CodexCapabilities, error) {
	bin := c.Bin()
	version, err := runText(ctx, bin, "--version")
	if err != nil {
		return process.CodexCapabilities{}, &ProbeError{
			Code: "version_failed",
			Bin:  bin,
			Args: []string{"--version"},
			Err:  err,
		}
	}

	return process.CodexCapabilities{
		Version:        firstLine(version),
		SupportsExec:   commandWorks(ctx, bin, "exec", "--help"),
		SupportsResume: commandWorks(ctx, bin, "exec", "resume", "--help"),
	}, nil
}

func runText(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", commandError(err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func commandWorks(ctx context.Context, bin string, args ...string) bool {
	_, err := runText(ctx, bin, args...)
	return err == nil
}

func commandError(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, stderr)
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	line, _, _ := strings.Cut(value, "\n")
	return strings.TrimSpace(line)
}

func IsProbeError(err error) bool {
	var probeErr *ProbeError
	return errors.As(err, &probeErr)
}
