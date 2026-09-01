package consolidate

import (
	"context"
	"database/sql"
	"errors"
	"github.com/fmazzalomo/pitcrew/internal/project"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"sort"
	"strings"
)

var ErrConflict = errors.New("divergent workflow graph requires one complete source choice")

type Service struct{}
type sourceGraph struct {
	candidateID string
	graph       Graph
}
type snapshot struct {
	db     *sql.DB
	tx     *sql.Tx
	graphs []sourceGraph
}

func (Service) Consolidate(ctx context.Context, destination *sql.DB, resolved project.Project, manifest Manifest) error {
	discovery, err := project.DiscoverLegacy(resolved)
	if err != nil || manifest.Validate(resolved.ID, discovery) != nil {
		return ErrInvalidManifest
	}
	snapshots, err := openSnapshots(ctx, discovery.Candidates)
	if err != nil {
		return err
	}
	// End SQLite's own clean-WAL sidecar lifecycle before comparing source
	// fingerprints. The complete graphs are already resident in snapshots.
	closeSnapshots(snapshots)
	current, err := project.DiscoverLegacy(resolved)
	if err != nil || current.CandidateSetID != discovery.CandidateSetID {
		return ErrInvalidManifest
	}
	selected, legacyDivergent, err := selectGraphs(snapshots, manifest)
	if err != nil {
		return err
	}
	tx, err := destination.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS consolidation_acknowledgements(project_id TEXT NOT NULL,candidate_set_id TEXT NOT NULL,PRIMARY KEY(project_id,candidate_set_id))`); err != nil {
		return err
	}
	ids := make([]string, 0, len(selected))
	retained := map[string]bool{}
	for _, workflowID := range manifest.RetainExisting {
		retained[workflowID] = true
	}
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, workflowID := range ids {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM workflows WHERE id=?`, workflowID).Scan(&count); err != nil {
			return err
		}
		if count == 1 {
			existing, err := LoadGraph(ctx, tx, workflowID)
			if err != nil {
				return ErrConflict
			}
			if retained[workflowID] {
				if existing.Hash == selected[workflowID].Hash && !legacyDivergent[workflowID] {
					return ErrConflict
				}
				delete(retained, workflowID)
				continue
			}
			if existing.Hash != selected[workflowID].Hash {
				return ErrConflict
			}
			continue
		}
		if retained[workflowID] {
			return ErrConflict
		}
		if err := insertGraph(ctx, tx, selected[workflowID]); err != nil {
			return err
		}
	}
	if len(retained) != 0 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO consolidation_acknowledgements(project_id,candidate_set_id) VALUES(?,?)`, resolved.ID, discovery.CandidateSetID); err != nil {
		return err
	}
	return tx.Commit()
}
func openSnapshots(ctx context.Context, candidates []project.LegacyCandidate) ([]snapshot, error) {
	var result []snapshot
	for _, candidate := range candidates {
		uri := url.URL{Scheme: "file", Path: candidate.StatePath}
		query := uri.Query()
		query.Set("mode", "ro")
		if _, err := os.Lstat(candidate.StatePath + "-wal"); errors.Is(err, os.ErrNotExist) {
			// A clean closed WAL database is fully checkpointed. Immutable mode
			// prevents SQLite from creating its own empty WAL sidecar.
			query.Set("immutable", "1")
		} else if err != nil {
			closeSnapshots(result)
			return nil, err
		}
		uri.RawQuery = query.Encode()
		db, err := sql.Open("sqlite", uri.String())
		if err != nil {
			closeSnapshots(result)
			return nil, err
		}
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			_ = db.Close()
			closeSnapshots(result)
			return nil, err
		}
		result = append(result, snapshot{db: db, tx: tx})
		item := &result[len(result)-1]
		rows, err := tx.QueryContext(ctx, `SELECT id FROM workflows ORDER BY id`)
		if err == nil {
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err == nil {
					var graph Graph
					graph, err = LoadGraph(ctx, tx, id)
					if err == nil {
						item.graphs = append(item.graphs, sourceGraph{candidate.ID, graph})
					}
				}
				if err != nil {
					break
				}
			}
			if closeErr := rows.Close(); err == nil {
				err = closeErr
			}
		}
		if err != nil {
			closeSnapshots(result)
			return nil, err
		}
	}
	return result, nil
}
func closeSnapshots(items []snapshot) {
	for _, item := range items {
		_ = item.tx.Rollback()
		_ = item.db.Close()
	}
}
func selectGraphs(snapshots []snapshot, manifest Manifest) (map[string]Graph, map[string]bool, error) {
	byWorkflow := map[string][]sourceGraph{}
	for _, item := range snapshots {
		for _, graph := range item.graphs {
			byWorkflow[graph.graph.WorkflowID] = append(byWorkflow[graph.graph.WorkflowID], graph)
		}
	}
	choices := map[string]string{}
	for _, choice := range manifest.Choices {
		choices[choice.WorkflowID] = choice.CandidateID
	}
	retained := map[string]bool{}
	for _, workflowID := range manifest.RetainExisting {
		retained[workflowID] = true
	}
	selected := map[string]Graph{}
	divergent := map[string]bool{}
	for workflowID, copies := range byWorkflow {
		sort.Slice(copies, func(i, j int) bool { return copies[i].candidateID < copies[j].candidateID })
		hashes := map[string]bool{}
		for _, copy := range copies {
			hashes[copy.graph.Hash] = true
		}
		chosen := copies[0]
		if len(hashes) > 1 {
			divergent[workflowID] = true
			if retained[workflowID] {
				selected[workflowID] = chosen.graph
				continue
			}
			candidateID, ok := choices[workflowID]
			if !ok {
				return nil, nil, ErrConflict
			}
			found := false
			for _, copy := range copies {
				if copy.candidateID == candidateID {
					chosen, found = copy, true
				}
			}
			if !found {
				return nil, nil, ErrConflict
			}
			delete(choices, workflowID)
		}
		selected[workflowID] = chosen.graph
	}
	if len(choices) != 0 {
		return nil, nil, ErrConflict
	}
	return selected, divergent, nil
}
func insertGraph(ctx context.Context, tx *sql.Tx, graph Graph) error {
	var artifactID, activityID int64
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(id),0)+1 FROM artifacts`).Scan(&artifactID); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(id),0)+1 FROM activities`).Scan(&activityID); err != nil {
		return err
	}
	graph, err := graph.RemapSurrogates(artifactID, activityID)
	if err != nil {
		return err
	}
	for _, table := range graph.Tables {
		if len(table.Rows) == 0 {
			continue
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(table.Columns)), ",")
		statement := "INSERT INTO " + table.Name + "(" + strings.Join(table.Columns, ",") + ") VALUES(" + placeholders + ")"
		for _, row := range table.Rows {
			if _, err := tx.ExecContext(ctx, statement, []any(row)...); err != nil {
				return err
			}
		}
	}
	return nil
}
