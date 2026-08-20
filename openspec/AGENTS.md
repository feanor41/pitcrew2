<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/principles (id 4439)
  - pitcrew2/scope (id 4445)
  - pitcrew2/product-shape (id 4437)
  - pitcrew2/orchestration-model (id 4444)
  - pitcrew2/agent-roles (id 4442)
NOT byte-identical to the originals. Decisions are verbatim from Engram;
surrounding guidance is reconstructed.
-->

# AGENTS.md — pitcrew v2

This file is the runtime contract for any agent (LLM subagent of the host
runtime) that invokes the `pitcrew` CLI. It restates the durable governance
of the repository. The canonical source of the four maxims is `MAXIMS.md`
at the repo root; every agent is expected to internalize them.

---

## Principles

The four maxims of the harness, restated here for agents that read this file
before they read `MAXIMS.md`. See `MAXIMS.md` for the canonical wording.

1. **Technical English internally; the user's language externally.** The
   control plane emits English; the agent's user-facing reply is in the
   user's language. Do not localize the system surface; localize the
   conversation around it.

2. **The harness serves the result, never the other way around.** Defaults
   are safe; every default has a documented, auditable escape hatch.

3. **The harness is the usual path, not the only path.** For trivial work,
   skip the harness. For everything else, use it.

4. **Short scope, easy to complete, always.** Decompose large ambitions into
   stacked changes. A change that grows past its budget is split, not merged
   by exception.

A principle is changed only by an explicit edit to `## Principles` (here and
in the foundation proposal), which becomes its own OpenSpec change.

---

## Scope

pitcrew v2 is a personal harness with a fixed scope. It coordinates the
development work of **one person on one machine**, possibly across multiple
local projects (one project per CLI invocation). It is not multi-user, not
remote, not distributed, not high-availability, not audit-grade.

The six rules of scope:

- **One user.** Filesystem permissions are the trust boundary. No accounts,
  no authn, no authz.
- **One machine.** Local subprocess. No HTTP, no gRPC, no WebSocket, no RPC,
  no polling, no remote procedure of any kind.
- **One writer per database.** One SQLite connection per process. No daemon,
  no IPC, no shared cache. Busy timeout rejects concurrent writers.
- **One or more projects locally.** Each project has its own
  `<project>/.pitcrew/state.db`. Each CLI invocation operates on exactly one
  project. No global registry, no cross-project index.
- **No multi-tenancy.** No tenants, no organizations, no teams, no shared
  workspaces. A second user installs their own clone.
- **No threat model beyond "the agent is on your machine".** The handle
  claim protects secrets from leaking into agent context / logs / shell
  history — not from external attackers. No encryption at rest beyond the
  filesystem. No audit-grade logging for compliance.

Any future change that introduces a network surface, auth, multi-user state,
daemon mode, or cross-machine sync is a scope violation and must be
justified explicitly in its own OpenSpec proposal.

---

## What is NOT in this repository

- **No embedded TUI.** `pitcrew` is a pure CLI binary. The TUI is a separate
  binary (`pitcrew-tui`) that consumes `pitcrew` as a subprocess and parses
  JSON. The TUI is its own OpenSpec change (`add-pitcrew-tui`).
- **No `internal/installer` package.** The runtime installer is an external
  POSIX shell script (`scripts/install-templates.sh`).
- **No `internal/master` package.** Agents (LLM subagents of the host
  runtime) are the Master. They call `pitcrew` as a subprocess and decide
  the workflow choreography. The control plane does not orchestrate; the
  agents do.
- **No v1 data migration.** v1 (`agent-controller` in `$PATH`) stays usable
  for those who need it. v2 is a clean start.

---

## Orchestration model

The control plane is a **shared memory**, not a Master proxy. Every role
(Master, Explorer, Specifier, Designer, TaskPlanner, Implementer, Reviewer,
Archivist) invokes the `pitcrew` CLI directly. The Master orchestrates by
sending short messages; it does not relay content.

Two channels:

- **Master ↔ role channel.** Workflow id, current revision, one-line
  instruction (Master → role), one-line status (role → Master). No content.
- **Role ↔ control plane channel.** Full content. Role reads prior context
  via `workflow show`, writes artefact via its subcommand.

Consequences:

- The CLI does NOT distinguish callers by identity. Every subcommand is
  available to every caller. Role-based authorization is enforced by prompt
  fragments, not by the CLI.
- The Master is the only role that talks to the user. The Master is NOT
  the only role that talks to the control plane.
- The Implementer returns the handle **path** (not the handle contents) to
  the Master. The Master passes the handle path to the Reviewer. The handle
  contents never leave the Implementer or Reviewer.

---

## Roles and their CLI surface

Eight roles, each with a minimal subcommand surface. The canonical sequence
(one unit at a time, in dependency order):

```
user → Master → Explorer → Specifier → Designer → TaskPlanner
      → Master approves
      → Implementer (claim + unit-tdd)
      → Reviewer (unit-review)
      → Implementer (unit-complete)
      → repeat per unit
      → Archivist (complete)
```

| Role         | Subcommands                          |
|--------------|---------------------------------------|
| Master       | `new`, `show`, `approve-plan`, `complete` (optional) |
| Explorer     | `explore`                             |
| Specifier    | `spec`                                |
| Designer     | `design`                              |
| TaskPlanner  | `plan`                                |
| Implementer  | `claim`, `unit-tdd`, `unit-complete`  |
| Reviewer     | `unit-review`                         |
| Archivist    | `complete`                            |

Hand-off contract:

- Every role returns when it has called its assigned subcommand and received
  success.
- Every role returns its output (the subcommand's JSON payload, including
  the new revision) to the Master.
- The Master is the only role that holds the long-lived workflow context.
- The Implementer returns the handle path (not the handle contents) to the
  Master.

Failure handling:

- Exit code `3` or `4` → Master decides whether to retry after re-inspecting
  (`workflow show`) or surface to the user.
- Exit code `5` (handle error) → Master waits 5 minutes for expiry, then
  re-claims.
- Master SHALL NOT retry blindly on CAS error; SHALL re-inspect first.
