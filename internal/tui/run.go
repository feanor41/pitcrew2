package tui

import (
	"errors"
	"io"

	tea "charm.land/bubbletea/v2"
)

var ErrUninitialized = errors.New("No PitCrew repository is initialized for this project.")

func NewUnavailable(err error) Model { return Model{err: err} }

func Run(model Model, input io.Reader, output io.Writer) error {
	_, err := tea.NewProgram(runtimeModel{model}, tea.WithInput(input), tea.WithOutput(output)).Run()
	return err
}

type runtimeModel struct{ Model }

func (m runtimeModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	next, command := m.Model.Update(message)
	return runtimeModel{next}, command
}
