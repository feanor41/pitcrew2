package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
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

func TestHistoricalOrderingOnlyPlanRetainsDoneDependencyReadiness(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const workflowID = "wf-historical-ready"
	producer, consumer := "wu-historical-producer", "wu-historical-consumer"
	body := `{"summary":"legacy","scope":"internal","work_units":[{"id":"` + producer + `","description":"producer","scope":"internal/a","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"` + consumer + `","description":"consumer","scope":"internal/b","areas":[],"depends_on":["` + producer + `"],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1}`
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'implementing','goal','now','now')`, []any{workflowID}},
		{`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,'legacy','internal',1,?)`, []any{workflowID, body}},
		{`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,'producer','internal/a','[]','[]',1,1,'done',1)`, []any{producer, workflowID}},
		{`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,'consumer','internal/b','[]',?,1,1,'pending',1)`, []any{consumer, workflowID, `["` + producer + `"]`}},
	} {
		if _, err = s.DB().ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	ready, err := NewService(s, time.Now).Ready(ctx, workflowID)
	if err != nil || len(ready) != 1 || ready[0].ID != consumer {
		t.Fatalf("historical readiness = %#v, %v", ready, err)
	}
}

func TestCoverageSubmissionRequiresStructuredSpecificationAndPersistsPairs(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const workflowID = "wf-000000000000000000000041"
	if _, err = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'designing','goal','now','now')`, workflowID); err != nil {
		t.Fatal(err)
	}
	artifact, err := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'specification','structured','specifier',1,'now')`, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	artifactID, _ := artifact.LastInsertId()
	entries := []struct{ kind, id, parent string }{
		{"requirement", "REQ-COV-001", ""},
		{"scenario", "SCN-COV-001", "REQ-COV-001"},
		{"scenario", "SCN-COV-002", "REQ-COV-001"},
	}
	for _, entry := range entries {
		var parent any
		if entry.parent != "" {
			parent = entry.parent
		}
		if _, err = s.DB().ExecContext(ctx, `INSERT INTO normative_entries(workflow_id,artifact_id,phase,entry_kind,stable_id,parent_id,operation,body_json) VALUES(?,?,?,?,?,?, 'add','{}')`, workflowID, artifactID, "specification", entry.kind, entry.id, parent); err != nil {
			t.Fatal(err)
		}
	}
	p := validPlan()
	p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-COV-001", ScenarioIDs: []string{"SCN-COV-001"}}}
	p.Units[1].Coverage = []Coverage{{RequirementID: "REQ-COV-001", ScenarioIDs: []string{"SCN-COV-002"}}}
	p.Units[0].present.coverage, p.Units[1].present.coverage = true, true
	if _, err = NewService(s, time.Now).Submit(ctx, workflowID, 1, "planner", p); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM unit_coverage WHERE workflow_id=?`, workflowID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("persisted coverage count=%d err=%v", count, err)
	}
}

