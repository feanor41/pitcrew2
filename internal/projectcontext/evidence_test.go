package projectcontext

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfineEvidenceResolvesExistingSymlinkComponents(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	inside := filepath.Join(root, "docs")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"inside": inside, "outside": outside} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name    string
		path    string
		wantErr bool
	}{{"ordinary missing suffix", "new/evidence.md", false}, {"inside symlink", "inside/evidence.md", false}, {"outside symlink", "outside/evidence.md", true}} {
		t.Run(tt.name, func(t *testing.T) {
			record := recordFixture()
			record.Facts["stack"][0].Evidence.Path = tt.path
			err := ConfineEvidence(root, record)
			if errors.Is(err, ErrEvidenceOutsideCheckout) != tt.wantErr || err != nil && !tt.wantErr {
				t.Fatalf("ConfineEvidence(%q) error = %v", tt.path, err)
			}
		})
	}
}

func TestConfineEvidenceUsesEachActiveCheckout(t *testing.T) {
	main, linked := t.TempDir(), t.TempDir()
	if err := os.Symlink(linked, filepath.Join(main, "linked")); err != nil {
		t.Fatal(err)
	}
	record := recordFixture()
	record.Facts["stack"][0].Evidence.Path = "linked/evidence.md"
	if err := ConfineEvidence(main, record); !errors.Is(err, ErrEvidenceOutsideCheckout) {
		t.Fatalf("main checkout error = %v", err)
	}
	record.Facts["stack"][0].Evidence.Path = "evidence.md"
	if err := ConfineEvidence(linked, record); err != nil {
		t.Fatalf("linked checkout error = %v", err)
	}
}

func recordFixture() Record {
	facts := make(map[string][]Fact, len(categories))
	for _, category := range Categories() {
		facts[category] = []Fact{{Assertion: category, ObservedAt: "2026-08-30T12:00:00Z", Evidence: Evidence{Path: "README.md"}}}
	}
	return Record{SchemaVersion: SchemaVersion, Facts: facts}
}
