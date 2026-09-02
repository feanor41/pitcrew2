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

	"github.com/fmazzalomo/pitcrew/internal/handles"
	"github.com/fmazzalomo/pitcrew/internal/history"
	"github.com/fmazzalomo/pitcrew/internal/project"
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
	if err := os.MkdirAll(filepath.Join(root, "internal", "feature"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "feature", "change.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tddFile := writeInput(t, root, "tdd.json", `{"red_command":"go test -run X","red_outcome":"exit 1","green_command":"go test -run X","green_outcome":"exit 0","refactor_summary":"clean","validation_command":"go test ./...","validation_outcome":"exit 0","changed_paths":"internal/feature"}`)
	tddResult := mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath, "--input-file", tddFile))
	if !strings.Contains(string(tddResult), `"next_action":"workflow handoff-review"`) {
		t.Fatalf("TDD next action=%s", tddResult)
	}
	unitProjection := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID, "--view", "unit", "--unit-id", unitID))
	for _, want := range []string{`"change_baseline"`, `"accepted_budget":100`, `"additions":2`, `"deletions":0`, `"changed_lines":2`, `"base_revision"`, `"baseline_digest"`, `"result_digest"`} {
		if !strings.Contains(string(unitProjection), want) {
			t.Fatalf("unit projection omits %s: %s", want, unitProjection)
		}
	}
	if strings.Contains(string(unitProjection), root) || strings.Contains(string(unitProjection), handlePath) || strings.Contains(string(unitProjection), "claim_id") {
		t.Fatalf("unit projection leaked private authority: %s", unitProjection)
	}
	reviewHandlePath := handoffReview(t, root, wfID, unitID, "reviewer")
	reviewFile := writeInput(t, root, "review.json", `{"verdict":"approved","summary":"good","findings":""}`)
	mustOK(t, runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", reviewHandlePath, "--input-file", reviewFile))
	completed := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath))
	if _, err := os.Stat(handlePath); !os.IsNotExist(err) {
		t.Fatalf("completed handle remains: %v", err)
	}
	deadHandle := runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath)
	if deadHandle.code != 5 || deadHandle.stdout != "" {
		t.Fatalf("dead handle=%#v", deadHandle)
	}
	revision = workflowRevision(t, completed)
	aggregateReview := writeInput(t, root, "aggregate-review.json", `{"verdict":"approved","summary":"requirements satisfied","findings":""}`)
	final := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "aggregate-reviewer", "--input-file", aggregateReview))
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
			Records []struct {
				Kind, Content, Actor string
			} `json:"records"`
			Timeline []struct {
				Action string `json:"action"`
			} `json:"timeline"`
		} `json:"data"`
	}
	if err := json.Unmarshal(shown, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Data.Artifacts) != 4 || document.Data.Artifacts[0].Kind != "exploration" || document.Data.Artifacts[2].Revision != 4 || document.Data.Artifacts[3].Kind != "aggregate_review" {
		t.Fatalf("artifacts=%#v", document.Data.Artifacts)
	}
	kinds := make([]string, len(document.Data.Records))
	for i, record := range document.Data.Records {
		kinds[i] = record.Kind
	}
	if !strings.Contains(strings.Join(kinds, ","), "plan") || !strings.Contains(strings.Join(kinds, ","), "evidence") || !strings.Contains(strings.Join(kinds, ","), "review") || !strings.Contains(strings.Join(kinds, ","), "aggregate_review") || len(document.Data.Timeline) == 0 || strings.Contains(string(shown), "claim_secret") || strings.Contains(string(shown), handlePath) {
		t.Fatalf("review inspection incomplete or leaked: %s", shown)
	}
	wantActions := []string{"workflow_created", "exploration_recorded", "specification_recorded", "design_recorded", "plan_submitted", "plan_approved", "implementation_started", "unit_claimed", "unit_tdd_recorded", "unit_review_handed_off", "unit_review_recorded", "unit_completed", "aggregate_review_recorded", "workflow_completed"}
	wantSubjects := []string{wfID, "1", "2", "3", wfID, wfID, wfID + "@7", unitID, unitID + "@1", unitID, unitID + "@1", unitID, "4", wfID + "@9"}
	got, subjects := storedActivities(t, root, wfID)
	if strings.Join(got, ",") != strings.Join(wantActions, ",") || strings.Join(subjects, ",") != strings.Join(wantSubjects, ",") {
		t.Fatalf("activities=%v want=%v", got, wantActions)
	}
}

func TestReleaseUnitClaimRestoresReadyFlowWithoutLeakingAuthority(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, workflowRev := setupImplementingUnit(t, root)
	claim := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "handles")))
	handlePath := stringField(t, claim, "handle_path")

	released := mustOK(t, runAt(t, root, "workflow", "release-unit-claim",
		"--workflow-id", wfID, "--workflow-revision", itoa(workflowRev), "--unit-id", unitID, "--revision", "1",
		"--actor", "implementer", "--claim-handle", handlePath, "--reason", "reassign bounded work"))
	if !strings.Contains(string(released), `"workflow_revision":8`) || !strings.Contains(string(released), `"unit_revision":2`) ||
		!strings.Contains(string(released), `"unit_state":"pending"`) || !strings.Contains(string(released), `"next_action":"return to aion"`) {
		t.Fatalf("release=%s", released)
	}
	if strings.Contains(string(released), handlePath) || strings.Contains(string(released), "claim_id") || strings.Contains(string(released), "secret") {
		t.Fatalf("release leaked authority: %s", released)
	}
	ready := mustOK(t, runAt(t, root, "workflow", "list-ready-units", "--workflow-id", wfID))
	if !strings.Contains(string(ready), unitID) {
		t.Fatalf("released unit is not ready: %s", ready)
	}
	postRelease := fullBrief(t, root, "pc2-implementer", "--workflow-id", wfID, "--unit-id", unitID)
	postContext := postRelease["context"].(map[string]any)
	if postRelease["next_action"] != "return to aion" || len(stringSlice(postContext["allowed_actions"])) != 0 {
		t.Fatalf("released authority remained in brief: %#v", postRelease)
	}
	shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID, "--view", "audit"))
	if strings.Count(string(shown), `"Activity":"unit_claim_released"`) != 1 || strings.Contains(string(shown), handlePath) || strings.Contains(string(shown), "secret_hash") {
		t.Fatalf("release history is not singular and sanitized: %s", shown)
	}

	replay := runAt(t, root, "workflow", "release-unit-claim", "--workflow-id", wfID, "--workflow-revision", "8", "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--claim-handle", handlePath, "--reason", "replay")
	if replay.code != 5 || replay.stdout != "" {
		t.Fatalf("replay=%#v", replay)
	}
	second := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--handle-dir", filepath.Join(root, "handles-2")))
	if stringField(t, second, "handle_path") == "" {
		t.Fatal("reclaim omitted handle path")
	}
	// Brief projection uses its wall clock rather than the injected CLI test
	// clock, so keep this synthetic claim live for the authority assertion.
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`UPDATE handles SET expires_at='2099-08-31T10:00:00Z' WHERE workflow_id=? AND unit_id=? AND claim_generation=2`, wfID, unitID)
	_ = s.Close()
	if err != nil {
		t.Fatal(err)
	}
	claimed := fullBrief(t, root, "pc2-implementer", "--workflow-id", wfID, "--unit-id", unitID)
	allowed := strings.Join(stringSlice(claimed["context"].(map[string]any)["allowed_actions"]), ",")
	if claimed["next_action"] != "workflow unit-tdd" || allowed != "workflow unit-tdd,workflow release-unit-claim" {
		t.Fatalf("reclaimed authority=%#v", claimed)
	}
}

