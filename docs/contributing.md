# Contributing to PitCrew

Keep every change local, reviewable, and proven. PitCrew is a personal subprocess harness; expanding it into a service is out of scope.

## Quick verification

Run these checks before requesting review:

```sh
sh scripts/tests/run.sh
go test ./...
go build ./...
go vet ./...
gofmt -w $(find cmd internal -name '*.go' -type f) maxims_embed.go
git diff --check
```

The shell suite uses temporary homes and must not modify your real runtime configuration.

## Development loop

1. Read the relevant OpenSpec requirement and scenario.
2. Write the smallest failing behavioral test.
3. Implement only enough production behavior to pass.
4. Add a different case that triangulates the behavior.
5. Run the focused test, runtime harness, and full validation.
6. Keep tests and user-facing documentation with the behavior they prove.

Use Go's standard test library. Filesystem tests use temporary directories; SQLite integration tests use temporary project roots. Do not depend on a real home directory or external network service.

## Review boundaries

A work unit targets at most 400 authored changed lines and 60 review minutes. When it grows beyond that boundary, split by autonomous behavior rather than by file type. Each slice must include its tests, runtime evidence, and rollback boundary.

## Architecture guardrails

Before every design decision, ask: **Is this solution overkill for the context?** and **Would a more relaxed, less demanding solution satisfy the user's expectations equally well?** Choose the least demanding solution that
fully satisfies the goal, material risks, and existing constraints. Only when
selecting stronger rigor, briefly name the protected constraint and
explain why the simpler option is insufficient in the design-bearing output.
Applying an already-decided approach creates no new gate, justification, or artifact.

- One local project store at `.pitcrew/state.db` per invocation.
- One SQLite connection; no daemon, IPC, network, shared cache, or remote API.
- `MAXIMS.md` is canonical. Change it only through its own OpenSpec change.
- The command surface is closed. Adding a command or flag requires an OpenSpec change.
- `pitcrew tui` is the only visual entry point. Keep it same-process, project-local, read-only, non-initializing, and free of self-subprocesses.
- Large command payloads travel only through strict, no-follow `--input-file` JSON.
- Production claims remain opaque. Never add a raw-token input or output path.
- Implementer and Reviewer actor labels remain distinct collision metadata, not authentication.
- The installer remains POSIX `/bin/sh`; do not add bash arrays, `[[ ... ]]`, process substitution, or shell-specific options.

## Installer changes

When changing the public runtime installer or `scripts/install-templates.sh`, prove all of these:

- each exact `pitcrew install codex|opencode|claude|pi` selector installs only its selected runtime, even when all other homes and override variables exist;
- every supported runtime installs exactly eight native agents plus `pitcrew/agent-contract.md` outside agent discovery: Codex: `agents/*.toml` with underscore native identities; OpenCode: `agents/*.md`; Claude Code: `agents/*.md`; Pi: `agents/*.md`;
- every Aion definition resolves exactly the six native specialist identities, while Daimon can hand off only to Aion and specialists cannot delegate;
- every role contains the exact canonical `MAXIMS.md` bytes and hand-off reminder;
- a byte-identical reinstall is a no-op;
- the public command warns before transactionally refreshing only differing current and legacy PitCrew-managed filenames, and preserves unrelated files and application configuration;
- direct script invocation protects differing managed files unless `--overwrite` is explicit;
- a partial failure restores every touched file and removes installer-created directories and temporary files;
- a built binary installs from embedded assets outside the checkout and leaves no extraction residue;
- unsupported direct-script runtime detection names Codex, OpenCode, Claude Code, and Pi.

Pi installation additionally requires Node.js, an installed and active `pi-subagents` version 0.25.0 or newer, and an integer `maxSubagentDepth` of at least 3 in `<pi-agent-home>/extensions/subagent/config.json`. Depth three permits exactly the host-to-Daimon, Daimon-to-Aion, and Aion-to-specialist launches while specialist definitions omit `subagent`. Exact `npm:pi-subagents` identity may include a non-empty version/range suffix; near-name packages and missing, malformed, or insufficient depth configuration must fail before mutation. The installer never installs the extension, accesses the network, or modifies Pi configuration. The shell suite exercises all four public commands with isolated native schemas, identity sets, dispatch graphs, managed refresh, unrelated-file preservation, legacy migration, rollback, cleanup, and byte-stable reinstalls. When the OpenCode CLI is available it also compares `opencode --pure agent list` with the expected registry. Codex, Claude Code, and Pi have no stable offline invocation command, so offline schema and dispatch-graph validation proves discovery eligibility, not model execution.

OpenCode installation additionally requires OpenCode 1.18.23 or newer and an
effective top-level integer `subagent_depth` of at least 2 in the target
project. Verify the resolved value from that project with:

```sh
opencode --pure debug config
```

Upgrade older OpenCode installations to at least 1.18.23. Set
`"subagent_depth": 2` in global configuration unless higher-precedence project
configuration overrides it, then rerun `pitcrew install opencode` as printed by
the failure. Malformed or incompatible resolved output must be fixed in the
configuration reported by the verification command. The installer validates
this prerequisite before any target write and never rewrites user JSON or
JSONC. Depth two preserves the existing bounded topology: Daimon can call only
Aion, Aion can call only the six specialists, and specialists cannot delegate.

The real nested-runtime probe is isolated, opt-in, and may consume provider
tokens. It reports `SKIP`, never `PASS`, when it is not enabled or lacks the CLI
or credentials:

```sh
PITCREW_OPENCODE_DEPTH_PROBE=1 sh scripts/tests/opencode-depth-runtime.sh
```

The probe copies `~/.local/share/opencode/auth.json` into a temporary data home.
Set `PITCREW_OPENCODE_AUTH_FILE` for a different credential file,
`PITCREW_OPENCODE_DEPTH_ENV_CREDENTIALS=1` when the selected provider uses
environment credentials, and `PITCREW_OPENCODE_DEPTH_MODEL` to override the
default `openai/gpt-5.6-sol` model. It proves both the default depth-one failure
and global depth-two success without reading or changing real OpenCode config.

Run `sh scripts/tests/run.sh` with `/bin/sh`, not Bash. The suite builds a standalone binary and exercises the four public selectors, prerequisite failures, managed updates, idempotency, rollback, signals, and cleanup. If `shellcheck` is available, run it with shell dialect `sh` and resolve applicable findings.

Before a managed refresh or direct overwrite migration, preserve any custom instructions needed from legacy `master.md`, customized `daimon.md`, or a pre-existing `aion.md` outside managed role filenames. Public installation authorizes bounded refresh and emits a warning; direct invocation refuses differing managed definitions without `--overwrite`. Both use the same transactional rollback, and arbitrary customization is not translated automatically.

## Scope changes

Proposals involving HTTP, RPC, authentication, multi-user state, cross-project coordination, TUI mutation or a separate TUI process, v1 migration, `internal/aion`, `internal/daimon`, or `internal/installer` require a separate explicit OpenSpec change. Aion and Daimon are external agent roles, not daemons or control-plane packages. Concurrent Daimon availability depends on host support for addressable agents; do not smuggle a service, IPC, polling, durable inbox, or new database state into an implementation patch.

For TUI changes, use direct `Model.Update` tests and deterministic wide/narrow goldens. Then build `cmd/pitcrew` and exercise wide, minimum, Arrow/Vim, activity drill-down, and `q` through a real PTY while snapshotting the database. The harness must observe the shared current version, preserve exact focus across resize, exit successfully, and leave the snapshot unchanged. Routing tests still require exact `tui` dispatch, rejected extras, and an injected subprocess trap.
