// Package projectcontext defines bounded, evidence-backed project context.
package projectcontext

const (
	SchemaVersion       = 1
	MaxEncodedBytes     = 64 * 1024
	MaxFactsPerCategory = 32
	MaxComponentBytes   = 1024
)

var categories = [...]string{
	"stack", "runtime", "deployment", "architecture", "documentation", "sdd",
}

// Record is one schema-versioned context snapshot.
type Record struct {
	SchemaVersion int               `json:"schema_version"`
	Facts         map[string][]Fact `json:"facts"`
}

// Fact is an observed assertion backed by one evidence mode.
type Fact struct {
	Assertion  string   `json:"assertion"`
	ObservedAt string   `json:"observed_at"`
	Evidence   Evidence `json:"evidence"`
}

// Evidence identifies either a repository file or a command result.
type Evidence struct {
	Path      string `json:"path,omitempty"`
	LineRange string `json:"line_range,omitempty"`
	Command   string `json:"command,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// Categories returns the required categories in their canonical order.
func Categories() []string {
	result := make([]string, len(categories))
	copy(result, categories[:])
	return result
}

// CloneRecord returns a deep copy suitable for crossing ownership boundaries.
func CloneRecord(record Record) Record {
	clone := Record{SchemaVersion: record.SchemaVersion}
	if record.Facts == nil {
		return clone
	}
	clone.Facts = make(map[string][]Fact, len(record.Facts))
	for category, facts := range record.Facts {
		clone.Facts[category] = append([]Fact(nil), facts...)
		if clone.Facts[category] == nil {
			clone.Facts[category] = []Fact{}
		}
	}
	return clone
}
