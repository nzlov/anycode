package codexcli

import (
	"context"
	"errors"

	"github.com/nzlov/anycode/internal/domain/process"
)

var ErrProcessLifecycleUnsupported = errors.New("codex process lifecycle is not implemented")

func (c *Client) Start(context.Context, process.CodexStartInput) (process.CodexHandle, error) {
	return process.CodexHandle{}, ErrProcessLifecycleUnsupported
}

func (c *Client) Resume(context.Context, process.CodexResumeInput) (process.CodexHandle, error) {
	return process.CodexHandle{}, ErrProcessLifecycleUnsupported
}

func (c *Client) Stop(context.Context, process.RunID) error {
	return ErrProcessLifecycleUnsupported
}

func (c *Client) Events(context.Context, process.CodexHandle) (<-chan process.CodexEvent, error) {
	return nil, ErrProcessLifecycleUnsupported
}