func TestUnitTDDRejectsOverBudgetChangesWithoutLeakingOrBypass(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, _ := setupImplementingUnit(t, root)
	claim := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "handles")))
	handlePath := stringField(t, claim, "handle_path")
	if err := os.MkdirAll(filepath.Join(root, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "over-budget.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := writeInput(t, root, "over-budget-tdd.json", `{"red_command":"red","red_outcome":"exit 1","green_command":"green","green_outcome":"exit 0","refactor_summary":"","validation_command":"all","validation_outcome":"exit 0","changed_paths":"internal"}`)
	failed := runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath, "--input-file", input)
	if failed.code != 3 || !strings.Contains(failed.stderr, "measured 2, accepted 1") || strings.Contains(failed.stderr, root) || strings.Contains(failed.stderr, handlePath) {
		t.Fatalf("over-budget failure=%#v", failed)
	}
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var evidenceRows, measurementRows int
	if err = s.DB().QueryRow(`SELECT count(*) FROM evidence WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&evidenceRows); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT count(*) FROM unit_change_measurements WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&measurementRows); err != nil {
		t.Fatal(err)
	}
	if evidenceRows != 0 || measurementRows != 0 {
		t.Fatalf("over-budget command persisted evidence=%d measurement=%d", evidenceRows, measurementRows)
	}
}

func TestReleasedReclaimRequiresRecoveryAfterRevocationOrExpiry(t *testing.T) {
	for _, mode := range []string{"explicit revoke", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			wfID, unitID, workflowRev := setupImplementingUnit(t, root)
			first := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "first")))
			mustOK(t, runAt(t, root, "workflow", "release-unit-claim",
				"--workflow-id", wfID, "--workflow-revision", itoa(workflowRev), "--unit-id", unitID, "--revision", "1",
				"--actor", "implementer", "--claim-handle", stringField(t, first, "handle_path"), "--reason", "reassign bounded work"))
			reclaimed := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--handle-dir", filepath.Join(root, "reclaimed")))
			reclaimedPath := stringField(t, reclaimed, "handle_path")

			switch mode {
			case "explicit revoke":
				s, err := store.Open(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				manager := handles.New(s, func() time.Time { return time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC) }, nil)
				if err = manager.Revoke(context.Background(), reclaimedPath, "implementer"); err != nil {
					s.Close()
					t.Fatal(err)
				}
				if err = s.Close(); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				tdd := writeInput(t, root, "expired-reclaim-tdd.json", `{"red_command":"red","red_outcome":"exit 1","green_command":"green","green_outcome":"exit 0","refactor_summary":"","validation_command":"all","validation_outcome":"exit 0","changed_paths":"internal"}`)
				expired := runAtTime(t, root, time.Date(2026, 8, 20, 15, 16, 0, 0, time.UTC), "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--claim-handle", reclaimedPath, "--input-file", tdd)
				if expired.code != 5 || expired.stdout != "" {
					t.Fatalf("expiry=%#v", expired)
				}
			}

			ordinary := runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--handle-dir", filepath.Join(root, "ordinary"))
			if ordinary.code != 3 || ordinary.stdout != "" {
				t.Fatalf("ordinary reclaim after %s=%#v", mode, ordinary)
			}
			shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID, "--view", "coordination"))
			var projection struct {
				Data history.Projection `json:"data"`
			}
			if err := json.Unmarshal(shown, &projection); err != nil {
				t.Fatal(err)
			}
			current := projection.Data.Coordination.Current
			if current == nil || current.ID != unitID || current.Status != "Recovery" || len(projection.Data.Coordination.Ready) != 0 {
				t.Fatalf("coordination after %s=%s", mode, shown)
			}
			brief := fullBrief(t, root, "pc2-implementer", "--workflow-id", wfID, "--unit-id", unitID)
			if brief["next_action"] != "workflow recover-unit-claim" || strings.Join(stringSlice(brief["context"].(map[string]any)["allowed_actions"]), ",") != "workflow recover-unit-claim" {
				t.Fatalf("brief after %s=%#v", mode, brief)
			}
			recovered := mustOK(t, runAt(t, root, "workflow", "recover-unit-claim", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--handle-dir", filepath.Join(root, "recovery")))
			if stringField(t, recovered, "handle_path") == "" {
				t.Fatalf("recovery after %s omitted handle path", mode)
			}
		})
	}
}

func TestDeliveryCommandsTraceEachRouteExactlyOnce(t *testing.T) {
	root := t.TempDir()
	start := writeInput(t, root, "delivery-start.json", `{"operation_key":"request-17","route":"direct_inline","goal":"Fix the release note","route_reason":"one bounded file"}`)
	first := mustOK(t, runAt(t, root, "delivery", "start", "--actor", "aion", "--input-file", start))
	traceID := deliveryID(t, first)
	if !strings.HasPrefix(traceID, "dl-") || !strings.Contains(string(first), `"status":"in_progress"`) || !strings.Contains(string(first), `"next_action":"delivery show"`) {
		t.Fatalf("start=%s", first)
	}
	replayed := mustOK(t, runAt(t, root, "delivery", "start", "--actor", "aion", "--input-file", start))
	if deliveryID(t, replayed) != traceID {
		t.Fatalf("idempotent replay created another identity: %s", replayed)
	}

	update := writeInput(t, root, "delivery-update.json", `{"status":"blocked","summary":"waiting for operator","next_action":"answer question"}`)
	updated := mustOK(t, runAt(t, root, "delivery", "update", "--delivery-id", traceID, "--revision", "1", "--actor", "aion", "--input-file", update))
	if !strings.Contains(string(updated), `"revision":2`) || !strings.Contains(string(updated), `"next_action":"answer question"`) {
		t.Fatalf("update=%s", updated)
	}
	shown := mustOK(t, runAt(t, root, "delivery", "show", "--delivery-id", traceID))
	if !strings.Contains(string(shown), `"route":"direct_inline"`) || !strings.Contains(string(shown), `"summary":"waiting for operator"`) {
		t.Fatalf("show=%s", shown)
	}
	results := mustOK(t, runAt(t, root, "delivery", "search", "--query", "operator"))
	if ids := deliverySearchIDs(t, results); len(ids) != 1 || ids[0] != traceID {
		t.Fatalf("search did not return one identity: %s", results)
	}

	wf := mustOK(t, runAt(t, root, "workflow", "new", "--name", "Complex", "--goal", "Migrate durable state", "--actor", "aion"))
	wfID := workflowID(t, wf)
	wfShown := mustOK(t, runAt(t, root, "delivery", "show", "--delivery-id", wfID))
	if !strings.Contains(string(wfShown), `"route":"full_workflow"`) || !strings.Contains(string(wfShown), `"workflow":`) {
		t.Fatalf("workflow delivery=%s", wfShown)
	}
	fullResults := mustOK(t, runAt(t, root, "delivery", "search", "--query", "full_workflow"))
	if ids := deliverySearchIDs(t, fullResults); len(ids) != 1 || ids[0] != wfID {
		t.Fatalf("workflow search duplicated identity: %s", fullResults)
	}

	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var direct, workflows int
	if err = s.DB().QueryRow(`SELECT count(*) FROM direct_delivery_traces`).Scan(&direct); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT count(*) FROM workflows`).Scan(&workflows); err != nil {
		t.Fatal(err)
	}
	if direct != 1 || workflows != 1 {
		t.Fatalf("direct=%d workflows=%d", direct, workflows)
	}
}

