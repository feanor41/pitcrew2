package agentbrief

import (
	"encoding/json"

	"github.com/fmazzalomo/pitcrew/internal/history"
)

type PhaseEntry struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	ParentID string          `json:"parent_id,omitempty"`
	Body     json.RawMessage `json:"body"`
}
type PhaseContext struct {
	WorkflowID string       `json:"workflow_id"`
	Revision   int64        `json:"revision"`
	State      string       `json:"state"`
	Entries    []PhaseEntry `json:"entries"`
}
type Coverage = history.UnitCoverage
type ScenarioResult struct {
	ScenarioID string `json:"scenario_id"`
	Status     string `json:"status"`
}
type EvidenceSummary struct {
	Revision          int64  `json:"revision"`
	RedOutcome        string `json:"red_outcome"`
	GreenOutcome      string `json:"green_outcome"`
	RefactorSummary   string `json:"refactor_summary"`
	ValidationOutcome string `json:"validation_outcome"`
}
type ReviewSummary struct {
	Revision   int64  `json:"revision"`
	Verdict    string `json:"verdict"`
	Summary    string `json:"summary"`
	Findings   string `json:"findings"`
	PlanImpact string `json:"plan_impact"`
}
type UnitContext struct {
	WorkflowID       string           `json:"workflow_id"`
	WorkflowRevision int64            `json:"workflow_revision"`
	UnitID           string           `json:"unit_id"`
	UnitRevision     int64            `json:"unit_revision"`
	State            string           `json:"state"`
	Description      string           `json:"description"`
	Scope            string           `json:"scope"`
	Areas            []string         `json:"areas"`
	DependsOn        []string         `json:"depends_on"`
	Coverage         []Coverage       `json:"coverage"`
	EvidenceRequired []string         `json:"evidence_required"`
	Evidence         *EvidenceSummary `json:"evidence,omitempty"`
	ScenarioResults  []ScenarioResult `json:"scenario_results,omitempty"`
	Review           *ReviewSummary   `json:"review,omitempty"`
}
type AggregateUnit struct {
	UnitID          string           `json:"unit_id"`
	UnitRevision    int64            `json:"unit_revision"`
	State           string           `json:"state"`
	Coverage        []Coverage       `json:"coverage"`
	ScenarioResults []ScenarioResult `json:"scenario_results,omitempty"`
}
type CorrectionAuthority struct {
	Allowed   int    `json:"allowed"`
	Used      int    `json:"used"`
	Authority string `json:"authority"`
}
type AggregateContext struct {
	WorkflowID          string               `json:"workflow_id"`
	Revision            int64                `json:"revision"`
	State               string               `json:"state"`
	Units               []AggregateUnit      `json:"units"`
	CorrectionAuthority *CorrectionAuthority `json:"correction_authority,omitempty"`
}

func (b Brief) WithPhase(projection history.Projection) Brief {
	phases := map[string]int{"exploration": 0, "specification": 1, "design": 2}
	phase := &PhaseContext{WorkflowID: projection.Workflow.ID, Revision: projection.Workflow.Revision, State: projection.Workflow.State, Entries: []PhaseEntry{}}
	for _, entry := range projection.Phase.Normative.Entries {
		if phases[entry.Phase] <= map[string]int{"pc2-explorer": 0, "pc2-specifier": 1, "pc2-designer": 2, "pc2-task-planner": 3}[b.Contract.Role] {
			phase.Entries = append(phase.Entries, PhaseEntry{entry.ID, entry.Kind, entry.ParentID, entry.Body})
		}
	}
	b.Context = &Context{Kind: "phase", Phase: phase}
	wantState := map[string]string{"pc2-explorer": "draft", "pc2-specifier": "exploring", "pc2-designer": "specifying", "pc2-task-planner": "designing"}[b.Contract.Role]
	b.NextAction = map[bool]string{true: b.NextAction, false: "return to aion"}[projection.Workflow.State == wantState]
	return b
}

