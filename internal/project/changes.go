package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ChangeBaseline is the immutable repository identity captured for one attempt.
type ChangeBaseline struct {
	ProjectID    string
	CheckoutRoot string
	BaseRevision string
	ResultDigest string
}

// ChangeMeasurement is an auditable count for one stable repository state.
type ChangeMeasurement struct {
	Additions    int
	Deletions    int
	ChangedLines int
	ResultDigest string
}

func CaptureChangeBaseline(workingDir string) (ChangeBaseline, error) {
	resolved, err := Resolve(workingDir)
	if err != nil {
		return ChangeBaseline{}, err
	}
	fingerprint, err := CaptureRepositoryFingerprint(resolved)
	if err != nil {
		return ChangeBaseline{}, err
	}
	return ChangeBaseline{resolved.ID, resolved.CheckoutRoot, fingerprint.HeadRevision, fingerprint.ResultDigest}, nil
}

// MeasureChangedLines counts textual additions plus deletions from baseline to
// the complete current checkout state, including eligible untracked files.
func MeasureChangedLines(baseline ChangeBaseline, scopes []string) (ChangeMeasurement, error) {
	resolved, err := Resolve(baseline.CheckoutRoot)
	if err != nil || resolved.ID != baseline.ProjectID || resolved.CheckoutRoot != baseline.CheckoutRoot {
		return ChangeMeasurement{}, fmt.Errorf("measure changed lines: %w", ErrRepositoryChanged)
	}
	if decoded, decodeErr := hex.DecodeString(baseline.ResultDigest); decodeErr != nil || len(decoded) != sha256.Size {
		return ChangeMeasurement{}, errors.New("measure changed lines: corrupt baseline digest")
	}
	if len(scopes) == 0 {
		return ChangeMeasurement{}, errors.New("measure changed lines: at least one scope is required")
	}
	canonicalScopes, err := NormalizeChangeScopes(scopes)
	if err != nil {
		return ChangeMeasurement{}, err
	}
	pathspecs := make([]string, len(canonicalScopes))
	for index, scope := range canonicalScopes {
		pathspecs[index] = ":(top,literal)" + scope
	}
	if _, err = runGit(resolved.CheckoutRoot, "cat-file", "-e", baseline.BaseRevision+"^{commit}"); err != nil {
		return ChangeMeasurement{}, fmt.Errorf("measure changed lines: invalid baseline revision: %w", err)
	}
	first, err := measureChangedLinesOnce(resolved.CheckoutRoot, baseline.BaseRevision, canonicalScopes, pathspecs)
	if err != nil {
		return ChangeMeasurement{}, err
	}
	second, err := measureChangedLinesOnce(resolved.CheckoutRoot, baseline.BaseRevision, canonicalScopes, pathspecs)
	if err != nil || second != first {
		return ChangeMeasurement{}, fmt.Errorf("measure changed lines: %w during capture", ErrRepositoryChanged)
	}
	return first, nil
}

func measureChangedLinesOnce(root, revision string, canonicalScopes, pathspecs []string) (ChangeMeasurement, error) {
	args := append([]string{"diff", "--numstat", "-z", "--find-renames", revision, "--"}, pathspecs...)
	numstat, err := runGit(root, args...)
	if err != nil {
		return ChangeMeasurement{}, err
	}
	seen := make(map[string]struct{})
	additions, deletions, err := parseNumstat(numstat, canonicalScopes, seen)
	if err != nil {
		return ChangeMeasurement{}, err
	}
	stageArgs := append([]string{"ls-files", "--stage", "-z", "--"}, pathspecs...)
	stages, err := runGit(root, stageArgs...)
	if err != nil {
		return ChangeMeasurement{}, err
	}
	for _, record := range bytes.Split(stages, []byte{0}) {
		if bytes.HasPrefix(record, []byte("160000 ")) || bytes.HasPrefix(record, []byte("120000 ")) {
			return ChangeMeasurement{}, errors.New("measure changed lines: submodule content is indeterminate")
		}
	}
	untrackedArgs := append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, pathspecs...)
	untracked, err := runGit(root, untrackedArgs...)
	if err != nil {
		return ChangeMeasurement{}, err
	}
	for _, raw := range bytes.Split(untracked, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		if err := acceptMeasuredPath(raw, canonicalScopes, seen); err != nil {
			return ChangeMeasurement{}, err
		}
		name := string(raw)
		full := filepath.Join(root, filepath.FromSlash(name))
		if !strings.HasPrefix(full, root+string(filepath.Separator)) {
			return ChangeMeasurement{}, errors.New("measure changed lines: unsafe untracked path")
		}
		info, statErr := os.Lstat(full)
		if statErr != nil || !info.Mode().IsRegular() {
			return ChangeMeasurement{}, errors.New("measure changed lines: unsafe or disappearing untracked file")
		}
		content, readErr := os.ReadFile(full)
		if readErr != nil {
			return ChangeMeasurement{}, fmt.Errorf("measure changed lines: read untracked file: %w", readErr)
		}
		if bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content) {
			return ChangeMeasurement{}, fmt.Errorf("measure changed lines: binary untracked file %q", name)
		}
		lines := bytes.Count(content, []byte{'\n'})
		if len(content) > 0 && content[len(content)-1] != '\n' {
			lines, err = checkedChangedLineAdd(lines, 1)
			if err != nil {
				return ChangeMeasurement{}, err
			}
		}
		additions, err = checkedChangedLineAdd(additions, lines)
		if err != nil {
			return ChangeMeasurement{}, err
		}
	}
	patchArgs := append([]string{"diff", "--binary", "--no-ext-diff", revision, "--"}, pathspecs...)
	patch, err := runGit(root, patchArgs...)
	if err != nil {
		return ChangeMeasurement{}, err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(revision))
	for _, scope := range canonicalScopes {
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(scope))
	}
	_, _ = digest.Write(numstat)
	_, _ = digest.Write(patch)
	for _, raw := range bytes.Split(untracked, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(string(raw))))
		if readErr != nil {
			return ChangeMeasurement{}, fmt.Errorf("measure changed lines: reread untracked file: %w", readErr)
		}
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(raw)
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(content)
	}
	total, err := checkedChangedLineAdd(additions, deletions)
	if err != nil {
		return ChangeMeasurement{}, err
	}
	return ChangeMeasurement{Additions: additions, Deletions: deletions, ChangedLines: total, ResultDigest: hex.EncodeToString(digest.Sum(nil))}, nil
}

