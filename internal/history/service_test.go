package history

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestServiceProjectsNamedGridAndExactActivityTimeline(t *testing.T) {
	ctx := context.Background()
	service := New(openHistory(t, true))
	workflows, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join([]string{workflows[0].ID, workflows[1].ID, workflows[2].ID}, ","); got != "wf-active,wf-new,wf-old" {
		t.Fatalf("created order = %s", got)
	}
	if workflows[0].Name != "Active delivery" || workflows[0].NameDerived {
		t.Fatalf("persisted name = %q derived=%v", workflows[0].Name, workflows[0].NameDerived)
	}
	if workflows[1].Name != "new goal" || !workflows[1].NameDerived {
		t.Fatalf("fallback name = %q derived=%v", workflows[1].Name, workflows[1].NameDerived)
	}

	detail, err := service.Detail(ctx, "wf-new")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Workflow.Name != "new goal" || !detail.Workflow.NameDerived {
		t.Fatalf("detail name = %q derived=%v", detail.Workflow.Name, detail.Workflow.NameDerived)
	}
	for i := 1; i < len(detail.Timeline); i++ {
		if detail.Timeline[i-1].At > detail.Timeline[i].At {
			t.Fatalf("timeline not chronological: %#v", detail.Timeline)
		}
	}
	if sameTime := timelineEntry(detail.Timeline, "exploration"); sameTime.RecordID != "artifact:2" || !sameTime.Legacy {
		t.Fatalf("same-time legacy record was suppressed: %#v", sameTime)
	}
	for _, want := range []struct {
		action, actor, kind, subject string
	}{
		{"workflow_created", "daimon", "workflow", "wf-new"},
		{"plan_submitted", "planner", "plan", "wf-new"},
		{"unit_review_recorded", "reviewer", "review", "wu-new@3"},
	} {
		entry := timelineEntry(detail.Timeline, want.action)
		if entry.Actor != want.actor || entry.SubjectKind != want.kind || entry.SubjectID != want.subject || entry.Legacy {
			t.Errorf("timeline %s = %#v", want.action, entry)
		}
		resolved, err := service.ResolveActivity(ctx, entry)
		if err != nil || resolved.Record.Kind != want.kind {
			t.Errorf("ResolveActivity(%s) = %#v, %v", want.action, resolved, err)
		}
	}
	workflowResult, _ := service.ResolveActivity(ctx, timelineEntry(detail.Timeline, "workflow_created"))
	planResult, _ := service.ResolveActivity(ctx, timelineEntry(detail.Timeline, "plan_submitted"))
	if workflowResult.Record.ID == planResult.Record.ID {
		t.Fatalf("subject-kind collision resolved to same record: %q", workflowResult.Record.ID)
	}
	seenKinds := map[string]bool{}
	for _, entry := range detail.Timeline {
		resolved, err := service.ResolveActivity(ctx, entry)
		if err != nil {
			t.Fatalf("ResolveActivity(%#v): %v", entry, err)
		}
		seenKinds[resolved.Record.Kind] = true
	}
	for _, kind := range []string{"workflow", "event", "exploration", "plan", "work_unit", "evidence", "review"} {
		if !seenKinds[kind] {
			t.Errorf("unresolved durable kind %q", kind)
		}
	}
}

func TestServiceReadsLegacySchemaHonestlyWithoutMigration(t *testing.T) {
	ctx := context.Background()
	root := legacyHistoryRoot(t)
	before := schemaSnapshot(t, root)
	opened, err := store.OpenReadOnly(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Store.Close() })
	service := New(opened.Store)
	workflows, err := service.List(ctx)
	if err != nil || len(workflows) != 1 || workflows[0].Name != "Legacy goal" || !workflows[0].NameDerived {
		t.Fatalf("legacy List() = %#v, %v", workflows, err)
	}
	detail, err := service.Detail(ctx, "wf-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Timeline) != 2 {
		t.Fatalf("legacy timeline = %#v", detail.Timeline)
	}
	for _, entry := range detail.Timeline {
		if !entry.Legacy || entry.Actor == "" || entry.At == "" {
			t.Fatalf("dishonest legacy entry = %#v", entry)
		}
	}
	if got := schemaSnapshot(t, root); got != before {
		t.Fatalf("read-only history changed schema\nbefore: %s\nafter:  %s", before, got)
	}
}

func TestServiceProjectsDeterministicProjectHistory(t *testing.T) {
	ctx := context.Background()
	empty := openHistory(t, false)
	if got, err := New(empty).List(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty List() = %v, %v", got, err)
	}

	s := openHistory(t, true)
	service := New(s)
	got, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if states := []string{got[0].State, got[1].State, got[2].State}; strings.Join(states, ",") != "planning,abandoned,completed" {
		t.Fatalf("states = %v", states)
	}
	detail, err := service.Detail(ctx, "wf-new")
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"event", "exploration", "plan", "work_unit", "evidence", "review"}
	for _, want := range wantKinds {
		if !hasRecord(detail.Records, want) {
			t.Errorf("missing %q record: %#v", want, detail.Records)
		}
	}
	joined := recordText(detail.Records)
	for _, want := range []string{"wu-dependency", "approved=1", "red-text", "review-text"} {
		if !strings.Contains(joined, want) {
			t.Errorf("detail missing %q", want)
		}
	}
	if strings.Contains(joined, "raw-handle-secret") {
		t.Fatal("claim internals leaked into detail")
	}

	other := openHistory(t, false)
	if isolated, err := New(other).List(ctx); err != nil || len(isolated) != 0 {
		t.Fatalf("isolated List() = %v, %v", isolated, err)
	}
}

