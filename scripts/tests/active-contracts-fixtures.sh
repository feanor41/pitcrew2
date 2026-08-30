#!/bin/sh

set -eu

source_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixture_root=$(mktemp -d "${TMPDIR:-/tmp}/pitcrew-active-contracts.XXXXXX")
trap 'rm -rf "$fixture_root"' EXIT HUP INT TERM

mkdir -p "$fixture_root/scripts/tests" "$fixture_root/docs" \
  "$fixture_root/openspec/changes/archive" \
  "$fixture_root/openspec/specs/cli-surface" \
  "$fixture_root/openspec/specs/runtime-install"
cp "$source_root/scripts/tests/active-contracts.sh" "$fixture_root/scripts/tests/"

printf '%s\n' \
  'Daimon interviews the user and communicates Aion-acknowledged facts.' \
  'before repository mutation' \
  'retain the stable operation key until start acknowledgement' \
  'replay the identical start after a lost response' \
  'inspect and resume the same delivery identity' \
  'one delivery identity, not one fallible invocation' \
  'retain the delivery ID and current revision' \
  'meaningful observed fact' \
  'last observed status' \
  'MUST NOT create a direct delivery trace' \
  'existing project-context deployment facts as the release map' \
  'before any repository, binary, backup, runtime, or publication mutation' \
  'canonical repository and version source' \
  'exact validation commands' \
  'binary build command and install target' \
  'persistent rollback procedure' \
  'supported runtime set' \
  'detected runtime subset selected for refresh' \
  'each selected runtime exact installer-managed file set and deterministic expected digest evidence' \
  'Repair missing or inadequate release facts with one bounded context record replacement while preserving unrelated facts' \
  'Mechanical release execution remains direct inline regardless of mapped file count' \
  'same-version digest mismatch is not convergence' \
  'resume the same identity from observed physical state' \
  'Publish only when the accepted release map selects publication' \
  'release engine, command, schema, parallel status, daemon, polling, or IPC' \
  'first admission gate' \
  'acknowledged before any repository mutation' \
  'stop before mutation and surface the capability boundary' \
  'never backfill a trace after work has started' \
  'does not interpose on or prevent host filesystem writes' \
  'transcript-free composition' \
  'workflow ID and current revision' \
  'role or unit ID' \
  'applicable opaque handle path' \
  'workflow show --view coordination' \
  'workflow show --view phase' \
  'workflow show --view unit --unit-id' \
  'workflow show --view aggregate' \
  'never simulate it by replaying conversation history or transcript content' \
  'inspects project context once on demand' \
  'exactly one `pc2-sdd-initializer` attempt' \
  'bypasses it for `complete`' \
  'never schedules recurring scans' \
  'exactly the seven specialists' \
  'Daimon targets only Aion' \
  'specialists never delegate' \
  '`context inspect`, `context initialize`, `context record`' \
  >"$fixture_root/AGENTS.md"
mkdir -p "$fixture_root/openspec"
printf '%s\n' 'Daimon preserves conversational continuity.' >"$fixture_root/openspec/AGENTS.md"
printf '%s\n' 'Daimon documentation' 'pitcrew workflow show --actor master' \
  >"$fixture_root/docs/guide.md"
printf '%s\n' 'Aion canonical specification' 'Daimon SHALL NOT invoke workflow commands and receives only Aion-acknowledged facts or clarification requests.' \
  >"$fixture_root/openspec/specs/cli-surface/spec.md"
