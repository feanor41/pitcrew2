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
	if err := readOnly.ApplyMigrations(ctx, []Migration{{Version: len(schemaMigrations) + 1, Name: "forbidden", SQL: `CREATE TABLE migrated (id TEXT)`}}); err == nil {
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
	for _, table := range []string{"workflows", "events", "plans", "work_units", "evidence", "reviews", "handles", "direct_delivery_traces", "project_context", "project_context_audits", "workflow_baselines", "normative_entries", "unit_coverage", "verification_records", "reviewed_checkpoints"} {
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

func TestMigrationV6AddsBoundedCoordinationFoundationsWithoutRewritingV5(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	legacy := openStoreAtMigration(t, root, 5)
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-v5',11,'implementing','preserve workflow','created','updated')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-v5','design','preserve artifact','designer',7,'recorded')`,
		`INSERT INTO project_context(singleton,schema_version,content,actor,updated_at) VALUES(1,1,'{"schema_version":1,"facts":{"stack":[],"runtime":[],"deployment":[],"architecture":[],"documentation":[],"sdd":[]}}','aion','2026-08-30T17:00:00Z')`,
	} {
		if _, err := legacy.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	var workflow, artifact, projectContext string
	if err := migrated.DB().QueryRowContext(ctx, `SELECT id||':'||revision||':'||state||':'||goal||':'||created_at||':'||updated_at FROM workflows WHERE id='wf-v5'`).Scan(&workflow); err != nil {
		t.Fatal(err)
	}
	if err := migrated.DB().QueryRowContext(ctx, `SELECT kind||':'||content||':'||actor||':'||accepted_revision||':'||recorded_at FROM artifacts WHERE workflow_id='wf-v5'`).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	if err := migrated.DB().QueryRowContext(ctx, `SELECT schema_version||':'||actor||':'||updated_at FROM project_context WHERE singleton=1`).Scan(&projectContext); err != nil {
		t.Fatal(err)
	}
	if workflow != "wf-v5:11:implementing:preserve workflow:created:updated" || artifact != "design:preserve artifact:designer:7:recorded" || projectContext != "1:aion:2026-08-30T17:00:00Z" {
		t.Fatalf("V5 rows changed: workflow=%q artifact=%q context=%q", workflow, artifact, projectContext)
	}
	for _, table := range []string{"workflow_baselines", "normative_entries", "unit_coverage", "verification_records", "reviewed_checkpoints"} {
		var tables, rows int
		if err := migrated.DB().QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&tables); err != nil || tables != 1 {
			t.Fatalf("table %s count=%d err=%v", table, tables, err)
		}
		if err := migrated.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("historical %s rows=%d err=%v", table, rows, err)
		}
	}
	var migrations int
	if err := migrated.DB().QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil || migrations != len(schemaMigrations) {
		t.Fatalf("migrations=%d err=%v; want %d", migrations, err, len(schemaMigrations))
	}
}

func TestMigrationV6FoundationsBindReferencedRecords(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-a',1,'implementing','a','now','now')`,
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-b',1,'implementing','b','now','now')`,
		`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES('wu-a','wf-a','unit','scope','[]','[]',1,1,'claimed',1)`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-a','specification','spec','actor',1,'now')`,
	} {
		if _, err := s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	var artifactID int64
	if err := s.DB().QueryRowContext(ctx, `SELECT id FROM artifacts WHERE workflow_id='wf-a'`).Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	for name, statement := range map[string]string{
		"baseline child":        `INSERT INTO workflow_baselines(child_id,predecessor_id,predecessor_revision,artifact_manifest_json) VALUES('wf-missing','wf-also-missing',1,'[]')`,
		"normative workflow":    `INSERT INTO normative_entries(workflow_id,artifact_id,phase,entry_kind,stable_id,parent_id,operation,body_json) VALUES('wf-missing',1,'specification','requirement','REQ-1',NULL,'add','{}')`,
		"normative mismatch":    fmt.Sprintf(`INSERT INTO normative_entries(workflow_id,artifact_id,phase,entry_kind,stable_id,parent_id,operation,body_json) VALUES('wf-b',%d,'specification','requirement','REQ-1',NULL,'add','{}')`, artifactID),
		"coverage workflow":     `INSERT INTO unit_coverage(workflow_id,unit_id,requirement_id,scenario_id) VALUES('wf-missing','wu-missing','REQ-1','SCN-1')`,
		"coverage mismatch":     `INSERT INTO unit_coverage(workflow_id,unit_id,requirement_id,scenario_id) VALUES('wf-b','wu-a','REQ-1','SCN-1')`,
		"verification workflow": `INSERT INTO verification_records(id,workflow_id,unit_id,unit_revision,tier,command,outcome,fingerprint,scenario_ids_json,reused_from_id,actor,recorded_at) VALUES('vr-1','wf-missing',NULL,NULL,'focused','go test','passed','fp','[]',NULL,'actor','now')`,
		"verification mismatch": `INSERT INTO verification_records(id,workflow_id,unit_id,unit_revision,tier,command,outcome,fingerprint,scenario_ids_json,reused_from_id,actor,recorded_at) VALUES('vr-2','wf-b','wu-a',1,'focused','go test','passed','fp','[]',NULL,'actor','now')`,
		"verification reuse":    `INSERT INTO verification_records(id,workflow_id,unit_id,unit_revision,tier,command,outcome,fingerprint,scenario_ids_json,reused_from_id,actor,recorded_at) VALUES('vr-3','wf-a',NULL,NULL,'affected','go test','passed','fp','[]','vr-missing','actor','now')`,
		"checkpoint workflow":   `INSERT INTO reviewed_checkpoints(workflow_id,aggregate_revision,project_id,checkout_root,base_revision,head_revision,result_digest,dirty,commit_ref,delivery_id,recorded_at) VALUES('wf-missing',1,'project','/checkout','base','head','digest',0,NULL,NULL,'now')`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.DB().ExecContext(ctx, statement); err == nil {
				t.Fatal("orphaned foundation row was accepted")
			}
		})
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
	nextVersion := len(schemaMigrations) + 1
	if err := s.ApplyMigrations(ctx, []Migration{{Version: nextVersion, Name: "safe", SQL: `CREATE TABLE safe (id TEXT)`}, {Version: nextVersion + 1, Name: "safer", SQL: `CREATE TABLE safer (id TEXT)`}}); err != nil {
		t.Fatalf("safe ordered migrations: %v", err)
	}
	var applied int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&applied); err != nil || applied != len(schemaMigrations)+2 {
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
	if err := migrated.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil || migrations != len(schemaMigrations) {
		t.Fatalf("migration count = %d, %v; want %d", migrations, err, len(schemaMigrations))
	}
}

