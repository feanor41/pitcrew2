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
	"github.com/charmbracelet/x/ansi"
	"github.com/fmazzalomo/pitcrew/internal/history"
)

var updateGoldens = flag.Bool("update", false, "update TUI golden files")

func TestMain(m *testing.M) {
	_ = os.Unsetenv("NO_COLOR")
	os.Exit(m.Run())
}

func TestViewRepresentativeLayouts(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
	}{
		{"wide", 166, 30},
		{"canonical", 120, 30},
		{"canonical-29", 120, 29},
		{"compact", 80, 24},
		{"detail-60", 60, 24},
		{"minimum-detail", 60, 16},
		{"below-minimum", 59, 15},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			got := model.View().Content
			for _, line := range strings.Split(got, "\n") {
				if width := lipgloss.Width(line); width > test.width {
					t.Fatalf("rendered line width %d exceeds terminal width %d", width, test.width)
				}
			}
			if len(strings.Split(got, "\n")) > test.height {
				t.Fatalf("rendered frame exceeds terminal height %d", test.height)
			}
			assertGolden(t, test.name, got)
		})
	}
}

func TestViewHomeUsesSharedBorderedHeaderAndExactActions(t *testing.T) {
	model := New(fakeLoader{})
	model.loading = false
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := model.View().Content
	plain := ansi.Strip(got)
	for _, identity := range []string{"PitCrew2", "Control Plane", "v0.14.0"} {
		if !strings.Contains(plain, identity) {
			t.Fatalf("home header missing %q:\n%s", identity, got)
		}
	}
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "╰") {
		t.Fatalf("home header is not bordered:\n%s", got)
	}
	last := -1
	for _, action := range []string{"Install in Runtime", "Configure Runtime Models", "Workflows", "Exit"} {
		if count := strings.Count(plain, action); count != 1 {
			t.Fatalf("home action %q count = %d:\n%s", action, count, got)
		}
		position := strings.Index(plain, action)
		if position <= last {
			t.Fatalf("home action %q is out of order:\n%s", action, got)
		}
		last = position
	}
	if strings.Count(plain, "▶") != 1 {
		t.Fatalf("home selection marker count = %d:\n%s", strings.Count(plain, "▶"), got)
	}
	for _, line := range strings.Split(got, "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Fatalf("home line width %d exceeds 80: %q", width, line)
		}
	}
}

func TestViewSharedHeaderIsBoundedAtSupportedWidths(t *testing.T) {
	for _, width := range []int{60, 80, 120} {
		for _, screen := range []Screen{HomeScreen, WorkflowsScreen, ResultsScreen, DetailScreen} {
			model := Model{screen: screen, width: width, height: 24}
			header := model.header()
			lines := strings.Split(header, "\n")
			if len(lines) != 4 {
				t.Fatalf("width %d screen %v header has %d rows:\n%s", width, screen, len(lines), header)
			}
			for _, line := range lines {
				if got := lipgloss.Width(line); got != width {
					t.Fatalf("width %d screen %v header line has %d cells: %q", width, screen, got, line)
				}
			}
			plain := ansi.Strip(header)
			if !strings.Contains(plain, "PitCrew2") || !strings.Contains(plain, "Control Plane") || !strings.Contains(plain, "v0.14.0") {
				t.Fatalf("width %d screen %v header identity incomplete:\n%s", width, screen, header)
			}
		}
	}
}

