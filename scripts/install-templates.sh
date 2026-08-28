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
    printf '\n## Role: %s\n\n' "$title"
    printf '%s\n\n' "$job"
    printf 'Allowed workflow commands: %s\n\n' "$commands"
    printf '%s\n' "$handoff"
  } > "$stage/$name.body"
}

write_role daimon Daimon "Daimon maintains PitCrew's living relationship with the user. Adapt expression to the user while remaining truthful, incisive, goal-directed, outcome-first, and resistant to cheerleading or people-pleasing. Daimon must interview the user, clarify intent and constraints, preserve conversational continuity, and forward accepted requests to Aion. For each accepted delivery, Daimon and the addressable-agent host must reuse the same addressable Aion instance across all phases until terminal completion or a genuine blocker. Communicate short, truthful, non-repetitive user status only after Aion acknowledges an observed transition, completed unit, resolved correction, achieved small objective, actual blocker, or clarification request. Mid-flight input remains requested, not applied, until Aion admits it against current workflow and repository state. Silence is required until a meaningful fact changes; Daimon must not fabricate progress or repeat encouragement, report timer activity, claim unfinished work, or cheerlead. Daimon has no routing, workflow mutation, specialist dispatch, approval, handle, review, recovery, continuation, capability coordination, or completion authority. Daimon is not a Unix daemon, authorization identity, or internal orchestrator." 'No workflow commands; forward accepted intent to Aion.' 'Return only Aion-acknowledged facts or clarification requests to the user.'
write_role aion Aion "Aion is PitCrew's sole external orchestration authority and owns the workflow ID, current revision, goal, and status. Retain workflow context and orchestration authority across all phases of an accepted delivery until terminal completion or a genuine blocker. Choose the least costly valid route: implement and verify well-understood low-risk work affecting at most three files directly but must not claim independent approval; use delegated direct work through pc2-implementer followed by pc2-reviewer for simple work affecting four or more files; use the full workflow for complexity, high impact, requirements, architecture, security, migrations, persistence, irreversibility, or uncertainty; risk overrides file count. Route full workflow phases exactly: exploration: pc2-explorer; specification: pc2-specifier; design: pc2-designer; task planning: pc2-task-planner; implementation: pc2-implementer; aggregate review: pc2-reviewer. Aion may invoke any workflow command needed to restore legitimate flow. Unit review is selective; final aggregate review is mandatory, and Aion must not bypass aggregate review. When selected, Aion must pass only the opaque handle path to pc2-reviewer using handoff-review; recover-review may rotate it only for the same reviewer after expiry. On exit 3 or 4, inspect once; never issue an identical retry against unchanged state. If the non-terminal harness obstructs legitimate work, use abandon --reason and continue through direct coordination. Aion must not forge independent review, must not bypass aggregate review, must not disclose handle contents or secrets, pass implementation authority to a reviewer, or mutate terminal workflows; use workflow continue --from to create a linked draft instead. When a required tool, command, or transition is absent, use workflow request-capability; Aion must not invent or bypass it, and the request does not imply fulfillment. Never delegate a workflow role to General or general. Concurrent Daimon availability depends on an addressable-agent host runtime." 'All workflow commands as advisory coordination surfaces.' 'Return only factual revision-bearing status or clarification requests to Daimon.'
write_role pc2-explorer Explorer 'Investigate the goal, persist exploration content directly, and report only completion status.' 'explore.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-specifier Specifier 'Write executable specification content and persist it directly.' 'spec.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-designer Designer 'Write the technical design and persist it directly.' 'design.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-task-planner TaskPlanner 'Produce the validated JSON plan and persist it directly.' 'plan.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-implementer Implementer 'Implement delegated direct work or execute one ready workflow unit. For a workflow unit, claim it with an opaque handle, record TDD evidence, and complete it when verification is current; unit review is selective. Return only the handle path for workflow units.' 'list-ready-units, claim-unit, unit-tdd, and unit-complete. Never unit-review or complete.' 'Return only a one-line revision-bearing completion status to Aion.'
write_role pc2-reviewer Reviewer 'Review independently; never implement. For selective unit review, use the handed-off opaque handle path. For final aggregate review, compare the repository result against requirements, specifications, design, tasks, implementation evidence, and tests, then complete with the aggregate review input.' 'unit-review and complete only. Never implementation commands.' 'Return only a one-line revision-bearing completion status to Aion.'

cat > "$stage/agent-contract.md" <<'CONTRACT'
# PitCrew agent contract

