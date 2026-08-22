# Spec: event-store

## Purpose

Define the project-local SQLite persistence, durable records, migrations, and revision safety.

## Requirements


### Requirement: Local single-process store

Each project SHALL use only `<project>/.pitcrew/state.db`; one invocation SHALL open one SQLite connection. There SHALL be no global registry, cross-project index, daemon, IPC, shared cache, HTTP, RPC, polling, or remote access. Filesystem permissions SHALL be the trust boundary; no additional encryption at rest is required.

#### Scenario: Projects remain isolated

- GIVEN two project roots
- WHEN each invokes the CLI
- THEN each SHALL access only its own local database

### Requirement: Connection policy

Every open SHALL apply `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`, `busy_timeout=5000`, and `temp_store=MEMORY`. A writer blocked beyond five seconds SHALL fail with exit code `3`.

#### Scenario: Busy writer times out

- GIVEN another writer holds a transaction
- WHEN five seconds elapse
- THEN exit code 3 SHALL result without partial mutation

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

Artifacts, events, activities, evidence, and reviews SHALL be append-only. Aggregate reviews SHALL use artifacts of kind `aggregate_review` rather than a new table or fake unit. Actor values SHALL be declarative collision/audit metadata, not credentials. Activities SHALL contain only navigation-safe identifiers: no claim secret, secret hash, handle contents, or handle path. Plain claim secrets SHALL never be stored.

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

### Requirement: Revision compare-and-swap

Every mutating command except `workflow new` SHALL require `--revision <n>`. Workflow-scoped commands SHALL compare it to the workflow revision; unit-scoped commands SHALL compare it to the work-unit revision. A mismatch SHALL return exit code `4` with no mutation. Multi-record effects SHALL be atomic; final `unit-complete` and aggregate-review-backed `complete` SHALL verify and update every affected current row in one transaction.

#### Scenario: Unit CAS mismatch

- GIVEN unit revision 3
- WHEN unit-review passes revision 2
- THEN exit code 4 SHALL result without changed rows

### Requirement: Migrations

Schema changes SHALL be ordered and versioned at startup. A migration that drops records or rewrites existing rows SHALL be rejected with a clear error naming the migration.

#### Scenario: Destructive migration is rejected

- GIVEN a migration drops or rewrites rows
- WHEN startup applies it
- THEN startup SHALL fail naming that migration

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
