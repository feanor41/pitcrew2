package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/checkpoint"
	"github.com/fmazzalomo/pitcrew/internal/correction"
	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/project"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

var (
	ErrInvalidState   = errors.New("invalid unit state")
	ErrInvalidHandle  = errors.New("invalid claim handle")
	ErrReviewRequired = errors.New("approved review required")
)

type DB = *sql.DB
type Verdict string
type PlanImpact string
type VerificationTier string

const (
	Approved    Verdict    = "approved"
	Corrections Verdict    = "corrections"
	Inside      PlanImpact = "inside"
	Outside     PlanImpact = "outside"

	Focused         VerificationTier = "focused"
	AffectedPackage VerificationTier = "affected_package"
	AggregateFull   VerificationTier = "aggregate_full"
	PublicationFull VerificationTier = "publication_full"
)

type VerificationRun struct {
	ID                    string           `json:"id"`
	Tier                  VerificationTier `json:"tier"`
	Command               string           `json:"command"`
	Outcome               string           `json:"outcome"`
	RepositoryFingerprint string           `json:"repository_fingerprint"`
	ScenarioIDs           []string         `json:"scenario_ids"`
	ReusedFromID          string           `json:"reused_from_id,omitempty"`
}

type ScenarioResult struct {
	ScenarioID     string `json:"scenario_id"`
	Outcome        string `json:"outcome"`
	VerificationID string `json:"verification_id"`
}

type ReviewedCheckpoint struct {
	ProjectID    string  `json:"project_id"`
	CheckoutRoot string  `json:"checkout_root"`
	BaseRevision string  `json:"base_revision"`
	HeadRevision string  `json:"head_revision"`
	ResultDigest string  `json:"result_digest"`
	Dirty        bool    `json:"dirty"`
	CommitRef    *string `json:"commit_ref,omitempty"`
	DeliveryID   *string `json:"delivery_id,omitempty"`
}

type TDDRecord struct {
	RedCommand        string            `json:"red_command"`
	RedOutcome        string            `json:"red_outcome"`
	GreenCommand      string            `json:"green_command"`
	GreenOutcome      string            `json:"green_outcome"`
	RefactorSummary   string            `json:"refactor_summary"`
	ValidationCommand string            `json:"validation_command"`
	ValidationOutcome string            `json:"validation_outcome"`
	ChangedPaths      string            `json:"changed_paths"`
	VerificationRuns  []VerificationRun `json:"verification_runs,omitempty"`
	ScenarioResults   []ScenarioResult  `json:"scenario_results,omitempty"`
	present           map[string]bool
}
type Review struct {
	WorkflowID string     `json:"workflow_id"`
	UnitID     string     `json:"unit_id"`
	Revision   int64      `json:"revision"`
	Verdict    Verdict    `json:"verdict"`
	Summary    string     `json:"summary"`
	Findings   string     `json:"findings"`
	PlanImpact PlanImpact `json:"plan_impact"`
	Actor      string     `json:"-"`
}
type ReviewOutcome struct {
	NextRevision         int64
	PlanRevisionRequired bool
}
type AggregateReview struct {
	Verdict          Verdict             `json:"verdict"`
	Summary          string              `json:"summary"`
	Findings         string              `json:"findings"`
	VerificationRuns []VerificationRun   `json:"verification_runs,omitempty"`
	Checkpoint       *ReviewedCheckpoint `json:"checkpoint,omitempty"`
	Actor            string              `json:"-"`
}
type AggregateOutcome struct {
	Revision   int64  `json:"revision"`
	State      string `json:"state"`
	NextAction string `json:"next_action"`
}
type Service struct {
	db             *sql.DB
	now            func() time.Time
	enforceChanges bool
}

func New(s *store.Store, now func() time.Time) *Service { return &Service{db: s.DB(), now: now} }
func (s *Service) WithChangeEnforcement() *Service      { s.enforceChanges = true; return s }

