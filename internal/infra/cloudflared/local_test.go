package cloudflared

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	domain "github.com/nzlov/anycode/internal/domain/tunnel"
)

func TestLocalTunnelAuthenticationForwardingAndClose(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || strings.Contains(r.Header.Get("Cookie"), "anycode_tunnel_") {
			t.Error("credential forwarded")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/next?q=1", http.StatusFound)
			return
		}
		if r.URL.Path == "/ws" {
			conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			for {
				kind, body, err := conn.ReadMessage()
				if err != nil {
					break
				}
				if err := conn.WriteMessage(kind, body); err != nil {
					break
				}
			}
			return
		}
		_, _ = io.WriteString(w, r.URL.RequestURI())
	}))
	defer origin.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(origin.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	runtime, err := New("/nonexistent/cloudflared")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.CloseAll(context.Background())
	start := func(id string) domain.Tunnel {
		item, err := runtime.Start(context.Background(), domain.StartInput{Tunnel: domain.Tunnel{ID: domain.ID(id), SessionID: "s", Mode: domain.ModeLocal, Port: port}, Auth: "secret"})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	item := start("tunnel-local")
	other := start("tunnel-other")
	server := httptest.NewServer(http.HandlerFunc(runtime.ServeLocalHTTP))
	defer server.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string, cookie *http.Cookie) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer anycode-secret")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	if res := get(item.URL, nil); res.StatusCode != 401 {
		t.Fatalf("unauthenticated status %d", res.StatusCode)
	}
	if res := get(item.URL+"?anycode_auth=bad", nil); res.StatusCode != 401 {
		t.Fatalf("bad auth status %d", res.StatusCode)
	}
	login := get(item.AccessURL, nil)
	if login.StatusCode != 303 || login.Header.Get("Location") != item.URL {
		t.Fatalf("login = %d %s", login.StatusCode, login.Header.Get("Location"))
	}
	cookies := login.Cookies()
	if len(cookies) != 1 || cookies[0].Path != item.URL || !cookies[0].HttpOnly || cookies[0].Secure {
		t.Fatalf("cookies %#v", cookies)
	}
	cookie := cookies[0]
	res := get(item.URL+"assets/app.js?x=1", cookie)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(body) != "/assets/app.js?x=1" {
		t.Fatalf("forward %d %s", res.StatusCode, body)
	}
	if res := get(other.URL, cookie); res.StatusCode != 401 {
		t.Fatalf("cross-tunnel auth %d", res.StatusCode)
	}
	if res := get(item.URL+"redirect", cookie); res.Header.Get("Location") != item.URL+"next?q=1" {
		t.Fatalf("redirect %s", res.Header.Get("Location"))
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+item.URL+"ws", http.Header{"Cookie": {cookie.String()}, "Origin": {server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil || string(data) != "hello" {
		t.Fatalf("websocket %q %v", data, err)
	}
	if err := runtime.Close(context.Background(), item.ID); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("closed tunnel left WebSocket active")
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("closing tunnel did not disconnect WebSocket")
	}
	if res := get(item.URL, cookie); res.StatusCode != 404 {
		t.Fatalf("closed status %d", res.StatusCode)
	}
}
