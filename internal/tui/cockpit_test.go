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
	want := map[string]string{"executable": "workflow list-ready-units", "plan_notice": "Planned progress unavailable", "acknowledged_next": "ask reviewer"}
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
