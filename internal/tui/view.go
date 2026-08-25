package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/fmazzalomo/pitcrew/internal/history"
)

const minWidth, minHeight = 60, 16

func (m Model) View() tea.View { view := tea.NewView(m.render()); view.AltScreen = true; return view }

func (m Model) render() string {
	header := m.header()
	if m.width < minWidth || m.height < minHeight {
		return trimTrailing(strings.Join([]string{header, flight.warn.Render("Terminal too small"), "Need at least 60×16; resize to continue.", flight.footer.Render("q quit")}, "\n"))
	}
	body := m.body()
	if m.searchFocused {
		body = flight.focus.Render("SEARCH › ") + m.query + "█\n" + body
	}
	body = clipLines(body, max(1, m.height-2))
	footer := fitText(fmt.Sprintf("%s  ·  %dx%d", m.footerHints(), m.width, m.height), m.width)
	return trimTrailing(strings.Join([]string{header, body, flight.footer.Render(footer)}, "\n"))
}

func (m Model) header() string {
	identity := flight.brand.Render("PitCrew2") + "  " + flight.subtitle.Render("Control Plane") + "  " + flight.version.Render("v"+m.Version())
	return fitStyled(identity, m.width)
}

func (m Model) footerHints() string {
	if m.searchFocused {
		return m.Hints()
	}
	if m.screen == DetailScreen && (m.opened.Record.ID != "" || len(m.opened.Detail.Occurrences) == 0 && len(m.opened.Detail.Records) > 0) {
		current, total := m.detailPosition()
		if m.width < 96 {
			return fmt.Sprintf("j/k • h back • r refresh • q quit • line %d/%d", current, total)
		}
		return fmt.Sprintf("↑/k ↓/j scroll • ←/h back • r refresh • line %d/%d • q quit", current, total)
	}
	if m.screen == DetailScreen {
		current, total := m.detailPosition()
		if m.width < 96 {
			return fmt.Sprintf("j/k • enter • r refresh • q quit • row %d/%d", current, total)
		}
		return fmt.Sprintf("↑/k ↓/j move • pg/home/end • enter evidence • r refresh • row %d/%d • q quit", current, total)
	}
	return m.Hints()
}

