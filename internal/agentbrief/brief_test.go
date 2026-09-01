package agentbrief

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/maxims"
)

func TestEveryBriefCarriesTheCanonicalSharedMaximsContract(t *testing.T) {
	roles := []struct {
		role, workflowID, unitID string
	}{
		{role: "daimon"},
		{role: "aion"},
		{role: "pc2-explorer", workflowID: "wf-x"},
		{role: "pc2-specifier", workflowID: "wf-x"},
		{role: "pc2-designer", workflowID: "wf-x"},
		{role: "pc2-task-planner", workflowID: "wf-x"},
		{role: "pc2-implementer", workflowID: "wf-x", unitID: "wu-x"},
		{role: "pc2-reviewer", workflowID: "wf-x"},
		{role: "pc2-sdd-initializer"},
	}

	var sharedDigest string
	for _, tc := range roles {
		brief, err := New(tc.role, tc.workflowID, tc.unitID)
		if err != nil {
			t.Fatal(err)
		}
		shared := brief.SharedContract
		if shared.ContractVersion != SharedContractVersion || shared.Maxims != maxims.Text() || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(shared.ContractDigest) {
			t.Fatalf("%s shared contract = %#v", tc.role, shared)
		}
		canonical, err := json.Marshal(struct {
			ContractVersion string `json:"contract_version"`
			Maxims          string `json:"maxims"`
		}{ContractVersion: SharedContractVersion, Maxims: maxims.Text()})
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("%x", sha256.Sum256(canonical)); shared.ContractDigest != want {
			t.Fatalf("%s shared digest=%s want=%s", tc.role, shared.ContractDigest, want)
		}
		if sharedDigest == "" {
			sharedDigest = shared.ContractDigest
		} else if shared.ContractDigest != sharedDigest {
			t.Fatalf("%s shared digest=%s want common=%s", tc.role, shared.ContractDigest, sharedDigest)
		}
	}
}

func TestStableContractsCarryBootstrapMechanicsNotRuntimePrompts(t *testing.T) {
	cases := []struct {
		role, workflowID, unitID string
		must                     []string
	}{
		{role: "daimon", must: []string{
			"retain the active user-visible turn while Aion remains active",
			"host-native mailbox and user steering",
			"meaningful Aion-acknowledged fact exactly once",
			"wait no longer than five minutes before one truthful quiet notice per continuous quiet interval",
			"steered input to Aion as requested state",
			"completed, interrupted, cancelled, timed-out, failed, blocked, needs-user, user-owned-gate, or abandoned outcome",
			"never promise a future unsolicited update after finalizing unless the host provides a push channel",
			"disclose missing host liveness instead of simulating it",
			"exactly one Aion",
			"mutate no workflow or repository state",
		}},
		{role: "aion", must: []string{"inspect active continuity first", "admit exactly once before mutation", "retain and resume one delivery identity", "delegate only the seven specialists", "never invent status or authority"}},
		{role: "pc2-explorer", workflowID: "wf-x", must: []string{"retrieve the scoped brief before action", "dynamic allowed_actions", "return to Aion", "never delegate"}},
		{role: "pc2-implementer", workflowID: "wf-x", unitID: "wu-x", must: []string{"retrieve the scoped brief before action", "dynamic allowed_actions", "return to Aion", "never delegate"}},
		{role: "pc2-reviewer", workflowID: "wf-x", must: []string{"retrieve the scoped brief before action", "dynamic allowed_actions", "return to Aion", "never delegate"}},
	}
	for _, tc := range cases {
		brief, err := New(tc.role, tc.workflowID, tc.unitID)
		if err != nil {
			t.Fatal(err)
		}
		contract := strings.Join(append(append([]string{}, brief.Contract.Responsibilities...), brief.Contract.Invariants...), "; ")
		for _, required := range tc.must {
			if !strings.Contains(contract, required) {
				t.Fatalf("%s stable contract omitted %q: %s", tc.role, required, contract)
			}
		}
	}
}
