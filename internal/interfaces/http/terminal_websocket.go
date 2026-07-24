package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	terminaldomain "github.com/nzlov/anycode/internal/domain/terminal"
)

const (
	terminalAuthTimeout = 5 * time.Second
	terminalWriteWait   = 10 * time.Second
	terminalReadLimit   = 64 << 10
)

type terminalSessionReader interface {
	GetSession(ctx context.Context, id sessiondomain.ID) (sessionapp.DetailDTO, error)
}

type terminalClientMessage struct {
	Type          string `json:"type"`
	Authorization string `json:"authorization"`
	Cols          uint16 `json:"cols"`
	Rows          uint16 `json:"rows"`
}

type terminalServerMessage struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

func newTerminalWebSocketHandler(sessions terminalSessionReader, runtime terminaldomain.Runtime, accessKey string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessions == nil || runtime == nil {
			http.Error(w, "terminal unavailable", http.StatusServiceUnavailable)
			return
		}
		upgrader := websocket.Upgrader{
			HandshakeTimeout: terminalAuthTimeout,
			CheckOrigin:      terminalOriginAllowed,
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(terminalReadLimit)
		_ = conn.SetReadDeadline(time.Now().Add(terminalAuthTimeout))
		var init terminalClientMessage
		if err := conn.ReadJSON(&init); err != nil || init.Type != "connection_init" || (accessKey != "" && !validBearer(accessKey, init.Authorization)) {
			writeTerminalJSON(conn, terminalServerMessage{Type: "error", Message: "unauthorized"})
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "unauthorized"), time.Now().Add(terminalWriteWait))
			return
		}
		_ = conn.SetReadDeadline(time.Time{})

		sessionID := sessiondomain.ID(strings.TrimSpace(r.PathValue("id")))
		session, err := sessions.GetSession(r.Context(), sessionID)
		if err != nil || session.Mode != sessiondomain.ModeTerminal {
			writeTerminalJSON(conn, terminalServerMessage{Type: "error", Message: "terminal session not found"})
			return
		}
		subscription, err := runtime.Subscribe(terminaldomain.SessionID(sessionID))
		if err != nil {
			writeTerminalJSON(conn, terminalServerMessage{Type: "error", Message: "terminal is not running"})
			return
		}
		defer subscription.Close()
		if err := writeTerminalJSON(conn, terminalServerMessage{Type: "ready"}); err != nil {
			return
		}
		if len(subscription.Replay) > 0 {
			if err := writeTerminalBinary(conn, subscription.Replay); err != nil {
				return
			}
		}

		controls := make(chan terminalServerMessage, 4)
		writerDone := make(chan struct{})
		stopWriter := make(chan struct{})
		go terminalSocketWriter(conn, subscription.Output, controls, stopWriter, writerDone)
		defer func() {
			close(stopWriter)
			_ = conn.Close()
			<-writerDone
		}()
		for {
			messageType, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			switch messageType {
			case websocket.BinaryMessage:
				if err := runtime.Write(terminaldomain.SessionID(sessionID), payload); err != nil {
					nonBlockingTerminalControl(controls, terminalServerMessage{Type: "error", Message: "terminal input failed"})
					return
				}
			case websocket.TextMessage:
				var message terminalClientMessage
				if json.Unmarshal(payload, &message) != nil {
					continue
				}
				if message.Type == "resize" {
					if err := runtime.Resize(terminaldomain.SessionID(sessionID), message.Cols, message.Rows); err != nil {
						nonBlockingTerminalControl(controls, terminalServerMessage{Type: "error", Message: "terminal resize failed"})
						return
					}
				}
			}
		}
	})
}

func terminalSocketWriter(conn *websocket.Conn, output <-chan []byte, controls <-chan terminalServerMessage, stop <-chan struct{}, done chan<- struct{}) {
	defer func() {
		_ = conn.Close()
		close(done)
	}()
	for {
		select {
		case chunk, ok := <-output:
			if !ok {
				_ = writeTerminalJSON(conn, terminalServerMessage{Type: "exit"})
				return
			}
			if err := writeTerminalBinary(conn, chunk); err != nil {
				return
			}
		case message := <-controls:
			if err := writeTerminalJSON(conn, message); err != nil {
				return
			}
		case <-stop:
			return
		}
	}
}

func writeTerminalJSON(conn *websocket.Conn, message terminalServerMessage) error {
	_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
	return conn.WriteJSON(message)
}

func writeTerminalBinary(conn *websocket.Conn, data []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(terminalWriteWait))
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func nonBlockingTerminalControl(target chan<- terminalServerMessage, message terminalServerMessage) {
	select {
	case target <- message:
	default:
	}
}

func terminalOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
		return false
	}
	if r.TLS != nil {
		return parsed.Scheme == "https"
	}
	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	if strings.EqualFold(forwardedProto, "https") {
		return parsed.Scheme == "https"
	}
	return parsed.Scheme == "http"
}