func (r TDDRecord) Validate() error {
	if r.present != nil {
		for _, name := range []string{"red_command", "red_outcome", "green_command", "green_outcome", "refactor_summary", "validation_command", "validation_outcome", "changed_paths"} {
			if !r.present[name] {
				return fmt.Errorf("%s is required", strings.ReplaceAll(name, "_", " "))
			}
		}
	}
	for name, value := range map[string]string{"red command": r.RedCommand, "red outcome": r.RedOutcome, "green command": r.GreenCommand, "green outcome": r.GreenOutcome, "validation command": r.ValidationCommand, "validation outcome": r.ValidationOutcome, "changed paths": r.ChangedPaths} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	redExit, ok := ParseOutcome(r.RedOutcome)
	if !ok || redExit == 0 {
		return fmt.Errorf("red outcome must record a failing exit")
	}
	greenExit, ok := ParseOutcome(r.GreenOutcome)
	if !ok || greenExit != 0 {
		return fmt.Errorf("green outcome must record exit 0")
	}
	validationExit, ok := ParseOutcome(r.ValidationOutcome)
	if !ok || validationExit != 0 {
		return fmt.Errorf("validation outcome must record exit 0")
	}
	for _, raw := range strings.Split(r.ChangedPaths, ",") {
		prefix := strings.TrimSpace(raw)
		if prefix == "" || path.IsAbs(prefix) || path.Clean(prefix) != prefix || prefix == "." || prefix == ".." || strings.HasPrefix(prefix, "../") || strings.ContainsAny(prefix, "*?[\\") {
			return fmt.Errorf("changed paths must be normalized repository-relative paths")
		}
	}
	return nil
}

// ParseOutcome returns the leading process exit code from a recorded outcome.
func ParseOutcome(outcome string) (int, bool) {
	text := strings.TrimSpace(outcome)
	if !strings.HasPrefix(text, "exit ") {
		return 0, false
	}
	codeText := strings.TrimSpace(strings.TrimPrefix(text, "exit "))
	if before, _, found := strings.Cut(codeText, ":"); found {
		codeText = strings.TrimSpace(before)
	}
	code, err := strconv.Atoi(codeText)
	return code, err == nil && code >= 0
}

func (r VerificationRun) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("verification ID is required")
	}
	switch r.Tier {
	case Focused, AffectedPackage, AggregateFull, PublicationFull:
	default:
		return fmt.Errorf("invalid verification tier %q", r.Tier)
	}
	if strings.TrimSpace(r.Command) == "" {
		return fmt.Errorf("verification command is required")
	}
	if strings.TrimSpace(r.RepositoryFingerprint) == "" {
		return fmt.Errorf("repository fingerprint is required")
	}
	if _, ok := ParseOutcome(r.Outcome); !ok {
		return fmt.Errorf("verification outcome must record an exit")
	}
	if r.ReusedFromID == r.ID && r.ReusedFromID != "" {
		return fmt.Errorf("verification cannot reuse itself")
	}
	if _, err := normalizedScenarioIDs(r.ScenarioIDs); err != nil {
		return err
	}
	return nil
}

func normalizedScenarioIDs(values []string) ([]string, error) {
	result := append([]string(nil), values...)
	sort.Strings(result)
	for index, value := range result {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return nil, fmt.Errorf("verification scenario IDs must be non-empty and normalized")
		}
		if index > 0 && result[index-1] == value {
			return nil, fmt.Errorf("verification scenario IDs must be unique")
		}
	}
	return result, nil
}

func (r *TDDRecord) UnmarshalJSON(data []byte) error {
	type wire TDDRecord
	var value wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = TDDRecord(value)
	r.present = map[string]bool{}
	for name := range fields {
		r.present[name] = true
	}
	return nil
}
func (r Review) Validate() error {
	if r.Verdict != Approved && r.Verdict != Corrections {
		return fmt.Errorf("invalid review verdict")
	}
	if r.Verdict == Corrections && (strings.TrimSpace(r.Findings) == "" || (r.PlanImpact != Inside && r.PlanImpact != Outside)) {
		return fmt.Errorf("corrections require findings and plan impact")
	}
	if r.Verdict == Approved && r.PlanImpact != "" {
		return fmt.Errorf("approved review must omit plan impact")
	}
	return nil
}

func (r AggregateReview) Validate() error {
	if r.Verdict != Approved && r.Verdict != Corrections {
		return fmt.Errorf("invalid aggregate review verdict")
	}
	if strings.TrimSpace(r.Actor) == "" {
		return fmt.Errorf("aggregate reviewer actor is required")
	}
	if r.Verdict == Corrections && strings.TrimSpace(r.Findings) == "" {
		return fmt.Errorf("aggregate corrections require findings")
	}
	return nil
}

