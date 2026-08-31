// Package agentbrief defines the stable, role-scoped bootstrap contract used by
// agents before they inspect project state.
package agentbrief

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const ContractVersion = "agent-brief/v1"

type Contract struct {
	ContractVersion string   `json:"contract_version"`
	Role            string   `json:"role"`
	Responsibility  string   `json:"responsibility"`
	Contacts        []string `json:"contacts"`
	AllowedCommands []string `json:"allowed_commands"`
	Invariants      []string `json:"invariants"`
	ContextPolicy   string   `json:"context_policy"`
}

type Brief struct {
	Contract
	ContractDigest string `json:"contract_digest"`
	WorkflowID     string `json:"workflow_id,omitempty"`
	UnitID         string `json:"unit_id,omitempty"`
}

func New(role, workflowID, unitID string) (Brief, error) {
	contract, ok := contracts()[role]
	if !ok {
		return Brief{}, fmt.Errorf("unknown role %q", role)
	}
	if err := validateContext(role, workflowID, unitID); err != nil {
		return Brief{}, err
	}
	canonical, err := json.Marshal(contract)
	if err != nil {
		return Brief{}, err
	}
	sum := sha256.Sum256(canonical)
	return Brief{Contract: contract, ContractDigest: hex.EncodeToString(sum[:]), WorkflowID: workflowID, UnitID: unitID}, nil
}

func WriteText(w io.Writer, brief Brief) error {
	_, err := fmt.Fprintf(w, "PitCrew agent brief\ncontract_version: %s\ncontract_digest: %s\nrole: %s\nresponsibility: %s\ncontacts: %s\nallowed_commands: %s\ninvariants: %s\ncontext_policy: %s\nworkflow_id: %s\nunit_id: %s\n",
		brief.ContractVersion, brief.ContractDigest, brief.Role, brief.Responsibility,
		strings.Join(brief.Contacts, ", "), strings.Join(brief.AllowedCommands, ", "),
		strings.Join(brief.Invariants, "; "), brief.ContextPolicy, brief.WorkflowID, brief.UnitID)
	return err
}

func validateContext(role, workflowID, unitID string) error {
	switch role {
	case "daimon", "pc2-sdd-initializer":
		if workflowID != "" || unitID != "" {
			return fmt.Errorf("role %s forbids workflow and unit context", role)
		}
	case "aion":
		if unitID != "" {
			return fmt.Errorf("role aion forbids unit-scoped context")
		}
	case "pc2-explorer", "pc2-specifier", "pc2-designer", "pc2-task-planner":
		if workflowID == "" || unitID != "" {
			return fmt.Errorf("role %s requires workflow context and forbids unit context", role)
		}
	case "pc2-implementer":
		if workflowID == "" || unitID == "" {
			return fmt.Errorf("role pc2-implementer requires workflow and unit context")
		}
	case "pc2-reviewer":
		if workflowID == "" {
			return fmt.Errorf("role pc2-reviewer requires workflow context")
		}
	}
	return nil
}

func contracts() map[string]Contract {
	makeContract := func(role, responsibility, policy string, contacts, commands []string) Contract {
		return Contract{
			ContractVersion: ContractVersion, Role: role, Responsibility: responsibility,
			Contacts: contacts, AllowedCommands: commands,
			Invariants:    []string{"technical English internally", "truthful evidence and progress", "never expose opaque handle contents"},
			ContextPolicy: policy,
		}
	}
	return map[string]Contract{
		"daimon":              makeContract("daimon", "Own the user conversation and relay only Aion-acknowledged facts.", "no workflow or unit context", []string{"aion"}, nil),
		"aion":                makeContract("aion", "Own routing, admission, continuity, and orchestration authority.", "optional workflow context; no unit context", []string{"daimon", "pc2 specialists"}, []string{"delivery", "workflow"}),
		"pc2-sdd-initializer": makeContract("pc2-sdd-initializer", "Initialize missing project context once when Aion requests it.", "no workflow or unit context", []string{"aion"}, []string{"context inspect", "context initialize", "context record"}),
		"pc2-explorer":        makeContract("pc2-explorer", "Explore the accepted workflow scope and return repository evidence.", "workflow context required; no unit context", []string{"aion"}, []string{"workflow show", "workflow explore"}),
		"pc2-specifier":       makeContract("pc2-specifier", "Write executable requirements and scenarios for the accepted workflow.", "workflow context required; no unit context", []string{"aion"}, []string{"workflow show", "workflow spec"}),
		"pc2-designer":        makeContract("pc2-designer", "Design the accepted workflow proportionally from repository evidence.", "workflow context required; no unit context", []string{"aion"}, []string{"workflow show", "workflow design"}),
		"pc2-task-planner":    makeContract("pc2-task-planner", "Create short dependency-ordered work units with explicit coverage.", "workflow context required; no unit context", []string{"aion"}, []string{"workflow show", "workflow plan"}),
		"pc2-implementer":     makeContract("pc2-implementer", "Implement one claimed unit with current TDD evidence.", "workflow and unit context required", []string{"aion"}, []string{"workflow show", "workflow list-ready-units", "workflow claim-unit", "workflow unit-tdd", "workflow unit-complete"}),
		"pc2-reviewer":        makeContract("pc2-reviewer", "Independently review one unit or the aggregate result.", "workflow context required; optional unit context", []string{"aion"}, []string{"workflow show", "workflow unit-review", "workflow complete"}),
	}
}
