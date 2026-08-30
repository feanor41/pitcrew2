package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestContinuationPinsManifestAndResolvesTypedSupersession(t *testing.T) {
	svc, db := testService(t)
	ctx := context.Background()
	source, err := svc.Create(ctx, "Original", "goal", "specifier")
	if err != nil {
		t.Fatal(err)
	}
	source, err = svc.RecordNormativeArtifact(ctx, source.ID, source.Revision, Explore, "exploration", NormativeArtifact{
		Content: "baseline exploration", SchemaVersion: 1,
		Entries: []NormativeEntry{{Kind: "section", ID: "SEC-CONTEXT", Operation: "add", Body: json.RawMessage(`{"text":"old context"}`)}},
	}, "explorer")
	if err != nil {
		t.Fatal(err)
	}
	source, err = svc.RecordNormativeArtifact(ctx, source.ID, source.Revision, Specify, "specification", NormativeArtifact{
		Content: "baseline specification", SchemaVersion: 1,
		Entries: []NormativeEntry{
			{Kind: "requirement", ID: "REQ-CONT-001", Operation: "add", Body: json.RawMessage(`{"text":"old"}`)},
			{Kind: "scenario", ID: "SCN-CONT-001", ParentID: "REQ-CONT-001", Operation: "add", Body: json.RawMessage(`{"text":"kept"}`)},
		},
	}, "specifier")
	if err != nil {
		t.Fatal(err)
	}
	source, err = svc.RecordNormativeArtifact(ctx, source.ID, source.Revision, Design, "design", NormativeArtifact{
		Content: "baseline design", SchemaVersion: 1,
		Entries: []NormativeEntry{{Kind: "section", ID: "SEC-DESIGN", Operation: "add", Body: json.RawMessage(`{"text":"design"}`)}},
	}, "designer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE workflows SET state='completed' WHERE id=?`, source.ID); err != nil {
		t.Fatal(err)
	}

	continued, err := svc.Continue(ctx, source.ID, "aion")
	if err != nil {
		t.Fatal(err)
	}
	var predecessorID, manifestJSON string
	var predecessorRevision int64
	if err = db.QueryRow(`SELECT predecessor_id,predecessor_revision,artifact_manifest_json FROM workflow_baselines WHERE child_id=?`, continued.Workflow.ID).Scan(&predecessorID, &predecessorRevision, &manifestJSON); err != nil {
		t.Fatal(err)
	}
	var manifest []ArtifactIdentity
	if err = json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		t.Fatal(err)
	}
	if predecessorID != source.ID || predecessorRevision != source.Revision || len(manifest) != 3 {
		t.Fatalf("baseline=%s/%d manifest=%s", predecessorID, predecessorRevision, manifestJSON)
	}
	for i, wantKind := range []string{"exploration", "specification", "design"} {
		if manifest[i].ID < 1 || manifest[i].Kind != wantKind || manifest[i].Revision != int64(i+2) {
			t.Fatalf("manifest[%d]=%#v", i, manifest[i])
		}
	}

	child := continued.Workflow
	child, err = svc.RecordNormativeArtifact(ctx, child.ID, child.Revision, Explore, "exploration", NormativeArtifact{Content: "no exploration changes", SchemaVersion: 1}, "explorer")
	if err != nil {
		t.Fatal(err)
	}
	child, err = svc.RecordNormativeArtifact(ctx, child.ID, child.Revision, Specify, "specification", NormativeArtifact{
		Content: "replace requirement", SchemaVersion: 1,
		Entries: []NormativeEntry{{Kind: "requirement", ID: "REQ-CONT-001", Operation: "replace", Body: json.RawMessage(`{"text":"new"}`)}},
	}, "specifier")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.ResolveNormative(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Structured || resolved.Baseline == nil || resolved.Baseline.WorkflowID != source.ID || resolved.Baseline.Revision != source.Revision || len(resolved.Baseline.Artifacts) != 3 || len(resolved.Entries) != 4 {
		t.Fatalf("resolved=%#v", resolved)
	}
	byID := map[string]ResolvedNormativeEntry{}
	for _, entry := range resolved.Entries {
		byID[entry.ID] = entry
	}
	requirement := byID["REQ-CONT-001"]
	if string(requirement.Body) != `{"text":"new"}` || requirement.Source.WorkflowID != child.ID || requirement.Source.ArtifactID < 1 || requirement.Source.Revision != child.Revision || requirement.Operation != "replace" {
		t.Fatalf("requirement=%#v", requirement)
	}
	scenario := byID["SCN-CONT-001"]
	if scenario.ParentID != "REQ-CONT-001" || scenario.Source.WorkflowID != source.ID || scenario.Source.ArtifactID != manifest[1].ID || scenario.Source.Revision != 3 {
		t.Fatalf("scenario=%#v", scenario)
	}
}

