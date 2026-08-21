package maxims

import (
	"encoding/json"
	"os"
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
