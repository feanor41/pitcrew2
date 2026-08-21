package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestWorkflowJSONUsesTheAggregateContract(t *testing.T) {
	encoded, err := json.Marshal(Workflow{ID: "wf-id", Revision: 3, State: Designing, Goal: "goal", CreatedAt: "created", UpdatedAt: "updated"})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"id":"wf-id","revision":3,"state":"designing","goal":"goal","created_at":"created","updated_at":"updated"}` {
		t.Fatalf("workflow JSON = %s", encoded)
	}
}

func TestLifecyclePersistsTransitionsAndAppendOnlyEvents(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	wf, err := svc.Create(ctx, "ship safely", "master")
	if err != nil || wf.State != Draft || wf.Revision != 1 {
		t.Fatalf("Create() = %#v, %v", wf, err)
	}
	steps := []struct {
		event EventType
		state State
	}{
		{Explore, Exploring}, {Specify, Specifying}, {Design, Designing},
		{Plan, Planning}, {ApprovePlan, PlanApproved}, {BeginImplementation, Implementing},
		{AllUnitsCompleted, ReadyToComplete}, {Complete, Completed},
	}
	for _, step := range steps {
		wf, err = svc.Transition(ctx, wf.ID, wf.Revision, step.event, "actor")
		if err != nil || wf.State != step.state {
			t.Fatalf("Transition(%s) = %#v, %v", step.event, wf, err)
		}
	}
	events, err := svc.Events(ctx, wf.ID)
	if err != nil || len(events) != 9 || events[len(events)-1].RevisionAfter != wf.Revision {
		t.Fatalf("Events() = %#v, %v", events, err)
	}
}

func TestInvalidTransitionAndCASDoNotMutate(t *testing.T) {
	svc, _ := testService(t)
	wf, err := svc.Create(context.Background(), "goal", "master")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(context.Background(), wf.ID, wf.Revision, Specify, "specifier"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid transition error = %v", err)
	}
	if _, err := svc.Transition(context.Background(), wf.ID, wf.Revision+1, Explore, "explorer"); !errors.Is(err, store.ErrCASMismatch) {
		t.Fatalf("CAS error = %v", err)
	}
	got, _ := svc.Get(context.Background(), wf.ID)
	events, _ := svc.Events(context.Background(), wf.ID)
	if got.Revision != 1 || got.State != Draft || len(events) != 1 {
		t.Fatalf("workflow mutated: %#v, events=%d", got, len(events))
	}
}

func TestTransitionMatrixExecutesEveryLegalAndRejectsEverySourceState(t *testing.T) {
	legal := []struct {
		from  State
		event EventType
		to    State
	}{
		{Draft, Explore, Exploring},
		{Draft, BeginImplementation, Implementing},
		{Exploring, Specify, Specifying},
		{Exploring, Design, Designing},
		{Exploring, BeginImplementation, Implementing},
		{Specifying, Design, Designing},
		{Designing, Plan, Planning},
		{Planning, ApprovePlan, PlanApproved},
		{PlanApproved, BeginImplementation, Implementing},
		{Implementing, AllUnitsCompleted, ReadyToComplete},
		{ReadyToComplete, Complete, Completed},
	}
	for _, tt := range legal {
		t.Run(string(tt.from)+"_"+string(tt.event), func(t *testing.T) {
			svc, db := testService(t)
			wf, err := svc.Create(context.Background(), "goal", "master")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE workflows SET state=? WHERE id=?`, tt.from, wf.ID); err != nil {
				t.Fatal(err)
			}
			got, err := svc.Transition(context.Background(), wf.ID, 1, tt.event, "actor")
			if err != nil || got.State != tt.to || got.Revision != 2 {
				t.Fatalf("Transition(%s,%s)=%#v, %v", tt.from, tt.event, got, err)
			}
			events, err := svc.Events(context.Background(), wf.ID)
			if err != nil || len(events) != 2 || events[1].From != tt.from || events[1].To != tt.to {
				t.Fatalf("events=%#v, %v", events, err)
			}
		})
	}
	for _, from := range []State{Draft, Exploring, Specifying, Designing, Planning, PlanApproved, Implementing, ReadyToComplete} {
		t.Run(string(from)+"_abandon", func(t *testing.T) {
			svc, db := testService(t)
			wf, err := svc.Create(context.Background(), "goal", "master")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE workflows SET state=? WHERE id=?`, from, wf.ID); err != nil {
				t.Fatal(err)
			}
			got, err := svc.Abandon(context.Background(), wf.ID, 1, "master", "stop")
			if err != nil || got.State != Abandoned || got.Revision != 2 {
				t.Fatalf("Abandon(%s)=%#v, %v", from, got, err)
			}
		})
	}

	invalid := []struct {
		from     State
		event    EventType
		expected []State
	}{
		{Draft, Complete, []State{ReadyToComplete}},
		{Exploring, Plan, []State{Designing}},
		{Specifying, Explore, []State{Draft}},
		{Designing, Specify, []State{Exploring}},
		{Planning, Design, []State{Exploring, Specifying}},
		{PlanApproved, ApprovePlan, []State{Planning}},
		{Implementing, Plan, []State{Designing}},
		{ReadyToComplete, BeginImplementation, []State{Draft, Exploring, PlanApproved}},
		{Completed, Complete, []State{ReadyToComplete}},
		{Abandoned, Explore, []State{Draft}},
	}
	for _, tt := range invalid {
		t.Run(string(tt.from)+"_rejects_"+string(tt.event), func(t *testing.T) {
			svc, db := testService(t)
			wf, err := svc.Create(context.Background(), "goal", "master")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE workflows SET state=? WHERE id=?`, tt.from, wf.ID); err != nil {
				t.Fatal(err)
			}
			_, err = svc.Transition(context.Background(), wf.ID, 1, tt.event, "actor")
			var transitionErr *TransitionError
			if !errors.As(err, &transitionErr) || transitionErr.Current != tt.from || !statesEqual(transitionErr.Expected, tt.expected) {
				t.Fatalf("transition error=%#v, want current=%s expected=%v", err, tt.from, tt.expected)
			}
			message := err.Error()
			if !strings.Contains(message, "current state "+string(tt.from)) {
				t.Fatalf("error omits current state: %q", message)
			}
			for _, expected := range tt.expected {
				if !strings.Contains(message, string(expected)) {
					t.Fatalf("error omits expected state %s: %q", expected, message)
				}
			}
			got, _ := svc.Get(context.Background(), wf.ID)
			events, _ := svc.Events(context.Background(), wf.ID)
			if got.State != tt.from || got.Revision != 1 || len(events) != 1 {
				t.Fatalf("invalid transition mutated workflow=%#v events=%d", got, len(events))
			}
		})
	}
}

