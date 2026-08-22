package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

var (
	ErrInvalidTransition = errors.New("invalid workflow transition")
	ErrInvalidName       = errors.New("workflow name must contain 1 to 80 runes")
	ErrNotFound          = errors.New("workflow not found")
)

type DB = *sql.DB
type State string
type EventType string

const (
	Draft               State     = "draft"
	Exploring           State     = "exploring"
	Specifying          State     = "specifying"
	Designing           State     = "designing"
	Planning            State     = "planning"
	PlanApproved        State     = "plan_approved"
	Implementing        State     = "implementing"
	ReadyToComplete     State     = "ready_to_complete"
	Completed           State     = "completed"
	Abandoned           State     = "abandoned"
	Explore             EventType = "explore"
	Specify             EventType = "spec"
	Design              EventType = "design"
	Plan                EventType = "plan"
	ApprovePlan         EventType = "approve_plan"
	BeginImplementation EventType = "begin_implementation"
	AllUnitsCompleted   EventType = "all_units_completed"
	Complete            EventType = "complete"
)

type Workflow struct {
	ID          string `json:"id"`
	Revision    int64  `json:"revision"`
	State       State  `json:"state"`
	Name        string `json:"name"`
	NameDerived bool   `json:"name_derived"`
	Goal        string `json:"goal"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}
type Event struct {
	WorkflowID    string `json:"workflow_id"`
	From          State  `json:"from_state"`
	To            State  `json:"to_state"`
	Actor         string `json:"actor"`
	Reason        string `json:"reason"`
	RevisionAfter int64  `json:"revision_after"`
	At            string `json:"at"`
}
type TransitionError struct {
	Current  State
	Expected []State
	Event    EventType
}

func (e *TransitionError) Error() string {
	expected := make([]string, len(e.Expected))
	for i, state := range e.Expected {
		expected[i] = string(state)
	}
	if len(expected) == 0 {
		expected = []string{"no valid source state"}
	}
	return fmt.Sprintf("%v: current state %s; expected %s for %s", ErrInvalidTransition, e.Current, strings.Join(expected, " or "), e.Event)
}

func (e *TransitionError) Unwrap() error { return ErrInvalidTransition }

func transitionError(current State, event EventType) error {
	return &TransitionError{Current: current, Expected: expectedSources(event), Event: event}
}

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(s *store.Store, now func() time.Time) *Service { return &Service{db: s.DB(), now: now} }

func (s *Service) Create(ctx context.Context, name, goal, actor string) (Workflow, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return Workflow{}, err
	}
	id, err := ids.NewWorkflow()
	if err != nil {
		return Workflow{}, err
	}
	now := s.now()
	at := ids.FormatTime(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workflow{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES(?,1,?,?,?,?,?)`, id, Draft, name, goal, at, at); err != nil {
		return Workflow{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,?,?,?,?,1,?)`, id, "", Draft, actor, "", at); err != nil {
		return Workflow{}, err
	}
	if err = activity.AppendTx(ctx, tx, activity.New(id, "", activity.WorkflowCreated, actor, now, activity.WorkflowSubject(id))); err != nil {
		return Workflow{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workflow{}, err
	}
	return Workflow{ID: id, Revision: 1, State: Draft, Name: name, Goal: goal, CreatedAt: at, UpdatedAt: at}, nil
}

func (s *Service) Get(ctx context.Context, id string) (Workflow, error) {
	var w Workflow
	var name sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,revision,state,name,goal,created_at,updated_at FROM workflows WHERE id=?`, id).Scan(&w.ID, &w.Revision, &w.State, &name, &w.Goal, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Workflow{}, ErrNotFound
	}
	if err == nil {
		w.Name, w.NameDerived = DisplayName(name, w.Goal)
	}
	return w, err
}

func (s *Service) Transition(ctx context.Context, id string, expected int64, event EventType, actor string) (Workflow, error) {
	return s.transition(ctx, id, expected, event, actor, "")
}

