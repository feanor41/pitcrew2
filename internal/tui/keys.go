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
	actionQuit
	actionRefresh
)

func actionFor(key tea.KeyPressMsg) action {
	stroke := key.Keystroke()
	if stroke == "ctrl+c" {
		return actionQuit
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
