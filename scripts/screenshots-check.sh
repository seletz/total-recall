#!/usr/bin/env bash
set -euo pipefail

# Check that the committed TUI screenshots are up to date: render every shot
# into a temporary directory (tools/screenshots) and compare the images with
# docs/assets/screenshots/ and the text frames with the goldens in
# internal/screenshot/testdata/frames/. A difference means the TUI changed
# and `mise run docs:screenshots` has not been run since; the script then
# says so and exits 1. `mise run docs:screenshots:check` and the CI call it.
#
# Usage: scripts/screenshots-check.sh [-db build/demo.db] [--goldens-only]
#
# Without -db the demo archive is built in the temporary directory.
# --goldens-only compares only the text frames and skips the images.

ROOT=$(cd "$(dirname "$0")/.." && pwd)
PNG_DIR="$ROOT/docs/assets/screenshots"
GOLDEN_DIR="$ROOT/internal/screenshot/testdata/frames"
DB=""
GOLDENS_ONLY=0

while [ $# -gt 0 ]; do
  case "$1" in
    -db) DB="${2:?-db needs a path}"; shift 2 ;;
    -db=*) DB="${1#-db=}"; shift ;;
    --goldens-only) GOLDENS_ONLY=1; shift ;;
    *) echo "usage: $0 [-db FILE] [--goldens-only]" >&2; exit 2 ;;
  esac
done

TMP=$(mktemp -d "${TMPDIR:-/tmp}/total-recall-screenshots-check.XXXXXX")
trap 'rm -rf "$TMP"' EXIT

ARGS=(-work "$TMP/work" -out "$TMP/png" -golden "$TMP/golden")
if [ -n "$DB" ]; then
  ARGS+=(-db "$(cd "$(dirname "$DB")" && pwd)/$(basename "$DB")")
fi
if [ "$GOLDENS_ONLY" = 1 ]; then
  ARGS+=(-no-png)
fi
(cd "$ROOT" && go run ./tools/screenshots "${ARGS[@]}")

stale=0
if ! diff -r "$GOLDEN_DIR" "$TMP/golden" >"$TMP/golden.diff"; then
  echo "goldens in internal/screenshot/testdata/frames/ are stale:" >&2
  sed -e "s#$TMP/golden#<fresh render>#g" "$TMP/golden.diff" | head -n 40 >&2
  stale=1
fi
if [ "$GOLDENS_ONLY" = 0 ]; then
  if ! diff -rq "$PNG_DIR" "$TMP/png" >"$TMP/png.diff"; then
    echo "screenshots in docs/assets/screenshots/ are stale:" >&2
    sed -e "s#$TMP/png#<fresh render>#g" "$TMP/png.diff" >&2
    stale=1
  fi
fi
if [ "$stale" -ne 0 ]; then
  echo "run \`mise run docs:screenshots\` and commit docs/assets/screenshots/ and internal/screenshot/testdata/frames/" >&2
  exit 1
fi
if [ "$GOLDENS_ONLY" = 1 ]; then
  echo "goldens are up to date (images not checked)"
else
  echo "screenshots and goldens are up to date"
fi
