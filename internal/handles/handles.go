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
	"unicode/utf8"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/correction"
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
	Path         string
	ClaimID      string
	Generation   int
	UnitRevision int64
	Secret       string
}

type CorrectionGroup struct {
	CausalInvariant string   `json:"causal_invariant"`
	Findings        []string `json:"findings"`
	UnitIDs         []string `json:"unit_ids"`
}
type RecoveryAssignment struct {
	UnitID string `json:"unit_id"`
	Actor  string `json:"actor"`
}
type AggregateRecoveryRequest struct {
	AggregateReviewRevision int64                `json:"aggregate_review_revision"`
	Groups                  []CorrectionGroup    `json:"groups"`
	Assignments             []RecoveryAssignment `json:"assignments"`
}
type AggregateRecoveryResult struct {
	Handles []IssueResult `json:"handles"`
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
	return m.issue(ctx, workflowID, unitID, 0, actor, dir, purpose, false, false, issueAction(purpose))
}

func (m *Manager) IssueAt(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string) (IssueResult, error) {
	return m.IssueAtForPurpose(ctx, workflowID, unitID, revision, actor, dir, PurposeImplementation)
}

func (m *Manager) IssueAtForPurpose(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string, purpose Purpose) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, revision, actor, dir, purpose, false, false, issueAction(purpose))
}

func (m *Manager) HandoffReviewAt(ctx context.Context, workflowID, unitID string, revision int64, reviewer, dir string) (IssueResult, error) {
	return m.IssueAtForPurpose(ctx, workflowID, unitID, revision, reviewer, dir, PurposeReview)
}

func (m *Manager) IssueDebug(ctx context.Context, workflowID, unitID, actor, dir string) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, 0, actor, dir, PurposeImplementation, false, true, "")
}

func (m *Manager) IssueDebugAt(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, revision, actor, dir, PurposeImplementation, false, true, "")
}

func (m *Manager) Recover(ctx context.Context, workflowID, unitID, actor, dir string) (IssueResult, error) {
	return m.RecoverForPurpose(ctx, workflowID, unitID, actor, dir, PurposeImplementation)
}

func (m *Manager) RecoverForPurpose(ctx context.Context, workflowID, unitID, actor, dir string, purpose Purpose) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, 0, actor, dir, purpose, true, false, "")
}

func (m *Manager) RecoverAt(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string) (IssueResult, error) {
	return m.RecoverAtForPurpose(ctx, workflowID, unitID, revision, actor, dir, PurposeImplementation)
}

func (m *Manager) RecoverAtForPurpose(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string, purpose Purpose) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, revision, actor, dir, purpose, true, false, "")
}

func (m *Manager) RecoverReviewAt(ctx context.Context, workflowID, unitID string, revision int64, reviewer, dir string) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, revision, reviewer, dir, PurposeReview, true, false, activity.UnitReviewRecovered)
}

// RecoverAggregateAt reopens one explicitly selected completed unit after the
// latest aggregate review requested corrections. It creates fresh implementation
// authority; callers must use the returned handle for new TDD evidence.
func (m *Manager) RecoverAggregateAt(ctx context.Context, workflowID, unitID string, revision int64, actor, dir string) (IssueResult, error) {
	return m.issue(ctx, workflowID, unitID, revision, actor, dir, PurposeImplementation, true, false, activity.UnitAggregateRecovered)
}

