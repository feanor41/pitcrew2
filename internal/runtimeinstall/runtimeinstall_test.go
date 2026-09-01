package runtimeinstall

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractsCanonicalFilesPrivatelyAndPreservesProcessContract(t *testing.T) {
	canonicalInstaller, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install-templates.sh"))
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	cwd := t.TempDir()
	var extractedRoot string
	var stdin bytes.Buffer
	stdin.WriteString("input")
	var stdout, stderr bytes.Buffer
	env := append(os.Environ(), "GO_WANT_RUNTIMEINSTALL_HELPER=1")
	code := Run("opencode", Dependencies{
		Stdin: &stdin, Stdout: &stdout, Stderr: &stderr, Cwd: cwd, Env: env,
		MkdirTemp: func(string, string) (string, error) {
			extractedRoot = filepath.Join(base, "private")
			return extractedRoot, os.Mkdir(extractedRoot, 0o700)
		},
		Command: func(name string, args ...string) *exec.Cmd {
			if name != "/bin/sh" || len(args) != 2 || args[0] != filepath.Join(extractedRoot, "scripts", "install-templates.sh") || args[1] != "opencode" {
				t.Fatalf("command = %q %q", name, args)
			}
			assertFile := func(path string, want []byte, mode os.FileMode) {
				t.Helper()
				got, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(got, want) {
					t.Fatalf("read %s: %v; canonical=%v", path, readErr, bytes.Equal(got, want))
				}
				info, statErr := os.Stat(path)
				if statErr != nil || info.Mode().Perm() != mode {
					t.Fatalf("mode %s = %v, %v; want %v", path, info.Mode().Perm(), statErr, mode)
				}
			}
			assertFile(args[0], canonicalInstaller, 0o700)
			if _, err := os.Stat(filepath.Join(extractedRoot, "MAXIMS.md")); !os.IsNotExist(err) {
				t.Fatalf("unused MAXIMS.md was extracted: %v", err)
			}
			info, statErr := os.Stat(extractedRoot)
			if statErr != nil || info.Mode().Perm() != 0o700 {
				t.Fatalf("private directory mode = %v, %v", info.Mode().Perm(), statErr)
			}
			return exec.Command(os.Args[0], "-test.run=TestRuntimeInstallHelperProcess")
		},
	})
	if code != 23 || stdout.String() != "stdout:"+cwd+":input" || stderr.String() != "stderr" {
		t.Fatalf("result = %d, %q, %q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(extractedRoot); !os.IsNotExist(err) {
		t.Fatalf("extraction directory survived: %v", err)
	}
}

func TestRepositoryAgentGuideIsOnlyTheDaimonBootstrap(t *testing.T) {
	want := []byte("Work in this repository is performed by interacting directly with the `daimon` agent by default.\n")
	got, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("AGENTS.md = %q, want exact one-line bootstrap", got)
	}
}

func TestRuntimeInstallHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_RUNTIMEINSTALL_HELPER") != "1" {
		return
	}
	input, _ := os.ReadFile("/dev/stdin")
	wd, _ := os.Getwd()
	_, _ = os.Stdout.WriteString("stdout:" + wd + ":" + string(input))
	_, _ = os.Stderr.WriteString("stderr")
	os.Exit(23)
}

func TestRunSuppressesInstallerSuccessOutputWhenCleanupFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run("codex", Dependencies{
		Stdout: &stdout,
		Stderr: &stderr,
		Env:    append(os.Environ(), "GO_WANT_RUNTIMEINSTALL_SUCCESS_HELPER=1"),
		RemoveAll: func(path string) error {
			_ = os.RemoveAll(path)
			return errors.New("cleanup failed")
		},
		Command: func(string, ...string) *exec.Cmd {
			return exec.Command(os.Args[0], "-test.run=TestRuntimeInstallSuccessHelperProcess")
		},
	})

	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no success output", stdout.String())
	}
	if !strings.Contains(stderr.String(), "remove private extraction: cleanup failed") {
		t.Fatalf("stderr = %q, want cleanup diagnostic", stderr.String())
	}
}

func TestRuntimeInstallSuccessHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_RUNTIMEINSTALL_SUCCESS_HELPER") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString("installer success\n")
	os.Exit(0)
}

func TestRunReportsExtractionAndExecutionFailures(t *testing.T) {
	var stderr bytes.Buffer
	code := Run("codex", Dependencies{Stderr: &stderr, MkdirTemp: func(string, string) (string, error) {
		return "", errors.New("no private directory")
	}})
	if code != 1 || !strings.Contains(stderr.String(), "no private directory") {
		t.Fatalf("setup failure = %d, %q", code, stderr.String())
	}

	stderr.Reset()
	code = Run("pi", Dependencies{Stderr: &stderr, Command: func(string, ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "missing"))
	}})
	if code != 1 || stderr.Len() == 0 {
		t.Fatalf("execution failure = %d, %q", code, stderr.String())
	}
}
