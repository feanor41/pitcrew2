# Spec: cli-surface

## Purpose

Define the closed CLI, command inputs, envelopes, errors, and caller identity semantics.

## Requirements


### Requirement: Closed command and input contract

The CLI SHALL expose only `install`, `project`, `context`, `delivery`, `principles`, `tui`,
global `--help`/`--version`, and the 24 `workflow` commands below. Flags SHALL be
long-form. Each listed flag is required unless bracketed; `--input-file` SHALL
name a readable regular file containing one JSON document and SHALL be the only
transport for delivery mutations, artifacts, operational reports, plans,
evidence, reviews, and consolidation bodies.

| Command | Required inputs |
|---|---|
| `install` | exactly one of `codex`, `opencode`, `claude`, or `pi` |
| `project inspect` | none |
| `project consolidate` | `--input-file <path>` |
| `context inspect` | none |
| `context initialize` | none |
| `context record` | `--actor <nonblank-bounded-label> --input-file <path>` |
| `delivery start` | `--actor <label> --input-file <path>` |
| `delivery update` | `--delivery-id <dl-id> --revision <n> --actor <label> --input-file <path>` |
| `delivery show` | `--delivery-id <dl-id|wf-id>` |
| `delivery search` | `--query <nonblank-text>` |
| `delivery active` | none; extra arguments are rejected |
| `new` | `--name <text> --goal <text> --actor <label>` |
| `continue` | `--from <terminal-wf-id> --actor <label>` |
| `show` | `--workflow-id <wf-id> [--view coordination|phase|unit|aggregate|audit] [--unit-id <wu-id>]`; unit id is required only for the unit view |
| `progress` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `request-capability` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `explore`, `spec`, `design` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `plan`, `amend-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `approve-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> [--approve-exception <wu-id> ...]` |
| `list-ready-units` | `--workflow-id <wf-id>` |
| `begin-implementation` | `--workflow-id <wf-id> --revision <n> --actor <label>` |
| `complete` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `authorize-correction` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `abandon` | `--workflow-id <wf-id> --revision <n> --actor <label> --reason <text>` |
| `claim-unit`, `recover-unit-claim`, `handoff-review`, `recover-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `recover-aggregate` | `--workflow-id <wf-id> --revision <n> --actor <label> --handle-dir <dir>` plus exactly one of `--input-file <path>` or historical `--unit-id <wu-id>` |
| `unit-tdd`, `unit-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path> --input-file <path>` |
| `unit-complete` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path>` |

`pitcrew install` SHALL accept no aliases, mixed-case runtime names, flags, or extra arguments. `pitcrew install --help` SHALL perform no installation and SHALL print exactly:

```text
Usage: pitcrew install <codex|opencode|claude|pi>

Installs or updates PitCrew agents for one runtime.

Runtimes: codex, opencode, claude, pi
Read the four maxims of the harness: pitcrew principles.
```

Root help SHALL list `install codex|opencode|claude|pi`, `project inspect|consolidate`, and `context inspect|initialize|record`. Unknown flags, missing flags, unreadable/non-regular input files, malformed JSON, or an invalid install runtime SHALL fail with exit code `2` before mutation. `--name` SHALL be explicit, non-empty after trimming, and bounded by the workflow name limit; it SHALL NOT be derived for new workflows. A well-formed payload that violates its domain contract SHALL fail with exit code `3` without mutation. `--handle-dir` is the only production handle-output selector. The hidden claim debug flag is defined by `claim-handles`. No install, install-help, install-usage, project-inspect, context-inspect, TUI, help, version, or principles path SHALL initialize project state.

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

### Requirement: Closed project-context commands

`context inspect` and `context initialize` SHALL accept no flags. `context
record` SHALL accept only `--actor` and `--input-file`; its input SHALL be one
strict schema-v1 snapshot in a readable regular non-symlink file. Inspect SHALL
be non-creating and return the truthful inspection with `next_action:"context
initialize"` unless complete. Initialize SHALL return `inspection` and
`persisted`; record SHALL return the resulting inspection. Transport or JSON
failures SHALL exit 2, while project, domain, consolidation, and store failures
SHALL exit 3. Existing commands and representations SHALL remain unchanged.

#### Scenario: Context transport is strict and read-only inspection is inert

- GIVEN an absent project context and record inputs containing an unknown field, malformed JSON, a symlink, or an extra argument
- WHEN the context commands run
- THEN inspection SHALL create no state and only the exact record invocation SHALL reach domain behavior

### Requirement: Direct delivery command contract

