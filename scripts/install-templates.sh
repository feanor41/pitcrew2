#!/bin/sh
set -eu

usage() {
  printf 'Usage: scripts/install-templates.sh [--overwrite]\n' >&2
  exit 2
}

overwrite=0
case $# in
  0) ;;
  1) [ "$1" = "--overwrite" ] || usage; overwrite=1 ;;
  *) usage ;;
esac

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
ROOT=$(CDPATH= cd "$SCRIPT_DIR/.." && pwd)
MAXIMS=$ROOT/MAXIMS.md
[ -r "$MAXIMS" ] || { printf 'pitcrew installer: cannot read %s\n' "$MAXIMS" >&2; exit 1; }

runtime=
target=
if [ -n "${CODEX_HOME:-}" ]; then runtime=Codex; target=$CODEX_HOME/prompts
elif [ -n "${OPENCODE_CONFIG_DIR:-}" ]; then runtime=OpenCode; target=$OPENCODE_CONFIG_DIR/agents
elif [ -n "${CLAUDE_CONFIG_DIR:-}" ]; then runtime='Claude Code'; target=$CLAUDE_CONFIG_DIR/prompts
elif [ -n "${PI_AGENT_HOME:-}" ]; then runtime=Pi; target=$PI_AGENT_HOME/agents
elif [ -d "${HOME:?HOME is required}/.codex" ]; then runtime=Codex; target=$HOME/.codex/prompts
elif [ -d "$HOME/.config/opencode" ]; then runtime=OpenCode; target=$HOME/.config/opencode/agents
elif [ -d "$HOME/.claude" ]; then runtime='Claude Code'; target=$HOME/.claude/prompts
elif [ -d "$HOME/.pi/agent" ]; then runtime=Pi; target=$HOME/.pi/agent/agents
else
  printf '%s\n' 'pitcrew installer: unsupported runtime.' >&2
  printf '%s\n' 'Supported runtimes: Codex, OpenCode, Claude Code, Pi.' >&2
  exit 1
fi

umask 077
target_created=0
if [ -e "$target" ] || [ -L "$target" ]; then
  [ -d "$target" ] && [ ! -L "$target" ] || { printf 'pitcrew installer: %s is not a directory\n' "$target" >&2; exit 1; }
else
  mkdir -p "$target"
  target_created=1
fi
stage=${TMPDIR:-/tmp}/pitcrew-install.$$
mkdir "$stage" || { printf 'pitcrew installer: cannot create private staging directory\n' >&2; exit 1; }
manifest=$stage/manifest
changes=$stage/changes
: > "$manifest"
: > "$changes"
committed=0

rollback() {
  [ -f "$manifest" ] || return 0
  reversed=$stage/rollback-manifest
  awk '{ entry[NR]=$0 } END { for (i=NR; i>0; i--) print entry[i] }' "$manifest" > "$reversed"
  while read -r status name; do
    [ -n "$name" ] || continue
    case $status in
      existing)
        temporary=$target/.rollback.$name.$$
        cp -p "$stage/backup.$name" "$temporary" && mv "$temporary" "$target/$name"
        ;;
      new) rm -f "$target/$name" ;;
    esac
  done < "$reversed"
}
cleanup() {
  code=$?
  trap - 0 HUP INT TERM
  if [ "$code" -ne 0 ] && [ "$committed" -eq 0 ]; then rollback || :; fi
  rm -f "$target"/.*.new.$$ "$target"/.rollback.*.$$ 2>/dev/null || :
  rm -rf "$stage"
  if [ "$code" -ne 0 ] && [ "$target_created" -eq 1 ]; then rmdir "$target" 2>/dev/null || :; fi
  exit "$code"
}
trap cleanup 0
trap 'exit 1' HUP INT TERM

handoff='Persist your output through the control plane yourself. Return only a one-line completion status to Daimon, the sole coordinator and user contact.'
write_role() {
  name=$1
  title=$2
  job=$3
  commands=$4
  {
    printf '%s\n' 'Internalize the four maxims below. They are your operating system.'
    printf '%s\n' 'Every decision you make is subordinate to them.'
    cat "$MAXIMS"
    printf '\n## Role: %s\n\n' "$title"
    printf '%s\n\n' "$job"
    printf 'Allowed workflow commands: %s\n\n' "$commands"
    printf '%s\n' "$handoff"
  } > "$stage/$name.md"
}

