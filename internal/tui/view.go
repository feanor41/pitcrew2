package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/fmazzalomo/pitcrew/internal/history"
)

const minWidth, minHeight, wideWidth = 60, 16, 96

var largeWordmark = []string{
	"╔═╗ ╦ ╔╦╗ ╔═╗ ╦═╗ ╔═╗ ╦ ╦ ╔═╗",
	"╠═╝ ║  ║  ║   ╠╦╝ ║╣  ║║║ ╔═╝",
	"╩   ╩  ╩  ╚═╝ ╩╚═ ╚═╝ ╚╩╝ ╚═╝",
}

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}

func (m Model) render() string {
	if m.width < minWidth || m.height < minHeight {
		return m.header(true, "") + "\n" +
			flight.warn.Render("Terminal too small") + "\nNeed at least 60×16; resize to continue.\n\n" + flight.footer.Render("q quit")
	}
	mode := "SINGLE PANE"
	if m.width >= wideWidth {
		mode = "MULTI PANE"
	}
	header := m.header(false, mode)
	body := m.body(mode == "MULTI PANE")
	if m.searchFocused {
		body = flight.focus.Render("SEARCH › ") + m.query + "█\n" + body
	}
	footer := flight.footer.Render(fmt.Sprintf("%s  ·  %dx%d", m.footerHints(), m.width, m.height))
	return trimTrailing(strings.Join([]string{header, body, footer}, "\n"))
}

func trimTrailing(value string) string {
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	return strings.Join(lines, "\n")
}

func (m Model) header(minimum bool, mode string) string {
	identity := flight.brand.Render(strings.Join(largeWordmark, "\n")) + "\n" +
		flight.brand.Render("PitCrew2") + "  " + flight.subtitle.Render("Control Plane") + "  " + flight.version.Render("v"+m.Version())
	if minimum {
		return identity
	}
	identity += "  ·  " + flight.mode.Render(mode)
	return flight.panel.Width(max(20, m.width-4)).Render(identity)
}

func (m Model) footerHints() string {
	if m.searchFocused {
		return m.Hints()
	}
	if m.width < wideWidth {
		if m.screen == DetailScreen {
			current, total := m.detailPosition()
			return fmt.Sprintf("↑/k ↓/j scroll • h back • line %d/%d • q quit", current, total)
		}
		return "↑/k ↓/j move • h back • l/enter open • / search • q quit"
	}
	if m.screen == DetailScreen {
		current, total := m.detailPosition()
		return fmt.Sprintf("↑/k ↓/j scroll • ←/h back • line %d/%d • / search • q quit", current, total)
	}
	return m.Hints()
}

func (m Model) body(wide bool) string {
	if m.loading {
		return statePanel("LOADING", "Reading project history…")
	}
	if m.err != nil {
		if errors.Is(m.err, ErrUninitialized) {
			return statePanel("NOT INITIALIZED", ErrUninitialized.Error())
		}
		return statePanel("READ ERROR", "Could not read PitCrew history.\n"+m.err.Error()+"\nCheck the database and try again.")
	}
	if m.screen == ResultsScreen && len(m.results) == 0 {
		return statePanel("SEARCH", fmt.Sprintf("No results for %q.\nquery: %s", m.query, m.query))
	}
	if m.screen == WorkflowsScreen && len(m.workflows) == 0 {
		return statePanel("HISTORY", "No workflow history yet.")
	}
	if wide {
		available := max(3, m.height-9)
		if m.screen != DetailScreen {
			return flight.panel.Width(m.width - 4).MaxHeight(available).Render(m.singlePane())
		}
		left := flight.panel.Width(34).MaxHeight(available).Render("WORKFLOW\n" + m.rail())
		right := flight.panel.Width(max(40, m.width-43)).MaxHeight(available).Render("HISTORY\n" + m.canvas())
		return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	}
	panel := flight.panel.Width(max(40, m.width-4)).MaxHeight(max(3, m.height-9))
	return panel.Render(m.singlePane())
}

func statePanel(title, message string) string {
	return flight.panel.Render(flight.title.Render(title) + "\n\n" + message)
}

func (m Model) rail() string {
	if m.screen == DetailScreen {
		w := m.opened.Detail.Workflow
		return strings.Join([]string{flight.title.Render(displayName(w)), "Created  " + formatTime(w.CreatedAt), "Updated  " + formatTime(w.UpdatedAt), "State    " + w.State, fmt.Sprintf("ID       %s · r%d", w.ID, w.Revision), "", "Goal", w.Goal}, "\n")
	}
	return m.list()
}

func (m Model) singlePane() string {
	switch m.screen {
	case DetailScreen:
		if m.height <= minHeight+2 {
			w := m.opened.Detail.Workflow
			return fmt.Sprintf("%s  %s\nCreated %s  Updated %s\nGoal %s\n%s", truncateDisplay(displayName(w), 28), statusLabel(w.State), formatTime(w.CreatedAt), formatTime(w.UpdatedAt), truncateDisplay(w.Goal, max(12, m.width-10)), m.canvas())
		}
		return "WORKFLOW\n" + m.rail() + "\n\nHISTORY\n" + m.canvas()
	case ResultsScreen:
		return "SEARCH RESULTS\nquery: " + m.query + "\n" + m.list()
	default:
		return "WORKFLOWS\n" + m.list()
	}
}