func (s *Service) RecordTDD(ctx context.Context, wfID, unitID string, revision int64, r TDDRecord) error {
	return s.RecordTDDAs(ctx, wfID, unitID, revision, "caller", r)
}

func (s *Service) RecordTDDAs(ctx context.Context, wfID, unitID string, revision int64, actor string, r TDDRecord) error {
	if err := r.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.RecordTDDAsTx(ctx, tx, wfID, unitID, revision, actor, r); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) RecordTDDAsTx(ctx context.Context, tx *sql.Tx, wfID, unitID string, revision int64, actor string, r TDDRecord) error {
	return s.recordTDDAsTx(ctx, tx, wfID, unitID, "", revision, actor, r)
}

func (s *Service) RecordTDDWithClaimAsTx(ctx context.Context, tx *sql.Tx, wfID, unitID, claimID string, revision int64, actor string, r TDDRecord) error {
	return s.recordTDDAsTx(ctx, tx, wfID, unitID, claimID, revision, actor, r)
}

func (s *Service) recordTDDAsTx(ctx context.Context, tx *sql.Tx, wfID, unitID, claimID string, revision int64, actor string, r TDDRecord) error {
	if err := r.Validate(); err != nil {
		return err
	}
	var err error
	var state string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM work_units WHERE id=? AND workflow_id=?`, unitID, wfID).Scan(&state, &current); err != nil {
		return err
	}
	if current != revision {
		return store.ErrCASMismatch
	}
	if state != "pending" {
		return fmt.Errorf("%w: current state %s; expected pending", ErrInvalidState, state)
	}
	if claimID != "" {
		if err = s.enforceChangeBudget(ctx, tx, wfID, unitID, claimID, revision, "evidence"); err != nil {
			return err
		}
	}
	covered, err := loadCoveredScenarios(ctx, tx, wfID, unitID)
	if err != nil {
		return err
	}
	if len(covered) > 0 {
		if err := validateStructuredUnitEvidence(r, covered); err != nil {
			return err
		}
	}
	now := s.now()
	at := ids.FormatTime(now)
	if err := persistVerificationRuns(ctx, tx, wfID, &unitID, &revision, actor, at, r.VerificationRuns); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO evidence(workflow_id,unit_id,revision,actor,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, wfID, unitID, revision, actor, r.RedCommand, r.RedOutcome, r.GreenCommand, r.GreenOutcome, r.RefactorSummary, r.ValidationCommand, r.ValidationOutcome, r.ChangedPaths, at)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE work_units SET state='reviewing' WHERE id=? AND revision=?`, unitID, revision)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return store.ErrCASMismatch
	}
	return activity.AppendTx(ctx, tx, activity.New(wfID, unitID, activity.UnitTDDRecorded, actor, now, activity.EvidenceSubject(unitID, revision)))
}

