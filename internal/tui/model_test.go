package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/fmazzalomo/pitcrew/internal/history"
)

func TestModelArrowAndVimKeysAreEquivalent(t *testing.T) {
	workflows := []history.Workflow{{ID: "one"}, {ID: "two"}, {ID: "three"}}
	for _, test := range []struct {
		name       string
		first      tea.KeyPressMsg
		second     tea.KeyPressMsg
		start      Model
		wantScreen Screen
		wantIndex  int
	}{
		{"down", special(tea.KeyDown), textKey("j"), Model{screen: WorkflowsScreen, workflows: workflows}, WorkflowsScreen, 1},
		{"up", special(tea.KeyUp), textKey("k"), Model{screen: WorkflowsScreen, workflows: workflows, selected: 2}, WorkflowsScreen, 1},
		{"back", special(tea.KeyLeft), textKey("h"), Model{screen: DetailScreen, selected: 2}, WorkflowsScreen, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, _ := test.start.Update(test.first)
			second, _ := test.start.Update(test.second)
			if first.screen != test.wantScreen || first.selected != test.wantIndex || first.screen != second.screen || first.selected != second.selected || !reflect.DeepEqual(first.detail, second.detail) {
				t.Fatalf("arrow=%#v vim=%#v; want screen=%v selected=%d", first, second, test.wantScreen, test.wantIndex)
			}
		})
	}

	loader := fakeLoader{detail: history.Detail{Workflow: workflows[0]}}
	for _, key := range []tea.KeyPressMsg{special(tea.KeyRight), textKey("l"), special(tea.KeyEnter)} {
		model := New(loader)
		model.screen = WorkflowsScreen
		model.workflows = workflows
		_, command := model.Update(key)
		if command == nil {
			t.Fatalf("%q did not select the focused workflow", key.String())
		}
		if _, ok := command().(detailLoadedMsg); !ok {
			t.Fatalf("%q command returned %T", key.String(), command())
		}
	}
}

func TestModelHomeActionsNavigateHonestlyAndKeepWorkflowSelection(t *testing.T) {
	loader := fakeLoader{}
	model := New(loader)
	model.workflows = []history.Workflow{{ID: "one"}, {ID: "two"}}
	model.selected = 1
	if model.screen != HomeScreen || model.homeSelected != 0 {
		t.Fatalf("initial home state = screen:%v selection:%d", model.screen, model.homeSelected)
	}

	for _, key := range []tea.KeyPressMsg{special(tea.KeyUp), textKey("k")} {
		candidate, _ := model.Update(key)
		if candidate.homeSelected != 0 {
			t.Fatalf("%q moved above first action: %d", key.String(), candidate.homeSelected)
		}
	}

	install, command := model.Update(special(tea.KeyEnter))
	if command != nil || install.screen != HomeScreen || !strings.Contains(install.homeNotice, "read-only TUI") || !strings.Contains(install.homeNotice, "pitcrew install <codex|opencode|claude|pi>") {
		t.Fatalf("install action = screen:%v notice:%q command:%v", install.screen, install.homeNotice, command != nil)
	}

	configure, _ := model.Update(textKey("j"))
	configure, command = configure.Update(special(tea.KeyEnter))
	if command != nil || configure.screen != HomeScreen || !strings.Contains(configure.homeNotice, "Runtime model configuration is unavailable") {
		t.Fatalf("configure action = screen:%v notice:%q command:%v", configure.screen, configure.homeNotice, command != nil)
	}

	workflows, _ := configure.Update(textKey("j"))
	workflows, command = workflows.Update(special(tea.KeyEnter))
	if command != nil || workflows.screen != WorkflowsScreen || workflows.selected != 1 {
		t.Fatalf("workflows action = screen:%v selection:%d command:%v", workflows.screen, workflows.selected, command != nil)
	}
	home, _ := workflows.Update(special(tea.KeyEscape))
	if home.screen != HomeScreen || home.selected != 1 {
		t.Fatalf("back to home = screen:%v workflow selection:%d", home.screen, home.selected)
	}

	exit := model
	for range 10 {
		exit, _ = exit.Update(textKey("j"))
	}
	if exit.homeSelected != 3 {
		t.Fatalf("home selection did not clamp at Exit: %d", exit.homeSelected)
	}
	_, command = exit.Update(special(tea.KeyEnter))
	if command == nil {
		t.Fatal("Exit did not return a managed quit command")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("Exit command returned %T", command())
	}
}

func TestModelBackgroundWorkflowLoadDoesNotLeaveHome(t *testing.T) {
	model := New(fakeLoader{})
	model, _ = model.Update(workflowsLoadedMsg{loadMeta: loadMeta{kind: loadWorkflows, generation: 1}, workflows: []history.Workflow{{ID: "wf"}}})
	if model.screen != HomeScreen || len(model.workflows) != 1 || model.loading || model.err != nil {
		t.Fatalf("background load state = screen:%v workflows:%d loading:%v err:%v", model.screen, len(model.workflows), model.loading, model.err)
	}
	model = New(fakeLoader{})
	model, _ = model.Update(loadFailedMsg{loadMeta: loadMeta{kind: loadWorkflows, generation: 1}, err: errors.New("database is locked")})
	if model.screen != HomeScreen || model.loading || model.err == nil {
		t.Fatalf("background failure state = screen:%v loading:%v err:%v", model.screen, model.loading, model.err)
	}
}

