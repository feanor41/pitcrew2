package history

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/plan"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestServiceProjectsNamedGridAndExactActivityTimeline(t *testing.T) {
	ctx := context.Background()
	service := New(openHistory(t, true))
	workflows, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join([]string{workflows[0].ID, workflows[1].ID, workflows[2].ID}, ","); got != "wf-active,wf-new,wf-old" {
		t.Fatalf("created order = %s", got)
	}
	if workflows[0].Name != "Active delivery" || workflows[0].NameDerived {
		t.Fatalf("persisted name = %q derived=%v", workflows[0].Name, workflows[0].NameDerived)
	}
	if workflows[1].Name != "new goal" || !workflows[1].NameDerived {
		t.Fatalf("fallback name = %q derived=%v", workflows[1].Name, workflows[1].NameDerived)
	}

	detail, err := service.Detail(ctx, "wf-new")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Workflow.Name != "new goal" || !detail.Workflow.NameDerived {
		t.Fatalf("detail name = %q derived=%v", detail.Workflow.Name, detail.Workflow.NameDerived)
	}
	created := occurrence(detail.Occurrences, "workflow_created")
	if resolved, err := service.ResolveOccurrence(ctx, "wf-new", created.ID, created.RecordID); err != nil || resolved.Record.Kind != "workflow" || resolved.Record.ID != "workflow:wf-new" {
		t.Fatalf("ResolveOccurrence(workflow_created) = %#v, %v", resolved, err)
	}
	for i := 1; i < len(detail.Timeline); i++ {
		if detail.Timeline[i-1].At > detail.Timeline[i].At {
			t.Fatalf("timeline not chronological: %#v", detail.Timeline)
		}
	}
	if sameTime := timelineEntry(detail.Timeline, "exploration"); sameTime.RecordID != "artifact:2" || !sameTime.Legacy {
		t.Fatalf("same-time legacy record was suppressed: %#v", sameTime)
	}
	for _, want := range []struct {
		action, actor, kind, subject string
	}{
		{"workflow_created", "daimon", "workflow", "wf-new"},
		{"plan_submitted", "planner", "plan", "wf-new"},
		{"unit_review_recorded", "reviewer", "review", "wu-new@3"},
	} {
		entry := timelineEntry(detail.Timeline, want.action)
		if entry.Actor != want.actor || entry.SubjectKind != want.kind || entry.SubjectID != want.subject || entry.Legacy {
			t.Errorf("timeline %s = %#v", want.action, entry)
		}
		resolved, err := service.ResolveActivity(ctx, entry)
		if err != nil || resolved.Record.Kind != want.kind {
			t.Errorf("ResolveActivity(%s) = %#v, %v", want.action, resolved, err)
		}
	}
	workflowResult, _ := service.ResolveActivity(ctx, timelineEntry(detail.Timeline, "workflow_created"))
	planResult, _ := service.ResolveActivity(ctx, timelineEntry(detail.Timeline, "plan_submitted"))
	if workflowResult.Record.ID == planResult.Record.ID {
		t.Fatalf("subject-kind collision resolved to same record: %q", workflowResult.Record.ID)
	}
	seenKinds := map[string]bool{}
	for _, entry := range detail.Timeline {
		resolved, err := service.ResolveActivity(ctx, entry)
		if err != nil {
			t.Fatalf("ResolveActivity(%#v): %v", entry, err)
		}
		seenKinds[resolved.Record.Kind] = true
	}
	for _, kind := range []string{"workflow", "event", "exploration", "plan", "work_unit", "evidence", "review"} {
		if !seenKinds[kind] {
			t.Errorf("unresolved durable kind %q", kind)
		}
	}
}

func TestServiceClassifiesCorrectionsDependenciesAndClaimExpiry(t *testing.T) {
	now := time.Date(2026, 8, 22, 17, 0, 0, 0, time.UTC)
	opened := openHistory(t, true)
	detail, err := New(opened, func() time.Time { return now }).Detail(context.Background(), "wf-active")
	if err != nil {
		t.Fatal(err)
	}
	s := detail.Synopsis
	if s.Total != 7 || s.Done != 1 || s.Correction != 1 || s.Claimed != 1 || s.Reviewing != 1 || s.DependencyWaiting != 1 || s.Recovery != 1 || s.Ready != 0 || s.NextAction != "workflow list-ready-units" {
		t.Fatalf("classified synopsis = %#v", s)
	}
	if s.Current == nil || s.Current.ID != "wu-correction" || s.Blocker == nil || s.Blocker.Reason != "fix typed projection" {
		t.Fatalf("correction precedence = %#v", s)
	}
	claim, review := occurrence(detail.Occurrences, "unit_claimed"), occurrence(detail.Occurrences, "unit_review_recorded")
	if claim.Attempt != nil || review.Attempt == nil || *review.Attempt != 1 {
		t.Fatalf("attempt truth: claim=%#v review=%#v", claim, review)
	}
	ready, err := plan.NewService(opened, func() time.Time { return now }).Ready(context.Background(), "wf-active")
	if err != nil || len(ready) != s.Ready {
		t.Fatalf("readiness parity: executable=%#v synopsis=%#v err=%v", ready, s, err)
	}
}

