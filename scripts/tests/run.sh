#!/bin/sh
set -eu

ROOT=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)
INSTALLER=$ROOT/scripts/install-templates.sh
TMP_ROOT=${TMPDIR:-/tmp}/pitcrew-installer-tests.$$
mkdir -p "$TMP_ROOT"
trap 'rm -rf "$TMP_ROOT"' EXIT HUP INT TERM

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
assert_file() { [ -f "$1" ] || fail "missing file $1"; }
assert_absent() { [ ! -e "$1" ] && [ ! -L "$1" ] || fail "unexpected path $1"; }
snapshot() { find "$1" -type f -exec cksum {} \; | sort > "$2"; }
assert_no_temps() { find "$1" -name '.pitcrew-install.*' -o -name '.*.md.new.*' | grep . >/dev/null && fail "installer temporary remains in $1" || :; }
roles='daimon pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer'
legacy_roles='explorer specifier designer task-planner implementer reviewer archivist pc2-archivist'
assert_role_set() {
  for role in $roles; do assert_file "$1/$role.md"; done
  assert_file "$1/agent-contract.md"
  [ "$(find "$1" -type f -name '*.md' | wc -l | tr -d ' ')" -eq 8 ] || fail "unexpected prompt set in $1"
  assert_absent "$1/master.md"
  for role in $legacy_roles; do assert_absent "$1/$role.md"; done
}

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
assert_role_set "$target"
for role in $roles; do
  file=$target/$role.md
  assert_file "$file"
  first=$(sed -n '1p' "$file")
  second=$(sed -n '2p' "$file")
  [ "$first" = 'Internalize the four maxims below. They are your operating system.' ] || fail "$role prefix line 1"
  [ "$second" = 'Every decision you make is subordinate to them.' ] || fail "$role prefix line 2"
  grep -F 'Return only a one-line completion status to Daimon, the sole coordinator and user contact.' "$file" >/dev/null || fail "$role hand-off reminder"
  maxim_lines=$(wc -l < "$ROOT/MAXIMS.md" | tr -d ' ')
  sed -n "3,$((maxim_lines + 2))p" "$file" > "$TMP_ROOT/$role.maxims"
  cmp "$ROOT/MAXIMS.md" "$TMP_ROOT/$role.maxims" || fail "$role maxims drift"
done
for contract in 'sole bridge between the user and sub-agents' 'Adapt expression to the user' truthful incisive goal-directed outcome-first 'resistant to cheerleading' 'not a Unix daemon' 'authorization identity' 'internal orchestrator'; do
  grep -F "$contract" "$target/daimon.md" >/dev/null || fail "Daimon contract omitted $contract"
done
for obstruction_rule in 'On exit 3 or 4' 'inspect once' 'harness obstructs legitimate work' 'never issue an identical retry'; do
  grep -F "$obstruction_rule" "$target/daimon.md" >/dev/null || fail "Daimon obstruction rule omitted $obstruction_rule"
  grep -F "$obstruction_rule" "$target/agent-contract.md" >/dev/null || fail "agent contract obstruction rule omitted $obstruction_rule"
done
for route in 'at most three files' 'four or more files' 'risk overrides file count' 'delegated direct' 'full workflow' 'exploration: pc2-explorer' 'specification: pc2-specifier' 'design: pc2-designer' 'task planning: pc2-task-planner' 'implementation: pc2-implementer' 'aggregate review: pc2-reviewer' 'Never delegate a workflow role to General or general'; do
  grep -F "$route" "$target/daimon.md" >/dev/null || fail "Daimon routing omitted $route"
  grep -F "$route" "$target/agent-contract.md" >/dev/null || fail "agent contract routing omitted $route"
done
for authority in 'may invoke any workflow command' 'abandon --reason' 'must not claim independent approval' 'must not bypass aggregate review'; do
  grep -F "$authority" "$target/daimon.md" >/dev/null || fail "Daimon authority omitted $authority"
done
for handle_rule in 'must not disclose handle contents or secrets' 'must pass only the opaque handle path to pc2-reviewer'; do
  grep -F "$handle_rule" "$target/daimon.md" >/dev/null || fail "Daimon handle contract omitted $handle_rule"
done
for review_rule in 'Unit review is selective' 'Final aggregate review is mandatory' 'requirements, specifications, design, tasks, implementation evidence, and tests'; do
  grep -F "$review_rule" "$target/agent-contract.md" >/dev/null || fail "agent contract review rule omitted $review_rule"
done
assert_file "$target/agent-contract.md"
for prohibited in '--claim-token' '--emit-plain-token' '--print-claim-handle-secret-once' 'same identity' 'CAS'; do
  grep -F -- "$prohibited" "$target/agent-contract.md" >/dev/null || fail "contract omitted $prohibited"
done

snapshot "$target" "$TMP_ROOT/before.cksum"
CODEX_HOME=$codex sh "$INSTALLER"
snapshot "$target" "$TMP_ROOT/after.cksum"
cmp "$TMP_ROOT/before.cksum" "$TMP_ROOT/after.cksum" || fail "reinstall changed bytes"

