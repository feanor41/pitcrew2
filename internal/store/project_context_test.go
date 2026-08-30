package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/projectcontext"
)

func TestMigration5PreservesPopulatedV4(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	legacy := openStoreAtMigration(t, root, 4)
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-v4',9,'implementing','keep workflow','created','updated')`,
		`INSERT INTO direct_delivery_traces(id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at) VALUES('dl-000000000000000000000001','keep-delivery','direct_inline','keep delivery','bounded','completed','done','none',3,'aion','aion','created','updated','finished')`,
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
	var workflow, delivery string
	if err := migrated.DB().QueryRowContext(ctx, `SELECT id||':'||revision||':'||state||':'||goal||':'||created_at||':'||updated_at FROM workflows WHERE id='wf-v4'`).Scan(&workflow); err != nil {
		t.Fatal(err)
	}
	if err := migrated.DB().QueryRowContext(ctx, `SELECT id||':'||operation_key||':'||route||':'||goal||':'||route_reason||':'||status||':'||summary||':'||next_action||':'||revision||':'||creator_actor||':'||updater_actor||':'||created_at||':'||updated_at||':'||finished_at FROM direct_delivery_traces WHERE operation_key='keep-delivery'`).Scan(&delivery); err != nil {
		t.Fatal(err)
	}
	if workflow != "wf-v4:9:implementing:keep workflow:created:updated" {
		t.Fatalf("workflow changed: %q", workflow)
	}
	if delivery != "dl-000000000000000000000001:keep-delivery:direct_inline:keep delivery:bounded:completed:done:none:3:aion:aion:created:updated:finished" {
		t.Fatalf("delivery changed: %q", delivery)
	}
	for query, want := range map[string]int{
		`SELECT count(*) FROM schema_migrations`:      len(schemaMigrations),
		`SELECT count(*) FROM project_context`:        0,
		`SELECT count(*) FROM project_context_audits`: 0,
	} {
		var got int
		if err := migrated.DB().QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d, %v; want %d", query, got, err, want)
		}
	}
}

