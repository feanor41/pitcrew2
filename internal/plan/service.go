package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
)

var (
	ErrNotFound        = errors.New("plan not found")
	ErrInvalidApproval = errors.New("invalid plan approval")
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewService(s *store.Store, now func() time.Time) *Service { return &Service{db: s.DB(), now: now} }

func (s *Service) Submit(ctx context.Context, workflowID string, expected int64, actor string, p Plan) (workflow.Workflow, error) {
	if err := Validate(p); err != nil {
		return workflow.Workflow{}, err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return workflow.Workflow{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workflow.Workflow{}, err
	}
	defer tx.Rollback()
	current, err := workflowRow(ctx, tx, workflowID)
	if err != nil {
		return workflow.Workflow{}, err
	}
	if current.Revision != expected {
		return workflow.Workflow{}, store.ErrCASMismatch
	}
	if current.State != workflow.Designing {
		return workflow.Workflow{}, fmt.Errorf("%w: current state %s; expected designing", workflow.ErrInvalidTransition, current.State)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,?,?,?,?)`, workflowID, p.Summary, p.Scope, p.MaxParallelUnits, string(body)); err != nil {
		return workflow.Workflow{}, err
	}
	for _, unit := range p.Units {
		areas, _ := json.Marshal(unit.Areas)
		deps, _ := json.Marshal(unit.DependsOn)
		exception, _ := json.Marshal(unit.AdmissionException)
		var exceptionValue any
		if unit.AdmissionException != nil {
			exceptionValue = string(exception)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,admission_exception,revision) VALUES(?,?,?,?,?,?,?,?,?,?,1)`, unit.ID, workflowID, unit.Description, unit.Scope, string(areas), string(deps), unit.EstimatedChangedLines, unit.EstimatedReviewMinutes, Pending, exceptionValue)
		if err != nil {
			return workflow.Workflow{}, err
		}
	}
	return commitWorkflowTransition(ctx, tx, current, workflow.Planning, actor, s.now(), activity.PlanSubmitted)
}

func (s *Service) Approve(ctx context.Context, workflowID string, expected int64, actor string, approved []string) (workflow.Workflow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return workflow.Workflow{}, err
	}
	defer tx.Rollback()
	current, err := workflowRow(ctx, tx, workflowID)
	if err != nil {
		return workflow.Workflow{}, err
	}
	if current.Revision != expected {
		return workflow.Workflow{}, store.ErrCASMismatch
	}
	if current.State != workflow.Planning {
		return workflow.Workflow{}, fmt.Errorf("%w: current state %s; expected planning", workflow.ErrInvalidTransition, current.State)
	}
	p, err := loadPlan(ctx, tx, workflowID)
	if err != nil {
		return workflow.Workflow{}, err
	}
	if err = Approve(p, approved); err != nil {
		return workflow.Workflow{}, fmt.Errorf("%w: %v", ErrInvalidApproval, err)
	}
	for _, unitID := range approved {
		result, updateErr := tx.ExecContext(ctx, `UPDATE work_units SET admission_exception_approved=1 WHERE workflow_id=? AND id=? AND admission_exception IS NOT NULL`, workflowID, unitID)
		if updateErr != nil {
			return workflow.Workflow{}, updateErr
		}
		if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
			if changeErr != nil {
				return workflow.Workflow{}, changeErr
			}
			return workflow.Workflow{}, fmt.Errorf("admission exception approval target %s is not persisted", unitID)
		}
	}
	return commitWorkflowTransition(ctx, tx, current, workflow.PlanApproved, actor, s.now(), activity.PlanApproved)
}

func (s *Service) Ready(ctx context.Context, workflowID string) ([]WorkUnit, error) {
	var state workflow.State
	if err := s.db.QueryRowContext(ctx, `SELECT state FROM workflows WHERE id=?`, workflowID).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return nil, workflow.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if state != workflow.PlanApproved && state != workflow.Implementing {
		return nil, fmt.Errorf("%w: current state %s; expected plan_approved or implementing", workflow.ErrInvalidTransition, state)
	}
	p, err := s.load(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,state FROM work_units WHERE workflow_id=?`, workflowID)
	if err != nil {
		return nil, err
	}
	states := map[string]UnitState{}
	for rows.Next() {
		var id string
		var state UnitState
		if err = rows.Scan(&id, &state); err != nil {
			rows.Close()
			return nil, err
		}
		states[id] = state
	}
	rows.Close()
	for i := range p.Units {
		p.Units[i].State = states[p.Units[i].ID]
	}
	handles := map[string]bool{}
	rows, err = s.db.QueryContext(ctx, `SELECT unit_id FROM handles WHERE workflow_id=? AND state!='revoked'`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		handles[id] = true
	}
	return ReadyUnits(p, handles), rows.Err()
}

func (s *Service) load(ctx context.Context, workflowID string) (Plan, error) {
	return loadPlan(ctx, s.db, workflowID)
}

type planQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadPlan(ctx context.Context, queryer planQueryer, workflowID string) (Plan, error) {
	var body string
	err := queryer.QueryRowContext(ctx, `SELECT body FROM plans WHERE workflow_id=?`, workflowID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	err = json.Unmarshal([]byte(body), &p)
	return p, err
}

func workflowRow(ctx context.Context, tx *sql.Tx, id string) (workflow.Workflow, error) {
	var w workflow.Workflow
	err := tx.QueryRowContext(ctx, `SELECT id,revision,state,goal,created_at,updated_at FROM workflows WHERE id=?`, id).Scan(&w.ID, &w.Revision, &w.State, &w.Goal, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return workflow.Workflow{}, workflow.ErrNotFound
	}
	return w, err
}
func commitWorkflowTransition(ctx context.Context, tx *sql.Tx, current workflow.Workflow, next workflow.State, actor string, now time.Time, action activity.Action) (workflow.Workflow, error) {
	at := ids.FormatTime(now)
	revision := current.Revision + 1
	result, err := tx.ExecContext(ctx, `UPDATE workflows SET state=?,revision=?,updated_at=? WHERE id=? AND revision=?`, next, revision, at, current.ID, current.Revision)
	if err != nil {
		return workflow.Workflow{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return workflow.Workflow{}, err
	}
	if changed != 1 {
		return workflow.Workflow{}, store.ErrCASMismatch
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,?,?,?,?,?,?)`, current.ID, current.State, next, actor, "", revision, at); err != nil {
		return workflow.Workflow{}, err
	}
	if err = activity.AppendTx(ctx, tx, activity.New(current.ID, "", action, actor, now, activity.PlanSubject(current.ID))); err != nil {
		return workflow.Workflow{}, err
	}
	if err = tx.Commit(); err != nil {
		return workflow.Workflow{}, err
	}
	current.State, current.Revision, current.UpdatedAt = next, revision, at
	return current, nil
}
