package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestPlanJSONUsesTheSubmissionContract(t *testing.T) {
	p := validPlan()
	p.Units[0].AdmissionException = &AdmissionException{Justification: "indivisible"}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, key := range []string{`"summary"`, `"scope"`, `"work_units"`, `"max_parallel_units"`, `"estimated_changed_lines"`, `"admission_exception"`} {
		if !strings.Contains(text, key) {
			t.Fatalf("plan JSON %s lacks %s", text, key)
		}
	}
}

func TestCoverageJSONUsesStableStructuredIdentifiers(t *testing.T) {
	p := validPlan()
	p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-COV-001", ScenarioIDs: []string{"SCN-COV-001", "SCN-COV-002"}}}
	p.Units[1].Coverage = []Coverage{{RequirementID: "REQ-COV-002", ScenarioIDs: []string{"SCN-COV-003"}}}
	p.Units[0].present.coverage = true
	p.Units[1].present.coverage = true
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"coverage":[{"requirement_id":"REQ-COV-001","scenario_ids":["SCN-COV-001","SCN-COV-002"]}]`) {
		t.Fatalf("coverage JSON = %s", encoded)
	}
	var decoded Plan
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Units[0].HasCoverage() || len(decoded.Units[0].Coverage) != 1 {
		t.Fatalf("decoded coverage = %#v", decoded.Units[0])
	}
}

func TestValidateCoverageRejectsMalformedOrDuplicateIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Plan)
		want string
	}{
		{"empty coverage", func(p *Plan) { p.Units[0].present.coverage = true }, "coverage must not be empty"},
		{"invalid requirement", func(p *Plan) {
			p.Units[0].Coverage = []Coverage{{RequirementID: "requirement one", ScenarioIDs: []string{"SCN-ONE"}}}
		}, "invalid requirement id"},
		{"no scenarios", func(p *Plan) { p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-ONE"}} }, "scenario_ids must not be empty"},
		{"invalid scenario", func(p *Plan) {
			p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-ONE", ScenarioIDs: []string{"scenario one"}}}
		}, "invalid scenario id"},
		{"duplicate scenario", func(p *Plan) {
			p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-ONE", ScenarioIDs: []string{"SCN-ONE", "SCN-ONE"}}}
		}, "duplicate coverage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validPlan()
			p.Units[0].present.coverage = true
			tt.edit(&p)
			if err := Validate(p); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v; want %q", err, tt.want)
			}
		})
	}
}

func TestCoverageJSONRejectsMissingOrUnknownFields(t *testing.T) {
	for _, input := range []string{
		`{"requirement_id":"REQ-ONE"}`,
		`{"scenario_ids":["SCN-ONE"]}`,
		`{"requirement_id":"REQ-ONE","scenario_ids":["SCN-ONE"],"extra":true}`,
	} {
		var coverage Coverage
		if err := json.Unmarshal([]byte(input), &coverage); err == nil {
			t.Fatalf("malformed coverage accepted: %s", input)
		}
	}
}

func TestAggregateCorrectionPolicyJSONNormalizesAndValidatesCanonicalContract(t *testing.T) {
	base := `{"summary":"one","scope":"internal","work_units":[{"id":"wu-000000000000000000000001","description":"unit","scope":"internal/plan","areas":["internal/plan"],"depends_on":[],"estimated_changed_lines":1,"estimated_review_minutes":1}],"max_parallel_units":1`
	for _, rounds := range []int{0, 1} {
		var p Plan
		input := base + fmt.Sprintf(`,"aggregate_correction_policy":{"automatic_rounds":%d,"on_exhaustion":"require_user_authorization"}}`, rounds)
		if err := json.Unmarshal([]byte(input), &p); err != nil {
			t.Fatalf("rounds %d: %v", rounds, err)
		}
		if err := Validate(p); err != nil {
			t.Fatalf("rounds %d: %v", rounds, err)
		}
		if !p.HasAggregateCorrectionPolicy() || p.AggregateCorrectionPolicy.AutomaticRounds != rounds {
			t.Fatalf("rounds %d decoded as %#v", rounds, p)
		}
	}

	malformed := []string{
		`,"aggregate_correction_policy":{"automatic_rounds":-1,"on_exhaustion":"require_user_authorization"}}`,
		`,"aggregate_correction_policy":{"automatic_rounds":2,"on_exhaustion":"require_user_authorization"}}`,
		`,"aggregate_correction_policy":{"automatic_rounds":1,"on_exhaustion":"retry_forever"}}`,
		`,"aggregate_correction_policy":{"automatic_rounds":1,"on_exhaustion":"require_user_authorization","extra":true}}`,
		`,"aggregate_correction_policy":{"automatic_rounds":1}}`,
		`,"aggregate_correction_policy":null}`,
	}
	for _, suffix := range malformed {
		var p Plan
		if err := json.Unmarshal([]byte(base+suffix), &p); err == nil {
			if err = Validate(p); err == nil {
				t.Fatalf("malformed policy accepted: %s", suffix)
			}
		}
	}
}

func TestHistoricalPlanDefaultsPolicyWithoutBecomingPolicyAware(t *testing.T) {
	encoded, err := json.Marshal(validPlan())
	if err != nil {
		t.Fatal(err)
	}
	var historical Plan
	if err = json.Unmarshal(encoded, &historical); err != nil {
		t.Fatal(err)
	}
	if historical.HasAggregateCorrectionPolicy() {
		t.Fatal("historical plan became policy-aware")
	}
	if historical.AggregateCorrectionPolicy != DefaultAggregateCorrectionPolicy() {
		t.Fatalf("historical effective policy = %#v", historical.AggregateCorrectionPolicy)
	}
	aware := NormalizeForSubmission(historical)
	if !aware.HasAggregateCorrectionPolicy() || aware.AggregateCorrectionPolicy != DefaultAggregateCorrectionPolicy() {
		t.Fatalf("normalized submission = %#v", aware)
	}
}

func TestValidateAcceptsIndependentAdmittedUnits(t *testing.T) {
	p := validPlan()
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	p.Units[1].Scope = "internal/foobar"
	if err := Validate(p); err != nil {
		t.Fatalf("segment-distinct scopes overlap: %v", err)
	}
}

func TestValidateRejectsUnsafeOrInconsistentPlans(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Plan)
		want string
	}{
		{"unknown dependency", func(p *Plan) { p.Units[0].DependsOn = []string{"wu-000000000000000000000099"} }, "unknown dependency"},
		{"cycle", func(p *Plan) {
			p.Units[0].DependsOn = []string{p.Units[1].ID}
			p.Units[1].DependsOn = []string{p.Units[0].ID}
		}, "cycle"},
		{"overlap", func(p *Plan) { p.Units[1].Scope = "internal/foo/child" }, "overlaps"},
		{"glob", func(p *Plan) { p.Units[0].Scope = "internal/*" }, "prefix"},
		{"absolute", func(p *Plan) { p.Units[0].Scope = "/tmp/escape" }, "prefix"},
		{"traversal", func(p *Plan) { p.Units[0].Areas = []string{"../secret"} }, "prefix"},
		{"budget", func(p *Plan) { p.Units[0].EstimatedChangedLines = 401 }, "admission exception"},
		{"parallelism", func(p *Plan) { p.MaxParallelUnits = 0 }, "max_parallel_units"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validPlan()
			tt.edit(&p)
			if err := Validate(p); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v; want %q", err, tt.want)
			}
		})
	}
}

func TestApproveExceptionsRequiresExplicitMatchingApproval(t *testing.T) {
	p := validPlan()
	p.Units[0].EstimatedReviewMinutes = 61
	p.Units[0].AdmissionException = &AdmissionException{Justification: "indivisible schema"}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	if err := Approve(p, nil); err == nil {
		t.Fatal("missing exception approval accepted")
	}
	if err := Approve(p, []string{p.Units[0].ID}); err != nil {
		t.Fatalf("Approve() = %v", err)
	}
}

func TestReadyUnitsHonorsDependenciesHandlesAndParallelLimit(t *testing.T) {
	p := validPlan()
	p.MaxParallelUnits = 1
	p.Units[1].DependsOn = []string{p.Units[0].ID}
	ready := ReadyUnits(p, map[string]bool{})
	if len(ready) != 1 || ready[0].ID != p.Units[0].ID {
		t.Fatalf("ready = %#v", ready)
	}
	p.Units[0].State = Done
	ready = ReadyUnits(p, map[string]bool{p.Units[1].ID: true})
	if len(ready) != 0 {
		t.Fatalf("active handle unit ready = %#v", ready)
	}
	ready = ReadyUnits(p, nil)
	if len(ready) != 1 || ready[0].ID != p.Units[1].ID {
		t.Fatalf("ready after dependency = %#v", ready)
	}
}

func TestValidateTypedDependencyConsumptions(t *testing.T) {
	p := validPlan()
	p.Units[1].DependsOn = []string{p.Units[0].ID}
	p.Units[0].Coverage = []Coverage{{RequirementID: "REQ-DEP", ScenarioIDs: []string{"SCN-DEP-RESULT"}}}
	p.Units[1].Coverage = []Coverage{{RequirementID: "REQ-DEP", ScenarioIDs: []string{"SCN-CONSUMER"}}}
	p.Units[1].DependencyConsumptions = []DependencyConsumption{{ProducerUnitID: p.Units[0].ID, ScenarioIDs: []string{"SCN-DEP-RESULT"}}}
	p = NormalizeForSubmission(p)
	if err := Validate(p); err != nil {
		t.Fatalf("valid causal dependency rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*Plan)
		want string
	}{
		{"missing consumption", func(p *Plan) { p.Units[1].DependencyConsumptions = nil }, "missing dependency consumption"},
		{"empty selector", func(p *Plan) { p.Units[1].DependencyConsumptions[0].ScenarioIDs = nil }, "scenario_ids must not be empty"},
		{"wrong producer", func(p *Plan) { p.Units[1].DependencyConsumptions[0].ProducerUnitID = p.Units[1].ID }, "self dependency consumption"},
		{"wrong owner", func(p *Plan) { p.Units[1].DependencyConsumptions[0].ScenarioIDs = []string{"SCN-OTHER"} }, "not assigned to producer"},
		{"duplicate selector", func(p *Plan) {
			p.Units[1].DependencyConsumptions[0].ScenarioIDs = []string{"SCN-DEP-RESULT", "SCN-DEP-RESULT"}
		}, "duplicate dependency selector"},
		{"duplicate group", func(p *Plan) {
			p.Units[1].DependencyConsumptions = append(p.Units[1].DependencyConsumptions, p.Units[1].DependencyConsumptions[0])
		}, "duplicate dependency consumption"},
		{"cycle", func(p *Plan) {
			p.Units[0].DependsOn = []string{p.Units[1].ID}
			p.Units[0].DependencyConsumptions = []DependencyConsumption{{ProducerUnitID: p.Units[1].ID, ScenarioIDs: []string{"SCN-CONSUMER"}}}
		}, "cycle"},
		{"indirect producer", func(p *Plan) {
			p.Units = append(p.Units, WorkUnit{ID: "wu-000000000000000000000003", Description: "third", Scope: "internal/third", Areas: []string{"internal/third"}, DependsOn: []string{p.Units[1].ID}, DependencyConsumptions: []DependencyConsumption{{ProducerUnitID: p.Units[0].ID, ScenarioIDs: []string{"SCN-DEP-RESULT"}}}})
		}, "non-direct producer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := p
			candidate.Units = append([]WorkUnit(nil), p.Units...)
			candidate.Units[1].DependencyConsumptions = append([]DependencyConsumption(nil), p.Units[1].DependencyConsumptions...)
			tt.edit(&candidate)
			candidate = NormalizeForSubmission(candidate)
			if err := Validate(candidate); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v; want %q", err, tt.want)
			}
		})
	}
}

func TestReadyRequiresEveryResultFromEveryDirectProducer(t *testing.T) {
	first, second, consumer := "wu-000000000000000000000001", "wu-000000000000000000000002", "wu-000000000000000000000003"
	p := NormalizeForSubmission(Plan{Summary: "multi producer", Scope: "internal", MaxParallelUnits: 1, Units: []WorkUnit{
		{ID: first, State: Done}, {ID: second, State: Done},
		{ID: consumer, State: Pending, DependsOn: []string{first, second}, DependencyConsumptions: []DependencyConsumption{
			{ProducerUnitID: first, ScenarioIDs: []string{"SCN-A", "SCN-B"}},
			{ProducerUnitID: second, ScenarioIDs: []string{"SCN-C"}},
		}},
	}})
	satisfied := map[string]bool{
		dependencyResultKey(consumer, first, "SCN-A"): true,
		dependencyResultKey(consumer, first, "SCN-B"): true,
	}
	if ready := readyUnits(p, nil, satisfied); len(ready) != 0 {
		t.Fatalf("consumer ready without every upstream result: %#v", ready)
	}
	satisfied[dependencyResultKey(consumer, second, "SCN-C")] = true
	if ready := readyUnits(p, nil, satisfied); len(ready) != 1 || ready[0].ID != consumer {
		t.Fatalf("consumer not ready with every upstream result: %#v", ready)
	}
}

func validPlan() Plan {
	return Plan{Summary: "two units", Scope: "internal", MaxParallelUnits: 2, Units: []WorkUnit{
		{ID: "wu-000000000000000000000001", Description: "first", Scope: "internal/foo", Areas: []string{"internal/foo"}, EstimatedChangedLines: 100, EstimatedReviewMinutes: 20, State: Pending},
		{ID: "wu-000000000000000000000002", Description: "second", Scope: "internal/bar", Areas: []string{"internal/bar"}, EstimatedChangedLines: 100, EstimatedReviewMinutes: 20, State: Pending},
	}}
}
