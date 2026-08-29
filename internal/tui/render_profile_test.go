package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderProfileEnvironmentPrecedence(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		ascii, color bool
	}{
		{"default", nil, false, true},
		{"no color keeps unicode", map[string]string{"NO_COLOR": ""}, false, false},
		{"explicit ascii keeps color", map[string]string{"PITCREW_ASCII": "1"}, true, true},
		{"dumb overrides all", map[string]string{"TERM": "dumb", "PITCREW_ASCII": "0"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := renderProfileFromEnv(func(key string) (string, bool) { value, ok := tt.env[key]; return value, ok })
			if profile.ASCII != tt.ascii || profile.Color != tt.color {
				t.Fatalf("profile = %#v", profile)
			}
			if tt.ascii && profile.Glyphs.Done != "[x]" || !tt.ascii && profile.Glyphs.Done != "✓" {
				t.Fatalf("glyphs = %#v", profile.Glyphs)
			}
			rendered := profile.style(stateDone).Render(profile.Glyphs.Done + " Done")
			if !tt.color && ansi.Strip(rendered) != rendered {
				t.Fatalf("disabled profile emitted ANSI: %q", rendered)
			}
			if tt.color && !strings.Contains(rendered, "\x1b[") {
				t.Fatalf("colored profile emitted no ANSI: %q", rendered)
			}
		})
	}
}

func TestGlyphSetCentralizesTreeAndStateMeaning(t *testing.T) {
	for _, glyphs := range []glyphSet{unicodeGlyphs(), asciiGlyphs()} {
		for name, value := range map[string]string{
			"done": glyphs.Done, "active": glyphs.Active, "ready": glyphs.Ready,
			"waiting": glyphs.Waiting, "warning": glyphs.Warning, "expanded": glyphs.Expanded,
			"collapsed": glyphs.Collapsed, "branch": glyphs.Branch, "last": glyphs.Last, "selection": glyphs.Selection,
		} {
			if value == "" {
				t.Fatalf("%s glyph is empty: %#v", name, glyphs)
			}
		}
	}
}