func TestModelBackToHomeRejectsLateDrillDownLoads(t *testing.T) {
	workflow := history.Workflow{ID: "wf"}
	model := New(fakeLoader{detail: history.Detail{Workflow: workflow}})
	model.screen, model.workflows = WorkflowsScreen, []history.Workflow{workflow}
	loadingDetail, command := model.Update(special(tea.KeyEnter))
	if command == nil || loadingDetail.activeLoad != loadDetail {
		t.Fatalf("detail load = kind:%v command:%v", loadingDetail.activeLoad, command != nil)
	}
	home, _ := loadingDetail.Update(special(tea.KeyEscape))
	home, _ = home.Update(command())
	if home.screen != HomeScreen || home.opened.Detail.Workflow.ID != "" {
		t.Fatalf("late detail stole navigation: screen:%v opened:%q", home.screen, home.opened.Detail.Workflow.ID)
	}

	model = New(fakeLoader{results: []history.SearchResult{{WorkflowID: "wf"}}})
	model.screen = WorkflowsScreen
	model, _ = model.Update(textKey("/"))
	model, command = model.Update(special(tea.KeyEnter))
	if command == nil || model.activeLoad != loadResults {
		t.Fatalf("results load = kind:%v command:%v", model.activeLoad, command != nil)
	}
	home, _ = model.Update(special(tea.KeyEscape))
	home, _ = home.Update(command())
	if home.screen != HomeScreen || len(home.results) != 0 {
		t.Fatalf("late results stole navigation: screen:%v results:%d", home.screen, len(home.results))
	}
}

func TestModelWorkflowWindowTracksSelectionAtMinimumHeight(t *testing.T) {
	workflows := make([]history.Workflow, 12)
	for i := range workflows {
		workflows[i] = history.Workflow{ID: fmt.Sprintf("wf-%02d", i)}
	}
	model := Model{screen: WorkflowsScreen, workflows: workflows}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	for range 7 {
		model, _ = model.Update(textKey("j"))
	}
	if model.selected != 7 || model.workflowTop != 3 {
		t.Fatalf("minimum workflow window = selected:%d top:%d", model.selected, model.workflowTop)
	}
	for range 20 {
		model, _ = model.Update(textKey("k"))
	}
	if model.selected != 0 || model.workflowTop != 0 {
		t.Fatalf("upper workflow window clamp = selected:%d top:%d", model.selected, model.workflowTop)
	}

	model.selected, model.workflowTop = 11, 7
	model, _ = model.Update(workflowsLoadedMsg{workflows: workflows[:3]})
	if model.selected != 2 || model.workflowTop != 0 {
		t.Fatalf("replacement workflow window = selected:%d top:%d", model.selected, model.workflowTop)
	}
}

func TestModelSearchAcceptsVimTextAndOpensExactResult(t *testing.T) {
	result := history.SearchResult{WorkflowID: "wf", RecordID: "artifact:42", Kind: "proposal"}
	resolution := history.Resolution{Record: history.Record{ID: "artifact:42", Content: "exact"}}
	model := New(fakeLoader{results: []history.SearchResult{result}, resolution: resolution})
	model.screen = WorkflowsScreen
	model, _ = model.Update(textKey("/"))
	for _, key := range []string{"h", "j", "k", "l"} {
		model, _ = model.Update(textKey(key))
	}
	if model.query != "hjkl" || !model.searchFocused || model.selected != 0 {
		t.Fatalf("search state = query %q focused=%v selected=%d", model.query, model.searchFocused, model.selected)
	}
	model, command := model.Update(special(tea.KeyEnter))
	if model.searchFocused || command == nil {
		t.Fatalf("submit state = %#v, command nil=%v", model, command == nil)
	}
	model, _ = model.Update(command())
	if model.screen != ResultsScreen || len(model.results) != 1 {
		t.Fatalf("results state = %#v", model)
	}
	model, command = model.Update(special(tea.KeyEnter))
	model, _ = model.Update(command())
	if model.screen != DetailScreen || model.opened.Record.ID != "artifact:42" || model.opened.Record.Content != "exact" {
		t.Fatalf("opened resolution = %#v", model.opened)
	}
}

