package history

import (
	"context"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

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
		`INSERT INTO workflows VALUES('wf-old',4,'completed','shared-needle old goal','2024-01-01T00:00:00Z','2024-01-02T00:00:00Z')`,
		`INSERT INTO workflows VALUES('wf-new',8,'abandoned','new goal','2025-01-01T00:00:00Z','2025-01-09T00:00:00Z')`,
		`INSERT INTO workflows VALUES('wf-active',2,'planning','active goal','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z')`,
		`INSERT INTO events VALUES('wf-new','planning','abandoned','daimon','event-reason',8,'2025-01-03T00:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration','literal % _ "quoted" café ','explorer',2,'2025-01-04T00:00:00Z')`,
		`INSERT INTO plans VALUES('wf-new','plan title','internal/history',1,'plan-text')`,
		`INSERT INTO work_units VALUES('wu-new','wf-new','unit-text','internal/history','["history"]','["wu-dependency"]',12,5,'reviewing','{"justification":"approved"}',1,3)`,
		`INSERT INTO evidence VALUES('wf-new','wu-new',3,'implementer','red cmd','exit 1 red-text','green cmd','exit 0','clean','go test','exit 0','internal/history','2025-01-06T00:00:00Z')`,
		`INSERT INTO reviews VALUES('wf-new','wu-new',3,'reviewer','approved','review-text shared-needle','', '', '2025-01-08T00:00:00Z')`,
		`INSERT INTO handles VALUES('claim','wf-new','wu-new','active','raw-handle-secret','implementer','2025-01-01T00:00:00Z','2025-01-02T00:00:00Z',1)`,
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
