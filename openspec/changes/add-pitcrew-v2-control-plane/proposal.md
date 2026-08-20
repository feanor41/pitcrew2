<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/principles (id 4439)
  - pitcrew2/product-shape (id 4437)
  - pitcrew2/scope (id 4445)
  - pitcrew2/agent-roles (id 4442)
  - pitcrew2/orchestration-model (id 4444)
  - pitcrew2/foundation-change (id 4441)
  - session summary (id 4440)
NOT byte-identical to the originals. Section headings, ordering, and
decisions follow the documented layout. Original was ~172 lines.
-->

# Change: add-pitcrew-v2-control-plane

## Principles

The four maxims of the harness, restated here so the foundation change
inherits them by reference. See `MAXIMS.md` at the repo root for the
canonical wording.

1. **Technical English internally; the user's language externally.**
2. **The harness serves the result, never the other way around.**
3. **The harness is the usual path, not the only path.**
4. **Short scope, easy to complete, always.**

A principle is changed only by an explicit edit to this section (and to
`MAXIMS.md`), which becomes its own OpenSpec change.

---

## Why

The current control plane (v1, `agent-controller`) has three structural
pathologies that make the product an obstacle to its own evolution:

1. **Monolith.** CLI + TUI + installer + master + domain live in a single
   Go binary. The v1 repository had to disable its own product
   (`agent-controller`) to develop itself.
2. **Surface inflation.** 33 subcommands with overlapping authority. Each
   new feature adds flags, escape hatches, and reconciliation paths.
3. **Insecure defaults.** The default claim path prints the raw token to
   stdout. The opaque handle exists but the path of least resistance is
   the insecure one — once the secret is in stdout it lives in agent
   context and shell history.

The v1 evolution roadmap (compact, claim handles, contingent scopes,
amendments, reopen, parent/child, TUI tree) was explored and proposed but
never applied. v2 treats it as **context**, not as a contract. We start
clean with a smaller surface and stricter defaults.

v1 (`agent-controller` in `$PATH`) stays usable for those who need it. v2
is a clean start with **no data migration**.

---

## What Changes

Nine items, ordered by dependency.

1. **Two binaries + one external script.** `pitcrew` (CLI, this change),
   `pitcrew-tui` (separate change), `scripts/install-templates.sh` (POSIX
   shell, idempotent). No `internal/installer`, no `internal/master`,
   no embedded TUI.
2. **Opaque claim handles as the only claim path.** No `--emit-plain-token`
   flag, no `--claim-token` alternative on per-unit subcommands. Operator-
   only debugging escape: `--print-claim-handle-secret-once` (revokes the
   handle immediately, not advertised in `--help`, not for agent templates).
3. **Eight roles, thin Master.** Master orchestrates by sending short
   messages; roles call the CLI directly. The control plane is shared
   memory, not a Master proxy.
4. **16 subcommands, closed list.** Tentative list frozen in `design.md`
   § 1. No new subcommand without an OpenSpec change.
5. **SQLite local store.** One writer per process, `<project>/.pitcrew/state.db`,
   CAS-by-revision, PRAGMAs as in `specs/event-store/spec.md`.
6. **POSIX shell installer.** `scripts/install-templates.sh` writes one
   prompt fragment per role + one agent-contract fragment. Refuses to
   overwrite the Master fragment without `--overwrite`.
7. **Four Maxims embedded in the binary.** `MAXIMS.md` is the canonical
   source. The CLI embeds it via `//go:embed` and exposes it via
   `pitcrew principles`.
8. **Scope rules locked.** No HTTP, no remote API, no RPC, no multi-tenancy,
   no cross-project coordination, no audit-grade logging, no HA /
   scaling / hardening. See `## Non-Goals` below.
9. **No implementation yet.** This change produces only OpenSpec artifacts.
   Implementation begins only after the user explicitly chooses to start,
   and is split into stacked changes (`add-pitcrew-store-and-domain`,
   `add-pitcrew-cli-runtime`, `add-pitcrew-subcommands`,
   `add-pitcrew-installer`).

---

## Capabilities

Seven new capabilities. Each is documented by a per-capability spec under
`specs/<cap>/spec.md`.

| # | Capability             | Spec                                          |
|---|------------------------|------------------------------------------------|
| 1 | `cli-surface`          | `specs/cli-surface/spec.md`                   |
| 2 | `workflow-lifecycle`   | `specs/workflow-lifecycle/spec.md`            |
| 3 | `plan-and-work-units`  | `specs/plan-and-work-units/spec.md`           |
| 4 | `tdd-and-review`       | `specs/tdd-and-review/spec.md`                |
| 5 | `claim-handles`        | `specs/claim-handles/spec.md`                 |
| 6 | `event-store`          | `specs/event-store/spec.md`                   |
| 7 | `runtime-install`      | `specs/runtime-install/spec.md`               |

The role contract (`agent-roles`) is documented inside `cli-surface` and
`runtime-install` rather than as its own capability — it is the seam
between the control plane and its consumers.

---

## Impact

- **Affected code:** none (greenfield change).
- **New code:** `cmd/pitcrew/main.go`, `internal/<packages>` as in
  `design.md` § 3, `scripts/install-templates.sh`.
- **Schema migration:** none for v1. v2 starts with an empty
  `.pitcrew/state.db` per project.
- **Breaking changes:** none for end users (v1 binary continues to work).
  Breaking for anyone who depended on v1's 33 subcommands and raw claim
  token; v2 ships only the 16 documented subcommands with opaque handles.

---

## Non-Goals

What this change explicitly does NOT do.

- No production authn / authz. Filesystem permissions are the trust boundary.
- No distributed execution. No daemon, no IPC, no remote procedure.
- No plugin API. The binary's surface is closed.
- No v1 data migration. v1 and v2 coexist.
- No compact → full fallback (deferred to `add-compact-inspection`).
- No self-hosting of design (deferred to a future change).
- No graphical UI. The TUI is its own change (`add-pitcrew-tui`).
- No HTTP, no remote API, no RPC, no multi-tenancy, no cross-project
  coordination, no audit-grade logging, no HA / scaling / hardening.

A future change that introduces any of the above must justify the scope
expansion explicitly in its own OpenSpec proposal.
