package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

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
	for _, sql := range []string{`DROP TABLE workflows`, `UPDATE workflows SET goal = ''`, `DELETE FROM workflows`} {
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
