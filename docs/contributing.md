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

- One local project store at `.pitcrew/state.db` per invocation.
- One SQLite connection; no daemon, IPC, network, shared cache, or remote API.
- `MAXIMS.md` is canonical. Change it only through its own OpenSpec change.
- The command surface is closed. Adding a command or flag requires an OpenSpec change.
- Large command payloads travel only through strict, no-follow `--input-file` JSON.
- Production claims remain opaque. Never add a raw-token input or output path.
- Implementer and Reviewer actor labels remain distinct collision metadata, not authentication.
- The installer remains POSIX `/bin/sh`; do not add bash arrays, `[[ ... ]]`, process substitution, or shell-specific options.

## Installer changes

When changing `scripts/install-templates.sh`, prove all of these:

- every supported runtime installs eight role fragments plus `agent-contract.md`;
- every role contains the exact filesystem `MAXIMS.md` bytes and hand-off reminder;
- a byte-identical reinstall is a no-op;
- an existing `master.md` or customized `daimon.md` is protected unless `--overwrite` is explicit;
- a partial failure restores every touched file;
- unsupported runtime detection names Codex, OpenCode, Claude Code, and Pi.

Run `sh scripts/tests/run.sh` with `/bin/sh`, not Bash. If `shellcheck` is available, run it with shell dialect `sh` and resolve applicable findings.

Before an overwrite migration, preserve any custom instructions needed from legacy `master.md`. The installer warns before the explicit destructive cutover to canonical `daimon.md`; arbitrary customization is not translated automatically.

## Scope changes

Proposals involving HTTP, RPC, authentication, multi-user state, cross-project coordination, an embedded TUI, v1 migration, `internal/daimon`, or `internal/installer` require a separate explicit OpenSpec change. Daimon is an external agent role, not a Unix daemon or control-plane package. Do not smuggle architectural expansion into an implementation patch.
