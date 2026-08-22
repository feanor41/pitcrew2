package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestOpenReadOnlyMissingStateDoesNotCreateFilesystem(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "missing-project")

	result, err := OpenReadOnly(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != Uninitialized || result.Store != nil {
		t.Fatalf("result = %#v; want uninitialized without a store", result)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project root was created or cannot be inspected: %v", err)
	}

	root = t.TempDir()
	result, err = OpenReadOnly(ctx, root)
	if err != nil || result.State != Uninitialized || result.Store != nil {
		t.Fatalf("existing empty root result = %#v, %v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".pitcrew")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".pitcrew was created or cannot be inspected: %v", err)
	}
}

func TestOpenReadOnlyRejectsNonRegularState(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, path string)
	}{
		{name: "directory", setup: func(t *testing.T, path string) {
			t.Helper()
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", setup: func(t *testing.T, path string) {
			t.Helper()
			target := filepath.Join(t.TempDir(), "target.db")
			if err := os.WriteFile(target, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".pitcrew")
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "state.db")
			test.setup(t, path)

			result, err := OpenReadOnly(context.Background(), root)
			if result.Store != nil || !errors.Is(err, ErrInvalidState) {
				t.Fatalf("result = %#v, error = %v; want invalid state", result, err)
			}
		})
	}
}