printf '%s\n' 'active user-visible turn' 'host-native dual wait/select' 'requested state' \
  'terminal completion, a genuine blocker, or user cancellation' \
  'exactly one request-capability' 'polling, daemon, IPC, or durable inbox' \
  'before repository mutation' \
  'retain the stable operation key until start acknowledgement' \
  'replay the identical start after a lost response' \
  'inspect and resume the same delivery identity' \
  'one delivery identity, not one fallible invocation' \
  'retain the delivery ID and current revision' \
  'meaningful observed fact' \
  'last observed status' \
  'MUST NOT create a direct delivery trace' \
  'first admission gate' \
  'acknowledged before any repository mutation' \
  'stop before mutation and surface the capability boundary' \
  'never backfill a trace after work has started' \
  'does not interpose on or prevent host filesystem writes' \
  'transcript-free composition' \
  'workflow ID and current revision' \
  'role or unit ID' \
  'applicable opaque handle path' \
  'workflow show --view coordination' \
  'workflow show --view phase' \
  'workflow show --view unit --unit-id' \
  'workflow show --view aggregate' \
  'never simulate it by replaying conversation history or transcript content' \
  'exactly all nine native definitions' \
  'target exactly the seven specialists' \
  'inspect project context once on demand' \
  'exactly one `pc2-sdd-initializer` attempt' \
  'bypass initialization when context is `complete`' \
  'never schedule recurring context scans' \
  'pitcrew context inspect' \
  'pitcrew context initialize' \
  'pitcrew context record' \
  >"$fixture_root/openspec/specs/runtime-install/spec.md"
printf '%s\n' 'immutable archive' \
  >"$fixture_root/openspec/changes/archive/spec.md"

append_agent_continuity() {
  printf '%s\n' \
    '`delivery active`' \
    'zero active candidates' \
    'exactly one active candidate' \
    'more than one active candidate' \
    'one identity-specific inspection' \
    'same delivery identity and current revision' \
    'does not select by recency, display order, route, goal similarity, or status' \
    'routine projected `next_action`' \
    'stable semantic key' \
    'unit identity, attempt, and outcome' \
    'current actionable or terminal fact' \
    'does not replay historical progress' \
    >>"$fixture_root/AGENTS.md"
}

append_cli_continuity() {
  printf '%s\n' \
    '| `delivery active` | none' \
    'zero active candidates' \
    'exactly one active candidate' \
    'multiple active candidates' \
    'one identity-specific inspection' \
    'SHALL NOT select by recency, ordering, route, goal similarity, or status' \
    >>"$fixture_root/openspec/specs/cli-surface/spec.md"
}

append_agent_continuity
append_cli_continuity
printf '%s\n' \
  'unchanged capability requirement' \
  'SHALL NOT append a duplicate request' \
  'direct-only delivery has no supported durable capability-request surface' \
  'SHALL NOT invent a workflow or parallel lifecycle' \
  >>"$fixture_root/openspec/specs/runtime-install/spec.md"
printf '%s\n' \
  '| `delivery active` | None' \
  'aion admit new delivery' \
  'delivery show --delivery-id <id>' \
  'aion clarify delivery identity' \
  'Direct-only capability gaps' \
  >"$fixture_root/docs/cli-reference.md"
printf '%s\n' \
  'Run `delivery active` before admitting new work' \
  'stable semantic key' \
  'unit identity, attempt, and outcome' \
  'do not replay historical progress' \
  'direct-only delivery has no supported durable capability-request surface' \
  'existing project-context deployment facts as the release map' \
  'before any repository, binary, backup, runtime, or publication mutation' \
  'canonical repository and version source' \
  'exact validation commands' \
  'binary build command and install target' \
  'persistent rollback procedure' \
  'supported runtime set' \
  'detected runtime subset selected for refresh' \
  'each selected runtime exact installer-managed file set and deterministic expected digest evidence' \
  'Repair missing or inadequate release facts with one bounded context record replacement while preserving unrelated facts' \
  'Mechanical release execution remains direct inline regardless of mapped file count' \
  'same-version digest mismatch is not convergence' \
  'resume the same identity from observed physical state' \
  'Publish only when the accepted release map selects publication' \
  'release engine, command, schema, parallel status, daemon, polling, or IPC' \
  >"$fixture_root/scripts/install-templates.sh"

if ! sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "clean active contracts were rejected" >&2
  exit 1
fi

grep -Fv 'pitcrew context initialize' "$fixture_root/openspec/specs/runtime-install/spec.md" >"$fixture_root/runtime.tmp"
mv "$fixture_root/runtime.tmp" "$fixture_root/openspec/specs/runtime-install/spec.md"
if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "missing initializer command contract escaped validation" >&2
  exit 1
fi
printf '%s\n' 'pitcrew context initialize' >>"$fixture_root/openspec/specs/runtime-install/spec.md"

