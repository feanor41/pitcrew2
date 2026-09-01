package store

import (
	"context"
	"reflect"
	"testing"
)

func TestMigrationV7AddsEmptyRoadmapTablesWithoutRewritingV6(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	legacy := openStoreAtMigration(t, root, 6)
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,goal,name,created_at,updated_at) VALUES('wf-v6',13,'implementing','preserve workflow','V6 workflow','created','updated')`,
		`INSERT INTO direct_delivery_traces(id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at) VALUES('dl-111111111111111111111111','v7-preservation','direct_inline','preserve delivery','bounded','completed','kept','none',4,'aion','aion','created','updated','finished')`,
	} {
		if _, err := legacy.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	wantWorkflow := queryJoinedRow(t, legacy, `SELECT id,revision,state,goal,name,created_at,updated_at FROM workflows WHERE id='wf-v6'`)
	wantDelivery := queryJoinedRow(t, legacy, `SELECT id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at FROM direct_delivery_traces WHERE operation_key='v7-preservation'`)
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := queryJoinedRow(t, migrated, `SELECT id,revision,state,goal,name,created_at,updated_at FROM workflows WHERE id='wf-v6'`); !reflect.DeepEqual(got, wantWorkflow) {
		t.Fatalf("workflow row changed: got %v, want %v", got, wantWorkflow)
	}
	if got := queryJoinedRow(t, migrated, `SELECT id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at FROM direct_delivery_traces WHERE operation_key='v7-preservation'`); !reflect.DeepEqual(got, wantDelivery) {
		t.Fatalf("delivery row changed: got %v, want %v", got, wantDelivery)
	}
	assertRoadmapV7(t, migrated)
	if err := migrated.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertRoadmapV7(t, reopened)
}

func TestRoadmapV7RowsRemainInsideCanonicalProjectStore(t *testing.T) {
	ctx := context.Background()
	firstProject, firstPaths := centralFixture(t)
	secondProject, secondPaths := centralFixture(t)
	first, err := OpenProject(ctx, firstProject, firstPaths)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenProject(ctx, secondProject, secondPaths)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	insertRoadmapItem(t, first, "rm-111111111111111111111111", "first")
	insertRoadmapItem(t, second, "rm-222222222222222222222222", "second")
	if got := roadmapIDs(t, first); !reflect.DeepEqual(got, []string{"rm-111111111111111111111111"}) {
		t.Fatalf("first project roadmap ids = %v", got)
	}
	if got := roadmapIDs(t, second); !reflect.DeepEqual(got, []string{"rm-222222222222222222222222"}) {
		t.Fatalf("second project roadmap ids = %v", got)
	}
}

func assertRoadmapV7(t *testing.T, s *Store) {
	t.Helper()
	var migrations int
	if err := s.DB().QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil || migrations != len(schemaMigrations) {
		t.Fatalf("migration count = %d, %v; want %d", migrations, err, len(schemaMigrations))
	}
	var name string
	if err := s.DB().QueryRow(`SELECT name FROM schema_migrations WHERE version=7`).Scan(&name); err != nil || name != "roadmap inbox" {
		t.Fatalf("migration 7 name = %q, %v", name, err)
	}
	for _, table := range []string{"roadmap_items", "roadmap_bindings"} {
		var count int
		if err := s.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("table %s count = %d, %v", table, count, err)
		}
		if err := s.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("table %s rows = %d, %v", table, count, err)
		}
	}
}

func insertRoadmapItem(t *testing.T, s *Store, id, title string) {
	t.Helper()
	if _, err := s.DB().Exec(`INSERT INTO roadmap_items(id,title,body,provenance_json,created_at,local_lifecycle) VALUES(?,?,?,?,'2026-09-01T12:00:00.000Z','captured')`, id, title, "body", `{"source":"test"}`); err != nil {
		t.Fatal(err)
	}
}

func roadmapIDs(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.DB().Query(`SELECT id FROM roadmap_items ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func queryJoinedRow(t *testing.T, s *Store, query string) []any {
	t.Helper()
	rows, err := s.DB().Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal("expected one row")
	}
	values := make([]any, len(columns))
	pointers := make([]any, len(values))
	for index := range values {
		pointers[index] = &values[index]
	}
	if err := rows.Scan(pointers...); err != nil {
		t.Fatal(err)
	}
	return values
}
