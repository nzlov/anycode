package shellinit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunnerExecutesMultilineScriptInWorktree(t *testing.T) {
	dir := t.TempDir()
	result, err := New().Run(context.Background(), dir, "pwd\nprintf 'first\\n'\nprintf 'second\\n'")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Success || result.ExitCode != nil || result.OutputTruncated {
		t.Fatalf("Run() result = %#v", result)
	}
	want := dir + "\nfirst\nsecond\n"
	if result.Output != want {
		t.Fatalf("Run() output = %q, want %q", result.Output, want)
	}
}

func TestRunnerReturnsNonzeroExitAsResult(t *testing.T) {
	result, err := New().Run(context.Background(), t.TempDir(), "printf 'failed\\n' >&2\nexit 7")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Success || result.ExitCode == nil || *result.ExitCode != 7 || result.Output != "failed\n" {
		t.Fatalf("Run() result = %#v", result)
	}
}

func TestRunnerKeepsBoundedOutputTail(t *testing.T) {
	result, err := New().Run(context.Background(), t.TempDir(), "head -c 70000 /dev/zero | tr '\\000' x\nprintf 'tail'")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Success || !result.OutputTruncated {
		t.Fatalf("Run() result = %#v", result)
	}
	if len(result.Output) != maxOutputBytes || !strings.HasSuffix(result.Output, "tail") {
		t.Fatalf("bounded output len/suffix = %d/%t", len(result.Output), strings.HasSuffix(result.Output, "tail"))
	}
}

func TestTailBufferDoesNotMarkExactLimitAsTruncated(t *testing.T) {
	buffer := newTailBuffer(4)
	if _, err := buffer.Write([]byte("test")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if buffer.String() != "test" || buffer.Truncated() {
		t.Fatalf("tail buffer = %q truncated=%t", buffer.String(), buffer.Truncated())
	}
}

func TestRunnerCancelsAndWaitsForProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := dir + "/child.pid"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := New().Run(ctx, dir, fmt.Sprintf("(trap '' TERM; exec sleep 30) & echo $! > %q\nwait", pidFile))
		done <- err
	}()

	childPID := waitForPID(t, pidFile)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not wait for process group termination")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(childPID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child process %d is still present after cancellation", childPID)
}

func processRunning(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err == nil {
		closing := strings.LastIndexByte(string(raw), ')')
		if closing >= 0 && len(raw) > closing+2 {
			return raw[closing+2] != 'Z'
		}
	}
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func TestRunnerReportsStartFailure(t *testing.T) {
	result, err := New().Run(context.Background(), t.TempDir()+"/missing", "echo never")
	if err == nil || result.Success {
		t.Fatalf("Run() = %#v, %v", result, err)
	}
}

func waitForPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatalf("parse child pid: %v", err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child pid file %q was not created", path)
	return 0
}