func TestModelSearchUsesTextInputEditingAndRestoresScreenOnCancel(t *testing.T) {
	model := New(fakeLoader{})
	model.screen = ResultsScreen
	model.query = "previous"

	model, command := model.Update(textKey("/"))
	if command == nil || !model.search.Focused() || model.search.Value() != "" {
		t.Fatalf("focused search = focused:%v value:%q command:%v", model.search.Focused(), model.search.Value(), command != nil)
	}
	for _, key := range []tea.KeyPressMsg{textKey("h"), textKey("q"), textKey("r"), textKey("l"), special(tea.KeyLeft), special(tea.KeyBackspace), textKey("k")} {
		model, _ = model.Update(key)
	}
	if model.search.Value() != "hqkl" || model.screen != ResultsScreen {
		t.Fatalf("edited search = value:%q screen:%v", model.search.Value(), model.screen)
	}

	model, _ = model.Update(special(tea.KeyEscape))
	if model.search.Focused() || model.query != "previous" || model.screen != ResultsScreen {
		t.Fatalf("cancelled search = focused:%v query:%q screen:%v", model.search.Focused(), model.query, model.screen)
	}
}

func TestModelEvidenceViewportScrollReflowAndContentInvalidation(t *testing.T) {
	record := history.Record{ID: "artifact:7", Title: "Validation", Revision: 1, Content: "# Result\n\n" + strings.Repeat("line of evidence\n", 30)}
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Records: []history.Record{record}}
	model := New(fakeLoader{})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 16})
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail, Record: record}})
	if model.evidence.recordID != record.ID || model.evidence.mode != evidenceMarkdown || model.viewport.GetContent() == "" {
		t.Fatalf("prepared evidence = cache:%#v content:%q", model.evidence, model.viewport.GetContent())
	}

	for range 5 {
		model, _ = model.Update(textKey("j"))
	}
	beforeOffset, beforeBody := model.viewport.YOffset(), model.evidence.body
	if beforeOffset == 0 {
		t.Fatal("viewport did not scroll")
	}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	if model.evidence.width == 70 || model.evidence.body == beforeBody || model.viewport.YOffset() == 0 {
		t.Fatalf("width reflow = width:%d changed:%v offset:%d", model.evidence.width, model.evidence.body != beforeBody, model.viewport.YOffset())
	}

	updated := record
	updated.Content = "# Updated\n\n" + strings.Repeat("replacement evidence\n", 20)
	updatedDetail := detail
	updatedDetail.Records = []history.Record{updated}
	meta := model.startLoad(loadDetail, true)
	model, _ = model.Update(detailLoadedMsg{loadMeta: meta, resolution: history.Resolution{Detail: updatedDetail}})
	if !strings.Contains(model.viewport.GetContent(), "Updated") || model.evidence.source == evidenceSource(record) {
		t.Fatalf("same-record refresh did not invalidate cache: %#v", model.evidence)
	}
	if model.viewport.YOffset() > beforeOffset {
		t.Fatalf("refresh offset grew from %d to %d", beforeOffset, model.viewport.YOffset())
	}

	refreshedBody, refreshedSource, refreshedOffset := model.evidence.body, model.evidence.source, model.viewport.YOffset()
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if model.evidence.body != refreshedBody || model.evidence.source != refreshedSource || model.viewport.YOffset() > refreshedOffset {
		t.Fatalf("height-only resize rerendered or advanced evidence: cache:%#v offset:%d", model.evidence, model.viewport.YOffset())
	}

	next := history.Record{ID: "artifact:8", Content: strings.Repeat("next evidence\n", 20)}
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: history.Detail{Workflow: detail.Workflow, Records: []history.Record{next}}, Record: next}})
	if model.evidence.recordID != next.ID || model.viewport.YOffset() != 0 {
		t.Fatalf("new evidence did not reset viewport: cache:%#v offset:%d", model.evidence, model.viewport.YOffset())
	}
}

func TestModelEvidenceBackClearsDerivedStateAndKeepsOccurrence(t *testing.T) {
	record := history.Record{ID: "artifact:7", Content: strings.Repeat("evidence\n", 20)}
	occurrence := history.Occurrence{ID: "activity:7", RecordID: record.ID}
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{occurrence}, Records: []history.Record{record}}
	model := New(fakeLoader{})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 16})
	model.detail.occurrenceID = occurrence.ID
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail, Record: record}})
	model, _ = model.Update(textKey("j"))
	model, _ = model.Update(special(tea.KeyEscape))
	if model.opened.Record.ID != "" || model.detail.occurrenceID != occurrence.ID || model.evidence.recordID != "" || model.viewport.GetContent() != "" || model.viewport.YOffset() != 0 {
		t.Fatalf("back state = opened:%q detail:%#v cache:%#v offset:%d content:%q", model.opened.Record.ID, model.detail, model.evidence, model.viewport.YOffset(), model.viewport.GetContent())
	}
}

func TestModelVersionAndOccurrenceDrillDown(t *testing.T) {
	record := history.Record{ID: "artifact:7", Kind: "specification", Content: "Scenario: exact Gherkin"}
	occurrence := history.Occurrence{ID: "activity:7", RecordID: record.ID, Work: "Specification"}
	want := history.Resolution{Detail: history.Detail{Occurrences: []history.Occurrence{occurrence}, Records: []history.Record{record}}, Record: record}
	model := New(fakeLoader{occurrenceResolution: want})
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: want.Detail}})
	if model.Version() != "0.12.0" {
		t.Fatalf("Version() = %q", model.Version())
	}
	for _, key := range []tea.KeyPressMsg{special(tea.KeyEnter), special(tea.KeyRight), textKey("l")} {
		candidate, command := model.Update(key)
		if command == nil {
			t.Fatalf("%q did not open focused activity", key.String())
		}
		candidate, _ = candidate.Update(command())
		if candidate.opened.Record.ID != "artifact:7" || candidate.detail.occurrenceID != "activity:7" {
			t.Fatalf("%q opened %#v with cursor %#v", key.String(), candidate.opened.Record, candidate.detail)
		}
	}
}

