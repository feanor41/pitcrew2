package agentbrief

import (
	"strings"
	"testing"
)

func TestWorkSummaryKeepsIntentAndReviewGateButDropsExecutionClauses(t *testing.T) {
	input := "Static safe brief + digest. Red→green: GOCACHE=/tmp/pc2 go test ./internal/agentbrief -run TestBrief. Review: safety/identity gate.\nCommand: bash ./scripts/tests/run.sh"
	got := workSummary(input)
	if got != "Static safe brief + digest. Review: safety/identity gate." {
		t.Fatalf("workSummary()=%q", got)
	}
	for _, forbidden := range []string{"GOCACHE=", "go test", "-run", "./internal", "bash", "./scripts"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("workSummary leaked %q: %q", forbidden, got)
		}
	}
}

func TestWorkSummaryRedactsDotRelativeReferenceWithoutDroppingIntent(t *testing.T) {
	got := workSummary("Keep the accepted behavior described in ./docs/spec.md.")
	if got != "Keep the accepted behavior described in [redacted-path]." {
		t.Fatalf("workSummary()=%q", got)
	}
}
