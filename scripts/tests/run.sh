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
roles='daimon aion pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer'
legacy_roles='explorer specifier designer task-planner implementer reviewer archivist pc2-archivist'
role_path() {
  directory=$1 role=$2
  native=$(printf '%s' "$role" | tr '-' '_')
  if [ -f "$directory/$native.toml" ]; then printf '%s' "$directory/$native.toml"; else printf '%s' "$directory/$role.md"; fi
}
contract_path() {
  directory=$1
  if [ -f "$directory/../pitcrew/agent-contract.md" ]; then printf '%s' "$directory/../pitcrew/agent-contract.md"; else printf '%s' "$directory/agent-contract.md"; fi
}
assert_role_set() {
  if find "$1" -maxdepth 1 -type f -name '*.toml' | grep . >/dev/null; then
    assert_codex_registry "${1%/agents}"
    return
  fi
  for role in $roles; do assert_file "$1/$role.md"; done
  if [ -f "$1/../pitcrew/agent-contract.md" ]; then
    assert_absent "$1/agent-contract.md"
    [ "$(find "$1" -type f -name '*.md' | wc -l | tr -d ' ')" -eq 9 ] || fail "unexpected agent set in $1"
  else
    assert_file "$1/agent-contract.md"
    [ "$(find "$1" -type f -name '*.md' | wc -l | tr -d ' ')" -eq 10 ] || fail "unexpected prompt set in $1"
  fi
  assert_absent "$1/master.md"
  for role in $legacy_roles; do assert_absent "$1/$role.md"; done
}

assert_codex_registry() {
  registry=$1/agents
  expected='aion daimon pc2_designer pc2_explorer pc2_implementer pc2_reviewer pc2_sdd_initializer pc2_specifier pc2_task_planner'
  actual=$(find "$registry" -maxdepth 1 -type f -name '*.toml' -exec basename {} .toml \; | sort | tr '\n' ' ' | sed 's/ $//')
  [ "$actual" = "$expected" ] || fail "unexpected Codex registry: $actual"
  assert_file "$1/pitcrew/agent-contract.md"
  assert_absent "$registry/agent-contract.toml"
  for role in $expected; do
    file=$registry/$role.toml
    grep -F 'name = "' "$file" >/dev/null || fail "$role missing Codex name"
    grep -F 'description = "' "$file" >/dev/null || fail "$role missing Codex description"
    grep -F "developer_instructions = '''" "$file" >/dev/null || fail "$role missing Codex instructions"
  done
  for graph_target in pc2_explorer pc2_specifier pc2_designer pc2_task_planner pc2_implementer pc2_reviewer pc2_sdd_initializer; do
    grep -F "$graph_target" "$registry/aion.toml" >/dev/null || fail "Codex Aion cannot resolve $graph_target"
  done
}

assert_opencode_registry() {
  registry=$1/agents
  expected='aion daimon pc2-designer pc2-explorer pc2-implementer pc2-reviewer pc2-sdd-initializer pc2-specifier pc2-task-planner'
  actual=$(find "$registry" -maxdepth 1 -type f -name '*.md' -exec basename {} .md \; | sort | tr '\n' ' ' | sed 's/ $//')
  [ "$actual" = "$expected" ] || fail "unexpected OpenCode registry: $actual"
  assert_file "$1/pitcrew/agent-contract.md"
  assert_absent "$registry/agent-contract.md"
  grep -F 'mode: primary' "$registry/daimon.md" >/dev/null || fail 'OpenCode Daimon is not primary'
  grep -F '    aion: allow' "$registry/daimon.md" >/dev/null || fail 'OpenCode Daimon cannot hand off to Aion'
  grep -F 'mode: all' "$registry/aion.md" >/dev/null || fail 'OpenCode Aion is not dispatch-capable'
  for graph_target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer; do
    grep -F "    $graph_target: allow" "$registry/aion.md" >/dev/null || fail "OpenCode Aion cannot resolve $graph_target"
    grep -F 'mode: subagent' "$registry/$graph_target.md" >/dev/null || fail "$graph_target is not an OpenCode subagent"
  done
}
assert_claude_registry() {
  registry=$1/agents
  expected='aion daimon pc2-designer pc2-explorer pc2-implementer pc2-reviewer pc2-sdd-initializer pc2-specifier pc2-task-planner'
  actual=$(find "$registry" -maxdepth 1 -type f -name '*.md' -exec basename {} .md \; | sort | tr '\n' ' ' | sed 's/ $//')
  [ "$actual" = "$expected" ] || fail "unexpected Claude registry: $actual"
  assert_file "$1/pitcrew/agent-contract.md"
  assert_absent "$registry/agent-contract.md"
  grep -F 'name: daimon' "$registry/daimon.md" >/dev/null || fail 'Claude Daimon metadata is invalid'
  grep -F 'tools: Agent' "$registry/daimon.md" >/dev/null || fail 'Claude Daimon cannot hand off to Aion'
  grep -F 'name: aion' "$registry/aion.md" >/dev/null || fail 'Claude Aion metadata is invalid'
  grep -F 'Claude delegation targets: pc2-explorer, pc2-specifier, pc2-designer, pc2-task-planner, pc2-implementer, pc2-reviewer, pc2-sdd-initializer.' "$registry/aion.md" >/dev/null || fail 'Claude Aion cannot resolve specialist targets'
  for graph_target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer; do
    grep -F "name: $graph_target" "$registry/$graph_target.md" >/dev/null || fail "$graph_target missing Claude metadata"
    grep -F 'disallowedTools: Agent' "$registry/$graph_target.md" >/dev/null || fail "$graph_target can unexpectedly delegate in Claude"
  done
}
seed_pi_subagents() {
  home=$1 version=$2 active=$3 package_source=${4:-npm:pi-subagents}
  mkdir -p "$home/npm/node_modules/pi-subagents" "$home/extensions/subagent"
  printf '{"name":"pi-subagents","version":"%s"}\n' "$version" > "$home/npm/node_modules/pi-subagents/package.json"
  printf '%s\n' '{"maxSubagentDepth":3}' > "$home/extensions/subagent/config.json"
  if [ "$active" = yes ]; then
    printf '{"packages":["%s"]}\n' "$package_source" > "$home/settings.json"
  else
    printf '%s\n' '{"packages":[]}' > "$home/settings.json"
  fi
}
assert_pi_registry() {
  registry=$1/agents
  expected='aion daimon pc2-designer pc2-explorer pc2-implementer pc2-reviewer pc2-sdd-initializer pc2-specifier pc2-task-planner'
  actual=$(find "$registry" -maxdepth 1 -type f -name '*.md' -exec basename {} .md \; | sort | tr '\n' ' ' | sed 's/ $//')
  [ "$actual" = "$expected" ] || fail "unexpected Pi registry: $actual"
  assert_file "$1/pitcrew/agent-contract.md"
  assert_absent "$registry/agent-contract.md"
  grep -F 'tools: read, grep, find, ls, bash, edit, write, subagent' "$registry/aion.md" >/dev/null || fail 'Pi Aion lacks nested delegation eligibility'
  grep -F 'maxSubagentDepth: 3' "$registry/daimon.md" >/dev/null || fail 'Pi Daimon cannot reach Aion and a specialist'
  grep -F 'maxSubagentDepth: 3' "$registry/aion.md" >/dev/null || fail 'Pi Aion cannot reach a specialist through Daimon'
  for literal in \
    'official Pi subagent runtime' \
    'injected contact_supervisor' \
    'reason: "progress_update"' \
    'personally observes and accepts one changed meaningful fact' \
    'exactly once' \
    'workflow ID and revision' \
    'next action' \
    'timer' \
    'raw specialist result or prose' \
    'unverified work' \
    'unchanged or repeated fact' \
    'routine completion handoff'; do
    grep -F -- "$literal" "$registry/aion.md" >/dev/null || fail "Pi Aion relay contract omitted $literal"
  done
  for literal in \
    'current official Pi subagent child' \
    'native progress_update' \
    'Aion acknowledgement' \
    'exactly one concise factual user update' \
    'raw specialist prose' \
    'direct specialist event' \
    'raw result-delivery event' \
    'timer' \
    'unchanged or repeated fact' \
    'second translation for the same Aion event' \
    "live as Aion's live addressable parent"; do
    grep -F -- "$literal" "$registry/daimon.md" >/dev/null || fail "Pi Daimon relay contract omitted $literal"
  done
  for graph_target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer; do
    grep -F "name: $graph_target" "$registry/$graph_target.md" >/dev/null || fail "$graph_target missing Pi metadata"
    grep '^tools: .*subagent' "$registry/$graph_target.md" >/dev/null && fail "$graph_target can unexpectedly delegate in Pi" || :
  done
}
assert_proportional_contract() {
  destination=$1
  for role in $roles; do
    file=$(role_path "$destination" "$role")
    maxim_lines=$(wc -l < "$ROOT/MAXIMS.md" | tr -d ' ')
    start=$(grep -nF 'Internalize the four maxims below. They are your operating system.' "$file" | head -1 | cut -d: -f1)
    sed -n "$((start + 2)),$((start + maxim_lines + 1))p" "$file" > "$TMP_ROOT/$role.proportional.maxims"
    cmp "$ROOT/MAXIMS.md" "$TMP_ROOT/$role.proportional.maxims" || fail "$role proportional maxims drift in $destination"
  done
  contract_file=$(contract_path "$destination")
  for contract in \
    'exists only to help the user achieve the stated goal' \
    'Is this solution overkill for the context?' \
    "Would a more relaxed, less demanding solution satisfy the user's expectations equally well?" \
    'least demanding solution that fully satisfies' \
    'name the protected constraint and explain why the simpler option is insufficient' \
    'claim secrecy' 'opaque-handle boundaries' 'reviewer independence' 'truthful evidence and progress' \
    'CAS inspection requirements' 'workflow integrity' 'terminal immutability' 'safety boundaries'; do
    grep -F "$contract" "$contract_file" >/dev/null || fail "agent contract proportional-design rule omitted $contract in $destination"
  done
  grep -F 'Applying an already-decided approach creates no new gate, justification, or artifact.' "$contract_file" >/dev/null || fail "agent contract omitted mechanical-execution exemption in $destination"
}