func loadCoveredScenarios(ctx context.Context, tx *sql.Tx, wfID, unitID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT scenario_id FROM unit_coverage WHERE workflow_id=? AND unit_id=? ORDER BY scenario_id`, wfID, unitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var scenarioID string
		if err := rows.Scan(&scenarioID); err != nil {
			return nil, err
		}
		result = append(result, scenarioID)
	}
	return result, rows.Err()
}

func validateStructuredUnitEvidence(record TDDRecord, covered []string) error {
	runs := make(map[string]VerificationRun, len(record.VerificationRuns))
	tiers := map[VerificationTier]bool{}
	for _, run := range record.VerificationRuns {
		if err := run.Validate(); err != nil {
			return err
		}
		if _, exists := runs[run.ID]; exists {
			return fmt.Errorf("duplicate verification ID %q", run.ID)
		}
		exit, _ := ParseOutcome(run.Outcome)
		if exit != 0 {
			return fmt.Errorf("%s verification must record exit 0", run.Tier)
		}
		runs[run.ID] = run
		tiers[run.Tier] = true
	}
	for _, tier := range []VerificationTier{Focused, AffectedPackage} {
		if !tiers[tier] {
			return fmt.Errorf("current %s verification is required", tier)
		}
	}
	results := make(map[string]ScenarioResult, len(record.ScenarioResults))
	for _, result := range record.ScenarioResults {
		if strings.TrimSpace(result.ScenarioID) == "" || strings.TrimSpace(result.VerificationID) == "" {
			return fmt.Errorf("scenario result requires scenario and verification IDs")
		}
		if _, exists := results[result.ScenarioID]; exists {
			return fmt.Errorf("duplicate current scenario result %s", result.ScenarioID)
		}
		exit, ok := ParseOutcome(result.Outcome)
		if !ok || exit != 0 {
			return fmt.Errorf("scenario result %s must record exit 0", result.ScenarioID)
		}
		run, ok := runs[result.VerificationID]
		if !ok || !contains(run.ScenarioIDs, result.ScenarioID) {
			return fmt.Errorf("scenario result %s must reference a current verification covering it", result.ScenarioID)
		}
		results[result.ScenarioID] = result
	}
	for _, scenarioID := range covered {
		if _, ok := results[scenarioID]; !ok {
			return fmt.Errorf("current scenario result is required for %s", scenarioID)
		}
	}
	return nil
}

func persistVerificationRuns(ctx context.Context, tx *sql.Tx, wfID string, unitID *string, unitRevision *int64, actor, at string, runs []VerificationRun) error {
	for _, run := range runs {
		if err := run.Validate(); err != nil {
			return err
		}
		scenarios, _ := normalizedScenarioIDs(run.ScenarioIDs)
		if run.ReusedFromID != "" {
			var tier, command, outcome, fingerprint, encoded string
			if err := tx.QueryRowContext(ctx, `SELECT tier,command,outcome,fingerprint,scenario_ids_json FROM verification_records WHERE id=?`, run.ReusedFromID).Scan(&tier, &command, &outcome, &fingerprint, &encoded); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("reused verification %s does not identify an immutable record", run.ReusedFromID)
				}
				return err
			}
			var priorScenarios []string
			if err := json.Unmarshal([]byte(encoded), &priorScenarios); err != nil {
				return fmt.Errorf("reused verification %s has invalid scenario set: %w", run.ReusedFromID, err)
			}
			priorScenarios, err := normalizedScenarioIDs(priorScenarios)
			if err != nil {
				return err
			}
			if VerificationTier(tier) != run.Tier {
				return fmt.Errorf("reused verification tier does not match")
			}
			if command != run.Command {
				return fmt.Errorf("reused verification command does not match")
			}
			if fingerprint != run.RepositoryFingerprint {
				return fmt.Errorf("reused verification repository fingerprint does not match")
			}
			if !equalStrings(priorScenarios, scenarios) {
				return fmt.Errorf("reused verification scenario set does not match")
			}
			if exit, ok := ParseOutcome(outcome); !ok || exit != 0 {
				return fmt.Errorf("reused verification is not an immutable success")
			}
		}
		encoded, err := json.Marshal(scenarios)
		if err != nil {
			return err
		}
		var unit, revision any
		if unitID != nil {
			unit = *unitID
		}
		if unitRevision != nil {
			revision = *unitRevision
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO verification_records(id,workflow_id,unit_id,unit_revision,tier,command,outcome,fingerprint,scenario_ids_json,reused_from_id,actor,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, wfID, unit, revision, run.Tier, run.Command, run.Outcome, run.RepositoryFingerprint, string(encoded), nullable(run.ReusedFromID), actor, at); err != nil {
			return err
		}
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Service) RecordReview(ctx context.Context, r Review) (ReviewOutcome, error) {
	if err := r.Validate(); err != nil {
		return ReviewOutcome{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReviewOutcome{}, err
	}
	defer tx.Rollback()
	outcome, err := s.RecordReviewTx(ctx, tx, r)
	if err != nil {
		return ReviewOutcome{}, err
	}
	return outcome, tx.Commit()
}

func (s *Service) RecordReviewTx(ctx context.Context, tx *sql.Tx, r Review) (ReviewOutcome, error) {
	if err := r.Validate(); err != nil {
		return ReviewOutcome{}, err
	}
	var err error
	var state string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM work_units WHERE id=? AND workflow_id=?`, r.UnitID, r.WorkflowID).Scan(&state, &current); err != nil {
		return ReviewOutcome{}, err
	}
	if current != r.Revision {
		return ReviewOutcome{}, store.ErrCASMismatch
	}
	if state != "reviewing" {
		return ReviewOutcome{}, fmt.Errorf("%w: current state %s; expected reviewing", ErrInvalidState, state)
	}
	var tddActor string
	if err = tx.QueryRowContext(ctx, `SELECT actor FROM evidence WHERE workflow_id=? AND unit_id=? AND revision=?`, r.WorkflowID, r.UnitID, r.Revision).Scan(&tddActor); err != nil {
		return ReviewOutcome{}, ErrInvalidState
	}
	if strings.TrimSpace(r.Actor) == "" || r.Actor == tddActor {
		return ReviewOutcome{}, fmt.Errorf("%w: implementer and reviewer actors must differ", ErrInvalidState)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM evidence WHERE workflow_id=? AND unit_id=? AND revision=?`, r.WorkflowID, r.UnitID, r.Revision).Scan(&count); err != nil || count != 1 {
		if err != nil {
			return ReviewOutcome{}, err
		}
		return ReviewOutcome{}, ErrInvalidState
	}
	now := s.now()
	at := ids.FormatTime(now)
	_, err = tx.ExecContext(ctx, `INSERT INTO reviews(workflow_id,unit_id,revision,actor,verdict,summary,findings,plan_impact,recorded_at) VALUES(?,?,?,?,?,?,?,?,?)`, r.WorkflowID, r.UnitID, r.Revision, r.Actor, r.Verdict, r.Summary, r.Findings, r.PlanImpact, at)
	if err != nil {
		return ReviewOutcome{}, err
	}
	if r.Verdict == Approved {
		if _, err = tx.ExecContext(ctx, `UPDATE unit_change_measurements SET reviewed_digest=result_digest WHERE workflow_id=? AND unit_id=? AND unit_revision=? AND stage='evidence'`, r.WorkflowID, r.UnitID, r.Revision); err != nil {
			return ReviewOutcome{}, err
		}
	}
	outcome := ReviewOutcome{NextRevision: r.Revision, PlanRevisionRequired: r.PlanImpact == Outside}
	if r.Verdict == Corrections {
		result, updateErr := tx.ExecContext(ctx, `UPDATE work_units SET state='pending',revision=revision+1 WHERE id=? AND revision=?`, r.UnitID, r.Revision)
		err = updateErr
		if err != nil {
			return ReviewOutcome{}, err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return ReviewOutcome{}, err
			}
			return ReviewOutcome{}, store.ErrCASMismatch
		}
		outcome.NextRevision++
	}
	return outcome, activity.AppendTx(ctx, tx, activity.New(r.WorkflowID, r.UnitID, activity.UnitReviewRecorded, r.Actor, now, activity.ReviewSubject(r.UnitID, r.Revision)))
}

func (s *Service) CompleteUnit(ctx context.Context, wfID, unitID string, unitRevision, workflowRevision int64, handleValid bool, actor string) error {
	return s.completeUnit(ctx, wfID, unitID, "", unitRevision, workflowRevision, handleValid, actor)
}

func (s *Service) CompleteUnitWithClaim(ctx context.Context, wfID, unitID, claimID string, unitRevision, workflowRevision int64, actor string) error {
	return s.completeUnit(ctx, wfID, unitID, claimID, unitRevision, workflowRevision, true, actor)
}

func (s *Service) CompleteUnitWithClaimTx(ctx context.Context, tx *sql.Tx, wfID, unitID, claimID string, unitRevision, workflowRevision int64, actor string) error {
	return s.completeUnitTx(ctx, tx, wfID, unitID, claimID, unitRevision, workflowRevision, true, actor)
}

func (s *Service) completeUnit(ctx context.Context, wfID, unitID, claimID string, unitRevision, workflowRevision int64, handleValid bool, actor string) error {
	if !handleValid {
		return ErrInvalidHandle
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.completeUnitTx(ctx, tx, wfID, unitID, claimID, unitRevision, workflowRevision, handleValid, actor); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) completeUnitTx(ctx context.Context, tx *sql.Tx, wfID, unitID, claimID string, unitRevision, workflowRevision int64, handleValid bool, actor string) error {
	if !handleValid {
		return ErrInvalidHandle
	}
	var err error
	var state string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM work_units WHERE id=? AND workflow_id=?`, unitID, wfID).Scan(&state, &current); err != nil {
		return err
	}
	if current != unitRevision {
		return store.ErrCASMismatch
	}
	if state != "reviewing" {
		return fmt.Errorf("%w: current state %s; expected reviewing", ErrInvalidState, state)
	}
	if s.enforceChanges {
		if err = s.enforceChangeBudget(ctx, tx, wfID, unitID, claimID, unitRevision, "completion"); err != nil {
			return err
		}
	}
	if claimID != "" {
		result, updateErr := tx.ExecContext(ctx, `UPDATE handles SET state='revoked' WHERE claim_id=? AND workflow_id=? AND unit_id=? AND state='active'`, claimID, wfID, unitID)
		if updateErr != nil {
			return updateErr
		}
		if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
			if changeErr != nil {
				return changeErr
			}
			return ErrInvalidHandle
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE work_units SET state='done' WHERE id=? AND revision=?`, unitID, unitRevision)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return store.ErrCASMismatch
	}
	now := s.now()
	var remaining int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM work_units WHERE workflow_id=? AND state!='done'`, wfID).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		var state string
		var current int64
		if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM workflows WHERE id=?`, wfID).Scan(&state, &current); err != nil {
			return err
		}
		if current != workflowRevision {
			return store.ErrCASMismatch
		}
		if state != "implementing" {
			return ErrInvalidState
		}
		at := ids.FormatTime(now)
		if _, err = tx.ExecContext(ctx, `UPDATE workflows SET state='ready_to_complete',revision=revision+1,updated_at=? WHERE id=? AND revision=?`, at, wfID, workflowRevision); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,?,?,?,?,?,?)`, wfID, "implementing", "ready_to_complete", actor, "", workflowRevision+1, at)
		if err != nil {
			return err
		}
	}
	return activity.AppendTx(ctx, tx, activity.New(wfID, unitID, activity.UnitCompleted, actor, now, activity.UnitSubject(unitID)))
}

