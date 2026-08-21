package tui

import "charm.land/lipgloss/v2"

var flight = struct {
	header, mode, panel, focus, title, muted, good, warn, bad, footer lipgloss.Style
}{
	header: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7DD3FC")),
	mode:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FBBF24")),
	panel:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#475569")).Padding(0, 1),
	focus:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#67E8F9")),
	title:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E2E8F0")),
	muted:  lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")),
	good:   lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC")),
	warn:   lipgloss.NewStyle().Foreground(lipgloss.Color("#FCD34D")),
	bad:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FCA5A5")),
	footer: lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")),
}

func statusLabel(state string) string {
	switch state {
	case "completed":
		return flight.good.Render("[DONE]")
	case "abandoned":
		return flight.bad.Render("[ABANDONED]")
	default:
		return flight.warn.Render("[ACTIVE]")
	}
}
