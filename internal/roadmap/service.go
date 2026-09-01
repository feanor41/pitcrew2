// Package roadmap persists project-local candidates before an explicit
// authority handoff to an external tracker.
package roadmap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/ids"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

var ErrNotFound = errors.New("roadmap item not found")

type LocalLifecycle string
type BindingState string
type Authority string

const (
	Captured     LocalLifecycle = "captured"
	Acknowledged LocalLifecycle = "acknowledged"
	Unbound      BindingState   = "unbound"
	Bound        BindingState   = "bound"
	Local        Authority      = "local"
	External     Authority      = "external"
)

type Binding struct {
	Provider       string `json:"provider"`
	Namespace      string `json:"namespace"`
	ExternalID     string `json:"external_id"`
	URL            string `json:"url"`
	PreparedDigest string `json:"prepared_digest"`
	AcknowledgedAt string `json:"acknowledged_at"`
}

type Item struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Body           string          `json:"body"`
	Provenance     json.RawMessage `json:"provenance"`
	CreatedAt      string          `json:"created_at"`
	LocalLifecycle LocalLifecycle  `json:"local_lifecycle"`
	BindingState   BindingState    `json:"binding_state"`
	Authority      Authority       `json:"authority"`
	Binding        *Binding        `json:"binding,omitempty"`
}

type Summary struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	CreatedAt      string         `json:"created_at"`
	LocalLifecycle LocalLifecycle `json:"local_lifecycle"`
	BindingState   BindingState   `json:"binding_state"`
	Authority      Authority      `json:"authority"`
}

type CaptureInput struct {
	Title      string          `json:"title"`
	Body       string          `json:"body"`
	Provenance json.RawMessage `json:"provenance"`
}

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewService(s *store.Store, now func() time.Time) *Service {
	return &Service{db: s.DB(), now: now}
}

func (s *Service) Capture(ctx context.Context, input CaptureInput) (Item, error) {
	if strings.TrimSpace(input.Title) == "" {
		return Item{}, errors.New("title is required")
	}
	if strings.TrimSpace(input.Body) == "" {
		return Item{}, errors.New("body is required")
	}
	provenance, err := compactObject(input.Provenance)
	if err != nil {
		return Item{}, err
	}
	id, err := ids.NewRoadmap()
	if err != nil {
		return Item{}, err
	}
	createdAt := ids.FormatTime(s.now())
	if _, err := s.db.ExecContext(ctx, `INSERT INTO roadmap_items(id,title,body,provenance_json,created_at,local_lifecycle) VALUES(?,?,?,?,?,'captured')`, id, input.Title, input.Body, string(provenance), createdAt); err != nil {
		return Item{}, fmt.Errorf("capture roadmap item: %w", err)
	}
	return Item{
		ID:             id,
		Title:          input.Title,
		Body:           input.Body,
		Provenance:     provenance,
		CreatedAt:      createdAt,
		LocalLifecycle: Captured,
		BindingState:   Unbound,
		Authority:      Local,
	}, nil
}

func (s *Service) Show(ctx context.Context, id string) (Item, error) {
	item, err := scanItem(s.db.QueryRowContext(ctx, itemQuery+` WHERE i.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return item, err
}

func (s *Service) List(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT i.id,i.title,i.created_at,i.local_lifecycle,b.roadmap_id
FROM roadmap_items i
LEFT JOIN roadmap_bindings b ON b.roadmap_id=i.id
ORDER BY i.created_at DESC,i.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Summary, 0)
	for rows.Next() {
		var item Summary
		var bound sql.NullString
		if err := rows.Scan(&item.ID, &item.Title, &item.CreatedAt, &item.LocalLifecycle, &bound); err != nil {
			return nil, err
		}
		item.BindingState, item.Authority = derivedAuthority(bound.Valid)
		items = append(items, item)
	}
	return items, rows.Err()
}

func compactObject(raw json.RawMessage) (json.RawMessage, error) {
	var decoded any
	if len(raw) == 0 || json.Unmarshal(raw, &decoded) != nil {
		return nil, errors.New("provenance must be one JSON object")
	}
	if _, ok := decoded.(map[string]any); !ok {
		return nil, errors.New("provenance must be one JSON object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, errors.New("provenance must be one JSON object")
	}
	return json.RawMessage(compact.Bytes()), nil
}

const itemQuery = `
SELECT i.id,i.title,i.body,i.provenance_json,i.created_at,i.local_lifecycle,
       b.provider,b.namespace,b.external_id,b.url,b.prepared_digest,b.acknowledged_at
FROM roadmap_items i
LEFT JOIN roadmap_bindings b ON b.roadmap_id=i.id`

type rowScanner interface{ Scan(...any) error }

func scanItem(row rowScanner) (Item, error) {
	var item Item
	var provenance []byte
	var provider, namespace, externalID, url, digest, acknowledgedAt sql.NullString
	if err := row.Scan(&item.ID, &item.Title, &item.Body, &provenance, &item.CreatedAt, &item.LocalLifecycle, &provider, &namespace, &externalID, &url, &digest, &acknowledgedAt); err != nil {
		return Item{}, err
	}
	item.Provenance = json.RawMessage(provenance)
	item.BindingState, item.Authority = derivedAuthority(provider.Valid)
	if provider.Valid {
		item.Binding = &Binding{
			Provider:       provider.String,
			Namespace:      namespace.String,
			ExternalID:     externalID.String,
			URL:            url.String,
			PreparedDigest: digest.String,
			AcknowledgedAt: acknowledgedAt.String,
		}
	}
	return item, nil
}

func derivedAuthority(bound bool) (BindingState, Authority) {
	if bound {
		return Bound, External
	}
	return Unbound, Local
}
