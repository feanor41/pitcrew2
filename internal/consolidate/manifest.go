package consolidate

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/fmazzalomo/pitcrew/internal/project"
	"io"
	"regexp"
)

var ErrInvalidManifest = errors.New("invalid consolidation manifest")
var workflowIDPattern = regexp.MustCompile(`^wf-[0-9a-f]{24}$`)

type Choice struct {
	WorkflowID  string `json:"workflow_id"`
	CandidateID string `json:"candidate_id"`
}
type Manifest struct {
	ProjectID      string   `json:"project_id"`
	CandidateIDs   []string `json:"candidate_ids"`
	Choices        []Choice `json:"choices"`
	RetainExisting []string `json:"retain_existing,omitempty"`
}

func DecodeManifest(reader io.Reader) (Manifest, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, ErrInvalidManifest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || manifest.basicValidation() != nil {
		return Manifest{}, ErrInvalidManifest
	}
	return manifest, nil
}
func (m Manifest) basicValidation() error {
	if !digestID(m.ProjectID) || len(m.CandidateIDs) == 0 {
		return ErrInvalidManifest
	}
	seenCandidates, seenWorkflows := map[string]bool{}, map[string]bool{}
	for _, id := range m.CandidateIDs {
		if !digestID(id) || seenCandidates[id] {
			return ErrInvalidManifest
		}
		seenCandidates[id] = true
	}
	for _, choice := range m.Choices {
		if !workflowIDPattern.MatchString(choice.WorkflowID) || !seenCandidates[choice.CandidateID] || seenWorkflows[choice.WorkflowID] {
			return ErrInvalidManifest
		}
		seenWorkflows[choice.WorkflowID] = true
	}
	for _, workflowID := range m.RetainExisting {
		if !workflowIDPattern.MatchString(workflowID) || seenWorkflows[workflowID] {
			return ErrInvalidManifest
		}
		seenWorkflows[workflowID] = true
	}
	return nil
}
func (m Manifest) Validate(projectID string, discovery project.LegacyDiscovery) error {
	if m.basicValidation() != nil || m.ProjectID != projectID || len(m.CandidateIDs) != len(discovery.Candidates) {
		return ErrInvalidManifest
	}
	actual := map[string]bool{}
	for _, candidate := range discovery.Candidates {
		actual[candidate.ID] = true
	}
	for _, id := range m.CandidateIDs {
		if !actual[id] {
			return ErrInvalidManifest
		}
	}
	return nil
}
func digestID(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}
