#!/bin/sh
set -eu

usage() {
  printf 'Usage: scripts/install-templates.sh [--overwrite] | codex|opencode|claude|pi\n' >&2
  exit 2
}

overwrite=0
public_install=0
selected_runtime=
case $# in
  0) ;;
  1)
    case $1 in
      --overwrite) overwrite=1 ;;
      codex|opencode|claude|pi) selected_runtime=$1; public_install=1; overwrite=1 ;;
      *) usage ;;
    esac
    ;;
  *) usage ;;
esac

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
ROOT=$(CDPATH= cd "$SCRIPT_DIR/.." && pwd)
MAXIMS=$ROOT/MAXIMS.md
[ -r "$MAXIMS" ] || { printf 'pitcrew installer: cannot read %s\n' "$MAXIMS" >&2; exit 1; }

runtime=
runtime_root=
registry=
legacy_registry=
extension=md
if [ -n "$selected_runtime" ]; then
  case $selected_runtime in
    codex) runtime=Codex; runtime_root=${CODEX_HOME:-${HOME:?HOME is required}/.codex}; registry=$runtime_root/agents; legacy_registry=$runtime_root/prompts; extension=toml ;;
    opencode) runtime=OpenCode; runtime_root=${OPENCODE_CONFIG_DIR:-${HOME:?HOME is required}/.config/opencode}; registry=$runtime_root/agents; legacy_registry=$registry ;;
    claude) runtime='Claude Code'; runtime_root=${CLAUDE_CONFIG_DIR:-${HOME:?HOME is required}/.claude}; registry=$runtime_root/agents; legacy_registry=$runtime_root/prompts ;;
    pi) runtime=Pi; runtime_root=${PI_AGENT_HOME:-${HOME:?HOME is required}/.pi/agent}; registry=$runtime_root/agents; legacy_registry=$registry ;;
  esac
elif [ -n "${CODEX_HOME:-}" ]; then runtime=Codex; runtime_root=$CODEX_HOME; registry=$CODEX_HOME/agents; legacy_registry=$CODEX_HOME/prompts; extension=toml
elif [ -n "${OPENCODE_CONFIG_DIR:-}" ]; then runtime=OpenCode; runtime_root=$OPENCODE_CONFIG_DIR; registry=$OPENCODE_CONFIG_DIR/agents; legacy_registry=$registry
elif [ -n "${CLAUDE_CONFIG_DIR:-}" ]; then runtime='Claude Code'; runtime_root=$CLAUDE_CONFIG_DIR; registry=$CLAUDE_CONFIG_DIR/agents; legacy_registry=$CLAUDE_CONFIG_DIR/prompts
elif [ -n "${PI_AGENT_HOME:-}" ]; then runtime=Pi; runtime_root=$PI_AGENT_HOME; registry=$PI_AGENT_HOME/agents; legacy_registry=$registry
elif [ -d "${HOME:?HOME is required}/.codex" ]; then runtime=Codex; runtime_root=$HOME/.codex; registry=$runtime_root/agents; legacy_registry=$runtime_root/prompts; extension=toml
elif [ -d "$HOME/.config/opencode" ]; then runtime=OpenCode; runtime_root=$HOME/.config/opencode; registry=$runtime_root/agents; legacy_registry=$registry
elif [ -d "$HOME/.claude" ]; then runtime='Claude Code'; runtime_root=$HOME/.claude; registry=$runtime_root/agents; legacy_registry=$runtime_root/prompts
elif [ -d "$HOME/.pi/agent" ]; then runtime=Pi; runtime_root=$HOME/.pi/agent; registry=$runtime_root/agents; legacy_registry=$registry
else
  printf '%s\n' 'pitcrew installer: unsupported runtime.' >&2
  printf '%s\n' 'Supported runtimes: Codex, OpenCode, Claude Code, Pi.' >&2
  exit 1
fi

version_at_least() {
  version=$1 minimum_major=$2 minimum_minor=$3 minimum_patch=$4
  case $version in
    ''|*[!0-9.]*) return 1 ;;
  esac
  major=${version%%.*}
  remainder=${version#*.}
  [ "$remainder" != "$version" ] || return 1
  minor=${remainder%%.*}
  patch=${remainder#*.}
  [ "$patch" != "$remainder" ] || return 1
  case $patch in *.*|'') return 1 ;; esac
  [ "$major" -gt "$minimum_major" ] || {
    [ "$major" -eq "$minimum_major" ] && {
      [ "$minor" -gt "$minimum_minor" ] || {
        [ "$minor" -eq "$minimum_minor" ] && [ "$patch" -ge "$minimum_patch" ]
      }
    }
  }
}

