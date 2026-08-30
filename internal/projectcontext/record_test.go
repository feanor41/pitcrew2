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

func TestRecorderWiresCentralIdentityConfinementNoopRollbackAndLegacyGate(t *testing.T) {
	root, dataHome := t.TempDir(), t.TempDir()
	main, linked := filepath.Join(root, "main"), filepath.Join(root, "linked")
	makeLinkedRepository(t, main, linked)
	for _, checkout := range []string{main, linked} {
		if err := os.WriteFile(filepath.Join(checkout, "README.md"), []byte("evidence\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	record := completeRecord()
	got, persisted, err := realRecorder(main, dataHome).Record(context.Background(), record, "initializer", time.Unix(1, 0))
	if err != nil || !persisted || got.Status != pc.Complete || got.CheckoutRoot != main {
		t.Fatalf("main record = %#v, %v, %v", got, persisted, err)
	}
	got, persisted, err = realRecorder(linked, dataHome).Record(context.Background(), record, "initializer", time.Unix(2, 0))
	if err != nil || persisted || got.Status != pc.Complete || got.CheckoutRoot != linked || auditCount(t, main, dataHome) != 1 {
		t.Fatalf("linked no-op = %#v, %v, %v", got, persisted, err)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(main, "escape")); err != nil {
		t.Fatal(err)
	}
	escape := pc.CloneRecord(record)
	escape.Facts["stack"][0].Evidence.Path = "escape/secret.md"
	if _, _, err = realRecorder(main, dataHome).Record(context.Background(), escape, "initializer", time.Unix(3, 0)); !errors.Is(err, pc.ErrEvidenceOutsideCheckout) || auditCount(t, main, dataHome) != 1 {
		t.Fatalf("confined record error=%v audits=%d", err, auditCount(t, main, dataHome))
	}

	opened := openCentral(t, main, dataHome)
	if _, err = opened.DB().Exec(`CREATE TRIGGER fail_context_audit BEFORE INSERT ON project_context_audits BEGIN SELECT RAISE(ABORT,'audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	_ = opened.Close()
	changed := pc.CloneRecord(record)
	changed.Facts["architecture"][0].Assertion = "changed"
	if _, _, err = realRecorder(main, dataHome).Record(context.Background(), changed, "initializer", time.Unix(4, 0)); err == nil {
		t.Fatal("audit failure did not roll back")
	}
	inspected, err := realService(main, dataHome).Inspect(context.Background())
	if err != nil || !reflect.DeepEqual(inspected.Facts, record.Facts) || auditCount(t, main, dataHome) != 1 {
		t.Fatalf("rollback inspection=%#v err=%v", inspected, err)
	}

	legacyPath := filepath.Join(main, ".pitcrew", "state.db")
	if err := os.Mkdir(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy-source"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(legacyPath)
	_, _, err = realRecorder(main, dataHome).Record(context.Background(), changed, "initializer", time.Unix(5, 0))
	var recovery *pc.RecoveryError
	after, _ := os.ReadFile(legacyPath)
	if !errors.As(err, &recovery) || !errors.Is(err, pc.ErrLegacyBlocked) || recovery.NextAction != pc.LegacyRecoveryNextAction || !reflect.DeepEqual(before, after) || auditCount(t, main, dataHome) != 1 {
		t.Fatalf("legacy gate error=%#v source=%q audits=%d", err, after, auditCount(t, main, dataHome))
	}
}

func TestRecorderPreservesOrdinaryGateErrors(t *testing.T) {
	root := t.TempDir()
	recorder := pc.NewRecorder(func(context.Context) (pc.RecordTarget, error) {
		return pc.RecordTarget{CheckoutRoot: root, GateLegacy: func(context.Context) error { return context.Canceled }, Commit: func(context.Context, pc.Record, string, time.Time) (pc.Inspection, bool, error) {
			t.Fatal("ordinary gate error reached commit")
			return pc.Inspection{}, false, nil
		}}, nil
	})
	_, _, err := recorder.Record(context.Background(), completeRecord(), "actor", time.Now())
	var recovery *pc.RecoveryError
	if err != context.Canceled || errors.As(err, &recovery) {
		t.Fatalf("ordinary gate error = %#v", err)
	}
	resolved := false
	invalid := pc.NewRecorder(func(context.Context) (pc.RecordTarget, error) { resolved = true; return pc.RecordTarget{}, nil })
	if _, _, err := invalid.Record(context.Background(), pc.Record{}, "actor", time.Now()); !errors.Is(err, pc.ErrInvalidRecord) || resolved {
		t.Fatalf("invalid record error=%v resolved=%v", err, resolved)
	}
}

func realRecorder(cwd, dataHome string) pc.Recorder {
	return pc.NewRecorder(func(context.Context) (pc.RecordTarget, error) {
		initial, err := project.Inspect(cwd, dataHome)
		if err != nil {
			return pc.RecordTarget{}, err
		}
		return pc.RecordTarget{CheckoutRoot: initial.Project.CheckoutRoot, GateLegacy: func(ctx context.Context) error {
			current, err := project.Inspect(cwd, dataHome)
			if err != nil {
				return err
			}
			acknowledged := ""
			read, err := store.OpenProjectReadOnly(ctx, current.Project, current.Paths)
			if err != nil {
				return err
			}
			if read.State != store.Uninitialized {
				defer read.Store.Close()
				var exists int
				_ = read.Store.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='consolidation_acknowledgements'`).Scan(&exists)
				if exists == 1 {
					_ = read.Store.DB().QueryRow(`SELECT candidate_set_id FROM consolidation_acknowledgements WHERE project_id=? AND candidate_set_id=?`, current.Project.ID, current.Legacy.CandidateSetID).Scan(&acknowledged)
				}
			}
			if err := project.GateLegacy(current.Legacy, acknowledged); errors.Is(err, project.ErrMigrationRequired) {
				return pc.ErrLegacyBlocked
			} else {
				return err
			}
		}, Commit: func(ctx context.Context, record pc.Record, actor string, at time.Time) (pc.Inspection, bool, error) {
			current, err := project.Inspect(cwd, dataHome)
			if err != nil {
				return pc.Inspection{}, false, err
			}
			opened, err := store.OpenProject(ctx, current.Project, current.Paths)
			if err != nil {
				return pc.Inspection{}, false, err
			}
			changed, err := opened.ReplaceProjectContext(ctx, record, actor, at)
			_ = opened.Close()
			if err != nil {
				return pc.Inspection{}, false, err
			}
			inspection, err := realService(cwd, dataHome).Inspect(ctx)
			return inspection, changed, err
		}}, nil
	})
}

func openCentral(t *testing.T, cwd, dataHome string) *store.Store {
	t.Helper()
	inspected, err := project.Inspect(cwd, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.OpenProject(context.Background(), inspected.Project, inspected.Paths)
	if err != nil {
		t.Fatal(err)
	}
	return opened
}

func auditCount(t *testing.T, cwd, dataHome string) int {
	t.Helper()
	opened := openCentral(t, cwd, dataHome)
	defer opened.Close()
	var count int
	if err := opened.DB().QueryRow(`SELECT count(*) FROM project_context_audits`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
