package tui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type stubEvidenceRenderer struct {
	body string
	err  error
}

func (s stubEvidenceRenderer) Render(string) (string, error) {
	return s.body, s.err
}

func TestRenderEvidenceChoosesMarkdownOnlyForBlockStructure(t *testing.T) {
	tests := []struct {
		name    string
		content string
		mode    evidenceRenderMode
	}{
		{name: "heading and list", content: "# Result\n\n- first\n- second", mode: evidenceMarkdown},
		{name: "fenced code", content: "```go\nfmt.Println(\"ok\")\n```", mode: evidenceMarkdown},
		{name: "ordinary prose", content: "A *single* emphasized phrase is still ordinary prose.", mode: evidencePlain},
		{name: "json record", content: `{"state":"complete","revision":4}`, mode: evidencePlain},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, mode := renderEvidenceWithFactory(tt.content, 24, func(int) (evidenceRenderer, error) {
				return stubEvidenceRenderer{body: "rendered markdown"}, nil
			})
			if mode != tt.mode {
				t.Fatalf("mode = %q, want %q", mode, tt.mode)
			}
			if body == "" {
				t.Fatal("rendered evidence is empty")
			}
		})
	}
}

func TestRenderEvidenceFallsBackToCompletePlainContent(t *testing.T) {
	content := "# Durable result\n\n- exact evidence remains reachable"
	tests := []struct {
		name    string
		factory evidenceRendererFactory
	}{
		{
			name: "renderer construction fails",
			factory: func(int) (evidenceRenderer, error) {
				return nil, errors.New("construction failed")
			},
		},
		{
			name: "rendering fails",
			factory: func(int) (evidenceRenderer, error) {
				return stubEvidenceRenderer{err: errors.New("render failed")}, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, mode := renderEvidenceWithFactory(content, 18, tt.factory)
			if mode != evidencePlain {
				t.Fatalf("mode = %q, want %q", mode, evidencePlain)
			}
			assertEvidenceWidth(t, body, 18)
			for _, fragment := range []string{"# Durable result", "- exact evidence", "remains reachable"} {
				if !strings.Contains(strings.ReplaceAll(body, "\n", ""), fragment) {
					t.Fatalf("fallback lost %q:\n%s", fragment, body)
				}
			}
		})
	}
}

func TestNormalizeEvidenceIsCellWidthSafe(t *testing.T) {
	tests := []struct {
		name    string
		content string
		width   int
	}{
		{name: "long token", content: strings.Repeat("abcdefghij", 8), width: 11},
		{name: "CJK", content: "履歴証拠を幅安全に表示する", width: 8},
		{name: "combining", content: strings.Repeat("e\u0301", 15), width: 7},
		{name: "emoji grapheme", content: "agents 👩🏽‍💻 coordinate 🧑‍🚀 safely", width: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := normalizeEvidence(tt.content, tt.width)
			assertEvidenceWidth(t, body, tt.width)
			if strings.ReplaceAll(body, "\n", "") != strings.ReplaceAll(tt.content, "\n", "") {
				t.Fatalf("normalization changed content:\n got %q\nwant %q", body, tt.content)
			}
		})
	}
}

func TestRenderEvidenceUsesPositiveWidth(t *testing.T) {
	body, mode := renderEvidenceWithFactory("plain", 0, nil)
	if mode != evidencePlain || body != "p\nl\na\ni\nn" {
		t.Fatalf("renderEvidenceWithFactory() = (%q, %q), want one-cell plain fallback", body, mode)
	}
}

func TestRenderEvidenceFormatsMarkdownWithinWidth(t *testing.T) {
	body, mode := renderEvidence("# Result\n\n- first item\n- second item\n\n```go\nfmt.Println(\"ok\")\n```", 32)
	if mode != evidenceMarkdown {
		t.Fatalf("mode = %q, want %q", mode, evidenceMarkdown)
	}
	assertEvidenceWidth(t, body, 32)
	plainBody := ansi.Strip(body)
	for _, fragment := range []string{"Result", "first item", "fmt.Println"} {
		if !strings.Contains(plainBody, fragment) {
			t.Fatalf("rendered markdown lost %q:\n%s", fragment, body)
		}
	}
}

func assertEvidenceWidth(t *testing.T, body string, width int) {
	t.Helper()
	for lineNumber, line := range strings.Split(body, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d width = %d, want <= %d: %q", lineNumber+1, got, width, line)
		}
	}
}
