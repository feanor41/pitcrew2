package history

import "encoding/json"

type UnitDefinition struct {
	ID                     string         `json:"id"`
	Description            string         `json:"description"`
	Scope                  string         `json:"scope"`
	Areas                  []string       `json:"areas"`
	DependsOn              []string       `json:"depends_on"`
	EstimatedChangedLines  int            `json:"estimated_changed_lines"`
	EstimatedReviewMinutes int            `json:"estimated_review_minutes"`
	State                  string         `json:"state"`
	Revision               int64          `json:"revision"`
	Coverage               []UnitCoverage `json:"coverage,omitempty"`
}

type UnitCoverage struct {
	RequirementID string `json:"requirement_id"`
	ScenarioID    string `json:"scenario_id"`
}

type UnitEvidence struct {
	Revision          int64  `json:"revision"`
	RedCommand        string `json:"red_command"`
	RedOutcome        string `json:"red_outcome"`
	GreenCommand      string `json:"green_command"`
	GreenOutcome      string `json:"green_outcome"`
	RefactorSummary   string `json:"refactor_summary"`
	ValidationCommand string `json:"validation_command"`
	ValidationOutcome string `json:"validation_outcome"`
	ChangedPaths      string `json:"changed_paths"`
	RecordedAt        string `json:"recorded_at"`
}

type UnitReview struct {
	Revision   int64  `json:"revision"`
	Verdict    string `json:"verdict"`
	Summary    string `json:"summary"`
	Findings   string `json:"findings"`
	PlanImpact string `json:"plan_impact"`
	RecordedAt string `json:"recorded_at"`
}

type UnitProjection struct {
	Definition           UnitDefinition `json:"definition"`
	Evidence             *UnitEvidence  `json:"evidence,omitempty"`
	Review               *UnitReview    `json:"review,omitempty"`
	ClaimReleasedCurrent bool           `json:"-"`
}

type PlanProjection struct {
	Summary          string `json:"summary"`
	Scope            string `json:"scope"`
	MaxParallelUnits int    `json:"max_parallel_units"`
	Body             string `json:"body"`
}

type VerificationProjection struct {
	ID                    string          `json:"id"`
	UnitID                string          `json:"unit_id,omitempty"`
	UnitRevision          *int64          `json:"unit_revision,omitempty"`
	Tier                  string          `json:"tier"`
	Command               string          `json:"command"`
	Outcome               string          `json:"outcome"`
	RepositoryFingerprint string          `json:"repository_fingerprint"`
	ScenarioIDs           json.RawMessage `json:"scenario_ids"`
	ReusedFromID          string          `json:"reused_from_id,omitempty"`
	RecordedAt            string          `json:"recorded_at"`
}

type CheckpointProjection struct {
	AggregateRevision int64   `json:"aggregate_revision"`
	ProjectID         string  `json:"project_id"`
	CheckoutRoot      string  `json:"checkout_root"`
	BaseRevision      string  `json:"base_revision"`
	HeadRevision      string  `json:"head_revision"`
	ResultDigest      string  `json:"result_digest"`
	Dirty             bool    `json:"dirty"`
	CommitRef         *string `json:"commit_ref,omitempty"`
	DeliveryID        *string `json:"delivery_id,omitempty"`
	RecordedAt        string  `json:"recorded_at"`
}

type AggregateProjection struct {
	Normative    NormativeProjection      `json:"normative"`
	Plan         *PlanProjection          `json:"plan,omitempty"`
	Units        []UnitProjection         `json:"units,omitempty"`
	Correction   *CorrectionStatus        `json:"correction,omitempty"`
	Verification []VerificationProjection `json:"verification,omitempty"`
	Checkpoint   *CheckpointProjection    `json:"checkpoint,omitempty"`
}
