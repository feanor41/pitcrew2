<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/foundation-change (id 4441)
  - design.md § 10 (locally reconstructed)
NOT byte-identical to the originals. Original was ~95-110 lines. The
plan, work unit, dependency, admission, readiness, and exception rules
are reconstructed from the documented narrative.
-->

# Spec: plan-and-work-units

## Purpose

Define the shape of a plan, the shape of a work unit, the rules for
dependency validation and admission, and the rules for readiness and
exceptions. The plan is the bridge between the Designer (technical
design) and the Implementer (unit-by-unit execution).

## Requirements

### R1 — Plan shape

A plan SHALL be a JSON document with:

- `summary` (short string, ≤ 200 chars)
- `scope` (comma-separated list of repository-relative paths)
- `work_units` (array of work unit definitions, see R2)
- `max_parallel_units` (integer ≥ 1)

### R2 — Work unit shape

Each work unit SHALL have:

- `id` (assigned at submission; format `wu-<24hex>`)
- `description` (short string, ≤ 200 chars)
- `scope` (repository-relative prefix; no globs)
- `areas` (array of repository-relative prefixes; no globs)
- `depends_on` (array of unit ids that must complete first)
- `estimated_changed_lines` (integer; advisory)
- `estimated_review_minutes` (integer; advisory)
- `admission_exception` (optional object with `justification`)

### R3 — Repository-relative prefixes

Plan-level and work-unit `scope` and `areas` SHALL be exact
repository-relative file or directory prefixes. They SHALL NOT use
`*`, `?`, or `[` glob characters.

### R4 — Dependency validation

The plan SHALL be rejected if:

- any `depends_on` references an unknown unit id,
- the dependency graph contains a cycle,
- any two units have overlapping `scope` or `areas` prefixes and
  the implementation route does not explicitly approve parallel
  writing into the same prefix.

### R5 — Admission policy

The default plan SHALL be admitted when:

- every work unit has `estimated_changed_lines ≤ 400`, AND
- every work unit has `estimated_review_minutes ≤ 60`.

A unit MAY exceed the default budget only with
`admission_exception.justification` and explicit per-unit approval
via `--approve-exception <unit-id>` at plan approval. The exception
SHALL be recorded in the plan.

### R6 — Readiness

A work unit SHALL be reported as `ready` by `workflow list-ready-units`
when:

- all `depends_on` units are in state `done`,
- the unit is in state `pending`,
- no handle is currently active for the unit.

The CLI SHALL decide readiness and parallelism; the agent SHALL NOT.

### R7 — Stacked changes

A plan SHALL be splittable into stacked changes. Each stacked change
SHALL be ≤ ~400 changed lines and ≤ ~60 review minutes. Splitting is
an explicit decision by the Master, not the CLI.

### R8 — Plan approval

`workflow approve-plan` SHALL accept either:

- a clean approval (all units admitted), OR
- an approval with one or more `--approve-exception <unit-id>`
  flags, each corresponding to a unit with `admission_exception`.

Approval SHALL NOT be possible if the plan contains units that exceed
the budget and have no `admission_exception`.

## Scenarios

### S1 — Plan submission with cycle

> WHEN the TaskPlanner submits a plan whose dependency graph
> contains a cycle,
> THEN the CLI SHALL reject the plan with exit code `3` and a
> message naming the cycle.

### S2 — Plan approval with exception

> WHEN the Master invokes
> `workflow approve-plan --approve-exception <unit-id>`,
> THEN the CLI SHALL record the exception, transition the
> workflow to `plan_approved`, and the unit SHALL remain eligible
> for execution.

### S3 — Readiness listing

> WHEN the Implementer invokes `workflow list-ready-units`,
> THEN the CLI SHALL return only units whose deps are satisfied,
> who are `pending`, and who have no active handle.

### S4 — Overlapping scope

> WHEN two work units declare overlapping `scope` prefixes and the
> Master has not explicitly approved parallel writing into that
> prefix,
> THEN the plan SHALL be rejected with exit code `3`.
