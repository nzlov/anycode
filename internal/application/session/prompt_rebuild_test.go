package session

import (
	"context"
	"strings"
	"testing"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

func TestExecuteSessionRebuiltPromptDeduplicatesAppends(t *testing.T) {
	for _, threadID := range []string{"codex-session-old", ""} {
		name := "unavailable thread"
		if threadID == "" {
			name = "restart without thread"
		}
		t.Run(name, func(t *testing.T) {
			repo := newFakeRepository()
			repo.sessions["session-1"] = domain.Session{
				ID:             "session-1",
				ProjectID:      "project-1",
				Requirement:    "implement session",
				Mode:           domain.ModeChat,
				Status:         domain.StatusStopped,
				CodexSessionID: threadID,
				WorktreePath:   "/workspace/session-1",
			}
			repo.appends = []domain.PromptAppend{
				{ID: "append-1", SessionID: "session-1", Body: "continue work", Status: domain.PromptAppendDispatched},
				{ID: "append-2", SessionID: "session-1", Body: "verify build", Status: domain.PromptAppendDispatched},
				{ID: "append-3", SessionID: "session-1", Body: "\n continue work \t", Status: domain.PromptAppendPending},
				{
					ID: "append-4", SessionID: "session-1", Body: "continue work", Status: domain.PromptAppendPending,
					Annotations: []domain.PromptAnnotation{{ID: "annotation-1", Content: "inspect edge cases"}},
				},
				{
					ID: "append-5", SessionID: "session-1", Body: " continue work ", Status: domain.PromptAppendPending,
					Annotations: []domain.PromptAnnotation{{ID: "annotation-2", Content: "\ninspect edge cases\n"}},
				},
				{
					ID: "append-6", SessionID: "session-1", Status: domain.PromptAppendPending,
					Annotations: []domain.PromptAnnotation{{ID: "annotation-3", Content: "retain manual changes"}},
				},
				{
					ID: "append-7", SessionID: "session-1", Body: " \n ", Status: domain.PromptAppendPending,
					Annotations: []domain.PromptAnnotation{{ID: "annotation-4", Content: " retain manual changes "}},
				},
			}
			codex := &fakeCodexProcess{
				resumeErr:   processdomain.ErrThreadUnavailable,
				startHandle: processdomain.CodexHandle{CodexSessionID: "codex-session-new"},
			}
			service := New(repo, newFakeProjectRepository("project-1"), WithProcesses(newFakeProcessRepository(), codex))
			service.generateID = func() (domain.ID, error) { return "process-run-1", nil }

			got, err := service.ExecuteSessionWithOptions(context.Background(), "session-1", StartSessionOptions{Force: true})
			if err != nil {
				t.Fatalf("ExecuteSession() error = %v", err)
			}
			if got.Status != domain.StatusRunning || got.CodexSessionID != "codex-session-new" || !codex.startCalled {
				t.Fatalf("ExecuteSession() = %#v, start called = %v", got, codex.startCalled)
			}
			if codex.resumeCalled != (threadID != "") {
				t.Fatalf("resume called = %v, thread ID = %q", codex.resumeCalled, threadID)
			}
			want := strings.Join([]string{
				rebuiltPromptNotice,
				"原始需求：\nimplement session",
				"追加描述：\ncontinue work",
				"追加描述：\nverify build",
				"追加描述：\ncontinue work\n\ninspect edge cases",
				"追加描述：\nretain manual changes",
			}, "\n\n")
			if prompt := codexInputText(codex.startInput.Input); prompt != want {
				t.Fatalf("rebuilt prompt = %q, want %q", prompt, want)
			}
			if len(repo.appends) != 7 {
				t.Fatalf("prompt append history count = %d, want 7", len(repo.appends))
			}
			for _, promptAppend := range repo.appends[2:] {
				if promptAppend.Status != domain.PromptAppendInflight || promptAppend.DispatchedProcessRunID != "process-run-1" {
					t.Fatalf("pending append was not marked inflight: %#v", promptAppend)
				}
			}
		})
	}
}