func TestServiceProjectsBoundedCorrectionAuthorityAndContextualActivities(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	unitID := "wu-000000000000000000000088"
	readyID := "wu-000000000000000000000089"
	planBody := `{"summary":"bounded","scope":"internal","work_units":[{"id":"` + unitID + `","description":"unit","scope":"internal/x","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"` + readyID + `","description":"ready unit","scope":"internal/y","areas":[],"depends_on":[],"estimated_changed_lines":2,"estimated_review_minutes":2}],"max_parallel_units":1,"aggregate_correction_policy":{"automatic_rounds":1,"on_exhaustion":"require_user_authorization"}}`
	for _, statement := range []struct {
		query string
		args  []any
	}{{`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-active',8,'ready_to_complete','Bounded','goal','now','now')`, nil}, {`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES('wf-active','bounded','internal',1,?)`, []any{planBody}}, {`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,'wf-active','unit','internal/x','[]','[]',1,1,'done',1)`, []any{unitID}}, {`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,'wf-active','ready unit','internal/y','[]','[]',2,2,'pending',1)`, []any{readyID}}} {
		if _, err = s.DB().Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	facts := []struct {
		kind, body, action string
		revision           int
	}{
		{"aggregate_review", `{"verdict":"corrections","findings":"first blocker"}`, "aggregate_review_recorded", 5},
		{"aggregate_correction", `{"aggregate_review_revision":5,"groups":[],"assignments":[],"authority":"automatic"}`, "aggregate_correction_started", 6},
		{"aggregate_review", `{"verdict":"corrections","findings":"latest blocker"}`, "aggregate_review_recorded", 7},
		{"correction_authorization", `{"aggregate_review_revision":7,"reason":"user approved","user_direction_confirmed":true}`, "correction_authorized", 8},
	}
	for _, fact := range facts {
		result, err := s.DB().Exec(`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-active',?,?, 'actor',?,'2026-08-29T20:00:00Z')`, fact.kind, fact.body, fact.revision)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		if _, err = s.DB().Exec(`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-active',NULL,?,'actor','2026-08-29T20:00:00Z','artifact',?)`, fact.action, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.DB().Exec(`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-active','progress','{"status":"blocked","summary":"awaiting correction","next_action":"workflow recover-aggregate"}','aion',8,'2026-08-29T20:00:01Z')`); err != nil {
		t.Fatal(err)
	}
	service := New(s)
	detail, err := service.Detail(ctx, "wf-active")
	if err != nil {
		t.Fatal(err)
	}
	c := detail.Synopsis.CorrectionPolicy
	if c == nil || !c.PolicyAware || c.Used != 1 || c.Allowed != 1 || c.BlockerRevision != 7 || c.Authority != "authorized" || detail.Synopsis.NextAction != "workflow recover-aggregate" {
		t.Fatalf("correction synopsis = %#v next=%q", c, detail.Synopsis.NextAction)
	}
	if detail.Synopsis.Blocker == nil || detail.Synopsis.Blocker.Reason != "latest blocker" {
		t.Fatalf("latest blocker = %#v", detail.Synopsis.Blocker)
	}
	bounded, err := service.Project(ctx, "wf-active", ViewCoordination, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]any{
		"current":              {detail.Synopsis.Current, bounded.Coordination.Current},
		"blocker":              {detail.Synopsis.Blocker, bounded.Coordination.Blocker},
		"correction authority": {detail.Synopsis.CorrectionPolicy, bounded.Coordination.CorrectionAuthority},
		"progress":             {detail.Synopsis.Progress, bounded.Coordination.LatestProgress},
	} {
		left, _ := json.Marshal(pair[0])
		right, _ := json.Marshal(pair[1])
		if !bytes.Equal(left, right) {
			t.Errorf("coordination %s parity: audit=%s bounded=%s", name, left, right)
		}
	}
	if detail.Synopsis.Ready != len(bounded.Coordination.Ready) {
		t.Errorf("coordination ready parity: audit=%d bounded=%d", detail.Synopsis.Ready, len(bounded.Coordination.Ready))
	}
	var auditReady *UnitStatus
	if detail.Synopsis.Planned != nil {
		for i := range detail.Synopsis.Planned.Units {
			if detail.Synopsis.Planned.Units[i].Status == "Ready" {
				auditReady = &detail.Synopsis.Planned.Units[i]
			}
		}
	}
	if auditReady == nil || len(bounded.Coordination.Ready) != 1 {
		t.Fatalf("missing authoritative ready unit: audit=%#v bounded=%#v", auditReady, bounded.Coordination.Ready)
	} else {
		left, _ := json.Marshal(auditReady)
		right, _ := json.Marshal(bounded.Coordination.Ready[0])
		if !bytes.Equal(left, right) {
			t.Errorf("coordination ready content parity: audit=%s bounded=%s", left, right)
		}
	}
	delivery, err := New(s).GetDelivery(ctx, "wf-active")
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Delivery.Status != "blocked" || delivery.Delivery.Summary != "latest blocker" || delivery.Delivery.NextAction != "workflow recover-aggregate" {
		t.Fatalf("delivery correction truth = %#v", delivery.Delivery)
	}
	blocked, err := New(s).SearchDeliveries(ctx, "blocked")
	if err != nil || len(blocked) != 1 || blocked[0].DeliveryID != "wf-active" {
		t.Fatalf("blocked delivery search = %#v, %v", blocked, err)
	}
	for _, action := range []string{"aggregate_correction_started", "correction_authorized"} {
		if timelineEntry(detail.Timeline, action).Action != action {
			t.Fatalf("missing %s", action)
		}
	}
	for _, record := range detail.Records {
		if strings.Contains(record.Content, "handle_path") || strings.Contains(record.Content, "secret_hash") {
			t.Fatalf("secret leaked: %#v", record)
		}
	}
}

func TestServiceProjectsLatestValidProgressByInsertionOrder(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	s, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-progress',3,'implementing','Progress','goal','created','updated')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-progress','progress','not-json','daimon',3,'one')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-progress','progress','{"status":"advanced","summary":"first","next_action":"test"}','daimon',3,'two')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-progress','exploration','ignore me','explorer',3,'three')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-progress','progress','{"status":"blocked","summary":"waiting","next_action":"unblock"}','daimon',3,'four')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-progress','progress','{"status":"blocked","summary":"invalid","next_action":"none","extra":true}','daimon',3,'five')`,
		`INSERT INTO activities(workflow_id,action,actor,at,subject_kind,subject_id) VALUES('wf-progress','progress_recorded','daimon','four','artifact','4')`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	before := operationalSnapshot(t, s.DB(), "wf-progress")
	detail, err := New(s).Detail(ctx, "wf-progress")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Synopsis.Progress == nil || detail.Synopsis.Progress.Status != "blocked" || detail.Synopsis.Progress.Summary != "waiting" || detail.Synopsis.Progress.NextAction != "unblock" {
		t.Fatalf("progress=%#v", detail.Synopsis.Progress)
	}
	entry := timelineEntry(detail.Timeline, "progress_recorded")
	resolved, err := New(s).ResolveActivity(ctx, entry)
	if err != nil || resolved.Record.Kind != "progress" || resolved.Record.Content == "" {
		t.Fatalf("progress drill-down=%#v err=%v", resolved, err)
	}
	if after := operationalSnapshot(t, s.DB(), "wf-progress"); after != before {
		t.Fatalf("history projection mutated database: %q -> %q", before, after)
	}
}

func TestServicePlannedWorkUsesAcceptedOrderAndStableProgress(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	s, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	planBody := `{"summary":"three ordered units","scope":"internal/history","work_units":[{"id":"wu-000000000000000000000001","description":"finished projection","scope":"internal/history/one.go","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"wu-000000000000000000000002","description":"active projection","scope":"internal/history/two.go","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"wu-000000000000000000000003","description":"correct projection","scope":"internal/history/three.go","areas":[],"depends_on":["wu-000000000000000000000002"],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1}`
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-planned',7,'implementing','Planned','goal','created','updated')`,
		`INSERT INTO plans VALUES('wf-planned','three ordered units','internal/history',1,'` + planBody + `')`,
		`INSERT INTO work_units VALUES('wu-000000000000000000000001','wf-planned','finished projection','internal/history/one.go','[]','[]',1,1,'done',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-000000000000000000000002','wf-planned','active projection','internal/history/two.go','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-000000000000000000000003','wf-planned','correct projection','internal/history/three.go','[]','["wu-000000000000000000000002"]',1,1,'pending',NULL,0,2)`,
		`INSERT INTO work_units VALUES('wu-unplanned','wf-planned','legacy extra','legacy','[]','[]',1,1,'done',NULL,0,1)`,
		`INSERT INTO reviews VALUES('wf-planned','wu-000000000000000000000003',1,'reviewer','corrections','summary','fix percentage rounding','','reviewed')`,
		`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation) VALUES('claim-planned','wf-planned','wu-000000000000000000000002','active','secret-hash','implementer','2026-08-28T12:00:00Z','2030-01-01T00:00:00Z',1)`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := New(s, func() time.Time { return time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC) }).Detail(ctx, "wf-planned")
	if err != nil {
		t.Fatal(err)
	}
	planned := detail.Synopsis.Planned
	if planned == nil || planned.Done != 1 || planned.Total != 3 || planned.Percent != 33 || detail.Synopsis.PlanNotice != "" {
		t.Fatalf("planned progress = %#v notice=%q", planned, detail.Synopsis.PlanNotice)
	}
	if len(planned.Pending) != 2 || planned.Pending[0].ID != "wu-000000000000000000000002" || planned.Pending[0].Status != "Claimed" || planned.Pending[1].ID != "wu-000000000000000000000003" || planned.Pending[1].Status != "Correction" || planned.Pending[1].Reason != "fix percentage rounding" {
		t.Fatalf("ordered pending work = %#v", planned.Pending)
	}
	if len(planned.Units) != 3 || planned.Units[0].Status != "Done" || planned.Units[1].Status != "Claimed" || planned.Units[2].Status != "Correction" {
		t.Fatalf("ordered unit progress = %#v", planned.Units)
	}
	if _, err = s.DB().ExecContext(ctx, `UPDATE work_units SET state='done' WHERE workflow_id='wf-planned' AND id!='wu-unplanned'`); err != nil {
		t.Fatal(err)
	}
	detail, err = New(s).Detail(ctx, "wf-planned")
	if err != nil {
		t.Fatal(err)
	}
	planned = detail.Synopsis.Planned
	if planned == nil || planned.Done != 3 || planned.Total != 3 || planned.Percent != 100 || len(planned.Pending) != 0 {
		t.Fatalf("complete planned progress = %#v", planned)
	}
}

func TestServicePlannedWorkOmitsUnavailablePrecision(t *testing.T) {
	validBody := `{"summary":"one unit","scope":"internal/history","work_units":[{"id":"wu-000000000000000000000001","description":"missing unit","scope":"internal/history/missing.go","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1}`
	for _, tc := range []struct {
		name, planBody, notice string
	}{
		{name: "absent", notice: "No planned work yet"},
		{name: "malformed", planBody: "not-json", notice: "Planned progress unavailable: incomplete plan data"},
		{name: "unreconciled", planBody: validBody, notice: "Planned progress unavailable: incomplete plan data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, root := context.Background(), t.TempDir()
			s, err := store.Open(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err = s.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-notice',3,'designing','Notice','goal','created','updated')`); err != nil {
				t.Fatal(err)
			}
			if tc.planBody != "" {
				if _, err = s.DB().ExecContext(ctx, `INSERT INTO plans VALUES('wf-notice','summary','internal/history',1,?)`, tc.planBody); err != nil {
					t.Fatal(err)
				}
			}
			detail, err := New(s).Detail(ctx, "wf-notice")
			if err != nil {
				t.Fatal(err)
			}
			if detail.Synopsis.Planned != nil || detail.Synopsis.PlanNotice != tc.notice {
				t.Fatalf("planned=%#v notice=%q", detail.Synopsis.Planned, detail.Synopsis.PlanNotice)
			}
		})
	}
}

