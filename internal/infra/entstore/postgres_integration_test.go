package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"github.com/jackc/pgx/v5"
	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/project"
)

func TestPostgresStoreIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("ANYCODE_TEST_POSTGRES_URL"))
	if databaseURL == "" {
		t.Skip("ANYCODE_TEST_POSTGRES_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse PostgreSQL test URL: %v", err)
	}
	admin, err := sql.Open(postgresDriverName, parsed.String())
	if err != nil {
		t.Fatalf("open PostgreSQL test database: %v", err)
	}
	schemaName := fmt.Sprintf("anycode_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := admin.ExecContext(context.Background(), `CREATE SCHEMA `+quotedSchema); err != nil {
		admin.Close()
		t.Fatalf("create PostgreSQL test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+quotedSchema+` CASCADE`)
		_ = admin.Close()
	})
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()

	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: parsed.String(), DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("open PostgreSQL store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if store.dialectName != dialect.Postgres {
		t.Fatalf("dialect = %q", store.dialectName)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate PostgreSQL store: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("repeat PostgreSQL migration: %v", err)
	}

	want := project.Project{
		ID:   "postgres-project",
		Name: "PostgreSQL",
		Path: project.ProjectPath{Value: "/workspaces/postgres"},
	}
	if err := store.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		return tx.Projects().Save(ctx, want)
	}); err != nil {
		t.Fatalf("save project in PostgreSQL transaction: %v", err)
	}
	got, err := store.Projects().Find(ctx, want.ID)
	if err != nil {
		t.Fatalf("find PostgreSQL project: %v", err)
	}
	if got.Name != want.Name || got.Path != want.Path {
		t.Fatalf("project = %#v", got)
	}
}
