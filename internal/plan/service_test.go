package plan

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestAggregateCorrectionPolicySubmissionPersistsDefaultAndRejectsInvalidBeforeMutation(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const workflowID = "wf-000000000000000000000001"
	if _, err = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'designing','goal','now','now')`, workflowID); err != nil {
		t.Fatal(err)
	}
	service := NewService(s, func() time.Time { return time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC) })
	invalid := validPlan()
	invalid.AggregateCorrectionPolicy = AggregateCorrectionPolicy{AutomaticRounds: 2, OnExhaustion: RequireUserAuthorization}
	invalid.present.aggregateCorrectionPolicy = true
	if _, err = service.Submit(ctx, workflowID, 1, "planner", invalid); err == nil {
		t.Fatal("invalid policy accepted")
	}
	var revision int
	var count int
	if err = s.DB().QueryRowContext(ctx, `SELECT revision FROM workflows WHERE id=?`, workflowID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM plans WHERE workflow_id=?`, workflowID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if revision != 1 || count != 0 {
		t.Fatalf("invalid submission mutated state: revision=%d plans=%d", revision, count)
	}

	if _, err = service.Submit(ctx, workflowID, 1, "planner", validPlan()); err != nil {
		t.Fatal(err)
	}
	var body string
	if err = s.DB().QueryRowContext(ctx, `SELECT body FROM plans WHERE workflow_id=?`, workflowID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	var persisted Plan
	if err = json.Unmarshal([]byte(body), &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.HasAggregateCorrectionPolicy() || persisted.AggregateCorrectionPolicy != DefaultAggregateCorrectionPolicy() {
		t.Fatalf("persisted policy = %#v in %s", persisted.AggregateCorrectionPolicy, body)
	}
}

func TestHistoricalPlanLoadProjectsDefaultWithoutRewritingBody(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	historical := `{"summary":"one unit","scope":"internal","work_units":[{"id":"wu-000000000000000000000001","description":"unit","scope":"internal/plan","areas":["internal/plan"],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1}`
	if _, err = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-historical',2,'planning','goal','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().ExecContext(ctx, `INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES('wf-historical','one unit','internal',1,?)`, historical); err != nil {
		t.Fatal(err)
	}
	p, err := NewService(s, time.Now).load(ctx, "wf-historical")
	if err != nil {
		t.Fatal(err)
	}
	if p.HasAggregateCorrectionPolicy() || p.AggregateCorrectionPolicy != DefaultAggregateCorrectionPolicy() {
		t.Fatalf("historical projection = %#v", p)
	}
	var after string
	if err = s.DB().QueryRowContext(ctx, `SELECT body FROM plans WHERE workflow_id='wf-historical'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != historical {
		t.Fatalf("historical body rewritten:\n%s\nwant:\n%s", after, historical)
	}
}

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
