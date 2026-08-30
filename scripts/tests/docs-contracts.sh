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
require docs/contributing.md '## Recoverable release operations'
require docs/contributing.md 'Project-context deployment facts are the release map'
require docs/contributing.md 'before any'
require docs/contributing.md 'release-target mutation'
require docs/contributing.md 'A matching version with a different digest is stale'
require docs/contributing.md 'GitHub publication is optional'
require docs/contributing.md 'canonical repository and version source'
require docs/contributing.md 'exact validation commands'
require docs/contributing.md 'binary build command and install target'
require docs/contributing.md 'persistent rollback procedure'
require docs/contributing.md 'supported runtime set'
require docs/contributing.md 'detected runtime subset selected for refresh'
require docs/contributing.md 'each selected runtime exact installer-managed file set and deterministic expected digest evidence'
require docs/contributing.md 'Do not add a release command, release schema, parallel status store, daemon, polling, or IPC.'
require openspec/AGENTS.md '<data-home>/pitcrew/projects/<project-id>/state.db'
require openspec/specs/event-store/spec.md 'SHA-256 of the canonical Git common-directory path'
require openspec/specs/tui-inspection/spec.md 'central state database read-only'
require openspec/specs/cli-surface/spec.md '`project inspect`'
require openspec/specs/cli-surface/spec.md '`project consolidate`'
require openspec/specs/claim-handles/spec.md '<data-home>/pitcrew/projects/<project-id>/handles/'
require openspec/specs/runtime-install/spec.md 'exactly all nine native definitions'
require openspec/specs/runtime-install/spec.md 'pc2-sdd-initializer'
require AGENTS.md 'Aion targets exactly the seven specialists'
require docs/todo.md 'GitHub Issues is the source of truth for PitCrew backlog work:'
require docs/todo.md 'https://github.com/feanor41/pitcrew2/issues/132'
require docs/todo.md 'https://github.com/feanor41/pitcrew2/issues/135'
require docs/todo.md 'Do not add new unchecked work to this file.'

if grep -Fq 'current project owns `.pitcrew/state.db`' README.md; then
  echo 'README still claims checkout-local workflow ownership' >&2
  exit 1
fi

printf 'documentation contracts: ok\n'
