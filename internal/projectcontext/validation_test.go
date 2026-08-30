package projectcontext

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateAcceptsSchemaV1Record(t *testing.T) {
	if err := Validate(validRecord()); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidRecords(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{"unsupported schema", func(r *Record) { r.SchemaVersion = 2 }},
		{"missing category", func(r *Record) { delete(r.Facts, "runtime") }},
		{"unknown category", func(r *Record) { delete(r.Facts, "runtime"); r.Facts["unknown"] = nil }},
		{"too many facts", func(r *Record) { r.Facts["stack"] = append(r.Facts["stack"], make([]Fact, MaxFactsPerCategory)...) }},
		{"blank assertion", func(r *Record) { r.Facts["stack"][0].Assertion = " \t" }},
		{"long assertion", func(r *Record) { r.Facts["stack"][0].Assertion = strings.Repeat("a", MaxComponentBytes+1) }},
		{"invalid observed_at", func(r *Record) { r.Facts["stack"][0].ObservedAt = "yesterday" }},
		{"long evidence", func(r *Record) { r.Facts["stack"][0].Evidence.Path = strings.Repeat("p", MaxComponentBytes+1) }},
		{"no evidence", func(r *Record) { r.Facts["stack"][0].Evidence = Evidence{} }},
		{"file and command", func(r *Record) {
			r.Facts["stack"][0].Evidence.Command = "go test"
			r.Facts["stack"][0].Evidence.Summary = "passes"
		}},
		{"command missing summary", func(r *Record) { r.Facts["stack"][0].Evidence = Evidence{Command: "go test"} }},
		{"summary missing command", func(r *Record) { r.Facts["stack"][0].Evidence = Evidence{Summary: "passes"} }},
		{"command line range", func(r *Record) {
			r.Facts["stack"][0].Evidence = Evidence{Command: "go test", Summary: "passes", LineRange: "1-2"}
		}},
		{"absolute path", func(r *Record) { r.Facts["stack"][0].Evidence.Path = "/README.md" }},
		{"traversing path", func(r *Record) { r.Facts["stack"][0].Evidence.Path = "../README.md" }},
		{"non-normalized path", func(r *Record) { r.Facts["stack"][0].Evidence.Path = "docs/../README.md" }},
		{"backslash path", func(r *Record) { r.Facts["stack"][0].Evidence.Path = `docs\README.md` }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := validRecord()
			tt.mutate(&record)
			if err := Validate(record); !errors.Is(err, ErrInvalidRecord) {
				t.Fatalf("Validate() error = %v; want ErrInvalidRecord", err)
			}
		})
	}
}

func TestValidateRejectsOversizedEncodedRecord(t *testing.T) {
	record := validRecord()
	for _, category := range Categories() {
		record.Facts[category] = make([]Fact, MaxFactsPerCategory)
		for i := range record.Facts[category] {
			record.Facts[category][i] = Fact{Assertion: strings.Repeat("a", MaxComponentBytes), ObservedAt: "2026-08-30T12:34:56Z", Evidence: Evidence{Path: "README.md"}}
		}
	}
	if err := Validate(record); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Validate() error = %v; want ErrInvalidRecord", err)
	}
}

func TestValidateActorRequiresBoundedNonblankLabel(t *testing.T) {
	for _, actor := range []string{"", " \t", strings.Repeat("a", MaxComponentBytes+1)} {
		if err := ValidateActor(actor); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("ValidateActor(%q) error = %v; want ErrInvalidRecord", actor, err)
		}
	}
	if err := ValidateActor("pc2-implementer"); err != nil {
		t.Fatalf("ValidateActor(valid) error = %v", err)
	}
}

func validRecord() Record {
	facts := make(map[string][]Fact, len(Categories()))
	for _, category := range Categories() {
		facts[category] = []Fact{{Assertion: category + " fact", ObservedAt: "2026-08-30T12:34:56Z", Evidence: Evidence{Path: "README.md", LineRange: "1-2"}}}
	}
	return Record{SchemaVersion: SchemaVersion, Facts: facts}
}
