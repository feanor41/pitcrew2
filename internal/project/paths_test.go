package project_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/project"
)

func TestPathsDerivesOpaqueCentralLocationsWithoutMutation(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "absent-data-home")
	projectID := strings.Repeat("a", 64)

	paths, err := project.DerivePaths(dataHome, projectID)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(dataHome, "pitcrew", "projects", projectID)
	if paths.ProjectRoot != wantRoot ||
		paths.IdentityPath != filepath.Join(wantRoot, "identity.json") ||
		paths.StatePath != filepath.Join(wantRoot, "state.db") ||
		paths.WorktreeRoot != filepath.Join(wantRoot, "worktrees") ||
		paths.HandleRoot != filepath.Join(wantRoot, "handles") {
		t.Fatalf("unexpected paths: %#v", paths)
	}
	if _, err := os.Stat(dataHome); !os.IsNotExist(err) {
		t.Fatalf("DerivePaths mutated data home: %v", err)
	}
}

func TestPathsRejectsRelativeHomeAndEscapingProjectID(t *testing.T) {
	validID := strings.Repeat("b", 64)
	for name, input := range map[string][2]string{
		"relative data home": {"relative", validID},
		"short project ID":   {t.TempDir(), "abc"},
		"escaping ID":        {t.TempDir(), "../" + validID},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := project.DerivePaths(input[0], input[1]); err == nil {
				t.Fatal("DerivePaths accepted unsafe input")
			}
		})
	}
}
