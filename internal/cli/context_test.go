package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/envelope"
	"github.com/fmazzalomo/pitcrew/internal/projectcontext"
	"github.com/fmazzalomo/pitcrew/internal/store"
)

func TestContextInspectIsReadOnlyAndInitializePersistsBoundedInventory(t *testing.T) {
	root, dataHome := contextRepository(t), filepath.Join(t.TempDir(), "absent")
	missing := runCentral(t, root, dataHome, "context", "inspect")
	if missing.code != 0 || !strings.Contains(missing.stdout, `"status":"missing"`) || !strings.Contains(missing.stdout, `"next_action":"context initialize"`) {
		t.Fatalf("missing inspect = %#v", missing)
	}
	if _, err := os.Lstat(dataHome); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspect created central state: %v", err)
	}
	initialized := runCentral(t, root, dataHome, "context", "initialize")
	if initialized.code != 0 || !strings.Contains(initialized.stdout, `"persisted":true`) {
		t.Fatalf("initialize = %#v", initialized)
	}
	inspected := runCentral(t, root, dataHome, "context", "inspect")
	if inspected.code != 0 || !strings.Contains(inspected.stdout, `"status":"complete"`) || !strings.Contains(inspected.stdout, `"next_action":"workflow new"`) {
		t.Fatalf("initialized inspect = %#v", inspected)
	}
}

func TestContextRecordUsesStrictTransportAndDomainExit(t *testing.T) {
	root, dataHome := contextRepository(t), t.TempDir()
	record := contextRecord()
	encoded, _ := json.Marshal(record)
	input := writeInput(t, t.TempDir(), "context.json", string(encoded))
	if got := runCentral(t, root, dataHome, "context", "record", "--actor", "aion", "--input-file", input); got.code != 0 || !strings.Contains(got.stdout, `"status":"complete"`) {
		t.Fatalf("record = %#v", got)
	}

	bad := writeInput(t, t.TempDir(), "bad.json", `{"schema_version":1,"facts":{},"unknown":true}`)
	link := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(input, link); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"context", "record", "--actor", "aion"}, {"context", "record", "--actor", "aion", "--input-file", bad}, {"context", "record", "--actor", "aion", "--input-file", link}, {"context", "inspect", "--json"}} {
		if got := runCentral(t, root, dataHome, args...); got.code != int(envelope.Usage) || got.stdout != "" {
			t.Fatalf("usage %v = %#v", args, got)
		}
	}

	invalid := contextRecord()
	invalid.Facts["stack"][0].Evidence.Path = "../escape"
	encoded, _ = json.Marshal(invalid)
	badDomain := writeInput(t, t.TempDir(), "invalid.json", string(encoded))
	if got := runCentral(t, root, dataHome, "context", "record", "--actor", "aion", "--input-file", badDomain); got.code != int(envelope.State) {
		t.Fatalf("domain failure = %#v", got)
	}
}

func TestContextMutationPreservesLegacyConsolidationGate(t *testing.T) {
	root := contextRepository(t)
	legacy, err := store.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	_ = legacy.Close()
	encoded, _ := json.Marshal(contextRecord())
	input := writeInput(t, t.TempDir(), "context.json", string(encoded))
	got := runCentral(t, root, t.TempDir(), "context", "record", "--actor", "aion", "--input-file", input)
	if got.code != int(envelope.State) || !strings.Contains(got.stderr, projectcontext.LegacyRecoveryNextAction) {
		t.Fatalf("legacy gate = %#v", got)
	}
}

func TestContextStateMappingDoesNotChangeExistingProjectCommand(t *testing.T) {
	root := "/"
	if got := runCentral(t, root, t.TempDir(), "context", "inspect"); got.code != int(envelope.State) {
		t.Fatalf("context project error = %#v", got)
	}
	if got := runCentral(t, root, t.TempDir(), "project", "inspect"); got.code != int(envelope.Internal) {
		t.Fatalf("existing project mapping changed = %#v", got)
	}
}

func contextRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{".git", "cmd", "internal", "docs", "openspec"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"go.mod": "module example.test/context\n\ngo 1.26\n", "Dockerfile": "FROM scratch\n", "README.md": "# Context\n", "AGENTS.md": "# Agents\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func contextRecord() projectcontext.Record {
	facts := map[string][]projectcontext.Fact{}
	for _, category := range projectcontext.Categories() {
		facts[category] = []projectcontext.Fact{{Assertion: category, ObservedAt: "2026-08-30T12:00:00Z", Evidence: projectcontext.Evidence{Path: "README.md"}}}
	}
	return projectcontext.Record{SchemaVersion: projectcontext.SchemaVersion, Facts: facts}
}
