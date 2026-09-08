package codexcli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nzlov/anycode/internal/domain/process"
)

func TestSideTurnConfigAndAttachmentLifetime(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "close Side", true: "runtime shutdown"}[shutdown], func(t *testing.T) {
			requestLog := filepath.Join(t.TempDir(), "requests.jsonl")
			t.Setenv("SIDE_REQUEST_LOG", requestLog)
			bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"userAgent":"side-test"}}'
IFS= read -r request
IFS= read -r request
printf '%s\n' '{"id":2,"result":{"thread":{"id":"side-thread"}}}'
IFS= read -r request
printf '%s\n' "$request" >> "$SIDE_REQUEST_LOG"
printf '%s\n' '{"id":3,"result":{"turn":{"id":"turn-1"}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"side-thread","turn":{"id":"turn-1","status":"completed","items":[]}}}'
IFS= read -r request
printf '%s\n' "$request" >> "$SIDE_REQUEST_LOG"
printf '%s\n' '{"id":4,"error":{"code":-32600,"message":"model unavailable"}}'
IFS= read -r request
printf '%s\n' "$request" >> "$SIDE_REQUEST_LOG"
printf '%s\n' '{"id":5,"result":{"turn":{"id":"turn-2"}}}'
printf '%s\n' '{"method":"turn/completed","params":{"threadId":"side-thread","turn":{"id":"turn-2","status":"completed","items":[]}}}'
cat >/dev/null
`)
			client := New(bin, WithCodexHome(t.TempDir()))
			t.Cleanup(func() { _ = client.Close() })
			ctx := context.Background()
			handle, err := client.Fork(ctx, process.CodexForkInput{
				SourceCodexSessionID: "parent-thread", Ephemeral: true,
				CodexStartInput: process.CodexStartInput{
					ProcessRunID: "run-1", SessionID: "session-1", Workdir: t.TempDir(),
					Model: "first-model", ReasoningEffort: "high", FastMode: true, PermissionMode: "read-only",
					Input: []process.CodexInputItem{{Type: "localImage", Name: "image.png", Data: []byte("image")}, {Type: "mention", Name: "empty.txt", Data: []byte{}}},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			events, err := client.EphemeralEvents(ctx, handle.ProcessRunID)
			if err != nil {
				t.Fatal(err)
			}
			for range events {
			}
			readRequests := func() []map[string]any {
				t.Helper()
				data, err := os.ReadFile(requestLog)
				if err != nil {
					t.Fatal(err)
				}
				var requests []map[string]any
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					var request map[string]any
					if err := json.Unmarshal([]byte(line), &request); err != nil {
						t.Fatal(err)
					}
					requests = append(requests, request["params"].(map[string]any))
				}
				return requests
			}
			first := readRequests()[0]
			if first["model"] != "first-model" || first["effort"] != "high" || first["serviceTier"] != "priority" {
				t.Fatalf("first config = %#v", first)
			}
			var paths []string
			for _, item := range first["input"].([]any) {
				path := item.(map[string]any)["path"].(string)
				paths = append(paths, path)
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("file removed between turns: %v", err)
				}
			}
			resume := process.CodexResumeInput{
				ProcessRunID: "run-2", SessionID: "session-1", CodexSessionID: handle.CodexSessionID,
				Model: "second-model", ReasoningEffort: "low", FastMode: false, PermissionMode: "read-only",
				Input: []process.CodexInputItem{{Type: "mention", Name: "notes.txt", Data: []byte("notes")}},
			}
			foreign := resume
			foreign.SessionID = "other-session"
			if _, err := client.ContinueLoaded(ctx, foreign); !errors.Is(err, process.ErrThreadUnavailable) {
				t.Fatalf("foreign continuation = %v", err)
			}
			rejected := resume
			rejected.Model = "reject-model"
			if _, err := client.ContinueLoaded(ctx, rejected); err == nil {
				t.Fatal("expected failed turn")
			}
			failedPath := readRequests()[1]["input"].([]any)[0].(map[string]any)["path"].(string)
			if _, err := os.Stat(failedPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed turn leaked its upload")
			}
			for _, path := range paths {
				if _, err := os.Stat(path); err != nil {
					t.Fatal("failed follow-up removed earlier uploads")
				}
			}
			handle, err = client.ContinueLoaded(ctx, resume)
			if err != nil {
				t.Fatal(err)
			}
			events, err = client.EphemeralEvents(ctx, handle.ProcessRunID)
			if err != nil {
				t.Fatal(err)
			}
			for range events {
			}
			last := readRequests()[2]
			if last["model"] != "second-model" || last["effort"] != "low" || last["serviceTier"] != "default" || last["sandboxPolicy"].(map[string]any)["type"] != "readOnly" {
				t.Fatalf("follow-up config = %#v", last)
			}
			paths = append(paths, last["input"].([]any)[0].(map[string]any)["path"].(string))
			if shutdown {
				err = client.Close()
			} else {
				err = client.StopEphemeral(ctx, handle.ProcessRunID)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("Side input leaked: %s", path)
				}
			}
			if !shutdown {
				if _, err := client.ContinueLoaded(ctx, resume); !errors.Is(err, process.ErrThreadUnavailable) {
					t.Fatalf("closed Side continuation = %v", err)
				}
			}
		})
	}
}
