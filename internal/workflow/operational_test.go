package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestAppendOperationalPreservesWorkflowAndOrdersReports(t *testing.T) {
	svc, db := testService(t)
	wf, err := svc.Create(context.Background(), "Work", "goal", "daimon")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := svc.Get(context.Background(), wf.ID)
	eventsBefore, _ := svc.Events(context.Background(), wf.ID)
	for _, content := range []string{`{"status":"advanced","summary":"first","next_action":"test"}`, `{"status":"blocked","summary":"second","next_action":"wait"}`} {
		if _, err = svc.AppendOperational(context.Background(), wf.ID, 1, "progress", content, "daimon", activity.ProgressRecorded); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := svc.Get(context.Background(), wf.ID)
	eventsAfter, _ := svc.Events(context.Background(), wf.ID)
	artifacts, _ := svc.Artifacts(context.Background(), wf.ID)
	if after != before || len(eventsAfter) != len(eventsBefore) || len(artifacts) != 2 || artifacts[0].Content == artifacts[1].Content || artifacts[0].Revision != 1 || artifacts[1].Revision != 1 {
		t.Fatalf("workflow=%#v before=%#v events=%d/%d artifacts=%#v", after, before, len(eventsAfter), len(eventsBefore), artifacts)
	}
	var activities int
	if err = db.QueryRow(`SELECT count(*) FROM activities WHERE workflow_id=? AND action='progress_recorded' AND subject_kind='artifact'`, wf.ID).Scan(&activities); err != nil || activities != 2 {
		t.Fatalf("activities=%d err=%v", activities, err)
	}
}

func TestAppendOperationalRejectsStaleTerminalAndRollsBackActivityFailure(t *testing.T) {
	for _, tt := range []struct {
		name    string
		state   State
		expect  int64
		trigger bool
		want    error
	}{
		{"stale", Draft, 2, false, store.ErrCASMismatch},
		{"completed", Completed, 1, false, ErrInvalidTransition},
		{"abandoned", Abandoned, 1, false, ErrInvalidTransition},
		{"activity failure", Draft, 1, true, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := testService(t)
			wf, err := svc.Create(context.Background(), "Work", "goal", "daimon")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = db.Exec(`UPDATE workflows SET state=? WHERE id=?`, tt.state, wf.ID)
			if tt.trigger {
				_, _ = db.Exec(`CREATE TRIGGER fail_progress BEFORE INSERT ON activities WHEN NEW.action='progress_recorded' BEGIN SELECT RAISE(ABORT,'activity failure'); END`)
			}
			before, _ := svc.Get(context.Background(), wf.ID)
			_, err = svc.AppendOperational(context.Background(), wf.ID, tt.expect, "progress", `{}`, "daimon", activity.ProgressRecorded)
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want=%v", err, tt.want)
			}
			if tt.want == nil && err == nil {
				t.Fatal("forced failure succeeded")
			}
			after, _ := svc.Get(context.Background(), wf.ID)
			var artifacts, progressActivities int
			_ = db.QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=?`, wf.ID).Scan(&artifacts)
			_ = db.QueryRow(`SELECT count(*) FROM activities WHERE workflow_id=? AND action='progress_recorded'`, wf.ID).Scan(&progressActivities)
			if after != before || artifacts != 0 || progressActivities != 0 {
				t.Fatalf("mutation after error: before=%#v after=%#v artifacts=%d activities=%d", before, after, artifacts, progressActivities)
			}
		})
	}
}
