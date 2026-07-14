package entstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/domain/question"
	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/domain/setting"
	"github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	"github.com/tursodatabase/libsql-client-go/libsql"
	_ "turso.tech/database/tursogo"
)

const (
	tursoDriverName  = "turso"
	libsqlDriverName = "libsql"
)

type OpenOptions struct {
	DatabaseURL string
	AuthToken   string
	DataDir     string
}

type Store struct {
	client *ent.Client
	db     *sql.DB
}

type databaseTarget struct {
	DriverName  string
	DatabaseURL string
	AuthToken   string
}

var _ port.UnitOfWork = (*Store)(nil)

func OpenFromEnv(ctx context.Context) (*Store, error) {
	return Open(ctx, OpenOptions{
		DatabaseURL: os.Getenv("TURSO_DATABASE_URL"),
		AuthToken:   os.Getenv("TURSO_AUTH_TOKEN"),
		DataDir:     os.Getenv("ANYCODE_DATA_DIR"),
	})
}

func Open(ctx context.Context, opts OpenOptions) (*Store, error) {
	target, err := databaseTargetForOptions(opts)
	if err != nil {
		return nil, err
	}
	db, err := openDatabase(target)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
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
	if err := s.client.Schema.Create(ctx); err != nil {
		return err
	}
	// GLUE: Backfill queues written before queue_initial_start; remove after legacy databases no longer need upgrading.
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions
		SET queue_initial_start = CASE
			WHEN status = ? AND queue_kind = ? AND NOT EXISTS (
				SELECT 1 FROM process_runs WHERE process_runs.session_id = sessions.id
			) THEN 1
			ELSE 0
		END
		WHERE queue_initial_start IS NULL`, string(session.StatusQueued), string(session.QueueKindStart)); err != nil {
		return fmt.Errorf("backfill session queue initial start: %w", err)
	}
	// GLUE: Collapse duplicate legacy active runs before the database starts enforcing the lifecycle invariant.
	if _, err := s.db.ExecContext(ctx, `UPDATE process_runs AS older
		SET status = ?,
			failure_reason = CASE WHEN failure_reason = '' THEN ? ELSE failure_reason END,
			finished_at = COALESCE(finished_at, CURRENT_TIMESTAMP)
		WHERE status IN (?, ?, ?, ?)
		AND EXISTS (
			SELECT 1 FROM process_runs AS newer
			WHERE newer.session_id = older.session_id
			AND newer.status IN (?, ?, ?, ?)
			AND (
				newer.started_at > older.started_at OR
				(newer.started_at = older.started_at AND newer.id > older.id)
			)
		)`,
		string(process.StatusExited),
		"superseded active process run during uniqueness migration",
		string(process.StatusStarting), string(process.StatusRunning), string(process.StatusWaitingUser), string(process.StatusStopping),
		string(process.StatusStarting), string(process.StatusRunning), string(process.StatusWaitingUser), string(process.StatusStopping),
	); err != nil {
		return fmt.Errorf("collapse duplicate active process runs: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS process_runs_one_active_per_session
		ON process_runs(session_id)
		WHERE status IN ('starting', 'running', 'waiting_user', 'stopping')`); err != nil {
		return fmt.Errorf("create active process run uniqueness index: %w", err)
	}
	// GLUE: Legacy pending answer_user rows lacked an origin; remove after all supported databases have this column populated.
	if _, err := s.db.ExecContext(ctx, `UPDATE question_batches
		SET origin_process_run_id = (
			SELECT process_runs.id
			FROM process_runs
			WHERE process_runs.session_id = question_batches.session_id
			AND process_runs.status IN ('starting', 'running', 'waiting_user', 'stopping')
			LIMIT 1
		)
		WHERE status = ?
		AND origin_process_run_id = ''
		AND 1 = (
			SELECT COUNT(*)
			FROM process_runs
			WHERE process_runs.session_id = question_batches.session_id
			AND process_runs.status IN ('starting', 'running', 'waiting_user', 'stopping')
		)`, string(question.BatchPending)); err != nil {
		return fmt.Errorf("backfill question origin process run: %w", err)
	}
	return nil
}

func (s *Store) Projects() *ProjectRepository {
	return NewProjectRepository(s.client)
}

