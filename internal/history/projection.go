package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/plan"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
)

type UnitStatus struct {
	ID, Description, Status, Reason string
	Attempt                         int64
	Derived                         bool
}
type Synopsis struct {
	Total, Done, Ready, Claimed, Reviewing, DependencyWaiting, Correction, Recovery int
	Current, Blocker                                                                *UnitStatus
	NextAction                                                                      string
}
type Occurrence struct {
	ID, RecordID, At, Phase, Work, Activity, Actor, Outcome, Reason string
	RelatedRecordIDs                                                []string
	Attempt                                                         *int64
	Legacy                                                          bool
}
type unitFact struct {
	status     UnitStatus
	state      string
	deps       []string
	claim      plan.ClaimStatus
	correction *recordFact
	unknown    bool
}
type recordFact struct {
	outcome, reason, eventTo string
	attempt                  *int64
	authority                int64
}

func (s *Service) project(ctx context.Context, detail *Detail) error {
	detail.Synopsis.NextAction = workflow.NextAction(workflow.State(detail.Workflow.State))
	units, err := s.unitFacts(ctx, detail.Workflow.ID)
	if err != nil {
		return err
	}
	facts, err := s.recordFacts(ctx, detail)
	if err != nil {
		return err
	}
	states := map[string]string{}
	for id, unit := range units {
		states[id] = unit.state
	}
	ready, err := s.readyFacts(ctx, detail.Workflow.ID, units)
	if err != nil {
		return err
	}
	for _, unit := range units {
		status := &unit.status
		status.Status, status.Reason = classify(unit, states, ready[status.ID], s.now())
		switch status.Status {
		case "Done":
			detail.Synopsis.Done++
		case "Ready":
			detail.Synopsis.Ready++
		case "Claimed":
			detail.Synopsis.Claimed++
		case "Reviewing":
			detail.Synopsis.Reviewing++
		case "Dependency waiting":
			detail.Synopsis.DependencyWaiting++
		case "Correction":
			detail.Synopsis.Correction++
		case "Recovery":
			detail.Synopsis.Recovery++
		}
		if status.Status != "Done" && status.Status != "Queued" {
			choose(&detail.Synopsis, status, status.Status == "Correction" || status.Status == "Dependency waiting")
		}
		detail.Synopsis.Total++
	}
	if detail.Workflow.State == string(workflow.ReadyToComplete) {
		var latest recordFact
		for _, fact := range facts {
			if fact.authority > latest.authority {
				latest = fact
			}
		}
		if latest.eventTo == "aggregate_corrections" {
			choose(&detail.Synopsis, &UnitStatus{Description: "Aggregate review", Status: "Correction", Reason: latest.reason, Derived: true}, true)
		}
	}
	if detail.Workflow.State == string(workflow.Completed) || detail.Workflow.State == string(workflow.Abandoned) {
		detail.Synopsis.Current = nil
	}
	detail.Occurrences = coalesce(detail, units, facts)
	return nil
}

func classify(unit unitFact, states map[string]string, ready bool, now time.Time) (string, string) {
	switch {
	case unit.unknown:
		return "Unknown", ""
	case unit.state == "done":
		return "Done", ""
	case unit.state == "pending" && unit.correction != nil:
		return "Correction", unit.correction.reason
	case plan.ClaimActive(unit.claim, now):
		return "Claimed", ""
	case unit.state == "reviewing":
		return "Reviewing", ""
	case unit.claim.State != "" && unit.claim.State != "revoked":
		return "Recovery", "Latest claim expired"
	case blockedBy(unit.deps, states):
		return "Dependency waiting", "Waiting for dependencies"
	case ready:
		return "Ready", ""
	default:
		return "Queued", ""
	}
}

