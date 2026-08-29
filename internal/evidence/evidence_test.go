package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
)

func TestTDDRecordJSONUsesTheEvidenceContract(t *testing.T) {
	encoded, err := json.Marshal(validTDD())
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, key := range []string{`"red_command"`, `"red_outcome"`, `"green_command"`, `"green_outcome"`, `"refactor_summary"`, `"validation_command"`, `"validation_outcome"`, `"changed_paths"`} {
		if !strings.Contains(text, key) {
			t.Fatalf("evidence JSON %s lacks %s", text, key)
		}
	}
}

func TestTDDRecordRequiresCompleteEvidence(t *testing.T) {
	valid := validTDD()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		clear func(*TDDRecord)
	}{
		{"red command", func(r *TDDRecord) { r.RedCommand = "" }},
		{"red outcome", func(r *TDDRecord) { r.RedOutcome = "" }},
		{"green command", func(r *TDDRecord) { r.GreenCommand = "" }},
		{"green outcome", func(r *TDDRecord) { r.GreenOutcome = "" }},
		{"validation command", func(r *TDDRecord) { r.ValidationCommand = "" }},
		{"validation outcome", func(r *TDDRecord) { r.ValidationOutcome = "" }},
		{"changed paths", func(r *TDDRecord) { r.ChangedPaths = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := valid
			tt.clear(&record)
			if err := record.Validate(); err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestTDDOutcomeSemanticsRequireRedFailureAndSuccessfulGreenValidation(t *testing.T) {
	tests := []struct {
		name       string
		red        string
		green      string
		validation string
		wantError  string
	}{
		{"red passed", "exit 0: test unexpectedly passed", "exit 0", "exit 0", "red outcome must record a failing exit"},
		{"red arbitrary prose", "passed", "exit 0", "exit 0", "red outcome must record a failing exit"},
		{"green failed", "exit 1: behavior missing", "exit 1: assertion failed", "exit 0", "green outcome must record exit 0"},
		{"green arbitrary prose", "exit 1", "failed", "exit 0", "green outcome must record exit 0"},
		{"validation failed", "exit 2: behavior missing", "exit 0: focused test passed", "exit 2: package failed", "validation outcome must record exit 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validTDD()
			record.RedOutcome = tt.red
			record.GreenOutcome = tt.green
			record.ValidationOutcome = tt.validation
			if err := record.Validate(); err == nil || err.Error() != tt.wantError {
				t.Fatalf("Validate() error=%v, want %q", err, tt.wantError)
			}
		})
	}
	valid := validTDD()
	valid.RedOutcome = "  exit 17: compile error proved the missing behavior  "
	valid.GreenOutcome = "exit 0: focused test passed"
	valid.ValidationOutcome = " exit 0 "
	if err := valid.Validate(); err != nil {
		t.Fatalf("plausible outcome formatting rejected: %v", err)
	}
}

func TestRecordReviewCorrectionsIncrementRevisionAndPersistFindings(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	ctx := context.Background()
	if err := svc.RecordTDD(ctx, wfID, unitID, 1, validTDD()); err != nil {
		t.Fatal(err)
	}
	outcome, err := svc.RecordReview(ctx, Review{WorkflowID: wfID, UnitID: unitID, Revision: 1, Actor: "reviewer", Verdict: Corrections, Findings: "add boundary test", PlanImpact: Inside})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.NextRevision != 2 || outcome.PlanRevisionRequired {
		t.Fatalf("outcome = %#v", outcome)
	}
	var state string
	var revision int
	if err := db.QueryRow(`SELECT state, revision FROM work_units WHERE id=?`, unitID).Scan(&state, &revision); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || revision != 2 {
		t.Fatalf("unit state=%s revision=%d", state, revision)
	}
	var findings string
	if err := db.QueryRow(`SELECT findings FROM reviews WHERE unit_id=? AND revision=1`, unitID).Scan(&findings); err != nil || findings != "add boundary test" {
		t.Fatalf("findings=%q, %v", findings, err)
	}
}

func TestCompletionAllowsSelectiveReviewAndRequiresValidHandle(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	ctx := context.Background()
	if err := svc.RecordTDD(ctx, wfID, unitID, 1, validTDD()); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteUnit(ctx, wfID, unitID, 1, 1, false, "implementer"); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("invalid handle error=%v", err)
	}
	if err := svc.CompleteUnit(ctx, wfID, unitID, 1, 1, true, "implementer"); err != nil {
		t.Fatal(err)
	}
	var unitState, workflowState string
	if err := db.QueryRow(`SELECT state FROM work_units WHERE id=?`, unitID).Scan(&unitState); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state FROM workflows WHERE id=?`, wfID).Scan(&workflowState); err != nil {
		t.Fatal(err)
	}
	if unitState != "done" || workflowState != "ready_to_complete" {
		t.Fatalf("unit=%s workflow=%s", unitState, workflowState)
	}
}

func TestCompleteAggregateAppendsReviewAndAppliesVerdictAtomically(t *testing.T) {
	tests := []struct {
		name, verdict, findings, wantState, wantNext string
	}{
		{"corrections preserve ready state", string(Corrections), "fix integration", "ready_to_complete", "workflow recover-aggregate"},
		{"approval completes workflow", string(Approved), "", "completed", "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db, wfID, unitID := evidenceService(t)
			_, err := db.Exec(`UPDATE workflows SET state='ready_to_complete',revision=2 WHERE id=?`, wfID)
			if err == nil {
				_, err = db.Exec(`UPDATE work_units SET state='done',revision=2 WHERE id=?`, unitID)
			}
			if err == nil {
				_, err = db.Exec(`INSERT INTO evidence(workflow_id,unit_id,revision,actor,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at) VALUES(?,?,1,'aggregate-reviewer','red','exit 1','green','exit 0','','validate','exit 0','internal','now')`, wfID, unitID)
			}
			if err != nil {
				t.Fatal(err)
			}
			out, err := svc.CompleteAggregate(context.Background(), wfID, 2, AggregateReview{Actor: "aggregate-reviewer", Verdict: Verdict(tt.verdict), Summary: "checked sources", Findings: tt.findings})
			if err != nil {
				t.Fatal(err)
			}
			if out.Revision != 3 || out.State != tt.wantState || out.NextAction != tt.wantNext {
				t.Fatalf("outcome=%#v", out)
			}
			var state string
			var revision, artifacts, events, activities int
			_ = db.QueryRow(`SELECT state,revision FROM workflows WHERE id=?`, wfID).Scan(&state, &revision)
			_ = db.QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=? AND kind='aggregate_review'`, wfID).Scan(&artifacts)
			_ = db.QueryRow(`SELECT count(*) FROM events WHERE workflow_id=? AND revision_after=3`, wfID).Scan(&events)
			_ = db.QueryRow(`SELECT count(*) FROM activities WHERE workflow_id=? AND action='aggregate_review_recorded'`, wfID).Scan(&activities)
			if state != tt.wantState || revision != 3 || artifacts != 1 || events != 1 || activities != 1 {
				t.Fatalf("state=%s revision=%d artifacts=%d events=%d activities=%d", state, revision, artifacts, events, activities)
			}
		})
	}
}

