# Spec: runtime-install

## Purpose

Define the contract for `pitcrew install codex|opencode|claude|pi` and its canonical
POSIX installer. It configures one supported runtime, not the PitCrew binary.

## Requirements

### Requirement: POSIX and standalone execution

The installer SHALL remain POSIX `/bin/sh`. The binary SHALL embed the canonical
script and maxims, extract both privately, preserve cwd and streams, invoke
`/bin/sh`, and always clean up. It SHALL work outside the checkout without
assumptions and SHALL NOT open `.pitcrew/state.db`. Setup or cleanup failures
SHALL exit 1 with an actionable `pitcrew install:` diagnostic; child exit status
SHALL otherwise be preserved.

#### Scenario: Standalone binary installs from embedded assets

- GIVEN only a compiled PitCrew binary and an isolated runtime home
- WHEN an exact public install command runs outside the checkout
- THEN installation SHALL use canonical embedded bytes
- AND no extracted asset SHALL remain

### Requirement: Roll back partial writes and remain idempotent

Rendering, prerequisite checks, native-schema validation, and graph validation
SHALL precede mutation. Failure or handled interruption SHALL restore every
replaced or removed file byte-for-byte and remove installer-created files and
directories in reverse order. Repeating a successful installation with identical
assets SHALL not rewrite installed bytes.

#### Scenario: Partial failure rolls back

- GIVEN a simulated failure after one write
- WHEN installation aborts
- THEN the exact prior target tree SHALL be restored

### Requirement: One native definition per role

The selected runtime SHALL receive native definitions for:

- `daimon`
- `aion`
- `pc2-explorer`
- `pc2-specifier`
- `pc2-designer`
- `pc2-task-planner`
- `pc2-implementer`
- `pc2-reviewer`

Codex SHALL use `agents/*.toml` with underscore native identities. OpenCode,
Claude Code, and Pi SHALL use `agents/*.md` with hyphenated identities. Each
definition SHALL contain canonical `MAXIMS.md` bytes, native schema, tools,
permissions, role responsibility, and its exact hand-off reminder. Obsolete
unprefixed roles and `pc2-archivist` SHALL be absent.

#### Scenario: Every native role is installed

- GIVEN a selected runtime whose prerequisites pass
- WHEN installation succeeds
- THEN exactly all eight native definitions SHALL exist and validate
- AND obsolete PitCrew definitions SHALL be absent

### Requirement: Separated agent contract and bounded graph

The installer SHALL write `<runtime-root>/pitcrew/agent-contract.md` outside
agent discovery. It SHALL record opaque-handle boundaries, distinct
Implementer/Reviewer actors, CAS inspection, and the prohibitions on
`--claim-token`, `--emit-plain-token`, and
`--print-claim-handle-secret-once`. Daimon SHALL target only Aion, Aion SHALL
target exactly the six specialists, and specialists SHALL NOT delegate.

#### Scenario: Contract and graph validate before mutation

- GIVEN staged runtime definitions
- WHEN the installer validates their declared targets
- THEN every bounded edge and common prohibition SHALL match the role contract

### Requirement: Live user turn and addressable Aion

For each accepted non-terminal delivery, generated Daimon instructions SHALL
retain the active user-visible turn and the same addressable Aion. Daimon SHALL
use the host-native dual wait/select for either an event from that Aion or
steered user input. User input SHALL be forwarded to that Aion as requested state,
not applied state, before Daimon resumes the same wait/select. Only Aion-acknowledged
changed meaningful facts MAY reach the user: accepted transitions, completed
units, resolved corrections, achieved objectives, actual blockers, or
clarification requests. Daimon SHALL remain silent otherwise and exit the live
turn only for terminal completion, a genuine blocker, or user cancellation.

If the host cannot keep the turn and Aion concurrently addressable, Daimon SHALL
surface that missing capability once. Aion SHALL record exactly one request-capability
and SHALL NOT imply fulfillment or fabricate live progress. No runtime SHALL
compensate with polling, daemon, IPC, or durable inbox behavior.