func TestServiceSearchIsLiteralBoundedAndResolvable(t *testing.T) {
	ctx := context.Background()
	service := New(openHistory(t, true))
	tests := []struct{ query, kind string }{
		{"%", "exploration"}, {"_", "exploration"}, {`"quoted"`, "exploration"},
		{"CAFÉ", "exploration"}, {"event-reason", "event"}, {"plan-text", "plan"},
		{"unit-text", "work_unit"}, {"red-text", "evidence"}, {"review-text", "review"},
		{"bounded-token", "exploration"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := service.Search(ctx, tt.query)
			if err != nil || len(got) == 0 || got[0].Kind != tt.kind {
				t.Fatalf("Search(%q) = %#v, %v", tt.query, got, err)
			}
			if got[0].WorkflowID == "" || len([]rune(got[0].Context)) > ContextRunes {
				t.Fatalf("identity/context = %#v", got[0])
			}
			if strings.Contains("work_unit evidence review", tt.kind) && (got[0].UnitID == "" || got[0].Revision != 3) {
				t.Fatalf("unit identity = %#v", got[0])
			}
			if resolved, err := service.Resolve(ctx, got[0]); err != nil || resolved.Detail.Workflow.ID != got[0].WorkflowID || resolved.Record.ID != got[0].RecordID {
				t.Fatalf("Resolve() = %#v, %v", resolved, err)
			}
		})
	}
	for _, query := range []string{"", "   ", "no-match", "raw-handle-secret"} {
		if got, err := service.Search(ctx, query); err != nil || len(got) != 0 {
			t.Fatalf("Search(%q) = %#v, %v", query, got, err)
		}
	}
	got, err := service.Search(ctx, "shared-needle")
	if err != nil || len(got) != 2 || got[0].Kind != "review" || got[1].Kind != "goal" {
		t.Fatalf("ordered Search() = %#v, %v", got, err)
	}
	stable, err := service.Search(ctx, "stable-order")
	if err != nil || len(stable) != 2 || stable[0].Kind != "design" || stable[1].Kind != "specification" {
		t.Fatalf("stable Search() = %#v, %v", stable, err)
	}
	many, err := service.Search(ctx, "many-match")
	if err != nil || len(many) != 205 {
		t.Fatalf("complete Search() count = %d, %v", len(many), err)
	}
	collisions, err := service.Search(ctx, "collision-record")
	if err != nil || len(collisions) != 2 || collisions[0].RecordID == collisions[1].RecordID {
		t.Fatalf("collision identities = %#v, %v", collisions, err)
	}
	for _, result := range collisions {
		resolved, err := service.Resolve(ctx, result)
		if err != nil || result.Context != strings.TrimSpace(resolved.Record.Title+" "+resolved.Record.Content) {
			t.Fatalf("collision Resolve() = %#v, %v", resolved, err)
		}
	}
}

