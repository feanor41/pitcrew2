package tui

import (
	"context"
	"errors"
	"fmt"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/fmazzalomo/pitcrew/internal/history"
	"github.com/fmazzalomo/pitcrew/internal/version"
)

type Loader interface {
	List(context.Context) ([]history.Workflow, error)
	Detail(context.Context, string) (history.Detail, error)
	Search(context.Context, string) ([]history.SearchResult, error)
	Resolve(context.Context, history.SearchResult) (history.Resolution, error)
	ResolveActivity(context.Context, history.Activity) (history.Resolution, error)
}

type occurrenceResolver interface {
	ResolveOccurrence(context.Context, string, string, string) (history.Resolution, error)
}

type Screen uint8

const (
	HomeScreen Screen = iota
	WorkflowsScreen
	ResultsScreen
	DetailScreen
)

type homeAction uint8

const (
	homeInstall homeAction = iota
	homeConfigureModels
	homeWorkflows
	homeExit
)

var homeActions = [...]string{
	"Install in Runtime",
	"Configure Runtime Models",
	"Workflows",
	"Exit",
}

type Model struct {
	loader          Loader
	screen          Screen
	homeSelected    int
	homeNotice      string
	workflows       []history.Workflow
	workflowTop     int
	results         []history.SearchResult
	opened          history.Resolution
	selected        int
	query           string
	searchFocused   bool
	searchOriginal  string
	search          textinput.Model
	viewport        viewport.Model
	evidence        evidenceCache
	componentsReady bool
	loading         bool
	err             error
	width           int
	height          int
	detail          detailCursor
	detailParent    Screen
	generation      uint64
	activeLoad      loadKind
	loadPreserves   bool
}

type evidenceCache struct {
	recordID string
	source   string
	width    int
	body     string
	mode     evidenceRenderMode
}

type detailCursor struct {
	occurrenceID    string
	occurrenceTop   int
	related         bool
	relatedRecordID string
	recordID        string
	line            int
	top             int
}

type loadKind uint8

const (
	loadUnknown loadKind = iota
	loadWorkflows
	loadResults
	loadDetail
)

type loadMeta struct {
	kind       loadKind
	generation uint64
	preserve   bool
}
type workflowsLoadedMsg struct {
	loadMeta
	workflows []history.Workflow
}
type resultsLoadedMsg struct {
	loadMeta
	results []history.SearchResult
}
type detailLoadedMsg struct {
	loadMeta
	resolution history.Resolution
}
type loadFailedMsg struct {
	loadMeta
	err error
}

func New(loader Loader) Model {
	m := Model{loader: loader, loading: true, generation: 1, activeLoad: loadWorkflows}
	m.ensureComponents()
	return m
}

func (Model) Version() string { return version.Current }

func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		workflows, err := m.loader.List(context.Background())
		if err != nil {
			return loadFailedMsg{loadMeta: loadMeta{kind: loadWorkflows, generation: m.generation}, err: err}
		}
		return workflowsLoadedMsg{loadMeta: loadMeta{kind: loadWorkflows, generation: m.generation}, workflows: workflows}
	}
}

