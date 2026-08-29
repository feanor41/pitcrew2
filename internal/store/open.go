package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	projectpkg "github.com/fmazzalomo/pitcrew/internal/project"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type identityMarker struct {
	Version      int    `json:"version"`
	ProjectID    string `json:"project_id"`
	GitCommonDir string `json:"git_common_dir"`
}

func OpenProject(ctx context.Context, resolved projectpkg.Project, paths projectpkg.Paths) (*Store, error) {
	dataHome, err := validateLayout(resolved, paths)
	if err != nil {
		return nil, err
	}
	if err := rejectSymlinkComponents(dataHome); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataHome, 0o700); err != nil {
		return nil, invalidPath(dataHome, err)
	}
	if err := rejectSymlinkComponents(dataHome); err != nil {
		return nil, err
	}
	if _, err := secureInfo(dataHome, true, 0); err != nil {
		return nil, err
	}
	for _, path := range []string{filepath.Join(dataHome, "pitcrew"), filepath.Join(dataHome, "pitcrew", "projects"), paths.ProjectRoot, paths.WorktreeRoot, paths.HandleRoot} {
		if err := mkdirPrivate(path); err != nil {
			return nil, err
		}
	}
	unlock, err := lockInit(filepath.Join(paths.ProjectRoot, ".initialize.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	markerExists, err := exists(paths.IdentityPath)
	if err != nil {
		return nil, err
	}
	stateExists, err := exists(paths.StatePath)
	if err != nil {
		return nil, err
	}
	if stateExists && !markerExists {
		return nil, invalidPath(paths.StatePath, errors.New("state exists without identity marker"))
	}
	markerCreated := !markerExists
	if markerExists {
		err = checkIdentity(paths.IdentityPath, resolved)
	} else {
		err = writeIdentity(paths.IdentityPath, resolved)
	}
	if err != nil {
		return nil, err
	}
	stateCreated := !stateExists
	if stateExists {
		_, err = secureInfo(paths.StatePath, false, 0o600)
	} else {
		err = createPrivate(paths.StatePath)
	}
	if err != nil {
		if markerCreated {
			_ = os.Remove(paths.IdentityPath)
		}
		return nil, err
	}
	if err = checkSQLite(paths.StatePath); err == nil {
		var opened *Store
		opened, err = openWritablePath(ctx, paths.StatePath)
		if err == nil {
			err = checkSQLite(paths.StatePath)
		}
		if err == nil {
			return opened, nil
		}
		if opened != nil {
			_ = opened.Close()
		}
	}
	if stateCreated {
		removeSQLite(paths.StatePath)
	}
	if markerCreated {
		_ = os.Remove(paths.IdentityPath)
	}
	return nil, err
}
func OpenProjectReadOnly(ctx context.Context, resolved projectpkg.Project, paths projectpkg.Paths) (OpenReadOnlyResult, error) {
	dataHome, err := validateLayout(resolved, paths)
	if err != nil {
		return OpenReadOnlyResult{}, err
	}
	if err := rejectSymlinkComponents(dataHome); err != nil {
		return OpenReadOnlyResult{}, err
	}
	hierarchy := []string{dataHome, filepath.Join(dataHome, "pitcrew"), filepath.Join(dataHome, "pitcrew", "projects"), paths.ProjectRoot}
	for index, path := range hierarchy {
		if ok, err := exists(path); err != nil {
			return OpenReadOnlyResult{}, err
		} else if !ok {
			return OpenReadOnlyResult{State: Uninitialized}, nil
		}
		mode := os.FileMode(0o700)
		if index == 0 {
			mode = 0
		}
		if _, err := secureInfo(path, true, mode); err != nil {
			return OpenReadOnlyResult{}, err
		}
	}
	markerExists, err := exists(paths.IdentityPath)
	if err != nil {
		return OpenReadOnlyResult{}, err
	}
	stateExists, err := exists(paths.StatePath)
	if err != nil {
		return OpenReadOnlyResult{}, err
	}
	if markerExists {
		if err := checkIdentity(paths.IdentityPath, resolved); err != nil {
			return OpenReadOnlyResult{}, err
		}
	}
	if !stateExists {
		return OpenReadOnlyResult{State: Uninitialized}, nil
	}
	if !markerExists {
		return OpenReadOnlyResult{}, invalidPath(paths.StatePath, errors.New("state exists without identity marker"))
	}
	for _, path := range []string{paths.WorktreeRoot, paths.HandleRoot} {
		if _, err := secureInfo(path, true, 0o700); err != nil {
			return OpenReadOnlyResult{}, err
		}
	}
	if err := checkSQLite(paths.StatePath); err != nil {
		return OpenReadOnlyResult{}, err
	}
	return openReadOnlyPath(ctx, paths.StatePath)
}
func rejectSymlinkComponents(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return invalidPath(current, errors.New("unsafe parent component"))
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return invalidPath(current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
func validateLayout(resolved projectpkg.Project, paths projectpkg.Paths) (string, error) {
	digest := sha256.Sum256([]byte(filepath.Clean(resolved.CommonDir)))
	if !filepath.IsAbs(resolved.CommonDir) || resolved.ID != hex.EncodeToString(digest[:]) {
		return "", invalidPath(paths.ProjectRoot, errors.New("project identity mismatch"))
	}
	dataHome := filepath.Dir(filepath.Dir(filepath.Dir(paths.ProjectRoot)))
	want, err := projectpkg.DerivePaths(dataHome, resolved.ID)
	if err != nil || want != paths {
		return "", invalidPath(paths.ProjectRoot, errors.New("central path mismatch"))
	}
	return dataHome, nil
}
func exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, invalidPath(path, err)
	}
	return true, nil
}
func mkdirPrivate(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return invalidPath(path, err)
	}
	_, err := secureInfo(path, true, 0o700)
	return err
}
func secureInfo(path string, wantDir bool, mode os.FileMode) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, invalidPath(path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() != wantDir || !wantDir && !info.Mode().IsRegular() || mode != 0 && info.Mode().Perm() != mode {
		return nil, invalidPath(path, fmt.Errorf("unsafe mode %s", info.Mode()))
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return nil, invalidPath(path, errors.New("wrong owner"))
	}
	return info, nil
}
func createPrivate(path string) error {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_EXCL|syscall.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return invalidPath(path, err)
	}
	return syscall.Close(fd)
}
func lockInit(path string) (func(), error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, invalidPath(path, err)
	}
	if _, err := secureInfo(path, false, 0o600); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = syscall.Close(fd) }, nil
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = syscall.Close(fd)
			return nil, invalidPath(path, err)
		}
		if time.Now().After(deadline) {
			_ = syscall.Close(fd)
			return nil, invalidPath(path, errors.New("initialization is busy"))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func writeIdentity(path string, resolved projectpkg.Project) error {
	content, _ := json.Marshal(identityMarker{1, resolved.ID, resolved.CommonDir})
	temp, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return invalidPath(path, err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	_, err = temp.Write(append(content, '\n'))
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Link(tempPath, path)
	}
	if err != nil {
		return invalidPath(path, err)
	}
	return nil
}
func checkIdentity(path string, resolved projectpkg.Project) error {
	if _, err := secureInfo(path, false, 0o600); err != nil {
		return err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return invalidPath(path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var marker identityMarker
	if err = decoder.Decode(&marker); err == nil {
		if trailing := decoder.Decode(&struct{}{}); !errors.Is(trailing, io.EOF) {
			err = errors.New("trailing marker data")
		}
	}
	if err != nil || marker.Version != 1 || marker.ProjectID != resolved.ID || marker.GitCommonDir != resolved.CommonDir {
		return invalidPath(path, errors.New("identity marker mismatch"))
	}
	return nil
}
func checkSQLite(path string) error {
	if _, err := secureInfo(path, false, 0o600); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if ok, err := exists(path + suffix); err != nil {
			return err
		} else if ok {
			info, err := secureInfo(path+suffix, false, 0)
			if err != nil || info.Mode().Perm()&0o077 != 0 {
				return invalidPath(path+suffix, errors.New("unsafe SQLite sidecar"))
			}
		}
	}
	return nil
}
func invalidPath(path string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrInvalidState, path, err)
}
func removeSQLite(path string) {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		_ = os.Remove(candidate)
	}
}