func statesEqual(got, want []State) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestAbandonRecordsReasonAndRetainsRelatedRows(t *testing.T) {
	svc, db := testService(t)
	wf, err := svc.Create(context.Background(), "goal", "master")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,?,?,?,?)`, wf.ID, "plan", "internal", 1, `{}`); err != nil {
		t.Fatal(err)
	}
	abandoned, err := svc.Abandon(context.Background(), wf.ID, wf.Revision, "master", "no longer needed")
	if err != nil || abandoned.State != Abandoned {
		t.Fatalf("Abandon() = %#v, %v", abandoned, err)
	}
	events, _ := svc.Events(context.Background(), wf.ID)
	var plans int
	if err := db.QueryRow(`SELECT count(*) FROM plans WHERE workflow_id=?`, wf.ID).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if plans != 1 || events[len(events)-1].Reason != "no longer needed" {
		t.Fatalf("plans=%d, event=%#v", plans, events[len(events)-1])
	}
	if _, err := svc.Abandon(context.Background(), wf.ID, abandoned.Revision, "master", "again"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal abandon error = %v", err)
	}
}

func TestAbandonRequiresAReason(t *testing.T) {
	svc, _ := testService(t)
	wf, err := svc.Create(context.Background(), "goal", "master")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Abandon(context.Background(), wf.ID, wf.Revision, "master", "  "); err == nil {
		t.Fatal("empty abandonment reason was accepted")
	}
}

func testService(t *testing.T) (*Service, DB) {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	return New(s, func() time.Time { return now }), s.DB()
}