func TestServiceRecordsUseLabeledMarkdownWithoutDroppingFields(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	s, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	planBody := `{"summary":"record plan","scope":"internal/history","work_units":[{"id":"wu-000000000000000000000001","description":"record unit","scope":"internal/history/projection.go","areas":[],"depends_on":[],"estimated_changed_lines":12,"estimated_review_minutes":8}],"max_parallel_units":1}`
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-records',7,'implementing','Records','goal','created','updated')`,
		`INSERT INTO plans VALUES('wf-records','record plan','internal/history',1,'` + planBody + `')`,
		`INSERT INTO work_units VALUES('wu-000000000000000000000001','wf-records','record unit','internal/history/projection.go','["internal/history/service.go"]','["wu-dependency"]',12,8,'reviewing','{"justification":"bounded exception"}',1,3)`,
		`INSERT INTO evidence VALUES('wf-records','wu-000000000000000000000001',3,'implementer','go test red','exit 1: expected failure','go test green','exit 0','kept helpers small','go test ./internal/history','exit 0','internal/history','evidence-time')`,
		`INSERT INTO reviews VALUES('wf-records','wu-000000000000000000000001',3,'reviewer','corrections','review summary','repair labels','no plan change','review-time')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-records','progress','{"status":"blocked","summary":"waiting for review","next_action":"workflow unit-review"}','daimon',7,'progress-time')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-records','aggregate_review','{"verdict":"corrections","summary":"aggregate review summary","findings":"repair aggregate labels"}','aggregate-reviewer',7,'aggregate-time')`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := New(s).Detail(ctx, "wf-records")
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{}
	for _, record := range detail.Records {
		contents[record.Kind] = record.Content
	}
	for kind, fragments := range map[string][]string{
		"plan":             {"# Accepted plan", "**Summary:** record plan", "**Scope:** internal/history", "**Max parallel units:** 1", "```json", `"work_units"`},
		"work_unit":        {"# Work unit", "**Description:** record unit", "**Areas:**", "internal/history/service.go", "**Dependencies:**", "wu-dependency", "**Estimated changed lines:** 12", "**Estimated review minutes:** 8", "**State:** reviewing", "bounded exception", "**Exception approved:** yes"},
		"evidence":         {"# TDD evidence", "## Red", "go test red", "expected failure", "## Green", "go test green", "## Refactor", "kept helpers small", "## Validation", "go test ./internal/history", "**Changed paths:** internal/history"},
		"review":           {"# Unit review", "**Verdict:** corrections", "**Summary:** review summary", "**Findings:** repair labels", "**Plan impact:** no plan change"},
		"progress":         {"# Progress report", "**Status:** blocked", "**Summary:** waiting for review", "**Next action:** workflow unit-review"},
		"aggregate_review": {"# Aggregate review", "**Verdict:** corrections", "**Summary:** aggregate review summary", "**Findings:** repair aggregate labels"},
	} {
		for _, fragment := range fragments {
			if !strings.Contains(contents[kind], fragment) {
				t.Errorf("%s record omitted %q:\n%s", kind, fragment, contents[kind])
			}
		}
	}
}