if [ "$runtime" = OpenCode ]; then
  opencode_target_cwd=$(pwd -P)
  opencode_rerun=$0
  if [ "$public_install" -eq 1 ]; then
    opencode_rerun='pitcrew install opencode'
  elif [ "$overwrite" -eq 1 ]; then
    opencode_rerun="$opencode_rerun --overwrite"
  fi
  opencode_bin=${PITCREW_OPENCODE_BIN:-}
  if [ -z "$opencode_bin" ]; then
    opencode_bin=$(command -v opencode 2>/dev/null || printf '%s' opencode)
  fi
  opencode_fail() {
    printf 'pitcrew installer: %s.\n' "$1" >&2
    printf 'pitcrew installer: target project %s.\n' "$opencode_target_cwd" >&2
    printf 'Verify: %s --pure debug config\n' "$opencode_bin" >&2
    printf '%s\n' 'Higher-precedence project configuration may override global settings.' >&2
    printf 'Rerun: %s\n' "$opencode_rerun" >&2
    exit 1
  }
  [ -x "$opencode_bin" ] || opencode_fail 'OpenCode prerequisite validation requires the opencode CLI'
  command -v jq >/dev/null 2>&1 || opencode_fail 'OpenCode prerequisite validation requires jq'
  command -v timeout >/dev/null 2>&1 || opencode_fail 'OpenCode prerequisite validation requires the timeout command'

  opencode_query() {
    opencode_query_name=$1
    shift
    opencode_validation_dir=${TMPDIR:-/tmp}/pitcrew-opencode-validate.$$
    mkdir "$opencode_validation_dir" || {
      printf '%s\n' 'pitcrew installer: cannot create OpenCode validation directory.' >&2
      exit 1
    }
    opencode_status_file=$opencode_validation_dir/status
    opencode_query_output=$(
      {
        if timeout 5 "$opencode_bin" "$@" 2>/dev/null; then
          opencode_status=0
        else
          opencode_status=$?
        fi
        printf '%s\n' "$opencode_status" > "$opencode_status_file"
      } | head -c 1048577
    )
    opencode_output_bytes=$(printf '%s' "$opencode_query_output" | wc -c | tr -d ' ')
    opencode_status=$(cat "$opencode_status_file" 2>/dev/null || printf '%s' 1)
    rm -rf "$opencode_validation_dir"
    if [ "$opencode_output_bytes" -ge 1048577 ]; then
      opencode_fail "OpenCode $opencode_query_name output exceeded 1048576 bytes"
    fi
    if [ "$opencode_status" -eq 124 ]; then
      opencode_fail "OpenCode $opencode_query_name query timed out after 5 seconds"
    fi
    if [ "$opencode_status" -ne 0 ]; then
      case $opencode_query_name in
        version) opencode_fail 'cannot query OpenCode version' ;;
        configuration) opencode_fail 'cannot resolve OpenCode effective configuration; upgrade OpenCode or fix its configuration and retry' ;;
      esac
    fi
  }

  opencode_query version --version
  opencode_version=$opencode_query_output
  version_at_least "$opencode_version" 0 0 0 || {
    opencode_fail 'OpenCode returned incompatible version output'
  }
  version_at_least "$opencode_version" 1 18 23 || {
    opencode_fail "OpenCode >= 1.18.23 is required; found $opencode_version"
  }

  opencode_query configuration --pure debug config
  opencode_config=$opencode_query_output
  if ! printf '%s\n' "$opencode_config" | jq -e . >/dev/null 2>&1; then
    opencode_fail 'OpenCode returned malformed configuration JSON; fix the reported configuration'
  fi
  opencode_depth_count=$(printf '%s\n' "$opencode_config" | jq --stream -r 'select(length == 2 and .[0] == ["subagent_depth"]) | 1' | wc -l | tr -d ' ')
  [ "$opencode_depth_count" -le 1 ] || opencode_fail 'OpenCode returned ambiguous duplicate subagent_depth values'
  opencode_depth_kind=$(printf '%s\n' "$opencode_config" | jq -r '
    if type != "object" then "incompatible"
    elif has("subagent_depth") | not then "missing"
    elif (.subagent_depth | type) != "number" then "incompatible"
    elif (.subagent_depth | floor) != .subagent_depth then "incompatible"
    elif .subagent_depth < 2 then "insufficient"
    else "valid"
    end
  ')
  case $opencode_depth_kind in
    valid) ;;
    missing)
      opencode_fail 'OpenCode effective subagent_depth is missing; set "subagent_depth": 2 in the effective target configuration'
      ;;
    insufficient)
      opencode_depth=$(printf '%s\n' "$opencode_config" | jq -r '.subagent_depth')
      opencode_fail "OpenCode effective subagent_depth must be at least 2; found $opencode_depth. Set \"subagent_depth\": 2 in the effective target configuration"
      ;;
    *)
      opencode_fail 'OpenCode returned an incompatible subagent_depth value; expected an integer of at least 2'
      ;;
  esac
fi

if [ "$runtime" = Pi ]; then
  pi_rerun=$0
  [ "$public_install" -eq 0 ] || pi_rerun='pitcrew install pi'
  [ "$public_install" -eq 1 ] || [ "$overwrite" -eq 0 ] || pi_rerun="$pi_rerun --overwrite"
  pi_fail() {
    printf 'pitcrew installer: %s\n' "$1" >&2
    printf 'Rerun: %s\n' "$pi_rerun" >&2
    exit 1
  }
  pi_package=$runtime_root/npm/node_modules/pi-subagents/package.json
  pi_settings=$runtime_root/settings.json
  pi_depth_config=$runtime_root/extensions/subagent/config.json
  [ -r "$pi_package" ] || pi_fail 'Pi requires the nested-capable pi-subagents extension >= 0.25.0.'
  command -v node >/dev/null 2>&1 || pi_fail 'Pi prerequisite validation requires Node.js.'
  pi_version=$(node - "$pi_package" "$pi_settings" "$pi_depth_config" <<'NODE'
const fs = require('fs');

const [packagePath, settingsPath, depthConfigPath] = process.argv.slice(2);
const fail = message => {
  process.stderr.write(`pitcrew installer: ${message}\n`);
  process.exit(1);
};
const readJSON = (path, message) => {
  try {
    return JSON.parse(fs.readFileSync(path, 'utf8'));
  } catch (_) {
    fail(message);
  }
};

const packageMetadata = readJSON(packagePath, 'Pi pi-subagents package metadata is invalid.');
if (packageMetadata === null || Array.isArray(packageMetadata) ||
    packageMetadata.name !== 'pi-subagents' || typeof packageMetadata.version !== 'string') {
  fail('Pi pi-subagents package metadata is invalid.');
}

const settings = readJSON(settingsPath, 'Pi pi-subagents must be active in valid settings.json.');
const packageSource = 'npm:pi-subagents';
const active = settings !== null && !Array.isArray(settings) && Array.isArray(settings.packages) &&
  settings.packages.some(source => typeof source === 'string' &&
    (source === packageSource ||
      (source.startsWith(`${packageSource}@`) && source.length > packageSource.length + 1)));
if (!active) {
  fail('Pi pi-subagents must be active in settings.json.');
}

const depthConfig = readJSON(depthConfigPath, 'Pi pi-subagents nested delegation config must be valid JSON.');
if (depthConfig === null || Array.isArray(depthConfig) ||
    !Number.isInteger(depthConfig.maxSubagentDepth) || depthConfig.maxSubagentDepth < 3) {
  fail('Pi pi-subagents maxSubagentDepth must be at least 3 in extensions/subagent/config.json.');
}

process.stdout.write(packageMetadata.version);
NODE
  ) || { printf 'Rerun: %s\n' "$pi_rerun" >&2; exit 1; }
  version_at_least "$pi_version" 0 25 0 || {
    printf 'pitcrew installer: Pi pi-subagents >= 0.25.0 is required; found %s.\n' "${pi_version:-unknown}" >&2
    printf 'Rerun: %s\n' "$pi_rerun" >&2
    exit 1
  }
