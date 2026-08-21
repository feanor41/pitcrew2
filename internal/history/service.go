package history

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/fmazzalomo/pitcrew/internal/store"
)

const ContextRunes = 120

type Workflow struct {
	ID        string
	Revision  int64
	State     string
	Goal      string
	CreatedAt string
	UpdatedAt string
}

type Record struct {
	ID         string
	WorkflowID string
	Kind       string
	UnitID     string
	Revision   int64
	Title      string
	Content    string
	At         string
}

type Detail struct {
	Workflow Workflow
	Records  []Record
}

type SearchResult struct {
	WorkflowID string
	RecordID   string
	Kind       string
	UnitID     string
	Revision   int64
	Context    string
	At         string
}

type Resolution struct {
	Detail Detail
	Record Record
}

type Service struct{ db *sql.DB }

func New(s *store.Store) *Service { return &Service{db: s.DB()} }

func (s *Service) List(ctx context.Context) ([]Workflow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, revision, state, goal, created_at, updated_at
FROM workflows ORDER BY updated_at DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workflows []Workflow
	for rows.Next() {
		var workflow Workflow
		if err := rows.Scan(&workflow.ID, &workflow.Revision, &workflow.State, &workflow.Goal, &workflow.CreatedAt, &workflow.UpdatedAt); err != nil {
			return nil, err
		}
		workflows = append(workflows, workflow)
	}
	return workflows, rows.Err()
}

func (s *Service) Detail(ctx context.Context, workflowID string) (Detail, error) {
	var detail Detail
	err := s.db.QueryRowContext(ctx, `SELECT id, revision, state, goal, created_at, updated_at
FROM workflows WHERE id=?`, workflowID).Scan(&detail.Workflow.ID, &detail.Workflow.Revision,
		&detail.Workflow.State, &detail.Workflow.Goal, &detail.Workflow.CreatedAt, &detail.Workflow.UpdatedAt)
	if err != nil {
		return Detail{}, err
	}
	rows, err := s.db.QueryContext(ctx, detailQuery, workflowID, workflowID, workflowID, workflowID, workflowID, workflowID)
	if err != nil {
		return Detail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		record := Record{WorkflowID: workflowID}
		if err := rows.Scan(&record.ID, &record.Kind, &record.UnitID, &record.Revision, &record.Title, &record.Content, &record.At); err != nil {
			return Detail{}, err
		}
		detail.Records = append(detail.Records, record)
	}
	return detail, rows.Err()
}

func (s *Service) Search(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	workflows, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	for _, workflow := range workflows {
		if context, ok := matchContext(workflow.Goal, query); ok {
			results = append(results, SearchResult{WorkflowID: workflow.ID, RecordID: "goal", Kind: "goal", Context: context, At: workflow.UpdatedAt})
		}
		detail, err := s.Detail(ctx, workflow.ID)
		if err != nil {
			return nil, err
		}
		for _, record := range detail.Records {
			text := strings.TrimSpace(record.Title + " " + record.Content)
			if context, ok := matchContext(text, query); ok {
				results = append(results, SearchResult{WorkflowID: workflow.ID, RecordID: record.ID, Kind: record.Kind,
					UnitID: record.UnitID, Revision: record.Revision, Context: context, At: record.At})
			}
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if a.At != b.At {
			return a.At > b.At
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.WorkflowID != b.WorkflowID {
			return a.WorkflowID < b.WorkflowID
		}
		if a.UnitID != b.UnitID {
			return a.UnitID < b.UnitID
		}
		if a.Revision != b.Revision {
			return a.Revision < b.Revision
		}
		return a.RecordID < b.RecordID
	})
	return results, nil
}

func (s *Service) Resolve(ctx context.Context, result SearchResult) (Resolution, error) {
	detail, err := s.Detail(ctx, result.WorkflowID)
	if err != nil {
		return Resolution{}, err
	}
	if result.RecordID == "goal" {
		return Resolution{Detail: detail, Record: Record{ID: "goal", WorkflowID: result.WorkflowID,
			Kind: "goal", Title: "Goal", Content: detail.Workflow.Goal, At: detail.Workflow.UpdatedAt}}, nil
	}
	for _, record := range detail.Records {
		if record.ID == result.RecordID {
			return Resolution{Detail: detail, Record: record}, nil
		}
	}
	return Resolution{}, sql.ErrNoRows
}

func matchContext(text, query string) (string, bool) {
	runes := []rune(text)
	visible := []rune(strings.ToLower(text))
	needle := []rune(strings.ToLower(query))
	index := -1
	for i := 0; i+len(needle) <= len(visible); i++ {
		if string(visible[i:i+len(needle)]) == string(needle) {
			index = i
			break
		}
	}
	if index < 0 {
		return "", false
	}
	start := index - ContextRunes/2
	if start < 0 {
		start = 0
	}
	end := start + ContextRunes
	if end > len(runes) {
		end = len(runes)
		start = max(0, end-ContextRunes)
	}
	return string(runes[start:end]), true
}

const detailQuery = `SELECT record_id, kind, unit_id, revision, title, content, at FROM (
SELECT 'event:' || revision_after record_id, 'event' kind, '' unit_id, revision_after revision, from_state || ' -> ' || to_state title,
 actor || ' ' || reason content, at FROM events WHERE workflow_id=?
UNION ALL SELECT 'artifact:' || id, kind, '', accepted_revision, kind, actor || ' ' || content, recorded_at
 FROM artifacts WHERE workflow_id=?
UNION ALL SELECT 'plan', 'plan', '', 0, summary, scope || ' ' || max_parallel_units || ' ' || body,
 (SELECT updated_at FROM workflows WHERE id=plans.workflow_id) FROM plans WHERE workflow_id=?
UNION ALL SELECT 'work_unit:' || id, 'work_unit', id, revision, description,
 scope || ' ' || areas || ' ' || depends_on || ' ' || state || ' ' ||
 COALESCE(admission_exception,'') || ' approved=' || admission_exception_approved,
 (SELECT updated_at FROM workflows WHERE id=work_units.workflow_id) FROM work_units WHERE workflow_id=?
UNION ALL SELECT 'evidence:' || unit_id || ':' || revision, 'evidence', unit_id, revision, red_command,
 actor || ' ' || red_outcome || ' ' || green_command || ' ' || green_outcome || ' ' ||
 refactor_summary || ' ' || validation_command || ' ' || validation_outcome || ' ' || changed_paths, recorded_at
 FROM evidence WHERE workflow_id=?
UNION ALL SELECT 'review:' || unit_id || ':' || revision, 'review', unit_id, revision, verdict,
 actor || ' ' || summary || ' ' || findings || ' ' || plan_impact, recorded_at
 FROM reviews WHERE workflow_id=?
) ORDER BY at ASC, kind ASC, unit_id ASC, revision ASC, record_id ASC`