func (s *Service) enforceChangeBudget(ctx context.Context, tx *sql.Tx, wfID, unitID, claimID string, revision int64, stage string) error {
	var baseline project.ChangeBaseline
	var unitScope, areasJSON, storedScopes, scopeDigest string
	var accepted, storedBudget int
	err := tx.QueryRowContext(ctx, `SELECT b.project_id,b.checkout_root,b.base_revision,b.baseline_digest,b.scopes_json,b.scope_digest,b.accepted_budget,u.scope,u.areas,u.estimated_changed_lines
		FROM unit_change_baselines b JOIN work_units u ON u.workflow_id=b.workflow_id AND u.id=b.unit_id
		WHERE b.workflow_id=? AND b.unit_id=?`, wfID, unitID).
		Scan(&baseline.ProjectID, &baseline.CheckoutRoot, &baseline.BaseRevision, &baseline.ResultDigest, &storedScopes, &scopeDigest, &storedBudget, &unitScope, &areasJSON, &accepted)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("changed-line baseline is unavailable; recover or reimplement the unit")
	}
	if err != nil {
		return err
	}
	var areas []string
	if err = json.Unmarshal([]byte(areasJSON), &areas); err != nil {
		return errors.New("changed-line scope is invalid; replan the unit")
	}
	scopes, normalizeErr := project.NormalizeChangeScopes(append([]string{unitScope}, areas...))
	if normalizeErr != nil {
		return errors.New("changed-line scope is invalid; replan the unit")
	}
	scopesBytes, _ := json.Marshal(scopes)
	digest := sha256.Sum256(scopesBytes)
	if storedScopes != string(scopesBytes) || scopeDigest != hex.EncodeToString(digest[:]) || storedBudget != accepted {
		return errors.New("changed-line baseline no longer matches accepted scope and budget; replan the unit")
	}
	measurement, err := project.MeasureChangedLines(baseline, scopes)
	if err != nil {
		return err
	}
	if measurement.ChangedLines > accepted {
		return fmt.Errorf("changed-line budget exceeded: measured %d, accepted %d; split or replan the unit", measurement.ChangedLines, accepted)
	}
	if stage == "completion" {
		var evidenceDigest, reviewedDigest string
		if err = tx.QueryRowContext(ctx, `SELECT result_digest,coalesce(reviewed_digest,'') FROM unit_change_measurements WHERE workflow_id=? AND unit_id=? AND unit_revision=? AND stage='evidence'`, wfID, unitID, revision).Scan(&evidenceDigest, &reviewedDigest); err != nil {
			return errors.New("changed-line evidence has not been approved for completion")
		}
		expectedDigest := evidenceDigest
		var reviews int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM reviews WHERE workflow_id=? AND unit_id=? AND revision=?`, wfID, unitID, revision).Scan(&reviews); err != nil {
			return err
		}
		if reviews != 0 {
			if reviewedDigest == "" || reviewedDigest != evidenceDigest {
				return errors.New("changed-line evidence has not been approved for completion")
			}
			expectedDigest = reviewedDigest
		}
		if measurement.ResultDigest != expectedDigest {
			return errors.New("repository changed after review; record new evidence and review")
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO unit_change_measurements(workflow_id,unit_id,unit_revision,stage,additions,deletions,changed_lines,accepted_budget,claim_id,base_revision,baseline_digest,result_digest,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, wfID, unitID, revision, stage, measurement.Additions, measurement.Deletions, measurement.ChangedLines, accepted, claimID, baseline.BaseRevision, baseline.ResultDigest, measurement.ResultDigest, ids.FormatTime(s.now()))
	return err
}

func (s *Service) CompleteAggregate(ctx context.Context, wfID string, revision int64, review AggregateReview) (AggregateOutcome, error) {
	if err := review.Validate(); err != nil {
		return AggregateOutcome{}, err
	}
	payload, err := json.Marshal(review)
	if err != nil {
		return AggregateOutcome{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AggregateOutcome{}, err
	}
	defer tx.Rollback()
	var state string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM workflows WHERE id=?`, wfID).Scan(&state, &current); err != nil {
		return AggregateOutcome{}, err
	}
	if current != revision {
		return AggregateOutcome{}, store.ErrCASMismatch
	}
	if state != "ready_to_complete" {
		return AggregateOutcome{}, fmt.Errorf("%w: current workflow state %s; expected ready_to_complete", ErrInvalidState, state)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM work_units WHERE workflow_id=? AND state!='done'`, wfID).Scan(&count); err != nil {
		return AggregateOutcome{}, err
	}
	if count != 0 {
		return AggregateOutcome{}, fmt.Errorf("%w: aggregate review requires all units done", ErrInvalidState)
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM evidence e JOIN work_units u ON u.id=e.unit_id AND u.workflow_id=e.workflow_id AND u.revision=e.revision WHERE e.workflow_id=? AND e.actor=?`, wfID, review.Actor).Scan(&count); err != nil {
		return AggregateOutcome{}, err
	}
	if count != 0 {
		return AggregateOutcome{}, fmt.Errorf("%w: implementer and aggregate reviewer actors must differ", ErrInvalidState)
	}
	var structuredUnits int
	if err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT unit_id) FROM unit_coverage WHERE workflow_id=?`, wfID).Scan(&structuredUnits); err != nil {
		return AggregateOutcome{}, err
	}
	if review.Verdict == Approved && structuredUnits > 0 {
		if err = validateAggregateBundle(ctx, tx, wfID, review); err != nil {
			return AggregateOutcome{}, err
		}
	}
	projection, err := correction.Project(ctx, tx, wfID, "workflow complete")
	if err != nil {
		return AggregateOutcome{}, err
	}
	if projection.BlockerRevision != 0 {
		return AggregateOutcome{}, fmt.Errorf("%w: unresolved aggregate correction blocker at revision %d", ErrInvalidState, projection.BlockerRevision)
	}
	now := s.now()
	at, nextRevision, nextState := ids.FormatTime(now), revision+1, "ready_to_complete"
	if review.Verdict == Approved {
		nextState = "completed"
		if structuredUnits > 0 {
			if err = persistVerificationRuns(ctx, tx, wfID, nil, nil, review.Actor, at, review.VerificationRuns); err != nil {
				return AggregateOutcome{}, err
			}
			reviewed, checkpointErr := review.Checkpoint.reviewed(wfID, nextRevision, now)
			if checkpointErr != nil {
				return AggregateOutcome{}, checkpointErr
			}
			if err = checkpoint.Persist(ctx, tx, reviewed); err != nil {
				return AggregateOutcome{}, err
			}
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE workflows SET state=?,revision=?,updated_at=? WHERE id=? AND revision=?`, nextState, nextRevision, at, wfID, revision)
	if err != nil {
		return AggregateOutcome{}, err
	}
	if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
		if changeErr != nil {
			return AggregateOutcome{}, changeErr
		}
		return AggregateOutcome{}, store.ErrCASMismatch
	}
	artifact, err := tx.ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,?,?,?,?,?)`, wfID, "aggregate_review", string(payload), review.Actor, nextRevision, at)
	if err != nil {
		return AggregateOutcome{}, err
	}
	artifactID, err := artifact.LastInsertId()
	if err != nil {
		return AggregateOutcome{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,?,?,?,?,?,?)`, wfID, state, nextState, review.Actor, string(review.Verdict), nextRevision, at); err != nil {
		return AggregateOutcome{}, err
	}
	if err = activity.AppendTx(ctx, tx, activity.New(wfID, "", activity.AggregateReviewRecorded, review.Actor, now, activity.ArtifactSubject(artifactID))); err != nil {
		return AggregateOutcome{}, err
	}
	if review.Verdict == Approved {
		if err = activity.AppendTx(ctx, tx, activity.New(wfID, "", activity.WorkflowCompleted, review.Actor, now, activity.EventSubject(wfID, nextRevision))); err != nil {
			return AggregateOutcome{}, err
		}
	}
	nextAction := "none"
	if review.Verdict == Corrections {
		projection, err = correction.Project(ctx, tx, wfID, "workflow complete")
		if err != nil {
			return AggregateOutcome{}, err
		}
		nextAction = projection.NextAction
	}
	if err = tx.Commit(); err != nil {
		return AggregateOutcome{}, err
	}
	return AggregateOutcome{Revision: nextRevision, State: nextState, NextAction: nextAction}, nil
}

