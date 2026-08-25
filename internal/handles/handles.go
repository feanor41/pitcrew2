package handles

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

const (
	timestampLayout = "2006-01-02T15:04:05.000Z"
	handleLifetime  = 5 * time.Minute
)

var (
	ErrInvalid           = errors.New("invalid claim handle")
	ErrExpired           = errors.New("expired claim handle")
	ErrUnsafePath        = errors.New("unsafe claim handle path")
	ErrUnsafePermissions = errors.New("unsafe claim handle permissions")
	ErrIdentityCollision = errors.New("implementer and reviewer identities must differ")
	ErrRecoveryForbidden = errors.New("claim recovery is forbidden")
	ErrAlreadyClaimed    = errors.New("work unit already has a claim")
	ErrInvalidState      = errors.New("invalid work unit state")
)

type State string
type Operation string
type Purpose string

const (
	Intent   State     = "intent"
	Active   State     = "active"
	TDD      Operation = "tdd"
	Review   Operation = "review"
	Complete Operation = "complete"

	PurposeImplementation Purpose = "implementation"
	PurposeReview         Purpose = "review"
)

type Handle struct {
	Version    int    `json:"version"`
	State      State  `json:"state"`
	WorkflowID string `json:"workflow_id"`
	UnitID     string `json:"unit_id"`
	ClaimID    string `json:"claim_id"`
	SecretHash string `json:"secret_hash"`
	IssuedAt   string `json:"issued_at"`
	ExpiresAt  string `json:"expires_at"`
}

type IssueResult struct {
	Path       string
	ClaimID    string
	Generation int
	Secret     string
}

type Manager struct {
	db      *sql.DB
	now     func() time.Time
	entropy io.Reader
}

func New(s *store.Store, now func() time.Time, entropy io.Reader) *Manager {
	return &Manager{db: s.DB(), now: now, entropy: entropy}
}

func (m *Manager) Issue(ctx context.Context, workflowID, unitID, actor, dir string) (IssueResult, error) {
	return m.IssueForPurpose(ctx, workflowID, unitID, actor, dir, PurposeImplementation)
}

func (m *Manager) IssueForPurpose(ctx context.Context, workflowID, unitID, actor, dir string, purpose Purpose) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, 0, actor, dir, purpose, false, false)
}

func (m *Manager) IssueAt(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string) (IssueResult, error) {
	return m.IssueAtForPurpose(ctx, workflowID, unitID, revision, actor, dir, PurposeImplementation)
}

func (m *Manager) IssueAtForPurpose(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string, purpose Purpose) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, revision, actor, dir, purpose, false, false)
}

func (m *Manager) IssueDebug(ctx context.Context, workflowID, unitID, actor, dir string) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, 0, actor, dir, PurposeImplementation, false, true)
}

func (m *Manager) IssueDebugAt(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, revision, actor, dir, PurposeImplementation, false, true)
}

func (m *Manager) Recover(ctx context.Context, workflowID, unitID, actor, dir string) (IssueResult, error) {
	return m.RecoverForPurpose(ctx, workflowID, unitID, actor, dir, PurposeImplementation)
}

func (m *Manager) RecoverForPurpose(ctx context.Context, workflowID, unitID, actor, dir string, purpose Purpose) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, 0, actor, dir, purpose, true, false)
}

func (m *Manager) RecoverAt(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string) (IssueResult, error) {
	return m.RecoverAtForPurpose(ctx, workflowID, unitID, revision, actor, dir, PurposeImplementation)
}

func (m *Manager) RecoverAtForPurpose(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string, purpose Purpose) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, revision, actor, dir, purpose, true, false)
}

