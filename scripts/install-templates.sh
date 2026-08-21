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
mkdir -p "$target"
stage=$target/.pitcrew-install.$$
mkdir "$stage" || { printf 'pitcrew installer: cannot create staging directory\n' >&2; exit 1; }
manifest=$stage/manifest
: > "$manifest"
committed=0

rollback() {
  [ -f "$manifest" ] || return 0
  while read -r status name; do
    [ -n "$name" ] || continue
    case $status in
      existing) cp "$stage/backup.$name" "$target/$name" ;;
      new) rm -f "$target/$name" ;;
    esac
  done < "$manifest"
}
cleanup() {
  code=$?
  trap - 0 HUP INT TERM
  if [ "$code" -ne 0 ] && [ "$committed" -eq 0 ]; then rollback || :; fi
  rm -f "$target"/.*.md.new.$$ 2>/dev/null || :
  rm -rf "$stage"
  exit "$code"
}
trap cleanup 0 HUP INT TERM

handoff='You do not return your output to the Master. You call the control plane yourself. The Master only learns that you finished.'
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

write_role master Master 'Coordinate the user goal and keep only workflow identity, revision, and short completion statuses in context.' 'new, show, approve-plan, abandon, and optional complete.'
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
- Call the control plane directly and return only a one-line revision-bearing completion status to the Master.
CONTRACT

if [ -e "$target/master.md" ] && ! cmp -s "$stage/master.md" "$target/master.md" && [ "$overwrite" -ne 1 ]; then
  printf 'pitcrew installer: refusing to overwrite %s without --overwrite\n' "$target/master.md" >&2
  exit 1
fi

writes=0
for name in master explorer specifier designer task-planner implementer reviewer archivist agent-contract; do
  source=$stage/$name.md
  destination=$target/$name.md
  if [ -f "$destination" ] && cmp -s "$source" "$destination"; then continue; fi
  if [ -e "$destination" ]; then
    [ -f "$destination" ] || { printf 'pitcrew installer: %s is not a regular file\n' "$destination" >&2; exit 1; }
    cp "$destination" "$stage/backup.$name.md"
    printf 'existing %s.md\n' "$name" >> "$manifest"
  else
    printf 'new %s.md\n' "$name" >> "$manifest"
  fi
  temporary=$target/.$name.md.new.$$
  cp "$source" "$temporary"
  chmod 600 "$temporary"
  mv "$temporary" "$destination"
  writes=$((writes + 1))
  if [ -n "${PITCREW_TEST_FAIL_AFTER_WRITES:-}" ] && [ "$writes" -eq "$PITCREW_TEST_FAIL_AFTER_WRITES" ]; then
    printf 'pitcrew installer: simulated write failure\n' >&2
    false
  fi
done

committed=1
printf 'Installed PitCrew prompts for %s in %s\n' "$runtime" "$target"
