package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

var (
	ErrInvalidNormativeArtifact = errors.New("invalid normative artifact")
	ErrInvalidBaseline          = errors.New("invalid workflow baseline")
	ErrLineageCycle             = errors.New("workflow lineage cycle")
	ErrLineageDepth             = errors.New("workflow lineage exceeds depth 32")
)

var normativeIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+$`)

type NormativeArtifact struct {
	Content       string           `json:"content"`
	SchemaVersion int              `json:"schema_version"`
	Entries       []NormativeEntry `json:"entries"`
}

type NormativeEntry struct {
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	ParentID  string          `json:"parent_id,omitempty"`
	Operation string          `json:"operation"`
	Body      json.RawMessage `json:"body,omitempty"`
}

type ArtifactIdentity struct {
	ID       int64  `json:"artifact_id"`
	Kind     string `json:"kind"`
	Revision int64  `json:"accepted_revision"`
}

type NormativeSource struct {
	WorkflowID string `json:"workflow_id"`
	ArtifactID int64  `json:"artifact_id"`
	Revision   int64  `json:"accepted_revision"`
}

type ResolvedNormativeEntry struct {
	Phase     string          `json:"phase"`
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	ParentID  string          `json:"parent_id,omitempty"`
	Operation string          `json:"operation"`
	Body      json.RawMessage `json:"body"`
	Source    NormativeSource `json:"source"`
}

type BaselineIdentity struct {
	WorkflowID string             `json:"workflow_id"`
	Revision   int64              `json:"revision"`
	Artifacts  []ArtifactIdentity `json:"artifacts"`
}

type ResolvedNormative struct {
	WorkflowID string                   `json:"workflow_id"`
	Structured bool                     `json:"structured"`
	Baseline   *BaselineIdentity        `json:"baseline,omitempty"`
	Entries    []ResolvedNormativeEntry `json:"entries"`
}

type Artifact struct {
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Actor      string `json:"actor"`
	Revision   int64  `json:"revision"`
	RecordedAt string `json:"recorded_at"`
}

func (s *Service) RecordArtifact(ctx context.Context, workflowID string, expected int64, event EventType, kind, content, actor string) (Workflow, error) {
	return s.recordArtifact(ctx, workflowID, expected, event, kind, content, actor, nil)
}

// RecordNormativeArtifact records a schema-v1 stage artifact and its typed
// entries atomically. RecordArtifact remains the compatibility path for prose
// artifacts and never guesses structure from their content.
func (s *Service) RecordNormativeArtifact(ctx context.Context, workflowID string, expected int64, event EventType, kind string, input NormativeArtifact, actor string) (Workflow, error) {
	if input.SchemaVersion != 1 {
		return Workflow{}, fmt.Errorf("%w: schema_version must be 1", ErrInvalidNormativeArtifact)
	}
	if err := validateNormativeEntries(input.Entries); err != nil {
		return Workflow{}, err
	}
	return s.recordArtifact(ctx, workflowID, expected, event, kind, input.Content, actor, input.Entries)
}

func (s *Service) recordArtifact(ctx context.Context, workflowID string, expected int64, event EventType, kind, content, actor string, entries []NormativeEntry) (Workflow, error) {
	if strings.TrimSpace(content) == "" {
		return Workflow{}, fmt.Errorf("artifact content is required")
	}
	if len(entries) > 0 && !slices.Contains([]string{"exploration", "specification", "design"}, kind) {
		return Workflow{}, fmt.Errorf("%w: unsupported phase %q", ErrInvalidNormativeArtifact, kind)
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
	now := s.now()
	at, revision := ids.FormatTime(now), expected+1
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
	artifactResult, err := tx.ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,?,?,?,?,?)`, workflowID, kind, content, actor, revision, at)
	if err != nil {
		return Workflow{}, err
	}
	artifactID, err := artifactResult.LastInsertId()
	if err != nil {
		return Workflow{}, err
	}
	for _, entry := range entries {
		body := entry.Body
		if len(body) == 0 {
			body = json.RawMessage(`null`)
		}
		var parent any
		if entry.ParentID != "" {
			parent = entry.ParentID
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO normative_entries(workflow_id,artifact_id,phase,entry_kind,stable_id,parent_id,operation,body_json) VALUES(?,?,?,?,?,?,?,?)`, workflowID, artifactID, kind, entry.Kind, entry.ID, parent, entry.Operation, string(body)); err != nil {
			return Workflow{}, err
		}
	}
	if entries != nil {
		if _, err = resolveNormative(ctx, tx, workflowID); err != nil {
			return Workflow{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(workflow_id,from_state,to_state,actor,reason,revision_after,at) VALUES(?,?,?,?,?,?,?)`, workflowID, current.State, next, actor, "", revision, at); err != nil {
		return Workflow{}, err
	}
	action := map[EventType]activity.Action{Explore: activity.ExplorationRecorded, Specify: activity.SpecificationRecorded, Design: activity.DesignRecorded}[event]
	if err = activity.AppendTx(ctx, tx, activity.New(workflowID, "", action, actor, now, activity.ArtifactSubject(artifactID))); err != nil {
		return Workflow{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workflow{}, err
	}
	current.State, current.Revision, current.UpdatedAt = next, revision, at
	return current, nil
}

func validateNormativeEntries(entries []NormativeEntry) error {
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if !slices.Contains([]string{"requirement", "scenario", "section"}, entry.Kind) {
			return fmt.Errorf("%w: unsupported entry kind %q", ErrInvalidNormativeArtifact, entry.Kind)
		}
		if !normativeIDPattern.MatchString(entry.ID) || (entry.Kind == "requirement" && !strings.HasPrefix(entry.ID, "REQ-")) || (entry.Kind == "scenario" && !strings.HasPrefix(entry.ID, "SCN-")) {
			return fmt.Errorf("%w: invalid %s id %q", ErrInvalidNormativeArtifact, entry.Kind, entry.ID)
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			return fmt.Errorf("%w: duplicate id %s", ErrInvalidNormativeArtifact, entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if !slices.Contains([]string{"add", "replace", "remove"}, entry.Operation) {
			return fmt.Errorf("%w: unsupported operation %q", ErrInvalidNormativeArtifact, entry.Operation)
		}
		if entry.Kind == "scenario" && entry.Operation == "add" && entry.ParentID == "" {
			return fmt.Errorf("%w: scenario %s requires parent_id", ErrInvalidNormativeArtifact, entry.ID)
		}
		if entry.ParentID != "" && !normativeIDPattern.MatchString(entry.ParentID) {
			return fmt.Errorf("%w: invalid parent_id %q", ErrInvalidNormativeArtifact, entry.ParentID)
		}
		if entry.Operation != "remove" && (len(entry.Body) == 0 || !json.Valid(entry.Body)) {
			return fmt.Errorf("%w: %s requires valid JSON body", ErrInvalidNormativeArtifact, entry.ID)
		}
		if len(entry.Body) > 0 && !json.Valid(entry.Body) {
			return fmt.Errorf("%w: invalid JSON body for %s", ErrInvalidNormativeArtifact, entry.ID)
		}
	}
	return nil
}

// ResolveNormative returns the bounded effective normative set. It reads only
// normalized entries and pinned artifact identities; opaque legacy artifact
// bodies are deliberately never parsed or upgraded.
func (s *Service) ResolveNormative(ctx context.Context, workflowID string) (ResolvedNormative, error) {
	return resolveNormative(ctx, s.db, workflowID)
}

// ResolveNormativeInTransaction exposes the canonical baseline-plus-delta
// resolver to transactional domain collaborators without duplicating lineage
// semantics or weakening the caller's snapshot.
func ResolveNormativeInTransaction(ctx context.Context, tx *sql.Tx, workflowID string) (ResolvedNormative, error) {
	return resolveNormative(ctx, tx, workflowID)
}

type normativeQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type normativeResolution struct {
	entries    map[string]ResolvedNormativeEntry
	structured bool
}

func resolveNormative(ctx context.Context, q normativeQuerier, workflowID string) (ResolvedNormative, error) {
	if _, err := workflowFrom(ctx, q, workflowID); err != nil {
		return ResolvedNormative{}, err
	}
	resolved, baseline, err := resolveNormativeAt(ctx, q, workflowID, 0, map[string]bool{})
	if err != nil {
		return ResolvedNormative{}, err
	}
	entries := make([]ResolvedNormativeEntry, 0, len(resolved.entries))
	for _, entry := range resolved.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		phaseRank := map[string]int{"exploration": 0, "specification": 1, "design": 2}
		if phaseRank[entries[i].Phase] != phaseRank[entries[j].Phase] {
			return phaseRank[entries[i].Phase] < phaseRank[entries[j].Phase]
		}
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].ID < entries[j].ID
	})
	return ResolvedNormative{WorkflowID: workflowID, Structured: resolved.structured, Baseline: baseline, Entries: entries}, nil
}

