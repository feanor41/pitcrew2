# PitCrew CLI reference

PitCrew exposes `install`, `project`, `tui`, `principles`, two global flags, and exactly 23 `workflow` subcommands. Every flag is long-form. Commands not listed here do not exist.

The external role channel is `user ↔ Daimon ↔ Aion ↔ specialists`. Daimon interviews, clarifies intent, preserves conversational continuity, and reports only Aion-acknowledged facts or questions. Aion alone owns routing and workflow coordination. For each accepted delivery, Daimon and the addressable-agent host reuse the same addressable Aion instance across all phases until terminal completion or a genuine blocker; Aion retains workflow context and authority throughout. Mid-flight input remains requested, not applied, until Aion admits it against current state; concurrent Daimon availability depends on host support for addressable agents, not a PitCrew daemon, service, IPC, polling, or inbox.

## Quick path

```sh
pitcrew workflow new --name "Ship the change" --goal "Deliver the accepted behavior" --actor aion
pitcrew workflow show --workflow-id wf-<24hex>
pitcrew tui
pitcrew principles
```

A full workflow progresses through exploration, specification, design, planning, approval, implementation, and independent aggregate review/completion. Unit review is selective. Use the `next_action` returned by each successful envelope rather than guessing the next transition.

## Runtime installation

Install or update the native PitCrew integration for exactly one agentic app:

```sh
pitcrew install codex
pitcrew install opencode
pitcrew install claude
pitcrew install pi
```

Tokens are exact, lowercase, and alias-free. The command installs eight native
agents plus `pitcrew/agent-contract.md`, but not the binary, another runtime,
packages, a daemon, or workflow state. Success prints exactly `Installed
PitCrew agents for <Runtime> in <registry>`.

The selected token overrides cross-runtime detection and uses only its override
or default root:

| Runtime | Override / default root | Native registry | Legacy registry |
|---|---|---|---|
| Codex | `CODEX_HOME` / `~/.codex` | `agents/*.toml` | `prompts/*.md` |
| OpenCode | `OPENCODE_CONFIG_DIR` / `~/.config/opencode` | `agents/*.md` | prior PitCrew entries in `agents/` |
| Claude Code | `CLAUDE_CONFIG_DIR` / `~/.claude` | `agents/*.md` | `prompts/*.md` |
| Pi | `PI_AGENT_HOME` / `~/.pi/agent` | `agents/*.md` | prior PitCrew entries in `agents/` |

The public command transactionally refreshes only PitCrew-managed filenames and
warns exactly: `pitcrew installer: WARNING: PitCrew-managed definitions are
being refreshed; custom content must live outside managed role files.` This is
the public-command warning. The legacy `pitcrew installer: WARNING: replacing
prompts or legacy names; preserve desired custom text before continuing.` is
reserved for direct `scripts/install-templates.sh --overwrite` invocation.
Unrelated files and application configuration are never rewritten; identical
reruns are write no-ops, and failures roll back.

OpenCode additionally requires OpenCode 1.18.23 or newer, `jq`, `timeout`, and
effective top-level `"subagent_depth": 2` (or greater) in the caller's target
project. From that project, inspect the resolved configuration with:

```sh
opencode --pure debug config
```

Upgrade older installations to at least 1.18.23. Set the value in global
configuration or the higher-precedence project configuration that controls the
resolved result, then use the exact verification and `pitcrew install opencode`
rerun commands
printed by a failure. Fix malformed or incompatible resolved output at the
reported configuration source. PitCrew fails before target writes when the
prerequisite is absent or invalid; it does not rewrite user JSON or JSONC.
Depth two enables the existing `Daimon -> Aion -> specialist` call chain
without broadening permissions: Daimon still targets only Aion, Aion only the
six specialists, and specialists cannot delegate.

Pi additionally requires Node.js, an installed and active `pi-subagents`
version 0.25.0 or newer, and `maxSubagentDepth: 3` (or greater) in
`<pi-agent-home>/extensions/subagent/config.json`. Depth three is required for
the bounded `Daimon -> Aion -> specialist` chain. PitCrew validates exact
package identity and accepts a non-empty version/range suffix; it does not
install the extension, access the network, or modify Pi configuration.

## Closed command matrix