// RecoverAggregateBatchAt atomically reopens one bounded causal correction batch.
func (m *Manager) RecoverAggregateBatchAt(ctx context.Context, workflowID string, workflowRevision int64, coordinator, dir string, request AggregateRecoveryRequest) (AggregateRecoveryResult, error) {
	units, actors, err := validateAggregateRequest(coordinator, request)
	if err != nil {
		return AggregateRecoveryResult{}, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return AggregateRecoveryResult{}, err
	}
	defer tx.Rollback()
	var state string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM workflows WHERE id=?`, workflowID).Scan(&state, &current); err != nil {
		return AggregateRecoveryResult{}, err
	}
	if current != workflowRevision {
		return AggregateRecoveryResult{}, store.ErrCASMismatch
	}
	if state != "ready_to_complete" {
		return AggregateRecoveryResult{}, fmt.Errorf("%w: current state %s; expected ready_to_complete", ErrInvalidState, state)
	}
	projection, err := correction.Project(ctx, tx, workflowID, "")
	if err != nil {
		return AggregateRecoveryResult{}, err
	}
	if projection.BlockerRevision != request.AggregateReviewRevision || (projection.Authority != correction.AuthorityAutomatic && projection.Authority != correction.AuthorityAuthorized) {
		return AggregateRecoveryResult{}, ErrRecoveryForbidden
	}
	for _, unitID := range units {
		var unitState string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM work_units WHERE workflow_id=? AND id=?`, workflowID, unitID).Scan(&unitState); err != nil {
			return AggregateRecoveryResult{}, err
		}
		if unitState != "done" {
			return AggregateRecoveryResult{}, fmt.Errorf("%w: unit %s current state %s; expected done", ErrInvalidState, unitID, unitState)
		}
	}
	if err = prepareDirectory(dir); err != nil {
		return AggregateRecoveryResult{}, err
	}
	now, at := m.now().UTC(), ids.FormatTime(m.now())
	authorizationID := int64(0)
	if projection.Authority == correction.AuthorityAuthorized {
		authorizationID, err = currentAuthorizationID(ctx, tx, workflowID, projection.BlockerRevision)
		if err != nil {
			return AggregateRecoveryResult{}, err
		}
	}
	fact := struct {
		AggregateReviewRevision int64                `json:"aggregate_review_revision"`
		Groups                  []CorrectionGroup    `json:"groups"`
		Assignments             []RecoveryAssignment `json:"assignments"`
		Authority               correction.Authority `json:"authority"`
		AuthorizationArtifactID int64                `json:"authorization_artifact_id,omitempty"`
	}{request.AggregateReviewRevision, request.Groups, request.Assignments, projection.Authority, authorizationID}
	body, err := json.Marshal(fact)
	if err != nil {
		return AggregateRecoveryResult{}, err
	}
	artifact, err := tx.ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,?,?,?,?,?)`, workflowID, "aggregate_correction", string(body), coordinator, workflowRevision+1, at)
	if err != nil {
		return AggregateRecoveryResult{}, err
	}
	artifactID, err := artifact.LastInsertId()
	if err != nil {
		return AggregateRecoveryResult{}, err
	}
	result := AggregateRecoveryResult{Handles: make([]IssueResult, 0, len(units))}
	var paths []string
	committed := false
	defer func() {
		if !committed {
			for _, path := range paths {
				_ = os.Remove(path)
			}
		}
	}()
	for _, unitID := range units {
		if _, err = tx.ExecContext(ctx, `UPDATE handles SET state='revoked' WHERE workflow_id=? AND unit_id=? AND purpose='implementation' AND state!='revoked'`, workflowID, unitID); err != nil {
			return AggregateRecoveryResult{}, err
		}
		var unitRevision int64
		if err = tx.QueryRowContext(ctx, `UPDATE work_units SET state='pending',revision=revision+1 WHERE workflow_id=? AND id=? AND state='done' RETURNING revision`, workflowID, unitID).Scan(&unitRevision); err != nil {
			return AggregateRecoveryResult{}, err
		}
		var generation int
		if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(claim_generation),0)+1 FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='implementation'`, workflowID, unitID).Scan(&generation); err != nil {
			return AggregateRecoveryResult{}, err
		}
		claimID, randomErr := randomHex(m.entropy, 16)
		if randomErr != nil {
			return AggregateRecoveryResult{}, randomErr
		}
		secret, randomErr := randomHex(m.entropy, 16)
		if randomErr != nil {
			return AggregateRecoveryResult{}, randomErr
		}
		digest := sha256.Sum256([]byte(secret))
		h := Handle{Version: 1, State: Intent, WorkflowID: workflowID, UnitID: unitID, ClaimID: claimID, SecretHash: hex.EncodeToString(digest[:]), IssuedAt: ids.FormatTime(now), ExpiresAt: ids.FormatTime(now.Add(handleLifetime))}
		if _, err = tx.ExecContext(ctx, `INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES(?,?,?,?,?,?,?,?,?,'implementation')`, claimID, workflowID, unitID, Intent, h.SecretHash, actors[unitID], h.IssuedAt, h.ExpiresAt, generation); err != nil {
			return AggregateRecoveryResult{}, err
		}
		path := filepath.Join(dir, claimID+".json")
		if err = writeExclusive(path, h); err != nil {
			return AggregateRecoveryResult{}, err
		}
		paths = append(paths, path)
		result.Handles = append(result.Handles, IssueResult{Path: path, ClaimID: claimID, Generation: generation, UnitRevision: unitRevision})
	}
	workflowUpdate, err := tx.ExecContext(ctx, `UPDATE workflows SET state='implementing',revision=revision+1,updated_at=? WHERE id=? AND revision=? AND state='ready_to_complete'`, at, workflowID, workflowRevision)
	if err != nil {
		return AggregateRecoveryResult{}, err
	}
	if changed, changeErr := workflowUpdate.RowsAffected(); changeErr != nil || changed != 1 {
		if changeErr != nil {
			return AggregateRecoveryResult{}, changeErr
		}
		return AggregateRecoveryResult{}, store.ErrCASMismatch
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,'ready_to_complete','implementing',?,'aggregate_corrections',?,?)`, workflowID, coordinator, workflowRevision+1, at); err != nil {
		return AggregateRecoveryResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES(?,NULL,'aggregate_correction_started',?,?,'artifact',?)`, workflowID, coordinator, at, fmt.Sprint(artifactID)); err != nil {
		return AggregateRecoveryResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return AggregateRecoveryResult{}, err
	}
	committed = true
	return result, nil
}