func TestNormativeResolverRejectsUnsafeLineageAndInvalidOperations(t *testing.T) {
	t.Run("cycle", func(t *testing.T) {
		svc, db := testService(t)
		a := insertTerminalWorkflow(t, svc, db, "a")
		b := insertTerminalWorkflow(t, svc, db, "b")
		insertBaseline(t, db, a.ID, b.ID, b.Revision, `[]`)
		insertBaseline(t, db, b.ID, a.ID, a.Revision, `[]`)
		if _, err := svc.ResolveNormative(context.Background(), a.ID); !errors.Is(err, ErrLineageCycle) {
			t.Fatalf("cycle error=%v", err)
		}
	})
	t.Run("depth", func(t *testing.T) {
		svc, db := testService(t)
		chain := make([]Workflow, 34)
		for i := range chain {
			chain[i] = insertTerminalWorkflow(t, svc, db, fmt.Sprintf("w-%d", i))
		}
		for i := 1; i < len(chain); i++ {
			insertBaseline(t, db, chain[i].ID, chain[i-1].ID, chain[i-1].Revision, `[]`)
		}
		if _, err := svc.ResolveNormative(context.Background(), chain[len(chain)-1].ID); !errors.Is(err, ErrLineageDepth) {
			t.Fatalf("depth error=%v", err)
		}
	})
	for _, tt := range []struct {
		name   string
		mutate func(t *testing.T, db DB, source Workflow, child Workflow)
	}{
		{"mutable baseline", func(t *testing.T, db DB, source Workflow, _ Workflow) {
			_, _ = db.Exec(`UPDATE workflows SET state='draft' WHERE id=?`, source.ID)
		}},
		{"revision mismatch", func(t *testing.T, db DB, source Workflow, _ Workflow) {
			_, _ = db.Exec(`UPDATE workflows SET revision=revision+1 WHERE id=?`, source.ID)
		}},
		{"manifest mismatch", func(t *testing.T, db DB, source Workflow, _ Workflow) {
			_, _ = db.Exec(`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'specification','late','actor',1,'now')`, source.ID)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, db := testService(t)
			source := insertTerminalWorkflow(t, svc, db, "source")
			child := insertTerminalWorkflow(t, svc, db, "child")
			insertBaseline(t, db, child.ID, source.ID, source.Revision, `[]`)
			tt.mutate(t, db, source, child)
			if _, err := svc.ResolveNormative(context.Background(), child.ID); !errors.Is(err, ErrInvalidBaseline) {
				t.Fatalf("baseline error=%v", err)
			}
		})
	}
	for _, operation := range []string{"replace", "remove"} {
		t.Run("unknown "+operation, func(t *testing.T) {
			svc, db := testService(t)
			wf := insertTerminalWorkflow(t, svc, db, "unknown")
			insertNormativeRow(t, db, wf.ID, "requirement", "REQ-MISSING", "", operation)
			if _, err := svc.ResolveNormative(context.Background(), wf.ID); !errors.Is(err, ErrInvalidNormativeArtifact) || !strings.Contains(err.Error(), "unknown") {
				t.Fatalf("operation error=%v", err)
			}
		})
	}
	t.Run("duplicate stable id", func(t *testing.T) {
		svc, db := testService(t)
		wf := insertTerminalWorkflow(t, svc, db, "duplicate")
		insertNormativeRow(t, db, wf.ID, "requirement", "REQ-DUP", "", "add")
		insertNormativeRow(t, db, wf.ID, "section", "REQ-DUP", "", "add")
		if _, err := svc.ResolveNormative(context.Background(), wf.ID); !errors.Is(err, ErrInvalidNormativeArtifact) {
			t.Fatalf("duplicate error=%v", err)
		}
	})
}

func TestLegacyAndUnrelatedWorkflowsResolveStandaloneWithoutInventedStructure(t *testing.T) {
	svc, db := testService(t)
	legacy := insertTerminalWorkflow(t, svc, db, "legacy")
	if _, err := db.Exec(`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'specification','# prose only','actor',1,'now')`, legacy.ID); err != nil {
		t.Fatal(err)
	}
	resolved, err := svc.ResolveNormative(context.Background(), legacy.ID)
	if err != nil || resolved.Structured || resolved.Baseline != nil || len(resolved.Entries) != 0 {
		t.Fatalf("legacy=%#v error=%v", resolved, err)
	}
	fresh := insertTerminalWorkflow(t, svc, db, "fresh")
	insertNormativeRow(t, db, fresh.ID, "requirement", "REQ-OWN", "", "add")
	resolved, err = svc.ResolveNormative(context.Background(), fresh.ID)
	if err != nil || !resolved.Structured || resolved.Baseline != nil || len(resolved.Entries) != 1 || resolved.Entries[0].Source.WorkflowID != fresh.ID {
		t.Fatalf("fresh=%#v error=%v", resolved, err)
	}
}