func TestReplaceProjectContextLoadsAndAuditsChangedCategories(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	first := storeRecord("first")
	at := time.Date(2026, 8, 30, 14, 0, 0, 0, time.FixedZone("offset", -3*60*60))
	changed, err := s.ReplaceProjectContext(ctx, first, "pc2-implementer", at)
	if err != nil || !changed {
		t.Fatalf("first replace changed=%v err=%v", changed, err)
	}

	snapshot, found, err := s.LoadProjectContext(ctx)
	if err != nil || !found {
		t.Fatalf("load found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(snapshot.Record, first) || snapshot.Actor != "pc2-implementer" || snapshot.UpdatedAt != "2026-08-30T17:00:00Z" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	assertAudits(t, s, []string{`["stack","runtime","deployment","architecture","documentation","sdd"]`})

	second := projectcontext.CloneRecord(first)
	second.Facts["documentation"][0].Assertion = "changed docs"
	second.Facts["stack"][0].Assertion = "changed stack"
	changed, err = s.ReplaceProjectContext(ctx, second, "next-actor", at.Add(time.Hour))
	if err != nil || !changed {
		t.Fatalf("second replace changed=%v err=%v", changed, err)
	}
	assertAudits(t, s, []string{`["stack","runtime","deployment","architecture","documentation","sdd"]`, `["stack","documentation"]`})
}

func TestReplaceProjectContextNoOpPreservesMetadataAndAudit(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	record := storeRecord("same")
	if changed, err := s.ReplaceProjectContext(ctx, record, "first", time.Unix(1, 0)); err != nil || !changed {
		t.Fatalf("initial replace changed=%v err=%v", changed, err)
	}
	if changed, err := s.ReplaceProjectContext(ctx, projectcontext.CloneRecord(record), "second", time.Unix(2, 0)); err != nil || changed {
		t.Fatalf("no-op changed=%v err=%v", changed, err)
	}
	snapshot, found, err := s.LoadProjectContext(ctx)
	if err != nil || !found || snapshot.Actor != "first" || snapshot.UpdatedAt != "1970-01-01T00:00:01Z" {
		t.Fatalf("snapshot=%#v found=%v err=%v", snapshot, found, err)
	}
	assertAuditCount(t, s, 1)
}

func TestReplaceProjectContextCanonicalizesNilAndEmptyFactsAsNoOp(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	record := storeRecord("partial")
	record.Facts["runtime"] = nil
	if changed, err := s.ReplaceProjectContext(ctx, record, "first", time.Unix(1, 0)); err != nil || !changed {
		t.Fatalf("initial replace changed=%v err=%v", changed, err)
	}
	loaded, found, err := s.LoadProjectContext(ctx)
	if err != nil || !found || loaded.Record.Facts["runtime"] == nil {
		t.Fatalf("loaded=%#v found=%v err=%v; want canonical empty facts", loaded, found, err)
	}
	if changed, err := s.ReplaceProjectContext(ctx, loaded.Record, "second", time.Unix(2, 0)); err != nil || changed {
		t.Fatalf("load-replace changed=%v err=%v", changed, err)
	}
	after, _, err := s.LoadProjectContext(ctx)
	if err != nil || after.Actor != "first" || after.UpdatedAt != "1970-01-01T00:00:01Z" {
		t.Fatalf("metadata changed: %#v err=%v", after, err)
	}
	assertAuditCount(t, s, 1)
}

func TestReplaceProjectContextRefusesCorruptExistingSnapshot(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	valid := storeRecord("candidate")
	content := `{"schema_version":1,"facts":{"stack":[],"runtime":[],"deployment":[],"architecture":[],"documentation":[],"sdd":[]},"unknown":true}`
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO project_context(singleton,schema_version,content,actor,updated_at) VALUES(1,1,?,'first','2026-08-30T17:00:00Z')`, content); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.ReplaceProjectContext(ctx, valid, "second", time.Unix(2, 0)); changed || !errors.Is(err, projectcontext.ErrInvalidRecord) {
		t.Fatalf("replace corrupt changed=%v err=%v", changed, err)
	}
	var stored string
	if err := s.DB().QueryRowContext(ctx, `SELECT content FROM project_context WHERE singleton=1`).Scan(&stored); err != nil || stored != content {
		t.Fatalf("corrupt row overwritten: %q err=%v", stored, err)
	}
	assertAuditCount(t, s, 0)
}

func TestReplaceProjectContextInvalidAndSQLiteFailureRollback(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	valid := storeRecord("valid")
	if changed, err := s.ReplaceProjectContext(ctx, valid, "first", time.Unix(1, 0)); err != nil || !changed {
		t.Fatal(err)
	}
	invalid := projectcontext.CloneRecord(valid)
	delete(invalid.Facts, "runtime")
	if changed, err := s.ReplaceProjectContext(ctx, invalid, "second", time.Unix(2, 0)); changed || !errors.Is(err, projectcontext.ErrInvalidRecord) {
		t.Fatalf("invalid changed=%v err=%v", changed, err)
	}
	if _, err := s.DB().ExecContext(ctx, `CREATE TRIGGER fail_context_audit BEFORE INSERT ON project_context_audits BEGIN SELECT RAISE(ABORT, 'audit blocked'); END`); err != nil {
		t.Fatal(err)
	}
	changedRecord := projectcontext.CloneRecord(valid)
	changedRecord.Facts["runtime"][0].Assertion = "must roll back"
	if changed, err := s.ReplaceProjectContext(ctx, changedRecord, "second", time.Unix(2, 0)); err == nil || changed {
		t.Fatalf("failed replace changed=%v err=%v", changed, err)
	}
	snapshot, found, err := s.LoadProjectContext(ctx)
	if err != nil || !found || !reflect.DeepEqual(snapshot.Record, valid) || snapshot.Actor != "first" {
		t.Fatalf("rollback snapshot=%#v found=%v err=%v", snapshot, found, err)
	}
	assertAuditCount(t, s, 1)
}

func TestLoadProjectContextRejectsCorruptStorage(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, found, err := s.LoadProjectContext(ctx); err != nil || found {
		t.Fatalf("empty load found=%v err=%v", found, err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO project_context(singleton,schema_version,content,actor,updated_at) VALUES(1,1,'{"schema_version":1,"facts":{}}','actor','not-a-time')`); err != nil {
		t.Fatal(err)
	}
	if snapshot, found, err := s.LoadProjectContext(ctx); !found || !errors.Is(err, projectcontext.ErrInvalidRecord) || !reflect.DeepEqual(snapshot, ProjectContextSnapshot{}) {
		t.Fatalf("corrupt load snapshot=%#v found=%v err=%v", snapshot, found, err)
	}
}

func TestLoadProjectContextTreatsV4SchemaAsMissing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	legacy := openStoreAtMigration(t, root, 4)
	defer legacy.Close()

	before := logicalSnapshot(t, legacy.Path())
	snapshot, found, err := legacy.LoadProjectContext(ctx)
	if err != nil || found || !reflect.DeepEqual(snapshot, ProjectContextSnapshot{}) {
		t.Fatalf("pre-V5 load snapshot=%#v found=%v err=%v; want missing", snapshot, found, err)
	}
	if after := logicalSnapshot(t, legacy.Path()); !reflect.DeepEqual(after, before) {
		t.Fatalf("pre-V5 load mutated storage\nbefore: %v\nafter:  %v", before, after)
	}
}

func TestLoadProjectContextRejectsCorruptV5WithoutContextTable(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE project_context`); err != nil {
		t.Fatal(err)
	}

	if snapshot, found, err := s.LoadProjectContext(ctx); found || err == nil || !reflect.DeepEqual(snapshot, ProjectContextSnapshot{}) {
		t.Fatalf("declared V5 load snapshot=%#v found=%v err=%v; want corruption error", snapshot, found, err)
	}
}

func openStoreAtMigration(t *testing.T, root string, count int) *Store {
	t.Helper()
	dir := root + "/.pitcrew"
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dir+"/state.db")
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db, path: dir + "/state.db"}
	if err := s.ApplyMigrations(context.Background(), schemaMigrations[:count]); err != nil {
		t.Fatal(err)
	}
	return s
}

func storeRecord(prefix string) projectcontext.Record {
	facts := make(map[string][]projectcontext.Fact)
	for _, category := range projectcontext.Categories() {
		facts[category] = []projectcontext.Fact{{Assertion: prefix + " " + category, ObservedAt: "2026-08-30T12:34:56Z", Evidence: projectcontext.Evidence{Path: "README.md"}}}
	}
	return projectcontext.Record{SchemaVersion: 1, Facts: facts}
}

func assertAudits(t *testing.T, s *Store, want []string) {
	t.Helper()
	rows, err := s.DB().Query(`SELECT changed_categories FROM project_context_audits ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		got = append(got, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("audits = %v; want %v", got, want)
	}
}

func assertAuditCount(t *testing.T, s *Store, want int) {
	t.Helper()
	var got int
	if err := s.DB().QueryRow(`SELECT count(*) FROM project_context_audits`).Scan(&got); err != nil || got != want {
		t.Fatalf("audit count=%d err=%v; want %d", got, err, want)
	}
}
