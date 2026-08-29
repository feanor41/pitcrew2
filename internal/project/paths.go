package project

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
)

// ErrUnsafePath indicates that central project paths could escape their
// injected data home.
var ErrUnsafePath = errors.New("unsafe project path")

// Paths contains the private central locations derived for one project.
type Paths struct {
	ProjectRoot  string
	IdentityPath string
	StatePath    string
	WorktreeRoot string
	HandleRoot   string
}

// DerivePaths derives central paths without creating or inspecting them.
func DerivePaths(dataHome, projectID string) (Paths, error) {
	if !filepath.IsAbs(dataHome) {
		return Paths{}, fmt.Errorf("%w: data home must be absolute", ErrUnsafePath)
	}
	decoded, err := hex.DecodeString(projectID)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != projectID {
		return Paths{}, fmt.Errorf("%w: project ID must be a lowercase SHA-256 digest", ErrUnsafePath)
	}

	dataHome = filepath.Clean(dataHome)
	root := filepath.Join(dataHome, "pitcrew", "projects", projectID)
	rel, err := filepath.Rel(dataHome, root)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return Paths{}, fmt.Errorf("%w: project root escapes data home", ErrUnsafePath)
	}
	return Paths{
		ProjectRoot:  root,
		IdentityPath: filepath.Join(root, "identity.json"),
		StatePath:    filepath.Join(root, "state.db"),
		WorktreeRoot: filepath.Join(root, "worktrees"),
		HandleRoot:   filepath.Join(root, "handles"),
	}, nil
}
