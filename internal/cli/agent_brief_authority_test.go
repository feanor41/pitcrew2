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
	for _, tc := range []struct{ role, workflow, unit, action string }{
		{role: "pc2-implementer", workflow: "wf-ready", unit: "wu-ready", action: "workflow claim-unit"},
		{role: "pc2-implementer", workflow: "wf-claimed", unit: "wu-claimed", action: "workflow unit-tdd"},
		{role: "pc2-implementer", workflow: "wf-correction", unit: "wu-correction", action: "workflow recover-unit-claim"},
		{role: "pc2-reviewer", workflow: "wf-reviewing", unit: "wu-reviewing", action: "workflow unit-review"},
	} {
		brief := fullBrief(t, root, tc.role, "--workflow-id", tc.workflow, "--unit-id", tc.unit)
		context := brief["context"].(map[string]any)
		if brief["next_action"] != tc.action || strings.Join(stringSlice(context["allowed_actions"]), ",") != tc.action {
			t.Fatalf("action case %#v=%#v", tc, brief)
		}
	}
}

func fullBrief(t *testing.T, root, role string, contextArgs ...string) map[string]any {
	t.Helper()
	args := append(append([]string{"agent", "brief", "--role", role}, contextArgs...), "--json")
	result := runAt(t, root, args...)
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