func operationalSnapshot(t *testing.T, db *sql.DB, workflowID string) string {
	t.Helper()
	var snapshot string
	if err := db.QueryRow(`SELECT revision||':'||state||':'||updated_at||':'||(SELECT count(*) FROM events WHERE workflow_id=w.id)||':'||(SELECT count(*) FROM artifacts WHERE workflow_id=w.id)||':'||(SELECT count(*) FROM activities WHERE workflow_id=w.id) FROM workflows w WHERE id=?`, workflowID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestServiceUsesLatestAggregateAuthorityAndResolvesCoalescedOccurrence(t *testing.T) {
	service := New(openHistory(t, true))
	detail, err := service.Detail(context.Background(), "wf-old")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Synopsis.Blocker != nil {
		t.Fatalf("stale aggregate correction: %#v", detail.Synopsis)
	}
	var aggregates []Occurrence
	for _, entry := range detail.Occurrences {
		if entry.Activity == "aggregate_review_recorded" {
			aggregates = append(aggregates, entry)
		}
	}
	if len(aggregates) != 2 {
		t.Fatalf("same-time aggregate occurrences = %#v", aggregates)
	}
	var linked Occurrence
	for _, entry := range aggregates {
		if len(entry.RelatedRecordIDs) > 0 {
			linked = entry
		}
	}
	if linked.ID == "" {
		t.Fatalf("final transition not explicitly coalesced: %#v", aggregates)
	}
	resolved, err := service.ResolveOccurrence(context.Background(), "wf-old", linked.ID, linked.RelatedRecordIDs[0])
	if err != nil || resolved.Record.Kind != "event" {
		t.Fatalf("ResolveOccurrence() = %#v, %v", resolved, err)
	}
}

func occurrence(entries []Occurrence, action string) Occurrence {
	for _, entry := range entries {
		if entry.Activity == action {
			return entry
		}
	}
	return Occurrence{}
}

func TestServiceReadsLegacySchemaHonestlyWithoutMigration(t *testing.T) {
	ctx := context.Background()
	root := legacyHistoryRoot(t)
	databasePath := filepath.Join(root, ".pitcrew", "state.db")
	beforeBytes, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, root)
	opened, err := store.OpenReadOnly(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Store.Close() })
	service := New(opened.Store)
	deliveries, err := service.ListDeliveries(ctx)
	if err != nil || len(deliveries) != 1 || deliveries[0].ID != "wf-legacy" || deliveries[0].Route != FullWorkflow {
		t.Fatalf("legacy ListDeliveries() = %#v, %v", deliveries, err)
	}
	results, err := service.SearchDeliveries(ctx, "legacy evidence")
	if err != nil || len(results) != 1 || results[0].DeliveryID != "wf-legacy" {
		t.Fatalf("legacy SearchDeliveries() = %#v, %v", results, err)
	}
	workflows, err := service.List(ctx)
	if err != nil || len(workflows) != 1 || workflows[0].Name != "Legacy goal" || !workflows[0].NameDerived {
		t.Fatalf("legacy List() = %#v, %v", workflows, err)
	}
	detail, err := service.Detail(ctx, "wf-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Timeline) != 2 {
		t.Fatalf("legacy timeline = %#v", detail.Timeline)
	}
	for _, entry := range detail.Timeline {
		if !entry.Legacy || entry.Actor == "" || entry.At == "" {
			t.Fatalf("dishonest legacy entry = %#v", entry)
		}
	}
	if got := schemaSnapshot(t, root); got != before {
		t.Fatalf("read-only history changed schema\nbefore: %s\nafter:  %s", before, got)
	}
	afterBytes, err := os.ReadFile(databasePath)
	if err != nil || !bytes.Equal(afterBytes, beforeBytes) {
		t.Fatalf("read-only history changed legacy database bytes: %v", err)
	}
}

