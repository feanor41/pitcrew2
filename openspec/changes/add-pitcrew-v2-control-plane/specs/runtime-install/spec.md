<!--
Reconstructed from Engram observations on 2026-08-20.
Source observations:
  - pitcrew2/product-shape (id 4437)
  - pitcrew2/maxims (id 4443)
  - pitcrew2/agent-roles (id 4442)
  - pitcrew2/orchestration-model (id 4444)
NOT byte-identical to the originals. Original was ~95-110 lines. The
POSIX shell contract, the prompt fragments, the prohibitions, and the
master-fragment overwrite protection are reconstructed from the
documented narrative.
-->

# Spec: runtime-install

## Purpose

Define the contract for `scripts/install-templates.sh`, the external
POSIX shell installer that wires `pitcrew` into supported LLM
runtimes (Codex, OpenCode, Claude Code, Pi). The installer is the
seam between the binary and the agents that consume it.

## Requirements

### R1 — POSIX shell

The installer SHALL be written in POSIX shell (`/bin/sh`). It SHALL
NOT use bashisms. It SHALL be idempotent: running it twice SHALL be
equivalent to running it once.

### R2 — Roll back partial writes

The installer SHALL roll back partial writes on failure. If a write
fails mid-way, the installer SHALL restore the previous state of any
file it modified and SHALL exit non-zero.

### R3 — One prompt fragment per role

The installer SHALL write one prompt fragment per role:

- `master.md`
- `explorer.md`
- `specifier.md`
- `designer.md`
- `task-planner.md`
- `implementer.md`
- `reviewer.md`
- `archivist.md`

Each fragment SHALL begin with the verbatim text of `MAXIMS.md`,
prefixed with:

> Internalize the four maxims below. They are your operating system.
> Every decision you make is subordinate to them.

### R4 — Agent-contract fragment

The installer SHALL write one agent-contract fragment that records
the prohibitions common to every role. The prohibitions SHALL include:

- no `--claim-token` flag,
- no `--emit-plain-token` flag,
- no `--print-claim-handle-secret-once` flag,
- no same-identity collisions between Implementer and Reviewer,
- no blind retries on CAS error.

### R5 — Master fragment overwrite protection

The installer SHALL refuse to overwrite `master.md` without an
explicit `--overwrite` flag. All other fragments SHALL be overwritten
without ceremony. The Master fragment is the default user-facing
agent prompt; overwriting it silently is a footgun.

### R6 — Reading MAXIMS.md

The installer SHALL read `MAXIMS.md` from the filesystem (not from
the embedded binary) when building prompt fragments. The maxims in
the fragment SHALL match `MAXIMS.md` byte-for-byte at install time.

### R7 — Hand-off reminder

Every role fragment SHALL include the reminder:

> You do not return your output to the Master. You call the control
> plane yourself. The Master only learns that you finished.

### R8 — Supported runtimes

The installer SHALL detect the host runtime by environment variables
and well-known config paths:

- Codex: `~/.codex/prompts/`
- OpenCode: `~/.config/opencode/agents/`
- Claude Code: `~/.claude/prompts/`
- Pi: `~/.pi/agent/agents/`

Unsupported runtimes SHALL cause a clear error and a non-zero exit.

### R9 — Smoke tests

The installer SHALL be exercised by POSIX shell smoke tests under
`scripts/tests/`. The smoke tests SHALL cover:

- idempotency (running twice),
- partial-write rollback (simulated mid-write failure),
- Master fragment overwrite refusal without `--overwrite`,
- unsupported runtime detection.

## Scenarios

### S1 — Idempotent install

> WHEN the installer runs twice in succession,
> THEN the second run SHALL be a no-op and SHALL exit `0`.

### S2 — Rollback on partial failure

> WHEN the installer fails mid-write (for example, a permission
> error on a later fragment),
> THEN the installer SHALL restore every previously modified file
> to its original content and SHALL exit non-zero.

### S3 — Master fragment overwrite refused

> WHEN the installer runs and `master.md` already exists,
> THEN the installer SHALL refuse to overwrite it and SHALL exit
> non-zero with a message naming `--overwrite`.

### S4 — Unsupported runtime

> WHEN no supported runtime is detected,
> THEN the installer SHALL print a clear error listing the
> supported runtimes and SHALL exit non-zero.

### S5 — Hand-off reminder present

> WHEN the installer writes any role fragment,
> THEN the fragment SHALL contain the hand-off reminder from R7
> and SHALL begin with the verbatim MAXIMS.md content.
