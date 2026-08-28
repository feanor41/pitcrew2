package version

import (
	"regexp"
	"testing"
)

const semVer2Pattern = `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`

func TestVersionCurrentIsCanonicalSemVer(t *testing.T) {
	if Current != "0.12.0" {
		t.Fatalf("Current = %q; want 0.12.0", Current)
	}
	if !regexp.MustCompile(semVer2Pattern).MatchString(Current) {
		t.Fatalf("Current = %q; want SemVer 2.0.0", Current)
	}
}

func TestVersionSemVerPatternRejectsInvalidReleaseValues(t *testing.T) {
	pattern := regexp.MustCompile(semVer2Pattern)
	for _, value := range []string{"0.2.0", "1.0.0-alpha.1", "1.2.3+build.5"} {
		if !pattern.MatchString(value) {
			t.Fatalf("valid version %q did not match SemVer 2.0.0 pattern", value)
		}
	}
	for _, value := range []string{"0.2", "v0.2.0", "01.2.0", "0.2.0-", "1.0.0-01"} {
		if pattern.MatchString(value) {
			t.Fatalf("invalid version %q matched SemVer 2.0.0 pattern", value)
		}
	}
}
