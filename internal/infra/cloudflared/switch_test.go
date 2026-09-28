package cloudflared

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/tunnel"
)

func TestSwitchModePreservesIdentityAndKeepsOriginalOnFailure(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer origin.Close()
	runtime, err := New("/missing/cloudflared")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.CloseAll(context.Background())
	initial, err := runtime.Start(context.Background(), domain.StartInput{Tunnel: domain.Tunnel{ID: "stable", SessionID: "s", Name: "preview", Mode: domain.ModeLocal, Port: origin.Listener.Addr().(*net.TCPAddr).Port, CreatedAt: time.Now().UTC()}, Auth: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.SwitchMode(context.Background(), initial.ID, domain.ModeCF); err == nil {
		t.Fatal("expected startup failure")
	}
	items, _ := runtime.List(context.Background())
	if len(items) != 1 || items[0] != initial {
		t.Fatalf("failed switch changed original: %#v", items)
	}
	if _, err := runtime.SwitchMode(context.Background(), initial.ID, "invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	bin := filepath.Join(t.TempDir(), "cloudflared")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'https://switch-test.trycloudflare.com\\n'\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runtime.bin = bin
	cf, err := runtime.SwitchMode(context.Background(), initial.ID, domain.ModeCF)
	if err != nil {
		t.Fatal(err)
	}
	if cf.ID != initial.ID || cf.Name != initial.Name || cf.Port != initial.Port || cf.CreatedAt != initial.CreatedAt || cf.Mode != domain.ModeCF {
		t.Fatalf("switched tunnel %#v", cf)
	}
	request := httptest.NewRequest(http.MethodGet, initial.AccessURL, nil)
	response := httptest.NewRecorder()
	runtime.ServeLocalHTTP(response, request)
	if response.Code != 404 {
		t.Fatalf("old route still active: %d", response.Code)
	}
	runtime.mu.RLock()
	cfEntry := runtime.entries[initial.ID]
	runtime.mu.RUnlock()
	local, err := runtime.SwitchMode(context.Background(), initial.ID, domain.ModeLocal)
	if err != nil {
		t.Fatal(err)
	}
	if local != initial {
		t.Fatalf("round-trip changed identity: %#v", local)
	}
	select {
	case <-cfEntry.done:
	default:
		t.Fatal("old CF process still active")
	}
	items, _ = runtime.List(context.Background())
	if len(items) != 1 || items[0] != initial {
		t.Fatalf("registry after switch: %#v", items)
	}
}

func TestCloseDuringSwitchDoesNotResurrectTunnel(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer origin.Close()
	bin := filepath.Join(t.TempDir(), "cloudflared")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntrap 'exit 0' TERM INT\nwhile :; do sleep 1; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	runtime, err := New(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.CloseAll(context.Background())
	initial, err := runtime.Start(context.Background(), domain.StartInput{Tunnel: domain.Tunnel{ID: "stable", SessionID: "s", Mode: domain.ModeLocal, Port: origin.Listener.Addr().(*net.TCPAddr).Port}, Auth: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := runtime.SwitchMode(ctx, initial.ID, domain.ModeCF); done <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		runtime.mu.RLock()
		count := len(runtime.entries)
		runtime.mu.RUnlock()
		if count == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("replacement never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	items, _ := runtime.List(context.Background())
	if len(items) != 1 || items[0] != initial {
		t.Fatalf("pending replacement was exposed %#v", items)
	}
	if _, err := runtime.SwitchMode(ctx, initial.ID, domain.ModeCF); err == nil {
		t.Fatal("concurrent switch accepted")
	}
	if err := runtime.Close(context.Background(), initial.ID); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed switch succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("switch did not stop")
	}
	items, _ = runtime.List(context.Background())
	if len(items) != 0 {
		t.Fatalf("closed tunnel resurrected %#v", items)
	}
}