func TestModelSemanticOccurrenceSurvivesResizeAndBack(t *testing.T) {
	seen := []string{}
	occurrences := []history.Occurrence{
		{ID: "activity:1", RecordID: "artifact:7"},
		{ID: "activity:2", RecordID: "artifact:7"},
	}
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: occurrences}
	model := New(fakeLoader{occurrenceIDs: &seen, occurrenceResolution: history.Resolution{Detail: detail, Record: history.Record{ID: "artifact:7"}}})
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail}})
	model, _ = model.Update(textKey("j"))
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	if model.detail.occurrenceID != "activity:2" {
		t.Fatalf("repeated activity focus = %#v", model.detail)
	}
	model, command := model.Update(special(tea.KeyEnter))
	if command == nil {
		t.Fatal("focused repeated activity did not open")
	}
	model, _ = model.Update(command())
	if !reflect.DeepEqual(seen, []string{"activity:2"}) {
		t.Fatalf("resolved occurrences = %v", seen)
	}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if model.opened.Record.ID != "artifact:7" || model.detail.occurrenceID != "activity:2" {
		t.Fatalf("exact detail identity lost on resize: %#v", model.detail)
	}
	model, _ = model.Update(special(tea.KeyEscape))
	if model.opened.Record.ID != "" || model.detail.occurrenceID != "activity:2" || model.screen != DetailScreen {
		t.Fatalf("back did not restore semantic focus: %#v", model)
	}
}

func TestModelOccurrenceNavigationPageHomeEndAndClamps(t *testing.T) {
	occurrences := make([]history.Occurrence, 12)
	for i := range occurrences {
		occurrences[i] = history.Occurrence{ID: fmt.Sprintf("activity:%d", i), Work: fmt.Sprintf("unit-%d", i)}
	}
	detail := history.Detail{Synopsis: history.Synopsis{Current: &history.UnitStatus{Description: "unit-9", Status: "Claimed"}}, Occurrences: occurrences}
	model, _ := (Model{width: 120, height: 20}).Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail}})
	if model.detail.occurrenceID != "activity:9" {
		t.Fatalf("initial operational focus = %#v", model.detail)
	}
	model, _ = model.Update(special(tea.KeyHome))
	arrow, _ := model.Update(special(tea.KeyDown))
	vim, _ := model.Update(textKey("j"))
	if arrow.detail.occurrenceID != "activity:1" || !reflect.DeepEqual(arrow.detail, vim.detail) {
		t.Fatalf("semantic Arrow/Vim mismatch: arrow=%#v vim=%#v", arrow.detail, vim.detail)
	}
	model, _ = model.Update(special(tea.KeyPgDown))
	if model.detail.occurrenceID != "activity:2" {
		t.Fatalf("page down focus = %#v", model.detail)
	}
	model, _ = model.Update(special(tea.KeyPgUp))
	if model.detail.occurrenceID != "activity:0" {
		t.Fatalf("page up focus = %#v", model.detail)
	}
	model, _ = model.Update(special(tea.KeyEnd))
	if model.detail.occurrenceID != "activity:11" {
		t.Fatalf("end semantic focus = %#v", model.detail)
	}
	for range 20 {
		model, _ = model.Update(textKey("j"))
	}
	if model.detail.occurrenceID != "activity:11" {
		t.Fatalf("lower clamp = %#v", model.detail)
	}
}

func TestModelOccurrencePageUsesRenderedHeightAndRetainsFocusOnResize(t *testing.T) {
	record := history.Record{ID: "artifact:preview", Content: "preview"}
	occurrences := make([]history.Occurrence, 6)
	for i := range occurrences {
		occurrences[i] = history.Occurrence{ID: fmt.Sprintf("activity:%d", i), RecordID: record.ID}
	}
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: occurrences, Records: []history.Record{record}}
	model, _ := (Model{width: 120, height: 20}).Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail}})
	model, _ = model.Update(special(tea.KeyHome))
	model, _ = model.Update(special(tea.KeyPgDown))
	if model.detail.occurrenceID != "activity:2" {
		t.Fatalf("measured page focus = %#v", model.detail)
	}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	if model.detail.occurrenceID != "activity:2" || model.detail.occurrenceTop != 1 {
		t.Fatalf("resized semantic window = %#v", model.detail)
	}
}

