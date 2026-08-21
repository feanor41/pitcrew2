package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTUIExactSameProcessDispatchAndSubprocessTrap(t *testing.T) {
	root := t.TempDir()
	trapDir := t.TempDir()
	marker := filepath.Join(trapDir, "spawned")
	trap := filepath.Join(trapDir, "pitcrew-tui")
	if err := os.WriteFile(trap, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", trapDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	called := 0
	runner := func(gotRoot string, input io.Reader, output io.Writer) error {
		called++
		if gotRoot != root {
			t.Fatalf("runner root = %q, want %q", gotRoot, root)
		}
		_, _ = io.WriteString(output, "same-process")
		return nil
	}
	run := func(args ...string) result {
		var stdout, stderr bytes.Buffer
		code := Run(args, Dependencies{Stdin: strings.NewReader("q"), Stdout: &stdout, Stderr: &stderr, ProjectRoot: root, TUIRunner: runner})
		return result{code, stdout.String(), stderr.String()}
	}

	if got := run("tui"); got.code != 0 || got.stdout != "same-process" || got.stderr != "" || called != 1 {
		t.Fatalf("exact dispatch = %#v, calls=%d", got, called)
	}
	if got := run("tui", "extra"); got.code != 2 || got.stdout != "" || called != 1 {
		t.Fatalf("extra argument = %#v, calls=%d", got, called)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("subprocess trap was executed: %v", err)
	}
}