func TestDeliveryActiveReportsZeroOneAndAmbiguousCandidatesWithoutSelecting(t *testing.T) {
	emptyRoot := t.TempDir()
	empty := mustOK(t, runAt(t, emptyRoot, "delivery", "active"))
	if _, err := os.Stat(filepath.Join(emptyRoot, ".pitcrew")); !os.IsNotExist(err) {
		t.Fatalf("active discovery initialized project state: %v", err)
	}
	var emptyDocument struct {
		Data struct {
			Deliveries []history.Delivery `json:"deliveries"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal(empty, &emptyDocument); err != nil {
		t.Fatal(err)
	}
	if emptyDocument.Data.Deliveries == nil || len(emptyDocument.Data.Deliveries) != 0 || emptyDocument.NextAction != "aion admit new delivery" {
		t.Fatalf("empty active discovery = %s", empty)
	}

	root := t.TempDir()
	start := writeInput(t, root, "active-start.json", `{"operation_key":"active-request","route":"direct_inline","goal":"Retain exact identity","route_reason":"bounded work"}`)
	started := mustOK(t, runAt(t, root, "delivery", "start", "--actor", "aion", "--input-file", start))
	directID := deliveryID(t, started)
	one := mustOK(t, runAt(t, root, "delivery", "active"))
	var oneDocument struct {
		Data struct {
			Deliveries []history.Delivery `json:"deliveries"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal(one, &oneDocument); err != nil {
		t.Fatal(err)
	}
	if len(oneDocument.Data.Deliveries) != 1 || oneDocument.Data.Deliveries[0].ID != directID || oneDocument.Data.Deliveries[0].Revision != 1 || oneDocument.NextAction != "delivery show --delivery-id "+directID {
		t.Fatalf("single active discovery = %s", one)
	}
	if repeated := mustOK(t, runAt(t, root, "delivery", "active")); !bytes.Equal(one, repeated) {
		t.Fatalf("repeat active discovery differs:\nfirst=%s\nsecond=%s", one, repeated)
	}

	created := mustOK(t, runAt(t, root, "workflow", "new", "--name", "Second candidate", "--goal", "Require explicit selection", "--actor", "aion"))
	workflowID := workflowID(t, created)
	many := mustOK(t, runAt(t, root, "delivery", "active"))
	var manyDocument struct {
		Data struct {
			Deliveries []history.Delivery `json:"deliveries"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal(many, &manyDocument); err != nil {
		t.Fatal(err)
	}
	if len(manyDocument.Data.Deliveries) != 2 || manyDocument.NextAction != "aion clarify delivery identity" {
		t.Fatalf("ambiguous active discovery = %s", many)
	}
	ids := map[string]bool{}
	for _, candidate := range manyDocument.Data.Deliveries {
		ids[candidate.ID] = true
	}
	if !ids[directID] || !ids[workflowID] {
		t.Fatalf("ambiguous candidates lost an identity: %s", many)
	}
	explicit := mustOK(t, runAt(t, root, "delivery", "show", "--delivery-id", workflowID))
	if !strings.Contains(string(explicit), `"id":"`+workflowID+`"`) || strings.Contains(string(explicit), `"id":"`+directID+`"`) {
		t.Fatalf("explicit inspection selected wrong identity: %s", explicit)
	}
	activeAfterShow := mustOK(t, runAt(t, root, "delivery", "active"))
	if !bytes.Equal(many, activeAfterShow) {
		t.Fatalf("read-only selection flow mutated candidates:\nbefore=%s\nafter=%s", many, activeAfterShow)
	}
}

func TestDeliveryActiveSharesCentralStateAcrossLinkedWorktreesAndIsolatesClones(t *testing.T) {
	root := t.TempDir()
	main, linked, clone := filepath.Join(root, "main"), filepath.Join(root, "linked"), filepath.Join(root, "clone")
	admin := filepath.Join(main, ".git", "worktrees", "linked")
	for _, directory := range []string{admin, linked, filepath.Join(clone, ".git")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(linked, ".git"):     "gitdir: " + admin + "\n",
		filepath.Join(admin, "commondir"): "../..\n",
		filepath.Join(admin, "gitdir"):    filepath.Join(linked, ".git") + "\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	dataHome := filepath.Join(root, "data")
	start := writeInput(t, root, "central-active-start.json", `{"operation_key":"central-active","route":"direct_inline","goal":"Share active state","route_reason":"linked worktree proof"}`)
	created := mustOK(t, runCentral(t, main, dataHome, "delivery", "start", "--actor", "aion", "--input-file", start))
	deliveryID := deliveryID(t, created)
	mainActive := mustOK(t, runCentral(t, main, dataHome, "delivery", "active"))
	linkedActive := mustOK(t, runCentral(t, linked, dataHome, "delivery", "active"))
	if !bytes.Equal(mainActive, linkedActive) {
		t.Fatalf("linked worktree active view differs:\nmain=%s\nlinked=%s", mainActive, linkedActive)
	}
	var activeDocument struct {
		Data struct {
			Deliveries []history.Delivery `json:"deliveries"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal(linkedActive, &activeDocument); err != nil {
		t.Fatal(err)
	}
	if len(activeDocument.Data.Deliveries) != 1 || activeDocument.Data.Deliveries[0].ID != deliveryID || activeDocument.NextAction != "delivery show --delivery-id "+deliveryID {
		t.Fatalf("linked active cardinality = %s", linkedActive)
	}

	cloneInspection, err := project.Inspect(clone, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	if cloneInspection.Project.ID == "" || cloneInspection.Project.ID == projectIDFor(t, main) {
		t.Fatalf("independent clone did not receive an isolated identity: %#v", cloneInspection.Project)
	}
	if _, err := os.Stat(cloneInspection.Paths.ProjectRoot); !os.IsNotExist(err) {
		t.Fatalf("clone central state existed before active discovery: %v", err)
	}
	cloneActive := mustOK(t, runCentral(t, clone, dataHome, "delivery", "active"))
	if !strings.Contains(string(cloneActive), `"deliveries":[]`) || !strings.Contains(string(cloneActive), `"next_action":"aion admit new delivery"`) {
		t.Fatalf("independent clone leaked active state: %s", cloneActive)
	}
	if _, err := os.Stat(cloneInspection.Paths.ProjectRoot); !os.IsNotExist(err) {
		t.Fatalf("clone active discovery created central state: %v", err)
	}
	for _, checkout := range []string{linked, clone} {
		if _, err := os.Stat(filepath.Join(checkout, ".pitcrew")); !os.IsNotExist(err) {
			t.Fatalf("active discovery created checkout-local state in %s: %v", checkout, err)
		}
	}
	if afterClone := mustOK(t, runCentral(t, main, dataHome, "delivery", "active")); !bytes.Equal(mainActive, afterClone) {
		t.Fatalf("clone discovery changed canonical active state:\nbefore=%s\nafter=%s", mainActive, afterClone)
	}
}

func projectIDFor(t *testing.T, root string) string {
	t.Helper()
	resolved, err := project.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved.ID
}

func TestDeliveryUpdateRejectsStaleNoOpTerminalAndInvalidWithoutMutation(t *testing.T) {
	root := t.TempDir()
	invalidStart := writeInput(t, root, "invalid-start.json", `{"operation_key":"request-18","route":"full_workflow","goal":"Update generated prompts","route_reason":"wrong physical route"}`)
	if result := runAt(t, root, "delivery", "start", "--actor", "aion", "--input-file", invalidStart); result.code != 3 || result.stdout != "" {
		t.Fatalf("invalid start=%#v", result)
	}
	start := writeInput(t, root, "start.json", `{"operation_key":"request-18","route":"delegated_direct","goal":"Update generated prompts","route_reason":"several mechanical files"}`)
	created := mustOK(t, runAt(t, root, "delivery", "start", "--actor", "aion", "--input-file", start))
	id := deliveryID(t, created)
	noOp := writeInput(t, root, "noop.json", `{"status":"in_progress","summary":"","next_action":""}`)
	if result := runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "1", "--actor", "aion", "--input-file", noOp); result.code != 3 || result.stdout != "" {
		t.Fatalf("no-op=%#v", result)
	}
	invalid := writeInput(t, root, "invalid.json", `{"status":"invented","summary":"no","next_action":"no"}`)
	if result := runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "1", "--actor", "aion", "--input-file", invalid); result.code != 3 || result.stdout != "" {
		t.Fatalf("invalid=%#v", result)
	}
	blocked := writeInput(t, root, "blocked.json", `{"status":"blocked","summary":"dependency unavailable","next_action":"inspect dependency"}`)
	mustOK(t, runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "1", "--actor", "aion", "--input-file", blocked))
	if result := runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "1", "--actor", "aion", "--input-file", blocked); result.code != 4 || result.stdout != "" || !strings.Contains(result.stderr, "delivery revision mismatch for "+id) || strings.Contains(result.stderr, "workflow revision mismatch") || !strings.Contains(result.stderr, "pitcrew delivery show --delivery-id "+id) {
		t.Fatalf("stale=%#v", result)
	}
	completed := writeInput(t, root, "completed.json", `{"status":"completed","summary":"verified","next_action":"none"}`)
	mustOK(t, runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "2", "--actor", "aion", "--input-file", completed))
	before := mustOK(t, runAt(t, root, "delivery", "show", "--delivery-id", id))
	if result := runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "3", "--actor", "aion", "--input-file", blocked); result.code != 3 || result.stdout != "" {
		t.Fatalf("terminal=%#v", result)
	}
	after := mustOK(t, runAt(t, root, "delivery", "show", "--delivery-id", id))
	if string(before) != string(after) {
		t.Fatalf("rejected mutations changed trace:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestWorkflowContinueCreatesInspectableSuccessorAndPreservesSource(t *testing.T) {
	for _, tt := range []struct {
		state    string
		revision int64
	}{{"abandoned", 2}, {"completed", 9}} {
		t.Run(tt.state, func(t *testing.T) {
			root := t.TempDir()
			created := mustOK(t, runAt(t, root, "workflow", "new", "--name", "Ship", "--goal", "exact goal", "--actor", "daimon"))
			sourceID := workflowID(t, created)
			if tt.state == "abandoned" {
				mustOK(t, runAt(t, root, "workflow", "abandon", "--workflow-id", sourceID, "--revision", "1", "--actor", "daimon", "--reason", "continue separately"))
			} else {
				s, err := store.Open(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.DB().Exec(`UPDATE workflows SET state='completed',revision=? WHERE id=?`, tt.revision, sourceID)
				_ = s.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			before := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", sourceID))
			continued := mustOK(t, runAt(t, root, "workflow", "continue", "--from", sourceID, "--actor", "daimon"))
			var response struct {
				Data struct {
					Workflow struct {
						ID, State, Name, Goal string
						Revision              int64
					} `json:"workflow"`
					Predecessor struct {
						ID, State string
						Revision  int64
					} `json:"predecessor"`
				} `json:"data"`
				NextAction string `json:"next_action"`
			}
			if err := json.Unmarshal(continued, &response); err != nil {
				t.Fatal(err)
			}
			if response.Data.Workflow.ID == sourceID || response.Data.Workflow.State != "draft" || response.Data.Workflow.Revision != 1 || response.Data.Workflow.Name != "Ship" || response.Data.Workflow.Goal != "exact goal" || response.Data.Predecessor.ID != sourceID || response.Data.Predecessor.State != tt.state || response.Data.Predecessor.Revision != tt.revision || response.NextAction != "workflow explore" {
				t.Fatalf("continue=%s", continued)
			}
			shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", response.Data.Workflow.ID))
			if !strings.Contains(string(shown), `"kind":"continuation"`) || !strings.Contains(string(shown), `"action":"continuation_recorded"`) || !strings.Contains(string(shown), sourceID) || !strings.Contains(string(shown), `\"predecessor_state\":\"`+tt.state+`\"`) {
				t.Fatalf("successor show=%s", shown)
			}
			if after := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", sourceID)); string(after) != string(before) {
				t.Fatalf("source changed:\nbefore=%s\nafter=%s", before, after)
			}
		})
	}
}

func TestWorkflowContinueRejectsMalformedSourceBeforeOpeningStore(t *testing.T) {
	root := t.TempDir()
	result := runAt(t, root, "workflow", "continue", "--from", "wf-bad", "--actor", "daimon")
	if result.code != 2 || result.stdout != "" {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, ".pitcrew")); !os.IsNotExist(err) {
		t.Fatalf("store opened: %v", err)
	}
}

func TestWorkflowProgressAppendsCanonicalReportsWithoutTransition(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	before := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	for i, body := range []string{`{"status":"advanced","summary":"  unit done  ","next_action":"  test  "}`, `{"status":"blocked","summary":" waiting ","next_action":" unblock "}`} {
		input := writeInput(t, root, "progress-"+itoa(int64(i))+".json", body)
		result := mustOK(t, runAt(t, root, "workflow", "progress", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon", "--input-file", input))
		if !strings.Contains(string(result), `"progress":`) || !strings.Contains(string(result), `"next_action":"`+[]string{"test", "unblock"}[i]+`"`) {
			t.Fatalf("progress=%s", result)
		}
	}
	after := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	advanced, blocked := strings.Index(string(after), `\"summary\":\"unit done\"`), strings.Index(string(after), `\"summary\":\"waiting\"`)
	if workflowRevision(t, after) != revision || workflowState(t, after) != workflowState(t, before) || advanced < 0 || blocked < advanced || !strings.Contains(string(after), `"action":"progress_recorded"`) {
		t.Fatalf("show=%s", after)
	}
}

func TestWorkflowProgressRejectsInvalidStaleAndTerminalWithoutMutation(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	for _, tt := range []struct {
		name, body string
		code       int
	}{
		{"unknown field", `{"status":"advanced","summary":"done","next_action":"test","extra":true}`, 2},
		{"unknown status", `{"status":"moving","summary":"done","next_action":"test"}`, 3},
		{"padded status", `{"status":" advanced ","summary":"done","next_action":"test"}`, 3},
		{"blank summary", `{"status":"advanced","summary":" ","next_action":"test"}`, 3},
		{"missing next action", `{"status":"advanced","summary":"done"}`, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
			input := writeInput(t, root, strings.ReplaceAll(tt.name, " ", "-")+".json", tt.body)
			result := runAt(t, root, "workflow", "progress", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon", "--input-file", input)
			if result.code != tt.code || result.stdout != "" {
				t.Fatalf("result=%#v", result)
			}
			if after := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID)); string(after) != string(before) {
				t.Fatalf("invalid progress mutated workflow")
			}
		})
	}
	valid := writeInput(t, root, "valid-progress.json", `{"status":"advanced","summary":"done","next_action":"test"}`)
	if stale := runAt(t, root, "workflow", "progress", "--workflow-id", wfID, "--revision", "2", "--actor", "daimon", "--input-file", valid); stale.code != 4 {
		t.Fatalf("stale=%#v", stale)
	}
	mustOK(t, runAt(t, root, "workflow", "abandon", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon", "--reason", "stop"))
	if terminal := runAt(t, root, "workflow", "progress", "--workflow-id", wfID, "--revision", "2", "--actor", "daimon", "--input-file", valid); terminal.code != 3 {
		t.Fatalf("terminal=%#v", terminal)
	}
}

func TestWorkflowRequestCapabilityAppendsInspectableRequestsWithoutLifecycle(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	before := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	for i, body := range []string{`{"capability":"  browser tool  ","reason":"  verify UI  ","blocked_action":"  inspect page  "}`, `{"capability":"review transition","reason":"independent review","blocked_action":"handoff"}`} {
		input := writeInput(t, root, "capability-"+itoa(int64(i))+".json", body)
		result := mustOK(t, runAt(t, root, "workflow", "request-capability", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon", "--input-file", input))
		if !strings.Contains(string(result), `"capability_request":`) || !strings.Contains(string(result), `"next_action":"aion coordinate requested capability"`) {
			t.Fatalf("request=%s", result)
		}
	}
	after := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	first, second := strings.Index(string(after), `\"capability\":\"browser tool\"`), strings.Index(string(after), `\"capability\":\"review transition\"`)
	if workflowRevision(t, after) != revision || workflowState(t, after) != workflowState(t, before) || nestedWorkflowString(t, after, "updated_at") != nestedWorkflowString(t, before, "updated_at") || first < 0 || second < first || !strings.Contains(string(after), `"kind":"capability_request"`) || !strings.Contains(string(after), `"action":"capability_requested"`) {
		t.Fatalf("show=%s", after)
	}
	for _, forbidden := range []string{"claim_secret", `\"fulfilled\"`, `\"resolved\"`, `\"owner\"`, `\"status\"`} {
		if strings.Contains(string(after), forbidden) {
			t.Fatalf("capability request inferred lifecycle or leaked secret %q: %s", forbidden, after)
		}
	}
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	results, err := history.New(s).Search(context.Background(), "browser tool")
	if err != nil || len(results) == 0 || results[0].Kind != "capability_request" {
		t.Fatalf("capability search=%#v err=%v", results, err)
	}
	resolved, err := history.New(s).Resolve(context.Background(), results[0])
	if err != nil || resolved.Record.Kind != "capability_request" || strings.Contains(resolved.Record.Content, "secret") {
		t.Fatalf("capability drill-down=%#v err=%v", resolved, err)
	}
}

func TestWorkflowRequestCapabilityRejectsInvalidStaleAndTerminalWithoutMutation(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	for _, tt := range []struct {
		name, body string
		code       int
	}{
		{"unknown field", `{"capability":"tool","reason":"needed","blocked_action":"work","extra":true}`, 2},
		{"missing capability", `{"reason":"needed","blocked_action":"work"}`, 3},
		{"blank capability", `{"capability":" ","reason":"needed","blocked_action":"work"}`, 3},
		{"blank reason", `{"capability":"tool","reason":" ","blocked_action":"work"}`, 3},
		{"blank blocked action", `{"capability":"tool","reason":"needed","blocked_action":" "}`, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
			input := writeInput(t, root, strings.ReplaceAll(tt.name, " ", "-")+".json", tt.body)
			result := runAt(t, root, "workflow", "request-capability", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon", "--input-file", input)
			if result.code != tt.code || result.stdout != "" {
				t.Fatalf("result=%#v", result)
			}
			if after := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID)); string(after) != string(before) {
				t.Fatal("invalid capability request mutated workflow")
			}
		})
	}
	valid := writeInput(t, root, "valid-capability.json", `{"capability":"tool","reason":"needed","blocked_action":"work"}`)
	if stale := runAt(t, root, "workflow", "request-capability", "--workflow-id", wfID, "--revision", "2", "--actor", "daimon", "--input-file", valid); stale.code != 4 {
		t.Fatalf("stale=%#v", stale)
	}
	mustOK(t, runAt(t, root, "workflow", "abandon", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon", "--reason", "stop"))
	if terminal := runAt(t, root, "workflow", "request-capability", "--workflow-id", wfID, "--revision", "2", "--actor", "daimon", "--input-file", valid); terminal.code != 3 {
		t.Fatalf("terminal=%#v", terminal)
	}
}

func TestReviewHandoffIssuesIndependentOpaqueAuthority(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, implementationPath := setupReviewingUnit(t, root, "implementer")
	handoff := mustOK(t, runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", filepath.Join(root, "review-handles")))
	reviewPath := stringField(t, handoff, "handle_path")
	if reviewPath == implementationPath || !strings.Contains(string(handoff), `"next_action":"workflow unit-review"`) {
		t.Fatalf("handoff=%s", handoff)
	}
	data, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "reviewer") || strings.Contains(string(data), "implementer") {
		t.Fatalf("handle leaked actor identity: %s", data)
	}
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var implementationState, reviewState string
	if err = s.DB().QueryRow(`SELECT state FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='implementation'`, wfID, unitID).Scan(&implementationState); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT state FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='review'`, wfID, unitID).Scan(&reviewState); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if implementationState != "active" || reviewState != "intent" {
		t.Fatalf("implementation=%s review=%s", implementationState, reviewState)
	}
	if duplicate := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", filepath.Join(root, "duplicate")); duplicate.code != 3 {
		t.Fatalf("duplicate=%#v", duplicate)
	}
	reviewFile := writeInput(t, root, "handoff-review.json", `{"verdict":"approved","summary":"good","findings":""}`)
	if wrong := runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", implementationPath, "--input-file", reviewFile); wrong.code != 5 {
		t.Fatalf("implementation authority reviewed unit: %#v", wrong)
	}
	mustOK(t, runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", reviewPath, "--input-file", reviewFile))
	if _, err := os.Stat(reviewPath); !os.IsNotExist(err) {
		t.Fatalf("consumed review handle remains: %v", err)
	}
	if _, err := os.Stat(implementationPath); err != nil {
		t.Fatalf("review consumed implementation authority: %v", err)
	}
	actions, _ := storedActivities(t, root, wfID)
	if !strings.Contains(strings.Join(actions, ","), "unit_review_handed_off,unit_review_recorded") {
		t.Fatalf("activities=%v", actions)
	}
}

func TestReviewHandoffRejectsInvalidAuthorityWithoutMutation(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, _ := setupReviewingUnit(t, root, "implementer")
	for name, test := range map[string]struct {
		revision, actor string
		want            int
	}{
		"stale revision": {"2", "reviewer", 4},
		"same actor":     {"1", "implementer", 3},
	} {
		dir := filepath.Join(root, strings.ReplaceAll(name, " ", "-"))
		failed := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", test.revision, "--actor", test.actor, "--handle-dir", dir)
		if failed.code != test.want {
			t.Fatalf("%s=%#v", name, failed)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s created handle directory: %v", name, err)
		}
	}
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`DELETE FROM evidence WHERE workflow_id=? AND unit_id=?`, wfID, unitID); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if failed := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", filepath.Join(root, "missing-evidence")); failed.code != 3 {
		t.Fatalf("missing evidence=%#v", failed)
	}
	root = t.TempDir()
	wfID, unitID, _ = setupReviewingUnit(t, root, "implementer")
	expiringPath := handoffReview(t, root, wfID, unitID, "reviewer")
	reviewInput := writeInput(t, root, "expired-review.json", `{"verdict":"approved","summary":"good","findings":""}`)
	if expired := runAtTime(t, root, time.Date(2026, 8, 20, 15, 16, 0, 0, time.UTC), "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", expiringPath, "--input-file", reviewInput); expired.code != 5 {
		t.Fatalf("expired review=%#v", expired)
	}
	if replacement := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "other-reviewer", "--handle-dir", filepath.Join(root, "forbidden-replacement")); replacement.code != 3 {
		t.Fatalf("handoff replaced expired current authority=%#v", replacement)
	}
	root = t.TempDir()
	wfID, unitID, _ = setupImplementingUnit(t, root)
	if failed := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", filepath.Join(root, "pending")); failed.code != 3 {
		t.Fatalf("pending unit=%#v", failed)
	}
	root = t.TempDir()
	wfID, unitID, _ = setupReviewingUnit(t, root, "implementer")
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	unsafeDir := filepath.Join(root, "unsafe")
	if err := os.Symlink(realDir, unsafeDir); err != nil {
		t.Fatal(err)
	}
	if failed := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", unsafeDir); failed.code != 5 {
		t.Fatalf("unsafe directory=%#v", failed)
	}
	s, err = store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE workflows SET state='completed' WHERE id=?`, wfID); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if failed := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", filepath.Join(root, "terminal")); failed.code != 3 {
		t.Fatalf("terminal workflow=%#v", failed)
	}
}

func TestRecoverReviewRotatesOnlyReviewAuthorityAndRestoresCompletion(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, implementationPath := setupReviewingUnit(t, root, "implementer")
	reviewPath := handoffReview(t, root, wfID, unitID, "reviewer")
	args := []string{"workflow", "recover-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", filepath.Join(root, "recovered-review")}
	if live := runAt(t, root, args...); live.code != 3 {
		t.Fatalf("live recovery=%#v", live)
	}
	stale := append([]string{}, args...)
	stale[7] = "2"
	if failed := runAt(t, root, stale...); failed.code != 4 {
		t.Fatalf("stale recovery=%#v", failed)
	}
	reviewInput := writeInput(t, root, "recovery-review.json", `{"verdict":"approved","summary":"good","findings":""}`)
	now := time.Date(2026, 8, 20, 15, 16, 0, 0, time.UTC)
	wrongActor := append([]string{}, args...)
	wrongActor[9] = "other-reviewer"
	if wrong := runAtTime(t, root, now, wrongActor...); wrong.code != 3 {
		t.Fatalf("wrong actor recovery=%#v", wrong)
	}
	recovered := mustOK(t, runAtTime(t, root, now, args...))
	recoveredReviewPath := stringField(t, recovered, "handle_path")
	if recoveredReviewPath == reviewPath || strings.Contains(string(recovered), "claim_secret") {
		t.Fatalf("review recovery=%s", recovered)
	}
	if staleHandle := runAtTime(t, root, now, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", reviewPath, "--input-file", reviewInput); staleHandle.code != 5 {
		t.Fatalf("stale review authority=%#v", staleHandle)
	}
	if duplicate := runAtTime(t, root, now, args...); duplicate.code != 3 {
		t.Fatalf("duplicate recovery=%#v", duplicate)
	}
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var implementationGeneration, reviewGeneration int
	_ = s.DB().QueryRow(`SELECT max(claim_generation) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='implementation'`, wfID, unitID).Scan(&implementationGeneration)
	_ = s.DB().QueryRow(`SELECT max(claim_generation) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='review'`, wfID, unitID).Scan(&reviewGeneration)
	_ = s.Close()
	if implementationGeneration != 1 || reviewGeneration != 2 {
		t.Fatalf("implementation generation=%d review generation=%d", implementationGeneration, reviewGeneration)
	}
	mustOK(t, runAtTime(t, root, now, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", recoveredReviewPath, "--input-file", reviewInput))
	if verdict := runAtTime(t, root, now, args...); verdict.code != 3 {
		t.Fatalf("verdict recovery=%#v", verdict)
	}
	if wrong := runAtTime(t, root, now, "workflow", "recover-unit-claim", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "other-implementer", "--handle-dir", filepath.Join(root, "wrong-implementation")); wrong.code != 3 {
		t.Fatalf("wrong implementation recovery=%#v", wrong)
	}
	implementation := mustOK(t, runAtTime(t, root, now, "workflow", "recover-unit-claim", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "recovered-implementation")))
	if stringField(t, implementation, "handle_path") == implementationPath {
		t.Fatal("implementation recovery reused expired authority")
	}
	mustOK(t, runAtTime(t, root, now, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", stringField(t, implementation, "handle_path")))
	if terminal := runAtTime(t, root, now, args...); terminal.code != 3 {
		t.Fatalf("terminal recovery=%#v", terminal)
	}
	actions, _ := storedActivities(t, root, wfID)
	if !strings.Contains(strings.Join(actions, ","), "unit_review_recovered,unit_review_recorded") {
		t.Fatalf("activities=%v", actions)
	}
	pendingRoot := t.TempDir()
	pendingWF, pendingUnit, _ := setupImplementingUnit(t, pendingRoot)
	if pending := runAt(t, pendingRoot, "workflow", "recover-review", "--workflow-id", pendingWF, "--unit-id", pendingUnit, "--revision", "1", "--actor", "reviewer", "--handle-dir", filepath.Join(pendingRoot, "review")); pending.code != 3 {
		t.Fatalf("pending recovery=%#v", pending)
	}
}

func TestAmendPlanRejectsDeclarativeActorsWithoutStructuralAuthority(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	for _, stage := range []string{"explore", "spec", "design"} {
		input := writeInput(t, root, stage+"-amendment-setup.json", `{"content":"`+stage+`"}`)
		revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", stage, "--workflow-id", wfID, "--revision", itoa(revision), "--actor", stage, "--input-file", input)))
	}
	originalUnit := "wu-000000000000000000000001"
	missingUnit := "wu-000000000000000000000002"
	originalPlan := writeInput(t, root, "original-plan.json", `{"summary":"validated scope","scope":"internal","max_parallel_units":1,"work_units":[{"id":"`+originalUnit+`","description":"original unit","scope":"internal/original","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}]}`)
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "planner", "--input-file", originalPlan)))
	amendedPlan := writeInput(t, root, "validated-missing-unit-plan.json", `{"summary":"validated scope","scope":"internal","max_parallel_units":2,"work_units":[{"id":"`+originalUnit+`","description":"original unit","scope":"internal/original","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"`+missingUnit+`","description":"validated missing unit","scope":"internal/missing","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}]}`)
	before := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	for _, actor := range []string{"aion", "external-planner"} {
		if result := runAt(t, root, "workflow", "amend-plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", actor, "--input-file", amendedPlan); result.code != 3 || !strings.Contains(result.stderr, "structural plan amendment authority") {
			t.Fatalf("actor %q authorized amendment or got the wrong rejection: %#v", actor, result)
		}
	}
	after := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	if workflowRevision(t, after) != workflowRevision(t, before) || !strings.Contains(string(after), originalUnit) || strings.Contains(string(after), missingUnit) || strings.Contains(string(after), `"kind":"plan_superseded"`) {
		t.Fatalf("unauthorized amendment mutated the plan: %s", after)
	}
}

func TestRecoverAggregateAllowsFreshTDDForDoneUnitAfterCorrections(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, implementationHandle := setupReviewingUnit(t, root, "implementer")
	completed := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", implementationHandle))
	corrections := writeInput(t, root, "aggregate-corrections-for-recovery.json", `{"verdict":"corrections","summary":"missing aggregate behavior","findings":"add the missing behavior"}`)
	corrected := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(workflowRevision(t, completed)), "--actor", "aggregate-reviewer", "--input-file", corrections))
	revision := workflowRevision(t, corrected)
	if workflowState(t, corrected) != "ready_to_complete" {
		t.Fatalf("aggregate corrections=%s", corrected)
	}
	recoveryInput := aggregateRecoveryInput(t, root, "aggregate-recovery.json", revision, unitID, "external-recovery")
	recovered := mustOK(t, runAt(t, root, "workflow", "recover-aggregate", "--workflow-id", wfID, "--input-file", recoveryInput, "--revision", itoa(revision), "--actor", "external-recovery", "--handle-dir", filepath.Join(root, "aggregate-recovery-handles")))
	if !strings.Contains(string(recovered), `"unit_revision":2`) || !strings.Contains(string(recovered), `"next_action":"workflow unit-tdd"`) || strings.Contains(string(recovered), "claim_secret") {
		t.Fatalf("aggregate recovery=%s", recovered)
	}
	freshTDD := writeInput(t, root, "aggregate-recovery-tdd.json", `{"red_command":"go test -run TestMissingAggregateBehavior","red_outcome":"exit 1: aggregate recovery behavior missing","green_command":"go test -run TestMissingAggregateBehavior","green_outcome":"exit 0","refactor_summary":"","validation_command":"go test ./internal/cli","validation_outcome":"exit 0","changed_paths":"internal/cli"}`)
	mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "external-recovery", "--claim-handle", aggregateHandlePath(t, recovered), "--input-file", freshTDD))
	if state, evidenceCount := storedUnitEvidence(t, root, wfID, unitID); state != "reviewing" || evidenceCount != 2 {
		t.Fatalf("fresh aggregate recovery state=%s evidence=%d", state, evidenceCount)
	}
}

