# Spec: workflow-lifecycle

## Purpose

Define the workflow aggregate, legal transitions, durable stage artifacts, and inspection.

## Requirements


### Requirement: Aggregate and states

A workflow SHALL carry `id` (`wf-<24hex>`), monotonic `revision` mirroring the event-log head, `goal`, UTC `created_at`/`updated_at`, and state. Ordinary states SHALL be `draft`, `exploring`, `specifying`, `designing`, `planning`, `plan_approved`, `implementing`, `ready_to_complete`, and terminal `completed`; `abandoned` SHALL be exceptional terminal. Work-unit `reviewing` SHALL NOT be an aggregate state.

#### Scenario: Workflow creation

- GIVEN a non-empty goal and actor
- WHEN workflow new succeeds
- THEN it SHALL create and return `data.workflow` in draft revision 1 with a `wf-<24hex>` id

### Requirement: Transition table

Only these transitions SHALL succeed; others SHALL return exit code `3` without mutation.

| From | Command | To |
|---|---|---|
| `draft` | `explore` | `exploring` |
| `draft` | `begin-implementation` | `implementing` |
| `exploring` | `spec` | `specifying` |
| `exploring` | `design` | `designing` |
| `exploring` | `begin-implementation` | `implementing` |
| `specifying` | `design` | `designing` |
| `designing` | `plan` | `planning` |
| `planning` | `approve-plan` | `plan_approved` |
| `plan_approved` | `begin-implementation` | `implementing` |
| `implementing` | final `unit-complete` | `ready_to_complete` |
| `ready_to_complete` | `complete` | `completed` |
| any non-terminal | `abandon` | `abandoned` |

#### Scenario: Each transition is enforced

- GIVEN each R2 source state
- WHEN its listed command or an unlisted command runs
- THEN only the listed transition SHALL succeed

### Requirement: Stage artifact input and retention

`explore`, `spec`, and `design` SHALL each read from `--input-file` exactly one JSON object:

```json
{"content":"non-empty UTF-8 string"}
```

The command SHALL infer kind `exploration`, `specification`, or `design`. On the legal transition it SHALL append a durable artifact with workflow id, kind, content, caller-declared actor, accepted workflow revision, and timestamp. Artifacts SHALL NOT be overwritten or deleted by later transitions or abandonment.

#### Scenario: All stage artifacts persist

- GIVEN each stage command in its legal state with valid input
- WHEN explore, spec, and design succeed
- THEN each exact content, kind, actor, and revision SHALL persist

### Requirement: Inspection

`workflow show --workflow-id` SHALL return the aggregate in `data.workflow` and every durable stage artifact in `data.artifacts`, ordered by accepted revision then insertion order. Each artifact SHALL expose `kind`, `content`, `actor`, `revision`, and `recorded_at`. A completed or abandoned workflow SHALL remain queryable.

#### Scenario: Show retrieves durable artifacts

- GIVEN a workflow with three stage artifacts
- WHEN workflow show runs
- THEN data.artifacts SHALL return all three in required order

### Requirement: Events, CAS, and abandonment

Every transition SHALL append an event containing workflow id, from/to state, declarative actor, resulting revision, UTC time, and reason (empty except abandonment). Mutating workflow commands SHALL require the current workflow `--revision`; mismatch SHALL return exit code `4`. `abandon` SHALL require non-empty `--reason` and SHALL retain all workflow data.

#### Scenario: Abandonment is retained

- GIVEN a non-terminal workflow at the supplied revision
- WHEN abandon succeeds with a reason
- THEN its final event and all prior data SHALL remain queryable
