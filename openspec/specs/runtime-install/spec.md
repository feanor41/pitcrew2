# Spec: runtime-install

## Purpose

Define the contract for `scripts/install-templates.sh`, the
POSIX installer that wires `pitcrew` into supported LLM
runtimes (Codex, OpenCode, Claude Code, Pi). The installer is the
seam between the binary and the agents that consume it.

## Requirements


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

- `daimon.md`
- `aion.md`
- `pc2-explorer.md`
- `pc2-specifier.md`
- `pc2-designer.md`
- `pc2-task-planner.md`
- `pc2-implementer.md`
- `pc2-reviewer.md`

Each fragment SHALL begin with the verbatim text of `MAXIMS.md`,
prefixed with:

> Internalize the four maxims below. They are your operating system.
> Every decision you make is subordinate to them.

#### Scenario: Every role fragment is written

- GIVEN a supported runtime
- WHEN installation succeeds
- THEN all eight named role fragments SHALL exist
- AND obsolete unprefixed role fragments and `pc2-archivist.md` SHALL be absent

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

### Requirement: Prompt overwrite protection

The installer SHALL refuse to overwrite any differing current or obsolete prompt, including customized `daimon.md` and pre-existing `aion.md`,
without an explicit `--overwrite` flag. Overwrite installation SHALL transactionally
refresh current prompts and remove obsolete prompts, including `pc2-archivist.md`;
failure or interruption SHALL restore the exact prior prompt set.

#### Scenario: Prompt overwrite is refused

- GIVEN any managed prompt already differs
- WHEN installation lacks --overwrite
- THEN it SHALL fail without changing installed bytes

### Requirement: Reading MAXIMS.md

The installer SHALL read `MAXIMS.md` from the filesystem (not from
the embedded binary) when building prompt fragments. The maxims in
the fragment SHALL match `MAXIMS.md` byte-for-byte at install time.

#### Scenario: Installed maxims are exact

- GIVEN filesystem MAXIMS.md bytes
- WHEN fragments are generated
- THEN each maxim prefix SHALL match those bytes

### Requirement: Role-specific hand-off reminders

Specialist fragments SHALL return one-line revision-bearing status or permitted opaque paths only to Aion. Aion SHALL return factual revision-bearing status or clarification requests to Daimon. Daimon SHALL forward accepted intent to Aion and communicate only Aion-acknowledged facts or questions to the user.

#### Scenario: Reminder is present

- GIVEN any generated role fragment
- WHEN its content is read
- THEN it SHALL contain the reminder for its exact channel in `user ↔ Daimon ↔ Aion ↔ specialists`

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
- differing prompt refusal without `--overwrite`,
- unsupported runtime detection.

#### Scenario: Installer contracts are exercised

- GIVEN the shell smoke-test suite
- WHEN it runs
- THEN it SHALL cover all four listed behaviors
