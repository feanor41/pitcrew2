package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/project"
	"github.com/fmazzalomo/pitcrew/internal/store"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
)

func TestReviewedChangeDigestMustMatchCompletion(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	checkout := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", checkout}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "--quiet")
	run("config", "user.email", "pitcrew@example.test")
	run("config", "user.name", "PitCrew Test")
	if err := os.Mkdir(filepath.Join(checkout, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "internal", "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "base")
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	areas := `["internal"]`
	scopeDigest := sha256.Sum256([]byte(areas))
	claimID := "claim-reviewed-digest"
	if _, err = db.Exec(`UPDATE work_units SET areas=?,estimated_changed_lines=2 WHERE workflow_id=? AND id=?`, areas, wfID, unitID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES(?,?,?,?,?,?,?,?,?,?)`, claimID, wfID, unitID, "active", "hash", "implementer", "issued", "expires", 1, "implementation"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO unit_change_baselines(workflow_id,unit_id,project_id,checkout_root,base_revision,baseline_digest,scopes_json,scope_digest,accepted_budget,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, wfID, unitID, baseline.ProjectID, baseline.CheckoutRoot, baseline.BaseRevision, baseline.ResultDigest, areas, fmt.Sprintf("%x", scopeDigest), 2, "now"); err != nil {
		t.Fatal(err)
	}
	reviewed := []byte("one\ntwo\n")
	if err = os.WriteFile(filepath.Join(checkout, "internal", "new.txt"), reviewed, 0o600); err != nil {
		t.Fatal(err)
	}
	tx, _ := db.Begin()
	if err = svc.RecordTDDWithClaimAsTx(context.Background(), tx, wfID, unitID, claimID, 1, "implementer", validTDD()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var additions, deletions, total int
	var baseRevision, baselineDigest string
	if err = db.QueryRow(`SELECT additions,deletions,changed_lines,base_revision,baseline_digest FROM unit_change_measurements WHERE workflow_id=? AND unit_id=? AND unit_revision=1 AND stage='evidence'`, wfID, unitID).Scan(&additions, &deletions, &total, &baseRevision, &baselineDigest); err != nil {
		t.Fatal(err)
	}
	if additions != 2 || deletions != 0 || total != 2 || baseRevision != baseline.BaseRevision || baselineDigest != baseline.ResultDigest {
		t.Fatalf("persisted measurement=(%d,%d,%d,%q,%q)", additions, deletions, total, baseRevision, baselineDigest)
	}
	if _, err = svc.RecordReview(context.Background(), Review{WorkflowID: wfID, UnitID: unitID, Revision: 1, Actor: "reviewer", Verdict: Approved, Summary: "approved"}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "internal", "new.txt"), []byte("changed\ntext\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = svc.WithChangeEnforcement().CompleteUnitWithClaim(context.Background(), wfID, unitID, claimID, 1, 1, "implementer"); err == nil || !strings.Contains(err.Error(), "changed after review") {
		t.Fatalf("post-review change error=%v", err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "internal", "new.txt"), reviewed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = svc.CompleteUnitWithClaim(context.Background(), wfID, unitID, claimID, 1, 1, "implementer"); err != nil {
		t.Fatal(err)
	}
	var state string
	var completionMeasurements int
	_ = db.QueryRow(`SELECT state FROM work_units WHERE workflow_id=? AND id=?`, wfID, unitID).Scan(&state)
	_ = db.QueryRow(`SELECT count(*) FROM unit_change_measurements WHERE workflow_id=? AND unit_id=? AND unit_revision=1 AND stage='completion'`, wfID, unitID).Scan(&completionMeasurements)
	if state != "done" || completionMeasurements != 1 {
		t.Fatalf("completion state=%s measurements=%d", state, completionMeasurements)
	}
}

func TestChangedLineEvidenceRejectsNPlusOneWithoutPersistingFacts(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	checkout := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", checkout}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "--quiet")
	run("config", "user.email", "pitcrew@example.test")
	run("config", "user.name", "PitCrew Test")
	if err := os.Mkdir(filepath.Join(checkout, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "internal", "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "base")
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	scopes := `["internal"]`
	scopeDigest := sha256.Sum256([]byte(scopes))
	claimID := "claim-n-plus-one"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`UPDATE work_units SET areas=?,estimated_changed_lines=1 WHERE workflow_id=? AND id=?`, []any{`[]`, wfID, unitID}},
		{`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES(?,?,?,?,?,?,?,?,?,?)`, []any{claimID, wfID, unitID, "active", "hash", "implementer", "issued", "expires", 1, "implementation"}},
		{`INSERT INTO unit_change_baselines(workflow_id,unit_id,project_id,checkout_root,base_revision,baseline_digest,scopes_json,scope_digest,accepted_budget,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, []any{wfID, unitID, baseline.ProjectID, baseline.CheckoutRoot, baseline.BaseRevision, baseline.ResultDigest, scopes, fmt.Sprintf("%x", scopeDigest), 1, "now"}},
	} {
		if _, err = db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(checkout, "internal", "new.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = svc.RecordTDDWithClaimAsTx(context.Background(), tx, wfID, unitID, claimID, 1, "implementer", validTDD())
	_ = tx.Rollback()
	if err == nil || !strings.Contains(err.Error(), "measured 2, accepted 1") {
		t.Fatalf("N+1 error=%v", err)
	}
	var evidenceRows, measurementRows int
	var state string
	_ = db.QueryRow(`SELECT count(*) FROM evidence WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&evidenceRows)
	_ = db.QueryRow(`SELECT count(*) FROM unit_change_measurements WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&measurementRows)
	_ = db.QueryRow(`SELECT state FROM work_units WHERE workflow_id=? AND id=?`, wfID, unitID).Scan(&state)
	if evidenceRows != 0 || measurementRows != 0 || state != "pending" {
		t.Fatalf("N+1 changed durable state: evidence=%d measurements=%d state=%s", evidenceRows, measurementRows, state)
	}
}

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

func TestParseOutcomeAcceptsExitPrefixWithDiagnostic(t *testing.T) {
	for _, tc := range []struct {
		input string
		exit  int
		ok    bool
	}{{"exit 0: focused passed", 0, true}, {"exit 2: package failed", 2, true}, {"passed", 0, false}} {
		exit, ok := ParseOutcome(tc.input)
		if exit != tc.exit || ok != tc.ok {
			t.Fatalf("ParseOutcome(%q)=(%d,%v), want (%d,%v)", tc.input, exit, ok, tc.exit, tc.ok)
		}
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

func TestCoverageRequiresCurrentSuccessfulScenarioResultsAndUnitTiers(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	if _, err := db.Exec(`INSERT INTO unit_coverage(workflow_id,unit_id,requirement_id,scenario_id) VALUES(?,?,?,?),(?,?,?,?)`, wfID, unitID, "REQ-COV-001", "SCN-COV-001", wfID, unitID, "REQ-VER-001", "SCN-VER-001"); err != nil {
		t.Fatal(err)
	}
	record := structuredTDD("fingerprint-a")
	record.ScenarioResults = record.ScenarioResults[:1]
	if err := svc.RecordTDD(context.Background(), wfID, unitID, 1, record); err == nil || !strings.Contains(err.Error(), "SCN-VER-001") {
		t.Fatalf("missing scenario result error = %v", err)
	}
	var evidenceCount, verificationCount int
	_ = db.QueryRow(`SELECT count(*) FROM evidence WHERE workflow_id=?`, wfID).Scan(&evidenceCount)
	_ = db.QueryRow(`SELECT count(*) FROM verification_records WHERE workflow_id=?`, wfID).Scan(&verificationCount)
	if evidenceCount != 0 || verificationCount != 0 {
		t.Fatalf("rejected evidence mutated evidence=%d verification=%d", evidenceCount, verificationCount)
	}

	record = structuredTDD("fingerprint-a")
	record.VerificationRuns = record.VerificationRuns[:1]
	if err := svc.RecordTDD(context.Background(), wfID, unitID, 1, record); err == nil || !strings.Contains(err.Error(), "affected_package") {
		t.Fatalf("missing affected-package error = %v", err)
	}

	record = structuredTDD("fingerprint-a")
	marker := filepath.Join(t.TempDir(), "stored-command-ran")
	record.VerificationRuns[0].Command = "touch " + marker
	if err := svc.RecordTDD(context.Background(), wfID, unitID, 1, record); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stored command was executed or marker cannot be inspected: %v", err)
	}
	_ = db.QueryRow(`SELECT count(*) FROM verification_records WHERE workflow_id=?`, wfID).Scan(&verificationCount)
	if verificationCount != 2 {
		t.Fatalf("verification records = %d", verificationCount)
	}
}

func TestReuseRequiresImmutableMatchingSuccessfulVerification(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*VerificationRun)
		want   string
	}{
		{"stale fingerprint", func(run *VerificationRun) { run.RepositoryFingerprint = "fingerprint-b" }, "fingerprint"},
		{"different command", func(run *VerificationRun) { run.Command = "go test ./internal/evidence -count=2" }, "command"},
		{"different tier", func(run *VerificationRun) { run.Tier = AffectedPackage }, "tier"},
		{"different scenarios", func(run *VerificationRun) { run.ScenarioIDs = []string{"SCN-OTHER-001"} }, "scenario set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, db, wfID, unitID := evidenceService(t)
			if _, err := db.Exec(`INSERT INTO unit_coverage(workflow_id,unit_id,requirement_id,scenario_id) VALUES(?,?,?,?)`, wfID, unitID, "REQ-COV-001", "SCN-COV-001"); err != nil {
				t.Fatal(err)
			}
			seed := structuredTDD("fingerprint-a")
			seed.ScenarioResults = seed.ScenarioResults[:1]
			for index := range seed.VerificationRuns {
				seed.VerificationRuns[index].ScenarioIDs = []string{"SCN-COV-001"}
			}
			if err := svc.RecordTDD(context.Background(), wfID, unitID, 1, seed); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE work_units SET state='pending',revision=2 WHERE id=?`, unitID); err != nil {
				t.Fatal(err)
			}
			reused := structuredTDD("fingerprint-a")
			reused.ScenarioResults = reused.ScenarioResults[:1]
			for index := range reused.VerificationRuns {
				reused.VerificationRuns[index].ScenarioIDs = []string{"SCN-COV-001"}
				reused.VerificationRuns[index].ReusedFromID = seed.VerificationRuns[index].ID
				reused.VerificationRuns[index].ID += "-reuse"
			}
			tt.mutate(&reused.VerificationRuns[0])
			reused.ScenarioResults[0].VerificationID = reused.VerificationRuns[1].ID
			if reused.VerificationRuns[0].Tier != Focused {
				reused.VerificationRuns = append(reused.VerificationRuns, VerificationRun{ID: "vr-current-focused", Tier: Focused, Command: "go test ./internal/evidence -run Coverage", Outcome: "exit 0", RepositoryFingerprint: "fingerprint-a", ScenarioIDs: []string{"SCN-COV-001"}})
			}
			if err := svc.RecordTDD(context.Background(), wfID, unitID, 2, reused); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("reuse error = %v, want %q", err, tt.want)
			}
		})
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

func structuredTDD(fingerprint string) TDDRecord {
	record := validTDD()
	record.VerificationRuns = []VerificationRun{
		{ID: "vr-focused", Tier: Focused, Command: "go test ./internal/evidence -run Coverage", Outcome: "exit 0", RepositoryFingerprint: fingerprint, ScenarioIDs: []string{"SCN-COV-001", "SCN-VER-001"}},
		{ID: "vr-package", Tier: AffectedPackage, Command: "go test ./internal/evidence", Outcome: "exit 0", RepositoryFingerprint: fingerprint, ScenarioIDs: []string{"SCN-COV-001", "SCN-VER-001"}},
	}
	record.ScenarioResults = []ScenarioResult{
		{ScenarioID: "SCN-COV-001", Outcome: "exit 0", VerificationID: "vr-focused"},
		{ScenarioID: "SCN-VER-001", Outcome: "exit 0", VerificationID: "vr-focused"},
	}
	return record
}
