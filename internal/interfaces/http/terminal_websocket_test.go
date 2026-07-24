package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	terminaldomain "github.com/nzlov/anycode/internal/domain/terminal"
)

func TestTerminalWebSocketAuthenticatesAndForwardsIO(t *testing.T) {
	output := make(chan []byte, 1)
	runtime := &fakeTerminalWebSocketRuntime{
		output:  output,
		replay:  []byte("replayed\r\n"),
		writes:  make(chan []byte, 1),
		resizes: make(chan [2]uint16, 1),
	}
	handler := newTerminalWebSocketHandler(fakeTerminalSessionReader{}, runtime, "secret")
	server := httptest.NewServer(handler)
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial terminal websocket: %v", err)
	}
	defer conn.Close()
	writeSocketJSON(t, conn, map[string]any{"type": "connection_init", "authorization": "Bearer secret"})
	assertSocketMessageType(t, conn, "ready")
	messageType, replay, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read replay: %v", err)
	}
	if messageType != websocket.BinaryMessage || string(replay) != "replayed\r\n" {
		t.Fatalf("replay type/data = %d/%q", messageType, replay)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("pwd\r")); err != nil {
		t.Fatalf("write terminal input: %v", err)
	}
	select {
	case input := <-runtime.writes:
		if string(input) != "pwd\r" {
			t.Fatalf("runtime input = %q", input)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not receive terminal input")
	}
	writeSocketJSON(t, conn, map[string]any{"type": "resize", "cols": 101, "rows": 37})
	select {
	case size := <-runtime.resizes:
		if size != [2]uint16{101, 37} {
			t.Fatalf("runtime resize = %v", size)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not receive terminal resize")
	}

	output <- []byte("/workspace\r\n")
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read terminal output: %v", err)
	}
	if messageType != websocket.BinaryMessage || string(payload) != "/workspace\r\n" {
		t.Fatalf("terminal output type/data = %d/%q", messageType, payload)
	}
}

func TestTerminalWebSocketRejectsInvalidConnectionInit(t *testing.T) {
	handler := newTerminalWebSocketHandler(fakeTerminalSessionReader{}, &fakeTerminalWebSocketRuntime{}, "secret")
	server := httptest.NewServer(handler)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial terminal websocket: %v", err)
	}
	defer conn.Close()
	writeSocketJSON(t, conn, map[string]any{"type": "connection_init", "authorization": "Bearer wrong"})
	message := readSocketMessage(t, conn)
	if message["type"] != "error" || message["message"] != "unauthorized" {
		t.Fatalf("auth error = %#v", message)
	}
}

func TestTerminalOriginAllowedRequiresSameHostAndScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://anycode.example/api/terminals/1/ws", nil)
	req.Header.Set("Origin", "http://anycode.example")
	if !terminalOriginAllowed(req) {
		t.Fatal("same-origin request was rejected")
	}
	req.Header.Set("Origin", "https://attacker.example")
	if terminalOriginAllowed(req) {
		t.Fatal("cross-origin request was allowed")
	}
}

type fakeTerminalSessionReader struct{}

func (fakeTerminalSessionReader) GetSession(context.Context, sessiondomain.ID) (sessionapp.DetailDTO, error) {
	return sessionapp.DetailDTO{DTO: sessionapp.DTO{Mode: sessiondomain.ModeTerminal}}, nil
}

type fakeTerminalWebSocketRuntime struct {
	output  chan []byte
	replay  []byte
	writes  chan []byte
	resizes chan [2]uint16
}

func (r *fakeTerminalWebSocketRuntime) Start(context.Context, terminaldomain.StartInput) (terminaldomain.Handle, error) {
	return terminaldomain.Handle{}, nil
}

func (r *fakeTerminalWebSocketRuntime) Write(_ terminaldomain.SessionID, data []byte) error {
	if r.writes != nil {
		r.writes <- append([]byte(nil), data...)
	}
	return nil
}

func (r *fakeTerminalWebSocketRuntime) Resize(_ terminaldomain.SessionID, cols uint16, rows uint16) error {
	if r.resizes != nil {
		r.resizes <- [2]uint16{cols, rows}
	}
	return nil
}

func (r *fakeTerminalWebSocketRuntime) Stop(context.Context, terminaldomain.SessionID) error {
	return nil
}

func (r *fakeTerminalWebSocketRuntime) Subscribe(terminaldomain.SessionID) (terminaldomain.OutputSubscription, error) {
	return terminaldomain.OutputSubscription{Replay: r.replay, Output: r.output, Close: func() {}}, nil
}

func (r *fakeTerminalWebSocketRuntime) Summary(terminaldomain.SessionID) (terminaldomain.Summary, error) {
	return terminaldomain.Summary{}, nil
}

func (r *fakeTerminalWebSocketRuntime) Close() error { return nil }
