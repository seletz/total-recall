#!/usr/bin/env bash
set -euo pipefail

# Check that the committed figures of the docs are up to date: render them
# into a temporary directory (tools/charts) and compare the files with
# docs/assets/charts/. A difference means the figures changed and `mise run
# docs:charts` has not been run since; the script then says so and exits 1.
# `mise run docs:charts:check` and the CI call it.
#
# Usage: scripts/charts-check.sh [-db build/demo.db]
#
# Without -db the demo archive is built in a temporary directory.

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SVG_DIR="$ROOT/docs/assets/charts"
DB=""

while [ $# -gt 0 ]; do
  case "$1" in
    -db) DB="${2:?-db needs a path}"; shift 2 ;;
    -db=*) DB="${1#-db=}"; shift ;;
    *) echo "usage: $0 [-db FILE]" >&2; exit 2 ;;
  esac
done

TMP=$(mktemp -d "${TMPDIR:-/tmp}/total-recall-charts-check.XXXXXX")
trap 'rm -rf "$TMP"' EXIT

ARGS=(-out "$TMP/charts")
if [ -n "$DB" ]; then
  ARGS+=(-db "$(cd "$(dirname "$DB")" && pwd)/$(basename "$DB")")
fi
(cd "$ROOT" && go run ./tools/charts "${ARGS[@]}")

if ! diff -r "$SVG_DIR" "$TMP/charts" >"$TMP/charts.diff"; then
  echo "figures in docs/assets/charts/ are stale:" >&2
  sed -e "s#$TMP/charts#<fresh render>#g" "$TMP/charts.diff" | head -n 40 >&2
  echo "run \`mise run docs:charts\` and commit docs/assets/charts/" >&2
  exit 1
fi
echo "figures are up to date"
