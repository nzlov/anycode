package mcpstdio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestRunAcknowledgesDeliveryAfterWritingResponse(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "mcp.sock")
	written := make(chan struct{}, 1)
	ackAfterWrite := make(chan bool, 1)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp/sessions/session-1":
			if got := r.Header.Get("X-AnyCode-MCP-Transport"); got != "stdio" {
				t.Errorf("transport header = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": `{"batchId":"batch-1","status":"answered"}`}},
					"isError": false,
				},
			})
		case "/mcp/sessions/session-1/deliveries/batch-1/ack":
			select {
			case <-written:
				ackAfterWrite <- true
			default:
				ackAfterWrite <- false
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()

	var output bytes.Buffer
	writer := writeObserver{Writer: &output, written: written}
	request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"answer_user"}}`
	err = Run(context.Background(), strings.NewReader(frame(request)), writer, Config{
		SessionID: "session-1",
		Socket:    socketPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if afterWrite := <-ackAfterWrite; !afterWrite {
		t.Fatal("delivery was acknowledged before the MCP response was written")
	}
	if batchID := directAnswerBatchID([]byte(unframe(t, output.String()))); batchID != "batch-1" {
		t.Fatalf("response = %s", output.String())
	}
}

func TestRunFailsDirectDeliveryWhenFinalBoundaryFails(t *testing.T) {
	for _, test := range []struct {
		name      string
		writer    io.Writer
		ackStatus int
	}{
		{name: "stdout write", writer: errorWriter{}, ackStatus: http.StatusNoContent},
		{name: "delivery ack", writer: &bytes.Buffer{}, ackStatus: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			socketPath := filepath.Join(dir, "mcp.sock")
			failed := make(chan string, 1)
			listener, err := net.Listen("unix", socketPath)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/mcp/sessions/session-1":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"jsonrpc": "2.0", "id": 1,
						"result": map[string]any{"content": []map[string]any{{"type": "text", "text": `{"batchId":"batch-1","status":"answered"}`}}, "isError": false},
					})
				case "/mcp/sessions/session-1/deliveries/batch-1/ack":
					w.WriteHeader(test.ackStatus)
				case "/mcp/sessions/session-1/deliveries/batch-1/fail":
					failed <- r.URL.Path
					w.WriteHeader(http.StatusNoContent)
				default:
					http.NotFound(w, r)
				}
			})}
			defer server.Close()
			go func() { _ = server.Serve(listener) }()

			request := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"answer_user"}}`
			err = Run(context.Background(), strings.NewReader(frame(request)), test.writer, Config{SessionID: "session-1", Socket: socketPath})
			if err == nil {
				t.Fatal("final boundary failure was not returned")
			}
			select {
			case path := <-failed:
				if !strings.HasSuffix(path, "/fail") {
					t.Fatalf("failure path = %q", path)
				}
			case <-time.After(time.Second):
				t.Fatal("delivery failure was not reported")
			}
		})
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

type writeObserver struct {
	io.Writer
	written chan<- struct{}
}

func (w writeObserver) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	if err == nil {
		select {
		case w.written <- struct{}{}:
		default:
		}
	}
	return n, err
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