func TestViewWorkflowsUsesResponsiveBorderedNonWrappingGrid(t *testing.T) {
	workflows := make([]history.Workflow, 12)
	for i := range workflows {
		workflows[i] = history.Workflow{
			ID: fmt.Sprintf("wf-%02d", i), Name: strings.Repeat("界", 40) + fmt.Sprintf("-%02d", i), State: "ready_to_complete",
			CreatedAt: fmt.Sprintf("2026-08-28T01:%02d:00Z", i),
		}
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 60, Height: 16}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			model := Model{screen: WorkflowsScreen, workflows: workflows, selected: 7}
			model, _ = model.Update(size)
			got := model.View().Content
			plain := ansi.Strip(got)
			for _, heading := range []string{"Started", "Workflow", "Status"} {
				if !strings.Contains(plain, heading) {
					t.Fatalf("grid heading %q missing:\n%s", heading, got)
				}
			}
			for _, boundary := range []string{"╭", "┬", "├", "┼", "╰", "┴"} {
				if !strings.Contains(plain, boundary) {
					t.Fatalf("grid boundary %q missing:\n%s", boundary, got)
				}
			}
			if strings.Count(plain, "▶") != 1 || !strings.Contains(plain, "…") {
				t.Fatalf("grid lacks explicit selection or truncation:\n%s", got)
			}
			for _, line := range strings.Split(got, "\n") {
				if width := lipgloss.Width(line); width > size.Width {
					t.Fatalf("grid line width %d exceeds %d: %q", width, size.Width, line)
				}
			}
		})
	}
}

func TestViewWorkflowsShowsTruthfulLoadingEmptyAndErrorStates(t *testing.T) {
	for _, test := range []struct {
		name  string
		model Model
		want  []string
	}{
		{"loading", Model{screen: WorkflowsScreen, loading: true}, []string{"WORKFLOWS", "Loading workflows…"}},
		{"empty", Model{screen: WorkflowsScreen}, []string{"WORKFLOWS", "No workflows are available."}},
		{"error", Model{screen: WorkflowsScreen, err: errors.New("database is locked")}, []string{"WORKFLOWS", "Could not load workflows.", "database is locked"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _ := test.model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
			plain := ansi.Strip(model.View().Content)
			for _, want := range test.want {
				if !strings.Contains(plain, want) {
					t.Fatalf("workflow state missing %q:\n%s", want, model.View().Content)
				}
			}
			if !strings.Contains(plain, "h back") || !strings.Contains(plain, "r refresh") || !strings.Contains(plain, "q quit") {
				t.Fatalf("workflow state controls are incomplete:\n%s", model.View().Content)
			}
		})
	}
}

func TestViewWorkflowMockupGoldens(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
	}{
		{"workflows-80", 80, 24},
		{"workflows-60", 60, 16},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _ := workflowGridViewModel().Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			assertGolden(t, test.name, model.View().Content)
		})
	}
}

func TestViewWorkflowNoColorGolden(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	model, _ := workflowGridViewModel().Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	got := model.View().Content
	assertNoANSI(t, got)
	for _, want := range []string{"Started", "Workflow", "Status", "▶", "[DONE]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("NO_COLOR workflow frame missing %q:\n%s", want, got)
		}
	}
	assertGolden(t, "workflow-no-color", got)
}

func TestViewUsesSharedHierarchyAndNativeComponents(t *testing.T) {
	model := detailViewModel()
	model.opened.Record = history.Record{
		ID: "evidence:markdown", Kind: "evidence", Actor: "pc2-implementer",
		Title: "Validation", Content: "# Result\n\n- focused tests pass",
	}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	got := model.View().Content
	for _, label := range []string{"Actor", "Record"} {
		if !strings.Contains(got, flight.label.Render(label)) {
			t.Fatalf("evidence label %q is not bold:\n%s", label, got)
		}
	}
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "Result") || strings.Contains(plain, "# Result") || !strings.Contains(plain, "focused tests pass") {
		t.Fatalf("evidence does not render the prepared Markdown viewport:\n%s", got)
	}
}

func TestViewNoColorGolden(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	model := detailViewModel()
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	got := model.View().Content
	assertNoANSI(t, got)
	assertGolden(t, "no-color", got)
}

func TestViewWideDetailNoColorGolden(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	model, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: 166, Height: 30})
	got := model.View().Content
	assertNoANSI(t, got)
	assertGolden(t, "wide-no-color", got)
}

