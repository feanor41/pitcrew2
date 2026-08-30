package history

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
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
