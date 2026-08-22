# Delta for event-store

## MODIFIED Requirements

### Requirement: Durable schema

The schema SHALL preserve:

| Record | Required data |
|---|---|
| workflows | id, revision, state, goal, nullable historical name, created/updated times |
| events | workflow id, from/to state, actor, reason, resulting revision, time |
| activities | id, workflow id, optional unit id, action, actor, time, stable subject kind and id |
| artifacts | workflow id, kind, content, actor, accepted revision, time |
| plans | workflow id, summary, scope, parallelism, JSON body |
| work units | id, workflow id, definition fields, state, exception JSON, revision |
| evidence | workflow/unit ids, unit revision, actor, TDD fields, time |
| reviews | workflow/unit ids, unit revision, actor, verdict fields, time |
| handles | claim/workflow/unit ids, state, secret hash, times, generation, actor identity |

Artifacts, events, activities, evidence, and reviews SHALL be append-only. Actor values SHALL be declarative collision/audit metadata, not credentials. Activities SHALL contain only navigation-safe identifiers: no claim secret, secret hash, handle contents, or handle path. Plain claim secrets SHALL never be stored.

(Previously: The schema had no workflow name or activity ledger.)

#### Scenario: Artifacts remain durable

- GIVEN an accepted design artifact
- WHEN later transitions occur
- THEN its append-only row SHALL remain unchanged and queryable

#### Scenario: Additive historical migration

- GIVEN a database created before workflow names and activities
- WHEN the ordered migration runs
- THEN existing rows SHALL remain unchanged and queryable
- AND no historical activity SHALL be fabricated

## ADDED Requirements

### Requirement: Transactional control-plane activity

Every successful mutating workflow command SHALL append exactly one activity in the same transaction as its durable result. The activity SHALL identify its exact result through stable subject kind and id and record the declared actor, action, and UTC timestamp. This ledger SHALL be additive to workflow transition events. Failed commands and read-only interactions, including `show`, `list-ready-units`, `tui`, `principles`, help, and version, SHALL append no activity.

#### Scenario: Successful mutation commits one navigable activity

- GIVEN any valid mutating command
- WHEN its transaction commits
- THEN its result and exactly one activity SHALL commit together
- AND the activity SHALL resolve to that exact result

#### Scenario: Failed mutation leaves no activity

- GIVEN a mutating command fails validation, state, CAS, handle, or transaction processing
- WHEN persisted rows are inspected
- THEN neither its result nor an activity for that attempt SHALL exist

#### Scenario: Reads leave no activity

- GIVEN any read-only interaction
- WHEN it succeeds
- THEN the activity ledger SHALL remain unchanged