func (m Model) Update(message tea.Msg) (Model, tea.Cmd) {
	m.ensureComponents()
	switch msg := message.(type) {
	case workflowsLoadedMsg:
		if !m.acceptLoad(msg.loadMeta) {
			return m, nil
		}
		selectedID := ""
		if m.selected >= 0 && m.selected < len(m.workflows) {
			selectedID = m.workflows[m.selected].ID
		}
		m.workflows, m.loading, m.err = msg.workflows, false, nil
		m.selected = reconcileIndex(len(m.workflows), m.selected, func(i int) bool { return m.workflows[i].ID == selectedID })
		m.reconcileWorkflowWindow()
		m.commitLoad(msg.generation)
	case resultsLoadedMsg:
		if !m.acceptLoad(msg.loadMeta) {
			return m, nil
		}
		selectedResult, hadSelection := history.SearchResult{}, m.selected >= 0 && m.selected < len(m.results)
		if hadSelection {
			selectedResult = m.results[m.selected]
		}
		m.results, m.loading, m.err = msg.results, false, nil
		m.screen = ResultsScreen
		if msg.preserve && hadSelection {
			m.selected = reconcileIndex(len(m.results), m.selected, func(i int) bool {
				result := m.results[i]
				return result.WorkflowID == selectedResult.WorkflowID && result.RecordID == selectedResult.RecordID && result.Kind == selectedResult.Kind && result.UnitID == selectedResult.UnitID && result.Revision == selectedResult.Revision
			})
		} else {
			m.selected = 0
		}
		m.commitLoad(msg.generation)
	case detailLoadedMsg:
		if !m.acceptLoad(msg.loadMeta) {
			return m, nil
		}
		cursor := m.detail
		previousRecord := m.opened.Record
		if msg.preserve {
			msg.resolution.Record = refreshedRecord(msg.resolution.Detail, previousRecord)
		}
		m.opened, m.loading, m.err = msg.resolution, false, nil
		if !msg.preserve {
			m.detailParent = WorkflowsScreen
			if m.screen == ResultsScreen {
				m.detailParent = ResultsScreen
			}
		}
		m.screen = DetailScreen
		if msg.preserve {
			m.detail = cursor
		} else if cursor.occurrenceID != "" {
			m.detail = cursor
			m.detail.recordID = msg.resolution.Record.ID
		} else {
			m.detail = detailCursor{occurrenceID: cursor.occurrenceID, recordID: msg.resolution.Record.ID}
		}
		m.reconcileDetail()
		m.prepareEvidence()
		m.commitLoad(msg.generation)
	case loadFailedMsg:
		if !m.acceptLoad(msg.loadMeta) {
			return m, nil
		}
		m.loading, m.err = false, msg.err
		if msg.kind == loadWorkflows {
			m.reconcileWorkflowWindow()
		}
		m.commitLoad(msg.generation)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.search.SetWidth(max(1, msg.Width-10))
		m.reconcileWorkflowWindow()
		m.reconcileDetail()
		m.prepareEvidence()
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}
	if m.search.Focused() {
		var command tea.Cmd
		m.search, command = m.search.Update(message)
		m.searchFocused = m.search.Focused()
		return m, command
	}
	return m, nil
}

func (m Model) updateKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Keystroke() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.search.Focused() {
		switch key.Keystroke() {
		case "enter":
			m.query = m.search.Value()
			m.search.Blur()
			m.searchFocused = false
			query := m.query
			meta := m.startLoad(loadResults, false)
			return m, func() tea.Msg {
				results, err := m.loader.Search(context.Background(), query)
				if err != nil {
					return loadFailedMsg{loadMeta: meta, err: err}
				}
				return resultsLoadedMsg{loadMeta: meta, results: results}
			}
		case "esc":
			m.search.Blur()
			m.searchFocused = false
			m.query = m.searchOriginal
			return m, nil
		default:
			var command tea.Cmd
			m.search, command = m.search.Update(key)
			m.searchFocused = m.search.Focused()
			m.query = m.search.Value()
			return m, command
		}
	}
	if m.opened.Record.ID != "" {
		switch key.Keystroke() {
		case "left", "h", "esc":
			m.closeEvidence()
			return m, nil
		case "q":
			return m, tea.Quit
		case "r":
			return m.refreshActive()
		default:
			var command tea.Cmd
			m.viewport, command = m.viewport.Update(key)
			m.detail.recordID = m.opened.Record.ID
			m.detail.line, m.detail.top = m.viewport.YOffset(), m.viewport.YOffset()
			return m, command
		}
	}
	if m.screen == HomeScreen {
		return m.updateHomeKey(key)
	}
	switch actionFor(key) {
	case actionUp:
		if m.relatedRecordMode() {
			m.moveRelatedRecord(-1)
		} else if m.screen == DetailScreen {
			m.moveFocused(-1)
		} else if m.selected > 0 {
			m.selected--
			if m.screen == WorkflowsScreen {
				m.reconcileWorkflowWindow()
			}
		}
	case actionDown:
		if m.relatedRecordMode() {
			m.moveRelatedRecord(1)
		} else if m.screen == DetailScreen {
			m.moveFocused(1)
		} else if m.selected+1 < m.itemCount() {
			m.selected++
			if m.screen == WorkflowsScreen {
				m.reconcileWorkflowWindow()
			}
		}
	case actionBack:
		switch m.screen {
		case DetailScreen:
			if m.relatedRecordMode() {
				m.detail.related = false
				return m, nil
			}
			m.cancelLoad()
			m.screen = m.detailParent
			if m.screen != ResultsScreen {
				m.screen = WorkflowsScreen
			}
		case ResultsScreen:
			m.cancelLoad()
			m.screen = WorkflowsScreen
			m.reconcileWorkflowWindow()
		case WorkflowsScreen:
			if m.loading && m.activeLoad != loadWorkflows {
				m.cancelLoad()
			}
			m.screen = HomeScreen
		}
	case actionPageUp:
		if m.relatedRecordMode() {
			m.moveRelatedRecord(-m.relatedRecordPageSize())
		} else if m.occurrenceMode() {
			m.moveOccurrence(-m.measuredOccurrencePage(-1))
		}
	case actionPageDown:
		if m.relatedRecordMode() {
			m.moveRelatedRecord(m.relatedRecordPageSize())
		} else if m.occurrenceMode() {
			m.moveOccurrence(m.measuredOccurrencePage(1))
		}
	case actionHome:
		if m.relatedRecordMode() {
			m.setRelatedRecordIndex(0)
		} else if m.occurrenceMode() {
			m.setOccurrenceIndex(0)
		}
	case actionEnd:
		if m.relatedRecordMode() {
			if occurrence, ok := m.focusedOccurrence(); ok {
				m.setRelatedRecordIndex(len(m.supportingRecordIDs(occurrence)) - 1)
			}
		} else if m.occurrenceMode() {
			m.setOccurrenceIndex(len(m.opened.Detail.Occurrences) - 1)
		}
	case actionSelect:
		return m.openSelected()
	case actionSearch:
		m.searchOriginal = m.query
		m.search.Reset()
		m.searchFocused = true
		return m, m.search.Focus()
	case actionQuit:
		return m, tea.Quit
	case actionRefresh:
		return m.refreshActive()
	}
	return m, nil
}

