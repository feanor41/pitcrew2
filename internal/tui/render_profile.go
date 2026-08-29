package tui

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

type glyphSet struct {
	Done, Active, Ready, Waiting, Warning string
	Expanded, Collapsed, Branch, Last     string
	Selection                             string
}

type renderProfile struct {
	ASCII, Color                                       bool
	Glyphs                                             glyphSet
	ActiveBorder, InactiveBorder, SelectedRow          lipgloss.Style
	Success, Running, Ready, Waiting, Warning, Failure lipgloss.Style
	Muted, Title, Label, Footer                        lipgloss.Style
}

func currentRenderProfile() renderProfile { return renderProfileFromEnv(os.LookupEnv) }

func renderProfileFromEnv(lookup func(string) (string, bool)) renderProfile {
	term, _ := lookup("TERM")
	dumb := strings.EqualFold(term, "dumb")
	asciiValue, _ := lookup("PITCREW_ASCII")
	ascii := dumb || asciiValue == "1"
	_, noColor := lookup("NO_COLOR")
	profile := renderProfile{ASCII: ascii, Color: !dumb && !noColor, Glyphs: unicodeGlyphs()}
	if ascii {
		profile.Glyphs = asciiGlyphs()
	}
	profile.applyStyles(profile.Color)
	return profile
}

func (p *renderProfile) applyStyles(color bool) {
	if !color {
		plain := lipgloss.NewStyle()
		p.ActiveBorder, p.InactiveBorder, p.SelectedRow = plain, plain, plain
		p.Success, p.Running, p.Ready, p.Waiting = plain, plain, plain, plain
		p.Warning, p.Failure, p.Muted = plain, plain, plain
		p.Title, p.Label, p.Footer = plain, plain, plain
		return
	}
	style := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	p.ActiveBorder, p.InactiveBorder = style("#67E8F9").Bold(true), style("#475569")
	p.SelectedRow, p.Success, p.Running = style("#E0F2FE").Bold(true), style("#86EFAC"), style("#67E8F9")
	p.Ready, p.Waiting, p.Warning = style("#C4B5FD"), style("#94A3B8"), style("#FCD34D")
	p.Failure, p.Muted, p.Title = style("#FCA5A5").Bold(true), style("#94A3B8"), style("#E2E8F0").Bold(true)
	p.Label = lipgloss.NewStyle().Bold(true)
	p.Footer = style("#CBD5E1")
}

func (p renderProfile) style(state semanticState) lipgloss.Style {
	styles := [...]lipgloss.Style{p.Muted, p.Success, p.Running, p.Ready, p.Waiting, p.Warning, p.Failure}
	if int(state) >= len(styles) {
		return p.Muted
	}
	return styles[state]
}

func unicodeGlyphs() glyphSet {
	return glyphSet{Done: "✓", Active: "●", Ready: "›", Waiting: "○", Warning: "!", Expanded: "▾", Collapsed: "▸", Branch: "├─", Last: "└─", Selection: "▶"}
}

func asciiGlyphs() glyphSet {
	return glyphSet{Done: "[x]", Active: "[*]", Ready: "[>]", Waiting: "[ ]", Warning: "[!]", Expanded: "[-]", Collapsed: "[+]", Branch: "|-", Last: "`-", Selection: ">"}
}