func TestServiceProjectsDeterministicProjectHistory(t *testing.T) {
	ctx := context.Background()
	empty := openHistory(t, false)
	if got, err := New(empty).List(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty List() = %v, %v", got, err)
	}

	s := openHistory(t, true)
	service := New(s)
	got, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if states := []string{got[0].State, got[1].State, got[2].State}; strings.Join(states, ",") != "implementing,abandoned,completed" {
		t.Fatalf("states = %v", states)
	}
	detail, err := service.Detail(ctx, "wf-new")
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []string{"event", "exploration", "plan", "work_unit", "evidence", "review"}
	for _, want := range wantKinds {
		if !hasRecord(detail.Records, want) {
			t.Errorf("missing %q record: %#v", want, detail.Records)
		}
	}
	joined := recordText(detail.Records)
	for _, want := range []string{"wu-dependency", "**Exception approved:** yes", "red-text", "review-text"} {
		if !strings.Contains(joined, want) {
			t.Errorf("detail missing %q", want)
		}
	}
	if strings.Contains(joined, "raw-handle-secret") {
		t.Fatal("claim internals leaked into detail")
	}

	other := openHistory(t, false)
	if isolated, err := New(other).List(ctx); err != nil || len(isolated) != 0 {
		t.Fatalf("isolated List() = %v, %v", isolated, err)
	}
}

