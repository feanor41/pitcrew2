package correction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

var ErrAuthorizationForbidden = errors.New("correction authorization is forbidden")

type AuthorizationRequest struct {
	AggregateReviewRevision int64  `json:"aggregate_review_revision"`
	Reason                  string `json:"reason"`
	UserDirectionConfirmed  bool   `json:"user_direction_confirmed"`
}

func (r AuthorizationRequest) Validate() error {
	if r.AggregateReviewRevision < 1 || strings.TrimSpace(r.Reason) == "" || utf8.RuneCountInString(r.Reason) > 1024 || !r.UserDirectionConfirmed {
		return ErrAuthorizationForbidden
	}
	return nil
}

type AuthorizationOutcome struct {
	Revision   int64  `json:"revision"`
	State      string `json:"state"`
	ArtifactID int64  `json:"artifact_id"`
}

type AuthorizationService struct {
	db  *sql.DB
	now func() time.Time
}

func NewAuthorizationService(s *store.Store, now func() time.Time) *AuthorizationService {
	return &AuthorizationService{db: s.DB(), now: now}
}

func (s *AuthorizationService) Authorize(ctx context.Context, workflowID string, expected int64, actor string, request AuthorizationRequest) (AuthorizationOutcome, error) {
	if strings.TrimSpace(actor) == "" || request.Validate() != nil {
		return AuthorizationOutcome{}, ErrAuthorizationForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthorizationOutcome{}, err
	}
	defer tx.Rollback()
	var state string
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT state,revision FROM workflows WHERE id=?`, workflowID).Scan(&state, &revision); err != nil {
		return AuthorizationOutcome{}, err
	}
	if revision != expected {
		return AuthorizationOutcome{}, store.ErrCASMismatch
	}
	if state != "ready_to_complete" {
		return AuthorizationOutcome{}, ErrAuthorizationForbidden
	}
	projection, err := Project(ctx, tx, workflowID, "")
	if err != nil {
		return AuthorizationOutcome{}, err
	}
	if projection.BlockerRevision != request.AggregateReviewRevision || projection.Authority != AuthorityNone || projection.NextAction != "user authorization required" {
		return AuthorizationOutcome{}, ErrAuthorizationForbidden
	}
	body, err := json.Marshal(request)
	if err != nil {
		return AuthorizationOutcome{}, err
	}
	at, next := ids.FormatTime(s.now()), expected+1
	update, err := tx.ExecContext(ctx, `UPDATE workflows SET revision=?,updated_at=? WHERE id=? AND revision=? AND state='ready_to_complete'`, next, at, workflowID, expected)
	if err != nil {
		return AuthorizationOutcome{}, err
	}
	if changed, changeErr := update.RowsAffected(); changeErr != nil || changed != 1 {
		if changeErr != nil {
			return AuthorizationOutcome{}, changeErr
		}
		return AuthorizationOutcome{}, store.ErrCASMismatch
	}
	artifact, err := tx.ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,'correction_authorization',?,?,?,?)`, workflowID, string(body), actor, next, at)
	if err != nil {
		return AuthorizationOutcome{}, err
	}
	artifactID, err := artifact.LastInsertId()
	if err != nil {
		return AuthorizationOutcome{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,'ready_to_complete','ready_to_complete',?,'correction_authorized',?,?)`, workflowID, actor, next, at); err != nil {
		return AuthorizationOutcome{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO activities(workflow_id,unit_id,action,actor,at,subject_kind,subject_id) VALUES(?,NULL,'correction_authorized',?,?,'artifact',?)`, workflowID, actor, at, fmt.Sprint(artifactID)); err != nil {
		return AuthorizationOutcome{}, err
	}
	if err = tx.Commit(); err != nil {
		return AuthorizationOutcome{}, err
	}
	return AuthorizationOutcome{Revision: next, State: state, ArtifactID: artifactID}, nil
}
