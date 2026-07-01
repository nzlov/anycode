package entstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	_ "modernc.org/sqlite"
)

const sqliteDriverName = "sqlite"

type OpenOptions struct {
	DatabaseURL string
	AuthToken   string
	DataDir     string
}

type Store struct {
	client *ent.Client
	db     *sql.DB
}

func OpenFromEnv(ctx context.Context) (*Store, error) {
	return Open(ctx, OpenOptions{
		DatabaseURL: os.Getenv("TURSO_DATABASE_URL"),
		AuthToken:   os.Getenv("TURSO_AUTH_TOKEN"),
		DataDir:     os.Getenv("ANYCODE_DATA_DIR"),
	})
}

func Open(ctx context.Context, opts OpenOptions) (*Store, error) {
	dsn, err := sqliteDSN(opts)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	drv := entsql.OpenDB(dialect.SQLite, db)
	return &Store{
		client: ent.NewClient(ent.Driver(drv)),
		db:     db,
	}, nil
}

func (s *Store) Client() *ent.Client {
	return s.client
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	if s.client != nil {
		return s.client.Close()
	}
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) Migrate(ctx context.Context) error {
	if s == nil || s.client == nil {
		return errors.New("entstore: nil store")
	}
	return s.client.Schema.Create(ctx)
}

func (s *Store) Projects() *ProjectRepository {
	return NewProjectRepository(s.client)
}

func sqliteDSN(opts OpenOptions) (string, error) {
	url := strings.TrimSpace(opts.DatabaseURL)
	switch {
	case url == "":
		dataDir := opts.DataDir
		if dataDir == "" {
			dataDir = "./data"
		}
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return "", fmt.Errorf("create data dir: %w", err)
		}
		return withForeignKeys(filepath.Join(dataDir, "anycode.db")), nil
	case isRemoteLibSQLURL(url):
		if opts.AuthToken == "" {
			return "", errors.New("entstore: TURSO_AUTH_TOKEN is required for remote libSQL/Turso database")
		}
		return "", errors.New("entstore: remote libSQL/Turso driver is not linked in this build")
	case strings.HasPrefix(url, "file:"):
		return withForeignKeys(url), nil
	default:
		return withForeignKeys(url), nil
	}
}

func isRemoteLibSQLURL(url string) bool {
	return strings.HasPrefix(url, "libsql://") ||
		strings.HasPrefix(url, "https://") ||
		strings.HasPrefix(url, "http://")
}

func withForeignKeys(dsn string) string {
	if strings.Contains(dsn, "_fk=") {
		return dsn
	}
	if !strings.HasPrefix(dsn, "file:") && dsn != ":memory:" {
		dsn = "file:" + dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "_fk=1"
}