func TestServiceSearchIsLiteralBoundedAndResolvable(t *testing.T) {
	ctx := context.Background()
	service := New(openHistory(t, true))
	tests := []struct{ query, kind string }{
		{"%", "exploration"}, {"_", "exploration"}, {`"quoted"`, "exploration"},
		{"CAFÉ", "exploration"}, {"event-reason", "event"}, {"plan-text", "plan"},
		{"unit-text", "work_unit"}, {"red-text", "evidence"}, {"review-text", "review"},
		{"bounded-token", "exploration"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got, err := service.Search(ctx, tt.query)
			if err != nil || len(got) == 0 || got[0].Kind != tt.kind {
				t.Fatalf("Search(%q) = %#v, %v", tt.query, got, err)
			}
			if got[0].WorkflowID == "" || len([]rune(got[0].Context)) > ContextRunes {
				t.Fatalf("identity/context = %#v", got[0])
			}
			if strings.Contains("work_unit evidence review", tt.kind) && (got[0].UnitID == "" || got[0].Revision != 3) {
				t.Fatalf("unit identity = %#v", got[0])
			}
			if resolved, err := service.Resolve(ctx, got[0]); err != nil || resolved.Detail.Workflow.ID != got[0].WorkflowID || resolved.Record.ID != got[0].RecordID {
				t.Fatalf("Resolve() = %#v, %v", resolved, err)
			}
		})
	}
	for _, query := range []string{"", "   ", "no-match", "raw-handle-secret"} {
		if got, err := service.Search(ctx, query); err != nil || len(got) != 0 {
			t.Fatalf("Search(%q) = %#v, %v", query, got, err)
		}
	}
	got, err := service.Search(ctx, "shared-needle")
	if err != nil || len(got) != 2 || got[0].Kind != "review" || got[1].Kind != "goal" {
		t.Fatalf("ordered Search() = %#v, %v", got, err)
	}
	stable, err := service.Search(ctx, "stable-order")
	if err != nil || len(stable) != 2 || stable[0].Kind != "design" || stable[1].Kind != "specification" {
		t.Fatalf("stable Search() = %#v, %v", stable, err)
	}
	many, err := service.Search(ctx, "many-match")
	if err != nil || len(many) != 205 {
		t.Fatalf("complete Search() count = %d, %v", len(many), err)
	}
	collisions, err := service.Search(ctx, "collision-record")
	if err != nil || len(collisions) != 2 || collisions[0].RecordID == collisions[1].RecordID {
		t.Fatalf("collision identities = %#v, %v", collisions, err)
	}
	for _, result := range collisions {
		resolved, err := service.Resolve(ctx, result)
		if err != nil || result.Context != strings.TrimSpace(resolved.Record.Title+" "+resolved.Record.Content) {
			t.Fatalf("collision Resolve() = %#v, %v", resolved, err)
		}
	}
}

func TestServiceListsDirectDelegatedAndFullDeliveriesExactlyOnce(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	s, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-blocked',2,'implementing','Blocked workflow','workflow goal','2026-08-29T10:00:00Z','2026-08-29T12:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-blocked','progress','{"status":"blocked","summary":"waiting on input","next_action":"ask user"}','aion',2,'2026-08-29T12:00:00Z')`,
		`INSERT INTO direct_delivery_traces VALUES('dl-000000000000000000000001','inline-key','direct_inline','inline goal','small change','completed','shipped inline','none',2,'aion','aion','2026-08-29T11:00:00Z','2026-08-29T11:30:00Z','2026-08-29T11:30:00Z')`,
		`INSERT INTO direct_delivery_traces VALUES('dl-000000000000000000000002','delegated-key','delegated_direct','delegated goal','multiple files','in_progress','worker running','collect result',1,'aion','aion','2026-08-29T10:00:00Z','2026-08-29T12:30:00Z',NULL)`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	deliveries, err := New(s).ListDeliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 3 {
		t.Fatalf("deliveries = %#v", deliveries)
	}
	if got := strings.Join([]string{deliveries[0].ID, deliveries[1].ID, deliveries[2].ID}, ","); got != "dl-000000000000000000000001,dl-000000000000000000000002,wf-blocked" {
		t.Fatalf("order = %s", got)
	}
	byID := map[string]Delivery{}
	for _, item := range deliveries {
		if _, duplicate := byID[item.ID]; duplicate {
			t.Fatalf("duplicate delivery %q", item.ID)
		}
		byID[item.ID] = item
	}
	if got := byID["wf-blocked"]; got.Route != FullWorkflow || got.Status != "blocked" || got.Summary != "waiting on input" || got.NextAction != "ask user" {
		t.Fatalf("workflow projection = %#v", got)
	}
	if got := byID["dl-000000000000000000000001"]; got.Route != "direct_inline" || got.Status != "completed" || got.FinishedAt == "" {
		t.Fatalf("direct projection = %#v", got)
	}
}

