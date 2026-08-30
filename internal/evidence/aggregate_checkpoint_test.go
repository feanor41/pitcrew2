package evidence

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestAggregateApprovalRequiresCurrentBundleAndIdentifiableCheckpoint(t *testing.T) {
	svc, db, wfID, unitID := evidenceService(t)
	if _, err := db.Exec(`INSERT INTO unit_coverage(workflow_id,unit_id,requirement_id,scenario_id) VALUES(?,?,?,?)`, wfID, unitID, "REQ-CHK-001", "SCN-CHK-001"); err != nil {
		t.Fatal(err)
	}
	unit := structuredTDD("fingerprint-a")
	unit.ScenarioResults = []ScenarioResult{{ScenarioID: "SCN-CHK-001", Outcome: "exit 0", VerificationID: unit.VerificationRuns[0].ID}}
	for index := range unit.VerificationRuns {
		unit.VerificationRuns[index].ScenarioIDs = []string{"SCN-CHK-001"}
	}
	if err := svc.RecordTDD(context.Background(), wfID, unitID, 1, unit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE work_units SET state='done' WHERE id=?`, unitID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE workflows SET state='ready_to_complete',revision=2 WHERE id=?`, wfID); err != nil {
		t.Fatal(err)
	}
	review := AggregateReview{Actor: "aggregate-reviewer", Verdict: Approved, VerificationRuns: []VerificationRun{{ID: "vr-aggregate", Tier: AggregateFull, Command: "go test ./...", Outcome: "exit 0", RepositoryFingerprint: "fingerprint-a", ScenarioIDs: []string{"SCN-CHK-001"}}}}
	if _, err := svc.CompleteAggregate(context.Background(), wfID, 2, review); err == nil || !strings.Contains(err.Error(), "checkpoint") {
		t.Fatalf("missing checkpoint error = %v", err)
	}
	review.Checkpoint = identifiableCheckpoint(t, true)
	review.VerificationRuns = nil
	if _, err := svc.CompleteAggregate(context.Background(), wfID, 2, review); err == nil || !strings.Contains(err.Error(), "aggregate_full") {
		t.Fatalf("incomplete aggregate bundle error = %v", err)
	}
	review.VerificationRuns = []VerificationRun{{ID: "vr-aggregate", Tier: AggregateFull, Command: "go test ./...", Outcome: "exit 0", RepositoryFingerprint: "fingerprint-a", ScenarioIDs: []string{"SCN-CHK-001"}}}
	review.Checkpoint = &ReviewedCheckpoint{Dirty: true}
	if _, err := svc.CompleteAggregate(context.Background(), wfID, 2, review); err == nil || !strings.Contains(err.Error(), "checkpoint") {
		t.Fatalf("unidentified checkpoint error = %v", err)
	}
	review.Checkpoint = identifiableCheckpoint(t, true)
	if out, err := svc.CompleteAggregate(context.Background(), wfID, 2, review); err != nil || out.State != "completed" {
		t.Fatalf("dirty aggregate completion = %#v, %v", out, err)
	}
	var dirty int
	if err := db.QueryRow(`SELECT dirty FROM reviewed_checkpoints WHERE workflow_id=? AND aggregate_revision=3`, wfID).Scan(&dirty); err != nil || dirty != 1 {
		t.Fatalf("checkpoint dirty=%d, %v", dirty, err)
	}
}

func TestPublicationVerificationIsSeparateFromAggregateBundle(t *testing.T) {
	record := VerificationRun{ID: "vr-publication", Tier: PublicationFull, Command: "sh scripts/tests/run.sh", Outcome: "exit 0", RepositoryFingerprint: "fingerprint-a"}
	if err := record.Validate(); err != nil {
		t.Fatalf("publication record should be persistable without becoming an aggregate gate: %v", err)
	}
}

func identifiableCheckpoint(t *testing.T, dirty bool) *ReviewedCheckpoint {
	t.Helper()
	return &ReviewedCheckpoint{
		ProjectID:    strings.Repeat("1", 64),
		CheckoutRoot: filepath.Clean(t.TempDir()),
		BaseRevision: strings.Repeat("2", 40),
		HeadRevision: strings.Repeat("3", 40),
		ResultDigest: strings.Repeat("4", 64),
		Dirty:        dirty,
	}
}
