package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var (
	ErrCASMismatch  = errors.New("workflow revision mismatch")
	ErrInvalidState = errors.New("PitCrew state database is not a regular file")
)

type RepositoryState string

const (
	Uninitialized RepositoryState = "uninitialized"
	Initialized   RepositoryState = "initialized"
)

type OpenReadOnlyResult struct {
	State RepositoryState
	Store *Store
}

type InvalidStateError struct {
	Path string
	Mode os.FileMode
}

func (e *InvalidStateError) Error() string {
	return fmt.Sprintf("invalid PitCrew state database %q: mode %s", e.Path, e.Mode)
}

func (e *InvalidStateError) Unwrap() error { return ErrInvalidState }

type Store struct {
	db   *sql.DB
	path string
}

type Migration struct {
	Version int
	Name    string
	SQL     string
}

func Open(ctx context.Context, projectRoot string) (*Store, error) {
	dir := filepath.Join(projectRoot, ".pitcrew")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return openWritablePath(ctx, filepath.Join(dir, "state.db"))
}
func openWritablePath(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: path}
	for _, pragma := range []string{"journal_mode=WAL", "synchronous=NORMAL", "foreign_keys=ON", "busy_timeout=5000", "temp_store=MEMORY"} {
		if _, err := db.ExecContext(ctx, "PRAGMA "+pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("apply PRAGMA %s: %w", pragma, err)
		}
	}
	if err := s.ApplyMigrations(ctx, schemaMigrations); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func OpenReadOnly(ctx context.Context, projectRoot string) (OpenReadOnlyResult, error) {
	path := filepath.Join(projectRoot, ".pitcrew", "state.db")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return OpenReadOnlyResult{State: Uninitialized}, nil
	}
	if err != nil {
		return OpenReadOnlyResult{}, fmt.Errorf("inspect PitCrew state database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return OpenReadOnlyResult{}, &InvalidStateError{Path: path, Mode: info.Mode()}
	}

	return openReadOnlyPath(ctx, path)
}
func openReadOnlyPath(ctx context.Context, path string) (OpenReadOnlyResult, error) {
	uri := url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Set("mode", "ro")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return OpenReadOnlyResult{}, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	closeOnError := func(err error) (OpenReadOnlyResult, error) {
		_ = db.Close()
		return OpenReadOnlyResult{}, err
	}
	if err := db.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("open PitCrew state database read-only: %w", err))
	}
	for _, pragma := range []string{"query_only=ON", "foreign_keys=ON", "busy_timeout=5000"} {
		if _, err := db.ExecContext(ctx, "PRAGMA "+pragma); err != nil {
			return closeOnError(fmt.Errorf("apply read-only PRAGMA %s: %w", pragma, err))
		}
	}
	s := &Store{db: db, path: path}
	return OpenReadOnlyResult{State: Initialized, Store: s}, nil
}

