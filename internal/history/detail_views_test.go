package history

import (
	"context"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestProjectUnitDoesNotExecuteCoordinationAllUnitOrHandleQueries(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-bounded',2,'implementing','Bounded','goal','now','now')`,
		`INSERT INTO work_units VALUES('wu-selected','wf-bounded','selected','internal/history','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-unselected','wf-bounded','unselected','internal/history','[]','[]',1,1,'pending',NULL,0,1)`,
		`ALTER TABLE work_units RENAME TO work_units_authority_sentinel`,
		`CREATE VIEW work_units AS SELECT id,workflow_id,description,scope,areas,CASE WHEN id='wu-unselected' THEN json_extract('malformed-json','$') ELSE depends_on END depends_on,estimated_changed_lines,estimated_review_minutes,state,admission_exception,admission_exception_approved,revision FROM work_units_authority_sentinel`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	service := New(s)
	var workUnitQueries []struct {
		query string
		args  []any
	}
	service.queryTrace = func(query string, args []any) {
		if strings.Contains(query, "work_units") {
			workUnitQueries = append(workUnitQueries, struct {
				query string
				args  []any
			}{query, append([]any(nil), args...)})
		}
	}
	projection, err := service.Project(ctx, "wf-bounded", ViewUnit, "wu-selected")
	if err != nil || projection.Unit == nil || projection.Unit.Definition.ID != "wu-selected" {
		t.Fatalf("unit projection executed workflow-wide work-unit query: %#v, %v", projection, err)
	}
	if len(workUnitQueries) != 1 || len(workUnitQueries[0].args) != 2 || !strings.Contains(workUnitQueries[0].query, "WHERE workflow_id=? AND id=?") {
		t.Fatalf("unit projection observed unbounded work-unit SQL: %#v", workUnitQueries)
	}
	assertOnlySelectedProjection(t, projection, ViewUnit)
}

func TestUnitReleaseProjectionIsConsumedByLaterClaim(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	wfID, unitID := "wf-000000000000000000000001", "wu-000000000000000000000001"
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-000000000000000000000001',2,'implementing','Release','goal','now','now')`,
		`INSERT INTO work_units VALUES('wu-000000000000000000000001','wf-000000000000000000000001','unit','internal/history','[]','[]',1,1,'pending',NULL,0,2)`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-000000000000000000000001','unit_claim_release','{"unit_id":"wu-000000000000000000000001","released_unit_revision":1,"unit_revision_after":2,"reason":"reassign"}','implementer',2,'now')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-000000000000000000000001','wu-000000000000000000000001','unit_claim_released','implementer','now','artifact','1')`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	service := New(s)
	projection, err := service.Project(ctx, wfID, ViewUnit, unitID)
	if err != nil || projection.Unit == nil || !projection.Unit.ClaimReleasedCurrent {
		t.Fatalf("unconsumed release projection=%#v err=%v", projection.Unit, err)
	}
	if _, err = s.DB().ExecContext(ctx, `INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES(?,?,'unit_claimed','implementer','later','work_unit',?)`, wfID, unitID, unitID); err != nil {
		t.Fatal(err)
	}
	projection, err = service.Project(ctx, wfID, ViewUnit, unitID)
	if err != nil || projection.Unit == nil || projection.Unit.ClaimReleasedCurrent {
		t.Fatalf("consumed release projection=%#v err=%v", projection.Unit, err)
	}
}
