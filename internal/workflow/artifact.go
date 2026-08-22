package workflow

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

type Artifact struct {
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Actor      string `json:"actor"`
	Revision   int64  `json:"revision"`
	RecordedAt string `json:"recorded_at"`
}

func (s *Service) RecordArtifact(ctx context.Context, workflowID string, expected int64, event EventType, kind, content, actor string) (Workflow, error) {
	if strings.TrimSpace(content) == "" {
		return Workflow{}, fmt.Errorf("artifact content is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workflow{}, err
	}
	defer tx.Rollback()
	current, err := workflowInTx(ctx, tx, workflowID)
	if err != nil {
		return Workflow{}, err
	}
	if current.Revision != expected {
		return Workflow{}, store.ErrCASMismatch
	}
	next, ok := nextState(current.State, event)
	if !ok {
		return Workflow{}, transitionError(current.State, event)
	}
	at, revision := ids.FormatTime(s.now()), expected+1
	result, err := tx.ExecContext(ctx, `UPDATE workflows SET state=?,revision=?,updated_at=? WHERE id=? AND revision=?`, next, revision, at, workflowID, expected)
	if err != nil {
		return Workflow{}, err
	}
	if changed, changeErr := result.RowsAffected(); changeErr != nil || changed != 1 {
		if changeErr != nil {
			return Workflow{}, changeErr
		}
		return Workflow{}, store.ErrCASMismatch
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,?,?,?,?,?)`, workflowID, kind, content, actor, revision, at); err != nil {
		return Workflow{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,?,?,?,?,?,?)`, workflowID, current.State, next, actor, "", revision, at); err != nil {
		return Workflow{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workflow{}, err
	}
	current.State, current.Revision, current.UpdatedAt = next, revision, at
	return current, nil
}

func (s *Service) Artifacts(ctx context.Context, workflowID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind,content,actor,accepted_revision,recorded_at FROM artifacts WHERE workflow_id=? ORDER BY accepted_revision,id`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Artifact{}
	for rows.Next() {
		var artifact Artifact
		if err := rows.Scan(&artifact.Kind, &artifact.Content, &artifact.Actor, &artifact.Revision, &artifact.RecordedAt); err != nil {
			return nil, err
		}
		result = append(result, artifact)
	}
	return result, rows.Err()
}

func workflowInTx(ctx context.Context, tx *sql.Tx, id string) (Workflow, error) {
	var current Workflow
	var name sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id,revision,state,name,goal,created_at,updated_at FROM workflows WHERE id=?`, id).Scan(&current.ID, &current.Revision, &current.State, &name, &current.Goal, &current.CreatedAt, &current.UpdatedAt)
	if err == sql.ErrNoRows {
		return Workflow{}, ErrNotFound
	}
	if err == nil {
		current.Name, current.NameDerived = DisplayName(name, current.Goal)
	}
	return current, err
}