#### Scenario: User steering returns to the same wait

- GIVEN Daimon is waiting on the retained Aion and the user supplies new input
- WHEN the host-native selection yields that input
- THEN Daimon SHALL forward it to the same Aion as requested state
- AND resume waiting after Aion admits, rejects, or requests clarification

#### Scenario: Missing concurrency is durable once

- GIVEN a host lacks native concurrent user/Aion selection
- WHEN Daimon detects the limitation
- THEN it SHALL notify Aion once and Aion SHALL record one capability request
- AND no agent SHALL poll or create another transport

#### Scenario: Pi runtime evidence remains honest

- GIVEN the opt-in official Pi supervisor smoke is disabled or its stable native prerequisites are unavailable
- WHEN runtime verification runs
- THEN it SHALL report `SKIP` rather than claim live-turn proof
- AND static Pi instructions SHALL retain the same acknowledgement and transport prohibitions

### Requirement: Aion supplies economical delivery facts

After selecting `direct_inline` or `delegated_direct`, generated Aion
instructions SHALL establish one trace before repository mutation with `delivery start`,
using the accepted goal, selected route, bounded route rationale, and
a stable operation key. Aion SHALL retain the stable operation key until start acknowledgement
and replay the identical start after a lost response. Idempotency SHALL guarantee one delivery identity, not one fallible invocation.
Once acknowledged, Aion
SHALL retain the delivery ID and current revision. On interrupted or CAS re-entry,
Aion SHALL inspect and resume the same delivery identity and SHALL NOT mint a
replacement key or trace. It SHALL update only after a meaningful observed fact
or truthful terminal outcome. If the provider disappears, the trace SHALL retain
its last observed status; agents SHALL NOT infer a terminal outcome. A full
workflow SHALL use `workflow new` as its one durable trace and Aion MUST NOT create a direct delivery trace
in addition. Specialists SHALL NOT create or
update a parallel trace.

#### Scenario: Every route has one provider-owned trace

- GIVEN Aion accepts work and selects one proportional route
- WHEN work starts and later changes meaningfully
- THEN a direct route SHALL have one retained `dl-*` identity before mutation
- AND a lost start response SHALL be replayed with the retained operation key
- AND a full workflow SHALL have one `wf-*` identity and zero direct traces
- AND updates SHALL describe only facts Aion actually observed

### Requirement: Explicit selection and current registry paths

The public command SHALL select exactly one lowercase, alias-free runtime and
ignore homes and overrides for every other runtime. A missing selected root MAY
be created transactionally. Direct script invocation without a selector SHALL
retain backward-compatible autodetection.

| Token | Override / default root | Native registry | Legacy registry |
|---|---|---|---|
| `codex` | `CODEX_HOME` / `~/.codex` | `agents/*.toml` | `prompts/*.md` |
| `opencode` | `OPENCODE_CONFIG_DIR` / `~/.config/opencode` | `agents/*.md` | prior PitCrew entries in `agents/` |
| `claude` | `CLAUDE_CONFIG_DIR` / `~/.claude` | `agents/*.md` | `prompts/*.md` |
| `pi` | `PI_AGENT_HOME` / `~/.pi/agent` | `agents/*.md` | prior PitCrew entries in `agents/` |

#### Scenario: Explicit selection cannot drift

- GIVEN all four runtime homes and overrides exist
- WHEN `pitcrew install pi` runs
- THEN only the selected Pi paths MAY be inspected or mutated

### Requirement: Managed update boundary

The public command SHALL authorize refresh only of current and legacy
PitCrew-managed filenames. Before replacing differing bytes, stderr SHALL emit
exactly `pitcrew installer: WARNING: PitCrew-managed definitions are being refreshed; custom content must live outside managed role files.`
Unrelated files and application configuration SHALL remain byte-identical.
Direct script invocation SHALL continue to refuse differing managed files unless
`--overwrite` is explicit. Direct `--overwrite` SHALL retain its distinct legacy
warning: `pitcrew installer: WARNING: replacing prompts or legacy names; preserve desired custom text before continuing.`
Unsafe, non-regular, symlink, or unwritable targets SHALL fail before commit.

