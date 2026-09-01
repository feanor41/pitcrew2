package roadmap_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/roadmap"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestCapturePersistsOnlyMinimalLocallyAuthoritativeCandidate(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	now := time.Date(2026, 9, 1, 15, 4, 5, 678900000, time.FixedZone("local", -3*60*60))
	service := roadmap.NewService(s, func() time.Time { return now })

	item, err := service.Capture(ctx, roadmap.CaptureInput{
		Title:      "Durable finding",
		Body:       "Preserve the finding before publication.",
		Provenance: json.RawMessage("{\n  \"kind\": \"conversation\", \"source\": 168\n}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^rm-[0-9a-f]{24}$`).MatchString(item.ID) {
		t.Fatalf("roadmap id = %q", item.ID)
	}
	if item.Title != "Durable finding" || item.Body != "Preserve the finding before publication." || string(item.Provenance) != `{"kind":"conversation","source":168}` {
		t.Fatalf("captured content = %#v", item)
	}
	if item.CreatedAt != "2026-09-01T18:04:05.678Z" || item.LocalLifecycle != roadmap.Captured || item.BindingState != roadmap.Unbound || item.Authority != roadmap.Local || item.Binding != nil {
		t.Fatalf("captured state = %#v", item)
	}

	var stored []byte
	if err := s.DB().QueryRowContext(ctx, `SELECT provenance_json FROM roadmap_items WHERE id=?`, item.ID).Scan(&stored); err != nil || string(stored) != string(item.Provenance) {
		t.Fatalf("stored provenance = %q, %v", stored, err)
	}
}

func TestCaptureRejectsInvalidCandidateWithoutMutation(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	service := roadmap.NewService(s, time.Now)
	for name, input := range map[string]roadmap.CaptureInput{
		"blank title":        {Title: " \t\n", Body: "body", Provenance: json.RawMessage(`{"source":"test"}`)},
		"blank body":         {Title: "title", Body: " \t\n", Provenance: json.RawMessage(`{"source":"test"}`)},
		"missing provenance": {Title: "title", Body: "body"},
		"null provenance":    {Title: "title", Body: "body", Provenance: json.RawMessage(`null`)},
		"array provenance":   {Title: "title", Body: "body", Provenance: json.RawMessage(`[]`)},
		"trailing JSON":      {Title: "title", Body: "body", Provenance: json.RawMessage(`{} {}`)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Capture(ctx, input); err == nil {
				t.Fatal("Capture accepted invalid input")
			}
		})
	}
	var count int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM roadmap_items`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("roadmap item count = %d, %v", count, err)
	}
}

func TestShowAndListAreDeterministicAndDeriveAuthorityFromBinding(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	service := roadmap.NewService(s, func() time.Time { return now })

	empty, err := service.List(ctx)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty list = %#v, %v", empty, err)
	}
	first := mustCapture(t, service, ctx, "first")
	second := mustCapture(t, service, ctx, "second")
	now = now.Add(time.Minute)
	latest := mustCapture(t, service, ctx, "latest")
	if _, err := s.DB().ExecContext(ctx, `UPDATE roadmap_items SET local_lifecycle='acknowledged' WHERE id=?`, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO roadmap_bindings(roadmap_id,provider,namespace,external_id,url,prepared_digest,acknowledged_at) VALUES(?,?,?,?,?,?,?)`, second.ID, "github", "feanor41/pitcrew2", "168", "https://github.com/feanor41/pitcrew2/issues/168", "sha256:digest", "2026-09-01T12:01:00.000Z"); err != nil {
		t.Fatal(err)
	}

	bound, err := service.Show(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bound.BindingState != roadmap.Bound || bound.Authority != roadmap.External || bound.LocalLifecycle != roadmap.Acknowledged || bound.Binding == nil || bound.Binding.ExternalID != "168" {
		t.Fatalf("bound view = %#v", bound)
	}
	unbound, err := service.Show(ctx, first.ID)
	if err != nil || unbound.BindingState != roadmap.Unbound || unbound.Authority != roadmap.Local || unbound.Binding != nil {
		t.Fatalf("unbound view = %#v, %v", unbound, err)
	}

	wantIDs := []string{latest.ID}
	if first.ID < second.ID {
		wantIDs = append(wantIDs, first.ID, second.ID)
	} else {
		wantIDs = append(wantIDs, second.ID, first.ID)
	}
	for range 2 {
		got, err := service.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		gotIDs := make([]string, len(got))
		for index := range got {
			gotIDs[index] = got[index].ID
		}
		if !reflect.DeepEqual(gotIDs, wantIDs) {
			t.Fatalf("list ids = %v, want %v", gotIDs, wantIDs)
		}
		for _, summary := range got {
			if summary.ID == second.ID && (summary.BindingState != roadmap.Bound || summary.Authority != roadmap.External) {
				t.Fatalf("bound summary = %#v", summary)
			}
		}
	}
	if _, err := service.Show(ctx, "rm-ffffffffffffffffffffffff"); !errors.Is(err, roadmap.ErrNotFound) {
		t.Fatalf("unknown roadmap error = %v", err)
	}
}

func mustCapture(t *testing.T, service *roadmap.Service, ctx context.Context, title string) roadmap.Item {
	t.Helper()
	item, err := service.Capture(ctx, roadmap.CaptureInput{Title: title, Body: title + " body", Provenance: json.RawMessage(`{"source":"test"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
