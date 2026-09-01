# PitCrew

PitCrew is a **local subprocess control plane for agent-assisted software
delivery**. It gives one person, on one machine, a durable way to coordinate a
project without turning the coordination layer into a service. Agents call the
`pitcrew` CLI, and PitCrew records workflow state, evidence, and the next valid
action in one central database shared by every linked worktree of the project.

It is a harness, not an autonomous agent platform: PitCrew does not run a
daemon, contact models, choose what to build, or replace the agent runtime that
hosts the conversation.

## Why PitCrew?

Agent work can lose context, blur responsibilities, or claim progress that was
never verified. PitCrew keeps the useful parts explicit:

- one durable, project-local account of the workflow;
- clear separation between conversation, coordination, implementation, and
  review;
- revision-checked transitions and an executable `next_action`;
- evidence attached to the work rather than relayed through chat; and
- a read-only terminal view for understanding progress at a glance.

The process stays proportional to the result. Small, understood changes can be
handled directly. Broader but straightforward changes can be delegated. Work
with material uncertainty or stronger assurance needs can use the complete
workflow. The harness supplies rigor where it earns its cost; it is never the
goal itself.

## Mental model

```text
user <-> Daimon <-> Aion <-> pc2-* specialists
                      \          /
                       \        /
                    pitcrew subprocess
                           |
       <data-home>/pitcrew/projects/<project-id>/state.db
```

- **Daimon** keeps the user conversation coherent and reports only facts that
  Aion has acknowledged.
- **Aion** owns routing, workflow context, and mutation sequencing.
- **`pc2-*` specialists** investigate, specify, design, plan, implement, or
  review bounded work.
- **PitCrew** is the local ledger and transition boundary. These roles live in
  the external agent runtime; they are not background services inside PitCrew.

## Install from source

PitCrew currently requires **Go 1.26.5**.

```sh
git clone https://github.com/feanor41/pitcrew2.git
cd pitcrew2
go install ./cmd/pitcrew
pitcrew --version
```

`go install` writes the binary to `GOBIN`, or to `$(go env GOPATH)/bin` when
`GOBIN` is empty. Ensure that directory is on `PATH` if `pitcrew` is not found:

```sh
go env GOBIN
go env GOPATH
```

### Install the runtime integration

Binary installation and runtime integration are separate steps. The following
command installs or refreshes PitCrew's native agent definitions for Codex; it
does **not** install the `pitcrew` binary or create workflow state:

```sh
pitcrew install codex
```

Use the runtime you actually host agents with:

```sh
pitcrew install opencode
pitcrew install claude
pitcrew install pi
```