func TestRecoverAggregateRejectsInvalidSelectionsAndPreservesCorrectionRecord(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, implementationHandle := setupReviewingUnit(t, root, "implementer")
	completed := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", implementationHandle))
	revision := workflowRevision(t, completed)
	recoveryInput := aggregateRecoveryInput(t, root, "batch-recovery.json", revision+1, unitID, "external-recovery")
	args := []string{"workflow", "recover-aggregate", "--workflow-id", wfID, "--input-file", recoveryInput, "--revision", itoa(revision), "--actor", "external-recovery"}
	if noCorrections := runAt(t, root, append(args, "--handle-dir", filepath.Join(root, "no-corrections"))...); noCorrections.code != 3 {
		t.Fatalf("recovery without corrections=%#v", noCorrections)
	}
	corrections := writeInput(t, root, "aggregate-corrections.json", `{"verdict":"corrections","summary":"not aligned","findings":"fix requirement"}`)
	corrected := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "aggregate-reviewer", "--input-file", corrections))
	revision = workflowRevision(t, corrected)
	if stale := runAt(t, root, append(args, "--handle-dir", filepath.Join(root, "stale"))...); stale.code != 4 {
		t.Fatalf("stale recovery=%#v", stale)
	}
	if debug := runAt(t, root, "workflow", "recover-aggregate", "--workflow-id", wfID, "--unit-id", unitID, "--revision", itoa(revision), "--actor", "external-recovery", "--handle-dir", filepath.Join(root, "debug"), "--print-claim-handle-secret-once"); debug.code != 2 {
		t.Fatalf("recovery accepted one-shot flag=%#v", debug)
	}
	if multiple := runAt(t, root, "workflow", "recover-aggregate", "--workflow-id", wfID, "--unit-id", unitID, "--unit-id", "wu-000000000000000000000002", "--revision", itoa(revision), "--actor", "external-recovery", "--handle-dir", filepath.Join(root, "multiple")); multiple.code != 2 {
		t.Fatalf("recovery accepted multiple selections=%#v", multiple)
	}
	recovered := mustOK(t, runAt(t, root, "workflow", "recover-aggregate", "--workflow-id", wfID, "--input-file", recoveryInput, "--revision", itoa(revision), "--actor", "external-recovery", "--handle-dir", filepath.Join(root, "recovered")))
	handlePath := aggregateHandlePath(t, recovered)
	if expired := runAtTime(t, root, time.Date(2026, 8, 20, 15, 16, 0, 0, time.UTC), "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "external-recovery", "--claim-handle", handlePath, "--input-file", writeInput(t, root, "expired-tdd.json", `{"red_command":"red","red_outcome":"exit 1","green_command":"green","green_outcome":"exit 0","refactor_summary":"","validation_command":"all","validation_outcome":"exit 0","changed_paths":"internal"}`)); expired.code != 5 {
		t.Fatalf("aggregate recovery handle did not expire=%#v", expired)
	}
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var evidenceCount, correctionsCount int
	if err = s.DB().QueryRow(`SELECT count(*) FROM evidence WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&evidenceCount); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=? AND kind='aggregate_review'`, wfID).Scan(&correctionsCount); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if evidenceCount != 1 || correctionsCount != 1 {
		t.Fatalf("recovery did not preserve evidence=%d corrections=%d", evidenceCount, correctionsCount)
	}
	approval := writeInput(t, root, "aggregate-approval.json", `{"verdict":"approved","summary":"aligned","findings":""}`)
	// The expired recovered handle leaves the unit pending, so complete cannot
	// bypass the correction cycle; recover a fresh handle, record evidence, and finish.
	fresh := mustOK(t, runAt(t, root, "workflow", "recover-unit-claim", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "external-recovery", "--handle-dir", filepath.Join(root, "fresh")))
	freshTDD := writeInput(t, root, "fresh-tdd.json", `{"red_command":"red","red_outcome":"exit 1","green_command":"green","green_outcome":"exit 0","refactor_summary":"","validation_command":"all","validation_outcome":"exit 0","changed_paths":"internal"}`)
	mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "external-recovery", "--claim-handle", stringField(t, fresh, "handle_path"), "--input-file", freshTDD))
	ready := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "external-recovery", "--claim-handle", stringField(t, fresh, "handle_path")))
	terminal := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(workflowRevision(t, ready)), "--actor", "aggregate-reviewer", "--input-file", approval))
	if rejected := runAt(t, root, "workflow", "recover-aggregate", "--workflow-id", wfID, "--input-file", recoveryInput, "--revision", itoa(workflowRevision(t, terminal)), "--actor", "external-recovery", "--handle-dir", filepath.Join(root, "terminal")); rejected.code != 3 {
		t.Fatalf("terminal recovery=%#v", rejected)
	}
}

