#!/usr/bin/env bash
# Spike #349: split the synthetic demo archive into the archives of two
# hosts (as the pool demo of #347) under build/pool/. Synthetic data only.
set -euo pipefail
cd "$(dirname "$0")/.."
[ -f build/demo.db ] || go run ./tools/demo -out build/demo.db
mkdir -p build/pool
host_a="(SELECT s.id FROM sessions s LEFT JOIN sessions p ON p.id = s.parent_id
  WHERE COALESCE(p.cwd, s.cwd) LIKE '%/dotfiles%' OR COALESCE(p.cwd, s.cwd) LIKE '%/fahrtenbuch%'
     OR COALESCE(p.cwd, s.cwd) LIKE '%/kompass\_\_worktrees/%' ESCAPE '\\')"
for h in host-a host-b; do
  rm -f "build/pool/$h.db" "build/pool/$h.db-wal" "build/pool/$h.db-shm"
  sqlite3 build/demo.db "VACUUM INTO 'build/pool/$h.db'"
done
sqlite3 build/pool/host-a.db "DELETE FROM events WHERE session_id NOT IN $host_a; DELETE FROM sessions WHERE id NOT IN $host_a; VACUUM;"
sqlite3 build/pool/host-b.db "DELETE FROM events WHERE session_id IN $host_a; DELETE FROM sessions WHERE id IN $host_a; VACUUM;"
for h in host-a host-b; do
  printf '%s: %s sessions, %s events\n' "$h" "$(sqlite3 build/pool/$h.db 'SELECT COUNT(*) FROM sessions')" "$(sqlite3 build/pool/$h.db 'SELECT COUNT(*) FROM events')"
done
