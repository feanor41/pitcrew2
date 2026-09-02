package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestAgentBriefRecoveredCorrectionClaimAuthorizesUnitTDD(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, _ := setupReviewingUnit(t, root, "implementer")
	reviewHandle := handoffReview(t, root, wfID, unitID, "reviewer")
	review := writeInput(t, root, "correction.json", `{"verdict":"corrections","summary":"changes required","findings":"add regression coverage","plan_impact":"inside"}`)
	mustOK(t, runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", reviewHandle, "--input-file", review))
	recoveryTime := time.Date(2026, 8, 20, 15, 16, 0, 0, time.UTC)

	beforeRecovery := fullBriefAt(t, root, recoveryTime, "pc2-implementer", "--workflow-id", wfID, "--unit-id", unitID)
	if beforeRecovery["next_action"] != "workflow recover-unit-claim" {
		t.Fatalf("brief before recovery=%#v", beforeRecovery)
	}

	mustOK(t, runAtTime(t, root, recoveryTime, "workflow", "recover-unit-claim", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "2", "--actor", "implementer", "--handle-dir", filepath.Join(root, "recovered")))
	afterRecovery := fullBriefAt(t, root, recoveryTime, "pc2-implementer", "--workflow-id", wfID, "--unit-id", unitID)
	context := afterRecovery["context"].(map[string]any)
	if afterRecovery["next_action"] != "workflow unit-tdd" || strings.Join(stringSlice(context["allowed_actions"]), ",") != "workflow unit-tdd,workflow release-unit-claim" {
		t.Fatalf("brief after recovery=%#v", afterRecovery)
	}
}

func TestAgentBriefAionWorkflowContextIsDurableBoundedAndReadOnly(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-active',4,'implementing','Active','goal','now','now')`,
		`INSERT INTO plans VALUES('wf-active','summary','internal/path',1,'{}')`,
		`INSERT INTO work_units VALUES('wu-ready','wf-active','ready','internal/path','["internal/file.go"]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-completed',8,'completed','Done','goal','now','now')`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-abandoned',9,'abandoned','Stopped','goal','now','now')`,
	} {
		if _, err = s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	active := fullBrief(t, root, "aion", "--workflow-id", "wf-active")
	activeContext := active["context"].(map[string]any)
	if activeContext["kind"] != "coordination" || active["next_action"] != "workflow list-ready-units" || strings.Join(stringSlice(activeContext["allowed_actions"]), ",") != "workflow list-ready-units" {
		t.Fatalf("active Aion brief=%#v", active)
	}
	activeText := runAt(t, root, "agent", "brief", "--role", "aion", "--workflow-id", "wf-active")
	if activeText.code != 0 || !strings.Contains(activeText.stdout, `"workflow_id":"wf-active"`) || !strings.Contains(activeText.stdout, `"allowed_actions":["workflow list-ready-units"]`) {
		t.Fatalf("text Aion coordination drifted: %#v", activeText)
	}
	for _, id := range []string{"wf-completed", "wf-abandoned"} {
		terminal := fullBrief(t, root, "aion", "--workflow-id", id)
		context := terminal["context"].(map[string]any)
		if terminal["next_action"] != "none" || len(stringSlice(context["allowed_actions"])) != 0 {
			t.Fatalf("terminal Aion brief %s=%#v", id, terminal)
		}
		if terminal["contract_digest"] != active["contract_digest"] {
			t.Fatalf("dynamic state changed stable digest: active=%v terminal=%v", active["contract_digest"], terminal["contract_digest"])
		}
	}

	empty := t.TempDir()
	missing := runAt(t, empty, "agent", "brief", "--role", "aion", "--workflow-id", "wf-missing", "--json")
	_, statErr := os.Stat(filepath.Join(empty, ".pitcrew"))
	if missing.code == 0 || !os.IsNotExist(statErr) {
		t.Fatalf("missing Aion workflow succeeded or mutated: %#v stat=%v", missing, statErr)
	}
}

func TestAgentBriefUnitAuthorityRejectsDependencyTerminalAndPathLeakage(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-units',5,'implementing','Units','goal','now','now')`,
		`INSERT INTO plans VALUES('wf-units','summary','internal/path',1,'{}')`,
		`INSERT INTO work_units VALUES('wu-prereq','wf-units','prereq','internal/prereq','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-dependent','wf-units','dependent','internal/dependent','["internal/dependent.go"]','["wu-prereq"]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-terminal',3,'abandoned','Terminal','goal','now','now')`,
		`INSERT INTO plans VALUES('wf-terminal','summary','internal/path',1,'{}')`,
		`INSERT INTO work_units VALUES('wu-terminal','wf-terminal','terminal','internal/terminal','["internal/terminal.go"]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-ready',7,'implementing','Ready','goal','now','now')`,
		`INSERT INTO plans VALUES('wf-ready','summary','internal/path',1,'{"summary":"ready","scope":"internal","work_units":[{"id":"wu-ready","description":"ready","scope":"internal/ready","areas":[],"depends_on":[]}],"max_parallel_units":1}')`,
		`INSERT INTO work_units VALUES('wu-ready','wf-ready','ready','internal/ready','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-claimed',7,'implementing','Claimed','goal','now','now')`,
		`INSERT INTO plans VALUES('wf-claimed','summary','internal/path',1,'{"summary":"claimed","scope":"internal","work_units":[{"id":"wu-claimed","description":"claimed","scope":"internal/claimed","areas":[],"depends_on":[]}],"max_parallel_units":1}')`,
		`INSERT INTO work_units VALUES('wu-claimed','wf-claimed','claimed','internal/claimed','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO handles VALUES('claim-action','wf-claimed','wu-claimed','active','opaque-hash','actor','2026-08-31T10:00:00Z','2099-08-31T10:00:00Z',1,'implementation')`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-correction',7,'implementing','Correction','goal','now','now')`,
		`INSERT INTO plans VALUES('wf-correction','summary','internal/path',1,'{"summary":"correction","scope":"internal","work_units":[{"id":"wu-correction","description":"correction","scope":"internal/correction","areas":[],"depends_on":[]}],"max_parallel_units":1}')`,
		`INSERT INTO work_units VALUES('wu-correction','wf-correction','correction','internal/correction','[]','[]',1,1,'pending',NULL,0,2)`,
		`INSERT INTO reviews VALUES('wf-correction','wu-correction',1,'reviewer','corrections','summary','finding','','now')`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-reviewing',7,'implementing','Reviewing','goal','now','now')`,
		`INSERT INTO plans VALUES('wf-reviewing','summary','internal/path',1,'{"summary":"reviewing","scope":"internal","work_units":[{"id":"wu-reviewing","description":"reviewing","scope":"internal/reviewing","areas":[],"depends_on":[]}],"max_parallel_units":1}')`,
		`INSERT INTO work_units VALUES('wu-reviewing','wf-reviewing','reviewing','internal/reviewing','[]','[]',1,1,'reviewing',NULL,0,1)`,
	} {
		if _, err = s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ workflow, unit string }{{workflow: "wf-units", unit: "wu-dependent"}, {workflow: "wf-terminal", unit: "wu-terminal"}} {
		brief := fullBrief(t, root, "pc2-implementer", "--workflow-id", tc.workflow, "--unit-id", tc.unit)
		context := brief["context"].(map[string]any)
		if brief["next_action"] != "return to aion" || len(stringSlice(context["allowed_actions"])) != 0 {
			t.Fatalf("unit authority %#v=%#v", tc, brief)
		}
		encoded, _ := json.Marshal(brief)
		text := strings.ToLower(string(encoded))
		for _, forbidden := range []string{`"scope":`, `"areas":`, `"path":`, `"handle":`, `"secret":`, `"hash":`, `"audit":`, "internal/"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("brief leaked %q: %s", forbidden, encoded)
			}
		}
	}
	for _, tc := range []struct{ role, workflow, unit, action, allowed string }{
		{role: "pc2-implementer", workflow: "wf-ready", unit: "wu-ready", action: "workflow claim-unit", allowed: "workflow claim-unit"},
		{role: "pc2-implementer", workflow: "wf-claimed", unit: "wu-claimed", action: "workflow unit-tdd", allowed: "workflow unit-tdd,workflow release-unit-claim"},
		{role: "pc2-implementer", workflow: "wf-correction", unit: "wu-correction", action: "workflow recover-unit-claim"},
		{role: "pc2-reviewer", workflow: "wf-reviewing", unit: "wu-reviewing", action: "workflow unit-review"},
	} {
		if tc.allowed == "" {
			tc.allowed = tc.action
		}
		brief := fullBrief(t, root, tc.role, "--workflow-id", tc.workflow, "--unit-id", tc.unit)
		context := brief["context"].(map[string]any)
		if brief["next_action"] != tc.action || strings.Join(stringSlice(context["allowed_actions"]), ",") != tc.allowed {
			t.Fatalf("action case %#v=%#v", tc, brief)
		}
	}
}

