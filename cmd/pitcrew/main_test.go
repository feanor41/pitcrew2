package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunDelegatesGlobalVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr, t.TempDir()); code != 0 || stdout.String() != "pitcrew 0.20.1\n" || stderr.Len() != 0 {
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
	if len(entries) != 9 {
		t.Fatalf("installed agents = %d, want 9", len(entries))
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "pitcrew", "agent-contract.md")); err != nil {
		t.Fatalf("installed contract: %v", err)
	}
	contract, err := os.ReadFile(filepath.Join(home, ".codex", "pitcrew", "agent-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"first admission gate",
		"does not interpose on or prevent host filesystem writes",
		"transcript-free composition",
		"workflow ID and current revision",
		"workflow show --view unit --unit-id",
		"never simulate it by replaying conversation history or transcript content",
	} {
		if !bytes.Contains(contract, []byte(required)) {
			t.Fatalf("standalone installed contract omitted %q", required)
		}
	}
	roleClauses := map[string]string{
		"daimon.toml":              "Do not invoke workflow commands",
		"aion.toml":                "Never forge or bypass independent review",
		"pc2_explorer.toml":        "bounded phase view",
		"pc2_specifier.toml":       "bounded phase view",
		"pc2_designer.toml":        "bounded phase view",
		"pc2_task_planner.toml":    "bounded phase view",
		"pc2_implementer.toml":     "bounded unit view",
		"pc2_reviewer.toml":        "bounded unit or aggregate view",
		"pc2_sdd_initializer.toml": "No workflow ID, transcript, or handle is required",
	}
	for _, entry := range entries {
		body, readErr := os.ReadFile(filepath.Join(registry, entry.Name()))
		if readErr != nil || !bytes.Contains(body, []byte(roleClauses[entry.Name()])) {
			t.Fatalf("installed role %s omitted role-specific coordination clause: %v", entry.Name(), readErr)
		}
	}
	rolePermissions := map[string]string{
		"daimon.toml":           "Allowed workflow commands: No workflow commands; forward accepted intent to Aion.",
		"pc2_explorer.toml":     "Allowed workflow commands: workflow show --view phase and workflow explore.",
		"pc2_specifier.toml":    "Allowed workflow commands: workflow show --view phase and workflow spec.",
		"pc2_designer.toml":     "Allowed workflow commands: workflow show --view phase and workflow design.",
		"pc2_task_planner.toml": "Allowed workflow commands: workflow show --view phase and workflow plan.",
		"pc2_implementer.toml":  "Allowed workflow commands: workflow show --view unit --unit-id <wu-id>, workflow list-ready-units, workflow claim-unit, workflow unit-tdd, and workflow unit-complete. Never workflow unit-review or workflow complete.",
		"pc2_reviewer.toml":     "Allowed workflow commands: workflow show --view unit --unit-id <wu-id>, workflow show --view aggregate, workflow unit-review, and workflow complete only. Never implementation commands.",
	}
	for role, permission := range rolePermissions {
		body, readErr := os.ReadFile(filepath.Join(registry, role))
		if readErr != nil || !bytes.Contains(body, []byte("\n"+permission+"\n")) {
			t.Fatalf("installed role %s permission drift: %v", role, readErr)
		}
	}
	var roleBytes int
	for _, entry := range entries {
		body, _ := os.ReadFile(filepath.Join(registry, entry.Name()))
		roleBytes += len(body)
	}
	if roleBytes > 49000 {
		t.Fatalf("installed role prompts use %d bytes; budget is 49000", roleBytes)
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
	if output, err := exec.Command("git", "-C", root, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("initialize unconfigured Git project: %v: %s", err, output)
	}
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
	var output synchronizedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(output.String(), "Install in Runtime") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(output.String(), "Install in Runtime") {
		t.Fatalf("home did not become ready before navigation:\n%s", output.String())
	}
	_, _ = io.WriteString(stdin, "jj\r")
	time.Sleep(100 * time.Millisecond)
	_, _ = io.WriteString(stdin, "q")
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("PTY run: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "Install in Runtime") || !strings.Contains(output.String(), "Configure Runtime Models") || !strings.Contains(output.String(), "Deliveries") {
		t.Fatalf("Home did not launch before Deliveries:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Could not load deliveries.") || !strings.Contains(output.String(), "No PitCrew repository is initialized for this project.") {
		t.Fatalf("uninitialized message missing:\n%s", output.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".git" {
		t.Fatalf("clean project mutated: entries=%v err=%v", entries, err)
	}
}

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
