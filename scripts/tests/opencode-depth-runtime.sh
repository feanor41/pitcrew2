#!/bin/sh
set -eu

skip() {
  printf 'SKIP: %s\n' "$*"
  exit 0
}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

version_at_least() {
  version=$1 minimum_major=$2 minimum_minor=$3 minimum_patch=$4
  case $version in ''|*[!0-9.]*) return 1 ;; esac
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

[ "${PITCREW_OPENCODE_DEPTH_PROBE:-}" = 1 ] ||
  skip 'set PITCREW_OPENCODE_DEPTH_PROBE=1 to run the isolated OpenCode depth probe'

opencode_bin=${PITCREW_OPENCODE_BIN:-}
if [ -z "$opencode_bin" ]; then
  opencode_bin=$(command -v opencode 2>/dev/null || :)
fi
[ -n "$opencode_bin" ] && [ -x "$opencode_bin" ] ||
  skip 'the opencode CLI is unavailable'
command -v timeout >/dev/null 2>&1 || skip 'the timeout command is unavailable'

opencode_version=$("$opencode_bin" --version 2>/dev/null || :)
version_at_least "$opencode_version" 1 18 23 ||
  fail "OpenCode >= 1.18.23 is required; found ${opencode_version:-unknown}"

source_data_home=${XDG_DATA_HOME:-${HOME:?HOME is required}/.local/share}
auth_file=${PITCREW_OPENCODE_AUTH_FILE:-$source_data_home/opencode/auth.json}
environment_credentials=${PITCREW_OPENCODE_DEPTH_ENV_CREDENTIALS:-}
if [ ! -s "$auth_file" ] && [ "$environment_credentials" != 1 ]; then
  skip 'OpenCode credentials are unavailable; log in or set PITCREW_OPENCODE_DEPTH_ENV_CREDENTIALS=1 for provider environment credentials'
fi

probe_root=$(mktemp -d "${TMPDIR:-/tmp}/pitcrew-opencode-depth.XXXXXX") ||
  fail 'cannot create an isolated probe directory'
trap 'rm -rf "$probe_root"' EXIT HUP INT TERM

xdg_config=$probe_root/xdg-config
config=$xdg_config/opencode
project=$probe_root/project
data=$probe_root/data
state=$probe_root/state
cache=$probe_root/cache
home=$probe_root/home
mkdir -p "$config/agents" "$project" "$data/opencode" "$state" "$cache" "$home"
if [ -s "$auth_file" ]; then
  cp "$auth_file" "$data/opencode/auth.json"
  chmod 600 "$data/opencode/auth.json"
fi

cat > "$config/agents/probe-root.md" <<'EOF'
---
description: Starts the isolated OpenCode depth probe.
mode: primary
permission:
  task:
    "*": deny
    probe-aion: allow
---
Invoke probe-aion exactly once. Return its result unchanged. Do not inspect files.
EOF

cat > "$config/agents/probe-aion.md" <<'EOF'
---
description: Reproduces the Aion orchestration level.
mode: all
permission:
  task:
    "*": deny
    probe-leaf: allow
---
Invoke probe-leaf exactly once. Return its result unchanged. Do not inspect files.
EOF

cat > "$config/agents/probe-leaf.md" <<'EOF'
---
description: Reproduces a PitCrew specialist.
mode: subagent
permission:
  read: allow
  task:
    "*": deny
---
Read probe.txt and return its exact single line. Never delegate.
EOF

printf '%s\n' 'DEPTH_PROBE_PAYLOAD=verified' > "$project/probe.txt"

write_config() {
  depth=$1
  if [ -n "$depth" ]; then
    printf '{"default_agent":"probe-root","share":"disabled","subagent_depth":%s}\n' "$depth" > "$config/opencode.json"
  else
    printf '%s\n' '{"default_agent":"probe-root","share":"disabled"}' > "$config/opencode.json"
  fi
}

run_probe() {
  output=$1
  model=${PITCREW_OPENCODE_DEPTH_MODEL:-openai/gpt-5.6-sol}
  (
    cd "$project"
    HOME=$home \
      XDG_CONFIG_HOME=$xdg_config \
      XDG_DATA_HOME=$data \
      XDG_STATE_HOME=$state \
      XDG_CACHE_HOME=$cache \
      OPENCODE_CONFIG_DIR=$config \
      timeout 90 "$opencode_bin" --pure run \
        --agent probe-root --model "$model" --format json --dir "$project" \
        'Invoke probe-aion exactly once and return its result unchanged.'
  ) > "$output" 2>&1 || return $?
}

run_phase() {
  phase=$1 expected=$2 forbidden=$3
  attempt=1
  while [ "$attempt" -le 3 ]; do
    output=$probe_root/$phase-attempt-$attempt.jsonl
    if run_probe "$output" &&
      grep -F "$expected" "$output" >/dev/null &&
      ! grep -F "$forbidden" "$output" >/dev/null; then
      return 0
    fi
    attempt=$((attempt + 1))
  done
  fail "$phase OpenCode probe was inconclusive after 3 attempts of at most 90 seconds each"
}

depth_rejection='Subagent depth limit reached (1)'
depth_payload='DEPTH_PROBE_PAYLOAD=verified'

write_config ''
run_phase default-depth "$depth_rejection" "$depth_payload"

write_config 2
run_phase depth-2 "$depth_payload" "$depth_rejection"

printf '%s\n' 'PASS: OpenCode default depth rejected Daimon -> Aion -> specialist and global subagent_depth 2 allowed it'