func TestModelRelatedRecordDrillDownResolvesPreviewedRecordExactly(t *testing.T) {
	seen := []string{}
	record := history.Record{ID: "artifact:related", Content: "related evidence"}
	occurrence := history.Occurrence{ID: "activity:1", RelatedRecordIDs: []string{"artifact:related"}}
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{occurrence}, Records: []history.Record{record}}
	want := history.Resolution{Detail: detail, Record: record}
	model := New(fakeLoader{occurrenceRecordIDs: &seen, occurrenceResolution: want})
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail}})
	model, command := model.Update(special(tea.KeyEnter))
	if command == nil {
		t.Fatal("related record drill-down command is nil")
	}
	model, _ = model.Update(command())
	if !reflect.DeepEqual(seen, []string{"artifact:related"}) || model.opened.Record.ID != record.ID {
		t.Fatalf("related drill-down = seen %v record %#v", seen, model.opened.Record)
	}
}

func TestModelRelatedRecordLevelSelectsEveryRecordExactly(t *testing.T) {
	records := []history.Record{
		{ID: "artifact:primary", Kind: "specification", Title: "Specification", Content: "primary"},
		{ID: "review:one", Kind: "review", Title: "Implementation review", Content: "review"},
		{ID: "evidence:one", Kind: "evidence", Title: "Focused validation", Content: "validation"},
	}
	occurrence := history.Occurrence{
		ID: "activity:multi", RecordID: records[0].ID,
		RelatedRecordIDs: []string{records[1].ID, records[0].ID, records[2].ID},
	}
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{occurrence}, Records: records}

	for index, want := range records {
		t.Run(want.ID, func(t *testing.T) {
			seen := []string{}
			model := New(fakeLoader{occurrenceRecordIDs: &seen, occurrenceResolution: history.Resolution{Detail: detail}})
			model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail}})
			model, command := model.Update(special(tea.KeyEnter))
			if command != nil || !model.relatedRecordMode() || model.detail.relatedRecordID != records[0].ID {
				t.Fatalf("related level = command:%v cursor:%#v", command != nil, model.detail)
			}
			for range index {
				model, _ = model.Update(textKey("j"))
			}
			model, command = model.Update(special(tea.KeyEnter))
			if command == nil {
				t.Fatalf("selected related record %q did not resolve", want.ID)
			}
			model, _ = model.Update(command())
			if !reflect.DeepEqual(seen, []string{want.ID}) || model.opened.Record.ID != want.ID {
				t.Fatalf("resolved related record = seen:%v opened:%q, want %q", seen, model.opened.Record.ID, want.ID)
			}
		})
	}
}

func TestModelRelatedRecordFocusSurvivesBackResizeAndRefresh(t *testing.T) {
	records := []history.Record{{ID: "artifact:one"}, {ID: "review:two"}, {ID: "evidence:three"}}
	occurrence := history.Occurrence{ID: "activity:multi", RecordID: records[0].ID, RelatedRecordIDs: []string{records[1].ID, records[2].ID}}
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{occurrence}, Records: records}
	model := New(fakeLoader{detail: detail, occurrenceResolution: history.Resolution{Detail: detail}})
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail}})
	model, _ = model.Update(special(tea.KeyEnter))
	model, _ = model.Update(textKey("j"))

	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	if model.detail.occurrenceID != occurrence.ID || model.detail.relatedRecordID != records[1].ID || !model.relatedRecordMode() {
		t.Fatalf("resize lost related focus: %#v", model.detail)
	}
	model = runRefresh(t, model)
	if model.detail.occurrenceID != occurrence.ID || model.detail.relatedRecordID != records[1].ID || !model.relatedRecordMode() {
		t.Fatalf("refresh lost related focus: %#v", model.detail)
	}

	model, command := model.Update(special(tea.KeyEnter))
	if command == nil {
		t.Fatal("focused related record did not open")
	}
	model, _ = model.Update(command())
	model, _ = model.Update(special(tea.KeyEscape))
	if model.opened.Record.ID != "" || model.detail.occurrenceID != occurrence.ID || model.detail.relatedRecordID != records[1].ID || !model.relatedRecordMode() {
		t.Fatalf("evidence back lost related focus: opened:%q cursor:%#v", model.opened.Record.ID, model.detail)
	}
	model, _ = model.Update(special(tea.KeyEscape))
	if model.screen != DetailScreen || model.relatedRecordMode() || model.detail.occurrenceID != occurrence.ID || model.detail.relatedRecordID != records[1].ID {
		t.Fatalf("related-level back lost focus: screen:%v cursor:%#v", model.screen, model.detail)
	}
	model, command = model.Update(special(tea.KeyEnter))
	if command != nil || !model.relatedRecordMode() || model.detail.relatedRecordID != records[1].ID {
		t.Fatalf("re-entered related level lost focus: command:%v cursor:%#v", command != nil, model.detail)
	}
}

