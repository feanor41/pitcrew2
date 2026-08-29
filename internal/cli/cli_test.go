package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/envelope"
	"github.com/fmazzalomo/pitcrew/internal/handles"
	"github.com/fmazzalomo/pitcrew/internal/maxims"
	"github.com/fmazzalomo/pitcrew/internal/runtimeinstall"
	"github.com/fmazzalomo/pitcrew/internal/store"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
)

func TestPrinciplesPrintsExactTextOrStructuredJSON(t *testing.T) {
	textResult := runCLI(t, "principles")
	if textResult.code != 0 || textResult.stdout != maxims.Text() || textResult.stderr != "" {
		t.Fatalf("text result = %#v", textResult)
	}
	jsonResult := runCLI(t, "principles", "--json")
	if jsonResult.code != 0 || jsonResult.stderr != "" {
		t.Fatalf("JSON result = %#v", jsonResult)
	}
	var got []maxims.Maxim
	if err := json.Unmarshal([]byte(jsonResult.stdout), &got); err != nil || len(got) != 4 {
		t.Fatalf("principles JSON=%q, %v", jsonResult.stdout, err)
	}
}

func TestHelpEndsWithEpilogueAndHidesForbiddenSurfaces(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"workflow", "--help"}, {"workflow", "new", "--help"}, {"workflow", "show", "--help"}, {"principles", "--help"}} {
		result := runCLI(t, args...)
		if result.code != 0 || result.stderr != "" {
			t.Fatalf("help %v = %#v", args, result)
		}
		if !strings.HasSuffix(result.stdout, helpEpilogue+"\n") {
			t.Fatalf("help %v lacks epilogue: %q", args, result.stdout)
		}
		for _, forbidden := range []string{"--print-claim-handle-secret-once", "--claim-token", "--emit-plain-token", "daemon", "rpc", "pitcrew-tui", "v1 migration"} {
			if strings.Contains(strings.ToLower(result.stdout), strings.ToLower(forbidden)) {
				t.Fatalf("help %v exposes %q", args, forbidden)
			}
		}
	}
}

func TestUsageFailuresAreStderrOnlySingleLineAndLongFlagsOnly(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"workflow", "new", "-goal", "x"}, {"workflow", "new", "--goal"}, {"workflow", "plan"}} {
		result := runCLI(t, args...)
		if result.code != int(envelope.Usage) || result.stdout != "" || strings.Count(strings.TrimSuffix(result.stderr, "\n"), "\n") != 0 {
			t.Fatalf("usage %v = %#v", args, result)
		}
		var failure envelope.Failure
		if err := json.Unmarshal([]byte(result.stderr), &failure); err != nil || failure.OK || failure.Error.Code != "usage" {
			t.Fatalf("failure=%#v, %v", failure, err)
		}
	}
}

func TestInstallDispatchesOnlyExactSupportedTargetWithoutOpeningProjectStore(t *testing.T) {
	for _, target := range []string{"codex", "opencode", "claude", "pi"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			var called string
			var got runtimeinstall.Dependencies
			var stdout, stderr bytes.Buffer
			deps := Dependencies{
				Stdin: &bytes.Buffer{}, Stdout: &stdout, Stderr: &stderr, ProjectRoot: root,
				InstallRunner: func(target string, deps runtimeinstall.Dependencies) int {
					called, got = target, deps
					return 23
				},
			}

			if code := Run([]string{"install", target}, deps); code != 23 {
				t.Fatalf("exit = %d, want 23", code)
			}
			if called != target || got.Stdin != deps.Stdin || got.Stdout != deps.Stdout || got.Stderr != deps.Stderr || got.Cwd != root {
				t.Fatalf("dispatch = %q, %#v", called, got)
			}
			if _, err := os.Stat(filepath.Join(root, ".pitcrew")); !os.IsNotExist(err) {
				t.Fatalf("install opened project store: %v", err)
			}
		})
	}
}

func TestInstallHelpAndUsageAreClosed(t *testing.T) {
	help := runCLI(t, "install", "--help")
	wantHelp := "Usage: pitcrew install <codex|opencode|claude|pi>\n\n" +
		"Installs or updates PitCrew agents for one runtime.\n\n" +
		"Runtimes: codex, opencode, claude, pi\n" + helpEpilogue + "\n"
	if help.code != 0 || help.stderr != "" || help.stdout != wantHelp {
		t.Fatalf("install help = %#v", help)
	}
	root := runCLI(t, "--help")
	if !strings.Contains(root.stdout, "  install codex|opencode|claude|pi\n") {
		t.Fatalf("root help omits install targets: %q", root.stdout)
	}

	for _, args := range [][]string{{"install"}, {"install", "CODEX"}, {"install", "unknown"}, {"install", "codex", "extra"}, {"install", "--overwrite"}} {
		called := false
		var stdout, stderr bytes.Buffer
		code := Run(args, Dependencies{Stdout: &stdout, Stderr: &stderr, ProjectRoot: t.TempDir(), InstallRunner: func(string, runtimeinstall.Dependencies) int {
			called = true
			return 0
		}})
		if code != int(envelope.Usage) || stdout.Len() != 0 || called {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q called=%v", args, code, stdout.String(), stderr.String(), called)
		}
		var failure envelope.Failure
		if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil || failure.Error.Message != "usage: pitcrew install <codex|opencode|claude|pi>" {
			t.Fatalf("args=%v usage=%q parse=%v", args, stderr.String(), err)
		}
	}
}

