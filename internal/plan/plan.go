package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

type UnitState string

const (
	Pending UnitState = "pending"
	Done    UnitState = "done"
)

type AdmissionException struct {
	Justification string `json:"justification"`
}
type WorkUnit struct {
	ID                     string              `json:"id"`
	Description            string              `json:"description"`
	Scope                  string              `json:"scope"`
	Areas                  []string            `json:"areas"`
	DependsOn              []string            `json:"depends_on"`
	EstimatedChangedLines  int                 `json:"estimated_changed_lines"`
	EstimatedReviewMinutes int                 `json:"estimated_review_minutes"`
	State                  UnitState           `json:"-"`
	AdmissionException     *AdmissionException `json:"admission_exception,omitempty"`
	present                workUnitPresence
}
type Plan struct {
	Summary          string            `json:"summary"`
	Scope            string            `json:"scope"`
	Units            []WorkUnit        `json:"work_units"`
	MaxParallelUnits int               `json:"max_parallel_units"`
	OverlapApprovals []OverlapApproval `json:"overlap_approvals,omitempty"`
	present          planPresence
}

type OverlapApproval struct {
	UnitIDs       []string `json:"unit_ids"`
	Justification string   `json:"justification"`
}

type workUnitPresence struct{ id, description, scope, areas, depends, lines, minutes bool }
type planPresence struct{ summary, scope, units, parallel bool }

func (u *WorkUnit) UnmarshalJSON(data []byte) error {
	type wire struct {
		ID                     *string             `json:"id"`
		Description            *string             `json:"description"`
		Scope                  *string             `json:"scope"`
		Areas                  *[]string           `json:"areas"`
		DependsOn              *[]string           `json:"depends_on"`
		EstimatedChangedLines  *int                `json:"estimated_changed_lines"`
		EstimatedReviewMinutes *int                `json:"estimated_review_minutes"`
		AdmissionException     *AdmissionException `json:"admission_exception"`
	}
	var value wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	u.present = workUnitPresence{value.ID != nil, value.Description != nil, value.Scope != nil, value.Areas != nil, value.DependsOn != nil, value.EstimatedChangedLines != nil, value.EstimatedReviewMinutes != nil}
	if value.ID != nil {
		u.ID = *value.ID
	}
	if value.Description != nil {
		u.Description = *value.Description
	}
	if value.Scope != nil {
		u.Scope = *value.Scope
	}
	if value.Areas != nil {
		u.Areas = *value.Areas
	}
	if value.DependsOn != nil {
		u.DependsOn = *value.DependsOn
	}
	if value.EstimatedChangedLines != nil {
		u.EstimatedChangedLines = *value.EstimatedChangedLines
	}
	if value.EstimatedReviewMinutes != nil {
		u.EstimatedReviewMinutes = *value.EstimatedReviewMinutes
	}
	u.AdmissionException = value.AdmissionException
	return nil
}

func (p *Plan) UnmarshalJSON(data []byte) error {
	type wire struct {
		Summary          *string           `json:"summary"`
		Scope            *string           `json:"scope"`
		Units            *[]WorkUnit       `json:"work_units"`
		MaxParallelUnits *int              `json:"max_parallel_units"`
		OverlapApprovals []OverlapApproval `json:"overlap_approvals"`
	}
	var value wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	p.present = planPresence{value.Summary != nil, value.Scope != nil, value.Units != nil, value.MaxParallelUnits != nil}
	if value.Summary != nil {
		p.Summary = *value.Summary
	}
	if value.Scope != nil {
		p.Scope = *value.Scope
	}
	if value.Units != nil {
		p.Units = *value.Units
	}
	if value.MaxParallelUnits != nil {
		p.MaxParallelUnits = *value.MaxParallelUnits
	}
	p.OverlapApprovals = value.OverlapApprovals
	return nil
}

var unitIDPattern = regexp.MustCompile(`^wu-[0-9a-f]{24}$`)

