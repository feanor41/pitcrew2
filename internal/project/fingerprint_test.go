package project_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/project"
)

func TestCaptureRepositoryFingerprintDistinguishesCleanAndDirtyState(t *testing.T) {
	checkout := initRepository(t)
	resolved, err := project.Resolve(checkout)
	if err != nil {
		t.Fatal(err)
	}

	clean, err := project.CaptureRepositoryFingerprint(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if clean.ProjectID != resolved.ID || clean.CheckoutRoot != resolved.CheckoutRoot || clean.HeadRevision == "" || clean.ResultDigest == "" {
		t.Fatalf("clean fingerprint = %#v", clean)
	}
	if clean.Dirty || clean.Staged || clean.Unstaged || clean.Untracked {
		t.Fatalf("clean fingerprint reported dirt: %#v", clean)
	}

	mustRunGit(t, checkout, "checkout", "--", "tracked.txt")
	if err := os.WriteFile(filepath.Join(checkout, "tracked.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "add", "--", "tracked.txt")
	staged, err := project.CaptureRepositoryFingerprint(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !staged.Dirty || !staged.Staged || staged.Unstaged || staged.Untracked || staged.ResultDigest == clean.ResultDigest {
		t.Fatalf("staged fingerprint = %#v", staged)
	}

	if err := os.WriteFile(filepath.Join(checkout, "tracked.txt"), []byte("worktree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unstaged, err := project.CaptureRepositoryFingerprint(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !unstaged.Staged || !unstaged.Unstaged || unstaged.Untracked || unstaged.ResultDigest == staged.ResultDigest {
		t.Fatalf("unstaged fingerprint = %#v", unstaged)
	}

	if err := os.WriteFile(filepath.Join(checkout, "untracked.txt"), []byte("untracked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	untracked, err := project.CaptureRepositoryFingerprint(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !untracked.Staged || !untracked.Unstaged || !untracked.Untracked || untracked.ResultDigest == unstaged.ResultDigest {
		t.Fatalf("untracked fingerprint = %#v", untracked)
	}
}

func TestCaptureRepositoryFingerprintBindsCanonicalCheckoutAndRejectsGitFailure(t *testing.T) {
	first := initRepository(t)
	second := initRepository(t)
	resolved, err := project.Resolve(first)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := project.Resolve(second)
	if err != nil {
		t.Fatal(err)
	}

	forged := resolved
	forged.CheckoutRoot = foreign.CheckoutRoot
	if _, err := project.CaptureRepositoryFingerprint(forged); err == nil {
		t.Fatal("CaptureRepositoryFingerprint accepted a foreign checkout root")
	}
	forged = resolved
	forged.CheckoutRoot = "relative"
	if _, err := project.CaptureRepositoryFingerprint(forged); err == nil {
		t.Fatal("CaptureRepositoryFingerprint accepted a relative checkout root")
	}

	if err := os.Rename(filepath.Join(first, ".git"), filepath.Join(first, ".git-away")); err != nil {
		t.Fatal(err)
	}
	if _, err := project.CaptureRepositoryFingerprint(resolved); err == nil {
		t.Fatal("CaptureRepositoryFingerprint accepted a checkout after Git failed")
	}
}

func initRepository(t *testing.T) string {
	t.Helper()
	checkout := filepath.Join(t.TempDir(), "--closed-argv-checkout")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "init", "--quiet")
	mustRunGit(t, checkout, "config", "user.email", "pitcrew@example.test")
	mustRunGit(t, checkout, "config", "user.name", "PitCrew Test")
	if err := os.WriteFile(filepath.Join(checkout, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, checkout, "add", "--", "tracked.txt")
	mustRunGit(t, checkout, "commit", "--quiet", "-m", "base")
	return checkout
}

func mustRunGit(t *testing.T, checkout string, args ...string) {
	t.Helper()
	argv := append([]string{"-C", checkout}, args...)
	command := exec.Command("git", argv...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", argv, err, output)
	}
}
