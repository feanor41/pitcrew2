package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

var ErrMigrationRequired = errors.New("legacy PitCrew history requires explicit consolidation")

type LegacyCandidate struct {
	ID           string `json:"id"`
	CheckoutRoot string `json:"checkout_root"`
	StatePath    string `json:"state_path"`
}

type LegacyDiagnostic struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type LegacyDiscovery struct {
	Candidates     []LegacyCandidate  `json:"candidates"`
	Diagnostics    []LegacyDiagnostic `json:"diagnostics"`
	CandidateSetID string             `json:"candidate_set_id"`
}

// DiscoverLegacy inspects only the main checkout and reciprocal linked
// worktree records owned by the resolved common directory.
func DiscoverLegacy(resolved Project) (LegacyDiscovery, error) {
	result := LegacyDiscovery{}
	checkouts := map[string]bool{filepath.Dir(resolved.CommonDir): true}
	worktrees := filepath.Join(resolved.CommonDir, "worktrees")
	if info, err := os.Lstat(worktrees); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			result.Diagnostics = append(result.Diagnostics, LegacyDiagnostic{worktrees, "unsafe worktree metadata directory"})
		} else if err := requireSafeOwnerAndMode(worktrees, info); err != nil {
			result.Diagnostics = append(result.Diagnostics, LegacyDiagnostic{worktrees, err.Error()})
		} else if entries, err := os.ReadDir(worktrees); err != nil {
			return result, err
		} else {
			for _, entry := range entries {
				admin := filepath.Join(worktrees, entry.Name())
				checkout, err := linkedCheckout(admin, resolved.CommonDir)
				if err != nil {
					result.Diagnostics = append(result.Diagnostics, LegacyDiagnostic{admin, err.Error()})
					continue
				}
				checkouts[checkout] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}
	for checkout := range checkouts {
		candidate, ok, err := legacyCandidate(checkout)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, LegacyDiagnostic{filepath.Join(checkout, ".pitcrew", "state.db"), err.Error()})
		} else if ok {
			result.Candidates = append(result.Candidates, candidate)
		}
	}
	sort.Slice(result.Candidates, func(i, j int) bool { return result.Candidates[i].StatePath < result.Candidates[j].StatePath })
	sort.Slice(result.Diagnostics, func(i, j int) bool { return result.Diagnostics[i].Path < result.Diagnostics[j].Path })
	if len(result.Candidates) > 0 {
		hash := sha256.New()
		for _, candidate := range result.Candidates {
			_, _ = io.WriteString(hash, candidate.ID+"\n")
		}
		result.CandidateSetID = hex.EncodeToString(hash.Sum(nil))
	}
	return result, nil
}

func linkedCheckout(admin, commonDir string) (string, error) {
	info, err := os.Lstat(admin)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("stale or unsafe worktree record")
	}
	if err := requireSafeOwnerAndMode(admin, info); err != nil {
		return "", err
	}
	backlink, err := readMetadataLine(filepath.Join(admin, "gitdir"))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(backlink) {
		backlink = filepath.Join(admin, backlink)
	}
	backlink = filepath.Clean(backlink)
	if filepath.Base(backlink) != ".git" {
		return "", errors.New("worktree backlink does not name .git")
	}
	checkout := filepath.Dir(backlink)
	resolved, err := Resolve(checkout)
	if err != nil || resolved.CommonDir != commonDir || resolved.CheckoutRoot != checkout {
		return "", errors.New("worktree backlink is stale or belongs to another common directory")
	}
	return checkout, nil
}

func legacyCandidate(checkout string) (LegacyCandidate, bool, error) {
	directory := filepath.Join(checkout, ".pitcrew")
	path := filepath.Join(directory, "state.db")
	if err := rejectSymlinkParents(directory); err != nil {
		return LegacyCandidate{}, false, err
	}
	directoryBefore, err := os.Lstat(directory)
	if os.IsNotExist(err) {
		return LegacyCandidate{}, false, nil
	}
	if err != nil || directoryBefore.Mode()&os.ModeSymlink != 0 || !directoryBefore.IsDir() {
		return LegacyCandidate{}, false, fmt.Errorf("unsafe legacy directory: %v", err)
	}
	if err := requireSafeOwnerAndMode(directory, directoryBefore); err != nil {
		return LegacyCandidate{}, false, err
	}
	dbHash, exists, err := fileFingerprint(path)
	if err != nil || !exists {
		return LegacyCandidate{}, false, err
	}
	walHash, walExists, err := fileFingerprint(path + "-wal")
	if err != nil {
		return LegacyCandidate{}, false, err
	}
	if !walExists {
		walHash = "-"
	}
	directoryAfter, err := os.Lstat(directory)
	if err != nil || !os.SameFile(directoryBefore, directoryAfter) || legacyMetadata(directoryBefore) != legacyMetadata(directoryAfter) {
		return LegacyCandidate{}, false, errors.New("legacy directory changed while inspecting")
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{path, legacyMetadata(directoryBefore), dbHash, walHash}, "\x00")))
	return LegacyCandidate{ID: hex.EncodeToString(digest[:]), CheckoutRoot: checkout, StatePath: path}, true, nil
}

func fileFingerprint(path string) (string, bool, error) {
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return "", false, fmt.Errorf("unsafe legacy file: %v", err)
	}
	if err := requireSafeOwnerAndMode(path, before); err != nil {
		return "", false, err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	after, statErr := file.Stat()
	closeErr := file.Close()
	if copyErr != nil || statErr != nil || closeErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || legacyMetadata(before) != legacyMetadata(after) {
		return "", false, errors.New("legacy file changed while inspecting")
	}
	if err := requireSafeOwnerAndMode(path, after); err != nil {
		return "", false, err
	}
	return legacyMetadata(before) + ":" + hex.EncodeToString(hash.Sum(nil)), true, nil
}

func legacyMetadata(info os.FileInfo) string {
	metadata := fmt.Sprintf("%#o", info.Mode().Perm())
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		metadata += fmt.Sprintf(":%d:%d", stat.Uid, stat.Gid)
	}
	return metadata
}

// GateLegacy prevents mutation until the exact discovered set is acknowledged.
func GateLegacy(discovery LegacyDiscovery, acknowledgedSetID string) error {
	if len(discovery.Candidates) > 0 && (discovery.CandidateSetID == "" || acknowledgedSetID != discovery.CandidateSetID) {
		return ErrMigrationRequired
	}
	return nil
}
