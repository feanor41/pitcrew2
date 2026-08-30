package history

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/fmazzalomo/pitcrew/internal/correction"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
)

func (s *Service) Project(ctx context.Context, workflowID string, view View, unitID string) (Projection, error) {
	if view == ViewUnit {
		if strings.TrimSpace(unitID) == "" {
			return Projection{}, fmt.Errorf("unit view requires unit id")
		}
	} else if unitID != "" {
		return Projection{}, fmt.Errorf("unit id is only valid for unit view")
	}
	if view == ViewAudit {
		detail, err := s.Detail(ctx, workflowID)
		if err != nil {
			return Projection{}, err
		}
		return Projection{View: view, Workflow: identity(detail.Workflow), Audit: &detail}, nil
	}
	if view != ViewCoordination && view != ViewPhase && view != ViewUnit && view != ViewAggregate {
		return Projection{}, fmt.Errorf("unsupported workflow view %q", view)
	}
	wf, err := s.workflowIdentity(ctx, workflowID)
	if err != nil {
		return Projection{}, err
	}
	result := Projection{View: view, Workflow: identity(wf)}
	switch view {
	case ViewCoordination:
		coordination, err := s.coordination(ctx, wf)
		if err != nil {
			return Projection{}, err
		}
		result.Coordination = &coordination
		return result, nil
	case ViewPhase:
		normative, err := s.normativeProjection(ctx, workflowID)
		if err != nil {
			return Projection{}, err
		}
		result.Phase = &PhaseProjection{Normative: normative}
	case ViewUnit:
		unit, err := s.unitProjection(ctx, workflowID, unitID)
		if err != nil {
			return Projection{}, err
		}
		result.Unit = &unit
	case ViewAggregate:
		aggregate, err := s.aggregateProjection(ctx, wf)
		if err != nil {
			return Projection{}, err
		}
		result.Aggregate = &aggregate
	}
	return result, nil
}

func identity(w Workflow) WorkflowIdentity {
	return WorkflowIdentity{ID: w.ID, Revision: w.Revision, State: w.State}
}

func (s *Service) workflowIdentity(ctx context.Context, workflowID string) (Workflow, error) {
	nameExpr, err := s.workflowNameExpr(ctx)
	if err != nil {
		return Workflow{}, err
	}
	return scanWorkflow(s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT id,revision,state,%s,goal,created_at,updated_at FROM workflows WHERE id=?`, nameExpr), workflowID))
}

func (s *Service) coordination(ctx context.Context, wf Workflow) (Coordination, error) {
	result := Coordination{NextAction: workflow.NextAction(workflow.State(wf.State))}
	var err error
	result.LatestProgress, err = s.latestProgress(ctx, wf.ID)
	if err != nil {
		return Coordination{}, err
	}
	units, err := s.unitFacts(ctx, wf.ID)
	if err != nil {
		return Coordination{}, err
	}
	states := map[string]string{}
	for id, unit := range units {
		states[id] = unit.state
	}
	ready, err := s.readyFacts(ctx, wf.ID, units)
	if err != nil {
		return Coordination{}, err
	}
	for id, unit := range units {
		unit.status.Status, unit.status.Reason = classify(unit, states, ready[id], s.now())
		if unit.status.Status == "Ready" {
			result.Ready = append(result.Ready, unit.status)
		}
		if unit.status.Status != "Done" && unit.status.Status != "Queued" {
			candidate := unit.status
			if result.Current == nil || preferred(candidate, *result.Current) {
				result.Current = &candidate
			}
			if candidate.Status == "Correction" || candidate.Status == "Dependency waiting" {
				if result.Blocker == nil || preferred(candidate, *result.Blocker) {
					result.Blocker = &candidate
				}
			}
		}
	}
	sort.Slice(result.Ready, func(i, j int) bool { return result.Ready[i].ID < result.Ready[j].ID })
	projected, err := correction.Project(ctx, s.db, wf.ID, result.NextAction)
	if err == nil {
		result.NextAction = projected.NextAction
		result.CorrectionAuthority = &CorrectionStatus{projected.PolicyAware, projected.Allowed, projected.Used, projected.BlockerRevision, projected.BlockerContent, string(projected.Authority)}
		if projected.BlockerRevision != 0 && wf.State != string(workflow.Completed) && wf.State != string(workflow.Abandoned) {
			blocker := UnitStatus{Description: "Aggregate review", Status: "Correction", Reason: correctionBlockerReason(projected.BlockerContent), Derived: true}
			result.Blocker, result.Current = &blocker, &blocker
		}
	}
	if wf.State == string(workflow.Completed) || wf.State == string(workflow.Abandoned) {
		result.Current = nil
		result.Ready = nil
		result.Blocker = nil
	}
	return result, nil
}

func (s *Service) normativeProjection(ctx context.Context, workflowID string) (NormativeProjection, error) {
	resolved, err := s.resolveNormative(ctx, workflowID)
	if err != nil {
		return NormativeProjection{}, err
	}
	result := NormativeProjection{
		WorkflowID: resolved.WorkflowID,
		Structured: resolved.Structured,
		Baseline:   resolved.Baseline,
		Entries:    resolved.Entries,
	}
	rows, err := s.queryContext(ctx, `SELECT a.id,a.kind,a.content,a.actor,a.accepted_revision,a.recorded_at
FROM artifacts a
WHERE a.workflow_id=?
  AND a.kind IN ('exploration','specification','design')
  AND NOT EXISTS (SELECT 1 FROM normative_entries n WHERE n.workflow_id=a.workflow_id AND n.artifact_id=a.id)
ORDER BY a.accepted_revision,a.id`, workflowID)
	if err != nil {
		return NormativeProjection{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var artifact AcceptedArtifact
		artifact.Source.WorkflowID = workflowID
		if err = rows.Scan(&artifact.Source.ArtifactID, &artifact.Kind, &artifact.Content, &artifact.Source.Actor, &artifact.Source.Revision, &artifact.Source.RecordedAt); err != nil {
			return NormativeProjection{}, err
		}
		result.Artifacts = append(result.Artifacts, artifact)
	}
	if err = rows.Err(); err != nil {
		return NormativeProjection{}, err
	}
	return result, nil
}