assert_authority_contract() {
  destination=$1
  daimon=$(role_path "$destination" daimon)
  aion=$(role_path "$destination" aion)
  contract=$(contract_path "$destination")
  grep -F 'reuse the same addressable Aion instance across all phases until terminal completion or a genuine blocker' "$daimon" >/dev/null || fail "Daimon continuity drift in $destination"
  grep -F 'sole external orchestration authority' "$aion" >/dev/null || fail "Aion authority drift in $destination"
  grep -F 'Retain workflow context and orchestration authority across all phases of an accepted delivery until terminal completion or a genuine blocker.' "$aion" >/dev/null || fail "Aion continuity drift in $destination"
  grep -F 'Return only factual revision-bearing status or clarification requests to Daimon.' "$aion" >/dev/null || fail "Aion hand-off drift in $destination"
  grep -F 'reuse one addressable Aion instance across all phases until terminal completion or a genuine blocker' "$contract" >/dev/null || fail "shared continuity drift in $destination"
  for live_rule in \
    'retain the active user-visible turn' \
    'host-native dual wait/select' \
    'same addressable Aion event or steered user input' \
    'forward it to that Aion as requested state' \
    'resume the same wait/select' \
    'terminal completion, a genuine blocker, or user cancellation' \
    'surface the missing host concurrency exactly once to Aion' \
    'never poll, start a daemon, use IPC, or create an inbox'; do
    grep -F "$live_rule" "$daimon" >/dev/null || fail "Daimon live-turn rule omitted $live_rule in $destination"
    grep -F "$live_rule" "$contract" >/dev/null || fail "shared live-turn rule omitted $live_rule in $destination"
  done
  grep -F 'record exactly one unchanged workflow request-capability' "$aion" >/dev/null || fail "Aion concurrency capability deduplication rule omitted in $destination"
  for terminal_rule in \
    'Reviewer alone runs `workflow complete` and returns the terminal result' \
    'relays it before the first publication action' \
    'broader delivery continues, and gives the actual next action' \
    'final delivery-only report omits that terminal key'; do
    grep -F "$terminal_rule" "$aion" >/dev/null || fail "Aion terminal-report ordering omitted $terminal_rule in $destination"
  done
  for silence_rule in \
    'Terminal facts require the Reviewer terminal result and Aion relay first' \
    'If there is no new accepted fact, emit nothing' \
    'Without a live Aion relay, do not synthesize an update'; do
    grep -F "$silence_rule" "$daimon" >/dev/null || fail "Daimon terminal-report silence omitted $silence_rule in $destination"
  done
  for specialist in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer; do
    file=$(role_path "$destination" "$specialist")
    grep -F 'Return only a one-line revision-bearing completion status to Aion.' "$file" >/dev/null || fail "$specialist hand-off drift in $destination"
  done
}

assert_context_initializer_contract() {
  destination=$1
  aion=$(role_path "$destination" aion)
  initializer=$(role_path "$destination" pc2-sdd-initializer)
  contract=$(contract_path "$destination")
  for rule in \
    'inspect project context once on demand' \
    'exactly one pc2-sdd-initializer attempt when context is missing or incomplete' \
    'bypass initialization when context is complete' \
    'never schedule recurring context scans'; do
    grep -F "$rule" "$aion" >/dev/null || fail "Aion context routing omitted $rule in $destination"
    grep -F "$rule" "$contract" >/dev/null || fail "shared context routing omitted $rule in $destination"
  done
  grep -F 'pitcrew context inspect' "$initializer" >/dev/null || fail "initializer cannot inspect context in $destination"
  grep -F 'pitcrew context initialize' "$initializer" >/dev/null || fail "initializer initialize command was not rendered literally in $destination"
  grep -F 'pitcrew context record' "$initializer" >/dev/null || fail "initializer cannot record context in $destination"
  grep -F 'Allowed workflow commands: none.' "$initializer" >/dev/null || fail "initializer unexpectedly owns workflow commands in $destination"
  grep -F 'Never delegate.' "$initializer" >/dev/null || fail "initializer delegation prohibition omitted in $destination"
  grep -F 'Return only a one-line context-bearing completion status to Aion.' "$initializer" >/dev/null || fail "initializer hand-off drift in $destination"
}

assert_delivery_trace_contract() {
  destination=$1
  aion=$(role_path "$destination" aion)
  contract=$(contract_path "$destination")
  for rule in \
    'before repository mutation' \
    'retain the stable operation key until start acknowledgement' \
    'replay the identical start after a lost response' \
    'inspect and resume the same delivery identity' \
    'one delivery identity, not one fallible invocation' \
    'retain the delivery ID and current revision' \
    'meaningful observed fact' \
    'last observed status' \
    'must not create a direct delivery trace'; do
    grep -F "$rule" "$aion" >/dev/null || fail "Aion delivery-trace contract omitted $rule in $destination"
    grep -F "$rule" "$contract" >/dev/null || fail "shared delivery-trace contract omitted $rule in $destination"
  done
}

assert_first_mutation_gate_contract() {
  destination=$1
  aion=$(role_path "$destination" aion)
  contract=$(contract_path "$destination")
  for rule in \
    'first admission gate' \
    'acknowledged before any repository mutation' \
    'stop before mutation and surface the capability boundary' \
    'never backfill a trace after work has started' \
    'does not interpose on or prevent host filesystem writes'; do
    grep -F "$rule" "$aion" >/dev/null || fail "Aion first-mutation gate omitted $rule in $destination"
    grep -F "$rule" "$contract" >/dev/null || fail "shared first-mutation gate omitted $rule in $destination"
  done
}

assert_transcript_minimal_handoff_contract() {
  destination=$1
  contract=$(contract_path "$destination")
  for rule in \
    'transcript-free composition' \
    'workflow ID and current revision' \
    'role or unit ID' \
    'applicable opaque handle path' \
    'workflow show --view coordination' \
    'workflow show --view phase' \
    'workflow show --view unit --unit-id' \
    'workflow show --view aggregate' \
    'surface the capability boundary' \
    'never simulate it by replaying conversation history or transcript content'; do
    grep -F "$rule" "$contract" >/dev/null || fail "shared transcript-minimal handoff omitted $rule in $destination"
  done

	grep -F 'Do not invoke workflow commands' "$(role_path "$destination" daimon)" >/dev/null || fail "Daimon command boundary omitted in $destination"
	grep -F 'Aion-acknowledged facts or clarification requests' "$(role_path "$destination" daimon)" >/dev/null || fail "Daimon acknowledgement boundary omitted in $destination"
	grep -F 'first admission gate' "$(role_path "$destination" aion)" >/dev/null || fail "Aion admission boundary omitted in $destination"
	grep -F 'Never forge or bypass independent review' "$(role_path "$destination" aion)" >/dev/null || fail "Aion review boundary omitted in $destination"
	for role in pc2-explorer pc2-specifier pc2-designer pc2-task-planner; do
		file=$(role_path "$destination" "$role")
		grep -F 'Accept only the workflow ID and current revision' "$file" >/dev/null || fail "$role minimal handoff identity omitted in $destination"
		grep -F 'bounded phase view' "$file" >/dev/null || fail "$role bounded view omitted in $destination"
	done
	grep -F 'bounded unit view' "$(role_path "$destination" pc2-implementer)" >/dev/null || fail "Implementer bounded handoff omitted in $destination"
	grep -F 'bounded unit or aggregate view' "$(role_path "$destination" pc2-reviewer)" >/dev/null || fail "Reviewer bounded handoff omitted in $destination"
	grep -F 'Never implement, forge approval, or accept implementation authority' "$(role_path "$destination" pc2-reviewer)" >/dev/null || fail "Reviewer authority boundary omitted in $destination"
	grep -F 'No workflow ID, transcript, or handle is required' "$(role_path "$destination" pc2-sdd-initializer)" >/dev/null || fail "Initializer bounded handoff omitted in $destination"
}

