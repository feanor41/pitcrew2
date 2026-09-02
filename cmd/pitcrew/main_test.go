package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestRunDelegatesGlobalVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr, t.TempDir()); code != 0 || stdout.String() != "pitcrew 0.24.0\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestScopedAgentBriefCommandsActivateAgainstAcceptedWorkflow(t *testing.T) {
	binary, project, dataHome := filepath.Join(t.TempDir(), "pitcrew"), t.TempDir(), filepath.Join(t.TempDir(), "data")
	if output, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build pitcrew: %v\n%s", err, output)
	}
	if output, err := exec.Command("git", "-C", project, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("initialize project: %v: %s", err, output)
	}
	run := func(args ...string) ([]byte, error) {
		command := exec.Command(binary, args...)
		command.Dir = project
		command.Env = append(os.Environ(), "XDG_DATA_HOME="+dataHome)
		return command.CombinedOutput()
	}
	created, err := run("workflow", "new", "--name", "Scoped", "--goal", "activate scoped briefs", "--actor", "aion")
	if err != nil {
		t.Fatalf("create workflow: %v\n%s", err, created)
	}
	var envelope struct {
		Data struct {
			Workflow struct {
				ID string `json:"id"`
			} `json:"workflow"`
		} `json:"data"`
	}
	if json.Unmarshal(created, &envelope) != nil || envelope.Data.Workflow.ID == "" {
		t.Fatalf("workflow envelope=%s", created)
	}
	var databasePath string
	_ = filepath.Walk(dataHome, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr == nil && info != nil && info.Name() == "state.db" {
			databasePath = path
		}
		return nil
	})
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	wfID, unitID := envelope.Data.Workflow.ID, "wu-scoped"
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{query: `UPDATE workflows SET state='implementing',revision=7 WHERE id=?`, args: []any{wfID}},
		{query: `INSERT INTO plans VALUES(?,'summary','internal',1,'{}')`, args: []any{wfID}},
		{query: `INSERT INTO work_units VALUES(?,?,'scoped unit','internal/scoped','[]','[]',1,1,'reviewing',NULL,0,1)`, args: []any{unitID, wfID}},
	} {
		if _, err = database.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"agent", "brief", "--role", "pc2-explorer", "--workflow-id", wfID, "--json"},
		{"agent", "brief", "--role", "pc2-implementer", "--workflow-id", wfID, "--unit-id", unitID, "--json"},
		{"agent", "brief", "--role", "pc2-reviewer", "--workflow-id", wfID, "--unit-id", unitID, "--json"},
		{"agent", "brief", "--role", "pc2-reviewer", "--workflow-id", wfID, "--json"},
	} {
		if output, runErr := run(args...); runErr != nil || !bytes.Contains(output, []byte(`"ok":true`)) {
			t.Fatalf("valid scoped brief %v: %v\n%s", args, runErr, output)
		}
	}
	for _, args := range [][]string{
		{"agent", "brief", "--role", "pc2-explorer", "--json"},
		{"agent", "brief", "--role", "pc2-implementer", "--workflow-id", wfID, "--json"},
		{"agent", "brief", "--role", "pc2-reviewer", "--json"},
		{"agent", "brief", "--role", "pc2-explorer", "--workflow-id", "wf-missing", "--json"},
	} {
		if output, runErr := run(args...); runErr == nil || bytes.Contains(output, []byte(`"ok":true`)) {
			t.Fatalf("invalid scoped brief %v succeeded: %s", args, output)
		}
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
	if _, err := os.Stat(filepath.Join(home, ".codex", "pitcrew", "agent-contract.md")); !os.IsNotExist(err) {
		t.Fatalf("legacy agent contract was generated: %v", err)
	}
	roleNames := map[string]string{
		"daimon.toml": "daimon", "aion.toml": "aion",
		"pc2_explorer.toml": "pc2-explorer", "pc2_specifier.toml": "pc2-specifier",
		"pc2_designer.toml": "pc2-designer", "pc2_task_planner.toml": "pc2-task-planner",
		"pc2_implementer.toml": "pc2-implementer", "pc2_reviewer.toml": "pc2-reviewer",
		"pc2_sdd_initializer.toml": "pc2-sdd-initializer",
	}
	for _, entry := range entries {
		body, readErr := os.ReadFile(filepath.Join(registry, entry.Name()))
		role := roleNames[entry.Name()]
		brief := "pitcrew agent brief --role " + role
		briefAt, actionAt := bytes.Index(body, []byte(brief)), bytes.Index(body, []byte("before taking action"))
		if readErr != nil || briefAt < 0 || actionAt < 0 || briefAt >= actionAt {
			t.Fatalf("installed role %s omitted its pre-action bootstrap: %v", entry.Name(), readErr)
		}
		for _, forbidden := range []string{"THE FOUR MAXIMS", "Allowed workflow commands:", "correction budget", "release map", "Shared orchestration contract"} {
			if bytes.Contains(body, []byte(forbidden)) {
				t.Fatalf("installed role %s embeds manual content %q", entry.Name(), forbidden)
			}
		}
	}
	daimon, _ := os.ReadFile(filepath.Join(registry, "daimon.toml"))
	aion, _ := os.ReadFile(filepath.Join(registry, "aion.toml"))
	if !bytes.Contains(daimon, []byte("delegate only to aion")) {
		t.Fatal("Daimon target boundary drifted")
	}
	targets := "pc2_explorer, pc2_specifier, pc2_designer, pc2_task_planner, pc2_implementer, pc2_reviewer, pc2_sdd_initializer"
	if !bytes.Contains(aion, []byte("delegate only to "+targets)) {
		t.Fatal("Aion target boundary drifted")
	}
	for file := range roleNames {
		if file == "daimon.toml" || file == "aion.toml" {
			continue
		}
		body, _ := os.ReadFile(filepath.Join(registry, file))
		if !bytes.Contains(body, []byte("do not delegate; return to aion")) {
			t.Fatalf("specialist %s target boundary drifted", file)
		}
	}
	want := "Installed PitCrew agents for Codex in " + registry + "\n"
	if stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("stdout=%q, want %q; stderr=%q", stdout.String(), want, stderr.String())
	}
	aionBefore, _ := os.ReadFile(filepath.Join(registry, "aion.toml"))
	custom := filepath.Join(registry, "custom.toml")
	if err := os.WriteFile(custom, []byte("unrelated = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reinstall := exec.Command(binary, "install", "codex")
	reinstall.Dir = workingDirectory
	reinstall.Env = cmd.Env
	if output, err := reinstall.CombinedOutput(); err != nil {
		t.Fatalf("idempotent reinstall: %v\n%s", err, output)
	}
	aionAfter, _ := os.ReadFile(filepath.Join(registry, "aion.toml"))
	customAfter, _ := os.ReadFile(custom)
	if !bytes.Equal(aionBefore, aionAfter) || string(customAfter) != "unrelated = true\n" {
		t.Fatal("reinstall was not idempotent or changed an unrelated file")
	}
	if matches, err := filepath.Glob(filepath.Join(temporaryDirectory, "pitcrew-install-*")); err != nil || len(matches) != 0 {
		t.Fatalf("private extraction remains: %v (err=%v)", matches, err)
	}

	project, dataHome := t.TempDir(), filepath.Join(t.TempDir(), "data")
	if output, err := exec.Command("git", "-C", project, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("initialize clean project: %v: %s", err, output)
	}
	for _, call := range []struct {
		args []string
		want string
	}{
		{[]string{"agent", "brief", "--role", "daimon"}, "next_action: handoff to aion"},
		{[]string{"agent", "brief", "--role", "daimon", "--json"}, `"next_action":"handoff to aion"`},
		{[]string{"agent", "brief", "--role", "aion"}, "next_action: aion admit new delivery"},
		{[]string{"agent", "brief", "--role", "aion", "--json"}, `"next_action":"aion admit new delivery"`},
	} {
		brief := exec.Command(binary, call.args...)
		brief.Dir = project
		brief.Env = append(os.Environ(), "XDG_DATA_HOME="+dataHome)
		output, err := brief.CombinedOutput()
		if err != nil || !bytes.Contains(output, []byte(call.want)) {
			t.Fatalf("brief %v: %v\n%s", call.args, err, output)
		}
	}
	status, err := exec.Command("git", "-C", project, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
	if err != nil || len(status) != 0 {
		t.Fatalf("agent bootstrap mutated checkout: %v, %q", err, status)
	}
	if _, err := os.Stat(dataHome); !os.IsNotExist(err) {
		t.Fatalf("agent bootstrap created data home: %v", err)
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
