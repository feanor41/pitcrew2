package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

var (
	ErrNotFound            = errors.New("delivery not found")
	ErrIdempotencyConflict = errors.New("delivery operation key conflicts with existing input")
	ErrInvalidTransition   = errors.New("invalid delivery status transition")
	ErrTerminal            = errors.New("terminal delivery is immutable")
	ErrNoChange            = errors.New("delivery update has no change")
)

// RevisionConflict preserves the shared CAS exit classification while naming
// the direct delivery and the one read-only command needed before deciding.
type RevisionConflict struct {
	DeliveryID string
	Expected   int64
	Current    int64
}

func (e *RevisionConflict) InspectCommand() string {
	return "pitcrew delivery show --delivery-id " + e.DeliveryID
}
func (e *RevisionConflict) Error() string {
	current := "changed"
	if e.Current > 0 {
		current = fmt.Sprintf("%d", e.Current)
	}
	return fmt.Sprintf("delivery revision mismatch for %s: expected %d, current %s; inspect with: %s", e.DeliveryID, e.Expected, current, e.InspectCommand())
}
func (e *RevisionConflict) Unwrap() error { return store.ErrCASMismatch }

type Route string
type Status string

const (
	DirectInline    Route  = "direct_inline"
	DelegatedDirect Route  = "delegated_direct"
	InProgress      Status = "in_progress"
	Blocked         Status = "blocked"
	Interrupted     Status = "interrupted"
	Completed       Status = "completed"
	Cancelled       Status = "cancelled"
	Failed          Status = "failed"
)

type Trace struct {
	ID           string `json:"id"`
	OperationKey string `json:"operation_key"`
	Route        Route  `json:"route"`
	Goal         string `json:"goal"`
	RouteReason  string `json:"route_reason"`
	Status       Status `json:"status"`
	Summary      string `json:"summary"`
	NextAction   string `json:"next_action"`
	Revision     int64  `json:"revision"`
	CreatorActor string `json:"creator_actor"`
	UpdaterActor string `json:"updater_actor"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	FinishedAt   string `json:"finished_at,omitempty"`
}

type StartInput struct {
	OperationKey string `json:"operation_key"`
	Route        Route  `json:"route"`
	Goal         string `json:"goal"`
	RouteReason  string `json:"route_reason"`
}

type UpdateInput struct {
	Status     Status `json:"status"`
	Summary    string `json:"summary"`
	NextAction string `json:"next_action"`
}

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewService(s *store.Store, now func() time.Time) *Service { return &Service{db: s.DB(), now: now} }

func (s *Service) Start(ctx context.Context, actor string, in StartInput) (Trace, error) {
	actor, in.OperationKey, in.Goal, in.RouteReason = strings.TrimSpace(actor), strings.TrimSpace(in.OperationKey), strings.TrimSpace(in.Goal), strings.TrimSpace(in.RouteReason)
	if err := validateText("actor", actor, 128, true); err != nil {
		return Trace{}, err
	}
	if err := validateText("operation key", in.OperationKey, 128, true); err != nil {
		return Trace{}, err
	}
	if in.Route != DirectInline && in.Route != DelegatedDirect {
		return Trace{}, fmt.Errorf("route must be direct_inline or delegated_direct")
	}
	if err := validateText("goal", in.Goal, 4000, true); err != nil {
		return Trace{}, err
	}
	if err := validateText("route reason", in.RouteReason, 500, false); err != nil {
		return Trace{}, err
	}
	id, err := ids.NewDelivery()
	if err != nil {
		return Trace{}, err
	}
	at := ids.FormatTime(s.now())
	trace := Trace{ID: id, OperationKey: in.OperationKey, Route: in.Route, Goal: in.Goal, RouteReason: in.RouteReason, Status: InProgress, Revision: 1, CreatorActor: actor, UpdaterActor: actor, CreatedAt: at, UpdatedAt: at}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Trace{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO direct_delivery_traces(id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,NULL) ON CONFLICT(operation_key) DO NOTHING`, trace.ID, trace.OperationKey, trace.Route, trace.Goal, trace.RouteReason, trace.Status, "", "", trace.Revision, actor, actor, at, at)
	if err != nil {
		return Trace{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Trace{}, err
	}
	if changed == 0 {
		existing, err := get(ctx, tx, "operation_key", in.OperationKey)
		if err != nil {
			return Trace{}, err
		}
		if existing.Route != in.Route || existing.Goal != in.Goal || existing.RouteReason != in.RouteReason || existing.CreatorActor != actor {
			return Trace{}, ErrIdempotencyConflict
		}
		return existing, nil
	}
	if err = tx.Commit(); err != nil {
		return Trace{}, err
	}
	return trace, nil
}

