package entstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/setting"
)

func TestQuickCommandRepositoryPersistsDuplicatesAndDeletesByID(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	repo := store.Settings()
	createdAt := time.Date(2026, 7, 11, 3, 0, 0, 0, time.UTC)
	for _, command := range []setting.QuickCommand{
		{ID: "command-1", Content: "检查测试", CreatedAt: createdAt},
		{ID: "command-2", Content: "检查测试", CreatedAt: createdAt.Add(time.Second)},
	} {
		if err := repo.Create(ctx, command); err != nil {
			t.Fatalf("create command: %v", err)
		}
	}

	page, err := repo.List(ctx, setting.QuickCommandQuery{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("list commands: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "command-2" || page.Total != 2 || page.Page != 1 || page.PageSize != 1 {
		t.Fatalf("page = %#v", page)
	}
	page, err = repo.List(ctx, setting.QuickCommandQuery{Page: 2, PageSize: 1})
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "command-1" || page.Total != 2 {
		t.Fatalf("second page = %#v", page)
	}
	page, err = repo.List(ctx, setting.QuickCommandQuery{Page: 99, PageSize: 1})
	if err != nil {
		t.Fatalf("list out-of-range page: %v", err)
	}
	if page.Page != 2 || len(page.Items) != 1 || page.Items[0].ID != "command-1" {
		t.Fatalf("clamped page = %#v", page)
	}
	if err := repo.Delete(ctx, "command-1"); err != nil {
		t.Fatalf("delete command: %v", err)
	}
	page, err = repo.List(ctx, setting.QuickCommandQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "command-2" || page.Total != 1 {
		t.Fatalf("page after delete = %#v", page)
	}
	if err := repo.Delete(ctx, "missing"); !errors.Is(err, setting.ErrQuickCommandNotFound) {
		t.Fatalf("delete missing error = %v", err)
	}
}

func TestQuickCommandRepositoryNormalizesPagination(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	repo := store.Settings()
	page, err := repo.List(ctx, setting.QuickCommandQuery{})
	if err != nil {
		t.Fatalf("list empty commands: %v", err)
	}
	if page.Page != 1 || page.PageSize != 20 || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("empty page = %#v", page)
	}
	if err := repo.Create(ctx, setting.QuickCommand{ID: "command-1", Content: "检查测试"}); err != nil {
		t.Fatalf("create command: %v", err)
	}
	page, err = repo.List(ctx, setting.QuickCommandQuery{Page: -1, PageSize: -1})
	if err != nil {
		t.Fatalf("list commands with negative pagination: %v", err)
	}
	if page.Page != 1 || page.PageSize != 20 || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("normalized page = %#v", page)
	}
}

func TestQuickCommandMigrationPreservesExistingTables(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, "CREATE TABLE legacy_marker (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "INSERT INTO legacy_marker (id) VALUES ('kept')"); err != nil {
		t.Fatalf("insert legacy marker: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	var marker string
	if err := store.db.QueryRowContext(ctx, "SELECT id FROM legacy_marker").Scan(&marker); err != nil {
		t.Fatalf("read legacy marker: %v", err)
	}
	if marker != "kept" {
		t.Fatalf("legacy marker = %q", marker)
	}
	if err := store.Settings().Create(ctx, setting.QuickCommand{ID: "command-1", Content: "检查测试"}); err != nil {
		t.Fatalf("create command after migration: %v", err)
	}
}