func TestPreservedFailureResultChainConvergesAfterApprovedReview(t *testing.T) {
	root := t.TempDir()
	wfID, unitID, implementationHandle := setupStructuredReviewingUnit(t, root)
	reviewHandle := handoffReview(t, root, wfID, unitID, "reviewer")
	review := writeInput(t, root, "approved-review.json", `{"verdict":"approved","summary":"current result approved","findings":""}`)
	mustOK(t, runAt(t, root, "workflow", "unit-review", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "reviewer", "--claim-handle", reviewHandle, "--input-file", review))
	progress := writeInput(t, root, "stale-progress.json", `{"status":"advanced","summary":"review still pending","next_action":"workflow unit-review"}`)
	mustOK(t, runAt(t, root, "workflow", "progress", "--workflow-id", wfID, "--revision", "7", "--actor", "aion", "--input-file", progress))

	secondUnit := "wu-000000000000000000000002"
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	unreviewedUnit := "wu-000000000000000000000000"
	planBody := `{"summary":"preserved result chain","scope":"internal","max_parallel_units":3,"work_units":[{"id":"` + unreviewedUnit + `","description":"unreviewed sibling","scope":"internal/unreviewed","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"` + unitID + `","description":"reviewed unit","scope":"internal/reviewed","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1,"coverage":[{"requirement_id":"REQ-184","scenario_ids":["SCN-184"]}]},{"id":"` + secondUnit + `","description":"ready sibling","scope":"internal/ready","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}]}`
	if _, err = s.DB().Exec(`UPDATE plans SET body=? WHERE workflow_id=?`, planBody, wfID); err == nil {
		_, err = s.DB().Exec(`INSERT INTO work_units VALUES(?,?,?,'internal/ready','[]','[]',1,1,'pending',NULL,0,1)`, secondUnit, wfID, "ready sibling")
	}
	if err == nil {
		_, err = s.DB().Exec(`INSERT INTO work_units VALUES(?,?,?,'internal/unreviewed','[]','[]',1,1,'reviewing',NULL,0,1)`, unreviewedUnit, wfID, "unreviewed sibling")
	}
	if closeErr := s.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	for name, result := range map[string]result{
		"coordination": runAt(t, root, "workflow", "show", "--workflow-id", wfID, "--view", "coordination"),
		"audit":        runAt(t, root, "workflow", "show", "--workflow-id", wfID),
		"delivery":     runAt(t, root, "delivery", "show", "--delivery-id", wfID),
		"active":       runAt(t, root, "delivery", "active"),
		"ready units":  runAt(t, root, "workflow", "list-ready-units", "--workflow-id", wfID),
	} {
		if result.code != 0 || !strings.Contains(result.stdout, `"next_action":"workflow unit-complete"`) {
			t.Fatalf("%s did not project durable completion over stale progress: %#v", name, result)
		}
	}
	coordination := runAt(t, root, "workflow", "show", "--workflow-id", wfID, "--view", "coordination")
	var coordinationDocument struct {
		Data struct {
			Coordination struct {
				Current struct{ ID, Status string } `json:"current"`
			} `json:"coordination"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(coordination.stdout), &coordinationDocument); err != nil || coordinationDocument.Data.Coordination.Current.ID != unitID || coordinationDocument.Data.Coordination.Current.Status != "Reviewing" {
		t.Fatalf("coordination selected malformed or non-completable current unit: %s", coordination.stdout)
	}
	audit := runAt(t, root, "workflow", "show", "--workflow-id", wfID)
	if !strings.Contains(audit.stdout, "review still pending") || !strings.Contains(audit.stdout, `"next_action":"workflow unit-complete"`) {
		t.Fatalf("stale progress was not visibly demoted beneath durable authority: %#v", audit)
	}
	aion := fullBrief(t, root, "aion", "--workflow-id", wfID)
	aionContext := aion["context"].(map[string]any)
	if aion["next_action"] != "handoff to pc2-implementer" || len(stringSlice(aionContext["allowed_actions"])) != 0 {
		t.Fatalf("Aion did not project role-local completion handoff: %#v", aion)
	}
	aionCurrent := aionContext["coordination"].(map[string]any)["current"].(map[string]any)
	if aionCurrent["unit_id"] != unitID || aionCurrent["status"] != "Reviewing" {
		t.Fatalf("Aion received malformed completion target: %#v", aionCurrent)
	}
	reviewer := fullBrief(t, root, "pc2-reviewer", "--workflow-id", wfID, "--unit-id", unitID)
	reviewerContext := reviewer["context"].(map[string]any)
	if reviewer["next_action"] != "return to aion" || len(stringSlice(reviewerContext["allowed_actions"])) != 0 {
		t.Fatalf("approved current review was reauthorized: %#v", reviewer)
	}
	reviewerJSON, _ := json.Marshal(reviewer)
	if !strings.Contains(string(reviewerJSON), `"scenario_id":"SCN-184"`) || !strings.Contains(string(reviewerJSON), `"status":"passed"`) {
		t.Fatalf("review projection lost accepted scenario evidence: %s", reviewerJSON)
	}

	implementer := fullBrief(t, root, "pc2-implementer", "--workflow-id", wfID, "--unit-id", unitID)
	implementerContext := implementer["context"].(map[string]any)
	if implementer["next_action"] != "workflow unit-complete" || strings.Join(stringSlice(implementerContext["allowed_actions"]), ",") != "workflow unit-complete" {
		t.Fatalf("approved current review did not converge on completion: %#v", implementer)
	}

	completed := runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", implementationHandle)
	if completed.code != 0 || !strings.Contains(completed.stdout, `"state":"done"`) || !strings.Contains(completed.stdout, `"next_action":"workflow list-ready-units"`) {
		t.Fatalf("authoritative completion did not advance once: %#v", completed)
	}
	repeated := runAt(t, root, "workflow", "unit-complete", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", implementationHandle)
	if repeated.code != 5 || repeated.stdout != "" {
		t.Fatalf("consumed completion authority was reusable: %#v", repeated)
	}
}

func setupStructuredReviewingUnit(t *testing.T, root string) (string, string, string) {
	t.Helper()
	wfID, unitID, _ := setupImplementingUnit(t, root)
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO unit_coverage(workflow_id,unit_id,requirement_id,scenario_id) VALUES(?,?,?,?)`, wfID, unitID, "REQ-184", "SCN-184")
	if closeErr := s.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	claim := mustOK(t, runAt(t, root, "workflow", "claim-unit", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--handle-dir", filepath.Join(root, "handles")))
	handle := stringField(t, claim, "handle_path")
	tdd := writeInput(t, root, "structured-reviewing-tdd.json", `{"red_command":"go test -run Preserved","red_outcome":"exit 1","green_command":"go test -run Preserved","green_outcome":"exit 0","refactor_summary":"","validation_command":"go test ./...","validation_outcome":"exit 0","changed_paths":"internal","verification_runs":[{"id":"focused-184","tier":"focused","command":"go test -run Preserved","outcome":"exit 0","repository_fingerprint":"fingerprint-184","scenario_ids":["SCN-184"]},{"id":"package-184","tier":"affected_package","command":"go test ./internal/cli","outcome":"exit 0","repository_fingerprint":"fingerprint-184","scenario_ids":["SCN-184"]}],"scenario_results":[{"scenario_id":"SCN-184","outcome":"exit 0","verification_id":"focused-184"}]}`)
	mustOK(t, runAt(t, root, "workflow", "unit-tdd", "--workflow-id", wfID, "--unit-id", unitID, "--revision", "1", "--actor", "implementer", "--claim-handle", handle, "--input-file", tdd))
	return wfID, unitID, handle
}

func fullBrief(t *testing.T, root, role string, contextArgs ...string) map[string]any {
	t.Helper()
	args := append(append([]string{"agent", "brief", "--role", role}, contextArgs...), "--json")
	result := runAt(t, root, args...)
	return parseFullBrief(t, role, result)
}

func fullBriefAt(t *testing.T, root string, now time.Time, role string, contextArgs ...string) map[string]any {
	t.Helper()
	args := append(append([]string{"agent", "brief", "--role", role}, contextArgs...), "--json")
	return parseFullBrief(t, role, runAtTime(t, root, now, args...))
}

func parseFullBrief(t *testing.T, role string, result result) map[string]any {
	t.Helper()
	var document map[string]any
	if result.code != 0 || json.Unmarshal([]byte(result.stdout), &document) != nil {
		t.Fatalf("brief %s=%#v", role, result)
	}
	return document["data"].(map[string]any)["brief"].(map[string]any)
}

func stringSlice(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.(string))
	}
	return result
}