func (m Model) updateHomeKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch actionFor(key) {
	case actionUp:
		m.homeSelected = max(0, m.homeSelected-1)
	case actionDown:
		m.homeSelected = min(len(homeActions)-1, m.homeSelected+1)
	case actionSelect:
		switch homeAction(m.homeSelected) {
		case homeInstall:
			m.homeNotice = "Installation is unavailable in the read-only TUI. Run pitcrew install <codex|opencode|claude|pi> in your shell."
		case homeConfigureModels:
			m.homeNotice = "Runtime model configuration is unavailable in the read-only TUI."
		case homeWorkflows:
			m.homeNotice = ""
			m.screen = WorkflowsScreen
			m.reconcileWorkflowWindow()
		case homeExit:
			return m, tea.Quit
		}
	case actionQuit:
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) reconcileWorkflowWindow() {
	if len(m.workflows) == 0 {
		m.selected, m.workflowTop = 0, 0
		return
	}
	m.selected = max(0, min(m.selected, len(m.workflows)-1))
	visible := m.workflowVisibleRows()
	if m.selected < m.workflowTop {
		m.workflowTop = m.selected
	} else if m.selected >= m.workflowTop+visible {
		m.workflowTop = m.selected - visible + 1
	}
	m.workflowTop = max(0, min(m.workflowTop, max(0, len(m.workflows)-visible)))
}

func (m Model) workflowVisibleRows() int {
	noticeRows := 0
	if m.loadPreserves && (m.loading || m.err != nil) && len(m.workflows) > 0 {
		noticeRows = 1
	}
	gridHeight := m.height - 6 - 1 - noticeRows
	return max(1, gridHeight-4)
}

func (m *Model) ensureComponents() {
	if m.componentsReady {
		return
	}
	m.search = textinput.New()
	m.search.Prompt = ""
	m.search.SetWidth(max(1, m.width-10))
	m.viewport = viewport.New(viewport.WithWidth(max(1, m.width-2)), viewport.WithHeight(max(1, m.evidenceHeight())))
	m.viewport.SoftWrap = false
	m.componentsReady = true
	if m.searchFocused {
		m.search.SetValue(m.query)
		m.searchOriginal = m.query
		_ = m.search.Focus()
	}
}