Runtime-specific prerequisites and managed-file behavior are documented in
the [CLI reference](docs/cli-reference.md#runtime-installation).

## Quick start

Run workflow commands from the project you want PitCrew to track:

```sh
pitcrew workflow new \
  --name "Ship the change" \
  --goal "Deliver the accepted behavior" \
  --actor aion
```

The command creates that project's central database and returns a workflow
ID, current revision, and `next_action`. Inspect it at any time:

```sh
pitcrew workflow show --workflow-id wf-<24hex>
```

Follow the returned `next_action` rather than guessing the next transition.
For a visual, read-only overview, open:

```sh
pitcrew tui
```

### Roadmap Inbox

Capture medium- and long-term findings without turning them into immediate work:

```sh
pitcrew roadmap capture --input-file finding.json
pitcrew roadmap prepare-github --roadmap-id rm-<24hex> --input-file github.json
pitcrew roadmap acknowledge --roadmap-id rm-<24hex> --input-file published.json
```

The inbox is project-local and offline. `prepare-github` produces deterministic
issue content for an operator-selected tool; PitCrew never creates the GitHub
issue. GitHub becomes authoritative only after the operator records the
canonical published issue with `acknowledge`.

Workflow detail is an operations cockpit with four keyboard-reachable panes:
Tree, Status, Units, and Activity. At `120x30` or larger all four panes form a
2x2 grid; smaller supported terminals show one focused pane with tabs. Use
`Tab` / `Shift+Tab` to change panes, arrow or Vim keys to move, `Enter` to
expand or inspect exact evidence, `/` to search, `r` to refresh, and `q` to
exit. Meaning remains visible with `NO_COLOR`; set `PITCREW_ASCII=1` for ASCII
glyphs (`TERM=dumb` enables both monochrome and ASCII fallbacks).

## Rigor that matches the work

| Route | Use it when | What it adds |
|---|---|---|
| **Direct** | The change is small, understood, and low risk. | Implementation and ordinary repository verification, without ceremonial workflow artifacts. |
| **Delegated direct** | The change is straightforward but benefits from a bounded specialist hand-off. | Delegated implementation and an appropriately scoped review. |
| **Full workflow** | Requirements, architecture, persistence, security, irreversibility, or uncertainty need durable reasoning. | Exploration, specification, design, planning, evidence-backed implementation, and independent aggregate review. |

The full route is deliberately explicit, but it is not a badge of quality for
work that does not need it. PitCrew's governing question is always whether a
less demanding route would satisfy the expected outcome equally well.

## Local and inspectable by design

PitCrew has a deliberately narrow boundary:

- **Stable project identity:** `project-id` is the lowercase SHA-256 of the
  canonical Git common-directory path. Main and linked worktrees share it; an
  independent clone or a moved common directory receives a different project ID.
- **Central private state:** state lives at
  `${XDG_DATA_HOME:-$HOME/.local/share}/pitcrew/projects/<project-id>/state.db`.
  The same `0700` project root owns `worktrees/` and `handles/`; database and
  identity files are `0600`. There is no cross-project registry or cache.
- **No service layer:** no HTTP, RPC, daemon, polling loop, or remote API is
  involved.
- **Read-only TUI:** `pitcrew tui` resolves and reads the central database in the current
  process. It does not initialize a project, mutate a workflow, run migrations,
  or invoke another executable.
- **Opaque authority:** implementation and review claims cross role boundaries
  through opaque handle files, never exposed plain tokens.
- **Truthful concurrency:** mutations are revision checked. After a revision
  conflict, inspect current state instead of retrying blindly.
- **Bounded inputs:** durable artifacts use strict JSON input files rather than
  large command-line payloads.

## Existing checkout-local history

Run `pitcrew project inspect` first. It is read-only and reports the resolved
identity, central paths, initialization state, and the exact bounded set of
legacy checkout-local databases. If candidates exist, create the strict
manifest described in the [CLI reference](docs/cli-reference.md#project-inspection-and-consolidation),
then run `pitcrew project consolidate --input-file <path>`. Consolidation copies
complete workflow graphs atomically; source databases and WAL files remain in
place for recovery.

For exact flags, payload schemas, exit behavior, lifecycle rules, and TUI keys,
use the [CLI reference](docs/cli-reference.md).

## Documentation map

Start here, then follow the document that matches the question:

| Document | Read it when... |
|---|---|
| [MAXIMS.md](MAXIMS.md) | You need the principles that govern every operator, agent, and design decision. Read this before changing PitCrew behavior. |
| [AGENTS.md](AGENTS.md) | You are an agent working in this repository and need the role contract, routing rules, or hand-off boundaries. |
| [docs/cli-reference.md](docs/cli-reference.md) | You need the exact public commands, flags, JSON payloads, response envelopes, exits, runtime installation contract, or TUI behavior. |
| [docs/contributing.md](docs/contributing.md) | You are changing the code and need the test loop, review limits, architecture guardrails, or installer/TUI verification expectations. |
| [openspec/specs/](openspec/specs/) | You are designing, implementing, or reviewing behavior and need the active normative requirements and scenarios. |

`README.md` is the orientation layer; it intentionally does not duplicate the
closed CLI contract or the active specifications.

## Contributing

Read [MAXIMS.md](MAXIMS.md), find the relevant requirement under
[openspec/specs/](openspec/specs/), and follow
[docs/contributing.md](docs/contributing.md). Keep changes local, bounded,
reviewable, and proven.
