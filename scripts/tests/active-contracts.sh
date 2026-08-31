#!/bin/sh
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
archive_root="$repo_root/openspec/changes/archive"
archive_before=$(mktemp "${TMPDIR:-/tmp}/pitcrew-archive-before.XXXXXX")
archive_after=$(mktemp "${TMPDIR:-/tmp}/pitcrew-archive-after.XXXXXX")
expected_agents=$(mktemp "${TMPDIR:-/tmp}/pitcrew-agents.XXXXXX")
trap 'rm -f "$archive_before" "$archive_after" "$expected_agents"' EXIT HUP INT TERM
archive_snapshot() {
  destination=$1
  find "$archive_root" -type f -print | LC_ALL=C sort |
    while IFS= read -r path; do sha256sum "$path"; done >"$destination"
}
archive_snapshot "$archive_before"
printf '%s\n' 'Work in this repository is performed by interacting directly with the `daimon` agent by default.' >"$expected_agents"
if ! cmp -s "$expected_agents" "$repo_root/AGENTS.md"; then
  echo 'AGENTS.md is not the exact one-line Daimon selector' >&2
  exit 1
fi
# The binary, rather than repository or generated prompt manuals, is the
# versioned source of role identity and current authority.
GOCACHE=${GOCACHE:-${TMPDIR:-/tmp}/pitcrew-active-contracts-gocache} \
  go test ./internal/agentbrief ./cmd/pitcrew \
  -run 'TestStableContractsCarryBootstrapMechanicsNotRuntimePrompts|TestScopedAgentBriefCommandsActivateAgainstAcceptedWorkflow' \
  -count=1
runtime_contract="$repo_root/openspec/specs/runtime-install/spec.md"
for required in \
  'pitcrew agent brief --role' \
  'contract_version' \
  'contract_digest' \
  'SHALL NOT create `pitcrew/agent-contract.md`' \
  'recognized checksum' \
  'Modified, non-regular, and unrelated files'; do
  grep -Fq "$required" "$runtime_contract" || {
    printf 'runtime installation contract omitted: %s\n' "$required" >&2
    exit 1
  }
done
# Public Go tests exercise native least privilege, the closed graph, and real
# scoped brief activation without duplicating their phrase inventory here.
GOCACHE=${GOCACHE:-${TMPDIR:-/tmp}/pitcrew-active-contracts-gocache} \
  go test ./internal/runtimeinstall ./cmd/pitcrew \
  -run 'TestRepositoryAgentGuideIsOnlyTheDaimonBootstrap|TestRuntimeInstall|TestScopedAgentBriefCommandsActivateAgainstAcceptedWorkflow' \
  -count=1
archive_snapshot "$archive_after"
if ! cmp -s "$archive_before" "$archive_after"; then
  echo 'archived OpenSpec content changed during active-contract validation' >&2
  exit 1
fi
echo 'active contracts: clean'