func (s *Store) Sessions() *SessionRepository {
	return NewSessionRepository(s.client)
}

func (s *Store) Attachments() *AttachmentRepository {
	return NewAttachmentRepository(s.client)
}

func (s *Store) Events() *EventStore {
	return NewEventStore(s.client)
}

func (s *Store) Processes() *ProcessRepository {
	return NewProcessRepository(s.client)
}

func (s *Store) Questions() *QuestionRepository {
	return NewQuestionRepository(s.client)
}

func (s *Store) Settings() setting.Repository {
	return NewQuickCommandRepository(s.client)
}

func (s *Store) Workflows() *WorkflowRepository {
	return NewWorkflowRepository(s.client)
}

func (s *Store) Do(ctx context.Context, fn func(ctx context.Context, tx port.Tx) error) error {
	if s == nil || s.client == nil {
		return errors.New("entstore: nil store")
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	if err := fn(ctx, transaction{client: tx.Client()}); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w; rollback transaction: %v", err, rollbackErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

type transaction struct {
	client *ent.Client
}

func (t transaction) Projects() project.Repository {
	return NewProjectRepository(t.client)
}

func (t transaction) Sessions() session.Repository {
	return NewSessionRepository(t.client)
}

func (t transaction) Workflows() workflow.Repository {
	return newWorkflowRepositoryInTx(t.client)
}

func (t transaction) Questions() question.Repository {
	return newQuestionRepositoryInTx(t.client)
}

func (t transaction) Processes() process.Repository {
	return NewProcessRepository(t.client)
}

func (t transaction) Events() event.Store {
	return NewEventStore(t.client)
}

func databaseTargetForOptions(opts OpenOptions) (databaseTarget, error) {
	databaseURL := strings.TrimSpace(opts.DatabaseURL)
	if databaseURL == "" {
		dataDir := opts.DataDir
		if dataDir == "" {
			dataDir = "./data"
		}
		return databaseTarget{
			DriverName:  tursoDriverName,
			DatabaseURL: filepath.Join(dataDir, "anycode.turso.db"),
		}, nil
	}
	if databaseURL == ":memory:" || strings.HasPrefix(databaseURL, ":memory:?") {
		return databaseTarget{DriverName: tursoDriverName, DatabaseURL: databaseURL}, nil
	}

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return databaseTarget{}, fmt.Errorf("parse database URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "":
		return databaseTarget{DriverName: tursoDriverName, DatabaseURL: databaseURL}, nil
	case "file":
		return databaseTarget{}, errors.New("entstore: file: URLs are not supported for local Turso; use a plain filesystem path")
	case "libsql", "https":
		if parsed.Host == "" {
			return databaseTarget{}, errors.New("entstore: remote Turso database URL host is required")
		}
		if strings.TrimSpace(opts.AuthToken) == "" {
			return databaseTarget{}, errors.New("entstore: TURSO_AUTH_TOKEN is required for remote libSQL/Turso database")
		}
		parsed.Scheme = scheme
		return databaseTarget{
			DriverName:  libsqlDriverName,
			DatabaseURL: parsed.String(),
			AuthToken:   opts.AuthToken,
		}, nil
	case "http":
		return databaseTarget{}, errors.New("entstore: insecure http database URL is not supported; use libsql:// or https://")
	default:
		return databaseTarget{}, fmt.Errorf("entstore: unsupported database URL scheme %q", parsed.Scheme)
	}
}

func openDatabase(target databaseTarget) (*sql.DB, error) {
	switch target.DriverName {
	case libsqlDriverName:
		connector, err := libsql.NewConnector(target.DatabaseURL, libsql.WithAuthToken(target.AuthToken))
		if err != nil {
			return nil, err
		}
		return sql.OpenDB(connector), nil
	case tursoDriverName:
		if err := ensureLocalDatabaseDir(target.DatabaseURL); err != nil {
			return nil, err
		}
		return sql.Open(tursoDriverName, target.DatabaseURL)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", target.DriverName)
	}
}

func ensureLocalDatabaseDir(dsn string) error {
	databasePath, _, _ := strings.Cut(dsn, "?")
	if databasePath == ":memory:" {
		return nil
	}
	dir := filepath.Dir(databasePath)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create local Turso database directory: %w", err)
	}
	return nil
}
