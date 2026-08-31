package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/history"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

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
