#!/bin/sh
set -eu

source_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
fixture_parent=$(mktemp -d "${TMPDIR:-/tmp}/pitcrew-active-contracts.XXXXXX")
fixture_root=$fixture_parent/source
cache_root=$fixture_parent/gocache
archive_before=$fixture_parent/archive-before
archive_after=$fixture_parent/archive-after
trap 'rm -rf "$fixture_parent"' EXIT HUP INT TERM
mkdir -p "$fixture_root" "$cache_root"

# Exercise the public gate in a self-contained source tree. Never mutate or
# consult the source checkout after this bounded copy is made.
tar -C "$source_root" \
  --exclude='./.git' \
  --exclude='./.tmp-worktrees' \
  --exclude='./.cache' \
  --exclude='*.test' \
  -cf - . | tar -C "$fixture_root" -xf -

archive_snapshot() {
  destination=$1
  find "$fixture_root/openspec/changes/archive" -type f -print | LC_ALL=C sort |
    while IFS= read -r path; do sha256sum "$path"; done >"$destination"
}

run_gate() {
  (cd "$fixture_root" && GOCACHE="$cache_root" GOFLAGS="${GOFLAGS:-} -buildvcs=false" sh scripts/tests/active-contracts.sh) >/dev/null 2>&1
}

expect_rejection() {
  label=$1
  if run_gate; then
    printf 'active contract mutation escaped validation: %s\n' "$label" >&2
    exit 1
  fi
}

archive_snapshot "$archive_before"
if ! run_gate; then
  echo 'complete copied source was rejected' >&2
  exit 1
fi

# Exact repository selector boundary.
cp "$fixture_root/AGENTS.md" "$fixture_parent/AGENTS.saved"
printf '%s\n' 'Work directly with workflow manuals.' >"$fixture_root/AGENTS.md"
expect_rejection 'changed exact AGENTS selector'
mv "$fixture_parent/AGENTS.saved" "$fixture_root/AGENTS.md"

# Stable role mechanics are owned and tested by the binary contract.
contract=$fixture_root/internal/agentbrief/brief.go
cp "$contract" "$fixture_parent/brief.saved"
sed 's/mutate no workflow or repository state/mutate workflow and repository state/' \
  "$fixture_parent/brief.saved" >"$contract"
expect_rejection 'removed Daimon no-mutation invariant'
mv "$fixture_parent/brief.saved" "$contract"

# Installed role bootstraps and graph are verified through a real standalone
# install, not by duplicating installer prose in this fixture.
installer=$fixture_root/scripts/install-templates.sh
cp "$installer" "$fixture_parent/installer.saved"
sed 's/pitcrew agent brief --role \$name/pitcrew role manual --role \$name/g' \
  "$fixture_parent/installer.saved" >"$installer"
expect_rejection 'removed role-scoped agent brief bootstrap'
mv "$fixture_parent/installer.saved" "$installer"

# The gate also owns concise public runtime-install specification facts.
runtime_spec=$fixture_root/openspec/specs/runtime-install/spec.md
cp "$runtime_spec" "$fixture_parent/runtime-spec.saved"
grep -Fv 'contract_digest' "$fixture_parent/runtime-spec.saved" >"$runtime_spec"
expect_rejection 'removed required runtime-install contract digest fact'
mv "$fixture_parent/runtime-spec.saved" "$runtime_spec"

if ! run_gate; then
  echo 'restored copied source was rejected' >&2
  exit 1
fi
archive_snapshot "$archive_after"
if ! cmp -s "$archive_before" "$archive_after"; then
  echo 'copied archive tree changed during fixture validation' >&2
  exit 1
fi

echo 'active contract fixtures: passed'
