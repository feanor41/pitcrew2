package project_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/project"
)

func TestMeasureChangedLinesCountsTrackedCommittedAndUntrackedText(t *testing.T) {
	checkout := initRepository(t)
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "tracked.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "add", "--", "tracked.txt")
	mustRunGit(t, checkout, "commit", "--quiet", "-m", "post claim")
	if err = os.WriteFile(filepath.Join(checkout, "new.txt"), []byte("a\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(checkout, "outside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "outside", "ignored-by-scope.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	measurement, err := project.MeasureChangedLines(baseline, []string{"tracked.txt", "new.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.ChangedLines != 5 || measurement.ResultDigest == "" {
		t.Fatalf("measurement = %#v, want 5 changed lines and an audit digest", measurement)
	}
}

func TestChangedLineMeasurementFailsClosed(t *testing.T) {
	checkout := initRepository(t)
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "binary.dat"), []byte{'a', 0, 'b'}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = project.MeasureChangedLines(baseline, []string{"binary.dat"}); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("binary measurement error = %v", err)
	}
	if _, err = project.MeasureChangedLines(baseline, []string{"../escape"}); err == nil {
		t.Fatal("unsafe scope was accepted")
	}

	baseline.ResultDigest = "corrupt"
	if _, err = project.MeasureChangedLines(baseline, []string{"tracked.txt"}); err == nil || !strings.Contains(err.Error(), "corrupt baseline") {
		t.Fatalf("corrupt baseline error = %v", err)
	}
}

func TestMeasureChangedLinesTreatsScopesAsLiteralPaths(t *testing.T) {
	checkout := initRepository(t)
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "outside.txt"), []byte("not in scope\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	measurement, err := project.MeasureChangedLines(baseline, []string{":!safe"})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.ChangedLines != 0 {
		t.Fatalf("literal exclusion-like scope measured %d outside lines", measurement.ChangedLines)
	}
}

func TestMeasureChangedLinesRejectsMalformedGitNumstat(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	shim := "#!/bin/sh\nfor arg do [ \"$arg\" = --numstat ] && break; done\n" +
		"[ \"$arg\" = --numstat ] || exec " + realGit + " \"$@\"\n" +
		"case \"$PC_NUMSTAT_CASE\" in\n" +
		"missing) printf '1\\t0\\tsafe/file'; exit;;\n" +
		"trailing) printf '1\\t0\\tsafe/file\\000garbage\\000'; exit;;\n" +
		"duplicate) printf '1\\t0\\tsafe/file\\0001\\t0\\tsafe/file\\000'; exit;;\n" +
		"rename) printf '0\\t0\\t\\000safe/old\\000'; exit;;\n" +
		"outside) printf '1\\t0\\toutside\\000'; exit;;\n" +
		"overflow) printf '9223372036854775807\\t1\\tsafe/file\\000'; exit;;\n" +
		"binary) printf -- '-\\t-\\tsafe/file\\000'; exit;;\n" +
		"esac\nexec " + realGit + " \"$@\"\n"
	if err = os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	checkout := initRepository(t)
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, scenario := range []string{"missing", "trailing", "duplicate", "rename", "outside", "overflow", "binary"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("PC_NUMSTAT_CASE", scenario)
			if _, err := project.MeasureChangedLines(baseline, []string{"safe"}); err == nil {
				t.Fatal("malformed Git numstat was accepted")
			}
		})
	}
}
