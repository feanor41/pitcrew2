// Package checkpoint defines and persists immutable identities for reviewed
// repository results. Aggregate approval policy and workflow transitions live
// outside this package.
package checkpoint

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/project"
)

// ErrInvalid identifies a checkpoint that cannot name one reviewed result.
var ErrInvalid = errors.New("invalid reviewed checkpoint")

// Reviewed is the immutable persistence domain for one aggregate revision.
type Reviewed struct {
	WorkflowID        string
	AggregateRevision int64
	ProjectID         string
	CheckoutRoot      string
	BaseRevision      string
	HeadRevision      string
	ResultDigest      string
	Dirty             bool
	CommitRef         *string
	DeliveryID        *string
	RecordedAt        time.Time
}

// NewReviewed binds a captured repository fingerprint to an aggregate
// revision. Publication references are optional, including for dirty results.
func NewReviewed(workflowID string, aggregateRevision int64, fingerprint project.RepositoryFingerprint, commitRef, deliveryID *string, recordedAt time.Time) (Reviewed, error) {
	result := Reviewed{
		WorkflowID:        workflowID,
		AggregateRevision: aggregateRevision,
		ProjectID:         fingerprint.ProjectID,
		CheckoutRoot:      fingerprint.CheckoutRoot,
		BaseRevision:      fingerprint.BaseRevision,
		HeadRevision:      fingerprint.HeadRevision,
		ResultDigest:      fingerprint.ResultDigest,
		Dirty:             fingerprint.Dirty,
		CommitRef:         copyString(commitRef),
		DeliveryID:        copyString(deliveryID),
		RecordedAt:        recordedAt.UTC(),
	}
	if fingerprint.Dirty != (fingerprint.Staged || fingerprint.Unstaged || fingerprint.Untracked) {
		return Reviewed{}, fmt.Errorf("%w: dirty status conflicts with repository distinctions", ErrInvalid)
	}
	if err := validate(result); err != nil {
		return Reviewed{}, err
	}
	return result, nil
}

// Execer is implemented by both *sql.DB and *sql.Tx so an aggregate service
// can include checkpoint persistence in its own atomic transition.
type Execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Persist inserts every identifying field in one statement. The V6 primary
// key makes a reviewed workflow revision immutable rather than updateable.
func Persist(ctx context.Context, destination Execer, reviewed Reviewed) error {
	if destination == nil {
		return fmt.Errorf("%w: persistence destination is required", ErrInvalid)
	}
	if err := validate(reviewed); err != nil {
		return err
	}
	_, err := destination.ExecContext(ctx, `
INSERT INTO reviewed_checkpoints(
    workflow_id, aggregate_revision, project_id, checkout_root,
    base_revision, head_revision, result_digest, dirty,
    commit_ref, delivery_id, recorded_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		reviewed.WorkflowID,
		reviewed.AggregateRevision,
		reviewed.ProjectID,
		reviewed.CheckoutRoot,
		reviewed.BaseRevision,
		reviewed.HeadRevision,
		reviewed.ResultDigest,
		boolInt(reviewed.Dirty),
		nullableString(reviewed.CommitRef),
		nullableString(reviewed.DeliveryID),
		reviewed.RecordedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("persist reviewed checkpoint: %w", err)
	}
	return nil
}

func validate(reviewed Reviewed) error {
	if !validPrefixedID(reviewed.WorkflowID, "wf-") {
		return fmt.Errorf("%w: workflow ID is required", ErrInvalid)
	}
	if reviewed.AggregateRevision <= 0 {
		return fmt.Errorf("%w: aggregate revision must be positive", ErrInvalid)
	}
	if !validHex(reviewed.ProjectID, 64) {
		return fmt.Errorf("%w: project ID must be a SHA-256 digest", ErrInvalid)
	}
	if !filepath.IsAbs(reviewed.CheckoutRoot) || filepath.Clean(reviewed.CheckoutRoot) != reviewed.CheckoutRoot {
		return fmt.Errorf("%w: checkout root must be canonical and absolute", ErrInvalid)
	}
	if !validObjectID(reviewed.BaseRevision) || !validObjectID(reviewed.HeadRevision) {
		return fmt.Errorf("%w: base and head revisions must identify Git objects", ErrInvalid)
	}
	if !validHex(reviewed.ResultDigest, 64) {
		return fmt.Errorf("%w: result digest must be a SHA-256 digest", ErrInvalid)
	}
	if reviewed.RecordedAt.IsZero() {
		return fmt.Errorf("%w: recorded time is required", ErrInvalid)
	}
	for name, value := range map[string]*string{"commit reference": reviewed.CommitRef, "delivery ID": reviewed.DeliveryID} {
		if value != nil && (strings.TrimSpace(*value) == "" || strings.ContainsRune(*value, '\x00')) {
			return fmt.Errorf("%w: %s must be non-empty", ErrInvalid, name)
		}
	}
	return nil
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func validObjectID(value string) bool {
	return validHex(value, 40) || validHex(value, 64)
}

func validHex(value string, length int) bool {
	if len(value) != length || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded)*2 == length
}

func validPrefixedID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return false
	}
	for _, character := range value[len(prefix):] {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
