package projectcontext

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestInspectMissingReturnsDeterministicEmptyView(t *testing.T) {
	svc := New(Dependencies{Resolve: func(context.Context) (Resolved, error) {
		return Resolved{CheckoutRoot: "/checkout", Load: func(context.Context) (Record, string, bool, error) {
			return Record{}, "", false, nil
		}}, nil
	}})

	got, err := svc.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != Missing || got.SchemaVersion != 0 || got.CheckoutRoot != "/checkout" || got.UpdatedAt != "" || len(got.Facts) != 0 {
		t.Fatalf("inspection = %#v", got)
	}
	if !reflect.DeepEqual(got.Coverage, map[string]bool{"stack": false, "runtime": false, "deployment": false, "architecture": false, "documentation": false, "sdd": false}) {
		t.Fatalf("coverage = %#v", got.Coverage)
	}
	encoded, _ := json.Marshal(got)
	if string(encoded) != `{"status":"missing","facts":{},"coverage":{"architecture":false,"deployment":false,"documentation":false,"runtime":false,"sdd":false,"stack":false},"missing_or_invalid":["stack","runtime","deployment","architecture","documentation","sdd"],"checkout_root":"/checkout"}` {
		t.Fatalf("missing JSON = %s", encoded)
	}
	if !reflect.DeepEqual(got.MissingOrInvalid, Categories()) {
		t.Fatalf("missing = %v; want %v", got.MissingOrInvalid, Categories())
	}
	for _, category := range Categories() {
		if got.Coverage[category] {
			t.Fatalf("category %q unexpectedly covered", category)
		}
	}
}

func TestInspectValidSnapshotsReportCoverageAndDefensiveFacts(t *testing.T) {
	tests := []struct {
		name        string
		empty       string
		wantStatus  Status
		wantMissing []string
	}{
		{name: "complete", wantStatus: Complete, wantMissing: []string{}},
		{name: "incomplete", empty: "deployment", wantStatus: Incomplete, wantMissing: []string{"deployment"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := inspectionRecord()
			if tt.empty != "" {
				record.Facts[tt.empty] = nil
			}
			svc := fakeService(record, "2026-08-30T17:00:00Z", true, nil)
			got, err := svc.Inspect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.wantStatus || got.SchemaVersion != SchemaVersion || got.UpdatedAt != "2026-08-30T17:00:00Z" || !reflect.DeepEqual(got.MissingOrInvalid, tt.wantMissing) {
				t.Fatalf("inspection = %#v", got)
			}
			for _, category := range Categories() {
				if got.Coverage[category] != (category != tt.empty) {
					t.Fatalf("coverage[%q] = %v", category, got.Coverage[category])
				}
			}
			got.Facts["stack"][0].Assertion = "caller mutation"
			again, err := svc.Inspect(context.Background())
			if err != nil || again.Facts["stack"][0].Assertion == "caller mutation" {
				t.Fatalf("facts were not defensively copied: %#v, %v", again, err)
			}
		})
	}
}

func TestInspectInvalidStoredSnapshotExposesNoFacts(t *testing.T) {
	record := inspectionRecord()
	tests := []error{
		ErrInvalidRecord,
		errors.Join(errors.New("decode stored snapshot"), ErrInvalidRecord),
	}
	for _, storedErr := range tests {
		got, err := fakeService(record, "2026-08-30T17:00:00Z", true, storedErr).Inspect(context.Background())
		if err != nil || got.Status != Incomplete || got.SchemaVersion != 0 || len(got.Facts) != 0 || got.UpdatedAt != "" || !reflect.DeepEqual(got.MissingOrInvalid, Categories()) {
			t.Fatalf("invalid inspection = %#v, %v", got, err)
		}
	}
}

func fakeService(record Record, updatedAt string, found bool, loadErr error) *Service {
	return New(Dependencies{Resolve: func(context.Context) (Resolved, error) {
		return Resolved{CheckoutRoot: "/checkout", Load: func(context.Context) (Record, string, bool, error) {
			return CloneRecord(record), updatedAt, found, loadErr
		}}, nil
	}})
}

func inspectionRecord() Record {
	facts := make(map[string][]Fact, len(categories))
	for _, category := range Categories() {
		facts[category] = []Fact{{Assertion: category + " fact", ObservedAt: "2026-08-30T12:00:00Z", Evidence: Evidence{Path: "README.md"}}}
	}
	return Record{SchemaVersion: SchemaVersion, Facts: facts}
}
