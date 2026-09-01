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
require docs/cli-reference.md '<!-- cli-docs:navigation:start -->'
require docs/cli-reference.md '<a id="command-catalog"></a>'
require docs/cli-reference.md '<!-- cli-docs:diagram:control-plane-calls:start -->'
require docs/cli-reference.md '<!-- cli-docs:diagram:admission-routing:start -->'
require docs/cli-reference.md '<!-- cli-docs:diagram:direct-delivery:start -->'
require docs/cli-reference.md '<!-- cli-docs:diagram:aggregate:start -->'
require docs/cli-reference.md '<!-- cli-docs:diagram:unit-authority:start -->'
require docs/cli-reference.md 'Identical replay returns the same delivery identity'
require docs/cli-reference.md 'in_progress --> blocked: observed blocker'
require docs/cli-reference.md 'blocked --> interrupted: observed interruption'
require docs/cli-reference.md 'interrupted --> blocked: observed blocker'
require docs/cli-reference.md 'blocked --> cancelled: observed cancellation'
require docs/cli-reference.md 'blocked --> failed: observed failure'
require docs/cli-reference.md 'interrupted --> cancelled: observed cancellation'
require docs/cli-reference.md 'interrupted --> failed: observed failure'
require docs/cli-reference.md 'A direct capability gap'
require docs/cli-reference.md 'never creates a separate persisted state'
require docs/cli-reference.md 'independent revision-1 draft child with pinned normative lineage'
require docs/cli-reference.md 'ready_to_complete --> ready_to_complete: complete corrections; persist blocker only'
require docs/cli-reference.md 'ready_to_complete --> ready_to_complete: authorize-correction; authority only'
require docs/cli-reference.md 'ready_to_complete --> implementing: recover-aggregate; reopen units at next revisions'
require docs/cli-reference.md 'A corrections verdict persists only the'
require docs/cli-reference.md '`recover-aggregate` advances reopened units to their next revisions'
require docs/cli-reference.md 'workflow continue --from; predecessor immutable'
require docs/cli-reference.md 'The last unit atomically advances the workflow to `ready_to_complete`'
require docs/cli-reference.md 'Every Control Plane request is a fresh local `pitcrew` subprocess'
require docs/cli-reference.md 'never calls a model or agent'
require docs/cli-reference.md 'No role reads or writes `state.db` directly'
require docs/cli-reference.md '<!-- cli-docs:profiles:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:agent:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:agent-brief:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:global-help:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:project-inspect:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:context-record:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:delivery-active:start -->'
require docs/cli-reference.md '<!-- cli-docs:aggregate-workflow-profiles:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:workflow-new:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:workflow-complete:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:workflow-authorize-correction:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:workflow-abandon:start -->'
require docs/cli-reference.md '<!-- cli-docs:work-unit-profiles:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:workflow-claim-unit:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:workflow-recover-aggregate:start -->'
require docs/cli-reference.md '<!-- cli-docs:profile:workflow-unit-complete:start -->'
require docs/cli-reference.md 'nine minimal native role bootstraps'
require docs/cli-reference.md 'exploring --> designing: design shortcut'
require docs/cli-reference.md 'Every non-terminal aggregate state can transition to `abandoned`'
require docs/cli-reference.md 'Persisted: reviewing'
require docs/cli-reference.md 'Effective handle/activity status is projected separately'
require docs/cli-reference.md 'pitcrew project consolidate --input-file <path>'
require docs/cli-reference.md '"candidate_ids"'
require docs/cli-reference.md 'Source databases and WAL files are never deleted or rewritten.'
require docs/cli-reference.md 'pitcrew agent brief'
require docs/cli-reference.md 'read-only, versioned source'
require docs/cli-reference.md 'contract_version'
require docs/cli-reference.md 'contract_digest'
require docs/cli-reference.md 'Every response is one composite brief in fixed order'
require docs/cli-reference.md 'shared_contract.contract_version'
require docs/cli-reference.md 'shared_contract.contract_digest'
require docs/cli-reference.md 'complete canonical embedded'
require docs/cli-reference.md 'shared_maxims_begin'
require docs/cli-reference.md 'no standalone `MAXIMS.md` is deployed to the runtime'
require docs/contributing.md 'nine minimal native role bootstraps'
require docs/contributing.md 'role-scoped `pitcrew agent brief` before action'
require docs/contributing.md 'recognized prior managed `pitcrew/agent-contract.md`'
require docs/contributing.md 'File count is evidence, not a classifier'
require openspec/AGENTS.md 'count is evidence, never a classifier'
require openspec/AGENTS.md 'larger mechanical,'
require openspec/AGENTS.md 'Stronger routing names the protected'
require openspec/AGENTS.md '<data-home>/pitcrew/projects/<project-id>/state.db'
require openspec/specs/event-store/spec.md 'SHA-256 of the canonical Git common-directory path'
require openspec/specs/tui-inspection/spec.md 'central state database read-only'
require openspec/specs/cli-surface/spec.md '`project inspect`'
require openspec/specs/cli-surface/spec.md '`project consolidate`'
require openspec/specs/claim-handles/spec.md '<data-home>/pitcrew/projects/<project-id>/handles/'
require openspec/specs/tdd-and-review/spec.md 'Equal file counts permit different routes'
require openspec/specs/tdd-and-review/spec.md 'Larger mechanical work remains direct'
require openspec/specs/tdd-and-review/spec.md 'Bounded handoff materially helps'
require openspec/specs/runtime-install/spec.md 'Minimal executable role bootstraps'
require openspec/specs/runtime-install/spec.md 'SHALL NOT create `pitcrew/agent-contract.md`'
require openspec/specs/runtime-install/spec.md 'one composite brief ordered as shared operating contract'
require openspec/specs/runtime-install/spec.md 'SHALL NOT extract or deploy a standalone `MAXIMS.md`'
require openspec/specs/runtime-install/spec.md 'recognized checksum'
require openspec/specs/runtime-install/spec.md 'Modified, non-regular, and unrelated files'
require docs/todo.md 'GitHub Issues is the source of truth for PitCrew backlog work:'
require docs/todo.md 'Do not add new unchecked work to this file.'
if grep -Fq 'current project owns `.pitcrew/state.db`' README.md; then
  echo 'README still claims checkout-local workflow ownership' >&2
  exit 1
