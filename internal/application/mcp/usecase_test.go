package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	domain "github.com/nzlov/anycode/internal/domain/mcp"
	process "github.com/nzlov/anycode/internal/domain/process"
	project "github.com/nzlov/anycode/internal/domain/project"
	session "github.com/nzlov/anycode/internal/domain/session"
)

type memoryRepo struct {
	entries map[domain.Scope]map[string]domain.Entry
}

func (r *memoryRepo) List(_ context.Context, s domain.Scope) ([]domain.Entry, error) {
	out := []domain.Entry{}
	for _, e := range r.entries[s] {
		out = append(out, e)
	}
	return out, nil
}
func (r *memoryRepo) Save(_ context.Context, e domain.Entry) error {
	if r.entries[e.Scope] == nil {
		r.entries[e.Scope] = map[string]domain.Entry{}
	}
	r.entries[e.Scope][e.Name] = e
	return nil
}
func (r *memoryRepo) Delete(_ context.Context, s domain.Scope, n string) error {
	delete(r.entries[s], n)
	return nil
}

type projectFinder struct{}

func (projectFinder) Find(_ context.Context, id project.ID) (project.Project, error) {
	return project.Project{ID: id, Path: project.ProjectPath{Value: "/tmp"}}, nil
}

type sessionFinder struct{}

func (sessionFinder) Find(_ context.Context, id session.ID) (session.Session, error) {
	return session.Session{ID: id, ProjectID: "p", WorktreePath: "/tmp/card"}, nil
}

type fakeRuntime struct {
	calls       int
	discoveries int
	onDiscover  func()
}

func (r *fakeRuntime) Discover(context.Context, string, string, domain.Definition, string) ([]domain.Tool, string, error) {
	r.discoveries++
	if r.onDiscover != nil {
		r.onDiscover()
	}
	return []domain.Tool{{Name: "secret_tool", Description: "private_description", InputSchema: json.RawMessage(`{"type":"object"}`)}}, "private_instructions", nil
}
func (r *fakeRuntime) Call(context.Context, string, string, domain.Definition, string, string, json.RawMessage) (domain.Result, error) {
	r.calls++
	return domain.Result{Content: []domain.Content{{Type: "text", Text: "ok"}}}, nil
}
func TestSwitchesAndSecretRedaction(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{entries: map[domain.Scope]map[string]domain.Entry{}}
	runtime := &fakeRuntime{}
	s := New(repo, runtime, projectFinder{}, sessionFinder{})
	global := domain.Scope{Kind: "global"}
	card := domain.Scope{Kind: "session", ID: "c"}
	on, off := true, false
	definition := &domain.Definition{Transport: "http", URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "private-token"}}
	if err := s.Save(ctx, global, "docs", definition, &on); err != nil {
		t.Fatal(err)
	}
	views, err := s.List(ctx, global)
	if err != nil || views[0].Definition.Headers["Authorization"] != SecretMask {
		t.Fatalf("secret exposed: %#v %v", views, err)
	}
	views, _ = s.List(ctx, card)
	if views[0].Definition != nil {
		t.Fatal("card exposes connection configuration")
	}
	call := process.DynamicToolCall{SessionID: "c", Tool: "mcp_discover", Arguments: json.RawMessage(`{}`)}
	result, err := s.HandleDynamicTool(ctx, call)
	if err != nil || !strings.Contains(result.Content[0].Text, "secret_tool") {
		t.Fatalf("discovery: %#v %v", result, err)
	}
	if err := s.Save(ctx, card, "docs", nil, &off); err != nil {
		t.Fatal(err)
	}
	result, err = s.HandleDynamicTool(ctx, call)
	if err != nil || result.Content[0].Text != "[]" || runtime.discoveries != 1 {
		t.Fatalf("disabled information leak: %#v %v", result, err)
	}
	call.Tool = "mcp_call"
	call.Arguments = json.RawMessage(`{"server":"docs","tool":"secret_tool","arguments":{}}`)
	if _, err := s.HandleDynamicTool(ctx, call); err == nil || runtime.calls != 0 {
		t.Fatal("stale call was allowed")
	}
	if err := s.Delete(ctx, card, "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HandleDynamicTool(ctx, call); err != nil || runtime.calls != 1 {
		t.Fatalf("reset: %v", err)
	}
	if err := s.Save(ctx, card, "docs", definition, &on); err == nil {
		t.Fatal("card changed definition")
	}
	maskedDefinition := *definition
	maskedDefinition.Headers = map[string]string{"Authorization": SecretMask}
	if err := s.Save(ctx, global, "docs", &maskedDefinition, &on); err != nil {
		t.Fatal(err)
	}
	saved, _, _ := s.resolve(ctx, global)
	if saved[0].Definition.Headers["Authorization"] != "private-token" {
		t.Fatal("editing lost secret")
	}
}
func TestDisableDuringDiscovery(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{entries: map[domain.Scope]map[string]domain.Entry{}}
	runtime := &fakeRuntime{}
	s := New(repo, runtime, projectFinder{}, sessionFinder{})
	on, off := true, false
	if err := s.Save(ctx, domain.Scope{Kind: "global"}, "docs", &domain.Definition{Transport: "stdio", Command: "test"}, &on); err != nil {
		t.Fatal(err)
	}
	runtime.onDiscover = func() {
		if err := s.Save(ctx, domain.Scope{Kind: "session", ID: "c"}, "docs", nil, &off); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.HandleDynamicTool(ctx, process.DynamicToolCall{SessionID: "c", Tool: "mcp_discover", Arguments: json.RawMessage(`{}`)})
	if err != nil || result.Content[0].Text != "[]" {
		t.Fatalf("concurrent disable leaked: %#v %v", result, err)
	}
}
