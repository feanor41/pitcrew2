#!/bin/sh

set -eu

source_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixture_root=$(mktemp -d "${TMPDIR:-/tmp}/pitcrew-active-contracts.XXXXXX")
trap 'rm -rf "$fixture_root"' EXIT HUP INT TERM

mkdir -p "$fixture_root/scripts/tests" "$fixture_root/docs" \
  "$fixture_root/openspec/changes/archive" \
  "$fixture_root/openspec/specs/cli-surface"
cp "$source_root/scripts/tests/active-contracts.sh" "$fixture_root/scripts/tests/"

printf '%s\n' 'Daimon interviews the user and communicates Aion-acknowledged facts.' >"$fixture_root/AGENTS.md"
mkdir -p "$fixture_root/openspec"
printf '%s\n' 'Daimon preserves conversational continuity.' >"$fixture_root/openspec/AGENTS.md"
printf '%s\n' 'Daimon documentation' 'pitcrew workflow show --actor master' \
  >"$fixture_root/docs/guide.md"
printf '%s\n' 'Aion canonical specification' \
  >"$fixture_root/openspec/specs/cli-surface/spec.md"
printf '%s\n' 'immutable archive' \
  >"$fixture_root/openspec/changes/archive/spec.md"

if ! sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "clean active contracts were rejected" >&2
  exit 1
fi

printf '%s\n' 'Forbidden Master canonical specification' >"$fixture_root/openspec/specs/cli-surface/spec.md"
if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "active canonical specification escaped validation" >&2
  exit 1
fi
printf '%s\n' 'Aion canonical specification' >"$fixture_root/openspec/specs/cli-surface/spec.md"

printf '%s\n' 'Daimon coordinates workflow recovery.' >"$fixture_root/AGENTS.md"
if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "forbidden Daimon orchestration grant was not rejected" >&2
  exit 1
fi
printf '%s\n' 'Daimon interviews the user and communicates Aion-acknowledged facts.' >"$fixture_root/AGENTS.md"

printf '%s\n' 'Forbidden Master documentation' >"$fixture_root/docs/guide.md"
if sh "$fixture_root/scripts/tests/active-contracts.sh" >/dev/null 2>&1; then
  echo "forbidden documentation vocabulary was not rejected" >&2
  exit 1
fi

echo "active contract fixtures: passed"
