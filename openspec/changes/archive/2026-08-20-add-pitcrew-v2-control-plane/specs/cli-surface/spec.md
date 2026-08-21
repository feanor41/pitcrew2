# Spec: cli-surface

## Purpose

Define the closed CLI, command inputs, envelopes, errors, and caller identity semantics.

## ADDED Requirements


### Requirement: Closed command and input contract

The CLI SHALL expose only `principles`, global `--help`/`--version`, and the 16 `workflow` commands below. Flags SHALL be long-form. Each listed flag is required unless bracketed; `--input-file` SHALL name a readable regular file containing one JSON document and SHALL be the only transport for artifact, plan, evidence, and review bodies.

| Command | Required inputs |
|---|---|
| `new` | `--goal <text> --actor <label>` |
| `show` | `--workflow-id <wf-id>` |
| `explore`, `spec`, `design` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `plan` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `approve-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> [--approve-exception <wu-id> ...]` |
| `list-ready-units` | `--workflow-id <wf-id>` |
| `begin-implementation`, `complete` | `--workflow-id <wf-id> --revision <n> --actor <label>` |
| `abandon` | `--workflow-id <wf-id> --revision <n> --actor <label> --reason <text>` |
| `claim-unit`, `recover-unit-claim` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `unit-tdd`, `unit-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path> --input-file <path>` |
| `unit-complete` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path>` |

Unknown flags, missing flags, unreadable/non-regular input files, or malformed JSON SHALL fail with exit code `2` before mutation. A well-formed payload that violates its domain contract SHALL fail with exit code `3` without mutation. `--handle-dir` is the only production handle-output selector. The hidden claim debug flag is defined by `claim-handles`.

#### Scenario: Every command enforces its row

- GIVEN each closed command
- WHEN it is invoked with and without every input in its R1 row
- THEN only the complete valid invocation SHALL pass argument validation

### Requirement: Representations and exits

Each successful workflow command SHALL emit one JSON document:

```json
{"ok":true,"data":{},"warnings":[],"next_action":"..."}
```

Failures SHALL write one single-line error envelope to stderr, nothing to stdout, and use exactly: `1` internal, `2` usage, `3` state, `4` CAS, `5` handle. State errors SHALL name current and expected state. `principles` SHALL emit embedded `MAXIMS.md` bytes, or a raw array with `--json`; help/version are plain text. Every help output SHALL end with `Read the four maxims of the harness: pitcrew principles.`

#### Scenario: Success and failure representations

- GIVEN one successful and one failing workflow invocation
- WHEN their outputs are captured
- THEN each SHALL use the specified stream, envelope, and exit code

### Requirement: Declarative actor metadata

`--actor` SHALL be a non-empty caller-declared label recorded with mutations and used only to detect Implementer/Reviewer collisions. It SHALL NOT authenticate, authorize, select commands, or establish trust. Every command remains callable by every local caller; role restrictions remain prompt rules.

#### Scenario: Actor does not authorize

- GIVEN any non-empty actor label
- WHEN a syntactically valid command is invoked
- THEN access SHALL NOT be granted or denied because of that label

### Requirement: Role and hand-off contract

The role map SHALL remain: Master (`new`, `show`, `approve-plan`, `abandon`, optional `complete`); Explorer (`explore`); Specifier (`spec`); Designer (`design`); TaskPlanner (`plan`); Implementer (`list-ready-units`, `claim-unit`, `unit-tdd`, `unit-complete`); Reviewer (`unit-review` only); Archivist (`complete`). Implementers SHALL NOT review and SHALL hand off only the handle path. The CLI SHALL NOT enforce this map.

#### Scenario: Role map is advisory

- GIVEN a local caller outside the documented role map
- WHEN it invokes a valid command
- THEN domain rules SHALL apply without role authorization
