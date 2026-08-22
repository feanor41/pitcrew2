package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestArtifactPlanUnitReviewAndCompletionLifecycle(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	for _, stage := range []struct{ command, actor, content string }{{"explore", "explorer", "facts"}, {"spec", "specifier", "gherkin"}, {"design", "designer", "architecture"}} {
		input := writeInput(t, root, stage.command+".json", `{"content":"`+stage.content+`"}`)
		response := mustOK(t, runAt(t, root, "workflow", stage.command, "--workflow-id", wfID, "--revision", itoa(revision), "--actor", stage.actor, "--input-file", input))
		revision = workflowRevision(t, response)
	}
	unitID := "wu-000000000000000000000001"
	planBody := `{"summary":"one unit","scope":"internal","max_parallel_units":1,"work_units":[{"id":"` + unitID + `","description":"implement","scope":"internal/feature","areas":["internal/feature"],"depends_on":[],"estimated_changed_lines":100,"estimated_review_minutes":20}]}`
	planFile := writeInput(t, root, "plan.json", planBody)
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "planner", "--input-file", planFile)))
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "approve-plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon")))
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "begin-implementation", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon")))
	ready := mustOK(t, runAt(t, root, "workflow", "list-ready-units", "--workflow-id", wfID))
	if !strings.Contains(string(ready), unitID) {
		t.Fatalf("ready=%s", ready)
	}
	handleDir := filepath.Join(root, "handles")
	claim := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", handleDir))
	handlePath := stringField(t, claim, "handle_path")
	if info, err := os.Stat(handlePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("handle=%v %v", info, err)
	}
	tddFile := writeInput(t, root, "tdd.json", `{"red_command":"go test -run X","red_outcome":"exit 1","green_command":"go test -run X","green_outcome":"exit 0","refactor_summary":"clean","validation_command":"go test ./...","validation_outcome":"exit 0","changed_paths":"internal/feature"}`)
	mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath, "--input-file", tddFile))
	reviewFile := writeInput(t, root, "review.json", `{"verdict":"approved","summary":"good","findings":""}`)
	mustOK(t, runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", handlePath, "--input-file", reviewFile))
	completed := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath))
	if _, err := os.Stat(handlePath); !os.IsNotExist(err) {
		t.Fatalf("completed handle remains: %v", err)
	}
	deadHandle := runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath)
	if deadHandle.code != 5 || deadHandle.stdout != "" {
		t.Fatalf("dead handle=%#v", deadHandle)
	}
	revision = workflowRevision(t, completed)
	final := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "archivist"))
	if state := workflowState(t, final); state != "completed" {
		t.Fatalf("state=%s", state)
	}
	shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	var document struct {
		Data struct {
			Artifacts []struct {
				Kind, Content, Actor string
				Revision             int64
			} `json:"artifacts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(shown, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Data.Artifacts) != 3 || document.Data.Artifacts[0].Kind != "exploration" || document.Data.Artifacts[2].Revision != 4 {
		t.Fatalf("artifacts=%#v", document.Data.Artifacts)
	}
	wantActions := []string{"workflow_created", "exploration_recorded", "specification_recorded", "design_recorded", "plan_submitted", "plan_approved", "implementation_started", "unit_claimed", "unit_tdd_recorded", "unit_review_recorded", "unit_completed", "workflow_completed"}
	wantSubjects := []string{wfID, "1", "2", "3", wfID, wfID, wfID + "@7", unitID, unitID + "@1", unitID + "@1", unitID, wfID + "@9"}
	got, subjects := storedActivities(t, root, wfID)
	if strings.Join(got, ",") != strings.Join(wantActions, ",") || strings.Join(subjects, ",") != strings.Join(wantSubjects, ",") {
		t.Fatalf("activities=%v want=%v", got, wantActions)
	}
}

func TestActivityFailureRollsBackDomainAndHandleMutations(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	installFailingActivityTrigger(t, root)
	input := writeInput(t, root, "explore.json", `{"content":"facts"}`)
	if failed := runAt(t, root, "workflow", "explore", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "explorer", "--input-file", input); failed.code != 1 {
		t.Fatalf("artifact failure=%#v", failed)
	}
	s, _ := store.Open(context.Background(), root)
	defer s.Close()
	var revisionAfter, artifacts, activities int
	_ = s.DB().QueryRow(`SELECT revision FROM workflows WHERE id=?`, wfID).Scan(&revisionAfter)
	_ = s.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=?`, wfID).Scan(&artifacts)
	_ = s.DB().QueryRow(`SELECT count(*) FROM activities WHERE workflow_id=?`, wfID).Scan(&activities)
	if revisionAfter != 1 || artifacts != 0 || activities != 1 {
		t.Fatalf("failed artifact mutated revision=%d artifacts=%d activities=%d", revisionAfter, artifacts, activities)
	}
	_ = s.Close()

	root = t.TempDir()
	wfID, revision = createWorkflow(t, root)
	for _, stage := range []string{"explore", "spec", "design"} {
		input = writeInput(t, root, stage+".json", `{"content":"`+stage+`"}`)
		revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", stage, "--workflow-id", wfID, "--revision", itoa(revision), "--actor", stage, "--input-file", input)))
	}
	installFailingActivityTrigger(t, root)
	planFile := writeInput(t, root, "plan.json", `{"summary":"one","scope":"internal","max_parallel_units":1,"work_units":[{"id":"wu-000000000000000000000001","description":"unit","scope":"internal/unit","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}]}`)
	if failed := runAt(t, root, "workflow", "plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "planner", "--input-file", planFile); failed.code != 1 {
		t.Fatalf("plan failure=%#v", failed)
	}
	s, _ = store.Open(context.Background(), root)
	var plans int
	_ = s.DB().QueryRow(`SELECT count(*) FROM plans WHERE workflow_id=?`, wfID).Scan(&plans)
	if plans != 0 {
		t.Fatalf("failed plan persisted %d rows", plans)
	}
	_ = s.Close()

	root = t.TempDir()
	wfID, unitID, _ := setupImplementingUnit(t, root)
	installFailingActivityTrigger(t, root)
	handleDir := filepath.Join(root, "failed-handle")
	if failed := runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", handleDir); failed.code != 1 {
		t.Fatalf("claim failure=%#v", failed)
	}
	entries, err := os.ReadDir(handleDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed claim left files=%v error=%v", entries, err)
	}

	root = t.TempDir()
	wfID, unitID, _ = setupImplementingUnit(t, root)
	claim := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "handles")))
	handlePath := stringField(t, claim, "handle_path")
	before, _ := os.ReadFile(handlePath)
	installFailingActivityTrigger(t, root)
	tdd := writeInput(t, root, "tdd.json", `{"red_command":"red","red_outcome":"exit 1","green_command":"green","green_outcome":"exit 0","refactor_summary":"","validation_command":"all","validation_outcome":"exit 0","changed_paths":"internal"}`)
	if failed := runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath, "--input-file", tdd); failed.code != 1 {
		t.Fatalf("evidence failure=%#v", failed)
	}
	after, _ := os.ReadFile(handlePath)
	state, count := storedUnitEvidence(t, root, wfID, unitID)
	if !bytes.Equal(before, after) || state != "pending" || count != 0 {
		t.Fatalf("failed evidence mutated file=%t state=%s evidence=%d", !bytes.Equal(before, after), state, count)
	}
}

