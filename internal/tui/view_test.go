package tui

import (
	"errors"
	"flag"
	"fmt"
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
		{"wide", detailViewModel(), 166, 30},
		{"canonical", detailViewModel(), 120, 28},
		{"without-actor", detailViewModel(), 96, 26},
		{"compact", detailViewModel(), 80, 24},
		{"minimum-detail", detailViewModel(), 60, 16},
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
			for _, identity := range []string{"PitCrew2", "Control Plane", "v0.4.0"} {
				if !strings.Contains(got, identity) {
					t.Fatalf("view missing identity %q:\n%s", identity, got)
				}
			}
			if !strings.Contains(got, flight.version.Render("v0.4.0")) {
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
	wide, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 28})
	narrow, _ := wide.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
	if wide.selected != 1 || narrow.selected != 1 {
		t.Fatalf("resize lost focus or layout: wide=%d narrow=%d", wide.selected, narrow.selected)
	}
}

func TestViewDetailFootersAdvertiseRefresh(t *testing.T) {
	occurrence := detailViewModel()
	evidence := detailViewModel()
	evidence.opened.Record = evidence.opened.Detail.Records[0]
	for _, test := range []struct {
		name string
		Model
	}{{"occurrence", occurrence}, {"evidence", evidence}} {
		for _, width := range []int{60, 120} {
			t.Run(fmt.Sprintf("%s/%d", test.name, width), func(t *testing.T) {
				model, _ := test.Model.Update(tea.WindowSizeMsg{Width: width, Height: 16})
				footer := model.footerHints()
				check(t, strings.Contains(footer, "r refresh") && strings.Contains(footer, "q quit"), "detail footer = %q", footer)
				used := lipgloss.Width(fmt.Sprintf("%s  ·  %dx%d", footer, width, 16))
				check(t, used <= width, "detail footer width %d exceeds %d: %q", used, width, footer)
			})
		}
	}
}

func TestViewGridMetadataTimelineAndVersionAccent(t *testing.T) {
	grid, _ := workflowViewModel().Update(tea.WindowSizeMsg{Width: 112, Height: 28})
	for _, want := range []string{"Started", "Work", "Status", "2026-08-22 03:17", "Redesign TUI history"} {
		if !strings.Contains(grid.View().Content, want) {
			t.Fatalf("grid missing %q:\n%s", want, grid.View().Content)
		}
	}
	if !strings.Contains(grid.View().Content, flight.version.Render("v0.4.0")) {
		t.Fatalf("version lacks dedicated accent:\n%s", grid.View().Content)
	}
	detail, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: 120, Height: 28})
	for _, want := range []string{"Redesign TUI history", "IMPLEMENTING", "r7", "Goal", "2/3 done", "Current", "Next", "HISTORY", "When", "Phase", "Work", "Try", "Activity", "Actor", "Outcome / reason", "pc2-specifier", "Recorded specification"} {
		if !strings.Contains(detail.View().Content, want) {
			t.Fatalf("detail missing %q:\n%s", want, detail.View().Content)
		}
	}
	if got := detail.View().Content; strings.Contains(got, "RESULTS") || strings.Contains(got, "MULTI PANE") || strings.Contains(got, "SINGLE PANE") {
		t.Fatalf("detail retained storage-oriented or layout-mode copy:\n%s", got)
	}
}