func TestModelInitialOccurrenceFocusPrecedence(t *testing.T) {
	occurrences := []history.Occurrence{{ID: "old", Work: "other"}, {ID: "unit", Work: "target"}, {ID: "aggregate", Outcome: "Corrections"}, {ID: "terminal", Activity: "workflow_completed"}, {ID: "latest", Work: "other"}}
	for _, test := range []struct {
		name     string
		workflow history.Workflow
		synopsis history.Synopsis
		want     string
	}{
		{"correction", history.Workflow{}, history.Synopsis{Blocker: &history.UnitStatus{Description: "target", Status: "Correction"}}, "unit"},
		{"aggregate correction", history.Workflow{}, history.Synopsis{Blocker: &history.UnitStatus{Description: "Aggregate review", Status: "Correction"}}, "aggregate"},
		{"current", history.Workflow{}, history.Synopsis{Current: &history.UnitStatus{Description: "target", Status: "Reviewing"}}, "unit"},
		{"terminal", history.Workflow{State: "completed"}, history.Synopsis{}, "terminal"},
		{"blocked", history.Workflow{}, history.Synopsis{Blocker: &history.UnitStatus{Description: "target", Status: "Dependency waiting"}}, "unit"},
		{"latest", history.Workflow{}, history.Synopsis{}, "latest"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _ := (Model{}).Update(detailLoadedMsg{resolution: history.Resolution{Detail: history.Detail{Workflow: test.workflow, Synopsis: test.synopsis, Occurrences: occurrences}}})
			if model.detail.occurrenceID != test.want {
				t.Fatalf("focus = %q, want %q", model.detail.occurrenceID, test.want)
			}
		})
	}
}

func TestModelAsyncResizeBackQuitAndHints(t *testing.T) {
	model := New(fakeLoader{})
	model, _ = model.Update(workflowsLoadedMsg{workflows: []history.Workflow{{ID: "one"}, {ID: "two"}}})
	model.screen = WorkflowsScreen
	model.selected = 1
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if model.width != 120 || model.height != 40 || model.selected != 1 {
		t.Fatalf("resized model = %#v", model)
	}
	if hints := model.Hints(); !strings.Contains(hints, "↑/k") || !strings.Contains(hints, "/ search") || !strings.Contains(hints, "q quit") {
		t.Fatalf("navigation hints = %q", hints)
	}
	model, _ = model.Update(textKey("/"))
	if hints := model.Hints(); !strings.Contains(hints, "enter search") || !strings.Contains(hints, "esc cancel") {
		t.Fatalf("search hints = %q", hints)
	}
	model, _ = model.Update(special(tea.KeyEscape))
	if model.searchFocused {
		t.Fatal("escape did not cancel search focus")
	}
	_, command := model.Update(textKey("q"))
	if command == nil {
		t.Fatal("q did not produce a quit command")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatalf("quit command returned %T", command())
	}

	want := errors.New("load failed")
	model, _ = model.Update(loadFailedMsg{err: want})
	if !errors.Is(model.err, want) || model.loading {
		t.Fatalf("failed async state = %#v", model)
	}
}

func TestModelExactResultFocusSurvivesResize(t *testing.T) {
	records := []history.Record{
		{ID: "artifact:1", Kind: "artifact", UnitID: "wu", Revision: 1, Content: "first"},
		{ID: "artifact:2", Kind: "artifact", UnitID: "wu", Revision: 1, Content: strings.Repeat("界", 80)},
	}
	for _, target := range records {
		resolution := history.Resolution{Detail: history.Detail{Records: records}, Record: target}
		model, _ := (Model{}).Update(detailLoadedMsg{resolution: resolution})
		if model.detail.recordID != target.ID || model.detail.line != 0 {
			t.Fatalf("exact result %q focus = %#v", target.ID, model.detail)
		}
	}
	resolution := history.Resolution{Detail: history.Detail{Records: records}, Record: records[1]}
	model, _ := (Model{}).Update(detailLoadedMsg{resolution: resolution})
	for _, size := range []tea.WindowSizeMsg{{Width: 112, Height: 28}, {Width: 60, Height: 16}, {Width: 112, Height: 28}} {
		model, _ = model.Update(size)
		current, total := model.detailPosition()
		if model.detail.recordID != "artifact:2" || current < 1 || current > total {
			t.Fatalf("resize %dx%d lost exact focus: detail=%#v position=%d/%d", size.Width, size.Height, model.detail, current, total)
		}
	}
}

func TestModelDetailEvidenceArrowAndVimParityAndClamps(t *testing.T) {
	base := Model{screen: DetailScreen, width: 60, height: 16, opened: history.Resolution{Detail: history.Detail{Records: []history.Record{
		{ID: "one", Kind: "evidence", Content: strings.Repeat("alpha ", 24)},
		{ID: "two", Kind: "review", Content: "omega"},
	}}}}
	base.reconcileDetail()
	for _, pair := range [][2]tea.KeyPressMsg{{special(tea.KeyDown), textKey("j")}, {special(tea.KeyUp), textKey("k")}} {
		arrow, _ := base.Update(pair[0])
		vim, _ := base.Update(pair[1])
		if !reflect.DeepEqual(arrow.detail, vim.detail) {
			t.Fatalf("detail navigation differs: arrow=%#v vim=%#v", arrow.detail, vim.detail)
		}
		base = arrow
	}
	for range 100 {
		base, _ = base.Update(textKey("k"))
	}
	if current, _ := base.detailPosition(); current != 1 {
		t.Fatalf("upper clamp position = %d", current)
	}
	for range 100 {
		base, _ = base.Update(textKey("j"))
	}
	if current, total := base.detailPosition(); current != total || base.detail.recordID != "two" {
		t.Fatalf("lower clamp = %d/%d, record=%q", current, total, base.detail.recordID)
	}
}

