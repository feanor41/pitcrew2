package activity

import (
	"context"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestActivityAppendAcceptsOnlyTypedNavigationSafeSubjects(t *testing.T) {
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.DB().Exec(`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-000000000000000000000001',1,'draft','work','goal','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES('wu-000000000000000000000001','wf-000000000000000000000001','unit','internal','[]','[]',1,1,'pending',1)`); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		entry   Entry
		wantErr bool
	}{
		{"workflow", New("wf-000000000000000000000001", "", WorkflowCreated, "actor", time.Now(), WorkflowSubject("wf-000000000000000000000001")), false},
		{"evidence", New("wf-000000000000000000000001", "wu-000000000000000000000001", UnitTDDRecorded, "actor", time.Now(), EvidenceSubject("wu-000000000000000000000001", 2)), false},
		{"review handoff", New("wf-000000000000000000000001", "wu-000000000000000000000001", UnitReviewHandedOff, "reviewer", time.Now(), UnitSubject("wu-000000000000000000000001")), false},
		{"aggregate review", New("wf-000000000000000000000001", "", AggregateReviewRecorded, "reviewer", time.Now(), ArtifactSubject(1)), false},
		{"path rejected", New("wf-000000000000000000000001", "", WorkflowCreated, "actor", time.Now(), Subject{Kind: Workflow, ID: "/tmp/handle.json"}), true},
		{"claim-like kind rejected", New("wf-000000000000000000000001", "wu-000000000000000000000001", UnitClaimed, "actor", time.Now(), Subject{Kind: "claim", ID: "secret"}), true},
		{"action subject mismatch", New("wf-000000000000000000000001", "", WorkflowCreated, "actor", time.Now(), UnitSubject("wu-000000000000000000000001")), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := s.DB().BeginTx(context.Background(), nil)
			if err == nil {
				err = AppendTx(context.Background(), tx, tt.entry)
			}
			_ = tx.Rollback()
			if (err != nil) != tt.wantErr {
				t.Fatalf("AppendTx() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestActivityAppendRollsBackWithItsDomainTransaction(t *testing.T) {
	s, _ := store.Open(context.Background(), t.TempDir())
	defer s.Close()
	tx, _ := s.DB().BeginTx(context.Background(), nil)
	_, _ = tx.Exec(`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-000000000000000000000001',1,'draft','work','goal','now','now')`)
	entry := New("wf-000000000000000000000001", "", WorkflowCreated, "actor", time.Now(), WorkflowSubject("wf-000000000000000000000001"))
	if err := AppendTx(context.Background(), tx, entry); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	for _, table := range []string{"workflows", "activities"} {
		var count int
		if err := s.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d error=%v", table, count, err)
		}
	}
}
