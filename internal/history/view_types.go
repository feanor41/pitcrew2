package history

import "github.com/fmazzalomo/pitcrew/internal/workflow"

// View selects one deliberately bounded workflow projection. Audit is the
// only view that loads the historical record graph.
type View string

const (
	ViewCoordination View = "coordination"
	ViewPhase        View = "phase"
	ViewUnit         View = "unit"
	ViewAggregate    View = "aggregate"
	ViewAudit        View = "audit"
)

type WorkflowIdentity struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	State    string `json:"state"`
}

type Coordination struct {
	NextAction          string            `json:"next_action"`
	Current             *UnitStatus       `json:"current,omitempty"`
	Ready               []UnitStatus      `json:"ready,omitempty"`
	Blocker             *UnitStatus       `json:"blocker,omitempty"`
	CorrectionAuthority *CorrectionStatus `json:"correction_authority,omitempty"`
	LatestProgress      *Progress         `json:"latest_progress,omitempty"`
}

type ArtifactProvenance struct {
	WorkflowID string `json:"workflow_id"`
	ArtifactID int64  `json:"artifact_id"`
	Revision   int64  `json:"accepted_revision"`
	Actor      string `json:"actor"`
	RecordedAt string `json:"recorded_at"`
}

type AcceptedArtifact struct {
	Kind    string             `json:"kind"`
	Content string             `json:"content"`
	Source  ArtifactProvenance `json:"source"`
}

// NormativeProjection is a bounded compatibility union. Typed workflows emit
// resolved entries; opaque workflows emit their accepted stage artifacts
// without attempting to infer structure from prose.
type NormativeProjection struct {
	WorkflowID string                            `json:"workflow_id"`
	Structured bool                              `json:"structured"`
	Baseline   *workflow.BaselineIdentity        `json:"baseline,omitempty"`
	Entries    []workflow.ResolvedNormativeEntry `json:"entries"`
	Artifacts  []AcceptedArtifact                `json:"artifacts,omitempty"`
}

type PhaseProjection struct {
	Normative NormativeProjection `json:"normative"`
}

// Projection is a tagged union. Only the selected view pointer is populated;
// fields from the audit graph cannot accidentally enter bounded JSON.
type Projection struct {
	View         View                 `json:"view"`
	Workflow     WorkflowIdentity     `json:"workflow"`
	Coordination *Coordination        `json:"coordination,omitempty"`
	Phase        *PhaseProjection     `json:"phase,omitempty"`
	Unit         *UnitProjection      `json:"unit,omitempty"`
	Aggregate    *AggregateProjection `json:"aggregate,omitempty"`
	Audit        *Detail              `json:"audit,omitempty"`
}
