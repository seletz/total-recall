#!/usr/bin/env bash
# Start the debug build of the app against the synthetic demo archive, with
# the clock of the search screenshots (2026-11-02 10:00): extra arguments
# go to the app, e.g. --view activity --now 2026-06-07T12:00.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DEMO_HOME=$(bash "$ROOT/macos/scripts/demo-home.sh")
cd "$ROOT/macos"
swift build -c debug
BIN=$(swift build -c debug --show-bin-path)/TotalRecall
HOME=$DEMO_HOME PATH=$DEMO_HOME/.local/bin:$PATH exec "$BIN" --now 2026-11-02T10:00 --query "goreleaser cask" "$@"