func (m Model) list() string {
	var lines []string
	if m.screen == ResultsScreen {
		for i, result := range m.results {
			lines = append(lines, focus(i == m.selected)+fmt.Sprintf("%s · %s\n  %s", result.Kind, result.WorkflowID, result.Context))
		}
	} else {
		width := max(18, m.evidenceWidth()-44)
		lines = append(lines, "  Started           | Work"+strings.Repeat(" ", max(1, width-4))+" | Status")
		for i, workflow := range m.workflows {
			lines = append(lines, focus(i == m.selected)+fmt.Sprintf("%-17s | %-*s | %s", formatTime(workflow.CreatedAt), width, truncateDisplay(displayName(workflow), width), statusLabel(workflow.State)))
		}
	}
	return strings.Join(lines, "\n")
}

func focus(selected bool) string {
	if selected {
		return flight.focus.Render("▶ ")
	}
	return "  "
}

func (m Model) canvas() string {
	detail := m.opened.Detail
	if detail.Workflow.ID == "" {
		return "Select a workflow to inspect its evidence."
	}
	lines := []string{}
	m.reconcileDetail()
	layout := m.evidenceLines()
	current, _ := m.detailPosition()
	end := min(len(layout), m.detail.top+m.evidenceHeight())
	for i := m.detail.top; i < end; i++ {
		lines = append(lines, focus(i == current-1)+layout[i].text)
	}
	if len(detail.Records) == 0 {
		lines = append(lines, flight.muted.Render("No evidence recorded for this workflow."))
	}
	return strings.Join(lines, "\n")
}

type evidenceLine struct {
	recordID string
	local    int
	text     string
	activity *history.Activity
}

func (m Model) evidenceLines() []evidenceLine {
	return evidenceLayout(m.opened, max(1, m.evidenceWidth()-4))
}

func (m Model) evidenceWidth() int {
	if m.width >= wideWidth {
		return max(40, m.width-39)
	}
	return max(40, m.width-4)
}

func (m Model) evidenceHeight() int {
	if m.width < wideWidth && m.height <= minHeight+2 {
		return 2
	}
	return max(1, m.height-12)
}

func evidenceLayout(resolution history.Resolution, width int) []evidenceLine {
	detail := resolution.Detail
	var lines []evidenceLine
	for index, entry := range detail.Timeline {
		entry := entry
		lead := fmt.Sprintf("%s  %s", formatTime(entry.At), entry.Actor)
		if index == 0 {
			lead = "ACTIVITY  " + lead
		}
		action := actionLabel(entry.Action)
		if entry.Legacy {
			action += "  [legacy]"
		}
		local := 0
		for _, part := range []string{lead, "  " + action} {
			for _, wrapped := range wrapDisplay(part, width) {
				lines = append(lines, evidenceLine{entry.ID, local, wrapped, &entry})
				local++
			}
		}
	}
	records := detail.Records
	if resolution.Record.ID != "" {
		found := false
		for _, record := range records {
			found = found || record.ID == resolution.Record.ID
		}
		if !found {
			records = append([]history.Record{resolution.Record}, records...)
		}
	}
	for index, record := range records {
		identity := record.Kind
		if record.UnitID != "" {
			identity += " · " + record.UnitID
		}
		prefix := ""
		if index == 0 {
			prefix = "RESULTS  "
		}
		parts := []string{fmt.Sprintf("%s%s  r%d  %s", prefix, identity, record.Revision, record.Title)}
		parts = append(parts, strings.Split(record.Content, "\n")...)
		parts = append(parts, record.At)
		local := 0
		for _, part := range parts {
			for _, wrapped := range wrapDisplay(part, width) {
				lines = append(lines, evidenceLine{recordID: record.ID, local: local, text: wrapped})
				local++
			}
		}
	}
	return lines
}

func displayName(workflow history.Workflow) string {
	if workflow.NameDerived {
		return workflow.Name + " [derived]"
	}
	if workflow.Name != "" {
		return workflow.Name
	}
	return workflow.Goal
}

func formatTime(value string) string {
	value = strings.Replace(value, "T", " ", 1)
	if len(value) >= 16 {
		return value[:16]
	}
	return value
}

func actionLabel(value string) string {
	labels := map[string]string{
		"exploration_recorded": "Recorded exploration", "specification_recorded": "Recorded specification", "design_recorded": "Recorded design",
		"plan_submitted": "Submitted plan", "plan_approved": "Approved plan", "implementation_started": "Started implementation",
		"workflow_created": "Created workflow", "workflow_completed": "Completed workflow", "workflow_abandoned": "Abandoned workflow",
		"unit_claimed": "Claimed work unit", "unit_claim_recovered": "Recovered work unit claim", "unit_completed": "Completed work unit",
		"unit_tdd_recorded": "Recorded TDD", "unit_review_recorded": "Recorded review",
	}
	if label := labels[value]; label != "" {
		return label
	}
	words := strings.Fields(strings.ReplaceAll(value, "_", " "))
	if len(words) > 0 {
		words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	}
	return strings.Join(words, " ")
}

func truncateDisplay(value string, width int) string {
	for lipgloss.Width(value) > width && value != "" {
		value = string([]rune(value)[:len([]rune(value))-1])
	}
	return value
}

func wrapDisplay(text string, width int) []string {
	lines, line, used := []string{}, "", 0
	for _, r := range text {
		cell := lipgloss.Width(string(r))
		if used+cell > width && line != "" {
			lines, line, used = append(lines, line), "", 0
		}
		line += string(r)
		used += cell
	}
	return append(lines, line)
}
