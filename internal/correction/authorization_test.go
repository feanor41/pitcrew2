package correction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestAuthorizeCorrectionRequiresExactExhaustedBlockerAndAppendsAtomically(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	wfID := "wf-000000000000000000000099"
	unitID := "wu-000000000000000000000099"
	body := `{"summary":"zero","scope":"internal","work_units":[{"id":"` + unitID + `","description":"unit","scope":"internal/x","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1,"aggregate_correction_policy":{"automatic_rounds":0,"on_exhaustion":"require_user_authorization"}}`
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,5,'ready_to_complete','goal','now','now')`, []any{wfID}},
		{`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,'zero','internal',1,?)`, []any{wfID, body}},
		{`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,'unit','internal/x','[]','[]',1,1,'done',1)`, []any{unitID, wfID}},
		{`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'aggregate_review','{"verdict":"corrections","findings":"new blocker"}','reviewer',5,'now')`, []any{wfID}},
	}
	for _, statement := range statements {
		if _, err = s.DB().Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	service := NewAuthorizationService(s, func() time.Time { return time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC) })
	request := AuthorizationRequest{AggregateReviewRevision: 5, Reason: "user approved one correction", UserDirectionConfirmed: true}
	out, err := service.Authorize(ctx, wfID, 5, "aion", request)
	if err != nil || out.Revision != 6 || out.State != "ready_to_complete" || out.ArtifactID == 0 {
		t.Fatalf("outcome=%#v error=%v", out, err)
	}
	var revision, artifacts, activities, events int
	_ = s.DB().QueryRow(`SELECT revision FROM workflows WHERE id=?`, wfID).Scan(&revision)
	_ = s.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=? AND kind='correction_authorization'`, wfID).Scan(&artifacts)
	_ = s.DB().QueryRow(`SELECT count(*) FROM activities WHERE workflow_id=? AND action='correction_authorized'`, wfID).Scan(&activities)
	_ = s.DB().QueryRow(`SELECT count(*) FROM events WHERE workflow_id=? AND revision_after=6 AND from_state=to_state`, wfID).Scan(&events)
	if revision != 6 || artifacts != 1 || activities != 1 || events != 1 {
		t.Fatalf("revision=%d artifacts=%d activities=%d events=%d", revision, artifacts, activities, events)
	}
	if _, err = service.Authorize(ctx, wfID, 6, "forged-user-label", request); !errors.Is(err, ErrAuthorizationForbidden) {
		t.Fatalf("repeated authorization error=%v", err)
	}
	if _, err = service.Authorize(ctx, wfID, 5, "aion", request); !errors.Is(err, store.ErrCASMismatch) {
		t.Fatalf("stale authorization error=%v", err)
	}
}
