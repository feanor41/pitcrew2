# Spec: cli-surface

## Purpose

Define the closed CLI, command inputs, envelopes, errors, and caller identity semantics.

## Requirements


### Requirement: Closed command and input contract

The CLI SHALL expose only `install`, `principles`, `tui`, global `--help`/`--version`, and the 23 `workflow` commands below. Flags SHALL be long-form. Each listed flag is required unless bracketed; `--input-file` SHALL name a readable regular file containing one JSON document and SHALL be the only transport for artifact, operational report, plan, evidence, and review bodies.

| Command | Required inputs |
|---|---|
| `install` | exactly one of `codex`, `opencode`, `claude`, or `pi` |
| `new` | `--name <text> --goal <text> --actor <label>` |
| `continue` | `--from <terminal-wf-id> --actor <label>` |
| `show` | `--workflow-id <wf-id>` |
| `progress` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `request-capability` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `explore`, `spec`, `design` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `plan`, `amend-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `approve-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> [--approve-exception <wu-id> ...]` |
| `list-ready-units` | `--workflow-id <wf-id>` |
| `begin-implementation` | `--workflow-id <wf-id> --revision <n> --actor <label>` |
| `complete` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `abandon` | `--workflow-id <wf-id> --revision <n> --actor <label> --reason <text>` |
| `claim-unit`, `recover-unit-claim`, `recover-aggregate`, `handoff-review`, `recover-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `unit-tdd`, `unit-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path> --input-file <path>` |
| `unit-complete` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path>` |

`pitcrew install` SHALL accept no aliases, mixed-case runtime names, flags, or extra arguments. `pitcrew install --help` SHALL perform no installation and SHALL print exactly:

```text
Usage: pitcrew install <codex|opencode|claude|pi>

Installs or updates PitCrew agents for one runtime.