func TestMigration3PreservesHandlesAsImplementationAuthority(t *testing.T) {
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
	if err := legacy.ApplyMigrations(ctx, schemaMigrations[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-legacy',1,'implementing','goal','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES('wu-legacy','wf-legacy','unit','internal','[]','[]',1,1,'pending',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation) VALUES('claim','wf-legacy','wu-legacy','active','hash','actor','now','later',4)`); err != nil {
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
	var purpose string
	var generation int
	if err := migrated.db.QueryRowContext(ctx, `SELECT purpose,claim_generation FROM handles WHERE claim_id='claim'`).Scan(&purpose, &generation); err != nil {
		t.Fatal(err)
	}
	if purpose != "implementation" || generation != 4 {
		t.Fatalf("legacy handle purpose=%q generation=%d", purpose, generation)
	}
}

func TestDirectTraceMigrationIsAdditiveAndPreservesWorkflowGraphs(t *testing.T) {
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
	if err := legacy.ApplyMigrations(ctx, schemaMigrations[:3]); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO workflows(id,revision,state,goal,name,created_at,updated_at) VALUES('wf-legacy',7,'implementing','preserve me','Named','created','updated')`,
		`INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES('wf-legacy','plan_approved','implementing','aion','begin',7,'event-time')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-legacy','specification','spec bytes','specifier',3,'artifact-time')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	var workflow, event, artifact string
	if err := migrated.db.QueryRowContext(ctx, `SELECT id || ':' || revision || ':' || state || ':' || goal || ':' || name || ':' || created_at || ':' || updated_at FROM workflows WHERE id='wf-legacy'`).Scan(&workflow); err != nil {
		t.Fatal(err)
	}
	if err := migrated.db.QueryRowContext(ctx, `SELECT from_state || ':' || to_state || ':' || actor || ':' || reason || ':' || revision_after || ':' || at FROM events WHERE workflow_id='wf-legacy'`).Scan(&event); err != nil {
		t.Fatal(err)
	}
	if err := migrated.db.QueryRowContext(ctx, `SELECT kind || ':' || content || ':' || actor || ':' || accepted_revision || ':' || recorded_at FROM artifacts WHERE workflow_id='wf-legacy'`).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	if workflow != "wf-legacy:7:implementing:preserve me:Named:created:updated" || event != "plan_approved:implementing:aion:begin:7:event-time" || artifact != "specification:spec bytes:specifier:3:artifact-time" {
		t.Fatalf("legacy graph changed: workflow=%q event=%q artifact=%q", workflow, event, artifact)
	}
	for _, check := range []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM schema_migrations`, len(schemaMigrations)},
		{`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='direct_delivery_traces'`, 1},
		{`SELECT count(*) FROM direct_delivery_traces`, 0},
	} {
		var got int
		if err := migrated.db.QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s = %d, %v; want %d", check.query, got, err, check.want)
		}
	}
}

func TestPreTraceSchemaOpensReadOnlyWithoutMigration(t *testing.T) {
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
	if err := legacy.ApplyMigrations(ctx, schemaMigrations[:3]); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := OpenReadOnly(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Store.Close()
	var migrations, directTables int
	if err := opened.Store.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if err := opened.Store.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='direct_delivery_traces'`).Scan(&directTables); err != nil {
		t.Fatal(err)
	}
	if migrations != 3 || directTables != 0 {
		t.Fatalf("read-only legacy schema migrated: migrations=%d direct_tables=%d", migrations, directTables)
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
