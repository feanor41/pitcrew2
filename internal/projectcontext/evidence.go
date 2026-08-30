package projectcontext

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrEvidenceOutsideCheckout = errors.New("project context evidence escapes active checkout")

func ConfineEvidence(checkoutRoot string, record Record) error {
	root, err := filepath.Abs(filepath.Clean(checkoutRoot))
	if err != nil || !filepath.IsAbs(root) {
		return evidenceError("invalid checkout root", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return evidenceError("resolve checkout root", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return evidenceError("checkout root is not an accessible directory", err)
	}
	for _, facts := range record.Facts {
		for _, fact := range facts {
			if fact.Evidence.Path == "" {
				continue
			}
			candidate := filepath.Join(root, filepath.FromSlash(fact.Evidence.Path))
			resolved, err := resolveExistingComponents(candidate)
			if err != nil {
				return evidenceError("resolve "+fact.Evidence.Path, err)
			}
			relative, err := filepath.Rel(root, resolved)
			if err != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return evidenceError(fact.Evidence.Path, err)
			}
		}
	}
	return nil
}

func resolveExistingComponents(candidate string) (string, error) {
	current := filepath.Clean(candidate)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func evidenceError(detail string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrEvidenceOutsideCheckout, detail)
	}
	return fmt.Errorf("%w: %s: %v", ErrEvidenceOutsideCheckout, detail, err)
}