func (b Brief) WithUnit(unitProjection, coordination, aggregate history.Projection, reviewer bool) Brief {
	definition := unitProjection.Unit.Definition
	unit := &UnitContext{WorkflowID: unitProjection.Workflow.ID, WorkflowRevision: unitProjection.Workflow.Revision, UnitID: definition.ID, UnitRevision: definition.Revision, State: definition.State, Description: definition.Description, Scope: definition.Scope, Areas: definition.Areas, DependsOn: definition.DependsOn, Coverage: coverage(definition.Coverage), EvidenceRequired: []string{"red-green TDD", "current affected-package verification", "result for every covered scenario"}}
	unit.ScenarioResults = scenarioResults(aggregate, definition.ID, definition.Revision)
	if reviewer {
		if evidence := unitProjection.Unit.Evidence; evidence != nil {
			unit.Evidence = &EvidenceSummary{evidence.Revision, evidence.RedOutcome, evidence.GreenOutcome, evidence.RefactorSummary, evidence.ValidationOutcome}
		}
		if review := unitProjection.Unit.Review; review != nil {
			unit.Review = &ReviewSummary{review.Revision, review.Verdict, review.Summary, review.Findings, review.PlanImpact}
		}
		b.NextAction = map[bool]string{true: "workflow unit-review", false: "return to aion"}[definition.State == "reviewing"]
	} else {
		b.NextAction = "return to aion"
		if coordination.Coordination != nil && coordination.Coordination.Current != nil && coordination.Coordination.Current.ID == definition.ID {
			switch coordination.Coordination.Current.Status {
			case "Correction":
				b.NextAction = "workflow recover-unit-claim"
			case "Claimed":
				b.NextAction = "workflow unit-tdd"
			}
		}
		if definition.State == "pending" && b.NextAction == "return to aion" {
			b.NextAction = "workflow claim-unit"
		}
	}
	b.Context = &Context{Kind: "unit", Unit: unit}
	return b
}

func (b Brief) WithAggregate(projection history.Projection) Brief {
	result := &AggregateContext{WorkflowID: projection.Workflow.ID, Revision: projection.Workflow.Revision, State: projection.Workflow.State, Units: []AggregateUnit{}}
	for _, projected := range projection.Aggregate.Units {
		d := projected.Definition
		result.Units = append(result.Units, AggregateUnit{d.ID, d.Revision, d.State, coverage(d.Coverage), scenarioResults(projection, d.ID, d.Revision)})
	}
	if projection.Aggregate.Correction != nil {
		result.CorrectionAuthority = &CorrectionAuthority{projection.Aggregate.Correction.Allowed, projection.Aggregate.Correction.Used, projection.Aggregate.Correction.Authority}
	}
	b.Context = &Context{Kind: "aggregate", Aggregate: result}
	b.NextAction = map[bool]string{true: "workflow complete", false: "return to aion"}[projection.Workflow.State == "ready_to_complete"]
	return b
}

func coverage(values []history.UnitCoverage) []Coverage {
	result := make([]Coverage, 0, len(values))
	for _, value := range values {
		result = append(result, Coverage{value.RequirementID, value.ScenarioID})
	}
	return result
}
func scenarioResults(projection history.Projection, unitID string, revision int64) []ScenarioResult {
	var result []ScenarioResult
	if projection.Aggregate == nil {
		return result
	}
	seen := map[string]bool{}
	for _, run := range projection.Aggregate.Verification {
		if run.UnitID != unitID || run.UnitRevision == nil || *run.UnitRevision != revision {
			continue
		}
		var ids []string
		_ = json.Unmarshal(run.ScenarioIDs, &ids)
		for _, id := range ids {
			if !seen[id] {
				result = append(result, ScenarioResult{id, run.Outcome})
				seen[id] = true
			}
		}
	}
	return result
}
