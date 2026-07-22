package codexcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSlashCommandsMatchesSupportedAppServerActions(t *testing.T) {
	commands := New("codex").SlashCommands()
	if len(commands) != 4 || commands[0].Name != "/review" || commands[1].Name != "/compact" || commands[2].Name != "/goal" || commands[3].Name != "/plan" {
		t.Fatalf("SlashCommands() = %#v", commands)
	}
	if !commands[0].AcceptsArgs || commands[0].RequiresThread || commands[1].AcceptsArgs || !commands[1].RequiresThread ||
		!commands[2].AcceptsArgs || !commands[2].RequiresThread || !commands[3].AcceptsArgs || commands[3].RequiresThread {
		t.Fatalf("SlashCommands() metadata = %#v", commands)
	}
}

func TestAppServerFileMentionsOnlyIncludesWorkspaceFiles(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"src/main.go": "package main\n",
		"docs/a b.md": "# docs\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside-link.txt")); err != nil {
		t.Fatal(err)
	}

	mentions := appServerFileMentions(
		`inspect @src/main.go and @"docs/a b.md" @../outside.txt @outside-link.txt @missing.go @src/main.go`,
		root,
	)
	if len(mentions) != 2 {
		t.Fatalf("appServerFileMentions() = %#v", mentions)
	}
	wantNames := []string{"src/main.go", "docs/a b.md"}
	for index, mention := range mentions {
		if mention["type"] != "mention" || mention["name"] != wantNames[index] {
			t.Fatalf("mention[%d] = %#v", index, mention)
		}
		if mention["path"] != filepath.Join(root, filepath.FromSlash(wantNames[index])) {
			t.Fatalf("mention[%d] path = %q", index, mention["path"])
		}
	}
}

func TestStartInputMapsSlashCommandsToAppServerMethods(t *testing.T) {
	tests := []struct {
		name       string
		prompt     string
		wantMethod string
		assert     func(*testing.T, map[string]any)
	}{
		{
			name:       "compact",
			prompt:     "/compact",
			wantMethod: "thread/compact/start",
		},
		{
			name:       "review workspace",
			prompt:     "/review",
			wantMethod: "review/start",
			assert: func(t *testing.T, request map[string]any) {
				target := request["params"].(map[string]any)["target"].(map[string]any)
				if target["type"] != "uncommittedChanges" {
					t.Fatalf("review target = %#v", target)
				}
			},
		},
		{
			name:       "review instructions",
			prompt:     "/review focus on auth",
			wantMethod: "review/start",
			assert: func(t *testing.T, request map[string]any) {
				target := request["params"].(map[string]any)["target"].(map[string]any)
				if target["type"] != "custom" || target["instructions"] != "focus on auth" {
					t.Fatalf("review target = %#v", target)
				}
			},
		},
		{
			name:       "goal",
			prompt:     "/goal ship fuzzy prompt completion",
			wantMethod: "thread/goal/set",
			assert: func(t *testing.T, request map[string]any) {
				params := request["params"].(map[string]any)
				if params["objective"] != "ship fuzzy prompt completion" || params["status"] != "active" {
					t.Fatalf("goal params = %#v", params)
				}
			},
		},
		{
			name:       "plan",
			prompt:     "/plan inspect @src/main.go",
			wantMethod: "turn/start",
			assert: func(t *testing.T, request map[string]any) {
				params := request["params"].(map[string]any)
				mode := params["collaborationMode"].(map[string]any)
				settings := mode["settings"].(map[string]any)
				if mode["mode"] != "plan" || settings["model"] != "gpt-test" || settings["reasoning_effort"] != "high" || settings["developer_instructions"] != nil {
					t.Fatalf("plan collaboration mode = %#v", mode)
				}
				inputs := params["input"].([]any)
				text := inputs[0].(map[string]any)
				if text["type"] != "text" || text["text"] != "inspect @src/main.go" {
					t.Fatalf("plan inputs = %#v", inputs)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			connection := &appServerConnection{
				reader: bufio.NewReader(strings.NewReader(`{"id":1,"result":{}}` + "\n")),
				writer: &output,
			}
			if _, err := connection.startInput("thread-1", appServerRunInput{
				prompt: test.prompt, model: "gpt-test", reasoningEffort: "high",
			}); err != nil {
				t.Fatal(err)
			}
			var request map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &request); err != nil {
				t.Fatal(err)
			}
			if request["method"] != test.wantMethod {
				t.Fatalf("method = %q, want %q", request["method"], test.wantMethod)
			}
			if test.assert != nil {
				test.assert(t, request)
			}
		})
	}
}

func TestStartInputRejectsSlashCommandsWithoutRequiredArguments(t *testing.T) {
	for _, prompt := range []string{"/goal", "/plan"} {
		t.Run(prompt, func(t *testing.T) {
			connection := &appServerConnection{
				reader: bufio.NewReader(strings.NewReader("")),
				writer: &bytes.Buffer{},
			}
			if _, err := connection.startInput("thread-1", appServerRunInput{prompt: prompt, model: "gpt-test"}); err == nil {
				t.Fatalf("startInput(%q) error = nil", prompt)
			}
		})
	}
}

func TestSearchFilesUsesAppServerFuzzySearchAndFiltersResults(t *testing.T) {
	requests := filepath.Join(t.TempDir(), "requests")
	bin := fakeCodex(t, `#!/bin/sh
IFS= read -r request
printf '%s\n' "$request" >> "$CODEX_STDIN_FILE"
printf '%s\n' '{"id":1,"result":{"userAgent":"test"}}'
IFS= read -r request
printf '%s\n' "$request" >> "$CODEX_STDIN_FILE"
IFS= read -r request
printf '%s\n' "$request" >> "$CODEX_STDIN_FILE"
printf '%s\n' '{"id":2,"result":{"files":[{"path":"src/main.go","match_type":"file","score":87,"indices":[4,5]},{"path":"src","match_type":"directory","score":70,"indices":[]},{"path":"../secret","match_type":"file","score":60,"indices":[]},{"path":"/absolute","match_type":"file","score":50,"indices":[]}]}}'
cat >/dev/null
`)
	t.Setenv("CODEX_STDIN_FILE", requests)
	root := t.TempDir()

	matches, err := New(bin).SearchFiles(context.Background(), root, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Path != "src/main.go" || matches[0].Score != 87 || !reflect.DeepEqual(matches[0].Indices, []uint32{4, 5}) {
		t.Fatalf("SearchFiles() = %#v", matches)
	}
	content, err := os.ReadFile(requests)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"method":"fuzzyFileSearch"`) || !strings.Contains(string(content), `"query":"main"`) {
		t.Fatalf("app-server requests = %s", content)
	}
}
