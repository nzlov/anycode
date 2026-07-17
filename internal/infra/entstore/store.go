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
	"time"

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
	drv := newImmediateTransactionDriver(db)
	return &Store{
		client: ent.NewClient(ent.Driver(drv)),
		db:     db,
	}, nil
}

type immediateTransactionDriver struct {
	dialect.Driver
	db *sql.DB
}

func newImmediateTransactionDriver(db *sql.DB) dialect.Driver {
	return &immediateTransactionDriver{
		Driver: entsql.OpenDB(dialect.SQLite, db),
		db:     db,
	}
}

func (d *immediateTransactionDriver) Tx(ctx context.Context) (dialect.Tx, error) {
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, errors.Join(err, conn.Close())
	}
	drv := entsql.NewDriver(dialect.SQLite, entsql.Conn{ExecQuerier: conn})
	return &immediateTransaction{ExecQuerier: drv, conn: conn}, nil
}

type immediateTransaction struct {
	dialect.ExecQuerier
	conn *sql.Conn
}

func (t *immediateTransaction) Commit() error {
	return t.finish("COMMIT")
}

func (t *immediateTransaction) Rollback() error {
	return t.finish("ROLLBACK")
}

func (t *immediateTransaction) finish(statement string) error {
	_, err := t.conn.ExecContext(context.Background(), statement)
	return errors.Join(err, t.conn.Close())
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
	if err := s.dropRemovedStorage(ctx); err != nil {
		return err
	}
	if err := s.client.Schema.Create(ctx); err != nil {
		return err
	}
	if err := s.migrateWorkflowApprovalOutputFields(ctx); err != nil {
		return err
	}
	if err := s.migrateSessionFileColumns(ctx); err != nil {
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

// GLUE: Remove the superseded schema shape. This is intentionally destructive because legacy data is not supported.
func (s *Store) dropRemovedStorage(ctx context.Context) error {
	legacyNodeRuns, err := s.columnExists(ctx, "node_runs", "workflow_run_id")
	if err != nil {
		return err
	}
	statements := make([]string, 0, 3)
	if legacyNodeRuns {
		statements = append(statements, `DROP TABLE node_runs`)
	}
	statements = append(statements,
		`DROP TABLE IF EXISTS workflow_runs`,
		`DROP TABLE IF EXISTS codex_transcript_sources`,
	)
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("remove superseded storage with %q: %w", statement, err)
		}
	}
	for _, item := range []struct {
		table  string
		column string
	}{
		{table: "sessions", column: "queue_workflow_run_id"},
		{table: "question_batches", column: "workflow_run_id"},
		{table: "event_records", column: "workflow_run_id"},
	} {
		hasColumn, err := s.columnExists(ctx, item.table, item.column)
		if err != nil {
			return err
		}
		if hasColumn {
			statement := fmt.Sprintf(`ALTER TABLE %s DROP COLUMN %s`, item.table, item.column)
			if _, err := s.db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("remove superseded column %s.%s: %w", item.table, item.column, err)
			}
		}
	}
	return nil
}

func (s *Store) columnExists(ctx context.Context, table string, column string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
		return false, fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	return count != 0, nil
}

func (s *Store) migrateWorkflowApprovalOutputFields(ctx context.Context) error {
	rows, err := s.client.WorkflowDefinition.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("list workflow definitions for approval output migration: %w", err)
	}
	type update struct {
		id        string
		graph     map[string]any
		updatedAt time.Time
	}
	updates := make([]update, 0)
	for _, row := range rows {
		graph, changed, err := workflowGraphWithoutApprovalOutputFields(row.Graph)
		if err != nil {
			return fmt.Errorf("inspect workflow definition %q for approval output migration: %w", row.ID, err)
		}
		if changed {
			updates = append(updates, update{id: row.ID, graph: graph, updatedAt: row.UpdatedAt})
		}
	}
	if len(updates) == 0 {
		return nil
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin workflow approval output migration: %w", err)
	}
	defer tx.Rollback()
	for _, item := range updates {
		if err := tx.WorkflowDefinition.UpdateOneID(item.id).
			SetGraph(item.graph).
			SetUpdatedAt(item.updatedAt).
			Exec(ctx); err != nil {
			return fmt.Errorf("migrate workflow definition %q approval output fields: %w", item.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workflow approval output migration: %w", err)
	}
	return nil
}

// GLUE: This map rewrite preserves unknown graph JSON while applying the domain's reserved approval namespace; remove after all supported databases cross this migration.
func workflowGraphWithoutApprovalOutputFields(graph map[string]any) (map[string]any, bool, error) {
	nodesKey, nodesValue, ok := workflowGraphField(graph, "Nodes", "nodes")
	if !ok || nodesValue == nil {
		return graph, false, nil
	}
	nodes, ok := nodesValue.([]any)
	if !ok {
		return nil, false, errors.New("workflow graph nodes must be an array")
	}

	resultNodes := append([]any(nil), nodes...)
	changed := false
	for index, rawNode := range nodes {
		node, ok := rawNode.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("workflow graph node %d must be an object", index)
		}
		fieldsKey, fieldsValue, ok := workflowGraphField(node, "OutputFields", "outputFields")
		if !ok || fieldsValue == nil {
			continue
		}
		fields, ok := fieldsValue.([]any)
		if !ok {
			return nil, false, fmt.Errorf("workflow graph node %d output fields must be an array", index)
		}
		kept := make([]any, 0, len(fields))
		for fieldIndex, rawField := range fields {
			field, ok := rawField.(map[string]any)
			if !ok {
				return nil, false, fmt.Errorf("workflow graph node %d output field %d must be an object", index, fieldIndex)
			}
			_, keyValue, hasKey := workflowGraphField(field, "Key", "key")
			if hasKey {
				key, ok := keyValue.(string)
				if !ok {
					return nil, false, fmt.Errorf("workflow graph node %d output field %d key must be a string", index, fieldIndex)
				}
				if workflow.IsApprovalOutputField(key) {
					changed = true
					continue
				}
			}
			kept = append(kept, rawField)
		}
		if len(kept) == len(fields) {
			continue
		}
		resultNode := make(map[string]any, len(node))
		for key, value := range node {
			resultNode[key] = value
		}
		resultNode[fieldsKey] = kept
		resultNodes[index] = resultNode
	}
	if !changed {
		return graph, false, nil
	}
	result := make(map[string]any, len(graph))
	for key, value := range graph {
		result[key] = value
	}
	result[nodesKey] = resultNodes
	return result, true, nil
}

func workflowGraphField(value map[string]any, keys ...string) (string, any, bool) {
	for _, key := range keys {
		if field, ok := value[key]; ok {
			return key, field, true
		}
	}
	return "", nil, false
}

func (s *Store) Projects() *ProjectRepository {
	return NewProjectRepository(s.client)
}

func (s *Store) Sessions() *SessionRepository {
	return NewSessionRepository(s.client)
}

func (s *Store) Attachments() *AttachmentRepository {
	return NewAttachmentRepository(s.client, s.db)
}

func (s *Store) migrateSessionFileColumns(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS session_attachments_session_source_key
		ON session_attachments(session_id, source_key)
		WHERE role = 'artifact' AND source_key <> ''`); err != nil {
		return fmt.Errorf("create session artifact source key index: %w", err)
	}
	return nil
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
