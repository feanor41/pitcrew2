package history

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestProjectPhaseUnitAndAggregateSelectOnlyCurrentFacts(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db := s.DB()
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-new',6,'implementing','Views','goal','2025-01-01T00:00:00Z','2025-01-07T00:00:00Z')`,
		`INSERT INTO plans VALUES('wf-new','plan','scope',1,'{}')`,
		`INSERT INTO work_units VALUES('wu-new','wf-new','unit','internal/history','[]','[]',12,5,'reviewing',NULL,0,3)`,
		`INSERT INTO evidence VALUES('wf-new','wu-new',3,'implementer','red','exit 1','green','exit 0','clean','go test','exit 0','internal/history','2025-01-06T00:00:00Z')`,
		`INSERT INTO reviews VALUES('wf-new','wu-new',3,'reviewer','approved','good','','inside','2025-01-07T00:00:00Z')`,
	} {
		if _, err = db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	var artifactID int64
	result, err := db.ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','specification','structured','specifier',6,'2025-01-07T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	artifactID, _ = result.LastInsertId()
	if _, err = db.ExecContext(ctx, `INSERT INTO normative_entries(workflow_id,artifact_id,phase,entry_kind,stable_id,parent_id,operation,body_json) VALUES('wf-new',?,'specification','requirement','REQ-VIEW-001',NULL,'add','{"text":"bounded"}')`, artifactID); err != nil {
		t.Fatal(err)
	}

	service := New(s)
	phase, err := service.Project(ctx, "wf-new", ViewPhase, "")
	if err != nil || phase.Phase == nil || len(phase.Phase.Normative.Entries) != 1 {
		t.Fatalf("phase projection = %#v, %v", phase, err)
	}
	if len(phase.Phase.Normative.Artifacts) != 0 {
		t.Fatalf("structured phase duplicated artifact content: %#v", phase.Phase.Normative.Artifacts)
	}
	assertOnlySelectedProjection(t, phase, ViewPhase)
	unit, err := service.Project(ctx, "wf-new", ViewUnit, "wu-new")
	if err != nil || unit.Unit == nil || unit.Unit.Definition.ID != "wu-new" || unit.Unit.Evidence == nil || unit.Unit.Evidence.Revision != 3 || unit.Unit.Review == nil || unit.Unit.Review.Revision != 3 {
		t.Fatalf("unit projection = %#v, %v", unit, err)
	}
	assertOnlySelectedProjection(t, unit, ViewUnit)
	aggregate, err := service.Project(ctx, "wf-new", ViewAggregate, "")
	if err != nil || aggregate.Aggregate == nil || len(aggregate.Aggregate.Normative.Entries) != 1 || aggregate.Aggregate.Plan == nil || len(aggregate.Aggregate.Units) != 1 {
		t.Fatalf("aggregate projection = %#v, %v", aggregate, err)
	}
	if len(aggregate.Aggregate.Normative.Artifacts) != 0 {
		t.Fatalf("structured aggregate duplicated artifact content: %#v", aggregate.Aggregate.Normative.Artifacts)
	}
	assertOnlySelectedProjection(t, aggregate, ViewAggregate)
	if _, err = service.Project(ctx, "wf-new", ViewUnit, ""); err == nil {
		t.Fatal("unit view accepted empty unit id")
	}
	if _, err = service.Project(ctx, "wf-new", ViewCoordination, "wu-new"); err == nil {
		t.Fatal("coordination view accepted unit id")
	}
}

func TestProjectLegacyPhaseAndAggregatePreserveAcceptedStageArtifactsOnceWithProvenance(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-current',8,'ready_to_complete','Current','goal','now','now')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-current','exploration','exact exploration','pc2-explorer',2,'2026-08-29T20:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-current','specification','exact specification','pc2-specifier',3,'2026-08-29T20:01:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-current','design','exact design','pc2-designer',4,'2026-08-29T20:02:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-current','aggregate_review','review authority is not normative','pc2-reviewer',8,'2026-08-29T20:03:00Z')`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	service := New(s)
	var artifactQueries []struct {
		query string
		args  []any
	}
	service.queryTrace = func(query string, args []any) {
		if strings.Contains(query, "FROM artifacts a") {
			artifactQueries = append(artifactQueries, struct {
				query string
				args  []any
			}{query, append([]any(nil), args...)})
		}
	}
	for _, view := range []View{ViewPhase, ViewAggregate} {
		projection, projectErr := service.Project(ctx, "wf-current", view, "")
		if projectErr != nil {
			t.Fatal(projectErr)
		}
		var normative NormativeProjection
		if view == ViewPhase {
			normative = projection.Phase.Normative
		} else {
			normative = projection.Aggregate.Normative
		}
		if normative.Structured || len(normative.Entries) != 0 || len(normative.Artifacts) != 3 {
			t.Fatalf("%s legacy normative projection = %#v", view, normative)
		}
		for i, want := range []struct {
			kind, content, actor string
			revision             int64
		}{
			{"exploration", "exact exploration", "pc2-explorer", 2},
			{"specification", "exact specification", "pc2-specifier", 3},
			{"design", "exact design", "pc2-designer", 4},
		} {
			artifact := normative.Artifacts[i]
			if artifact.Kind != want.kind || artifact.Content != want.content || artifact.Source.WorkflowID != "wf-current" || artifact.Source.ArtifactID == 0 || artifact.Source.Revision != want.revision || artifact.Source.Actor != want.actor || artifact.Source.RecordedAt == "" {
				t.Errorf("%s artifact[%d] = %#v", view, i, artifact)
			}
		}
		raw, _ := json.Marshal(projection)
		for _, content := range []string{"exact exploration", "exact specification", "exact design"} {
			if bytes.Count(raw, []byte(content)) != 1 {
				t.Errorf("%s content %q count = %d in %s", view, content, bytes.Count(raw, []byte(content)), raw)
			}
		}
		if bytes.Contains(raw, []byte("review authority is not normative")) {
			t.Errorf("%s leaked aggregate-review authority into normative context: %s", view, raw)
		}
	}
	if len(artifactQueries) != 2 {
		t.Fatalf("legacy bounded views artifact queries = %#v", artifactQueries)
	}
	for _, observed := range artifactQueries {
		if len(observed.args) != 1 || observed.args[0] != "wf-current" || !strings.Contains(observed.query, "WHERE a.workflow_id=?") {
			t.Errorf("unbounded legacy artifact query = %#v", observed)
		}
	}
}