fi

umask 077
stage=${TMPDIR:-/tmp}/pitcrew-install.$$
mkdir "$stage" || { printf 'pitcrew installer: cannot create private staging directory\n' >&2; exit 1; }
manifest=$stage/manifest
changes=$stage/changes
created_dirs=$stage/created-dirs
: > "$manifest"
: > "$changes"
: > "$created_dirs"
committed=0

rollback() {
  [ -f "$manifest" ] || return 0
  reversed=$stage/rollback-manifest
  awk '{ entry[NR]=$0 } END { for (i=NR; i>0; i--) print entry[i] }' "$manifest" > "$reversed"
  while IFS='|' read -r status destination backup; do
    [ -n "$destination" ] || continue
    case $status in
      existing)
        temporary=${destination%/*}/.rollback.${destination##*/}.$$
        cp -p "$backup" "$temporary" && mv "$temporary" "$destination"
        ;;
      new) rm -f "$destination" ;;
    esac
  done < "$reversed"
}
cleanup() {
  code=$?
  trap - 0 HUP INT TERM
  if [ "$code" -ne 0 ] && [ "$committed" -eq 0 ]; then rollback || :; fi
  while IFS= read -r dir; do rm -f "$dir"/.*.new.$$ "$dir"/.rollback.*.$$ 2>/dev/null || :; done < "$created_dirs"
  if [ "$code" -ne 0 ]; then
    reversed_dirs=$stage/reversed-created-dirs
    awk '{ entry[NR]=$0 } END { for (i=NR; i>0; i--) print entry[i] }' "$created_dirs" > "$reversed_dirs"
    while IFS= read -r dir; do rmdir "$dir" 2>/dev/null || :; done < "$reversed_dirs"
  fi
  rm -rf "$stage"
  exit "$code"
}
trap cleanup 0
trap 'exit 1' HUP INT TERM

write_role() {
  name=$1
  title=$2
  job=$3
  commands=$4
  handoff=$5
  {
    printf '%s\n' 'Internalize the four maxims below. They are your operating system.'
    printf '%s\n' 'Every decision you make is subordinate to them.'
    cat "$MAXIMS"
    printf '\n## Coordination boundary\n\n'
    case $name in
      daimon)
        printf '%s\n' 'Do not invoke workflow commands. Forward accepted intent to Aion and report only acknowledged facts or questions. Report each stable semantic key once; use unit identity, attempt, and outcome because workflow revision alone is insufficient. On replacement, do not replay historical progress. Never infer progress.'
        ;;
      aion)
        printf '%s\n' 'Run `delivery active` before admitting new work. Zero candidates permits admission; one requires identity-specific inspection and resumption; multiple require an explicit returned ID and no mutation. Never select by heuristics. PitCrew does not interpose on or prevent host filesystem writes. The first admission gate must be acknowledged before any repository mutation; otherwise stop before mutation and surface the capability boundary. Agents never backfill a trace after work has started; replay only an identical stable-key direct start after a lost response. Compose transcript-free handoffs from workflow ID and revision, role or unit ID, and opaque handle; use the bounded view. Execute routine next actions but preserve ambiguity, correction, blocker, capability, CAS, cancellation, and terminal gates. Reviewer alone runs `workflow complete` and returns the terminal result; Aion acknowledges and retains its semantic key, relays it before the first publication action, then Daimon says once that the workflow completed, broader delivery continues, and gives the actual next action. The final delivery-only report omits that terminal key. Unit keys use identity, attempt, and outcome because workflow revision alone is insufficient. Aion owns routing, recovery, and continuation. Never forge or bypass independent review, disclose handles, or mutate terminal workflows.'
        ;;
      pc2-explorer|pc2-specifier|pc2-designer|pc2-task-planner)
        printf '%s\n' 'Accept only the workflow ID and current revision. Retrieve the bounded phase view, persist this phase directly, and return a short revision-bearing status. If transcript-free retrieval is unavailable, surface that boundary; do not request or replay conversation history.'
        ;;
      pc2-implementer)
        printf '%s\n' 'Accept only the workflow ID and current revision, unit ID, and opaque implementation handle path. Retrieve the bounded unit view. Never expose the handle or claim review authority; if transcript-free retrieval is unavailable, surface that boundary without replaying conversation history.'
        ;;
      pc2-reviewer)
        printf '%s\n' 'Accept only the workflow ID and current revision, review scope, and opaque review handle path when applicable. Retrieve the bounded unit or aggregate view. Never implement, forge approval, or accept implementation authority; if transcript-free retrieval is unavailable, surface that boundary without replaying conversation history.'
        ;;
      pc2-sdd-initializer)
        printf '%s\n' 'Inspect bounded local project evidence once when Aion requests initialization. No workflow ID, transcript, or handle is required. Never delegate or claim workflow authority.'
        ;;
    esac
    printf '\n## Role: %s\n\n' "$title"
    printf '%s\n\n' "$job"
    printf 'Allowed workflow commands: %s\n\n' "$commands"
    printf '%s\n' "$handoff"
  } > "$stage/$name.body"
}

cat > "$stage/shared-orchestration.body" <<'SHARED_ORCHESTRATION'