`delivery start` SHALL accept strict `{operation_key,route,goal,route_reason}`
JSON, where route is only `direct_inline` or `delegated_direct`, and return one
`dl-*` trace in `in_progress` at revision 1. Identical operation-key input SHALL
return the same identity; this safe replay SHALL recover a lost response without
creating another trace, while conflicting reuse SHALL fail without mutation.
`delivery update` SHALL accept strict `{status,summary,next_action}` JSON and use
revision CAS. Status SHALL be one of `in_progress`, `blocked`, `interrupted`,
`completed`, `cancelled`, or `failed`; terminal traces SHALL be immutable.
`delivery show` SHALL resolve either physical trace kind, and `delivery search`
SHALL reuse the unified bounded literal projection.

`delivery active` SHALL be an argument-free, read-only, non-initializing view
over that same unified projection. It SHALL include direct traces only in
`in_progress`, `blocked`, or `interrupted` and every non-terminal workflow, and
SHALL return delivery identity, route, current status, revision, and derived
`next_action`. Terminal and unknown direct states SHALL be excluded. Linked
worktrees SHALL share the canonical project result; independent clones SHALL
remain isolated. Ordering SHALL be stable for display but SHALL NOT confer
selection authority.

#### Scenario: Active candidate cardinality controls continuation

- GIVEN zero active candidates
- WHEN Aion runs `delivery active`
- THEN discovery SHALL succeed without creating project state and normal admission MAY continue
- GIVEN exactly one active candidate
- WHEN Aion continues work
- THEN it SHALL perform one identity-specific inspection and retain that identity and revision
- GIVEN multiple active candidates without an explicit returned ID in accepted user intent
- WHEN Aion considers continuation
- THEN it SHALL mutate nothing and request clarification
- AND it SHALL NOT select by recency, ordering, route, goal similarity, or status

#### Scenario: Active discovery preserves project isolation

- GIVEN one canonical repository with a linked worktree and an independent clone
- WHEN each checkout runs `delivery active`
- THEN the main checkout and linked worktree SHALL return the same candidates
- AND the independent clone SHALL return only its own candidates without creating state

#### Scenario: Direct commands never synthesize workflow machinery

- GIVEN an accepted direct or delegated delivery
- WHEN Aion starts, updates, shows, and searches it
- THEN exactly one direct row SHALL exist with no workflow lifecycle records
- AND stale CAS SHALL exit 4 without mutation
- AND full-workflow route input SHALL be rejected

### Requirement: Project inspection and consolidation surfaces

`project inspect` SHALL resolve the canonical Git common directory without mutation and return `project_id`, `git_common_dir`, `checkout_root`, `initialized`, `repository_move_boundary`, central `paths` (`project_root`, `state_path`, `worktree_root`, `handle_root`), and `legacy` (`candidates`, `diagnostics`, `candidate_set_id`). `project consolidate` SHALL accept a strict manifest with `project_id`, exact `candidate_ids`, and zero or more whole-workflow `choices` containing `workflow_id` and `candidate_id`. Unknown fields, malformed IDs, stale or incomplete source sets, incomplete graphs, and unchosen divergence SHALL fail closed. Success SHALL return the acknowledged `project_id` and `candidate_set_id` only after the atomic import commits.

#### Scenario: Changed inspection requires recovery

- GIVEN sources changed after inspection or a prior import failed
- WHEN consolidation validates the submitted manifest
- THEN it SHALL fail without partial central writes or source mutation
- AND the operator SHALL inspect again and submit a new exact manifest rather than retry unchanged input

#### Scenario: Project-independent surfaces remain inert

- GIVEN a non-repository directory or unusable central root
- WHEN help, version, principles, or install help/usage runs
- THEN it SHALL complete without resolving or initializing project state

### Requirement: Representations and exits

Each successful workflow command SHALL emit one JSON document:

```json
{"ok":true,"data":{},"warnings":[],"next_action":"..."}
```

Workflow and install-argument failures SHALL write one single-line error envelope to stderr, nothing to stdout, and use exactly: `1` internal, `2` usage, `3` state, `4` CAS, `5` handle. State errors SHALL name current and expected state. After valid install dispatch, the embedded POSIX installer SHALL stream actionable plain diagnostics directly to stderr, preserve its non-zero process status, and emit no success stdout on failure. Successful installation SHALL emit exactly `Installed PitCrew agents for <Runtime> in <registry>` followed by a newline; managed-update warnings MAY precede it on stderr. `principles` SHALL emit embedded `MAXIMS.md` bytes, or a raw array with `--json`; help/version are plain text. Every help output SHALL end with `Read the four maxims of the harness: pitcrew principles.` PitCrew's current canonical version SHALL be `0.20.1` and MUST conform to Semantic Versioning 2.0.0. Global `--version` and the TUI header MUST resolve the identical current version from one canonical version source.

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

