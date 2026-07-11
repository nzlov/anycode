package shellinit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
)

const (
	maxOutputBytes   = 64 * 1024
	terminateGrace   = 500 * time.Millisecond
	terminateTimeout = 5 * time.Second
)

type Runner struct{}

func New() *Runner {
	return &Runner{}
}

func (*Runner) Run(ctx context.Context, worktreePath string, script string) (session.WorktreeInitResult, error) {
	if err := ctx.Err(); err != nil {
		return session.WorktreeInitResult{}, err
	}

	operationCtx := context.WithoutCancel(ctx)
	executionCtx, cancelExecution := context.WithCancel(operationCtx)
	stopCancellationBridge := context.AfterFunc(ctx, cancelExecution)
	defer func() {
		stopCancellationBridge()
		cancelExecution()
	}()

	output := newTailBuffer(maxOutputBytes)
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = worktreePath
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return resultFromWait(output, err), fmt.Errorf("start worktree init command: %w", err)
	}

	waited := make(chan error, 1)
	go func() {
		waited <- cmd.Wait()
	}()

	select {
	case err := <-waited:
		return finishResult(output, err)
	case <-executionCtx.Done():
		select {
		case err := <-waited:
			return finishResult(output, err)
		default:
		}

		terminationCtx, cancelTermination := context.WithTimeout(operationCtx, terminateTimeout)
		defer cancelTermination()
		waitErr, terminateErr := terminateProcessGroup(terminationCtx, cmd, waited)
		result := resultFromWait(output, waitErr)
		requestErr := ctx.Err()
		if requestErr == nil {
			requestErr = executionCtx.Err()
		}
		return result, errors.Join(requestErr, terminateErr)
	}
}

func finishResult(output *tailBuffer, waitErr error) (session.WorktreeInitResult, error) {
	result := resultFromWait(output, waitErr)
	if waitErr == nil || isExitError(waitErr) {
		return result, nil
	}
	return result, fmt.Errorf("wait for worktree init command: %w", waitErr)
}

func resultFromWait(output *tailBuffer, waitErr error) session.WorktreeInitResult {
	result := session.WorktreeInitResult{
		Success:         waitErr == nil,
		Output:          output.String(),
		OutputTruncated: output.Truncated(),
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		exitCode := exitErr.ExitCode()
		result.ExitCode = &exitCode
	}
	return result
}

func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func terminateProcessGroup(ctx context.Context, cmd *exec.Cmd, waited <-chan error) (error, error) {
	pid := cmd.Process.Pid
	var terminateErr error
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		terminateErr = fmt.Errorf("terminate worktree init process group: %w", err)
	}

	grace := time.NewTimer(terminateGrace)
	defer grace.Stop()
	waitCh := waited
	var waitErr error
	parentWaited := false

waitForGrace:
	for {
		select {
		case waitErr = <-waitCh:
			parentWaited = true
			waitCh = nil
		case <-grace.C:
			break waitForGrace
		case <-ctx.Done():
			break waitForGrace
		}
	}

	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		terminateErr = errors.Join(terminateErr, fmt.Errorf("kill worktree init process group: %w", err))
	}
	if parentWaited {
		return waitErr, terminateErr
	}
	select {
	case waitErr = <-waited:
		return waitErr, terminateErr
	case <-ctx.Done():
		return nil, errors.Join(terminateErr, fmt.Errorf("wait for worktree init process group: %w", ctx.Err()))
	}
}

type tailBuffer struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	written := len(p)
	if b.limit <= 0 {
		b.truncated = b.truncated || written > 0
		return written, nil
	}
	if len(p) >= b.limit {
		hadData := len(b.data) > 0
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		b.truncated = b.truncated || hadData || len(p) > b.limit
		return written, nil
	}
	if overflow := len(b.data) + len(p) - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return written, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func (b *tailBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
