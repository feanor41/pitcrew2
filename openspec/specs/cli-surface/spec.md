# Spec: cli-surface

## Purpose

Define the closed CLI, command inputs, envelopes, errors, and caller identity semantics.

## Requirements


### Requirement: Closed command and input contract

The CLI SHALL expose only `principles`, global `--help`/`--version`, and the 16 `workflow` commands below. Flags SHALL be long-form. Each listed flag is required unless bracketed; `--input-file` SHALL name a readable regular file containing one JSON document and SHALL be the only transport for artifact, plan, evidence, and review bodies.

| Command | Required inputs |
|---|---|
| `new` | `--name <text> --goal <text> --actor <label>` |
| `show` | `--workflow-id <wf-id>` |
| `explore`, `spec`, `design` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `plan` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `approve-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> [--approve-exception <wu-id> ...]` |
| `list-ready-units` | `--workflow-id <wf-id>` |
| `begin-implementation` | `--workflow-id <wf-id> --revision <n> --actor <label>` |
| `complete` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `abandon` | `--workflow-id <wf-id> --revision <n> --actor <label> --reason <text>` |
| `claim-unit`, `recover-unit-claim` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `unit-tdd`, `unit-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path> --input-file <path>` |
| `unit-complete` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path>` |

Unknown flags, missing flags, unreadable/non-regular input files, or malformed JSON SHALL fail with exit code `2` before mutation. `--name` SHALL be explicit, non-empty after trimming, and bounded by the workflow name limit; it SHALL NOT be derived for new workflows. A well-formed payload that violates its domain contract SHALL fail with exit code `3` without mutation. `--handle-dir` is the only production handle-output selector. The hidden claim debug flag is defined by `claim-handles`.

(Previously: `workflow new` required only `--goal` and `--actor` and had no explicit-name contract.)

#### Scenario: Every command enforces its row

- GIVEN each closed command
- WHEN it is invoked with and without every input in its row
- THEN only the complete valid invocation SHALL pass argument validation

#### Scenario: New workflow requires an explicit name

- GIVEN `workflow new` without `--name` or with a blank or over-limit value
- WHEN argument and domain validation run
- THEN creation SHALL fail without persisted mutation

### Requirement: Representations and exits

Each successful workflow command SHALL emit one JSON document:

```json
{"ok":true,"data":{},"warnings":[],"next_action":"..."}
```

Failures SHALL write one single-line error envelope to stderr, nothing to stdout, and use exactly: `1` internal, `2` usage, `3` state, `4` CAS, `5` handle. State errors SHALL name current and expected state. `principles` SHALL emit embedded `MAXIMS.md` bytes, or a raw array with `--json`; help/version are plain text. Every help output SHALL end with `Read the four maxims of the harness: pitcrew principles.` PitCrew's current canonical version SHALL be `0.3.0` and MUST conform to Semantic Versioning 2.0.0. Global `--version` and the TUI header MUST resolve the identical current version from one canonical version source.

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

### Requirement: Declarative actor metadata

`--actor` SHALL be a non-empty caller-declared label recorded with mutations and used only to detect Implementer/Reviewer collisions. It SHALL NOT authenticate, authorize, select commands, or establish trust. Every command remains callable by every local caller; role restrictions remain prompt rules.

#### Scenario: Actor does not authorize

- GIVEN any non-empty actor label
- WHEN a syntactically valid command is invoked
- THEN access SHALL NOT be granted or denied because of that label

### Requirement: Role and hand-off contract

The advisory role map SHALL be: Daimon (all commands when coordination requires them); Explorer (`explore`); Specifier (`spec`); Designer (`design`); TaskPlanner (`plan`); Implementer (`list-ready-units`, `claim-unit`, `unit-tdd`, `unit-complete`); Reviewer (`unit-review`, `complete`). Implementers SHALL NOT review and SHALL hand off only the handle path for workflow units. There SHALL be no Archivist role. The CLI SHALL NOT enforce this map.

#### Scenario: Role map is advisory

- GIVEN a local caller outside the documented role map
- WHEN it invokes a valid command
- THEN domain rules SHALL apply without role authorization