func validateAggregateRequest(coordinator string, request AggregateRecoveryRequest) ([]string, map[string]string, error) {
	if strings.TrimSpace(coordinator) == "" || utf8.RuneCountInString(coordinator) > 128 || request.AggregateReviewRevision < 1 || len(request.Groups) == 0 || len(request.Groups) > 16 {
		return nil, nil, ErrRecoveryForbidden
	}
	seen := map[string]bool{}
	var units []string
	for _, group := range request.Groups {
		if strings.TrimSpace(group.CausalInvariant) == "" || utf8.RuneCountInString(group.CausalInvariant) > 256 || len(group.Findings) == 0 || len(group.Findings) > 32 || len(group.UnitIDs) == 0 {
			return nil, nil, ErrRecoveryForbidden
		}
		for _, finding := range group.Findings {
			if strings.TrimSpace(finding) == "" || utf8.RuneCountInString(finding) > 1024 {
				return nil, nil, ErrRecoveryForbidden
			}
		}
		for _, unitID := range group.UnitIDs {
			if seen[unitID] || strings.TrimSpace(unitID) == "" {
				return nil, nil, ErrRecoveryForbidden
			}
			seen[unitID], units = true, append(units, unitID)
		}
	}
	actors := map[string]string{}
	for _, assignment := range request.Assignments {
		if !seen[assignment.UnitID] || actors[assignment.UnitID] != "" || strings.TrimSpace(assignment.Actor) == "" || utf8.RuneCountInString(assignment.Actor) > 128 {
			return nil, nil, ErrRecoveryForbidden
		}
		actors[assignment.UnitID] = assignment.Actor
	}
	if len(actors) != len(units) {
		return nil, nil, ErrRecoveryForbidden
	}
	return units, actors, nil
}