func resolveNormativeAt(ctx context.Context, q normativeQuerier, workflowID string, depth int, stack map[string]bool) (normativeResolution, *BaselineIdentity, error) {
	if stack[workflowID] {
		return normativeResolution{}, nil, ErrLineageCycle
	}
	stack[workflowID] = true
	defer delete(stack, workflowID)

	result := normativeResolution{entries: map[string]ResolvedNormativeEntry{}}
	var predecessorID, manifestJSON string
	var predecessorRevision int64
	err := q.QueryRowContext(ctx, `SELECT predecessor_id,predecessor_revision,artifact_manifest_json FROM workflow_baselines WHERE child_id=?`, workflowID).Scan(&predecessorID, &predecessorRevision, &manifestJSON)
	var baseline *BaselineIdentity
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return normativeResolution{}, nil, err
	}
	if err == nil {
		if depth >= 32 {
			return normativeResolution{}, nil, ErrLineageDepth
		}
		predecessor, getErr := workflowFrom(ctx, q, predecessorID)
		if getErr != nil || (predecessor.State != Completed && predecessor.State != Abandoned) || predecessor.Revision != predecessorRevision {
			return normativeResolution{}, nil, fmt.Errorf("%w: predecessor identity is mutable or mismatched", ErrInvalidBaseline)
		}
		manifest, decodeErr := decodeArtifactManifest(manifestJSON)
		if decodeErr != nil {
			return normativeResolution{}, nil, decodeErr
		}
		currentManifest, manifestErr := artifactManifest(ctx, q, predecessorID)
		if manifestErr != nil {
			return normativeResolution{}, nil, manifestErr
		}
		if !slices.Equal(manifest, currentManifest) {
			return normativeResolution{}, nil, fmt.Errorf("%w: predecessor artifact manifest mismatch", ErrInvalidBaseline)
		}
		parent, _, parentErr := resolveNormativeAt(ctx, q, predecessorID, depth+1, stack)
		if parentErr != nil {
			return normativeResolution{}, nil, parentErr
		}
		result = parent
		baseline = &BaselineIdentity{WorkflowID: predecessorID, Revision: predecessorRevision, Artifacts: manifest}
	}

	rows, err := q.QueryContext(ctx, `SELECT n.phase,n.entry_kind,n.stable_id,coalesce(n.parent_id,''),n.operation,n.body_json,a.id,a.kind,a.accepted_revision
FROM normative_entries n JOIN artifacts a ON a.workflow_id=n.workflow_id AND a.id=n.artifact_id
WHERE n.workflow_id=? ORDER BY a.accepted_revision,a.id,n.entry_kind,n.stable_id`, workflowID)
	if err != nil {
		return normativeResolution{}, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry ResolvedNormativeEntry
		var body, artifactKind string
		if err = rows.Scan(&entry.Phase, &entry.Kind, &entry.ID, &entry.ParentID, &entry.Operation, &body, &entry.Source.ArtifactID, &artifactKind, &entry.Source.Revision); err != nil {
			return normativeResolution{}, nil, err
		}
		entry.Source.WorkflowID = workflowID
		entry.Body = json.RawMessage(body)
		if entry.Phase != artifactKind || !json.Valid(entry.Body) {
			return normativeResolution{}, nil, fmt.Errorf("%w: entry %s has mismatched phase or body", ErrInvalidNormativeArtifact, entry.ID)
		}
		if err = validateNormativeEntries([]NormativeEntry{{Kind: entry.Kind, ID: entry.ID, ParentID: entry.ParentID, Operation: entry.Operation, Body: entry.Body}}); err != nil {
			return normativeResolution{}, nil, err
		}
		current, exists := result.entries[entry.ID]
		switch entry.Operation {
		case "add":
			if exists {
				return normativeResolution{}, nil, fmt.Errorf("%w: duplicate id %s", ErrInvalidNormativeArtifact, entry.ID)
			}
		case "replace":
			if !exists {
				return normativeResolution{}, nil, fmt.Errorf("%w: unknown replace id %s", ErrInvalidNormativeArtifact, entry.ID)
			}
			if current.Kind != entry.Kind {
				return normativeResolution{}, nil, fmt.Errorf("%w: replacement changes kind for %s", ErrInvalidNormativeArtifact, entry.ID)
			}
			if entry.ParentID == "" {
				entry.ParentID = current.ParentID
			}
		case "remove":
			if !exists {
				return normativeResolution{}, nil, fmt.Errorf("%w: unknown remove id %s", ErrInvalidNormativeArtifact, entry.ID)
			}
			if current.Kind != entry.Kind {
				return normativeResolution{}, nil, fmt.Errorf("%w: removal changes kind for %s", ErrInvalidNormativeArtifact, entry.ID)
			}
			delete(result.entries, entry.ID)
			result.structured = true
			continue
		}
		result.entries[entry.ID] = entry
		result.structured = true
	}
	if err = rows.Err(); err != nil {
		return normativeResolution{}, nil, err
	}
	for _, entry := range result.entries {
		if entry.Kind != "scenario" {
			continue
		}
		parent, ok := result.entries[entry.ParentID]
		if !ok || parent.Kind != "requirement" {
			return normativeResolution{}, nil, fmt.Errorf("%w: scenario %s has unknown requirement parent %s", ErrInvalidNormativeArtifact, entry.ID, entry.ParentID)
		}
	}
	return result, baseline, nil
}