assert_role_prompt_budget() {
	destination=$1
	files=
	for role in $roles; do files="$files $(role_path "$destination" "$role")"; done
	bytes=$(cat $files | wc -c | tr -d ' ')
	words=$(cat $files | wc -w | tr -d ' ')
  aion=$(role_path "$destination" aion)
  case $aion in
    *.toml) baseline_bytes=46073 baseline_words=6372 ;;
    *)
      if grep -F 'Pi native supervisor rule' "$aion" >/dev/null; then
        baseline_bytes=48553 baseline_words=6751
      elif grep -F 'mode: all' "$aion" >/dev/null; then
        baseline_bytes=46116 baseline_words=6378
      else
        baseline_bytes=45969 baseline_words=6352
      fi
      ;;
  esac
  [ "$bytes" -le "$baseline_bytes" ] || fail "installed role prompts grew from $baseline_bytes baseline bytes to $bytes"
  [ "$words" -le "$baseline_words" ] || fail "installed role token proxy grew from $baseline_words baseline words to $words"
}

assert_role_view_permissions() {
  destination=$1
  daimon=$(role_path "$destination" daimon)
  initializer=$(role_path "$destination" pc2-sdd-initializer)
  grep -Fx 'Allowed workflow commands: No workflow commands; forward accepted intent to Aion.' "$daimon" >/dev/null || fail "Daimon workflow permission drift in $destination"
  grep -Fx 'Allowed workflow commands: none.' "$initializer" >/dev/null || fail "initializer workflow permission drift in $destination"
  for role_and_command in \
    'pc2-explorer|explore' \
    'pc2-specifier|spec' \
    'pc2-designer|design' \
    'pc2-task-planner|plan'; do
    role=${role_and_command%%|*}
    command=${role_and_command#*|}
    file=$(role_path "$destination" "$role")
    grep -Fx "Allowed workflow commands: workflow show --view phase and workflow $command." "$file" >/dev/null || fail "$role bounded-view permission drift in $destination"
  done
  implementer=$(role_path "$destination" pc2-implementer)
  reviewer=$(role_path "$destination" pc2-reviewer)
  grep -Fx 'Allowed workflow commands: workflow show --view unit --unit-id <wu-id>, workflow list-ready-units, workflow claim-unit, workflow unit-tdd, and workflow unit-complete. Never workflow unit-review or workflow complete.' "$implementer" >/dev/null || fail "Implementer bounded-view permission drift in $destination"
  grep -Fx 'Allowed workflow commands: workflow show --view unit --unit-id <wu-id>, workflow show --view aggregate, workflow unit-review, and workflow complete only. Never implementation commands.' "$reviewer" >/dev/null || fail "Reviewer bounded-view permission drift in $destination"
}

assert_exact_maxims() {
  destination=$1
  maxim_lines=$(wc -l < "$ROOT/MAXIMS.md" | tr -d ' ')
  for role in $roles; do
    file=$(role_path "$destination" "$role")
    start=$(grep -nF 'Internalize the four maxims below. They are your operating system.' "$file" | head -1 | cut -d: -f1)
    [ -n "$start" ] || fail "$role omitted embedded maxims in $destination"
    sed -n "$((start + 2)),$((start + maxim_lines + 1))p" "$file" > "$TMP_ROOT/public-$role.maxims"
    cmp "$ROOT/MAXIMS.md" "$TMP_ROOT/public-$role.maxims" || fail "$role maxims drift in $destination"
  done
}

sh -n "$INSTALLER" || fail "installer is not POSIX-shell parseable"
sh -n "$ROOT/scripts/tests/pi-supervisor-runtime.sh" || fail "Pi supervisor runtime test is not POSIX-shell parseable"
PITCREW_PI_SUPERVISOR_REGRESSION=1 sh "$ROOT/scripts/tests/pi-supervisor-runtime.sh"

unsupported=$TMP_ROOT/unsupported
mkdir -p "$unsupported"
if env -i HOME="$unsupported" PATH="$PATH" sh "$INSTALLER" >"$TMP_ROOT/unsupported.out" 2>"$TMP_ROOT/unsupported.err"; then
  fail "unsupported runtime succeeded"
fi
for runtime in Codex OpenCode 'Claude Code' Pi; do
  grep "$runtime" "$TMP_ROOT/unsupported.err" >/dev/null || fail "unsupported error omitted $runtime"
done

fake_bin=$TMP_ROOT/fake-bin
mkdir -p "$fake_bin"
cat > "$fake_bin/opencode" <<'FAKE_OPENCODE'
#!/bin/sh
set -eu
case "$*" in
  '--version')
    case ${PITCREW_TEST_OPENCODE_VERSION:-1.18.23} in
      timeout) sleep 6; printf '%s\n' 1.18.23 ;;
      oversized) awk 'BEGIN { for (i=0; i<1100000; i++) printf "1"; print "" }' ;;
      control) printf '1.18.23\001\n' ;;
      command-failure) printf '%s\n' 'version failed' >&2; exit 1 ;;
      *) printf '%s\n' "${PITCREW_TEST_OPENCODE_VERSION:-1.18.23}" ;;
    esac
    ;;
  '--pure debug config')
    [ -z "${PITCREW_TEST_OPENCODE_CWD_FILE:-}" ] || pwd -P > "$PITCREW_TEST_OPENCODE_CWD_FILE"
    case ${PITCREW_TEST_OPENCODE_CONFIG:-depth2} in
      depth2) printf '%s\n' '{"subagent_depth":2}' ;;
      depth3) printf '%s\n' '{"subagent_depth":3}' ;;
      missing) printf '%s\n' '{}' ;;
      depth1) printf '%s\n' '{"subagent_depth":1}' ;;
      negative) printf '%s\n' '{"subagent_depth":-1}' ;;
      fractional) printf '%s\n' '{"subagent_depth":2.5}' ;;
      duplicate) printf '%s\n' '{"subagent_depth":2,"subagent_depth":3}' ;;
      malformed) printf '%s\n' 'not-json' ;;
      incompatible) printf '%s\n' '{"subagent_depth":"2"}' ;;
      control) printf '{"subagent_depth":2,"bad":"\001"}\n' ;;
      oversized) awk 'BEGIN { printf "{\"subagent_depth\":2,\"padding\":\""; for (i=0; i<1100000; i++) printf "x"; print "\"}" }' ;;
      timeout) sleep 6; printf '%s\n' '{"subagent_depth":2}' ;;
      command-failure) printf '%s\n' 'unsupported debug command' >&2; exit 1 ;;
      *) exit 2 ;;
    esac
    ;;
  '--pure agent list')
    for file in "${OPENCODE_CONFIG_DIR:?}"/agents/*.md; do
      name=${file##*/}
      printf '%s (subagent)\n' "${name%.md}"
    done
    ;;
  *) exit 2 ;;
esac
FAKE_OPENCODE
chmod +x "$fake_bin/opencode"
PATH=$fake_bin:$PATH
export PATH
PITCREW_OPENCODE_BIN=$fake_bin/opencode
export PITCREW_OPENCODE_BIN

public_binary=$TMP_ROOT/pitcrew
(cd "$ROOT" && GOCACHE=${GOCACHE:-$TMP_ROOT/go-cache} go build -o "$public_binary" ./cmd/pitcrew)

public_default_home=$TMP_ROOT/public-default-home
mkdir -p "$public_default_home"
env -i HOME="$public_default_home" PATH="$PATH" TMPDIR="$TMP_ROOT" "$public_binary" install codex > "$TMP_ROOT/public-default.out"
assert_codex_registry "$public_default_home/.codex"
grep -Fx "Installed PitCrew agents for Codex in $public_default_home/.codex/agents" "$TMP_ROOT/public-default.out" >/dev/null || fail 'public Codex default success output drifted'

public_codex=$TMP_ROOT/public-codex
public_opencode=$TMP_ROOT/public-opencode
public_claude=$TMP_ROOT/public-claude
public_pi=$TMP_ROOT/public-pi
seed_pi_subagents "$public_pi" 0.25.0 yes
cp "$public_pi/settings.json" "$TMP_ROOT/public-pi.settings.before"
cp "$public_pi/npm/node_modules/pi-subagents/package.json" "$TMP_ROOT/public-pi.package.before"
mkdir -p "$public_codex" "$public_opencode" "$public_claude"
snapshot "$public_opencode" "$TMP_ROOT/public-opencode.initial"
snapshot "$public_claude" "$TMP_ROOT/public-claude.initial"
snapshot "$public_pi" "$TMP_ROOT/public-pi.initial"