func (s *Service) Abandon(ctx context.Context, id string, expected int64, actor, reason string) (Workflow, error) {
	if strings.TrimSpace(reason) == "" {
		return Workflow{}, errors.New("abandonment reason is required")
	}
	return s.transition(ctx, id, expected, "abandon", actor, reason)
}

func (s *Service) transition(ctx context.Context, id string, expected int64, event EventType, actor, reason string) (Workflow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workflow{}, err
	}
	defer tx.Rollback()
	current, err := workflowInTx(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Workflow{}, ErrNotFound
	}
	if err != nil {
		return Workflow{}, err
	}
	if current.Revision != expected {
		return Workflow{}, store.ErrCASMismatch
	}
	next, ok := nextState(current.State, event)
	if !ok {
		return Workflow{}, transitionError(current.State, event)
	}
	now := s.now()
	at, revision := ids.FormatTime(now), expected+1
	result, err := tx.ExecContext(ctx, `UPDATE workflows SET state=?,revision=?,updated_at=? WHERE id=? AND revision=?`, next, revision, at, id, expected)
	if err != nil {
		return Workflow{}, err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return Workflow{}, err
		}
		return Workflow{}, store.ErrCASMismatch
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,?,?,?,?,?,?)`, id, current.State, next, actor, reason, revision, at); err != nil {
		return Workflow{}, err
	}
	if action, ok := transitionActivity(event); ok {
		if err = activity.AppendTx(ctx, tx, activity.New(id, "", action, actor, now, activity.EventSubject(id, revision))); err != nil {
			return Workflow{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Workflow{}, err
	}
	current.State, current.Revision, current.UpdatedAt = next, revision, at
	return current, nil
}

func transitionActivity(event EventType) (activity.Action, bool) {
	switch event {
	case BeginImplementation:
		return activity.ImplementationStarted, true
	case Complete:
		return activity.WorkflowCompleted, true
	case "abandon":
		return activity.WorkflowAbandoned, true
	default:
		return "", false
	}
}

func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if count := len([]rune(name)); count == 0 || count > 80 {
		return "", ErrInvalidName
	}
	return name, nil
}

func DisplayName(name sql.NullString, goal string) (string, bool) {
	if name.Valid {
		return name.String, false
	}
	for _, line := range strings.Split(goal, "\n") {
		candidate := strings.Join(strings.Fields(line), " ")
		if candidate == "" {
			continue
		}
		runes := []rune(candidate)
		if len(runes) > 80 {
			candidate = string(runes[:80])
		}
		return candidate, true
	}
	return "Untitled workflow", true
}

func (s *Service) Events(ctx context.Context, id string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT workflow_id,from_state,to_state,actor,reason,revision_after,at FROM events WHERE workflow_id=? ORDER BY revision_after`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.WorkflowID, &e.From, &e.To, &e.Actor, &e.Reason, &e.RevisionAfter, &e.At); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func nextState(from State, event EventType) (State, bool) {
	if event == "abandon" && from != Completed && from != Abandoned {
		return Abandoned, true
	}
	next, ok := transitionTable[from][event]
	return next, ok
}

var transitionTable = map[State]map[EventType]State{
	Draft:      {Explore: Exploring},
	Exploring:  {Explore: Exploring, Specify: Specifying, Design: Designing},
	Specifying: {Specify: Specifying, Design: Designing}, Designing: {Design: Designing, Plan: Planning}, Planning: {ApprovePlan: PlanApproved},
	PlanApproved: {BeginImplementation: Implementing}, Implementing: {AllUnitsCompleted: ReadyToComplete},
}

var nonTerminalStates = []State{Draft, Exploring, Specifying, Designing, Planning, PlanApproved, Implementing, ReadyToComplete}

func expectedSources(event EventType) []State {
	if event == "abandon" {
		return append([]State(nil), nonTerminalStates...)
	}
	var expected []State
	for _, state := range nonTerminalStates {
		if _, ok := transitionTable[state][event]; ok {
			expected = append(expected, state)
		}
	}
	return expected
}