func TestAggregateReviewCorrectionsCASActorAndTerminalIntegrity(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, handlePath := setupReviewingUnit(t, root, "implementer")
	completed := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handlePath))
	revision := workflowRevision(t, completed)
	corrections := writeInput(t, root, "aggregate-corrections.json", `{"verdict":"corrections","summary":"not aligned","findings":"fix requirement"}`)
	corrected := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "aggregate-reviewer", "--input-file", corrections))
	if workflowState(t, corrected) != "ready_to_complete" || workflowRevision(t, corrected) != revision+1 || !strings.Contains(string(corrected), `"next_action":"workflow recover-aggregate"`) {
		t.Fatalf("corrections=%s", corrected)
	}
	assertRejectedAggregate := func(name, actor string, attemptedRevision int64, wantCode int) {
		t.Helper()
		before := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
		failed := runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(attemptedRevision), "--actor", actor, "--input-file", corrections)
		if failed.code != wantCode {
			t.Fatalf("%s=%#v", name, failed)
		}
		after := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
		if workflowRevision(t, before) != workflowRevision(t, after) || strings.Count(string(before), `"kind":"aggregate_review"`) != strings.Count(string(after), `"kind":"aggregate_review"`) {
			t.Fatalf("%s mutated workflow", name)
		}
	}
	assertRejectedAggregate("stale CAS", "other-reviewer", revision, 4)
	assertRejectedAggregate("same actor", "implementer", revision+1, 3)
	approval := writeInput(t, root, "aggregate-approval.json", `{"verdict":"approved","summary":"aligned","findings":""}`)
	assertRejectedAggregate("unresolved approval", "other-reviewer", revision+1, 3)
	recoveryInput := aggregateRecoveryInput(t, root, "integrity-recovery.json", revision+1, unitID, "repairer")
	recovered := mustOK(t, runAt(t, root, "workflow", "recover-aggregate", "--workflow-id", wfID, "--revision", itoa(revision+1), "--actor", "aion", "--handle-dir", filepath.Join(root, "integrity-handles"), "--input-file", recoveryInput))
	tdd := writeInput(t, root, "integrity-tdd.json", `{"red_command":"red","red_outcome":"exit 1","green_command":"green","green_outcome":"exit 0","refactor_summary":"","validation_command":"all","validation_outcome":"exit 0","changed_paths":"internal"}`)
	mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "repairer", "--claim-handle", aggregateHandlePath(t, recovered), "--input-file", tdd))
	ready := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "repairer", "--claim-handle", aggregateHandlePath(t, recovered)))
	approved := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(workflowRevision(t, ready)), "--actor", "other-reviewer", "--input-file", approval))
	if workflowState(t, approved) != "completed" {
		t.Fatalf("approval=%s", approved)
	}
	assertRejectedAggregate("terminal", "third-reviewer", workflowRevision(t, approved), 3)
}