#### Scenario: Public managed refresh is bounded

- GIVEN old managed definitions, legacy names, and an unrelated custom file
- WHEN the public command succeeds
- THEN managed bytes SHALL refresh and legacy names SHALL be removed
- AND the warning SHALL precede mutation while the unrelated file remains exact

### Requirement: OpenCode nested orchestration prerequisite

Before writing any target file for OpenCode, the installer SHALL require
OpenCode 1.18.23 or newer, `jq`, `timeout`, and an unambiguous effective
top-level integer `subagent_depth` of at least 2 from
`opencode --pure debug config` in the caller working directory.

Failure SHALL report the directory, exact verification command, configuration
precedence, remediation (`"subagent_depth": 2`), and the durable rerun command
`pitcrew install opencode`. PitCrew SHALL NOT create, parse, or rewrite user
OpenCode JSON or JSONC. A greater global depth SHALL NOT broaden the installed
`Daimon -> Aion -> specialist` permission edges.

#### Scenario: Insufficient effective depth fails without writes

- GIVEN unsupported tooling or missing, malformed, ambiguous, or insufficient
  effective depth
- WHEN `pitcrew install opencode` runs
- THEN it SHALL fail before target mutation with actionable public guidance

### Requirement: Pi extension prerequisite

Before target writes, Pi SHALL require Node.js, an installed and active
`pi-subagents` version 0.25.0 or newer, and a valid object at
`<pi-agent-home>/extensions/subagent/config.json` whose integer
`maxSubagentDepth` is at least 3. Exact `npm:pi-subagents` identity MAY include
a non-empty version/range suffix; near-name packages SHALL fail. Installed
Daimon and Aion definitions SHALL declare depth 3 so the bounded
`Daimon -> Aion -> specialist` chain is executable while specialists remain
unable to delegate. PitCrew SHALL NOT install packages, access the network, or
modify Pi configuration.

#### Scenario: Invalid Pi extension fails without writes

- GIVEN a missing, inactive, too-old, malformed, or near-name extension, or
  missing, malformed, or insufficient nested-depth configuration
- WHEN `pitcrew install pi` runs
- THEN it SHALL fail before mutation with the durable public rerun command

### Requirement: Runtime output

Success SHALL exit 0 and write exactly `Installed PitCrew agents for <Runtime>
in <registry>` followed by a newline, using `Codex`, `OpenCode`, `Claude Code`,
or `Pi`. Prerequisite, validation, write, rollback, or wrapper failure SHALL
preserve actionable plain stderr and non-zero status without success stdout or
a temporary extraction path.

### Requirement: Opt-in OpenCode runtime depth probe

The repository SHALL provide an isolated, opt-in real OpenCode probe. It MAY
copy credentials only into its temporary data home and SHALL NOT read or mutate
real effective configuration. Without explicit enablement, compatible CLI, and
credentials it SHALL report `SKIP`, never `PASS`. When enabled it SHALL prove
default depth-one rejection and global depth-two specialist success.

#### Scenario: Unavailable real-runtime prerequisites skip

- GIVEN the probe is not enabled or lacks the CLI or credentials
- WHEN it runs
- THEN it SHALL exit successfully with `SKIP` and SHALL NOT claim `PASS`

### Requirement: Four-runtime public verification

POSIX shell tests SHALL exercise all four public selectors, missing roots,
selection isolation, native schemas, identity sets, maxims, bounded dispatch,
managed refresh, unrelated preservation, byte-idempotency, legacy cleanup,
prerequisite failures, rollback, signals, and cleanup. Focused and full Go tests
SHALL prove CLI dispatch, wrapper behavior, and standalone embedded execution.

#### Scenario: Complete public contract is exercised

- GIVEN the repository verification suite
- WHEN shell syntax/suite, focused/full Go tests, build, vet, formatting, diff,
  and standalone probes run
- THEN all four public installation contracts SHALL remain proven
