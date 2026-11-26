#!/usr/bin/env bash
# Regenerate the JSON fixtures of the Core tests from the synthetic demo
# archive: every fixture is the stdout of one `total-recall ... --json` call
# against the demo home (macos/scripts/demo-home.sh). Paths below the demo
# home become /Users/mara, the repository root /repo, so no local path lands
# in the repository. The argument list of each fixture is written next to it
# (NAME.args), the tests check that the Core builds exactly these arguments.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=$ROOT/macos/Tests/TotalRecallCoreTests/Fixtures
DEMO_HOME=$(bash "$ROOT/macos/scripts/demo-home.sh")
export HOME=$DEMO_HOME PATH=$DEMO_HOME/.local/bin:$PATH
unset TOTAL_RECALL_DB TOTAL_RECALL_CONFIG TOTAL_RECALL_BACKUP_DIR
mkdir -p "$OUT"
rm -f "$OUT"/*.json "$OUT"/*.args

# name <TAB> arguments (one per field, separated by "|")
while IFS=$'\t' read -r name args; do
  [ -z "$name" ] && continue
  IFS='|' read -r -a argv <<<"$args"
  printf '%s\n' "${argv[@]}" > "$OUT/$name.args"
  # A failing call writes its JSON error object to stdout; keep it.
  total-recall "${argv[@]}" > "$OUT/$name.json" 2>/dev/null || true
  sed -i '' -e "s#$DEMO_HOME#/Users/mara#g" -e "s#$ROOT#/repo#g" "$OUT/$name.json"
done <<'LIST'
search	search|goreleaser cask|--limit|50|--json
search-types	search|goreleaser cask|--type|event,session,memory|--limit|50|--json
search-attachments	search|--has-attachment|--type|event,session|--limit|50|--json
search-memory	search|goreleaser cask|--type|memory|--limit|50|--json
show	show|claude:7d1e4c20-0001-4000-8000-000000000001|--all|--json
show-attachments	show|claude:7d1e4c20-0010-4000-8000-000000000010|--all|--json
show-missing	show|claude:does-not-exist|--all|--json
stats	stats|--since|2026-10-05|--until|2026-11-01|--json
series	stats|--since|2026-10-05|--until|2026-11-01|--period|day|--by|project|--json
workday	stats|--workday|--blocks|--since|2026-05-15|--until|2026-06-07|--json
day	stats|--day|2026-06-02|--json
day-all	stats|--day|2026-06-02|--all-events|--json
timeline	timeline|--since|2026-05-15|--until|2026-06-09|--fields|ts,project|--json
memory	memory|--deleted|--json
memory-history	memory|--project|kompass|--history|MEMORY.md|--json
blob	blob|91a028d70acc|--json
status	status|--json
config	config|show|--json
sessions	sessions|--limit|0|--all-sessions|--json
LIST
# Smaller copy of the session list: only the fields the Core reads.
jq -c '[.[] | {id, display_name, cwd, git_root, git_remote_url, started_at, ended_at, parent_id}]' \
  "$OUT/sessions.json" > "$OUT/sessions.min.json" && mv "$OUT/sessions.min.json" "$OUT/sessions.json"
ls -la "$OUT" | awk '{print $5, $9}'