func TestCompleteAggregateExhaustionAndUnresolvedBlockerRejectWithoutMutation(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	_, _ = db.Exec(`UPDATE workflows SET state='ready_to_complete',revision=2 WHERE id=?`, wfID)
	_, _ = db.Exec(`UPDATE work_units SET state='done' WHERE id=?`, unitID)
	zeroBudget := `{"summary":"one","scope":"internal","work_units":[{"id":"` + unitID + `","description":"unit","scope":"internal","areas":[],"depends_on":[],"estimated_changed_lines":10,"estimated_review_minutes":5}],"max_parallel_units":1,"aggregate_correction_policy":{"automatic_rounds":0,"on_exhaustion":"require_user_authorization"}}`
	if _, err := db.Exec(`UPDATE plans SET body=? WHERE workflow_id=?`, zeroBudget, wfID); err != nil {
		t.Fatal(err)
	}
	out, err := svc.CompleteAggregate(context.Background(), wfID, 2, AggregateReview{Actor: "reviewer", Verdict: Corrections, Summary: "final review", Findings: "new blocker"})
	if err != nil {
		t.Fatal(err)
	}
	if out.NextAction != "user authorization required" || out.Revision != 3 {
		t.Fatalf("exhausted outcome = %#v", out)
	}
	var eventsBefore int
	_ = db.QueryRow(`SELECT count(*) FROM events WHERE workflow_id=?`, wfID).Scan(&eventsBefore)
	_, err = svc.CompleteAggregate(context.Background(), wfID, 3, AggregateReview{Actor: "another-reviewer", Verdict: Approved, Summary: "retry"})
	if !errors.Is(err, ErrInvalidState) || !strings.Contains(err.Error(), "unresolved aggregate correction blocker") {
		t.Fatalf("repeated completion error = %v", err)
	}
	var state string
	var revision, artifacts, events int
	_ = db.QueryRow(`SELECT state,revision FROM workflows WHERE id=?`, wfID).Scan(&state, &revision)
	_ = db.QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=? AND kind='aggregate_review'`, wfID).Scan(&artifacts)
	_ = db.QueryRow(`SELECT count(*) FROM events WHERE workflow_id=?`, wfID).Scan(&events)
	if state != "ready_to_complete" || revision != 3 || artifacts != 1 || events != eventsBefore {
		t.Fatalf("rejected retry mutated state=%s revision=%d artifacts=%d events=%d", state, revision, artifacts, events)
	}
}

func TestCompleteAggregateRollsBackWhenActivityCannotPersist(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	_, _ = db.Exec(`UPDATE workflows SET state='ready_to_complete',revision=2 WHERE id=?`, wfID)
	_, _ = db.Exec(`UPDATE work_units SET state='done' WHERE id=?`, unitID)
	_, _ = db.Exec(`CREATE TRIGGER reject_aggregate_activity BEFORE INSERT ON activities WHEN NEW.action='aggregate_review_recorded' BEGIN SELECT RAISE(ABORT, 'reject activity'); END`)
	_, err := svc.CompleteAggregate(context.Background(), wfID, 2, AggregateReview{Actor: "reviewer", Verdict: Approved})
	if err == nil {
		t.Fatal("expected activity persistence failure")
	}
	var state string
	var revision, artifacts, events int
	_ = db.QueryRow(`SELECT state,revision FROM workflows WHERE id=?`, wfID).Scan(&state, &revision)
	_ = db.QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=?`, wfID).Scan(&artifacts)
	_ = db.QueryRow(`SELECT count(*) FROM events WHERE workflow_id=? AND revision_after=3`, wfID).Scan(&events)
	if state != "ready_to_complete" || revision != 2 || artifacts != 0 || events != 0 {
		t.Fatalf("state=%s revision=%d artifacts=%d events=%d", state, revision, artifacts, events)
	}
}