printf 'custom explorer\n' > "$target/pc2-explorer.md"
snapshot "$target" "$TMP_ROOT/custom-role.before"
if CODEX_HOME=$codex sh "$INSTALLER" >"$TMP_ROOT/custom-role.out" 2>"$TMP_ROOT/custom-role.err"; then fail 'custom role accepted without overwrite'; fi
snapshot "$target" "$TMP_ROOT/custom-role.after"
cmp "$TMP_ROOT/custom-role.before" "$TMP_ROOT/custom-role.after" || fail 'custom role refusal changed files'
grep -F 'custom explorer' "$target/pc2-explorer.md" >/dev/null || fail 'custom role was overwritten'
CODEX_HOME=$codex sh "$INSTALLER" --overwrite >/dev/null
grep -F 'custom explorer' "$target/pc2-explorer.md" >/dev/null && fail 'overwrite did not refresh custom role'

printf 'custom master\n' > "$target/master.md"
for role in $legacy_roles; do printf 'legacy %s\n' "$role" > "$target/$role.md"; done
snapshot "$target" "$TMP_ROOT/custom.cksum"
if CODEX_HOME=$codex sh "$INSTALLER" >"$TMP_ROOT/refuse.out" 2>"$TMP_ROOT/refuse.err"; then
  fail "legacy master accepted without --overwrite"
fi
grep -F 'custom master' "$target/master.md" >/dev/null || fail "custom master was changed"
snapshot "$target" "$TMP_ROOT/refused.cksum"
cmp "$TMP_ROOT/custom.cksum" "$TMP_ROOT/refused.cksum" || fail "refused install changed files"
grep -F -- '--overwrite' "$TMP_ROOT/refuse.err" >/dev/null || fail 'legacy refusal omitted overwrite guidance'
CODEX_HOME=$codex sh "$INSTALLER" --overwrite >"$TMP_ROOT/migrate.out" 2>"$TMP_ROOT/migrate.err"
grep -F 'preserve desired custom text' "$TMP_ROOT/migrate.err" >/dev/null || fail 'overwrite warning omitted customization risk'
assert_absent "$target/master.md"
for role in $legacy_roles; do assert_absent "$target/$role.md"; done
assert_file "$target/daimon.md"

differ=$TMP_ROOT/differing-daimon
CODEX_HOME=$differ sh "$INSTALLER" >/dev/null
printf 'custom daimon\n' > "$differ/prompts/daimon.md"
snapshot "$differ/prompts" "$TMP_ROOT/differ.before"
if CODEX_HOME=$differ sh "$INSTALLER" >"$TMP_ROOT/differ.out" 2>"$TMP_ROOT/differ.err"; then fail 'custom Daimon accepted without overwrite'; fi
snapshot "$differ/prompts" "$TMP_ROOT/differ.after"
cmp "$TMP_ROOT/differ.before" "$TMP_ROOT/differ.after" || fail 'Daimon refusal changed files'

for case_name in fail-remove signal-remove fail-writes signal-writes; do
  rollback=$TMP_ROOT/rollback-$case_name
  mkdir -p "$rollback/prompts"
  printf 'previous master\n' > "$rollback/prompts/master.md"
  printf 'previous daimon\n' > "$rollback/prompts/daimon.md"
  printf 'previous explorer\n' > "$rollback/prompts/explorer.md"
  printf 'previous pc2 explorer\n' > "$rollback/prompts/pc2-explorer.md"
  snapshot "$rollback/prompts" "$TMP_ROOT/$case_name.before"
  case $case_name in
    fail-remove) injection=PITCREW_TEST_FAIL_AFTER_MASTER_REMOVAL=1 ;;
    signal-remove) injection=PITCREW_TEST_SIGNAL_AFTER_MASTER_REMOVAL=1 ;;
    fail-writes) injection=PITCREW_TEST_FAIL_AFTER_WRITES=2 ;;
    signal-writes) injection=PITCREW_TEST_SIGNAL_AFTER_WRITES=2 ;;
  esac
  if env CODEX_HOME="$rollback" "$injection" sh "$INSTALLER" --overwrite >"$TMP_ROOT/$case_name.out" 2>"$TMP_ROOT/$case_name.err"; then fail "$case_name succeeded"; fi
  snapshot "$rollback/prompts" "$TMP_ROOT/$case_name.after"
  cmp "$TMP_ROOT/$case_name.before" "$TMP_ROOT/$case_name.after" || fail "$case_name did not restore all files"
  assert_no_temps "$rollback"
done

