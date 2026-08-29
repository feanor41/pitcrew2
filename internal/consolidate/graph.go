// Package consolidate validates and relocates complete workflow graphs.
package consolidate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrIncompleteGraph = errors.New("incomplete workflow graph")

type Row []any
type Table struct {
	Name    string
	Columns []string
	Rows    []Row
}
type Graph struct {
	WorkflowID string
	Tables     []Table
	Hash       string
}
type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type tableSpec struct{ name, columns, filter, order string }

var graphTables = []tableSpec{
	{"workflows", "id,revision,state,goal,created_at,updated_at,name", "id", "id"},
	{"events", "workflow_id,from_state,to_state,actor,reason,revision_after,at", "workflow_id", "revision_after"},
	{"artifacts", "id,workflow_id,kind,content,actor,accepted_revision,recorded_at", "workflow_id", "id"},
	{"plans", "workflow_id,summary,scope,max_parallel_units,body", "workflow_id", "workflow_id"},
	{"work_units", "id,workflow_id,description,scope,areas,depends_on,estimated_changed_lines,estimated_review_minutes,state,admission_exception,admission_exception_approved,revision", "workflow_id", "id"},
	{"evidence", "workflow_id,unit_id,revision,actor,red_command,red_outcome,green_command,green_outcome,refactor_summary,validation_command,validation_outcome,changed_paths,recorded_at", "workflow_id", "unit_id,revision"},
	{"reviews", "workflow_id,unit_id,revision,actor,verdict,summary,findings,plan_impact,recorded_at", "workflow_id", "unit_id,revision"},
	{"handles", "claim_id,workflow_id,unit_id,state,secret_hash,actor_identity,issued_at,expires_at,claim_generation,purpose", "workflow_id", "claim_id"},
	{"activities", "id,workflow_id,unit_id,action,actor,at,subject_kind,subject_id", "workflow_id", "id"},
}

func LoadGraph(ctx context.Context, source Queryer, workflowID string) (Graph, error) {
	graph := Graph{WorkflowID: workflowID}
	for _, spec := range graphTables {
		columns := strings.Split(spec.columns, ",")
		query := "SELECT " + spec.columns + " FROM " + spec.name + " WHERE " + spec.filter + "=? ORDER BY " + spec.order
		rows, err := source.QueryContext(ctx, query, workflowID)
		if err != nil {
			return Graph{}, fmt.Errorf("load %s: %w", spec.name, err)
		}
		table := Table{Name: spec.name, Columns: columns}
		for rows.Next() {
			values, targets := make(Row, len(columns)), make([]any, len(columns))
			for index := range values {
				targets[index] = &values[index]
			}
			if err := rows.Scan(targets...); err != nil {
				_ = rows.Close()
				return Graph{}, err
			}
			for index, value := range values {
				if bytes, ok := value.([]byte); ok {
					values[index] = string(bytes)
				}
			}
			table.Rows = append(table.Rows, values)
		}
		if err := rows.Close(); err != nil {
			return Graph{}, err
		}
		graph.Tables = append(graph.Tables, table)
	}
	if err := graph.validate(); err != nil {
		return Graph{}, err
	}
	return graph.canonical()
}

func (g Graph) Rows(name string) []Row {
	for _, table := range g.Tables {
		if table.Name == name {
			return table.Rows
		}
	}
	return nil
}

