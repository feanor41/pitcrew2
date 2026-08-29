package tui

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/fmazzalomo/pitcrew/internal/history"
)

const minWidth, minHeight = 60, 16

func (m Model) View() tea.View {
	previous := flight
	noColor := noColorEnabled()
	if noColor {
		flight = newFlightStyles(true)
	}
	defer func() { flight = previous }()
	content := m.render()
	if noColor {
		content = ansi.Strip(content)
	}
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

func (m Model) render() string {
	if m.width < minWidth || m.height < minHeight {
		header := m.compactHeader()
		guidance := strings.Join([]string{
			header,
			flight.warn.Render(ellipsize("Terminal too small", m.width)),
			ellipsize("Need at least 60×16; resize to continue.", m.width),
			flight.footer.Render(ellipsize("q quit", m.width)),
		}, "\n")
		return trimTrailing(clipLines(guidance, max(1, m.height)))
	}
	header := m.header()
	body := m.body()
	if m.searchFocused {
		body = flight.focus.Render("SEARCH › ") + m.search.View() + "\n" + body
	}
	bodyHeight := max(1, m.height-6)
	if m.wideDetailMode() && !m.searchFocused {
		bodyHeight = max(1, m.height-5)
	}
	body = clipLines(body, bodyHeight)
	footer := fitText(fmt.Sprintf("%s  ·  %dx%d", m.footerHints(), m.width, m.height), m.width)
	if m.wideDetailMode() && !m.searchFocused {
		return trimTrailing(strings.Join([]string{header, body, flight.footer.Render(footer)}, "\n"))
	}
	return trimTrailing(strings.Join([]string{header, "", body, flight.footer.Render(footer)}, "\n"))
}

func (m Model) header() string {
	width := max(4, m.width)
	contentWidth := width - 4
	brand := flight.brand.Render(ellipsize("PitCrew2", contentWidth))
	version := flight.version.Render("v" + m.Version())
	if lipgloss.Width(version) > contentWidth {
		version = flight.version.Render(ellipsize("v"+m.Version(), contentWidth))
	}
	brandWidth, versionWidth := lipgloss.Width(brand), lipgloss.Width(version)
	if brandWidth+versionWidth > contentWidth {
		brand = flight.brand.Render(ellipsize("PitCrew2", max(0, contentWidth-versionWidth)))
		brandWidth = lipgloss.Width(brand)
	}
	first := brand + strings.Repeat(" ", max(0, contentWidth-brandWidth-versionWidth)) + version
	second := flight.subtitle.Render(ellipsize("Control Plane", contentWidth))
	line := func(value string) string {
		return flight.border.Render("│") + " " + fitText(value, contentWidth) + " " + flight.border.Render("│")
	}
	return strings.Join([]string{
		flight.border.Render("╭" + strings.Repeat("─", width-2) + "╮"),
		line(first),
		line(second),
		flight.border.Render("╰" + strings.Repeat("─", width-2) + "╯"),
	}, "\n")
}

func (m Model) compactHeader() string {
	identity := flight.brand.Render("PitCrew2") + "  " + flight.subtitle.Render("Control Plane") + "  " + flight.version.Render("v"+m.Version())
	return fitStyled(identity, m.width)
}

func (m Model) footerHints() string {
	if m.searchFocused {
		return m.Hints()
	}
	if m.screen == DetailScreen && (m.opened.Record.ID != "" || len(m.opened.Detail.Occurrences) == 0 && len(m.opened.Detail.Records) > 0) {
		current, total := m.detailViewPosition()
		if m.width < 96 {
			return fmt.Sprintf("j/k • h back • r refresh • q quit • line %d/%d", current, total)
		}
		return fmt.Sprintf("↑/k ↓/j scroll • ←/h back • r refresh • line %d/%d • q quit", current, total)
	}
	if m.relatedRecordMode() {
		current, total := m.detailPosition()
		if m.width < 96 {
			return fmt.Sprintf("j/k • h back • enter • r refresh • q quit • record %d/%d", current, total)
		}
		return fmt.Sprintf("↑/k ↓/j move • ←/h back • enter evidence • r refresh • record %d/%d • q quit", current, total)
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
	if m.screen == HomeScreen {
		return m.homeView()
	}
	if m.screen == WorkflowsScreen {
		return m.workflowsView()
	}
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
	var body string
	switch m.screen {
	case DetailScreen:
		body = m.detailView()
	case ResultsScreen:
		body = flight.title.Render("SEARCH RESULTS") + "\n" + labeled("Query", m.query) + "\n" + m.list()
	}
	if m.loading {
		return flight.muted.Render("REFRESHING · keeping current selection") + "\n" + body
	}
	if m.err != nil {
		return flight.bad.Render("REFRESH FAILED · "+m.err.Error()+" · press r to retry") + "\n" + body
	}
	return body
}

func (m Model) workflowsView() string {
	lines := []string{flight.title.Render("WORKFLOWS")}
	hasData := len(m.workflows) > 0
	if m.loading && (!m.loadPreserves || !hasData) {
		return strings.Join(append(lines, "Loading workflows…"), "\n")
	}
	if m.err != nil && (!m.loadPreserves || !hasData) {
		return strings.Join(append(lines, "Could not load workflows.", ellipsize(m.err.Error(), m.width)), "\n")
	}
	if !hasData {
		return strings.Join(append(lines, "No workflows are available."), "\n")
	}
	if m.loading {
		lines = append(lines, flight.muted.Render("REFRESHING · keeping current selection"))
	} else if m.err != nil {
		lines = append(lines, flight.bad.Render(ellipsize("REFRESH FAILED · "+m.err.Error()+" · press r to retry", m.width)))
	}
	gridHeight := max(4, m.height-6-len(lines))
	return strings.Join(append(lines, m.workflowGrid(gridHeight)), "\n")
}

func (m Model) workflowGrid(height int) string {
	startedWidth, statusWidth := 19, 11
	workflowWidth := max(1, m.width-40)
	horizontal := func(left, middle, right string) string {
		return flight.border.Render(left + strings.Repeat("─", startedWidth+2) + middle + strings.Repeat("─", workflowWidth+2) + middle + strings.Repeat("─", statusWidth+2) + right)
	}
	row := func(started, workflow, status string) string {
		return flight.border.Render("│") + " " + started + " " + flight.border.Render("│") + " " + workflow + " " + flight.border.Render("│") + " " + status + " " + flight.border.Render("│")
	}
	lines := []string{
		horizontal("╭", "┬", "╮"),
		row(fitLabel("Started", startedWidth), fitLabel("Workflow", workflowWidth), fitLabel("Status", statusWidth)),
		horizontal("├", "┼", "┤"),
	}
	visible := max(0, height-4)
	start := max(0, min(m.workflowTop, len(m.workflows)))
	end := min(len(m.workflows), start+visible)
	for i := start; i < end; i++ {
		workflow := m.workflows[i]
		started := focus(i == m.selected) + fitText(formatTime(workflow.CreatedAt), 17)
		lines = append(lines, row(started, fitText(displayName(workflow), workflowWidth), fitStatus(workflow.State, statusWidth)))
	}
	lines = append(lines, horizontal("╰", "┴", "╯"))
	return strings.Join(lines, "\n")
}

func (m Model) homeView() string {
	lines := make([]string, 0, len(homeActions)+2)
	for i, label := range homeActions {
		lines = append(lines, fitStyled(focus(i == m.homeSelected)+label, m.width))
	}
	if m.homeNotice != "" {
		lines = append(lines, "")
		for _, line := range wrapDisplay(m.homeNotice, max(1, m.width)) {
			lines = append(lines, flight.warn.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) detailViewPosition() (int, int) {
	if m.opened.Record.ID != "" && m.componentsReady {
		total := m.viewport.TotalLineCount()
		if total == 0 {
			return 0, 0
		}
		return min(total, m.viewport.YOffset()+m.viewport.VisibleLineCount()), total
	}
	return m.detailPosition()
}

func (m Model) hasActiveData() bool {
	switch m.screen {
	case HomeScreen:
		return true
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
	lines = append(lines, "  "+fitLabel("Started", 17)+" │ "+fitLabel("Work", workWidth)+" │ "+fitLabel("Status", statusWidth))
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
	if m.wideDetailMode() {
		return m.wideDetailView()
	}
	if m.opened.Record.ID != "" {
		return strings.Join(m.recordLines(), "\n")
	}
	if len(detail.Occurrences) == 0 && len(detail.Records) > 0 {
		return strings.Join(m.recordLines(), "\n")
	}
	lines := m.operationalHeaderLines()
	available := max(1, m.height-6-len(lines))
	unitRows := 2
	if m.height >= 24 {
		unitRows = 4
	}
	units := m.unitProgressLines(unitRows)
	if len(units) > available-2 {
		units = units[:max(0, available-2)]
	}
	lines = append(lines, units...)
	lines = append(lines, fmt.Sprintf("%s  %d events", flight.title.Render("TIMELINE"), len(detail.Occurrences)))
	if len(detail.Occurrences) == 0 {
		return strings.Join(append(lines, flight.muted.Render("No operational history recorded.")), "\n")
	}
	available = max(1, m.height-6-len(lines))
	return strings.Join(append(lines, m.gridLines(available)...), "\n")
}

func (m Model) wideDetailMode() bool {
	return m.screen == DetailScreen && m.opened.Record.ID == "" && m.opened.Detail.Workflow.ID != "" && m.width >= 120 && m.height >= 30
}

func (m Model) wideDetailView() string {
	lines := m.operationalHeaderLines()
	available := max(3, m.height-5-len(lines))
	widths := resize(m.width-1, 2, 3)
	unitModel, timelineModel := m, m
	unitModel.width, timelineModel.width = widths[0], widths[1]
	left := unitModel.unitProgressLines(available)
	right := append([]string{fmt.Sprintf("%s  %d events", flight.title.Render("TIMELINE"), len(m.opened.Detail.Occurrences))}, timelineModel.gridLines(max(1, available-1))...)
	left = wideFill(left, available, widths[0])
	right = wideFill(right, available, widths[1])
	for i := range available {
		lines = append(lines, left[i]+flight.border.Render("│")+right[i])
	}
	return strings.Join(lines, "\n")
}

func (m Model) operationalHeaderLines() []string {
	w, s := m.opened.Detail.Workflow, m.opened.Detail.Synopsis
	identity := fmt.Sprintf("%s  ·  %s  ·  r%d", displayName(w), strings.ToUpper(w.State), w.Revision)
	done, total, percent := s.Done, s.Total, 0
	if s.Planned != nil {
		done, total, percent = s.Planned.Done, s.Planned.Total, s.Planned.Percent
	} else if total > 0 {
		percent = done * 100 / total
	}
	current := "No active unit"
	if s.Current != nil {
		current = fmt.Sprintf("%s · %s", s.Current.Status, zeroDash(s.Current.Description))
	}
	lines := []string{
		fitStyled(flight.title.Render(identity), m.width),
		labeledFit("PROGRESS", fmt.Sprintf("%d/%d done · %d%%", done, total, percent), m.width),
		labeledFit("NOW", current, m.width),
		labeledFit("NEXT", zeroDash(s.NextAction), m.width),
	}
	if s.Blocker != nil {
		lines = append(lines, flight.bad.Render(labeledFit("BLOCKED", zeroDash(s.Blocker.Reason), m.width)))
	}
	lines = append(lines, fitStyled(flight.label.Render("STAGES")+"  "+m.stageRail(), m.width))
	if m.height >= 24 {
		lines = append(lines, labeledFit("GOAL", zeroDash(w.Goal), m.width))
	}
	return lines
}

func (m Model) stageRail() string {
	stages := []string{"Explore", "Spec", "Design", "Plan", "Build", "Review"}
	short := []string{"Ex", "Sp", "De", "Pl", "Bu", "Re"}
	active := lifecyclePosition(m.opened.Detail.Workflow.State, m.opened.Detail.Occurrences)
	completed := completedLifecycleStages(m.opened.Detail.Workflow.State, m.opened.Detail.Occurrences)
	parts := make([]string, len(stages))
	for i, stage := range stages {
		symbol := "○"
		if completed[i] {
			symbol = "✓"
		} else if i == active {
			symbol = "●"
		}
		if m.opened.Detail.Workflow.State == "abandoned" && i == active {
			symbol = "!"
		}
		label := stage
		if m.width < 80 {
			label = short[i]
		}
		parts[i] = symbol + " " + label
	}
	separator := "  "
	if m.width >= 80 {
		separator = " ─ "
	}
	return strings.Join(parts, separator)
}

func lifecyclePosition(state string, occurrences []history.Occurrence) int {
	positions := map[string]int{"draft": 0, "exploring": 0, "specifying": 1, "designing": 2, "planning": 3, "plan_approved": 4, "implementing": 4, "ready_to_complete": 5}
	if state == "completed" {
		return -1
	}
	if position, ok := positions[state]; ok {
		return position
	}
	phasePositions := map[string]int{"explore": 0, "specify": 1, "spec": 1, "design": 2, "plan": 3, "implement": 4, "build": 4, "review": 5}
	active := 0
	for _, occurrence := range occurrences {
		if position, ok := phasePositions[strings.ToLower(occurrence.Phase)]; ok && position > active {
			active = position
		}
	}
	return active
}

func completedLifecycleStages(state string, occurrences []history.Occurrence) [6]bool {
	var completed [6]bool
	completionActions := map[string]int{
		"exploration_recorded":   0,
		"specification_recorded": 1,
		"design_recorded":        2,
		"plan_approved":          3,
		"workflow_completed":     5,
	}
	for _, occurrence := range occurrences {
		if stage, ok := completionActions[occurrence.Activity]; ok {
			completed[stage] = true
		}
		if occurrence.Activity == "unit_completed" && (state == "ready_to_complete" || state == "completed") {
			completed[4] = true
		}
		if occurrence.Activity == "aggregate_review_recorded" && occurrence.Outcome == "Approved" {
			completed[5] = true
		}
	}
	return completed
}

func (m Model) unitProgressLines(limit int) []string {
	s := m.opened.Detail.Synopsis
	done, total := s.Done, s.Total
	units := []history.UnitStatus{}
	if s.Planned != nil {
		done, total = s.Planned.Done, s.Planned.Total
		units = append(units, s.Planned.Units...)
		if len(units) == 0 {
			units = append(units, s.Planned.Pending...)
		}
	}
	if len(units) == 0 && s.Current != nil {
		units = append(units, *s.Current)
	}
	lines := []string{fmt.Sprintf("%s  %d/%d done", flight.title.Render("UNITS"), done, total)}
	capacity := max(0, limit-1)
	visible := min(len(units), capacity)
	showIndicator := len(units) > capacity
	if showIndicator && capacity > 1 {
		visible--
	} else if showIndicator && capacity == 1 {
		visible = 0
	}
	for _, unit := range units[:visible] {
		line := unitSymbol(unit.Status) + " " + zeroDash(unit.Description) + " · " + unit.Status
		if unit.Reason != "" {
			line += " · " + unit.Reason
		}
		lines = append(lines, fitStyled(line, max(1, m.width)))
	}
	if showIndicator && capacity > 0 {
		more := len(units) - visible
		indicator := fmt.Sprintf("+%d more", more)
		if capacity == 1 && len(units) > 0 {
			more = len(units) - 1
			indicator = unitSymbol(units[0].Status) + " " + zeroDash(units[0].Description)
			if more > 0 {
				indicator += fmt.Sprintf(" · +%d more", more)
			}
		}
		lines = append(lines, fitStyled(indicator, max(1, m.width)))
	}
	return lines
}

func unitSymbol(status string) string {
	switch status {
	case "Done":
		return "✓"
	case "Correction", "Recovery", "Dependency waiting":
		return "!"
	case "Claimed", "Reviewing":
		return "●"
	case "Ready":
		return "›"
	default:
		return "○"
	}
}

// resize distributes a fixed terminal dimension by relative panel weights.
func resize(total int, weights ...int) []int {
	result := make([]int, len(weights))
	weightTotal := 0
	for _, weight := range weights {
		weightTotal += weight
	}
	if total <= 0 || weightTotal == 0 {
		return result
	}
	used := 0
	for i, weight := range weights {
		result[i] = total * weight / weightTotal
		used += result[i]
	}
	for i := 0; used < total; i, used = (i+1)%len(result), used+1 {
		result[i]++
	}
	return result
}

func wideFill(lines []string, height, width int) []string {
	height, width = max(0, height), max(0, width)
	filled := make([]string, 0, height)
	for _, line := range lines {
		if len(filled) == height {
			break
		}
		// Frame contents are logical lines. ansi.Truncate is cell-aware and keeps
		// semantic styling sequences intact while truncating Unicode safely.
		line = ansi.Truncate(strings.ReplaceAll(line, "\n", " "), width, "…")
		filled = append(filled, line+strings.Repeat(" ", max(0, width-lipgloss.Width(line))))
	}
	for len(filled) < height {
		filled = append(filled, strings.Repeat(" ", width))
	}
	return filled
}

func (m Model) gridLines(available int) []string {
	occurrences := m.opened.Detail.Occurrences
	start := max(0, min(m.detail.occurrenceTop, len(occurrences)-1))
	selectedIndex := -1
	for i, occurrence := range occurrences {
		if occurrence.ID == m.detail.occurrenceID {
			selectedIndex = i
			break
		}
	}
	if selectedIndex >= 0 {
		start = min(start, selectedIndex)
		for selectedIndex-start+2 > available && start < selectedIndex {
			start++
		}
	}
	lines := []string{}
	for i := start; i < len(occurrences); i++ {
		selected := occurrences[i].ID == m.detail.occurrenceID
		parts := m.semanticOccurrenceLines(occurrences[i], selected)
		if len(lines)+len(parts) > available {
			if len(lines) == 0 && selected {
				lines = append(lines, parts[:min(len(parts), available)]...)
			}
			break
		}
		lines = append(lines, parts...)
	}
	return lines
}

func (m Model) semanticOccurrenceLines(occurrence history.Occurrence, selected bool) []string {
	when := formatTime(occurrence.At)
	if m.width < 80 {
		when = compactTime(occurrence.At)
	}
	occurrenceFocused := selected && !(m.relatedRecordMode() && occurrence.ID == m.detail.occurrenceID)
	meaning := zeroDash(occurrence.Work)
	outcome := outcomeText(occurrence)
	if outcome != "" {
		if m.width < 80 {
			meaning = outcome + " · " + meaning
		} else {
			meaning += " · " + outcome
		}
	}
	primary := focus(occurrenceFocused) + when + "  " + zeroDash(occurrence.Phase) + " · " + actionLabel(occurrence.Activity) + " — " + meaning
	lines := []string{fitStyled(primary, m.width)}
	if selected {
		if m.relatedRecordMode() && occurrence.ID == m.detail.occurrenceID {
			available := max(1, m.occurrenceAvailableLines()-len(lines))
			lines = append(lines, m.relatedRecordLines(occurrence, available)...)
		} else {
			context := ""
			if occurrence.Actor != "" {
				context = "actor " + occurrence.Actor
			}
			if occurrence.Attempt != nil {
				context += fmt.Sprintf(" · try %d", *occurrence.Attempt)
			}
			if occurrence.Reason != "" {
				context += " · " + occurrence.Reason
			}
			if context != "" {
				lines = append(lines, fitText("    "+context, m.width))
			}
		}
	}
	return lines
}

func (m Model) relatedRecordLines(occurrence history.Occurrence, available int) []string {
	ids := m.supportingRecordIDs(occurrence)
	if len(ids) == 0 {
		return nil
	}
	selected := 0
	for i, id := range ids {
		if id == m.detail.relatedRecordID {
			selected = i
			break
		}
	}
	row := func(index int) string {
		id := ids[index]
		title, metadata := id, []string{}
		for _, record := range m.opened.Detail.Records {
			if record.ID != id {
				continue
			}
			if strings.TrimSpace(record.Title) != "" {
				title = record.Title
			}
			if record.Kind != "" {
				metadata = append(metadata, record.Kind)
			}
			break
		}
		if title != id {
			metadata = append(metadata, id)
		}
		line := focus(index == selected) + title
		if len(metadata) > 0 {
			line += " · " + strings.Join(metadata, " · ")
		}
		return fitStyled(line, m.width)
	}
	if available <= 1 {
		return []string{row(selected)}
	}
	visible := min(len(ids), available-1)
	start := max(0, min(selected, len(ids)-visible))
	lines := []string{fmt.Sprintf("%s  %d", flight.title.Render("RELATED RECORDS"), len(ids))}
	for i := start; i < start+visible; i++ {
		lines = append(lines, row(i))
	}
	return lines
}

func (m Model) recordLines() []string {
	lines := []string{flight.title.Render("EVIDENCE") + "  " + zeroDash(m.opened.Record.Kind)}
	lines = append(lines, m.typedMetadataLines()...)
	if m.opened.Record.ID == "" {
		layout := m.evidenceLines()
		current, _ := m.detailPosition()
		end := min(len(layout), m.detail.top+m.evidenceHeight())
		for i := m.detail.top; i < end; i++ {
			lines = append(lines, focus(i == current-1)+layout[i].text)
		}
		return lines
	}
	if body := m.viewport.View(); body != "" {
		if noColorEnabled() {
			body = ansi.Strip(body)
		}
		lines = append(lines, strings.Split(strings.TrimRight(body, "\n"), "\n")...)
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
	items := []struct{ label, value string }{{"Actor", zeroDash(actor)}, {"Record", zeroDash(record.ID)}}
	if record.UnitID != "" {
		items = append(items, struct{ label, value string }{"Unit", record.UnitID})
	}
	if record.Revision > 0 {
		items = append(items, struct{ label, value string }{"Revision", fmt.Sprintf("%d", record.Revision)})
	}
	if found && occurrence.Attempt != nil {
		items = append(items, struct{ label, value string }{"Try", fmt.Sprintf("%d", *occurrence.Attempt)})
	}
	legacy := found && occurrence.Legacy || strings.HasPrefix(m.detail.occurrenceID, "legacy:")
	if legacy {
		items = append(items, struct{ label, value string }{"", flight.muted.Render("[legacy]")})
	}
	var lines []string
	for _, item := range items {
		rendered := item.value
		if item.label != "" {
			rendered = labeled(item.label, item.value)
		}
		if len(lines) == 0 || lipgloss.Width(lines[len(lines)-1]+" · "+rendered) > m.width {
			lines = append(lines, ellipsize(rendered, m.width))
		} else {
			lines[len(lines)-1] += " · " + rendered
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
	return max(1, m.height-len(m.typedMetadataLines())-7)
}

func labeled(label, value string) string { return flight.label.Render(label) + "  " + value }

func labeledFit(label, value string, width int) string {
	prefix := flight.label.Render(label) + "  "
	return prefix + ellipsize(value, max(1, width-lipgloss.Width(prefix)))
}

func fitLabel(value string, width int) string {
	value = ellipsize(value, width)
	return flight.label.Render(value) + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
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
	outcome, reason := strings.TrimSpace(o.Outcome), strings.TrimSpace(o.Reason)
	if semanticallyEquivalent(outcome, actionLabel(o.Activity)) {
		return reason
	}
	if reason == "" {
		return zeroDash(outcome)
	}
	if outcome == "" {
		return reason
	}
	return outcome + ": " + reason
}

func semanticallyEquivalent(left, right string) bool {
	leftWords, rightWords := semanticWords(left), semanticWords(right)
	if len(leftWords) == 0 || len(leftWords) != len(rightWords) {
		return false
	}
	for word, count := range leftWords {
		if rightWords[word] != count {
			return false
		}
	}
	return true
}

func semanticWords(value string) map[string]int {
	words := map[string]int{}
	for _, word := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		words[word]++
	}
	return words
}
func zeroDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
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