func (s *Service) readyFacts(ctx context.Context, workflowID string, units map[string]unitFact) (map[string]bool, error) {
	var body string
	if err := s.db.QueryRowContext(ctx, `SELECT body FROM plans WHERE workflow_id=?`, workflowID).Scan(&body); err != nil {
		if err == sql.ErrNoRows {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	var p plan.Plan
	if json.Unmarshal([]byte(body), &p) != nil {
		return map[string]bool{}, nil
	}
	var claims []plan.ClaimStatus
	for i := range p.Units {
		if unit, ok := units[p.Units[i].ID]; ok {
			p.Units[i].State = plan.UnitState(unit.state)
			claims = append(claims, unit.claim)
		}
	}
	ready := map[string]bool{}
	for _, unit := range plan.ReadyUnitsAt(p, claims, s.now()) {
		ready[unit.ID] = true
	}
	return ready, nil
}

func choose(s *Synopsis, unit *UnitStatus, blocker bool) {
	copy := *unit
	if blocker && (s.Blocker == nil || preferred(copy, *s.Blocker)) {
		s.Blocker = &copy
	}
	if s.Current == nil || preferred(copy, *s.Current) {
		s.Current = &copy
	}
}
func preferred(a, b UnitStatus) bool {
	ranks := map[string]int{"Correction": 0, "Claimed": 1, "Reviewing": 2, "Recovery": 3, "Dependency waiting": 4, "Ready": 5}
	ar, br := ranks[a.Status], ranks[b.Status]
	return ar < br || ar == br && a.ID < b.ID
}
func blockedBy(deps []string, states map[string]string) bool {
	for _, id := range deps {
		if states[id] != "done" {
			return true
		}
	}
	return false
}

func (s *Service) unitFacts(ctx context.Context, workflowID string) (map[string]unitFact, error) {
	var handles int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='handles'`).Scan(&handles); err != nil {
		return nil, err
	}
	claimColumns, claimJoin := `'', '', 0`, ``
	if handles != 0 {
		var purposeColumns int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('handles') WHERE name='purpose'`).Scan(&purposeColumns); err != nil {
			return nil, err
		}
		purposeJoin, purposeLatest := "", ""
		if purposeColumns != 0 {
			purposeJoin, purposeLatest = " AND h.purpose='implementation'", " AND h2.purpose='implementation'"
		}
		claimColumns = `COALESCE(h.state,''),COALESCE(h.expires_at,''),COALESCE(h.claim_generation,0)`
		claimJoin = ` LEFT JOIN handles h ON h.workflow_id=u.workflow_id AND h.unit_id=u.id` + purposeJoin + ` AND h.claim_generation=(SELECT MAX(h2.claim_generation) FROM handles h2 WHERE h2.workflow_id=u.workflow_id AND h2.unit_id=u.id` + purposeLatest + `)`
	}
	query := `SELECT u.id,u.description,u.depends_on,u.state,u.revision,COALESCE(r.verdict,''),COALESCE(r.findings,''),` + claimColumns + ` FROM work_units u LEFT JOIN reviews r ON r.workflow_id=u.workflow_id AND r.unit_id=u.id AND r.revision=u.revision-1` + claimJoin + ` WHERE u.workflow_id=? ORDER BY u.rowid`
	rows, err := s.db.QueryContext(ctx, query, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]unitFact{}
	for rows.Next() {
		var id, desc, depsJSON, state, verdict, findings, claimState, expiry string
		var rev, generation int64
		if err := rows.Scan(&id, &desc, &depsJSON, &state, &rev, &verdict, &findings, &claimState, &expiry, &generation); err != nil {
			return nil, err
		}
		var deps []string
		decodeErr := json.Unmarshal([]byte(depsJSON), &deps)
		unit := unitFact{status: UnitStatus{ID: id, Description: desc, Attempt: rev, Derived: true}, state: state, deps: deps, claim: plan.ClaimStatus{UnitID: id, State: claimState, Generation: generation}, unknown: decodeErr != nil}
		if expiry != "" {
			unit.claim.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiry)
			if err != nil {
				return nil, err
			}
		}
		if strings.Title(verdict) == "Corrections" {
			unit.correction = &recordFact{outcome: "Corrections", reason: findings}
		}
		result[id] = unit
	}
	return result, rows.Err()
}