type fakeLoader struct {
	detail               history.Detail
	workflows            []history.Workflow
	results              []history.SearchResult
	resolution           history.Resolution
	occurrenceResolution history.Resolution
	occurrenceIDs        *[]string
	occurrenceRecordIDs  *[]string
}

func (f fakeLoader) List(context.Context) ([]history.Workflow, error)       { return f.workflows, nil }
func (f fakeLoader) Detail(context.Context, string) (history.Detail, error) { return f.detail, nil }
func (f fakeLoader) Search(context.Context, string) ([]history.SearchResult, error) {
	return f.results, nil
}

func TestModelRefreshLatestGenerationAndWorkflowIdentity(t *testing.T) {
	model := New(fakeLoader{workflows: []history.Workflow{{ID: "old"}, {ID: "kept"}, {ID: "new"}}})
	model.screen = WorkflowsScreen
	model.workflows = []history.Workflow{{ID: "first"}, {ID: "kept"}}
	model.selected = 1
	first, firstCmd := model.Update(textKey("r"))
	second, secondCmd := first.Update(textKey("r"))
	check(t, firstCmd != nil && secondCmd != nil && second.loading, "refresh commands = first:%v second:%v loading=%v", firstCmd != nil, secondCmd != nil, second.loading)
	stale := firstCmd().(workflowsLoadedMsg)
	stale.workflows = []history.Workflow{{ID: "stale"}}
	secondMsg := secondCmd()
	second, _ = second.Update(secondMsg)
	second, _ = second.Update(stale)
	duplicate := secondMsg.(workflowsLoadedMsg)
	duplicate.workflows = []history.Workflow{{ID: "duplicate"}}
	second, _ = second.Update(duplicate)
	check(t, len(second.workflows) == 3 && second.workflows[second.selected].ID == "kept" && !second.loading && second.err == nil, "latest refresh state = %#v", second)
	second.workflows = []history.Workflow{{ID: "gone"}, {ID: "also-gone"}}
	second.selected = 1
	second = runRefresh(t, second)
	check(t, second.selected == 1 && second.workflows[1].ID == "kept", "deterministic fallback = selected %d in %#v", second.selected, second.workflows)
	for _, refresh := range []bool{false, true} {
		model := New(fakeLoader{workflows: []history.Workflow{{ID: "loaded"}}})
		model.screen = WorkflowsScreen
		command := model.Init()
		if refresh {
			model.workflows = []history.Workflow{{ID: "existing"}}
			model, command = model.Update(textKey("r"))
		}
		model, _ = model.Update(special(tea.KeyEscape))
		model, _ = model.Update(command())
		check(t, len(model.workflows) == 1 && model.workflows[0].ID == "loaded", "root load completion lost (refresh=%v): %#v", refresh, model)
	}
}
func TestModelRefreshPreservesResultAndDetailIdentity(t *testing.T) {
	kept := history.SearchResult{WorkflowID: "wf", RecordID: "record:2", Kind: "plan", UnitID: "u2", Revision: 3}
	model := New(fakeLoader{results: []history.SearchResult{{WorkflowID: "new", RecordID: "record:0"}, kept}})
	model.screen, model.query = ResultsScreen, "plan"
	model.results, model.selected = []history.SearchResult{{WorkflowID: "old", RecordID: "record:1"}, kept}, 1
	model = runRefresh(t, model)
	check(t, reflect.DeepEqual(model.results[model.selected], kept), "selected result = %#v, want %#v", model.results[model.selected], kept)
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Synopsis: history.Synopsis{Progress: &history.Progress{Status: "advanced", Summary: "new", NextAction: "review"}}, Occurrences: []history.Occurrence{{ID: "before"}, {ID: "activity:2"}, {ID: "after"}}}
	model = Model{loader: fakeLoader{detail: detail}, screen: DetailScreen, generation: 1}
	model.opened = history.Resolution{Detail: history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{{ID: "activity:1"}, {ID: "activity:2"}}}}
	model.detail.occurrenceID = "activity:2"
	model = runRefresh(t, model)
	check(t, model.detail.occurrenceID == "activity:2" && model.opened.Detail.Synopsis.Progress.Summary == "new", "refreshed progress/focus = detail:%#v synopsis:%#v", model.detail, model.opened.Detail.Synopsis)
	model.loader = fakeLoader{detail: history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{{ID: "fallback"}}}}
	model = runRefresh(t, model)
	check(t, model.detail.occurrenceID == "fallback", "missing occurrence fallback = %#v", model.detail)
	model = Model{loader: fakeLoader{detail: history.Detail{Workflow: history.Workflow{ID: "wf"}, Records: []history.Record{{ID: "record:1"}, {ID: "record:2", WorkflowID: "wf", Kind: "plan", Content: "updated"}}}}, screen: DetailScreen, generation: 1}
	model.opened, model.detail = history.Resolution{Detail: history.Detail{Workflow: history.Workflow{ID: "wf"}}, Record: history.Record{ID: "record:2", WorkflowID: "wf", Content: "old"}}, detailCursor{recordID: "record:2", line: 1}
	model = runRefresh(t, model)
	check(t, model.opened.Record.Content == "updated" && model.detail.recordID == "record:2", "record identity/content = record %#v cursor %#v", model.opened.Record, model.detail)
}
func TestModelRefreshErrorRetainsDataFocusAndSearchOwnsR(t *testing.T) {
	wantErr := errors.New("refresh failed")
	model := Model{screen: WorkflowsScreen, workflows: []history.Workflow{{ID: "one", Name: "First"}, {ID: "two", Name: "Second"}}, selected: 1, width: 100, height: 24, generation: 4, loading: true, loadPreserves: true}
	model, _ = model.Update(loadFailedMsg{loadMeta: loadMeta{kind: loadUnknown, generation: 4}, err: wantErr})
	check(t, len(model.workflows) == 2 && model.selected == 1 && errors.Is(model.err, wantErr), "non-destructive failure = %#v", model)
	check(t, strings.Contains(model.render(), "REFRESH FAILED") && strings.Contains(model.render(), "Second"), "refresh failure not visible with prior data:\n%s", model.render())
	model, _ = model.Update(textKey("/"))
	model.search.SetValue("sea")
	model, command := model.Update(textKey("r"))
	check(t, model.query == "sear" && !model.loading, "focused search refresh key = query %q command=%v loading=%v", model.query, command != nil, model.loading)
	model, command = NewUnavailable(errors.New("history unavailable")).Update(textKey("r"))
	check(t, command == nil && model.err.Error() == "history unavailable" && !model.loading, "unavailable refresh = command:%v error:%v loading:%v", command != nil, model.err, model.loading)
}
func TestModelRefreshCompletionCannotRestoreViewAfterBack(t *testing.T) {
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{{ID: "activity:1"}}}
	result := history.SearchResult{WorkflowID: "wf", RecordID: "record:1", Kind: "plan"}
	assertBackIgnoresRefresh(t, "detail", Model{loader: fakeLoader{detail: detail}, screen: DetailScreen, opened: history.Resolution{Detail: detail}, generation: 1})
	assertBackIgnoresRefresh(t, "results", Model{loader: fakeLoader{results: []history.SearchResult{result}}, screen: ResultsScreen, results: []history.SearchResult{result}, query: "plan", generation: 1})
}
func assertBackIgnoresRefresh(t *testing.T, name string, model Model) {
	t.Helper()
	loading, command := model.Update(textKey("r"))
	check(t, command != nil, "%s refresh command is nil", name)
	meta := loadMeta{kind: loading.activeLoad, generation: loading.generation, preserve: loading.loadPreserves}
	for _, msg := range []tea.Msg{command(), loadFailedMsg{loadMeta: meta, err: errors.New("late failure")}} {
		navigated, _ := loading.Update(special(tea.KeyEscape))
		wantScreen, wantOpened, wantResults, wantWorkflows, wantSelected, wantErr := navigated.screen, navigated.opened, navigated.results, navigated.workflows, navigated.selected, navigated.err
		navigated, _ = navigated.Update(msg)
		check(t, navigated.screen == wantScreen && reflect.DeepEqual(navigated.opened, wantOpened) && reflect.DeepEqual(navigated.results, wantResults) && reflect.DeepEqual(navigated.workflows, wantWorkflows) && navigated.selected == wantSelected && errors.Is(navigated.err, wantErr), "%s late %T altered navigated view", name, msg)
	}
}
func runRefresh(t *testing.T, model Model) Model {
	t.Helper()
	model, command := model.Update(textKey("r"))
	check(t, command != nil, "refresh command is nil")
	model, _ = model.Update(command())
	return model
}
func check(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}
func (f fakeLoader) Resolve(context.Context, history.SearchResult) (history.Resolution, error) {
	return f.resolution, nil
}
func (f fakeLoader) ResolveActivity(context.Context, history.Activity) (history.Resolution, error) {
	return history.Resolution{}, nil
}
func (f fakeLoader) ResolveOccurrence(_ context.Context, _, occurrenceID, recordID string) (history.Resolution, error) {
	if f.occurrenceIDs != nil {
		*f.occurrenceIDs = append(*f.occurrenceIDs, occurrenceID)
	}
	if f.occurrenceRecordIDs != nil {
		*f.occurrenceRecordIDs = append(*f.occurrenceRecordIDs, recordID)
	}
	resolution := f.occurrenceResolution
	for _, record := range resolution.Detail.Records {
		if record.ID == recordID {
			resolution.Record = record
			break
		}
	}
	return resolution, nil
}

func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }
func textKey(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: []rune(text)[0], Text: text})
}
