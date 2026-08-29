package consolidate_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/fmazzalomo/pitcrew/internal/consolidate"
	"github.com/fmazzalomo/pitcrew/internal/project"
	"github.com/fmazzalomo/pitcrew/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceImportsChosenWholeGraphAtomicallyAndIdempotently(t *testing.T) {
	ctx := context.Background()
	main, linked := consolidationCheckouts(t)
	first := mustStore(t, main)
	defer first.Close()
	second := mustStore(t, linked)
	defer second.Close()
	for _, source := range []*store.Store{first, second} {
		_, _ = source.DB().Exec(`PRAGMA wal_autocheckpoint=0`)
		_, _ = source.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	}
	seedGraph(t, first, 7, 11, "first")
	seedGraph(t, second, 70, 110, "chosen-from-wal")
	resolved, err := project.Resolve(linked)
	must(t, err)
	discovery, err := project.DiscoverLegacy(resolved)
	must(t, err)
	manifest := consolidate.Manifest{ProjectID: resolved.ID}
	for _, candidate := range discovery.Candidates {
		manifest.CandidateIDs = append(manifest.CandidateIDs, candidate.ID)
		if candidate.StatePath == second.Path() {
			manifest.Choices = []consolidate.Choice{{WorkflowID: graphWorkflowID, CandidateID: candidate.ID}}
		}
	}
	beforeDB, beforeWAL := readSource(t, second.Path()), readSource(t, second.Path()+"-wal")
	destination := mustStore(t, t.TempDir())
	defer destination.Close()
	service := consolidate.Service{}
	must(t, service.Consolidate(ctx, destination.DB(), resolved, manifest))
	must(t, service.Consolidate(ctx, destination.DB(), resolved, manifest))
	var content string
	var workflows int
	must(t, destination.DB().QueryRow(`SELECT content FROM artifacts WHERE workflow_id=?`, graphWorkflowID).Scan(&content))
	must(t, destination.DB().QueryRow(`SELECT count(*) FROM workflows`).Scan(&workflows))
	var acknowledged int
	err = destination.DB().QueryRow(`SELECT count(*) FROM consolidation_acknowledgements WHERE project_id=? AND candidate_set_id=?`, resolved.ID, discovery.CandidateSetID).Scan(&acknowledged)
	if err != nil || acknowledged != 1 || content != "chosen-from-wal" || workflows != 1 {
		t.Fatalf("content=%q workflows=%d acknowledged=%d err=%v", content, workflows, acknowledged, err)
	}
	if !bytes.Equal(beforeDB, readSource(t, second.Path())) || !bytes.Equal(beforeWAL, readSource(t, second.Path()+"-wal")) {
		t.Fatal("source database or WAL changed")
	}
	rollback := mustStore(t, t.TempDir())
	defer rollback.Close()
	seedGraph(t, rollback, 9, 13, "central")
	if err := service.Consolidate(ctx, rollback.DB(), resolved, manifest); !errors.Is(err, consolidate.ErrConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	if err := rollback.DB().QueryRow(`SELECT content FROM artifacts WHERE workflow_id=?`, graphWorkflowID).Scan(&content); err != nil || content != "central" {
		t.Fatalf("rollback content=%q err=%v", content, err)
	}
}
func TestServiceAcceptsStableClosedWALSource(t *testing.T) {
	ctx := context.Background()
	main, _ := consolidationCheckouts(t)
	source := mustStore(t, main)
	seedGraph(t, source, 7, 11, "closed-source")
	path := source.Path()
	must(t, source.Close())
	before := readSource(t, path)
	if _, err := os.Stat(path + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("closed source retained WAL: %v", err)
	}
	resolved, err := project.Resolve(main)
	must(t, err)
	discovery, err := project.DiscoverLegacy(resolved)
	must(t, err)
	manifest := consolidate.Manifest{ProjectID: resolved.ID, CandidateIDs: []string{discovery.Candidates[0].ID}}
	destination := mustStore(t, t.TempDir())
	defer destination.Close()
	if err := (consolidate.Service{}).Consolidate(ctx, destination.DB(), resolved, manifest); err != nil {
		t.Fatalf("stable closed source rejected: %v", err)
	}
	if !bytes.Equal(before, readSource(t, path)) {
		t.Fatal("closed source database changed")
	}
	if _, err := os.Stat(path + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("snapshot left WAL behind: %v", err)
	}
	changed := mustStore(t, main)
	_, err = changed.DB().Exec(`UPDATE workflows SET goal='changed'`)
	must(t, err)
	must(t, changed.Close())
	if err := (consolidate.Service{}).Consolidate(ctx, destination.DB(), resolved, manifest); !errors.Is(err, consolidate.ErrInvalidManifest) {
		t.Fatalf("changed source accepted with stale manifest: %v", err)
	}
}

func TestServiceRejectsManifestAfterLegacyPermissionsBecomeUnsafe(t *testing.T) {
	ctx := context.Background()
	main, _ := consolidationCheckouts(t)
	source := mustStore(t, main)
	seedGraph(t, source, 7, 11, "source")
	path := source.Path()
	must(t, source.Close())
	resolved, err := project.Resolve(main)
	must(t, err)
	discovery, err := project.DiscoverLegacy(resolved)
	must(t, err)
	manifest := consolidate.Manifest{ProjectID: resolved.ID, CandidateIDs: []string{discovery.Candidates[0].ID}}
	must(t, os.Chmod(filepath.Dir(path), 0o777))
	destination := mustStore(t, t.TempDir())
	defer destination.Close()
	if err := (consolidate.Service{}).Consolidate(ctx, destination.DB(), resolved, manifest); !errors.Is(err, consolidate.ErrInvalidManifest) {
		t.Fatalf("unsafe source accepted with stale manifest: %v", err)
	}
}

func consolidationCheckouts(t *testing.T) (string, string) {
	root := t.TempDir()
	main, linked := filepath.Join(root, "main"), filepath.Join(root, "linked")
	admin := filepath.Join(main, ".git", "worktrees", "linked")
	must(t, os.MkdirAll(admin, 0o755))
	must(t, os.MkdirAll(linked, 0o755))
	must(t, os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: "+admin+"\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(admin, "commondir"), []byte("../..\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(admin, "gitdir"), []byte(filepath.Join(linked, ".git")+"\n"), 0o600))
	return main, linked
}
func readSource(t *testing.T, path string) []byte {
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
func must(t *testing.T, err error) {
	if err != nil {
		t.Fatal(err)
	}
}
func mustStore(t *testing.T, root string) *store.Store {
	opened, err := store.Open(context.Background(), root)
	must(t, err)
	return opened
}
