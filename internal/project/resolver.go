// Package project resolves one durable identity for a Git repository and all
// of its linked worktrees.
package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var (
	// ErrNotRepository indicates that no enclosing Git checkout was found.
	ErrNotRepository = errors.New("working directory is not inside a Git checkout")
	// ErrUnsafeMetadata indicates malformed or unsafe Git administrative data.
	ErrUnsafeMetadata = errors.New("unsafe Git metadata")
)

// Project is the resolved identity of one Git common directory.
type Project struct {
	ID           string
	CheckoutRoot string
	CommonDir    string
}

// Resolver resolves repository identity without invoking Git or mutating the
// filesystem.
type Resolver struct{}

// Resolve is a convenience wrapper around Resolver.Resolve.
func Resolve(workingDir string) (Project, error) {
	return (Resolver{}).Resolve(workingDir)
}

// Resolve finds the enclosing checkout, validates its Git administrative
// metadata, and hashes the canonical common-directory path.
func (Resolver) Resolve(workingDir string) (Project, error) {
	checkout, dotGit, info, err := findCheckout(workingDir)
	if err != nil {
		return Project{}, err
	}

	var common string
	if info.IsDir() {
		common, err = canonicalSafeDirectory(dotGit)
	} else if info.Mode().IsRegular() {
		common, err = resolveLinkedCommonDir(checkout, dotGit)
	} else {
		err = fmt.Errorf("%w: %s is not a regular file or directory", ErrUnsafeMetadata, dotGit)
	}
	if err != nil {
		return Project{}, err
	}

	digest := sha256.Sum256([]byte(common))
	return Project{
		ID:           hex.EncodeToString(digest[:]),
		CheckoutRoot: checkout,
		CommonDir:    common,
	}, nil
}

func findCheckout(workingDir string) (string, string, os.FileInfo, error) {
	if strings.TrimSpace(workingDir) == "" {
		return "", "", nil, fmt.Errorf("%w: working directory is required", ErrNotRepository)
	}
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve working directory: %w", err)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve working directory: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", nil, fmt.Errorf("inspect working directory: %w", err)
	}
	if !info.IsDir() {
		return "", "", nil, fmt.Errorf("%w: working path is not a directory", ErrNotRepository)
	}

	for current := abs; ; current = filepath.Dir(current) {
		dotGit := filepath.Join(current, ".git")
		info, err := os.Lstat(dotGit)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				return "", "", nil, fmt.Errorf("%w: %s is a symlink", ErrUnsafeMetadata, dotGit)
			}
			if err := requireSafeOwnerAndMode(dotGit, info); err != nil {
				return "", "", nil, err
			}
			return current, dotGit, info, nil
		case os.IsNotExist(err):
			// Keep walking.
		default:
			return "", "", nil, fmt.Errorf("inspect %s: %w", dotGit, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return "", "", nil, ErrNotRepository
}

func resolveLinkedCommonDir(checkout, dotGit string) (string, error) {
	pointer, err := readMetadataLine(dotGit)
	if err != nil {
		return "", err
	}
	const prefix = "gitdir: "
	if !strings.HasPrefix(pointer, prefix) || strings.TrimSpace(strings.TrimPrefix(pointer, prefix)) == "" {
		return "", fmt.Errorf("%w: malformed %s", ErrUnsafeMetadata, dotGit)
	}
	adminPath := strings.TrimSpace(strings.TrimPrefix(pointer, prefix))
	if !filepath.IsAbs(adminPath) {
		adminPath = filepath.Join(checkout, adminPath)
	}
	adminPath = filepath.Clean(adminPath)
	admin, err := canonicalSafeDirectory(adminPath)
	if err != nil {
		return "", fmt.Errorf("%w: invalid linked-worktree admin directory: %v", ErrUnsafeMetadata, err)
	}

	commonValue, err := readMetadataLine(filepath.Join(admin, "commondir"))
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(commonValue) || commonValue == "" || strings.ContainsRune(commonValue, '\x00') {
		return "", fmt.Errorf("%w: commondir must be a relative path", ErrUnsafeMetadata)
	}
	common, err := canonicalSafeDirectory(filepath.Join(admin, commonValue))
	if err != nil {
		return "", fmt.Errorf("%w: invalid common directory: %v", ErrUnsafeMetadata, err)
	}

	rel, err := filepath.Rel(common, admin)
	if err != nil {
		return "", fmt.Errorf("%w: compare common and admin directories: %v", ErrUnsafeMetadata, err)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || parts[0] != "worktrees" || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return "", fmt.Errorf("%w: linked admin directory escapes common-dir worktrees", ErrUnsafeMetadata)
	}
	if _, err := canonicalSafeDirectory(filepath.Join(common, "worktrees")); err != nil {
		return "", fmt.Errorf("%w: unsafe worktrees metadata directory: %v", ErrUnsafeMetadata, err)
	}

	backlink, err := readMetadataLine(filepath.Join(admin, "gitdir"))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(backlink) {
		backlink = filepath.Join(admin, backlink)
	}
	backlink, err = filepath.EvalSymlinks(filepath.Clean(backlink))
	if err != nil {
		return "", fmt.Errorf("%w: invalid linked-worktree backlink: %v", ErrUnsafeMetadata, err)
	}
	canonicalDotGit, err := filepath.EvalSymlinks(dotGit)
	if err != nil {
		return "", fmt.Errorf("%w: resolve checkout metadata: %v", ErrUnsafeMetadata, err)
	}
	if backlink != canonicalDotGit {
		return "", fmt.Errorf("%w: linked-worktree backlink does not name %s", ErrUnsafeMetadata, dotGit)
	}
	return common, nil
}

func canonicalSafeDirectory(path string) (string, error) {
	clean, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("%s is not a non-symlink directory", clean)
	}
	if err := requireSafeOwnerAndMode(clean, info); err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", err
	}
	return filepath.Clean(canonical), nil
}

func readMetadataLine(path string) (string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("%w: inspect %s: %v", ErrUnsafeMetadata, path, err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a regular non-symlink file", ErrUnsafeMetadata, path)
	}
	if err := requireSafeOwnerAndMode(path, before); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%w: open %s: %v", ErrUnsafeMetadata, path, err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("%w: inspect open %s: %v", ErrUnsafeMetadata, path, err)
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s changed while opening", ErrUnsafeMetadata, path)
	}
	content := make([]byte, 4097)
	n, err := file.Read(content)
	if err != nil {
		return "", fmt.Errorf("%w: read %s: %v", ErrUnsafeMetadata, path, err)
	}
	if n == len(content) {
		return "", fmt.Errorf("%w: %s is too large", ErrUnsafeMetadata, path)
	}
	value := string(content[:n])
	if strings.ContainsRune(value, '\x00') || strings.Count(value, "\n") > 1 {
		return "", fmt.Errorf("%w: malformed %s", ErrUnsafeMetadata, path)
	}
	value = strings.TrimSuffix(value, "\n")
	value = strings.TrimSuffix(value, "\r")
	if value == "" {
		return "", fmt.Errorf("%w: empty %s", ErrUnsafeMetadata, path)
	}
	return value, nil
}

func requireSafeOwnerAndMode(path string, info os.FileInfo) error {
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: %s is group- or world-writable", ErrUnsafeMetadata, path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("%w: %s is not owned by the current user", ErrUnsafeMetadata, path)
	}
	return nil
}
