package projectcontext_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/project"
	pc "github.com/fmazzalomo/pitcrew/internal/projectcontext"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestInspectMissingIsReadOnlyAndSharedByLinkedWorktrees(t *testing.T) {
	root := t.TempDir()
	main, linked := filepath.Join(root, "main"), filepath.Join(root, "linked")
	makeLinkedRepository(t, main, linked)
	dataHome := filepath.Join(t.TempDir(), "absent")

	missing, err := realService(main, dataHome).Inspect(context.Background())
	if err != nil || missing.Status != pc.Missing || missing.CheckoutRoot != main {
		t.Fatalf("missing inspection = %#v, %v", missing, err)
	}
	for _, path := range []string{dataHome, filepath.Join(main, ".pitcrew")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read-only inspection created %s: %v", path, err)
		}
	}

	record := completeRecord()
	storeContext(t, main, dataHome, record)
	fromLinked, err := realService(filepath.Join(linked, "nested"), dataHome).Inspect(context.Background())
	if err != nil || fromLinked.Status != pc.Complete || fromLinked.CheckoutRoot != linked || !reflect.DeepEqual(fromLinked.Facts, record.Facts) {
		t.Fatalf("linked inspection = %#v, %v", fromLinked, err)
	}
}

func TestInspectIsolatesClonesAndCanonicalMoves(t *testing.T) {
	root, dataHome := t.TempDir(), t.TempDir()
	first, clone := filepath.Join(root, "first"), filepath.Join(root, "clone")
	makeRepository(t, first)
	makeRepository(t, clone)
	storeContext(t, first, dataHome, completeRecord())

	for name, cwd := range map[string]string{"clone": clone} {
		got, err := realService(cwd, dataHome).Inspect(context.Background())
		if err != nil || got.Status != pc.Missing {
			t.Fatalf("%s inspection = %#v, %v", name, got, err)
		}
	}
	moved := filepath.Join(root, "moved")
	if err := os.Rename(first, moved); err != nil {
		t.Fatal(err)
	}
	got, err := realService(moved, dataHome).Inspect(context.Background())
	if err != nil || got.Status != pc.Missing {
		t.Fatalf("moved inspection = %#v, %v", got, err)
	}
}

func TestInspectHidesInvalidAndUnsupportedStoredSnapshots(t *testing.T) {
	checkout, dataHome := filepath.Join(t.TempDir(), "checkout"), t.TempDir()
	makeRepository(t, checkout)
	storeContext(t, checkout, dataHome, completeRecord())
	inspected, err := project.Inspect(checkout, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE project_context SET content='not-json' WHERE singleton=1`,
		`UPDATE project_context SET content='{"schema_version":99,"facts":{}}',schema_version=99 WHERE singleton=1`,
	} {
		opened, err := store.OpenProject(context.Background(), inspected.Project, inspected.Paths)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := opened.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
		_ = opened.Close()
		got, err := realService(checkout, dataHome).Inspect(context.Background())
		if err != nil || got.Status != pc.Incomplete || len(got.Facts) != 0 || !reflect.DeepEqual(got.MissingOrInvalid, pc.Categories()) {
			t.Fatalf("invalid inspection = %#v, %v", got, err)
		}
	}
}

func TestInspectResolvesRelativeAbsoluteAndSubdirectoryWorkingDirectories(t *testing.T) {
	root := t.TempDir()
	checkout, nested, dataHome := filepath.Join(root, "checkout"), filepath.Join(root, "checkout", "nested"), t.TempDir()
	makeRepository(t, checkout)
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	storeContext(t, checkout, dataHome, completeRecord())
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	for _, cwd := range []string{"checkout/nested", nested, checkout} {
		got, err := realService(cwd, dataHome).Inspect(context.Background())
		if err != nil || got.Status != pc.Complete || got.CheckoutRoot != checkout {
			t.Fatalf("cwd %q inspection = %#v, %v", cwd, got, err)
		}
	}
}

func realService(cwd, dataHome string) *pc.Service {
	return pc.New(pc.Dependencies{Resolve: func(context.Context) (pc.Resolved, error) {
		resolved, err := project.Resolve(cwd)
		if err != nil {
			return pc.Resolved{}, err
		}
		inspected, err := project.Inspect(cwd, dataHome)
		if err != nil {
			return pc.Resolved{}, err
		}
		return pc.Resolved{CheckoutRoot: resolved.CheckoutRoot, Load: func(ctx context.Context) (pc.Record, string, bool, error) {
			opened, err := store.OpenProjectReadOnly(ctx, inspected.Project, inspected.Paths)
			if err != nil || opened.State == store.Uninitialized {
				return pc.Record{}, "", false, err
			}
			defer opened.Store.Close()
			snapshot, found, err := opened.Store.LoadProjectContext(ctx)
			return snapshot.Record, snapshot.UpdatedAt, found, err
		}}, nil
	}})
}

func storeContext(t *testing.T, cwd, dataHome string, record pc.Record) {
	t.Helper()
	inspected, err := project.Inspect(cwd, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.OpenProject(context.Background(), inspected.Project, inspected.Paths)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if changed, err := opened.ReplaceProjectContext(context.Background(), record, "identity-test", time.Unix(1, 0)); err != nil || !changed {
		t.Fatalf("store context changed=%v err=%v", changed, err)
	}
}

func completeRecord() pc.Record {
	facts := make(map[string][]pc.Fact)
	for _, category := range pc.Categories() {
		facts[category] = []pc.Fact{{Assertion: category + " fact", ObservedAt: "2026-08-30T12:00:00Z", Evidence: pc.Evidence{Path: "README.md"}}}
	}
	return pc.Record{SchemaVersion: pc.SchemaVersion, Facts: facts}
}

func makeRepository(t *testing.T, checkout string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func makeLinkedRepository(t *testing.T, main, linked string) {
	t.Helper()
	admin := filepath.Join(main, ".git", "worktrees", "linked")
	for _, dir := range []string{admin, filepath.Join(linked, "nested")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(linked, ".git"):     "gitdir: " + admin + "\n",
		filepath.Join(admin, "commondir"): "../..\n",
		filepath.Join(admin, "gitdir"):    filepath.Join(linked, ".git") + "\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
