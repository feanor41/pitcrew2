<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/foundation-change (id 4441)
  - pitcrew2/claim-handles (id 4438)
  - pitcrew2/product-shape (id 4437)
  - pitcrew2/agent-roles (id 4442)
  - pitcrew2/orchestration-model (id 4444)
  - pitcrew2/scope (id 4445)
  - pitcrew2/maxims (id 4443)
NOT byte-identical to the originals. Section ordering follows the documented
layout; technical decisions are verbatim from Engram where stated. Original
was ~422 lines.
-->

# Design: add-pitcrew-v2-control-plane

This document closes every open technical question raised during the
foundation proposal. It is normative; later implementation work treats
the decisions here as binding unless an OpenSpec change explicitly
amends them.

---

## 1. CLI surface — 16 subcommands (closed list)

The CLI exposes exactly 16 subcommands. New subcommands require an
explicit OpenSpec change.

| Group       | Subcommand                       | Purpose                          |
|-------------|----------------------------------|----------------------------------|
| workflow    | `workflow new`                   | start a workflow                 |
| workflow    | `workflow show`                  | read current aggregate           |
| workflow    | `workflow explore`               | record exploration evidence      |
| workflow    | `workflow spec`                  | record Gherkin (spec)            |
| workflow    | `workflow design`                | record technical design          |
| workflow    | `workflow plan`                  | record plan + work units         |
| workflow    | `workflow approve-plan`          | approve the plan                 |
| workflow    | `workflow begin-implementation` | transition aggregate → implementing |
| workflow    | `workflow complete`              | mark workflow completed          |
| unit        | `workflow claim-unit`            | claim a work unit                |
| unit        | `workflow recover-unit-claim`    | re-issue a handle                |
| unit        | `workflow unit-tdd`              | record TDD evidence              |
| unit        | `workflow unit-review`           | record independent review        |
| unit        | `workflow unit-complete`         | close a work unit                |
| runtime     | `pitcrew principles`             | print the four maxims            |
| runtime     | `pitcrew --version` / `--help`   | identity + epilogue              |

Subcommands are invokable from any caller. The CLI does not distinguish
callers by identity. Role-based authorization is enforced by prompt
fragments, not by the CLI.

### Output envelope

Every subcommand emits exactly one JSON document on stdout:

```json
{
  "ok": true,
  "data": { "...": "..." },
  "warnings": [],
  "next_action": "..."
}
```

Failures emit a single-line error envelope on stderr and a non-zero exit
code; nothing on stdout.

### Exit codes

| Code | Meaning       |
|------|----------------|
| 0    | ok             |
| 1    | internal       |
| 2    | usage          |
| 3    | state          |
| 4    | cas            |
| 5    | handle         |

### Flag conventions

- All flags are long-form (`--flag`). No single-letter aliases.
- `--json` on `pitcrew principles` switches output to a structured array.
- `--handle-dir <dir>` is the only way to direct claim handle output.
- `--print-claim-handle-secret-once` is an operator-only debugging
  escape. It prints the secret to stdout, immediately revokes the
  handle in the same process, and is **not** advertised in the production
  `--help` text. Agent templates SHALL NOT use this flag.
- `--approve-exception <unit-id>` may be passed per unit at plan
  approval to bypass admission for an indivisible unit.

### Help epilogue

Every `--help` output ends with the line:

> Read the four maxims of the harness: `pitcrew principles`.

---

## 2. Claim handles — opaque only, no production escape

pitcrew v2 supports one and only one claim path: opaque handle files at
`0600` written into a caller-supplied `--handle-dir`. There is no
`--emit-plain-token` flag, no `--claim-token` alternative on any per-unit
subcommand, and no agent template that produces a raw bearer secret.

### Handle file format

