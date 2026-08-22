package tui

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/fmazzalomo/pitcrew/internal/history"
)

var updateGoldens = flag.Bool("update", false, "update TUI golden files")

func TestViewRepresentativeLayouts(t *testing.T) {
	for _, test := range []struct {
		name   string
		model  Model
		width  int
		height int
	}{
		{"wide", detailViewModel(), 112, 28},
		{"narrow", workflowViewModel(), 72, 24},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _ := test.model.Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			got := model.View().Content
			for _, line := range strings.Split(got, "\n") {
				if width := lipgloss.Width(line); width > test.width {
					t.Fatalf("rendered line width %d exceeds terminal width %d", width, test.width)
				}
			}
			assertGolden(t, test.name, got)
		})
	}
}

func TestViewStatesAndResize(t *testing.T) {
	tests := []struct {
		name  string
		model Model
		want  []string
	}{
		{"uninitialized", NewUnavailable(ErrUninitialized), []string{"No PitCrew repository is initialized for this project.", "q quit"}},
		{"empty", Model{}, []string{"No workflow history yet.", "q quit"}},
		{"no results", Model{screen: ResultsScreen, query: "missing"}, []string{`No results for "missing".`, "query: missing"}},
		{"error", NewUnavailable(errors.New("database is locked")), []string{"Could not read PitCrew history.", "database is locked", "q quit"}},
		{"search input", Model{workflows: workflowViewModel().workflows, searchFocused: true, query: "review"}, []string{"SEARCH ›", "review█", "enter search"}},
		{"minimum", workflowViewModel(), []string{"Terminal too small", "60×16", "q quit"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height := 72, 24
			if test.name == "minimum" {
				width, height = 42, 10
			}
			model, _ := test.model.Update(tea.WindowSizeMsg{Width: width, Height: height})
			got := model.View().Content
			for _, identity := range []string{"PitCrew2", "Control Plane", "v0.3.0"} {
				if !strings.Contains(got, identity) {
					t.Fatalf("view missing identity %q:\n%s", identity, got)
				}
			}
			if !strings.Contains(got, flight.version.Render("v0.3.0")) {
				t.Fatalf("view lacks version accent:\n%s", got)
			}
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Fatalf("view missing %q:\n%s", want, got)
				}
			}
		})
	}

	model := workflowViewModel()
	model.selected = 1
	wide, _ := model.Update(tea.WindowSizeMsg{Width: 112, Height: 28})
	narrow, _ := wide.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
	if wide.selected != 1 || narrow.selected != 1 || !strings.Contains(wide.View().Content, "MULTI PANE") || !strings.Contains(narrow.View().Content, "SINGLE PANE") {
		t.Fatalf("resize lost focus or layout: wide=%d narrow=%d", wide.selected, narrow.selected)
	}
}

func TestViewGridMetadataTimelineAndVersionAccent(t *testing.T) {
	grid, _ := workflowViewModel().Update(tea.WindowSizeMsg{Width: 112, Height: 28})
	for _, want := range []string{"Started", "Work", "Status", "2026-08-22 03:17", "Redesign TUI history"} {
		if !strings.Contains(grid.View().Content, want) {
			t.Fatalf("grid missing %q:\n%s", want, grid.View().Content)
		}
	}
	if !strings.Contains(grid.View().Content, flight.version.Render("v0.3.0")) {
		t.Fatalf("version lacks dedicated accent:\n%s", grid.View().Content)
	}
	detail, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: 112, Height: 28})
	for _, want := range []string{"Redesign TUI history", "Created", "Updated", "Goal", "State", "ACTIVITY", "pc2-specifier", "Recorded specification", "Scenario: exact Gherkin"} {
		if !strings.Contains(detail.View().Content, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail.View().Content)
		}
	}
}

func TestViewShowsResolvedWorkflowResultOutsideStoredRecords(t *testing.T) {
	model := Model{screen: DetailScreen, opened: history.Resolution{
		Detail: history.Detail{Workflow: history.Workflow{ID: "wf", Name: "Exact workflow", Goal: "Inspect the linked aggregate"}},
		Record: history.Record{ID: "workflow:wf", Kind: "workflow", Title: "Exact workflow", Content: "Exact linked workflow result"},
	}}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
	if got := model.View().Content; !strings.Contains(got, "RESULTS  workflow") || !strings.Contains(got, "Exact linked workflow result") {
		t.Fatalf("resolved workflow result is not inspectable:\n%s", got)
	}
}

func TestViewMarksDerivedHistoricalName(t *testing.T) {
	model := Model{workflows: []history.Workflow{{Name: "Legacy goal", NameDerived: true, CreatedAt: "2026-08-20T12:00:00Z"}}}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
	if got := model.View().Content; !strings.Contains(got, "Legacy goal [derived]") {
		t.Fatalf("derived name is not marked:\n%s", got)
	}
}

