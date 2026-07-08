package mcpstdio

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunForwardsFramedRequestsToUnixSocketMCP(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "mcp.sock")
	token := "secret"
	gotPath := make(chan string, 1)
	gotAuth := make(chan string, 1)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		gotAuth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"tools": []map[string]any{{"name": "answer_user"}},
			},
		})
	})}
	defer server.Close()
	go func() {
		_ = server.Serve(listener)
	}()

	request := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	var output bytes.Buffer
	err = Run(context.Background(), strings.NewReader(frame(request)), &output, Config{
		SessionID: "session-1",
		Socket:    socketPath,
		AuthToken: token,
	})
	if err != nil {
		t.Fatal(err)
	}

	if path := <-gotPath; path != "/mcp/sessions/session-1" {
		t.Fatalf("path = %q", path)
	}
	if auth := <-gotAuth; auth != "Bearer "+token {
		t.Fatalf("auth = %q", auth)
	}
	body := unframe(t, output.String())
	if !strings.Contains(body, `"answer_user"`) {
		t.Fatalf("response missing answer_user: %s", body)
	}
}

func TestRunUsesAuthTokenFromEnv(t *testing.T) {
	t.Setenv("ANYCODE_MCP_TOKEN", "from-env")
	cfg := Config{AuthToken: ""}

	if got := cfg.authToken(); got != "from-env" {
		t.Fatalf("authToken = %q", got)
	}
}

func TestRunDoesNotWriteResponseForNotification(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "mcp.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})}
	defer server.Close()
	go func() {
		_ = server.Serve(listener)
	}()

	var output bytes.Buffer
	err = Run(context.Background(), strings.NewReader(frame(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)), &output, Config{
		SessionID: "session-1",
		Socket:    socketPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("notification output = %q", output.String())
	}
}

func frame(body string) string {
	return "Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

func unframe(t *testing.T, value string) string {
	t.Helper()
	_, rest, ok := strings.Cut(value, "\r\n\r\n")
	if !ok {
		t.Fatalf("missing frame separator: %q", value)
	}
	return rest
}