CODEX_HOME=$public_codex OPENCODE_CONFIG_DIR=$public_opencode CLAUDE_CONFIG_DIR=$public_claude PI_AGENT_HOME=$public_pi \
  "$public_binary" install codex > "$TMP_ROOT/public-codex.out"
assert_codex_registry "$public_codex"
assert_exact_maxims "$public_codex/agents"
snapshot "$public_opencode" "$TMP_ROOT/public-opencode.after-codex"
snapshot "$public_claude" "$TMP_ROOT/public-claude.after-codex"
snapshot "$public_pi" "$TMP_ROOT/public-pi.after-codex"
cmp "$TMP_ROOT/public-opencode.initial" "$TMP_ROOT/public-opencode.after-codex" || fail 'public Codex selection inspected or changed OpenCode'
cmp "$TMP_ROOT/public-claude.initial" "$TMP_ROOT/public-claude.after-codex" || fail 'public Codex selection inspected or changed Claude'
cmp "$TMP_ROOT/public-pi.initial" "$TMP_ROOT/public-pi.after-codex" || fail 'public Codex selection inspected or changed Pi'

caller_cwd_public=$TMP_ROOT/public-opencode-cwd
mkdir -p "$caller_cwd_public"
(cd "$caller_cwd_public" && CODEX_HOME=$public_codex OPENCODE_CONFIG_DIR=$public_opencode CLAUDE_CONFIG_DIR=$public_claude PI_AGENT_HOME=$public_pi \
  PITCREW_TEST_OPENCODE_CWD_FILE=$TMP_ROOT/public-opencode.cwd "$public_binary" install opencode > "$TMP_ROOT/public-opencode.out")
assert_opencode_registry "$public_opencode"
assert_exact_maxims "$public_opencode/agents"
[ "$(cat "$TMP_ROOT/public-opencode.cwd")" = "$caller_cwd_public" ] || fail 'public OpenCode validation did not retain caller cwd'

CODEX_HOME=$public_codex OPENCODE_CONFIG_DIR=$public_opencode CLAUDE_CONFIG_DIR=$public_claude PI_AGENT_HOME=$public_pi \
  "$public_binary" install claude > "$TMP_ROOT/public-claude.out"
assert_claude_registry "$public_claude"
assert_exact_maxims "$public_claude/agents"
CODEX_HOME=$public_codex OPENCODE_CONFIG_DIR=$public_opencode CLAUDE_CONFIG_DIR=$public_claude PI_AGENT_HOME=$public_pi \
"$public_binary" install pi > "$TMP_ROOT/public-pi.out"
assert_pi_registry "$public_pi"
assert_exact_maxims "$public_pi/agents"
cmp "$TMP_ROOT/public-pi.settings.before" "$public_pi/settings.json" || fail 'public Pi install changed settings.json'
cmp "$TMP_ROOT/public-pi.package.before" "$public_pi/npm/node_modules/pi-subagents/package.json" || fail 'public Pi install changed package metadata'

public_pi_versioned=$TMP_ROOT/public-pi-versioned
seed_pi_subagents "$public_pi_versioned" 0.50.0 yes 'npm:pi-subagents@^0.50.0'
PI_AGENT_HOME=$public_pi_versioned "$public_binary" install pi >/dev/null
assert_pi_registry "$public_pi_versioned"
public_pi_near_name=$TMP_ROOT/public-pi-near-name
seed_pi_subagents "$public_pi_near_name" 0.50.0 yes 'npm:pi-subagents-extra@0.50.0'
if PI_AGENT_HOME=$public_pi_near_name "$public_binary" install pi > "$TMP_ROOT/public-pi-near-name.out" 2> "$TMP_ROOT/public-pi-near-name.err"; then
  fail 'public Pi near-name prerequisite succeeded'
fi
grep -F 'Rerun: pitcrew install pi' "$TMP_ROOT/public-pi-near-name.err" >/dev/null || fail 'public Pi prerequisite omitted durable rerun guidance'
assert_absent "$public_pi_near_name/agents"
assert_absent "$public_pi_near_name/pitcrew"

snapshot "$public_codex" "$TMP_ROOT/public-codex.idempotent-before"
CODEX_HOME=$public_codex "$public_binary" install codex > "$TMP_ROOT/public-codex.idempotent-out" 2> "$TMP_ROOT/public-codex.idempotent-err"
snapshot "$public_codex" "$TMP_ROOT/public-codex.idempotent-after"
cmp "$TMP_ROOT/public-codex.idempotent-before" "$TMP_ROOT/public-codex.idempotent-after" || fail 'public byte-identical reinstall changed Codex files'
[ ! -s "$TMP_ROOT/public-codex.idempotent-err" ] || fail 'public byte-identical reinstall warned'

printf '%s\n' 'old managed bytes' > "$public_codex/agents/aion.toml"
printf '%s\n' 'unrelated custom bytes' > "$public_codex/agents/my-agent.toml"
mkdir -p "$public_codex/prompts"
printf '%s\n' 'obsolete PitCrew master' > "$public_codex/prompts/master.md"
CODEX_HOME=$public_codex "$public_binary" install codex > "$TMP_ROOT/public-update.out" 2> "$TMP_ROOT/public-update.err"
grep -F 'PitCrew-managed definitions are being refreshed' "$TMP_ROOT/public-update.err" >/dev/null || fail 'public managed refresh omitted warning'
grep -F 'custom content must live outside managed role files' "$TMP_ROOT/public-update.err" >/dev/null || fail 'public managed refresh warning omitted customization boundary'
grep -F 'old managed bytes' "$public_codex/agents/aion.toml" >/dev/null && fail 'public update retained old managed bytes'
grep -Fx 'unrelated custom bytes' "$public_codex/agents/my-agent.toml" >/dev/null || fail 'public update changed unrelated file'
assert_absent "$public_codex/prompts/master.md"

public_failure=$TMP_ROOT/public-failure
mkdir -p "$public_failure"
printf '%s\n' preserved > "$public_failure/unrelated"
snapshot "$public_failure" "$TMP_ROOT/public-failure.before"
if OPENCODE_CONFIG_DIR=$public_failure PITCREW_TEST_OPENCODE_CONFIG=depth1 "$public_binary" install opencode > "$TMP_ROOT/public-failure.out" 2> "$TMP_ROOT/public-failure.err"; then
  fail 'public OpenCode prerequisite failure succeeded'
fi
grep -F 'Rerun: pitcrew install opencode' "$TMP_ROOT/public-failure.err" >/dev/null || fail 'public prerequisite omitted durable rerun guidance'
grep -F '"subagent_depth": 2' "$TMP_ROOT/public-failure.err" >/dev/null || fail 'public OpenCode prerequisite omitted remediation'
grep -F "$caller_cwd_public" "$TMP_ROOT/public-failure.err" >/dev/null && fail 'public prerequisite leaked unrelated path'
snapshot "$public_failure" "$TMP_ROOT/public-failure.after"
cmp "$TMP_ROOT/public-failure.before" "$TMP_ROOT/public-failure.after" || fail 'public prerequisite failure mutated target'

public_missing_cli=$TMP_ROOT/public-missing-cli
mkdir -p "$public_missing_cli"
if OPENCODE_CONFIG_DIR=$public_missing_cli PITCREW_OPENCODE_BIN=$TMP_ROOT/missing-opencode "$public_binary" install opencode > "$TMP_ROOT/public-missing-cli.out" 2> "$TMP_ROOT/public-missing-cli.err"; then
  fail 'public OpenCode missing CLI prerequisite succeeded'
fi
grep -F 'requires the opencode CLI' "$TMP_ROOT/public-missing-cli.err" >/dev/null || fail 'public OpenCode missing CLI error drifted'
grep -F 'Rerun: pitcrew install opencode' "$TMP_ROOT/public-missing-cli.err" >/dev/null || fail 'public OpenCode missing CLI omitted rerun guidance'
assert_absent "$public_missing_cli/agents"
assert_absent "$public_missing_cli/pitcrew"

public_rollback=$TMP_ROOT/public-rollback
CODEX_HOME=$public_rollback "$public_binary" install codex >/dev/null
printf '%s\n' 'previous aion' > "$public_rollback/agents/aion.toml"
mkdir -p "$public_rollback/prompts"
printf '%s\n' 'previous master' > "$public_rollback/prompts/master.md"
snapshot "$public_rollback" "$TMP_ROOT/public-rollback.before"
if CODEX_HOME=$public_rollback PITCREW_TEST_FAIL_AFTER_WRITES=1 "$public_binary" install codex >/dev/null 2>&1; then fail 'public rollback injection succeeded'; fi
snapshot "$public_rollback" "$TMP_ROOT/public-rollback.after"
cmp "$TMP_ROOT/public-rollback.before" "$TMP_ROOT/public-rollback.after" || fail 'public failure did not roll back exact target tree'
assert_no_temps "$public_rollback"