func TestViewWideDetailUsesFixedThirtyRowLayoutAndPreservesOccurrence(t *testing.T) {
	model, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: 166, Height: 30})
	selected := model.detail.occurrenceID
	plain := ansi.Strip(model.View().Content)
	if lines := strings.Split(plain, "\n"); len(lines) != 30 {
		t.Fatalf("wide detail has %d rows, want 30:\n%s", len(lines), plain)
	}
	for _, want := range []string{"PROGRESS", "NOW", "NEXT", "STAGES", "UNITS", "TIMELINE", "│"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("wide detail missing %q:\n%s", want, plain)
		}
	}
	for _, unwanted := range []string{"WORKFLOW TREE", "PENDING / BLOCKED", "RELATED RECORDS / EVIDENCE"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("wide detail retained %q:\n%s", unwanted, plain)
		}
	}
	if strings.Count(plain, "▶") != 1 {
		t.Fatalf("wide focus marker count differs from one:\n%s", plain)
	}
	model.loader = fakeLoader{occurrenceResolution: history.Resolution{Detail: model.opened.Detail}}
	opened, command := model.Update(special(tea.KeyEnter))
	if command == nil {
		t.Fatal("wide timeline selection is not openable")
	}
	opened, _ = opened.Update(command())
	if opened.opened.Record.ID != "review:1" {
		t.Fatalf("opened %q, want review:1", opened.opened.Record.ID)
	}
	narrow, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	if narrow.detail.occurrenceID != selected {
		t.Fatalf("resize changed occurrence: got %q want %q", narrow.detail.occurrenceID, selected)
	}
}

func TestViewWideDetailThresholdAndUnicodeTruncation(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
		wide          bool
	}{
		{"120x30 enables wide detail", 120, 30, true},
		{"119x30 disables wide detail", 119, 30, false},
		{"120x29 disables wide detail", 120, 29, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := detailViewModel()
			model.opened.Detail.Workflow.Name = strings.Repeat("界", 80)
			model, _ = model.Update(tea.WindowSizeMsg{Width: test.width, Height: test.height})
			got := model.View().Content
			if model.wideDetailMode() != test.wide {
				t.Fatalf("wide detail = %v, want %v", model.wideDetailMode(), test.wide)
			}
			body := strings.Join(strings.Split(ansi.Strip(got), "\n")[4:], "\n")
			if strings.Contains(body, "│") != test.wide {
				t.Fatalf("split presence differs from wide=%v:\n%s", test.wide, got)
			}
			for _, line := range strings.Split(got, "\n") {
				if lipgloss.Width(line) > test.width {
					t.Fatalf("line exceeds %d: %q", test.width, line)
				}
			}
			if !strings.Contains(ansi.Strip(got), "…") {
				t.Fatalf("Unicode identity was not truncated:\n%s", got)
			}
		})
	}
}

func TestViewNoColorEmitsNoANSIAcrossScreens(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	home := Model{screen: HomeScreen}
	workflows := workflowGridViewModel()
	detail := detailViewModel()
	evidence := detailViewModel()
	evidence.opened.Record = evidence.opened.Detail.Records[0]
	for name, model := range map[string]Model{
		"home": home, "workflows": workflows, "detail": detail, "evidence": evidence,
	} {
		t.Run(name, func(t *testing.T) {
			model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			assertNoANSI(t, model.View().Content)
		})
	}
}

func TestViewFramesStayWithinTerminalBounds(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 166, Height: 30}, {Width: 80, Height: 24}, {Width: 60, Height: 16}, {Width: 42, Height: 10}} {
		model := detailViewModel()
		model.opened.Record = model.opened.Detail.Records[0]
		model, _ = model.Update(size)
		lines := strings.Split(model.View().Content, "\n")
		if len(lines) > size.Height {
			t.Fatalf("%dx%d frame has %d lines", size.Width, size.Height, len(lines))
		}
		for _, line := range lines {
			if width := lipgloss.Width(line); width > size.Width {
				t.Fatalf("%dx%d frame has %d-cell line: %q", size.Width, size.Height, width, line)
			}
		}
	}
}

