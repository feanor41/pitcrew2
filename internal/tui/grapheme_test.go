package tui

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestGraphemeEllipsizeNeverSplitsClusters(t *testing.T) {
	for _, tt := range []struct {
		name, value string
		width       int
		want        string
	}{
		{"combining", "Ame\u0301lie", 4, "Ame\u0301…"},
		{"cjk", "界界界", 5, "界界…"},
		{"zwj", "A👩‍💻B", 3, "A…"},
		{"variation selector", "✈️ trip", 3, "✈️…"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := graphemeEllipsize(tt.value, tt.width); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
			if got := lipgloss.Width(graphemeEllipsize(tt.value, tt.width)); got > tt.width {
				t.Fatalf("width %d > %d", got, tt.width)
			}
		})
	}
}

func TestGraphemeWrapHardBreaksAndTerminatesOnZeroWidth(t *testing.T) {
	got := graphemeWrap("e\u0301界\n\u0301x", 2)
	want := []string{"e\u0301", "界", "\u0301x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for _, line := range got {
		if lipgloss.Width(line) > 2 {
			t.Fatalf("line too wide: %q", line)
		}
	}
	if got := graphemeWrap("abc", 0); len(got) != 1 || got[0] != "" {
		t.Fatalf("zero width = %#v", got)
	}
}

func TestGraphemeWrapReplacesAnIndivisibleOverwideCluster(t *testing.T) {
	lines := graphemeWrap("界", 1)
	if !reflect.DeepEqual(lines, []string{"…"}) {
		t.Fatalf("over-wide cluster policy = %#v", lines)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 1 {
			t.Fatalf("line width %d exceeds requested width: %q", got, line)
		}
	}
}