Runtimes: codex, opencode, claude, pi
Read the four maxims of the harness: pitcrew principles.
```

Root help SHALL list `install codex|opencode|claude|pi`. Unknown flags, missing flags, unreadable/non-regular input files, malformed JSON, or an invalid install runtime SHALL fail with exit code `2` before mutation. `--name` SHALL be explicit, non-empty after trimming, and bounded by the workflow name limit; it SHALL NOT be derived for new workflows. A well-formed payload that violates its domain contract SHALL fail with exit code `3` without mutation. `--handle-dir` is the only production handle-output selector. The hidden claim debug flag is defined by `claim-handles`. No install, install-help, or install-usage path SHALL open or create `.pitcrew/state.db`.

#### Scenario: Every command enforces its row

- GIVEN each closed command
- WHEN it is invoked with and without every input in its row
- THEN only the complete valid invocation SHALL pass argument validation

#### Scenario: Operational reports require strict file transport

- GIVEN `workflow progress` or `workflow request-capability` without its input file or with an unknown payload field
- WHEN argument or JSON validation runs
- THEN exit code `2` SHALL result before mutation

#### Scenario: New workflow requires an explicit name

- GIVEN `workflow new` without `--name` or with a blank or over-limit value
- WHEN argument and domain validation run
- THEN creation SHALL fail without persisted mutation

#### Scenario: Installation dispatch is closed and project-inert

- GIVEN each exact supported runtime, or a missing, unknown, mixed-case, or extra argument
- WHEN `pitcrew install` validates its arguments
- THEN only an exact supported runtime SHALL invoke installation once with the original caller working directory
- AND every rejected invocation SHALL leave runtime targets and `.pitcrew` unchanged

### Requirement: Representations and exits

Each successful workflow command SHALL emit one JSON document:

```json
{"ok":true,"data":{},"warnings":[],"next_action":"..."}
```

Workflow and install-argument failures SHALL write one single-line error envelope to stderr, nothing to stdout, and use exactly: `1` internal, `2` usage, `3` state, `4` CAS, `5` handle. State errors SHALL name current and expected state. After valid install dispatch, the embedded POSIX installer SHALL stream actionable plain diagnostics directly to stderr, preserve its non-zero process status, and emit no success stdout on failure. Successful installation SHALL emit exactly `Installed PitCrew agents for <Runtime> in <registry>` followed by a newline; managed-update warnings MAY precede it on stderr. `principles` SHALL emit embedded `MAXIMS.md` bytes, or a raw array with `--json`; help/version are plain text. Every help output SHALL end with `Read the four maxims of the harness: pitcrew principles.` PitCrew's current canonical version SHALL be `0.14.0` and MUST conform to Semantic Versioning 2.0.0. Global `--version` and the TUI header MUST resolve the identical current version from one canonical version source.

(Previously: Version output was plain text without a canonical baseline, SemVer policy, or shared CLI/TUI source.)

#### Scenario: Success and failure representations

- GIVEN one successful and one failing workflow invocation
- WHEN their outputs are captured
- THEN each SHALL use the specified stream, envelope, and exit code

#### Scenario: Version identity is canonical

- GIVEN a build with a configured current version
- WHEN global `--version` and the TUI header are rendered
- THEN both SHALL expose the identical value from the canonical source
- AND the baseline or any later release value SHALL conform to SemVer 2.0.0

#### Scenario: Installation preserves the runtime diagnostic contract

- GIVEN installation succeeds or fails after valid dispatch
- WHEN stdout, stderr, and exit status are captured
- THEN success SHALL use the one canonical success line
- AND failure SHALL preserve actionable installer stderr and status without JSON wrapping or a success line
- AND neither stream SHALL expose the temporary embedded-asset path

### Requirement: Declarative actor metadata

`--actor` SHALL be a non-empty caller-declared label recorded with mutations and used only to detect Implementer/Reviewer collisions. It SHALL NOT authenticate, authorize, select commands, or establish trust. Every command remains callable by every local caller; role restrictions remain prompt rules.

#### Scenario: Actor does not authorize

- GIVEN any non-empty actor label
- WHEN a syntactically valid command is invoked
- THEN access SHALL NOT be granted or denied because of that label

### Requirement: Explicit amendment-authority boundary

`amend-plan` SHALL validate its closed input matrix and plan payload, then exit `3` with an explicit structural-authority error in this revision. No opaque plan-amendment authority exists. The command SHALL NOT inspect or mutate workflow state, plans, units, artifacts, events, or CAS, and `--actor` including `aion` SHALL NOT change that result. A later implementation SHALL add non-forgeable structural authority before permitting amendment.

#### Scenario: Declarative Aion cannot amend a plan

- GIVEN a valid unapproved planning record and either `--actor aion` or another non-empty actor label
- WHEN `amend-plan` is invoked with a valid payload
- THEN each invocation SHALL exit `3` without changing the plan or workflow revision

### Requirement: Aggregate recovery command

`recover-aggregate` SHALL require exactly one unit selection and the matrix inputs above. It SHALL reject its secret-print flag. It SHALL return only an opaque handle path, selected unit id, and next unit revision when the current aggregate CAS revision is supplied, workflow state is `ready_to_complete`, the newest aggregate verdict is `corrections`, and the selected unit is done. State/selection/verdict failures SHALL exit `3`, stale aggregate CAS SHALL exit `4`, and handle failures SHALL exit `5`.

#### Scenario: Aggregate recovery keeps authority opaque

- GIVEN a corrections aggregate verdict and exactly one eligible done unit
- WHEN `recover-aggregate` succeeds
- THEN its response SHALL reveal no bearer secret
- AND duplicate or multiple unit selection SHALL fail without mutation

### Requirement: Role and hand-off contract

The advisory role map SHALL be: Daimon (user interviews, intent, continuity, and factual communication; no workflow commands); Aion (all commands when coordination requires them); Explorer (`explore`); Specifier (`spec`); Designer (`design`); TaskPlanner (`plan`); Implementer (`list-ready-units`, `claim-unit`, `unit-tdd`, `unit-complete`); Reviewer (`unit-review`, `complete`). Implementers SHALL NOT review and SHALL hand off only the handle path for workflow units. There SHALL be no Archivist role. The CLI SHALL NOT enforce this map.

#### Scenario: Role map is advisory

- GIVEN a local caller outside the documented role map
- WHEN it invokes a valid command
- THEN domain rules SHALL apply without role authorization

### Requirement: User intent and runtime boundary

Daimon SHALL interview, clarify, preserve continuity, forward accepted requests, and communicate only Aion-acknowledged facts or clarification requests. Mid-flight input SHALL remain requested, not applied, until Aion admits it against current workflow and repository state. Aion SHALL be the sole orchestration authority and own workflow context, mutations, specialist dispatch, approvals, recovery, continuation, capability coordination, and completion. PitCrew SHALL NOT add a daemon, service, IPC, polling, network API, durable inbox, database state, or lifecycle; concurrent Daimon availability depends on host support for addressable agents.

#### Scenario: Replacement Aion recovers from durable state

- GIVEN orchestration restarts
- WHEN replacement Aion reads `workflow show`
- THEN it SHALL reconstruct current context without hidden process state