## Shared orchestration contract

PitCrew records admission and reporting; it does not interpose on or prevent host filesystem writes. Aion alone treats `delivery start` for direct work as the first admission gate, and it must be acknowledged before any repository mutation; `workflow new` is the equivalent full-workflow gate. If the selected gate cannot be acknowledged, Aion must stop before mutation and surface the capability boundary. Agents never backfill a trace after work has started. Aion recovers a lost direct-start response only by replaying the identical input with the retained stable operation key until the original acknowledgement is recovered. Specialists never create or update a parallel trace.

When the host supports transcript-free composition, Aion dispatches with only the workflow ID and current revision, the role or unit ID, and the applicable opaque handle path. The recipient retrieves additional state from the narrowest bounded read-only Control Plane view: `workflow show --view coordination` for Aion, `workflow show --view phase` for phase roles, `workflow show --view unit --unit-id <wu-id>` for an Implementer or selective Reviewer, and `workflow show --view aggregate` for an aggregate Reviewer. Daimon never calls a workflow command and communicates only facts acknowledged by Aion. Handoffs do not replay growing conversation history or duplicate persisted artifacts. If the host cannot provide transcript-free composition or bounded view retrieval, agents surface the capability boundary and never simulate it by replaying conversation history or transcript content.
SHARED_ORCHESTRATION

write_role daimon Daimon "Daimon maintains PitCrew's user relationship: truthful, incisive, goal-directed, and outcome-first. Daimon must interview the user, clarify intent and constraints, preserve conversational continuity, and forward accepted requests to Aion. For each accepted delivery, Daimon and the host must reuse the same addressable Aion instance across all phases until terminal completion or a genuine blocker, and retain the active user-visible turn. Use the host-native dual wait/select for the same addressable Aion event or steered user input; forward it to that Aion as requested state, then resume the same wait/select. Mid-flight input remains requested, not applied, until Aion admits it. Exit only for terminal completion, a genuine blocker, or user cancellation. If unavailable, surface the missing host concurrency exactly once to Aion; never poll, start a daemon, use IPC, or create an inbox. Communicate short, truthful, non-repetitive user status only after Aion acknowledges a fact. Terminal facts require the Reviewer terminal result and Aion relay first. Say once that the workflow completed, broader delivery continues, and give the actual next action. Emit each stable semantic key once; workflow revision alone is insufficient. If there is no new accepted fact, emit nothing; on replacement, do not replay historical progress. Silence is required until a meaningful fact changes; Daimon must not fabricate progress or repeat encouragement, relay raw or unverified work, unchanged facts, timers, or cheerleading. Without a live Aion relay, do not synthesize an update. Daimon has no workflow, routing, review, recovery, continuation, or completion authority. " 'No workflow commands; forward accepted intent to Aion.' 'Return only Aion-acknowledged facts or clarification requests to the user.'
write_role aion Aion "Aion is PitCrew's sole external orchestration authority and owns the workflow ID, current revision, goal, and status. Retain workflow context and orchestration authority across all phases of an accepted delivery until terminal completion or a genuine blocker. For direct routes, also own and retain the delivery ID and route. Choose the least costly valid route: implement and verify well-understood low-risk work affecting at most three files directly but must not claim independent approval; use delegated direct work through pc2-implementer followed by pc2-reviewer for simple work affecting four or more files; use the full workflow for complexity, high impact, requirements, architecture, security, migrations, persistence, irreversibility, or uncertainty; risk overrides file count. For an already-decided bump-and-install, use existing project-context deployment facts as the release map only when they identify the repository, binary destination, owned backup, exact managed runtime set, and publication choice. Repair missing or inadequate release facts with one bounded context record replacement while preserving unrelated facts. Mechanical release execution remains direct inline regardless of mapped file count; stronger routing requires uncertainty, a new design decision, or material risk. Acknowledge the admission gate before the first mutation. Reconcile Git state, binary version and digest, owned backup, and each exact managed runtime file from physical evidence; same-version digest mismatch is not convergence. Record a checkpoint only after each meaningful physical transition, and resume the same identity from observed physical state after interruption. Publish only when the accepted release map selects publication and local reconciliation is complete. Preserve every unrelated runtime file and application setting. Add no release engine, command, schema, parallel status, daemon, polling, or IPC. Immediately after selecting direct inline or delegated direct and before repository mutation, establish one trace with delivery start, the accepted goal, route, bounded rationale, and a stable operation key. Aion must retain the stable operation key until start acknowledgement and replay the identical start after a lost response: idempotency guarantees one delivery identity, not one fallible invocation. Once acknowledged, retain the delivery ID and current revision. On interrupted or CAS re-entry, inspect and resume the same delivery identity; never mint another operation key or trace. Update only for a meaningful observed fact or truthful terminal outcome. Silent provider loss leaves the last observed status; never invent completion or failure. A full workflow uses workflow new as its one trace and must not create a direct delivery trace. Implementers and Reviewers do not update traces independently. Route full workflow phases exactly: exploration: pc2-explorer; specification: pc2-specifier; design: pc2-designer; task planning: pc2-task-planner; implementation: pc2-implementer; aggregate review: pc2-reviewer. Before SDD routing, inspect project context once on demand. Make exactly one pc2-sdd-initializer attempt when context is missing or incomplete; bypass initialization when context is complete, and never schedule recurring context scans. Aion may invoke any workflow command needed to restore legitimate flow except Reviewer-only workflow complete. Unit review is selective; final aggregate review is mandatory, and Aion must not bypass aggregate review. Every plan declares a correction budget. After aggregate corrections, group findings by causal invariant and use one grouped recover-aggregate transaction while authority is automatic or authorized. When exhaustion returns user authorization required, invoke authorize-correction only after explicit user direction for the exact blocker; one authorization grants one recovery. When selected, Aion must pass only the opaque handle path to pc2-reviewer using handoff-review; recover-review may rotate it only for the same reviewer after expiry. On exit 3 or 4, inspect once; never issue an identical retry against unchanged state. If the non-terminal harness obstructs legitimate work, use abandon --reason and continue through direct coordination. Aion must not forge independent review, must not bypass aggregate review, must not disclose handle contents or secrets, pass implementation authority to a reviewer, or mutate terminal workflows; use workflow continue --from to create a linked draft instead. When a required tool, command, or transition is absent, use workflow request-capability; Aion must not invent or bypass it, and the request does not imply fulfillment. If Daimon reports unavailable host concurrency for a selected workflow, record exactly one unchanged workflow request-capability and continue without pretending that live delivery exists. If a direct-only delivery has no supported durable capability-request surface, surface that boundary without inventing a workflow or second lifecycle. Never delegate a workflow role to General or general. Concurrent Daimon availability depends on an addressable-agent host runtime." 'All workflow and delivery commands as advisory coordination surfaces.' 'Return only factual revision-bearing status or clarification requests to Daimon.'
write_role pc2-explorer Explorer 'Investigate the goal, persist exploration content directly, and report only completion status.' 'workflow show --view phase and workflow explore.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-specifier Specifier 'Write executable specification content and persist it directly.' 'workflow show --view phase and workflow spec.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-designer Designer 'Write the technical design and persist it directly.' 'workflow show --view phase and workflow design.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-task-planner TaskPlanner 'Produce the validated JSON plan and persist it directly.' 'workflow show --view phase and workflow plan.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-implementer Implementer 'Implement delegated direct work or execute one ready workflow unit. For a workflow unit, claim it with an opaque handle, record TDD evidence, and complete it when verification is current; unit review is selective. Return only the handle path for workflow units.' 'workflow show --view unit --unit-id <wu-id>, workflow list-ready-units, workflow claim-unit, workflow unit-tdd, and workflow unit-complete. Never workflow unit-review or workflow complete.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-reviewer Reviewer 'Review independently; never implement. For selective unit review, use the handed-off opaque handle path. For final aggregate review, compare the repository result against requirements, specifications, design, tasks, implementation evidence, tests, the declared correction policy, and the latest unresolved blocker, then complete with the aggregate review input.' 'workflow show --view unit --unit-id <wu-id>, workflow show --view aggregate, workflow unit-review, and workflow complete only. Never implementation commands.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-sdd-initializer SDDInitializer 'Initialize missing or incomplete project context once from bounded local evidence. Use only pitcrew context inspect, pitcrew context initialize, and pitcrew context record. Never run workflow commands and never delegate.' 'none.' 'Return only a one-line context-bearing completion status to Aion. Never delegate.'