func TestOpenReadOnlyPreservesLogicalStateAndRejectsMutation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writable, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writable.DB().ExecContext(ctx, `INSERT INTO workflows(id, revision, state, goal, created_at, updated_at) VALUES('wf-read-only', 7, 'implementing', 'inspect safely', 'before', 'before')`); err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}

	databasePath := filepath.Join(root, ".pitcrew", "state.db")
	beforeLogical := logicalSnapshot(t, databasePath)
	beforeFiles := treeSnapshot(t, root)
	result, err := OpenReadOnly(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != Initialized || result.Store == nil {
		t.Fatalf("result = %#v; want initialized store", result)
	}
	readOnly := result.Store
	if readOnly.Path() != databasePath {
		t.Fatalf("path = %q; want %q", readOnly.Path(), databasePath)
	}
	if got := readOnly.DB().Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d; want 1", got)
	}
	for name, want := range map[string]string{"query_only": "1", "foreign_keys": "1", "busy_timeout": "5000"} {
		var got string
		if err := readOnly.DB().QueryRowContext(ctx, "PRAGMA "+name).Scan(&got); err != nil || got != want {
			t.Fatalf("PRAGMA %s = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, statement := range []string{
		`INSERT INTO workflows(id, revision, state, goal, created_at, updated_at) VALUES('forbidden', 1, 'draft', '', '', '')`,
		`UPDATE workflows SET revision=8 WHERE id='wf-read-only'`,
		`CREATE TABLE forbidden (id TEXT)`,
	} {
		if _, err := readOnly.DB().ExecContext(ctx, statement); err == nil {
			t.Fatalf("read-only store accepted %q", statement)
		}
	}
	if err := readOnly.ApplyMigrations(ctx, []Migration{{Version: 3, Name: "forbidden", SQL: `CREATE TABLE migrated (id TEXT)`}}); err == nil {
		t.Fatal("read-only store accepted a migration")
	}
	if err := readOnly.Close(); err != nil {
		t.Fatal(err)
	}

	afterLogical := logicalSnapshot(t, databasePath)
	if !reflect.DeepEqual(afterLogical, beforeLogical) {
		t.Fatalf("logical state changed\nbefore: %v\nafter:  %v", beforeLogical, afterLogical)
	}
	afterFiles := treeSnapshot(t, root)
	assertOnlySQLiteSidecarsAdded(t, beforeFiles, afterFiles)
}

func logicalSnapshot(t *testing.T, path string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT type, name, tbl_name, coalesce(sql, '') FROM sqlite_master ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot []string
	for rows.Next() {
		var kind, name, table, statement string
		if err := rows.Scan(&kind, &name, &table, &statement); err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, fmt.Sprintf("schema:%s:%s:%s:%s", kind, name, table, statement))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err = db.Query(`SELECT id, revision, state, goal, created_at, updated_at FROM workflows ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, state, goal, created, updated string
		var revision int64
		if err := rows.Scan(&id, &revision, &state, &goal, &created, &updated); err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, fmt.Sprintf("workflow:%s:%d:%s:%s:%s:%s", id, revision, state, goal, created, updated))
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err = db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, fmt.Sprintf("count:%s:%d", table, count))
	}
	return snapshot
}

func treeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	if err := filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func assertOnlySQLiteSidecarsAdded(t *testing.T, before, after []string) {
	t.Helper()
	known := make(map[string]bool, len(before))
	for _, path := range before {
		known[path] = true
	}
	for _, path := range after {
		if known[path] {
			continue
		}
		if !strings.HasSuffix(path, "state.db-wal") && !strings.HasSuffix(path, "state.db-shm") {
			t.Fatalf("unexpected path created by read-only open: %q", path)
		}
	}
}

func TestOpenCreatesLocalSchemaAndAppliesPragmas(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Path() != filepath.Join(root, ".pitcrew", "state.db") {
		t.Fatalf("path = %q", s.Path())
	}
	if s.db.Stats().MaxOpenConnections != 1 {
		t.Fatalf("max open connections = %d", s.db.Stats().MaxOpenConnections)
	}
	pragmas := map[string]string{"journal_mode": "wal", "synchronous": "1", "foreign_keys": "1", "busy_timeout": "5000", "temp_store": "2"}
	for name, want := range pragmas {
		var got string
		if err := s.db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&got); err != nil || got != want {
			t.Fatalf("PRAGMA %s = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, table := range []string{"workflows", "events", "plans", "work_units", "evidence", "reviews", "handles"} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s count = %d, %v", table, count, err)
		}
	}
	for table, column := range map[string]string{"events": "reason", "work_units": "revision", "handles": "actor_identity"} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&count); err != nil || count != 1 {
			t.Fatalf("column %s.%s count = %d, %v", table, column, count, err)
		}
	}
}

func TestMigrationsAreOrderedAndRejectDestructiveSQL(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.ApplyMigrations(ctx, []Migration{{Version: 3, Name: "late", SQL: `CREATE TABLE late (id TEXT)`}, {Version: 2, Name: "early", SQL: `CREATE TABLE early (id TEXT)`}}); err == nil {
		t.Fatal("out-of-order migrations were accepted")
	}
	var lateTables int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='late'`).Scan(&lateTables); err != nil || lateTables != 0 {
		t.Fatalf("out-of-order batch changed schema: count=%d, err=%v", lateTables, err)
	}
	for _, sql := range []string{`DROP TABLE workflows`, `UPDATE workflows SET goal = ''`, `DELETE FROM workflows`, `ALTER TABLE workflows ADD COLUMN unsafe TEXT`, `ALTER TABLE workflows ADD COLUMN name TEXT NOT NULL`} {
		if err := s.ApplyMigrations(ctx, []Migration{{Version: 2, Name: "destructive", SQL: sql}}); err == nil {
			t.Fatalf("destructive migration %q was accepted", sql)
		}
	}
	if err := s.ApplyMigrations(ctx, []Migration{{Version: 2, Name: "safe", SQL: `CREATE TABLE safe (id TEXT)`}, {Version: 3, Name: "safer", SQL: `CREATE TABLE safer (id TEXT)`}}); err != nil {
		t.Fatalf("safe ordered migrations: %v", err)
	}
	var applied int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil || applied != 3 {
		t.Fatalf("applied migrations = %d, %v", applied, err)
	}
}

func TestMigration2PreservesV1RowsAndAddsNameAndActivities(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, ".pitcrew")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	legacy := &Store{db: db}
	if err := legacy.ApplyMigrations(ctx, schemaMigrations[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-legacy',3,'designing','legacy goal','created','updated')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	var id, goal string
	var revision int
	var name sql.NullString
	if err := migrated.db.QueryRowContext(ctx, `SELECT id,revision,goal,name FROM workflows WHERE id='wf-legacy'`).Scan(&id, &revision, &goal, &name); err != nil {
		t.Fatal(err)
	}
	if id != "wf-legacy" || revision != 3 || goal != "legacy goal" || name.Valid {
		t.Fatalf("legacy row changed: id=%q revision=%d goal=%q name=%#v", id, revision, goal, name)
	}
	for _, table := range []string{"activities"} {
		var count int
		if err := migrated.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s count = %d, %v", table, count, err)
		}
	}
	var activities int
	if err := migrated.db.QueryRowContext(ctx, `SELECT count(*) FROM activities`).Scan(&activities); err != nil || activities != 0 {
		t.Fatalf("historical activities = %d, %v; want none fabricated", activities, err)
	}
	var migrations int
	if err := migrated.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil || migrations != 2 {
		t.Fatalf("migration count = %d, %v; want 2", migrations, err)
	}
}

func TestCompareAndSwapRevisionIsAtomic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, `INSERT INTO workflows(id, revision, state, goal, created_at, updated_at) VALUES('wf-test', 7, 'draft', 'goal', 'now', 'now')`)
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.CompareAndSwapRevision(ctx, "wf-test", 7)
	if err != nil || next != 8 {
		t.Fatalf("CAS success = %d, %v", next, err)
	}
	if _, err := s.CompareAndSwapRevision(ctx, "wf-test", 7); !errors.Is(err, ErrCASMismatch) {
		t.Fatalf("CAS mismatch error = %v", err)
	}
	var revision int64
	if err := s.db.QueryRowContext(ctx, `SELECT revision FROM workflows WHERE id='wf-test'`).Scan(&revision); err != nil || revision != 8 {
		t.Fatalf("revision = %d, %v", revision, err)
	}
}

func TestBusyTimeoutRejectsConcurrentWriter(t *testing.T) {
	root := t.TempDir()
	first, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	tx, err := first.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO workflows(id, revision, state, goal, created_at, updated_at) VALUES('wf-lock', 1, 'draft', 'goal', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	started := time.Now()
	_, err = second.db.ExecContext(ctx, `INSERT INTO workflows(id, revision, state, goal, created_at, updated_at) VALUES('wf-blocked', 1, 'draft', 'goal', 'now', 'now')`)
	if err == nil || time.Since(started) < 4*time.Second {
		t.Fatalf("concurrent write error = %v after %s", err, time.Since(started))
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