func TestServiceSearchesEveryDeliveryFieldOnceAndResolvesPhysicalTruth(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	s, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, statement := range []string{
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-search',2,'designing','Search workflow','workflow needle','2026-08-29T10:00:00Z','2026-08-29T12:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-search','design','durable-record-needle','designer',2,'2026-08-29T11:00:00Z')`,
		`INSERT INTO direct_delivery_traces VALUES('dl-000000000000000000000003','search-key','delegated_direct','direct-goal-needle','reason-needle','blocked','summary-needle','action-needle',2,'aion','aion','2026-08-29T11:00:00Z','2026-08-29T13:00:00Z',NULL)`,
	} {
		if _, err = s.DB().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	service := New(s)
	for _, tc := range []struct{ query, id string }{
		{"DL-000000000000000000000003", "dl-000000000000000000000003"}, {"delegated_direct", "dl-000000000000000000000003"},
		{"blocked", "dl-000000000000000000000003"}, {"direct-goal-needle", "dl-000000000000000000000003"},
		{"reason-needle", "dl-000000000000000000000003"}, {"summary-needle", "dl-000000000000000000000003"},
		{"action-needle", "dl-000000000000000000000003"}, {"full_workflow", "wf-search"},
		{"designing", "wf-search"}, {"durable-record-needle", "wf-search"},
	} {
		got, err := service.SearchDeliveries(ctx, tc.query)
		if err != nil || len(got) != 1 || got[0].DeliveryID != tc.id {
			t.Fatalf("SearchDeliveries(%q) = %#v, %v", tc.query, got, err)
		}
	}
	direct, err := service.GetDelivery(ctx, "dl-000000000000000000000003")
	if err != nil || direct.Workflow != nil || direct.Delivery.RouteReason != "reason-needle" {
		t.Fatalf("direct detail = %#v, %v", direct, err)
	}
	full, err := service.GetDelivery(ctx, "wf-search")
	if err != nil || full.Workflow == nil || full.Delivery.Route != FullWorkflow || full.Workflow.Workflow.State != "designing" {
		t.Fatalf("workflow detail = %#v, %v", full, err)
	}
}

func openHistory(t *testing.T, seeded bool) *store.Store {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	s, err := store.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-old',4,'completed','shared-needle old goal','2024-01-01T00:00:00Z','2024-01-02T00:00:00Z')`,
		`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES('wf-new',8,'abandoned','new goal','2025-01-01T00:00:00Z','2025-01-09T00:00:00Z')`,
		`INSERT INTO workflows(id,revision,state,name,goal,created_at,updated_at) VALUES('wf-active',2,'implementing','Active delivery','active goal','2026-01-01T00:00:00Z','2023-01-02T00:00:00Z')`,
		`INSERT INTO events VALUES('wf-old','ready_to_complete','completed','aggregate-reviewer','approved',4,'2024-01-02T00:00:00Z')`,
		`INSERT INTO events VALUES('wf-new','planning','abandoned','daimon','event-reason',8,'2025-01-03T00:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration','literal % _ "quoted" café ','explorer',2,'2025-01-04T00:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration','same-time legacy','explorer',2,'2025-01-04T00:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-old','aggregate_review','{"verdict":"corrections","findings":"old"}','aggregate-reviewer',3,'2024-01-01T23:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-old','aggregate_review','{"verdict":"approved","findings":""}','aggregate-reviewer',4,'2024-01-02T00:00:00Z')`,
		`INSERT INTO plans VALUES('wf-active','active plan','scope',1,'{"summary":"active plan","scope":"scope","work_units":[{"id":"wu-done","description":"done","scope":"scope","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"wu-correction","description":"correct","scope":"scope","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"wu-claimed","description":"claimed","scope":"scope","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"wu-dependency","description":"dependency","scope":"scope","areas":[],"depends_on":["wu-correction"],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"wu-recovery","description":"recovery","scope":"scope","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"wu-ready","description":"ready","scope":"scope","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"wu-fresh","description":"fresh","scope":"scope","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1}')`,
		`INSERT INTO plans VALUES('wf-new','plan title','internal/history',1,'plan-text')`,
		`INSERT INTO work_units VALUES('wu-new','wf-new','unit-text','internal/history','["history"]','["wu-dependency"]',12,5,'reviewing','{"justification":"approved"}',1,3)`,
		`INSERT INTO evidence VALUES('wf-new','wu-new',3,'implementer','red cmd','exit 1 red-text','green cmd','exit 0','clean','go test','exit 0','internal/history','2025-01-06T00:00:00Z')`,
		`INSERT INTO reviews VALUES('wf-new','wu-new',3,'reviewer','approved','review-text shared-needle','', '', '2025-01-08T00:00:00Z')`,
		`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation) VALUES('claim','wf-new','wu-new','active','raw-handle-secret','implementer','2025-01-01T00:00:00Z','2025-01-02T00:00:00Z',1)`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new',NULL,'workflow_created','daimon','2025-01-01T00:00:00Z','workflow','wf-new')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new',NULL,'workflow_abandoned','daimon','2025-01-03T00:00:00Z','event','wf-new@8')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new',NULL,'exploration_recorded','explorer','2025-01-04T00:00:00Z','artifact','1')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new',NULL,'plan_submitted','planner','2025-01-05T00:00:00Z','plan','wf-new')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new','wu-new','unit_claimed','implementer','2025-01-05T01:00:00Z','work_unit','wu-new')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new','wu-new','unit_tdd_recorded','implementer','2025-01-06T00:00:00Z','evidence','wu-new@3')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-new','wu-new','unit_review_recorded','reviewer','2025-01-08T00:00:00Z','review','wu-new@3')`,
		`INSERT INTO work_units VALUES('wu-done','wf-active','done','scope','[]','[]',1,1,'done',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-correction','wf-active','correct','scope','[]','[]',1,1,'pending',NULL,0,2)`,
		`INSERT INTO work_units VALUES('wu-claimed','wf-active','claimed','scope','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-dependency','wf-active','dependency','scope','[]','["wu-correction"]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-recovery','wf-active','recovery','scope','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-ready','wf-active','ready','scope','[]','[]',1,1,'pending',NULL,0,1)`,
		`INSERT INTO work_units VALUES('wu-fresh','wf-active','fresh','scope','[]','[]',1,1,'reviewing',NULL,0,2)`,
		`INSERT INTO reviews VALUES('wf-active','wu-correction',1,'reviewer','corrections','needs work','fix typed projection','','2026-08-22T16:00:00Z')`,
		`INSERT INTO reviews VALUES('wf-active','wu-fresh',1,'reviewer','corrections','old work','superseded by evidence','','2026-08-22T15:00:00Z')`,
		`INSERT INTO evidence VALUES('wf-active','wu-fresh',2,'implementer','red','fail','green','pass','clean','test','pass','internal/history','2026-08-22T16:10:00Z')`,
		`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation) VALUES('old','wf-active','wu-claimed','active','old-secret','actor','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',1)`,
		`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation) VALUES('live','wf-active','wu-claimed','active','live-secret','actor','2026-08-22T16:00:00Z','2030-01-01T00:00:00Z',2)`,
		`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES('review','wf-active','wu-claimed','active','review-secret','reviewer','2026-08-22T16:00:00Z','2026-08-22T16:01:00Z',99,'review')`,
		`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation) VALUES('expired','wf-active','wu-recovery','active','expired-secret','actor','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z',1)`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-active','wu-claimed','unit_claimed','actor','2026-08-22T16:30:00Z','work_unit','wu-claimed')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-active','wu-correction','unit_review_recorded','reviewer','2026-08-22T16:00:00Z','review','wu-correction@1')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-old',NULL,'aggregate_review_recorded','aggregate-reviewer','2024-01-02T00:00:00Z','artifact','4')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-old',NULL,'aggregate_review_recorded','aggregate-reviewer','2024-01-02T00:00:00Z','artifact','4')`,
		`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES('wf-old',NULL,'workflow_completed','aggregate-reviewer','2024-01-02T00:00:00Z','event','wf-old@4')`,
	}
	if seeded {
		for _, statement := range statements {
			if _, err := s.DB().ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	if seeded {
		long := strings.Repeat("before ", 30) + "bounded-token" + strings.Repeat(" after", 30)
		for _, row := range []struct{ kind, content string }{{"exploration", long}, {"design", "stable-order"}, {"specification", "stable-order"}} {
			if _, err := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new',?,?, 'actor',3,'2025-01-05T00:00:00Z')`, row.kind, row.content); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 205; i++ {
			if _, err := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration','many-match','actor',4,'2025-01-07T00:00:00Z')`); err != nil {
				t.Fatal(err)
			}
		}
		for _, content := range []string{"collision-record one", "collision-record two"} {
			if _, err := s.DB().ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-new','exploration',?,'actor',5,'2025-01-07T00:00:00Z')`, content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := store.OpenReadOnly(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Store.Close() })
	return result.Store
}

