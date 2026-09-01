package roadmap_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/roadmap"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestAcknowledgeAtomicallyRecordsBindingAndExternalAuthority(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	now := time.Date(2026, 9, 1, 18, 30, 0, 123000000, time.UTC)
	service := roadmap.NewService(s, func() time.Time { return now })
	item := mustCapture(t, service, ctx, "publish me")
	prepared := mustPrepare(t, service, ctx, item.ID, "feanor41/pitcrew2")

	acknowledged, err := service.Acknowledge(ctx, item.ID, validAcknowledgement(prepared, "168"))
	if err != nil {
		t.Fatal(err)
	}
	if acknowledged.LocalLifecycle != roadmap.Acknowledged || acknowledged.BindingState != roadmap.Bound || acknowledged.Authority != roadmap.External || acknowledged.Binding == nil {
		t.Fatalf("acknowledged item = %#v", acknowledged)
	}
	want := &roadmap.Binding{Provider: "github", Namespace: "feanor41/pitcrew2", ExternalID: "168", URL: "https://github.com/feanor41/pitcrew2/issues/168", PreparedDigest: prepared.Digest, AcknowledgedAt: "2026-09-01T18:30:00.123Z"}
	if !reflect.DeepEqual(acknowledged.Binding, want) {
		t.Fatalf("binding = %#v, want %#v", acknowledged.Binding, want)
	}
	shown, err := service.Show(ctx, item.ID)
	if err != nil || !reflect.DeepEqual(shown, acknowledged) {
		t.Fatalf("shown item = %#v, %v", shown, err)
	}
}