func TestViewColumnHeadingsAreBoldAndValuesRemainNormal(t *testing.T) {
	model, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	frame := model.View().Content
	for _, heading := range []string{"PROGRESS", "NOW", "NEXT", "STAGES", "UNITS", "TIMELINE"} {
		if !strings.Contains(frame, flight.label.Render(heading)) && !strings.Contains(frame, flight.title.Render(heading)) {
			t.Fatalf("heading %q lacks hierarchy:\n%s", heading, frame)
		}
	}
	for _, value := range []string{"Render diseño operativo", "workflow list-ready-units", "Recorded review"} {
		if strings.Contains(frame, flight.label.Render(value)) {
			t.Fatalf("value %q is bold:\n%s", value, frame)
		}
	}
}

func TestViewMinimumGuidanceFitsVeryNarrowTerminal(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 1, Height: 10}, {Width: 10, Height: 10}, {Width: 20, Height: 10}, {Width: 20, Height: 2}} {
		model, _ := workflowViewModel().Update(size)
		got := model.View().Content
		lines := strings.Split(got, "\n")
		if len(lines) > size.Height {
			t.Fatalf("minimum frame has %d lines at %dx%d:\n%s", len(lines), size.Width, size.Height, got)
		}
		for _, line := range lines {
			if width := lipgloss.Width(line); width > size.Width {
				t.Fatalf("minimum guidance line is %d cells at width %d: %q\n%s", width, size.Width, line, got)
			}
		}
	}
	model, _ := workflowViewModel().Update(tea.WindowSizeMsg{Width: 20, Height: 10})
	plain := ansi.Strip(model.View().Content)
	for _, want := range []string{"Terminal too small", "Need at least", "q quit"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("minimum guidance is missing %q:\n%s", want, model.View().Content)
		}
	}
}

func TestViewStatesAndResize(t *testing.T) {
	tests := []struct {
		name  string
		model Model
		want  []string
	}{
		{"uninitialized", Model{screen: WorkflowsScreen, err: ErrUninitialized}, []string{"No PitCrew repository is initialized for this project.", "q quit"}},
		{"empty", Model{screen: WorkflowsScreen}, []string{"No workflows are available.", "q quit"}},
		{"no results", Model{screen: ResultsScreen, query: "missing"}, []string{`No results for "missing".`, "query: missing"}},
		{"error", Model{screen: WorkflowsScreen, err: errors.New("database is locked")}, []string{"Could not load workflows.", "database is locked", "q quit"}},
		{"search input", Model{screen: WorkflowsScreen, workflows: workflowViewModel().workflows, searchFocused: true, query: "review"}, []string{"SEARCH ›", "review", "enter search"}},
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
			plain := ansi.Strip(got)
			for _, identity := range []string{"PitCrew2", "Control Plane", "v0.14.0"} {
				if !strings.Contains(got, identity) {
					t.Fatalf("view missing identity %q:\n%s", identity, got)
				}
			}
			if !strings.Contains(got, flight.version.Render("v0.14.0")) {
				t.Fatalf("view lacks version accent:\n%s", got)
			}
			for _, want := range test.want {
				if !strings.Contains(plain, want) {
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

func TestViewDetailRendersOrderedUnitProgressAtResponsiveSizes(t *testing.T) {
	model := detailViewModel()
	model.opened.Detail.Synopsis.Total, model.opened.Detail.Synopsis.Done = 6, 3 // Includes one legacy unplanned completion.
	model.opened.Detail.Synopsis.Planned.Total, model.opened.Detail.Synopsis.Planned.Done, model.opened.Detail.Synopsis.Planned.Percent = 5, 2, 40
	model.opened.Detail.Synopsis.Blocker = &history.UnitStatus{ID: "wu-fix", Description: "Fix blocked rendering", Status: "Correction", Reason: "Missing failure coverage"}
	model.opened.Detail.Synopsis.Current = model.opened.Detail.Synopsis.Blocker
	model.opened.Detail.Synopsis.Planned.Units = []history.UnitStatus{
		{ID: "wu-done", Description: "Map signals", Status: "Done"},
		{ID: "wu-ready", Description: "Polish timeline", Status: "Ready"},
		{ID: "wu-fix", Description: "Fix blocked rendering", Status: "Correction", Reason: "Missing failure coverage"},
		{ID: "wu-wait", Description: "Verify resize", Status: "Dependency waiting"},
		{ID: "wu-queued", Description: "Document controls", Status: "Queued"},
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 166, Height: 30}, {Width: 80, Height: 24}, {Width: 60, Height: 16}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			rendered, _ := model.Update(size)
			plain := ansi.Strip(rendered.View().Content)
			for _, want := range []string{"PROGRESS  2/5 done", "NOW", "Correction · Fix blocked rendering", "NEXT", "BLOCKED", "Missing failure coverage", "UNITS  2/5 done"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("missing %q:\n%s", want, plain)
				}
			}
			if size.Width == 166 {
				mapAt, polishAt, fixAt := strings.Index(plain, "Map signals"), strings.Index(plain, "Polish timeline"), strings.Index(plain, "Fix blocked rendering · Correction")
				if mapAt < 0 || polishAt < mapAt || fixAt < polishAt {
					t.Fatalf("accepted plan order was not preserved:\n%s", plain)
				}
			}
			wantMore := ""
			if size.Width == 80 {
				wantMore = "+3 more"
			} else if size.Width == 60 {
				wantMore = "+4 more"
			}
			if wantMore != "" && !strings.Contains(plain, wantMore) {
				t.Fatalf("truncated unit rows lack accurate %q indicator:\n%s", wantMore, plain)
			}
		})
	}
}

