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
| `ready_to_complete` | `authorize-correction` for exhausted blocker | `ready_to_complete` |
| `ready_to_complete` | `recover-aggregate` after corrections | `implementing` |
| any non-terminal | `abandon` | `abandoned` |

#### Scenario: Each transition is enforced

- GIVEN each R2 source state
- WHEN its listed command or an unlisted command runs
- THEN only the listed transition SHALL succeed

### Requirement: Aggregate correction recovery

After a corrections aggregate review, the shared read-only correction projection SHALL expose policy awareness, rounds used/allowed, the latest unresolved blocker revision/content, `automatic|authorized|none` authority, and exactly one contextual next action. Initial review, findings, unit count, and failures consume no round; each successful grouped recovery or historical single-unit recovery consumes one. Over-budget history is exhausted. Automatic or exact unconsumed authorized authority returns `workflow recover-aggregate`; exhausted authority returns `user authorization required`; no blocker at `ready_to_complete` returns `workflow complete`; terminal returns `none`.

Policy-aware `recover-aggregate` SHALL require exact aggregate CAS and blocker revision, bounded non-empty causal groups whose unique union contains existing done units, and exactly one non-empty actor assignment per unit. It SHALL append one `aggregate_correction` artifact and one `aggregate_correction_started` activity, reopen all selected units once, revoke superseded handles, and move to `implementing` atomically. Historical plans MAY use the single-unit adapter. An exact unconsumed `correction_authorization` bound to the blocker grants one transaction. No path, hash, or secret SHALL persist in facts or activities.

#### Scenario: Recovery is a new correction cycle

- GIVEN `ready_to_complete` after a corrections aggregate verdict
- WHEN eligible grouped units are recovered
- THEN each selected unit becomes pending at its next revision
- AND the workflow becomes implementing at its next revision
- AND the original correction remains inspectable

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

### Requirement: Pinned delta continuation (REQ-CONT-001)

Continuation creation SHALL atomically pin the terminal predecessor workflow
id, exact revision, and ordered identities of its accepted exploration,
specification, and design artifacts. Schema-v1 stage inputs SHALL store typed
`requirement`, `scenario`, and `section` entries with stable ids, optional
parent ids, `add|replace|remove` operations, JSON bodies, and their exact
workflow/artifact/revision provenance. Phase and aggregate consumers SHALL use
a bounded resolver that applies the pinned baseline followed by ordered deltas
and emits each effective entry once with its winning provenance.

The resolver SHALL reject a nonterminal or revision-mismatched predecessor, an
artifact-manifest mismatch, a lineage cycle, lineage deeper than 32 edges,
duplicate stable ids, unknown replacements or removals, kind-changing
supersession, and scenarios without an effective requirement parent. Failed
creation, recording, or resolution SHALL NOT partially mutate the workflow.
Opaque historical artifacts and unrelated workflows SHALL remain standalone;
the resolver SHALL NOT parse prose to invent structure or identifiers, and a
continuation SHALL NOT upgrade them merely by existing.

#### Scenario: Supersession preserves exact provenance (SCN-CONT-001)

- GIVEN an immutable terminal predecessor with a pinned requirement and scenario
- WHEN its continuation replaces that requirement through a typed specification delta
- THEN the effective set SHALL contain the replacement exactly once with child artifact provenance
- AND the unchanged scenario SHALL retain exact predecessor artifact provenance
- AND mutable, mismatched, cyclic, over-depth, unknown, or duplicate lineage SHALL fail closed
- AND legacy prose-only and unrelated workflows SHALL retain standalone behavior

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