func TestViewLargeWordmarkAcrossLayouts(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 112, Height: 28}, {Width: 60, Height: 16}, {Width: 42, Height: 10}} {
		model, _ := workflowViewModel().Update(size)
		got := model.View().Content
		for _, row := range largeWordmark {
			if !strings.Contains(got, row) {
				t.Fatalf("%dx%d missing large wordmark row %q:\n%s", size.Width, size.Height, row, got)
			}
		}
		if !strings.Contains(got, "PitCrew2") || !strings.Contains(got, "Control Plane") || !strings.Contains(got, flight.version.Render("v0.3.0")) {
			t.Fatalf("%dx%d missing accessible identity or version accent:\n%s", size.Width, size.Height, got)
		}
	}
}

func TestViewNarrowDetailIsHeightBounded(t *testing.T) {
	model := detailViewModel()
	model.opened.Detail.Workflow.Goal = strings.Repeat("bounded goal ", 30)
	model.opened.Detail.Records[0].Content = strings.Repeat("bounded evidence ", 80)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	if got := model.View().Content; len(strings.Split(got, "\n")) > 16 {
		t.Fatalf("60x16 detail rendered %d lines:\n%s", len(strings.Split(got, "\n")), got)
	}
}

func TestViewDetailEvidenceIsFullyReachable(t *testing.T) {
	content := "FIRST FRAGMENT\n" + strings.Repeat("界", 120) + "\nMIDDLE FRAGMENT\n" + strings.Repeat("界", 120) + "\nFINAL FRAGMENT"
	model := Model{screen: DetailScreen, opened: history.Resolution{Detail: history.Detail{
		Workflow: history.Workflow{ID: "wf", State: "implementation", Goal: "Inspect all evidence"},
		Records:  []history.Record{{ID: "evidence:1", Kind: "evidence", Content: content}},
	}}}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	seenFirst, seenMiddle, seenFinal := false, false, false
	for {
		view := model.View().Content
		seenFirst = seenFirst || strings.Contains(view, "FIRST FRAGMENT")
		seenMiddle = seenMiddle || strings.Contains(view, "MIDDLE FRAGMENT")
		seenFinal = seenFinal || strings.Contains(view, "FINAL FRAGMENT")
		if strings.Count(view, "▶") != 1 {
			t.Fatalf("focus marker count = %d:\n%s", strings.Count(view, "▶"), view)
		}
		for _, line := range strings.Split(view, "\n") {
			if width := lipgloss.Width(line); width > 60 {
				t.Fatalf("rendered line width %d exceeds 60: %q", width, line)
			}
		}
		current, total := model.detailPosition()
		if current == total {
			if !strings.Contains(view, "line "+strconv.Itoa(total)+"/"+strconv.Itoa(total)) {
				t.Fatalf("final position hint missing:\n%s", view)
			}
			break
		}
		model, _ = model.Update(textKey("j"))
	}
	if !seenFirst || !seenMiddle || !seenFinal {
		t.Fatalf("reachable fragments: first=%v middle=%v final=%v", seenFirst, seenMiddle, seenFinal)
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update)", err)
	}
	if got != string(want) {
		t.Fatalf("view differs from %s\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func workflowViewModel() Model {
	return Model{workflows: []history.Workflow{
		{ID: "wf-alpha", Name: "Redesign TUI history", Revision: 7, State: "implementing", Goal: "Ship read-only terminal inspection", CreatedAt: "2026-08-22T03:17:38Z", UpdatedAt: "2026-08-22T03:18:00Z"},
		{ID: "wf-beta", Name: "Rename Daimon", Revision: 3, State: "completed", Goal: "Rename the coordination role", CreatedAt: "2026-08-20T12:00:00Z", UpdatedAt: "2026-08-20T13:00:00Z"},
	}}
}

func detailViewModel() Model {
	return Model{screen: DetailScreen, opened: history.Resolution{Detail: history.Detail{
		Workflow: history.Workflow{ID: "wf-alpha", Name: "Redesign TUI history", Revision: 7, State: "implementing", Goal: "Ship read-only terminal inspection", CreatedAt: "2026-08-22T03:17:38Z", UpdatedAt: "2026-08-22T03:18:00Z"},
		Timeline: []history.Activity{{ID: "activity:7", WorkflowID: "wf-alpha", Action: "specification_recorded", Actor: "pc2-specifier", At: "2026-08-22T03:18:00Z", RecordID: "artifact:1"}},
		Records: []history.Record{
			{ID: "artifact:1", Kind: "specification", Title: "Specification", Content: "Scenario: exact Gherkin", Revision: 2, At: "2026-08-22T03:18:00Z"},
			{ID: "review:1", Kind: "review", Title: "approved", Content: "Store access preserves logical state.", UnitID: "wu-store", Revision: 1, At: "2026-08-21T12:30:00Z"},
		},
	}}}
}
