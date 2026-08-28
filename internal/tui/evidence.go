package tui

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

type evidenceRenderMode string

const (
	evidencePlain    evidenceRenderMode = "plain"
	evidenceMarkdown evidenceRenderMode = "markdown"
)

type evidenceRenderer interface {
	Render(string) (string, error)
}

type evidenceRendererFactory func(width int) (evidenceRenderer, error)

var markdownBlock = regexp.MustCompile(`(?m)^[\t ]{0,3}(?:#{1,6}[\t ]+|[-*+][\t ]+|\d+[.)][\t ]+|>[\t ]?|` + "```" + `|~~~)`)

func renderEvidence(content string, width int) (string, evidenceRenderMode) {
	return renderEvidenceWithFactory(content, width, newEvidenceRenderer)
}

func renderEvidenceWithFactory(content string, width int, factory evidenceRendererFactory) (string, evidenceRenderMode) {
	width = max(1, width)
	if !markdownBlock.MatchString(content) || factory == nil {
		return normalizeEvidence(content, width), evidencePlain
	}

	renderer, err := factory(width)
	if err != nil {
		return normalizeEvidence(content, width), evidencePlain
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return normalizeEvidence(content, width), evidencePlain
	}
	return normalizeEvidence(strings.TrimRight(rendered, "\n"), width), evidenceMarkdown
}

func newEvidenceRenderer(width int) (evidenceRenderer, error) {
	return glamour.NewTermRenderer(
		glamour.WithStandardStyle(styles.DarkStyle),
		glamour.WithWordWrap(max(1, width)),
	)
}

func normalizeEvidence(content string, width int) string {
	width = max(1, width)
	var result strings.Builder
	lineWidth := 0
	iterator := graphemes.FromString(content)
	iterator.AnsiEscapeSequences = true
	for iterator.Next() {
		cluster := iterator.Value()
		if cluster == "\n" {
			result.WriteByte('\n')
			lineWidth = 0
			continue
		}
		clusterWidth := lipgloss.Width(cluster)
		if lineWidth > 0 && lineWidth+clusterWidth > width {
			result.WriteByte('\n')
			lineWidth = 0
		}
		result.WriteString(cluster)
		lineWidth += clusterWidth
	}
	return result.String()
}
