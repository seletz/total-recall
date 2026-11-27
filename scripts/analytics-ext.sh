#!/usr/bin/env bash
set -euo pipefail

# Download the DuckDB sqlite_scanner extension that total-recall-analytics
# embeds, for the DuckDB version and platforms pinned in
# analytics/internal/duck/ext.lock, and verify both checksums of the lock.
# The binary loads the extension from its own embedded copy and never from the
# network; this script is the only place that talks to extensions.duckdb.org,
# at build time.
#
# Usage:
#   scripts/analytics-ext.sh                     # the platform of `go env`
#   scripts/analytics-ext.sh osx_arm64 linux_amd64
#   scripts/analytics-ext.sh --all               # every platform of the lock
#   scripts/analytics-ext.sh --update-lock VERSION
#       download every platform for DuckDB VERSION (e.g. v2.0.1) and rewrite
#       the lock; for a bump of github.com/duckdb/duckdb-go/v2.
#
# Files land in analytics/internal/duck/ext/<platform>/ (gitignored).

ROOT=$(cd "$(dirname "$0")/.." && pwd)
LOCK="$ROOT/analytics/internal/duck/ext.lock"
DEST="$ROOT/analytics/internal/duck/ext"
NAME=sqlite_scanner
BASE=${DUCKDB_EXTENSIONS_URL:-https://extensions.duckdb.org}
LOCK_PLATFORMS="osx_arm64 osx_amd64 linux_amd64 linux_arm64"

sha() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

# fetch VERSION PLATFORM: download into DEST/PLATFORM, print "gz-sha ext-sha".
fetch() {
  local version=$1 platform=$2 dir tmp
  dir="$DEST/$platform"
  mkdir -p "$dir"
  tmp=$(mktemp -d)
  curl -sSfL --retry 3 -o "$tmp/$NAME.duckdb_extension.gz" "$BASE/$version/$platform/$NAME.duckdb_extension.gz"
  gzip -dc "$tmp/$NAME.duckdb_extension.gz" >"$tmp/$NAME.duckdb_extension"
  echo "$(sha "$tmp/$NAME.duckdb_extension.gz") $(sha "$tmp/$NAME.duckdb_extension")"
  mv "$tmp/$NAME.duckdb_extension.gz" "$dir/$NAME.duckdb_extension.gz"
  rm -rf "$tmp"
}

go_platform() {
  local os arch
  os=${GOOS:-$(go env GOOS)}
  arch=${GOARCH:-$(go env GOARCH)}
  case "$os" in darwin) os=osx ;; esac
  echo "${os}_${arch}"
}

if [ "${1:-}" = "--update-lock" ]; then
  version=${2:?usage: --update-lock VERSION}
  {
    echo "# DuckDB version bundled by github.com/duckdb/duckdb-go/v2 and the"
    echo "# sqlite_scanner extension total-recall-analytics embeds for it:"
    echo "# <extension> <platform> <sha256 of the .gz as downloaded> <sha256 of the extension>"
    echo "# Written by scripts/analytics-ext.sh --update-lock; a test checks the version"
    echo "# against the linked library."
    echo "duckdb $version"
    for p in $LOCK_PLATFORMS; do
      echo "$NAME $p $(fetch "$version" "$p")"
    done
  } >"$LOCK.new"
  mv "$LOCK.new" "$LOCK"
  echo "updated $LOCK"
  exit 0
fi

version=$(awk '$1 == "duckdb" { print $2 }' "$LOCK")
[ -n "$version" ] || { echo "no duckdb version in $LOCK" >&2; exit 1; }
if [ "${1:-}" = "--all" ]; then
  set -- $(awk -v n="$NAME" '$1 == n { print $2 }' "$LOCK")
elif [ $# -eq 0 ]; then
  set -- "$(go_platform)"
fi

status=0
for platform in "$@"; do
  want=$(awk -v n="$NAME" -v p="$platform" '$1 == n && $2 == p { print $3 " " $4 }' "$LOCK")
  if [ -z "$want" ]; then
    echo "$platform: not in $LOCK" >&2
    status=1
    continue
  fi
  file="$DEST/$platform/$NAME.duckdb_extension.gz"
  if [ -f "$file" ] && [ "$(sha "$file")" = "${want%% *}" ]; then
    echo "$platform: $NAME $version present"
    continue
  fi
  got=$(fetch "$version" "$platform")
  if [ "$got" != "$want" ]; then
    rm -f "$file"
    echo "$platform: checksum mismatch for $NAME $version: got $got, want $want" >&2
    status=1
    continue
  fi
  echo "$platform: $NAME $version downloaded and verified"
done
exit $status
