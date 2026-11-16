package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

type profileInstructions struct {
	text string
	err  error
}

func (p profileInstructions) Instructions(context.Context) (string, error) { return p.text, p.err }

func TestProfileIsInjectedIntoStartResumeAndForkDeveloperInstructions(t *testing.T) {
	for _, operation := range []string{"start", "resume", "fork"} {
		t.Run(operation, func(t *testing.T) {
			codex := &fakeCodexProcess{}
			s := New(newFakeRepository(), newFakeProjectRepository("p"), WithProcesses(nil, codex), WithAgentProfile(profileInstructions{text: "避免未验证就宣称完成"}))
			options := codexStartOptions{prompt: "实现功能"}
			if operation == "resume" {
				options.resumeCodexSessionID = "thread"
			}
			if operation == "fork" {
				options.forkCodexSessionID = "thread"
			}
			_, err := s.startCodexProcess(context.Background(), domain.Session{ID: "s", ProjectID: "p"}, processdomain.RunID("r"), options, "/workspace")
			if err != nil {
				t.Fatal(err)
			}
			instructions := codex.startInput.DeveloperInstructions
			if operation == "resume" {
				instructions = codex.resumeInput.DeveloperInstructions
			}
			if operation == "fork" {
				instructions = codex.forkInput.DeveloperInstructions
			}
			if !strings.Contains(instructions, "避免未验证就宣称完成") || !strings.Contains(instructions, "AnyCode") {
				t.Fatalf("instructions = %q", instructions)
			}
		})
	}
}

func TestProfileReadFailurePreventsStartingWithoutConfiguredInstructions(t *testing.T) {
	codex := &fakeCodexProcess{}
	s := New(newFakeRepository(), newFakeProjectRepository("p"), WithProcesses(nil, codex), WithAgentProfile(profileInstructions{err: errors.New("db unavailable")}))
	if _, err := s.startCodexProcess(context.Background(), domain.Session{ID: "s"}, "r", codexStartOptions{}, "/workspace"); err == nil {
		t.Fatal("profile error ignored")
	}
	if codex.startInput.ProcessRunID != "" {
		t.Fatal("process started despite profile read failure")
	}
}
