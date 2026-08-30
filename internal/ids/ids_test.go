package ids

import (
	"bytes"
	"errors"
	"regexp"
	"testing"
	"time"
)

func TestIdentifiersUseRequiredFormatsAndEntropy(t *testing.T) {
	tests := []struct {
		name    string
		newID   func() (string, error)
		pattern string
	}{
		{"workflow", NewWorkflow, `^wf-[0-9a-f]{24}$`},
		{"work unit", NewWorkUnit, `^wu-[0-9a-f]{24}$`},
		{"delivery", NewDelivery, `^dl-[0-9a-f]{24}$`},
		{"claim", NewClaim, `^[0-9a-f]{32}$`},
		{"secret", NewSecret, `^[0-9a-f]{32}$`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := tt.newID()
			if err != nil || !regexp.MustCompile(tt.pattern).MatchString(first) {
				t.Fatalf("new id = %q, %v", first, err)
			}
			second, err := tt.newID()
			if err != nil || second == first {
				t.Fatalf("second id = %q, %v; first = %q", second, err, first)
			}
		})
	}
}

func TestIdentifierGenerationReadsItsEntropySource(t *testing.T) {
	got, err := newPrefixed(bytes.NewReader(make([]byte, 32)), "wf-", 12)
	if err != nil || got != "wf-66687aadf862bd776c8fc18b" {
		t.Fatalf("newPrefixed() = %q, %v", got, err)
	}
	if _, err := newHex(errReader{}, 16); err == nil {
		t.Fatal("newHex() accepted a failed entropy source")
	}
}

func TestFormatTimeUsesRFC3339MillisecondsInUTC(t *testing.T) {
	input := time.Date(2026, 8, 20, 12, 34, 56, 987654321, time.FixedZone("local", -3*60*60))
	if got := FormatTime(input); got != "2026-08-20T15:34:56.987Z" {
		t.Fatalf("FormatTime() = %q", got)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
