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
		{ID: "wf-alpha", Revision: 7, State: "implementation", Goal: "Ship read-only terminal inspection", UpdatedAt: "2026-08-21T18:00:00Z"},
		{ID: "wf-beta", Revision: 3, State: "completed", Goal: "Rename the coordination role", UpdatedAt: "2026-08-20T12:00:00Z"},
	}}
}

func detailViewModel() Model {
	return Model{screen: DetailScreen, opened: history.Resolution{Detail: history.Detail{
		Workflow: history.Workflow{ID: "wf-alpha", Revision: 7, State: "implementation", Goal: "Ship read-only terminal inspection"},
		Records: []history.Record{
			{ID: "artifact:1", Kind: "proposal", Title: "Proposal", Content: "Embed a read-only TUI in the PitCrew CLI.", Revision: 2, At: "2026-08-21T10:00:00Z"},
			{ID: "review:1", Kind: "review", Title: "approved", Content: "Store access preserves logical state.", UnitID: "wu-store", Revision: 1, At: "2026-08-21T12:30:00Z"},
		},
	}}}
}
