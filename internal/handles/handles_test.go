package handles

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestImplementationClaimCapturesOneCleanScopedBaselineAndReusesIt(t *testing.T) {
	m, s, clock, wfID, unitID := testManager(t)
	checkout := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", checkout}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	run("init", "--quiet")
	run("config", "user.email", "pitcrew@example.test")
	run("config", "user.name", "PitCrew Test")
	if err := os.Mkdir(filepath.Join(checkout, "internal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "internal", "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "base")
	if err := os.WriteFile(filepath.Join(checkout, "outside.txt"), []byte("allowed dirt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE work_units SET areas='[]' WHERE workflow_id=? AND id=?`, wfID, unitID); err != nil {
		t.Fatal(err)
	}
	m.WithProjectRoot(checkout)
	if err := os.WriteFile(filepath.Join(checkout, "internal", "dirty.txt"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "dirty")); err == nil || !strings.Contains(err.Error(), "must be clean") {
		t.Fatalf("dirty claim error=%v", err)
	}
	var claims, baselines int
	_ = s.DB().QueryRow(`SELECT count(*) FROM handles WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&claims)
	_ = s.DB().QueryRow(`SELECT count(*) FROM unit_change_baselines WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&baselines)
	if claims != 0 || baselines != 0 {
		t.Fatalf("dirty claim mutated state: claims=%d baselines=%d", claims, baselines)
	}
	m.entropy = rand.Reader
	if err := os.Remove(filepath.Join(checkout, "internal", "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	first, err := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "first"))
	if err != nil {
		t.Fatal(err)
	}
	var baseRevision, scopesJSON string
	if err = s.DB().QueryRow(`SELECT base_revision,scopes_json FROM unit_change_baselines WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&baseRevision, &scopesJSON); err != nil {
		t.Fatal(err)
	}
	if scopesJSON != `["internal"]` {
		t.Fatalf("scopes=%s", scopesJSON)
	}
	clock.advance(16 * time.Minute)
	if _, err = m.Use(context.Background(), first.Path, "implementer", TDD); !errors.Is(err, ErrExpired) {
		t.Fatalf("expire before recovery: %v", err)
	}
	if _, err = m.Recover(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "second")); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.DB().QueryRow(`SELECT count(*) FROM unit_change_baselines WHERE workflow_id=? AND unit_id=? AND base_revision=?`, wfID, unitID, baseRevision).Scan(&count); err != nil || count != 1 {
		t.Fatalf("baseline count=%d err=%v", count, err)
	}
}

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
	if h.State != Intent || h.WorkflowID != wfID || h.UnitID != unitID || h.ExpiresAt != clock.now.Add(15*time.Minute).Format(timestampLayout) || len(h.SecretHash) != 64 {
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

func TestReleaseIntentAtAtomicallyRestoresReadyAuthority(t *testing.T) {
	m, db, _, wfID, unitID := testManager(t)
	issued, err := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	if err != nil {
		t.Fatal(err)
	}

	result, err := m.ReleaseIntentAt(context.Background(), ReleaseIntentRequest{
		WorkflowID: wfID, WorkflowRevision: 1, UnitID: unitID, UnitRevision: 1,
		Actor: "implementer", HandlePath: issued.Path, Reason: "reassign bounded work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkflowRevision != 2 || result.UnitRevision != 2 || result.UnitState != "pending" || !result.HandleFileRemoved {
		t.Fatalf("result=%#v", result)
	}
	if _, err = os.Stat(issued.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("released handle file remains: %v", err)
	}

	var workflowRevision, unitRevision int64
	var workflowState, unitState, handleState string
	if err = db.DB().QueryRow(`SELECT revision,state FROM workflows WHERE id=?`, wfID).Scan(&workflowRevision, &workflowState); err != nil {
		t.Fatal(err)
	}
	if err = db.DB().QueryRow(`SELECT revision,state FROM work_units WHERE workflow_id=? AND id=?`, wfID, unitID).Scan(&unitRevision, &unitState); err != nil {
		t.Fatal(err)
	}
	if err = db.DB().QueryRow(`SELECT state FROM handles WHERE claim_id=?`, issued.ClaimID).Scan(&handleState); err != nil {
		t.Fatal(err)
	}
	if workflowRevision != 2 || workflowState != "implementing" || unitRevision != 2 || unitState != "pending" || handleState != "revoked" {
		t.Fatalf("workflow=%s@%d unit=%s@%d handle=%s", workflowState, workflowRevision, unitState, unitRevision, handleState)
	}
	var artifactBody, eventReason, action, subjectKind, subjectID string
	if err = db.DB().QueryRow(`SELECT content FROM artifacts WHERE workflow_id=? AND kind='unit_claim_release'`, wfID).Scan(&artifactBody); err != nil {
		t.Fatal(err)
	}
	if artifactBody != `{"unit_id":"wu-000000000000000000000001","released_unit_revision":1,"unit_revision_after":2,"reason":"reassign bounded work"}` {
		t.Fatalf("artifact=%s", artifactBody)
	}
	if err = db.DB().QueryRow(`SELECT reason FROM events WHERE workflow_id=? AND revision_after=2`, wfID).Scan(&eventReason); err != nil {
		t.Fatal(err)
	}
	if err = db.DB().QueryRow(`SELECT action,subject_kind,subject_id FROM activities WHERE workflow_id=? ORDER BY id DESC LIMIT 1`, wfID).Scan(&action, &subjectKind, &subjectID); err != nil {
		t.Fatal(err)
	}
	if eventReason != "unit_claim_released" || action != "unit_claim_released" || subjectKind != "artifact" || subjectID == "" {
		t.Fatalf("event=%q activity=%s/%s/%s", eventReason, action, subjectKind, subjectID)
	}
	if _, err = m.ReleaseIntentAt(context.Background(), ReleaseIntentRequest{WorkflowID: wfID, WorkflowRevision: 2, UnitID: unitID, UnitRevision: 2, Actor: "implementer", HandlePath: issued.Path, Reason: "replay"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("replay error=%v", err)
	}
}

func TestReleaseIntentAtRejectsIneligibleFactsWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *store.Store, *mutableClock, string, string, IssueResult)
		change func(*ReleaseIntentRequest)
		want   error
	}{
		{name: "wrong owner", change: func(r *ReleaseIntentRequest) { r.Actor = "other" }, want: ErrIdentityCollision},
		{name: "stale workflow", change: func(r *ReleaseIntentRequest) { r.WorkflowRevision = 2 }, want: store.ErrCASMismatch},
		{name: "stale unit", change: func(r *ReleaseIntentRequest) { r.UnitRevision = 2 }, want: store.ErrCASMismatch},
		{name: "expired", mutate: func(_ *testing.T, _ *store.Store, c *mutableClock, _, _ string, _ IssueResult) {
			c.advance(16 * time.Minute)
		}, want: ErrExpired},
		{name: "active", mutate: func(t *testing.T, s *store.Store, _ *mutableClock, _, _ string, issued IssueResult) {
			if _, err := s.DB().Exec(`UPDATE handles SET state='active' WHERE claim_id=?`, issued.ClaimID); err != nil {
				t.Fatal(err)
			}
			var h Handle
			data, _ := os.ReadFile(issued.Path)
			if err := json.Unmarshal(data, &h); err != nil {
				t.Fatal(err)
			}
			h.State = Active
			if err := writeAtomic(issued.Path, h); err != nil {
				t.Fatal(err)
			}
		}, want: ErrInvalid},
		{name: "current evidence", mutate: func(t *testing.T, s *store.Store, _ *mutableClock, wf, unit string, _ IssueResult) {
			_, err := s.DB().Exec(`INSERT INTO evidence(workflow_id,unit_id,revision,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,actor,recorded_at) VALUES(?,?,1,'red','exit 1','green','exit 0','','all','exit 0','internal','implementer','now')`, wf, unit)
			if err != nil {
				t.Fatal(err)
			}
		}, want: ErrInvalidState},
		{name: "durable implementation activity after claim", mutate: func(t *testing.T, s *store.Store, _ *mutableClock, wf, unit string, _ IssueResult) {
			_, err := s.DB().Exec(`INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES(?,?,'unit_tdd_recorded','implementer','now','evidence',?)`, wf, unit, unit+"@1")
			if err != nil {
				t.Fatal(err)
			}
		}, want: ErrInvalidState},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, s, clock, wfID, unitID := testManager(t)
			issued, err := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(issued.Path)
			if tc.mutate != nil {
				tc.mutate(t, s, clock, wfID, unitID, issued)
			}
			request := ReleaseIntentRequest{WorkflowID: wfID, WorkflowRevision: 1, UnitID: unitID, UnitRevision: 1, Actor: "implementer", HandlePath: issued.Path, Reason: "reassign"}
			if tc.change != nil {
				tc.change(&request)
			}
			if _, err = m.ReleaseIntentAt(context.Background(), request); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want %v", err, tc.want)
			}
			var wfRev, unitRev int64
			var handleState string
			_ = s.DB().QueryRow(`SELECT revision FROM workflows WHERE id=?`, wfID).Scan(&wfRev)
			_ = s.DB().QueryRow(`SELECT revision FROM work_units WHERE workflow_id=? AND id=?`, wfID, unitID).Scan(&unitRev)
			_ = s.DB().QueryRow(`SELECT state FROM handles WHERE claim_id=?`, issued.ClaimID).Scan(&handleState)
			after, readErr := os.ReadFile(issued.Path)
			if wfRev != 1 || unitRev != 1 || handleState == "revoked" || readErr != nil || len(after) == 0 || len(before) == 0 {
				t.Fatalf("mutation wf=%d unit=%d handle=%s file=%v", wfRev, unitRev, handleState, readErr)
			}
		})
	}
}

func TestReleaseIntentAtActivityFailureRollsBackEveryDurableEffect(t *testing.T) {
	m, s, _, wfID, unitID := testManager(t)
	issued, err := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`CREATE TRIGGER reject_release_activity BEFORE INSERT ON activities WHEN NEW.action='unit_claim_released' BEGIN SELECT RAISE(ABORT,'reject release'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = m.ReleaseIntentAt(context.Background(), ReleaseIntentRequest{WorkflowID: wfID, WorkflowRevision: 1, UnitID: unitID, UnitRevision: 1, Actor: "implementer", HandlePath: issued.Path, Reason: "reassign"})
	if err == nil {
		t.Fatal("activity failure was accepted")
	}
	var wfRev, unitRev int64
	var handleState string
	var artifacts, events int
	_ = s.DB().QueryRow(`SELECT revision FROM workflows WHERE id=?`, wfID).Scan(&wfRev)
	_ = s.DB().QueryRow(`SELECT revision FROM work_units WHERE workflow_id=? AND id=?`, wfID, unitID).Scan(&unitRev)
	_ = s.DB().QueryRow(`SELECT state FROM handles WHERE claim_id=?`, issued.ClaimID).Scan(&handleState)
	_ = s.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=? AND kind='unit_claim_release'`, wfID).Scan(&artifacts)
	_ = s.DB().QueryRow(`SELECT count(*) FROM events WHERE workflow_id=? AND reason='unit_claim_released'`, wfID).Scan(&events)
	if wfRev != 1 || unitRev != 1 || handleState != "intent" || artifacts != 0 || events != 0 {
		t.Fatalf("wf=%d unit=%d handle=%s artifacts=%d events=%d", wfRev, unitRev, handleState, artifacts, events)
	}
	if _, err = os.Stat(issued.Path); err != nil {
		t.Fatalf("claim file changed: %v", err)
	}
}

func TestReleaseAuthorizationIsConsumedByImmediateReclaim(t *testing.T) {
	m, s, clock, wfID, unitID := testManager(t)
	dir := filepath.Join(t.TempDir(), "handles")
	first, err := m.Issue(context.Background(), wfID, unitID, "implementer", dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.ReleaseIntentAt(context.Background(), ReleaseIntentRequest{WorkflowID: wfID, WorkflowRevision: 1, UnitID: unitID, UnitRevision: 1, Actor: "implementer", HandlePath: first.Path, Reason: "reassign"}); err != nil {
		t.Fatal(err)
	}
	second, err := m.IssueAt(context.Background(), wfID, unitID, 2, "implementer", dir)
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(16 * time.Minute)
	if _, err = m.UseFor(context.Background(), second.Path, wfID, unitID, 2, "implementer", TDD); !errors.Is(err, ErrExpired) {
		t.Fatalf("expire immediate reclaim: %v", err)
	}
	if _, err = m.IssueAt(context.Background(), wfID, unitID, 2, "implementer", dir); !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("reused release authorization: %v", err)
	}
	var handles, claims int
	if err = s.DB().QueryRow(`SELECT count(*) FROM handles WHERE workflow_id=? AND unit_id=?`, wfID, unitID).Scan(&handles); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT count(*) FROM activities WHERE workflow_id=? AND unit_id=? AND action='unit_claimed'`, wfID, unitID).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if handles != 2 || claims != 2 {
		t.Fatalf("handles=%d claims=%d", handles, claims)
	}
}

func TestIssueRequiresImplementationToBeginWithoutHandleEffects(t *testing.T) {
	m, db, _, wfID, unitID := testManager(t)
	if _, err := db.DB().Exec(`UPDATE workflows SET state='plan_approved' WHERE id=?`, wfID); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "handles")

	result, err := m.Issue(context.Background(), wfID, unitID, "implementer", dir)
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("issue before implementation error=%v", err)
	}
	if result != (IssueResult{}) {
		t.Fatalf("issue before implementation returned result: %#v", result)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("issue before implementation created handle directory: %v", err)
	}
	var count int
	if err := db.DB().QueryRow(`SELECT count(*) FROM handles WHERE workflow_id=?`, wfID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("issue before implementation persisted %d handles", count)
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
	m, db, clock, wfID, unitID := testManager(t)
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
	if _, err := db.DB().Exec(`INSERT INTO evidence(workflow_id,unit_id,revision,actor,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, wfID, unitID, 1, "implementer", "red", "exit 1", "green", "exit 0", "", "all", "exit 0", "internal", "now"); err != nil {
		t.Fatal(err)
	}
	review, err := m.IssueForPurpose(context.Background(), wfID, unitID, "reviewer", dir, PurposeReview)
	if err != nil {
		t.Fatal(err)
	}
	for purpose, path := range map[Purpose]string{PurposeImplementation: implementation.Path, PurposeReview: review.Path} {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var handle Handle
		if unmarshalErr := json.Unmarshal(data, &handle); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		if want := clock.now.Add(15 * time.Minute).Format(timestampLayout); handle.ExpiresAt != want {
			t.Fatalf("%s lease expiry=%s want=%s", purpose, handle.ExpiresAt, want)
		}
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

func TestLeasePromotesWithoutRenewalAndEnforcesActorSeparation(t *testing.T) {
	m, _, clock, wfID, unitID := testManager(t)
	result, _ := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	issuedExpiry := clock.now.Add(15 * time.Minute).Format(timestampLayout)
	clock.advance(time.Minute)
	h, err := m.Use(context.Background(), result.Path, "implementer", TDD)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != Active || h.ExpiresAt != issuedExpiry {
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
	if h.ExpiresAt != issuedExpiry {
		t.Fatalf("review renewed lease=%#v", h)
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

func TestLeaseExpiresAtFifteenMinutesDespiteSuccessfulUse(t *testing.T) {
	m, _, clock, wfID, unitID := testManager(t)
	result, err := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	if err != nil {
		t.Fatal(err)
	}
	clock.advance(14 * time.Minute)
	if _, err = m.Use(context.Background(), result.Path, "implementer", TDD); err != nil {
		t.Fatal(err)
	}
	clock.advance(2 * time.Minute)
	if _, err = m.Use(context.Background(), result.Path, "implementer", TDD); !errors.Is(err, ErrExpired) {
		t.Fatalf("lease extended past issued cap: %v", err)
	}
}

func TestExpiredHandleIsAtomicallyDeletedWithoutUnitMutation(t *testing.T) {
	m, db, clock, wfID, unitID := testManager(t)
	result, _ := m.Issue(context.Background(), wfID, unitID, "implementer", filepath.Join(t.TempDir(), "handles"))
	clock.advance(16 * time.Minute)
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
	clock.advance(16 * time.Minute)
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

func TestRecoverAggregateBatchCreatesOneBoundedTransactionAndRejectsLegacyAdapter(t *testing.T) {
	m, db, _, wfID, units := aggregateManager(t, 1)
	dir := filepath.Join(t.TempDir(), "handles")
	if _, err := m.RecoverAggregateAt(context.Background(), wfID, units[0], 5, "coordinator", dir); !errors.Is(err, ErrRecoveryForbidden) {
		t.Fatalf("policy-aware legacy recovery error = %v", err)
	}
	if _, err := db.DB().Exec(`INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES('superseded',?,?,'active','hash','old','now','later',1,'implementation')`, wfID, units[0]); err != nil {
		t.Fatal(err)
	}
	request := AggregateRecoveryRequest{AggregateReviewRevision: 5, Groups: []CorrectionGroup{{CausalInvariant: "shared safety boundary", Findings: []string{"fix both units"}, UnitIDs: units}}, Assignments: []RecoveryAssignment{{UnitID: units[0], Actor: "implementer-a"}, {UnitID: units[1], Actor: "implementer-b"}}}
	result, err := m.RecoverAggregateBatchAt(context.Background(), wfID, 5, "coordinator", dir, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Handles) != 2 {
		t.Fatalf("handles = %#v", result.Handles)
	}
	for _, handle := range result.Handles {
		info, statErr := os.Stat(handle.Path)
		if statErr != nil || info.Mode().Perm() != 0o600 || handle.Secret != "" || handle.UnitRevision != 2 {
			t.Fatalf("handle=%#v info=%v error=%v", handle, info, statErr)
		}
	}
	var workflowState string
	var revision, pending, facts, activities, active, revoked int
	_ = db.DB().QueryRow(`SELECT state,revision FROM workflows WHERE id=?`, wfID).Scan(&workflowState, &revision)
	_ = db.DB().QueryRow(`SELECT count(*) FROM work_units WHERE workflow_id=? AND state='pending' AND revision=2`, wfID).Scan(&pending)
	_ = db.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=? AND kind='aggregate_correction'`, wfID).Scan(&facts)
	_ = db.DB().QueryRow(`SELECT count(*) FROM activities WHERE workflow_id=? AND action='aggregate_correction_started'`, wfID).Scan(&activities)
	_ = db.DB().QueryRow(`SELECT count(*) FROM handles WHERE workflow_id=? AND state='intent'`, wfID).Scan(&active)
	_ = db.DB().QueryRow(`SELECT count(*) FROM handles WHERE claim_id='superseded' AND state='revoked'`, wfID).Scan(&revoked)
	if workflowState != "implementing" || revision != 6 || pending != 2 || facts != 1 || activities != 1 || active != 2 || revoked != 1 {
		t.Fatalf("state=%s revision=%d pending=%d facts=%d activities=%d active=%d revoked=%d", workflowState, revision, pending, facts, activities, active, revoked)
	}
	var content string
	_ = db.DB().QueryRow(`SELECT content FROM artifacts WHERE workflow_id=? AND kind='aggregate_correction'`, wfID).Scan(&content)
	if !strings.Contains(content, `"authority":"automatic"`) || strings.Contains(content, dir) || strings.Contains(content, "secret_hash") || strings.Contains(content, "claim") {
		t.Fatalf("unsafe aggregate correction artifact: %s", content)
	}
}

func TestAggregateRecoveryHandleExpiresAtExactIssuedBoundaryWithoutMutation(t *testing.T) {
	m, db, clock, wfID, units := aggregateManager(t, 1)
	dir := filepath.Join(t.TempDir(), "handles")
	request := AggregateRecoveryRequest{
		AggregateReviewRevision: 5,
		Groups: []CorrectionGroup{{
			CausalInvariant: "shared safety boundary",
			Findings:        []string{"repair the selected unit"},
			UnitIDs:         []string{units[0]},
		}},
		Assignments: []RecoveryAssignment{{UnitID: units[0], Actor: "repairer"}},
	}
	result, err := m.RecoverAggregateBatchAt(context.Background(), wfID, 5, "coordinator", dir, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Handles) != 1 {
		t.Fatalf("handles = %#v", result.Handles)
	}
	handle := result.Handles[0]
	data, err := os.ReadFile(handle.Path)
	if err != nil {
		t.Fatal(err)
	}
	var document Handle
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	wantExpiry := clock.now.Add(15 * time.Minute).Format(timestampLayout)
	if document.IssuedAt != clock.now.Format(timestampLayout) || document.ExpiresAt != wantExpiry {
		t.Fatalf("aggregate recovery lease issued=%s expires=%s want expiry=%s", document.IssuedAt, document.ExpiresAt, wantExpiry)
	}

	clock.advance(15 * time.Minute)
	if _, err = m.UseForAtPurpose(context.Background(), handle.Path, wfID, units[0], 2, "repairer", TDD, PurposeImplementation); !errors.Is(err, ErrExpired) {
		t.Fatalf("exact-boundary use error = %v", err)
	}
	if _, err = os.Lstat(handle.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired aggregate recovery handle still exists: %v", err)
	}
	var handleState, unitState, workflowState string
	var unitRevision, workflowRevision int64
	if err = db.DB().QueryRow(`SELECT state FROM handles WHERE claim_id=?`, handle.ClaimID).Scan(&handleState); err != nil {
		t.Fatal(err)
	}
	if err = db.DB().QueryRow(`SELECT state,revision FROM work_units WHERE workflow_id=? AND id=?`, wfID, units[0]).Scan(&unitState, &unitRevision); err != nil {
		t.Fatal(err)
	}
	if err = db.DB().QueryRow(`SELECT state,revision FROM workflows WHERE id=?`, wfID).Scan(&workflowState, &workflowRevision); err != nil {
		t.Fatal(err)
	}
	if handleState != "revoked" || unitState != "pending" || unitRevision != 2 || workflowState != "implementing" || workflowRevision != 6 {
		t.Fatalf("handle=%s unit=%s@%d workflow=%s@%d", handleState, unitState, unitRevision, workflowState, workflowRevision)
	}
}

func TestRecoverAggregateBatchValidationAndFailureLeaveNoAuthorityOrFiles(t *testing.T) {
	m, db, _, wfID, units := aggregateManager(t, 1)
	dir := filepath.Join(t.TempDir(), "handles")
	duplicate := AggregateRecoveryRequest{AggregateReviewRevision: 5, Groups: []CorrectionGroup{{CausalInvariant: "shared", Findings: []string{"one"}, UnitIDs: []string{units[0], units[0]}}}, Assignments: []RecoveryAssignment{{UnitID: units[0], Actor: "implementer"}}}
	if _, err := m.RecoverAggregateBatchAt(context.Background(), wfID, 5, "coordinator", dir, duplicate); err == nil {
		t.Fatal("duplicate unit accepted")
	}
	if _, err := db.DB().Exec(`CREATE TRIGGER reject_correction_activity BEFORE INSERT ON activities WHEN NEW.action='aggregate_correction_started' BEGIN SELECT RAISE(ABORT, 'reject activity'); END`); err != nil {
		t.Fatal(err)
	}
	valid := AggregateRecoveryRequest{AggregateReviewRevision: 5, Groups: []CorrectionGroup{{CausalInvariant: "shared", Findings: []string{"one"}, UnitIDs: units}}, Assignments: []RecoveryAssignment{{UnitID: units[0], Actor: "a"}, {UnitID: units[1], Actor: "b"}}}
	if _, err := m.RecoverAggregateBatchAt(context.Background(), wfID, 5, "coordinator", dir, valid); err == nil {
		t.Fatal("activity failure accepted")
	}
	entries, _ := os.ReadDir(dir)
	var revision, facts, handles int
	_ = db.DB().QueryRow(`SELECT revision FROM workflows WHERE id=?`, wfID).Scan(&revision)
	_ = db.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE workflow_id=? AND kind='aggregate_correction'`, wfID).Scan(&facts)
	_ = db.DB().QueryRow(`SELECT count(*) FROM handles WHERE workflow_id=?`, wfID).Scan(&handles)
	if len(entries) != 0 || revision != 5 || facts != 0 || handles != 0 {
		t.Fatalf("rollback files=%d revision=%d facts=%d handles=%d", len(entries), revision, facts, handles)
	}
}

func aggregateManager(t *testing.T, automaticRounds int) (*Manager, *store.Store, *mutableClock, string, []string) {
	t.Helper()
	m, db, clock, wfID, first := testManager(t)
	units := []string{first, "wu-000000000000000000000002"}
	_, _ = db.DB().Exec(`UPDATE workflows SET state='ready_to_complete',revision=5 WHERE id=?`, wfID)
	_, _ = db.DB().Exec(`UPDATE work_units SET state='done' WHERE id=?`, first)
	_, _ = db.DB().Exec(`INSERT INTO work_units(id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,revision) VALUES(?,?,'second','internal/second','[]','[]',1,1,'done',1)`, units[1], wfID)
	body := fmt.Sprintf(`{"summary":"batch","scope":"internal","work_units":[{"id":"%s","description":"first","scope":"internal/first","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1},{"id":"%s","description":"second","scope":"internal/second","areas":[],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1,"aggregate_correction_policy":{"automatic_rounds":%d,"on_exhaustion":"require_user_authorization"}}`, units[0], units[1], automaticRounds)
	_, _ = db.DB().Exec(`INSERT INTO plans(workflow_id,summary,scope,max_parallel_units,body) VALUES(?,'batch','internal',1,?)`, wfID, body)
	_, _ = db.DB().Exec(`INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'aggregate_review','{"verdict":"corrections","findings":"fix"}','reviewer',5,'now')`, wfID)
	return m, db, clock, wfID, units
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
