package project_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/project"
)

func TestCaptureRepositoryFingerprintIdentifiesEmptyIndexAndUntrackedSymlink(t *testing.T) {
	checkout := initRepository(t)
	resolved, err := project.Resolve(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(checkout, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "add", "--all", "--")
	emptyIndex, err := project.CaptureRepositoryFingerprint(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !emptyIndex.Dirty || !emptyIndex.Staged || emptyIndex.Unstaged || emptyIndex.Untracked {
		t.Fatalf("empty-index fingerprint = %#v", emptyIndex)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside content must not be followed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(checkout, "untracked-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	first, err := project.CaptureRepositoryFingerprint(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Untracked {
		t.Fatalf("symlink fingerprint = %#v", first)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("different-target", link); err != nil {
		t.Fatal(err)
	}
	second, err := project.CaptureRepositoryFingerprint(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if second.ResultDigest == first.ResultDigest {
		t.Fatal("untracked symlink target did not affect result digest")
	}
}

func TestCaptureRepositoryFingerprintDisablesConfiguredFSMonitor(t *testing.T) {
	checkout := initRepository(t)
	sentinel := filepath.Join(t.TempDir(), "fsmonitor-executed")
	helper := filepath.Join(t.TempDir(), "fsmonitor")
	script := "#!/bin/sh\n: > " + strconv.Quote(sentinel) + "\nexit 0\n"
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "config", "core.fsmonitor", helper)
	resolved, err := project.Resolve(checkout)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := project.CaptureRepositoryFingerprint(resolved); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("configured fsmonitor executed: %v", err)
	}
}

func TestCaptureRepositoryFingerprintRejectsHeadChangeDuringCapture(t *testing.T) {
	checkout := initRepository(t)
	if err := os.WriteFile(filepath.Join(checkout, "tracked.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "add", "--", "tracked.txt")
	mustRunGit(t, checkout, "commit", "--quiet", "-m", "second")
	resolved, err := project.Resolve(checkout)
	if err != nil {
		t.Fatal(err)
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	parentOutput, err := exec.Command(realGit, "-C", checkout, "rev-parse", "HEAD^").Output()
	if err != nil {
		t.Fatal(err)
	}
	parent := string(parentOutput[:len(parentOutput)-1])
	bin := t.TempDir()
	counter := filepath.Join(bin, "counter")
	wrapper := filepath.Join(bin, "git")
	script := "#!/bin/sh\n" +
		"n=0; test ! -f " + strconv.Quote(counter) + " || n=$(cat " + strconv.Quote(counter) + ")\n" +
		"n=$((n+1)); printf '%s' \"$n\" > " + strconv.Quote(counter) + "\n" +
		"if test \"$n\" -eq 5; then " + strconv.Quote(realGit) + " -C " + strconv.Quote(checkout) + " update-ref HEAD " + strconv.Quote(parent) + " || exit 91; fi\n" +
		"exec " + strconv.Quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := project.CaptureRepositoryFingerprint(resolved); err == nil {
		count, _ := os.ReadFile(counter)
		t.Fatalf("CaptureRepositoryFingerprint accepted a hybrid digest after HEAD changed (git calls %q)", count)
	}
}
