# Spec: runtime-install

## Purpose

Define the contract for `scripts/install-templates.sh`, the
POSIX installer that wires `pitcrew` into supported LLM
runtimes (Codex, OpenCode, Claude Code, Pi). The installer is the
seam between the binary and the agents that consume it.

## ADDED Requirements


### Requirement: POSIX shell

The installer SHALL be written in POSIX shell (`/bin/sh`). It SHALL
NOT use bashisms. It SHALL be idempotent: running it twice SHALL be
equivalent to running it once.

#### Scenario: Repeated installation is idempotent

- GIVEN a completed installation
- WHEN the POSIX installer runs again
- THEN it SHALL exit 0 without changing installed bytes

### Requirement: Roll back partial writes

The installer SHALL roll back partial writes on failure. If a write
fails mid-way, the installer SHALL restore the previous state of any
file it modified and SHALL exit non-zero.

#### Scenario: Partial failure rolls back

- GIVEN a simulated failure after one write
- WHEN installation aborts
- THEN every touched file SHALL be restored

### Requirement: One prompt fragment per role

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

#### Scenario: Every role fragment is written

- GIVEN a supported runtime
- WHEN installation succeeds
- THEN all eight named role fragments SHALL exist

### Requirement: Agent-contract fragment

The installer SHALL write one agent-contract fragment that records
the prohibitions common to every role. The prohibitions SHALL include:

- no `--claim-token` flag,
- no `--emit-plain-token` flag,
- no `--print-claim-handle-secret-once` flag,
- no same-identity collisions between Implementer and Reviewer,
- no blind retries on CAS error.

#### Scenario: Prohibitions are installed

- GIVEN a successful installation
- WHEN the contract fragment is read
- THEN it SHALL contain every listed prohibition

### Requirement: Master fragment overwrite protection

The installer SHALL refuse to overwrite `master.md` without an
explicit `--overwrite` flag. All other fragments SHALL be overwritten
without ceremony. The Master fragment is the default user-facing
agent prompt; overwriting it silently is a footgun.

#### Scenario: Master overwrite is refused

- GIVEN master.md already exists
- WHEN installation lacks --overwrite
- THEN it SHALL fail without replacing master.md

### Requirement: Reading MAXIMS.md

The installer SHALL read `MAXIMS.md` from the filesystem (not from
the embedded binary) when building prompt fragments. The maxims in
the fragment SHALL match `MAXIMS.md` byte-for-byte at install time.

#### Scenario: Installed maxims are exact

- GIVEN filesystem MAXIMS.md bytes
- WHEN fragments are generated
- THEN each maxim prefix SHALL match those bytes

### Requirement: Hand-off reminder

Every role fragment SHALL include the reminder:

> You do not return your output to the Master. You call the control
> plane yourself. The Master only learns that you finished.

#### Scenario: Reminder is present

- GIVEN any generated role fragment
- WHEN its content is read
- THEN it SHALL contain the required reminder

### Requirement: Supported runtimes

The installer SHALL detect the host runtime by environment variables
and well-known config paths:

- Codex: `~/.codex/prompts/`
- OpenCode: `~/.config/opencode/agents/`
- Claude Code: `~/.claude/prompts/`
- Pi: `~/.pi/agent/agents/`

Unsupported runtimes SHALL cause a clear error and a non-zero exit.

#### Scenario: Unsupported runtime fails

- GIVEN no supported runtime is detected
- WHEN installation starts
- THEN it SHALL fail while listing supported runtimes

### Requirement: Smoke tests

The installer SHALL be exercised by POSIX shell smoke tests under
`scripts/tests/`. The smoke tests SHALL cover:

- idempotency (running twice),
- partial-write rollback (simulated mid-write failure),
- Master fragment overwrite refusal without `--overwrite`,
- unsupported runtime detection.

#### Scenario: Installer contracts are exercised

- GIVEN the shell smoke-test suite
- WHEN it runs
- THEN it SHALL cover all four listed behaviors
