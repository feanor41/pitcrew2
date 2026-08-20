<!--
Reconstructed from Engram observations on 2026-08-20.
Source observation:
  - pitcrew2/claim-handles (id 4438)
  - design.md § 2 (locally reconstructed)
NOT byte-identical to the originals. The handle file format, lifecycle,
recovery, and operator-only escape are reconstructed from the verbatim
content of observation 4438.
-->

# Spec: claim-handles

## Purpose

Define the only claim path that pitcrew v2 supports: opaque handle
files written to a caller-supplied directory. No raw bearer secret is
ever emitted on stdout, ever persisted in a handle file, or ever
returned to a Master. The handle claim protects secrets from leaking
into agent context, logs, and shell history.

## Requirements

### R1 — Opaque-only claim path

The CLI SHALL support one and only one claim path: opaque handle
files at file mode `0600`, written into a caller-supplied
`--handle-dir`. The CLI SHALL NOT expose:

- a `--emit-plain-token` flag,
- a `--claim-token` alternative on any per-unit subcommand,
- an agent template that produces a raw bearer secret.

### R2 — Handle file format

A handle file SHALL be a single JSON document:

```
{
  "version": 1,
  "state": "intent|active",
  "workflow_id": "wf-<24hex>",
  "unit_id": "wu-<24hex>",
  "claim_id": "<32hex>",
  "secret_hash": "<sha256hex>",
  "issued_at": "<RFC3339 UTC>",
  "expires_at": "<RFC3339 UTC>"
}
```

The plain secret SHALL never be written to the handle file. Only its
`sha256` is persisted.

### R3 — File and directory permissions

- The handle file SHALL have mode `0600`.
- The handle directory SHALL have mode `0700`.
- The owner of the file SHALL equal the caller.
- The CLI SHALL refuse to follow symlinks when reading the handle
  file.

### R4 — Lifecycle

- A handle SHALL be issued by `workflow claim-unit` with state
  `intent`.
- The handle SHALL be promoted to state `active` on the first
  successful unit subcommand.
- The expiry SHALL be **5 minutes** from `issued_at`.
- Every successful unit subcommand SHALL refresh `expires_at`.
- An expired handle SHALL be rejected with exit code `5`, and the
  handle file SHALL be deleted atomically.

### R5 — Operator-only escape

The CLI SHALL accept `--print-claim-handle-secret-once` as an
operator-only debugging flag. When used:

- The plain secret SHALL be printed to stdout exactly once.
- The handle SHALL be revoked immediately in the same process.
- The flag SHALL NOT appear in the production `--help` text of
  the subcommand.
- Agent templates SHALL NOT use this flag. The flag is for
  operators only.

### R6 — Recovery

A new handle MAY be issued via `workflow recover-unit-claim` only if:

- the unit is not in state `reviewing`, AND
- the unit has no TDD evidence for the current revision.

Recovery SHALL increment `claim_generation`. The recovered token
SHALL be a fresh secret; the old one SHALL be revoked.

### R7 — Hand-off discipline

The Implementer SHALL return the handle **path** (not the handle
contents) to the Master. The Master SHALL pass the handle path to
the Reviewer. The handle contents SHALL never leave the Implementer
or Reviewer.

## Scenarios

### S1 — Claim issues an intent handle

> WHEN the Implementer invokes
> `workflow claim-unit --unit <wu-id> --handle-dir <dir>`,
> THEN the CLI SHALL write a handle file at `<dir>/<claim_id>.json`
> with mode `0600`, state `intent`, and `expires_at` 5 minutes
> from `issued_at`. The CLI SHALL return the path on stdout.

### S2 — First unit subcommand promotes the handle

> WHEN the Implementer invokes `workflow unit-tdd --claim <path>`,
> THEN the CLI SHALL validate the handle, promote it to `active`,
> refresh `expires_at`, and run the TDD recording. Exit code `0`
> on success; `5` on expiry or invalid handle.

### S3 — Expired handle

> WHEN the Implementer invokes any unit subcommand with an
> expired handle,
> THEN the CLI SHALL return exit code `5` and delete the handle
> file atomically. No state mutation SHALL occur.

### S4 — Recovery after expired handle

> WHEN the Implementer invokes `workflow recover-unit-claim` and
> the unit is not in `reviewing` and has no TDD evidence for the
> current revision,
> THEN the CLI SHALL issue a new handle with incremented
> `claim_generation` and return its path.

### S5 — Operator-only escape revokes immediately

> WHEN an operator invokes
> `workflow claim-unit --print-claim-handle-secret-once`,
> THEN the CLI SHALL print the plain secret to stdout exactly
> once and revoke the handle in the same process. The `--help`
> text SHALL NOT mention this flag.
