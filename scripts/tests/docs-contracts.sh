#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$root"
require() {
  file=$1
  text=$2
  grep -Fq -- "$text" "$file" || {
    printf 'missing docs contract in %s: %s\n' "$file" "$text" >&2
    exit 1
  }
}
expected=$(mktemp "${TMPDIR:-/tmp}/pitcrew-docs-agents.XXXXXX")
trap 'rm -f "$expected"' EXIT HUP INT TERM
printf '%s\n' 'Work in this repository is performed by interacting directly with the `daimon` agent by default.' >"$expected"
cmp -s "$expected" AGENTS.md || {
  echo 'AGENTS.md must contain only the one-line Daimon selector' >&2
  exit 1
}
require README.md '${XDG_DATA_HOME:-$HOME/.local/share}/pitcrew/projects/<project-id>/state.db'
require README.md 'independent clone or a moved common directory receives a different project ID'
require docs/cli-reference.md 'pitcrew project inspect'
require docs/cli-reference.md 'pitcrew project consolidate --input-file <path>'
require docs/cli-reference.md 'Source databases and WAL files are never deleted or rewritten.'
require docs/cli-reference.md 'pitcrew agent brief'
require docs/cli-reference.md 'read-only, versioned source'
require docs/cli-reference.md 'contract_version'
require docs/cli-reference.md 'contract_digest'
require docs/contributing.md 'nine minimal native role bootstraps'
require docs/contributing.md 'role-scoped `pitcrew agent brief` before action'
require docs/contributing.md 'recognized prior managed `pitcrew/agent-contract.md`'
require openspec/AGENTS.md '<data-home>/pitcrew/projects/<project-id>/state.db'
require openspec/specs/event-store/spec.md 'SHA-256 of the canonical Git common-directory path'
require openspec/specs/tui-inspection/spec.md 'central state database read-only'
require openspec/specs/cli-surface/spec.md '`project inspect`'
require openspec/specs/cli-surface/spec.md '`project consolidate`'
require openspec/specs/claim-handles/spec.md '<data-home>/pitcrew/projects/<project-id>/handles/'
require openspec/specs/runtime-install/spec.md 'Minimal executable role bootstraps'
require openspec/specs/runtime-install/spec.md 'SHALL NOT create `pitcrew/agent-contract.md`'
require openspec/specs/runtime-install/spec.md 'recognized checksum'
require openspec/specs/runtime-install/spec.md 'Modified, non-regular, and unrelated files'
require docs/todo.md 'GitHub Issues is the source of truth for PitCrew backlog work:'
require docs/todo.md 'Do not add new unchecked work to this file.'
if grep -Fq 'current project owns `.pitcrew/state.db`' README.md; then
  echo 'README still claims checkout-local workflow ownership' >&2
  exit 1
fi
printf 'documentation contracts: ok\n'
