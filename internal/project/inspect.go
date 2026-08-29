package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// Inspection is the non-mutating view used by project inspection and routing.
type Inspection struct {
	Project                Project
	Paths                  Paths
	Initialized            bool
	RepositoryMoveBoundary bool
	Legacy                 LegacyDiscovery
}

// Inspect resolves identity, central paths, and legacy history without
// creating storage or opening SQLite.
func Inspect(workingDir, dataHome string) (Inspection, error) {
	resolved, err := Resolve(workingDir)
	if err != nil {
		return Inspection{}, err
	}
	paths, err := DerivePaths(dataHome, resolved.ID)
	if err != nil {
		return Inspection{}, err
	}
	if err := rejectSymlinkParents(paths.ProjectRoot); err != nil {
		return Inspection{}, err
	}
	initialized := false
	if info, err := os.Lstat(paths.StatePath); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
			return Inspection{}, fmt.Errorf("%w: unsafe central state %s", ErrUnsafePath, paths.StatePath)
		}
		if err := requireSafeOwnerAndMode(paths.StatePath, info); err != nil {
			return Inspection{}, err
		}
		initialized = true
	} else if !os.IsNotExist(err) {
		return Inspection{}, fmt.Errorf("inspect central state: %w", err)
	}
	legacy, err := DiscoverLegacy(resolved)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{Project: resolved, Paths: paths, Initialized: initialized, Legacy: legacy}, nil
}

func rejectSymlinkParents(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return fmt.Errorf("%w: unsafe parent component %s", ErrUnsafePath, current)
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%w: inspect parent %s: %v", ErrUnsafePath, current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
