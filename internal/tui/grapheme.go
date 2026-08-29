package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

func graphemeEllipsize(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if !strings.Contains(value, "\n") && lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var result strings.Builder
	used := 0
	iterator := graphemes.FromString(value)
	iterator.AnsiEscapeSequences = true
	for iterator.Next() {
		cluster := iterator.Value()
		if cluster == "\n" {
			break
		}
		cells := lipgloss.Width(cluster)
		if used+cells+1 > width {
			break
		}
		result.WriteString(cluster)
		used += cells
	}
	return result.String() + "…"
}

func graphemeWrap(value string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	lines := []string{}
	var line strings.Builder
	used := 0
	flush := func() {
		lines = append(lines, line.String())
		line.Reset()
		used = 0
	}
	iterator := graphemes.FromString(value)
	iterator.AnsiEscapeSequences = true
	for iterator.Next() {
		cluster := iterator.Value()
		if cluster == "\n" {
			flush()
			continue
		}
		cells := lipgloss.Width(cluster)
		if cells > width {
			if used > 0 {
				flush()
			}
			line.WriteString("…")
			used = 1
			flush()
			continue
		}
		if used > 0 && used+cells > width {
			flush()
		}
		line.WriteString(cluster)
		used += cells
	}
	if line.Len() > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}
