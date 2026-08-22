package tui

import (
	"context"

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
}

type detailCursor struct {
	recordID string
	line     int
	top      int
}

type workflowsLoadedMsg struct{ workflows []history.Workflow }
type resultsLoadedMsg struct{ results []history.SearchResult }
type detailLoadedMsg struct{ resolution history.Resolution }
type loadFailedMsg struct{ err error }

func New(loader Loader) Model {
	return Model{loader: loader, loading: true}
}

func (Model) Version() string { return version.Current }

func (m Model) Init() tea.Cmd {
	return func() tea.Msg {
		workflows, err := m.loader.List(context.Background())
		if err != nil {
			return loadFailedMsg{err}
		}
		return workflowsLoadedMsg{workflows}
	}
}

func (m Model) Update(message tea.Msg) (Model, tea.Cmd) {
	switch msg := message.(type) {
	case workflowsLoadedMsg:
		m.workflows, m.loading, m.err = msg.workflows, false, nil
	case resultsLoadedMsg:
		m.results, m.loading, m.err = msg.results, false, nil
		m.screen, m.selected = ResultsScreen, 0
	case detailLoadedMsg:
		m.opened, m.loading, m.err = msg.resolution, false, nil
		m.screen = DetailScreen
		m.detail = detailCursor{recordID: msg.resolution.Record.ID}
		m.reconcileDetail()
	case loadFailedMsg:
		m.loading, m.err = false, msg.err
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
			m.moveDetail(-1)
		} else if m.selected > 0 {
			m.selected--
		}
	case actionDown:
		if m.screen == DetailScreen {
			m.moveDetail(1)
		} else if m.selected+1 < m.itemCount() {
			m.selected++
		}
	case actionBack:
		m.screen = WorkflowsScreen
	case actionSelect:
		return m.openSelected()
	case actionSearch:
		m.searchFocused, m.query = true, ""
	case actionSubmit:
		m.searchFocused, m.loading = false, true
		query := m.query
		return m, func() tea.Msg {
			results, err := m.loader.Search(context.Background(), query)
			if err != nil {
				return loadFailedMsg{err}
			}
			return resultsLoadedMsg{results}
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
	}
	return m, nil
}

func (m Model) openSelected() (Model, tea.Cmd) {
	if m.screen == DetailScreen {
		lines := m.evidenceLines()
		index, _ := m.detailPosition()
		if index > 0 && index <= len(lines) && lines[index-1].activity != nil {
			activity := *lines[index-1].activity
			m.loading = true
			return m, func() tea.Msg {
				resolution, err := m.loader.ResolveActivity(context.Background(), activity)
				if err != nil {
					return loadFailedMsg{err}
				}
				return detailLoadedMsg{resolution}
			}
		}
	}
	if m.screen == WorkflowsScreen && m.selected < len(m.workflows) {
		workflowID := m.workflows[m.selected].ID
		m.loading = true
		return m, func() tea.Msg {
			detail, err := m.loader.Detail(context.Background(), workflowID)
			if err != nil {
				return loadFailedMsg{err}
			}
			return detailLoadedMsg{history.Resolution{Detail: detail}}
		}
	}
	if m.screen == ResultsScreen && m.selected < len(m.results) {
		result := m.results[m.selected]
		m.loading = true
		return m, func() tea.Msg {
			resolution, err := m.loader.Resolve(context.Background(), result)
			if err != nil {
				return loadFailedMsg{err}
			}
			return detailLoadedMsg{resolution}
		}
	}
	return m, nil
}

func (m Model) itemCount() int {
	if m.screen == ResultsScreen {
		return len(m.results)
	}
	return len(m.workflows)
}

func (m *Model) reconcileDetail() {
	lines := m.evidenceLines()
	if m.screen != DetailScreen || len(lines) == 0 {
		m.detail = detailCursor{}
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
	m.setDetailIndex(lines, index)
}

func (m *Model) moveDetail(delta int) {
	m.reconcileDetail()
	lines := m.evidenceLines()
	index, _ := m.detailPosition()
	index = max(1, min(len(lines), index+delta)) - 1
	m.setDetailIndex(lines, index)
}

func (m *Model) setDetailIndex(lines []evidenceLine, index int) {
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
	lines := m.evidenceLines()
	for i, line := range lines {
		if line.recordID == m.detail.recordID && line.local == m.detail.line {
			return i + 1, len(lines)
		}
	}
	return 0, len(lines)
}

func (m Model) Hints() string {
	if m.searchFocused {
		return "enter search • esc cancel • ctrl+c quit"
	}
	return "↑/k up • ↓/j down • ←/h back • →/l/enter select • / search • q quit"
}
