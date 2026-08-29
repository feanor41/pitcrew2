package project_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/project"
)

func TestResolveMainAndLinkedWorktreeShareCanonicalProject(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	linked := filepath.Join(root, "linked")
	common := filepath.Join(main, ".git")
	admin := filepath.Join(common, "worktrees", "linked")
	mustMkdirAll(t, admin)
	mustMkdirAll(t, filepath.Join(linked, "nested"))
	mustWrite(t, filepath.Join(linked, ".git"), "gitdir: "+admin+"\n")
	mustWrite(t, filepath.Join(admin, "commondir"), "../..\n")
	mustWrite(t, filepath.Join(admin, "gitdir"), filepath.Join(linked, ".git")+"\n")

	mainProject, err := project.Resolve(main)
	if err != nil {
		t.Fatal(err)
	}
	linkedProject, err := project.Resolve(filepath.Join(linked, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(common)
	if err != nil {
		t.Fatal(err)
	}
	if mainProject.CommonDir != canonical || linkedProject.CommonDir != canonical {
		t.Fatalf("common dirs = %q and %q, want %q", mainProject.CommonDir, linkedProject.CommonDir, canonical)
	}
	if mainProject.ID == "" || mainProject.ID != linkedProject.ID {
		t.Fatalf("project IDs = %q and %q", mainProject.ID, linkedProject.ID)
	}
	if linkedProject.CheckoutRoot != linked {
		t.Fatalf("checkout root = %q, want %q", linkedProject.CheckoutRoot, linked)
	}
}

func TestResolveIndependentClonesAndMovedRepositoryDiffer(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, checkout := range []string{first, second} {
		mustMkdirAll(t, filepath.Join(checkout, ".git"))
	}

	firstProject, err := project.Resolve(first)
	if err != nil {
		t.Fatal(err)
	}
	secondProject, err := project.Resolve(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstProject.ID == secondProject.ID {
		t.Fatalf("independent clones shared project ID %q", firstProject.ID)
	}

	moved := filepath.Join(root, "moved")
	if err := os.Rename(first, moved); err != nil {
		t.Fatal(err)
	}
	movedProject, err := project.Resolve(moved)
	if err != nil {
		t.Fatal(err)
	}
	if movedProject.ID == firstProject.ID {
		t.Fatalf("moved repository retained path-derived ID %q", movedProject.ID)
	}
}

func TestResolveRejectsForgedOrEscapingGitMetadata(t *testing.T) {
	root := t.TempDir()
	tests := map[string]func(string){
		"malformed git file": func(checkout string) {
			mustWrite(t, filepath.Join(checkout, ".git"), "not-a-gitdir\n")
		},
		"symlinked git metadata": func(checkout string) {
			outside := filepath.Join(root, "outside.git")
			mustMkdirAll(t, outside)
			if err := os.Symlink(outside, filepath.Join(checkout, ".git")); err != nil {
				t.Fatal(err)
			}
		},
		"escaping common dir": func(checkout string) {
			common := filepath.Join(root, "escape-main", ".git")
			admin := filepath.Join(common, "worktrees", "escape")
			mustMkdirAll(t, admin)
			mustWrite(t, filepath.Join(checkout, ".git"), "gitdir: "+admin+"\n")
			mustWrite(t, filepath.Join(admin, "commondir"), "../../..\n")
			mustWrite(t, filepath.Join(admin, "gitdir"), filepath.Join(checkout, ".git")+"\n")
		},
		"forged backlink": func(checkout string) {
			common := filepath.Join(root, "forged-main", ".git")
			admin := filepath.Join(common, "worktrees", "forged")
			mustMkdirAll(t, admin)
			mustWrite(t, filepath.Join(checkout, ".git"), "gitdir: "+admin+"\n")
			mustWrite(t, filepath.Join(admin, "commondir"), "../..\n")
			mustWrite(t, filepath.Join(admin, "gitdir"), filepath.Join(root, "someone-else", ".git")+"\n")
		},
	}

	for name, arrange := range tests {
		t.Run(name, func(t *testing.T) {
			checkout := filepath.Join(root, filepath.Base(name))
			mustMkdirAll(t, checkout)
			arrange(checkout)
			if _, err := project.Resolve(checkout); err == nil {
				t.Fatal("Resolve accepted unsafe Git metadata")
			}
		})
	}
}

func TestResolveRejectsDirectoryOutsideRepository(t *testing.T) {
	if _, err := project.Resolve(string(filepath.Separator)); err == nil {
		t.Fatal("Resolve accepted a directory outside a Git checkout")
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
