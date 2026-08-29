package project_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/project"
)

func TestLegacyDiscoversMainAndValidLinkedCheckoutCandidates(t *testing.T) {
	main, linked := linkedFixture(t)
	for index, checkout := range []string{main, linked} {
		mustMkdirAll(t, filepath.Join(checkout, ".pitcrew"))
		mustWrite(t, filepath.Join(checkout, ".pitcrew", "state.db"), string(rune('a'+index)))
	}
	resolved, err := project.Resolve(linked)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := project.DiscoverLegacy(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Candidates) != 2 || discovery.CandidateSetID == "" || len(discovery.Diagnostics) != 0 {
		t.Fatalf("discovery = %#v", discovery)
	}
	before := discovery.CandidateSetID
	mustWrite(t, filepath.Join(linked, ".pitcrew", "state.db"), "changed")
	discovery, err = project.DiscoverLegacy(resolved)
	if err != nil || discovery.CandidateSetID == before {
		t.Fatalf("changed discovery = %#v, %v", discovery, err)
	}
}

func TestLegacyDiagnosesStaleOrUnsafeEntriesWithoutFollowingThem(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main")
	common := filepath.Join(main, ".git")
	staleAdmin := filepath.Join(common, "worktrees", "stale")
	mustMkdirAll(t, staleAdmin)
	outside := filepath.Join(t.TempDir(), "outside")
	mustMkdirAll(t, filepath.Join(outside, ".pitcrew"))
	mustWrite(t, filepath.Join(outside, ".pitcrew", "state.db"), "unrelated")
	mustWrite(t, filepath.Join(staleAdmin, "gitdir"), filepath.Join(outside, ".git")+"\n")
	resolved, err := project.Resolve(main)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := project.DiscoverLegacy(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Candidates) != 0 || len(discovery.Diagnostics) != 1 {
		t.Fatalf("discovery = %#v", discovery)
	}
}

func TestLegacyDiagnosesSymlinkDatabase(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main")
	mustMkdirAll(t, filepath.Join(main, ".git"))
	mustMkdirAll(t, filepath.Join(main, ".pitcrew"))
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.db"), filepath.Join(main, ".pitcrew", "state.db")); err != nil {
		t.Fatal(err)
	}
	resolved, err := project.Resolve(main)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := project.DiscoverLegacy(resolved)
	if err != nil || len(discovery.Candidates) != 0 || len(discovery.Diagnostics) != 1 {
		t.Fatalf("discovery = %#v, %v", discovery, err)
	}
}

func TestLegacyDiagnosesGroupOrWorldWritableProtectedPaths(t *testing.T) {
	for _, unsafePath := range []string{"directory", "database", "wal"} {
		t.Run(unsafePath, func(t *testing.T) {
			main := filepath.Join(t.TempDir(), "main")
			legacyDir := filepath.Join(main, ".pitcrew")
			statePath := filepath.Join(legacyDir, "state.db")
			mustMkdirAll(t, filepath.Join(main, ".git"))
			mustMkdirAll(t, legacyDir)
			mustWrite(t, statePath, "legacy")
			if unsafePath == "wal" {
				mustWrite(t, statePath+"-wal", "wal")
			}
			path := map[string]string{"directory": legacyDir, "database": statePath, "wal": statePath + "-wal"}[unsafePath]
			if err := os.Chmod(path, map[string]os.FileMode{"directory": 0o777, "database": 0o666, "wal": 0o666}[unsafePath]); err != nil {
				t.Fatal(err)
			}
			resolved, err := project.Resolve(main)
			if err != nil {
				t.Fatal(err)
			}
			discovery, err := project.DiscoverLegacy(resolved)
			if err != nil || len(discovery.Candidates) != 0 || len(discovery.Diagnostics) != 1 || discovery.CandidateSetID != "" {
				t.Fatalf("discovery = %#v, %v", discovery, err)
			}
		})
	}
}

func TestLegacyCandidateSetTracksSafePermissionChanges(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main")
	statePath := filepath.Join(main, ".pitcrew", "state.db")
	mustMkdirAll(t, filepath.Join(main, ".git"))
	mustMkdirAll(t, filepath.Dir(statePath))
	mustWrite(t, statePath, "legacy")
	resolved, err := project.Resolve(main)
	if err != nil {
		t.Fatal(err)
	}
	before, err := project.DiscoverLegacy(resolved)
	if err != nil || len(before.Candidates) != 1 {
		t.Fatalf("initial discovery = %#v, %v", before, err)
	}
	if err := os.Chmod(statePath, 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := project.DiscoverLegacy(resolved)
	if err != nil || len(after.Candidates) != 1 || len(after.Diagnostics) != 0 || after.CandidateSetID == before.CandidateSetID {
		t.Fatalf("changed discovery = %#v, %v", after, err)
	}
}

func TestLegacyDiagnosesSymlinkedParentWithoutFingerprintingOutside(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main")
	mustMkdirAll(t, filepath.Join(main, ".git"))
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "state.db"), "outside")
	if err := os.Symlink(outside, filepath.Join(main, ".pitcrew")); err != nil {
		t.Fatal(err)
	}
	resolved, err := project.Resolve(main)
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := project.DiscoverLegacy(resolved)
	if err != nil || len(discovery.Candidates) != 0 || len(discovery.Diagnostics) != 1 {
		t.Fatalf("discovery = %#v, %v", discovery, err)
	}
	content, err := os.ReadFile(filepath.Join(outside, "state.db"))
	if err != nil || string(content) != "outside" {
		t.Fatalf("outside content = %q, %v", content, err)
	}
}

func TestGateRejectsUnacknowledgedOrChangedLegacySet(t *testing.T) {
	discovery := project.LegacyDiscovery{Candidates: []project.LegacyCandidate{{ID: "candidate"}}, CandidateSetID: "set-v1"}
	if err := project.GateLegacy(discovery, ""); !errors.Is(err, project.ErrMigrationRequired) {
		t.Fatalf("unacknowledged gate error = %v", err)
	}
	if err := project.GateLegacy(discovery, "set-v1"); err != nil {
		t.Fatal(err)
	}
	discovery.CandidateSetID = "set-v2"
	if err := project.GateLegacy(discovery, "set-v1"); !errors.Is(err, project.ErrMigrationRequired) {
		t.Fatalf("changed-set gate error = %v", err)
	}
	if err := project.GateLegacy(project.LegacyDiscovery{}, ""); err != nil {
		t.Fatal(err)
	}
}

func linkedFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	main, linked := filepath.Join(root, "main"), filepath.Join(root, "linked")
	common := filepath.Join(main, ".git")
	admin := filepath.Join(common, "worktrees", "linked")
	mustMkdirAll(t, admin)
	mustMkdirAll(t, linked)
	mustWrite(t, filepath.Join(linked, ".git"), "gitdir: "+admin+"\n")
	mustWrite(t, filepath.Join(admin, "commondir"), "../..\n")
	mustWrite(t, filepath.Join(admin, "gitdir"), filepath.Join(linked, ".git")+"\n")
	return main, linked
}
