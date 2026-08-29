package correction

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

const testWorkflowID = "wf-000000000000000000000091"

func TestProjectionDerivesAutomaticExhaustedAndAuthorizedAuthority(t *testing.T) {
	ctx, db := projectionDB(t, `{"summary":"bounded","scope":"internal","work_units":[{"id":"wu-000000000000000000000091","description":"unit","scope":"internal/x","areas":["internal/x"],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1,"aggregate_correction_policy":{"automatic_rounds":1,"on_exhaustion":"require_user_authorization"}}`, "ready_to_complete")
	review(t, db, 5, `{"verdict":"corrections","summary":"review","findings":"shared invariant"}`)
	p := project(t, ctx, db)
	if !p.PolicyAware || p.Allowed != 1 || p.Used != 0 || p.BlockerRevision != 5 || p.BlockerContent == "" || p.Authority != AuthorityAutomatic || p.NextAction != "workflow recover-aggregate" {
		t.Fatalf("automatic projection = %#v", p)
	}

	artifact(t, db, "aggregate_correction", 6, `{"aggregate_review_revision":5,"groups":[{"causal_invariant":"shared","findings":["one"],"unit_ids":["wu-000000000000000000000091"]}],"assignments":[{"unit_id":"wu-000000000000000000000091","actor":"implementer"}],"authority":"automatic"}`)
	review(t, db, 8, `{"verdict":"corrections","summary":"final","findings":"new blocker"}`)
	p = project(t, ctx, db)
	if p.Used != 1 || p.BlockerRevision != 8 || p.Authority != AuthorityNone || p.NextAction != "user authorization required" {
		t.Fatalf("exhausted projection = %#v", p)
	}

	artifact(t, db, "correction_authorization", 9, `{"aggregate_review_revision":7,"reason":"wrong blocker","user_direction_confirmed":true}`)
	p = project(t, ctx, db)
	if p.Authority != AuthorityNone {
		t.Fatalf("unrelated authorization granted authority: %#v", p)
	}
	authorizationID := artifact(t, db, "correction_authorization", 10, `{"aggregate_review_revision":8,"reason":"user approved one transaction","user_direction_confirmed":true}`)
	p = project(t, ctx, db)
	if p.Authority != AuthorityAuthorized || p.NextAction != "workflow recover-aggregate" {
		t.Fatalf("authorized projection = %#v", p)
	}
	artifact(t, db, "aggregate_correction", 11, correctionBody(8, authorizationID))
	p = project(t, ctx, db)
	if p.Used != 2 || p.BlockerRevision != 0 || p.Authority != AuthorityNone {
		t.Fatalf("consumed projection = %#v", p)
	}
}

func TestProjectionCountsOnlyCorrectionsNotReviewNoise(t *testing.T) {
	ctx, db := projectionDB(t, historicalPlan(), "ready_to_complete")
	review(t, db, 3, `{"verdict":"corrections","summary":"review","findings":"one"}`)
	for i := 0; i < 4; i++ {
		if _, err := db.Exec(`INSERT INTO reviews(workflow_id,unit_id,revision,actor,verdict,summary,findings,plan_impact,recorded_at) VALUES(?,?,?,?,?,?,?,?,?)`, testWorkflowID, "wu-000000000000000000000091", i+1, "reviewer", "corrections", "unit review", "finding", "", "now"); err != nil {
			t.Fatal(err)
		}
	}
	p := project(t, ctx, db)
	if p.PolicyAware || p.Allowed != 1 || p.Used != 0 || p.Authority != AuthorityAutomatic {
		t.Fatalf("review noise consumed a round: %#v", p)
	}
	for _, unit := range []string{"wu-000000000000000000000091", "wu-000000000000000000000092"} {
		if _, err := db.Exec(`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES(?,?, 'unit_aggregate_recovered','legacy','now','work_unit',?)`, testWorkflowID, unit, unit); err != nil {
			t.Fatal(err)
		}
	}
	for _, revision := range []int{4, 6} {
		if _, err := db.Exec(`INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,'ready_to_complete','implementing','legacy','aggregate_corrections',?,'now')`, testWorkflowID, revision); err != nil {
			t.Fatal(err)
		}
	}
	p = project(t, ctx, db)
	if p.Used != 2 || p.BlockerRevision != 0 || p.Authority != AuthorityNone {
		t.Fatalf("historical recoveries not counted, resolved, or exhausted: %#v", p)
	}
}

func TestProjectionTerminalWorkflowOverridesAuthorityAndNextAction(t *testing.T) {
	ctx, db := projectionDB(t, historicalPlan(), "completed")
	review(t, db, 3, `{"verdict":"corrections","findings":"stale blocker"}`)
	p := project(t, ctx, db)
	if p.Authority != AuthorityNone || p.NextAction != "none" {
		t.Fatalf("terminal workflow retained authority: %#v", p)
	}
}

func TestProjectionUsesCompletionOrCallerLifecycleActionWithoutBlocker(t *testing.T) {
	ctx, db := projectionDB(t, historicalPlan(), "ready_to_complete")
	p := project(t, ctx, db)
	if p.NextAction != "workflow complete" || p.Authority != AuthorityNone {
		t.Fatalf("completion projection = %#v", p)
	}
	if _, err := db.Exec(`UPDATE workflows SET state='implementing' WHERE id=?`, testWorkflowID); err != nil {
		t.Fatal(err)
	}
	p = project(t, ctx, db)
	if p.NextAction != "workflow list-ready-units" {
		t.Fatalf("caller lifecycle action lost: %#v", p)
	}
}

func projectionDB(t *testing.T, body, state string) (context.Context, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err = s.DB().Exec(`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,20,?,'goal','now','now')`, testWorkflowID, state); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,'bounded','internal',1,?)`, testWorkflowID, body); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{"wu-000000000000000000000091", "wu-000000000000000000000092"} {
		if _, err = s.DB().Exec(`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,'unit','internal/x','[]','[]',1,1,'done',1)`, unit, testWorkflowID); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, s.DB()
}

func historicalPlan() string {
	return `{"summary":"legacy","scope":"internal","work_units":[{"id":"wu-000000000000000000000091","description":"unit","scope":"internal/x","areas":["internal/x"],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1}`
}

func review(t *testing.T, db *sql.DB, revision int, body string) {
	t.Helper()
	artifact(t, db, "aggregate_review", revision, body)
}

func artifact(t *testing.T, db *sql.DB, kind string, revision int, body string) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,?,?,?,?,'now')`, testWorkflowID, kind, body, "actor", revision)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func correctionBody(reviewRevision int, authorizationID int64) string {
	return fmt.Sprintf(`{"aggregate_review_revision":%d,"groups":[{"causal_invariant":"shared","findings":["one"],"unit_ids":["wu-000000000000000000000091"]}],"assignments":[{"unit_id":"wu-000000000000000000000091","actor":"implementer"}],"authority":"authorized","authorization_artifact_id":%d}`, reviewRevision, authorizationID)
}

func project(t *testing.T, ctx context.Context, db *sql.DB) Projection {
	t.Helper()
	p, err := Project(ctx, db, testWorkflowID, "workflow list-ready-units")
	if err != nil {
		t.Fatal(err)
	}
	return p
}