func currentAuthorizationID(ctx context.Context, tx *sql.Tx, workflowID string, blockerRevision int64) (int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,content FROM artifacts WHERE workflow_id=? AND kind='correction_authorization' AND accepted_revision>? ORDER BY accepted_revision DESC,id DESC`, workflowID, blockerRevision)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var body string
		var value struct {
			AggregateReviewRevision int64 `json:"aggregate_review_revision"`
			UserDirectionConfirmed  bool  `json:"user_direction_confirmed"`
		}
		if err = rows.Scan(&id, &body); err != nil {
			return 0, err
		}
		if json.Unmarshal([]byte(body), &value) == nil && value.AggregateReviewRevision == blockerRevision && value.UserDirectionConfirmed {
			return id, nil
		}
	}
	return 0, ErrRecoveryForbidden
}

func (m *Manager) issue(ctx context.Context, workflowID, unitID string, expectedRevision int64, actor, dir string, purpose Purpose, recovery, debug bool, action activity.Action) (IssueResult, error) {
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
	var workflowRevision int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM workflows WHERE id=?`, workflowID).Scan(&workflowState, &workflowRevision); err != nil {
		return IssueResult{}, err
	}
	aggregateRecovery := action == activity.UnitAggregateRecovered
	if aggregateRecovery {
		if workflowState != "ready_to_complete" {
			return IssueResult{}, fmt.Errorf("%w: current state %s; expected ready_to_complete", ErrInvalidState, workflowState)
		}
		if workflowRevision != expectedRevision {
			return IssueResult{}, store.ErrCASMismatch
		}
		projection, projectionErr := correction.Project(ctx, tx, workflowID, "")
		if projectionErr != nil {
			return IssueResult{}, projectionErr
		}
		if projection.PolicyAware || projection.BlockerRevision == 0 || projection.Authority == correction.AuthorityNone {
			return IssueResult{}, ErrRecoveryForbidden
		}
		var aggregateBody string
		if err = tx.QueryRowContext(ctx, `SELECT content FROM artifacts WHERE workflow_id=? AND kind='aggregate_review' ORDER BY id DESC LIMIT 1`, workflowID).Scan(&aggregateBody); err != nil {
			return IssueResult{}, ErrRecoveryForbidden
		}
		var aggregate struct {
			Verdict string `json:"verdict"`
		}
		if err = json.Unmarshal([]byte(aggregateBody), &aggregate); err != nil || aggregate.Verdict != "corrections" {
			return IssueResult{}, ErrRecoveryForbidden
		}
	} else if workflowState != "plan_approved" && workflowState != "implementing" {
		return IssueResult{}, fmt.Errorf("%w: current state %s; expected plan_approved or implementing", ErrInvalidState, workflowState)
	}
	if (action == activity.UnitReviewHandedOff || action == activity.UnitReviewRecovered) && workflowState != "implementing" {
		return IssueResult{}, fmt.Errorf("%w: current state %s; expected implementing", ErrInvalidState, workflowState)
	}
	var unitState string
	var unitRevision int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM work_units WHERE workflow_id=? AND id=?`, workflowID, unitID).Scan(&unitState, &unitRevision); err != nil {
		return IssueResult{}, err
	}
	if aggregateRecovery {
		if unitState != "done" {
			return IssueResult{}, fmt.Errorf("%w: current state %s; expected done", ErrInvalidState, unitState)
		}
		// Aggregate corrections plus the matching workflow and unit revisions are
		// the recovery authority. actor remains declarative metadata for the new
		// handle and cannot authorize this transition.
		result, updateErr := tx.ExecContext(ctx, `UPDATE work_units SET state='pending',revision=revision+1 WHERE workflow_id=? AND id=? AND state='done' AND revision=?`, workflowID, unitID, unitRevision)
		if updateErr != nil {
			return IssueResult{}, updateErr
		}
		if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
			if changeErr != nil {
				return IssueResult{}, changeErr
			}
			return IssueResult{}, store.ErrCASMismatch
		}
		at := ids.FormatTime(m.now())
		result, updateErr = tx.ExecContext(ctx, `UPDATE workflows SET state='implementing',revision=revision+1,updated_at=? WHERE id=? AND revision=? AND state='ready_to_complete'`, at, workflowID, workflowRevision)
		if updateErr != nil {
			return IssueResult{}, updateErr
		}
		if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
			if changeErr != nil {
				return IssueResult{}, changeErr
			}
			return IssueResult{}, store.ErrCASMismatch
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,?,?,?,?,?,?)`, workflowID, "ready_to_complete", "implementing", actor, "aggregate_corrections", workflowRevision+1, at); err != nil {
			return IssueResult{}, err
		}
		unitRevision++
		workflowState, unitState, expectedRevision = "implementing", "pending", unitRevision
	}
	if expectedRevision > 0 && unitRevision != expectedRevision && !aggregateRecovery {
		return IssueResult{}, store.ErrCASMismatch
	}
	wantUnitState := "pending"
	if purpose == PurposeReview {
		wantUnitState = "reviewing"
	}
	if !recovery && unitState != wantUnitState {
		return IssueResult{}, fmt.Errorf("%w: current state %s; expected %s", ErrInvalidState, unitState, wantUnitState)
	}
	if action == activity.UnitReviewHandedOff || action == activity.UnitReviewRecovered {
		var evidenceActor string
		if err = tx.QueryRowContext(ctx, `SELECT actor FROM evidence WHERE workflow_id=? AND unit_id=? AND revision=?`, workflowID, unitID, unitRevision).Scan(&evidenceActor); err != nil {
			return IssueResult{}, ErrInvalidState
		}
		if actor == evidenceActor {
			return IssueResult{}, ErrIdentityCollision
		}
		var verdicts int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM reviews WHERE workflow_id=? AND unit_id=? AND revision=?`, workflowID, unitID, unitRevision).Scan(&verdicts); err != nil {
			return IssueResult{}, err
		}
		if verdicts != 0 {
			return IssueResult{}, ErrInvalidState
		}
	}
	var existing, generation int
	if err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(max(claim_generation),0) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose=?`, workflowID, unitID, purpose).Scan(&existing, &generation); err != nil {
		return IssueResult{}, err
	}
	if recovery {
		if action == activity.UnitReviewRecovered {
			if unitState != "reviewing" || existing == 0 {
				return IssueResult{}, ErrRecoveryForbidden
			}
			var owner, state, expires string
			if err = tx.QueryRowContext(ctx, `SELECT actor_identity,state,expires_at FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='review' ORDER BY claim_generation DESC LIMIT 1`, workflowID, unitID).Scan(&owner, &state, &expires); err != nil {
				return IssueResult{}, err
			}
			if actor != owner {
				return IssueResult{}, ErrIdentityCollision
			}
			var latestHandoff, previousReview int64
			if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(CASE WHEN action='unit_review_handed_off' THEN id END),0),coalesce(max(CASE WHEN action='unit_review_recorded' AND subject_id=? THEN id END),0) FROM activities WHERE workflow_id=? AND unit_id=?`, fmt.Sprintf("%s@%d", unitID, unitRevision-1), workflowID, unitID).Scan(&latestHandoff, &previousReview); err != nil {
				return IssueResult{}, err
			}
			if latestHandoff <= previousReview {
				return IssueResult{}, ErrRecoveryForbidden
			}
			expiresAt, parseErr := time.Parse(timestampLayout, expires)
			if state != "revoked" && (parseErr != nil || m.now().Before(expiresAt)) {
				return IssueResult{}, ErrAlreadyClaimed
			}
		} else if unitState == "reviewing" {
			if existing == 0 {
				return IssueResult{}, ErrRecoveryForbidden
			}
			var evidenceActor string
			if err = tx.QueryRowContext(ctx, `SELECT actor FROM evidence WHERE workflow_id=? AND unit_id=? AND revision=?`, workflowID, unitID, unitRevision).Scan(&evidenceActor); err != nil || actor != evidenceActor {
				return IssueResult{}, ErrRecoveryForbidden
			}
		} else {
			var evidence int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM evidence WHERE workflow_id=? AND unit_id=? AND revision=?`, workflowID, unitID, unitRevision).Scan(&evidence); err != nil {
				return IssueResult{}, err
			}
			if evidence != 0 {
				return IssueResult{}, ErrRecoveryForbidden
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE handles SET state='revoked' WHERE workflow_id=? AND unit_id=? AND purpose=? AND state!='revoked'`, workflowID, unitID, purpose); err != nil {
			return IssueResult{}, err
		}
	} else if existing != 0 {
		if action != activity.UnitReviewHandedOff {
			return IssueResult{}, ErrAlreadyClaimed
		}
		var live, previousCorrections int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM handles WHERE workflow_id=? AND unit_id=? AND purpose='review' AND state!='revoked'`, workflowID, unitID).Scan(&live); err != nil {
			return IssueResult{}, err
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM reviews WHERE workflow_id=? AND unit_id=? AND revision=? AND verdict='corrections'`, workflowID, unitID, unitRevision-1).Scan(&previousCorrections); err != nil {
			return IssueResult{}, err
		}
		var latestHandoff, previousReview int64
		if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(CASE WHEN action='unit_review_handed_off' THEN id END),0),coalesce(max(CASE WHEN action='unit_review_recorded' AND subject_id=? THEN id END),0) FROM activities WHERE workflow_id=? AND unit_id=?`, fmt.Sprintf("%s@%d", unitID, unitRevision-1), workflowID, unitID).Scan(&latestHandoff, &previousReview); err != nil {
			return IssueResult{}, err
		}
		if live != 0 || previousCorrections != 1 || previousReview <= latestHandoff {
			return IssueResult{}, ErrAlreadyClaimed
		}
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
	issuedState := Intent
	if recovery && purpose == PurposeImplementation && unitState == "reviewing" {
		issuedState = Active
	}
	h := Handle{Version: 1, State: issuedState, WorkflowID: workflowID, UnitID: unitID, ClaimID: claimID, SecretHash: hex.EncodeToString(digest[:]), IssuedAt: ids.FormatTime(issued), ExpiresAt: ids.FormatTime(expires)}
	path := filepath.Join(dir, claimID+".json")
	generation++
	if _, err = tx.ExecContext(ctx, `INSERT INTO handles(claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose) VALUES(?,?,?,?,?,?,?,?,?,?)`, claimID, workflowID, unitID, issuedState, h.SecretHash, actor, h.IssuedAt, h.ExpiresAt, generation, purpose); err != nil {
		return IssueResult{}, err
	}
	if action == "" {
		action = activity.UnitClaimed
		if recovery {
			action = activity.UnitClaimRecovered
		}
	}
	if err = activity.AppendTx(ctx, tx, activity.New(workflowID, unitID, action, actor, issued, activity.UnitSubject(unitID))); err != nil {
		return IssueResult{}, err
	}
	if err = writeAtomic(path, h); err != nil {
		return IssueResult{}, err
	}
	result := IssueResult{Path: path, ClaimID: claimID, Generation: generation, UnitRevision: unitRevision}
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

func issueAction(p Purpose) activity.Action {
	if p == PurposeReview {
		return activity.UnitReviewHandedOff
	}
	return ""
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

func writeExclusive(path string, h Handle) error {
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
	if err = os.Link(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err = os.Remove(tmp); err != nil {
		_ = os.Remove(path)
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