```json
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

- File mode `0600`. Directory mode `0700`. Owner == caller. No symlinks.
- The plain secret never touches the filesystem. Only its `sha256` is
  persisted in the handle file.

### Lifecycle

- Issued by `workflow claim-unit` with state `intent`.
- Promoted to `active` on the first successful unit subcommand.
- Expiry: **5 minutes** from `issued_at`. Every successful unit
  subcommand refreshes `expires_at`.
- Expired handles are rejected with exit code `5` and the file is
  deleted atomically.

### Recovery

A new handle can be issued via `workflow recover-unit-claim` only if:

- the unit is not in `reviewing`, AND
- there is no TDD evidence for the current revision.

Recovery increments `claim_generation`. The recovered token remains
secret under the same handling rules as the original.

---

## 3. Go package layout

```
pitcrew/
├── cmd/
│   └── pitcrew/
│       └── main.go              # CLI entry; embeds MAXIMS.md
├── internal/
│   ├── cli/                     # flag parsing, dispatch, exit codes
│   ├── envelope/                # JSON output envelope
│   ├── store/                   # SQLite connection, migrations, CAS
│   ├── workflow/                # workflow aggregate, transitions
│   ├── plan/                    # plan validation, work unit readiness
│   ├── evidence/                # TDD evidence shape + storage
│   ├── handles/                 # claim handles + recovery
│   ├── maxims/                  # embed + parse MAXIMS.md
│   └── ids/                     # identity formats (wf-, wu-, claim_id)
├── scripts/
│   └── install-templates.sh     # POSIX installer for prompt fragments
├── MAXIMS.md                    # canonical source of the four maxims
├── openspec/                    # this directory
├── go.mod                       # module github.com/fmazzalomo/pitcrew
└── go.sum
```

There is **no** `cmd/pitcrew-tui/` in this change. The TUI is the next
change. There is **no** `internal/installer` and **no**
`internal/master`. Agents are the Master.

---

## 4. Identity formats

| Identity   | Format            | Source                            |
|------------|-------------------|-----------------------------------|
| workflow   | `wf-<24hex>`      | first 12 bytes of SHA-256         |
| work unit  | `wu-<24hex>`      | first 12 bytes of SHA-256         |
| claim_id   | `<32hex>`         | 16 random bytes, hex-encoded      |
| secret     | `<32hex>`         | 16 random bytes, hex-encoded      |
| timestamps | RFC3339 UTC       | `2006-01-02T15:04:05.000Z`        |

All random bytes are read from `crypto/rand`. No `math/rand` anywhere
in production paths.

---

## 5. SQLite store — schema, migrations, CAS

Per-project store at `<project>/.pitcrew/state.db`. One writer per
process. No daemon, no IPC, no shared cache.

### PRAGMAs

- `journal_mode = WAL`
- `synchronous = NORMAL`
- `foreign_keys = ON`
- `busy_timeout = 5000` (5s; concurrent writers rejected after that)
- `temp_store = MEMORY`

### Schema (high level)

- `workflows` — one row per workflow aggregate.
- `events` — append-only log of state transitions.
- `plans` — one row per approved plan, JSON body.
- `work_units` — one row per unit, status, deps, owner.
- `evidence` — one row per unit TDD/validation event.
- `reviews` — one row per unit review verdict.
- `handles` — one row per issued claim handle, with `secret_hash`.

### Migrations

Schema changes are versioned. The store applies migrations in order at
startup; no destructive migrations are accepted.

### CAS-by-revision

Every state-mutating subcommand takes `--revision <int>`. The mutation
succeeds only if the current revision equals `--revision`. Mismatch
returns exit code `4` (cas). Master SHALL NOT retry blindly on cas;
SHALL re-inspect first.

---

## 6. Agent roles — eight identities, thin Master

See `openspec/AGENTS.md` for the full role contract. Design summary:

- **Master** is the user's front and the only role that talks to the
  user. It holds the long-lived workflow context.
- **Implementer** and **Reviewer** are distinct identities. The
  runtime configures them as separate subagents so the same agent
  does not implement and review its own work.
- **Roles** do not return their output to the Master. They call the
  control plane themselves. The Master only learns that they
  finished.
- The handle path is the only secret-adjacent value that crosses
  role boundaries.

---

## 7. Testing strategy

- Stack: Go 1.26+, `modernc.org/sqlite` (no CGO).
- Test runner: stdlib only. No `testify`, no `gomock`, no `go-cmp`.
- Single command: `go test ./...`.
- Each subcommand has at least one happy-path and one failure-path
  golden test.
- The store layer has property tests on the CAS-by-revision contract.
- The installer script has POSIX shell smoke tests under
  `scripts/tests/`.

---

## 8. Build, install, embed

- `MAXIMS.md` is embedded via `//go:embed` at build time. The maxims
  travel with the executable.
- `pitcrew principles` prints the embedded text verbatim by default.
- The installer script reads `MAXIMS.md` from the filesystem (not
  from the binary) when building prompt fragments.
- A maxim is changed only by an explicit edit to `MAXIMS.md`, and
  that edit becomes its own OpenSpec change.

---

## 9. Workflow aggregate — 9 states

The workflow aggregate transitions through 9 states. Transitions are
defined in `specs/workflow-lifecycle/spec.md`. The full transition
table is normative there. High-level list:

`draft → exploring → specifying → designing → planning →
plan_approved → implementing → reviewing → ready_to_complete →
completed`. (Plus terminal `abandoned`.)

---

## 10. Plan and work units — admission, readiness, exceptions

- A plan is submitted via `workflow plan` as JSON.
- The plan is validated for dependency cycles before acceptance.
- `workflow approve-plan` either approves the whole plan or, with
  `--approve-exception <unit-id>`, approves a plan with one or more
  indivisible-unit exceptions. Each exception is recorded with
  justification.
- `workflow list-ready-units` reports which units are ready
  (deps satisfied, not claimed, not done). The CLI decides
  readiness and parallelism; never the agent.

---

## 11. What is NOT in this design

- No HTTP, no remote API, no RPC of any kind.
- No multi-tenancy, no organizations, no teams, no shared workspaces.
- No cross-project coordination, no global registry.
- No audit-grade logging, no compliance hooks.
- No HA, no scaling, no hardening against external attackers.
- No daemon, no IPC, no shared cache.
- No v1 data migration.
- No embedded TUI in this change.
- No `internal/installer` package.
- No `internal/master` package.
- No `--emit-plain-token` flag in production paths.
- No `--claim-token` alternative on per-unit subcommands.

---

## 12. Informative — Master's final context shape

After several units of execution, the Master's context contains only:

- workflow id,
- current revision,
- the user's original goal,
- a short status log of `agent → subcommand → ok | next_action`.

Role-produced content (Gherkin, design, TDD evidence, review summary)
lives in the control plane and in the role's own context. The Master
never relays it. This is what keeps the Master a thin coordinator.
