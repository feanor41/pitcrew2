package roadmap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

var ErrBindingConflict = errors.New("roadmap binding conflicts with existing acknowledgement")

type DigestConflict struct {
	RoadmapID string
}

func (e *DigestConflict) Error() string {
	return fmt.Sprintf("roadmap publication digest changed for %s; prepare again", e.RoadmapID)
}

func (e *DigestConflict) Unwrap() error { return store.ErrCASMismatch }

type AcknowledgeInput struct {
	Provider       string `json:"provider"`
	Namespace      string `json:"namespace"`
	ExternalID     string `json:"external_id"`
	URL            string `json:"url"`
	PreparedDigest string `json:"prepared_digest"`
}

func (s *Service) Acknowledge(ctx context.Context, id string, input AcknowledgeInput) (Item, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback()

	item, err := scanItem(tx.QueryRowContext(ctx, itemQuery+` WHERE i.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	if item.Binding != nil {
		if sameBinding(item.Binding, input) {
			return item, nil
		}
		return Item{}, ErrBindingConflict
	}
	if err := validateAcknowledgement(input); err != nil {
		return Item{}, err
	}
	prepared, err := prepareGitHub(item, input.Provider, input.Namespace)
	if err != nil {
		return Item{}, err
	}
	if input.PreparedDigest != prepared.Digest {
		return Item{}, &DigestConflict{RoadmapID: id}
	}

	acknowledgedAt := ids.FormatTime(s.now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO roadmap_bindings(roadmap_id,provider,namespace,external_id,url,prepared_digest,acknowledged_at) VALUES(?,?,?,?,?,?,?)`, id, input.Provider, input.Namespace, input.ExternalID, input.URL, input.PreparedDigest, acknowledgedAt); err != nil {
		if bindingCollision(ctx, tx, input) {
			return Item{}, ErrBindingConflict
		}
		return Item{}, fmt.Errorf("record roadmap binding: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE roadmap_items SET local_lifecycle='acknowledged' WHERE id=? AND local_lifecycle='captured'`, id)
	if err != nil {
		return Item{}, fmt.Errorf("acknowledge roadmap item: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Item{}, err
	}
	if changed != 1 {
		return Item{}, errors.New("roadmap item is not captured")
	}
	acknowledged, err := scanItem(tx.QueryRowContext(ctx, itemQuery+` WHERE i.id=?`, id))
	if err != nil {
		return Item{}, err
	}
	if err := tx.Commit(); err != nil {
		return Item{}, err
	}
	return acknowledged, nil
}

func sameBinding(binding *Binding, input AcknowledgeInput) bool {
	return binding.Provider == input.Provider &&
		binding.Namespace == input.Namespace &&
		binding.ExternalID == input.ExternalID &&
		binding.URL == input.URL &&
		binding.PreparedDigest == input.PreparedDigest
}

var (
	positiveDecimal = regexp.MustCompile(`^[1-9][0-9]*$`)
	sha256Digest    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func validateAcknowledgement(input AcknowledgeInput) error {
	if err := validateGitHubTarget(input.Provider, input.Namespace); err != nil {
		return err
	}
	if !positiveDecimal.MatchString(input.ExternalID) {
		return errors.New("external_id must be a positive decimal GitHub issue number")
	}
	wantURL := "https://github.com/" + input.Namespace + "/issues/" + input.ExternalID
	if input.URL != wantURL {
		return errors.New("url must be the canonical GitHub issue URL")
	}
	if !sha256Digest.MatchString(input.PreparedDigest) {
		return errors.New("prepared_digest must be a lowercase SHA-256 digest")
	}
	return nil
}

func bindingCollision(ctx context.Context, tx *sql.Tx, input AcknowledgeInput) bool {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM roadmap_bindings WHERE (provider=? AND namespace=? AND external_id=?) OR url=?`, input.Provider, input.Namespace, input.ExternalID, input.URL).Scan(&count)
	return err == nil && count != 0
}
