package checkpoint_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/checkpoint"
	"github.com/fmazzalomo/pitcrew/internal/project"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestNewReviewedAcceptsIdentifiableDirtyResultWithoutPublication(t *testing.T) {
	fingerprint := identifiableFingerprint(t)
	fingerprint.Dirty = true
	fingerprint.Untracked = true
	recordedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

	reviewed, err := checkpoint.NewReviewed("wf-000000000000000000000001", 7, fingerprint, nil, nil, recordedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !reviewed.Dirty || reviewed.CommitRef != nil || reviewed.DeliveryID != nil || reviewed.ResultDigest != fingerprint.ResultDigest {
		t.Fatalf("reviewed checkpoint = %#v", reviewed)
	}
}

func TestNewReviewedRejectsUnidentifiedOrInconsistentResult(t *testing.T) {
	tests := map[string]func(*project.RepositoryFingerprint){
		"project":  func(value *project.RepositoryFingerprint) { value.ProjectID = "" },
		"checkout": func(value *project.RepositoryFingerprint) { value.CheckoutRoot = "relative" },
		"base":     func(value *project.RepositoryFingerprint) { value.BaseRevision = "" },
		"head":     func(value *project.RepositoryFingerprint) { value.HeadRevision = "" },
		"digest":   func(value *project.RepositoryFingerprint) { value.ResultDigest = "" },
		"dirt": func(value *project.RepositoryFingerprint) {
			value.Dirty = false
			value.Staged = true
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			fingerprint := identifiableFingerprint(t)
			mutate(&fingerprint)
			if _, err := checkpoint.NewReviewed("wf-000000000000000000000001", 1, fingerprint, nil, nil, time.Now()); err == nil {
				t.Fatal("NewReviewed accepted an unidentified result")
			}
		})
	}
}

func TestPersistStoresOneImmutableReviewedCheckpoint(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.DB().ExecContext(ctx, `INSERT INTO workflows(id,revision,state,goal,created_at,updated_at) VALUES(?,7,'reviewing','goal','now','now')`, "wf-000000000000000000000001"); err != nil {
		t.Fatal(err)
	}
	fingerprint := identifiableFingerprint(t)
	fingerprint.Dirty = true
	fingerprint.Staged = true
	recordedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	reviewed, err := checkpoint.NewReviewed("wf-000000000000000000000001", 7, fingerprint, nil, nil, recordedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Persist(ctx, database.DB(), reviewed); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Persist(ctx, database.DB(), reviewed); err == nil {
		t.Fatal("Persist rewrote an immutable checkpoint")
	}

	var projectID, checkoutRoot, base, head, digest, at string
	var dirty int
	if err := database.DB().QueryRowContext(ctx, `SELECT project_id,checkout_root,base_revision,head_revision,result_digest,dirty,recorded_at FROM reviewed_checkpoints WHERE workflow_id=? AND aggregate_revision=?`, reviewed.WorkflowID, reviewed.AggregateRevision).Scan(&projectID, &checkoutRoot, &base, &head, &digest, &dirty, &at); err != nil {
		t.Fatal(err)
	}
	if projectID != fingerprint.ProjectID || checkoutRoot != fingerprint.CheckoutRoot || base != fingerprint.BaseRevision || head != fingerprint.HeadRevision || digest != fingerprint.ResultDigest || dirty != 1 || at != recordedAt.Format(time.RFC3339Nano) {
		t.Fatalf("stored checkpoint = %q %q %q %q %q %d %q", projectID, checkoutRoot, base, head, digest, dirty, at)
	}
}

func identifiableFingerprint(t *testing.T) project.RepositoryFingerprint {
	t.Helper()
	return project.RepositoryFingerprint{
		ProjectID:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CheckoutRoot: filepath.Join(t.TempDir(), "checkout"),
		BaseRevision: "0123456789abcdef0123456789abcdef01234567",
		HeadRevision: "0123456789abcdef0123456789abcdef01234567",
		ResultDigest: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
	}
}
