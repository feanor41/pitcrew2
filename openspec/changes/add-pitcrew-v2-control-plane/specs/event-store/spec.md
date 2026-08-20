<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/foundation-change (id 4441)
  - design.md § 5 (locally reconstructed)
NOT byte-identical to the originals. Original was ~95-110 lines. The
SQLite schema, PRAGMAs, migrations, and CAS contract are reconstructed
from the documented narrative.
-->

# Spec: event-store

## Purpose

Define the local SQLite store that backs the workflow aggregate, the
event log, plans, work units, evidence, reviews, and handles. The
store is local to one project, one writer per process, no daemon, no
IPC, no shared cache.

## Requirements

### R1 — Per-project store

Each project SHALL have its own store at `<project>/.pitcrew/state.db`.
The CLI SHALL operate on exactly one project per invocation. There
SHALL be no global registry, no cross-project index, no cross-project
coordination.

### R2 — One writer per database

Each CLI invocation SHALL open exactly one SQLite connection. There
SHALL be no daemon, no IPC, no shared cache. The busy timeout SHALL
reject concurrent writers after 5 seconds.

### R3 — PRAGMAs

The store SHALL apply these PRAGMAs on every connection open:

- `journal_mode = WAL`
- `synchronous = NORMAL`
- `foreign_keys = ON`
- `busy_timeout = 5000`
- `temp_store = MEMORY`

### R4 — Schema (high level)

The schema SHALL include:

- `workflows` — one row per workflow aggregate
  (`id`, `revision`, `state`, `goal`, `created_at`, `updated_at`).
- `events` — append-only log of state transitions
  (`workflow_id`, `from_state`, `to_state`, `actor`, `revision_after`, `at`).
- `plans` — one row per approved plan (`workflow_id`, `summary`,
  `scope`, `max_parallel_units`, `body` JSON).
- `work_units` — one row per unit (`id`, `workflow_id`, `description`,
  `scope`, `areas`, `depends_on`, `estimated_changed_lines`,
  `estimated_review_minutes`, `state`, `admission_exception` JSON).
- `evidence` — one row per unit TDD/validation event
  (`workflow_id`, `unit_id`, `revision`, `red_command`, `red_outcome`,
  `green_command`, `green_outcome`, `refactor_summary`,
  `validation_command`, `validation_outcome`, `changed_paths`,
  `recorded_at`).
- `reviews` — one row per unit review verdict (`workflow_id`,
  `unit_id`, `revision`, `verdict`, `summary`, `findings`,
  `plan_impact`, `recorded_at`).
- `handles` — one row per issued claim handle (`claim_id`,
  `workflow_id`, `unit_id`, `state`, `secret_hash`, `issued_at`,
  `expires_at`, `claim_generation`).

### R5 — Migrations

Schema changes SHALL be versioned. The store SHALL apply migrations
in order at startup. No destructive migration SHALL be accepted; any
migration that drops or rewrites existing rows SHALL be rejected at
load time with a clear error.

### R6 — CAS-by-revision

Every state-mutating subcommand SHALL take `--revision <n>` and SHALL
succeed only if the current revision equals `<n>`. Mismatch SHALL
return exit code `4`. The CLI SHALL NOT mutate state on mismatch.

### R7 — No remote access

The store SHALL NOT expose any HTTP, gRPC, WebSocket, RPC, polling,
or remote procedure. All access SHALL be via the local subprocess
CLI.

### R8 — Encryption

The store SHALL NOT encrypt data at rest beyond what the filesystem
provides. The trust boundary is filesystem permissions.

## Scenarios

### S1 — Store opens with default PRAGMAs

> WHEN the CLI opens `<project>/.pitcrew/state.db`,
> THEN the connection SHALL apply all PRAGMAs from R3 and SHALL
> be ready to read/write.

### S2 — CAS success

> WHEN a subcommand is invoked with `--revision 7` and the
> current revision is `7`,
> THEN the mutation SHALL be applied and the revision SHALL
> increment to `8`.

### S3 — CAS mismatch

> WHEN a subcommand is invoked with `--revision 7` and the
> current revision is `8`,
> THEN the mutation SHALL NOT be applied and the CLI SHALL
> return exit code `4`.

### S4 — Destructive migration rejected

> WHEN a proposed migration drops or rewrites existing rows,
> THEN the store SHALL refuse to load it at startup with a clear
> error naming the offending migration.

### S5 — Busy timeout

> WHEN a second CLI invocation attempts to open the same store
> while the first is mid-transaction,
> THEN the second invocation SHALL wait up to `busy_timeout`
> milliseconds and SHALL return exit code `3` if the timeout
> elapses.
