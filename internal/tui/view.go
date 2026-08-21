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

func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}

func (m Model) render() string {
	if m.width < minWidth || m.height < minHeight {
		return flight.header.Render("PITCREW  WORKFLOW FLIGHT RECORDER") + "\n\n" +
			flight.warn.Render("Terminal too small") + "\nNeed at least 60×16; resize to continue.\n\n" + flight.footer.Render("q quit")
	}
	mode := "SINGLE PANE"
	if m.width >= wideWidth {
		mode = "MULTI PANE"
	}
	header := flight.header.Render("PITCREW  WORKFLOW FLIGHT RECORDER") + "  " + flight.mode.Render(mode)
	body := m.body(mode == "MULTI PANE")
	if m.searchFocused {
		body = flight.focus.Render("SEARCH › ") + m.query + "█\n" + body
	}
	footer := flight.footer.Render(fmt.Sprintf("%s  ·  %dx%d", m.footerHints(), m.width, m.height))
	return strings.Join([]string{header, body, footer}, "\n")
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
		left := flight.panel.Width(30).MaxHeight(m.height - 3).Render("STATUS RAIL\n" + m.rail())
		right := flight.panel.Width(max(40, m.width-39)).Render("EVIDENCE CANVAS\n" + m.canvas())
		return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	}
	panel := flight.panel.Width(max(40, m.width-4))
	if m.screen != DetailScreen {
		panel = panel.MaxHeight(m.height - 3)
	}
	return panel.Render(m.singlePane())
}

func statePanel(title, message string) string {
	return flight.panel.Render(flight.title.Render(title) + "\n\n" + message)
}

func (m Model) rail() string {
	if m.screen == DetailScreen {
		w := m.opened.Detail.Workflow
		return fmt.Sprintf("%s\n%s\nrevision %d\n\nEXPLORE → SPEC → DESIGN\nPLAN → BUILD → REVIEW", statusLabel(w.State), w.ID, w.Revision)
	}
	return m.list()
}

func (m Model) singlePane() string {
	switch m.screen {
	case DetailScreen:
		return "EVIDENCE CANVAS\n" + m.canvas()
	case ResultsScreen:
		return "SEARCH RESULTS\nquery: " + m.query + "\n" + m.list()
	default:
		return "STATUS RAIL\n" + m.list()
	}
}

func (m Model) list() string {
	var lines []string
	if m.screen == ResultsScreen {
		for i, result := range m.results {
			lines = append(lines, focus(i == m.selected)+fmt.Sprintf("%s · %s\n  %s", result.Kind, result.WorkflowID, result.Context))
		}
	} else {
		for i, workflow := range m.workflows {
			lines = append(lines, focus(i == m.selected)+fmt.Sprintf("%s %s r%d\n  %s", statusLabel(workflow.State), workflow.ID, workflow.Revision, workflow.Goal))
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
	lines := []string{flight.title.Render(detail.Workflow.Goal), statusLabel(detail.Workflow.State) + fmt.Sprintf("  %s  revision %d", detail.Workflow.ID, detail.Workflow.Revision), ""}
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
}

func (m Model) evidenceLines() []evidenceLine {
	return evidenceLayout(m.opened.Detail.Records, max(1, m.evidenceWidth()-2))
}

func (m Model) evidenceWidth() int {
	if m.width >= wideWidth {
		return max(40, m.width-39)
	}
	return max(40, m.width-4)
}

func (m Model) evidenceHeight() int { return max(1, m.height-8) }

func evidenceLayout(records []history.Record, width int) []evidenceLine {
	var lines []evidenceLine
	for _, record := range records {
		identity := record.Kind
		if record.UnitID != "" {
			identity += " · " + record.UnitID
		}
		parts := []string{fmt.Sprintf("%s  r%d  %s", identity, record.Revision, record.Title)}
		parts = append(parts, strings.Split(record.Content, "\n")...)
		parts = append(parts, record.At)
		local := 0
		for _, part := range parts {
			for _, wrapped := range wrapDisplay(part, width) {
				lines = append(lines, evidenceLine{record.ID, local, wrapped})
				local++
			}
		}
	}
	return lines
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
