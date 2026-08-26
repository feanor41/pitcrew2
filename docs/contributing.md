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

When changing `scripts/install-templates.sh`, prove all of these:

- every supported runtime installs eight role fragments (`daimon.md`, `aion.md`, and six specialists) plus `agent-contract.md`;
- every role contains the exact filesystem `MAXIMS.md` bytes and hand-off reminder;
- a byte-identical reinstall is a no-op;
- an existing `master.md`, customized `daimon.md`, or differing `aion.md` is protected unless `--overwrite` is explicit;
- a partial failure restores every touched file;
- unsupported runtime detection names Codex, OpenCode, Claude Code, and Pi.

Run `sh scripts/tests/run.sh` with `/bin/sh`, not Bash. If `shellcheck` is available, run it with shell dialect `sh` and resolve applicable findings.

Before an overwrite migration, preserve any custom instructions needed from legacy `master.md`, customized `daimon.md`, or a pre-existing `aion.md`. Any differing managed prompt is refused without `--overwrite`; explicit overwrite uses the same transactional rollback and arbitrary customization is not translated automatically.

## Scope changes

Proposals involving HTTP, RPC, authentication, multi-user state, cross-project coordination, TUI mutation or a separate TUI process, v1 migration, `internal/aion`, `internal/daimon`, or `internal/installer` require a separate explicit OpenSpec change. Aion and Daimon are external agent roles, not daemons or control-plane packages. Concurrent Daimon availability depends on host support for addressable agents; do not smuggle a service, IPC, polling, durable inbox, or new database state into an implementation patch.

For TUI changes, use direct `Model.Update` tests and deterministic wide/narrow goldens. Then build `cmd/pitcrew` and exercise wide, minimum, Arrow/Vim, activity drill-down, and `q` through a real PTY while snapshotting the database. The harness must observe the shared current version, preserve exact focus across resize, exit successfully, and leave the snapshot unchanged. Routing tests still require exact `tui` dispatch, rejected extras, and an injected subprocess trap.
