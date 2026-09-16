package entstore

import (
	"context"
	"path/filepath"
	"testing"

	domain "github.com/nzlov/anycode/internal/domain/mcp"
)

func TestMCPConfigurationRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	options := OpenOptions{DatabaseURL: filepath.Join(dir, "mcp.db"), DataDir: dir}
	store, err := Open(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Kind: "global"}
	on, off := true, false
	entry := domain.Entry{Scope: scope, Name: "docs", Definition: &domain.Definition{Transport: "stdio", Command: "server", Env: map[string]string{"TOKEN": "secret"}}, Enabled: &on}
	if err = store.MCP().Save(ctx, entry); err != nil {
		t.Fatal(err)
	}
	entry.Enabled = &off
	if err = store.MCP().Save(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.MCP().List(ctx, scope)
	if err != nil || len(rows) != 1 || *rows[0].Enabled || rows[0].Definition.Env["TOKEN"] != "secret" {
		t.Fatalf("reload: %#v %v", rows, err)
	}
	card := domain.Scope{Kind: "session", ID: "c"}
	if err = store.MCP().Save(ctx, domain.Entry{Scope: card, Name: "docs", Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	rows, err = store.MCP().List(ctx, card)
	if err != nil || rows[0].Definition != nil || !*rows[0].Enabled {
		t.Fatalf("switch-only: %#v %v", rows, err)
	}
	if err = store.MCP().Delete(ctx, card, "docs"); err != nil {
		t.Fatal(err)
	}
	rows, err = store.MCP().List(ctx, scope)
	if err != nil || len(rows) != 1 {
		t.Fatal("deleting card override changed global")
	}
}
