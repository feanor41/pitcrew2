package activity

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/ids"
)

type Action string
type SubjectKind string
type Subject struct {
	Kind SubjectKind
	ID   string
}
type Entry struct {
	WorkflowID, UnitID string
	Action             Action
	Actor, At          string
	Subject            Subject
}

const (
	WorkflowCreated         Action      = "workflow_created"
	ExplorationRecorded     Action      = "exploration_recorded"
	SpecificationRecorded   Action      = "specification_recorded"
	DesignRecorded          Action      = "design_recorded"
	PlanSubmitted           Action      = "plan_submitted"
	PlanApproved            Action      = "plan_approved"
	ImplementationStarted   Action      = "implementation_started"
	WorkflowCompleted       Action      = "workflow_completed"
	WorkflowAbandoned       Action      = "workflow_abandoned"
	UnitClaimed             Action      = "unit_claimed"
	UnitClaimRecovered      Action      = "unit_claim_recovered"
	UnitTDDRecorded         Action      = "unit_tdd_recorded"
	UnitReviewHandedOff     Action      = "unit_review_handed_off"
	UnitReviewRecovered     Action      = "unit_review_recovered"
	UnitReviewRecorded      Action      = "unit_review_recorded"
	AggregateReviewRecorded Action      = "aggregate_review_recorded"
	UnitCompleted           Action      = "unit_completed"
	Workflow                SubjectKind = "workflow"
	Event                   SubjectKind = "event"
	Artifact                SubjectKind = "artifact"
	Plan                    SubjectKind = "plan"
	WorkUnit                SubjectKind = "work_unit"
	Evidence                SubjectKind = "evidence"
	Review                  SubjectKind = "review"
)

var (
	workflowID = regexp.MustCompile(`^wf-[0-9a-f]{24}$`)
	unitID     = regexp.MustCompile(`^wu-[0-9a-f]{24}$`)
	allowed    = map[Action]SubjectKind{
		WorkflowCreated: Workflow, ExplorationRecorded: Artifact, SpecificationRecorded: Artifact, DesignRecorded: Artifact,
		PlanSubmitted: Plan, PlanApproved: Plan, ImplementationStarted: Event, WorkflowCompleted: Event,
		WorkflowAbandoned: Event, UnitClaimed: WorkUnit, UnitClaimRecovered: WorkUnit, UnitTDDRecorded: Evidence, UnitReviewHandedOff: WorkUnit, UnitReviewRecovered: WorkUnit,
		UnitReviewRecorded: Review, UnitCompleted: WorkUnit, AggregateReviewRecorded: Artifact,
	}
)

func New(wf, unit string, action Action, actor string, at time.Time, subject Subject) Entry {
	return Entry{WorkflowID: wf, UnitID: unit, Action: action, Actor: actor, At: ids.FormatTime(at), Subject: subject}
}
func WorkflowSubject(id string) Subject { return Subject{Workflow, id} }
func EventSubject(id string, revision int64) Subject {
	return Subject{Event, id + "@" + strconv.FormatInt(revision, 10)}
}
func ArtifactSubject(id int64) Subject { return Subject{Artifact, strconv.FormatInt(id, 10)} }
func PlanSubject(id string) Subject    { return Subject{Plan, id} }
func UnitSubject(id string) Subject    { return Subject{WorkUnit, id} }
func EvidenceSubject(id string, revision int64) Subject {
	return Subject{Evidence, id + "@" + strconv.FormatInt(revision, 10)}
}
func ReviewSubject(id string, revision int64) Subject {
	return Subject{Review, id + "@" + strconv.FormatInt(revision, 10)}
}

func AppendTx(ctx context.Context, tx *sql.Tx, e Entry) error {
	if !workflowID.MatchString(e.WorkflowID) || strings.TrimSpace(e.Actor) == "" || e.At == "" || allowed[e.Action] != e.Subject.Kind || !validSubject(e.Subject) {
		return fmt.Errorf("invalid activity")
	}
	requiresUnit := e.Subject.Kind == WorkUnit || e.Subject.Kind == Evidence || e.Subject.Kind == Review
	if requiresUnit != (e.UnitID != "") || (e.UnitID != "" && !unitID.MatchString(e.UnitID)) {
		return fmt.Errorf("invalid activity unit")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES(?,NULLIF(?,''),?,?,?,?,?)`, e.WorkflowID, e.UnitID, e.Action, e.Actor, e.At, e.Subject.Kind, e.Subject.ID)
	return err
}

func validSubject(s Subject) bool {
	switch s.Kind {
	case Workflow, Plan:
		return workflowID.MatchString(s.ID)
	case WorkUnit:
		return unitID.MatchString(s.ID)
	case Event:
		parts := strings.Split(s.ID, "@")
		return len(parts) == 2 && workflowID.MatchString(parts[0]) && positive(parts[1])
	case Evidence, Review:
		parts := strings.Split(s.ID, "@")
		return len(parts) == 2 && unitID.MatchString(parts[0]) && positive(parts[1])
	case Artifact:
		return positive(s.ID)
	}
	return false
}
func positive(raw string) bool { n, err := strconv.ParseInt(raw, 10, 64); return err == nil && n > 0 }
