package roadmap_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/roadmap"
)

func TestPrepareGitHubReturnsExactMarkerAndCanonicalDigestVector(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	const id = "rm-111111111111111111111111"
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO roadmap_items(id,title,body,provenance_json,created_at,local_lifecycle) VALUES(?,?,?,?,?,'captured')`, id, "Durable finding", "Body\r\n\r\n", `{"source":"test"}`, "2026-09-01T12:00:00.000Z"); err != nil {
		t.Fatal(err)
	}
	service := roadmap.NewService(s, time.Now)

	publication, err := service.PrepareGitHub(ctx, id, roadmap.PrepareInput{Provider: "github", Namespace: "feanor41/pitcrew2"})
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- pitcrew-roadmap:v1 id=rm-111111111111111111111111 -->"
	if publication.RoadmapID != id || publication.Provider != "github" || publication.Namespace != "feanor41/pitcrew2" || publication.Title != "Durable finding" || publication.Marker != marker {
		t.Fatalf("publication identity = %#v", publication)
	}
	if publication.Body != "Body\n\n"+marker+"\n" {
		t.Fatalf("publication body = %q", publication.Body)
	}
	if publication.Digest != "sha256:664b153d7c02bbd2fb2a1861f7298cee2d9bb183bd005c3247d92f096aed4610" {
		t.Fatalf("publication digest = %q", publication.Digest)
	}
}

func TestPrepareGitHubIsByteStableAndLeavesStoreUnchanged(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	service := roadmap.NewService(s, func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) })
	item := mustCapture(t, service, ctx, "pure projection")
	before, err := service.Show(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var changesBefore int64
	if err := s.DB().QueryRowContext(ctx, `SELECT total_changes()`).Scan(&changesBefore); err != nil {
		t.Fatal(err)
	}

	first, err := service.PrepareGitHub(ctx, item.ID, roadmap.PrepareInput{Provider: "github", Namespace: "feanor41/pitcrew2"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.PrepareGitHub(ctx, item.ID, roadmap.PrepareInput{Provider: "github", Namespace: "feanor41/pitcrew2"})
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if !reflect.DeepEqual(first, second) || string(firstJSON) != string(secondJSON) {
		t.Fatalf("preparations differ:\n%s\n%s", firstJSON, secondJSON)
	}
	if strings.Contains(string(firstJSON), "timestamp") || strings.Contains(string(firstJSON), "created_at") || strings.Contains(string(firstJSON), "prepared_at") {
		t.Fatalf("publication invented time state: %s", firstJSON)
	}
	after, err := service.Show(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var changesAfter int64
	if err := s.DB().QueryRowContext(ctx, `SELECT total_changes()`).Scan(&changesAfter); err != nil {
		t.Fatal(err)
	}
	var bindings int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM roadmap_bindings`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) || changesAfter != changesBefore || bindings != 0 {
		t.Fatalf("prepare mutated store: before=%#v after=%#v changes=%d/%d bindings=%d", before, after, changesBefore, changesAfter, bindings)
	}
}

func TestPrepareGitHubRejectsInvalidOrBoundProjection(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	service := roadmap.NewService(s, time.Now)
	item := mustCapture(t, service, ctx, "validate")
	for name, input := range map[string]roadmap.PrepareInput{
		"missing provider":    {Namespace: "feanor41/pitcrew2"},
		"foreign provider":    {Provider: "jira", Namespace: "feanor41/pitcrew2"},
		"missing namespace":   {Provider: "github"},
		"padded namespace":    {Provider: "github", Namespace: " feanor41/pitcrew2 "},
		"uppercase namespace": {Provider: "github", Namespace: "Feanor41/pitcrew2"},
		"missing owner":       {Provider: "github", Namespace: "/pitcrew2"},
		"extra segment":       {Provider: "github", Namespace: "feanor41/pitcrew2/issues"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.PrepareGitHub(ctx, item.ID, input); err == nil {
				t.Fatal("PrepareGitHub accepted invalid input")
			}
		})
	}
	if _, err := s.DB().ExecContext(ctx, `UPDATE roadmap_items SET local_lifecycle='acknowledged' WHERE id=?`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().ExecContext(ctx, `INSERT INTO roadmap_bindings(roadmap_id,provider,namespace,external_id,url,prepared_digest,acknowledged_at) VALUES(?,?,?,?,?,?,?)`, item.ID, "github", "feanor41/pitcrew2", "168", "https://github.com/feanor41/pitcrew2/issues/168", "sha256:digest", "now"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PrepareGitHub(ctx, item.ID, roadmap.PrepareInput{Provider: "github", Namespace: "feanor41/pitcrew2"}); !errors.Is(err, roadmap.ErrAlreadyBound) {
		t.Fatalf("bound preparation error = %v", err)
	}
}
