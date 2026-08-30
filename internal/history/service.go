package history

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/store"
	workflowdomain "github.com/fmazzalomo/pitcrew/internal/workflow"
)

const ContextRunes = 120

type Workflow struct {
	ID          string `json:"id"`
	Revision    int64  `json:"revision"`
	State       string `json:"state"`
	Name        string `json:"name"`
	NameDerived bool   `json:"name_derived"`
	Goal        string `json:"goal"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

const FullWorkflow = "full_workflow"

// Delivery is the shared read projection for one physical direct trace or one
// existing workflow graph. Workflow-only APIs remain available below.
type Delivery struct {
	ID          string `json:"id"`
	Revision    int64  `json:"revision"`
	Route       string `json:"route"`
	Status      string `json:"status"`
	Name        string `json:"name"`
	NameDerived bool   `json:"name_derived"`
	Goal        string `json:"goal"`
	RouteReason string `json:"route_reason,omitempty"`
	Summary     string `json:"summary,omitempty"`
	NextAction  string `json:"next_action,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	FinishedAt  string `json:"finished_at,omitempty"`
}

type DeliveryDetail struct {
	Delivery Delivery `json:"delivery"`
	Workflow *Detail  `json:"workflow,omitempty"`
}

type DeliverySearchResult struct {
	DeliveryID string `json:"delivery_id"`
	Route      string `json:"route"`
	Status     string `json:"status"`
	Context    string `json:"context"`
	At         string `json:"at"`
}

