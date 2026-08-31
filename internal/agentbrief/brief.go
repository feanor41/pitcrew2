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

	"github.com/fmazzalomo/pitcrew/internal/history"
)

const ContractVersion = "1"

type StableContract struct {
	Role             string   `json:"role"`
	Identity         string   `json:"identity"`
	Responsibilities []string `json:"responsibilities"`
	AllowedHandoffs  []string `json:"allowed_handoffs"`
	AllowedCommands  []string `json:"allowed_commands"`
	Invariants       []string `json:"invariants"`
	BriefRequirement string   `json:"brief_requirement"`
}

type Context struct {
	Kind           string                    `json:"kind,omitempty"`
	AllowedActions []string                  `json:"allowed_actions"`
	WorkflowID     string                    `json:"workflow_id,omitempty"`
	UnitID         string                    `json:"unit_id,omitempty"`
	Continuity     *history.ActiveContinuity `json:"continuity,omitempty"`
	Coordination   *CoordinationContext      `json:"coordination,omitempty"`
	Phase          *PhaseContext             `json:"phase,omitempty"`
	Unit           *UnitContext              `json:"unit,omitempty"`
	Aggregate      *AggregateContext         `json:"aggregate,omitempty"`
}

func (b Brief) WithContinuity(continuity history.ActiveContinuity) Brief {
	b.Context, b.NextAction = &Context{Kind: "continuity", AllowedActions: allowedActions(continuity.NextAction), Continuity: &continuity}, continuity.NextAction
	return b
}

type Brief struct {
	ContractVersion string         `json:"contract_version"`
	ContractDigest  string         `json:"contract_digest"`
	Contract        StableContract `json:"contract"`
	Context         *Context       `json:"context,omitempty"`
	NextAction      string         `json:"next_action"`
}

func New(role, workflowID, unitID string) (Brief, error) {
	contract, ok := contracts()[role]
	if !ok {
		return Brief{}, fmt.Errorf("unknown role %q", role)
	}
	if err := validateContext(role, workflowID, unitID); err != nil {
		return Brief{}, err
	}
	canonical, err := json.Marshal(struct {
		ContractVersion string         `json:"contract_version"`
		Contract        StableContract `json:"contract"`
	}{ContractVersion: ContractVersion, Contract: contract})
	if err != nil {
		return Brief{}, err
	}
	sum := sha256.Sum256(canonical)
	brief := Brief{ContractVersion: ContractVersion, ContractDigest: hex.EncodeToString(sum[:]), Contract: contract, NextAction: nextAction(role, workflowID)}
	if workflowID != "" || unitID != "" {
		brief.Context = &Context{WorkflowID: workflowID, UnitID: unitID}
	}
	return brief, nil
}

func WriteText(w io.Writer, brief Brief) error {
	c := brief.Contract
	if _, err := fmt.Fprintf(w, "PitCrew agent brief\ncontract_version: %s\ncontract_digest: %s\nrole: %s\nidentity: %s\nresponsibilities: %s\nallowed_handoffs: %s\nallowed_commands: %s\ninvariants: %s\nbrief_requirement: %s\n", brief.ContractVersion, brief.ContractDigest, c.Role, c.Identity, strings.Join(c.Responsibilities, "; "), strings.Join(c.AllowedHandoffs, ", "), strings.Join(c.AllowedCommands, ", "), strings.Join(c.Invariants, "; "), c.BriefRequirement); err != nil {
		return err
	}
	if brief.Context != nil {
		if _, err := fmt.Fprintf(w, "context_kind: %s\nworkflow_id: %s\nunit_id: %s\n", brief.Context.Kind, brief.Context.WorkflowID, brief.Context.UnitID); err != nil {
			return err
		}
		if brief.Context.Continuity != nil {
			if _, err := fmt.Fprintf(w, "continuity_count: %d\ncontinuity_candidates: %s\n", brief.Context.Continuity.Count, formatCandidates(brief.Context.Continuity.Candidates)); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(brief.Context)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "dynamic_context: %s\n", encoded); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "next_action: %s\n", brief.NextAction)
	return err
}