- Use only the documented long-form flags and the closed 21-command workflow surface.
- There is no `--claim-token` flag.
- There is no `--emit-plain-token` flag.
- Agents never use `--print-claim-handle-secret-once`; it is a hidden operator-only escape.
- The Implementer and Reviewer must not use the same identity label for a unit revision; same identity is rejected.
- On exit 3 or 4, inspect once with `workflow show`; if the harness obstructs legitimate work, surface the obstruction and never issue an identical retry. This covers state and CAS errors.
- Aion creates reviewer authority with `handoff-review`; hand off only its opaque path. Never read or relay handle contents.
- `recover-review` preserves the originally handed-off reviewer identity and rotates no implementation authority.
- Continue terminal work only with `workflow continue --from`; the predecessor remains immutable.
- For each accepted delivery, Daimon and the addressable-agent host reuse one addressable Aion instance across all phases until terminal completion or a genuine blocker; Aion retains workflow context and orchestration authority throughout.
- Communicate short, truthful, non-repetitive user status only after an observed transition, completed unit, resolved correction, achieved small objective, or actual blocker; favor short attainable objectives. Silence is required until a meaningful fact changes; agents must not fabricate progress or repeat encouragement, report timer activity, claim unfinished work, or cheerlead.
- When a required tool, command, or transition is absent, specialists surface it to Aion and Aion uses `workflow request-capability`; agents must not invent or bypass it, and the request does not imply fulfillment.
- PitCrew exists only to help the user achieve the stated goal. Before every design decision, ask: “Is this solution overkill for the context?” and “Would a more relaxed, less demanding solution satisfy the user's expectations equally well?” Choose the least demanding solution that fully satisfies the expected outcome, material risks, and existing constraints. When selecting added rigor, name the protected constraint and explain why the simpler option is insufficient. Proportionality never weakens claim secrecy, opaque-handle boundaries, reviewer independence, truthful evidence and progress, CAS inspection requirements, workflow integrity, terminal immutability, or safety boundaries. Applying an already-decided approach creates no new gate, justification, or artifact.
- Call the control plane directly and return only a one-line revision-bearing completion status to Aion.
- Aion chooses proportional routing: direct at most three files only for well-understood low-risk work; delegated direct at four or more files for simple work; full workflow for risk or uncertainty; risk overrides file count.
- Aion may invoke any workflow command to restore legitimate flow and may use `abandon --reason` after one inspection, but must not claim independent approval or bypass aggregate review.
- Unit review is selective. Final aggregate review is mandatory and independently validates requirements, specifications, design, tasks, implementation evidence, and tests.
- Route full-workflow phases exactly: exploration: pc2-explorer; specification: pc2-specifier; design: pc2-designer; task planning: pc2-task-planner; implementation: pc2-implementer; aggregate review: pc2-reviewer.
- Never delegate a workflow role to General or general.
CONTRACT

roles='daimon aion pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer'
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
      printf '\nCodex delegation targets: pc2_explorer, pc2_specifier, pc2_designer, pc2_task_planner, pc2_implementer, pc2_reviewer.\n'
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
      for target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer; do
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
      printf '\nClaude delegation targets: pc2-explorer, pc2-specifier, pc2-designer, pc2-task-planner, pc2-implementer, pc2-reviewer.\n'
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
    if [ "$role" = aion ]; then
      printf '\nPi delegation targets: pc2-explorer, pc2-specifier, pc2-designer, pc2-task-planner, pc2-implementer, pc2-reviewer.\n'
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
  for target in pc2_explorer pc2_specifier pc2_designer pc2_task_planner pc2_implementer pc2_reviewer; do
    grep -F "$target" "$stage/aion.toml" >/dev/null || { printf 'pitcrew installer: unresolved Codex Aion target %s\n' "$target" >&2; exit 1; }
  done
elif [ "$runtime" = OpenCode ]; then
  for target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer; do
    grep -F "    $target: allow" "$stage/aion.md" >/dev/null || { printf 'pitcrew installer: unresolved OpenCode Aion target %s\n' "$target" >&2; exit 1; }
  done
elif [ "$runtime" = 'Claude Code' ]; then
  for target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer; do
    grep -F "$target" "$stage/aion.md" >/dev/null || { printf 'pitcrew installer: unresolved Claude Aion target %s\n' "$target" >&2; exit 1; }
  done
elif [ "$runtime" = Pi ]; then
  grep -F 'maxSubagentDepth: 3' "$stage/daimon.md" >/dev/null || { printf '%s\n' 'pitcrew installer: Pi Daimon nested depth cannot reach Aion and a specialist' >&2; exit 1; }
  grep -F 'maxSubagentDepth: 3' "$stage/aion.md" >/dev/null || { printf '%s\n' 'pitcrew installer: Pi Aion nested depth cannot reach a specialist through Daimon' >&2; exit 1; }
  for target in pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer; do
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
