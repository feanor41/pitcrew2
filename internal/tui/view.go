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
	body = clipLines(body, max(1, m.height-6))
	footer := fitText(fmt.Sprintf("%s  ·  %dx%d", m.footerHints(), m.width, m.height), m.width)
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
	pending := m.pendingWorkLines()
	if m.height <= minHeight && len(pending) > 2 {
		pending = pending[:2]
	}
	lines = append(lines, pending...)
	lines = append(lines, fmt.Sprintf("%s  %d occurrences", flight.title.Render("HISTORY"), len(detail.Occurrences)))
	if len(detail.Occurrences) == 0 {
		return strings.Join(append(lines, flight.muted.Render("No operational history recorded.")), "\n")
	}
	available := max(1, m.height-6-len(lines))
	return strings.Join(append(lines, m.gridLines(available)...), "\n")
}

func (m Model) synopsisLines() []string {
	d := m.opened.Detail
	w, s := d.Workflow, d.Synopsis
	identity := fmt.Sprintf("%s  %s  r%d", displayName(w), strings.ToUpper(w.State), w.Revision)
	lines := []string{flight.title.Render(ellipsize(identity, m.width)), labeledFit("Goal", w.Goal, m.width)}
	progress := ""
	if s.Planned != nil {
		progress = fmt.Sprintf("%d/%d planned · %d%%", s.Planned.Done, s.Planned.Total, s.Planned.Percent)
	} else if s.PlanNotice != "" {
		progress = s.PlanNotice
	} else {
		progress = fmt.Sprintf("%d/%d done", s.Done, s.Total)
		for _, part := range []struct {
			count int
			label string
		}{{s.Ready, "ready"}, {s.Claimed, "claimed"}, {s.Reviewing, "reviewing"}, {s.Correction, "correction"}, {s.DependencyWaiting, "waiting"}, {s.Recovery, "recovery"}} {
			if part.count > 0 {
				progress += fmt.Sprintf(" · %d %s", part.count, part.label)
			}
		}
	}
	if s.Current != nil && s.Planned == nil {
		label := derivedLabel("Current", s.Current.Derived)
		current := fmt.Sprintf("%s  %s · try %d · %s", label, s.Current.Status, s.Current.Attempt, s.Current.Description)
		if m.width < 80 {
			progress += "  |  " + current
		} else {
			lines = append(lines, labeledFit("Progress", strings.TrimPrefix(progress, "Progress  "), m.width), labeledFit(label, strings.TrimPrefix(current, label+"  "), m.width))
		}
	}
	if s.Current == nil || s.Planned != nil || m.width < 80 {
		lines = append(lines, labeledFit("Progress", progress, m.width))
	}
	if s.Blocker != nil {
		label := derivedLabel("Blocked", s.Blocker.Derived)
		lines = append(lines, flight.bad.Render(labeledFit(label, s.Blocker.Reason, m.width)))
	}
	if s.Progress != nil {
		marker, style := "[ADVANCED]", flight.good
		if s.Progress.Status == "blocked" {
			marker, style = "[BLOCKED]", flight.bad
		}
		prefix := "Report " + marker + " "
		lines = append(lines, flight.label.Render("Report")+" "+style.Render(marker)+" "+ellipsize(s.Progress.Summary, max(1, m.width-lipgloss.Width(prefix))))
		lines = append(lines, labeledFit("Report next", s.Progress.NextAction, m.width))
	}
	lines = append(lines, labeledFit("Next", zeroDash(s.NextAction), m.width))
	return lines
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
	if selectedIndex >= start {
		used := len(m.semanticOccurrenceLines(occurrences[selectedIndex], true))
		start = selectedIndex
		for i := selectedIndex - 1; i >= m.detail.occurrenceTop; i-- {
			height := len(m.semanticOccurrenceLines(occurrences[i], false))
			if used+height > available {
				break
			}
			used += height
			start = i
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

func (m Model) pendingWorkLines() []string {
	planned := m.opened.Detail.Synopsis.Planned
	if planned == nil || len(planned.Pending) == 0 {
		return nil
	}
	lines := []string{fmt.Sprintf("%s  %d", flight.title.Render("PENDING WORK"), len(planned.Pending))}
	for _, unit := range planned.Pending {
		status := "[" + strings.ToUpper(unit.Status) + "]"
		line := status + " " + zeroDash(unit.Description)
		if unit.Attempt > 0 {
			line += fmt.Sprintf(" · try %d", unit.Attempt)
		}
		if unit.Reason != "" {
			line += " · " + unit.Reason
		}
		lines = append(lines, fitText(line, m.width))
	}
	return lines
}

func (m Model) semanticOccurrenceLines(occurrence history.Occurrence, selected bool) []string {
	when := formatTime(occurrence.At)
	if m.width < 80 {
		when = compactTime(occurrence.At)
	}
	occurrenceFocused := selected && !(m.relatedRecordMode() && occurrence.ID == m.detail.occurrenceID)
	primary := focus(occurrenceFocused) + when + " · " + zeroDash(occurrence.Phase) + " · " + actionLabel(occurrence.Activity)
	context := zeroDash(occurrence.Work)
	if occurrence.Attempt != nil {
		context += fmt.Sprintf(" · try %d", *occurrence.Attempt)
	}
	if m.width >= 80 && occurrence.Actor != "" {
		context += " · " + occurrence.Actor
	}
	outcome := outcomeText(occurrence)
	lines := []string{fitStyled(primary, m.width)}
	if m.width < 80 {
		if outcome != "" {
			context += " · " + outcome
		}
		lines = append(lines, fitText("  "+context, m.width))
	} else {
		lines = append(lines, fitText("  "+context, m.width))
		if outcome != "" {
			lines = append(lines, fitText("  "+outcome, m.width))
		}
	}
	if selected {
		if m.relatedRecordMode() && occurrence.ID == m.detail.occurrenceID {
			available := max(1, m.occurrenceAvailableLines()-len(lines))
			lines = append(lines, m.relatedRecordLines(occurrence, available)...)
		} else {
			lines = append(lines, m.inlinePreviewLines(occurrence)...)
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

func (m Model) inlinePreviewLines(occurrence history.Occurrence) []string {
	record, ok := m.recordForOccurrence(occurrence)
	if !ok || strings.TrimSpace(record.Content) == "" || m.recordIntroducedBefore(occurrence, record.ID) {
		return nil
	}
	body, _ := renderEvidence(record.Content, max(1, m.width-4))
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		line = strings.TrimRight(ansi.Strip(line), " ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, fitText("    "+line, m.width))
		if len(lines) == 3 {
			break
		}
	}
	return lines
}

func (m Model) recordIntroducedBefore(occurrence history.Occurrence, recordID string) bool {
	current := -1
	for i, candidate := range m.opened.Detail.Occurrences {
		if candidate.ID == occurrence.ID {
			current = i
			break
		}
	}
	if current < 0 {
		return false
	}
	for _, candidate := range m.opened.Detail.Occurrences[:current] {
		for _, candidateID := range append([]string{candidate.RecordID}, candidate.RelatedRecordIDs...) {
			if recordID != "" && candidateID == recordID {
				return true
			}
		}
	}
	return false
}

func (m Model) recordForOccurrence(occurrence history.Occurrence) (history.Record, bool) {
	ids := append([]string{occurrence.RecordID}, occurrence.RelatedRecordIDs...)
	for _, id := range ids {
		for _, record := range m.opened.Detail.Records {
			if id != "" && record.ID == id {
				return record, true
			}
		}
	}
	return history.Record{}, false
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
	blank := 0
	if m.width >= 80 {
		blank = 1
	}
	return max(1, m.height-len(m.synopsisLines())-len(m.typedMetadataLines())-7-blank)
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
