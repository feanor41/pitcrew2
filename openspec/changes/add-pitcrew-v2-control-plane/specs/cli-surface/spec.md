<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/foundation-change (id 4441)
  - pitcrew2/agent-roles (id 4442)
  - pitcrew2/orchestration-model (id 4444)
  - design.md § 1, § 2 (locally reconstructed)
NOT byte-identical to the originals. Original was ~95-110 lines. The 16
subcommands, exit codes, flag conventions, and role contract are
verbatim from the documented layout.
-->

# Spec: cli-surface

## Purpose

Define the complete subcommand surface of the `pitcrew` CLI, the JSON
output envelope, the exit-code contract, and the flag conventions.
Authorization is **not** enforced by the CLI; every subcommand is
available to every caller. Role-based authorization is enforced by
prompt fragments.

## Requirements

### R1 — Subcommand surface (16 subcommands)

The CLI SHALL expose exactly the 16 subcommands listed in `design.md`
§ 1. The list is closed; adding a subcommand requires an OpenSpec
change.

### R2 — Output envelope

The CLI SHALL emit exactly one JSON document on stdout for every
successful invocation:

```
{
  "ok": true,
  "data": { "...": "..." },
  "warnings": [],
  "next_action": "..."
}
```

Failures SHALL emit a single-line error envelope on stderr with a
non-zero exit code; nothing SHALL be written to stdout on failure.

### R3 — Exit codes

The CLI SHALL use exactly these exit codes:

| Code | Meaning  |
|------|----------|
| 0    | ok       |
| 1    | internal |
| 2    | usage    |
| 3    | state    |
| 4    | cas      |
| 5    | handle   |

### R4 — Flag conventions

- All flags SHALL be long-form (`--flag`). No single-letter aliases.
- `--json` on `pitcrew principles` SHALL switch output to a
  structured array.
- `--handle-dir <dir>` SHALL be the only way to direct claim handle
  output.
- `--print-claim-handle-secret-once` SHALL be available only as an
  operator-only debugging escape; it SHALL NOT appear in the
  production `--help` text. Agent templates SHALL NOT use this flag.
- `--approve-exception <unit-id>` MAY be passed per unit at plan
  approval to bypass admission for an indivisible unit.

### R5 — Help epilogue

Every `--help` output SHALL end with the line:

> Read the four maxims of the harness: `pitcrew principles`.

### R6 — Callers

The CLI SHALL accept invocations from any caller. The CLI SHALL NOT
distinguish callers by identity. Authorization SHALL be a prompt
concern, not a CLI concern.

### R7 — Role contract (subcommand map)

The eight roles SHALL consume exactly these subcommands:

| Role         | Subcommands                          |
|--------------|---------------------------------------|
| Master       | `workflow new`, `workflow show`, `workflow approve-plan`, `workflow complete` (optional) |
| Explorer     | `workflow explore`                    |
| Specifier    | `workflow spec`                       |
| Designer     | `workflow design`                     |
| TaskPlanner  | `workflow plan`                       |
| Implementer  | `workflow claim-unit`, `workflow unit-tdd`, `workflow unit-complete` |
| Reviewer     | `workflow unit-review`                |
| Archivist    | `workflow complete`                   |

The Implementer SHALL NOT invoke `workflow unit-review`. The Reviewer
SHALL NOT invoke any other subcommand.

### R8 — Hand-off contract

The CLI SHALL NOT enforce any role-to-role hand-off rule. Roles
SHALL invoke subcommands directly; the Master SHALL NOT relay role
output. The Implementer SHALL return the handle **path** (not the
handle contents) to the Master.

## Scenarios

### S1 — Happy-path subcommand

> WHEN the Master invokes `workflow new --goal "<goal>"`,
> THEN the CLI SHALL emit a JSON envelope on stdout with
> `ok: true`, a `data.workflow.id` of the form `wf-<24hex>`,
> `data.workflow.revision: 1`, and `next_action` describing the
> next legal transition.

### S2 — State error

> WHEN an agent invokes a subcommand out of order (for example,
> `workflow unit-tdd` before `workflow claim-unit`),
> THEN the CLI SHALL return exit code `3` and an error envelope
> naming the current state and the expected state.

### S3 — CAS mismatch

> WHEN an agent passes `--revision <n>` and the current revision
> is `<n+1>` or later,
> THEN the CLI SHALL return exit code `4` and SHALL NOT mutate
> state. The Master SHALL re-inspect and SHALL NOT retry blindly.

### S4 — Handle error

> WHEN a unit subcommand is invoked with an expired or invalid
> handle,
> THEN the CLI SHALL return exit code `5` and SHALL delete the
> expired handle file atomically.

### S5 — Same-identity prohibition

> WHEN the same runtime identity attempts to invoke both
> `workflow unit-tdd` and `workflow unit-review` for the same
> unit revision,
> THEN the CLI SHALL return exit code `3` with a clear error
> naming the rule. The CLI SHALL NOT rely on identity alone;
> the prohibition is enforced by prompt fragments and the
> handle's `claim_generation`.