func Validate(p Plan) error {
	if p.present != (planPresence{}) && (!p.present.summary || !p.present.scope || !p.present.units || !p.present.parallel) {
		return fmt.Errorf("plan requires summary, scope, work_units, and max_parallel_units")
	}
	if strings.TrimSpace(p.Summary) == "" || utf8.RuneCountInString(p.Summary) > 200 {
		return fmt.Errorf("summary must contain 1-200 characters")
	}
	if p.MaxParallelUnits < 1 {
		return fmt.Errorf("max_parallel_units must be at least 1")
	}
	if len(p.Units) == 0 {
		return fmt.Errorf("work_units must not be empty")
	}
	knownIDs := map[string]bool{}
	for _, unit := range p.Units {
		if unit.present != (workUnitPresence{}) && (!unit.present.id || !unit.present.description || !unit.present.scope || !unit.present.areas || !unit.present.depends || !unit.present.lines || !unit.present.minutes) {
			return fmt.Errorf("work unit requires every declared field")
		}
		knownIDs[unit.ID] = true
	}
	seenOverlap := map[string]bool{}
	for _, approval := range p.OverlapApprovals {
		if len(approval.UnitIDs) != 2 || approval.UnitIDs[0] == approval.UnitIDs[1] || !knownIDs[approval.UnitIDs[0]] || !knownIDs[approval.UnitIDs[1]] || strings.TrimSpace(approval.Justification) == "" {
			return fmt.Errorf("invalid overlap approval")
		}
		a, b := approval.UnitIDs[0], approval.UnitIDs[1]
		if a > b {
			a, b = b, a
		}
		key := a + "\x00" + b
		if seenOverlap[key] {
			return fmt.Errorf("duplicate overlap approval")
		}
		seenOverlap[key] = true
	}
	for _, prefix := range strings.Split(p.Scope, ",") {
		if err := validPrefix(strings.TrimSpace(prefix)); err != nil {
			return err
		}
	}
	byID := make(map[string]WorkUnit, len(p.Units))
	for _, unit := range p.Units {
		if !unitIDPattern.MatchString(unit.ID) {
			return fmt.Errorf("invalid unit id %q", unit.ID)
		}
		if _, exists := byID[unit.ID]; exists {
			return fmt.Errorf("duplicate unit id %s", unit.ID)
		}
		byID[unit.ID] = unit
		if strings.TrimSpace(unit.Description) == "" || utf8.RuneCountInString(unit.Description) > 200 {
			return fmt.Errorf("unit %s description must contain 1-200 characters", unit.ID)
		}
		for _, prefix := range append([]string{unit.Scope}, unit.Areas...) {
			if err := validPrefix(prefix); err != nil {
				return fmt.Errorf("unit %s: %w", unit.ID, err)
			}
		}
		if unit.EstimatedChangedLines > 400 || unit.EstimatedReviewMinutes > 60 {
			if unit.AdmissionException == nil || strings.TrimSpace(unit.AdmissionException.Justification) == "" {
				return fmt.Errorf("unit %s requires an admission exception", unit.ID)
			}
		}
		if unit.EstimatedChangedLines < 0 || unit.EstimatedReviewMinutes < 0 {
			return fmt.Errorf("unit %s estimates must be non-negative", unit.ID)
		}
	}
	for _, unit := range p.Units {
		for _, dep := range unit.DependsOn {
			if _, ok := byID[dep]; !ok {
				return fmt.Errorf("unit %s has unknown dependency %s", unit.ID, dep)
			}
		}
	}
	if cycle := dependencyCycle(p.Units); cycle != "" {
		return fmt.Errorf("dependency cycle at %s", cycle)
	}
	for i := range p.Units {
		for j := i + 1; j < len(p.Units); j++ {
			if unitsOverlap(p.Units[i], p.Units[j]) && !approvedOverlap(p, p.Units[i].ID, p.Units[j].ID) {
				return fmt.Errorf("unit %s overlaps unit %s", p.Units[i].ID, p.Units[j].ID)
			}
		}
	}
	return nil
}

func Approve(p Plan, approved []string) error {
	if err := Validate(p); err != nil {
		return err
	}
	set := map[string]bool{}
	for _, id := range approved {
		if set[id] {
			return fmt.Errorf("duplicate admission exception approval %s", id)
		}
		set[id] = true
	}
	for _, unit := range p.Units {
		if unit.EstimatedChangedLines > 400 || unit.EstimatedReviewMinutes > 60 {
			if !set[unit.ID] {
				return fmt.Errorf("unit %s admission exception is not approved", unit.ID)
			}
			delete(set, unit.ID)
		}
	}
	if len(set) != 0 {
		return fmt.Errorf("approval names a unit without an admission exception")
	}
	return nil
}

func approvedOverlap(p Plan, a, b string) bool {
	for _, approval := range p.OverlapApprovals {
		if len(approval.UnitIDs) != 2 || approval.UnitIDs[0] == approval.UnitIDs[1] || strings.TrimSpace(approval.Justification) == "" {
			continue
		}
		if (approval.UnitIDs[0] == a && approval.UnitIDs[1] == b) || (approval.UnitIDs[0] == b && approval.UnitIDs[1] == a) {
			return true
		}
	}
	return false
}

func ReadyUnits(p Plan, activeHandles map[string]bool) []WorkUnit {
	states := map[string]UnitState{}
	active := 0
	for _, unit := range p.Units {
		states[unit.ID] = unit.State
		if activeHandles[unit.ID] {
			active++
		}
	}
	limit := p.MaxParallelUnits - active
	if limit < 1 {
		return nil
	}
	ready := make([]WorkUnit, 0, limit)
	for _, unit := range p.Units {
		if unit.State != Pending || activeHandles[unit.ID] {
			continue
		}
		ok := true
		for _, dep := range unit.DependsOn {
			if states[dep] != Done {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, unit)
			if len(ready) == limit {
				break
			}
		}
	}
	return ready
}

func validPrefix(prefix string) error {
	if prefix == "" || path.IsAbs(prefix) || path.Clean(prefix) != prefix || prefix == "." || prefix == ".." || strings.HasPrefix(prefix, "../") || strings.ContainsAny(prefix, "*?[\\") {
		return fmt.Errorf("invalid repository-relative prefix %q", prefix)
	}
	return nil
}
func prefixOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
func unitsOverlap(a, b WorkUnit) bool {
	aa := append([]string{a.Scope}, a.Areas...)
	bb := append([]string{b.Scope}, b.Areas...)
	for _, x := range aa {
		for _, y := range bb {
			if prefixOverlap(x, y) {
				return true
			}
		}
	}
	return false
}
func dependencyCycle(units []WorkUnit) string {
	deps := map[string][]string{}
	for _, u := range units {
		deps[u.ID] = u.DependsOn
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) string
	visit = func(id string) string {
		if visiting[id] {
			return id
		}
		if done[id] {
			return ""
		}
		visiting[id] = true
		for _, d := range deps[id] {
			if c := visit(d); c != "" {
				return c
			}
		}
		visiting[id] = false
		done[id] = true
		return ""
	}
	for id := range deps {
		if c := visit(id); c != "" {
			return c
		}
	}
	return ""
}
