package tui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

type flightStyles struct {
	brand, subtitle, version, mode, border, panel, focus, title, label, muted, good, warn, bad, footer lipgloss.Style
}

var flight = newFlightStyles(false)

func newFlightStyles(noColor bool) flightStyles {
	styles := flightStyles{
		brand:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E0F2FE")),
		subtitle: lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")),
		version:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F472B6")),
		mode:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FBBF24")),
		border:   lipgloss.NewStyle().Foreground(lipgloss.Color("#475569")),
		panel:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#475569")).Padding(0, 1),
		focus:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#67E8F9")),
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E2E8F0")),
		label:    lipgloss.NewStyle().Bold(true),
		muted:    lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8")),
		good:     lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC")),
		warn:     lipgloss.NewStyle().Foreground(lipgloss.Color("#FCD34D")),
		bad:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FCA5A5")),
		footer:   lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")),
	}
	if !noColor {
		return styles
	}
	plain := lipgloss.NewStyle()
	return flightStyles{
		brand: plain, subtitle: plain, version: plain, mode: plain, border: plain, panel: plain,
		focus: plain, title: plain, label: plain, muted: plain, good: plain,
		warn: plain, bad: plain, footer: plain,
	}
}

func noColorEnabled() bool {
	_, present := os.LookupEnv("NO_COLOR")
	return present
}

func statusLabel(state string) string {
	label := statusText(state)
	switch state {
	case "completed":
		return flight.good.Render(label)
	case "abandoned":
		return flight.bad.Bold(false).Render(label)
	default:
		return flight.warn.Render(label)
	}
}

func statusText(state string) string {
	if state == "completed" {
		return "[DONE]"
	}
	return "[" + strings.ToUpper(strings.ReplaceAll(state, "_", " ")) + "]"
}

func fitStatus(state string, width int) string {
	label := fitText(statusText(state), width)
	switch state {
	case "completed":
		return flight.good.Render(label)
	case "abandoned":
		return flight.bad.Bold(false).Render(label)
	default:
		return flight.warn.Render(label)
	}
}