func (m *Model) prepareEvidence() {
	m.ensureComponents()
	width := max(1, m.evidenceWidth()-2)
	height := max(1, m.evidenceHeight())
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(height)
	if m.opened.Record.ID == "" {
		m.clearEvidence()
		return
	}

	record := m.opened.Record
	source := evidenceSource(record)
	identityChanged := m.evidence.recordID != record.ID
	needsRender := identityChanged || m.evidence.source != source || m.evidence.width != width
	offset := m.viewport.YOffset()
	if needsRender {
		body, mode := renderEvidence(record.Content, width)
		m.evidence = evidenceCache{recordID: record.ID, source: source, width: width, body: body, mode: mode}
		m.viewport.SetContent(body)
	}
	if identityChanged {
		m.viewport.GotoTop()
	} else {
		m.viewport.SetYOffset(offset)
	}
	m.detail.recordID = record.ID
	m.detail.line, m.detail.top = m.viewport.YOffset(), m.viewport.YOffset()
}

func evidenceSource(record history.Record) string {
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s", record.ID, record.Title, record.Revision, record.Content)
}

func (m *Model) closeEvidence() {
	m.cancelLoad()
	m.opened.Record = history.Record{}
	m.detail.recordID, m.detail.line, m.detail.top = "", 0, 0
	m.clearEvidence()
	m.reconcileDetail()
}

func (m *Model) clearEvidence() {
	m.evidence = evidenceCache{}
	m.viewport.SetContent("")
	m.viewport.GotoTop()
}

func (m Model) openSelected() (Model, tea.Cmd) {
	if m.occurrenceMode() {
		if occurrence, ok := m.focusedOccurrence(); ok {
			recordIDs := m.supportingRecordIDs(occurrence)
			if len(recordIDs) == 0 {
				return m, nil
			}
			if len(recordIDs) > 1 && !m.relatedRecordMode() {
				m.detail.related = true
				m.reconcileRelatedRecord(occurrence)
				return m, nil
			}
			resolver, ok := m.loader.(occurrenceResolver)
			if !ok {
				m.err = errors.New("history loader cannot resolve occurrences")
				return m, nil
			}
			recordID := recordIDs[0]
			if m.relatedRecordMode() {
				recordID = m.detail.relatedRecordID
			}
			meta := m.startLoad(loadDetail, false)
			return m, func() tea.Msg {
				resolution, err := resolver.ResolveOccurrence(context.Background(), m.opened.Detail.Workflow.ID, occurrence.ID, recordID)
				if err != nil {
					return loadFailedMsg{loadMeta: meta, err: err}
				}
				return detailLoadedMsg{loadMeta: meta, resolution: resolution}
			}
		}
	}
	if m.screen == WorkflowsScreen && m.selected < len(m.workflows) {
		workflowID := m.workflows[m.selected].ID
		m.detail = detailCursor{}
		meta := m.startLoad(loadDetail, false)
		return m, func() tea.Msg {
			detail, err := m.loader.Detail(context.Background(), workflowID)
			if err != nil {
				return loadFailedMsg{loadMeta: meta, err: err}
			}
			return detailLoadedMsg{loadMeta: meta, resolution: history.Resolution{Detail: detail}}
		}
	}
	if m.screen == ResultsScreen && m.selected < len(m.results) {
		result := m.results[m.selected]
		m.detail = detailCursor{}
		meta := m.startLoad(loadDetail, false)
		return m, func() tea.Msg {
			resolution, err := m.loader.Resolve(context.Background(), result)
			if err != nil {
				return loadFailedMsg{loadMeta: meta, err: err}
			}
			return detailLoadedMsg{loadMeta: meta, resolution: resolution}
		}
	}
	return m, nil
}