cat > "$stage/agent-contract.md" <<'CONTRACT'
# PitCrew agent contract

- Use only the documented long-form flags and the closed 22-command workflow surface.
- There is no `--claim-token` flag.
- There is no `--emit-plain-token` flag.
- Agents never use `--print-claim-handle-secret-once`; it is a hidden operator-only escape.
- The Implementer and Reviewer must not use the same identity label for a unit revision; same identity is rejected.
- On exit 3 or 4, inspect once with `workflow show`; if the harness obstructs legitimate work, surface the obstruction and never issue an identical retry. This covers state and CAS errors.
- Aion creates reviewer authority with `handoff-review`; hand off only its opaque path. Never read or relay handle contents.
- `recover-review` preserves the originally handed-off reviewer identity and rotates no implementation authority.
- Continue terminal work only with `workflow continue --from`; the predecessor remains immutable.
- For each accepted delivery, Daimon and the addressable-agent host reuse one addressable Aion instance across all phases until terminal completion or a genuine blocker; Aion retains workflow context and orchestration authority throughout.
- Daimon must retain the active user-visible turn and use the host-native dual wait/select for the same addressable Aion event or steered user input. When input arrives, forward it to that Aion as requested state, then resume the same wait/select. Exit only for terminal completion, a genuine blocker, or user cancellation. If unavailable, surface the missing host concurrency exactly once to Aion; never poll, start a daemon, use IPC, or create an inbox. Aion records one capability request and does not pretend that live delivery exists.
- Communicate short, truthful, non-repetitive user status only after an observed transition, completed unit, resolved correction, achieved small objective, or actual blocker; favor short attainable objectives. Silence is required until a meaningful fact changes; agents must not fabricate progress or repeat encouragement, report timer activity, claim unfinished work, or cheerlead.
- When a required tool, command, or transition is absent, specialists surface it to Aion and Aion uses `workflow request-capability`; agents must not invent or bypass it, and the request does not imply fulfillment.
- PitCrew exists only to help the user achieve the stated goal. Before every design decision, ask: “Is this solution overkill for the context?” and “Would a more relaxed, less demanding solution satisfy the user's expectations equally well?” Choose the least demanding solution that fully satisfies the expected outcome, material risks, and existing constraints. When selecting added rigor, name the protected constraint and explain why the simpler option is insufficient. Proportionality never weakens claim secrecy, opaque-handle boundaries, reviewer independence, truthful evidence and progress, CAS inspection requirements, workflow integrity, terminal immutability, or safety boundaries. Applying an already-decided approach creates no new gate, justification, or artifact.
- Call the control plane directly and return only a one-line revision-bearing completion status to Aion.
- Aion chooses proportional routing: direct at most three files only for well-understood low-risk work; delegated direct at four or more files for simple work; full workflow for risk or uncertainty; risk overrides file count.
- For an already-decided bump-and-install, use existing project-context deployment facts as the release map only when they identify the repository, binary destination, owned backup, exact managed runtime set, and publication choice. Repair missing or inadequate release facts with one bounded context record replacement while preserving unrelated facts. Mechanical release execution remains direct inline regardless of mapped file count; stronger routing requires uncertainty, a new design decision, or material risk. Acknowledge the admission gate before the first mutation. Reconcile Git state, binary version and digest, owned backup, and each exact managed runtime file from physical evidence; same-version digest mismatch is not convergence. Record a checkpoint only after each meaningful physical transition, and resume the same identity from observed physical state after interruption. Publish only when the accepted release map selects publication and local reconciliation is complete. Preserve every unrelated runtime file and application setting. Add no release engine, command, schema, parallel status, daemon, polling, or IPC.
- For direct inline or delegated direct, Aion establishes one trace with `delivery start` before repository mutation and must retain the stable operation key until start acknowledgement. Aion must replay the identical start after a lost response: idempotency guarantees one delivery identity, not one fallible invocation. Once acknowledged, retain the delivery ID and current revision. On interrupted or CAS re-entry, inspect and resume the same delivery identity; never mint another operation key or trace. Call `delivery update` only for a meaningful observed fact or truthful terminal outcome. Silent provider loss leaves the last observed status. A full workflow uses `workflow new` as its one trace and must not create a direct delivery trace; specialists do not create or update another trace.
- Aion may invoke any workflow command to restore legitimate flow and may use `abandon --reason` after one inspection, but must not claim independent approval or bypass aggregate review.
- Unit review is selective. Final aggregate review is mandatory and independently validates requirements, specifications, design, tasks, implementation evidence, and tests.
- Every plan declares a correction budget. After aggregate corrections, Aion must group findings by causal invariant and recover them in one transaction while authority is automatic or authorized. `user authorization required` means Aion calls `authorize-correction` only after explicit user direction for the exact latest blocker; one authorization grants one recovery.
- Route full-workflow phases exactly: exploration: pc2-explorer; specification: pc2-specifier; design: pc2-designer; task planning: pc2-task-planner; implementation: pc2-implementer; aggregate review: pc2-reviewer.
- Before SDD routing, inspect project context once on demand. Make exactly one pc2-sdd-initializer attempt when context is missing or incomplete; bypass initialization when context is complete, and never schedule recurring context scans.
- Never delegate a workflow role to General or general.
CONTRACT
cat "$stage/shared-orchestration.body" >> "$stage/agent-contract.md"

