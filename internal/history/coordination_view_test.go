package history

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestProjectViewsAreBoundedParityPreservingAndSecretFree(t *testing.T) {
	ctx := context.Background()
	service := New(openHistory(t, true))
	coordination, err := service.Project(ctx, "wf-new", ViewCoordination, "")
	if err != nil {
		t.Fatal(err)
	}
	assertOnlySelectedProjection(t, coordination, ViewCoordination)
	audit, err := service.Project(ctx, "wf-new", ViewAudit, "")
	if err != nil {
		t.Fatal(err)
	}
	assertOnlySelectedProjection(t, audit, ViewAudit)
	coordinationJSON, _ := json.Marshal(coordination)
	auditJSON, _ := json.Marshal(audit)
	if coordination.Workflow.Revision != audit.Audit.Workflow.Revision || coordination.Coordination.NextAction != audit.Audit.Synopsis.NextAction {
		t.Fatalf("coordination/audit parity = %#v / %#v", coordination, audit)
	}
	if coordination.Workflow.State != "abandoned" || coordination.Coordination.Current != nil || coordination.Coordination.Blocker != nil || len(coordination.Coordination.Ready) != 0 {
		t.Fatalf("terminal coordination retained executable unit state: %#v", coordination.Coordination)
	}
	if len(coordinationJSON)*10 > len(auditJSON) {
		t.Fatalf("coordination is not at least 90%% smaller: coordination=%d audit=%d", len(coordinationJSON), len(auditJSON))
	}
	for _, raw := range [][]byte{coordinationJSON, auditJSON} {
		for _, forbidden := range []string{"raw-handle-secret", "claim_id", "secret_hash", "handle_path"} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Fatalf("public projection contains %q: %s", forbidden, raw)
			}
		}
	}
}

func TestTerminalCoordinationClearsOperationalBlockers(t *testing.T) {
	ctx := context.Background()
	opened, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-terminal',9,'abandoned','Terminal','goal','now','now')`,
		`INSERT INTO work_units VALUES('wu-dependency','wf-terminal','dependency','scope','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-blocked','wf-terminal','blocked','scope','[]','["wu-dependency"]',1,1,'pending',NULL,0,1)`,
	} {
		if _, err = opened.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	projection, err := New(opened).Project(ctx, "wf-terminal", ViewCoordination, "")
	if err != nil {
		t.Fatal(err)
	}
	coordination := projection.Coordination
	if coordination.NextAction != "none" || coordination.Current != nil || coordination.Blocker != nil || len(coordination.Ready) != 0 {
		t.Fatalf("terminal coordination retained executable unit state: %#v", coordination)
	}
}

func assertOnlySelectedProjection(t *testing.T, projection Projection, selected View) {
	t.Helper()
	present := map[View]bool{
		ViewCoordination: projection.Coordination != nil,
		ViewPhase:        projection.Phase != nil,
		ViewUnit:         projection.Unit != nil,
		ViewAggregate:    projection.Aggregate != nil,
		ViewAudit:        projection.Audit != nil,
	}
	for view, value := range present {
		if value != (view == selected) {
			t.Fatalf("projection %s selected pointers = %#v", selected, present)
		}
	}
}
