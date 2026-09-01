# Spec: runtime-install

## Purpose

Define the contract for `pitcrew install codex|opencode|claude|pi` and its
canonical POSIX installer. Installation configures one supported agent runtime;
it does not install the PitCrew binary or create workflow state.

## Requirements

### Requirement: POSIX standalone transaction

The installer SHALL remain POSIX `/bin/sh`. The binary SHALL embed the canonical
script, extract it privately, preserve cwd and streams, invoke `/bin/sh`, and
always clean up. Rendering, prerequisite checks, native-schema validation, and
graph validation SHALL precede mutation. Failure or handled interruption SHALL
restore replaced or removed files byte-for-byte and remove installer-created
files and directories. An identical reinstall SHALL be a write no-op.

#### Scenario: A standalone binary rolls back partial installation

- GIVEN only a compiled PitCrew binary and an isolated runtime home
- WHEN installation fails after a staged write
- THEN the prior target tree SHALL be restored exactly
- AND no extracted asset or workflow state SHALL remain

### Requirement: Minimal executable role bootstraps

The selected runtime SHALL receive native definitions for `daimon`, `aion`,
`pc2-explorer`, `pc2-specifier`, `pc2-designer`, `pc2-task-planner`,
`pc2-implementer`, `pc2-reviewer`, and `pc2-sdd-initializer`. Each definition
SHALL contain only stable role identity, the exact role-scoped
`pitcrew agent brief --role <hyphenated-role>` bootstrap, an instruction to
retrieve that brief before action, and its stable handoff boundary. Scoped roles
SHALL substitute received workflow and unit IDs in the required canonical
flags. Generated definitions SHALL NOT duplicate maxims, routing, correction,
release, workflow, or command manuals.

The versioned binary response is the dynamic contract source. It SHALL return
one composite brief ordered as shared operating contract, stable role contract,
bounded dynamic context when applicable, and `next_action`. The shared contract
SHALL expose its own `contract_version`, a deterministic `contract_digest`, and
the complete canonical embedded `MAXIMS.md` bytes. The role contract SHALL
retain its independent `contract_version` and deterministic `contract_digest`.
Dynamic context, current `allowed_actions`, and `next_action` SHALL affect
neither stable digest. Brief retrieval SHALL be read-only.

The runtime installer SHALL extract only the embedded POSIX installer needed
for execution. It SHALL NOT extract or deploy a standalone `MAXIMS.md`; the
canonical source travels inside the binary and reaches roles through the
composite brief. `pitcrew principles [--json]` SHALL remain the compatible
human and diagnostic representation.

#### Scenario: Every installed role retrieves current authority

- GIVEN a selected runtime whose prerequisites pass
- WHEN installation succeeds and a role begins work
- THEN all nine native definitions SHALL exist
- AND the role SHALL retrieve its valid scoped brief before action
- AND the brief SHALL contain the complete shared maxims contract before role authority
- AND an invalid or unscoped request SHALL fail without mutation

### Requirement: Native least privilege and bounded graph

Codex SHALL preserve native role and target metadata. OpenCode SHALL preserve
mode and permission metadata. Claude Code coordinators SHALL have shell and
Agent tools, while specialists SHALL have shell but no delegation. Pi
coordinators SHALL have shell and subagent tools, while specialists SHALL have
shell but no subagent tool and SHALL preserve supervisor depth wiring.

Daimon SHALL target exactly one Aion. Aion SHALL target exactly the seven
specialists. Specialists SHALL NOT delegate. The installer SHALL NOT create `pitcrew/agent-contract.md`;
stable mechanics belong to `pitcrew agent brief`,
not a generated support file.

