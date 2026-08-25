package tui

import (
	"context"
	"errors"

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
	WorkflowsScreen Screen = iota
	ResultsScreen
	DetailScreen
)

type Model struct {
	loader        Loader
	screen        Screen
	workflows     []history.Workflow
	results       []history.SearchResult
	opened        history.Resolution
	selected      int
	query         string
	searchFocused bool
	loading       bool
	err           error
	width         int
	height        int
	detail        detailCursor
	generation    uint64
	activeLoad    loadKind
	loadPreserves bool
}

type detailCursor struct {
	occurrenceID  string
	occurrenceTop int
	recordID      string
	line          int
	top           int
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
	return Model{loader: loader, loading: true, generation: 1, activeLoad: loadWorkflows}
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
		m.screen = DetailScreen
		if msg.preserve {
			m.detail = cursor
		} else {
			m.detail = detailCursor{occurrenceID: cursor.occurrenceID, recordID: msg.resolution.Record.ID}
		}
		m.reconcileDetail()
		m.commitLoad(msg.generation)
	case loadFailedMsg:
		if !m.acceptLoad(msg.loadMeta) {
			return m, nil
		}
		m.loading, m.err = false, msg.err
		m.commitLoad(msg.generation)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.reconcileDetail()
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch actionFor(key, m.searchFocused) {
	case actionUp:
		if m.screen == DetailScreen {
			m.moveFocused(-1)
		} else if m.selected > 0 {
			m.selected--
		}
	case actionDown:
		if m.screen == DetailScreen {
			m.moveFocused(1)
		} else if m.selected+1 < m.itemCount() {
			m.selected++
		}
	case actionBack:
		if m.screen == DetailScreen && m.opened.Record.ID != "" && m.detail.occurrenceID != "" {
			m.cancelLoad()
			m.opened.Record = history.Record{}
			m.detail.recordID, m.detail.line, m.detail.top = "", 0, 0
			m.reconcileDetail()
		} else if m.screen != WorkflowsScreen {
			m.cancelLoad()
			m.screen = WorkflowsScreen
		}
	case actionPageUp:
		if m.occurrenceMode() {
			m.moveOccurrence(-m.occurrencePageSize())
		}
	case actionPageDown:
		if m.occurrenceMode() {
			m.moveOccurrence(m.occurrencePageSize())
		}
	case actionHome:
		if m.occurrenceMode() {
			m.setOccurrenceIndex(0)
		}
	case actionEnd:
		if m.occurrenceMode() {
			m.setOccurrenceIndex(m.operationalOccurrenceIndex())
		}
	case actionSelect:
		return m.openSelected()
	case actionSearch:
		m.searchFocused, m.query = true, ""
	case actionSubmit:
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
	case actionCancel:
		m.searchFocused = false
	case actionDelete:
		runes := []rune(m.query)
		if len(runes) > 0 {
			m.query = string(runes[:len(runes)-1])
		}
	case actionText:
		m.query += key.Text
	case actionQuit:
		return m, tea.Quit
	case actionRefresh:
		return m.refreshActive()
	}
	return m, nil
}

func (m Model) openSelected() (Model, tea.Cmd) {
	if m.occurrenceMode() {
		if occurrence, ok := m.focusedOccurrence(); ok {
			resolver, ok := m.loader.(occurrenceResolver)
			if !ok {
				m.err = errors.New("history loader cannot resolve occurrences")
				return m, nil
			}
			meta := m.startLoad(loadDetail, false)
			return m, func() tea.Msg {
				resolution, err := resolver.ResolveOccurrence(context.Background(), m.opened.Detail.Workflow.ID, occurrence.ID, occurrence.RecordID)
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

func (m *Model) reconcileOccurrences() {
	occurrences := m.opened.Detail.Occurrences
	if len(occurrences) == 0 {
		m.detail.occurrenceID, m.detail.recordID, m.detail.occurrenceTop = "", "", 0
		return
	}
	for i, occurrence := range occurrences {
		if occurrence.ID == m.detail.occurrenceID {
			m.setOccurrenceIndex(i)
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
	m.detail.occurrenceID = occurrences[index].ID
	m.detail.recordID, m.detail.line = occurrences[index].ID, 0
	visible := m.occurrencePageSize()
	if index < m.detail.occurrenceTop {
		m.detail.occurrenceTop = index
	} else if index >= m.detail.occurrenceTop+visible {
		m.detail.occurrenceTop = index - visible + 1
	}
	m.detail.occurrenceTop = max(0, min(m.detail.occurrenceTop, max(0, len(occurrences)-visible)))
}

func (m Model) focusedOccurrence() (history.Occurrence, bool) {
	for _, occurrence := range m.opened.Detail.Occurrences {
		if occurrence.ID == m.detail.occurrenceID {
			return occurrence, true
		}
	}
	return history.Occurrence{}, false
}

func (m Model) occurrencePageSize() int {
	rows := max(1, m.height-12)
	if m.width > 0 && m.width < 80 {
		rows /= 2
	}
	return max(1, rows)
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
	if m.width > 0 && m.width < 80 {
		return "j/k move • enter • / search • r refresh • q quit"
	}
	return "↑/k up • ↓/j down • ←/h back • →/l/enter select • / search • r refresh • q quit"
}