public_signal=$TMP_ROOT/public-signal
CODEX_HOME=$public_signal "$public_binary" install codex >/dev/null
printf '%s\n' 'previous reviewer' > "$public_signal/agents/pc2_reviewer.toml"
snapshot "$public_signal" "$TMP_ROOT/public-signal.before"
if CODEX_HOME=$public_signal PITCREW_TEST_SIGNAL_AFTER_WRITES=1 "$public_binary" install codex >/dev/null 2>&1; then fail 'public signal injection succeeded'; fi
snapshot "$public_signal" "$TMP_ROOT/public-signal.after"
cmp "$TMP_ROOT/public-signal.before" "$TMP_ROOT/public-signal.after" || fail 'public signal did not roll back exact target tree'
assert_no_temps "$public_signal"

for depth_case in missing depth1 negative fractional duplicate malformed incompatible control oversized timeout command-failure; do
  depth_home=$TMP_ROOT/opencode-depth-$depth_case
  mkdir -p "$depth_home"
  printf '%s\n' preserved > "$depth_home/existing"
  snapshot "$depth_home" "$TMP_ROOT/depth-$depth_case.before"
  if OPENCODE_CONFIG_DIR=$depth_home PITCREW_TEST_OPENCODE_CONFIG=$depth_case sh "$INSTALLER" >"$TMP_ROOT/depth-$depth_case.out" 2>"$TMP_ROOT/depth-$depth_case.err"; then
    fail "OpenCode $depth_case effective depth accepted"
  fi
  case $depth_case in
    missing) expected_fragment='effective subagent_depth is missing' ;;
    depth1|negative) expected_fragment='effective subagent_depth must be at least 2' ;;
    fractional|incompatible) expected_fragment='incompatible subagent_depth value' ;;
    duplicate) expected_fragment='ambiguous duplicate subagent_depth values' ;;
    malformed|control) expected_fragment='malformed configuration JSON' ;;
    oversized) expected_fragment='configuration output exceeded 1048576 bytes' ;;
    timeout) expected_fragment='configuration query timed out after 5 seconds' ;;
    command-failure) expected_fragment='cannot resolve OpenCode effective configuration' ;;
  esac
  grep -F "$expected_fragment" "$TMP_ROOT/depth-$depth_case.err" >/dev/null || fail "OpenCode $depth_case error is not exact and actionable"
  grep -F "target project $ROOT" "$TMP_ROOT/depth-$depth_case.err" >/dev/null || fail "OpenCode $depth_case error omitted target cwd"
  grep -F "Verify: $fake_bin/opencode --pure debug config" "$TMP_ROOT/depth-$depth_case.err" >/dev/null || fail "OpenCode $depth_case error omitted verification command"
  grep -F 'Higher-precedence project configuration may override global settings.' "$TMP_ROOT/depth-$depth_case.err" >/dev/null || fail "OpenCode $depth_case error omitted precedence warning"
  grep -F "Rerun: $INSTALLER" "$TMP_ROOT/depth-$depth_case.err" >/dev/null || fail "OpenCode $depth_case error omitted exact rerun"
  snapshot "$depth_home" "$TMP_ROOT/depth-$depth_case.after"
  cmp "$TMP_ROOT/depth-$depth_case.before" "$TMP_ROOT/depth-$depth_case.after" || fail "OpenCode $depth_case prerequisite changed target files"
  assert_absent "$depth_home/agents"
  assert_absent "$depth_home/pitcrew"
done

for version_case in timeout oversized control command-failure; do
  version_home=$TMP_ROOT/opencode-version-$version_case
  mkdir -p "$version_home"
  printf '%s\n' preserved > "$version_home/existing"
  snapshot "$version_home" "$TMP_ROOT/version-$version_case.before"
  if OPENCODE_CONFIG_DIR=$version_home PITCREW_TEST_OPENCODE_VERSION=$version_case sh "$INSTALLER" >"$TMP_ROOT/version-$version_case.out" 2>"$TMP_ROOT/version-$version_case.err"; then
    fail "OpenCode $version_case version output accepted"
  fi
  case $version_case in
    timeout) expected_fragment='OpenCode version query timed out after 5 seconds' ;;
    oversized) expected_fragment='OpenCode version output exceeded 1048576 bytes' ;;
    control) expected_fragment='OpenCode returned incompatible version output' ;;
    command-failure) expected_fragment='cannot query OpenCode version' ;;
  esac
  grep -F "$expected_fragment" "$TMP_ROOT/version-$version_case.err" >/dev/null || fail "OpenCode $version_case version error is not actionable"
  grep -F "target project $ROOT" "$TMP_ROOT/version-$version_case.err" >/dev/null || fail "OpenCode $version_case version error omitted target cwd"
  grep -F "Verify: $fake_bin/opencode --pure debug config" "$TMP_ROOT/version-$version_case.err" >/dev/null || fail "OpenCode $version_case version error omitted verification command"
  grep -F "Rerun: $INSTALLER" "$TMP_ROOT/version-$version_case.err" >/dev/null || fail "OpenCode $version_case version error omitted exact rerun"
  snapshot "$version_home" "$TMP_ROOT/version-$version_case.after"
  cmp "$TMP_ROOT/version-$version_case.before" "$TMP_ROOT/version-$version_case.after" || fail "OpenCode $version_case version check changed target files"
  assert_absent "$version_home/agents"
  assert_absent "$version_home/pitcrew"
done

old_opencode=$TMP_ROOT/opencode-old
mkdir -p "$old_opencode"
if OPENCODE_CONFIG_DIR=$old_opencode PITCREW_TEST_OPENCODE_VERSION=1.18.22 sh "$INSTALLER" >"$TMP_ROOT/opencode-old.out" 2>"$TMP_ROOT/opencode-old.err"; then
  fail 'OpenCode 1.18.22 accepted'
fi
grep -F 'OpenCode >= 1.18.23 is required; found 1.18.22.' "$TMP_ROOT/opencode-old.err" >/dev/null || fail 'OpenCode version boundary error is not actionable'
assert_absent "$old_opencode/agents"

opencode_newer=$TMP_ROOT/opencode-newer
OPENCODE_CONFIG_DIR=$opencode_newer PITCREW_TEST_OPENCODE_VERSION=2.0.0 PITCREW_TEST_OPENCODE_CONFIG=depth3 sh "$INSTALLER" >/dev/null
assert_opencode_registry "$opencode_newer"

poison_bin=$TMP_ROOT/poison-bin
mkdir -p "$poison_bin"
cat > "$poison_bin/opencode" <<'POISON_OPENCODE'
#!/bin/sh
exit 99
POISON_OPENCODE
chmod +x "$poison_bin/opencode"
explicit_bin_home=$TMP_ROOT/opencode-explicit-bin
PATH=$poison_bin:$PATH OPENCODE_CONFIG_DIR=$explicit_bin_home PITCREW_OPENCODE_BIN=$fake_bin/opencode sh "$INSTALLER" >/dev/null
assert_opencode_registry "$explicit_bin_home"

caller_cwd=$TMP_ROOT/opencode-caller-cwd
caller_home=$TMP_ROOT/opencode-caller-home
mkdir -p "$caller_cwd"
(cd "$caller_cwd" && OPENCODE_CONFIG_DIR=$caller_home PITCREW_TEST_OPENCODE_CWD_FILE=$TMP_ROOT/opencode.cwd sh "$INSTALLER" >/dev/null)
[ "$(cat "$TMP_ROOT/opencode.cwd")" = "$caller_cwd" ] || fail 'OpenCode effective configuration was not resolved from caller cwd'

codex=$TMP_ROOT/codex
CODEX_HOME=$codex sh "$INSTALLER"
assert_codex_registry "$codex"
target=$codex/agents
assert_role_set "$target"
assert_proportional_contract "$target"
assert_authority_contract "$target"
assert_delivery_trace_contract "$target"
assert_first_mutation_gate_contract "$target"
assert_transcript_minimal_handoff_contract "$target"
assert_role_prompt_budget "$target"
assert_role_view_permissions "$target"
assert_context_initializer_contract "$target"
for role in $roles; do
  file=$(role_path "$target" "$role")
  assert_file "$file"
  start=$(grep -nF 'Internalize the four maxims below. They are your operating system.' "$file" | head -1 | cut -d: -f1)
  first=$(sed -n "${start}p" "$file")
  second=$(sed -n "$((start + 1))p" "$file")
  [ "$first" = 'Internalize the four maxims below. They are your operating system.' ] || fail "$role prefix line 1"
  [ "$second" = 'Every decision you make is subordinate to them.' ] || fail "$role prefix line 2"
  maxim_lines=$(wc -l < "$ROOT/MAXIMS.md" | tr -d ' ')
  sed -n "$((start + 2)),$((start + maxim_lines + 1))p" "$file" > "$TMP_ROOT/$role.maxims"
  cmp "$ROOT/MAXIMS.md" "$TMP_ROOT/$role.maxims" || fail "$role maxims drift"
