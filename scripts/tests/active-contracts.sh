#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
archive_root="$repo_root/openspec/changes/archive"
archive_before=$(mktemp "${TMPDIR:-/tmp}/pitcrew-archive-before.XXXXXX")
archive_after=$(mktemp "${TMPDIR:-/tmp}/pitcrew-archive-after.XXXXXX")
findings=$(mktemp "${TMPDIR:-/tmp}/pitcrew-active-findings.XXXXXX")
trap 'rm -f "$archive_before" "$archive_after" "$findings"' EXIT HUP INT TERM

archive_snapshot() {
  destination=$1
  find "$archive_root" -type f -print | LC_ALL=C sort |
    while IFS= read -r path; do sha256sum "$path"; done >"$destination"
}

active_contract_files() {
  cat <<'EOF'
AGENTS.md
openspec/AGENTS.md
EOF
  find "$repo_root/docs" -type f -print | LC_ALL=C sort |
    while IFS= read -r path; do
      printf '%s\n' "${path#"$repo_root/"}"
    done
  find "$repo_root/openspec/specs" -type f -print | LC_ALL=C sort |
    while IFS= read -r path; do
      printf '%s\n' "${path#"$repo_root/"}"
    done
}

allowed_legacy_line() {
  relative=$1
  line=$2
  case "$relative:$line" in
    docs/contributing.md:*master.md*) return 0 ;;
    *'--actor master'* | *'"actor":"master"'* | *'"actor": "master"'*) return 0 ;;
    *) return 1 ;;
  esac
}

archive_snapshot "$archive_before"

: >"$findings"
active_contract_files | while IFS= read -r relative; do
  grep -nE '(^|[^[:alnum:]_])[Mm]aster([^[:alnum:]_]|$)|master\.md|master revise plan' \
    "$repo_root/$relative" |
    while IFS= read -r line; do
      if ! allowed_legacy_line "$relative" "$line"; then
        printf '%s:%s\n' "$relative" "$line" >>"$findings"
      fi
    done || :
done

if test -s "$findings"; then
  cat "$findings" >&2
  echo "active contracts retain legacy Master vocabulary" >&2
  exit 1
fi

active_contract_files | while IFS= read -r relative; do
  grep -niE 'Daimon (may |MAY |uses |creates |passes |chooses |selects |orchestrates |coordinates |approves |holds |invokes )|Daimon.{0,80}(all workflow commands|proportional routing|sole coordinator)' \
    "$repo_root/$relative" >>"$findings" || :
done

if test -s "$findings"; then
  cat "$findings" >&2
  echo "active contracts grant orchestration authority to Daimon" >&2
  exit 1
fi

archive_snapshot "$archive_after"
if ! cmp -s "$archive_before" "$archive_after"; then
  echo "archived OpenSpec content changed during active-contract validation" >&2
  exit 1
fi

echo "active contracts: clean"
