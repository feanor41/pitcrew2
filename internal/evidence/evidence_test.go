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

func TestCompletionRequiresApprovedReviewAndValidHandle(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	ctx := context.Background()
	if err := svc.RecordTDD(ctx, wfID, unitID, 1, validTDD()); err != nil {
		t.Fatal(err)
	}
	if err := svc.CompleteUnit(ctx, wfID, unitID, 1, 1, false, "implementer"); !errors.Is(err, ErrInvalidHandle) {
		t.Fatalf("invalid handle error=%v", err)
	}
	if err := svc.CompleteUnit(ctx, wfID, unitID, 1, 1, true, "implementer"); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("missing review error=%v", err)
	}
	if _, err := svc.RecordReview(ctx, Review{WorkflowID: wfID, UnitID: unitID, Revision: 1, Actor: "reviewer", Verdict: Approved, Summary: "solid"}); err != nil {
		t.Fatal(err)
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
	return New(s, func() time.Time { return now }), s.DB(), wf.ID, unitID
}

func validTDD() TDDRecord {
	return TDDRecord{RedCommand: "go test -run TestX", RedOutcome: "exit 1", GreenCommand: "go test -run TestX", GreenOutcome: "exit 0", RefactorSummary: "", ValidationCommand: "go test ./...", ValidationOutcome: "exit 0", ChangedPaths: "internal/x"}
}