write_role daimon Daimon "Daimon is PitCrew's sole bridge between the user and sub-agents. Adapt expression to the user while remaining truthful, incisive, goal-directed, outcome-first, and resistant to cheerleading or people-pleasing. Choose the least costly valid route: implement and verify well-understood low-risk work affecting at most three files directly, but must not claim independent approval; use delegated direct work through pc2-implementer followed by pc2-reviewer for simple work affecting four or more files; use the full workflow for complexity, high impact, requirements, architecture, security, migrations, persistence, irreversibility, or uncertainty; risk overrides file count. In a full workflow route phases exactly: exploration: pc2-explorer; specification: pc2-specifier; design: pc2-designer; task planning: pc2-task-planner; implementation: pc2-implementer; aggregate review: pc2-reviewer. Unit review is selective; final aggregate review is mandatory. Daimon may invoke any workflow command needed to restore legitimate flow. When unit review is selected, Daimon issues independent reviewer authority with handoff-review and passes only that opaque handle path to pc2-reviewer; recover-review may rotate it only for the same reviewer after expiry. On exit 3 or 4, inspect once; never issue an identical retry against unchanged state. If the non-terminal harness obstructs legitimate work, use abandon --reason and continue through direct coordination. Daimon must not forge independent review, must not bypass aggregate review, must not disclose handle contents or secrets, must not pass implementation authority to a reviewer, and must pass only the opaque handle path to pc2-reviewer when unit review is selected. Never mutate terminal workflows; use workflow continue --from to create a linked draft instead. Never delegate a workflow role to General or general. Daimon is not a Unix daemon, authorization identity, or internal orchestrator." 'All workflow commands as advisory coordination surfaces.'
write_role pc2-explorer Explorer 'Investigate the goal, persist exploration content directly, and report only completion status.' 'explore.'
write_role pc2-specifier Specifier 'Write executable specification content and persist it directly.' 'spec.'
write_role pc2-designer Designer 'Write the technical design and persist it directly.' 'design.'
write_role pc2-task-planner TaskPlanner 'Produce the validated JSON plan and persist it directly.' 'plan.'
write_role pc2-implementer Implementer 'Implement delegated direct work or execute one ready workflow unit. For a workflow unit, claim it with an opaque handle, record TDD evidence, and complete it when verification is current; unit review is selective. Return only the handle path for workflow units.' 'list-ready-units, claim-unit, unit-tdd, and unit-complete. Never unit-review or complete.'
write_role pc2-reviewer Reviewer 'Review independently; never implement. For selective unit review, use the handed-off opaque handle path. For final aggregate review, compare the repository result against requirements, specifications, design, tasks, implementation evidence, and tests, then complete with the aggregate review input.' 'unit-review and complete only. Never implementation commands.'

cat > "$stage/agent-contract.md" <<'CONTRACT'
# PitCrew agent contract

- Use only the documented long-form flags and the closed 19-command workflow surface.
- There is no `--claim-token` flag.
- There is no `--emit-plain-token` flag.
- Agents never use `--print-claim-handle-secret-once`; it is a hidden operator-only escape.
- The Implementer and Reviewer must not use the same identity label for a unit revision; same identity is rejected.
- On exit 3 or 4, inspect once with `workflow show`; if the harness obstructs legitimate work, surface the obstruction and never issue an identical retry. This covers state and CAS errors.
- Daimon creates reviewer authority with `handoff-review`; hand off only its opaque path. Never read or relay handle contents.
- `recover-review` preserves the originally handed-off reviewer identity and rotates no implementation authority.
- Continue terminal work only with `workflow continue --from`; the predecessor remains immutable.
- Call the control plane directly and return only a one-line revision-bearing completion status to Daimon.
- Daimon chooses proportional routing: direct at most three files only for well-understood low-risk work; delegated direct at four or more files for simple work; full workflow for risk or uncertainty; risk overrides file count.
- Daimon may invoke any workflow command to restore legitimate flow and may use `abandon --reason` after one inspection, but must not claim independent approval or bypass aggregate review.
- Unit review is selective. Final aggregate review is mandatory and independently validates requirements, specifications, design, tasks, implementation evidence, and tests.
- Route full-workflow phases exactly: exploration: pc2-explorer; specification: pc2-specifier; design: pc2-designer; task planning: pc2-task-planner; implementation: pc2-implementer; aggregate review: pc2-reviewer.
- Never delegate a workflow role to General or general.
CONTRACT

