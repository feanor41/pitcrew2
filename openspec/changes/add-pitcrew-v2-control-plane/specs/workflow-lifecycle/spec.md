<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/foundation-change (id 4441)
  - design.md § 9 (locally reconstructed)
NOT byte-identical to the originals. Original was ~95-110 lines.
The 9-state model and the high-level transition table are reconstructed
from the documented narrative.
-->

# Spec: workflow-lifecycle

## Purpose

Define the workflow aggregate, its 9 states, and the transition table
that governs every state mutation. The workflow aggregate is the
authoritative record of a coordinated execution. Its lifecycle is
narrow: every transition is a recorded event; no transition happens
silently.

## Requirements

### R1 — Workflow aggregate

A workflow SHALL be represented by a single row in the `workflows`
table plus an append-only `events` log. The aggregate SHALL carry:

- `id` (format `wf-<24hex>`)
- `revision` (monotonically increasing integer)
- `state` (one of the 9 states in R2)
- `goal` (the original user goal, set at `workflow new`)
- `created_at`, `updated_at` (RFC3339 UTC)
- `current_revision` mirrors the head of the events log

### R2 — States (9)

The workflow aggregate SHALL have exactly these 9 states:

1. `draft`
2. `exploring`
3. `specifying`
4. `designing`
5. `planning`
6. `plan_approved`
7. `implementing`
8. `reviewing` (per-unit; aggregate state remains `implementing`
   unless ALL units are `reviewing` or later)
9. `ready_to_complete`

Plus the terminal states:

- `completed`
- `abandoned`

### R3 — Transition table

The aggregate SHALL accept only the following transitions. Any other
transition SHALL return exit code `3`.

| From              | Event / subcommand                | To                  |
|-------------------|------------------------------------|---------------------|
| `draft`           | `workflow explore`                | `exploring`         |
| `draft`           | `workflow begin-implementation`   | `implementing` (no plan path; trivial path) |
| `exploring`       | `workflow spec`                   | `specifying`        |
| `exploring`       | `workflow design`                 | `designing`         |
| `exploring`       | `workflow begin-implementation`   | `implementing`      |
| `specifying`      | `workflow design`                 | `designing`         |
| `designing`       | `workflow plan`                   | `planning`          |
| `planning`        | `workflow approve-plan`           | `plan_approved`     |
| `plan_approved`   | `workflow begin-implementation`   | `implementing`      |
| `implementing`    | per-unit `unit-complete` (all done) | `ready_to_complete` |
| `ready_to_complete` | `workflow complete`            | `completed`         |
| any non-terminal  | `workflow abandon --reason <r>`    | `abandoned`         |

### R4 — Event log

Every transition SHALL append one row to `events` with:

- `workflow_id`
- `from_state`
- `to_state`
- `actor` (caller-supplied label; not authenticated)
- `revision_after`
- `at` (RFC3339 UTC)

### R5 — CAS-by-revision

Every state-mutating subcommand SHALL take `--revision <n>` and SHALL
succeed only when the current revision equals `<n>`. Mismatch SHALL
return exit code `4`. The Master SHALL re-inspect and SHALL NOT retry
blindly.

### R6 — Abandonment

`workflow abandon --reason <r>` SHALL be available from any
non-terminal state. Abandonment SHALL NOT delete the workflow, its
events, its plan, or its units. The reason SHALL be recorded in the
final event.

## Scenarios

### S1 — Happy path

> WHEN the user invokes `workflow new --goal "<goal>"`,
> THEN a workflow SHALL be created in state `draft`, revision `1`.

### S2 — Plan approval unlocks implementation

> WHEN `workflow approve-plan --revision <n>` succeeds against a
> workflow in state `planning`,
> THEN the workflow SHALL transition to `plan_approved`, revision
> `<n+1>`.

### S3 — Out-of-order transition

> WHEN `workflow spec` is invoked against a workflow in state
> `implementing`,
> THEN the CLI SHALL return exit code `3` and SHALL NOT mutate
> state.

### S4 — Abandonment is recorded

> WHEN `workflow abandon --reason "<r>"` is invoked against a
> workflow in any non-terminal state,
> THEN the workflow SHALL transition to `abandoned`, the reason
> SHALL be recorded, and the workflow SHALL remain queryable via
> `workflow show`.
