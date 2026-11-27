#!/usr/bin/env bash
# Spike #347: deliver the demo pool (two hosts) to a central archive over
# HTTPS with the real binary and compare the --json outputs of the central
# archive with the hosts' local archives and the reference archive.
# Everything lives in build/pool; synthetic data only, never the live archive.
set -euo pipefail
cd "$(dirname "$0")/.."
D=build/pool
TR=build/total-recall
CGO_ENABLED=0 go build -o "$TR" ./cmd/total-recall
go run ./tools/pool -dir "$D" -hosts host-a,host-b >/dev/null
rm -f "$D"/central.db* "$D"/push-*.json "$D"/*.log
export TOTAL_RECALL_CONFIG="$PWD/$D/config.toml"
printf 'timezone = "Europe/Berlin"\n' > "$TOTAL_RECALL_CONFIG"

# Throwaway CA-less self-signed certificate for 127.0.0.1.
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=receiver" \
	-addext "subjectAltName=IP:127.0.0.1" -keyout "$D/key.pem" -out "$D/cert.pem" 2>/dev/null
: > "$D/hosts"
for h in host-a host-b; do
	openssl rand -hex 32 > "$D/$h.token"
	printf '%s %s\n' "$h" "$(tr -d '\n' < "$D/$h.token" | shasum -a 256 | cut -d' ' -f1)" >> "$D/hosts"
done
PORT=${PORT:-18787}
"$TR" --db "$D/central.db" receive --listen "127.0.0.1:$PORT" --hosts "$D/hosts" \
	--tls-cert "$D/cert.pem" --tls-key "$D/key.pem" > "$D/receive.out" 2>&1 &
RPID=$!
trap 'kill $RPID 2>/dev/null || true' EXIT
for _ in $(seq 50); do curl -sf --cacert "$D/cert.pem" "https://127.0.0.1:$PORT/v1/health" >/dev/null && break; sleep 0.1; done

for h in host-a host-b; do
	"$TR" --db "$D/$h.db" push --to "https://127.0.0.1:$PORT" --token-file "$D/$h.token" --ca "$D/cert.pem"
done
echo "second push of host-a (nothing new):"
"$TR" --db "$D/host-a.db" push --to "https://127.0.0.1:$PORT" --token-file "$D/host-a.token" --ca "$D/cert.pem"
echo "wrong token:"
echo nope > "$D/bad.token"
"$TR" --db "$D/host-a.db" push --to "https://127.0.0.1:$PORT" --token-file "$D/bad.token" --ca "$D/cert.pem" || true
kill $RPID; wait $RPID 2>/dev/null || true

q() { "$TR" --db "$D/$1.db" "${@:2}"; }
row() { # name, then the four numbers
	printf '%-34s %10s %10s %10s %10s  %s\n' "$@"
}
check() { # label, jq filter, command...
	local label=$1 filter=$2; shift 2
	local a b c r
	a=$(q host-a "$@" | jq "$filter"); b=$(q host-b "$@" | jq "$filter")
	c=$(q central "$@" | jq "$filter"); r=$(q all "$@" | jq "$filter")
	local ok="ok"
	if [ "${ADDITIVE:-1}" = 1 ]; then
		[ "$((a + b))" = "$c" ] || ok="central != host-a + host-b"
	fi
	[ "$c" = "$r" ] || ok="$ok; central != reference"
	row "$label" "$a" "$b" "$c" "$r" "$ok"
}
echo
row "" host-a host-b central reference ""
check "stats sessions" .total.sessions stats --json --since 2026-01-01
check "stats human events" .total.human stats --json --since 2026-01-01
check "stats agent events" .total.agent stats --json --since 2026-01-01
ADDITIVE=0 check "stats days with activity (union)" '.by_day | length' stats --json --since 2026-01-01
check "stats --tools calls" .total stats --tools --json --since 2026-01-01
check "search 'goreleaser cask' hits" length search --json --limit 0 "goreleaser cask"
check "search 'gpx' hits" length search --json --limit 0 gpx
check "sessions --json" length sessions --json --limit 0 --since 2026-01-01
for h in host-a host-b; do
	n=0; same=0
	for sid in $(sqlite3 "$D/$h.db" "SELECT id FROM sessions ORDER BY id LIMIT 25"); do
		n=$((n + 1))
		cmp -s <(q central show "$sid" --json) <(q "$h" show "$sid" --json) && same=$((same + 1))
	done
	echo "show --json: $same of $n sessions of $h identical in the central archive"
done
echo
echo "reclassify actors centrally (the coordinator on host-b started workers on host-a):"
q central reindex --actors
ADDITIVE=0 check "stats human events after reindex" .total.human stats --json --since 2026-01-01
echo
echo "central archive: sessions and events per host (sql)"
q central sql "SELECT host, count(DISTINCT s.id) AS sessions, count(e.seq) AS events FROM sessions s LEFT JOIN events e ON e.session_id = s.id GROUP BY host ORDER BY host"