func (g Graph) validate() error {
	if len(g.Rows("workflows")) != 1 || len(g.Rows("events")) == 0 || len(g.Rows("plans")) > 1 {
		return ErrIncompleteGraph
	}
	units, artifacts := map[string]bool{}, map[string]bool{}
	for _, row := range g.Rows("work_units") {
		units[text(row[0])] = true
	}
	for _, row := range g.Rows("artifacts") {
		artifacts[text(row[0])] = true
	}
	for _, table := range g.Tables {
		workflowColumn := column(table.Columns, "workflow_id")
		if table.Name == "workflows" {
			workflowColumn = 0
		}
		for _, row := range table.Rows {
			if workflowColumn < 0 || text(row[workflowColumn]) != g.WorkflowID {
				return ErrIncompleteGraph
			}
		}
	}
	for _, tableName := range []string{"evidence", "reviews", "handles"} {
		for _, row := range g.Rows(tableName) {
			if !units[text(row[columnFor(g, tableName, "unit_id")])] {
				return ErrIncompleteGraph
			}
		}
	}
	for _, row := range g.Rows("work_units") {
		var dependencies []string
		if err := json.Unmarshal([]byte(text(row[5])), &dependencies); err != nil {
			return ErrIncompleteGraph
		}
		for _, dependency := range dependencies {
			if !units[dependency] {
				return ErrIncompleteGraph
			}
		}
	}
	events, evidence, reviews := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, row := range g.Rows("events") {
		events[g.WorkflowID+"@"+text(row[5])] = true
	}
	for _, row := range g.Rows("evidence") {
		evidence[text(row[1])+"@"+text(row[2])] = true
	}
	for _, row := range g.Rows("reviews") {
		reviews[text(row[1])+"@"+text(row[2])] = true
	}
	validKinds := map[string]bool{"workflow": true, "event": true, "artifact": true, "plan": true, "work_unit": true, "evidence": true, "review": true}
	for _, row := range g.Rows("activities") {
		unit, kind, subject := text(row[2]), text(row[6]), text(row[7])
		if !validKinds[kind] || row[2] != nil && (!units[unit] || !strings.HasPrefix(subject, unit)) || kind == "artifact" && !artifacts[subject] ||
			kind == "event" && !events[subject] || kind == "work_unit" && !units[subject] ||
			kind == "evidence" && !evidence[subject] || kind == "review" && !reviews[subject] ||
			kind == "plan" && (len(g.Rows("plans")) != 1 || subject != g.WorkflowID) || kind == "workflow" && subject != g.WorkflowID {
			return ErrIncompleteGraph
		}
	}
	return nil
}

func (g Graph) canonical() (Graph, error) {
	result := cloneGraph(g)
	artifacts := result.Rows("artifacts")
	sortRows(artifacts, 1)
	artifactIDs := map[string]string{}
	for index, row := range artifacts {
		artifactIDs[text(row[0])] = fmt.Sprint(index + 1)
		row[0] = int64(index + 1)
	}
	activities := result.Rows("activities")
	for _, row := range activities {
		if text(row[6]) == "artifact" {
			row[7] = artifactIDs[text(row[7])]
		}
	}
	sortRows(activities, 1)
	for index, row := range activities {
		row[0] = int64(index + 1)
	}
	for index := range result.Tables {
		if result.Tables[index].Name != "artifacts" && result.Tables[index].Name != "activities" {
			sortRows(result.Tables[index].Rows, 0)
		}
	}
	content, err := json.Marshal(result.Tables)
	if err != nil {
		return Graph{}, err
	}
	digest := sha256.Sum256(content)
	result.Hash = hex.EncodeToString(digest[:])
	return result, nil
}

func (g Graph) RemapSurrogates(firstArtifactID, firstActivityID int64) (Graph, error) {
	if firstArtifactID < 1 || firstActivityID < 1 {
		return Graph{}, errors.New("surrogate IDs must be positive")
	}
	result := cloneGraph(g)
	mapping := map[string]string{}
	for index, row := range result.Rows("artifacts") {
		mapped := firstArtifactID + int64(index)
		mapping[text(row[0])] = fmt.Sprint(mapped)
		row[0] = mapped
	}
	for index, row := range result.Rows("activities") {
		row[0] = firstActivityID + int64(index)
		if text(row[6]) == "artifact" {
			row[7] = mapping[text(row[7])]
		}
	}
	return result, nil
}

func cloneGraph(g Graph) Graph {
	copy := Graph{WorkflowID: g.WorkflowID, Hash: g.Hash}
	for _, table := range g.Tables {
		cloned := Table{Name: table.Name, Columns: append([]string(nil), table.Columns...)}
		for _, row := range table.Rows {
			cloned.Rows = append(cloned.Rows, append(Row(nil), row...))
		}
		copy.Tables = append(copy.Tables, cloned)
	}
	return copy
}

func sortRows(rows []Row, skip int) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := json.Marshal(rows[i][skip:])
		right, _ := json.Marshal(rows[j][skip:])
		return string(left) < string(right) || string(left) == string(right) && text(rows[i][0]) < text(rows[j][0])
	})
}
func column(columns []string, name string) int {
	for index, candidate := range columns {
		if candidate == name {
			return index
		}
	}
	return -1
}
func columnFor(g Graph, tableName, name string) int {
	for _, table := range g.Tables {
		if table.Name == tableName {
			return column(table.Columns, name)
		}
	}
	return -1
}
func text(value any) string { return fmt.Sprint(value) }
