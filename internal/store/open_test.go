package store

import (
	"context"
	"errors"
	projectpkg "github.com/fmazzalomo/pitcrew/internal/project"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOpenProjectCreatesPrivateBoundCentralStore(t *testing.T) {
	resolved, paths := centralFixture(t)
	missing, err := OpenProjectReadOnly(context.Background(), resolved, paths)
	if err != nil || missing.State != Uninitialized || missing.Store != nil {
		t.Fatalf("missing = %#v, %v", missing, err)
	}
	for _, path := range []string{paths.ProjectRoot, paths.IdentityPath, paths.StatePath, paths.WorktreeRoot, paths.HandleRoot, filepath.Join(paths.ProjectRoot, ".initialize.lock")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read-only open mutated %s: %v", path, err)
		}
	}
	opened, err := OpenProject(context.Background(), resolved, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	for path, mode := range map[string]os.FileMode{paths.ProjectRoot: 0o700, paths.WorktreeRoot: 0o700, paths.HandleRoot: 0o700, paths.IdentityPath: 0o600, paths.StatePath: 0o600} {
		if info, err := os.Lstat(path); err != nil || info.Mode().Perm() != mode || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s = %v, %v; want %04o", path, info, err, mode)
		}
	}
}
func TestOpenProjectRejectsSymlinkedCentralAncestorWithoutFollowing(t *testing.T) {
	resolved, _ := centralFixture(t)
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	paths, err := projectpkg.DerivePaths(filepath.Join(link, "data"), resolved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenProjectReadOnly(context.Background(), resolved, paths); !errors.Is(err, ErrInvalidState) || got.Store != nil {
		t.Fatalf("read-only result=%#v error=%v", got, err)
	}
	if _, err := OpenProject(context.Background(), resolved, paths); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("writable error=%v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target mutated: %v, %v", entries, err)
	}
}
func TestOpenProjectReadOnlyAcceptsPartialAbsenceButRejectsMarkerSymlink(t *testing.T) {
	resolved, paths := centralFixture(t)
	if err := os.MkdirAll(paths.ProjectRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := OpenProjectReadOnly(context.Background(), resolved, paths)
	if err != nil || got.State != Uninitialized || got.Store != nil {
		t.Fatalf("partial result=%#v error=%v", got, err)
	}
	outside := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, paths.IdentityPath); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenProjectReadOnly(context.Background(), resolved, paths); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("marker error=%v", err)
	}
}
func TestOpenProjectFailsClosedOnUnsafeCentralPaths(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(resolved projectpkg.Project, paths projectpkg.Paths) error
	}{
		{"permissions", func(_ projectpkg.Project, p projectpkg.Paths) error { return os.Chmod(p.StatePath, 0o644) }},
		{"state symlink", func(_ projectpkg.Project, p projectpkg.Paths) error {
			if err := os.Remove(p.StatePath); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(filepath.Dir(p.ProjectRoot), "outside"), p.StatePath)
		}},
		{"identity mismatch", func(r projectpkg.Project, p projectpkg.Paths) error {
			return os.WriteFile(p.IdentityPath, []byte(`{"version":1,"project_id":"`+strings.Repeat("f", 64)+`","git_common_dir":"`+r.CommonDir+`"}`), 0o600)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, paths := centralFixture(t)
			opened, err := OpenProject(context.Background(), resolved, paths)
			if err != nil {
				t.Fatal(err)
			}
			_ = opened.Close()
			if err := tc.corrupt(resolved, paths); err != nil {
				t.Fatal(err)
			}
			if _, err := OpenProject(context.Background(), resolved, paths); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
func TestConcurrentOpenProjectProducesOneCompleteStore(t *testing.T) {
	resolved, paths := centralFixture(t)
	start, errs := make(chan struct{}), make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			opened, err := OpenProject(context.Background(), resolved, paths)
			if err == nil {
				err = opened.Close()
			}
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	opened, err := OpenProjectReadOnly(context.Background(), resolved, paths)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Store.Close()
	var migrations int
	if err := opened.Store.DB().QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&migrations); err != nil || migrations != len(schemaMigrations) {
		t.Fatalf("migrations = %d, %v", migrations, err)
	}
}
func centralFixture(t *testing.T) (projectpkg.Project, projectpkg.Paths) {
	t.Helper()
	checkout := filepath.Join(t.TempDir(), "checkout")
	if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := projectpkg.Resolve(checkout)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := projectpkg.DerivePaths(filepath.Join(t.TempDir(), "data"), resolved.ID)
	if err != nil {
		t.Fatal(err)
	}
	return resolved, paths
}
