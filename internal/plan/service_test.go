package plan

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestReadyIgnoresReviewAuthority(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	wfID, unitID := "wf-ready", "wu-ready"
	plan := Plan{Summary: "one unit", Scope: "internal", MaxParallelUnits: 1, Units: []WorkUnit{{ID: unitID, Description: "unit", Scope: "internal/plan", Areas: []string{"internal/plan"}, EstimatedChangedLines: 1, EstimatedReviewMinutes: 1}}}
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'implementing','goal','now','now')`, []any{wfID}},
		{`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,?,?,?,?)`, []any{wfID, plan.Summary, plan.Scope, 1, string(body)}},
		{`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,?,?,?,?,?,?,?,1)`, []any{unitID, wfID, "unit", "internal/plan", `[]`, `[]`, 1, 1, Pending}},
		{`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES('review',?,?,'active','hash','reviewer','2026-01-01T00:00:00Z','2030-01-01T00:00:00Z',1,'review')`, []any{wfID, unitID}},
	}
	for _, statement := range statements {
		if _, err := s.DB().ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := NewService(s, func() time.Time { return time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC) }).Ready(ctx, wfID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != unitID {
		t.Fatalf("review authority changed readiness: %#v", ready)
	}
}