func TestWorkflowCompleteAcceptsStructuredAggregateEvidenceAtomically(t *testing.T) {
	root := t.TempDir()
	wfID, _, revision := setupStructuredReadyWorkflow(t, root)
	approval := writeInput(t, root, "structured-approval.json", structuredAggregateApproval(root, "exit 0", true))
	completed := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "aggregate-reviewer", "--input-file", approval))
	if workflowState(t, completed) != "completed" || !strings.Contains(string(completed), `"next_action":"none"`) {
		t.Fatalf("completion=%s", completed)
	}
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var runs, checkpoints int
	if err = s.DB().QueryRow(`SELECT count(*) FROM verification_records WHERE workflow_id=? AND unit_id IS NULL AND tier='aggregate_full'`, wfID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT count(*) FROM reviewed_checkpoints WHERE workflow_id=?`, wfID).Scan(&checkpoints); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || checkpoints != 1 {
		t.Fatalf("aggregate bundle was not persisted atomically: runs=%d checkpoints=%d", runs, checkpoints)
	}
}

func TestWorkflowCompleteRejectsInvalidStructuredBundlesWithoutMutation(t *testing.T) {
	cases := map[string]struct {
		body       func(string) string
		stale      bool
		incomplete bool
		contains   string
	}{
		"missing checkpoint": {body: func(root string) string { return structuredAggregateApproval(root, "exit 0", false) }, contains: "checkpoint"},
		"invalid checkpoint": {body: func(string) string {
			return `{"verdict":"approved","summary":"checked","findings":"","verification_runs":[{"id":"aggregate-1","tier":"aggregate_full","command":"go test ./...","outcome":"exit 0","repository_fingerprint":"fingerprint-a","scenario_ids":["SCN-CLI-001"]}],"checkpoint":{"project_id":"","checkout_root":"","base_revision":"","head_revision":"","result_digest":"","dirty":false}}`
		}, contains: "checkpoint"},
		"missing verification": {body: func(root string) string {
			return strings.Replace(structuredAggregateApproval(root, "exit 0", true), `[{"id":"aggregate-1","tier":"aggregate_full","command":"go test ./...","outcome":"exit 0","repository_fingerprint":"fingerprint-a","scenario_ids":["SCN-CLI-001"]}]`, `[]`, 1)
		}, contains: "aggregate_full"},
		"failed verification":      {body: func(root string) string { return structuredAggregateApproval(root, "exit 1", true) }, contains: "aggregate_full"},
		"stale unit evidence":      {body: func(root string) string { return structuredAggregateApproval(root, "exit 0", true) }, stale: true, contains: "focused"},
		"incomplete unit evidence": {body: func(root string) string { return structuredAggregateApproval(root, "exit 0", true) }, incomplete: true, contains: "focused"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			var wfID, unitID string
			var revision int64
			if tc.incomplete {
				var handle string
				wfID, unitID, handle = setupReviewingUnit(t, root, "implementer")
				s, err := store.Open(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.DB().Exec(`INSERT INTO unit_coverage(workflow_id,unit_id,requirement_id,scenario_id) VALUES(?,?,?,?)`, wfID, unitID, "REQ-CLI-001", "SCN-CLI-001")
				_ = s.Close()
				if err != nil {
					t.Fatal(err)
				}
				revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handle)))
			} else {
				wfID, unitID, revision = setupStructuredReadyWorkflow(t, root)
			}
			if tc.stale {
				s, err := store.Open(context.Background(), root)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.DB().Exec(`UPDATE work_units SET revision=revision+1 WHERE workflow_id=? AND id=?`, wfID, unitID)
				_ = s.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			before := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
			input := writeInput(t, root, "invalid-aggregate.json", tc.body(root))
			failed := runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "aggregate-reviewer", "--input-file", input)
			if failed.code == 0 || failed.stdout != "" || !strings.Contains(failed.stderr, tc.contains) {
				t.Fatalf("failure=%#v", failed)
			}
			after := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
			if !bytes.Equal(before, after) {
				t.Fatalf("rejected aggregate mutated workflow")
			}
		})
	}
}

func TestAuthorizeCorrectionGatesExactExhaustedBlockerAndEnablesBatch(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, handle := setupReviewingUnit(t, root, "implementer")
	ready := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handle))
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE plans SET body=replace(body,'"automatic_rounds":1','"automatic_rounds":0') WHERE workflow_id=?`, wfID); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	corrections := writeInput(t, root, "exhausted-review.json", `{"verdict":"corrections","summary":"final","findings":"new blocker"}`)
	blocked := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(workflowRevision(t, ready)), "--actor", "reviewer", "--input-file", corrections))
	if !strings.Contains(string(blocked), `"next_action":"user authorization required"`) {
		t.Fatalf("blocked=%s", blocked)
	}
	blockerRevision := workflowRevision(t, blocked)
	authorization := writeInput(t, root, "authorization.json", `{"aggregate_review_revision":`+itoa(blockerRevision)+`,"reason":"user approved one transaction","user_direction_confirmed":true}`)
	authorized := mustOK(t, runAt(t, root, "workflow", "authorize-correction", "--workflow-id", wfID, "--revision", itoa(blockerRevision), "--actor", "aion", "--input-file", authorization))
	if workflowRevision(t, authorized) != blockerRevision+1 || !strings.Contains(string(authorized), `"next_action":"workflow recover-aggregate"`) {
		t.Fatalf("authorized=%s", authorized)
	}
	if repeated := runAt(t, root, "workflow", "authorize-correction", "--workflow-id", wfID, "--revision", itoa(blockerRevision+1), "--actor", "user", "--input-file", authorization); repeated.code != 3 {
		t.Fatalf("repeated authorization=%#v", repeated)
	}
	recovery := aggregateRecoveryInput(t, root, "authorized-recovery.json", blockerRevision, unitID, "repairer")
	recovered := mustOK(t, runAt(t, root, "workflow", "recover-aggregate", "--workflow-id", wfID, "--revision", itoa(blockerRevision+1), "--actor", "aion", "--handle-dir", filepath.Join(root, "authorized-handles"), "--input-file", recovery))
	if aggregateHandlePath(t, recovered) == "" || strings.Contains(string(recovered), "claim_secret") {
		t.Fatalf("recovered=%s", recovered)
	}
}

func TestWorkflowShowUsesProjectedCorrectionSynopsisAndAction(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, handle := setupReviewingUnit(t, root, "implementer")
	ready := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handle))
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE plans SET body=replace(body,'"automatic_rounds":1','"automatic_rounds":0') WHERE workflow_id=?`, wfID); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	corrections := writeInput(t, root, "show-exhausted-review.json", `{"verdict":"corrections","summary":"final","findings":"shared projection missing"}`)
	blocked := mustOK(t, runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", itoa(workflowRevision(t, ready)), "--actor", "reviewer", "--input-file", corrections))
	shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	var projection struct {
		Data struct {
			Synopsis history.Synopsis `json:"synopsis"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if err = json.Unmarshal(shown, &projection); err != nil {
		t.Fatal(err)
	}
	policy := projection.Data.Synopsis.CorrectionPolicy
	if policy == nil || !policy.PolicyAware || policy.Allowed != 0 || policy.Used != 0 || policy.BlockerRevision != workflowRevision(t, blocked) || policy.BlockerContent == "" || policy.Authority != "none" {
		t.Fatalf("correction synopsis=%#v response=%s", policy, shown)
	}
	if projection.NextAction != "user authorization required" || projection.Data.Synopsis.NextAction != projection.NextAction || strings.Contains(string(shown), `"next_action":"workflow complete"`) {
		t.Fatalf("contextual action=%q synopsis=%q response=%s", projection.NextAction, projection.Data.Synopsis.NextAction, shown)
	}
}