### Requirement: Proportional workflow inspection views

`workflow show` SHALL accept exactly `--view coordination|phase|unit|aggregate|audit` as an optional selector. `--unit-id` SHALL be required for `unit`, rejected for every other explicit view, and rejected when the view is omitted. Invalid values and combinations SHALL exit `2` before opening or mutating the store.

An omitted view and explicit `audit` SHALL preserve the existing full-audit response byte and schema contract. The `coordination`, `phase`, `unit`, and `aggregate` selections SHALL call the shared bounded history projection and return its tagged workflow identity plus exactly the selected payload. They SHALL NOT load or serialize the audit record graph, unrelated unit evidence, handle paths, hashes, or secrets.

Aion SHALL use `coordination` for summary-first coordination and initial inspection after a state or CAS failure. Daimon SHALL NOT invoke workflow commands and SHALL receive only Aion-acknowledged facts or clarification requests. Phase specialists SHALL use `phase`, Implementers and selective Reviewers SHALL use `unit`, and aggregate Reviewers SHALL use `aggregate`. Full `audit` remains the explicit compatibility and operator-debugging escape hatch.

#### Scenario: Omitted workflow view stays compatible

- GIVEN an existing workflow
- WHEN a caller invokes `workflow show` without `--view` and then with `--view audit`
- THEN the successful response bytes and schema SHALL match
- AND the response SHALL retain workflow, synopsis, artifacts, records, and timeline

#### Scenario: Bounded view excludes audit and claim material

- GIVEN a workflow with multiple unit results and historical activities
- WHEN a caller requests coordination, phase, one selected unit, or aggregate
- THEN exactly the selected tagged projection SHALL be returned
- AND the audit graph, unrelated evidence, handle paths, hashes, and secrets SHALL be absent

#### Scenario: Unit selector is closed before persistence

- GIVEN an absent project store
- WHEN `--unit-id` is omitted for the unit view or supplied for any other/omitted view
- THEN the command SHALL exit `2`
- AND it SHALL NOT initialize or mutate project state

### Requirement: Backward-compatible typed stage input

`explore`, `spec`, and `design` SHALL continue to accept the exact strict legacy
input `{content}` without inference or upgrade. They SHALL additionally accept
the exact strict typed input `{content,schema_version:1,entries:[...]}` and pass
it to the shared schema-v1 normative workflow API. `schema_version` and
`entries` SHALL appear together. Partial typed shapes, unsupported versions,
unknown fields, and invalid entries SHALL fail before workflow mutation.
Accepted typed entries and their prose artifact SHALL commit atomically and be
reachable through both phase and aggregate projections.

#### Scenario: Typed and legacy stages share one public command

- GIVEN a schema-v1 typed exploration followed by legacy prose specification
  and design inputs
- WHEN phase and aggregate views are requested
- THEN the normalized typed entries SHALL be reachable in both views
- AND the legacy prose artifacts SHALL retain their exact content

#### Scenario: Invalid typed stage input is inert

- GIVEN a partial typed shape, unsupported schema version, or invalid entry
- WHEN a stage command receives it
- THEN the command SHALL fail before changing workflow revision or artifacts

### Requirement: CLI handle use honors capped purpose-aware leases

Implementation and review commands SHALL honor the claim-handle contract's
purpose-aware fifteen-minute expiry measured from issue. A successful command
MUST NOT renew the lease, and no CLI flow SHALL add heartbeat polling or
background renewal.

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

`recover-aggregate` SHALL enforce the policy-aware/historical closed matrix above and reject its secret-print flag. Grouped input SHALL be strict `{aggregate_review_revision,groups:[{causal_invariant,findings,unit_ids}],assignments:[{unit_id,actor}]}`; authority and authorization identifiers are derived. Success returns only actor/unit/revision/opaque-path records. State, projection, selection, and authority failures SHALL exit `3`, stale aggregate CAS SHALL exit `4`, and handle failures SHALL exit `5`.

`authorize-correction` SHALL accept only strict `{aggregate_review_revision,reason,user_direction_confirmed:true}` for the exact exhausted blocker. Malformed, mixed, or unknown input exits `2`; state/authority mismatch exits `3`; stale CAS exits `4`.

#### Scenario: Aggregate recovery keeps authority opaque

- GIVEN a corrections aggregate verdict and eligible grouped done units
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
