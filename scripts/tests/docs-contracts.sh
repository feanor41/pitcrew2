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

require README.md '${XDG_DATA_HOME:-$HOME/.local/share}/pitcrew/projects/<project-id>/state.db'
require README.md 'independent clone or a moved common directory receives a different project ID'
require AGENTS.md 'canonical Git common directory'
require AGENTS.md 'Never retry a CAS failure blindly'
require docs/cli-reference.md 'pitcrew project inspect'
require docs/cli-reference.md 'pitcrew project consolidate --input-file <path>'
require docs/cli-reference.md '"candidate_ids"'
require docs/cli-reference.md 'Source databases and WAL files are never deleted or rewritten.'
require docs/contributing.md 'committed checkpoint exists before worktree cleanup'
require openspec/AGENTS.md '<data-home>/pitcrew/projects/<project-id>/state.db'
require openspec/specs/event-store/spec.md 'SHA-256 of the canonical Git common-directory path'
require openspec/specs/tui-inspection/spec.md 'central state database read-only'
require openspec/specs/cli-surface/spec.md '`project inspect`'
require openspec/specs/cli-surface/spec.md '`project consolidate`'
require openspec/specs/claim-handles/spec.md '<data-home>/pitcrew/projects/<project-id>/handles/'

if grep -Fq 'current project owns `.pitcrew/state.db`' README.md; then
  echo 'README still claims checkout-local workflow ownership' >&2
  exit 1
fi

printf 'documentation contracts: ok\n'
