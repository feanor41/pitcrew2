package tui

import tea "charm.land/bubbletea/v2"

type action uint8

const (
	actionNone action = iota
	actionUp
	actionDown
	actionPageUp
	actionPageDown
	actionHome
	actionEnd
	actionBack
	actionSelect
	actionSearch
	actionSubmit
	actionCancel
	actionDelete
	actionQuit
	actionRefresh
	actionText
)

func actionFor(key tea.KeyPressMsg, searchFocused bool) action {
	stroke := key.Keystroke()
	if stroke == "ctrl+c" {
		return actionQuit
	}
	if searchFocused {
		switch stroke {
		case "enter":
			return actionSubmit
		case "esc":
			return actionCancel
		case "backspace":
			return actionDelete
		}
		if key.Text != "" {
			return actionText
		}
		return actionNone
	}
	switch stroke {
	case "up", "k":
		return actionUp
	case "down", "j":
		return actionDown
	case "pgup":
		return actionPageUp
	case "pgdown":
		return actionPageDown
	case "home":
		return actionHome
	case "end":
		return actionEnd
	case "left", "h", "esc":
		return actionBack
	case "right", "l", "enter":
		return actionSelect
	case "/":
		return actionSearch
	case "q":
		return actionQuit
	case "r":
		return actionRefresh
	default:
		return actionNone
	}
}