func TestSemanticOccurrenceSuppressesEquivalentOutcomeButPreservesMeaning(t *testing.T) {
	model := Model{width: 120}
	for _, test := range []struct {
		name       string
		occurrence history.Occurrence
		want       []string
		unwanted   []string
	}{
		{
			name:       "same words",
			occurrence: history.Occurrence{Activity: "workflow_created", Outcome: "Created workflow"},
			want:       []string{"Created workflow"},
		},
		{
			name:       "equivalent word order",
			occurrence: history.Occurrence{Activity: "workflow_created", Outcome: "Workflow created"},
			want:       []string{"Created workflow"},
			unwanted:   []string{"Workflow created"},
		},
		{
			name:       "equivalent outcome with independent reason",
			occurrence: history.Occurrence{Activity: "unit_review_recorded", Outcome: "Review recorded", Reason: "review database unavailable"},
			want:       []string{"Recorded review", "review database unavailable"},
			unwanted:   []string{"Review recorded"},
		},
		{
			name:       "independent verdict and reason",
			occurrence: history.Occurrence{Activity: "unit_review_recorded", Outcome: "Corrections", Reason: "missing failure coverage"},
			want:       []string{"Recorded review", "Corrections: missing failure coverage"},
		},
		{
			name:       "independent failure and blocker",
			occurrence: history.Occurrence{Activity: "unit_tdd_recorded", Outcome: "Blocked", Reason: "focused tests failed"},
			want:       []string{"Recorded TDD", "Blocked: focused tests failed"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			plain := ansi.Strip(strings.Join(model.semanticOccurrenceLines(test.occurrence, false), "\n"))
			for _, want := range test.want {
				if !strings.Contains(plain, want) {
					t.Fatalf("semantic occurrence missing %q:\n%s", want, plain)
				}
			}
			for _, unwanted := range test.unwanted {
				if strings.Contains(plain, unwanted) {
					t.Fatalf("semantic occurrence repeats equivalent outcome %q:\n%s", unwanted, plain)
				}
			}
			if test.name == "same words" && strings.Count(plain, "Created workflow") != 1 {
				t.Fatalf("semantic occurrence repeats action heading as outcome:\n%s", plain)
			}
		})
	}
}

func TestTimelineDoesNotRenderEvidenceBodyBeforeDrillDown(t *testing.T) {
	model, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: 120, Height: 29})
	plain := ansi.Strip(model.View().Content)
	for _, evidence := range []string{"Unit review", "Verdict: approved", "Store access preserves logical state."} {
		if strings.Contains(plain, evidence) {
			t.Fatalf("timeline exposed evidence %q before Enter:\n%s", evidence, plain)
		}
	}
	if !strings.Contains(plain, "Recorded review") || !strings.Contains(plain, "Approved") {
		t.Fatalf("timeline lost event meaning while hiding evidence:\n%s", plain)
	}
}

