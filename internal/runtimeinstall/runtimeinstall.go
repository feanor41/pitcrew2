// Package runtimeinstall runs the canonical embedded runtime installer.
package runtimeinstall

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	pitcrew "github.com/fmazzalomo/pitcrew"
)

// Dependencies are the process and filesystem seams used by Run.
type Dependencies struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Cwd    string
	Env    []string

	MkdirTemp func(string, string) (string, error)
	RemoveAll func(string) error
	Command   func(string, ...string) *exec.Cmd
}

// Run privately extracts and executes the embedded installer for target. It
// returns the installer's exit status unchanged when the process starts.
func Run(target string, deps Dependencies) (code int) {
	if !supported(target) {
		writeError(deps.Stderr, fmt.Errorf("unsupported target %q", target))
		return 2
	}
	if deps.Stdout == nil {
		deps.Stdout = io.Discard
	}
	if deps.Stderr == nil {
		deps.Stderr = io.Discard
	}
	if deps.Cwd == "" {
		cwd, err := os.Getwd()
		if err != nil {
			writeError(deps.Stderr, err)
			return 1
		}
		deps.Cwd = cwd
	}
	if deps.Env == nil {
		deps.Env = os.Environ()
	}
	if deps.MkdirTemp == nil {
		deps.MkdirTemp = os.MkdirTemp
	}
	if deps.RemoveAll == nil {
		deps.RemoveAll = os.RemoveAll
	}
	if deps.Command == nil {
		deps.Command = exec.Command
	}

	root, err := deps.MkdirTemp("", "pitcrew-install-")
	if err != nil {
		writeError(deps.Stderr, err)
		return 1
	}
	var installerStdout bytes.Buffer
	defer func() {
		if err := deps.RemoveAll(root); err != nil {
			writeError(deps.Stderr, fmt.Errorf("remove private extraction: %w", err))
			if code == 0 {
				code = 1
			}
			return
		}
		if code == 0 {
			_, _ = io.Copy(deps.Stdout, &installerStdout)
		}
	}()
	if err := os.Chmod(root, 0o700); err != nil {
		writeError(deps.Stderr, fmt.Errorf("secure private extraction: %w", err))
		return 1
	}

	scriptsDir := filepath.Join(root, "scripts")
	if err := os.Mkdir(scriptsDir, 0o700); err != nil {
		writeError(deps.Stderr, fmt.Errorf("create private installer directory: %w", err))
		return 1
	}
	if err := os.Chmod(scriptsDir, 0o700); err != nil {
		writeError(deps.Stderr, fmt.Errorf("secure private installer directory: %w", err))
		return 1
	}
	installerPath := filepath.Join(scriptsDir, "install-templates.sh")
	if err := writePrivateFile(installerPath, pitcrew.InstallerScript, 0o700); err != nil {
		writeError(deps.Stderr, fmt.Errorf("extract installer: %w", err))
		return 1
	}
	if err := writePrivateFile(filepath.Join(root, "MAXIMS.md"), []byte(pitcrew.MaximsText), 0o600); err != nil {
		writeError(deps.Stderr, fmt.Errorf("extract maxims: %w", err))
		return 1
	}

	cmd := deps.Command("/bin/sh", installerPath, target)
	cmd.Stdin = deps.Stdin
	cmd.Stdout = &installerStdout
	cmd.Stderr = deps.Stderr
	cmd.Dir = deps.Cwd
	cmd.Env = deps.Env
	if err := cmd.Run(); err != nil {
		_, _ = io.Copy(deps.Stdout, &installerStdout)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
			return exitErr.ExitCode()
		}
		writeError(deps.Stderr, fmt.Errorf("execute installer: %w", err))
		return 1
	}
	return 0
}

func supported(target string) bool {
	switch target {
	case "codex", "opencode", "claude", "pi":
		return true
	default:
		return false
	}
}

func writePrivateFile(path string, content []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, content, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func writeError(w io.Writer, err error) {
	if w != nil {
		_, _ = fmt.Fprintf(w, "pitcrew install: %v\n", err)
	}
}
