package history

import (
	"context"
	"encoding/json"
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

func TestUnitProjectionIncludesBoundedChangeGateFacts(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	wfID, unitID := "wf-000000000000000000000001", "wu-000000000000000000000001"
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-000000000000000000000001',1,'reviewing','Gate','goal','now','now')`,
		`INSERT INTO work_units VALUES('wu-000000000000000000000001','wf-000000000000000000000001','unit','internal/history','[]','[]',3,1,'reviewing',NULL,0,1)`,
		`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES('claim','wf-000000000000000000000001','wu-000000000000000000000001','active','hash','actor','issued','expires',1,'implementation')`,
		`INSERT INTO unit_change_baselines(workflow_id,unit_id,project_id,checkout_root,base_revision,baseline_digest,scopes_json,scope_digest,accepted_budget,recorded_at) VALUES('wf-000000000000000000000001','wu-000000000000000000000001','project','/private/path','base','baseline','["internal/history"]','scope',3,'now')`,
		`INSERT INTO unit_change_measurements(workflow_id,unit_id,unit_revision,stage,additions,deletions,changed_lines,accepted_budget,claim_id,base_revision,baseline_digest,result_digest,reviewed_digest,recorded_at) VALUES('wf-000000000000000000000001','wu-000000000000000000000001',1,'evidence',2,1,3,3,'claim','base','baseline','result','result','now')`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	projection, err := New(s).Project(ctx, wfID, ViewUnit, unitID)
	if err != nil || projection.Unit == nil || projection.Unit.ChangeBaseline == nil {
		t.Fatalf("projection=%#v err=%v", projection, err)
	}
	unit := projection.Unit
	if unit.ChangeBaseline.BaseRevision != "base" || unit.ChangeBaseline.AcceptedBudget != 3 || string(unit.ChangeBaseline.Scopes) != `["internal/history"]` || len(unit.ChangeMeasurements) != 1 || unit.ChangeMeasurements[0].Additions != 2 || unit.ChangeMeasurements[0].Deletions != 1 || unit.ChangeMeasurements[0].ReviewedDigest != "result" {
		t.Fatalf("change facts=%#v/%#v", unit.ChangeBaseline, unit.ChangeMeasurements)
	}
	encodedBytes, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(encodedBytes)
	if strings.Contains(encoded, "/private/path") || strings.Contains(encoded, "claim") {
		t.Fatalf("projection leaked private authority: %s", encoded)
	}
}