func (s *Service) Update(ctx context.Context, id string, expected int64, actor string, in UpdateInput) (Trace, error) {
	actor, in.Summary, in.NextAction = strings.TrimSpace(actor), strings.TrimSpace(in.Summary), strings.TrimSpace(in.NextAction)
	if err := validateText("actor", actor, 128, true); err != nil {
		return Trace{}, err
	}
	if !validStatus(in.Status) {
		return Trace{}, fmt.Errorf("unsupported delivery status %q", in.Status)
	}
	if err := validateText("summary", in.Summary, 500, false); err != nil {
		return Trace{}, err
	}
	if err := validateText("next action", in.NextAction, 200, false); err != nil {
		return Trace{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Trace{}, err
	}
	defer tx.Rollback()
	current, err := get(ctx, tx, "id", id)
	if err != nil {
		return Trace{}, err
	}
	if current.Revision != expected {
		return Trace{}, &RevisionConflict{DeliveryID: id, Expected: expected, Current: current.Revision}
	}
	if terminal(current.Status) {
		return Trace{}, ErrTerminal
	}
	if !validTransition(current.Status, in.Status) {
		return Trace{}, fmt.Errorf("%w: %s to %s", ErrInvalidTransition, current.Status, in.Status)
	}
	if current.Status == in.Status && current.Summary == in.Summary && current.NextAction == in.NextAction {
		return Trace{}, ErrNoChange
	}
	at := ids.FormatTime(s.now())
	finished := any(nil)
	if terminal(in.Status) {
		finished = at
	}
	result, err := tx.ExecContext(ctx, `UPDATE direct_delivery_traces SET status=?,summary=?,next_action=?,revision=revision+1,updater_actor=?,updated_at=?,finished_at=? WHERE id=? AND revision=? AND status=?`, in.Status, in.Summary, in.NextAction, actor, at, finished, id, expected, current.Status)
	if err != nil {
		return Trace{}, err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return Trace{}, err
	} else if changed != 1 {
		return Trace{}, &RevisionConflict{DeliveryID: id, Expected: expected}
	}
	updated, err := get(ctx, tx, "id", id)
	if err != nil {
		return Trace{}, err
	}
	if err = tx.Commit(); err != nil {
		return Trace{}, err
	}
	return updated, nil
}

func (s *Service) Get(ctx context.Context, id string) (Trace, error) { return get(ctx, s.db, "id", id) }

func (s *Service) List(ctx context.Context) ([]Trace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at FROM direct_delivery_traces ORDER BY updated_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var traces []Trace
	for rows.Next() {
		trace, err := scan(rows)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
	}
	return traces, rows.Err()
}

type rowScanner interface{ Scan(...any) error }
type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func get(ctx context.Context, db queryRower, field, value string) (Trace, error) {
	if field != "id" && field != "operation_key" {
		return Trace{}, errors.New("unsupported delivery lookup")
	}
	trace, err := scan(db.QueryRowContext(ctx, `SELECT id,operation_key,route,goal,route_reason,status,summary,next_action,revision,creator_actor,updater_actor,created_at,updated_at,finished_at FROM direct_delivery_traces WHERE `+field+`=?`, value))
	if errors.Is(err, sql.ErrNoRows) {
		return Trace{}, ErrNotFound
	}
	return trace, err
}

func scan(row rowScanner) (Trace, error) {
	var trace Trace
	var finished sql.NullString
	err := row.Scan(&trace.ID, &trace.OperationKey, &trace.Route, &trace.Goal, &trace.RouteReason, &trace.Status, &trace.Summary, &trace.NextAction, &trace.Revision, &trace.CreatorActor, &trace.UpdaterActor, &trace.CreatedAt, &trace.UpdatedAt, &finished)
	if finished.Valid {
		trace.FinishedAt = finished.String
	}
	return trace, err
}

func validateText(field, value string, max int, required bool) error {
	count := utf8.RuneCountInString(value)
	if required && count == 0 {
		return fmt.Errorf("%s is required", field)
	}
	if count > max {
		return fmt.Errorf("%s exceeds %d runes", field, max)
	}
	return nil
}

func validStatus(status Status) bool {
	return status == InProgress || status == Blocked || status == Interrupted || terminal(status)
}
func terminal(status Status) bool {
	return status == Completed || status == Cancelled || status == Failed
}
func validTransition(from, to Status) bool {
	return (from == InProgress || from == Blocked || from == Interrupted) && validStatus(to)
}
