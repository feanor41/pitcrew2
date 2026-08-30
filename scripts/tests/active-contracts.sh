#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
archive_root="$repo_root/openspec/changes/archive"
archive_before=$(mktemp "${TMPDIR:-/tmp}/pitcrew-archive-before.XXXXXX")
archive_after=$(mktemp "${TMPDIR:-/tmp}/pitcrew-archive-after.XXXXXX")
findings=$(mktemp "${TMPDIR:-/tmp}/pitcrew-active-findings.XXXXXX")
trap 'rm -f "$archive_before" "$archive_after" "$findings"' EXIT HUP INT TERM

archive_snapshot() {
  destination=$1
  find "$archive_root" -type f -print | LC_ALL=C sort |
    while IFS= read -r path; do sha256sum "$path"; done >"$destination"
}

active_contract_files() {
  cat <<'EOF'
AGENTS.md
openspec/AGENTS.md
EOF
  find "$repo_root/docs" -type f -print | LC_ALL=C sort |
    while IFS= read -r path; do
      printf '%s\n' "${path#"$repo_root/"}"
    done
  find "$repo_root/openspec/specs" -type f -print | LC_ALL=C sort |
    while IFS= read -r path; do
      printf '%s\n' "${path#"$repo_root/"}"
    done
}

allowed_legacy_line() {
  relative=$1
  line=$2
  case "$relative:$line" in
    docs/contributing.md:*master.md*) return 0 ;;
    *'--actor master'* | *'"actor":"master"'* | *'"actor": "master"'*) return 0 ;;
    *) return 1 ;;
  esac
}

archive_snapshot "$archive_before"

: >"$findings"
active_contract_files | while IFS= read -r relative; do
  grep -nE '(^|[^[:alnum:]_])[Mm]aster([^[:alnum:]_]|$)|master\.md|master revise plan' \
    "$repo_root/$relative" |
    while IFS= read -r line; do
      if ! allowed_legacy_line "$relative" "$line"; then
        printf '%s:%s\n' "$relative" "$line" >>"$findings"
      fi
    done || :
done

cli_contract="$repo_root/openspec/specs/cli-surface/spec.md"
for required in \
  'Daimon SHALL NOT invoke workflow commands' \
  'only Aion-acknowledged facts or clarification requests' \
  '| `delivery active` | none' \
  'zero active candidates' \
  'exactly one active candidate' \
  'multiple active candidates' \
  'one identity-specific inspection' \
  'SHALL NOT select by recency, ordering, route, goal similarity, or status'; do
  grep -Fq "$required" "$cli_contract" || {
    printf 'CLI Daimon boundary omitted: %s\n' "$required" >&2
    exit 1
  }
done

for required in \
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
  'does not replay historical progress'; do
  grep -Fq "$required" "$repo_root/AGENTS.md" || {
    printf 'active continuity contract omitted: %s\n' "$required" >&2
    exit 1
  }
done

for required in \
  '`delivery active` | None' \
  'aion admit new delivery' \
  'delivery show --delivery-id <id>' \
  'aion clarify delivery identity' \
  'Direct-only capability gaps'; do
  grep -Fq "$required" "$repo_root/docs/cli-reference.md" || {
    printf 'active continuity CLI documentation omitted: %s\n' "$required" >&2
    exit 1
  }
done

for required in \
  'Run `delivery active` before admitting new work' \
  'stable semantic key' \
  'unit identity, attempt, and outcome' \
  'do not replay historical progress' \
  'direct-only delivery has no supported durable capability-request surface'; do
  grep -Fq "$required" "$repo_root/scripts/install-templates.sh" || {
    printf 'installed continuity contract omitted: %s\n' "$required" >&2
    exit 1
  }
done

if test -s "$findings"; then
  cat "$findings" >&2
  echo "active contracts retain legacy Master vocabulary" >&2
  exit 1
fi

active_contract_files | while IFS= read -r relative; do
  grep -niE 'Daimon (may |MAY |uses |creates |passes |chooses |selects |orchestrates |coordinates |approves |holds |invokes )|Daimon.{0,80}(all workflow commands|proportional routing|sole coordinator)' \
    "$repo_root/$relative" >>"$findings" || :
done

if test -s "$findings"; then
  cat "$findings" >&2
  echo "active contracts grant orchestration authority to Daimon" >&2
  exit 1
fi

runtime_contract="$repo_root/openspec/specs/runtime-install/spec.md"
for required in \
  'active user-visible turn' \
  'host-native dual wait/select' \
  'requested state' \
  'terminal completion, a genuine blocker, or user cancellation' \
  'exactly one request-capability' \
  'polling, daemon, IPC, or durable inbox' \
  'unchanged capability requirement' \
  'SHALL NOT append a duplicate request' \
  'direct-only delivery has no supported durable capability-request surface' \
  'SHALL NOT invent a workflow or parallel lifecycle'; do
  grep -Fq "$required" "$runtime_contract" || {
    printf 'runtime live-turn contract omitted: %s\n' "$required" >&2
    exit 1
  }
done

for required in \
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
  'never simulate it by replaying conversation history or transcript content'; do
  grep -Fq "$required" "$repo_root/AGENTS.md" || {
    printf 'active trace/handoff contract omitted: %s\n' "$required" >&2
    exit 1
  }
  grep -Fq "$required" "$runtime_contract" || {
    printf 'runtime trace/handoff contract omitted: %s\n' "$required" >&2
    exit 1
  }
done

for required in \
  'before repository mutation' \
  'retain the stable operation key until start acknowledgement' \
  'replay the identical start after a lost response' \
  'inspect and resume the same delivery identity' \
  'one delivery identity, not one fallible invocation' \
  'retain the delivery ID and current revision' \
  'meaningful observed fact' \
  'last observed status' \
  'MUST NOT create a direct delivery trace'; do
  grep -Fq "$required" "$repo_root/AGENTS.md" || {
    printf 'active Aion delivery-trace contract omitted: %s\n' "$required" >&2
    exit 1
  }
  grep -Fq "$required" "$runtime_contract" || {
    printf 'runtime Aion delivery-trace contract omitted: %s\n' "$required" >&2
    exit 1
  }
done

for required in \
  'inspects project context once on demand' \
  'exactly one `pc2-sdd-initializer` attempt' \
  'bypasses it for `complete`' \
  'never schedules recurring scans' \
  'exactly the seven specialists' \
  'Daimon targets only Aion' \
  'specialists never delegate' \
  '`context inspect`, `context initialize`, `context record`'; do
  grep -Fq "$required" "$repo_root/AGENTS.md" || {
    printf 'active project-context routing contract omitted: %s\n' "$required" >&2
    exit 1
  }
done

for required in \
  'exactly all nine native definitions' \
  'target exactly the seven specialists' \
  'inspect project context once on demand' \
  'exactly one `pc2-sdd-initializer` attempt' \
  'bypass initialization when context is `complete`' \
  'never schedule recurring context scans' \
  'pitcrew context inspect' \
  'pitcrew context initialize' \
  'pitcrew context record'; do
  grep -Fq "$required" "$runtime_contract" || {
    printf 'runtime project-context routing contract omitted: %s\n' "$required" >&2
    exit 1
  }
done

archive_snapshot "$archive_after"
if ! cmp -s "$archive_before" "$archive_after"; then
  echo "archived OpenSpec content changed during active-contract validation" >&2
  exit 1
fi

echo "active contracts: clean"
