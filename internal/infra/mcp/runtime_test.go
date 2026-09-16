package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	domain "github.com/nzlov/anycode/internal/domain/mcp"
)

func testServer() *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: "fixture", Version: "1"}, &sdk.ServerOptions{Instructions: "fixture instructions"})
	count := 0
	server.AddTool(&sdk.Tool{Name: "echo", Description: "echo input", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"fail": map[string]any{"type": "boolean"}}}}, func(_ context.Context, r *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		count++
		var args struct {
			Fail bool `json:"fail"`
		}
		_ = json.Unmarshal(r.Params.Arguments, &args)
		return &sdk.CallToolResult{IsError: args.Fail, Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf("%d", count)}, &sdk.ImageContent{MIMEType: "image/png", Data: []byte("png")}}, StructuredContent: map[string]any{"count": count}}, nil
	})
	return server
}
func TestStdioHelper(t *testing.T) {
	if os.Getenv("ANYCODE_MCP_HELPER") != "1" {
		return
	}
	if err := testServer().Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
func TestStdioLifecycleAndCardIsolation(t *testing.T) {
	ctx := context.Background()
	r := New()
	defer r.Close()
	d := domain.Definition{Transport: "stdio", Command: os.Args[0], Args: []string{"-test.run=^TestStdioHelper$"}, Env: map[string]string{"ANYCODE_MCP_HELPER": "1"}}
	tools, instructions, err := r.Discover(ctx, "a", "docs", d, t.TempDir())
	if err != nil || len(tools) != 1 || instructions != "fixture instructions" {
		t.Fatalf("discover: %#v %s %v", tools, instructions, err)
	}
	dir := t.TempDir()
	for _, card := range []string{"a", "b"} {
		result, err := r.Call(ctx, card, "docs", d, dir, "echo", json.RawMessage(`{}`))
		if err != nil || result.Content[0].Text != "1" || len(result.Content) != 3 || result.Content[1].Type != "image" {
			t.Fatalf("isolated call: %#v %v", result, err)
		}
	}
	result, err := r.Call(ctx, "a", "docs", d, dir, "echo", json.RawMessage(`{"fail":true}`))
	if err != nil || !result.IsError || result.Content[0].Text != "2" {
		t.Fatalf("retained state/error: %#v %v", result, err)
	}
	r.Close()
	if _, _, err := r.Discover(ctx, "a", "docs", d, dir); err == nil {
		t.Fatal("closed runtime accepted connection")
	}
}
func TestHTTPAuthenticationAndCall(t *testing.T) {
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return testServer() }, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	r := New()
	defer r.Close()
	d := domain.Definition{Transport: "http", URL: server.URL, Headers: map[string]string{"Authorization": "Bearer private-token"}}
	tools, _, err := r.Discover(context.Background(), "a", "docs", d, "")
	if err != nil || len(tools) != 1 {
		t.Fatalf("HTTP discovery: %#v %v", tools, err)
	}
	result, err := r.Call(context.Background(), "a", "docs", d, "", "echo", json.RawMessage(`{}`))
	if err != nil || result.Content[0].Text != "1" {
		t.Fatalf("HTTP call: %#v %v", result, err)
	}
}
