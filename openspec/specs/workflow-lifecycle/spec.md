# Spec: workflow-lifecycle

## Purpose

Define the workflow aggregate, legal transitions, durable stage artifacts, and inspection.

## Requirements


### Requirement: Aggregate and states

A workflow SHALL carry `id` (`wf-<24hex>`), monotonic `revision` mirroring the event-log head, explicit bounded short `name`, `goal`, UTC `created_at`/`updated_at`, and state. Ordinary states SHALL be `draft`, `exploring`, `specifying`, `designing`, `planning`, `plan_approved`, `implementing`, `ready_to_complete`, and terminal `completed`; `abandoned` SHALL be exceptional terminal. Work-unit `reviewing` SHALL NOT be an aggregate state. Historical rows created before names were persisted MAY lack `name`; read projections SHALL then derive a deterministic bounded fallback from the goal, mark it as derived, and SHALL NOT update the row. New workflows SHALL NOT use this fallback.

(Previously: Workflows carried no short name and no historical fallback rule.)

#### Scenario: Workflow creation

- GIVEN a non-empty explicit name, goal, and actor
- WHEN `workflow new` succeeds
- THEN it SHALL persist and return `data.workflow` with that exact name in draft revision 1 and a `wf-<24hex>` id

#### Scenario: Historical unnamed workflow

- GIVEN a pre-migration workflow row without a name
- WHEN it is projected for inspection
- THEN a deterministic bounded goal-derived name SHALL be returned as marked fallback
- AND the persisted row SHALL remain unchanged

### Requirement: Transition table

Only these transitions SHALL succeed; others SHALL return exit code `3` without mutation.

| From | Command | To |
|---|---|---|
| `draft` | `explore` | `exploring` |
| `exploring` | `explore` | `exploring` |
| `exploring` | `spec` | `specifying` |
| `exploring` | `design` | `designing` |
| `specifying` | `spec` | `specifying` |
| `specifying` | `design` | `designing` |
| `designing` | `design` | `designing` |
| `designing` | `plan` | `planning` |
| `planning` | `approve-plan` | `plan_approved` |
| `plan_approved` | `begin-implementation` | `implementing` |
| `implementing` | final `unit-complete` | `ready_to_complete` |
| `ready_to_complete` | approved `complete` | `completed` |
| `ready_to_complete` | corrections `complete` | `ready_to_complete` |
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

The command SHALL infer kind `exploration`, `specification`, or `design`. On the legal transition it SHALL append a durable artifact with workflow id, kind, content, caller-declared actor, accepted workflow revision, and timestamp. The control plane SHALL accept a repeated stage command while the workflow remains in its corresponding stage; each repetition SHALL append a new artifact and event, increment the revision, preserve the state, and keep the forward `next_action`. Artifacts SHALL NOT be overwritten or deleted by later transitions or abandonment.

#### Scenario: All stage artifacts persist

- GIVEN each stage command in its legal state with valid input
- WHEN explore, spec, and design succeed
- THEN each exact content, kind, actor, and revision SHALL persist

#### Scenario: A stage amendment remains append-only

- GIVEN a workflow in `exploring`, `specifying`, or `designing`
- WHEN its corresponding stage command succeeds again at the current revision
- THEN it SHALL append the amendment and a self-loop event at the next revision
- AND it SHALL preserve the current state and forward `next_action`
- AND stage commands SHALL remain rejected from later and terminal states

### Requirement: Inspection

`workflow show --workflow-id` SHALL return the aggregate in `data.workflow`, every durable stage artifact in `data.artifacts`, and review-relevant history in `data.records` and `data.timeline`. Inspection SHALL expose plans, units, evidence, and reviews without handles or secrets. A completed or abandoned workflow SHALL remain queryable.

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