grep -Fv 'host-native dual wait/select' "$fixture_root/openspec/specs/runtime-install/spec.md" >"$fixture_root/runtime.tmp"
mv "$fixture_root/runtime.tmp" "$fixture_root/openspec/specs/runtime-install/spec.md"
if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "missing live-turn contract escaped validation" >&2
  exit 1
fi
printf '%s\n' 'host-native dual wait/select' >>"$fixture_root/openspec/specs/runtime-install/spec.md"

for required_release_fact in \
  'canonical repository and version source' \
  'exact validation commands' \
  'binary build command and install target' \
  'persistent rollback procedure' \
  'supported runtime set' \
  'detected runtime subset selected for refresh' \
  'each selected runtime exact installer-managed file set and deterministic expected digest evidence'; do
  cp "$fixture_root/scripts/install-templates.sh" "$fixture_root/installer.saved"
  grep -Fv "$required_release_fact" "$fixture_root/installer.saved" >"$fixture_root/scripts/install-templates.sh"
  if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
    echo "missing generated release fact escaped validation: $required_release_fact" >&2
    exit 1
  fi
  mv "$fixture_root/installer.saved" "$fixture_root/scripts/install-templates.sh"
done

printf '%s\n' 'Forbidden Master canonical specification' >"$fixture_root/openspec/specs/cli-surface/spec.md"
if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "active canonical specification escaped validation" >&2
  exit 1
fi
printf '%s\n' 'Aion canonical specification' 'Daimon SHALL NOT invoke workflow commands and receives only Aion-acknowledged facts or clarification requests.' >"$fixture_root/openspec/specs/cli-surface/spec.md"
append_cli_continuity

printf '%s\n' 'Daimon coordinates workflow recovery.' >"$fixture_root/AGENTS.md"
if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "forbidden Daimon orchestration grant was not rejected" >&2
  exit 1
fi
printf '%s\n' \
  'Daimon interviews the user and communicates Aion-acknowledged facts.' \
  'before repository mutation' \
  'retain the stable operation key until start acknowledgement' \
  'replay the identical start after a lost response' \
  'inspect and resume the same delivery identity' \
  'one delivery identity, not one fallible invocation' \
  'retain the delivery ID and current revision' \
  'meaningful observed fact' \
  'last observed status' \
  'MUST NOT create a direct delivery trace' \
  'existing project-context deployment facts as the release map' \
  'before any repository, binary, backup, runtime, or publication mutation' \
  'canonical repository and version source' \
  'exact validation commands' \
  'binary build command and install target' \
  'persistent rollback procedure' \
  'supported runtime set' \
  'detected runtime subset selected for refresh' \
  'each selected runtime exact installer-managed file set and deterministic expected digest evidence' \
  'Repair missing or inadequate release facts with one bounded context record replacement while preserving unrelated facts' \
  'Mechanical release execution remains direct inline regardless of mapped file count' \
  'same-version digest mismatch is not convergence' \
  'resume the same identity from observed physical state' \
  'Publish only when the accepted release map selects publication' \
  'release engine, command, schema, parallel status, daemon, polling, or IPC' \
  'first admission gate' \
  'acknowledged before any repository mutation' \
  'stop before mutation and surface the capability boundary' \
  'never backfill a trace after work has started' \
  'does not interpose on or prevent host filesystem writes' \
  'transcript-free composition' \
  'workflow ID and current revision' \
  'role or unit ID' \
  'applicable opaque handle path' \
  'workflow show --view coordination' \
  'workflow show --view phase' \
  'workflow show --view unit --unit-id' \
  'workflow show --view aggregate' \
  'never simulate it by replaying conversation history or transcript content' \
  'inspects project context once on demand' \
  'exactly one `pc2-sdd-initializer` attempt' \
  'bypasses it for `complete`' \
  'never schedules recurring scans' \
  'exactly the seven specialists' \
  'Daimon targets only Aion' \
  'specialists never delegate' \
  '`context inspect`, `context initialize`, `context record`' \
  >"$fixture_root/AGENTS.md"
append_agent_continuity

printf '%s\n' 'Forbidden Master documentation' >"$fixture_root/docs/guide.md"
if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "forbidden documentation vocabulary was not rejected" >&2
  exit 1
fi

echo "active contract fixtures: passed"
