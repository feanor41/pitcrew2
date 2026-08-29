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
	if code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr, t.TempDir()); code != 0 || stdout.String() != "pitcrew 0.16.0\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestStandaloneBinaryInstallsCodexWithoutCheckout(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "pitcrew")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build pitcrew: %v\n%s", err, output)
	}

	home := filepath.Join(t.TempDir(), "home")
	workingDirectory := t.TempDir()
	temporaryDirectory := t.TempDir()
	cmd := exec.Command(binary, "install", "codex")
	cmd.Dir = workingDirectory
	cmd.Env = append(os.Environ(), "HOME="+home, "TMPDIR="+temporaryDirectory, "CODEX_HOME=", "OPENCODE_CONFIG_DIR=", "CLAUDE_CONFIG_DIR=", "PI_AGENT_HOME=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("standalone install: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	registry := filepath.Join(home, ".codex", "agents")
	entries, err := os.ReadDir(registry)
	if err != nil {
		t.Fatalf("read installed registry: %v", err)
	}
	if len(entries) != 8 {
		t.Fatalf("installed agents = %d, want 8", len(entries))
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "pitcrew", "agent-contract.md")); err != nil {
		t.Fatalf("installed contract: %v", err)
	}
	want := "Installed PitCrew agents for Codex in " + registry + "\n"
	if stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("stdout=%q, want %q; stderr=%q", stdout.String(), want, stderr.String())
	}
	if matches, err := filepath.Glob(filepath.Join(temporaryDirectory, "pitcrew-install-*")); err != nil || len(matches) != 0 {
		t.Fatalf("private extraction remains: %v (err=%v)", matches, err)
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
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "XDG_DATA_HOME="+filepath.Join(t.TempDir(), "data"))
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
	_, _ = io.WriteString(stdin, "jj\r")
	time.Sleep(100 * time.Millisecond)
	_, _ = io.WriteString(stdin, "q")
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("PTY run: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "Install in Runtime") || !strings.Contains(output.String(), "Configure Runtime Models") || !strings.Contains(output.String(), "Workflows") {
		t.Fatalf("Home did not launch before Workflows:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Could not load workflows.") || !strings.Contains(output.String(), "No PitCrew repository is initialized for this project.") {
		t.Fatalf("uninitialized message missing:\n%s", output.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("clean project mutated: entries=%v err=%v", entries, err)
	}
}
