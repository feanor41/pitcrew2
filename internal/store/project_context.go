package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/projectcontext"
)

// ProjectContextSnapshot is the validated singleton stored for one project.
type ProjectContextSnapshot struct {
	Record    projectcontext.Record
	Actor     string
	UpdatedAt string
}

// LoadProjectContext loads and validates the stored singleton without repair.
func (s *Store) LoadProjectContext(ctx context.Context) (ProjectContextSnapshot, bool, error) {
	available, err := s.projectContextAvailable(ctx)
	if err != nil || !available {
		return ProjectContextSnapshot{}, false, err
	}
	var schemaVersion int
	var content, actor, updatedAt string
	err = s.db.QueryRowContext(ctx, `SELECT schema_version,content,actor,updated_at FROM project_context WHERE singleton=1`).Scan(&schemaVersion, &content, &actor, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectContextSnapshot{}, false, nil
	}
	if err != nil {
		return ProjectContextSnapshot{}, false, err
	}
	snapshot, err := decodeProjectContext(schemaVersion, content, actor, updatedAt)
	if err != nil {
		return ProjectContextSnapshot{}, true, err
	}
	return snapshot, true, nil
}

func (s *Store) projectContextAvailable(ctx context.Context) (bool, error) {
	var migrationName string
	migrationErr := s.db.QueryRowContext(ctx, `SELECT name FROM schema_migrations WHERE version=5`).Scan(&migrationName)
	if migrationErr != nil && !errors.Is(migrationErr, sql.ErrNoRows) {
		return false, migrationErr
	}
	var tables int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('project_context','project_context_audits')`).Scan(&tables); err != nil {
		return false, err
	}
	if errors.Is(migrationErr, sql.ErrNoRows) && tables == 0 {
		return false, nil
	}
	if migrationErr != nil || migrationName != "project context" || tables != 2 {
		return false, fmt.Errorf("%w: inconsistent V5 project-context schema", ErrInvalidState)
	}
	return true, nil
}

func decodeProjectContext(schemaVersion int, content, actor, updatedAt string) (ProjectContextSnapshot, error) {
	var record projectcontext.Record
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return ProjectContextSnapshot{}, invalidStoredContext(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ProjectContextSnapshot{}, invalidStoredContext(errors.New("trailing content"))
	}
	if schemaVersion != record.SchemaVersion {
		return ProjectContextSnapshot{}, invalidStoredContext(errors.New("schema version mismatch"))
	}
	if err := projectcontext.Validate(record); err != nil {
		return ProjectContextSnapshot{}, invalidStoredContext(err)
	}
	if err := projectcontext.ValidateActor(actor); err != nil {
		return ProjectContextSnapshot{}, invalidStoredContext(err)
	}
	parsedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil || parsedAt.Location() != time.UTC {
		return ProjectContextSnapshot{}, invalidStoredContext(errors.New("updated_at must be UTC RFC3339"))
	}
	return ProjectContextSnapshot{Record: projectcontext.CloneRecord(record), Actor: actor, UpdatedAt: updatedAt}, nil
}

// ReplaceProjectContext atomically replaces changed content and appends its audit.
func (s *Store) ReplaceProjectContext(ctx context.Context, record projectcontext.Record, actor string, at time.Time) (bool, error) {
	if err := projectcontext.Validate(record); err != nil {
		return false, err
	}
	if err := projectcontext.ValidateActor(actor); err != nil {
		return false, err
	}
	record = projectcontext.CloneRecord(record)
	content, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	changed := projectcontext.Categories()
	var previousSchema int
	var previousContent, previousActor, previousUpdatedAt string
	err = tx.QueryRowContext(ctx, `SELECT schema_version,content,actor,updated_at FROM project_context WHERE singleton=1`).Scan(&previousSchema, &previousContent, &previousActor, &previousUpdatedAt)
	if err == nil {
		previous, decodeErr := decodeProjectContext(previousSchema, previousContent, previousActor, previousUpdatedAt)
		if decodeErr != nil {
			return false, decodeErr
		}
		changed = changedCategories(previous.Record, record)
		if len(changed) == 0 {
			return false, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	updatedAt := at.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_context(singleton,schema_version,content,actor,updated_at) VALUES(1,?,?,?,?) ON CONFLICT(singleton) DO UPDATE SET schema_version=excluded.schema_version,content=excluded.content,actor=excluded.actor,updated_at=excluded.updated_at`, record.SchemaVersion, string(content), actor, updatedAt); err != nil {
		return false, err
	}
	encodedChanged, _ := json.Marshal(changed)
	if _, err = tx.ExecContext(ctx, `INSERT INTO project_context_audits(actor,updated_at,changed_categories) VALUES(?,?,?)`, actor, updatedAt, string(encodedChanged)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func changedCategories(before, after projectcontext.Record) []string {
	var changed []string
	for _, category := range projectcontext.Categories() {
		if !reflect.DeepEqual(before.Facts[category], after.Facts[category]) {
			changed = append(changed, category)
		}
	}
	return changed
}

func invalidStoredContext(err error) error {
	return fmt.Errorf("%w: corrupt stored project context: %v", projectcontext.ErrInvalidRecord, err)
}