The dynamic Daimon brief SHALL require one retained user-visible turn while
Aion is active, host-native mailbox and user-steering waits, exactly-once relay
of meaningful acknowledged facts, one truthful notice no later than five
minutes into each continuous quiet interval, resumed waiting after steered
input is forwarded as requested state, distinct completion, interruption,
cancellation, timeout, failure, blocker, clarification, user-owned-gate, and
abandonment outcomes, no promise of later unsolicited updates after
finalization without a real host push channel, and truthful disclosure when
the host lacks bounded native liveness. Codex SHALL be documented as supporting
a bounded in-turn mailbox-and-steering wait but no post-final push. Pi SHALL be
documented as supporting `contact_supervisor` progress relay without a stable
native trace contract for steered dual wait. OpenCode and Claude Code SHALL be
documented as having no repository-verified bounded dual wait or unsolicited
push capability. Runtime bootstraps SHALL continue resolving this versioned
contract at activation time rather than duplicating it.

#### Scenario: Native graph validates before mutation

- GIVEN staged definitions for a supported runtime
- WHEN native metadata and declared targets are validated
- THEN the least-privilege tool contract and every bounded edge SHALL match
- AND validation failure SHALL occur before target mutation

### Requirement: Explicit selection and prerequisites

The public command SHALL select exactly one lowercase, alias-free runtime and
ignore homes and overrides for every other runtime. A missing selected root MAY
be created transactionally. Direct script invocation without a selector SHALL
retain backward-compatible autodetection.

| Token | Override / default root | Native registry |
|---|---|---|
| `codex` | `CODEX_HOME` / `~/.codex` | `agents/*.toml` |
| `opencode` | `OPENCODE_CONFIG_DIR` / `~/.config/opencode` | `agents/*.md` |
| `claude` | `CLAUDE_CONFIG_DIR` / `~/.claude` | `agents/*.md` |
| `pi` | `PI_AGENT_HOME` / `~/.pi/agent` | `agents/*.md` |

OpenCode SHALL require OpenCode 1.18.23 or newer, `jq`, `timeout`, and an
unambiguous effective top-level integer `subagent_depth` of at least 2 from
`opencode --pure debug config` in the caller working directory. Pi SHALL require
Node.js, active `pi-subagents` version 0.25.0 or newer, and integer
`maxSubagentDepth` of at least 3. Prerequisite failure SHALL occur before writes
and SHALL NOT modify application configuration or access the network.

#### Scenario: Explicit selection cannot drift

- GIVEN all four runtime homes and overrides exist
- WHEN `pitcrew install pi` runs
- THEN only selected Pi paths MAY be inspected or mutated

### Requirement: Checksum-gated legacy cleanup

The installer SHALL stop generating or referencing
`pitcrew/agent-contract.md`. A missing legacy file SHALL be a no-op. A regular
legacy file whose bytes match a recognized checksum of prior managed content
MAY be transactionally backed up and removed. Rollback SHALL restore that file
byte-for-byte. Modified, non-regular, and unrelated files SHALL be preserved and
reported; cleanup SHALL NOT blind-delete or traverse broadly.

Current role definitions remain installer-managed. The public command SHALL
warn before refreshing differing current managed definitions. Direct script
invocation SHALL refuse such differences unless `--overwrite` is explicit.
Unrelated files and application configuration SHALL remain byte-identical.

#### Scenario: Modified legacy content is preserved

- GIVEN a legacy support file differs from every recognized checksum
- WHEN installation succeeds
- THEN the file SHALL remain byte-identical and be reported
- AND recognized legacy content in the same transaction SHALL be restorable

### Requirement: Runtime output and verification

Success SHALL exit 0 and print exactly `Installed PitCrew agents for <Runtime>
in <registry>` followed by a newline. Failure SHALL preserve actionable plain
stderr and non-zero status without success stdout or a temporary extraction
path.

POSIX tests SHALL exercise all four public selectors, native schemas and tools,
the bounded graph, real brief activation, idempotency, checksum-gated cleanup,
modified and unrelated preservation, rollback, signals, and cleanup. The
opt-in real OpenCode probe SHALL report `SKIP`, never `PASS`, when compatible
runtime prerequisites or credentials are unavailable.

#### Scenario: Complete public contract is exercised

- GIVEN the repository verification suite
- WHEN shell, focused/full Go, build, vet, formatting, diff, and standalone
  probes run
- THEN the four runtime installation contracts SHALL remain proven
