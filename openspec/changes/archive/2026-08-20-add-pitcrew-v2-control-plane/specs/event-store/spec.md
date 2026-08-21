# Spec: event-store

## Purpose

Define the project-local SQLite persistence, durable records, migrations, and revision safety.

## ADDED Requirements


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
| workflows | id, revision, state, goal, created/updated times |
| events | workflow id, from/to state, actor, reason, resulting revision, time |
| artifacts | workflow id, kind, content, actor, accepted revision, time |
| plans | workflow id, summary, scope, parallelism, JSON body |
| work units | id, workflow id, definition fields, state, exception JSON, revision |
| evidence | workflow/unit ids, unit revision, actor, TDD fields, time |
| reviews | workflow/unit ids, unit revision, actor, verdict fields, time |
| handles | claim/workflow/unit ids, state, secret hash, times, generation, actor identity |

Artifacts, events, evidence, and reviews SHALL be append-only. Actor values SHALL be declarative collision/audit metadata, not credentials. Plain claim secrets SHALL never be stored.

#### Scenario: Artifacts remain durable

- GIVEN an accepted design artifact
- WHEN later transitions occur
- THEN its append-only row SHALL remain unchanged and queryable

### Requirement: Revision compare-and-swap

Every mutating command except `workflow new` SHALL require `--revision <n>`. Workflow-scoped commands SHALL compare it to the workflow revision; unit-scoped commands SHALL compare it to the work-unit revision. A mismatch SHALL return exit code `4` with no mutation. Multi-record effects SHALL be atomic; when final `unit-complete` also updates the aggregate, it SHALL verify and update the then-current workflow row in the same transaction.

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
