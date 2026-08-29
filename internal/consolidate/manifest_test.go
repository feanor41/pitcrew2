package consolidate_test

import (
	"github.com/fmazzalomo/pitcrew/internal/consolidate"
	"github.com/fmazzalomo/pitcrew/internal/project"
	"strings"
	"testing"
)

func TestManifestStrictShapeAndExactCandidateSet(t *testing.T) {
	projectID, a, b := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	discovery := project.LegacyDiscovery{CandidateSetID: strings.Repeat("d", 64), Candidates: []project.LegacyCandidate{{ID: a}, {ID: b}}}
	valid := `{"project_id":"` + projectID + `","candidate_ids":["` + b + `","` + a + `"],"choices":[{"workflow_id":"wf-000000000000000000000001","candidate_id":"` + b + `"}]}`
	manifest, err := consolidate.DecodeManifest(strings.NewReader(valid))
	if err != nil || manifest.Validate(projectID, discovery) != nil {
		t.Fatalf("valid manifest = %#v, %v", manifest, err)
	}
	for name, input := range map[string]string{
		"unknown field":       strings.TrimSuffix(valid, "}") + `,"extra":true}`,
		"trailing data":       valid + `{}`,
		"duplicate candidate": strings.Replace(valid, `"`+b+`","`+a+`"`, `"`+a+`","`+a+`"`, 1),
		"bad workflow":        strings.Replace(valid, "wf-000000000000000000000001", "bad", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := consolidate.DecodeManifest(strings.NewReader(input)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	manifest.CandidateIDs = manifest.CandidateIDs[:1]
	if err := manifest.Validate(projectID, discovery); err == nil {
		t.Fatal("incomplete inspected source set accepted")
	}
}