func workflowFrom(ctx context.Context, q normativeQuerier, id string) (Workflow, error) {
	var workflow Workflow
	var name sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id,revision,state,name,goal,created_at,updated_at FROM workflows WHERE id=?`, id).Scan(&workflow.ID, &workflow.Revision, &workflow.State, &name, &workflow.Goal, &workflow.CreatedAt, &workflow.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Workflow{}, ErrNotFound
	}
	if err == nil {
		workflow.Name, workflow.NameDerived = DisplayName(name, workflow.Goal)
	}
	return workflow, err
}

func artifactManifest(ctx context.Context, q normativeQuerier, workflowID string) ([]ArtifactIdentity, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,kind,accepted_revision FROM artifacts WHERE workflow_id=? AND kind IN ('exploration','specification','design') ORDER BY accepted_revision,id`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	manifest := []ArtifactIdentity{}
	for rows.Next() {
		var identity ArtifactIdentity
		if err = rows.Scan(&identity.ID, &identity.Kind, &identity.Revision); err != nil {
			return nil, err
		}
		manifest = append(manifest, identity)
	}
	return manifest, rows.Err()
}

func decodeArtifactManifest(content string) ([]ArtifactIdentity, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	var manifest []ArtifactIdentity
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%w: malformed artifact manifest", ErrInvalidBaseline)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: malformed artifact manifest", ErrInvalidBaseline)
	}
	for i, identity := range manifest {
		if identity.ID < 1 || identity.Revision < 1 || !slices.Contains([]string{"exploration", "specification", "design"}, identity.Kind) {
			return nil, fmt.Errorf("%w: malformed artifact identity", ErrInvalidBaseline)
		}
		if i > 0 && (manifest[i-1].Revision > identity.Revision || (manifest[i-1].Revision == identity.Revision && manifest[i-1].ID >= identity.ID)) {
			return nil, fmt.Errorf("%w: unordered or duplicate artifact identity", ErrInvalidBaseline)
		}
	}
	return manifest, nil
}

