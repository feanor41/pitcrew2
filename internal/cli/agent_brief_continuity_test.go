package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/history"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestAionDirectDeliveryContinuityRequiresInspectionThenAuthorizesRevisionBoundUpdate(t *testing.T) {
	root := t.TempDir()
	start := writeInput(t, root, "start.json", `{"operation_key":"issue-177","route":"direct_inline","goal":"repair direct authority","route_reason":"bounded control-plane bug"}`)
	started := mustOK(t, runAt(t, root, "delivery", "start", "--actor", "aion", "--input-file", start))
	id := deliveryID(t, started)
	show := "delivery show --delivery-id " + id
	updateRevision1 := "delivery update --delivery-id " + id + " --revision 1 --actor aion --input-file <path>"

	assertAionContinuityAction(t, root, show)
	shown := mustOK(t, runAt(t, root, "delivery", "show", "--delivery-id", id))
	if !strings.Contains(string(shown), `"next_action":"delivery update --delivery-id `+id+` --revision 1 --actor aion --input-file`) {
		t.Fatalf("inspection did not return update authority: %s", shown)
	}
	assertAionContinuityAction(t, root, updateRevision1)
	assertAionContinuityAction(t, root, updateRevision1)

	checkpoint := writeInput(t, root, "checkpoint.json", `{"status":"in_progress","summary":"root cause verified","next_action":"implement fix"}`)
	mustOK(t, runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "1", "--actor", "aion", "--input-file", checkpoint))
	assertAionContinuityAction(t, root, "delivery show --delivery-id "+id)

	mustOK(t, runAt(t, root, "delivery", "show", "--delivery-id", id))
	updateRevision2 := "delivery update --delivery-id " + id + " --revision 2 --actor aion --input-file <path>"
	assertAionContinuityAction(t, root, updateRevision2)
	if stale := runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "1", "--actor", "aion", "--input-file", checkpoint); stale.code != 4 || !strings.Contains(stale.stderr, "delivery show --delivery-id "+id) {
		t.Fatalf("stale update did not return inspection recovery: %#v", stale)
	}

	completed := writeInput(t, root, "completed.json", `{"status":"completed","summary":"verified","next_action":"none"}`)
	mustOK(t, runAt(t, root, "delivery", "update", "--delivery-id", id, "--revision", "2", "--actor", "aion", "--input-file", completed))
	assertAionContinuityAction(t, root, "aion admit new delivery")

	daimon := runAt(t, root, "agent", "brief", "--role", "daimon", "--json")
	var daimonDocument struct {
		Data struct {
			Brief struct {
				Context  any `json:"context"`
				Contract struct {
					AllowedCommands []string `json:"allowed_commands"`
				} `json:"contract"`
			} `json:"brief"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(daimon.stdout), &daimonDocument); daimon.code != 0 || err != nil || daimonDocument.Data.Brief.Context != nil || len(daimonDocument.Data.Brief.Contract.AllowedCommands) != 0 {
		t.Fatalf("daimon gained direct mutation authority: %#v", daimon)
	}
}

func TestAionContinuityReadsPreInspectionSchemaBeforeShowMigratesIt(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`INSERT INTO direct_delivery_traces(id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at) VALUES('dl-111111111111111111111111','legacy-v7','direct_inline','legacy continuity','bounded','in_progress','','',1,'aion','aion','created','updated',NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`DROP TABLE direct_delivery_inspections`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`DELETE FROM schema_migrations WHERE version=8`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}

	id := "dl-111111111111111111111111"
	assertAionContinuityAction(t, root, "delivery show --delivery-id "+id)
	mustOK(t, runAt(t, root, "delivery", "show", "--delivery-id", id))
	assertAionContinuityAction(t, root, "delivery update --delivery-id "+id+" --revision 1 --actor aion --input-file <path>")
}

func assertAionContinuityAction(t *testing.T, root, want string) {
	t.Helper()
	jsonBrief := runAt(t, root, "agent", "brief", "--role", "aion", "--json")
	textBrief := runAt(t, root, "agent", "brief", "--role", "aion")
	if jsonBrief.code != 0 || textBrief.code != 0 {
		t.Fatalf("json=%#v text=%#v", jsonBrief, textBrief)
	}
	var document struct {
		Data struct {
			Brief struct {
				Context struct {
					AllowedActions []string `json:"allowed_actions"`
				} `json:"context"`
				NextAction string `json:"next_action"`
			} `json:"brief"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal([]byte(jsonBrief.stdout), &document); err != nil {
		t.Fatal(err)
	}
	brief := document.Data.Brief
	if brief.NextAction != want || document.NextAction != want || strings.Join(brief.Context.AllowedActions, ",") != want {
		t.Fatalf("json authority=%s want=%q", jsonBrief.stdout, want)
	}
	if !strings.Contains(textBrief.stdout, "next_action: "+want+"\n") || !strings.Contains(textBrief.stdout, `"allowed_actions":["`+strings.TrimSuffix(want, "<path>")) {
		t.Fatalf("text authority=%s want=%q", textBrief.stdout, want)
	}
}

func TestAgentBriefAndDeliveryActiveShareDurableContinuity(t *testing.T) {
	zeroRoot := t.TempDir()
	assertContinuityEquivalent(t, zeroRoot, 0, "aion admit new delivery", nil)
	if _, err := os.Stat(filepath.Join(zeroRoot, ".pitcrew")); !os.IsNotExist(err) {
		t.Fatalf("zero continuity initialized state: %v", err)
	}

	root := t.TempDir()
	s, err := store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-continuity',4,'planning','Durable workflow','prove durable continuity','2026-08-31T10:00:00Z','2026-08-31T10:01:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-continuity','progress','{"status":"blocked","summary":"contradictory prose","next_action":"ask user"}','aion',4,'2026-08-31T10:02:00Z')`,
	}
	for _, statement := range statements {
		if _, err = s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	assertContinuityEquivalent(t, root, 1, "delivery show --delivery-id wf-continuity", []continuityCandidate{{"wf-continuity", "workflow", 4, "planning", "workflow approve-plan"}})

	s, err = store.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO direct_delivery_traces(id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at) VALUES('dl-000000000000000000000099','continuity-direct','direct_inline','direct goal','bounded','interrupted','durable summary','delivery show --delivery-id dl-000000000000000000000099',2,'aion','aion','2026-08-31T11:00:00Z','2026-08-31T11:01:00Z',NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	want := []continuityCandidate{
		{"dl-000000000000000000000099", "direct_delivery", 2, "interrupted", "delivery show --delivery-id dl-000000000000000000000099"},
		{"wf-continuity", "workflow", 4, "planning", "workflow approve-plan"},
	}
	assertContinuityEquivalent(t, root, 2, "aion clarify delivery identity", want)
	first := runAt(t, root, "agent", "brief", "--role", "aion", "--json")
	second := runAt(t, root, "agent", "brief", "--role", "aion", "--json")
	if first != second {
		t.Fatalf("ambiguous continuity is not deterministic: %#v / %#v", first, second)
	}
}

type continuityCandidate struct {
	DeliveryID string `json:"delivery_id"`
	Kind       string `json:"kind"`
	Revision   int64  `json:"revision"`
	Status     string `json:"status"`
	NextAction string `json:"next_action"`
}

func assertContinuityEquivalent(t *testing.T, root string, count int, next string, want []continuityCandidate) {
	t.Helper()
	active := runAt(t, root, "delivery", "active")
	briefResult := runAt(t, root, "agent", "brief", "--role", "aion", "--json")
	if active.code != 0 || briefResult.code != 0 {
		t.Fatalf("active=%#v brief=%#v", active, briefResult)
	}
	var activeDoc struct {
		Data struct {
			Deliveries []history.Delivery `json:"deliveries"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	var briefDoc struct {
		Data struct {
			Brief struct {
				Context struct {
					Kind       string `json:"kind"`
					Continuity struct {
						Count      int                   `json:"count"`
						Candidates []continuityCandidate `json:"candidates"`
					} `json:"continuity"`
				} `json:"context"`
				NextAction string `json:"next_action"`
			} `json:"brief"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if json.Unmarshal([]byte(active.stdout), &activeDoc) != nil || json.Unmarshal([]byte(briefResult.stdout), &briefDoc) != nil {
		t.Fatal("invalid JSON")
	}
	got := briefDoc.Data.Brief.Context.Continuity
	if got.Count != count || len(activeDoc.Data.Deliveries) != count || briefDoc.Data.Brief.Context.Kind != "continuity" || activeDoc.NextAction != next || briefDoc.NextAction != next || briefDoc.Data.Brief.NextAction != next {
		t.Fatalf("continuity mismatch: active=%s brief=%s", active.stdout, briefResult.stdout)
	}
	if want == nil {
		want = []continuityCandidate{}
	}
	wantJSON, gotJSON := mustJSON(want), mustJSON(got.Candidates)
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Fatalf("candidates=%s want=%s", gotJSON, wantJSON)
	}
	for i, delivery := range activeDoc.Data.Deliveries {
		if delivery.ID != got.Candidates[i].DeliveryID || delivery.Revision != got.Candidates[i].Revision || delivery.Status != got.Candidates[i].Status || delivery.NextAction != got.Candidates[i].NextAction {
			t.Fatalf("rich/bounded candidate mismatch: %#v / %#v", delivery, got.Candidates[i])
		}
	}
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }
