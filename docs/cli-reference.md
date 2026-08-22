# PitCrew CLI reference

PitCrew exposes `tui` and `principles`, two global flags, and exactly 16 `workflow` subcommands. Every flag is long-form. Commands not listed here do not exist.

## Quick path

```sh
pitcrew workflow new --name "Ship the change" --goal "Deliver the accepted behavior" --actor daimon
pitcrew workflow show --workflow-id wf-<24hex>
pitcrew tui
pitcrew principles
```

A full workflow progresses through exploration, specification, design, planning, approval, implementation, and independent aggregate review/completion. Unit review is selective. Use the `next_action` returned by each successful envelope rather than guessing the next transition.

## Closed command matrix

| Command | Required inputs |
|---|---|
| `tui` | None; extra arguments are rejected. |
| `workflow new` | `--name <text> --goal <text> --actor <label>` |
| `workflow show` | `--workflow-id <wf-id>` |
| `workflow explore` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow spec` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow design` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow plan` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow approve-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> [--approve-exception <wu-id> ...]` |
| `workflow list-ready-units` | `--workflow-id <wf-id>` |
| `workflow begin-implementation` | `--workflow-id <wf-id> --revision <n> --actor <label>` |
| `workflow complete` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow abandon` | `--workflow-id <wf-id> --revision <n> --actor <label> --reason <text>` |
| `workflow claim-unit` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `workflow recover-unit-claim` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `workflow unit-tdd` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path> --input-file <path>` |
| `workflow unit-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path> --input-file <path>` |
| `workflow unit-complete` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path>` |

Global `--help` and `--version` are flags, not commands. `principles [--json]` prints the embedded maxims as exact text or a raw structured JSON array.

## Read-only terminal inspection

`pitcrew tui` runs the embedded visual inspector in the current process. Its workflow detail is an operational dashboard: a compact full-width synopsis shows state, revision, goal, unit progress, current work, any unresolved blocker, and the executable next action; one full-width chronological grid then shows semantic occurrences as `When | Phase | Work | Try | Activity | Actor | Outcome / reason`. The grid retains all seven columns at 120 columns and above, moves Actor to progressive detail below 120, compacts at 80, and renders each occurrence on two lines from 60 through 79 columns. Arrow and Vim keys move by occurrence, Page/Home/End navigate the semantic stream, and Enter opens exact durable evidence without exposing claim handles or secrets. All views read only `<project>/.pitcrew/state.db`; the TUI does not initialize a repository, run migrations, mutate workflow state, or invoke another executable. An uninitialized project shows `No PitCrew repository is initialized for this project.` and remains unchanged. Use `/` to search and `q` to exit.

## JSON input files

`--input-file` is the only payload transport for stage artifacts, plans, TDD evidence, and reviews. PitCrew rejects symlinks, non-regular files, invalid UTF-8, unknown fields, malformed JSON, trailing data, and multiple documents before opening the project store.

### Stage artifact

```json
{"content":"non-empty artifact content"}
```

The command infers `exploration`, `specification`, or `design`. While a workflow remains in `exploring`, `specifying`, or `designing`, its corresponding stage command may be repeated to append an amendment. The amendment increments the revision without advancing the state, and the response keeps the forward `next_action`. `workflow show` returns all accepted artifacts ordered by revision and insertion order. Later and terminal states still reject stage amendments.

### Plan

```json
{
  "summary": "One reviewable unit",
  "scope": "internal/example",
  "max_parallel_units": 1,
  "work_units": [{
    "id": "wu-000000000000000000000001",
    "description": "Implement the example",
    "scope": "internal/example",
    "areas": ["internal/example"],
    "depends_on": [],
    "estimated_changed_lines": 200,
    "estimated_review_minutes": 30
  }]
}
```

Units over 400 changed lines or 60 review minutes require a non-empty `admission_exception.justification` and a matching repeatable `--approve-exception` during approval. Explicit `overlap_approvals` may name one exact unit pair with justification.

### TDD evidence

```json
{
  "red_command": "go test ./internal/example -run TestBehavior",
  "red_outcome": "exit 1: behavior missing",
  "green_command": "go test ./internal/example -run TestBehavior",
  "green_outcome": "exit 0",
  "refactor_summary": "Removed duplication",
  "validation_command": "go test ./...",
  "validation_outcome": "exit 0",
  "changed_paths": "internal/example"
}
```

### Unit review

Approved:

```json
{"verdict":"approved","summary":"Evidence and implementation match","findings":""}
```

Corrections:

```json
{"verdict":"corrections","summary":"Boundary missing","findings":"Add the expiry case","plan_impact":"inside"}
```

Outside-plan corrections use `"plan_impact":"outside"` and return `daimon revise plan`, requiring Daimon to revise the plan through a new OpenSpec change.

Unit review is optional: current TDD evidence plus the active owner handle can complete a unit without an approval. A corrections verdict still reopens the unit and requires fresh evidence.

### Aggregate review

`workflow complete` uses the approved review shape above, or a corrections payload without `plan_impact`. The independent reviewer compares the repository result and tests with requirements, every specification/design amendment, the approved plan and tasks, current implementation evidence, and unit reviews. Approval records the review and completes atomically. Corrections record the review, advance workflow CAS, remain `ready_to_complete`, and return control to Daimon for a fresh correction/review cycle.

## Envelopes and exit codes

Successful workflow commands write one JSON document to stdout:

```json
{"ok":true,"data":{},"warnings":[],"next_action":"workflow show"}
```

Failures write one single-line JSON error to stderr and nothing to stdout.

- `0 — ok`
- `1 — internal`
- `2 — usage`
- `3 — state`
- `4 — CAS`
- `5 — handle`

Workflow mutations compare `--revision` with the aggregate revision. Unit commands compare it with the unit revision. On exit `3` or `4`, inspect current state once. If the attempted work is legitimate but the harness blocks it, surface the obstruction; never repeat an identical command.

## Opaque claims

Production claim and recovery return only `data.handle_path`. Handle files are caller-owned `0600` JSON inside a `0700` directory; they contain a secret hash, never the plaintext secret. The first successful unit command activates the handle, successful commands refresh its five-minute expiry, and completion revokes it.

The hidden `--print-claim-handle-secret-once` flag is for operators only. It never appears in help or agent templates. It commits revocation before returning the secret exactly once at `data.claim_secret`.
