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

func validPlan() Plan {
	return Plan{Summary: "two units", Scope: "internal", MaxParallelUnits: 2, Units: []WorkUnit{
		{ID: "wu-000000000000000000000001", Description: "first", Scope: "internal/foo", Areas: []string{"internal/foo"}, EstimatedChangedLines: 100, EstimatedReviewMinutes: 20, State: Pending},
		{ID: "wu-000000000000000000000002", Description: "second", Scope: "internal/bar", Areas: []string{"internal/bar"}, EstimatedChangedLines: 100, EstimatedReviewMinutes: 20, State: Pending},
	}}
}
