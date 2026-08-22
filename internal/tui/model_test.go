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
	if model.Version() != "0.3.0" {
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
	results              []history.SearchResult
	resolution           history.Resolution
	occurrenceResolution history.Resolution
	occurrenceIDs        *[]string
}

func (f fakeLoader) List(context.Context) ([]history.Workflow, error)       { return nil, nil }
func (f fakeLoader) Detail(context.Context, string) (history.Detail, error) { return f.detail, nil }
func (f fakeLoader) Search(context.Context, string) ([]history.SearchResult, error) {
	return f.results, nil
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
