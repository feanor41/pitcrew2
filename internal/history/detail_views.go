package history

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fmazzalomo/pitcrew/internal/correction"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
)

func (s *Service) unitProjection(ctx context.Context, workflowID, unitID string) (UnitProjection, error) {
	var unit UnitProjection
	var areasJSON, depsJSON string
	err := s.queryRowContext(ctx, `SELECT id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision FROM work_units WHERE workflow_id=? AND id=?`, workflowID, unitID).
		Scan(&unit.Definition.ID, &unit.Definition.Description, &unit.Definition.Scope, &areasJSON, &depsJSON, &unit.Definition.EstimatedChangedLines, &unit.Definition.EstimatedReviewMinutes, &unit.Definition.State, &unit.Definition.Revision)
	if err != nil {
		return UnitProjection{}, err
	}
	if err = json.Unmarshal([]byte(areasJSON), &unit.Definition.Areas); err != nil {
		return UnitProjection{}, err
	}
	if err = json.Unmarshal([]byte(depsJSON), &unit.Definition.DependsOn); err != nil {
		return UnitProjection{}, err
	}
	coverageRows, err := s.db.QueryContext(ctx, `SELECT requirement_id,scenario_id FROM unit_coverage WHERE workflow_id=? AND unit_id=? ORDER BY requirement_id,scenario_id`, workflowID, unitID)
	if err != nil {
		return UnitProjection{}, err
	}
	for coverageRows.Next() {
		var coverage UnitCoverage
		if err = coverageRows.Scan(&coverage.RequirementID, &coverage.ScenarioID); err != nil {
			coverageRows.Close()
			return UnitProjection{}, err
		}
		unit.Definition.Coverage = append(unit.Definition.Coverage, coverage)
	}
	if err = coverageRows.Close(); err != nil {
		return UnitProjection{}, err
	}
	var evidence UnitEvidence
	err = s.db.QueryRowContext(ctx, `SELECT revision,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at FROM evidence WHERE workflow_id=? AND unit_id=? AND revision=?`, workflowID, unitID, unit.Definition.Revision).
		Scan(&evidence.Revision, &evidence.RedCommand, &evidence.RedOutcome, &evidence.GreenCommand, &evidence.GreenOutcome, &evidence.RefactorSummary, &evidence.ValidationCommand, &evidence.ValidationOutcome, &evidence.ChangedPaths, &evidence.RecordedAt)
	if err == nil {
		unit.Evidence = &evidence
	} else if err != sql.ErrNoRows {
		return UnitProjection{}, err
	}
	var review UnitReview
	err = s.db.QueryRowContext(ctx, `SELECT revision,verdict,summary,findings,plan_impact,recorded_at FROM reviews WHERE workflow_id=? AND unit_id=? AND revision=?`, workflowID, unitID, unit.Definition.Revision).
		Scan(&review.Revision, &review.Verdict, &review.Summary, &review.Findings, &review.PlanImpact, &review.RecordedAt)
	if err == nil {
		unit.Review = &review
	} else if err != sql.ErrNoRows {
		return UnitProjection{}, err
	}
	return unit, nil
}

func (s *Service) aggregateProjection(ctx context.Context, wf Workflow) (AggregateProjection, error) {
	normative, err := s.normativeProjection(ctx, wf.ID)
	if err != nil {
		return AggregateProjection{}, err
	}
	result := AggregateProjection{Normative: normative}
	projected, correctionErr := correction.Project(ctx, s.db, wf.ID, workflow.NextAction(workflow.State(wf.State)))
	if correctionErr == nil {
		result.Correction = &CorrectionStatus{projected.PolicyAware, projected.Allowed, projected.Used, projected.BlockerRevision, projected.BlockerContent, string(projected.Authority)}
	}
	var plan PlanProjection
	err = s.db.QueryRowContext(ctx, `SELECT summary,scope,max_parallel_units,body FROM plans WHERE workflow_id=?`, wf.ID).Scan(&plan.Summary, &plan.Scope, &plan.MaxParallelUnits, &plan.Body)
	if err == nil {
		result.Plan = &plan
	} else if err != sql.ErrNoRows {
		return AggregateProjection{}, err
	}
	rows, err := s.queryContext(ctx, `SELECT id FROM work_units WHERE workflow_id=? ORDER BY rowid`, wf.ID)
	if err != nil {
		return AggregateProjection{}, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return AggregateProjection{}, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return AggregateProjection{}, err
	}
	for _, id := range ids {
		unit, unitErr := s.unitProjection(ctx, wf.ID, id)
		if unitErr != nil {
			return AggregateProjection{}, unitErr
		}
		result.Units = append(result.Units, unit)
	}
	if err = s.loadVerification(ctx, wf.ID, &result); err != nil {
		return AggregateProjection{}, err
	}
	if err = s.loadCheckpoint(ctx, wf.ID, &result); err != nil {
		return AggregateProjection{}, err
	}
	return result, nil
}

func (s *Service) loadVerification(ctx context.Context, workflowID string, result *AggregateProjection) error {
	rows, err := s.db.QueryContext(ctx, `SELECT v.id,COALESCE(v.unit_id,''),v.unit_revision,v.tier,v.command,v.outcome,v.fingerprint,v.scenario_ids_json,COALESCE(v.reused_from_id,''),v.recorded_at FROM verification_records v LEFT JOIN work_units u ON u.workflow_id=v.workflow_id AND u.id=v.unit_id WHERE v.workflow_id=? AND (v.unit_id IS NULL OR v.unit_revision=u.revision) ORDER BY v.recorded_at,v.id`, workflowID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item VerificationProjection
		var revision sql.NullInt64
		var scenarios string
		if err = rows.Scan(&item.ID, &item.UnitID, &revision, &item.Tier, &item.Command, &item.Outcome, &item.RepositoryFingerprint, &scenarios, &item.ReusedFromID, &item.RecordedAt); err != nil {
			return err
		}
		if revision.Valid {
			value := revision.Int64
			item.UnitRevision = &value
		}
		item.ScenarioIDs = json.RawMessage(scenarios)
		result.Verification = append(result.Verification, item)
	}
	return rows.Err()
}

func (s *Service) loadCheckpoint(ctx context.Context, workflowID string, result *AggregateProjection) error {
	var item CheckpointProjection
	var dirty int
	err := s.db.QueryRowContext(ctx, `SELECT aggregate_revision,project_id,checkout_root,base_revision,head_revision,result_digest,dirty,commit_ref,delivery_id,recorded_at FROM reviewed_checkpoints WHERE workflow_id=? ORDER BY aggregate_revision DESC LIMIT 1`, workflowID).
		Scan(&item.AggregateRevision, &item.ProjectID, &item.CheckoutRoot, &item.BaseRevision, &item.HeadRevision, &item.ResultDigest, &dirty, &item.CommitRef, &item.DeliveryID, &item.RecordedAt)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	item.Dirty = dirty != 0
	result.Checkpoint = &item
	return nil
}
