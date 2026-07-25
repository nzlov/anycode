package tunnel

import (
	"context"
	"strings"
	"testing"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
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
	for index, item := range r.items {
		if item.ID == id {
			r.items = append(r.items[:index], r.items[index+1:]...)
			break
		}
	}
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

	result, err := service.Create(context.Background(), CreateInput{SessionID: "session-1", Name: "Vite preview", Port: 4173})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccessURL != "https://example.trycloudflare.com/?anycode_auth=secret-token" {
		t.Fatalf("access URL = %q", result.AccessURL)
	}
	if runtime.started.Auth != "secret-token" {
		t.Fatal("runtime did not receive the auth token")
	}
	if result.Tunnel.Name != "Vite preview" || runtime.started.Tunnel.Name != "Vite preview" {
		t.Fatalf("tunnel name = %q", result.Tunnel.Name)
	}
	if strings.Contains(result.Tunnel.URL, "secret-token") || result.Tunnel.AccessURL != result.AccessURL {
		t.Fatal("public URL or access URL is incorrect")
	}
}

func TestCreateAndClosePublishRunningTunnelCount(t *testing.T) {
	runtime := &runtimeStub{}
	publisher := &eventPublisherStub{}
	service := New(runtime, WithEventPublisher(publisher))
	service.random = func(int) (string, error) { return "value", nil }
	service.now = func() time.Time { return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) }

	result, err := service.Create(context.Background(), CreateInput{SessionID: "session-1", Name: "preview", Port: 4173})
	if err != nil {
		t.Fatal(err)
	}
	if got := publisher.events[len(publisher.events)-1].Payload["runningCount"]; got != 1 {
		t.Fatalf("created tunnel count = %#v", got)
	}
	if err := service.Close(context.Background(), result.Tunnel.ID); err != nil {
		t.Fatal(err)
	}
	if got := publisher.events[len(publisher.events)-1].Payload["runningCount"]; got != 0 {
		t.Fatalf("closed tunnel count = %#v", got)
	}
	for _, event := range publisher.events {
		if event.Type != "tunnel.count_updated" {
			t.Fatalf("event type = %q", event.Type)
		}
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
	if _, err := service.Create(context.Background(), CreateInput{SessionID: "session-1", Name: "app", Port: 8080}); err == nil {
		t.Fatal("expected reserved port validation error")
	}
}

func TestCreateRequiresName(t *testing.T) {
	service := New(&runtimeStub{})
	if _, err := service.Create(context.Background(), CreateInput{SessionID: "session-1", Port: 4173}); err == nil {
		t.Fatal("expected tunnel name validation error")
	}
}

type eventPublisherStub struct {
	events []eventdomain.DomainEvent
}

func (p *eventPublisherStub) PublishAfterCommit(_ context.Context, event eventdomain.DomainEvent) error {
	p.events = append(p.events, event)
	return nil
}