func openHistory(t *testing.T, seeded bool) *store.Store {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-old',4,'completed','shared-needle old goal','2024-01-01T00:00:00Z','2024-01-02T00:00:00Z')`,
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-new',8,'abandoned','new goal','2025-01-01T00:00:00Z','2025-01-09T00:00:00Z')`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-active',2,'planning','Active delivery','active goal','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z')`,
		`INSERT INTO events VALUES('wf-new','planning','abandoned','daimon','event-reason',8,'2025-01-03T00:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration','literal % _ "quoted" café ','explorer',2,'2025-01-04T00:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration','same-time legacy','explorer',2,'2025-01-04T00:00:00Z')`,
		`INSERT INTO plans VALUES('wf-new','plan title','internal/history',1,'plan-text')`,
		`INSERT INTO work_units VALUES('wu-new','wf-new','unit-text','internal/history','["history"]','["wu-dependency"]',12,5,'reviewing','{"justification":"approved"}',1,3)`,
		`INSERT INTO evidence VALUES('wf-new','wu-new',3,'implementer','red cmd','exit 1 red-text','green cmd','exit 0','clean','go test','exit 0','internal/history','2025-01-06T00:00:00Z')`,
		`INSERT INTO reviews VALUES('wf-new','wu-new',3,'reviewer','approved','review-text shared-needle','', '', '2025-01-08T00:00:00Z')`,
		`INSERT INTO handles VALUES('claim','wf-new','wu-new','active','raw-handle-secret','implementer','2025-01-01T00:00:00Z','2025-01-02T00:00:00Z',1)`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new',NULL,'workflow_created','daimon','2025-01-01T00:00:00Z','workflow','wf-new')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new',NULL,'workflow_abandoned','daimon','2025-01-03T00:00:00Z','event','wf-new@8')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new',NULL,'exploration_recorded','explorer','2025-01-04T00:00:00Z','artifact','1')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new',NULL,'plan_submitted','planner','2025-01-05T00:00:00Z','plan','wf-new')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new','wu-new','unit_claimed','implementer','2025-01-05T01:00:00Z','work_unit','wu-new')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new','wu-new','unit_tdd_recorded','implementer','2025-01-06T00:00:00Z','evidence','wu-new@3')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new','wu-new','unit_review_recorded','reviewer','2025-01-08T00:00:00Z','review','wu-new@3')`,
	}
	if seeded {
		for _, statement := range statements {
			if _, err := s.DB().ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	if seeded {
		long := strings.Repeat("before ", 30) + "bounded-token" + strings.Repeat(" after", 30)
		for _, row := range []struct{ kind, content string }{{"exploration", long}, {"design", "stable-order"}, {"specification", "stable-order"}} {
			if _, err := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new',?,?, 'actor',3,'2025-01-05T00:00:00Z')`, row.kind, row.content); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 205; i++ {
			if _, err := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration','many-match','actor',4,'2025-01-07T00:00:00Z')`); err != nil {
				t.Fatal(err)
			}
		}
		for _, content := range []string{"collision-record one", "collision-record two"} {
			if _, err := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration',?,'actor',5,'2025-01-07T00:00:00Z')`, content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := store.OpenReadOnly(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Store.Close() })
	return result.Store
}

func hasRecord(records []Record, kind string) bool {
	for _, record := range records {
		if record.Kind == kind {
			return true
		}
	}
	return false
}

func recordText(records []Record) string {
	var b strings.Builder
	for _, record := range records {
		b.WriteString(record.Content)
	}
	return b.String()
}

func timelineEntry(entries []Activity, action string) Activity {
	for _, entry := range entries {
		if entry.Action == action {
			return entry
		}
	}
	return Activity{}
}

func legacyHistoryRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pitcrew"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, ".pitcrew", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE workflows (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, state TEXT NOT NULL, goal TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE events (workflow_id TEXT NOT NULL, from_state TEXT NOT NULL, to_state TEXT NOT NULL, actor TEXT NOT NULL, reason TEXT NOT NULL, revision_after INTEGER NOT NULL, at TEXT NOT NULL)`,
		`CREATE TABLE artifacts (id INTEGER PRIMARY KEY AUTOINCREMENT, workflow_id TEXT NOT NULL, kind TEXT NOT NULL, content TEXT NOT NULL, actor TEXT NOT NULL, accepted_revision INTEGER NOT NULL, recorded_at TEXT NOT NULL)`,
		`CREATE TABLE plans (workflow_id TEXT PRIMARY KEY, summary TEXT NOT NULL, scope TEXT NOT NULL, max_parallel_units INTEGER NOT NULL, body TEXT NOT NULL)`,
		`CREATE TABLE work_units (id TEXT PRIMARY KEY, workflow_id TEXT NOT NULL, description TEXT NOT NULL, scope TEXT NOT NULL, areas TEXT NOT NULL, depends_on TEXT NOT NULL, estimated_changed_lines INTEGER NOT NULL, estimated_review_minutes INTEGER NOT NULL, state TEXT NOT NULL, admission_exception TEXT, admission_exception_approved INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE evidence (workflow_id TEXT NOT NULL, unit_id TEXT NOT NULL, revision INTEGER NOT NULL, actor TEXT NOT NULL, red_command TEXT NOT NULL, red_outcome TEXT NOT NULL, green_command TEXT NOT NULL, green_outcome TEXT NOT NULL, refactor_summary TEXT NOT NULL, validation_command TEXT NOT NULL, validation_outcome TEXT NOT NULL, changed_paths TEXT NOT NULL, recorded_at TEXT NOT NULL)`,
		`CREATE TABLE reviews (workflow_id TEXT NOT NULL, unit_id TEXT NOT NULL, revision INTEGER NOT NULL, actor TEXT NOT NULL, verdict TEXT NOT NULL, summary TEXT NOT NULL, findings TEXT NOT NULL, plan_impact TEXT NOT NULL, recorded_at TEXT NOT NULL)`,
		`INSERT INTO workflows VALUES('wf-legacy',3,'planning','Legacy goal','2023-01-01T00:00:00Z','2023-01-03T00:00:00Z')`,
		`INSERT INTO events VALUES('wf-legacy','draft','exploring','daimon','',2,'2023-01-02T00:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-legacy','exploration','legacy evidence','pc2-explorer',2,'2023-01-02T01:00:00Z')`,
		`INSERT INTO plans VALUES('wf-legacy','untimed plan','scope',1,'{}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return root
}

func schemaSnapshot(t *testing.T, root string) string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(root, ".pitcrew", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT type || ':' || name || ':' || sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	return strings.Join(values, "\n")
}