| Command | Required inputs |
|---|---|
| `install` | exactly one of `codex`, `opencode`, `claude`, or `pi` |
| `project inspect` | None. |
| `project consolidate` | `--input-file <path>` |
| `tui` | None; extra arguments are rejected. |
| `workflow new` | `--name <text> --goal <text> --actor <label>` |
| `workflow continue` | `--from <terminal-wf-id> --actor <label>` |
| `workflow show` | `--workflow-id <wf-id>` |
| `workflow progress` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow request-capability` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow explore` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow spec` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow design` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow plan` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow amend-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow approve-plan` | `--workflow-id <wf-id> --revision <n> --actor <label> [--approve-exception <wu-id> ...]` |
| `workflow list-ready-units` | `--workflow-id <wf-id>` |
| `workflow begin-implementation` | `--workflow-id <wf-id> --revision <n> --actor <label>` |
| `workflow complete` | `--workflow-id <wf-id> --revision <n> --actor <label> --input-file <path>` |
| `workflow abandon` | `--workflow-id <wf-id> --revision <n> --actor <label> --reason <text>` |
| `workflow claim-unit` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `workflow recover-unit-claim` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `workflow recover-aggregate` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --handle-dir <dir>` |
| `workflow handoff-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <reviewer-label> --handle-dir <dir>` |
| `workflow recover-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <reviewer-label> --handle-dir <dir>` |
| `workflow unit-tdd` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path> --input-file <path>` |
| `workflow unit-review` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path> --input-file <path>` |
| `workflow unit-complete` | `--workflow-id <wf-id> --unit-id <wu-id> --revision <n> --actor <label> --claim-handle <path>` |

## Project inspection and consolidation

The project ID is the lowercase SHA-256 of the canonical Git common-directory
path. A main checkout and its linked worktrees share one ID. An independent
clone or a repository moved to another canonical path receives a different ID
and therefore a different central root.

`pitcrew project inspect` is read-only and non-initializing. Its success
envelope contains:

```json
{
  "project_id": "<64hex>",
  "git_common_dir": "/canonical/repo/.git",
  "checkout_root": "/current/checkout",
  "initialized": false,
  "repository_move_boundary": false,
  "paths": {
    "project_root": "<data-home>/pitcrew/projects/<project-id>",
    "state_path": ".../state.db",
    "worktree_root": ".../worktrees",
    "handle_root": ".../handles"
  },
  "legacy": {
    "candidates": [{"id":"<64hex>","checkout_root":"...","state_path":".../.pitcrew/state.db"}],
    "diagnostics": [],
    "candidate_set_id": "<64hex>"
  }
}
```

If candidates exist, workflow mutation fails closed until their exact set is
acknowledged by a successful consolidation. Submit one strict JSON document:

```json
{
  "project_id": "<64hex>",
  "candidate_ids": ["<64hex>"],
  "choices": [{"workflow_id":"wf-<24hex>","candidate_id":"<64hex>"}]
}
```

`choices` is required only for divergent copies of the same complete workflow;
it selects one whole graph, never individual rows. Run
`pitcrew project consolidate --input-file <path>`. Success atomically writes
all selected graphs and returns `project_id` plus `candidate_set_id`. Equal
copies deduplicate deterministically. Conflicts or incomplete graphs roll back
the central transaction. Source databases and WAL files are never deleted or rewritten.

If inspection changes before import, inspect again and generate a new manifest;
do not edit candidate IDs or retry unchanged input. A successful repeat is
idempotent. The central root is private and defaults to
`${XDG_DATA_HOME:-$HOME/.local/share}/pitcrew/projects/<project-id>/`.

Global `--help` and `--version` are flags, not commands. `principles [--json]` prints the embedded maxims as exact text or a raw structured JSON array.

`pitcrew install --help` exits 0 without installation and prints:

```text
Usage: pitcrew install <codex|opencode|claude|pi>

Installs or updates PitCrew agents for one runtime.