func (s *Service) recordFacts(ctx context.Context, detail *Detail) (map[string]recordFact, error) {
	result := map[string]recordFact{}
	rows, err := s.db.QueryContext(ctx, `SELECT id,outcome,reason,event_to,attempt,authority FROM (
SELECT 'review:'||unit_id||':'||revision id,verdict outcome,findings reason,'' event_to,revision attempt,0 authority FROM reviews WHERE workflow_id=?
UNION ALL SELECT 'evidence:'||unit_id||':'||revision,'Validation recorded','','',revision,0 FROM evidence WHERE workflow_id=?
UNION ALL SELECT 'event:'||revision_after,to_state,reason,to_state,NULL,0 FROM events WHERE workflow_id=?
UNION ALL SELECT 'artifact:'||id,CASE WHEN json_valid(content) THEN json_extract(content,'$.verdict') ELSE '' END,
 CASE WHEN json_valid(content) THEN json_extract(content,'$.findings') ELSE '' END,
 CASE WHEN json_valid(content) AND json_extract(content,'$.verdict')='corrections' THEN 'aggregate_corrections' ELSE '' END,
 NULL,accepted_revision FROM artifacts WHERE workflow_id=? AND kind='aggregate_review')`, detail.Workflow.ID, detail.Workflow.ID, detail.Workflow.ID, detail.Workflow.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, outcome, reason, eventTo string
		var attempt sql.NullInt64
		var authority int64
		if err = rows.Scan(&id, &outcome, &reason, &eventTo, &attempt, &authority); err != nil {
			return nil, err
		}
		fact := recordFact{outcome: humanState(outcome), reason: reason, eventTo: eventTo, authority: authority}
		if attempt.Valid {
			n := attempt.Int64
			fact.attempt = &n
		}
		result[id] = fact
	}
	return result, rows.Err()
}

func coalesce(detail *Detail, units map[string]unitFact, facts map[string]recordFact) []Occurrence {
	entries := detail.Timeline
	consumed := map[int]bool{}
	var out []Occurrence
	for i, a := range entries {
		if consumed[i] {
			continue
		}
		f := facts[a.RecordID]
		if u, ok := units[a.UnitID]; ok && f.outcome == "Corrections" && u.correction != nil {
			f.reason = u.correction.reason
		}
		o := Occurrence{ID: a.ID, RecordID: a.RecordID, At: a.At, Phase: phase(a.Action), Activity: a.Action, Actor: a.Actor, Outcome: f.outcome, Reason: f.reason, Legacy: a.Legacy, Attempt: f.attempt}
		if u, ok := units[a.UnitID]; ok {
			o.Work = u.status.Description
		} else {
			o.Work = detail.Workflow.Name
		}
		expected := transitionFor(a.Action)
		if a.Action == "aggregate_review_recorded" {
			if f.outcome == "Approved" {
				expected = "completed"
			} else if f.outcome == "Corrections" {
				expected = "ready_to_complete"
			}
		}
		if expected != "" && !a.Legacy {
			for j, b := range entries {
				bf := facts[b.RecordID]
				semanticFinal := a.Action == "aggregate_review_recorded" && b.Action == "workflow_completed" && expected == "completed"
				if j != i && !consumed[j] && (b.Legacy && b.Action == "event" && bf.eventTo == expected || semanticFinal) && b.Actor == a.Actor && b.At == a.At {
					consumed[j] = true
					o.RelatedRecordIDs = append(o.RelatedRecordIDs, b.RecordID)
					break
				}
			}
		}
		if o.Outcome == "" {
			o.Outcome = humanActivity(a.Action)
		}
		out = append(out, o)
	}
	return out
}
func transitionFor(action string) string {
	return map[string]string{"workflow_created": "draft", "exploration_recorded": "exploring", "specification_recorded": "specifying", "design_recorded": "designing", "plan_submitted": "planning", "plan_approved": "plan_approved", "implementation_started": "implementing", "unit_completed": "ready_to_complete", "workflow_completed": "completed", "workflow_abandoned": "abandoned"}[action]
}
func phase(action string) string {
	for prefix, value := range map[string]string{"exploration_": "Explore", "specification_": "Specify", "design_": "Design", "plan_": "Plan", "unit_": "Implement", "aggregate_": "Review"} {
		if strings.HasPrefix(action, prefix) {
			return value
		}
	}
	if i := strings.IndexByte(action, '_'); i > 0 {
		return strings.Title(action[:i])
	}
	return "Workflow"
}
func humanActivity(v string) string { return strings.Title(strings.ReplaceAll(v, "_", " ")) }
func humanState(v string) string    { return strings.Title(strings.ReplaceAll(v, "_", " ")) }
func itoa(v int64) string           { return strconv.FormatInt(v, 10) }
