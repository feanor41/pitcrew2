package tui

import (
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/history"
)

func TestProjectCockpitPreservesSparseLifecycleAndAcceptedUnitOrder(t *testing.T) {
	detail := history.Detail{
		Workflow: history.Workflow{ID: "wf-1", Name: "Cockpit", State: "implementing"},
		Synopsis: history.Synopsis{Planned: &history.PlannedWork{Total: 2, Units: []history.UnitStatus{
			{ID: "wu-b", Description: "Second", Status: "Ready"},
			{ID: "wu-a", Description: "First", Status: "Done"},
		}}},
		Occurrences: []history.Occurrence{
			{ID: "o1", Activity: "exploration_recorded", Phase: "explore", RecordID: "artifact:1"},
			{ID: "o2", Activity: "unit_claimed", Phase: "build", RecordID: "claim:1"},
		},
		Records: []history.Record{
			{ID: "artifact:1", Kind: "exploration", Title: "Evidence"},
			{ID: "claim:1", Kind: "claim", UnitID: "wu-b", Title: "Claim"},
			{ID: "odd:1", Kind: "unknown", Title: "Retained"},
		},
	}

	got := projectCockpit(detail)
	if len(got.Root.Children) != 3 {
		t.Fatalf("root children = %#v", got.Root.Children)
	}
	if got.Root.Children[0].ID.Stage != stageExplore || got.Root.Children[0].State != stateDone {
		t.Fatalf("explore = %#v", got.Root.Children[0])
	}
	build := got.Root.Children[1]
	if build.ID.Stage != stageBuild || len(build.Children) != 2 {
		t.Fatalf("build = %#v", build)
	}
	if build.Children[0].ID.UnitID != "wu-b" || build.Children[1].ID.UnitID != "wu-a" {
		t.Fatalf("unit order = %#v", build.Children)
	}
	if got.Root.Children[2].ID.RecordID != "odd:1" {
		t.Fatalf("unmapped record was dropped or falsely staged: %#v", got.Root.Children[2])
	}
	for _, node := range got.Root.Children {
		if node.ID.Stage == stageSpec || node.ID.Stage == stageDesign || node.ID.Stage == stagePlan {
			t.Fatalf("sparse stage was invented: %#v", node)
		}
	}
}

func TestProjectCockpitKeepsExecutableAndAcknowledgedStatusSeparate(t *testing.T) {
	detail := history.Detail{Workflow: history.Workflow{ID: "wf", State: "implementing"}, Synopsis: history.Synopsis{
		NextAction: "workflow list-ready-units", PlanNotice: "Planned progress unavailable",
		Progress: &history.Progress{Status: "blocked", Summary: "waiting for reviewer", NextAction: "ask reviewer"},
	}}
	rows := projectCockpit(detail).StatusRows
	want := map[string]string{"route": history.FullWorkflow, "executable": "workflow list-ready-units", "plan_notice": "Planned progress unavailable", "acknowledged_next": "ask reviewer"}
	for _, row := range rows {
		if expected, ok := want[row.ID]; ok {
			if row.Value != expected {
				t.Fatalf("%s = %q", row.ID, row.Value)
			}
			delete(want, row.ID)
		}
		if row.ID == "plan_progress" {
			t.Fatalf("invented exact progress: %#v", row)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing status rows: %#v", want)
	}
}

func TestProjectCockpitShowsOneCorrectionAuthorityAndExecutableAction(t *testing.T) {
	detail := history.Detail{Workflow: history.Workflow{State: "ready_to_complete"}, Synopsis: history.Synopsis{
		NextAction: "workflow recover-aggregate", Blocker: &history.UnitStatus{Reason: "latest blocker"},
		CorrectionPolicy: &history.CorrectionStatus{PolicyAware: true, Allowed: 1, Used: 1, BlockerRevision: 7, Authority: "authorized"},
	}}
	rows := statusRows(detail)
	want := map[string]string{"correction_rounds": "1/1 used", "correction_blocker": "r7", "correction_authority": "authorized", "executable": "workflow recover-aggregate"}
	for _, row := range rows {
		if value, ok := want[row.ID]; ok && row.Value == value {
			delete(want, row.ID)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing correction rows: %#v from %#v", want, rows)
	}
	if actionLabel("aggregate_correction_started") != "Started aggregate correction" || actionLabel("correction_authorized") != "Authorized correction" {
		t.Fatal("correction activity labels are not contextual")
	}
}

func TestFlattenTreeUsesSemanticIdentityAndExpansion(t *testing.T) {
	root := treeNode{ID: treeNodeID{Kind: nodeWorkflow}, Label: "wf", Children: []treeNode{{
		ID: treeNodeID{Kind: nodeStage, Stage: stageBuild}, Label: "Build", Children: []treeNode{{
			ID: treeNodeID{Kind: nodeUnit, Stage: stageBuild, UnitID: "wu"}, Label: "unit",
		}},
	}}}
	rows := flattenTree(root, map[treeNodeID]bool{root.ID: true, root.Children[0].ID: true})
	if len(rows) != 3 || rows[2].Node.ID.UnitID != "wu" || rows[2].Depth != 2 {
		t.Fatalf("rows = %#v", rows)
	}
}
