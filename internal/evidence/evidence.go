package evidence

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/correction"
	"github.com/fmazzalomo/pitcrew/internal/ids"
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

const (
	Approved    Verdict    = "approved"
	Corrections Verdict    = "corrections"
	Inside      PlanImpact = "inside"
	Outside     PlanImpact = "outside"
)

type TDDRecord struct {
	RedCommand        string `json:"red_command"`
	RedOutcome        string `json:"red_outcome"`
	GreenCommand      string `json:"green_command"`
	GreenOutcome      string `json:"green_outcome"`
	RefactorSummary   string `json:"refactor_summary"`
	ValidationCommand string `json:"validation_command"`
	ValidationOutcome string `json:"validation_outcome"`
	ChangedPaths      string `json:"changed_paths"`
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
	Verdict  Verdict `json:"verdict"`
	Summary  string  `json:"summary"`
	Findings string  `json:"findings"`
	Actor    string  `json:"-"`
}
type AggregateOutcome struct {
	Revision   int64  `json:"revision"`
	State      string `json:"state"`
	NextAction string `json:"next_action"`
}
type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(s *store.Store, now func() time.Time) *Service { return &Service{db: s.DB(), now: now} }

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
	redExit, ok := outcomeExitCode(r.RedOutcome)
	if !ok || redExit == 0 {
		return fmt.Errorf("red outcome must record a failing exit")
	}
	greenExit, ok := outcomeExitCode(r.GreenOutcome)
	if !ok || greenExit != 0 {
		return fmt.Errorf("green outcome must record exit 0")
	}
	validationExit, ok := outcomeExitCode(r.ValidationOutcome)
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

func outcomeExitCode(outcome string) (int, bool) {
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
	now := s.now()
	at := ids.FormatTime(now)
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
