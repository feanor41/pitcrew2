package maxims

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTextMatchesCanonicalFileByteForByte(t *testing.T) {
	want, err := os.ReadFile("../../MAXIMS.md")
	if err != nil {
		t.Fatal(err)
	}
	if got := Text(); got != string(want) {
		t.Fatal("embedded maxims drifted from MAXIMS.md")
	}
}

func TestStructuredReturnsTheFourCanonicalMaxims(t *testing.T) {
	got, err := Structured()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("maxims count = %d", len(got))
	}
	if got[0].Number != 1 || got[0].Title != "Languages Have Two Surfaces" || got[0].Principle != "Technical English internally; the user's language externally." {
		t.Fatalf("first maxim = %#v", got[0])
	}
	if got[3].Number != 4 || got[3].Title != "Short Scope, Easy to Complete, Always" {
		t.Fatalf("fourth maxim = %#v", got[3])
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip []Maxim
	if err := json.Unmarshal(encoded, &roundTrip); err != nil || len(roundTrip) != 4 {
		t.Fatalf("JSON round trip = %#v, %v", roundTrip, err)
	}
}

func TestCanonicalMaximsAssignTrivialBypassToAion(t *testing.T) {
	text := strings.Join(strings.Fields(Text()), " ")
	if !strings.Contains(text, "the Aion agent is invited to skip the harness") {
		t.Fatal("canonical maxims do not assign the trivial-work decision to Aion")
	}
	for _, obsolete := range []string{"the Daimon agent is invited to skip the harness", "the Master agent is invited to skip the harness"} {
		if strings.Contains(text, obsolete) {
			t.Fatalf("canonical maxims retain obsolete coordinator wording %q", obsolete)
		}
	}
}

func TestCanonicalMaximsRequireProportionalDesignDecisions(t *testing.T) {
	text := strings.Join(strings.Fields(Text()), " ")
	required := []string{
		"PitCrew exists only to help the user achieve the stated goal.",
		"Is this solution overkill for the context?",
		"Would a more relaxed, less demanding solution satisfy the user's expectations equally well?",
		"Choose the least demanding solution that fully satisfies the expected outcome, material risks, and existing constraints.",
		"name the protected constraint and explain why the simpler option is insufficient",
		"claim secrecy",
		"reviewer independence",
		"terminal immutability",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Errorf("canonical maxims omit proportional-design contract %q", phrase)
		}
	}
}