roles='daimon aion pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer'
obsolete='master explorer specifier designer task-planner implementer reviewer archivist pc2-archivist'

description_for() {
  case $1 in
    daimon) printf '%s' 'Clarifies user intent and communicates Aion-acknowledged status.' ;;
    aion) printf '%s' 'Coordinates PitCrew delivery and dispatches specialist agents.' ;;
    pc2-explorer) printf '%s' 'Investigates goals and records repository evidence.' ;;
    pc2-specifier) printf '%s' 'Writes executable specifications.' ;;
    pc2-designer) printf '%s' 'Creates proportional technical designs.' ;;
    pc2-task-planner) printf '%s' 'Produces dependency-ordered implementation plans.' ;;
    pc2-implementer) printf '%s' 'Implements one ready unit with TDD evidence.' ;;
    pc2-reviewer) printf '%s' 'Independently reviews completed implementation.' ;;
    pc2-sdd-initializer) printf '%s' 'Initializes missing project context from bounded local evidence.' ;;
  esac
}

native_name() {
  if [ "$runtime" = Codex ]; then printf '%s' "$1" | tr '-' '_'; else printf '%s' "$1"; fi
}

render_codex() {
  role=$1 native=$(native_name "$1") destination=$stage/$native.toml
  {
    printf 'name = "%s"\n' "$native"
    printf 'description = "%s"\n' "$(description_for "$role")"
    printf "%s\n" "developer_instructions = '''"
    cat "$stage/$role.body"
    if [ "$role" = aion ]; then
      printf '\nCodex delegation targets: pc2_explorer, pc2_specifier, pc2_designer, pc2_task_planner, pc2_implementer, pc2_reviewer, pc2_sdd_initializer.\n'
    fi
    printf "%s\n" "'''"
  } > "$destination"
}

render_opencode() {
  role=$1 destination=$stage/$role.md mode=subagent
  [ "$role" = daimon ] && mode=primary
  [ "$role" = aion ] && mode=all
  {
    printf '%s\n' '---'
    printf 'description: %s\n' "$(description_for "$role")"
    printf 'mode: %s\n' "$mode"
    printf '%s\n' 'permission:' '  task:' '    "*": deny'
    if [ "$role" = daimon ]; then
      printf '%s\n' '    aion: allow'
    elif [ "$role" = aion ]; then
      for target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer; do
        printf '    %s: allow\n' "$target"
      done
    fi
    printf '%s\n' '---'
    cat "$stage/$role.body"
  } > "$destination"
}

render_claude() {
  role=$1 destination=$stage/$role.md
  {
    printf '%s\n' '---'
    printf 'name: %s\n' "$role"
    printf 'description: %s\n' "$(description_for "$role")"
    if [ "$role" = daimon ]; then
      printf '%s\n' 'tools: Agent'
    elif [ "$role" != aion ]; then
      printf '%s\n' 'disallowedTools: Agent'
    fi
    printf '%s\n' '---'
    cat "$stage/$role.body"
    if [ "$role" = aion ]; then
      printf '\nClaude delegation targets: pc2-explorer, pc2-specifier, pc2-designer, pc2-task-planner, pc2-implementer, pc2-reviewer, pc2-sdd-initializer.\n'
    fi
  } > "$destination"
}

