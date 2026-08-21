#!/bin/sh
set -eu

ROOT=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)
INSTALLER=$ROOT/scripts/install-templates.sh
TMP_ROOT=${TMPDIR:-/tmp}/pitcrew-installer-tests.$$
mkdir -p "$TMP_ROOT"
trap 'rm -rf "$TMP_ROOT"' EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_file() { [ -f "$1" ] || fail "missing file $1"; }

sh -n "$INSTALLER" || fail "installer is not POSIX-shell parseable"

unsupported=$TMP_ROOT/unsupported
mkdir -p "$unsupported"
if env -i HOME="$unsupported" PATH="$PATH" sh "$INSTALLER" >"$TMP_ROOT/unsupported.out" 2>"$TMP_ROOT/unsupported.err"; then
  fail "unsupported runtime succeeded"
fi
for runtime in Codex OpenCode 'Claude Code' Pi; do
  grep "$runtime" "$TMP_ROOT/unsupported.err" >/dev/null || fail "unsupported error omitted $runtime"
done

codex=$TMP_ROOT/codex
CODEX_HOME=$codex sh "$INSTALLER"
target=$codex/prompts
roles='master explorer specifier designer task-planner implementer reviewer archivist'
for role in $roles; do
  file=$target/$role.md
  assert_file "$file"
  first=$(sed -n '1p' "$file")
  second=$(sed -n '2p' "$file")
  [ "$first" = 'Internalize the four maxims below. They are your operating system.' ] || fail "$role prefix line 1"
  [ "$second" = 'Every decision you make is subordinate to them.' ] || fail "$role prefix line 2"
  grep -F 'You do not return your output to the Master. You call the control plane yourself. The Master only learns that you finished.' "$file" >/dev/null || fail "$role hand-off reminder"
  maxim_lines=$(wc -l < "$ROOT/MAXIMS.md" | tr -d ' ')
  sed -n "3,$((maxim_lines + 2))p" "$file" > "$TMP_ROOT/$role.maxims"
  cmp "$ROOT/MAXIMS.md" "$TMP_ROOT/$role.maxims" || fail "$role maxims drift"
done
assert_file "$target/agent-contract.md"
for prohibited in '--claim-token' '--emit-plain-token' '--print-claim-handle-secret-once' 'same identity' 'CAS'; do
  grep -F -- "$prohibited" "$target/agent-contract.md" >/dev/null || fail "contract omitted $prohibited"
done

find "$target" -type f -exec cksum {} \; | sort > "$TMP_ROOT/before.cksum"
CODEX_HOME=$codex sh "$INSTALLER"
find "$target" -type f -exec cksum {} \; | sort > "$TMP_ROOT/after.cksum"
cmp "$TMP_ROOT/before.cksum" "$TMP_ROOT/after.cksum" || fail "reinstall changed bytes"

printf 'custom explorer\n' > "$target/explorer.md"
CODEX_HOME=$codex sh "$INSTALLER"
grep -F 'custom explorer' "$target/explorer.md" >/dev/null && fail "non-Master fragment was not refreshed"

printf 'custom master\n' > "$target/master.md"
find "$target" -type f -exec cksum {} \; | sort > "$TMP_ROOT/custom.cksum"
if CODEX_HOME=$codex sh "$INSTALLER" >"$TMP_ROOT/refuse.out" 2>"$TMP_ROOT/refuse.err"; then
  fail "custom master overwritten without --overwrite"
fi
grep -F 'custom master' "$target/master.md" >/dev/null || fail "custom master was changed"
find "$target" -type f -exec cksum {} \; | sort > "$TMP_ROOT/refused.cksum"
cmp "$TMP_ROOT/custom.cksum" "$TMP_ROOT/refused.cksum" || fail "refused install changed files"
CODEX_HOME=$codex sh "$INSTALLER" --overwrite
grep -F 'custom master' "$target/master.md" >/dev/null && fail "--overwrite did not replace master"

rollback=$TMP_ROOT/rollback
mkdir -p "$rollback/prompts"
printf 'previous master\n' > "$rollback/prompts/master.md"
printf 'previous explorer\n' > "$rollback/prompts/explorer.md"
if CODEX_HOME=$rollback PITCREW_TEST_FAIL_AFTER_WRITES=2 sh "$INSTALLER" --overwrite >"$TMP_ROOT/rollback.out" 2>"$TMP_ROOT/rollback.err"; then
  fail "simulated partial failure succeeded"
fi
[ "$(cat "$rollback/prompts/master.md")" = 'previous master' ] || fail 'rollback did not restore master'
[ "$(cat "$rollback/prompts/explorer.md")" = 'previous explorer' ] || fail 'rollback did not restore explorer'
if find "$rollback/prompts" -type f -name '*.md' ! -name master.md ! -name explorer.md -print 2>/dev/null | grep . >/dev/null; then fail "partial install left generated files"; fi

for runtime in opencode claude pi; do
  home=$TMP_ROOT/$runtime
  mkdir -p "$home"
  case $runtime in
    opencode) OPENCODE_CONFIG_DIR=$home sh "$INSTALLER"; installed=$home/agents ;;
    claude) CLAUDE_CONFIG_DIR=$home sh "$INSTALLER"; installed=$home/prompts ;;
    pi) PI_AGENT_HOME=$home sh "$INSTALLER"; installed=$home/agents ;;
  esac
  assert_file "$installed/master.md"
  assert_file "$installed/agent-contract.md"
done

for document in "$ROOT/AGENTS.md" "$ROOT/docs/cli-reference.md" "$ROOT/docs/contributing.md"; do
  assert_file "$document"
done
for command in new show explore spec design plan approve-plan list-ready-units begin-implementation complete abandon claim-unit recover-unit-claim unit-tdd unit-review unit-complete; do
  grep -F "workflow $command" "$ROOT/docs/cli-reference.md" >/dev/null || fail "CLI reference omitted workflow $command"
done
for code in '0 — ok' '1 — internal' '2 — usage' '3 — state' '4 — CAS' '5 — handle'; do
  grep -F "$code" "$ROOT/docs/cli-reference.md" >/dev/null || fail "CLI reference omitted exit $code"
done
grep -F 'go test ./...' "$ROOT/docs/contributing.md" >/dev/null || fail 'contributing guide omitted Go validation'
grep -F 'sh scripts/tests/run.sh' "$ROOT/docs/contributing.md" >/dev/null || fail 'contributing guide omitted installer validation'

printf 'installer_smoke_tests=passed\n'
