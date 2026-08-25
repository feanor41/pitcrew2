package workflow

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestContinueCreatesLinkedDraftWithoutMutatingTerminalSource(t *testing.T) {
	for _, state := range []State{Completed, Abandoned} {
		t.Run(string(state), func(t *testing.T) {
			svc, db := testService(t)
			source, err := svc.Create(context.Background(), "Original", "exact\ngoal", "daimon")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE workflows SET state=?,revision=12 WHERE id=?`, state, source.ID); err != nil {
				t.Fatal(err)
			}
			wantName := "Original"
			if state == Abandoned {
				_, _ = db.Exec(`UPDATE workflows SET name=NULL WHERE id=?`, source.ID)
				wantName = "exact"
			}
			before := continuationSourceSnapshot(t, db, source.ID)

			first, err := svc.Continue(context.Background(), source.ID, "daimon")
			if err != nil {
				t.Fatal(err)
			}
			second, err := svc.Continue(context.Background(), source.ID, "daimon")
			if err != nil {
				t.Fatal(err)
			}
			if first.Workflow.ID == second.Workflow.ID || first.Workflow.State != Draft || first.Workflow.Revision != 1 || first.Workflow.Name != wantName || first.Workflow.NameDerived || first.Workflow.Goal != "exact\ngoal" {
				t.Fatalf("successors=%#v %#v", first, second)
			}
			if first.Predecessor.ID != source.ID || first.Predecessor.State != state || first.Predecessor.Revision != 12 {
				t.Fatalf("predecessor=%#v", first.Predecessor)
			}
			var kind, content, actor, action, subjectKind string
			var acceptedRevision, childEvents, childActivities int
			err = db.QueryRow(`SELECT a.kind,a.content,a.actor,a.accepted_revision,v.action,v.subject_kind FROM artifacts a JOIN activities v ON v.workflow_id=a.workflow_id AND v.subject_id=CAST(a.id AS TEXT) WHERE a.workflow_id=?`, first.Workflow.ID).
				Scan(&kind, &content, &actor, &acceptedRevision, &action, &subjectKind)
			if err != nil {
				t.Fatal(err)
			}
			_ = db.QueryRow(`SELECT count(*) FROM events WHERE workflow_id=?`, first.Workflow.ID).Scan(&childEvents)
			_ = db.QueryRow(`SELECT count(*) FROM activities WHERE workflow_id=?`, first.Workflow.ID).Scan(&childActivities)
			wantContent := fmt.Sprintf(`{"predecessor_workflow_id":%q,"predecessor_state":%q,"predecessor_revision":12}`, source.ID, state)
			if kind != "continuation" || content != wantContent || actor != "daimon" || acceptedRevision != 1 || action != "continuation_recorded" || subjectKind != "artifact" || childEvents != 1 || childActivities != 1 {
				t.Fatalf("lineage=%s %s %s %d %s %s events=%d activities=%d", kind, content, actor, acceptedRevision, action, subjectKind, childEvents, childActivities)
			}
			if after := continuationSourceSnapshot(t, db, source.ID); after != before {
				t.Fatalf("source mutated:\nbefore=%s\nafter=%s", before, after)
			}
		})
	}
}

func TestContinueRejectsEveryNonTerminalState(t *testing.T) {
	for _, state := range nonTerminalStates {
		t.Run(string(state), func(t *testing.T) {
			svc, db := testService(t)
			source, err := svc.Create(context.Background(), "Original", "goal", "daimon")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = db.Exec(`UPDATE workflows SET state=? WHERE id=?`, state, source.ID)
			if _, err = svc.Continue(context.Background(), source.ID, "daimon"); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Continue(%s) error=%v", state, err)
			}
		})
	}
}

func TestContinueRejectsInvalidSourcesAndRollsBackFailures(t *testing.T) {
	for _, tt := range []struct {
		name, source, actor, trigger string
		want                         error
	}{
		{"missing", "wf-000000000000000000000001", "daimon", "", ErrNotFound},
		{"nonterminal", "source", "daimon", "", ErrInvalidTransition},
		{"invalid actor", "source", " ", "", ErrInvalidActor},
		{"artifact failure", "source", "daimon", `CREATE TRIGGER fail_continuation BEFORE INSERT ON artifacts BEGIN SELECT RAISE(ABORT,'artifact failure'); END`, nil},
		{"activity failure", "source", "daimon", `CREATE TRIGGER fail_continuation BEFORE INSERT ON activities BEGIN SELECT RAISE(ABORT,'activity failure'); END`, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := testService(t)
			sourceID := tt.source
			if sourceID == "source" {
				source, err := svc.Create(context.Background(), "Original", "goal", "daimon")
				if err != nil {
					t.Fatal(err)
				}
				sourceID = source.ID
				if tt.name != "nonterminal" {
					_, _ = db.Exec(`UPDATE workflows SET state='abandoned',revision=2 WHERE id=?`, sourceID)
				}
			}
			if tt.trigger != "" {
				if _, err := db.Exec(tt.trigger); err != nil {
					t.Fatal(err)
				}
			}
			before := continuationSourceSnapshot(t, db, sourceID)
			var workflowCount int
			_ = db.QueryRow(`SELECT count(*) FROM workflows`).Scan(&workflowCount)
			_, err := svc.Continue(context.Background(), sourceID, tt.actor)
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want %v", err, tt.want)
			}
			if tt.want == nil && err == nil {
				t.Fatal("forced failure succeeded")
			}
			var afterCount int
			_ = db.QueryRow(`SELECT count(*) FROM workflows`).Scan(&afterCount)
			if afterCount != workflowCount || continuationSourceSnapshot(t, db, sourceID) != before {
				t.Fatalf("failed continuation mutated records: workflows %d -> %d", workflowCount, afterCount)
			}
		})
	}
}

func continuationSourceSnapshot(t *testing.T, db DB, id string) string {
	t.Helper()
	var row string
	_ = db.QueryRow(`SELECT printf('%s|%d|%s|%s|%s|%s|%s',id,revision,state,coalesce(name,'NULL'),goal,created_at,updated_at) FROM workflows WHERE id=?`, id).Scan(&row)
	for _, table := range []string{"events", "artifacts", "activities"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM `+table+` WHERE workflow_id=?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		row += fmt.Sprintf("|%s:%d", table, count)
	}
	return row
}
