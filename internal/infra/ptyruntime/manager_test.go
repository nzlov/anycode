package ptyruntime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	terminaldomain "github.com/nzlov/anycode/internal/domain/terminal"
)

func TestManagerRunsInteractiveShellAndReplaysOutput(t *testing.T) {
	manager := New(WithShell("/bin/sh"), WithStopTimeout(time.Second))
	t.Cleanup(func() { _ = manager.Close() })

	handle, err := manager.Start(context.Background(), terminaldomain.StartInput{
		SessionID: "session-1",
		Workdir:   t.TempDir(),
		Cols:      90,
		Rows:      24,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	subscription, err := manager.Subscribe("session-1")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	t.Cleanup(subscription.Close)

	if err := manager.Write("session-1", []byte("printf 'terminal-ready\\n'\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	waitForOutput(t, subscription.Output, "terminal-ready")

	reconnected, err := manager.Subscribe("session-1")
	if err != nil {
		t.Fatalf("Subscribe() reconnect error = %v", err)
	}
	defer reconnected.Close()
	if !bytes.Contains(reconnected.Replay, []byte("terminal-ready")) {
		t.Fatalf("Replay = %q, want terminal output", reconnected.Replay)
	}

	if err := manager.Write("session-1", []byte("exit 7\n")); err != nil {
		t.Fatalf("Write(exit) error = %v", err)
	}
	select {
	case result := <-handle.Exit:
		if result.ExitCode == nil || *result.ExitCode != 7 {
			t.Fatalf("ExitCode = %v, want 7", result.ExitCode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("terminal did not exit")
	}
}

func TestManagerResizeAndStop(t *testing.T) {
	manager := New(WithShell("/bin/sh"), WithStopTimeout(time.Second))
	t.Cleanup(func() { _ = manager.Close() })

	_, err := manager.Start(context.Background(), terminaldomain.StartInput{
		SessionID: "session-2",
		Workdir:   t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	subscription, err := manager.Subscribe("session-2")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()
	if err := manager.Resize("session-2", 101, 37); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}
	if err := manager.Write("session-2", []byte("stty size\n")); err != nil {
		t.Fatalf("Write(stty) error = %v", err)
	}
	waitForOutput(t, subscription.Output, "37 101")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := manager.Stop(ctx, "session-2"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := manager.Write("session-2", []byte("pwd\n")); !errors.Is(err, terminaldomain.ErrRunNotFound) {
		t.Fatalf("Write() after stop error = %v, want ErrRunNotFound", err)
	}
}

func TestManagerSummaryTracksDirectoryAndSubmittedCommands(t *testing.T) {
	manager := New(WithShell("/bin/sh"), WithStopTimeout(time.Second))
	t.Cleanup(func() { _ = manager.Close() })
	workdir := t.TempDir()
	_, err := manager.Start(context.Background(), terminaldomain.StartInput{
		SessionID: "session-summary",
		Workdir:   workdir,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	subscription, err := manager.Subscribe("session-summary")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer subscription.Close()
	if err := manager.Write("session-summary", []byte("mkdir child\ncd child\npwd\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	waitForOutput(t, subscription.Output, workdir+"/child")
	summary, err := manager.Summary("session-summary")
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.CurrentDirectory != "…/child" {
		t.Fatalf("CurrentDirectory = %q", summary.CurrentDirectory)
	}
	if len(summary.Commands) != 3 || summary.Commands[0] != "mkdir child" || summary.Commands[1] != "cd child" || summary.Commands[2] != "pwd" {
		t.Fatalf("Commands = %#v", summary.Commands)
	}
}

func TestTerminalWorkdirDefaultsToUserHome(t *testing.T) {
	workdir, err := terminalWorkdir("")
	if err != nil {
		t.Fatalf("terminalWorkdir() error = %v", err)
	}
	if displayWorkdir(workdir) != "~" {
		t.Fatalf("displayWorkdir(%q) = %q", workdir, displayWorkdir(workdir))
	}
}

func TestManagerWithEmptyHistoryDefaultsToUserHome(t *testing.T) {
	manager := New(WithShell("/bin/sh"), WithStopTimeout(time.Second), WithHistoryDir(t.TempDir()))
	t.Cleanup(func() { _ = manager.Close() })
	if _, err := manager.Start(context.Background(), terminaldomain.StartInput{
		SessionID: "session-empty-history",
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	summary, err := manager.Summary("session-empty-history")
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.CurrentDirectory != "~" {
		t.Fatalf("CurrentDirectory = %q, want ~", summary.CurrentDirectory)
	}
}

func TestManagerRestoresTerminalHistoryAfterRestart(t *testing.T) {
	historyDir := t.TempDir()
	workdir := t.TempDir()
	manager := New(WithShell("/bin/sh"), WithStopTimeout(time.Second), WithHistoryDir(historyDir))
	_, err := manager.Start(context.Background(), terminaldomain.StartInput{
		SessionID: "session-persisted",
		Workdir:   workdir,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	subscription, err := manager.Subscribe("session-persisted")
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if err := manager.Write("session-persisted", []byte("printf 'persisted-output\\n'\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	waitForOutput(t, subscription.Output, "persisted-output")
	subscription.Close()
	if err := manager.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	restarted := New(WithShell("/bin/sh"), WithStopTimeout(time.Second), WithHistoryDir(historyDir))
	restored, err := restarted.Subscribe("session-persisted")
	if err != nil {
		t.Fatalf("Subscribe() after restart error = %v", err)
	}
	defer restored.Close()
	if !bytes.Contains(restored.Replay, []byte("persisted-output")) {
		t.Fatalf("Replay after restart = %q", restored.Replay)
	}
	summary, err := restarted.Summary("session-persisted")
	if err != nil {
		t.Fatalf("Summary() after restart error = %v", err)
	}
	if len(summary.Commands) != 1 || summary.Commands[0] != "printf 'persisted-output\\n'" {
		t.Fatalf("Commands after restart = %#v", summary.Commands)
	}
}

func waitForOutput(t *testing.T, output <-chan []byte, want string) {
	t.Helper()
	var received strings.Builder
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-output:
			if !ok {
				t.Fatalf("output closed before %q; received %q", want, received.String())
			}
			received.Write(chunk)
			if strings.Contains(received.String(), want) {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %q; received %q", want, received.String())
		}
	}
}