func TestCompleteAggregateRejectsImplementerActorCASAndIncompleteUnitsWithoutMutation(t *testing.T) {
	tests := []struct {
		name, actor string
		revision    int64
		unitDone    bool
		want        error
	}{
		{"implementer collision", "implementer", 2, true, ErrInvalidState},
		{"stale workflow revision", "reviewer", 1, true, store.ErrCASMismatch},
		{"incomplete unit", "reviewer", 2, false, ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db, wfID, unitID := evidenceService(t)
			_, _ = db.Exec(`UPDATE workflows SET state='ready_to_complete',revision=2 WHERE id=?`, wfID)
			_, _ = db.Exec(`INSERT INTO evidence(workflow_id,unit_id,revision,actor,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at) VALUES(?,?,1,'implementer','red','exit 1','green','exit 0','','validate','exit 0','internal','now')`, wfID, unitID)
			if tt.unitDone {
				_, _ = db.Exec(`UPDATE work_units SET state='done' WHERE id=?`, unitID)
			}
			_, err := svc.CompleteAggregate(context.Background(), wfID, tt.revision, AggregateReview{Actor: tt.actor, Verdict: Approved})
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want=%v", err, tt.want)
			}
			var artifacts int
			_ = db.QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=?`, wfID).Scan(&artifacts)
			if artifacts != 0 {
				t.Fatalf("artifacts=%d", artifacts)
			}
		})
	}
}

func TestOutsidePlanCorrectionSignalsPlanRevision(t *testing.T) {
	svc, _, wfID, unitID := evidenceService(t)
	ctx := context.Background()
	if err := svc.RecordTDD(ctx, wfID, unitID, 1, validTDD()); err != nil {
		t.Fatal(err)
	}
	outcome, err := svc.RecordReview(ctx, Review{WorkflowID: wfID, UnitID: unitID, Revision: 1, Actor: "reviewer", Verdict: Corrections, Findings: "new scope required", PlanImpact: Outside})
	if err != nil || !outcome.PlanRevisionRequired || outcome.NextRevision != 2 {
		t.Fatalf("outside-plan outcome = %#v, %v", outcome, err)
	}
}

func evidenceService(t *testing.T) (*Service, DB, string, string) {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	wfSvc := workflow.New(s, func() time.Time { return now })
	wf, err := wfSvc.Create(context.Background(), "Work", "goal", "master")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE workflows SET state='implementing' WHERE id=?`, wf.ID); err != nil {
		t.Fatal(err)
	}
	unitID := "wu-000000000000000000000001"
	_, err = s.DB().Exec(`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,admission_exception,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, unitID, wf.ID, "unit", "internal", `[]`, `[]`, 10, 5, "pending", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	planBody := `{"summary":"one","scope":"internal","work_units":[{"id":"` + unitID + `","description":"unit","scope":"internal","areas":[],"depends_on":[],"estimated_changed_lines":10,"estimated_review_minutes":5}],"max_parallel_units":1,"aggregate_correction_policy":{"automatic_rounds":1,"on_exhaustion":"require_user_authorization"}}`
	if _, err = s.DB().Exec(`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,'one','internal',1,?)`, wf.ID, planBody); err != nil {
		t.Fatal(err)
	}
	return New(s, func() time.Time { return now }), s.DB(), wf.ID, unitID
}

func validTDD() TDDRecord {
	return TDDRecord{RedCommand: "go test -run TestX", RedOutcome: "exit 1", GreenCommand: "go test -run TestX", GreenOutcome: "exit 0", RefactorSummary: "", ValidationCommand: "go test ./...", ValidationOutcome: "exit 0", ChangedPaths: "internal/x"}
}