func TestViewRelatedRecordLevelShowsEverySupportingRecordAndFocus(t *testing.T) {
	records := []history.Record{
		{ID: "artifact:primary", Kind: "specification", Title: "Specification"},
		{ID: "review:one", Kind: "review", Title: "Implementation review"},
		{ID: "evidence:one", Kind: "evidence", Title: "Focused validation"},
	}
	occurrence := history.Occurrence{ID: "activity:multi", RecordID: records[0].ID, RelatedRecordIDs: []string{records[1].ID, records[2].ID}}
	detail := history.Detail{Workflow: history.Workflow{ID: "wf", Name: "Related records"}, Occurrences: []history.Occurrence{occurrence}, Records: records}
	model := New(fakeLoader{})
	model, _ = model.Update(detailLoadedMsg{resolution: history.Resolution{Detail: detail}})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(special(tea.KeyEnter))
	model, _ = model.Update(textKey("j"))
	plain := ansi.Strip(model.View().Content)

	if !strings.Contains(plain, "RELATED RECORDS  3") {
		t.Fatalf("related-record level heading missing:\n%s", plain)
	}
	for _, record := range records {
		if !strings.Contains(plain, record.Title) || !strings.Contains(plain, record.ID) {
			t.Fatalf("related-record level missing %#v:\n%s", record, plain)
		}
	}
	if !strings.Contains(plain, "▶ Implementation review") {
		t.Fatalf("related-record focus is not visible:\n%s", plain)
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
			t.Fatalf("grid missing %q", want)
		}
	}
	detail, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, want := range []string{"Redesign TUI history", "IMPLEMENTING", "r7", "2/3 done", "NOW", "NEXT", "STAGES", "UNITS", "TIMELINE", "Specify", "Recorded specification", "Recorded review"} {
		if !strings.Contains(ansi.Strip(detail.View().Content), want) {
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
	if got := model.View().Content; !strings.Contains(got, "EVIDENCE") || !strings.Contains(got, "Exact linked workflow result") {
		t.Fatalf("resolved workflow result is not inspectable:\n%s", got)
	}
}

func TestViewMarksDerivedHistoricalName(t *testing.T) {
	model := Model{screen: WorkflowsScreen, workflows: []history.Workflow{{Name: "Legacy goal", NameDerived: true, CreatedAt: "2026-08-20T12:00:00Z"}}}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 72, Height: 24})
	if got := model.View().Content; !strings.Contains(got, "Legacy goal [derived]") {
		t.Fatalf("derived name is not marked:\n%s", got)
	}
}

func TestViewCompactIdentityAcrossLayouts(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 112, Height: 28}, {Width: 60, Height: 16}, {Width: 42, Height: 10}} {
		model, _ := workflowViewModel().Update(size)
		got := model.View().Content
		if !strings.Contains(got, "PitCrew2") || !strings.Contains(got, "Control Plane") || !strings.Contains(got, flight.version.Render("v0.14.0")) {
			t.Fatalf("%dx%d missing accessible identity or version accent:\n%s", size.Width, size.Height, got)
		}
		if strings.Contains(got, "╔═╗") {
			t.Fatalf("%dx%d retained oversized wordmark:\n%s", size.Width, size.Height, got)
		}
	}
}

func TestViewSemanticHistoryBreakpoints(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 166, Height: 30}, {Width: 120, Height: 30}, {Width: 120, Height: 29}, {Width: 80, Height: 24}, {Width: 60, Height: 24}, {Width: 60, Height: 16}} {
		t.Run(fmt.Sprintf("%dx%d", size.Width, size.Height), func(t *testing.T) {
			model, _ := detailViewModel().Update(size)
			plain := ansi.Strip(model.View().Content)
			for _, want := range []string{"IMPLEMENTING", "NEXT", "workflow list-ready-units", "STAGES", "UNITS", "TIMELINE", "Recorded review"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("%dx%d missing %q:\n%s", size.Width, size.Height, want, plain)
				}
			}
			if strings.Count(plain, "▶") != 1 {
				t.Fatalf("%dx%d focus markers != 1:\n%s", size.Width, size.Height, plain)
			}
			if strings.Contains(plain, "Unit review") || strings.Contains(plain, "Verdict: approved") {
				t.Fatalf("%dx%d exposed evidence before Enter:\n%s", size.Width, size.Height, plain)
			}
		})
	}
}