func (m Model) refreshActive() (Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	switch m.screen {
	case ResultsScreen:
		query := m.query
		meta := m.startLoad(loadResults, true)
		return m, func() tea.Msg {
			results, err := m.loader.Search(context.Background(), query)
			if err != nil {
				return loadFailedMsg{loadMeta: meta, err: err}
			}
			return resultsLoadedMsg{loadMeta: meta, results: results}
		}
	case DetailScreen:
		workflowID := m.opened.Detail.Workflow.ID
		if workflowID == "" {
			return m, nil
		}
		meta := m.startLoad(loadDetail, true)
		return m, func() tea.Msg {
			detail, err := m.loader.Detail(context.Background(), workflowID)
			if err != nil {
				return loadFailedMsg{loadMeta: meta, err: err}
			}
			return detailLoadedMsg{loadMeta: meta, resolution: history.Resolution{Detail: detail}}
		}
	default:
		meta := m.startLoad(loadWorkflows, true)
		m.reconcileWorkflowWindow()
		return m, func() tea.Msg {
			workflows, err := m.loader.List(context.Background())
			if err != nil {
				return loadFailedMsg{loadMeta: meta, err: err}
			}
			return workflowsLoadedMsg{loadMeta: meta, workflows: workflows}
		}
	}
}

func (m *Model) cancelLoad() {
	if m.loading {
		m.generation++
		m.activeLoad = loadUnknown
		m.loading, m.loadPreserves = false, false
	}
}

func (m *Model) startLoad(kind loadKind, preserve bool) loadMeta {
	m.generation++
	m.activeLoad, m.loading, m.err, m.loadPreserves = kind, true, nil, preserve
	return loadMeta{kind: kind, generation: m.generation, preserve: preserve}
}

func (m Model) acceptLoad(meta loadMeta) bool {
	return meta.generation == 0 || meta.generation == m.generation && meta.kind == m.activeLoad
}

func (m *Model) commitLoad(generation uint64) {
	if generation > 0 {
		m.generation++
		m.activeLoad = loadUnknown
	}
}

func reconcileIndex(length, fallback int, matches func(int) bool) int {
	for i := range length {
		if matches(i) {
			return i
		}
	}
	if length == 0 {
		return 0
	}
	return max(0, min(fallback, length-1))
}

func refreshedRecord(detail history.Detail, previous history.Record) history.Record {
	workflow := detail.Workflow
	if previous.ID == "" {
		return history.Record{}
	}
	if previous.ID == "goal" {
		return history.Record{ID: "goal", WorkflowID: workflow.ID, Kind: "goal", Title: "Goal", Content: workflow.Goal, At: workflow.UpdatedAt}
	}
	if previous.ID == "workflow:"+workflow.ID {
		return history.Record{ID: previous.ID, WorkflowID: workflow.ID, Kind: "workflow", Title: workflow.Name, Content: workflow.Goal, At: workflow.CreatedAt}
	}
	for _, record := range detail.Records {
		if record.ID == previous.ID {
			return record
		}
	}
	return history.Record{}
}

func (m Model) itemCount() int {
	if m.screen == HomeScreen {
		return len(homeActions)
	}
	if m.screen == ResultsScreen {
		return len(m.results)
	}
	return len(m.workflows)
}

func (m *Model) reconcileDetail() {
	if m.screen != DetailScreen {
		m.detail = detailCursor{}
		return
	}
	if m.occurrenceMode() {
		m.reconcileOccurrences()
		return
	}
	lines := m.evidenceLines()
	if len(lines) == 0 {
		m.detail.recordID, m.detail.line, m.detail.top = "", 0, 0
		return
	}
	index := 0
	if m.detail.recordID != "" {
		for i, line := range lines {
			if line.recordID == m.detail.recordID && line.local <= m.detail.line {
				index = i
			}
		}
	}
	m.setEvidenceIndex(lines, index)
}

func (m *Model) moveFocused(delta int) {
	if m.occurrenceMode() {
		m.moveOccurrence(delta)
		return
	}
	m.reconcileDetail()
	lines := m.evidenceLines()
	index, _ := m.detailPosition()
	index = max(1, min(len(lines), index+delta)) - 1
	m.setEvidenceIndex(lines, index)
}

func (m *Model) setEvidenceIndex(lines []evidenceLine, index int) {
	if len(lines) == 0 {
		return
	}
	index = max(0, min(len(lines)-1, index))
	m.detail.recordID, m.detail.line = lines[index].recordID, lines[index].local
	visible := m.evidenceHeight()
	if index < m.detail.top {
		m.detail.top = index
	} else if index >= m.detail.top+visible {
		m.detail.top = index - visible + 1
	}
	m.detail.top = max(0, min(m.detail.top, max(0, len(lines)-visible)))
}