done
daimon_file=$(role_path "$target" daimon)
aion_file=$(role_path "$target" aion)
contract_file=$(contract_path "$target")
for contract in 'interview the user' 'clarify intent' 'conversational continuity' 'Aion-acknowledged facts' 'requested, not applied'; do
  grep -F "$contract" "$daimon_file" >/dev/null || fail "Daimon contract omitted $contract"
done
grep -F 'reuse the same addressable Aion instance across all phases until terminal completion or a genuine blocker' "$daimon_file" >/dev/null || fail 'Daimon omitted Aion delivery continuity'
for forbidden in 'may invoke any workflow command' 'Choose the least costly valid route' 'handoff-review' 'abandon --reason' 'aggregate review'; do
  grep -F "$forbidden" "$daimon_file" >/dev/null && fail "Daimon retained orchestration authority: $forbidden" || :
done
for authority in 'sole external orchestration authority' 'owns the workflow ID, current revision, goal, and status' 'may invoke any workflow command' 'abandon --reason' 'handoff-review' 'recover-review' 'workflow continue --from' 'workflow request-capability'; do
  grep -F "$authority" "$aion_file" >/dev/null || fail "Aion authority omitted $authority"
done
grep -F 'Concurrent Daimon availability depends on an addressable-agent host runtime.' "$aion_file" >/dev/null || fail 'Aion omitted host concurrency boundary'
grep -F 'Retain workflow context and orchestration authority across all phases of an accepted delivery until terminal completion or a genuine blocker.' "$aion_file" >/dev/null || fail 'Aion omitted retained delivery authority'
grep -F 'Return only factual revision-bearing status or clarification requests to Daimon.' "$aion_file" >/dev/null || fail 'Aion hand-off contract omitted'
for role in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer; do
  file=$(role_path "$target" "$role")
  grep -F 'Return only a one-line revision-bearing completion status to Aion.' "$file" >/dev/null || fail "$role does not report only to Aion"
  grep -F 'completion status to Daimon' "$file" >/dev/null && fail "$role still reports to Daimon" || :
done
for obstruction_rule in 'On exit 3 or 4' 'inspect once' 'harness obstructs legitimate work' 'never issue an identical retry'; do
  grep -F "$obstruction_rule" "$aion_file" >/dev/null || fail "Aion obstruction rule omitted $obstruction_rule"
  grep -F "$obstruction_rule" "$contract_file" >/dev/null || fail "agent contract obstruction rule omitted $obstruction_rule"
done
for route in 'at most three files' 'four or more files' 'risk overrides file count' 'delegated direct' 'full workflow' 'exploration: pc2-explorer' 'specification: pc2-specifier' 'design: pc2-designer' 'task planning: pc2-task-planner' 'implementation: pc2-implementer' 'aggregate review: pc2-reviewer' 'Never delegate a workflow role to General or general'; do
  grep -F "$route" "$aion_file" >/dev/null || fail "Aion routing omitted $route"
  grep -F "$route" "$contract_file" >/dev/null || fail "agent contract routing omitted $route"
done
for authority in 'may invoke any workflow command' 'abandon --reason' 'must not claim independent approval' 'must not bypass aggregate review'; do
  grep -F "$authority" "$aion_file" >/dev/null || fail "Aion authority omitted $authority"
done
for handle_rule in 'must not disclose handle contents or secrets' 'must pass only the opaque handle path to pc2-reviewer'; do
  grep -F "$handle_rule" "$aion_file" >/dev/null || fail "Aion handle contract omitted $handle_rule"
done
grep -F 'recover-review may rotate it only for the same reviewer after expiry' "$aion_file" >/dev/null || fail 'Aion review recovery contract omitted actor continuity'
grep -F 'recover-review` preserves the originally handed-off reviewer identity' "$contract_file" >/dev/null || fail 'agent contract omitted review recovery identity'
grep -F 'workflow continue --from to create a linked draft instead' "$aion_file" >/dev/null || fail 'Aion terminal continuation contract omitted'
grep -F 'Continue terminal work only with `workflow continue --from`' "$contract_file" >/dev/null || fail 'agent contract omitted terminal continuation'
for progress_rule in 'short, truthful, non-repetitive user status' 'Silence is required until a meaningful fact changes' 'must not fabricate progress or repeat encouragement'; do
  grep -F "$progress_rule" "$daimon_file" >/dev/null || fail "Daimon progress contract omitted $progress_rule"
  grep -F "$progress_rule" "$contract_file" >/dev/null || fail "agent contract progress contract omitted $progress_rule"
done
for capability_rule in 'required tool, command, or transition is absent' 'workflow request-capability' 'does not imply fulfillment' 'must not invent or bypass'; do
  grep -F "$capability_rule" "$aion_file" >/dev/null || fail "Aion capability contract omitted $capability_rule"
  grep -F "$capability_rule" "$contract_file" >/dev/null || fail "agent contract capability contract omitted $capability_rule"
done
for review_rule in 'Unit review is selective' 'Final aggregate review is mandatory' 'requirements, specifications, design, tasks, implementation evidence, and tests'; do
  grep -F "$review_rule" "$contract_file" >/dev/null || fail "agent contract review rule omitted $review_rule"
done
reviewer_file=$(role_path "$target" pc2-reviewer)
for correction_rule in 'correction budget' 'group findings by causal invariant' 'user authorization required' 'explicit user direction'; do
  grep -F "$correction_rule" "$aion_file" >/dev/null || fail "Aion bounded-correction contract omitted $correction_rule"
  grep -F "$correction_rule" "$contract_file" >/dev/null || fail "agent contract bounded-correction rule omitted $correction_rule"
done
for reviewer_rule in 'declared correction policy' 'latest unresolved blocker' 'never implement'; do
  grep -F "$reviewer_rule" "$reviewer_file" >/dev/null || fail "Reviewer bounded-correction contract omitted $reviewer_rule"
done
grep -F 'GitHub Issues is the source of truth for PitCrew backlog work:' "$ROOT/docs/todo.md" >/dev/null || fail 'GitHub backlog source contract omitted'
for issue in 132 133 134 135; do
  grep -F "https://github.com/feanor41/pitcrew2/issues/$issue" "$ROOT/docs/todo.md" >/dev/null || fail "GitHub backlog issue $issue omitted"
done
if grep -Eq '(^|[[:space:]])- \[ \]' "$ROOT/docs/todo.md"; then
  fail 'docs/todo.md regained an unchecked local backlog item'
fi
grep -F 'reuse one addressable Aion instance across all phases until terminal completion or a genuine blocker' "$contract_file" >/dev/null || fail 'agent contract omitted Aion delivery continuity'
assert_file "$contract_file"
for prohibited in '--claim-token' '--emit-plain-token' '--print-claim-handle-secret-once' 'same identity' 'CAS'; do
  grep -F -- "$prohibited" "$contract_file" >/dev/null || fail "contract omitted $prohibited"
done

missing_graph=$TMP_ROOT/missing-opencode-graph
if OPENCODE_CONFIG_DIR=$missing_graph PITCREW_TEST_DROP_STAGED_ROLE=pc2-reviewer sh "$INSTALLER" >"$TMP_ROOT/missing-graph.out" 2>"$TMP_ROOT/missing-graph.err"; then
  fail 'OpenCode install accepted a missing declared specialist'
fi
assert_absent "$missing_graph/agents"
assert_absent "$missing_graph/pitcrew"

for runtime in codex claude pi; do
  missing_graph=$TMP_ROOT/missing-$runtime-graph
  case $runtime in
    codex)
      if CODEX_HOME=$missing_graph PITCREW_TEST_DROP_STAGED_ROLE=pc2-reviewer sh "$INSTALLER" >"$TMP_ROOT/missing-$runtime.out" 2>"$TMP_ROOT/missing-$runtime.err"; then fail 'Codex install accepted a missing declared specialist'; fi
      ;;
    claude)
      if CLAUDE_CONFIG_DIR=$missing_graph PITCREW_TEST_DROP_STAGED_ROLE=pc2-reviewer sh "$INSTALLER" >"$TMP_ROOT/missing-$runtime.out" 2>"$TMP_ROOT/missing-$runtime.err"; then fail 'Claude install accepted a missing declared specialist'; fi
      ;;
    pi)
      seed_pi_subagents "$missing_graph" 0.25.0 yes
      if PI_AGENT_HOME=$missing_graph PITCREW_TEST_DROP_STAGED_ROLE=pc2-reviewer sh "$INSTALLER" >"$TMP_ROOT/missing-$runtime.out" 2>"$TMP_ROOT/missing-$runtime.err"; then fail 'Pi install accepted a missing declared specialist'; fi
      ;;
  esac
  assert_absent "$missing_graph/agents"
  assert_absent "$missing_graph/pitcrew"
done

claude_native=$TMP_ROOT/claude-native
CLAUDE_CONFIG_DIR=$claude_native sh "$INSTALLER"
assert_claude_registry "$claude_native"

