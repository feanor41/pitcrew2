package sddinitializer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/projectcontext"
)

func TestInitializeInspectsOnceAndBypassesCompleteContext(t *testing.T) {
	inspection := projectcontext.Inspection{Status: projectcontext.Complete, CheckoutRoot: t.TempDir()}
	inspector := &fakeInspector{inspection: inspection}
	recorder := &fakeRecorder{}
	collected := 0

	result, err := Initialize(context.Background(), Dependencies{
		Inspector: inspector,
		Recorder:  recorder,
		Collect: func(string, time.Time) (projectcontext.Record, error) {
			collected++
			return projectcontext.Record{}, errors.New("must not collect")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inspector.calls != 1 || collected != 0 || recorder.calls != 0 || result.Persisted {
		t.Fatalf("calls inspect=%d collect=%d record=%d result=%#v", inspector.calls, collected, recorder.calls, result)
	}
	if !reflect.DeepEqual(result.Inspection, inspection) {
		t.Fatalf("inspection=%#v", result.Inspection)
	}
}

func TestInitializeCollectsFixedShallowSignalsWithoutExecutingFiles(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "executed")
	files := map[string]string{
		"go.mod":     "module example.com/project\n\ngo 1.26.5\n",
		"Dockerfile": "FROM scratch\n",
		"README.md":  "#!/bin/sh\ntouch " + marker + "\n",
		"AGENTS.md":  "#!/bin/sh\ntouch " + marker + "\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"cmd", "internal", "openspec"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A tempting nested manifest proves collection is shallow and non-recursive.
	if err := os.WriteFile(filepath.Join(root, "cmd", "package.json"), []byte("{}"), 0o700); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 30, 14, 0, 0, 123, time.FixedZone("test", -3*60*60))
	inspector := &fakeInspector{inspection: projectcontext.Inspection{Status: projectcontext.Missing, CheckoutRoot: root}}
	recorder := &fakeRecorder{}
	result, err := Initialize(context.Background(), Dependencies{Inspector: inspector, Recorder: recorder, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Persisted || inspector.calls != 1 || recorder.calls != 1 || recorder.actor != Actor {
		t.Fatalf("result=%#v inspect=%d record=%d actor=%q", result, inspector.calls, recorder.calls, recorder.actor)
	}
	for _, category := range projectcontext.Categories() {
		if len(recorder.record.Facts[category]) == 0 {
			t.Fatalf("category %q not observed: %#v", category, recorder.record.Facts)
		}
	}
	if got := recorder.record.Facts["runtime"][0]; got.Assertion != "Go toolchain version 1.26.5." || got.ObservedAt != at.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("runtime fact=%#v", got)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository file was executed: %v", err)
	}
	if len(recorder.record.Facts["stack"]) != 1 {
		t.Fatalf("nested inventory was scanned: %#v", recorder.record.Facts["stack"])
	}
}

func TestInitializePreservesOrderAndDeduplicatesAssertionWithEvidence(t *testing.T) {
	at := time.Date(2026, 8, 30, 15, 0, 0, 0, time.UTC)
	existing := emptyRecord()
	existing.Facts["stack"] = []projectcontext.Fact{{
		Assertion: "Go module manifest.", ObservedAt: "2026-08-29T00:00:00Z",
		Evidence: projectcontext.Evidence{Path: "go.mod"},
	}}
	observed := emptyRecord()
	observed.Facts["stack"] = []projectcontext.Fact{
		{Assertion: "Go module manifest.", ObservedAt: at.Format(time.RFC3339), Evidence: projectcontext.Evidence{Path: "go.mod"}},
		{Assertion: "Node.js package manifest.", ObservedAt: at.Format(time.RFC3339), Evidence: projectcontext.Evidence{Path: "package.json"}},
	}
	inspector := &fakeInspector{inspection: projectcontext.Inspection{Status: projectcontext.Incomplete, Facts: existing.Facts, CheckoutRoot: t.TempDir()}}
	recorder := &fakeRecorder{}

	result, err := Initialize(context.Background(), Dependencies{
		Inspector: inspector, Recorder: recorder, Now: func() time.Time { return at },
		Collect: func(string, time.Time) (projectcontext.Record, error) { return observed, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Go module manifest.", "Node.js package manifest."}
	got := []string{recorder.record.Facts["stack"][0].Assertion, recorder.record.Facts["stack"][1].Assertion}
	if !result.Persisted || !reflect.DeepEqual(got, want) || recorder.record.Facts["stack"][0].ObservedAt != existing.Facts["stack"][0].ObservedAt {
		t.Fatalf("result=%#v facts=%#v", result, recorder.record.Facts["stack"])
	}

	inspector.inspection.Facts = recorder.record.Facts
	recorder.calls = 0
	result, err = Initialize(context.Background(), Dependencies{
		Inspector: inspector, Recorder: recorder, Now: func() time.Time { return at.Add(time.Hour) },
		Collect: func(string, time.Time) (projectcontext.Record, error) { return observed, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Persisted || recorder.calls != 0 {
		t.Fatalf("unchanged initialization persisted: %#v calls=%d", result, recorder.calls)
	}
}

func TestInitializeRejectsBoundOverflowWithoutRecording(t *testing.T) {
	existing := emptyRecord()
	for i := 0; i < projectcontext.MaxFactsPerCategory; i++ {
		existing.Facts["stack"] = append(existing.Facts["stack"], projectcontext.Fact{
			Assertion:  "retained-" + string(rune('a'+i)),
			ObservedAt: "2026-08-30T00:00:00Z",
			Evidence:   projectcontext.Evidence{Path: "go.mod"},
		})
	}
	observed := emptyRecord()
	observed.Facts["stack"] = []projectcontext.Fact{{Assertion: "overflow", ObservedAt: "2026-08-30T01:00:00Z", Evidence: projectcontext.Evidence{Path: "package.json"}}}
	recorder := &fakeRecorder{}
	_, err := Initialize(context.Background(), Dependencies{
		Inspector: &fakeInspector{inspection: projectcontext.Inspection{Status: projectcontext.Incomplete, Facts: existing.Facts, CheckoutRoot: t.TempDir()}},
		Recorder:  recorder,
		Collect:   func(string, time.Time) (projectcontext.Record, error) { return observed, nil },
	})
	if !errors.Is(err, projectcontext.ErrInvalidRecord) || recorder.calls != 0 || len(existing.Facts["stack"]) != projectcontext.MaxFactsPerCategory {
		t.Fatalf("err=%v calls=%d retained=%d", err, recorder.calls, len(existing.Facts["stack"]))
	}
}

type fakeInspector struct {
	inspection projectcontext.Inspection
	calls      int
}

func (s *fakeInspector) Inspect(context.Context) (projectcontext.Inspection, error) {
	s.calls++
	return s.inspection, nil
}

type fakeRecorder struct {
	record projectcontext.Record
	actor  string
	calls  int
}

func (s *fakeRecorder) Record(_ context.Context, record projectcontext.Record, actor string, _ time.Time) (projectcontext.Inspection, bool, error) {
	s.calls++
	s.record = projectcontext.CloneRecord(record)
	s.actor = actor
	missing, err := projectcontext.MissingCategories(record)
	if err != nil {
		return projectcontext.Inspection{}, false, err
	}
	return projectcontext.Inspection{Status: projectcontext.Incomplete, Facts: record.Facts, MissingOrInvalid: missing}, true, nil
}
