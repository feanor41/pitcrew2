package workflow

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNormativeArtifactResolvesStableStructuredContext(t *testing.T) {
	svc, _ := testService(t)
	wf, err := svc.Create(context.Background(), "typed", "goal", "aion")
	if err != nil {
		t.Fatal(err)
	}
	wf, err = svc.RecordNormativeArtifact(context.Background(), wf.ID, wf.Revision, Explore, "exploration", NormativeArtifact{
		SchemaVersion: 1,
		Content:       "typed context",
		Entries: []NormativeEntry{{
			Kind: "section", ID: "SEC-CONTEXT", Operation: "add", Body: json.RawMessage(`{"text":"bounded"}`),
		}},
	}, "explorer")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.ResolveNormative(context.Background(), wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Structured || len(resolved.Entries) != 1 || resolved.Entries[0].ID != "SEC-CONTEXT" || resolved.Entries[0].Source.Revision != wf.Revision {
		t.Fatalf("resolved=%#v", resolved)
	}
}
