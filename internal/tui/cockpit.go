package tui

import (
	"fmt"
	"strings"

	"github.com/fmazzalomo/pitcrew/internal/history"
)

type detailPane uint8

const (
	paneTree detailPane = iota
	paneStatus
	paneUnits
	paneActivity
)

type treeNodeKind uint8

const (
	nodeWorkflow treeNodeKind = iota
	nodeStage
	nodeUnit
	nodeRecord
)

type lifecycleStage uint8

const (
	stageExplore lifecycleStage = iota
	stageSpec
	stageDesign
	stagePlan
	stageBuild
	stageReview
)

type semanticState uint8

const (
	stateNeutral semanticState = iota
	stateDone
	stateActive
	stateReady
	stateWaiting
	stateWarning
	stateFailed
)

type treeNodeID struct {
	Kind             treeNodeKind
	Stage            lifecycleStage
	UnitID, RecordID string
}
type treeNode struct {
	ID, Parent treeNodeID
	Label      string
	State      semanticState
	RecordID   string
	Children   []treeNode
}
type treeRow struct {
	Node  treeNode
	Depth int
	Last  bool
}
type panelRow struct {
	ID, Label, Value string
	State            semanticState
}
type unitRow struct {
	ID, Label, Status, Reason string
	State                     semanticState
}
type activityRow struct {
	ID, Label, Actor, At string
	State                semanticState
}
type cockpitProjection struct {
	Root         treeNode
	StatusRows   []panelRow
	UnitRows     []unitRow
	ActivityRows []activityRow
}

var stageLabels = [...]string{"Explore", "Spec", "Design", "Plan", "Build", "Review"}

func projectCockpit(detail history.Detail) cockpitProjection {
	rootID := treeNodeID{Kind: nodeWorkflow}
	root := treeNode{ID: rootID, Label: displayName(detail.Workflow), State: semantic(detail.Workflow.State)}
	completed, active := completedLifecycleStages(detail.Workflow.State, detail.Occurrences), lifecyclePosition(detail.Workflow.State, detail.Occurrences)
	units := projectedUnits(detail.Synopsis)
	accepted, unitRecords, stageRecords := map[string]bool{}, map[string][]history.Record{}, map[lifecycleStage][]history.Record{}
	for _, unit := range units {
		accepted[unit.ID] = true
	}
	var unmatched []history.Record
	for _, record := range detail.Records {
		stage, ok := recordStage(record, detail.Occurrences)
		switch {
		case accepted[record.UnitID]:
			unitRecords[record.UnitID] = append(unitRecords[record.UnitID], record)
		case ok:
			stageRecords[stage] = append(stageRecords[stage], record)
		default:
			unmatched = append(unmatched, record)
		}
	}
	for stage := stageExplore; stage <= stageReview; stage++ {
		if !completed[stage] && int(stage) != active && len(stageRecords[stage]) == 0 && (stage != stageBuild || len(units) == 0) {
			continue
		}
		stageID := treeNodeID{Kind: nodeStage, Stage: stage}
		branch := treeNode{ID: stageID, Parent: rootID, Label: stageLabels[stage]}
		if completed[stage] {
			branch.State = stateDone
		} else if int(stage) == active {
			branch.State = stateActive
		}
		for _, record := range stageRecords[stage] {
			branch.Children = append(branch.Children, recordNode(record, stageID, stage))
		}
		if stage == stageBuild {
			for _, unit := range units {
				unitID := treeNodeID{Kind: nodeUnit, Stage: stageBuild, UnitID: unit.ID}
				node := treeNode{ID: unitID, Parent: stageID, Label: zeroDash(unit.Description), State: semantic(unit.Status)}
				for _, record := range unitRecords[unit.ID] {
					node.Children = append(node.Children, recordNode(record, unitID, stageBuild))
				}
				branch.Children = append(branch.Children, node)
			}
		}
		root.Children = append(root.Children, branch)
	}
	for _, record := range unmatched {
		root.Children = append(root.Children, recordNode(record, rootID, stageExplore))
	}
	projection := cockpitProjection{Root: root, StatusRows: statusRows(detail)}
	for _, unit := range units {
		projection.UnitRows = append(projection.UnitRows, unitRow{unit.ID, zeroDash(unit.Description), unit.Status, unit.Reason, semantic(unit.Status)})
	}
	for _, item := range detail.Occurrences {
		projection.ActivityRows = append(projection.ActivityRows, activityRow{item.ID, actionLabel(item.Activity), item.Actor, item.At, semantic(item.Outcome)})
	}
	return projection
}

