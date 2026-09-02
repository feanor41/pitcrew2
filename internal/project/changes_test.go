package project_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

func TestMeasureChangedLinesCoversRepositoryStatesWithinScope(t *testing.T) {
	checkout := initRepository(t)
	for name, content := range map[string]string{
		"scope/modified.txt":  "old\n",
		"scope/deleted.txt":   "gone\none\n",
		"scope/renamed.txt":   "rename\n",
		"scope/committed.txt": "base\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(checkout, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(checkout, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustRunGit(t, checkout, "add", "scope")
	mustRunGit(t, checkout, "commit", "--quiet", "-m", "scope base")
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "scope", "modified.txt"), []byte("new\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(checkout, "scope", "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "mv", "scope/renamed.txt", "scope/renamed-next.txt")
	if err = os.WriteFile(filepath.Join(checkout, "scope", "committed.txt"), []byte("commit\nnext\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "add", "scope/committed.txt")
	mustRunGit(t, checkout, "commit", "--quiet", "-m", "after baseline")
	if err = os.WriteFile(filepath.Join(checkout, "scope", "staged.txt"), []byte("stage\none\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "add", "scope/staged.txt")
	if err = os.WriteFile(filepath.Join(checkout, "scope", "untracked.txt"), []byte("untracked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, ".gitignore"), []byte("scope/ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "scope", "ignored.txt"), []byte("ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	measurement, err := project.MeasureChangedLines(baseline, []string{"scope", "scope/modified.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if measurement.Additions != 7 || measurement.Deletions != 4 || measurement.ChangedLines != 11 {
		t.Fatalf("measurement=%#v, want additions=7 deletions=4 total=11", measurement)
	}
}

func TestMeasureChangedLinesDigestIsScoped(t *testing.T) {
	checkout := initRepository(t)
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	first, err := project.MeasureChangedLines(baseline, []string{"tracked.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "outside.txt"), []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := project.MeasureChangedLines(baseline, []string{"tracked.txt"})
	if err != nil || second != first {
		t.Fatalf("outside-scope dirt changed measurement: first=%#v second=%#v err=%v", first, second, err)
	}
	if err = os.WriteFile(filepath.Join(checkout, "tracked.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	third, err := project.MeasureChangedLines(baseline, []string{"tracked.txt"})
	if err != nil || third.ResultDigest == first.ResultDigest {
		t.Fatalf("scoped edit did not change digest: first=%#v third=%#v err=%v", first, third, err)
	}
}

func TestNormalizeChangeScopesRemovesDuplicatesAndCoveredChildren(t *testing.T) {
	scopes, err := project.NormalizeChangeScopes([]string{"a/z", "a-", "a", "a/z"})
	if err != nil || !reflect.DeepEqual(scopes, []string{"a", "a-"}) {
		t.Fatalf("scopes=%v err=%v", scopes, err)
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
	shim := "#!/bin/sh\nfor arg do\n" +
		"  if [ \"$arg\" = --stage ] && [ \"$PC_NUMSTAT_CASE\" = submodule ]; then printf '160000 fake 0\\tsafe/module\\000'; exit; fi\n" +
		"  [ \"$arg\" = --numstat ] && break\ndone\n" +
		"[ \"$arg\" = --numstat ] || exec " + realGit + " \"$@\"\n" +
		"case \"$PC_NUMSTAT_CASE\" in\n" +
		"missing) printf '1\\t0\\tsafe/file'; exit;;\n" +
		"trailing) printf '1\\t0\\tsafe/file\\000garbage\\000'; exit;;\n" +
		"duplicate) printf '1\\t0\\tsafe/file\\0001\\t0\\tsafe/file\\000'; exit;;\n" +
		"rename) printf '0\\t0\\t\\000safe/old\\000'; exit;;\n" +
		"outside) printf '1\\t0\\toutside\\000'; exit;;\n" +
		"overflow) printf '9223372036854775807\\t1\\tsafe/file\\000'; exit;;\n" +
		"binary) printf -- '-\\t-\\tsafe/file\\000'; exit;;\n" +
		"failure) exit 23;;\n" +
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
	for _, scenario := range []string{"missing", "trailing", "duplicate", "rename", "outside", "overflow", "binary", "submodule", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("PC_NUMSTAT_CASE", scenario)
			if _, err := project.MeasureChangedLines(baseline, []string{"safe"}); err == nil {
				t.Fatal("malformed Git numstat was accepted")
			}
		})
	}
}

func TestMeasureChangedLinesRejectsUnstableCapture(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin, counter := t.TempDir(), filepath.Join(t.TempDir(), "calls")
	shim := "#!/bin/sh\nfor arg do [ \"$arg\" = --numstat ] && break; done\n" +
		"[ \"$arg\" = --numstat ] || exec " + realGit + " \"$@\"\n" +
		"n=0; [ -f \"$PC_RACE_COUNT\" ] && n=$(cat \"$PC_RACE_COUNT\"); n=$((n+1)); printf '%s' \"$n\" > \"$PC_RACE_COUNT\"; printf '%s\\t0\\tsafe/file\\000' \"$n\"\n"
	if err = os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	checkout := initRepository(t)
	baseline, err := project.CaptureChangeBaseline(checkout)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PC_RACE_COUNT", counter)
	if _, err = project.MeasureChangedLines(baseline, []string{"safe"}); err == nil || !strings.Contains(err.Error(), "changed during capture") {
		t.Fatalf("unstable capture error=%v", err)
	}
}