func (m Model) detailPosition() (int, int) {
	if m.relatedRecordMode() {
		occurrence, ok := m.focusedOccurrence()
		if !ok {
			return 0, 0
		}
		ids := m.supportingRecordIDs(occurrence)
		for i, id := range ids {
			if id == m.detail.relatedRecordID {
				return i + 1, len(ids)
			}
		}
		return 0, len(ids)
	}
	if m.occurrenceMode() {
		for i, occurrence := range m.opened.Detail.Occurrences {
			if occurrence.ID == m.detail.occurrenceID {
				return i + 1, len(m.opened.Detail.Occurrences)
			}
		}
		return 0, len(m.opened.Detail.Occurrences)
	}
	lines := m.evidenceLines()
	for i, line := range lines {
		if line.recordID == m.detail.recordID && line.local == m.detail.line {
			return i + 1, len(lines)
		}
	}
	return 0, len(lines)
}

func (m Model) occurrenceMode() bool {
	return m.screen == DetailScreen && m.opened.Record.ID == "" && len(m.opened.Detail.Occurrences) > 0
}

func (m Model) relatedRecordMode() bool {
	return m.occurrenceMode() && m.detail.related
}

func (m *Model) reconcileOccurrences() {
	occurrences := m.opened.Detail.Occurrences
	if len(occurrences) == 0 {
		m.detail.occurrenceID, m.detail.recordID, m.detail.occurrenceTop = "", "", 0
		m.detail.related, m.detail.relatedRecordID = false, ""
		return
	}
	for i, occurrence := range occurrences {
		if occurrence.ID == m.detail.occurrenceID {
			m.setOccurrenceIndex(i)
			m.reconcileRelatedRecord(occurrence)
			return
		}
	}
	m.setOccurrenceIndex(m.operationalOccurrenceIndex())
}

func (m *Model) moveOccurrence(delta int) {
	m.reconcileOccurrences()
	index, _ := m.detailPosition()
	m.setOccurrenceIndex(max(0, min(len(m.opened.Detail.Occurrences)-1, index-1+delta)))
}

func (m *Model) setOccurrenceIndex(index int) {
	occurrences := m.opened.Detail.Occurrences
	if len(occurrences) == 0 {
		return
	}
	index = max(0, min(len(occurrences)-1, index))
	previousID := m.detail.occurrenceID
	m.detail.occurrenceID = occurrences[index].ID
	if previousID != "" && previousID != m.detail.occurrenceID {
		m.detail.related = false
		m.detail.relatedRecordID = ""
	}
	m.detail.recordID, m.detail.line = occurrences[index].ID, 0
	if index < m.detail.occurrenceTop {
		m.detail.occurrenceTop = index
	}
	m.detail.occurrenceTop = max(0, min(m.detail.occurrenceTop, index))
	for m.detail.occurrenceTop < index && !m.occurrenceFits(m.detail.occurrenceTop, index) {
		m.detail.occurrenceTop++
	}
}