func TestCoverageSubmissionRejectsMissingUnknownOrMismatchedScenariosBeforeMutation(t *testing.T) {
	tests := []struct {
		name     string
		coverage []Coverage
		want     string
	}{
		{"missing declared scenario", []Coverage{{RequirementID: "REQ-COV-001", ScenarioIDs: []string{"SCN-COV-001"}}}, "SCN-COV-002"},
		{"unknown scenario", []Coverage{{RequirementID: "REQ-COV-001", ScenarioIDs: []string{"SCN-COV-001", "SCN-UNKNOWN"}}}, "unknown scenario SCN-UNKNOWN"},
		{"mismatched parent", []Coverage{{RequirementID: "REQ-COV-002", ScenarioIDs: []string{"SCN-COV-001", "SCN-COV-002"}}}, "belongs to requirement REQ-COV-001"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			s, err := store.Open(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			const workflowID = "wf-000000000000000000000042"
			_, _ = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'designing','goal','now','now')`, workflowID)
			artifact, _ := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'specification','structured','specifier',1,'now')`, workflowID)
			artifactID, _ := artifact.LastInsertId()
			for _, row := range [][3]string{{"requirement", "REQ-COV-001", ""}, {"requirement", "REQ-COV-002", ""}, {"scenario", "SCN-COV-001", "REQ-COV-001"}, {"scenario", "SCN-COV-002", "REQ-COV-001"}} {
				var parent any
				if row[2] != "" {
					parent = row[2]
				}
				_, _ = s.DB().ExecContext(ctx, `INSERT INTO normative_entries(workflow_id,artifact_id,phase,entry_kind,stable_id,parent_id,operation,body_json) VALUES(?,?,?,?,?,?, 'add','{}')`, workflowID, artifactID, "specification", row[0], row[1], parent)
			}
			p := validPlan()
			p.Units[0].Coverage = tt.coverage
			p.Units[1].Coverage = tt.coverage
			p.Units[0].present.coverage, p.Units[1].present.coverage = true, true
			if _, err = NewService(s, time.Now).Submit(ctx, workflowID, 1, "planner", p); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Submit() error=%v; want %q", err, tt.want)
			}
			var plans int
			_ = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM plans WHERE workflow_id=?`, workflowID).Scan(&plans)
			if plans != 0 {
				t.Fatalf("invalid coverage persisted plan")
			}
		})
	}
}

func TestLegacyOpaqueSpecificationPreservesCoverageFreePlanAndRejectsInventedIDs(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, withCoverage := range []bool{false, true} {
		workflowID := fmt.Sprintf("wf-%024x", 80+i)
		_, _ = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'designing','goal','now','now')`, workflowID)
		_, _ = s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'specification','opaque prose','specifier',1,'now')`, workflowID)
		p := validPlan()
		if withCoverage {
			p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-INVENTED", ScenarioIDs: []string{"SCN-INVENTED"}}}
			p.Units[0].present.coverage = true
		}
		_, err = NewService(s, time.Now).Submit(ctx, workflowID, 1, "planner", p)
		if !withCoverage && err != nil {
			t.Fatalf("legacy plan rejected: %v", err)
		}
		if withCoverage && (err == nil || !strings.Contains(err.Error(), "legacy opaque specification")) {
			t.Fatalf("invented coverage error=%v", err)
		}
	}
}

func TestContinuationPlanCoverageUsesEffectiveBaselineAndReplacementParent(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := func() time.Time { return time.Date(2026, 8, 30, 18, 0, 0, 0, time.UTC) }
	workflows := workflow.New(s, now)
	source, err := workflows.Create(ctx, "baseline", "cover inherited scenarios", "aion")
	if err != nil {
		t.Fatal(err)
	}
	source, err = workflows.RecordNormativeArtifact(ctx, source.ID, source.Revision, workflow.Explore, "exploration", workflow.NormativeArtifact{Content: "baseline exploration", SchemaVersion: 1}, "explorer")
	if err != nil {
		t.Fatal(err)
	}
	source, err = workflows.RecordNormativeArtifact(ctx, source.ID, source.Revision, workflow.Specify, "specification", workflow.NormativeArtifact{
		Content: "baseline specification", SchemaVersion: 1,
		Entries: []workflow.NormativeEntry{
			{Kind: "requirement", ID: "REQ-CONT-COV", Operation: "add", Body: json.RawMessage(`{"text":"requirement"}`)},
			{Kind: "scenario", ID: "SCN-CONT-INHERITED", ParentID: "REQ-CONT-COV", Operation: "add", Body: json.RawMessage(`{"text":"inherited"}`)},
			{Kind: "scenario", ID: "SCN-CONT-REPLACED", ParentID: "REQ-CONT-COV", Operation: "add", Body: json.RawMessage(`{"text":"old"}`)},
		},
	}, "specifier")
	if err != nil {
		t.Fatal(err)
	}
	source, err = workflows.RecordNormativeArtifact(ctx, source.ID, source.Revision, workflow.Design, "design", workflow.NormativeArtifact{Content: "baseline design", SchemaVersion: 1}, "designer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().ExecContext(ctx, `UPDATE workflows SET state='completed' WHERE id=?`, source.ID); err != nil {
		t.Fatal(err)
	}
	continued, err := workflows.Continue(ctx, source.ID, "aion")
	if err != nil {
		t.Fatal(err)
	}
	child := continued.Workflow
	child, err = workflows.RecordNormativeArtifact(ctx, child.ID, child.Revision, workflow.Explore, "exploration", workflow.NormativeArtifact{Content: "no exploration delta", SchemaVersion: 1}, "explorer")
	if err != nil {
		t.Fatal(err)
	}
	child, err = workflows.RecordNormativeArtifact(ctx, child.ID, child.Revision, workflow.Specify, "specification", workflow.NormativeArtifact{
		Content: "replace scenario body", SchemaVersion: 1,
		Entries: []workflow.NormativeEntry{{Kind: "scenario", ID: "SCN-CONT-REPLACED", Operation: "replace", Body: json.RawMessage(`{"text":"new"}`)}},
	}, "specifier")
	if err != nil {
		t.Fatal(err)
	}
	child, err = workflows.RecordNormativeArtifact(ctx, child.ID, child.Revision, workflow.Design, "design", workflow.NormativeArtifact{Content: "no design delta", SchemaVersion: 1}, "designer")
	if err != nil {
		t.Fatal(err)
	}

	p := validPlan()
	p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-CONT-COV", ScenarioIDs: []string{"SCN-CONT-INHERITED"}}}
	p.Units[1].Coverage = []Coverage{{RequirementID: "REQ-CONT-COV", ScenarioIDs: []string{"SCN-CONT-REPLACED"}}}
	p.Units[0].present.coverage, p.Units[1].present.coverage = true, true
	if _, err = NewService(s, now).Submit(ctx, child.ID, child.Revision, "planner", p); err != nil {
		t.Fatalf("continuation plan rejected effective coverage: %v", err)
	}
}

func TestReadyRequiresImplementationToBegin(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	wfID := "wf-ready-before-implementation"
	if _, err = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'plan_approved','goal','now','now')`, wfID); err != nil {
		t.Fatal(err)
	}

	ready, err := NewService(s, time.Now).Ready(ctx, wfID)
	if !errors.Is(err, workflow.ErrInvalidTransition) {
		t.Fatalf("ready before implementation error=%v", err)
	}
	if ready != nil {
		t.Fatalf("ready before implementation returned units: %#v", ready)
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

func TestCausalDependencySubmissionPersistsExactProducerSelectors(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const workflowID = "wf-000000000000000000000185"
	_, _ = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'designing','goal','now','now')`, workflowID)
	artifact, _ := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'specification','structured','specifier',1,'now')`, workflowID)
	artifactID, _ := artifact.LastInsertId()
	for _, row := range [][3]string{{"requirement", "REQ-DEP", ""}, {"scenario", "SCN-PRODUCED", "REQ-DEP"}, {"scenario", "SCN-CONSUMER", "REQ-DEP"}} {
		var parent any
		if row[2] != "" {
			parent = row[2]
		}
		_, _ = s.DB().ExecContext(ctx, `INSERT INTO normative_entries(workflow_id,artifact_id,phase,entry_kind,stable_id,parent_id,operation,body_json) VALUES(?,?,?,?,?,?,'add','{}')`, workflowID, artifactID, "specification", row[0], row[1], parent)
	}
	p := validPlan()
	p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-DEP", ScenarioIDs: []string{"SCN-PRODUCED"}}}
	p.Units[1].Coverage = []Coverage{{RequirementID: "REQ-DEP", ScenarioIDs: []string{"SCN-CONSUMER"}}}
	p.Units[1].DependsOn = []string{p.Units[0].ID}
	p.Units[1].DependencyConsumptions = []DependencyConsumption{{ProducerUnitID: p.Units[0].ID, ScenarioIDs: []string{"SCN-PRODUCED"}}}
	invalid := p
	invalid.Units = append([]WorkUnit(nil), p.Units...)
	invalid.Units[1].DependencyConsumptions = nil
	if _, err = NewService(s, time.Now).Submit(ctx, workflowID, 1, "planner", invalid); err == nil {
		t.Fatal("ordering-only submission accepted")
	}
	var revision, plans int
	var state string
	_ = s.DB().QueryRowContext(ctx, `SELECT revision,state FROM workflows WHERE id=?`, workflowID).Scan(&revision, &state)
	_ = s.DB().QueryRowContext(ctx, `SELECT count(*) FROM plans WHERE workflow_id=?`, workflowID).Scan(&plans)
	if revision != 1 || state != "designing" || plans != 0 {
		t.Fatalf("invalid causal submission mutated workflow: revision=%d state=%s plans=%d", revision, state, plans)
	}
	_, _ = s.DB().ExecContext(ctx, `CREATE TRIGGER fail_causal_persist BEFORE INSERT ON unit_dependency_consumptions BEGIN SELECT RAISE(FAIL,'injected persistence failure'); END`)
	if _, err = NewService(s, time.Now).Submit(ctx, workflowID, 1, "planner", p); err == nil {
		t.Fatal("injected persistence failure was ignored")
	}
	var units, coverage, consumptions int
	_ = s.DB().QueryRowContext(ctx, `SELECT revision,state FROM workflows WHERE id=?`, workflowID).Scan(&revision, &state)
	_ = s.DB().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM plans WHERE workflow_id=?),(SELECT count(*) FROM work_units WHERE workflow_id=?),(SELECT count(*) FROM unit_coverage WHERE workflow_id=?),(SELECT count(*) FROM unit_dependency_consumptions WHERE workflow_id=?)`, workflowID, workflowID, workflowID, workflowID).Scan(&plans, &units, &coverage, &consumptions)
	if revision != 1 || state != "designing" || plans+units+coverage+consumptions != 0 {
		t.Fatalf("failed persistence was not atomic: revision=%d state=%s rows=%d/%d/%d/%d", revision, state, plans, units, coverage, consumptions)
	}
	_, _ = s.DB().ExecContext(ctx, `DROP TRIGGER fail_causal_persist`)
	if _, err = NewService(s, time.Now).Submit(ctx, workflowID, 1, "planner", p); err != nil {
		t.Fatal(err)
	}
	var producer, scenario string
	if err = s.DB().QueryRowContext(ctx, `SELECT producer_unit_id,scenario_id FROM unit_dependency_consumptions WHERE workflow_id=? AND consumer_unit_id=?`, workflowID, p.Units[1].ID).Scan(&producer, &scenario); err != nil {
		t.Fatal(err)
	}
	if producer != p.Units[0].ID || scenario != "SCN-PRODUCED" {
		t.Fatalf("persisted causal selector = %s/%s", producer, scenario)
	}
}

func TestReadyRequiresExactPassingCurrentProducerResultForTypedDependency(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const workflowID = "wf-causal-ready"
	producer, consumer := "wu-000000000000000000000181", "wu-000000000000000000000182"
	p := Plan{Summary: "causal", Scope: "internal", MaxParallelUnits: 1, Units: []WorkUnit{
		{ID: producer, Description: "producer", Scope: "internal/a", Areas: []string{}, State: Done},
		{ID: consumer, Description: "consumer", Scope: "internal/b", Areas: []string{}, DependsOn: []string{producer}, DependencyConsumptions: []DependencyConsumption{{ProducerUnitID: producer, ScenarioIDs: []string{"SCN-RESULT"}}}},
	}}
	body, _ := json.Marshal(p)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'implementing','goal','now','now')`, []any{workflowID}},
		{`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,?,?,?,?)`, []any{workflowID, p.Summary, p.Scope, 1, string(body)}},
		{`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,?,?,?,?,?,?,?,2)`, []any{producer, workflowID, "producer", "internal/a", `[]`, `[]`, 1, 1, Done}},
		{`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,?,?,?,?,?,?,?,1)`, []any{consumer, workflowID, "consumer", "internal/b", `[]`, `["` + producer + `"]`, 1, 1, Pending}},
		{`INSERT INTO unit_dependency_consumptions(workflow_id,consumer_unit_id,producer_unit_id,scenario_id) VALUES(?,?,?,?)`, []any{workflowID, consumer, producer, "SCN-RESULT"}},
	}
	for _, statement := range statements {
		if _, err = s.DB().ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	insertRun := func(id, unit string, revision int, outcome string) {
		t.Helper()
		_, err = s.DB().ExecContext(ctx, `INSERT INTO verification_records(id,workflow_id,unit_id,unit_revision,tier,command,outcome,fingerprint,scenario_ids_json,actor,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,'tester','now')`, id, workflowID, unit, revision, "focused", "test", outcome, "fingerprint", `["SCN-RESULT"]`)
		if err != nil {
			t.Fatal(err)
		}
	}
	assertReady := func(want int) {
		t.Helper()
		ready, readyErr := NewService(s, time.Now).Ready(ctx, workflowID)
		if readyErr != nil || len(ready) != want {
			t.Fatalf("Ready() = %#v, %v; want %d units", ready, readyErr, want)
		}
	}
	insertRun("stale", producer, 1, "exit 0")
	insertRun("wrong-producer", consumer, 1, "exit 0")
	assertReady(0)
	const foreignWorkflow, foreignUnit = "wf-foreign-ready", "wu-foreign-producer"
	_, _ = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'implementing','foreign','now','now')`, foreignWorkflow)
	_, _ = s.DB().ExecContext(ctx, `INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,'foreign','internal/foreign','[]','[]',1,1,'done',2)`, foreignUnit, foreignWorkflow)
	_, _ = s.DB().ExecContext(ctx, `INSERT INTO verification_records(id,workflow_id,unit_id,unit_revision,tier,command,outcome,fingerprint,scenario_ids_json,actor,recorded_at) VALUES('foreign-result',?,?,2,'focused','test','exit 0','fingerprint','["SCN-RESULT"]','tester','now')`, foreignWorkflow, foreignUnit)
	_, _ = s.DB().ExecContext(ctx, `INSERT INTO evidence(workflow_id,unit_id,revision,actor,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at) VALUES(?,?,2,'tester','red','exit 1','green','exit 0','','all','exit 0','internal','now')`, foreignWorkflow, foreignUnit)
	assertReady(0)
	_, _ = s.DB().ExecContext(ctx, `INSERT INTO verification_records(id,workflow_id,unit_id,unit_revision,tier,command,outcome,fingerprint,scenario_ids_json,actor,recorded_at) VALUES('malformed',?,?,2,'focused','test','exit 0','fingerprint','not-json','tester','now')`, workflowID, producer)
	assertReady(0)
	insertRun("current", producer, 2, "exit 1")
	assertReady(0)
	_, _ = s.DB().ExecContext(ctx, `UPDATE verification_records SET outcome='exit 0' WHERE id='current'`)
	assertReady(0)
	_, _ = s.DB().ExecContext(ctx, `INSERT INTO evidence(workflow_id,unit_id,revision,actor,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at) VALUES(?,?,2,'tester','red','exit 1','green','exit 0','','all','exit 0','internal','now')`, workflowID, producer)
	assertReady(1)
	_, _ = s.DB().ExecContext(ctx, `UPDATE work_units SET revision=3 WHERE id=?`, producer)
	assertReady(0)
}