func storedActivities(t *testing.T, root, wfID string) ([]string, []string) {
	t.Helper()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.DB().Query(`SELECT action,subject_id FROM activities WHERE workflow_id=? ORDER BY id`, wfID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	var subjects []string
	for rows.Next() {
		var action, subject string
		if err = rows.Scan(&action, &subject); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
		subjects = append(subjects, subject)
	}
	return actions, subjects
}

func installFailingActivityTrigger(t *testing.T, root string) {
	t.Helper()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.DB().Exec(`CREATE TRIGGER fail_activity BEFORE INSERT ON activities BEGIN SELECT RAISE(ABORT,'activity failure'); END`); err != nil {
		t.Fatal(err)
	}
}

func TestCASActorCorrectionsRecoveryAbandonAndDebugClaim(t *testing.T) {
	root := t.TempDir()
	wfID, _ := createWorkflow(t, root)
	badCAS := runAt(t, root, "workflow", "explore", "--workflow-id", wfID, "--revision", "9", "--actor", "x", "--input-file", writeInput(t, root, "a.json", `{"content":"x"}`))
	if badCAS.code != 4 {
		t.Fatalf("CAS=%#v", badCAS)
	}
	abandoned := mustOK(t, runAt(t, root, "workflow", "abandon", "--workflow-id", wfID, "--revision", "1", "--actor", "daimon", "--reason", "superseded"))
	if workflowState(t, abandoned) != "abandoned" {
		t.Fatal(string(abandoned))
	}
	if actions, _ := storedActivities(t, root, wfID); strings.Join(actions, ",") != "workflow_created,workflow_abandoned" {
		t.Fatalf("abandon activities=%v", actions)
	}

	root = t.TempDir()
	wfID, unitID, handlePath := setupReviewingUnit(t, root, "implementer")
	before, err := os.ReadFile(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	invalidReview := writeInput(t, root, "invalid-review.json", `{"verdict":"approved","summary":"bad shape","findings":"","plan_impact":"inside"}`)
	invalid := runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", handlePath, "--input-file", invalidReview)
	after, readErr := os.ReadFile(handlePath)
	if invalid.code != 3 || readErr != nil || string(before) != string(after) {
		t.Fatalf("invalid review mutated handle: %#v %v", invalid, readErr)
	}
	reviewFile := writeInput(t, root, "correction.json", `{"verdict":"corrections","summary":"changes","findings":"add case","plan_impact":"inside"}`)
	collision := runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath, "--input-file", reviewFile)
	if collision.code != 3 {
		t.Fatalf("collision=%#v", collision)
	}
	correction := mustOK(t, runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", handlePath, "--input-file", reviewFile))
	if !strings.Contains(string(correction), `"unit_revision":2`) || !strings.Contains(string(correction), `"next_action":"workflow claim-unit"`) {
		t.Fatal(string(correction))
	}
	recovered := mustOK(t, runAt(t, root, "workflow", "recover-unit-claim", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--handle-dir", filepath.Join(root, "recovered")))
	if stringField(t, recovered, "handle_path") == handlePath {
		t.Fatal("recovery reused handle")
	}
	if actions, _ := storedActivities(t, root, wfID); actions[len(actions)-1] != "unit_claim_recovered" {
		t.Fatalf("recovery activity=%v", actions)
	}

	debugRoot := t.TempDir()
	debugWF, debugUnit, _ := setupImplementingUnit(t, debugRoot)
	casDir := filepath.Join(debugRoot, "cas-mismatch")
	claimCAS := runAt(t, debugRoot, "workflow", "claim-unit", "--workflow-id", debugWF, "--unit-id", debugUnit, "--revision", "2", "--actor", "operator", "--handle-dir", casDir)
	if claimCAS.code != 4 {
		t.Fatalf("claim CAS=%#v", claimCAS)
	}
	if _, err := os.Stat(casDir); !os.IsNotExist(err) {
		t.Fatalf("CAS created handle dir: %v", err)
	}
	debugDir := filepath.Join(debugRoot, "debug")
	debug := mustOK(t, runAt(t, debugRoot, "workflow", "claim-unit", "--workflow-id", debugWF, "--unit-id", debugUnit, "--revision", "1", "--actor", "operator", "--handle-dir", debugDir, "--print-claim-handle-secret-once"))
	secret := stringField(t, debug, "claim_secret")
	if len(secret) != 32 || strings.Count(string(debug), secret) != 1 {
		t.Fatalf("debug=%s", debug)
	}
	if path := optionalStringField(t, debug, "handle_path"); path != "" {
		t.Fatalf("debug exposed path %q", path)
	}
	entries, err := os.ReadDir(debugDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("debug handles=%v, %v", entries, err)
	}
	s, err := store.Open(context.Background(), debugRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var state, hash string
	if err = s.DB().QueryRow(`SELECT state,secret_hash FROM handles WHERE workflow_id=? AND unit_id=?`, debugWF, debugUnit).Scan(&state, &hash); err != nil {
		t.Fatal(err)
	}
	if state != "revoked" || strings.Contains(hash, secret) {
		t.Fatalf("debug persisted state=%s hash=%s", state, hash)
	}
	if actions, _ := storedActivities(t, debugRoot, debugWF); actions[len(actions)-1] != "unit_claimed" {
		t.Fatalf("debug claim activities=%v", actions)
	}
}

func TestOutsidePlanCorrectionNamesDaimonAsNextCoordinator(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, handlePath := setupReviewingUnit(t, root, "implementer")
	reviewFile := writeInput(t, root, "outside-correction.json", `{"verdict":"corrections","summary":"plan change","findings":"split the unit","plan_impact":"outside"}`)
	correction := mustOK(t, runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", handlePath, "--input-file", reviewFile))
	var response struct {
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal(correction, &response); err != nil {
		t.Fatal(err)
	}
	if response.NextAction != "daimon revise plan" {
		t.Fatalf("next_action=%q", response.NextAction)
	}
}

func TestMasterRemainsAnOpaqueHistoricalActorLabel(t *testing.T) {
	root := t.TempDir()
	created := mustOK(t, runAt(t, root, "workflow", "new", "--name", "Preserve history", "--goal", "preserve history", "--actor", "master"))
	wfID, revision := workflowID(t, created), workflowRevision(t, created)
	input := writeInput(t, root, "historical-actor.json", `{"content":"historical evidence"}`)
	mustOK(t, runAt(t, root, "workflow", "explore", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "master", "--input-file", input))
	shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	var response struct {
		Data struct {
			Artifacts []struct {
				Actor string `json:"actor"`
			} `json:"artifacts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(shown, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Artifacts) != 1 || response.Data.Artifacts[0].Actor != "master" {
		t.Fatalf("artifacts=%#v", response.Data.Artifacts)
	}
}

func TestFailedUnitCommandsPreserveClaimAtomically(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string, string, string)
		args    func(*testing.T, string, string, string) []string
	}{
		{
			name: "tdd domain rejection",
			args: func(t *testing.T, root, wfID, unitID string) []string {
				input := writeInput(t, root, "second-tdd.json", `{"red_command":"test red","red_outcome":"exit 1","green_command":"test green","green_outcome":"exit 0","refactor_summary":"","validation_command":"test all","validation_outcome":"exit 0","changed_paths":"internal"}`)
				return []string{"workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--input-file", input}
			},
		},
		{
			name: "review domain rejection",
			prepare: func(t *testing.T, root, wfID, unitID string) {
				s, err := store.Open(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				if _, err = s.DB().Exec(`DELETE FROM evidence WHERE workflow_id=? AND unit_id=?`, wfID, unitID); err != nil {
					t.Fatal(err)
				}
			},
			args: func(t *testing.T, root, wfID, unitID string) []string {
				input := writeInput(t, root, "failed-review.json", `{"verdict":"approved","summary":"looks good","findings":""}`)
				return []string{"workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--input-file", input}
			},
		},
		{
			name: "completion without approved review",
			args: func(_ *testing.T, _ string, wfID, unitID string) []string {
				return []string{"workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			wfID, unitID, handlePath := setupReviewingUnit(t, root, "implementer")
			if tt.prepare != nil {
				tt.prepare(t, root, wfID, unitID)
			}
			beforeFile, err := os.ReadFile(handlePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeState, beforeExpiry := storedClaim(t, root, wfID, unitID)
			args := append(tt.args(t, root, wfID, unitID), "--claim-handle", handlePath)
			failed := runAtTime(t, root, time.Date(2026, 8, 20, 15, 1, 0, 0, time.UTC), args...)
			if failed.code != 3 {
				t.Fatalf("failed command=%#v", failed)
			}
			afterFile, err := os.ReadFile(handlePath)
			if err != nil {
				t.Fatal(err)
			}
			afterState, afterExpiry := storedClaim(t, root, wfID, unitID)
			if !bytes.Equal(afterFile, beforeFile) || afterState != beforeState || afterExpiry != beforeExpiry {
				t.Fatalf("failed command mutated claim: file_equal=%t state=%s->%s expiry=%s->%s", bytes.Equal(afterFile, beforeFile), beforeState, afterState, beforeExpiry, afterExpiry)
			}
		})
	}
}

func TestFalseTDDOutcomesAreRejectedWithoutMutation(t *testing.T) {
	tests := []struct {
		name, red, green, validation string
	}{
		{"red passed", "exit 0: unexpectedly passed", "exit 0", "exit 0"},
		{"green failed", "exit 1: expected failure", "exit 1: assertion failed", "exit 0"},
		{"validation failed", "exit 1", "exit 0: focused pass", "exit 2: package failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			wfID, unitID, _ := setupImplementingUnit(t, root)
			claim := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "handles")))
			handlePath := stringField(t, claim, "handle_path")
			beforeFile, err := os.ReadFile(handlePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeState, beforeExpiry := storedClaim(t, root, wfID, unitID)
			input := writeInput(t, root, "false-outcome.json", `{"red_command":"go test -run X","red_outcome":`+strconv.Quote(tt.red)+`,"green_command":"go test -run X","green_outcome":`+strconv.Quote(tt.green)+`,"refactor_summary":"","validation_command":"go test ./...","validation_outcome":`+strconv.Quote(tt.validation)+`,"changed_paths":"internal/feature"}`)
			failed := runAtTime(t, root, time.Date(2026, 8, 20, 15, 1, 0, 0, time.UTC), "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath, "--input-file", input)
			if failed.code != 3 || failed.stdout != "" {
				t.Fatalf("false outcome result=%#v", failed)
			}
			afterFile, err := os.ReadFile(handlePath)
			if err != nil {
				t.Fatal(err)
			}
			afterState, afterExpiry := storedClaim(t, root, wfID, unitID)
			unitState, evidenceCount := storedUnitEvidence(t, root, wfID, unitID)
			if !bytes.Equal(beforeFile, afterFile) || beforeState != afterState || beforeExpiry != afterExpiry || unitState != "pending" || evidenceCount != 0 {
				t.Fatalf("false outcome mutated state: file_equal=%t claim=%s/%s->%s/%s unit=%s evidence=%d", bytes.Equal(beforeFile, afterFile), beforeState, beforeExpiry, afterState, afterExpiry, unitState, evidenceCount)
			}
		})
	}
}

func TestPlanApprovalGatesReadinessClaimsAndPersistsExplicitExceptions(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	for _, stage := range []string{"explore", "spec", "design"} {
		input := writeInput(t, root, stage+".json", `{"content":"`+stage+`"}`)
		revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", stage, "--workflow-id", wfID, "--revision", itoa(revision), "--actor", stage, "--input-file", input)))
	}
	first := "wu-000000000000000000000001"
	second := "wu-000000000000000000000002"
	planBody := `{"summary":"approval authority","scope":"internal","max_parallel_units":2,"work_units":[` +
		`{"id":"` + first + `","description":"oversized","scope":"internal/shared","areas":["internal/shared"],"depends_on":[],"estimated_changed_lines":401,"estimated_review_minutes":20,"admission_exception":{"justification":"indivisible migration"}},` +
		`{"id":"` + second + `","description":"ordinary","scope":"internal/shared/child","areas":["internal/shared/child"],"depends_on":[],"estimated_changed_lines":100,"estimated_review_minutes":20,"admission_exception":{"justification":"submitted but unnecessary"}}],` +
		`"overlap_approvals":[{"unit_ids":["` + first + `","` + second + `"],"justification":"ordered shared schema"}]}`
	planFile := writeInput(t, root, "approval-plan.json", planBody)
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "planner", "--input-file", planFile)))

	readyBefore := runAt(t, root, "workflow", "list-ready-units", "--workflow-id", wfID)
	if readyBefore.code != 3 || readyBefore.stdout != "" {
		t.Fatalf("preapproval readiness=%#v", readyBefore)
	}
	handleDir := filepath.Join(root, "preapproval-handles")
	claimBefore := runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", first, "--revision", "1", "--actor", "implementer", "--handle-dir", handleDir)
	if claimBefore.code != 3 || claimBefore.stdout != "" {
		t.Fatalf("preapproval claim=%#v", claimBefore)
	}
	if _, err := os.Stat(handleDir); !os.IsNotExist(err) {
		t.Fatalf("preapproval claim created handle directory: %v", err)
	}
	missingApproval := runAt(t, root, "workflow", "approve-plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon")
	if missingApproval.code != 3 || missingApproval.stdout != "" {
		t.Fatalf("missing exception approval=%#v", missingApproval)
	}
	firstApproved, secondApproved := storedExceptionApprovals(t, root, wfID, first, second)
	if firstApproved || secondApproved {
		t.Fatalf("failed approval persisted markers first=%t second=%t", firstApproved, secondApproved)
	}
	shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	if state := workflowState(t, shown); state != "planning" {
		t.Fatalf("failed approval changed workflow state=%s", state)
	}

	approved := mustOK(t, runAt(t, root, "workflow", "approve-plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon", "--approve-exception", first))
	if state := workflowState(t, approved); state != "plan_approved" {
		t.Fatalf("approved state=%s", state)
	}
	firstApproved, secondApproved = storedExceptionApprovals(t, root, wfID, first, second)
	if !firstApproved || secondApproved {
		t.Fatalf("persisted approvals first=%t second=%t", firstApproved, secondApproved)
	}
	readyAfter := mustOK(t, runAt(t, root, "workflow", "list-ready-units", "--workflow-id", wfID))
	if !strings.Contains(string(readyAfter), first) || !strings.Contains(string(readyAfter), second) {
		t.Fatalf("approved readiness=%s", readyAfter)
	}
	claimAfter := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", first, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "approved-handles")))
	if path := stringField(t, claimAfter, "handle_path"); path == "" {
		t.Fatal("approved claim omitted handle path")
	}
}

func storedExceptionApprovals(t *testing.T, root, wfID, first, second string) (bool, bool) {
	t.Helper()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var firstApproved, secondApproved bool
	if err = s.DB().QueryRow(`SELECT admission_exception_approved FROM work_units WHERE workflow_id=? AND id=?`, wfID, first).Scan(&firstApproved); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT admission_exception_approved FROM work_units WHERE workflow_id=? AND id=?`, wfID, second).Scan(&secondApproved); err != nil {
		t.Fatal(err)
	}
	return firstApproved, secondApproved
}

func storedUnitEvidence(t *testing.T, root, wfID, unitID string) (string, int) {
	t.Helper()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var state string
	var evidenceCount int
	if err = s.DB().QueryRow(`SELECT state FROM work_units WHERE workflow_id=? AND id=?`, wfID, unitID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT count(*) FROM evidence WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&evidenceCount); err != nil {
		t.Fatal(err)
	}
	return state, evidenceCount
}

func runAtTime(t *testing.T, root string, now time.Time, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, Dependencies{Stdout: &stdout, Stderr: &stderr, ProjectRoot: root, Now: func() time.Time { return now }})
	return result{code, stdout.String(), stderr.String()}
}

func storedClaim(t *testing.T, root, wfID, unitID string) (string, string) {
	t.Helper()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var state, expiry string
	if err = s.DB().QueryRow(`SELECT state,expires_at FROM handles WHERE workflow_id=? AND unit_id=? ORDER BY claim_generation DESC LIMIT 1`, wfID, unitID).Scan(&state, &expiry); err != nil {
		t.Fatal(err)
	}
	return state, expiry
}

func createWorkflow(t *testing.T, root string) (string, int64) {
	t.Helper()
	doc := mustOK(t, runAt(t, root, "workflow", "new", "--name", "Ship", "--goal", "ship", "--actor", "daimon"))
	return workflowID(t, doc), workflowRevision(t, doc)
}

func setupImplementingUnit(t *testing.T, root string) (string, string, int64) {
	t.Helper()
	wfID, revision := createWorkflow(t, root)
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "begin-implementation", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon")))
	unitID := "wu-000000000000000000000001"
	// The trivial implementation path has no plan, so seed its single unit through the durable schema seam.
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.DB().Exec(`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,admission_exception,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, unitID, wfID, "unit", "internal", `[]`, `[]`, 1, 1, "pending", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	return wfID, unitID, revision
}

func setupReviewingUnit(t *testing.T, root, actor string) (string, string, string) {
	t.Helper()
	wfID, unitID, _ := setupImplementingUnit(t, root)
	claim := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", actor, "--handle-dir", filepath.Join(root, "handles")))
	handlePath := stringField(t, claim, "handle_path")
	tdd := writeInput(t, root, "tdd.json", `{"red_command":"test red","red_outcome":"exit 1","green_command":"test green","green_outcome":"exit 0","refactor_summary":"","validation_command":"test all","validation_outcome":"exit 0","changed_paths":"internal"}`)
	mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", actor, "--claim-handle", handlePath, "--input-file", tdd))
	return wfID, unitID, handlePath
}

func mustOK(t *testing.T, result result) []byte {
	t.Helper()
	if result.code != 0 || result.stderr != "" {
		t.Fatalf("result=%#v", result)
	}
	return []byte(result.stdout)
}

func workflowID(t *testing.T, document []byte) string { return nestedWorkflowString(t, document, "id") }
func workflowState(t *testing.T, document []byte) string {
	return nestedWorkflowString(t, document, "state")
}
func workflowRevision(t *testing.T, document []byte) int64 {
	t.Helper()
	var v struct {
		Data struct {
			Workflow struct {
				Revision int64 `json:"revision"`
			} `json:"workflow"`
		} `json:"data"`
	}
	if err := json.Unmarshal(document, &v); err != nil {
		t.Fatal(err)
	}
	return v.Data.Workflow.Revision
}
func nestedWorkflowString(t *testing.T, document []byte, field string) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(document, &v); err != nil {
		t.Fatal(err)
	}
	return v["data"].(map[string]any)["workflow"].(map[string]any)[field].(string)
}
func stringField(t *testing.T, document []byte, field string) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(document, &v); err != nil {
		t.Fatal(err)
	}
	return v["data"].(map[string]any)[field].(string)
}
func optionalStringField(t *testing.T, document []byte, field string) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(document, &v); err != nil {
		t.Fatal(err)
	}
	value, _ := v["data"].(map[string]any)[field].(string)
	return value
}
func itoa(value int64) string { return strconv.FormatInt(value, 10) }
