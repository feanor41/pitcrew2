package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunDelegatesGlobalVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr, t.TempDir()); code != 0 || stdout.String() != "pitcrew 0.4.0\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestTUIRealPTYUninitializedQuitsWithoutCreatingState(t *testing.T) {
	script, err := exec.LookPath("script")
	if err != nil {
		t.Fatal("real PTY harness requires script(1)")
	}
	root, binary := t.TempDir(), filepath.Join(t.TempDir(), "pitcrew")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pitcrew: %v\n%s", err, output)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, script, "-qfec", "stty rows 24 cols 80; exec "+binary+" tui", "/dev/null")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	_, _ = io.WriteString(stdin, "q")
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("PTY run: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "No PitCrew repository is initialized for this project.") {
		t.Fatalf("uninitialized message missing:\n%s", output.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("clean project mutated: entries=%v err=%v", entries, err)
	}
}