func formatCandidates(candidates []history.ActiveCandidate) string {
	items := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, fmt.Sprintf("%s@%d:%s:%s", candidate.DeliveryID, candidate.Revision, candidate.Status, candidate.NextAction))
	}
	return strings.Join(items, ", ")
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

func nextAction(role, workflowID string) string {
	switch role {
	case "daimon":
		return "handoff to aion"
	case "aion":
		if workflowID == "" {
			return "aion admit new delivery"
		}
		return "workflow show"
	case "pc2-sdd-initializer":
		return "await aion context request"
	case "pc2-explorer":
		return "workflow explore"
	case "pc2-specifier":
		return "workflow spec"
	case "pc2-designer":
		return "workflow design"
	case "pc2-task-planner":
		return "workflow plan"
	default:
		return "workflow show"
	}
}

func contracts() map[string]StableContract {
	invariants := []string{"technical English internally", "truthful evidence and progress", "never expose opaque handle contents", "allowed_commands is potential interface only; current authority is conveyed only by dynamic next_action and allowed_actions"}
	c := func(role, identity, requirement string, responsibilities, handoffs, commands []string) StableContract {
		return StableContract{Role: role, Identity: identity, Responsibilities: responsibilities, AllowedHandoffs: handoffs, AllowedCommands: commands, Invariants: invariants, BriefRequirement: requirement}
	}
	aionHandoffs := []string{"daimon", "pc2-explorer", "pc2-specifier", "pc2-designer", "pc2-task-planner", "pc2-implementer", "pc2-reviewer", "pc2-sdd-initializer"}
	return map[string]StableContract{
		"daimon":              c("daimon", "Daimon", "no workflow or unit context", []string{"own the user conversation", "relay only Aion-acknowledged facts"}, []string{"aion"}, []string{}),
		"aion":                c("aion", "Aion", "optional workflow context; no unit context", []string{"own routing, admission, continuity, and orchestration authority"}, aionHandoffs, []string{"delivery", "workflow"}),
		"pc2-sdd-initializer": c("pc2-sdd-initializer", "SDD Initializer", "no workflow or unit context", []string{"initialize missing project context once when Aion requests it"}, []string{"aion"}, []string{"context inspect", "context initialize", "context record"}),
		"pc2-explorer":        c("pc2-explorer", "Explorer", "workflow context required; no unit context", []string{"explore the accepted workflow scope and return repository evidence"}, []string{"aion"}, []string{"workflow show", "workflow explore"}),
		"pc2-specifier":       c("pc2-specifier", "Specifier", "workflow context required; no unit context", []string{"write executable requirements and scenarios for the accepted workflow"}, []string{"aion"}, []string{"workflow show", "workflow spec"}),
		"pc2-designer":        c("pc2-designer", "Designer", "workflow context required; no unit context", []string{"design the accepted workflow proportionally from repository evidence"}, []string{"aion"}, []string{"workflow show", "workflow design"}),
		"pc2-task-planner":    c("pc2-task-planner", "Task Planner", "workflow context required; no unit context", []string{"create short dependency-ordered work units with explicit coverage"}, []string{"aion"}, []string{"workflow show", "workflow plan"}),
		"pc2-implementer":     c("pc2-implementer", "Implementer", "workflow and unit context required", []string{"implement one claimed unit with current TDD evidence"}, []string{"aion"}, []string{"workflow show", "workflow list-ready-units", "workflow claim-unit", "workflow unit-tdd", "workflow unit-complete"}),
		"pc2-reviewer":        c("pc2-reviewer", "Reviewer", "workflow context required; optional unit context", []string{"independently review one unit or the aggregate result"}, []string{"aion"}, []string{"workflow show", "workflow unit-review", "workflow complete"}),
	}
}
