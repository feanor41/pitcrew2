# Delta for workflow-lifecycle

## MODIFIED Requirements

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