func TestWorkflowShowCoordinationReturnsBoundedProjection(t *testing.T) {
	root := t.TempDir()
	wfID, _ := createWorkflow(t, root)

	shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID, "--view", "coordination"))
	var response struct {
		Data struct {
			View     string `json:"view"`
			Workflow struct {
				ID string `json:"id"`
			} `json:"workflow"`
			Coordination json.RawMessage `json:"coordination"`
			Audit        json.RawMessage `json:"audit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(shown, &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.View != "coordination" || response.Data.Workflow.ID != wfID || len(response.Data.Coordination) == 0 || len(response.Data.Audit) != 0 {
		t.Fatalf("coordination response = %s", shown)
	}
	for _, forbidden := range []string{`"records"`, `"timeline"`, `"artifacts"`, `"occurrences"`, `"content"`, `"handle_path"`, `"secret"`} {
		if bytes.Contains(shown, []byte(forbidden)) {
			t.Fatalf("coordination response exposed %s: %s", forbidden, shown)
		}
	}
}

func TestWorkflowShowOmittedAndExplicitAuditStayByteCompatible(t *testing.T) {
	root := t.TempDir()
	wfID, _ := createWorkflow(t, root)

	legacy := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	explicit := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID, "--view", "audit"))
	if !bytes.Equal(explicit, legacy) {
		t.Fatalf("explicit audit changed legacy response\nlegacy: %s\naudit:  %s", legacy, explicit)
	}
	for _, required := range []string{`"workflow"`, `"synopsis"`, `"artifacts"`, `"records"`, `"timeline"`} {
		if !bytes.Contains(legacy, []byte(required)) {
			t.Fatalf("legacy audit lacks %s: %s", required, legacy)
		}
	}
}

func TestWorkflowShowPhaseUnitAndAggregateSelectExactlyOneBoundedView(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, _ := setupImplementingUnit(t, root)
	views := []struct {
		name string
		args []string
	}{
		{"phase", []string{"--view", "phase"}},
		{"unit", []string{"--view", "unit", "--unit-id", unitID}},
		{"aggregate", []string{"--view", "aggregate"}},
	}
	for _, tt := range views {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"workflow", "show", "--workflow-id", wfID}, tt.args...)
			shown := mustOK(t, runAt(t, root, args...))
			var response struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(shown, &response); err != nil {
				t.Fatal(err)
			}
			if string(response.Data["view"]) != `"`+tt.name+`"` || len(response.Data[tt.name]) == 0 {
				t.Fatalf("%s response = %s", tt.name, shown)
			}
			for _, other := range []string{"coordination", "phase", "unit", "aggregate", "audit"} {
				if other != tt.name && len(response.Data[other]) != 0 {
					t.Fatalf("%s response populated %s: %s", tt.name, other, shown)
				}
			}
			for _, forbidden := range []string{`"timeline"`, `"occurrences"`, `"handle_path"`, `"secret"`} {
				if bytes.Contains(shown, []byte(forbidden)) {
					t.Fatalf("%s response exposed %s: %s", tt.name, forbidden, shown)
				}
			}
		})
	}
}

func TestTypedAndLegacyStageInputsReachPhaseAndAggregateViews(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	typed := writeInput(t, root, "typed-exploration.json", `{"content":"typed exploration","schema_version":1,"entries":[{"kind":"requirement","id":"REQ-TYPED-INPUT","operation":"add","body":{"text":"typed stage input reaches projections"}}]}`)
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "explore", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "explorer", "--input-file", typed)))
	for _, stage := range []string{"spec", "design"} {
		legacy := writeInput(t, root, stage+"-legacy.json", `{"content":"legacy `+stage+`"}`)
		revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", stage, "--workflow-id", wfID, "--revision", itoa(revision), "--actor", stage, "--input-file", legacy)))
	}

	for _, view := range []string{"phase", "aggregate"} {
		shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID, "--view", view))
		if !bytes.Contains(shown, []byte(`"structured":true`)) || !bytes.Contains(shown, []byte(`"id":"REQ-TYPED-INPUT"`)) || !bytes.Contains(shown, []byte(`"text":"typed stage input reaches projections"`)) {
			t.Fatalf("%s view did not resolve typed stage input: %s", view, shown)
		}
	}
	legacyAudit := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	for _, exact := range []string{`"content":"legacy spec"`, `"content":"legacy design"`} {
		if !bytes.Contains(legacyAudit, []byte(exact)) {
			t.Fatalf("legacy stage input changed shape; missing %s in %s", exact, legacyAudit)
		}
	}
}

func TestInvalidTypedStageEntryLeavesWorkflowUnchanged(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	invalid := writeInput(t, root, "invalid-typed-entry.json", `{"content":"invalid typed exploration","schema_version":1,"entries":[{"kind":"requirement","id":"not-stable","operation":"add","body":{"text":"invalid"}}]}`)
	result := runAt(t, root, "workflow", "explore", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "explorer", "--input-file", invalid)
	if result.code != 3 || result.stdout != "" || !strings.Contains(result.stderr, `"code":"state"`) {
		t.Fatalf("invalid typed input result=%#v", result)
	}
	shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	if workflowRevision(t, shown) != revision || bytes.Contains(shown, []byte(`"kind":"exploration"`)) || bytes.Contains(shown, []byte(`not-stable`)) {
		t.Fatalf("invalid typed input mutated workflow: %s", shown)
	}
}

func TestRepeatedDesignPersistsAmendment(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	for _, stage := range []struct{ command, content string }{{"explore", "facts"}, {"spec", "behavior"}, {"design", "first design"}} {
		input := writeInput(t, root, stage.command+".json", `{"content":"`+stage.content+`"}`)
		revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", stage.command, "--workflow-id", wfID, "--revision", itoa(revision), "--actor", stage.command, "--input-file", input)))
	}
	amendment := writeInput(t, root, "design-amendment.json", `{"content":"amended design"}`)
	response := mustOK(t, runAt(t, root, "workflow", "design", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "designer", "--input-file", amendment))
	var amended struct {
		Data struct {
			Workflow struct {
				Revision int64  `json:"revision"`
				State    string `json:"state"`
			} `json:"workflow"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal(response, &amended); err != nil {
		t.Fatal(err)
	}
	if amended.Data.Workflow.Revision != 5 || amended.Data.Workflow.State != "designing" || amended.NextAction != "workflow plan" {
		t.Fatalf("amended response=%#v", amended)
	}
	shown := mustOK(t, runAt(t, root, "workflow", "show", "--workflow-id", wfID))
	var inspection struct {
		Data struct {
			Artifacts []struct {
				Content  string `json:"content"`
				Revision int64  `json:"revision"`
			} `json:"artifacts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(shown, &inspection); err != nil {
		t.Fatal(err)
	}
	artifacts := inspection.Data.Artifacts
	if len(artifacts) != 4 || artifacts[2].Content != "first design" || artifacts[3].Content != "amended design" || artifacts[3].Revision != 5 {
		t.Fatalf("artifacts=%#v", artifacts)
	}
	actions, _ := storedActivities(t, root, wfID)
	if strings.Join(actions, ",") != "workflow_created,exploration_recorded,specification_recorded,design_recorded,design_recorded" {
		t.Fatalf("activities=%v", actions)
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

	root = t.TempDir()
	wfID, unitID, _ = setupReviewingUnit(t, root, "implementer")
	installFailingActivityTrigger(t, root)
	handoffDir := filepath.Join(root, "failed-handoff")
	if failed := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", handoffDir); failed.code != 1 {
		t.Fatalf("handoff failure=%#v", failed)
	}
	entries, err = os.ReadDir(handoffDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed handoff left files=%v error=%v", entries, err)
	}
	s, _ = store.Open(context.Background(), root)
	defer s.Close()
	var reviewHandles int
	_ = s.DB().QueryRow(`SELECT count(*) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='review'`, wfID, unitID).Scan(&reviewHandles)
	if reviewHandles != 0 {
		t.Fatalf("failed handoff persisted %d review handles", reviewHandles)
	}
	_ = s.Close()

	root = t.TempDir()
	wfID, unitID, _ = setupReviewingUnit(t, root, "implementer")
	reviewPath := handoffReview(t, root, wfID, unitID, "reviewer")
	before, _ = os.ReadFile(reviewPath)
	installFailingActivityTrigger(t, root)
	reviewInput := writeInput(t, root, "failed-review.json", `{"verdict":"approved","summary":"good","findings":""}`)
	if failed := runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", reviewPath, "--input-file", reviewInput); failed.code != 1 {
		t.Fatalf("review failure=%#v", failed)
	}
	after, err = os.ReadFile(reviewPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed review changed handle: equal=%t error=%v", bytes.Equal(before, after), err)
	}
	s, _ = store.Open(context.Background(), root)
	defer s.Close()
	var reviews int
	var reviewState string
	_ = s.DB().QueryRow(`SELECT count(*) FROM reviews WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&reviews)
	_ = s.DB().QueryRow(`SELECT state FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='review'`, wfID, unitID).Scan(&reviewState)
	if reviews != 0 || reviewState != "intent" {
		t.Fatalf("failed review persisted verdict=%d handle=%s", reviews, reviewState)
	}
	_ = s.Close()

	root = t.TempDir()
	wfID, unitID, _ = setupReviewingUnit(t, root, "implementer")
	reviewPath = handoffReview(t, root, wfID, unitID, "reviewer")
	reviewInput = writeInput(t, root, "expire-before-recovery.json", `{"verdict":"approved","summary":"good","findings":""}`)
	if expired := runAtTime(t, root, time.Date(2026, 8, 20, 15, 16, 0, 0, time.UTC), "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", reviewPath, "--input-file", reviewInput); expired.code != 5 {
		t.Fatalf("review expiry=%#v", expired)
	}
	installFailingActivityTrigger(t, root)
	recoveryDir := filepath.Join(root, "failed-review-recovery")
	if failed := runAt(t, root, "workflow", "recover-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--handle-dir", recoveryDir); failed.code != 1 {
		t.Fatalf("review recovery failure=%#v", failed)
	}
	entries, err = os.ReadDir(recoveryDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed review recovery left files=%v error=%v", entries, err)
	}
	s, _ = store.Open(context.Background(), root)
	defer s.Close()
	var recoveryGeneration int
	_ = s.DB().QueryRow(`SELECT count(*),max(claim_generation) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='review'`, wfID, unitID).Scan(&reviewHandles, &recoveryGeneration)
	if reviewHandles != 1 || recoveryGeneration != 1 {
		t.Fatalf("failed recovery handles=%d generation=%d", reviewHandles, recoveryGeneration)
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
	wfID, unitID, _ := setupReviewingUnit(t, root, "implementer")
	handlePath := handoffReview(t, root, wfID, unitID, "reviewer")
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
	if !strings.Contains(string(correction), `"unit_revision":2`) || !strings.Contains(string(correction), `"next_action":"workflow recover-unit-claim"`) {
		t.Fatal(string(correction))
	}
	recovered := mustOK(t, runAt(t, root, "workflow", "recover-unit-claim", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--handle-dir", filepath.Join(root, "recovered")))
	if stringField(t, recovered, "handle_path") == handlePath {
		t.Fatal("recovery reused handle")
	}
	if actions, _ := storedActivities(t, root, wfID); actions[len(actions)-1] != "unit_claim_recovered" {
		t.Fatalf("recovery activity=%v", actions)
	}
	recoveredPath := stringField(t, recovered, "handle_path")
	tdd2 := writeInput(t, root, "tdd-revision-2.json", `{"red_command":"red again","red_outcome":"exit 1","green_command":"green again","green_outcome":"exit 0","refactor_summary":"corrected","validation_command":"all","validation_outcome":"exit 0","changed_paths":"internal"}`)
	mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--claim-handle", recoveredPath, "--input-file", tdd2))
	if premature := runAt(t, root, "workflow", "recover-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "reviewer", "--handle-dir", filepath.Join(root, "premature-review-recovery")); premature.code != 3 {
		t.Fatalf("review recovery bypassed handoff=%#v", premature)
	}
	review2 := mustOK(t, runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "reviewer", "--handle-dir", filepath.Join(root, "review-revision-2")))
	review2Path := stringField(t, review2, "handle_path")
	if review2Path == handlePath {
		t.Fatal("later revision reused review handle")
	}
	s, openErr := store.Open(context.Background(), root)
	if openErr != nil {
		t.Fatal(openErr)
	}
	var reviewGeneration int
	_ = s.DB().QueryRow(`SELECT max(claim_generation) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='review'`, wfID, unitID).Scan(&reviewGeneration)
	_ = s.Close()
	if reviewGeneration != 2 {
		t.Fatalf("later review generation=%d", reviewGeneration)
	}
	approval := writeInput(t, root, "expired-revision-2-review.json", `{"verdict":"approved","summary":"good","findings":""}`)
	if expired := runAtTime(t, root, time.Date(2026, 8, 20, 15, 16, 0, 0, time.UTC), "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "reviewer", "--claim-handle", review2Path, "--input-file", approval); expired.code != 5 {
		t.Fatalf("later review expiry=%#v", expired)
	}
	if bypass := runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "other-reviewer", "--handle-dir", filepath.Join(root, "forbidden-revision-2-replacement")); bypass.code != 3 {
		t.Fatalf("later review handoff bypassed recovery=%#v", bypass)
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
	s, err = store.Open(context.Background(), debugRoot)
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

func TestOutsidePlanCorrectionNamesAionAsNextCoordinator(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, _ := setupReviewingUnit(t, root, "implementer")
	handlePath := handoffReview(t, root, wfID, unitID, "reviewer")
	reviewFile := writeInput(t, root, "outside-correction.json", `{"verdict":"corrections","summary":"plan change","findings":"split the unit","plan_impact":"outside"}`)
	correction := mustOK(t, runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", handlePath, "--input-file", reviewFile))
	var response struct {
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal(correction, &response); err != nil {
		t.Fatal(err)
	}
	if response.NextAction != "aion revise plan" {
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			wfID, unitID, handlePath := setupReviewingUnit(t, root, "implementer")
			if tt.name == "review domain rejection" {
				handlePath = handoffReview(t, root, wfID, unitID, "reviewer")
			}
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
	revision = workflowRevision(t, approved)
	firstApproved, secondApproved = storedExceptionApprovals(t, root, wfID, first, second)
	if !firstApproved || secondApproved {
		t.Fatalf("persisted approvals first=%t second=%t", firstApproved, secondApproved)
	}
	readyBeforeImplementation := runAt(t, root, "workflow", "list-ready-units", "--workflow-id", wfID)
	if readyBeforeImplementation.code != 3 || readyBeforeImplementation.stdout != "" {
		t.Fatalf("preimplementation readiness=%#v", readyBeforeImplementation)
	}
	approvedHandleDir := filepath.Join(root, "approved-handles")
	claimBeforeImplementation := runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", first, "--revision", "1", "--actor", "implementer", "--handle-dir", approvedHandleDir)
	if claimBeforeImplementation.code != 3 || claimBeforeImplementation.stdout != "" {
		t.Fatalf("preimplementation claim=%#v", claimBeforeImplementation)
	}
	if _, err := os.Stat(approvedHandleDir); !os.IsNotExist(err) {
		t.Fatalf("preimplementation claim created handle directory: %v", err)
	}
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "begin-implementation", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon")))
	readyAfter := mustOK(t, runAt(t, root, "workflow", "list-ready-units", "--workflow-id", wfID))
	if !strings.Contains(string(readyAfter), first) || !strings.Contains(string(readyAfter), second) {
		t.Fatalf("approved readiness=%s", readyAfter)
	}
	claimAfter := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", first, "--revision", "1", "--actor", "implementer", "--handle-dir", approvedHandleDir))
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
	unitID := "wu-000000000000000000000001"
	for _, stage := range []string{"explore", "spec", "design"} {
		input := writeInput(t, root, stage+"-setup.json", `{"content":"`+stage+`"}`)
		revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", stage, "--workflow-id", wfID, "--revision", itoa(revision), "--actor", stage, "--input-file", input)))
	}
	planFile := writeInput(t, root, "setup-plan.json", `{"summary":"one","scope":"internal","max_parallel_units":1,"work_units":[{"id":"`+unitID+`","description":"unit","scope":"internal","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}]}`)
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "planner", "--input-file", planFile)))
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "approve-plan", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon")))
	revision = workflowRevision(t, mustOK(t, runAt(t, root, "workflow", "begin-implementation", "--workflow-id", wfID, "--revision", itoa(revision), "--actor", "daimon")))
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

func setupStructuredReadyWorkflow(t *testing.T, root string) (string, string, int64) {
	t.Helper()
	wfID, unitID, _ := setupImplementingUnit(t, root)
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO unit_coverage(workflow_id,unit_id,requirement_id,scenario_id) VALUES(?,?,?,?)`, wfID, unitID, "REQ-CLI-001", "SCN-CLI-001")
	_ = s.Close()
	if err != nil {
		t.Fatal(err)
	}
	claim := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "handles")))
	handle := stringField(t, claim, "handle_path")
	tdd := writeInput(t, root, "structured-tdd.json", `{"red_command":"test red","red_outcome":"exit 1","green_command":"test green","green_outcome":"exit 0","refactor_summary":"","validation_command":"test all","validation_outcome":"exit 0","changed_paths":"internal","verification_runs":[{"id":"focused-1","tier":"focused","command":"go test -run Focused","outcome":"exit 0","repository_fingerprint":"fingerprint-a","scenario_ids":["SCN-CLI-001"]},{"id":"package-1","tier":"affected_package","command":"go test ./internal/cli","outcome":"exit 0","repository_fingerprint":"fingerprint-a","scenario_ids":["SCN-CLI-001"]}],"scenario_results":[{"scenario_id":"SCN-CLI-001","outcome":"exit 0","verification_id":"focused-1"}]}`)
	mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handle, "--input-file", tdd))
	ready := mustOK(t, runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handle))
	return wfID, unitID, workflowRevision(t, ready)
}

func structuredAggregateApproval(root, outcome string, checkpoint bool) string {
	body := `{"verdict":"approved","summary":"requirements satisfied","findings":"","verification_runs":[{"id":"aggregate-1","tier":"aggregate_full","command":"go test ./...","outcome":"` + outcome + `","repository_fingerprint":"fingerprint-a","scenario_ids":["SCN-CLI-001"]}]`
	if checkpoint {
		body += `,"checkpoint":{"project_id":"` + strings.Repeat("1", 64) + `","checkout_root":"` + root + `","base_revision":"` + strings.Repeat("2", 40) + `","head_revision":"` + strings.Repeat("3", 40) + `","result_digest":"` + strings.Repeat("4", 64) + `","dirty":false}`
	}
	return body + `}`
}

func handoffReview(t *testing.T, root, wfID, unitID, reviewer string) string {
	t.Helper()
	result := mustOK(t, runAt(t, root, "workflow", "handoff-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", reviewer, "--handle-dir", filepath.Join(root, "review-handles")))
	return stringField(t, result, "handle_path")
}

func mustOK(t *testing.T, result result) []byte {
	t.Helper()
	if result.code != 0 || result.stderr != "" {
		t.Fatalf("result=%#v", result)
	}
	return []byte(result.stdout)
}

func workflowID(t *testing.T, document []byte) string { return nestedWorkflowString(t, document, "id") }
func deliveryID(t *testing.T, document []byte) string {
	t.Helper()
	var v struct {
		Data struct {
			Delivery struct {
				ID string `json:"id"`
			} `json:"delivery"`
		} `json:"data"`
	}
	if err := json.Unmarshal(document, &v); err != nil {
		t.Fatal(err)
	}
	return v.Data.Delivery.ID
}
func deliverySearchIDs(t *testing.T, document []byte) []string {
	t.Helper()
	var v struct {
		Data struct {
			Deliveries []struct {
				ID string `json:"delivery_id"`
			} `json:"deliveries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(document, &v); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(v.Data.Deliveries))
	for i := range v.Data.Deliveries {
		ids[i] = v.Data.Deliveries[i].ID
	}
	return ids
}
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
func aggregateRecoveryInput(t *testing.T, root, name string, reviewRevision int64, unitID, actor string) string {
	t.Helper()
	body := `{"aggregate_review_revision":` + itoa(reviewRevision) + `,"groups":[{"causal_invariant":"shared","findings":["fix"],"unit_ids":["` + unitID + `"]}],"assignments":[{"unit_id":"` + unitID + `","actor":"` + actor + `"}]}`
	return writeInput(t, root, name, body)
}
func aggregateHandlePath(t *testing.T, document []byte) string {
	t.Helper()
	var value struct {
		Data struct {
			Handles []struct {
				HandlePath string `json:"handle_path"`
			} `json:"handles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(document, &value); err != nil || len(value.Data.Handles) != 1 {
		t.Fatalf("aggregate handles: %s error=%v", document, err)
	}
	return value.Data.Handles[0].HandlePath
}
func itoa(value int64) string { return strconv.FormatInt(value, 10) }