func TestWorkflowNewAndShowUseEnvelopeAndProjectLocalStore(t *testing.T) {
	root := t.TempDir()
	created := runAt(t, root, "workflow", "new", "--name", "Release 0.2", "--goal", "ship safely", "--actor", "daimon")
	if created.code != 0 || created.stderr != "" {
		t.Fatalf("new=%#v", created)
	}
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Workflow workflow.Workflow `json:"workflow"`
		} `json:"data"`
		NextAction string `json:"next_action"`
	}
	if err := json.Unmarshal([]byte(created.stdout), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || response.Data.Workflow.Name != "Release 0.2" || response.Data.Workflow.State != workflow.Draft || response.Data.Workflow.Revision != 1 || response.NextAction != "workflow explore" {
		t.Fatalf("new response=%#v", response)
	}
	if _, err := os.Stat(filepath.Join(root, ".pitcrew", "state.db")); err != nil {
		t.Fatal(err)
	}
	shown := runAt(t, root, "workflow", "show", "--workflow-id", response.Data.Workflow.ID)
	if shown.code != 0 || shown.stderr != "" {
		t.Fatalf("show=%#v", shown)
	}
	var showResponse map[string]any
	if err := json.Unmarshal([]byte(shown.stdout), &showResponse); err != nil || showResponse["ok"] != true {
		t.Fatalf("show response=%#v, %v", showResponse, err)
	}
}

func TestVersionIsAGlobalFlag(t *testing.T) {
	result := runCLI(t, "--version")
	if result.code != 0 || result.stdout != "pitcrew 0.14.0\n" || result.stderr != "" {
		t.Fatalf("version=%#v", result)
	}
}

func TestWorkflowNewRejectsInvalidNameWithoutOpeningStore(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		code envelope.ExitCode
	}{
		{name: "missing", args: []string{"workflow", "new", "--goal", "ship", "--actor", "daimon"}, code: envelope.Usage},
		{name: "blank", args: []string{"workflow", "new", "--name", "  ", "--goal", "ship", "--actor", "daimon"}, code: envelope.Usage},
		{name: "over limit", args: []string{"workflow", "new", "--name", strings.Repeat("界", 81), "--goal", "ship", "--actor", "daimon"}, code: envelope.State},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			result := runAt(t, root, test.args...)
			if result.code != int(test.code) {
				t.Fatalf("args=%v result=%#v", test.args, result)
			}
			if _, err := os.Stat(filepath.Join(root, ".pitcrew")); !os.IsNotExist(err) {
				t.Fatalf("invalid name opened store: %v", err)
			}
		})
	}
}

func TestErrorClassificationPreservesExitContract(t *testing.T) {
	tests := []struct {
		err  error
		want envelope.ExitCode
	}{{errors.New("boom"), envelope.Internal}, {ErrUsage, envelope.Usage}, {workflow.ErrInvalidTransition, envelope.State}, {store.ErrCASMismatch, envelope.CAS}, {handles.ErrExpired, envelope.Handle}}
	for _, tt := range tests {
		if got := classify(tt.err); got != tt.want {
			t.Fatalf("classify(%v)=%d want %d", tt.err, got, tt.want)
		}
	}
}

func TestStateErrorNamesCurrentAndExpectedState(t *testing.T) {
	root := t.TempDir()
	wfID, revision := createWorkflow(t, root)
	input := writeInput(t, root, "aggregate.json", `{"verdict":"approved","summary":"aggregate matches","findings":""}`)
	failed := runAt(t, root, "workflow", "complete", "--workflow-id", wfID, "--revision", strconv.FormatInt(revision, 10), "--actor", "pc2-reviewer", "--input-file", input)
	if failed.code != int(envelope.State) || failed.stdout != "" {
		t.Fatalf("state failure=%#v", failed)
	}
	if !strings.Contains(failed.stderr, "current workflow state draft") || !strings.Contains(failed.stderr, "expected ready_to_complete") {
		t.Fatalf("state error omits current/expected: %s", failed.stderr)
	}
}

type result struct {
	code           int
	stdout, stderr string
}

func runCLI(t *testing.T, args ...string) result { return runAt(t, t.TempDir(), args...) }
func runAt(t *testing.T, root string, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, Dependencies{Stdout: &stdout, Stderr: &stderr, ProjectRoot: root, Now: func() time.Time { return time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC) }})
	return result{code, stdout.String(), stderr.String()}
}
