package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestAgentBriefDynamicContextsAreBoundedAndRoleLocal(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-context',8,'ready_to_complete','Context','goal','now','now')`,
		`INSERT INTO plans VALUES('wf-context','summary /tmp/pc2 aggregate-token','scope',1,'{"secret_plan":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}')`,
		`INSERT INTO work_units VALUES('wu-selected','wf-context','selected /tmp/pc2 unit-secret aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','internal/selected','["internal/selected/a.go"]','[]',20,10,'reviewing',NULL,0,3)`,
		`INSERT INTO work_units VALUES('wu-unrelated','wf-context','secret sibling','internal/secret','[]','[]',20,10,'done',NULL,0,1)`,
		`INSERT INTO unit_coverage VALUES('wf-context','wu-selected','REQ-CTX-001','SCN-CTX-001')`,
		`INSERT INTO evidence VALUES('wf-context','wu-selected',3,'implementer','red /tmp/pc2 token command','secret outcome','green internal/pkg command','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','','secret validation command','exit 0','/secret/path','now')`,
		`INSERT INTO reviews VALUES('wf-context','wu-selected',3,'reviewer','approved','review /tmp/pc2 secret summary','review internal/pkg token finding','unplanned','now')`,
		`INSERT INTO verification_records VALUES('vr-1','wf-context','wu-selected',3,'focused','secret command','exit 0','secret-hash','["SCN-CTX-001"]',NULL,'actor','now')`,
		`INSERT INTO handles VALUES('claim-secret','wf-context','wu-selected','active','secret-token-hash','actor','2026-08-31T10:00:00Z','2099-08-31T10:00:00Z',1,'implementation')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-context','aggregate_review','secret unrelated artifact','reviewer',8,'now')`,
	} {
		if _, err = s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, phase := range []string{"exploration", "specification", "design"} {
		result, insertErr := s.DB().Exec(`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-context',?,?,'actor',8,'now')`, phase, phase)
		if insertErr != nil {
			t.Fatal(insertErr)
		}
		id, _ := result.LastInsertId()
		if _, insertErr = s.DB().Exec(`INSERT INTO normative_entries VALUES('wf-context',?,?, 'requirement',?,NULL,'add',?)`, id, phase, "REQ-"+strings.ToUpper(phase), `{"text":"/tmp/pc2 internal/pkg phase-token cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc `+phase+`"}`); insertErr != nil {
			t.Fatal(insertErr)
		}
	}
	phase := briefContext(t, root, "pc2-designer", "--workflow-id", "wf-context")
	if phase["kind"] != "phase" || len(phase["phase"].(map[string]any)["entries"].([]any)) != 3 {
		t.Fatalf("phase=%#v", phase)
	}
	unit := briefContext(t, root, "pc2-implementer", "--workflow-id", "wf-context", "--unit-id", "wu-selected")
	reviewer := briefContext(t, root, "pc2-reviewer", "--workflow-id", "wf-context", "--unit-id", "wu-selected")
	aggregate := briefContext(t, root, "pc2-reviewer", "--workflow-id", "wf-context")
	for name, value := range map[string]map[string]any{"phase": phase, "unit": unit, "reviewer": reviewer, "aggregate": aggregate} {
		data, _ := json.Marshal(value)
		text := strings.ToLower(string(data))
		for _, key := range []string{"handle", "claim", "path", "hash", "fingerprint", "token", "secret", "audit", "history", "artifacts", "reviews"} {
			if strings.Contains(text, `"`+key+`":`) {
				t.Fatalf("%s leaked %q: %s", name, key, data)
			}
		}
		if strings.Contains(text, "secret") {
			t.Fatalf("%s leaked secret marker: %s", name, data)
		}
	}
	if phase["next_action"] != "return to aion" || unit["next_action"] != "return to aion" || reviewer["next_action"] != "return to aion" || aggregate["next_action"] != "workflow complete" {
		t.Fatalf("role-local actions: phase=%v unit=%v reviewer=%v aggregate=%v", phase["next_action"], unit["next_action"], reviewer["next_action"], aggregate["next_action"])
	}
	if len(stringSlice(phase["allowed_actions"])) != 0 || len(stringSlice(unit["allowed_actions"])) != 0 || len(stringSlice(reviewer["allowed_actions"])) != 0 || strings.Join(stringSlice(aggregate["allowed_actions"]), ",") != "workflow complete" {
		t.Fatalf("dynamic authorities: phase=%v unit=%v reviewer=%v aggregate=%v", phase["allowed_actions"], unit["allowed_actions"], reviewer["allowed_actions"], aggregate["allowed_actions"])
	}
	unitJSON, reviewerJSON, aggregateJSON := mustJSON(unit), mustJSON(reviewer), mustJSON(aggregate)
	if strings.Contains(string(unitJSON), "wu-unrelated") || strings.Contains(string(reviewerJSON), "wu-unrelated") || strings.Contains(string(unitJSON), "unit-review") || strings.Contains(string(reviewerJSON), "claim-unit") {
		t.Fatalf("role boundary leak: unit=%s reviewer=%s", unitJSON, reviewerJSON)
	}
	if !strings.Contains(string(unitJSON), "SCN-CTX-001") || !strings.Contains(string(reviewerJSON), "SCN-CTX-001") || !strings.Contains(string(aggregateJSON), "wu-unrelated") {
		t.Fatalf("required summaries missing: %s / %s / %s", unitJSON, reviewerJSON, aggregateJSON)
	}
	fullCases := map[string]map[string]any{
		"phase":     fullBrief(t, root, "pc2-designer", "--workflow-id", "wf-context"),
		"unit":      fullBrief(t, root, "pc2-implementer", "--workflow-id", "wf-context", "--unit-id", "wu-selected"),
		"reviewer":  fullBrief(t, root, "pc2-reviewer", "--workflow-id", "wf-context", "--unit-id", "wu-selected"),
		"aggregate": fullBrief(t, root, "pc2-reviewer", "--workflow-id", "wf-context"),
	}
	for name, value := range fullCases {
		data, _ := json.Marshal(value)
		text := strings.ToLower(string(data))
		for _, forbidden := range []string{`"scope":`, `"areas":`, `"path":`, `"handle":`, `"secret":`, `"hash":`, `"audit":`, `"history":`, `"siblings":`, "/tmp/pc2", "internal/", "phase-token", "unit-secret", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("full %s brief leaked %q: %s", name, forbidden, data)
			}
		}
		roleArgs := map[string][]string{
			"phase":     {"pc2-designer", "--workflow-id", "wf-context"},
			"unit":      {"pc2-implementer", "--workflow-id", "wf-context", "--unit-id", "wu-selected"},
			"reviewer":  {"pc2-reviewer", "--workflow-id", "wf-context", "--unit-id", "wu-selected"},
			"aggregate": {"pc2-reviewer", "--workflow-id", "wf-context"},
		}[name]
		result := runAt(t, root, append([]string{"agent", "brief", "--role"}, roleArgs...)...)
		for _, forbidden := range []string{"/tmp/pc2", "internal/", "phase-token", "unit-secret", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"} {
			if strings.Contains(strings.ToLower(result.stdout), forbidden) {
				t.Fatalf("text %s brief leaked %q: %s", name, forbidden, result.stdout)
			}
		}
	}

	empty := t.TempDir()
	missing := runAt(t, empty, "agent", "brief", "--role", "pc2-explorer", "--workflow-id", "wf-missing", "--json")
	missingUnit := runAt(t, root, "agent", "brief", "--role", "pc2-implementer", "--workflow-id", "wf-context", "--unit-id", "wu-missing", "--json")
	_, statErr := os.Stat(filepath.Join(empty, ".pitcrew"))
	if missing.code == 0 || missingUnit.code == 0 || !os.IsNotExist(statErr) {
		t.Fatalf("not-found read mutated or succeeded: workflow=%#v unit=%#v stat=%v", missing, missingUnit, statErr)
	}
}

func briefContext(t *testing.T, root, role string, contextArgs ...string) map[string]any {
	args := append(append([]string{"agent", "brief", "--role", role}, contextArgs...), "--json")
	result := runAt(t, root, args...)
	var document map[string]any
	if result.code != 0 || json.Unmarshal([]byte(result.stdout), &document) != nil {
		t.Fatalf("brief %s=%#v", role, result)
	}
	brief := document["data"].(map[string]any)["brief"].(map[string]any)
	context := brief["context"].(map[string]any)
	context["next_action"] = brief["next_action"]
	return context
}
