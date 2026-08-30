package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ErrRepositoryChanged indicates that a repository can no longer be bound to
// the resolved project and checkout supplied by the caller.
var ErrRepositoryChanged = errors.New("repository identity changed")

// RepositoryFingerprint identifies the exact clean or dirty repository state
// observed in one canonical checkout.
type RepositoryFingerprint struct {
	ProjectID    string
	CheckoutRoot string
	BaseRevision string
	HeadRevision string
	ResultDigest string
	Dirty        bool
	Staged       bool
	Unstaged     bool
	Untracked    bool
}

// CaptureRepositoryFingerprint captures a read-only identity for the result
// currently visible in project.CheckoutRoot. Git is invoked only with fixed
// subcommands against the already-resolved canonical checkout.
func CaptureRepositoryFingerprint(project Project) (RepositoryFingerprint, error) {
	if err := revalidateProject(project); err != nil {
		return RepositoryFingerprint{}, err
	}

	headOutput, err := runGit(project.CheckoutRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return RepositoryFingerprint{}, err
	}
	head := strings.TrimSuffix(string(headOutput), "\n")
	head = strings.TrimSuffix(head, "\r")
	if !validObjectID(head) {
		return RepositoryFingerprint{}, fmt.Errorf("capture Git HEAD: invalid object ID %q", head)
	}
	cached, err := runGit(project.CheckoutRoot, "diff", "--binary", "--no-ext-diff", "--no-textconv", "--cached", "--")
	if err != nil {
		return RepositoryFingerprint{}, err
	}
	worktree, err := runGit(project.CheckoutRoot, "diff", "--binary", "--no-ext-diff", "--no-textconv", "--")
	if err != nil {
		return RepositoryFingerprint{}, err
	}
	untracked, err := captureUntracked(project.CheckoutRoot)
	if err != nil {
		return RepositoryFingerprint{}, err
	}
	if err := revalidateProject(project); err != nil {
		return RepositoryFingerprint{}, err
	}
	finalHeadOutput, err := runGit(project.CheckoutRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return RepositoryFingerprint{}, err
	}
	finalHead := strings.TrimSuffix(string(finalHeadOutput), "\n")
	finalHead = strings.TrimSuffix(finalHead, "\r")
	if finalHead != head {
		return RepositoryFingerprint{}, fmt.Errorf("%w: Git HEAD changed during capture", ErrRepositoryChanged)
	}

	digest := sha256.New()
	for _, field := range []struct {
		name  string
		value []byte
	}{
		{name: "project", value: []byte(project.ID)},
		{name: "checkout", value: []byte(project.CheckoutRoot)},
		{name: "head", value: []byte(head)},
		{name: "cached-diff", value: cached},
		{name: "worktree-diff", value: worktree},
		{name: "untracked", value: untracked.encoding},
	} {
		writeDigestField(digest, field.name, field.value)
	}

	staged := len(cached) != 0
	unstaged := len(worktree) != 0
	return RepositoryFingerprint{
		ProjectID:    project.ID,
		CheckoutRoot: project.CheckoutRoot,
		BaseRevision: head,
		HeadRevision: head,
		ResultDigest: hex.EncodeToString(digest.Sum(nil)),
		Dirty:        staged || unstaged || untracked.present,
		Staged:       staged,
		Unstaged:     unstaged,
		Untracked:    untracked.present,
	}, nil
}

func runGit(checkoutRoot string, args ...string) ([]byte, error) {
	argv := make([]string, 0, len(args)+8)
	argv = append(argv,
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"-c", "diff.external=",
		"-C", checkoutRoot,
	)
	argv = append(argv, args...)
	command := exec.Command("git", argv...)
	command.Env = closedGitEnvironment()
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("git %s failed: %s", args[0], strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("run git %s: %w", args[0], err)
	}
	return output, nil
}

func revalidateProject(project Project) error {
	if !filepath.IsAbs(project.CheckoutRoot) {
		return fmt.Errorf("%w: checkout root must be absolute", ErrRepositoryChanged)
	}
	current, err := Resolve(project.CheckoutRoot)
	if err != nil {
		return fmt.Errorf("%w: resolve checkout: %v", ErrRepositoryChanged, err)
	}
	if current.ID != project.ID || current.CheckoutRoot != project.CheckoutRoot || current.CommonDir != project.CommonDir {
		return fmt.Errorf("%w: checkout no longer matches resolved project", ErrRepositoryChanged)
	}
	return nil
}

func closedGitEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_") || name == "LC_ALL" {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_OPTIONAL_LOCKS=0",
		"LC_ALL=C",
	)
}

type untrackedState struct {
	present  bool
	encoding []byte
}

func captureUntracked(checkoutRoot string) (untrackedState, error) {
	output, err := runGit(checkoutRoot, "ls-files", "--others", "--exclude-standard", "-z", "--")
	if err != nil {
		return untrackedState{}, err
	}
	if len(output) == 0 {
		return untrackedState{}, nil
	}
	if output[len(output)-1] != 0 {
		return untrackedState{}, errors.New("capture untracked files: Git returned a malformed path list")
	}
	rawPaths := bytes.Split(output[:len(output)-1], []byte{0})
	paths := make([]string, len(rawPaths))
	for i, raw := range rawPaths {
		paths[i] = string(raw)
	}
	sort.Strings(paths)
	root, err := os.OpenRoot(checkoutRoot)
	if err != nil {
		return untrackedState{}, fmt.Errorf("confine untracked capture: %w", err)
	}
	defer root.Close()

	var encoding bytes.Buffer
	for _, relative := range paths {
		if !filepath.IsLocal(relative) || filepath.Clean(relative) != relative {
			return untrackedState{}, fmt.Errorf("capture untracked files: unsafe path %q", relative)
		}
		info, err := root.Lstat(relative)
		if err != nil {
			return untrackedState{}, fmt.Errorf("capture untracked file %q: %w", relative, err)
		}
		var kind string
		var content []byte
		switch {
		case info.Mode().IsRegular():
			kind = "file"
			var file *os.File
			file, err = root.Open(relative)
			if err == nil {
				var opened os.FileInfo
				opened, err = file.Stat()
				if err == nil && (!opened.Mode().IsRegular() || !os.SameFile(info, opened)) {
					err = errors.New("file changed while opening")
				}
				if err == nil {
					content, err = io.ReadAll(file)
				}
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
			}
		case info.Mode()&os.ModeSymlink != 0:
			kind = "symlink"
			var target string
			target, err = root.Readlink(relative)
			content = []byte(target)
		default:
			return untrackedState{}, fmt.Errorf("capture untracked file %q: unsupported type %s", relative, info.Mode().Type())
		}
		if err != nil {
			return untrackedState{}, fmt.Errorf("capture untracked file %q: %w", relative, err)
		}
		contentDigest := sha256.Sum256(content)
		writeDigestField(&encoding, "path", []byte(relative))
		writeDigestField(&encoding, "type", []byte(kind))
		writeDigestField(&encoding, "content-sha256", []byte(hex.EncodeToString(contentDigest[:])))
	}
	return untrackedState{present: true, encoding: encoding.Bytes()}, nil
}

type digestWriter interface {
	Write([]byte) (int, error)
}

func writeDigestField(destination digestWriter, name string, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(name)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write([]byte(name))
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded)*2 == len(value)
}
