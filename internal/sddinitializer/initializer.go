// Package sddinitializer performs one bounded, local project-context inventory.
package sddinitializer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/projectcontext"
)

const Actor = "pc2-sdd-initializer"
const maxManifestRead = 16 * 1024

var goVersion = regexp.MustCompile(`(?m)^go\s+([^\s]+)\s*$`)

type Inspector interface {
	Inspect(context.Context) (projectcontext.Inspection, error)
}

type Collector func(checkoutRoot string, observedAt time.Time) (projectcontext.Record, error)

type Dependencies struct {
	Inspector Inspector
	Recorder  projectcontext.Recorder
	Collect   Collector
	Now       func() time.Time
}

type Result struct {
	Inspection projectcontext.Inspection
	Record     projectcontext.Record
	Persisted  bool
}

// Initialize inspects exactly once and records at most one validated merge.
func Initialize(ctx context.Context, deps Dependencies) (Result, error) {
	if deps.Inspector == nil {
		return Result{}, errors.New("project context inspector is required")
	}
	inspection, err := deps.Inspector.Inspect(ctx)
	if err != nil {
		return Result{}, err
	}
	if inspection.Status == projectcontext.Complete {
		return Result{Inspection: inspection}, nil
	}
	if deps.Recorder == nil {
		return Result{}, errors.New("project context recorder is required")
	}
	if deps.Collect == nil {
		deps.Collect = collectLocal
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	at := deps.Now().UTC()
	observed, err := deps.Collect(inspection.CheckoutRoot, at)
	if err != nil {
		return Result{}, err
	}
	if err := projectcontext.Validate(observed); err != nil {
		return Result{}, err
	}
	candidate := merge(inspection.Facts, observed)
	if err := projectcontext.Validate(candidate); err != nil {
		return Result{}, err
	}
	if inspection.Status == projectcontext.Incomplete && sameFacts(inspection.Facts, candidate.Facts) {
		return Result{Inspection: inspection, Record: candidate}, nil
	}
	updated, persisted, err := deps.Recorder.Record(ctx, candidate, Actor, at)
	if err != nil {
		return Result{}, err
	}
	return Result{Inspection: updated, Record: candidate, Persisted: persisted}, nil
}

func merge(existing map[string][]projectcontext.Fact, observed projectcontext.Record) projectcontext.Record {
	merged := emptyRecord()
	retained := projectcontext.Record{SchemaVersion: projectcontext.SchemaVersion, Facts: existing}
	if projectcontext.Validate(retained) == nil {
		merged = projectcontext.CloneRecord(retained)
	}
	for _, category := range projectcontext.Categories() {
		for _, candidate := range observed.Facts[category] {
			if len(merged.Facts[category]) < projectcontext.MaxFactsPerCategory && !contains(merged.Facts[category], candidate) {
				merged.Facts[category] = append(merged.Facts[category], candidate)
			}
		}
	}
	return merged
}

func contains(facts []projectcontext.Fact, candidate projectcontext.Fact) bool {
	for _, fact := range facts {
		if fact.Assertion == candidate.Assertion && fact.Evidence == candidate.Evidence {
			return true
		}
	}
	return false
}

func sameFacts(left, right map[string][]projectcontext.Fact) bool {
	for _, category := range projectcontext.Categories() {
		if !reflect.DeepEqual(left[category], right[category]) {
			return false
		}
	}
	return true
}

func emptyRecord() projectcontext.Record {
	facts := make(map[string][]projectcontext.Fact)
	for _, category := range projectcontext.Categories() {
		facts[category] = []projectcontext.Fact{}
	}
	return projectcontext.Record{SchemaVersion: projectcontext.SchemaVersion, Facts: facts}
}

type signal struct{ category, path, assertion string }

var signals = []signal{
	{"stack", "go.mod", "Go module manifest."}, {"stack", "package.json", "Node.js package manifest."},
	{"stack", "pyproject.toml", "Python project manifest."}, {"stack", "Cargo.toml", "Rust package manifest."},
	{"stack", "Gemfile", "Ruby dependency manifest."}, {"stack", "pom.xml", "Maven project descriptor."},
	{"runtime", ".nvmrc", "Node.js runtime version file."}, {"runtime", ".tool-versions", "Runtime version inventory."},
	{"runtime", ".python-version", "Python runtime version file."}, {"runtime", "runtime.txt", "Runtime version file."},
	{"deployment", "Dockerfile", "Container build surface."}, {"deployment", "docker-compose.yml", "Compose deployment surface."},
	{"deployment", "docker-compose.yaml", "Compose deployment surface."}, {"deployment", "Procfile", "Process deployment surface."},
	{"deployment", "deploy", "Deployment configuration directory."}, {"deployment", "k8s", "Kubernetes deployment configuration directory."},
	{"deployment", "helm", "Helm deployment configuration directory."}, {"deployment", "terraform", "Terraform deployment configuration directory."},
	{"deployment", ".github/workflows", "CI or deployment workflow directory."},
	{"architecture", "cmd", "Command entry-point boundary."}, {"architecture", "internal", "Internal package boundary."},
	{"architecture", "pkg", "Reusable package boundary."}, {"architecture", "src", "Source tree boundary."},
	{"architecture", "app", "Application component boundary."}, {"architecture", "api", "API component boundary."},
	{"documentation", "README.md", "Repository overview documentation."}, {"documentation", "docs", "Internal documentation directory."},
	{"documentation", "CONTRIBUTING.md", "Contributor documentation."}, {"documentation", "ARCHITECTURE.md", "Architecture documentation."},
	{"sdd", "AGENTS.md", "Agent working conventions."}, {"sdd", "openspec", "OpenSpec change artifacts directory."},
	{"sdd", "specs", "Specification artifacts directory."}, {"sdd", ".github/PULL_REQUEST_TEMPLATE.md", "Pull request process template."},
}

func collectLocal(root string, at time.Time) (projectcontext.Record, error) {
	record := emptyRecord()
	observed := at.UTC().Format(time.RFC3339Nano)
	for _, item := range signals {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(item.path)))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return projectcontext.Record{}, fmt.Errorf("inspect %s: %w", item.path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		record.Facts[item.category] = append(record.Facts[item.category], projectcontext.Fact{
			Assertion: item.assertion, ObservedAt: observed, Evidence: projectcontext.Evidence{Path: item.path},
		})
	}
	if version, ok, err := readGoVersion(root); err != nil {
		return projectcontext.Record{}, err
	} else if ok {
		fact := projectcontext.Fact{Assertion: "Go toolchain version " + version + ".", ObservedAt: observed, Evidence: projectcontext.Evidence{Path: "go.mod"}}
		record.Facts["runtime"] = append([]projectcontext.Fact{fact}, record.Facts["runtime"]...)
	}
	return record, nil
}

func readGoVersion(root string) (string, bool, error) {
	path := filepath.Join(root, "go.mod")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	content, err := io.ReadAll(bufio.NewReader(io.LimitReader(file, maxManifestRead)))
	if err != nil {
		return "", false, err
	}
	match := goVersion.FindStringSubmatch(string(content))
	if len(match) != 2 || strings.TrimSpace(match[1]) == "" {
		return "", false, nil
	}
	return match[1], true, nil
}
