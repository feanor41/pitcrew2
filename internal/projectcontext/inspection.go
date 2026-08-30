package projectcontext

import (
	"context"
	"errors"
)

// Status is the truthful availability state of a project context snapshot.
type Status string

const (
	Missing    Status = "missing"
	Incomplete Status = "incomplete"
	Complete   Status = "complete"
)

// Inspection is a defensive, read-only projection of stored project context.
type Inspection struct {
	Status           Status            `json:"status"`
	SchemaVersion    int               `json:"schema_version,omitempty"`
	Facts            map[string][]Fact `json:"facts"`
	Coverage         map[string]bool   `json:"coverage"`
	MissingOrInvalid []string          `json:"missing_or_invalid"`
	UpdatedAt        string            `json:"updated_at,omitempty"`
	CheckoutRoot     string            `json:"checkout_root"`
}

// Loader reads an existing snapshot without initializing or migrating storage.
type Loader func(context.Context) (record Record, updatedAt string, found bool, err error)

// Resolved binds a physical checkout to its central read-only loader.
type Resolved struct {
	CheckoutRoot string
	Load         Loader
}

// Resolver resolves the current checkout and logical-project storage.
type Resolver func(context.Context) (Resolved, error)

// Dependencies configure inspection without coupling the model to persistence.
type Dependencies struct {
	Resolve Resolver
}

// Service provides workflow-independent project-context inspection.
type Service struct {
	resolve Resolver
}

// New constructs a project-context inspection service.
func New(deps Dependencies) *Service {
	return &Service{resolve: deps.Resolve}
}

// Inspect returns current context without creating, migrating, or writing state.
func (s *Service) Inspect(ctx context.Context) (Inspection, error) {
	resolved, err := s.resolve(ctx)
	if err != nil {
		return Inspection{}, err
	}
	result := emptyInspection(resolved.CheckoutRoot)
	record, updatedAt, found, err := resolved.Load(ctx)
	if err != nil {
		if errors.Is(err, ErrInvalidRecord) {
			result.Status = Incomplete
			return result, nil
		}
		return Inspection{}, err
	}
	if !found {
		return result, nil
	}
	missing, err := MissingCategories(record)
	if err != nil {
		result.Status = Incomplete
		return result, nil
	}
	result.Facts = CloneRecord(record).Facts
	result.SchemaVersion = record.SchemaVersion
	result.UpdatedAt = updatedAt
	result.MissingOrInvalid = missing
	for _, category := range Categories() {
		result.Coverage[category] = len(record.Facts[category]) > 0
	}
	if len(missing) == 0 {
		result.Status = Complete
	} else {
		result.Status = Incomplete
	}
	return result, nil
}

func emptyInspection(checkoutRoot string) Inspection {
	coverage := make(map[string]bool, len(categories))
	for _, category := range categories {
		coverage[category] = false
	}
	return Inspection{
		Status:           Missing,
		Facts:            map[string][]Fact{},
		Coverage:         coverage,
		MissingOrInvalid: Categories(),
		CheckoutRoot:     checkoutRoot,
	}
}
