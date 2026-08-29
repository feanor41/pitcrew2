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
   are safe; every default has a documented, auditable escape hatch. Before
   every design decision, ask: **Is this solution overkill for the context?**
   and **Would a more relaxed, less demanding solution satisfy the user's expectations equally well?** Choose the least demanding sufficient solution.
   When stronger rigor is necessary, the design-bearing output must briefly name the protected constraint and explain why the simpler option is insufficient.
   Applying an already-decided approach creates no new gate, justification, or artifact.

3. **The harness is the usual path, not the only path.** Use proportional
   direct, delegated-direct, or full-workflow routing according to risk and uncertainty.

4. **Short scope, easy to complete, always.** Decompose large ambitions into
   stacked changes. A change that grows past its budget is split, not merged
   by exception.

A principle is changed only by an explicit OpenSpec change that updates active
guidance and the canonical `MAXIMS.md`. Archived changes remain immutable.

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
- **One or more projects locally.** Each canonical Git common directory maps
  to one `<data-home>/pitcrew/projects/<project-id>/state.db`; linked worktrees
  share it. Each invocation operates on exactly one project. No registry or
  cross-project index exists.
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

- **No separate TUI binary.** The read-only TUI is embedded in `pitcrew` and
  invoked only as `pitcrew tui`. It opens the resolved central store in the same
  process, never initializes state, and never invokes `pitcrew` as a subprocess.
- **No `internal/installer` package.** The runtime installer is an external
  POSIX shell script (`scripts/install-templates.sh`).
- **No `internal/aion` or `internal/daimon` package.** Aion and Daimon are external LLM agent roles, not daemons or control-plane components. They call `pitcrew` as a subprocess; Aion decides workflow choreography while the control plane only enforces domain rules.
- **No v1 data migration.** v1 (`agent-controller` in `$PATH`) stays usable
  for those who need it. v2 is a clean start.

Checkout-local PitCrew v2 databases are legacy inputs, not active workflow
stores. `project inspect` discovers only bounded main/linked candidates without
initializing state. `project consolidate` imports an exact inspected set in one
transaction, acknowledges it only after success, and preserves every source.
Central `worktrees/` and `handles/` are private; cleanup requires a committed
checkpoint so the removed worktree is never the only copy of unfinished work.

---

## Orchestration model

The control plane is shared memory, not an agent proxy. The host channel carries `user ↔ Daimon ↔ Aion ↔ specialists`; every specialist and Aion invoke `pitcrew` directly. Daimon interviews the user, clarifies intent and constraints, preserves conversational continuity, forwards accepted requests, and communicates only Aion-acknowledged facts or clarification requests. A mid-flight request is requested, not applied, until Aion admits it against current workflow and repository state.

Aion is the sole external orchestration authority. It owns the workflow id, revision, goal, short status, route selection, mutation sequencing, specialist dispatch, approvals, handles, corrections, recovery, abandonment, continuation, capability requests, and aggregate completion. The role map remains a prompt contract: `--actor` is declarative collision metadata, not CLI authorization.

Concurrent Daimon availability depends on host support for addressable agents. PitCrew adds no daemon, service, IPC, polling, durable inbox, database state, or lifecycle to guarantee it. A replacement Aion reconstructs context with `workflow show`.

## Roles and their CLI surface

| Role | Subcommands |
|---|---|
| Daimon | none; communicates with the user and Aion |
| Aion | all workflow commands as advisory coordination surfaces |
| Explorer | `explore` |
| Specifier | `spec` |
| Designer | `design` |
| TaskPlanner | `plan` |
| Implementer | `list-ready-units`, `claim-unit`, `unit-tdd`, `unit-complete` |
| Reviewer | `unit-review`, `complete` |

Specialists persist full content through the control plane and return only a one-line revision-bearing status or permitted opaque handle path to Aion. The Implementer returns only the implementation handle path. When unit review is selected, Aion creates independent authority with `handoff-review` and passes only the review handle path to the Reviewer; `recover-review` preserves the original reviewer identity. Handle contents never cross role boundaries.

Routing is proportional. Aion directly implements and verifies well-understood low-risk work affecting at most three files without claiming independent approval. Simple work affecting four or more files uses pc2-implementer followed by one independent complete-change review. Complexity, impact, requirements, architecture, security, migrations, persistence, irreversibility, or uncertainty require the full workflow regardless of file count.

Unit review is selective. Every full workflow ends with an independent aggregate review against requirements, specifications, design, tasks, implementation evidence, and tests. On exit 3 or 4, Aion runs `workflow show` once and never repeats an identical command against unchanged state. It may abandon an obstructive non-terminal workflow with a recorded reason, but may not forge review, bypass aggregate review, disclose handle contents or secrets, discard evidence, or mutate terminal workflows. Terminal work continues only through `workflow continue --from`.
