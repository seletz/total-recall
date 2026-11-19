#!/usr/bin/env bash
# Render the screenshots of the macOS spike against the demo archive into
# macos/screenshots (one PNG per shot of internal/screenshot/frames.go and
# a README with the captions): macos/scripts/shots.sh [NAME,...]
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DEMO_HOME=$(bash "$ROOT/macos/scripts/demo-home.sh")
cd "$ROOT/macos"
swift build -c debug
BIN=$(swift build -c debug --show-bin-path)/TotalRecall
args=(--shots "$ROOT/macos/screenshots")
[ $# -gt 0 ] && args+=(--only "$1")
HOME=$DEMO_HOME PATH=$DEMO_HOME/.local/bin:$PATH "$BIN" "${args[@]}"