type Record struct {
	ID         string `json:"id"`
	WorkflowID string `json:"workflow_id"`
	Kind       string `json:"kind"`
	UnitID     string `json:"unit_id"`
	Revision   int64  `json:"revision"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	Actor      string `json:"actor"`
	At         string `json:"at"`
}

type Activity struct {
	ID          string `json:"id"`
	WorkflowID  string `json:"workflow_id"`
	UnitID      string `json:"unit_id"`
	Action      string `json:"action"`
	Actor       string `json:"actor"`
	At          string `json:"at"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	RecordID    string `json:"record_id"`
	Legacy      bool   `json:"legacy"`
}
type Detail struct {
	Workflow    Workflow     `json:"workflow"`
	Synopsis    Synopsis     `json:"synopsis"`
	Occurrences []Occurrence `json:"occurrences"`
	Records     []Record     `json:"records"`
	Timeline    []Activity   `json:"timeline"`
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

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(s *store.Store, clocks ...func() time.Time) *Service {
	now := time.Now
	if len(clocks) != 0 {
		now = clocks[0]
	}
	return &Service{db: s.DB(), now: now}
}

func (s *Service) ListDeliveries(ctx context.Context) ([]Delivery, error) {
	workflows, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	deliveries := make([]Delivery, 0, len(workflows))
	for _, workflow := range workflows {
		detail, err := s.Detail(ctx, workflow.ID)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, projectWorkflowDelivery(detail))
	}
	hasDirect, err := s.hasTable(ctx, "direct_delivery_traces")
	if err != nil || !hasDirect {
		return deliveries, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,revision,route,status,goal,route_reason,summary,next_action,created_at,updated_at,COALESCE(finished_at,'') FROM direct_delivery_traces`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Delivery
		if err := rows.Scan(&item.ID, &item.Revision, &item.Route, &item.Status, &item.Goal, &item.RouteReason, &item.Summary, &item.NextAction, &item.CreatedAt, &item.UpdatedAt, &item.FinishedAt); err != nil {
			return nil, err
		}
		item.Name, item.NameDerived = workflowdomain.DisplayName(sql.NullString{}, item.Goal)
		deliveries = append(deliveries, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(deliveries, func(i, j int) bool {
		if deliveries[i].CreatedAt != deliveries[j].CreatedAt {
			return deliveries[i].CreatedAt > deliveries[j].CreatedAt
		}
		return deliveries[i].ID < deliveries[j].ID
	})
	return deliveries, nil
}

func (s *Service) GetDelivery(ctx context.Context, id string) (DeliveryDetail, error) {
	if strings.HasPrefix(id, "wf-") {
		detail, err := s.Detail(ctx, id)
		if err != nil {
			return DeliveryDetail{}, err
		}
		return DeliveryDetail{Delivery: projectWorkflowDelivery(detail), Workflow: &detail}, nil
	}
	if !strings.HasPrefix(id, "dl-") {
		return DeliveryDetail{}, fmt.Errorf("delivery id must start with dl- or wf-")
	}
	hasDirect, err := s.hasTable(ctx, "direct_delivery_traces")
	if err != nil {
		return DeliveryDetail{}, err
	}
	if !hasDirect {
		return DeliveryDetail{}, sql.ErrNoRows
	}
	var item Delivery
	err = s.db.QueryRowContext(ctx, `SELECT id,revision,route,status,goal,route_reason,summary,next_action,created_at,updated_at,COALESCE(finished_at,'') FROM direct_delivery_traces WHERE id=?`, id).
		Scan(&item.ID, &item.Revision, &item.Route, &item.Status, &item.Goal, &item.RouteReason, &item.Summary, &item.NextAction, &item.CreatedAt, &item.UpdatedAt, &item.FinishedAt)
	if err != nil {
		return DeliveryDetail{}, err
	}
	item.Name, item.NameDerived = workflowdomain.DisplayName(sql.NullString{}, item.Goal)
	return DeliveryDetail{Delivery: item}, nil
}

func (s *Service) SearchDeliveries(ctx context.Context, query string) ([]DeliverySearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	deliveries, err := s.ListDeliveries(ctx)
	if err != nil {
		return nil, err
	}
	var results []DeliverySearchResult
	for _, item := range deliveries {
		text := strings.Join([]string{item.ID, item.Goal, item.Route, item.Status, item.RouteReason, item.Summary, item.NextAction}, " ")
		context, matched := matchContext(text, query)
		if !matched && item.Route == FullWorkflow {
			detail, detailErr := s.Detail(ctx, item.ID)
			if detailErr != nil {
				return nil, detailErr
			}
			for _, record := range detail.Records {
				if context, matched = matchContext(strings.TrimSpace(record.Title+" "+record.Content), query); matched {
					break
				}
			}
		}
		if matched {
			results = append(results, DeliverySearchResult{DeliveryID: item.ID, Route: item.Route, Status: item.Status, Context: context, At: item.UpdatedAt})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].At != results[j].At {
			return results[i].At > results[j].At
		}
		return results[i].DeliveryID < results[j].DeliveryID
	})
	return results, nil
}

func projectWorkflowDelivery(detail Detail) Delivery {
	workflow := detail.Workflow
	status, summary, nextAction := workflow.State, "", detail.Synopsis.NextAction
	if detail.Synopsis.Progress != nil {
		summary, nextAction = detail.Synopsis.Progress.Summary, detail.Synopsis.Progress.NextAction
		if detail.Synopsis.Progress.Status == "blocked" {
			status = "blocked"
		}
	}
	correction := detail.Synopsis.CorrectionPolicy
	if correction != nil && correction.BlockerRevision != 0 && workflow.State != string(workflowdomain.Completed) && workflow.State != string(workflowdomain.Abandoned) {
		status, summary = "blocked", correction.BlockerContent
		if detail.Synopsis.Blocker != nil && detail.Synopsis.Blocker.Status == "Correction" {
			summary = detail.Synopsis.Blocker.Reason
		}
	}
	return Delivery{ID: workflow.ID, Revision: workflow.Revision, Route: FullWorkflow, Status: status,
		Name: workflow.Name, NameDerived: workflow.NameDerived, Goal: workflow.Goal, Summary: summary,
		NextAction: nextAction, CreatedAt: workflow.CreatedAt, UpdatedAt: workflow.UpdatedAt}
}

func (s *Service) List(ctx context.Context) ([]Workflow, error) {
	nameExpr, err := s.workflowNameExpr(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT id, revision, state, %s, goal, created_at, updated_at
FROM workflows ORDER BY created_at DESC, id ASC`, nameExpr))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workflows []Workflow
	for rows.Next() {
		workflow, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, workflow)
	}
	return workflows, rows.Err()
}

