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

write_role daimon Daimon "Daimon is PitCrew's sole bridge between the user and sub-agents. Adapt expression to the user while remaining truthful, incisive, goal-directed, outcome-first, and resistant to cheerleading or people-pleasing. Coordinate the harness so process serves the user's desired result. Daimon is not a Unix daemon, authorization identity, or internal orchestrator." 'new, show, approve-plan, abandon, and optional complete.'
write_role explorer Explorer 'Investigate the goal, persist exploration content directly, and report only completion status.' 'explore.'
write_role specifier Specifier 'Write executable specification content and persist it directly.' 'spec.'
write_role designer Designer 'Write the technical design and persist it directly.' 'design.'
write_role task-planner TaskPlanner 'Produce the validated JSON plan and persist it directly.' 'plan.'
write_role implementer Implementer 'List ready units, claim one with an opaque handle, record TDD evidence, and complete only after approval. Return only the handle path.' 'list-ready-units, claim-unit, unit-tdd, and unit-complete. Never unit-review.'
write_role reviewer Reviewer 'Review one unit independently using the handed-off opaque handle path.' 'unit-review only. Never implementation commands.'
write_role archivist Archivist 'Complete a workflow only after every unit is done and the aggregate is ready.' 'complete only.'

cat > "$stage/agent-contract.md" <<'CONTRACT'
# PitCrew agent contract

- Use only the documented long-form flags and the closed 16-command workflow surface.
- There is no `--claim-token` flag.
- There is no `--emit-plain-token` flag.
- Agents never use `--print-claim-handle-secret-once`; it is a hidden operator-only escape.
- The Implementer and Reviewer must not use the same identity label for a unit revision; same identity is rejected.
- Never retry blindly after a CAS error. Run `workflow show`, inspect the new revision, and decide explicitly.
- Hand off only an opaque handle path. Never read or relay handle contents.
- Call the control plane directly and return only a one-line revision-bearing completion status to Daimon.
CONTRACT

names='daimon explorer specifier designer task-planner implementer reviewer archivist agent-contract'
for name in master $names; do
  destination=$target/$name.md
  if [ -L "$destination" ] || { [ -e "$destination" ] && [ ! -f "$destination" ]; }; then
    printf 'pitcrew installer: %s is not a regular file\n' "$destination" >&2
    exit 1
  fi
done

conflict=0
if [ -f "$target/master.md" ]; then
  conflict=1
  if [ "$overwrite" -ne 1 ]; then
    printf 'pitcrew installer: refusing legacy %s without --overwrite\n' "$target/master.md" >&2
    exit 1
  fi
fi
if [ -f "$target/daimon.md" ] && ! cmp -s "$stage/daimon.md" "$target/daimon.md"; then
  conflict=1
  if [ "$overwrite" -ne 1 ]; then
    printf 'pitcrew installer: refusing to overwrite %s without --overwrite\n' "$target/daimon.md" >&2
    exit 1
  fi
fi
if [ "$conflict" -eq 1 ]; then
  printf '%s\n' 'pitcrew installer: WARNING: replacing coordinator prompt; preserve desired custom text before continuing.' >&2
fi

if [ -f "$target/master.md" ]; then
  cp -p "$target/master.md" "$stage/backup.master.md"
fi

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

if [ -f "$target/master.md" ]; then
  printf 'existing master.md\n' >> "$manifest"
  rm -f "$target/master.md"
  if [ -n "${PITCREW_TEST_FAIL_AFTER_MASTER_REMOVAL:-}" ]; then false; fi
  if [ -n "${PITCREW_TEST_SIGNAL_AFTER_MASTER_REMOVAL:-}" ]; then kill -TERM "$$"; fi
fi

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