// AppendOperational records an observed fact without authorizing a lifecycle transition.
func (s *Service) AppendOperational(ctx context.Context, workflowID string, expected int64, kind, content, actor string, action activity.Action) (Artifact, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Artifact{}, err
	}
	defer tx.Rollback()
	current, err := workflowInTx(ctx, tx, workflowID)
	if err != nil {
		return Artifact{}, err
	}
	if current.Revision != expected {
		return Artifact{}, store.ErrCASMismatch
	}
	if !slices.Contains(nonTerminalStates, current.State) {
		return Artifact{}, &TransitionError{Current: current.State, Expected: nonTerminalStates, Event: "operational_report"}
	}
	now := s.now()
	at := ids.FormatTime(now)
	result, err := tx.ExecContext(ctx, `INSERT INTO artifacts(workflow_id,kind,content,actor,accepted_revision,recorded_at) VALUES(?,?,?,?,?,?)`, workflowID, kind, content, actor, expected, at)
	if err != nil {
		return Artifact{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Artifact{}, err
	}
	if err = activity.AppendTx(ctx, tx, activity.New(workflowID, "", action, actor, now, activity.ArtifactSubject(id))); err != nil {
		return Artifact{}, err
	}
	if err = tx.Commit(); err != nil {
		return Artifact{}, err
	}
	return Artifact{Kind: kind, Content: content, Actor: actor, Revision: expected, RecordedAt: at}, nil
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