for pi_case in absent old inactive decoy-settings malformed-package malformed-settings near-name missing-depth insufficient-depth malformed-depth; do
  pi_home=$TMP_ROOT/pi-$pi_case
  case $pi_case in
    absent) mkdir -p "$pi_home" ;;
    old) seed_pi_subagents "$pi_home" 0.24.9 yes ;;
    inactive) seed_pi_subagents "$pi_home" 0.25.0 no ;;
    decoy-settings)
      seed_pi_subagents "$pi_home" 0.25.0 no
      printf '%s\n' '{"packages":[],"note":"npm:pi-subagents"}' > "$pi_home/settings.json"
      ;;
    malformed-package)
      seed_pi_subagents "$pi_home" 0.25.0 yes
      printf '%s\n' 'not-json {"name":"pi-subagents","version":"0.25.0"}' > "$pi_home/npm/node_modules/pi-subagents/package.json"
      ;;
    malformed-settings)
      seed_pi_subagents "$pi_home" 0.25.0 yes
      printf '%s\n' 'not-json {"packages":["npm:pi-subagents"]}' > "$pi_home/settings.json"
      ;;
    near-name) seed_pi_subagents "$pi_home" 0.50.0 yes 'npm:pi-subagents-extra@0.50.0' ;;
    missing-depth)
      seed_pi_subagents "$pi_home" 0.25.0 yes
      rm "$pi_home/extensions/subagent/config.json"
      ;;
    insufficient-depth)
      seed_pi_subagents "$pi_home" 0.25.0 yes
      printf '%s\n' '{"maxSubagentDepth":2}' > "$pi_home/extensions/subagent/config.json"
      ;;
    malformed-depth)
      seed_pi_subagents "$pi_home" 0.25.0 yes
      printf '%s\n' '{"maxSubagentDepth":"3"}' > "$pi_home/extensions/subagent/config.json"
      ;;
  esac
  if PI_AGENT_HOME=$pi_home sh "$INSTALLER" >"$TMP_ROOT/pi-$pi_case.out" 2>"$TMP_ROOT/pi-$pi_case.err"; then
    fail "Pi $pi_case prerequisite accepted"
  fi
  assert_absent "$pi_home/agents"
  assert_absent "$pi_home/pitcrew"
done

pi_native=$TMP_ROOT/pi-native
seed_pi_subagents "$pi_native" 0.25.0 yes
PI_AGENT_HOME=$pi_native sh "$INSTALLER"
assert_pi_registry "$pi_native"

for pi_source in 'npm:pi-subagents@0.50.0' 'npm:pi-subagents@^0.50.0'; do
  pi_versioned=$TMP_ROOT/pi-versioned-$(printf '%s' "$pi_source" | tr -c '[:alnum:]' '-')
  seed_pi_subagents "$pi_versioned" 0.50.0 yes "$pi_source"
  PI_AGENT_HOME=$pi_versioned sh "$INSTALLER"
  assert_pi_registry "$pi_versioned"
done

snapshot "$target" "$TMP_ROOT/before.cksum"
CODEX_HOME=$codex sh "$INSTALLER"
snapshot "$target" "$TMP_ROOT/after.cksum"
cmp "$TMP_ROOT/before.cksum" "$TMP_ROOT/after.cksum" || fail "reinstall changed bytes"

printf 'custom explorer\n' > "$target/pc2_explorer.toml"
snapshot "$target" "$TMP_ROOT/custom-role.before"
if CODEX_HOME=$codex sh "$INSTALLER" >"$TMP_ROOT/custom-role.out" 2>"$TMP_ROOT/custom-role.err"; then fail 'custom role accepted without overwrite'; fi
snapshot "$target" "$TMP_ROOT/custom-role.after"
cmp "$TMP_ROOT/custom-role.before" "$TMP_ROOT/custom-role.after" || fail 'custom role refusal changed files'
grep -F 'custom explorer' "$target/pc2_explorer.toml" >/dev/null || fail 'custom role was overwritten'
CODEX_HOME=$codex sh "$INSTALLER" --overwrite >/dev/null
grep -F 'custom explorer' "$target/pc2_explorer.toml" >/dev/null && fail 'overwrite did not refresh custom role'

mkdir -p "$codex/prompts"
printf 'custom master\n' > "$codex/prompts/master.md"
for role in $legacy_roles; do printf 'legacy %s\n' "$role" > "$codex/prompts/$role.md"; done
snapshot "$codex" "$TMP_ROOT/custom.cksum"
if CODEX_HOME=$codex sh "$INSTALLER" >"$TMP_ROOT/refuse.out" 2>"$TMP_ROOT/refuse.err"; then
  fail "legacy master accepted without --overwrite"
fi
grep -F 'custom master' "$codex/prompts/master.md" >/dev/null || fail "custom master was changed"
snapshot "$codex" "$TMP_ROOT/refused.cksum"
cmp "$TMP_ROOT/custom.cksum" "$TMP_ROOT/refused.cksum" || fail "refused install changed files"
grep -F -- '--overwrite' "$TMP_ROOT/refuse.err" >/dev/null || fail 'legacy refusal omitted overwrite guidance'
CODEX_HOME=$codex sh "$INSTALLER" --overwrite >"$TMP_ROOT/migrate.out" 2>"$TMP_ROOT/migrate.err"
grep -F 'preserve desired custom text' "$TMP_ROOT/migrate.err" >/dev/null || fail 'overwrite warning omitted customization risk'
assert_absent "$codex/prompts/master.md"
for role in $legacy_roles; do assert_absent "$codex/prompts/$role.md"; done
assert_file "$target/daimon.toml"

for protected in daimon aion; do
  differ=$TMP_ROOT/differing-$protected
  CODEX_HOME=$differ sh "$INSTALLER" >/dev/null
  printf 'custom %s\n' "$protected" > "$differ/agents/$protected.toml"
  snapshot "$differ" "$TMP_ROOT/$protected.before"
  if CODEX_HOME=$differ sh "$INSTALLER" >"$TMP_ROOT/$protected.out" 2>"$TMP_ROOT/$protected.err"; then fail "custom $protected accepted without overwrite"; fi
  snapshot "$differ" "$TMP_ROOT/$protected.after"
  cmp "$TMP_ROOT/$protected.before" "$TMP_ROOT/$protected.after" || fail "$protected refusal changed files"
  CODEX_HOME=$differ sh "$INSTALLER" --overwrite >/dev/null
  grep -F "custom $protected" "$differ/agents/$protected.toml" >/dev/null && fail "$protected overwrite did not restore canonical prompt"
done

for case_name in fail-remove signal-remove fail-writes signal-writes; do
  rollback=$TMP_ROOT/rollback-$case_name
  mkdir -p "$rollback/prompts"
  printf 'previous master\n' > "$rollback/prompts/master.md"
  printf 'previous daimon\n' > "$rollback/prompts/daimon.md"
  printf 'previous aion\n' > "$rollback/prompts/aion.md"
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

for runtime in codex opencode claude pi; do
  rollback=$TMP_ROOT/cross-runtime-rollback-$runtime
  case $runtime in
    codex) CODEX_HOME=$rollback sh "$INSTALLER" >/dev/null; legacy=$rollback/prompts ;;
    opencode) OPENCODE_CONFIG_DIR=$rollback sh "$INSTALLER" >/dev/null; legacy=$rollback/agents ;;
    claude) CLAUDE_CONFIG_DIR=$rollback sh "$INSTALLER" >/dev/null; legacy=$rollback/prompts ;;
    pi) seed_pi_subagents "$rollback" 0.25.0 yes; PI_AGENT_HOME=$rollback sh "$INSTALLER" >/dev/null; legacy=$rollback/agents ;;
  esac
  mkdir -p "$legacy"
  printf 'legacy master\n' > "$legacy/master.md"
  case $runtime in codex) reviewer=$rollback/agents/pc2_reviewer.toml ;; *) reviewer=$rollback/agents/pc2-reviewer.md ;; esac
  printf 'custom reviewer\n' > "$reviewer"
  snapshot "$rollback" "$TMP_ROOT/$runtime.cross-runtime.before"
  case $runtime in
    codex) if CODEX_HOME=$rollback PITCREW_TEST_FAIL_AFTER_WRITES=1 sh "$INSTALLER" --overwrite >/dev/null 2>&1; then fail "$runtime rollback injection succeeded"; fi ;;
    opencode) if OPENCODE_CONFIG_DIR=$rollback PITCREW_TEST_FAIL_AFTER_WRITES=1 sh "$INSTALLER" --overwrite >/dev/null 2>&1; then fail "$runtime rollback injection succeeded"; fi ;;
    claude) if CLAUDE_CONFIG_DIR=$rollback PITCREW_TEST_FAIL_AFTER_WRITES=1 sh "$INSTALLER" --overwrite >/dev/null 2>&1; then fail "$runtime rollback injection succeeded"; fi ;;
    pi) if PI_AGENT_HOME=$rollback PITCREW_TEST_FAIL_AFTER_WRITES=1 sh "$INSTALLER" --overwrite >/dev/null 2>&1; then fail "$runtime rollback injection succeeded"; fi ;;
  esac
  snapshot "$rollback" "$TMP_ROOT/$runtime.cross-runtime.after"
  cmp "$TMP_ROOT/$runtime.cross-runtime.before" "$TMP_ROOT/$runtime.cross-runtime.after" || fail "$runtime rollback did not restore native and legacy files"
  assert_no_temps "$rollback"
