package handles

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestIssueWritesOpaqueIntentHandleWithStrictOwnershipAndModes(t *testing.T) {
	m, db, clock, wfID, unitID := testManager(t)
	dir := filepath.Join(t.TempDir(), "handles")
	result, err := m.Issue(context.Background(), wfID, unitID, "implementer", dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Secret != "" {
		t.Fatalf("normal issue returned secret %q", result.Secret)
	}
	dirInfo, _ := os.Stat(dir)
	fileInfo, err := os.Stat(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("modes dir=%o file=%o", dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
	}
	if runtime.GOOS != "windows" && fileInfo.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		t.Fatal("handle owner differs from caller")
	}
	data, _ := os.ReadFile(result.Path)
	secret := hex.EncodeToString(bytes.Repeat([]byte{2}, 16))
	if strings.Contains(string(data), secret) {
		t.Fatal("plain secret persisted in handle")
	}
	if strings.Contains(string(data), "implementer") {
		t.Fatal("actor identity persisted in handle")
	}
	var h Handle
	if err := json.Unmarshal(data, &h); err != nil {
		t.Fatal(err)
	}
	if h.State != Intent || h.WorkflowID != wfID || h.UnitID != unitID || h.ExpiresAt != clock.now.Add(5*time.Minute).Format(timestampLayout) || len(h.SecretHash) != 64 {
		t.Fatalf("handle = %#v", h)
	}
	var hash, actor string
	var generation int
	if err := db.DB().QueryRow(`SELECT secret_hash,actor_identity,claim_generation FROM handles WHERE claim_id=?`, h.ClaimID).Scan(&hash, &actor, &generation); err != nil {
		t.Fatal(err)
	}
	if hash != h.SecretHash || actor != "implementer" || generation != 1 {
		t.Fatalf("stored hash=%q actor=%q generation=%d", hash, actor, generation)
	}
	if !ownedBy(uint32(os.Geteuid()), uint32(os.Geteuid())) || ownedBy(1000, 1001) {
		t.Fatal("owner comparison is unsafe")
	}
}

func TestUseRejectsSymlinksAndWrongModes(t *testing.T) {
	m, _, _, wfID, unitID := testManager(t)
	result, err := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	if err != nil {
		t.Fatal(err)
	}
	real := result.Path + ".real"
	if err := os.Rename(result.Path, real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, result.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Use(context.Background(), result.Path, "implementer", TDD); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlink error=%v", err)
	}
	if err := os.Remove(result.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(real, result.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(result.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Use(context.Background(), result.Path, "implementer", TDD); !errors.Is(err, ErrUnsafePermissions) {
		t.Fatalf("mode error=%v", err)
	}
}

func TestPurposeScopedHandlesKeepIndependentGenerationsAndUse(t *testing.T) {
	m, db, _, wfID, unitID := testManager(t)
	dir := filepath.Join(t.TempDir(), "handles")
	implementation, err := m.IssueForPurpose(context.Background(), wfID, unitID, "implementer", dir, PurposeImplementation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.UseForPurpose(context.Background(), implementation.Path, "implementer", TDD, PurposeImplementation); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`UPDATE work_units SET state='reviewing' WHERE id=?`, unitID); err != nil {
		t.Fatal(err)
	}
	review, err := m.IssueForPurpose(context.Background(), wfID, unitID, "reviewer", dir, PurposeReview)
	if err != nil {
		t.Fatal(err)
	}
	if implementation.Generation != 1 || review.Generation != 1 {
		t.Fatalf("implementation generation=%d review generation=%d", implementation.Generation, review.Generation)
	}
	if _, err := m.Use(context.Background(), review.Path, "reviewer", TDD); !errors.Is(err, ErrInvalid) {
		t.Fatalf("legacy use accepted review authority for TDD: %v", err)
	}
	mutationCalled := false
	if _, err := m.UseForMutation(context.Background(), review.Path, wfID, unitID, 1, "reviewer", TDD, func(*sql.Tx, Handle) error {
		mutationCalled = true
		return nil
	}); !errors.Is(err, ErrInvalid) || mutationCalled {
		t.Fatalf("legacy mutation accepted review authority: error=%v called=%t", err, mutationCalled)
	}
	if _, err := m.UseForPurpose(context.Background(), implementation.Path, "implementer", TDD, PurposeImplementation); err != nil {
		t.Fatalf("review generation invalidated implementation authority: %v", err)
	}
	if _, err := m.UseForPurpose(context.Background(), review.Path, "reviewer", TDD, PurposeImplementation); !errors.Is(err, ErrInvalid) {
		t.Fatalf("review authority used as implementation: %v", err)
	}
	if _, err := m.UseForPurpose(context.Background(), implementation.Path, "reviewer", Review, PurposeReview); !errors.Is(err, ErrInvalid) {
		t.Fatalf("implementation authority used as review: %v", err)
	}
	if err := m.RevokeForPurpose(context.Background(), review.Path, "reviewer", PurposeImplementation); !errors.Is(err, ErrInvalid) {
		t.Fatalf("review authority revoked as implementation: %v", err)
	}
	if _, err := m.UseForPurpose(context.Background(), review.Path, "reviewer", Review, PurposeReview); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Use(context.Background(), review.Path, "reviewer", Complete); !errors.Is(err, ErrInvalid) {
		t.Fatalf("legacy use accepted review authority for completion: %v", err)
	}
	if _, err := m.UseForMutationAtPurpose(context.Background(), review.Path, wfID, unitID, 1, "reviewer", Review, PurposeReview, func(tx *sql.Tx, h Handle) error {
		_, err := tx.Exec(`UPDATE handles SET state='revoked' WHERE claim_id=?`, h.ClaimID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(review.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("consumed review handle still exists: %v", err)
	}
	var implementationState string
	if err := db.DB().QueryRow(`SELECT state FROM handles WHERE claim_id=?`, implementation.ClaimID).Scan(&implementationState); err != nil {
		t.Fatal(err)
	}
	if implementationState != "active" {
		t.Fatalf("review revocation changed implementation state to %q", implementationState)
	}
}

func TestIssueRejectsSymlinkHandleDirectory(t *testing.T) {
	m, _, _, wfID, unitID := testManager(t)
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Issue(context.Background(), wfID, unitID, "implementer", link); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("directory symlink error=%v", err)
	}
}

func TestUsePromotesRefreshesAndEnforcesActorSeparation(t *testing.T) {
	m, _, clock, wfID, unitID := testManager(t)
	result, _ := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	clock.advance(time.Minute)
	h, err := m.Use(context.Background(), result.Path, "implementer", TDD)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != Active || h.ExpiresAt != clock.now.Add(5*time.Minute).Format(timestampLayout) {
		t.Fatalf("promoted handle=%#v", h)
	}
	if _, err := m.Use(context.Background(), result.Path, "implementer", Review); !errors.Is(err, ErrIdentityCollision) {
		t.Fatalf("same actor review error=%v", err)
	}
	if _, err := m.Use(context.Background(), result.Path, "", Review); !errors.Is(err, ErrIdentityCollision) {
		t.Fatalf("empty reviewer error=%v", err)
	}
	clock.advance(time.Minute)
	h, err = m.Use(context.Background(), result.Path, "reviewer", Review)
	if err != nil {
		t.Fatal(err)
	}
	if h.ExpiresAt != clock.now.Add(5*time.Minute).Format(timestampLayout) {
		t.Fatalf("review refresh=%#v", h)
	}
	if _, err := m.Use(context.Background(), result.Path, "reviewer", Complete); !errors.Is(err, ErrIdentityCollision) {
		t.Fatalf("reviewer completion error=%v", err)
	}
	if _, err := m.Use(context.Background(), result.Path, "implementer", Complete); err != nil {
		t.Fatal(err)
	}
	if err := m.Revoke(context.Background(), result.Path, "implementer"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(result.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("revoked file exists: %v", err)
	}
	if _, err := m.Use(context.Background(), result.Path, "implementer", Complete); !errors.Is(err, ErrInvalid) {
		t.Fatalf("revoked handle error=%v", err)
	}
}

func TestExpiredHandleIsAtomicallyDeletedWithoutUnitMutation(t *testing.T) {
	m, db, clock, wfID, unitID := testManager(t)
	result, _ := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	clock.advance(6 * time.Minute)
	if _, err := m.Use(context.Background(), result.Path, "implementer", TDD); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry error=%v", err)
	}
	if _, err := os.Lstat(result.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired file still exists: %v", err)
	}
	var unitState, handleState string
	if err := db.DB().QueryRow(`SELECT state FROM work_units WHERE id=?`, unitID).Scan(&unitState); err != nil {
		t.Fatal(err)
	}
	if err := db.DB().QueryRow(`SELECT state FROM handles WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&handleState); err != nil {
		t.Fatal(err)
	}
	if unitState != "pending" || handleState != "revoked" {
		t.Fatalf("unit=%s handle=%s", unitState, handleState)
	}
}

func TestActivityCommitFailureRestoresPriorHandleBytes(t *testing.T) {
	m, db, _, wfID, unitID := testManager(t)
	result, _ := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	before, _ := os.ReadFile(result.Path)
	if _, err := db.DB().Exec(`PRAGMA defer_foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	_, err := m.UseForMutation(context.Background(), result.Path, wfID, unitID, 1, "implementer", TDD, func(tx *sql.Tx, _ Handle) error {
		_, insertErr := tx.Exec(`INSERT INTO activities(workflow_id,action,actor,at,subject_kind,subject_id) VALUES('wf-missing','unit_tdd_recorded','implementer','now','evidence','wu-000000000000000000000001@1')`)
		return insertErr
	})
	if err == nil {
		t.Fatal("deferred commit failure was accepted")
	}
	after, readErr := os.ReadFile(result.Path)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("handle restoration error=%v equal=%t", readErr, bytes.Equal(before, after))
	}
	var state string
	if err = db.DB().QueryRow(`SELECT state FROM handles WHERE claim_id=?`, result.ClaimID).Scan(&state); err != nil || state != "intent" {
		t.Fatalf("stored handle state=%s error=%v", state, err)
	}
}

func TestRecoverIncrementsGenerationAndEnforcesEvidenceRules(t *testing.T) {
	m, db, clock, wfID, unitID := testManager(t)
	dir := filepath.Join(t.TempDir(), "handles")
	first, _ := m.Issue(context.Background(), wfID, unitID, "implementer", dir)
	clock.advance(6 * time.Minute)
	if _, err := m.Use(context.Background(), first.Path, "implementer", TDD); !errors.Is(err, ErrExpired) {
		t.Fatalf("expire before recovery: %v", err)
	}
	second, err := m.Recover(context.Background(), wfID, unitID, "implementer", dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path || second.Generation != 2 {
		t.Fatalf("recovery first=%#v second=%#v", first, second)
	}
	var active, revoked int
	if err := db.DB().QueryRow(`SELECT count(*) FROM handles WHERE workflow_id=? AND unit_id=? AND state='active' OR workflow_id=? AND unit_id=? AND state='intent'`, wfID, unitID, wfID, unitID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if err := db.DB().QueryRow(`SELECT count(*) FROM handles WHERE workflow_id=? AND unit_id=? AND state='revoked'`, wfID, unitID).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if active != 1 || revoked != 1 {
		t.Fatalf("active=%d revoked=%d", active, revoked)
	}
	if _, err := db.DB().Exec(`UPDATE work_units SET state='reviewing' WHERE id=?`, unitID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Recover(context.Background(), wfID, unitID, "implementer", dir); !errors.Is(err, ErrRecoveryForbidden) {
		t.Fatalf("reviewing recovery error=%v", err)
	}
	if _, err := db.DB().Exec(`UPDATE work_units SET state='pending' WHERE id=?`, unitID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`INSERT INTO evidence(workflow_id,unit_id,revision,actor,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, wfID, unitID, 1, "implementer", "red", "fail", "green", "pass", "", "all", "pass", "x", "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Recover(context.Background(), wfID, unitID, "implementer", dir); !errors.Is(err, ErrRecoveryForbidden) {
		t.Fatalf("evidenced recovery error=%v", err)
	}
}

func TestDebugIssueReturnsSecretOnceAndRevokesImmediately(t *testing.T) {
	m, db, _, wfID, unitID := testManager(t)
	result, err := m.IssueDebug(context.Background(), wfID, unitID, "operator", filepath.Join(t.TempDir(), "handles"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Secret == "" {
		t.Fatal("debug issue omitted one-shot secret")
	}
	if _, err := os.Lstat(result.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("debug handle exists: %v", err)
	}
	var state string
	if err := db.DB().QueryRow(`SELECT state FROM handles WHERE claim_id=?`, result.ClaimID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "revoked" {
		t.Fatalf("debug state=%s", state)
	}
	if _, err := m.Use(context.Background(), result.Path, "operator", TDD); !errors.Is(err, ErrInvalid) {
		t.Fatalf("revoked debug use error=%v", err)
	}
}

type mutableClock struct{ now time.Time }

func (c *mutableClock) advance(d time.Duration) { c.now = c.now.Add(d) }
func testManager(t *testing.T) (*Manager, *store.Store, *mutableClock, string, string) {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wfID := "wf-000000000000000000000001"
	unitID := "wu-000000000000000000000001"
	_, err = s.DB().Exec(`INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,1,'implementing','goal','now','now')`, wfID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB().Exec(`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,admission_exception,revision) VALUES(?,?,?,?,?,?,?,?,?,?,1)`, unitID, wfID, "unit", "internal", `[]`, `[]`, 10, 5, "pending", nil)
	if err != nil {
		t.Fatal(err)
	}
	clock := &mutableClock{now: time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)}
	entropy := bytes.NewReader(append(bytes.Repeat([]byte{1}, 16), append(bytes.Repeat([]byte{2}, 16), bytes.Repeat([]byte{3}, 256)...)...))
	return New(s, func() time.Time { return clock.now }, entropy), s, clock, wfID, unitID
}