func TestViewShowsResolvedWorkflowResultOutsideStoredRecords(t *testing.T) {
	model := Model{screen: DetailScreen, opened: history.Resolution{
		Detail: history.Detail{Workflow: history.Workflow{ID: "wf", Name: "Exact workflow", Goal: "Inspect the linked aggregate"}},
		Record: history.Record{ID: "workflow:wf", Kind: "workflow", Title: "Exact workflow", Content: "Exact linked workflow result"},
	}}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
	if got := model.View().Content; !strings.Contains(got, "EVIDENCE") || !strings.Contains(got, "Exact linked workflow result") {
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

func TestViewCompactIdentityAcrossLayouts(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 112, Height: 28}, {Width: 60, Height: 16}, {Width: 42, Height: 10}} {
		model, _ := workflowViewModel().Update(size)
		got := model.View().Content
		if !strings.Contains(got, "PitCrew2") || !strings.Contains(got, "Control Plane") || !strings.Contains(got, flight.version.Render("v0.4.0")) {
			t.Fatalf("%dx%d missing accessible identity or version accent:\n%s", size.Width, size.Height, got)
		}
		if strings.Contains(got, "╔═╗") {
			t.Fatalf("%dx%d retained oversized wordmark:\n%s", size.Width, size.Height, got)
		}
	}
}

func TestViewOperationalGridBreakpoints(t *testing.T) {
	for _, test := range []struct {
		name        string
		width       int
		wantActor   bool
		wantTwoLine bool
	}{
		{"wide", 166, true, false},
		{"canonical", 120, true, false},
		{"without actor", 96, false, false},
		{"compact", 80, false, false},
		{"two line", 60, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: test.width, Height: 28})
			got := model.View().Content
			if strings.Contains(got, "RESULTS") || strings.Contains(got, "ACTIVITY  ") {
				t.Fatalf("duplicated streams at %d columns:\n%s", test.width, got)
			}
			actor := strings.Contains(got, "pc2-specifier")
			if actor != test.wantActor {
				t.Fatalf("actor visibility at %d = %v, want %v:\n%s", test.width, actor, test.wantActor, got)
			}
			if test.wantTwoLine && !strings.Contains(got, "  ↳ ") {
				t.Fatalf("60-column row is not rendered semantically across two lines:\n%s", got)
			}
			for _, line := range strings.Split(got, "\n") {
				if width := lipgloss.Width(line); width > test.width {
					t.Fatalf("rendered line width %d exceeds %d: %q", width, test.width, line)
				}
			}
		})
	}
}

