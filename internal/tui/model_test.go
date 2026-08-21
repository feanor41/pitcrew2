package tui

import (
	"context"
	"errors"
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
	detail     history.Detail
	results    []history.SearchResult
	resolution history.Resolution
}

func (f fakeLoader) List(context.Context) ([]history.Workflow, error)       { return nil, nil }
func (f fakeLoader) Detail(context.Context, string) (history.Detail, error) { return f.detail, nil }
func (f fakeLoader) Search(context.Context, string) ([]history.SearchResult, error) {
	return f.results, nil
}
func (f fakeLoader) Resolve(context.Context, history.SearchResult) (history.Resolution, error) {
	return f.resolution, nil
}

func special(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }
func textKey(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: []rune(text)[0], Text: text})
}
