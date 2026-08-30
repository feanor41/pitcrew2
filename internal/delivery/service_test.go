package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestServiceStartsIdempotentlyAndRejectsConflictingOrUnboundedInput(t *testing.T) {
	svc, db := testService(t, time.Date(2026, 8, 29, 1, 2, 3, 0, time.UTC))
	in := StartInput{OperationKey: " op-1 ", Route: DirectInline, Goal: " ship safely ", RouteReason: " small change "}
	first, err := svc.Start(context.Background(), " aion ", in)
	if err != nil || first.Status != InProgress || first.Revision != 1 || first.OperationKey != "op-1" || first.Goal != "ship safely" || first.CreatorActor != "aion" || first.FinishedAt != "" {
		t.Fatalf("Start() = %#v, %v", first, err)
	}
	replayed, err := svc.Start(context.Background(), "aion", in)
	if err != nil || replayed != first {
		t.Fatalf("replay = %#v, %v; want unchanged %#v", replayed, err, first)
	}
	if _, err = svc.Start(context.Background(), "aion", StartInput{OperationKey: "op-1", Route: DelegatedDirect, Goal: "ship safely"}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	invalid := []StartInput{
		{OperationKey: " ", Route: DirectInline, Goal: "goal"},
		{OperationKey: "fresh", Route: Route("full_workflow"), Goal: "goal"},
		{OperationKey: "fresh", Route: DirectInline, Goal: strings.Repeat("界", 4001)},
		{OperationKey: "fresh", Route: DirectInline, Goal: "goal", RouteReason: strings.Repeat("界", 501)},
	}
	for _, input := range invalid {
		if _, err = svc.Start(context.Background(), "aion", input); err == nil {
			t.Fatalf("invalid Start(%#v) succeeded", input)
		}
	}
	if _, err = svc.Start(context.Background(), " ", StartInput{OperationKey: "fresh", Route: DirectInline, Goal: "goal"}); err == nil {
		t.Fatal("blank actor succeeded")
	}
	var count int
	if err = db.DB().QueryRow(`SELECT count(*) FROM direct_delivery_traces`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rows = %d, %v; want 1", count, err)
	}
	if utf8.RuneCountInString(first.Goal) != len([]rune(first.Goal)) {
		t.Fatal("test rune accounting is inconsistent")
	}
}

func TestServiceCASLifecycleTerminalTruthAndNoOpSafety(t *testing.T) {
	clock := &steppingClock{at: time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC)}
	svc, _ := testService(t, clock.Now())
	svc.now = clock.Now
	trace, err := svc.Start(context.Background(), "aion", StartInput{OperationKey: "delivery", Route: DelegatedDirect, Goal: "goal"})
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := svc.Update(context.Background(), trace.ID, 1, "aion", UpdateInput{Status: Blocked, Summary: "dependency unavailable", NextAction: "wait for user"})
	if err != nil || blocked.Revision != 2 || blocked.FinishedAt != "" || blocked.UpdatedAt == trace.UpdatedAt {
		t.Fatalf("blocked = %#v, %v", blocked, err)
	}
	if _, err = svc.Update(context.Background(), trace.ID, 1, "aion", UpdateInput{Status: InProgress}); !errors.Is(err, store.ErrCASMismatch) {
		t.Fatalf("stale update error = %v", err)
	}
	var conflict *RevisionConflict
	if !errors.As(err, &conflict) || conflict.DeliveryID != trace.ID || conflict.Expected != 1 || conflict.Current != 2 || conflict.InspectCommand() != "pitcrew delivery show --delivery-id "+trace.ID || !strings.Contains(err.Error(), "delivery revision mismatch for "+trace.ID) || !strings.Contains(err.Error(), conflict.InspectCommand()) {
		t.Fatalf("stale update lacks delivery-aware inspection context: %#v, %v", conflict, err)
	}
	for _, input := range []UpdateInput{
		{Status: Status("unknown")},
		{Status: InProgress, Summary: strings.Repeat("界", 501)},
		{Status: InProgress, NextAction: strings.Repeat("界", 201)},
	} {
		if _, err = svc.Update(context.Background(), trace.ID, 2, "aion", input); err == nil {
			t.Fatalf("invalid update %#v succeeded", input)
		}
	}
	if _, err = svc.Update(context.Background(), trace.ID, 2, " ", UpdateInput{Status: InProgress}); err == nil {
		t.Fatal("blank update actor succeeded")
	}
	resumed, err := svc.Update(context.Background(), trace.ID, 2, "worker", UpdateInput{Status: InProgress, Summary: "dependency restored", NextAction: "run tests"})
	if err != nil || resumed.Status != InProgress || resumed.UpdaterActor != "worker" || resumed.Revision != 3 {
		t.Fatalf("resumed = %#v, %v", resumed, err)
	}
	if _, err = svc.Update(context.Background(), trace.ID, 3, "worker", UpdateInput{Status: InProgress, Summary: resumed.Summary, NextAction: resumed.NextAction}); !errors.Is(err, ErrNoChange) {
		t.Fatalf("no-op error = %v", err)
	}
	done, err := svc.Update(context.Background(), trace.ID, 3, "worker", UpdateInput{Status: Completed, Summary: "verified"})
	if err != nil || done.Revision != 4 || done.FinishedAt == "" || done.FinishedAt != done.UpdatedAt {
		t.Fatalf("completed = %#v, %v", done, err)
	}
	if _, err = svc.Update(context.Background(), trace.ID, 4, "worker", UpdateInput{Status: Failed}); !errors.Is(err, ErrTerminal) {
		t.Fatalf("terminal update error = %v", err)
	}
	got, err := svc.Get(context.Background(), trace.ID)
	if err != nil || got != done {
		t.Fatalf("Get() = %#v, %v; want %#v", got, err, done)
	}
}

func TestServiceDistinguishesOutcomesAndIsolatesProjects(t *testing.T) {
	for _, from := range []Status{InProgress, Blocked, Interrupted} {
		for _, to := range []Status{InProgress, Blocked, Interrupted, Completed, Cancelled, Failed} {
			svc, _ := testService(t, time.Now())
			trace, _ := svc.Start(context.Background(), "aion", StartInput{OperationKey: "same-key", Route: DirectInline, Goal: "goal"})
			if from != InProgress {
				trace, _ = svc.Update(context.Background(), trace.ID, trace.Revision, "aion", UpdateInput{Status: from})
			}
			got, err := svc.Update(context.Background(), trace.ID, trace.Revision, "aion", UpdateInput{Status: to, Summary: "observed"})
			if err != nil || (got.FinishedAt != "") != terminal(to) {
				t.Fatalf("%s to %s = %#v, %v", from, to, got, err)
			}
		}
	}
	left, _ := testService(t, time.Now())
	right, _ := testService(t, time.Now())
	a, errA := left.Start(context.Background(), "aion", StartInput{OperationKey: "shared", Route: DirectInline, Goal: "left"})
	b, errB := right.Start(context.Background(), "aion", StartInput{OperationKey: "shared", Route: DirectInline, Goal: "right"})
	if errA != nil || errB != nil || a.ID == b.ID {
		t.Fatalf("isolated starts = %#v/%v %#v/%v", a, errA, b, errB)
	}
	if _, err := left.Get(context.Background(), b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project Get error = %v", err)
	}
}

type steppingClock struct{ at time.Time }

func (c *steppingClock) Now() time.Time { c.at = c.at.Add(time.Second); return c.at }

func testService(t *testing.T, now time.Time) (*Service, *store.Store) {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewService(s, func() time.Time { return now }), s
}
