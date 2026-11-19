#!/usr/bin/env bash
# Lay out a synthetic home directory around the demo archive for the macOS
# spike: config with the [[project]] definitions of the screenshots, a
# Claude Code settings file with the hook block, ~/.claude.json with the MCP
# entry, one backup copy and the CLI on PATH. Nothing here is real data.
#
#   macos/scripts/demo-home.sh [HOME_DIR]   default build/macos-demo/home
#
# Prints the home directory. Needs build/demo.db (mise run demo:db) and the
# CLI binary ./total-recall (mise run build).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
DEMO_HOME=${1:-$ROOT/build/macos-demo/home}
DB=$ROOT/build/demo.db
BIN=$ROOT/total-recall
[ -f "$DB" ] || { echo "demo-home: $DB missing, run mise run demo:db" >&2; exit 1; }
[ -x "$BIN" ] || { echo "demo-home: $BIN missing, run mise run build" >&2; exit 1; }

rm -rf "$DEMO_HOME"
mkdir -p "$DEMO_HOME/.config/total-recall" "$DEMO_HOME/Library/Application Support/total-recall" \
  "$DEMO_HOME/.local/bin" "$DEMO_HOME/.claude/projects" "$DEMO_HOME/Documents/total-recall"

ln -s "$DB" "$DEMO_HOME/Library/Application Support/total-recall/archive.db"
ln -s "$BIN" "$DEMO_HOME/.local/bin/total-recall"

CFG=$DEMO_HOME/.config/total-recall/config.toml
HOME=$DEMO_HOME "$BIN" config init >/dev/null
# The project definitions of the TUI screenshots (internal/screenshot/env.go).
cat >> "$CFG" <<'TOML'

[[project]]
name = "kompass"
description = "Wanderkarten, alle Repositories"
color = "blue"
match = ["https://git.invalid/mara/kompass*", "~/src/kompass*"]

[[project]]
name = "wetterwarte"
description = "Messstation und Auswertung"
color = "purple"
match = "https://git.invalid/mara/wetterwarte"

[[project]]
name = "fahrtenbuch"
color = "teal"
match = "https://git.invalid/mara/fahrtenbuch"
TOML

HOME=$DEMO_HOME "$BIN" hooks print --harness claude > "$DEMO_HOME/.claude/settings.json"
cat > "$DEMO_HOME/.claude.json" <<JSON
{
  "mcpServers": {
    "total-recall": {
      "type": "stdio",
      "command": "$DEMO_HOME/.local/bin/total-recall",
      "args": ["mcp"]
    }
  }
}
JSON

# One backup copy, the night before the demo clock (2026-11-02 03:00 Berlin).
BK=$DEMO_HOME/Documents/total-recall/archive-2026-11-02.db.zst
echo "not a real backup" > "$BK"
TZ=Europe/Berlin touch -t 202611020300 "$BK"

echo "$DEMO_HOME"