func (m Model) supportingRecordIDs(occurrence history.Occurrence) []string {
	seen := make(map[string]struct{}, 1+len(occurrence.RelatedRecordIDs))
	ids := make([]string, 0, 1+len(occurrence.RelatedRecordIDs))
	for _, id := range append([]string{occurrence.RecordID}, occurrence.RelatedRecordIDs...) {
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (m *Model) reconcileRelatedRecord(occurrence history.Occurrence) {
	ids := m.supportingRecordIDs(occurrence)
	if len(ids) == 0 {
		m.detail.related = false
		m.detail.relatedRecordID = ""
		return
	}
	for _, id := range ids {
		if id == m.detail.relatedRecordID {
			return
		}
	}
	m.detail.relatedRecordID = ids[0]
}

func (m *Model) moveRelatedRecord(delta int) {
	current, total := m.detailPosition()
	if total == 0 {
		return
	}
	m.setRelatedRecordIndex(max(0, min(total-1, current-1+delta)))
}

func (m Model) relatedRecordPageSize() int {
	return max(1, m.occurrenceAvailableLines()-4)
}

func (m *Model) setRelatedRecordIndex(index int) {
	occurrence, ok := m.focusedOccurrence()
	if !ok {
		return
	}
	ids := m.supportingRecordIDs(occurrence)
	if len(ids) == 0 {
		return
	}
	index = max(0, min(len(ids)-1, index))
	m.detail.relatedRecordID = ids[index]
}

func (m Model) focusedOccurrence() (history.Occurrence, bool) {
	for _, occurrence := range m.opened.Detail.Occurrences {
		if occurrence.ID == m.detail.occurrenceID {
			return occurrence, true
		}
	}
	return history.Occurrence{}, false
}

func (m Model) measuredOccurrencePage(direction int) int {
	index, _ := m.detailPosition()
	index--
	if index < 0 {
		return 1
	}
	available := m.occurrenceAvailableLines()
	used, count := 0, 0
	for i := index; i >= 0 && i < len(m.opened.Detail.Occurrences); i += direction {
		height := len(m.semanticOccurrenceLines(m.opened.Detail.Occurrences[i], i == index))
		if count > 0 && used+height > available {
			break
		}
		used += height
		count++
		if used >= available {
			break
		}
	}
	return max(1, count)
}

func (m Model) occurrenceFits(start, selected int) bool {
	used := 0
	for i := start; i <= selected; i++ {
		height := len(m.semanticOccurrenceLines(m.opened.Detail.Occurrences[i], i == selected))
		if i == start && height > m.occurrenceAvailableLines() {
			return true
		}
		used += height
		if used > m.occurrenceAvailableLines() {
			return false
		}
	}
	return true
}

func (m Model) occurrenceAvailableLines() int {
	if m.wideDetailMode() {
		return max(1, m.height-5-len(m.operationalHeaderLines())-1)
	}
	unitRows := 2
	if m.height >= 24 {
		unitRows = 4
	}
	return max(1, m.height-6-len(m.operationalHeaderLines())-min(unitRows, len(m.unitProgressLines(unitRows)))-1)
}

func (m Model) operationalOccurrenceIndex() int {
	occurrences := m.opened.Detail.Occurrences
	if len(occurrences) == 0 {
		return 0
	}
	synopsis := m.opened.Detail.Synopsis
	if synopsis.Blocker != nil && synopsis.Blocker.Status == "Correction" {
		if index := latestWorkOccurrence(occurrences, synopsis.Blocker.Description); index >= 0 {
			return index
		}
		for i := len(occurrences) - 1; i >= 0; i-- {
			if occurrences[i].Outcome == "Corrections" {
				return i
			}
		}
	}
	if synopsis.Current != nil && (synopsis.Current.Status == "Claimed" || synopsis.Current.Status == "Reviewing") {
		if index := latestWorkOccurrence(occurrences, synopsis.Current.Description); index >= 0 {
			return index
		}
	}
	if m.opened.Detail.Workflow.State == "completed" || m.opened.Detail.Workflow.State == "abandoned" {
		want := "workflow_" + m.opened.Detail.Workflow.State
		for i := len(occurrences) - 1; i >= 0; i-- {
			if occurrences[i].Activity == want {
				return i
			}
		}
	}
	if synopsis.Blocker != nil {
		if index := latestWorkOccurrence(occurrences, synopsis.Blocker.Description); index >= 0 {
			return index
		}
	}
	return len(occurrences) - 1
}

func latestWorkOccurrence(occurrences []history.Occurrence, work string) int {
	for i := len(occurrences) - 1; i >= 0; i-- {
		if occurrences[i].Work == work {
			return i
		}
	}
	return -1
}

func (m Model) Hints() string {
	if m.searchFocused {
		return "enter search • esc cancel • ctrl+c quit"
	}
	if m.screen == HomeScreen {
		if m.width > 0 && m.width < 80 {
			return "j/k move • enter select • q quit"
		}
		return "↑/k up • ↓/j down • enter select • q quit"
	}
	if m.width > 0 && m.width < 80 {
		return "j/k • h back • enter • / search • r refresh • q quit"
	}
	return "↑/k up • ↓/j down • ←/h back • →/l/enter select • / search • r refresh • q quit"
}
