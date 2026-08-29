package consolidate_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/consolidate"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

const graphWorkflowID = "wf-000000000000000000000001"

func TestGraphRejectsIncompleteWholeGraph(t *testing.T) {
	s := graphStore(t)
	seedGraph(t, s, 7, 11, "same")
	if _, err := s.DB().Exec(`DELETE FROM events WHERE workflow_id=?`, graphWorkflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := consolidate.LoadGraph(context.Background(), s.DB(), graphWorkflowID); !errors.Is(err, consolidate.ErrIncompleteGraph) {
		t.Fatalf("missing-event error = %v", err)
	}
	if _, err := s.DB().Exec(`INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,'','draft','actor','',1,'now')`, graphWorkflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`INSERT INTO activities(id,workflow_id,action,actor,at,subject_kind,subject_id) VALUES(12,?,'exploration_recorded','actor','now','artifact','999')`, graphWorkflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := consolidate.LoadGraph(context.Background(), s.DB(), graphWorkflowID); !errors.Is(err, consolidate.ErrIncompleteGraph) {
		t.Fatalf("dangling-subject error = %v", err)
	}
	broken := graphStore(t)
	seedGraph(t, broken, 8, 12, "same")
	if _, err := broken.DB().Exec(`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES('wu-000000000000000000000001',?,'unit','scope','[]','["wu-000000000000000000000999"]',1,1,'pending',1)`, graphWorkflowID); err != nil {
		t.Fatal(err)
	}
	if _, err := consolidate.LoadGraph(context.Background(), broken.DB(), graphWorkflowID); !errors.Is(err, consolidate.ErrIncompleteGraph) {
		t.Fatalf("dangling dependency error = %v", err)
	}
}

func TestGraphCanonicalHashNormalizesSurrogateIDs(t *testing.T) {
	first := graphStore(t)
	seedGraph(t, first, 7, 11, "same")
	second := graphStore(t)
	seedGraph(t, second, 70, 110, "same")
	a, err := consolidate.LoadGraph(context.Background(), first.DB(), graphWorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := consolidate.LoadGraph(context.Background(), second.DB(), graphWorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash == "" || a.Hash != b.Hash {
		t.Fatalf("equal graph hashes = %q, %q", a.Hash, b.Hash)
	}
	third := graphStore(t)
	seedGraph(t, third, 700, 1100, "different")
	c, err := consolidate.LoadGraph(context.Background(), third.DB(), graphWorkflowID)
	if err != nil || c.Hash == a.Hash {
		t.Fatalf("divergent hash = %q, %v", c.Hash, err)
	}
}

func TestGraphRemapsSurrogatesWithoutRewritingOrMerging(t *testing.T) {
	s := graphStore(t)
	seedGraph(t, s, 7, 11, "same")
	graph, err := consolidate.LoadGraph(context.Background(), s.DB(), graphWorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	remapped, err := graph.RemapSurrogates(100, 200)
	if err != nil {
		t.Fatal(err)
	}
	artifact := remapped.Rows("artifacts")[0]
	activity := remapped.Rows("activities")[0]
	if remapped.WorkflowID != graphWorkflowID || len(remapped.Rows("artifacts")) != 1 || len(remapped.Rows("activities")) != 2 || artifact[0] != int64(100) || activity[0] != int64(200) || activity[7] != "100" {
		t.Fatalf("remapped graph = %#v", remapped)
	}
}

func graphStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedGraph(t *testing.T, s *store.Store, artifactID, activityID int64, content string) {
	t.Helper()
	statements := []string{
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at,name) VALUES('` + graphWorkflowID + `',1,'draft','goal','now','now','name')`,
		`INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES('` + graphWorkflowID + `','','draft','actor','',1,'now')`,
		`INSERT INTO artifacts(id,workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(` + strconv.FormatInt(artifactID, 10) + `,'` + graphWorkflowID + `','exploration','` + content + `','actor',1,'now')`,
		`INSERT INTO activities(id,workflow_id,action,actor,at,subject_kind,subject_id) VALUES(1,'` + graphWorkflowID + `','workflow_created','actor','now','workflow','` + graphWorkflowID + `')`,
		`INSERT INTO activities(id,workflow_id,action,actor,at,subject_kind,subject_id) VALUES(` + strconv.FormatInt(activityID, 10) + `,'` + graphWorkflowID + `','exploration_recorded','actor','now','artifact','` + strconv.FormatInt(artifactID, 10) + `')`,
	}
	for _, statement := range statements {
		if _, err := s.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
