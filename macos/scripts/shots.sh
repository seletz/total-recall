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
# The renderer draws at the display's scale; keep the committed images at
# 1x (the window size in points) so the directory stays small.
for f in "$ROOT"/macos/screenshots/*.png; do
  w=$(sips -g pixelWidth "$f" | awk '/pixelWidth/ {print $2}')
  [ "$w" -gt 1280 ] && sips --resampleWidth $((w / 2)) "$f" >/dev/null
done
