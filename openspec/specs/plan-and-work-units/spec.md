# Spec: plan-and-work-units

## Purpose

Define plan submission, validation, admission, approval, and executable-unit discovery.

## Requirements


### Requirement: Plan input

`workflow plan --input-file` SHALL read one JSON object with required `summary` (1–200 characters), `scope` (comma-separated repository-relative prefixes), `max_parallel_units` (integer ≥1), and non-empty `work_units` array. Each unit SHALL contain `id` (`wu-<24hex>`), `description` (1–200 characters), `scope`, `areas` (array), `depends_on` (unit-id array), `estimated_changed_lines` (non-negative integer), and `estimated_review_minutes` (non-negative integer); it MAY contain `admission_exception: {"justification":"non-empty"}`. The plan MAY contain `overlap_approvals`, an array of objects with exactly two distinct `unit_ids` and non-empty `justification`. It SHALL contain normalized `aggregate_correction_policy: {"automatic_rounds":0|1,"on_exhaustion":"require_user_authorization"}`; omission on a new submission normalizes to one automatic round, while historical stored bodies missing it project that default without row rewriting and remain distinguishable as historical. Malformed, unknown, negative, or over-budget policy values SHALL reject before mutation. No other payload transport SHALL exist.

#### Scenario: Valid plan is preserved

- GIVEN a valid plan JSON file
- WHEN workflow plan succeeds
- THEN every submitted unit field and order SHALL persist

### Requirement: Plan amendment authority is unavailable

`amend-plan` SHALL accept the same validated plan input only to enforce the closed CLI contract. This revision has no opaque, non-forgeable plan-amendment authority, so it SHALL reject every amendment with exit `3` before reading or replacing the current plan. Workflow state and CAS are not authorization, and declarative `--actor` labels SHALL not become a substitute authority.

#### Scenario: Planning record cannot be overwritten by a label

- GIVEN a workflow in `planning` with a valid submitted plan
- WHEN `amend-plan` is invoked by `aion` or any other actor with any revision
- THEN the plan and its work units SHALL remain unchanged

### Requirement: Plan amendment

`workflow amend-plan` SHALL validate the same strict plan input and require `--workflow-id`, `--revision`, and `--actor`. The current control plane has no opaque plan-amendment authority, so it SHALL reject amendment mutation rather than treat a caller label, workflow state, or CAS as authorization. `--actor` SHALL remain declarative metadata and SHALL NOT grant or deny authorization. Any future authorized amendment SHALL preserve the replaced plan body as an immutable `plan_superseded` artifact, atomically replace pending work-unit rows, remain in `planning`, and advance the workflow revision.

#### Scenario: Declarative actor cannot authorize amendment

- GIVEN any non-empty actor label, including `aion`, and otherwise valid planning input
- WHEN `amend-plan` is invoked without a structural amendment authority
- THEN it SHALL fail without replacing the plan or adding an artifact

#### Scenario: Amendment CAS and terminal protection

- GIVEN a stale revision or a workflow outside `planning`
- WHEN `amend-plan` is invoked
- THEN it SHALL fail without replacing the plan or adding an artifact

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