legacy_rollback=$TMP_ROOT/legacy-removal-rollback
mkdir -p "$legacy_rollback/prompts"
for role in $legacy_roles; do printf 'legacy %s\n' "$role" > "$legacy_rollback/prompts/$role.md"; done
snapshot "$legacy_rollback/prompts" "$TMP_ROOT/legacy-removal.before"
if CODEX_HOME=$legacy_rollback PITCREW_TEST_FAIL_AFTER_LEGACY_REMOVALS=1 sh "$INSTALLER" --overwrite >/dev/null 2>&1; then fail 'legacy removal fault succeeded'; fi
snapshot "$legacy_rollback/prompts" "$TMP_ROOT/legacy-removal.after"
cmp "$TMP_ROOT/legacy-removal.before" "$TMP_ROOT/legacy-removal.after" || fail 'legacy removal fault did not restore all files'
assert_no_temps "$legacy_rollback"

order_home=$TMP_ROOT/rollback-order
order_bin=$TMP_ROOT/rollback-order-bin
mkdir -p "$order_home/prompts" "$order_bin"
printf 'previous master\n' > "$order_home/prompts/master.md"
printf 'previous daimon\n' > "$order_home/prompts/daimon.md"
printf 'previous explorer\n' > "$order_home/prompts/explorer.md"
cat > "$order_bin/cp" <<'EOF'
#!/bin/sh
source_path=$1
if [ "$source_path" = -p ]; then source_path=$2; fi
case $source_path in */backup.*) printf '%s\n' "${source_path##*/backup.}" >> "$ROLLBACK_ORDER_FILE" ;; esac
exec /bin/cp "$@"
EOF
chmod 700 "$order_bin/cp"
if PATH="$order_bin:$PATH" ROLLBACK_ORDER_FILE="$TMP_ROOT/rollback.order" CODEX_HOME="$order_home" PITCREW_TEST_FAIL_AFTER_WRITES=2 sh "$INSTALLER" --overwrite >"$TMP_ROOT/order.out" 2>"$TMP_ROOT/order.err"; then fail 'rollback-order injection succeeded'; fi
printf '%s\n' daimon.md explorer.md master.md > "$TMP_ROOT/rollback.expected"
cmp "$TMP_ROOT/rollback.expected" "$TMP_ROOT/rollback.order" || fail 'rollback did not compensate in reverse mutation order'

empty=$TMP_ROOT/new-empty-target
stage_tmp=$TMP_ROOT/private-stage
mkdir -p "$stage_tmp"
if TMPDIR=$stage_tmp CODEX_HOME=$empty PITCREW_TEST_FAIL_AFTER_WRITES=1 sh "$INSTALLER" >"$TMP_ROOT/empty.out" 2>"$TMP_ROOT/empty.err"; then fail 'new-target failure succeeded'; fi
assert_absent "$empty/prompts"
[ -z "$(find "$stage_tmp" -mindepth 1 -print)" ] || fail 'private stage was not cleaned'

for kind in symlink directory; do
  unsafe=$TMP_ROOT/unsafe-$kind
  mkdir -p "$unsafe/prompts" "$unsafe/seed"
  case $kind in symlink) ln -s "$unsafe/seed/value" "$unsafe/prompts/daimon.md" ;; directory) mkdir "$unsafe/prompts/reviewer.md" ;; esac
  if CODEX_HOME=$unsafe sh "$INSTALLER" --overwrite >"$TMP_ROOT/$kind.out" 2>"$TMP_ROOT/$kind.err"; then fail "$kind coordinator accepted"; fi
  assert_absent "$unsafe/prompts/explorer.md"
done

for runtime in opencode claude pi; do
  home=$TMP_ROOT/$runtime
  mkdir -p "$home"
  case $runtime in
    opencode) OPENCODE_CONFIG_DIR=$home sh "$INSTALLER"; installed=$home/agents ;;
    claude) CLAUDE_CONFIG_DIR=$home sh "$INSTALLER"; installed=$home/prompts ;;
    pi) PI_AGENT_HOME=$home sh "$INSTALLER"; installed=$home/agents ;;
  esac
  assert_role_set "$installed"
  assert_file "$installed/agent-contract.md"
done

spaced=$TMP_ROOT/'home with spaces'
CODEX_HOME="$spaced" sh "$INSTALLER" >/dev/null
assert_role_set "$spaced/prompts"

for document in "$ROOT/AGENTS.md" "$ROOT/docs/cli-reference.md" "$ROOT/docs/contributing.md"; do
  assert_file "$document"
done
for command in new show explore spec design plan approve-plan list-ready-units begin-implementation complete abandon claim-unit recover-unit-claim handoff-review unit-tdd unit-review unit-complete; do
  grep -F "workflow $command" "$ROOT/docs/cli-reference.md" >/dev/null || fail "CLI reference omitted workflow $command"
done
for code in '0 — ok' '1 — internal' '2 — usage' '3 — state' '4 — CAS' '5 — handle'; do
  grep -F "$code" "$ROOT/docs/cli-reference.md" >/dev/null || fail "CLI reference omitted exit $code"
done
grep -F 'go test ./...' "$ROOT/docs/contributing.md" >/dev/null || fail 'contributing guide omitted Go validation'
grep -F 'sh scripts/tests/run.sh' "$ROOT/docs/contributing.md" >/dev/null || fail 'contributing guide omitted installer validation'

printf 'installer_smoke_tests=passed\n'