func (m Model) body() string {
	hasData := m.hasActiveData()
	if m.loading && (!m.loadPreserves || !hasData) {
		return statePanel("LOADING", "Reading project history…")
	}
	if m.err != nil && (!m.loadPreserves || !hasData) {
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
	var body string
	switch m.screen {
	case DetailScreen:
		body = m.detailView()
	case ResultsScreen:
		body = "SEARCH RESULTS\nquery: " + m.query + "\n" + m.list()
	default:
		body = "WORKFLOWS\n" + m.list()
	}
	if m.loading {
		return flight.muted.Render("REFRESHING · keeping current selection") + "\n" + body
	}
	if m.err != nil {
		return flight.bad.Render("REFRESH FAILED · "+m.err.Error()+" · press r to retry") + "\n" + body
	}
	return body
}

func (m Model) hasActiveData() bool {
	switch m.screen {
	case ResultsScreen:
		return len(m.results) > 0
	case DetailScreen:
		return m.opened.Detail.Workflow.ID != ""
	default:
		return len(m.workflows) > 0
	}
}

func statePanel(title, message string) string {
	return flight.panel.Render(flight.title.Render(title) + "\n\n" + message)
}

func (m Model) list() string {
	var lines []string
	if m.screen == ResultsScreen {
		for i, result := range m.results {
			lines = append(lines, focus(i == m.selected)+fmt.Sprintf("%s · %s\n  %s", result.Kind, result.WorkflowID, result.Context))
		}
		return strings.Join(lines, "\n")
	}
	statusWidth := 0
	for _, workflow := range m.workflows {
		statusWidth = max(statusWidth, lipgloss.Width(statusLabel(workflow.State)))
	}
	workWidth := min(max(18, m.width-44), max(1, m.width-25-statusWidth))
	lines = append(lines, "  Started           │ "+fitText("Work", workWidth)+" │ Status")
	for i, workflow := range m.workflows {
		lines = append(lines, focus(i == m.selected)+fmt.Sprintf("%-17s │ %s │ %s", formatTime(workflow.CreatedAt), fitText(displayName(workflow), workWidth), statusLabel(workflow.State)))
	}
	return strings.Join(lines, "\n")
}

func focus(selected bool) string {
	if selected {
		return flight.focus.Render("▶ ")
	}
	return "  "
}

func (m *Model) detailView() string {
	detail := m.opened.Detail
	if detail.Workflow.ID == "" {
		return "Select a workflow to inspect its evidence."
	}
	m.reconcileDetail()
	lines := m.synopsisLines()
	if m.width >= 80 {
		lines = append(lines, "")
	}
	if m.opened.Record.ID != "" {
		return strings.Join(append(lines, m.recordLines()...), "\n")
	}
	if len(detail.Occurrences) == 0 && len(detail.Records) > 0 {
		return strings.Join(append(lines, m.recordLines()...), "\n")
	}
	lines = append(lines, fmt.Sprintf("%s  %d occurrences", flight.title.Render("HISTORY"), len(detail.Occurrences)))
	if len(detail.Occurrences) == 0 {
		return strings.Join(append(lines, flight.muted.Render("No operational history recorded.")), "\n")
	}
	available := max(1, m.height-2-len(lines))
	return strings.Join(append(lines, m.gridLines(available)...), "\n")
}

func (m Model) synopsisLines() []string {
	d := m.opened.Detail
	w, s := d.Workflow, d.Synopsis
	identity := fmt.Sprintf("%s  %s  r%d", displayName(w), strings.ToUpper(w.State), w.Revision)
	lines := []string{flight.title.Render(ellipsize(identity, m.width)), "Goal  " + ellipsize(w.Goal, max(1, m.width-6))}
	progress := fmt.Sprintf("Progress  %d/%d done", s.Done, s.Total)
	for _, part := range []struct {
		count int
		label string
	}{{s.Ready, "ready"}, {s.Claimed, "claimed"}, {s.Reviewing, "reviewing"}, {s.Correction, "correction"}, {s.DependencyWaiting, "waiting"}, {s.Recovery, "recovery"}} {
		if part.count > 0 {
			progress += fmt.Sprintf(" · %d %s", part.count, part.label)
		}
	}
	if s.Current != nil {
		label := derivedLabel("Current", s.Current.Derived)
		current := fmt.Sprintf("%s  %s · try %d · %s", label, s.Current.Status, s.Current.Attempt, s.Current.Description)
		if m.width < 80 {
			progress += "  |  " + current
		} else {
			lines = append(lines, fitText(progress, m.width), fitText(current, m.width))
		}
	}
	if s.Current == nil || m.width < 80 {
		lines = append(lines, fitText(progress, m.width))
	}
	if s.Blocker != nil {
		lines = append(lines, flight.bad.Render(fitText(derivedLabel("Blocked", s.Blocker.Derived)+"  "+s.Blocker.Reason, m.width)))
	}
	lines = append(lines, fitText("Next  "+zeroDash(s.NextAction), m.width))
	return lines
}

func (m Model) gridLines(available int) []string {
	occurrences := m.opened.Detail.Occurrences
	start := max(0, min(m.detail.occurrenceTop, len(occurrences)-1))
	lines := []string{}
	if m.width >= 80 {
		lines = append(lines, m.gridRow(history.Occurrence{}, false, true))
	}
	for i := start; i < len(occurrences); i++ {
		parts := strings.Split(m.gridRow(occurrences[i], occurrences[i].ID == m.detail.occurrenceID, false), "\n")
		if len(lines)+len(parts) > available {
			break
		}
		lines = append(lines, parts...)
	}
	return lines
}

func (m Model) gridRow(o history.Occurrence, selected, header bool) string {
	if m.width < 80 {
		when, phase, attempt, outcome := compactTime(o.At), o.Phase, attemptLabel(o.Attempt), outcomeText(o)
		first := focus(selected) + fitText(when, 5) + " │ " + fitText(phase, 8) + " │ " + fitText(attempt, 3) + " │ " + ellipsize(outcome, max(1, m.width-26))
		second := "  ↳ " + ellipsize(zeroDash(o.Work)+" · "+actionLabel(o.Activity), max(1, m.width-4))
		return fitStyled(first, m.width) + "\n" + fitStyled(second, m.width)
	}
	actor := m.width >= 120
	whenWidth, phaseWidth, tryWidth, activityWidth, actorWidth := 16, 9, 3, 22, 14
	if m.width >= 166 {
		actorWidth = 20
	}
	if m.width < 96 {
		whenWidth, phaseWidth, activityWidth = 5, 6, 13
	}
	columnCount, fixed := 6, whenWidth+phaseWidth+tryWidth+activityWidth
	if actor {
		columnCount++
		fixed += actorWidth
	}
	flex := max(12, m.width-2-fixed-3*(columnCount-1))
	workWidth := flex / 2
	outcomeWidth := flex - workWidth
	values := []string{formatTime(o.At), o.Phase, o.Work, attemptLabel(o.Attempt), actionLabel(o.Activity)}
	widths := []int{whenWidth, phaseWidth, workWidth, tryWidth, activityWidth}
	if m.width < 96 {
		values[0] = compactTime(o.At)
	}
	if actor {
		values, widths = append(values, o.Actor), append(widths, actorWidth)
	}
	values, widths = append(values, outcomeText(o)), append(widths, outcomeWidth)
	if header {
		values = []string{"When", "Phase", "Work", "Try", "Activity"}
		if actor {
			values = append(values, "Actor")
		}
		values = append(values, "Outcome / reason")
	}
	for i := range values {
		values[i] = fitText(zeroDash(values[i]), widths[i])
	}
	return fitStyled(focus(selected)+strings.Join(values, " │ "), m.width)
}

func (m Model) recordLines() []string {
	lines := []string{flight.title.Render("EVIDENCE") + "  " + zeroDash(m.opened.Record.Kind)}
	lines = append(lines, m.typedMetadataLines()...)
	layout := m.evidenceLines()
	current, _ := m.detailPosition()
	end := min(len(layout), m.detail.top+m.evidenceHeight())
	for i := m.detail.top; i < end; i++ {
		lines = append(lines, focus(i == current-1)+layout[i].text)
	}
	return lines
}

func (m Model) typedMetadataLines() []string {
	record := m.opened.Record
	occurrence, found := m.focusedOccurrence()
	actor := record.Actor
	if actor == "" && found {
		actor = occurrence.Actor
	}
	items := []string{"Actor " + zeroDash(actor), "Record " + zeroDash(record.ID)}
	if record.UnitID != "" {
		items = append(items, "Unit "+record.UnitID)
	}
	if record.Revision > 0 {
		items = append(items, fmt.Sprintf("Revision %d", record.Revision))
	}
	if found && occurrence.Attempt != nil {
		items = append(items, fmt.Sprintf("Try %d", *occurrence.Attempt))
	}
	legacy := found && occurrence.Legacy || strings.HasPrefix(m.detail.occurrenceID, "legacy:")
	if legacy {
		items = append(items, flight.muted.Render("[legacy]"))
	}
	var lines []string
	for _, item := range items {
		if len(lines) == 0 || lipgloss.Width(lines[len(lines)-1]+" · "+item) > m.width {
			lines = append(lines, ellipsize(item, m.width))
		} else {
			lines[len(lines)-1] += " · " + item
		}
	}
	return lines
}

type evidenceLine struct {
	recordID string
	local    int
	text     string
	activity *history.Activity
}

func (m Model) evidenceLines() []evidenceLine {
	return evidenceLayout(m.opened, max(1, m.evidenceWidth()-2))
}
func (m Model) evidenceWidth() int { return max(1, m.width) }
func (m Model) evidenceHeight() int {
	return max(1, m.height-len(m.synopsisLines())-len(m.typedMetadataLines())-6)
}

func evidenceLayout(resolution history.Resolution, width int) []evidenceLine {
	records := resolution.Detail.Records
	if resolution.Record.ID != "" {
		records = []history.Record{resolution.Record}
	}
	var lines []evidenceLine
	for _, record := range records {
		parts := []string{record.Title}
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
func compactTime(value string) string {
	formatted := formatTime(value)
	if len(formatted) >= 16 {
		return formatted[11:16]
	}
	return formatted
}

func actionLabel(value string) string {
	labels := map[string]string{
		"exploration_recorded": "Recorded exploration", "specification_recorded": "Recorded specification", "design_recorded": "Recorded design", "plan_submitted": "Submitted plan", "plan_approved": "Approved plan", "implementation_started": "Started implementation", "workflow_created": "Created workflow", "workflow_completed": "Completed workflow", "workflow_abandoned": "Abandoned workflow", "unit_claimed": "Claimed work unit", "unit_claim_recovered": "Recovered work unit claim", "unit_completed": "Completed work unit", "unit_tdd_recorded": "Recorded TDD", "unit_review_recorded": "Recorded review", "aggregate_review_recorded": "Recorded aggregate review",
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

func outcomeText(o history.Occurrence) string {
	if o.Reason == "" {
		return zeroDash(o.Outcome)
	}
	return zeroDash(o.Outcome) + ": " + o.Reason
}
func attemptLabel(attempt *int64) string {
	if attempt == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *attempt)
}
func zeroDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}
func derivedLabel(label string, derived bool) string {
	if derived {
		return label + " [derived]"
	}
	return label
}

func fitText(value string, width int) string {
	value = ellipsize(value, width)
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}
func fitStyled(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	return ellipsize(value, width)
}
func ellipsize(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	result := ""
	for _, r := range value {
		if lipgloss.Width(result+string(r)+"…") > width {
			break
		}
		result += string(r)
	}
	return result + "…"
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
func clipLines(value string, height int) string {
	lines := strings.Split(value, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}
func trimTrailing(value string) string {
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}
