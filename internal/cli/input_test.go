package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestInputFileRejectsUnsafeOrNonStrictJSONBeforeOpeningStore(t *testing.T) {
	root := t.TempDir()
	valid := writeInput(t, root, "valid.json", `{"content":"finding"}`)
	bad := map[string]string{
		"unknown field":  `{"content":"x","extra":true}`,
		"trailing JSON":  `{"content":"x"}{"content":"y"}`,
		"malformed JSON": `{"content":`,
		"invalid UTF-8":  string([]byte{'{', '"', 'c', 'o', 'n', 't', 'e', 'n', 't', '"', ':', '"', 0xff, '"', '}'}),
	}
	for name, body := range bad {
		t.Run(name, func(t *testing.T) {
			path := writeInput(t, root, strings.ReplaceAll(name, " ", "-")+".json", body)
			result := runAt(t, root, "workflow", "explore", "--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "explorer", "--input-file", path)
			if result.code != 2 || result.stdout != "" || !strings.Contains(result.stderr, `"code":"usage"`) {
				t.Fatalf("result=%#v", result)
			}
		})
	}
	for name, path := range map[string]string{"directory": root, "missing": filepath.Join(root, "missing.json")} {
		t.Run(name, func(t *testing.T) {
			result := runAt(t, root, "workflow", "explore", "--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "explorer", "--input-file", path)
			if result.code != 2 {
				t.Fatalf("result=%#v", result)
			}
		})
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link.json")
		if err := os.Symlink(valid, link); err != nil {
			t.Fatal(err)
		}
		result := runAt(t, root, "workflow", "explore", "--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "explorer", "--input-file", link)
		if result.code != 2 {
			t.Fatalf("symlink result=%#v", result)
		}
		fifo := filepath.Join(root, "input.fifo")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		result = runAt(t, root, "workflow", "explore", "--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "explorer", "--input-file", fifo)
		if result.code != 2 {
			t.Fatalf("FIFO result=%#v", result)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".pitcrew")); !os.IsNotExist(err) {
		t.Fatalf("store opened before input validation: %v", err)
	}
}

func TestEveryWorkflowCommandRequiresItsExactFlagMatrix(t *testing.T) {
	root := t.TempDir()
	input := writeInput(t, root, "input.json", `{"content":"x"}`)
	base := []string{"--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "actor"}
	cases := map[string][]string{
		"new":                  {"--name", "work", "--goal", "x", "--actor", "actor"},
		"continue":             {"--from", "wf-000000000000000000000001", "--actor", "actor"},
		"progress":             append(append([]string{}, base...), "--input-file", input),
		"request-capability":   append(append([]string{}, base...), "--input-file", input),
		"show":                 {"--workflow-id", "wf-000000000000000000000001"},
		"explore":              append(append([]string{}, base...), "--input-file", input),
		"spec":                 append(append([]string{}, base...), "--input-file", input),
		"design":               append(append([]string{}, base...), "--input-file", input),
		"plan":                 append(append([]string{}, base...), "--input-file", input),
		"amend-plan":           append(append([]string{}, base...), "--input-file", input),
		"approve-plan":         append([]string{}, base...),
		"list-ready-units":     {"--workflow-id", "wf-000000000000000000000001"},
		"begin-implementation": append([]string{}, base...),
		"complete":             append(append([]string{}, base...), "--input-file", input),
		"authorize-correction": append(append([]string{}, base...), "--input-file", input),
		"abandon":              append(append([]string{}, base...), "--reason", "stop"),
		"claim-unit":           append(append([]string{}, base...), "--unit-id", "wu-000000000000000000000001", "--handle-dir", filepath.Join(root, "handles")),
		"recover-unit-claim":   append(append([]string{}, base...), "--unit-id", "wu-000000000000000000000001", "--handle-dir", filepath.Join(root, "handles")),
		"recover-aggregate":    append(append([]string{}, base...), "--input-file", input, "--handle-dir", filepath.Join(root, "handles")),
		"handoff-review":       append(append([]string{}, base...), "--unit-id", "wu-000000000000000000000001", "--handle-dir", filepath.Join(root, "handles")),
		"recover-review":       append(append([]string{}, base...), "--unit-id", "wu-000000000000000000000001", "--handle-dir", filepath.Join(root, "handles")),
		"unit-tdd":             append(append([]string{}, base...), "--unit-id", "wu-000000000000000000000001", "--claim-handle", filepath.Join(root, "handle.json"), "--input-file", input),
		"unit-review":          append(append([]string{}, base...), "--unit-id", "wu-000000000000000000000001", "--claim-handle", filepath.Join(root, "handle.json"), "--input-file", input),
		"unit-complete":        append(append([]string{}, base...), "--unit-id", "wu-000000000000000000000001", "--claim-handle", filepath.Join(root, "handle.json")),
	}
	for command, complete := range cases {
		t.Run(command, func(t *testing.T) {
			for i := 0; i < len(complete); i += 2 {
				missing := append([]string{}, complete[:i]...)
				missing = append(missing, complete[i+2:]...)
				result := runAt(t, t.TempDir(), append([]string{"workflow", command}, missing...)...)
				if result.code != 2 {
					t.Fatalf("missing %s => %#v", complete[i], result)
				}
			}
		})
	}
}

func TestExactFlagMatrixRejectsUnknownDuplicateShortAndMissing(t *testing.T) {
	for _, args := range [][]string{
		{"workflow", "new", "--goal", "x"},
		{"workflow", "new", "--name", "work", "--goal", "x", "--actor", "a", "--actor", "b"},
		{"workflow", "show", "-workflow-id", "wf-x"},
		{"workflow", "show", "--workflow-id", "wf-x", "--extra", "x"},
	} {
		result := runCLI(t, args...)
		if result.code != 2 || result.stdout != "" {
			t.Fatalf("%v => %#v", args, result)
		}
	}
}

func TestWorkflowShowRejectsInvalidViewCombinationsBeforeStoreOpen(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown view":         {"--workflow-id", "wf-000000000000000000000001", "--view", "summary"},
		"unit missing id":      {"--workflow-id", "wf-000000000000000000000001", "--view", "unit"},
		"unit id without view": {"--workflow-id", "wf-000000000000000000000001", "--unit-id", "wu-000000000000000000000001"},
		"unit id for phase":    {"--workflow-id", "wf-000000000000000000000001", "--view", "phase", "--unit-id", "wu-000000000000000000000001"},
		"unit id for audit":    {"--workflow-id", "wf-000000000000000000000001", "--view", "audit", "--unit-id", "wu-000000000000000000000001"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			result := runAt(t, root, append([]string{"workflow", "show"}, args...)...)
			if result.code != 2 || result.stdout != "" || !strings.Contains(result.stderr, `"code":"usage"`) {
				t.Fatalf("result=%#v", result)
			}
			if _, err := os.Stat(filepath.Join(root, ".pitcrew")); !os.IsNotExist(err) {
				t.Fatalf("store opened before view validation: %v", err)
			}
		})
	}
}

func TestInvalidTypedStageInputFailsBeforeMutation(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"missing entries":        `{"content":"x","schema_version":1}`,
		"entries without schema": `{"content":"x","entries":[]}`,
		"unsupported schema":     `{"content":"x","schema_version":2,"entries":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := writeInput(t, root, strings.ReplaceAll(name, " ", "-")+".json", body)
			result := runAt(t, root, "workflow", "explore", "--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "explorer", "--input-file", path)
			if result.code != 2 || result.stdout != "" || !strings.Contains(result.stderr, `"code":"usage"`) {
				t.Fatalf("result=%#v", result)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, ".pitcrew")); !os.IsNotExist(err) {
		t.Fatalf("store opened before typed input validation: %v", err)
	}
}

func TestDTOsRequireEveryDeclaredFieldBeforeStoreOpen(t *testing.T) {
	identity := []string{"--workflow-id", "wf-000000000000000000000001", "--unit-id", "wu-000000000000000000000001", "--revision", "1", "--actor", "actor", "--claim-handle", "/missing"}
	tests := []struct {
		name, command, body string
		args                []string
	}{
		{"plan areas", "plan", `{"summary":"x","scope":"internal","max_parallel_units":1,"work_units":[{"id":"wu-000000000000000000000001","description":"x","scope":"internal/x","depends_on":[],"estimated_changed_lines":0,"estimated_review_minutes":0}]}`, []string{"--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "planner"}},
		{"TDD refactor", "unit-tdd", `{"red_command":"r","red_outcome":"fail","green_command":"g","green_outcome":"pass","validation_command":"v","validation_outcome":"pass","changed_paths":"internal"}`, identity},
		{"TDD changed path", "unit-tdd", `{"red_command":"r","red_outcome":"fail","green_command":"g","green_outcome":"pass","refactor_summary":"","validation_command":"v","validation_outcome":"pass","changed_paths":"../secret"}`, identity},
		{"review summary", "unit-review", `{"verdict":"approved","findings":""}`, identity},
		{"aggregate review findings", "complete", `{"verdict":"corrections","summary":"changes"}`, []string{"--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "reviewer"}},
		{"correction authorization confirmation", "authorize-correction", `{"aggregate_review_revision":1,"reason":"approved"}`, []string{"--workflow-id", "wf-000000000000000000000001", "--revision", "1", "--actor", "aion"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeInput(t, root, "input.json", tt.body)
			args := append([]string{"workflow", tt.command}, tt.args...)
			args = append(args, "--input-file", path)
			result := runAt(t, root, args...)
			wantCode := 3
			if tt.command == "authorize-correction" {
				wantCode = 2
			}
			if result.code != wantCode {
				t.Fatalf("result=%#v", result)
			}
			if _, err := os.Stat(filepath.Join(root, ".pitcrew")); !os.IsNotExist(err) {
				t.Fatalf("store opened: %v", err)
			}
		})
	}
}

func writeInput(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
