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