func TestAcknowledgeExactReplayIsWriteFreeAndIdempotent(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	now := time.Date(2026, 9, 1, 18, 30, 0, 0, time.UTC)
	service := roadmap.NewService(s, func() time.Time { return now })
	item := mustCapture(t, service, ctx, "replay")
	prepared := mustPrepare(t, service, ctx, item.ID, "feanor41/pitcrew2")
	input := validAcknowledgement(prepared, "169")
	first, err := service.Acknowledge(ctx, item.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(24 * time.Hour)
	var changesBefore int64
	if err := s.DB().QueryRowContext(ctx, `SELECT total_changes()`).Scan(&changesBefore); err != nil {
		t.Fatal(err)
	}
	second, err := service.Acknowledge(ctx, item.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	var changesAfter int64
	if err := s.DB().QueryRowContext(ctx, `SELECT total_changes()`).Scan(&changesAfter); err != nil {
		t.Fatal(err)
	}
	var bindings int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM roadmap_bindings WHERE roadmap_id=?`, item.ID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, first) || changesAfter != changesBefore || bindings != 1 {
		t.Fatalf("replay changed state: first=%#v second=%#v changes=%d/%d bindings=%d", first, second, changesBefore, changesAfter, bindings)
	}
}

func TestAcknowledgeStaleDigestFailsCASWithoutMutation(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	service := roadmap.NewService(s, time.Now)
	item := mustCapture(t, service, ctx, "stale")
	prepared := mustPrepare(t, service, ctx, item.ID, "feanor41/pitcrew2")
	input := validAcknowledgement(prepared, "170")
	input.PreparedDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	if _, err := service.Acknowledge(ctx, item.ID, input); !errors.Is(err, store.ErrCASMismatch) {
		t.Fatalf("stale digest error = %v", err)
	}
	assertLocallyAuthoritative(t, service, s, ctx, item.ID)
}

func TestAcknowledgeRejectsInvalidTupleWithoutMutation(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	service := roadmap.NewService(s, time.Now)
	item := mustCapture(t, service, ctx, "validate acknowledgement")
	prepared := mustPrepare(t, service, ctx, item.ID, "feanor41/pitcrew2")
	valid := validAcknowledgement(prepared, "171")
	inputs := map[string]roadmap.AcknowledgeInput{
		"missing provider":  withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.Provider = "" }),
		"foreign provider":  withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.Provider = "jira" }),
		"bad namespace":     withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.Namespace = "Feanor41/pitcrew2" }),
		"missing external":  withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.ExternalID = "" }),
		"zero external":     withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.ExternalID = "0" }),
		"padded external":   withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.ExternalID = "0171" }),
		"nondecimal":        withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.ExternalID = "issue-171" }),
		"alternate host":    withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.URL = "https://example.test/feanor41/pitcrew2/issues/171" }),
		"query URL":         withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.URL += "?view=1" }),
		"fragment URL":      withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.URL += "#issue" }),
		"malformed digest":  withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.PreparedDigest = "digest" }),
		"incomplete digest": withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.PreparedDigest = "sha256:abc" }),
	}
	for name, input := range inputs {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Acknowledge(ctx, item.ID, input); err == nil {
				t.Fatal("Acknowledge accepted invalid input")
			}
			assertLocallyAuthoritative(t, service, s, ctx, item.ID)
		})
	}
}

func TestAcknowledgeConflictsPreserveOriginalBinding(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	service := roadmap.NewService(s, time.Now)
	item := mustCapture(t, service, ctx, "conflict")
	prepared := mustPrepare(t, service, ctx, item.ID, "feanor41/pitcrew2")
	original, err := service.Acknowledge(ctx, item.ID, validAcknowledgement(prepared, "172"))
	if err != nil {
		t.Fatal(err)
	}
	valid := validAcknowledgement(prepared, "172")
	conflicts := map[string]roadmap.AcknowledgeInput{
		"provider": withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.Provider = "jira" }),
		"namespace": withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) {
			in.Namespace = "other/project"
			in.URL = "https://github.com/other/project/issues/172"
		}),
		"external id": withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) {
			in.ExternalID = "173"
			in.URL = "https://github.com/feanor41/pitcrew2/issues/173"
		}),
		"url": withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) { in.URL += "?different=1" }),
		"digest": withAcknowledgement(valid, func(in *roadmap.AcknowledgeInput) {
			in.PreparedDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		}),
	}
	for name, input := range conflicts {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Acknowledge(ctx, item.ID, input); !errors.Is(err, roadmap.ErrBindingConflict) {
				t.Fatalf("conflict error = %v", err)
			}
			shown, err := service.Show(ctx, item.ID)
			if err != nil || !reflect.DeepEqual(shown, original) {
				t.Fatalf("binding changed: %#v, %v", shown, err)
			}
		})
	}
}

func TestAcknowledgeUniquenessAndInjectedFailureRollBackBothRows(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	service := roadmap.NewService(s, time.Now)
	first := mustCapture(t, service, ctx, "first binding")
	firstPrepared := mustPrepare(t, service, ctx, first.ID, "feanor41/pitcrew2")
	if _, err := service.Acknowledge(ctx, first.ID, validAcknowledgement(firstPrepared, "174")); err != nil {
		t.Fatal(err)
	}
	second := mustCapture(t, service, ctx, "unique collision")
	secondPrepared := mustPrepare(t, service, ctx, second.ID, "feanor41/pitcrew2")
	if _, err := service.Acknowledge(ctx, second.ID, validAcknowledgement(secondPrepared, "174")); !errors.Is(err, roadmap.ErrBindingConflict) {
		t.Fatalf("unique collision error = %v", err)
	}
	assertLocallyAuthoritative(t, service, s, ctx, second.ID)

	third := mustCapture(t, service, ctx, "injected failure")
	thirdPrepared := mustPrepare(t, service, ctx, third.ID, "feanor41/pitcrew2")
	if _, err := s.DB().ExecContext(ctx, `CREATE TRIGGER fail_roadmap_lifecycle BEFORE UPDATE OF local_lifecycle ON roadmap_items WHEN OLD.id='`+third.ID+`' BEGIN SELECT RAISE(ABORT, 'injected lifecycle failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Acknowledge(ctx, third.ID, validAcknowledgement(thirdPrepared, "175")); err == nil || errors.Is(err, roadmap.ErrBindingConflict) {
		t.Fatalf("injected failure error = %v", err)
	}
	assertLocallyAuthoritative(t, service, s, ctx, third.ID)
}

func mustPrepare(t *testing.T, service *roadmap.Service, ctx context.Context, id, namespace string) roadmap.PreparedPublication {
	t.Helper()
	prepared, err := service.PrepareGitHub(ctx, id, roadmap.PrepareInput{Provider: "github", Namespace: namespace})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func validAcknowledgement(prepared roadmap.PreparedPublication, externalID string) roadmap.AcknowledgeInput {
	return roadmap.AcknowledgeInput{
		Provider:       prepared.Provider,
		Namespace:      prepared.Namespace,
		ExternalID:     externalID,
		URL:            "https://github.com/" + prepared.Namespace + "/issues/" + externalID,
		PreparedDigest: prepared.Digest,
	}
}

func withAcknowledgement(input roadmap.AcknowledgeInput, change func(*roadmap.AcknowledgeInput)) roadmap.AcknowledgeInput {
	change(&input)
	return input
}

func assertLocallyAuthoritative(t *testing.T, service *roadmap.Service, s *store.Store, ctx context.Context, id string) {
	t.Helper()
	item, err := service.Show(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if item.LocalLifecycle != roadmap.Captured || item.BindingState != roadmap.Unbound || item.Authority != roadmap.Local || item.Binding != nil {
		t.Fatalf("roadmap item partially acknowledged: %#v", item)
	}
	var bindings int
	if err := s.DB().QueryRowContext(ctx, `SELECT count(*) FROM roadmap_bindings WHERE roadmap_id=?`, id).Scan(&bindings); err != nil || bindings != 0 {
		t.Fatalf("partial binding count = %d, %v", bindings, err)
	}
}
