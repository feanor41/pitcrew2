package cli

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/envelope"
	projectpkg "github.com/fmazzalomo/pitcrew/internal/project"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestContextInspectTreatsV4CentralStoreAsMissingWithoutMigration(t *testing.T) {
	root := contextRepository(t)
	dataHome := t.TempDir()
	resolved, err := projectpkg.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := projectpkg.DerivePaths(dataHome, resolved.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := store.OpenProject(context.Background(), resolved, paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.DB().Exec(`DROP TABLE project_context_audits; DROP TABLE project_context; DELETE FROM schema_migrations WHERE version >= 5`); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	before := centralSchemaVersion(t, paths.StatePath)
	got := runCentral(t, root, dataHome, "context", "inspect")
	if got.code != 0 || !strings.Contains(got.stdout, `"status":"missing"`) || !strings.Contains(got.stdout, `"next_action":"context initialize"`) {
		t.Fatalf("pre-V5 inspect = %#v", got)
	}
	if after := centralSchemaVersion(t, paths.StatePath); after != before || after != 4 {
		t.Fatalf("schema version changed: before=%d after=%d", before, after)
	}
	for _, table := range []string{"project_context", "project_context_audits"} {
		if count := centralTableCount(t, paths.StatePath, table); count != 0 {
			t.Fatalf("read-only inspect created %s", table)
		}
	}
}

func TestContextInspectRejectsCorruptV5WithoutRepair(t *testing.T) {
	root := contextRepository(t)
	dataHome := t.TempDir()
	resolved, err := projectpkg.Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := projectpkg.DerivePaths(dataHome, resolved.ID)
	if err != nil {
		t.Fatal(err)
	}
	corrupt, err := store.OpenProject(context.Background(), resolved, paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := corrupt.DB().Exec(`DROP TABLE project_context`); err != nil {
		t.Fatal(err)
	}
	if err := corrupt.Close(); err != nil {
		t.Fatal(err)
	}

	before := centralSchemaVersion(t, paths.StatePath)
	got := runCentral(t, root, dataHome, "context", "inspect")
	if got.code != int(envelope.State) || !strings.Contains(got.stderr, "inconsistent V5 project-context schema") {
		t.Fatalf("corrupt V5 inspect = %#v", got)
	}
	if after := centralSchemaVersion(t, paths.StatePath); after != before {
		t.Fatalf("corrupt V5 inspection changed schema: before=%d after=%d", before, after)
	}
	if count := centralTableCount(t, paths.StatePath, "project_context"); count != 0 {
		t.Fatal("corrupt V5 inspection repaired missing project_context table")
	}
}

func centralSchemaVersion(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func centralTableCount(t *testing.T, path, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