func TestViewWorkflowListKeepsEveryStatusWithinSixtyCells(t *testing.T) {
	states := []string{"draft", "exploring", "specifying", "designing", "planning", "plan_approved", "implementing", "ready_to_complete", "completed", "abandoned"}
	model := Model{screen: WorkflowsScreen}
	for i, state := range states {
		model.workflows = append(model.workflows, history.Workflow{
			ID: "wf-" + state, Name: "Critical workflow identity " + state, State: state,
			CreatedAt: "2026-08-22T03:" + fmt.Sprintf("%02d", i) + ":00Z",
		})
	}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	got := model.View().Content
	if !strings.Contains(got, "[READY TO …") {
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

func TestViewMinimumDetailPrioritizesSelectedUnicodeOccurrenceWithoutEvidence(t *testing.T) {
	model, _ := detailViewModel().Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	plain := ansi.Strip(model.View().Content)
	for _, want := range []string{"IMPLEMENTING", "NOW", "NEXT", "STAGES", "UNITS", "TIMELINE", "Recorded review", "Approved", "diseño"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("60x16 missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Unit review") {
		t.Fatalf("60x16 exposed evidence before Enter:\n%s", plain)
	}
}

func TestViewProgressiveDetailShowsTypedMetadataBelowActorBreakpoint(t *testing.T) {
	model := detailViewModel()
	model.detail.occurrenceID = "activity:3"
	model.opened.Record = history.Record{ID: "evidence:1", Kind: "evidence", UnitID: "wu-view", Revision: 1, Actor: "pc2-implementer", Title: "Validation", Content: "Focused tests passed", At: "2026-08-22T03:19:00Z"}
	model, _ = model.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	got := model.View().Content
	plain := ansi.Strip(got)
	for _, want := range []string{"Actor  pc2-implementer", "Record  evidence:1", "Unit  wu-view", "Revision  1", "Try  1"} {
		if !strings.Contains(plain, want) {
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

func TestViewBlockerIsConditionalAndVisibleAtMinimum(t *testing.T) {
	model := detailViewModel()
	without, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	if strings.Contains(ansi.Strip(without.View().Content), "BLOCKED") {
		t.Fatalf("blocker shown without blocker")
	}
	model.opened.Detail.Synopsis.Blocker = &history.UnitStatus{Description: "Render diseño operativo", Status: "Correction", Reason: "Review correction required", Derived: true}
	with, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	plain := ansi.Strip(with.View().Content)
	for _, want := range []string{"BLOCKED", "Review correction required", "NEXT", "TIMELINE", "Recorded review"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("minimum blocker view missing %q:\n%s", want, plain)
		}
	}
}

func TestViewStageRailNeverClaimsFutureStagesComplete(t *testing.T) {
	for _, test := range []struct {
		name, state, want, unwanted string
		occurrences                 []history.Occurrence
	}{
		{name: "sparse exploring", state: "exploring", want: "● Explore", unwanted: "✓ Explore"},
		{name: "recorded exploration", state: "specifying", occurrences: []history.Occurrence{{Activity: "exploration_recorded"}}, want: "✓ Explore ─ ● Spec", unwanted: "✓ Spec"},
		{name: "skipped spec", state: "designing", occurrences: []history.Occurrence{{Activity: "exploration_recorded"}}, want: "✓ Explore ─ ○ Spec ─ ● Design", unwanted: "✓ Spec"},
		{name: "sparse ready for review", state: "ready_to_complete", occurrences: []history.Occurrence{{Activity: "unit_completed"}}, want: "✓ Build ─ ● Review", unwanted: "✓ Plan"},
		{name: "sparse completed", state: "completed", occurrences: []history.Occurrence{{Activity: "workflow_completed"}}, want: "✓ Review", unwanted: "✓ Spec"},
		{name: "abandoned during design", state: "abandoned", occurrences: []history.Occurrence{{Phase: "Design", Activity: "exploration_recorded"}}, want: "✓ Explore ─ ○ Spec ─ ! Design", unwanted: "✓ Spec"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := detailViewModel()
			model.opened.Detail.Workflow.State = test.state
			model.opened.Detail.Occurrences = test.occurrences
			model.opened.Detail.Records = nil
			model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 29})
			plain := ansi.Strip(model.View().Content)
			if !strings.Contains(plain, test.want) || strings.Contains(plain, test.unwanted) {
				t.Fatalf("dishonest rail for %s:\n%s", test.state, plain)
			}
		})
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

func assertNoANSI(t *testing.T, got string) {
	t.Helper()
	if strings.ContainsRune(got, '\x1b') || ansi.Strip(got) != got {
		t.Fatalf("NO_COLOR output contains ANSI escape sequences: %q", got)
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
	return Model{screen: WorkflowsScreen, workflows: []history.Workflow{
		{ID: "wf-alpha", Name: "Redesign TUI history", Revision: 7, State: "implementing", Goal: "Ship read-only terminal inspection", CreatedAt: "2026-08-22T03:17:38Z", UpdatedAt: "2026-08-22T03:18:00Z"},
		{ID: "wf-beta", Name: "Rename Daimon", Revision: 3, State: "completed", Goal: "Rename the coordination role", CreatedAt: "2026-08-20T12:00:00Z", UpdatedAt: "2026-08-20T13:00:00Z"},
	}}
}

func workflowGridViewModel() Model {
	workflows := make([]history.Workflow, 8)
	states := []string{"completed", "implementing", "abandoned", "ready_to_complete"}
	for i := range workflows {
		workflows[i] = history.Workflow{
			ID: fmt.Sprintf("wf-%02d", i), Name: strings.Repeat("Unicode 界 goal ", i%3+1), State: states[i%len(states)],
			CreatedAt: fmt.Sprintf("2026-08-28T01:%02d:00Z", i),
		}
	}
	return Model{screen: WorkflowsScreen, workflows: workflows, selected: 6}
}

func detailViewModel() Model {
	attempt := int64(1)
	return Model{screen: DetailScreen, opened: history.Resolution{Detail: history.Detail{
		Workflow: history.Workflow{ID: "wf-alpha", Name: "Redesign TUI history", Revision: 7, State: "implementing", Goal: "Ship read-only terminal inspection", CreatedAt: "2026-08-22T03:17:38Z", UpdatedAt: "2026-08-22T03:18:00Z"},
		Synopsis: history.Synopsis{
			Total: 3, Done: 2, Reviewing: 1, NextAction: "workflow list-ready-units",
			Current: &history.UnitStatus{ID: "wu-view", Description: "Render diseño operativo", Status: "Reviewing", Attempt: 1, Derived: true},
			Planned: &history.PlannedWork{Total: 3, Done: 2, Percent: 66,
				Units: []history.UnitStatus{
					{ID: "wu-discover", Description: "Map history signals", Status: "Done", Attempt: 1},
					{ID: "wu-structure", Description: "Define responsive hierarchy", Status: "Done", Attempt: 1},
					{ID: "wu-view", Description: "Render diseño operativo", Status: "Reviewing", Attempt: 1},
				},
				Pending: []history.UnitStatus{{ID: "wu-view", Description: "Render diseño operativo", Status: "Reviewing", Attempt: 1}},
			},
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
			{ID: "review:1", Kind: "review", Title: "approved", Content: "# Unit review\n\n**Verdict:** approved\n\nStore access preserves logical state.", UnitID: "wu-store", Revision: 1, At: "2026-08-21T12:30:00Z"},
		},
	}}}
}
