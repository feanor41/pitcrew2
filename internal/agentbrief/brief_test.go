package agentbrief

import (
	"strings"
	"testing"
)

func TestStableContractsCarryBootstrapMechanicsNotRuntimePrompts(t *testing.T) {
	cases := []struct {
		role, workflowID, unitID string
		must                     []string
	}{
		{role: "daimon", must: []string{"stay addressable", "exactly one Aion", "mutate no workflow or repository state", "Aion-acknowledged facts"}},
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
