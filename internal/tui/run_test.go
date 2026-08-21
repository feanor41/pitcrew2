package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunExitsCleanlyOnQuit(t *testing.T) {
	var output bytes.Buffer
	if err := Run(New(fakeLoader{}), strings.NewReader("q"), &output); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "\x1b[?1049h") || !strings.Contains(got, "\x1b[?1049l") {
		t.Fatalf("runtime did not enter and restore the alternate screen: %q", got)
	}
}