// NormalizeChangeScopes validates, orders, de-duplicates, and removes scopes
// already covered by an accepted parent prefix.
func NormalizeChangeScopes(scopes []string) ([]string, error) {
	canonical := append([]string(nil), scopes...)
	for _, scope := range canonical {
		if scope == "" || strings.ContainsAny(scope, "\x00\\") || path.IsAbs(scope) || path.Clean(scope) != scope || scope == "." || scope == ".." || strings.HasPrefix(scope, "../") {
			return nil, fmt.Errorf("measure changed lines: unsafe scope %q", scope)
		}
	}
	sort.Strings(canonical)
	normalized := canonical[:0]
	for _, scope := range canonical {
		covered := false
		for _, accepted := range normalized {
			covered = covered || scope == accepted || strings.HasPrefix(scope, accepted+"/")
		}
		if covered {
			continue
		}
		normalized = append(normalized, scope)
	}
	return normalized, nil
}

func acceptMeasuredPath(raw []byte, scopes []string, seen map[string]struct{}) error {
	name := string(raw)
	if name == "" || !utf8.Valid(raw) || strings.Contains(name, "\\") || path.IsAbs(name) || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return errors.New("measure changed lines: unsafe Git path")
	}
	contained := false
	for _, scope := range scopes {
		contained = contained || name == scope || strings.HasPrefix(name, scope+"/")
	}
	if !contained {
		return errors.New("measure changed lines: Git path outside requested scopes")
	}
	if _, duplicate := seen[name]; duplicate {
		return errors.New("measure changed lines: duplicate Git path")
	}
	seen[name] = struct{}{}
	return nil
}

func parseNumstat(data []byte, scopes []string, seen map[string]struct{}) (int, int, error) {
	if len(data) == 0 {
		return 0, 0, nil
	}
	if data[len(data)-1] != 0 {
		return 0, 0, errors.New("measure changed lines: malformed Git numstat framing")
	}
	additions, deletions := 0, 0
	var err error
	parts := bytes.Split(data[:len(data)-1], []byte{0})
	for index := 0; index < len(parts); index++ {
		fields := bytes.SplitN(parts[index], []byte{'\t'}, 3)
		if len(fields) != 3 || bytes.Equal(fields[0], []byte("-")) || bytes.Equal(fields[1], []byte("-")) {
			return 0, 0, errors.New("measure changed lines: binary or malformed Git numstat")
		}
		added, addErr := strconv.Atoi(string(fields[0]))
		deleted, deleteErr := strconv.Atoi(string(fields[1]))
		if addErr != nil || deleteErr != nil || added < 0 || deleted < 0 {
			return 0, 0, errors.New("measure changed lines: malformed Git numstat")
		}
		if len(fields[2]) == 0 {
			if index+2 >= len(parts) || len(parts[index+1]) == 0 || len(parts[index+2]) == 0 {
				return 0, 0, errors.New("measure changed lines: malformed Git rename")
			}
			if err := acceptMeasuredPath(parts[index+1], scopes, seen); err != nil {
				return 0, 0, err
			}
			if err := acceptMeasuredPath(parts[index+2], scopes, seen); err != nil {
				return 0, 0, err
			}
			index += 2
		} else if err := acceptMeasuredPath(fields[2], scopes, seen); err != nil {
			return 0, 0, err
		}
		additions, err = checkedChangedLineAdd(additions, added)
		if err != nil {
			return 0, 0, err
		}
		deletions, err = checkedChangedLineAdd(deletions, deleted)
		if err != nil {
			return 0, 0, err
		}
	}
	return additions, deletions, nil
}

func checkedChangedLineAdd(left, right int) (int, error) {
	if right < 0 || left > int(^uint(0)>>1)-right {
		return 0, errors.New("measure changed lines: arithmetic overflow")
	}
	return left + right, nil
}
