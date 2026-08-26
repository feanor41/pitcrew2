# Spec: plan-and-work-units

## Purpose

Define plan submission, validation, admission, approval, and executable-unit discovery.

## Requirements


### Requirement: Plan input

`workflow plan --input-file` SHALL read one JSON object with required `summary` (1–200 characters), `scope` (comma-separated repository-relative prefixes), `max_parallel_units` (integer ≥1), and non-empty `work_units` array. Each unit SHALL contain `id` (`wu-<24hex>`), `description` (1–200 characters), `scope`, `areas` (array), `depends_on` (unit-id array), `estimated_changed_lines` (non-negative integer), and `estimated_review_minutes` (non-negative integer); it MAY contain `admission_exception: {"justification":"non-empty"}`. The plan MAY contain `overlap_approvals`, an array of objects with exactly two distinct `unit_ids` and non-empty `justification`. No other payload transport SHALL exist.

#### Scenario: Valid plan is preserved

- GIVEN a valid plan JSON file
- WHEN workflow plan succeeds
- THEN every submitted unit field and order SHALL persist

### Requirement: Prefixes and dependencies

Plan/unit scope and areas SHALL be normalized repository-relative file or directory prefixes without globs. Unit ids SHALL be unique. Every dependency SHALL name a submitted unit, and the graph SHALL be acyclic. Any overlap among two units' scope/areas SHALL return exit code `3` unless one `overlap_approvals` entry names that exact pair; the approval SHALL become effective only when Aion approves the plan.

#### Scenario: Invalid graph is rejected

- GIVEN an unknown dependency, cycle, or unapproved overlap
- WHEN the plan is submitted
- THEN exit code 3 SHALL result without persisted plan data

### Requirement: Admission and approval

A unit SHALL be admitted by default only when changed lines ≤400 and review minutes ≤60. A larger indivisible unit SHALL declare an admission exception and SHALL require a matching repeatable `--approve-exception <unit-id>` on `approve-plan`. Approval SHALL reject missing, unknown, duplicate, or unnecessary exception flags and SHALL persist approved exceptions.

#### Scenario: Oversized unit requires approval

- GIVEN an over-budget unit with justification
- WHEN approval omits its exception flag
- THEN exit code 3 SHALL result without plan approval

### Requirement: Readiness and parallelism

`list-ready-units --workflow-id` SHALL return in `data.units` only pending units whose dependencies are done and which lack an active handle, capped by `max_parallel_units` minus active claims. Ordering SHALL follow plan order. The CLI, not the agent, SHALL decide readiness.

#### Scenario: Ready units honor capacity

- GIVEN three ready units, one active claim, and parallelism two
- WHEN list-ready-units runs
- THEN only the first unclaimed unit SHALL be returned

### Requirement: Stacked delivery

Aion MAY split delivery into stacked changes; each chosen slice SHALL target ≤400 changed lines and ≤60 review minutes. This is an orchestration decision; the CLI SHALL only enforce per-unit admission.

#### Scenario: Stacking remains external

- GIVEN an admitted large delivery
- WHEN readiness is calculated
- THEN the CLI SHALL NOT create or reorder stacked changes