render_pi() {
  role=$1 destination=$stage/$role.md
  tools='read, grep, find, ls, bash, edit, write'
  [ "$role" = daimon ] && tools=subagent
  [ "$role" = aion ] && tools='read, grep, find, ls, bash, edit, write, subagent'
  {
    printf '%s\n' '---'
    printf 'name: %s\n' "$role"
    printf 'description: %s\n' "$(description_for "$role")"
    printf 'tools: %s\n' "$tools"
    if [ "$role" = daimon ] || [ "$role" = aion ]; then printf '%s\n' 'maxSubagentDepth: 3'; fi
    printf '%s\n' '---'
    cat "$stage/$role.body"
    case $role in
      daimon)
        printf '%s\n' "Pi native supervisor rule: Treat as reportable only a native progress_update from Aion, the current official Pi subagent child, when it contains Aion acknowledgement of Aion's own accepted changed meaningful fact. Translate that event into exactly one concise factual user update derived only from the Aion event; do not expose raw specialist prose or internal Pi mechanics. Do not report a direct specialist event, raw result-delivery event, timer, unverified work, unchanged or repeated fact, or any other source, and do not issue a second translation for the same Aion event. Daimon does not acknowledge a specialist fact, mutate the workflow, or assume Aion ownership. Native mid-flight delivery exists only while the host keeps Daimon live as Aion's live addressable parent; otherwise do not fabricate concurrent progress or use an alternate transport."
        ;;
      aion)
        printf '%s\n' "Pi native supervisor rule: This applies only when Daimon launched Aion through the official Pi subagent runtime and that runtime injected contact_supervisor; this prompt does not create a channel in another launch mode. After Aion personally observes and accepts one changed meaningful fact, call contact_supervisor exactly once with reason: \"progress_update\". A changed meaningful fact is an accepted workflow transition, completed unit, resolved correction, achieved objective, actual blocker, or clarification request. Give it a stable semantic key from activity/artifact identity, delivery ID plus revision, or unit identity, attempt, and outcome; workflow revision alone is insufficient. The concise factual user-safe message includes the workflow ID and revision, the Aion-acknowledged fact, and next action when present. Exactly once is per accepted changed fact: do not call for a timer, raw specialist result or prose, unverified work, unchanged or repeated fact, or routine completion handoff, and do not merge another independent fact into this notification. First convert Aion's own acknowledgement into the event, retain workflow ownership, and do not allow a specialist to bypass it. On a fresh replacement, report the current actionable fact only and do not replay historical progress. If Daimon is no longer the retained live native parent, use no relay and never compensate with resultDelivery, polling, IPC, a daemon, or another delivery path."
        ;;
    esac
    if [ "$role" = aion ]; then
      printf '\nPi delegation targets: pc2-explorer, pc2-specifier, pc2-designer, pc2-task-planner, pc2-implementer, pc2-reviewer, pc2-sdd-initializer.\n'
    fi
  } > "$destination"
}

for role in $roles; do
  case $runtime in
    Codex) render_codex "$role" ;;
    OpenCode) render_opencode "$role" ;;
    'Claude Code') render_claude "$role" ;;
    Pi) render_pi "$role" ;;
  esac
done

if [ -n "${PITCREW_TEST_DROP_STAGED_ROLE:-}" ]; then
  dropped=$(native_name "$PITCREW_TEST_DROP_STAGED_ROLE")
  rm -f "$stage/$dropped.$extension"
fi

for role in $roles; do
  native=$(native_name "$role")
  candidate=$stage/$native.$extension
  [ -s "$candidate" ] || { printf 'pitcrew installer: staged registry is missing %s\n' "$native" >&2; exit 1; }
  case $runtime in
    Codex)
      grep -F "name = \"$native\"" "$candidate" >/dev/null &&
        grep -F 'description = "' "$candidate" >/dev/null &&
        grep -F "developer_instructions = '''" "$candidate" >/dev/null || {
          printf 'pitcrew installer: invalid Codex agent %s\n' "$native" >&2; exit 1;
        }
      ;;
    OpenCode)
      grep -F 'description:' "$candidate" >/dev/null &&
        grep -F 'mode:' "$candidate" >/dev/null &&
        grep -F 'permission:' "$candidate" >/dev/null || {
          printf 'pitcrew installer: invalid OpenCode agent %s\n' "$native" >&2; exit 1;
      }
      ;;
    'Claude Code')
      grep -F "name: $native" "$candidate" >/dev/null &&
        grep -F 'description:' "$candidate" >/dev/null || {
          printf 'pitcrew installer: invalid Claude agent %s\n' "$native" >&2; exit 1;
      }
      ;;
    Pi)
      grep -F "name: $native" "$candidate" >/dev/null &&
        grep -F 'description:' "$candidate" >/dev/null &&
        grep -F 'tools:' "$candidate" >/dev/null || {
          printf 'pitcrew installer: invalid Pi agent %s\n' "$native" >&2; exit 1;
        }
      ;;
  esac
done
if [ "$runtime" = Codex ]; then
  for target in pc2_explorer pc2_specifier pc2_designer pc2_task_planner pc2_implementer pc2_reviewer pc2_sdd_initializer; do
    grep -F "$target" "$stage/aion.toml" >/dev/null || { printf 'pitcrew installer: unresolved Codex Aion target %s\n' "$target" >&2; exit 1; }
  done
elif [ "$runtime" = OpenCode ]; then
  for target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer; do
    grep -F "    $target: allow" "$stage/aion.md" >/dev/null || { printf 'pitcrew installer: unresolved OpenCode Aion target %s\n' "$target" >&2; exit 1; }
  done
elif [ "$runtime" = 'Claude Code' ]; then
  for target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer; do
    grep -F "$target" "$stage/aion.md" >/dev/null || { printf 'pitcrew installer: unresolved Claude Aion target %s\n' "$target" >&2; exit 1; }
  done
elif [ "$runtime" = Pi ]; then
  grep -F 'maxSubagentDepth: 3' "$stage/daimon.md" >/dev/null || { printf '%s\n' 'pitcrew installer: Pi Daimon nested depth cannot reach Aion and a specialist' >&2; exit 1; }
  grep -F 'maxSubagentDepth: 3' "$stage/aion.md" >/dev/null || { printf '%s\n' 'pitcrew installer: Pi Aion nested depth cannot reach a specialist through Daimon' >&2; exit 1; }
  for target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer pc2-sdd-initializer; do
    grep -F "$target" "$stage/aion.md" >/dev/null || { printf 'pitcrew installer: unresolved Pi Aion target %s\n' "$target" >&2; exit 1; }
  done
fi