names='daimon pc2-explorer pc2-specifier pc2-designer pc2-task-planner pc2-implementer pc2-reviewer agent-contract'
obsolete='master explorer specifier designer task-planner implementer reviewer archivist pc2-archivist'
for name in $obsolete $names; do
  destination=$target/$name.md
  if [ -L "$destination" ] || { [ -e "$destination" ] && [ ! -f "$destination" ]; }; then
    printf 'pitcrew installer: %s is not a regular file\n' "$destination" >&2
    exit 1
  fi
done

conflict=0
for name in $obsolete; do
  if [ -f "$target/$name.md" ]; then
    conflict=1
    if [ "$overwrite" -ne 1 ]; then
      printf 'pitcrew installer: refusing legacy %s without --overwrite\n' "$target/$name.md" >&2
      exit 1
    fi
  fi
done
for name in $names; do
  if [ -f "$target/$name.md" ] && ! cmp -s "$stage/$name.md" "$target/$name.md"; then
    conflict=1
    if [ "$overwrite" -ne 1 ]; then
      printf 'pitcrew installer: refusing to overwrite %s without --overwrite\n' "$target/$name.md" >&2
      exit 1
    fi
  fi
done
if [ "$conflict" -eq 1 ]; then
  printf '%s\n' 'pitcrew installer: WARNING: replacing prompts or legacy names; preserve desired custom text before continuing.' >&2
fi

for name in $obsolete; do
  if [ -f "$target/$name.md" ]; then cp -p "$target/$name.md" "$stage/backup.$name.md"; fi
done

for name in $names; do
  source=$stage/$name.md
  destination=$target/$name.md
  if [ -f "$destination" ] && cmp -s "$source" "$destination"; then continue; fi
  if [ -f "$destination" ]; then
    cp -p "$destination" "$stage/backup.$name.md"
    status=existing
  else
    status=new
  fi
  printf '%s %s.md\n' "$status" "$name" >> "$changes"
done

for name in $obsolete; do
  if [ -f "$target/$name.md" ]; then
    printf 'existing %s.md\n' "$name" >> "$manifest"
    rm -f "$target/$name.md"
    if [ "$name" = master ] && [ -n "${PITCREW_TEST_FAIL_AFTER_MASTER_REMOVAL:-}" ]; then false; fi
    if [ "$name" = master ] && [ -n "${PITCREW_TEST_SIGNAL_AFTER_MASTER_REMOVAL:-}" ]; then kill -TERM "$$"; fi
  fi
done
if [ -n "${PITCREW_TEST_FAIL_AFTER_LEGACY_REMOVALS:-}" ]; then false; fi

writes=0
while read -r status filename; do
  [ -n "$filename" ] || continue
  name=${filename%.md}
  source=$stage/$name.md
  destination=$target/$filename
  printf '%s %s\n' "$status" "$filename" >> "$manifest"
  temporary=$target/.$name.md.new.$$
  cp "$source" "$temporary"
  chmod 600 "$temporary"
  mv "$temporary" "$destination"
  writes=$((writes + 1))
  if [ -n "${PITCREW_TEST_FAIL_AFTER_WRITES:-}" ] && [ "$writes" -eq "$PITCREW_TEST_FAIL_AFTER_WRITES" ]; then
    printf 'pitcrew installer: simulated write failure\n' >&2
    false
  fi
  if [ -n "${PITCREW_TEST_SIGNAL_AFTER_WRITES:-}" ] && [ "$writes" -eq "$PITCREW_TEST_SIGNAL_AFTER_WRITES" ]; then kill -TERM "$$"; fi
done < "$changes"

committed=1
printf 'Installed PitCrew prompts for %s in %s\n' "$runtime" "$target"