func validateAggregateBundle(ctx context.Context, tx *sql.Tx, wfID string, review AggregateReview) error {
	if review.Checkpoint == nil {
		return fmt.Errorf("reviewed checkpoint is required")
	}
	aggregateSuccess := false
	for _, run := range review.VerificationRuns {
		if err := run.Validate(); err != nil {
			return err
		}
		exit, _ := ParseOutcome(run.Outcome)
		if run.Tier == AggregateFull && exit == 0 {
			aggregateSuccess = true
		}
	}
	if !aggregateSuccess {
		return fmt.Errorf("current aggregate_full verification is required")
	}
	rows, err := tx.QueryContext(ctx, `
SELECT u.id,u.revision
FROM work_units u
WHERE u.workflow_id=? AND EXISTS(
    SELECT 1 FROM unit_coverage c WHERE c.workflow_id=u.workflow_id AND c.unit_id=u.id
)`, wfID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var unitID string
		var revision int64
		if err := rows.Scan(&unitID, &revision); err != nil {
			return err
		}
		for _, tier := range []VerificationTier{Focused, AffectedPackage} {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM verification_records WHERE workflow_id=? AND unit_id=? AND unit_revision=? AND tier=? AND outcome LIKE 'exit 0%'`, wfID, unitID, revision, tier).Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("aggregate bundle lacks current %s verification for unit %s", tier, unitID)
			}
		}
	}
	return rows.Err()
}

func (value *ReviewedCheckpoint) reviewed(wfID string, aggregateRevision int64, now time.Time) (checkpoint.Reviewed, error) {
	if value == nil {
		return checkpoint.Reviewed{}, fmt.Errorf("reviewed checkpoint is required")
	}
	fingerprint := project.RepositoryFingerprint{
		ProjectID:    value.ProjectID,
		CheckoutRoot: value.CheckoutRoot,
		BaseRevision: value.BaseRevision,
		HeadRevision: value.HeadRevision,
		ResultDigest: value.ResultDigest,
		Dirty:        value.Dirty,
		Unstaged:     value.Dirty,
	}
	reviewed, err := checkpoint.NewReviewed(wfID, aggregateRevision, fingerprint, value.CommitRef, value.DeliveryID, now)
	if err != nil {
		return checkpoint.Reviewed{}, fmt.Errorf("reviewed checkpoint: %w", err)
	}
	return reviewed, nil
}