func projectedUnits(s history.Synopsis) []history.UnitStatus {
	if s.Planned != nil {
		if len(s.Planned.Units) > 0 {
			return s.Planned.Units
		}
		return s.Planned.Pending
	}
	if s.Current != nil {
		return []history.UnitStatus{*s.Current}
	}
	return nil
}

func recordNode(record history.Record, parent treeNodeID, stage lifecycleStage) treeNode {
	id := treeNodeID{Kind: nodeRecord, Stage: stage, UnitID: record.UnitID, RecordID: record.ID}
	return treeNode{ID: id, Parent: parent, Label: zeroDash(record.Title), RecordID: record.ID}
}

func recordStage(record history.Record, occurrences []history.Occurrence) (lifecycleStage, bool) {
	for _, item := range occurrences {
		matched := item.RecordID == record.ID
		for _, related := range item.RelatedRecordIDs {
			matched = matched || related == record.ID
		}
		if matched {
			if stage, ok := namedStage(item.Phase + " " + item.Activity); ok {
				return stage, true
			}
		}
	}
	return namedStage(record.Kind)
}

func namedStage(value string) (lifecycleStage, bool) {
	value = strings.ToLower(value)
	groups := [...][]string{{"explor"}, {"spec"}, {"design"}, {"plan"}, {"implement", "build", "unit", "claim", "tdd"}, {"review", "aggregate"}}
	for stage, terms := range groups {
		for _, term := range terms {
			if strings.Contains(value, term) {
				return lifecycleStage(stage), true
			}
		}
	}
	return 0, false
}

func statusRows(detail history.Detail) []panelRow {
	s := detail.Synopsis
	rows := []panelRow{{"state", "State", detail.Workflow.State, semantic(detail.Workflow.State)}}
	if s.Planned != nil {
		rows = append(rows, panelRow{"plan_progress", "Plan", fmt.Sprintf("%d/%d · %d%%", s.Planned.Done, s.Planned.Total, s.Planned.Percent), stateNeutral})
	} else if s.PlanNotice != "" {
		rows = append(rows, panelRow{"plan_notice", "Plan", s.PlanNotice, stateWarning})
	}
	if s.Current != nil {
		rows = append(rows, panelRow{"current", "Current", zeroDash(s.Current.Description), semantic(s.Current.Status)})
	}
	if s.Blocker != nil {
		rows = append(rows, panelRow{"blocker", "Blocked", zeroDash(s.Blocker.Reason), stateWarning})
	}
	if s.CorrectionPolicy != nil {
		rows = append(rows, panelRow{"correction_rounds", "Corrections", fmt.Sprintf("%d/%d used", s.CorrectionPolicy.Used, s.CorrectionPolicy.Allowed), stateNeutral})
		if s.CorrectionPolicy.BlockerRevision != 0 {
			rows = append(rows, panelRow{"correction_blocker", "Blocker review", fmt.Sprintf("r%d", s.CorrectionPolicy.BlockerRevision), stateWarning})
		}
		rows = append(rows, panelRow{"correction_authority", "Authority", zeroDash(s.CorrectionPolicy.Authority), semantic(s.CorrectionPolicy.Authority)})
	}
	rows = append(rows, panelRow{"executable", "Executable", zeroDash(s.NextAction), stateReady})
	if s.Progress != nil {
		rows = append(rows, panelRow{"acknowledged_status", "Acknowledged", zeroDash(s.Progress.Status), stateNeutral}, panelRow{"acknowledged_summary", "Report", zeroDash(s.Progress.Summary), stateNeutral}, panelRow{"acknowledged_next", "Acknowledged next", zeroDash(s.Progress.NextAction), stateNeutral})
	}
	return rows
}

func flattenTree(root treeNode, expanded map[treeNodeID]bool) []treeRow {
	var rows []treeRow
	var walk func(treeNode, int, bool)
	walk = func(node treeNode, depth int, last bool) {
		rows = append(rows, treeRow{node, depth, last})
		if expanded[node.ID] {
			for i, child := range node.Children {
				walk(child, depth+1, i == len(node.Children)-1)
			}
		}
	}
	walk(root, 0, true)
	return rows
}

func semantic(value string) semanticState {
	switch strings.ToLower(value) {
	case "done", "completed", "approved":
		return stateDone
	case "implementing", "ready_to_complete", "claimed", "reviewing":
		return stateActive
	case "ready":
		return stateReady
	case "correction", "corrections", "recovery", "dependency waiting", "blocked", "failed":
		return stateWarning
	case "abandoned":
		return stateFailed
	case "queued", "waiting":
		return stateWaiting
	default:
		return stateNeutral
	}
}