func TestViewWorkflowListKeepsEveryStatusWithinSixtyCells(t *testing.T) {
	states := []string{"draft", "exploring", "specifying", "designing", "planning", "plan_approved", "implementing", "ready_to_complete", "completed", "abandoned"}
	model := Model{}
	for i, state := range states {
		model.workflows = append(model.workflows, history.Workflow{
			ID: "wf-" + state, Name: "Critical workflow identity " + state, State: state,
			CreatedAt: "2026-08-22T03:" + fmt.Sprintf("%02d", i) + ":00Z",
		})
	}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	got := model.View().Content
	if !strings.Contains(got, "[READY TO COMPLETE]") {
		t.Fatalf("critical ready-to-complete identity was clipped:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if width := lipgloss.Width(line); width > 60 {
			t.Fatalf("workflow list line is %d cells at 60 columns: %q", width, line)
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

func TestViewMinimumDetailShowsMultipleUnicodeOccurrences(t *testing.T) {
	model := detailViewModel()
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	got := model.View().Content
	if len(strings.Split(got, "\n")) > 16 {
		t.Fatalf("60x16 detail rendered too many lines:\n%s", got)
	}
	if strings.Count(got, "  ↳ ") < 2 {
		t.Fatalf("60x16 detail does not expose multiple semantic occurrences:\n%s", got)
	}
	if !strings.Contains(got, "diseño") {
		t.Fatalf("Unicode occurrence missing:\n%s", got)
	}
}

func TestViewProgressiveDetailShowsTypedMetadataBelowActorBreakpoint(t *testing.T) {
	model := detailViewModel()
	model.detail.occurrenceID = "activity:3"
	model.opened.Record = history.Record{ID: "evidence:1", Kind: "evidence", UnitID: "wu-view", Revision: 1, Actor: "pc2-implementer", Title: "Validation", Content: "Focused tests passed", At: "2026-08-22T03:19:00Z"}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	got := model.View().Content
	for _, want := range []string{"Actor pc2-implementer", "Record evidence:1", "Unit wu-view", "Revision 1", "Try 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("progressive detail missing %q below actor breakpoint:\n%s", want, got)
		}
	}
}

func TestViewProgressiveDetailMarksLegacyOccurrence(t *testing.T) {
	model := detailViewModel()
	model.opened.Detail.Occurrences[0].Legacy = true
	model.detail.occurrenceID = "activity:1"
	model.opened.Record = history.Record{ID: "workflow:wf-alpha", Kind: "workflow", Title: "Legacy workflow", Content: "Known durable content", Actor: "daimon"}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	if got := model.View().Content; !strings.Contains(got, flight.muted.Render("[legacy]")) {
		t.Fatalf("legacy occurrence marker missing or not subdued:\n%s", got)
	}
}

func TestViewSynopsisMarksDerivedCurrentAndBlockerAtMinimum(t *testing.T) {
	model := detailViewModel()
	model.opened.Detail.Synopsis.Current.Derived = true
	model.opened.Detail.Synopsis.Blocker = &history.UnitStatus{Description: "Render diseño operativo", Status: "Correction", Reason: "Review correction required", Derived: true}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	got := model.View().Content
	for _, want := range []string{"Current [derived]", "Blocked [derived]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("minimum synopsis missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "  ↳ ") < 2 {
		t.Fatalf("derived synopsis consumed the minimum occurrence viewport:\n%s", got)
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
	attempt := int64(1)
	return Model{screen: DetailScreen, opened: history.Resolution{Detail: history.Detail{
		Workflow: history.Workflow{ID: "wf-alpha", Name: "Redesign TUI history", Revision: 7, State: "implementing", Goal: "Ship read-only terminal inspection", CreatedAt: "2026-08-22T03:17:38Z", UpdatedAt: "2026-08-22T03:18:00Z"},
		Synopsis: history.Synopsis{
			Total: 3, Done: 2, Reviewing: 1, NextAction: "workflow list-ready-units",
			Current: &history.UnitStatus{ID: "wu-view", Description: "Render diseño operativo", Status: "Reviewing", Attempt: 1, Derived: true},
		},
		Occurrences: []history.Occurrence{
			{ID: "activity:1", RecordID: "workflow:wf-alpha", At: "2026-08-22T03:17:38Z", Phase: "Explore", Work: "Redesign TUI history", Activity: "workflow_created", Actor: "daimon", Outcome: "Created workflow"},
			{ID: "activity:2", RecordID: "artifact:1", At: "2026-08-22T03:18:00Z", Phase: "Specify", Work: "Diseño operativo", Activity: "specification_recorded", Actor: "pc2-specifier", Outcome: "Recorded specification"},
			{ID: "activity:3", RecordID: "evidence:1", At: "2026-08-22T03:19:00Z", Phase: "Implement", Work: "Render diseño operativo", Attempt: &attempt, Activity: "unit_tdd_recorded", Actor: "pc2-implementer", Outcome: "Validation recorded"},
			{ID: "activity:4", RecordID: "review:1", At: "2026-08-22T03:20:00Z", Phase: "Review", Work: "Render diseño operativo", Attempt: &attempt, Activity: "unit_review_recorded", Actor: "pc2-reviewer", Outcome: "Approved"},
		},
		Timeline: []history.Activity{{ID: "activity:7", WorkflowID: "wf-alpha", Action: "specification_recorded", Actor: "pc2-specifier", At: "2026-08-22T03:18:00Z", RecordID: "artifact:1"}},
		Records: []history.Record{
			{ID: "artifact:1", Kind: "specification", Title: "Specification", Content: "Scenario: exact Gherkin", Revision: 2, At: "2026-08-22T03:18:00Z"},
			{ID: "review:1", Kind: "review", Title: "approved", Content: "Store access preserves logical state.", UnitID: "wu-store", Revision: 1, At: "2026-08-21T12:30:00Z"},
		},
	}}}
}