func (s *Service) Detail(ctx context.Context, workflowID string) (Detail, error) {
	var detail Detail
	nameExpr, err := s.workflowNameExpr(ctx)
	if err != nil {
		return Detail{}, err
	}
	detail.Workflow, err = scanWorkflow(s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT id, revision, state, %s, goal, created_at, updated_at
FROM workflows WHERE id=?`, nameExpr), workflowID))
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
		if err := rows.Scan(&record.ID, &record.Kind, &record.UnitID, &record.Revision, &record.Title, &record.Content, &record.Actor, &record.At); err != nil {
			return Detail{}, err
		}
		detail.Records = append(detail.Records, record)
	}
	if err := rows.Err(); err != nil {
		return Detail{}, err
	}
	detail.Timeline, err = s.timeline(ctx, workflowID, detail.Records)
	if err == nil {
		err = s.project(ctx, &detail)
	}
	return detail, err
}

func (s *Service) ResolveActivity(ctx context.Context, entry Activity) (Resolution, error) {
	detail, err := s.Detail(ctx, entry.WorkflowID)
	if err != nil {
		return Resolution{}, err
	}
	recordID, ok := subjectRecordID(entry.WorkflowID, entry.SubjectKind, entry.SubjectID)
	if !ok {
		return Resolution{}, sql.ErrNoRows
	}
	if entry.SubjectKind == "workflow" {
		w := detail.Workflow
		return Resolution{Detail: detail, Record: Record{ID: recordID, WorkflowID: w.ID, Kind: "workflow", Title: w.Name, Content: w.Goal, At: w.CreatedAt}}, nil
	}
	for _, record := range detail.Records {
		if record.ID == recordID {
			return Resolution{Detail: detail, Record: record}, nil
		}
	}
	return Resolution{}, sql.ErrNoRows
}

func (s *Service) ResolveOccurrence(ctx context.Context, workflowID, occurrenceID, recordID string) (Resolution, error) {
	detail, err := s.Detail(ctx, workflowID)
	if err != nil {
		return Resolution{}, err
	}
	for _, occurrence := range detail.Occurrences {
		if occurrence.ID != occurrenceID {
			continue
		}
		allowed := occurrence.RecordID == recordID
		for _, related := range occurrence.RelatedRecordIDs {
			allowed = allowed || related == recordID
		}
		if !allowed {
			return Resolution{}, sql.ErrNoRows
		}
		if recordID == "workflow:"+workflowID {
			w := detail.Workflow
			return Resolution{Detail: detail, Record: Record{ID: recordID, WorkflowID: w.ID, Kind: "workflow", Title: w.Name, Content: w.Goal, At: w.CreatedAt}}, nil
		}
		for _, record := range detail.Records {
			if record.ID == recordID {
				return Resolution{Detail: detail, Record: record}, nil
			}
		}
	}
	return Resolution{}, sql.ErrNoRows
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

func (s *Service) timeline(ctx context.Context, workflowID string, records []Record) ([]Activity, error) {
	var activityTables int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='activities'`).Scan(&activityTables)
	if err != nil {
		return nil, err
	}
	hasActivities := activityTables != 0
	var entries []Activity
	coveredRecords := map[string]bool{}
	if hasActivities {
		rows, err := s.db.QueryContext(ctx, `SELECT id,COALESCE(unit_id,''),action,actor,at,subject_kind,subject_id
FROM activities WHERE workflow_id=? ORDER BY at,id`, workflowID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			entry := Activity{WorkflowID: workflowID}
			if err := rows.Scan(&id, &entry.UnitID, &entry.Action, &entry.Actor, &entry.At, &entry.SubjectKind, &entry.SubjectID); err != nil {
				rows.Close()
				return nil, err
			}
			entry.ID = fmt.Sprintf("activity:%d", id)
			entry.RecordID, _ = subjectRecordID(workflowID, entry.SubjectKind, entry.SubjectID)
			coveredRecords[entry.RecordID] = true
			entries = append(entries, entry)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	for _, record := range records {
		if record.Actor == "" || record.At == "" || coveredRecords[record.ID] {
			continue
		}
		kind, subject, ok := recordSubject(record)
		if !ok {
			continue
		}
		entries = append(entries, Activity{ID: "legacy:" + record.ID, WorkflowID: workflowID, UnitID: record.UnitID,
			Action: record.Kind, Actor: record.Actor, At: record.At, SubjectKind: kind, SubjectID: subject, RecordID: record.ID, Legacy: true})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].At != entries[j].At {
			return entries[i].At < entries[j].At
		}
		if entries[i].Legacy != entries[j].Legacy {
			return !entries[i].Legacy
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func (s *Service) workflowNameExpr(ctx context.Context) (string, error) {
	hasName, err := s.hasColumn(ctx, "workflows", "name")
	if hasName {
		return "name", err
	}
	return "NULL", err
}

type scanner interface{ Scan(...any) error }

func scanWorkflow(row scanner) (Workflow, error) {
	var value Workflow
	var name sql.NullString
	err := row.Scan(&value.ID, &value.Revision, &value.State, &name, &value.Goal, &value.CreatedAt, &value.UpdatedAt)
	value.Name, value.NameDerived = workflowdomain.DisplayName(name, value.Goal)
	return value, err
}

func (s *Service) hasColumn(ctx context.Context, table, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Service) hasTable(ctx context.Context, table string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count)
	return count != 0, err
}

func subjectRecordID(workflowID, kind, subject string) (string, bool) {
	switch kind {
	case "workflow":
		return "workflow:" + subject, subject == workflowID
	case "plan":
		return "plan", subject == workflowID
	case "artifact":
		return "artifact:" + subject, subject != ""
	case "work_unit":
		return "work_unit:" + subject, subject != ""
	case "event":
		prefix := workflowID + "@"
		return "event:" + strings.TrimPrefix(subject, prefix), strings.HasPrefix(subject, prefix) && len(subject) > len(prefix)
	case "evidence", "review":
		parts := strings.Split(subject, "@")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return kind + ":" + parts[0] + ":" + parts[1], true
		}
	}
	return "", false
}

func recordSubject(record Record) (string, string, bool) {
	switch record.Kind {
	case "event":
		return "event", record.WorkflowID + "@" + fmt.Sprint(record.Revision), true
	case "evidence", "review":
		return record.Kind, record.UnitID + "@" + fmt.Sprint(record.Revision), true
	case "plan":
		return "plan", record.WorkflowID, true
	case "work_unit":
		return "work_unit", record.UnitID, true
	default:
		if strings.HasPrefix(record.ID, "artifact:") {
			return "artifact", strings.TrimPrefix(record.ID, "artifact:"), true
		}
	}
	return "", "", false
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

const detailQuery = `SELECT record_id, kind, unit_id, revision, title, content, actor, at FROM (
SELECT 'event:' || revision_after record_id, 'event' kind, '' unit_id, revision_after revision, from_state || ' -> ' || to_state title,
 reason content, actor, at FROM events WHERE workflow_id=?
UNION ALL SELECT 'artifact:' || id, kind, '', accepted_revision, kind,
 CASE
  WHEN kind='progress' AND json_valid(content) THEN
   '# Progress report' || char(10) || char(10) ||
   '**Status:** ' || COALESCE(json_extract(content, '$.status'), 'unavailable') || char(10) || char(10) ||
   '**Summary:** ' || COALESCE(json_extract(content, '$.summary'), 'unavailable') || char(10) || char(10) ||
   '**Next action:** ' || COALESCE(json_extract(content, '$.next_action'), 'unavailable')
  WHEN kind='aggregate_review' AND json_valid(content) THEN
   '# Aggregate review' || char(10) || char(10) ||
   '**Verdict:** ' || COALESCE(json_extract(content, '$.verdict'), 'unavailable') || char(10) || char(10) ||
   '**Summary:** ' || COALESCE(json_extract(content, '$.summary'), 'unavailable') || char(10) || char(10) ||
   '**Findings:** ' || COALESCE(json_extract(content, '$.findings'), 'unavailable')
  ELSE content
 END, actor, recorded_at
 FROM artifacts WHERE workflow_id=?
UNION ALL SELECT 'plan', 'plan', '', 0, summary,
 '# Accepted plan' || char(10) || char(10) ||
 '**Summary:** ' || summary || char(10) || char(10) ||
 '**Scope:** ' || scope || char(10) || char(10) ||
 '**Max parallel units:** ' || max_parallel_units || char(10) || char(10) ||
 '## Definition' || char(10) || char(10) || char(96) || char(96) || char(96) || 'json' || char(10) || body || char(10) || char(96) || char(96) || char(96), '',
 (SELECT updated_at FROM workflows WHERE id=plans.workflow_id) FROM plans WHERE workflow_id=?
UNION ALL SELECT 'work_unit:' || id, 'work_unit', id, revision, description,
 '# Work unit' || char(10) || char(10) ||
 '**Description:** ' || description || char(10) || char(10) ||
 '**Scope:** ' || scope || char(10) || char(10) ||
 '**Areas:** ' || areas || char(10) || char(10) ||
 '**Dependencies:** ' || depends_on || char(10) || char(10) ||
 '**Estimated changed lines:** ' || estimated_changed_lines || char(10) || char(10) ||
 '**Estimated review minutes:** ' || estimated_review_minutes || char(10) || char(10) ||
 '**State:** ' || state || char(10) || char(10) ||
 '**Admission exception:** ' || COALESCE(admission_exception, 'none') || char(10) || char(10) ||
 '**Exception approved:** ' || CASE admission_exception_approved WHEN 1 THEN 'yes' ELSE 'no' END, '',
 (SELECT updated_at FROM workflows WHERE id=work_units.workflow_id) FROM work_units WHERE workflow_id=?
UNION ALL SELECT 'evidence:' || unit_id || ':' || revision, 'evidence', unit_id, revision, 'TDD evidence',
 '# TDD evidence' || char(10) || char(10) ||
 '## Red' || char(10) || char(10) || '**Command:** ' || red_command || char(10) || char(10) || '**Outcome:** ' || red_outcome || char(10) || char(10) ||
 '## Green' || char(10) || char(10) || '**Command:** ' || green_command || char(10) || char(10) || '**Outcome:** ' || green_outcome || char(10) || char(10) ||
 '## Refactor' || char(10) || char(10) || refactor_summary || char(10) || char(10) ||
 '## Validation' || char(10) || char(10) || '**Command:** ' || validation_command || char(10) || char(10) || '**Outcome:** ' || validation_outcome || char(10) || char(10) ||
 '**Changed paths:** ' || changed_paths, actor, recorded_at
 FROM evidence WHERE workflow_id=?
UNION ALL SELECT 'review:' || unit_id || ':' || revision, 'review', unit_id, revision, verdict,
 '# Unit review' || char(10) || char(10) ||
 '**Verdict:** ' || verdict || char(10) || char(10) ||
 '**Summary:** ' || summary || char(10) || char(10) ||
 '**Findings:** ' || findings || char(10) || char(10) ||
 '**Plan impact:** ' || plan_impact, actor, recorded_at
 FROM reviews WHERE workflow_id=?
) ORDER BY at ASC, kind ASC, unit_id ASC, revision ASC, record_id ASC`