done

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
printf '%s\n' 3 2 1 > "$TMP_ROOT/rollback.expected"
cmp "$TMP_ROOT/rollback.expected" "$TMP_ROOT/rollback.order" || fail 'rollback did not compensate in reverse mutation order'

empty=$TMP_ROOT/new-empty-target
stage_tmp=$TMP_ROOT/private-stage
mkdir -p "$stage_tmp"
if TMPDIR=$stage_tmp CODEX_HOME=$empty PITCREW_TEST_FAIL_AFTER_WRITES=1 sh "$INSTALLER" >"$TMP_ROOT/empty.out" 2>"$TMP_ROOT/empty.err"; then fail 'new-target failure succeeded'; fi
assert_absent "$empty/prompts"
[ -z "$(find "$stage_tmp" -mindepth 1 -print)" ] || fail 'private stage was not cleaned'

nested_empty=$TMP_ROOT/new/nested/codex-home
if CODEX_HOME=$nested_empty PITCREW_TEST_FAIL_AFTER_WRITES=1 sh "$INSTALLER" >"$TMP_ROOT/nested-empty.out" 2>"$TMP_ROOT/nested-empty.err"; then fail 'nested new-target failure succeeded'; fi
assert_absent "$TMP_ROOT/new"

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
    claude) CLAUDE_CONFIG_DIR=$home sh "$INSTALLER"; installed=$home/agents ;;
    pi) seed_pi_subagents "$home" 0.25.0 yes; PI_AGENT_HOME=$home sh "$INSTALLER"; installed=$home/agents ;;
  esac
  assert_role_set "$installed"
  assert_proportional_contract "$installed"
  assert_authority_contract "$installed"
  assert_delivery_trace_contract "$installed"
  assert_first_mutation_gate_contract "$installed"
  assert_transcript_minimal_handoff_contract "$installed"
  assert_role_prompt_budget "$installed"
  assert_role_view_permissions "$installed"
  assert_context_initializer_contract "$installed"
  if [ "$runtime" = opencode ]; then
    assert_opencode_registry "$home"
    if command -v opencode >/dev/null 2>&1; then
      mkdir -p "$home/xdg-data" "$home/xdg-state" "$home/xdg-cache" "$home/runtime-home"
      HOME="$home/runtime-home" XDG_DATA_HOME="$home/xdg-data" XDG_STATE_HOME="$home/xdg-state" XDG_CACHE_HOME="$home/xdg-cache" OPENCODE_CONFIG_DIR="$home" \
        opencode --pure agent list > "$home/native-agent-list"
      grep -E '^(daimon|aion|pc2-(explorer|specifier|designer|task-planner|implementer|reviewer|sdd-initializer)) \(' "$home/native-agent-list" | sed 's/ (.*$//' | sort > "$home/native-agent-names"
      printf '%s\n' aion daimon pc2-designer pc2-explorer pc2-implementer pc2-reviewer pc2-sdd-initializer pc2-specifier pc2-task-planner > "$home/expected-agent-names"
      cmp "$home/expected-agent-names" "$home/native-agent-names" || fail 'OpenCode native discovery omitted or duplicated PitCrew agents'
    fi
  elif [ "$runtime" = claude ]; then
    assert_claude_registry "$home"
  elif [ "$runtime" = pi ]; then
    assert_pi_registry "$home"
  fi

  snapshot "$home" "$TMP_ROOT/$runtime.idempotent.before"
  case $runtime in
    opencode) OPENCODE_CONFIG_DIR=$home sh "$INSTALLER" >/dev/null ;;
    claude) CLAUDE_CONFIG_DIR=$home sh "$INSTALLER" >/dev/null ;;
    pi) PI_AGENT_HOME=$home sh "$INSTALLER" >/dev/null ;;
  esac
  snapshot "$home" "$TMP_ROOT/$runtime.idempotent.after"
  cmp "$TMP_ROOT/$runtime.idempotent.before" "$TMP_ROOT/$runtime.idempotent.after" || fail "$runtime reinstall changed bytes"

  printf 'custom reviewer\n' > "$installed/pc2-reviewer.md"
  snapshot "$home" "$TMP_ROOT/$runtime.overwrite.before"
  case $runtime in
    opencode) if OPENCODE_CONFIG_DIR=$home sh "$INSTALLER" >"$TMP_ROOT/$runtime.overwrite.out" 2>"$TMP_ROOT/$runtime.overwrite.err"; then fail "$runtime accepted a differing role without overwrite"; fi ;;
    claude) if CLAUDE_CONFIG_DIR=$home sh "$INSTALLER" >"$TMP_ROOT/$runtime.overwrite.out" 2>"$TMP_ROOT/$runtime.overwrite.err"; then fail "$runtime accepted a differing role without overwrite"; fi ;;
    pi) if PI_AGENT_HOME=$home sh "$INSTALLER" >"$TMP_ROOT/$runtime.overwrite.out" 2>"$TMP_ROOT/$runtime.overwrite.err"; then fail "$runtime accepted a differing role without overwrite"; fi ;;
  esac
  snapshot "$home" "$TMP_ROOT/$runtime.overwrite.after"
  cmp "$TMP_ROOT/$runtime.overwrite.before" "$TMP_ROOT/$runtime.overwrite.after" || fail "$runtime overwrite refusal changed files"
  case $runtime in
    opencode) OPENCODE_CONFIG_DIR=$home sh "$INSTALLER" --overwrite >/dev/null ;;
    claude) CLAUDE_CONFIG_DIR=$home sh "$INSTALLER" --overwrite >/dev/null ;;
    pi) PI_AGENT_HOME=$home sh "$INSTALLER" --overwrite >/dev/null ;;
  esac
  grep -F 'custom reviewer' "$installed/pc2-reviewer.md" >/dev/null && fail "$runtime overwrite did not restore the native role"
done

spaced=$TMP_ROOT/'home with spaces'
CODEX_HOME="$spaced" sh "$INSTALLER" >/dev/null
assert_role_set "$spaced/agents"

for document in "$ROOT/AGENTS.md" "$ROOT/docs/cli-reference.md" "$ROOT/docs/contributing.md"; do
  assert_file "$document"
done
for command in new continue show progress request-capability explore spec design plan approve-plan list-ready-units begin-implementation complete abandon claim-unit recover-unit-claim handoff-review recover-review unit-tdd unit-review unit-complete; do
  grep -F "workflow $command" "$ROOT/docs/cli-reference.md" >/dev/null || fail "CLI reference omitted workflow $command"
done
for code in '0 — ok' '1 — internal' '2 — usage' '3 — state' '4 — CAS' '5 — handle'; do
  grep -F "$code" "$ROOT/docs/cli-reference.md" >/dev/null || fail "CLI reference omitted exit $code"
done
grep -F 'go test ./...' "$ROOT/docs/contributing.md" >/dev/null || fail 'contributing guide omitted Go validation'
grep -F 'sh scripts/tests/run.sh' "$ROOT/docs/contributing.md" >/dev/null || fail 'contributing guide omitted installer validation'
for native_contract in \
  'Codex: `agents/*.toml`' \
  'OpenCode: `agents/*.md`' \
  'Claude Code: `agents/*.md`' \
  'Pi: `agents/*.md`' \
  '`pi-subagents` version 0.25.0 or newer' \
  'offline schema and dispatch-graph validation proves discovery eligibility, not model execution'; do
  grep -F "$native_contract" "$ROOT/docs/contributing.md" >/dev/null || fail "contributing guide omitted native validation contract: $native_contract"
done
for document in "$ROOT/AGENTS.md" "$ROOT/openspec/AGENTS.md" "$ROOT/docs/contributing.md"; do
  grep -F 'Is this solution overkill for the context?' "$document" >/dev/null || fail "active guidance omitted overkill question in $document"
  grep -F "Would a more relaxed, less demanding solution satisfy the user's expectations equally well?" "$document" >/dev/null || fail "active guidance omitted relaxed-solution question in $document"
  grep -F 'name the protected constraint' "$document" >/dev/null || fail "active guidance omitted named-constraint justification in $document"
  grep -F 'why the simpler option is insufficient' "$document" >/dev/null || fail "active guidance omitted simpler-option justification in $document"
done
for document in "$ROOT/AGENTS.md" "$ROOT/openspec/AGENTS.md" "$ROOT/docs/contributing.md"; do
  grep -F 'Applying an already-decided approach creates no new gate, justification, or artifact.' "$document" >/dev/null || fail "active guidance omitted mechanical-execution exemption in $document"
done

printf 'installer_smoke_tests=passed\n'
