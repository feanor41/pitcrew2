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
		{"down", special(tea.KeyDown), textKey("j"), Model{workflows: workflows}, WorkflowsScreen, 1},
		{"up", special(tea.KeyUp), textKey("k"), Model{workflows: workflows, selected: 2}, WorkflowsScreen, 1},
		{"back", special(tea.KeyLeft), textKey("h"), Model{screen: DetailScreen, selected: 2}, WorkflowsScreen, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, _ := test.start.Update(test.first)
			second, _ := test.start.Update(test.second)
			if first.screen != test.wantScreen || first.selected != test.wantIndex || !reflect.DeepEqual(first, second) {
				t.Fatalf("arrow=%#v vim=%#v; want screen=%v selected=%d", first, second, test.wantScreen, test.wantIndex)
			}
		})
	}

	loader := fakeLoader{detail: history.Detail{Workflow: workflows[0]}}
	for _, key := range []tea.KeyPressMsg{special(tea.KeyRight), textKey("l"), special(tea.KeyEnter)} {
		model := New(loader)
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

func TestModelSearchAcceptsVimTextAndOpensExactResult(t *testing.T) {
	result := history.SearchResult{WorkflowID: "wf", RecordID: "artifact:42", Kind: "proposal"}
	resolution := history.Resolution{Record: history.Record{ID: "artifact:42", Content: "exact"}}
	model := New(fakeLoader{results: []history.SearchResult{result}, resolution: resolution})
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

func TestModelVersionAndOccurrenceDrillDown(t *testing.T) {
	record := history.Record{ID: "artifact:7", Kind: "specification", Content: "Scenario: exact Gherkin"}
	occurrence := history.Occurrence{ID: "activity:7", RecordID: record.ID, Work: "Specification"}
	want := history.Resolution{Detail: history.Detail{Occurrences: []history.Occurrence{occurrence}, Records: []history.Record{record}}, Record: record}
	model := New(fakeLoader{occurrenceResolution: want})
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: want.Detail}})
	if model.Version() != "0.5.0" {
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
	if model.detail.occurrenceID != "activity:8" {
		t.Fatalf("page down focus = %#v", model.detail)
	}
	model, _ = model.Update(special(tea.KeyPgUp))
	if model.detail.occurrenceID != "activity:0" {
		t.Fatalf("page up focus = %#v", model.detail)
	}
	model, _ = model.Update(special(tea.KeyEnd))
	if model.detail.occurrenceID != "activity:9" {
		t.Fatalf("end operational focus = %#v", model.detail)
	}
	for range 20 {
		model, _ = model.Update(textKey("j"))
	}
	if model.detail.occurrenceID != "activity:11" {
		t.Fatalf("lower clamp = %#v", model.detail)
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
}

func (f fakeLoader) List(context.Context) ([]history.Workflow, error)       { return f.workflows, nil }
func (f fakeLoader) Detail(context.Context, string) (history.Detail, error) { return f.detail, nil }
func (f fakeLoader) Search(context.Context, string) ([]history.SearchResult, error) {
	return f.results, nil
}

func TestModelRefreshLatestGenerationAndWorkflowIdentity(t *testing.T) {
	model := New(fakeLoader{workflows: []history.Workflow{{ID: "old"}, {ID: "kept"}, {ID: "new"}}})
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
	detail := history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{{ID: "before"}, {ID: "activity:2"}, {ID: "after"}}}
	model = Model{loader: fakeLoader{detail: detail}, screen: DetailScreen, generation: 1}
	model.opened = history.Resolution{Detail: history.Detail{Workflow: history.Workflow{ID: "wf"}, Occurrences: []history.Occurrence{{ID: "activity:1"}, {ID: "activity:2"}}}}
	model.detail.occurrenceID = "activity:2"
	model = runRefresh(t, model)
	check(t, model.detail.occurrenceID == "activity:2", "occurrence focus = %#v", model.detail)
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
	model.searchFocused, model.query = true, "sea"
	model, command := model.Update(textKey("r"))
	check(t, command == nil && model.query == "sear" && !model.loading, "focused search refresh key = query %q command=%v loading=%v", model.query, command != nil, model.loading)
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
		want := navigated
		navigated, _ = navigated.Update(msg)
		check(t, reflect.DeepEqual(navigated, want), "%s late %T altered navigated view:\n got %#v\nwant %#v", name, msg, navigated, want)
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
func (f fakeLoader) ResolveOccurrence(_ context.Context, _, occurrenceID, _ string) (history.Resolution, error) {
	if f.occurrenceIDs != nil {
		*f.occurrenceIDs = append(*f.occurrenceIDs, occurrenceID)
	}
	return f.occurrenceResolution, nil
}

func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }
func textKey(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: []rune(text)[0], Text: text})
}
