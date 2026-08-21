package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/fmazzalomo/pitcrew/internal/history"
)

type Loader interface {
	List(context.Context) ([]history.Workflow, error)
	Detail(context.Context, string) (history.Detail, error)
	Search(context.Context, string) ([]history.SearchResult, error)
	Resolve(context.Context, history.SearchResult) (history.Resolution, error)
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
}

type workflowsLoadedMsg struct{ workflows []history.Workflow }
type resultsLoadedMsg struct{ results []history.SearchResult }
type detailLoadedMsg struct{ resolution history.Resolution }
type loadFailedMsg struct{ err error }

func New(loader Loader) Model {
	return Model{loader: loader, loading: true}
}

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
	case loadFailedMsg:
		m.loading, m.err = false, msg.err
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch actionFor(key, m.searchFocused) {
	case actionUp:
		if m.selected > 0 {
			m.selected--
		}
	case actionDown:
		if m.selected+1 < m.itemCount() {
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

func (m Model) Hints() string {
	if m.searchFocused {
		return "enter search • esc cancel • ctrl+c quit"
	}
	return "↑/k up • ↓/j down • ←/h back • →/l/enter select • / search • q quit"
}