func TestTypedDeltaValidationRollsBackAndContinuationCannotExtendPastDepth32(t *testing.T) {
	t.Run("unknown replacement and duplicate input", func(t *testing.T) {
		svc, db := testService(t)
		wf, err := svc.Create(context.Background(), "delta", "goal", "actor")
		if err != nil {
			t.Fatal(err)
		}
		before := continuationSourceSnapshot(t, db, wf.ID)
		_, err = svc.RecordNormativeArtifact(context.Background(), wf.ID, wf.Revision, Explore, "exploration", NormativeArtifact{
			Content: "bad delta", SchemaVersion: 1,
			Entries: []NormativeEntry{{Kind: "requirement", ID: "REQ-MISSING", Operation: "replace", Body: json.RawMessage(`{}`)}},
		}, "actor")
		if !errors.Is(err, ErrInvalidNormativeArtifact) || continuationSourceSnapshot(t, db, wf.ID) != before {
			t.Fatalf("unknown replacement error=%v", err)
		}
		_, err = svc.RecordNormativeArtifact(context.Background(), wf.ID, wf.Revision, Explore, "exploration", NormativeArtifact{
			Content: "duplicate delta", SchemaVersion: 1,
			Entries: []NormativeEntry{
				{Kind: "requirement", ID: "REQ-DUP", Operation: "add", Body: json.RawMessage(`{}`)},
				{Kind: "section", ID: "REQ-DUP", Operation: "add", Body: json.RawMessage(`{}`)},
			},
		}, "actor")
		if !errors.Is(err, ErrInvalidNormativeArtifact) || continuationSourceSnapshot(t, db, wf.ID) != before {
			t.Fatalf("duplicate error=%v", err)
		}
	})
	t.Run("continuation extension", func(t *testing.T) {
		svc, db := testService(t)
		chain := make([]Workflow, 33)
		for i := range chain {
			chain[i] = insertTerminalWorkflow(t, svc, db, fmt.Sprintf("bounded-%d", i))
		}
		for i := 1; i < len(chain); i++ {
			insertBaseline(t, db, chain[i].ID, chain[i-1].ID, chain[i-1].Revision, `[]`)
		}
		var before int
		_ = db.QueryRow(`SELECT count(*) FROM workflows`).Scan(&before)
		if _, err := svc.Continue(context.Background(), chain[len(chain)-1].ID, "actor"); !errors.Is(err, ErrLineageDepth) {
			t.Fatalf("extension error=%v", err)
		}
		var after int
		_ = db.QueryRow(`SELECT count(*) FROM workflows`).Scan(&after)
		if after != before {
			t.Fatalf("failed extension created workflow: %d -> %d", before, after)
		}
	})
}

func insertTerminalWorkflow(t *testing.T, svc *Service, db DB, name string) Workflow {
	t.Helper()
	wf, err := svc.Create(context.Background(), name, "goal", "actor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE workflows SET state='completed' WHERE id=?`, wf.ID); err != nil {
		t.Fatal(err)
	}
	wf.State = Completed
	return wf
}

func insertBaseline(t *testing.T, db DB, childID, predecessorID string, revision int64, manifest string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO workflow_baselines(child_id,predecessor_id,predecessor_revision,artifact_manifest_json) VALUES(?,?,?,?)`, childID, predecessorID, revision, manifest); err != nil {
		t.Fatal(err)
	}
}

func insertNormativeRow(t *testing.T, db DB, workflowID, kind, id, parentID, operation string) {
	t.Helper()
	result, err := db.Exec(`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'specification','structured','actor',1,'now')`, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	artifactID, _ := result.LastInsertId()
	if _, err = db.Exec(`INSERT INTO normative_entries(workflow_id,artifact_id,phase,entry_kind,stable_id,parent_id,operation,body_json) VALUES(?,?,?,?,?,?,?,?)`, workflowID, artifactID, "specification", kind, id, nullableString(parentID), operation, `{}`); err != nil {
		t.Fatal(err)
	}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

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