func (s *Store) Path() string { return s.path }
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) ApplyMigrations(ctx context.Context, migrations []Migration) error {
	last := 0
	for _, migration := range migrations {
		if migration.Version <= last {
			return fmt.Errorf("migration %d %q is out of order", migration.Version, migration.Name)
		}
		last = migration.Version
		if destructive(migration.SQL) {
			return fmt.Errorf("migration %d %q is destructive", migration.Version, migration.Name)
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		return err
	}
	for _, migration := range migrations {
		var applied int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=?`, migration.Version).Scan(&applied); err != nil {
			return err
		}
		if applied != 0 {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, migration.SQL); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name) VALUES(?, ?)`, migration.Version, migration.Name)
		}
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d %q: %w", migration.Version, migration.Name, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CompareAndSwapRevision(ctx context.Context, workflowID string, expected int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE workflows SET revision=revision+1 WHERE id=? AND revision=?`, workflowID, expected)
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if changed != 1 {
		return 0, ErrCASMismatch
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return expected + 1, nil
}

func destructive(statement string) bool {
	for _, raw := range strings.Split(statement, ";") {
		normalized := strings.ToUpper(strings.Join(strings.Fields(raw), " "))
		for _, forbidden := range []string{"DROP ", "DELETE ", "UPDATE ", "REPLACE ", "TRUNCATE ", "ALTER TABLE", "VACUUM"} {
			if strings.Contains(normalized, forbidden) && normalized != "ALTER TABLE WORKFLOWS ADD COLUMN NAME TEXT" && normalized != "ALTER TABLE HANDLES ADD COLUMN PURPOSE TEXT NOT NULL DEFAULT 'IMPLEMENTATION'" {
				return true
			}
		}
	}
	return false
}

var schemaMigrations = []Migration{{Version: 1, Name: "initial schema", SQL: `
CREATE TABLE workflows (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, state TEXT NOT NULL, goal TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE events (workflow_id TEXT NOT NULL REFERENCES workflows(id), from_state TEXT NOT NULL, to_state TEXT NOT NULL, actor TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', revision_after INTEGER NOT NULL, at TEXT NOT NULL, PRIMARY KEY(workflow_id, revision_after));
CREATE TABLE artifacts (id INTEGER PRIMARY KEY AUTOINCREMENT, workflow_id TEXT NOT NULL REFERENCES workflows(id), kind TEXT NOT NULL, content TEXT NOT NULL, actor TEXT NOT NULL, accepted_revision INTEGER NOT NULL, recorded_at TEXT NOT NULL);
CREATE TABLE plans (workflow_id TEXT PRIMARY KEY REFERENCES workflows(id), summary TEXT NOT NULL, scope TEXT NOT NULL, max_parallel_units INTEGER NOT NULL, body TEXT NOT NULL);
CREATE TABLE work_units (id TEXT PRIMARY KEY, workflow_id TEXT NOT NULL REFERENCES workflows(id), description TEXT NOT NULL, scope TEXT NOT NULL, areas TEXT NOT NULL, depends_on TEXT NOT NULL, estimated_changed_lines INTEGER NOT NULL, estimated_review_minutes INTEGER NOT NULL, state TEXT NOT NULL, admission_exception TEXT, admission_exception_approved INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL DEFAULT 1);
CREATE TABLE evidence (workflow_id TEXT NOT NULL REFERENCES workflows(id), unit_id TEXT NOT NULL REFERENCES work_units(id), revision INTEGER NOT NULL, actor TEXT NOT NULL, red_command TEXT NOT NULL, red_outcome TEXT NOT NULL, green_command TEXT NOT NULL, green_outcome TEXT NOT NULL, refactor_summary TEXT NOT NULL, validation_command TEXT NOT NULL, validation_outcome TEXT NOT NULL, changed_paths TEXT NOT NULL, recorded_at TEXT NOT NULL, PRIMARY KEY(workflow_id, unit_id, revision));
CREATE TABLE reviews (workflow_id TEXT NOT NULL REFERENCES workflows(id), unit_id TEXT NOT NULL REFERENCES work_units(id), revision INTEGER NOT NULL, actor TEXT NOT NULL, verdict TEXT NOT NULL, summary TEXT NOT NULL, findings TEXT NOT NULL, plan_impact TEXT NOT NULL, recorded_at TEXT NOT NULL, PRIMARY KEY(workflow_id, unit_id, revision));
CREATE TABLE handles (claim_id TEXT PRIMARY KEY, workflow_id TEXT NOT NULL REFERENCES workflows(id), unit_id TEXT NOT NULL REFERENCES work_units(id), state TEXT NOT NULL, secret_hash TEXT NOT NULL, actor_identity TEXT NOT NULL, issued_at TEXT NOT NULL, expires_at TEXT NOT NULL, claim_generation INTEGER NOT NULL);
`}, {Version: 2, Name: "workflow names and activities", SQL: `
ALTER TABLE workflows ADD COLUMN name TEXT;
CREATE TABLE activities (id INTEGER PRIMARY KEY AUTOINCREMENT, workflow_id TEXT NOT NULL REFERENCES workflows(id), unit_id TEXT REFERENCES work_units(id), action TEXT NOT NULL, actor TEXT NOT NULL, at TEXT NOT NULL, subject_kind TEXT NOT NULL, subject_id TEXT NOT NULL);
CREATE INDEX activities_workflow_time ON activities(workflow_id, at, id);
CREATE INDEX activities_subject ON activities(subject_kind, subject_id);
`}, {Version: 3, Name: "purpose scoped handles", SQL: `
ALTER TABLE handles ADD COLUMN purpose TEXT NOT NULL DEFAULT 'implementation';
`}, {Version: 4, Name: "direct delivery traces", SQL: `
CREATE TABLE direct_delivery_traces (
    id TEXT PRIMARY KEY CHECK(length(id) = 27 AND id GLOB 'dl-*' AND substr(id, 4) NOT GLOB '*[^0-9a-f]*'),
    operation_key TEXT NOT NULL UNIQUE CHECK(length(operation_key) BETWEEN 1 AND 128),
    route TEXT NOT NULL CHECK(route IN ('direct_inline', 'delegated_direct')),
    goal TEXT NOT NULL CHECK(length(goal) BETWEEN 1 AND 4000),
    route_reason TEXT NOT NULL CHECK(length(route_reason) <= 500),
    status TEXT NOT NULL CHECK(status IN ('in_progress', 'blocked', 'interrupted', 'completed', 'cancelled', 'failed')),
    summary TEXT NOT NULL CHECK(length(summary) <= 500),
    next_action TEXT NOT NULL CHECK(length(next_action) <= 200),
    revision INTEGER NOT NULL CHECK(revision > 0),
    creator_actor TEXT NOT NULL,
    updater_actor TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    finished_at TEXT
);
`}, {Version: 5, Name: "project context", SQL: `
CREATE TABLE project_context (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    schema_version INTEGER NOT NULL,
    content TEXT NOT NULL,
    actor TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE project_context_audits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    changed_categories TEXT NOT NULL
);
`}, {Version: 6, Name: "bounded coordination foundations", SQL: `
CREATE UNIQUE INDEX artifacts_workflow_id_id ON artifacts(workflow_id, id);
CREATE UNIQUE INDEX work_units_workflow_id_id ON work_units(workflow_id, id);
CREATE TABLE workflow_baselines (
    child_id TEXT PRIMARY KEY REFERENCES workflows(id),
    predecessor_id TEXT NOT NULL REFERENCES workflows(id),
    predecessor_revision INTEGER NOT NULL CHECK(predecessor_revision > 0),
    artifact_manifest_json TEXT NOT NULL
);
CREATE TABLE normative_entries (
    workflow_id TEXT NOT NULL REFERENCES workflows(id),
    artifact_id INTEGER NOT NULL,
    phase TEXT NOT NULL,
    entry_kind TEXT NOT NULL,
    stable_id TEXT NOT NULL,
    parent_id TEXT,
    operation TEXT NOT NULL,
    body_json TEXT NOT NULL,
    PRIMARY KEY(workflow_id, artifact_id, entry_kind, stable_id),
    FOREIGN KEY(workflow_id, artifact_id) REFERENCES artifacts(workflow_id, id)
);
CREATE TABLE unit_coverage (
    workflow_id TEXT NOT NULL REFERENCES workflows(id),
    unit_id TEXT NOT NULL,
    requirement_id TEXT NOT NULL,
    scenario_id TEXT NOT NULL,
    PRIMARY KEY(workflow_id, unit_id, requirement_id, scenario_id),
    FOREIGN KEY(workflow_id, unit_id) REFERENCES work_units(workflow_id, id)
);
CREATE TABLE verification_records (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflows(id),
    unit_id TEXT,
    unit_revision INTEGER,
    tier TEXT NOT NULL,
    command TEXT NOT NULL,
    outcome TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    scenario_ids_json TEXT NOT NULL,
    reused_from_id TEXT REFERENCES verification_records(id),
    actor TEXT NOT NULL,
    recorded_at TEXT NOT NULL,
    FOREIGN KEY(workflow_id, unit_id) REFERENCES work_units(workflow_id, id)
);
CREATE TABLE reviewed_checkpoints (
    workflow_id TEXT NOT NULL REFERENCES workflows(id),
    aggregate_revision INTEGER NOT NULL CHECK(aggregate_revision > 0),
    project_id TEXT NOT NULL,
    checkout_root TEXT NOT NULL,
    base_revision TEXT NOT NULL,
    head_revision TEXT NOT NULL,
    result_digest TEXT NOT NULL,
    dirty INTEGER NOT NULL CHECK(dirty IN (0, 1)),
    commit_ref TEXT,
    delivery_id TEXT REFERENCES direct_delivery_traces(id),
    recorded_at TEXT NOT NULL,
    PRIMARY KEY(workflow_id, aggregate_revision)
);
`}, {Version: 7, Name: "roadmap inbox", SQL: `
CREATE TABLE roadmap_items (
    id TEXT PRIMARY KEY CHECK(length(id) = 27 AND id GLOB 'rm-*' AND substr(id, 4) NOT GLOB '*[^0-9a-f]*'),
    title TEXT NOT NULL CHECK(length(trim(title)) > 0),
    body TEXT NOT NULL CHECK(length(trim(body)) > 0),
    provenance_json TEXT NOT NULL CHECK(json_valid(provenance_json) AND json_type(provenance_json) = 'object'),
    created_at TEXT NOT NULL,
    local_lifecycle TEXT NOT NULL CHECK(local_lifecycle IN ('captured', 'acknowledged'))
);
CREATE TABLE roadmap_bindings (
    roadmap_id TEXT PRIMARY KEY REFERENCES roadmap_items(id),
    provider TEXT NOT NULL,
    namespace TEXT NOT NULL,
    external_id TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    prepared_digest TEXT NOT NULL,
    acknowledged_at TEXT NOT NULL,
    UNIQUE(provider, namespace, external_id)
);
CREATE INDEX roadmap_items_created ON roadmap_items(created_at DESC, id);
`}, {Version: 8, Name: "direct delivery inspections", SQL: `
CREATE TABLE direct_delivery_inspections (
    delivery_id TEXT PRIMARY KEY REFERENCES direct_delivery_traces(id),
    revision INTEGER NOT NULL CHECK(revision > 0)
);
`}}