fi

if grep -Fq 'do not skip or reverse stages' docs/cli-reference.md; then
  echo 'CLI reference contradicts the exploring-to-designing shortcut' >&2
  exit 1
fi

if grep -Eq 'eight native agents|nine native agents plus `pitcrew/agent-contract.md`' docs/cli-reference.md; then
  echo 'CLI reference retains a stale native-agent installation contract' >&2
  exit 1
fi

if grep -Eq 'capability_blocker|aggregate_verdict|correction_blocker|correction_authority|awaiting_user|Every profile uses the same four fields\.' docs/cli-reference.md; then
  echo 'CLI reference contains a fictitious aggregate state or stale profile schema' >&2
  exit 1
fi

if grep -Fq 'A corrections verdict itself persists a new pending revision while the aggregate' docs/cli-reference.md; then
  echo 'CLI reference attributes aggregate recovery revision changes to the verdict' >&2
  exit 1
fi

if grep -Eq 'at most three files|four or more files' openspec/AGENTS.md openspec/specs/tdd-and-review/spec.md; then
  echo 'normative routing still contains deterministic file-count thresholds' >&2
  exit 1
fi

workflow_matrix_count=$(grep -c '^| `workflow [a-z-]*` |' docs/cli-reference.md)
workflow_profile_count=$(grep -c '^<!-- cli-docs:profile:workflow-[a-z-]*:start -->$' docs/cli-reference.md)
workflow_anchor_count=$(grep -c '^<a id="workflow-[a-z-]*"></a>$' docs/cli-reference.md || true)
test "$workflow_matrix_count" -eq 24 && test "$workflow_profile_count" -eq 24 && test "$workflow_anchor_count" -eq 24 || {
  printf 'workflow docs inventory mismatch: matrix=%s profiles=%s anchors=%s expected=24\n' "$workflow_matrix_count" "$workflow_profile_count" "$workflow_anchor_count" >&2
  exit 1
}

if grep -Eq -- '--claim-token|--emit-plain-token|/handles/[0-9a-f]{16,}\.json' docs/cli-reference.md; then
  echo 'CLI reference exposes a prohibited token or concrete handle path' >&2
  exit 1
fi

mermaid_count=$(grep -c '^```mermaid$' docs/cli-reference.md)
test "$mermaid_count" -eq 5 || {
  printf 'docs/cli-reference.md has %s Mermaid diagrams; expected 5\n' "$mermaid_count" >&2
  exit 1
}

printf 'documentation contracts: ok\n'