func (m *Manager) issue(ctx context.Context, workflowID, unitID string, expectedRevision int64, actor, dir string, purpose Purpose, recovery, debug bool) (IssueResult, error) {
	if strings.TrimSpace(actor) == "" {
		return IssueResult{}, ErrIdentityCollision
	}
	if !purpose.valid() {
		return IssueResult{}, ErrInvalid
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return IssueResult{}, err
	}
	defer tx.Rollback()
	var workflowState string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM workflows WHERE id=?`, workflowID).Scan(&workflowState); err != nil {
		return IssueResult{}, err
	}
	if workflowState != "plan_approved" && workflowState != "implementing" {
		return IssueResult{}, fmt.Errorf("%w: current state %s; expected plan_approved or implementing", ErrInvalidState, workflowState)
	}
	var unitState string
	var unitRevision int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM work_units WHERE workflow_id=? AND id=?`, workflowID, unitID).Scan(&unitState, &unitRevision); err != nil {
		return IssueResult{}, err
	}
	if expectedRevision > 0 && unitRevision != expectedRevision {
		return IssueResult{}, store.ErrCASMismatch
	}
	wantUnitState := "pending"
	if purpose == PurposeReview {
		wantUnitState = "reviewing"
	}
	if !recovery && unitState != wantUnitState {
		return IssueResult{}, fmt.Errorf("%w: current state %s; expected %s", ErrInvalidState, unitState, wantUnitState)
	}
	var existing, generation int
	if err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(max(claim_generation),0) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose=?`, workflowID, unitID, purpose).Scan(&existing, &generation); err != nil {
		return IssueResult{}, err
	}
	if recovery {
		var evidence int
		if unitState == "reviewing" {
			return IssueResult{}, ErrRecoveryForbidden
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM evidence WHERE workflow_id=? AND unit_id=? AND revision=?`, workflowID, unitID, unitRevision).Scan(&evidence); err != nil {
			return IssueResult{}, err
		}
		if evidence != 0 {
			return IssueResult{}, ErrRecoveryForbidden
		}
		if _, err = tx.ExecContext(ctx, `UPDATE handles SET state='revoked' WHERE workflow_id=? AND unit_id=? AND purpose=? AND state!='revoked'`, workflowID, unitID, purpose); err != nil {
			return IssueResult{}, err
		}
	} else if existing != 0 {
		return IssueResult{}, ErrAlreadyClaimed
	}
	if err = prepareDirectory(dir); err != nil {
		return IssueResult{}, err
	}
	claimID, err := randomHex(m.entropy, 16)
	if err != nil {
		return IssueResult{}, err
	}
	secret, err := randomHex(m.entropy, 16)
	if err != nil {
		return IssueResult{}, err
	}
	digest := sha256.Sum256([]byte(secret))
	issued := m.now().UTC()
	expires := issued.Add(handleLifetime)
	h := Handle{Version: 1, State: Intent, WorkflowID: workflowID, UnitID: unitID, ClaimID: claimID, SecretHash: hex.EncodeToString(digest[:]), IssuedAt: ids.FormatTime(issued), ExpiresAt: ids.FormatTime(expires)}
	path := filepath.Join(dir, claimID+".json")
	generation++
	if _, err = tx.ExecContext(ctx, `INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES(?,?,?,?,?,?,?,?,?,?)`, claimID, workflowID, unitID, Intent, h.SecretHash, actor, h.IssuedAt, h.ExpiresAt, generation, purpose); err != nil {
		return IssueResult{}, err
	}
	action := activity.UnitClaimed
	if recovery {
		action = activity.UnitClaimRecovered
	}
	if err = activity.AppendTx(ctx, tx, activity.New(workflowID, unitID, action, actor, issued, activity.UnitSubject(unitID))); err != nil {
		return IssueResult{}, err
	}
	if err = writeAtomic(path, h); err != nil {
		return IssueResult{}, err
	}
	result := IssueResult{Path: path, ClaimID: claimID, Generation: generation}
	if debug {
		if err = os.Remove(path); err != nil {
			return IssueResult{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE handles SET state='revoked' WHERE claim_id=?`, claimID); err != nil {
			return IssueResult{}, err
		}
		result.Secret = secret
	}
	if err = tx.Commit(); err != nil {
		_ = os.Remove(path)
		return IssueResult{}, err
	}
	return result, nil
}

func (m *Manager) Use(ctx context.Context, path, actor string, operation Operation) (Handle, error) {
	return m.use(ctx, path, "", "", 0, actor, operation, "")
}

func (m *Manager) UseForPurpose(ctx context.Context, path, actor string, operation Operation, purpose Purpose) (Handle, error) {
	return m.use(ctx, path, "", "", 0, actor, operation, purpose)
}

func (m *Manager) UseFor(ctx context.Context, path, workflowID, unitID string, revision int64, actor string, operation Operation) (Handle, error) {
	return m.use(ctx, path, workflowID, unitID, revision, actor, operation, "")
}

func (m *Manager) UseForAtPurpose(ctx context.Context, path, workflowID, unitID string, revision int64, actor string, operation Operation, purpose Purpose) (Handle, error) {
	return m.use(ctx, path, workflowID, unitID, revision, actor, operation, purpose)
}

// UseForMutation validates the claim and commits its promotion or refresh in
// the same transaction as mutate. A failed mutation leaves both the database
// claim and the on-disk handle unchanged.
func (m *Manager) UseForMutation(ctx context.Context, path, workflowID, unitID string, revision int64, actor string, operation Operation, mutate func(*sql.Tx, Handle) error) (Handle, error) {
	return m.useForMutation(ctx, path, workflowID, unitID, revision, actor, operation, "", mutate)
}

func (m *Manager) UseForMutationAtPurpose(ctx context.Context, path, workflowID, unitID string, revision int64, actor string, operation Operation, purpose Purpose, mutate func(*sql.Tx, Handle) error) (Handle, error) {
	return m.useForMutation(ctx, path, workflowID, unitID, revision, actor, operation, purpose, mutate)
}

func (m *Manager) use(ctx context.Context, path, workflowID, unitID string, revision int64, actor string, operation Operation, purpose Purpose) (Handle, error) {
	return m.useForMutation(ctx, path, workflowID, unitID, revision, actor, operation, purpose, nil)
}

func (m *Manager) useForMutation(ctx context.Context, path, workflowID, unitID string, revision int64, actor string, operation Operation, purpose Purpose, mutate func(*sql.Tx, Handle) error) (Handle, error) {
	if strings.TrimSpace(actor) == "" {
		return Handle{}, ErrIdentityCollision
	}
	if purpose != "" && !purpose.valid() {
		return Handle{}, ErrInvalid
	}
	h, err := readSecure(path)
	if err != nil {
		return Handle{}, err
	}
	if (workflowID != "" && h.WorkflowID != workflowID) || (unitID != "" && h.UnitID != unitID) {
		return Handle{}, ErrInvalid
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return Handle{}, err
	}
	defer tx.Rollback()
	if revision > 0 {
		var current int64
		if err = tx.QueryRowContext(ctx, `SELECT revision FROM work_units WHERE workflow_id=? AND id=?`, workflowID, unitID).Scan(&current); err != nil {
			return Handle{}, err
		}
		if current != revision {
			return Handle{}, store.ErrCASMismatch
		}
	}
	var dbState State
	var hash, owner, expires string
	var storedPurpose Purpose
	var generation, latestGeneration int
	err = tx.QueryRowContext(ctx, `SELECT state,secret_hash,actor_identity,expires_at,claim_generation,purpose FROM handles WHERE claim_id=? AND workflow_id=? AND unit_id=?`, h.ClaimID, h.WorkflowID, h.UnitID).Scan(&dbState, &hash, &owner, &expires, &generation, &storedPurpose)
	if errors.Is(err, sql.ErrNoRows) || dbState == "revoked" {
		return Handle{}, ErrInvalid
	}
	if err != nil {
		return Handle{}, err
	}
	if (purpose == "" && storedPurpose != PurposeImplementation) || (purpose != "" && storedPurpose != purpose) {
		return Handle{}, ErrInvalid
	}
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(claim_generation),0) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose=?`, h.WorkflowID, h.UnitID, storedPurpose).Scan(&latestGeneration); err != nil {
		return Handle{}, err
	}
	if generation != latestGeneration {
		return Handle{}, ErrInvalid
	}
	if hash != h.SecretHash || expires != h.ExpiresAt || dbState != h.State {
		return Handle{}, ErrInvalid
	}
	expiresAt, err := time.Parse(timestampLayout, h.ExpiresAt)
	if err != nil {
		return Handle{}, ErrInvalid
	}
	if !m.now().Before(expiresAt) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Handle{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE handles SET state='revoked' WHERE claim_id=?`, h.ClaimID); err == nil {
			err = tx.Commit()
		}
		if err != nil {
			_ = writeAtomic(path, h)
			return Handle{}, err
		}
		return Handle{}, ErrExpired
	}
	if purpose != "" && purpose != operation.purpose() {
		return Handle{}, ErrInvalid
	}
	switch operation {
	case TDD:
		if actor != owner {
			return Handle{}, ErrIdentityCollision
		}
	case Review:
		if purpose == PurposeReview {
			if actor != owner {
				return Handle{}, ErrIdentityCollision
			}
		} else {
			if actor == owner {
				return Handle{}, ErrIdentityCollision
			}
			if h.State != Active {
				return Handle{}, ErrInvalid
			}
		}
	case Complete:
		if actor != owner {
			return Handle{}, ErrIdentityCollision
		}
		if h.State != Active {
			return Handle{}, ErrInvalid
		}
	default:
		return Handle{}, ErrInvalid
	}
	if mutate != nil {
		if err = mutate(tx, h); err != nil {
			return Handle{}, err
		}
		if operation == Complete || purpose == PurposeReview && operation == Review {
			var completedState State
			if err = tx.QueryRowContext(ctx, `SELECT state FROM handles WHERE claim_id=?`, h.ClaimID).Scan(&completedState); err != nil {
				return Handle{}, err
			}
			if completedState != "revoked" {
				return Handle{}, ErrInvalid
			}
			if err = os.Remove(path); err != nil {
				return Handle{}, err
			}
			if err = tx.Commit(); err != nil {
				_ = writeAtomic(path, h)
				return Handle{}, err
			}
			return h, nil
		}
	}
	h.State = Active
	h.ExpiresAt = ids.FormatTime(m.now().Add(handleLifetime))
	result, err := tx.ExecContext(ctx, `UPDATE handles SET state=?,expires_at=? WHERE claim_id=? AND state!='revoked'`, h.State, h.ExpiresAt, h.ClaimID)
	if err != nil {
		return Handle{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Handle{}, err
	}
	if changed != 1 {
		return Handle{}, ErrInvalid
	}
	if err = writeAtomic(path, h); err != nil {
		return Handle{}, err
	}
	if err = tx.Commit(); err != nil {
		_ = writeAtomic(path, Handle{Version: h.Version, State: dbState, WorkflowID: h.WorkflowID, UnitID: h.UnitID, ClaimID: h.ClaimID, SecretHash: h.SecretHash, IssuedAt: h.IssuedAt, ExpiresAt: expires})
		return Handle{}, err
	}
	return h, nil
}

func (m *Manager) Revoke(ctx context.Context, path, actor string) error {
	return m.revoke(ctx, path, actor, "")
}

func (m *Manager) RevokeForPurpose(ctx context.Context, path, actor string, purpose Purpose) error {
	return m.revoke(ctx, path, actor, purpose)
}

func (m *Manager) revoke(ctx context.Context, path, actor string, purpose Purpose) error {
	if strings.TrimSpace(actor) == "" {
		return ErrIdentityCollision
	}
	h, err := readSecure(path)
	if err != nil {
		return err
	}
	var owner, state string
	var storedPurpose Purpose
	if err = m.db.QueryRowContext(ctx, `SELECT actor_identity,state,purpose FROM handles WHERE claim_id=?`, h.ClaimID).Scan(&owner, &state, &storedPurpose); errors.Is(err, sql.ErrNoRows) || state == "revoked" {
		return ErrInvalid
	} else if err != nil {
		return err
	}
	if actor != owner {
		return ErrIdentityCollision
	}
	if purpose != "" && (!purpose.valid() || purpose != storedPurpose) {
		return ErrInvalid
	}
	if err = os.Remove(path); err != nil {
		return err
	}
	result, err := m.db.ExecContext(ctx, `UPDATE handles SET state='revoked' WHERE claim_id=? AND state!='revoked'`, h.ClaimID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrInvalid
	}
	return nil
}

func (p Purpose) valid() bool { return p == PurposeImplementation || p == PurposeReview }

func (o Operation) purpose() Purpose {
	if o == Review {
		return PurposeReview
	}
	return PurposeImplementation
}

func RemoveRevokedFile(path string) error {
	if _, err := readSecure(path); err != nil {
		return err
	}
	return os.Remove(path)
}

func prepareDirectory(dir string) error {
	if info, err := os.Lstat(dir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return ErrUnsafePath
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o700 {
		return ErrUnsafePermissions
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && !ownedBy(stat.Uid, uint32(os.Geteuid())) {
		return ErrUnsafePermissions
	}
	return nil
}

func readSecure(path string) (Handle, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Handle{}, ErrInvalid
	}
	if err != nil {
		return Handle{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Handle{}, ErrUnsafePath
	}
	if info.Mode().Perm() != 0o600 {
		return Handle{}, ErrUnsafePermissions
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && !ownedBy(stat.Uid, uint32(os.Geteuid())) {
		return Handle{}, ErrUnsafePermissions
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ELOOP) {
		return Handle{}, ErrUnsafePath
	}
	if err != nil {
		return Handle{}, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	var h Handle
	if err = decoder.Decode(&h); err != nil {
		return Handle{}, ErrInvalid
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Handle{}, ErrInvalid
	}
	if h.Version != 1 || (h.State != Intent && h.State != Active) {
		return Handle{}, ErrInvalid
	}
	return h, nil
}

func writeAtomic(path string, h Handle) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := func() { _ = f.Close(); _ = os.Remove(tmp) }
	if err = json.NewEncoder(f).Encode(h); err != nil {
		cleanup()
		return err
	}
	if err = f.Sync(); err != nil {
		cleanup()
		return err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
func randomHex(source io.Reader, n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(source, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func ownedBy(actual, want uint32) bool { return actual == want }
