package agentbrief

import (
	"encoding/json"

	"github.com/fmazzalomo/pitcrew/internal/evidence"
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
type CoordinationUnit struct {
	UnitID  string `json:"unit_id,omitempty"`
	Status  string `json:"status"`
	Attempt int64  `json:"attempt,omitempty"`
}
type CoordinationContext struct {
	WorkflowID          string               `json:"workflow_id"`
	Revision            int64                `json:"revision"`
	State               string               `json:"state"`
	Current             *CoordinationUnit    `json:"current,omitempty"`
	Ready               []CoordinationUnit   `json:"ready"`
	Blocker             *CoordinationUnit    `json:"blocker,omitempty"`
	CorrectionAuthority *CorrectionAuthority `json:"correction_authority,omitempty"`
}
type Coverage = history.UnitCoverage
type ScenarioResult struct {
	ScenarioID string `json:"scenario_id"`
	Status     string `json:"status"`
}
type EvidenceSummary struct {
	Revision         int64  `json:"revision"`
	RedStatus        string `json:"red_status"`
	GreenStatus      string `json:"green_status"`
	ValidationStatus string `json:"validation_status"`
	RefactorSummary  string `json:"refactor_summary"`
}
type ReviewSummary struct {
	Revision   int64  `json:"revision"`
	Verdict    string `json:"verdict"`
	Summary    string `json:"summary"`
	Findings   string `json:"findings"`
	PlanImpact string `json:"plan_impact,omitempty"`
}
type UnitContext struct {
	WorkflowID       string           `json:"workflow_id"`
	WorkflowRevision int64            `json:"workflow_revision"`
	UnitID           string           `json:"unit_id"`
	UnitRevision     int64            `json:"unit_revision"`
	State            string           `json:"state"`
	WorkSummary      string           `json:"work_summary"`
	DependsOn        []string         `json:"depends_on"`
	Coverage         []Coverage       `json:"coverage"`
	EvidenceRequired []string         `json:"evidence_required"`
	Evidence         *EvidenceSummary `json:"evidence,omitempty"`
	ScenarioResults  []ScenarioResult `json:"scenario_results,omitempty"`
	Review           *ReviewSummary   `json:"review,omitempty"`
}

func (b Brief) WithCoordination(projection history.Projection) Brief {
	source := projection.Coordination
	result := &CoordinationContext{WorkflowID: projection.Workflow.ID, Revision: projection.Workflow.Revision, State: projection.Workflow.State, Ready: []CoordinationUnit{}}
	if source != nil {
		result.Current = coordinationUnit(source.Current)
		result.Blocker = coordinationUnit(source.Blocker)
		for _, ready := range source.Ready {
			result.Ready = append(result.Ready, *coordinationUnit(&ready))
		}
		if source.CorrectionAuthority != nil {
			result.CorrectionAuthority = &CorrectionAuthority{Allowed: source.CorrectionAuthority.Allowed, Used: source.CorrectionAuthority.Used, Authority: source.CorrectionAuthority.Authority}
		}
		b.NextAction = source.NextAction
	}
	b.Context = &Context{Kind: "coordination", AllowedActions: allowedActions(b.NextAction), Coordination: result}
	return b
}

func coordinationUnit(source *history.UnitStatus) *CoordinationUnit {
	if source == nil {
		return nil
	}
	return &CoordinationUnit{UnitID: source.ID, Status: source.Status, Attempt: source.Attempt}
}

type AggregateUnit struct {
	UnitID          string           `json:"unit_id"`
	UnitRevision    int64            `json:"unit_revision"`
	State           string           `json:"state"`
	WorkSummary     string           `json:"work_summary"`
	Coverage        []Coverage       `json:"coverage"`
	Evidence        *EvidenceSummary `json:"evidence,omitempty"`
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
			phase.Entries = append(phase.Entries, PhaseEntry{ID: entry.ID, Kind: entry.Kind, ParentID: entry.ParentID, Body: sanitizeBody(entry.Body)})
		}
	}
	b.Context = &Context{Kind: "phase", Phase: phase}
	wantState := map[string]string{"pc2-explorer": "draft", "pc2-specifier": "exploring", "pc2-designer": "specifying", "pc2-task-planner": "designing"}[b.Contract.Role]
	b.NextAction = map[bool]string{true: b.NextAction, false: "return to aion"}[projection.Workflow.State == wantState]
	b.Context.AllowedActions = allowedActions(b.NextAction)
	return b
}