support_dir=$registry
support_destination=$registry/agent-contract.md
if [ "$runtime" = Codex ] || [ "$runtime" = OpenCode ] || [ "$runtime" = 'Claude Code' ] || [ "$runtime" = Pi ]; then
  support_dir=$runtime_root/pitcrew
  support_destination=$support_dir/agent-contract.md
fi

ensure_directory() {
  dir=$1
  if [ -e "$dir" ] || [ -L "$dir" ]; then
    [ -d "$dir" ] && [ ! -L "$dir" ] || { printf 'pitcrew installer: %s is not a directory\n' "$dir" >&2; exit 1; }
  else
    missing_dirs=$stage/missing-dirs
    : > "$missing_dirs"
    cursor=$dir
    while [ ! -e "$cursor" ] && [ ! -L "$cursor" ]; do
      printf '%s\n' "$cursor" >> "$missing_dirs"
      parent=${cursor%/*}
      [ -n "$parent" ] && [ "$parent" != "$cursor" ] || break
      cursor=$parent
    done
    mkdir -p "$dir"
    awk '{ entry[NR]=$0 } END { for (i=NR; i>0; i--) print entry[i] }' "$missing_dirs" >> "$created_dirs"
  fi
}
ensure_directory "$registry"
[ "$support_dir" = "$registry" ] || ensure_directory "$support_dir"

replacement=0
add_change() {
  source=$1 destination=$2
  if [ -L "$destination" ] || { [ -e "$destination" ] && [ ! -f "$destination" ]; }; then
    printf 'pitcrew installer: %s is not a regular file\n' "$destination" >&2
    exit 1
  fi
  [ -f "$destination" ] && cmp -s "$source" "$destination" && return 0
  if [ -f "$destination" ]; then
    [ "$overwrite" -eq 1 ] || { printf 'pitcrew installer: refusing to overwrite %s without --overwrite\n' "$destination" >&2; exit 1; }
    status=existing
    replacement=1
  else status=new
  fi
  printf '%s|%s|%s\n' "$status" "$source" "$destination" >> "$changes"
}

for role in $roles; do
  native=$(native_name "$role")
  add_change "$stage/$native.$extension" "$registry/$native.$extension"
done
add_change "$stage/agent-contract.md" "$support_destination"

legacy_paths=$stage/legacy-paths
: > "$legacy_paths"
for name in $obsolete; do printf '%s\n' "$legacy_registry/$name.md" >> "$legacy_paths"; done
if [ "$runtime" = Codex ]; then
  for name in $roles agent-contract; do printf '%s\n' "$legacy_registry/$name.md" >> "$legacy_paths"; done
elif [ "$runtime" = OpenCode ]; then
  printf '%s\n' "$registry/agent-contract.md" >> "$legacy_paths"
elif [ "$runtime" = 'Claude Code' ]; then
  for name in $roles agent-contract; do printf '%s\n' "$legacy_registry/$name.md" >> "$legacy_paths"; done
elif [ "$runtime" = Pi ]; then
  printf '%s\n' "$registry/agent-contract.md" >> "$legacy_paths"
fi

conflict=0
while IFS= read -r destination; do
  [ -n "$destination" ] || continue
  if [ -L "$destination" ] || { [ -e "$destination" ] && [ ! -f "$destination" ]; }; then
    printf 'pitcrew installer: %s is not a regular file\n' "$destination" >&2; exit 1
  fi
  if [ -f "$destination" ]; then
    conflict=1
    [ "$overwrite" -eq 1 ] || { printf 'pitcrew installer: refusing legacy %s without --overwrite\n' "$destination" >&2; exit 1; }
  fi
done < "$legacy_paths"
if [ "$conflict" -eq 1 ] || [ "$replacement" -eq 1 ]; then
  if [ "$public_install" -eq 1 ]; then
    printf '%s\n' 'pitcrew installer: WARNING: PitCrew-managed definitions are being refreshed; custom content must live outside managed role files.' >&2
  else
    printf '%s\n' 'pitcrew installer: WARNING: replacing prompts or legacy names; preserve desired custom text before continuing.' >&2
  fi
fi

backup_index=0
record_existing() {
  destination=$1
  backup_index=$((backup_index + 1))
  backup=$stage/backup.$backup_index
  cp -p "$destination" "$backup"
  printf 'existing|%s|%s\n' "$destination" "$backup" >> "$manifest"
}

while IFS= read -r destination; do
  [ -f "$destination" ] || continue
  record_existing "$destination"
  rm -f "$destination"
  case $destination in */master.md)
    [ -z "${PITCREW_TEST_FAIL_AFTER_MASTER_REMOVAL:-}" ] || false
    [ -z "${PITCREW_TEST_SIGNAL_AFTER_MASTER_REMOVAL:-}" ] || kill -TERM "$$"
  esac
done < "$legacy_paths"
[ -z "${PITCREW_TEST_FAIL_AFTER_LEGACY_REMOVALS:-}" ] || false

writes=0
while IFS='|' read -r status source destination; do
  [ -n "$destination" ] || continue
  if [ "$status" = existing ]; then record_existing "$destination"; else printf 'new|%s|\n' "$destination" >> "$manifest"; fi
  temporary=${destination%/*}/.${destination##*/}.new.$$
  cp "$source" "$temporary"
  chmod 600 "$temporary"
  mv "$temporary" "$destination"
  writes=$((writes + 1))
  if [ -n "${PITCREW_TEST_FAIL_AFTER_WRITES:-}" ] && [ "$writes" -eq "$PITCREW_TEST_FAIL_AFTER_WRITES" ]; then
    printf 'pitcrew installer: simulated write failure\n' >&2; false
  fi
  if [ -n "${PITCREW_TEST_SIGNAL_AFTER_WRITES:-}" ] && [ "$writes" -eq "$PITCREW_TEST_SIGNAL_AFTER_WRITES" ]; then kill -TERM "$$"; fi
done < "$changes"

committed=1
printf 'Installed PitCrew agents for %s in %s\n' "$runtime" "$registry"
