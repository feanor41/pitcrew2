package cli

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestInitializerBriefStartsWithExecutableReadOnlyInspectionWithoutCreatingState(t *testing.T) {
	repository := t.TempDir()
	dataHome := t.TempDir()
	if output, err := exec.Command("git", "-C", repository, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	beforeRepository := treeSnapshot(t, repository)
	beforeData := treeSnapshot(t, dataHome)

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "pitcrew")
	build := exec.Command("go", "build", "-o", binary, "./cmd/pitcrew")
	build.Dir = filepath.Clean(filepath.Join(workingDirectory, "../.."))
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build pitcrew: %v: %s", buildErr, output)
	}
	run := func(args ...string) string {
		command := exec.Command(binary, args...)
		command.Dir = repository
		command.Env = append(os.Environ(), "XDG_DATA_HOME="+dataHome)
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			t.Fatalf("pitcrew %v: %v: %s", args, runErr, output)
		}
		return string(output)
	}
	for _, args := range [][]string{
		{"agent", "brief", "--role", "pc2-sdd-initializer"},
		{"agent", "brief", "--role", "pc2-sdd-initializer", "--json"},
	} {
		output := run(args...)
		if !strings.Contains(output, "context inspect") || !strings.Contains(output, "initializer") {
			t.Fatalf("initializer brief %v=%s", args, output)
		}
	}
	if output := run("context", "inspect"); !strings.Contains(output, `"status":"missing"`) {
		t.Fatalf("initializer action did not execute read-only inspection: %s", output)
	}
	if after := treeSnapshot(t, repository); strings.Join(beforeRepository, "\n") != strings.Join(after, "\n") {
		t.Fatalf("initializer activation mutated repository: before=%v after=%v", beforeRepository, after)
	}
	if after := treeSnapshot(t, dataHome); strings.Join(beforeData, "\n") != strings.Join(after, "\n") {
		t.Fatalf("initializer activation mutated data home: before=%v after=%v", beforeData, after)
	}
}

func treeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var result []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." {
			return err
		}
		if info.IsDir() {
			result = append(result, relative+"/")
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		sum := sha256.New()
		if _, err = io.Copy(sum, file); err != nil {
			return err
		}
		result = append(result, fmt.Sprintf("%s:%x", relative, sum.Sum(nil)))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(result)
	return result
}