func (b Brief) WithUnit(unitProjection, coordination, aggregate history.Projection, reviewer bool) Brief {
	definition := unitProjection.Unit.Definition
	unit := &UnitContext{WorkflowID: unitProjection.Workflow.ID, WorkflowRevision: unitProjection.Workflow.Revision, UnitID: definition.ID, UnitRevision: definition.Revision, State: definition.State, WorkSummary: workSummary(definition.Description), DependsOn: definition.DependsOn, Coverage: coverage(definition.Coverage), EvidenceRequired: []string{"red", "green", "validation", "scenario_results"}}
	unit.ScenarioResults = scenarioResults(aggregate, definition.ID, definition.Revision)
	if reviewer {
		if evidence := unitProjection.Unit.Evidence; evidence != nil {
			unit.Evidence = evidenceSummary(evidence)
		}
		if review := unitProjection.Unit.Review; review != nil {
			unit.Review = &ReviewSummary{Revision: review.Revision, Verdict: closedVerdict(review.Verdict), Summary: sanitizeNarrative(review.Summary), Findings: sanitizeNarrative(review.Findings), PlanImpact: closedPlanImpact(review.PlanImpact)}
		}
		b.NextAction = map[bool]string{true: "workflow unit-review", false: "return to aion"}[unitProjection.Workflow.State == "implementing" && definition.State == "reviewing"]
	} else {
		b.NextAction = "return to aion"
		if unitProjection.Workflow.State == "implementing" && coordination.Coordination != nil && coordination.Coordination.Current != nil && coordination.Coordination.Current.ID == definition.ID {
			switch coordination.Coordination.Current.Status {
			case "Correction":
				b.NextAction = "workflow recover-unit-claim"
			case "Claimed":
				b.NextAction = "workflow unit-tdd"
			}
		}
		if unitProjection.Workflow.State == "implementing" && definition.State == "pending" && b.NextAction == "return to aion" && coordination.Coordination != nil {
			for _, ready := range coordination.Coordination.Ready {
				if ready.ID == definition.ID {
					b.NextAction = "workflow claim-unit"
				}
			}
		}
	}
	b.Context = &Context{Kind: "unit", AllowedActions: allowedActions(b.NextAction), Unit: unit}
	return b
}

func (b Brief) WithAggregate(projection history.Projection) Brief {
	result := &AggregateContext{WorkflowID: projection.Workflow.ID, Revision: projection.Workflow.Revision, State: projection.Workflow.State, Units: []AggregateUnit{}}
	for _, projected := range projection.Aggregate.Units {
		d := projected.Definition
		result.Units = append(result.Units, AggregateUnit{UnitID: d.ID, UnitRevision: d.Revision, State: d.State, WorkSummary: workSummary(d.Description), Coverage: coverage(d.Coverage), Evidence: evidenceSummary(projected.Evidence), ScenarioResults: scenarioResults(projection, d.ID, d.Revision)})
	}
	if projection.Aggregate.Correction != nil {
		result.CorrectionAuthority = &CorrectionAuthority{Allowed: projection.Aggregate.Correction.Allowed, Used: projection.Aggregate.Correction.Used, Authority: projection.Aggregate.Correction.Authority}
	}
	b.NextAction = map[bool]string{true: "workflow complete", false: "return to aion"}[projection.Workflow.State == "ready_to_complete"]
	b.Context = &Context{Kind: "aggregate", AllowedActions: allowedActions(b.NextAction), Aggregate: result}
	return b
}

func coverage(values []history.UnitCoverage) []Coverage {
	result := make([]Coverage, 0, len(values))
	for _, value := range values {
		result = append(result, Coverage{RequirementID: value.RequirementID, ScenarioID: value.ScenarioID})
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
				result = append(result, ScenarioResult{ScenarioID: id, Status: outcomeStatus(run.Outcome)})
				seen[id] = true
			}
		}
	}
	return result
}

func outcomeStatus(outcome string) string {
	exit, ok := evidence.ParseOutcome(outcome)
	if !ok {
		return "unknown"
	}
	if exit == 0 {
		return "passed"
	}
	return "failed"
}

func evidenceSummary(source *history.UnitEvidence) *EvidenceSummary {
	if source == nil {
		return nil
	}
	return &EvidenceSummary{Revision: source.Revision, RedStatus: outcomeStatus(source.RedOutcome), GreenStatus: outcomeStatus(source.GreenOutcome), ValidationStatus: outcomeStatus(source.ValidationOutcome), RefactorSummary: sanitizeNarrative(source.RefactorSummary)}
}

func closedVerdict(verdict string) string {
	if verdict == "approved" || verdict == "corrections" {
		return verdict
	}
	return "unknown"
}

func closedPlanImpact(value string) string {
	if value == "inside" || value == "outside" {
		return value
	}
	return ""
}

func allowedActions(action string) []string {
	switch action {
	case "", "none", "return to aion", "handoff to aion", "await aion context request":
		return []string{}
	default:
		return []string{action}
	}
}