Runtimes: codex, opencode, claude, pi
Read the four maxims of the harness: pitcrew principles.
```

Missing, unknown, mixed-case, or extra install arguments exit 2 before opening
the project store or invoking the embedded installer. Their usage envelope has
message `usage: pitcrew install <codex|opencode|claude|pi>`.

`workflow continue` accepts only a completed or abandoned predecessor. It atomically creates a fresh revision-1 draft with the same name and exact goal, plus a child-owned `continuation` artifact that records the predecessor ID, terminal state, and revision. The predecessor receives no row, event, artifact, or activity writes, and one predecessor may have multiple independent successors. Success returns `data.workflow`, `data.predecessor`, and `next_action: "workflow explore"`.

## Read-only terminal inspection

`pitcrew tui` runs the embedded visual inspector in the current process. Workflow detail is a four-pane operations cockpit: **Tree** preserves stable workflow, stage, unit, and durable-record identity; **Status** keeps lifecycle facts and the executable next action separate from acknowledged progress; **Units** follows accepted plan order with textual status and reasons; **Activity** shows chronological human-readable actions and opens their exact durable evidence without exposing claim handles or secrets. Exact planned-work percentages appear only when a valid accepted plan reconciles with its work units; otherwise Status explains why precision is unavailable.

At `120x30` or larger the panes render as an exact-height 2x2 grid. Smaller supported terminals, including `80x24`, `60x24`, and the `60x16` minimum, render one focused pane with a tab strip rather than squeezing columns. `Tab` and `Shift+Tab` cycle panes in reading order and reverse order. Arrow and Vim keys move within the focused pane; Tree uses Left/`h` and Right/`l` to collapse, ascend, expand, or descend; Activity additionally supports Page Up/Page Down/Home/End. `Enter` expands Tree branches or opens the selected record/activity evidence. Left/`h` or `Esc` returns from drill-down. `/` opens a visible focused search row, where `Enter` submits and `Esc` cancels. `r` refreshes without losing semantic focus, except while search owns text input, and `q` exits outside text entry.

Color never carries meaning alone. `NO_COLOR` disables ANSI color while retaining selection and focused-pane cues. `PITCREW_ASCII=1` replaces Unicode state, tree, selection, and border glyphs with ASCII equivalents while retaining color when supported. `TERM=dumb` selects both ASCII and monochrome fallbacks. Refresh errors retain the prior data for retry. Home and Workflows retain their established compositions. All views resolve Git identity and read only the central state database; the TUI does not initialize a repository, run migrations, mutate workflow state, or invoke another executable. An uninitialized project shows `No PitCrew repository is initialized for this project.` and remains unchanged.

## JSON input files

`--input-file` is the only payload transport for stage artifacts, operational reports, plans, TDD evidence, and reviews. PitCrew rejects symlinks, non-regular files, invalid UTF-8, unknown fields, malformed JSON, trailing data, and multiple documents before opening the project store.

### Stage artifact

```json
{"content":"non-empty artifact content"}
```

The command infers `exploration`, `specification`, or `design`. While a workflow remains in `exploring`, `specifying`, or `designing`, its corresponding stage command may be repeated to append an amendment. The amendment increments the revision without advancing the state, and the response keeps the forward `next_action`. `workflow show` returns all accepted artifacts ordered by revision and insertion order. Later and terminal states still reject stage amendments.

### Progress report

```json
{"status":"advanced","summary":"Unit tests pass","next_action":"workflow handoff-review"}
```

`status` is exactly `advanced` or `blocked`; every field is required and trimmed before canonical storage. Progress observes the supplied non-terminal workflow revision and appends an artifact plus linked activity without changing state, revision, timestamp, or events. Repeated reports remain ordered. Success returns the typed report and uses its submitted `next_action`.

The TUI synopsis shows only the latest valid progress report, with a non-color advanced or blocked marker, its summary, and its report-specific next action. Lifecycle/unit facts and their executable next action remain separate. Every progress artifact remains available in chronological history and drill-down. Refresh reads the latest report without polling or writing. Daimon reports concise status only for a real transition, completed unit, resolved correction, achieved small objective, or observed blocker; otherwise it stays silent rather than fabricating movement or repeating encouragement.

### Capability request

```json
{"capability":"browser tool","reason":"UI behavior needs verification","blocked_action":"inspect the running page"}
```

Every field is required, trimmed, and stored canonically. The command observes an exact non-terminal revision, appends `capability_request` plus `capability_requested`, and returns `next_action: "aion coordinate requested capability"` without changing lifecycle state. Repeated requests remain ordered and generically inspectable. A request records a missing tool, command, or transition; it has no fulfillment, ownership, resolution, or status lifecycle and does not imply the capability exists.

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

### Amend plan

`workflow amend-plan` requires `--workflow-id`, syntactically valid `--revision`, non-empty declarative `--actor`, and the same strictly validated plan JSON file as `plan`. This control-plane revision has no opaque structural plan-amendment authority, so it always exits `3` with `amend-plan requires structural plan amendment authority; no such authority exists`. Neither `--actor aion` nor any other flag value can authorize, bypass, or change that outcome. The command does not inspect or mutate planning state, plan rows, work units, artifacts, events, or CAS; the revision is parsed only because the closed input matrix requires it. Submit corrected scope through the applicable new or continued workflow. A future authorized amendment must use non-forgeable authority, preserve the replaced plan as durable history, atomically replace pending units, and use CAS.

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

Outside-plan corrections use `"plan_impact":"outside"` and return `aion revise plan`, requiring Aion to revise the plan through a new OpenSpec change.

Unit review is optional: current TDD evidence plus the active implementation handle can complete a unit without an approval. When selected, Aion uses `handoff-review` to issue independent reviewer-owned authority and passes only its opaque path to the Reviewer; `unit-review` consumes that handle atomically with the verdict. If it expires before a verdict, `recover-review` rotates only review authority for the same reviewer. A corrections verdict reopens the unit, returns `recover-unit-claim`, and requires fresh evidence. During reviewing, that recovery command is restricted to the current evidence actor and yields completion-ready implementation authority.

### Aggregate review

`workflow complete` uses the approved review shape above, or a corrections payload without `plan_impact`. The independent reviewer compares the repository result and tests with requirements, every specification/design amendment, the approved plan and tasks, current implementation evidence, and unit reviews. Approval records the review and completes atomically. Corrections record the review, advance workflow CAS, remain `ready_to_complete`, return `aion coordinate aggregate corrections`, and require Aion to run a fresh correction/review cycle.

`workflow recover-aggregate` requires exactly one `--unit-id`, `--workflow-id`, `--revision`, declarative `--actor`, and `--handle-dir`. It succeeds only in `ready_to_complete`, when that exact workflow CAS revision is current, the newest `aggregate_review` verdict is `corrections`, and the selected unit is `done`. It atomically preserves all aggregate-review and unit-evidence records, returns the workflow to `implementing` at its next aggregate revision, reopens the selected unit at its next unit revision, records recovery, and returns only `data.handle_path`, `data.unit_id`, and `data.unit_revision` with `next_action: "workflow unit-tdd"`. Missing/no-longer-current corrections, a non-done selection, terminal state, duplicate/multiple selection, or repeated recovery exit `3`; stale workflow CAS exits `4`; unsafe, expired, revoked, or purpose-mismatched returned handles exit `5`. The command rejects the hidden secret-print flag and never emits a raw secret. Its opaque implementation handle is a `0600` file in caller `0700` storage, expires after five minutes, is activated by successful TDD, refreshed by successful unit commands, and is revoked by completion; eligible replacement is through `recover-unit-claim`. The caller label is metadata only, not recovery authority.

## Envelopes and exit codes

Successful workflow commands write one JSON document to stdout:

```json
{"ok":true,"data":{},"warnings":[],"next_action":"workflow show"}
```

Workflow failures and install argument failures write one single-line JSON
error to stderr and nothing to stdout. After valid install dispatch, runtime
prerequisite, validation, write, rollback, and wrapper failures retain their
actionable plain stderr and non-zero child/wrapper status; they are not wrapped
as workflow JSON and never emit the success line.

- `0 — ok`
- `1 — internal`
- `2 — usage`
- `3 — state`
- `4 — CAS`
- `5 — handle`

Workflow mutations compare `--revision` with the aggregate revision. Unit commands compare it with the unit revision. `amend-plan` is the explicit no-authority exception: it parses but never uses its revision because it always rejects before a mutation. On exit `3` or `4`, inspect current state once. If the attempted work is legitimate but the harness blocks it, surface the obstruction; never repeat an identical command.

## Opaque claims

Production claim and recovery (`claim-unit`, `recover-unit-claim`, `recover-review`, and `recover-aggregate`) return only `data.handle_path`. Handle files are caller-owned `0600` JSON inside a `0700` directory; they contain a secret hash, never the plaintext secret. The first successful unit command activates the handle, successful commands refresh its five-minute expiry, and completion revokes it. They are bound to their workflow, selected unit, revision, purpose, and declarative collision metadata; they cannot select another unit or survive expiry/revocation.

The hidden `--print-claim-handle-secret-once` flag is for operators only. It never appears in help or agent templates. It commits revocation before returning the secret exactly once at `data.claim_secret`.
