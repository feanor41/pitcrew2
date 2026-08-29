package project_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/project"
)

func TestInspectIsReadOnlyAndReportsDerivedUninitializedProject(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "checkout")
	mustMkdirAll(t, filepath.Join(checkout, ".git"))
	dataHome := filepath.Join(t.TempDir(), "absent-data")

	result, err := project.Inspect(checkout, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	if result.Project.ID == "" || result.Paths.StatePath == "" || result.Initialized || len(result.Legacy.Candidates) != 0 {
		t.Fatalf("inspection = %#v", result)
	}
	if _, err := os.Lstat(dataHome); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspection mutated data home: %v", err)
	}
}

func TestInspectReportsLegacyWithoutInitializingCentralState(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "checkout")
	mustMkdirAll(t, filepath.Join(checkout, ".git"))
	mustMkdirAll(t, filepath.Join(checkout, ".pitcrew"))
	mustWrite(t, filepath.Join(checkout, ".pitcrew", "state.db"), "legacy")
	dataHome := filepath.Join(t.TempDir(), "data")

	result, err := project.Inspect(checkout, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Legacy.Candidates) != 1 || result.Legacy.CandidateSetID == "" || result.Initialized {
		t.Fatalf("inspection = %#v", result)
	}
	if _, err := os.Lstat(dataHome); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspection initialized central state: %v", err)
	}
}

func TestInspectRejectsSymlinkedCentralParentWithoutFollowing(t *testing.T) {
	checkout := filepath.Join(t.TempDir(), "checkout")
	mustMkdirAll(t, filepath.Join(checkout, ".git"))
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Inspect(checkout, filepath.Join(link, "data")); !errors.Is(err, project.ErrUnsafePath) {
		t.Fatalf("inspection error = %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target mutated: %v, %v", entries, err)
	}
}