func hasRecord(records []Record, kind string) bool {
	for _, record := range records {
		if record.Kind == kind {
			return true
		}
	}
	return false
}

func recordText(records []Record) string {
	var b strings.Builder
	for _, record := range records {
		b.WriteString(record.Content)
	}
	return b.String()
}

func timelineEntry(entries []Activity, action string) Activity {
	for _, entry := range entries {
		if entry.Action == action {
			return entry
		}
	}
	return Activity{}
}

func legacyHistoryRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pitcrew"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, ".pitcrew", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE workflows (id TEXT PRIMARY KEY, revision INTEGER NOT NULL, state TEXT NOT NULL, goal TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE events (workflow_id TEXT NOT NULL, from_state TEXT NOT NULL, to_state TEXT NOT NULL, actor TEXT NOT NULL, reason TEXT NOT NULL, revision_after INTEGER NOT NULL, at TEXT NOT NULL)`,
		`CREATE TABLE artifacts (id INTEGER PRIMARY KEY AUTOINCREMENT, workflow_id TEXT NOT NULL, kind TEXT NOT NULL, content TEXT NOT NULL, actor TEXT NOT NULL, accepted_revision INTEGER NOT NULL, recorded_at TEXT NOT NULL)`,
		`CREATE TABLE plans (workflow_id TEXT PRIMARY KEY, summary TEXT NOT NULL, scope TEXT NOT NULL, max_parallel_units INTEGER NOT NULL, body TEXT NOT NULL)`,
		`CREATE TABLE work_units (id TEXT PRIMARY KEY, workflow_id TEXT NOT NULL, description TEXT NOT NULL, scope TEXT NOT NULL, areas TEXT NOT NULL, depends_on TEXT NOT NULL, estimated_changed_lines INTEGER NOT NULL, estimated_review_minutes INTEGER NOT NULL, state TEXT NOT NULL, admission_exception TEXT, admission_exception_approved INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE evidence (workflow_id TEXT NOT NULL, unit_id TEXT NOT NULL, revision INTEGER NOT NULL, actor TEXT NOT NULL, red_command TEXT NOT NULL, red_outcome TEXT NOT NULL, green_command TEXT NOT NULL, green_outcome TEXT NOT NULL, refactor_summary TEXT NOT NULL, validation_command TEXT NOT NULL, validation_outcome TEXT NOT NULL, changed_paths TEXT NOT NULL, recorded_at TEXT NOT NULL)`,
		`CREATE TABLE reviews (workflow_id TEXT NOT NULL, unit_id TEXT NOT NULL, revision INTEGER NOT NULL, actor TEXT NOT NULL, verdict TEXT NOT NULL, summary TEXT NOT NULL, findings TEXT NOT NULL, plan_impact TEXT NOT NULL, recorded_at TEXT NOT NULL)`,
		`INSERT INTO workflows VALUES('wf-legacy',3,'planning','Legacy goal','2023-01-01T00:00:00Z','2023-01-03T00:00:00Z')`,
		`INSERT INTO events VALUES('wf-legacy','draft','exploring','daimon','',2,'2023-01-02T00:00:00Z')`,
		`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES('wf-legacy','exploration','legacy evidence','pc2-explorer',2,'2023-01-02T01:00:00Z')`,
		`INSERT INTO plans VALUES('wf-legacy','untimed plan','scope',1,'{}')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return root
}

func schemaSnapshot(t *testing.T, root string) string {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(root, ".pitcrew", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT type || ':' || name || ':' || sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	return strings.Join(values, "\n")
}
