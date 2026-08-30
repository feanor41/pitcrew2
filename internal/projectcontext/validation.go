package projectcontext

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// ErrInvalidRecord identifies invalid schema-v1 context input.
var ErrInvalidRecord = errors.New("invalid project context")

// Validate checks the complete record, including its encoded size.
func Validate(record Record) error {
	if record.SchemaVersion != SchemaVersion {
		return invalid("unsupported schema version")
	}
	if len(record.Facts) != len(categories) {
		return invalid("facts must contain exactly the required categories")
	}
	for _, category := range categories {
		facts, ok := record.Facts[category]
		if !ok {
			return invalid("missing category " + category)
		}
		if len(facts) > MaxFactsPerCategory {
			return invalid("too many facts in " + category)
		}
		for _, fact := range facts {
			if err := validateFact(fact); err != nil {
				return err
			}
		}
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return invalid("record cannot be encoded")
	}
	if len(encoded) > MaxEncodedBytes {
		return invalid("record exceeds maximum size")
	}
	return nil
}

// ValidateActor checks the bounded actor label stored with a snapshot.
func ValidateActor(actor string) error {
	if strings.TrimSpace(actor) == "" || len(actor) > MaxComponentBytes {
		return invalid("actor must be nonblank and bounded")
	}
	return nil
}

// MissingCategories returns empty categories in canonical order.
func MissingCategories(record Record) ([]string, error) {
	if err := Validate(record); err != nil {
		return nil, err
	}
	missing := make([]string, 0, len(categories))
	for _, category := range categories {
		if len(record.Facts[category]) == 0 {
			missing = append(missing, category)
		}
	}
	return missing, nil
}

func validateFact(fact Fact) error {
	if strings.TrimSpace(fact.Assertion) == "" || len(fact.Assertion) > MaxComponentBytes {
		return invalid("assertion must be nonblank and bounded")
	}
	if _, err := time.Parse(time.RFC3339Nano, fact.ObservedAt); err != nil {
		return invalid("observed_at must be RFC3339")
	}
	evidence := fact.Evidence
	for _, component := range []string{evidence.Path, evidence.LineRange, evidence.Command, evidence.Summary} {
		if len(component) > MaxComponentBytes {
			return invalid("evidence component exceeds maximum size")
		}
	}
	hasFile := strings.TrimSpace(evidence.Path) != ""
	hasCommand := strings.TrimSpace(evidence.Command) != "" || strings.TrimSpace(evidence.Summary) != ""
	if hasFile == hasCommand {
		return invalid("evidence must be exactly one file or command reference")
	}
	if evidence.LineRange != "" && !hasFile {
		return invalid("evidence line_range requires a file path")
	}
	if hasFile {
		cleaned := path.Clean(strings.ReplaceAll(evidence.Path, "\\", "/"))
		if path.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != evidence.Path {
			return invalid("evidence path must be normalized and repository-relative")
		}
		return nil
	}
	if strings.TrimSpace(evidence.Command) == "" || strings.TrimSpace(evidence.Summary) == "" {
		return invalid("command evidence requires command and summary")
	}
	return nil
}

func invalid(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidRecord, detail)
}
