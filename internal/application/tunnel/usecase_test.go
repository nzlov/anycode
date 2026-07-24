package tunnel

import (
	"context"
	"strings"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/tunnel"
)

type runtimeStub struct {
	started domain.StartInput
	items   []domain.Tunnel
	closed  []domain.ID
}

func (r *runtimeStub) Start(_ context.Context, input domain.StartInput) (domain.Tunnel, error) {
	r.started = input
	input.Tunnel.Hostname = "example.trycloudflare.com"
	input.Tunnel.URL = "https://example.trycloudflare.com"
	input.Tunnel.AccessURL = "https://example.trycloudflare.com/?anycode_auth=" + input.Auth
	input.Tunnel.Status = domain.StatusRunning
	r.items = append(r.items, input.Tunnel)
	return input.Tunnel, nil
}

func (r *runtimeStub) List(context.Context) ([]domain.Tunnel, error) { return r.items, nil }
func (r *runtimeStub) Close(_ context.Context, id domain.ID) error {
	r.closed = append(r.closed, id)
	return nil
}
func (r *runtimeStub) CloseSession(context.Context, domain.SessionID) error { return nil }
func (r *runtimeStub) CloseAll(context.Context) error                       { return nil }

func TestCreateReturnsAnyCodeAuthURL(t *testing.T) {
	runtime := &runtimeStub{}
	service := New(runtime)
	service.random = func(size int) (string, error) {
		if size == 12 {
			return "ABC123", nil
		}
		return "secret-token", nil
	}
	service.now = func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) }

	result, err := service.Create(context.Background(), CreateInput{SessionID: "session-1", Port: 4173})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccessURL != "https://example.trycloudflare.com/?anycode_auth=secret-token" {
		t.Fatalf("access URL = %q", result.AccessURL)
	}
	if runtime.started.Auth != "secret-token" {
		t.Fatal("runtime did not receive the auth token")
	}
	if strings.Contains(result.Tunnel.URL, "secret-token") || result.Tunnel.AccessURL != result.AccessURL {
		t.Fatal("public URL or access URL is incorrect")
	}
}

func TestCloseOwnedRejectsAnotherSession(t *testing.T) {
	runtime := &runtimeStub{items: []domain.Tunnel{{ID: "tunnel-1", SessionID: "session-1"}}}
	service := New(runtime)

	if err := service.CloseOwned(context.Background(), "session-2", "tunnel-1"); err == nil {
		t.Fatal("expected an ownership error")
	}
	if len(runtime.closed) != 0 {
		t.Fatalf("closed tunnels = %#v", runtime.closed)
	}
}

func TestCreateRejectsReservedPort(t *testing.T) {
	service := New(&runtimeStub{}, WithReservedPorts(8080))
	if _, err := service.Create(context.Background(), CreateInput{SessionID: "session-1", Port: 8080}); err == nil {
		t.Fatal("expected reserved port validation error")
	}
}
