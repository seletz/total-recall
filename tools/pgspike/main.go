// Command pgspike is the PostgreSQL part of spike #347: it projects a
// (central) archive into PostgreSQL for pool-wide analysis and compares
// the result with the SQLite archive. Never point it at a real database:
// it drops and recreates the schemas tr and analytics.
//
//	go run ./tools/pgspike -db build/pool/central.db -dsn postgres://postgres:spike@127.0.0.1:55432/archive
//
// Schema tr holds the rows (no raw, no blobs: an analysis needs neither),
// schema analytics the stable, versioned views (*_v1) that notebooks and BI
// tools read. Two read-only roles: tr_analyst sees the views without any
// text, tr_reader also the redacted text of the messages.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	_ "modernc.org/sqlite"

	_ "github.com/seletz/total-recall/internal/store" // registers raw_text etc. with the sqlite driver
)

const schema = `
DROP SCHEMA IF EXISTS analytics CASCADE;
DROP SCHEMA IF EXISTS tr CASCADE;
CREATE SCHEMA tr;
CREATE TABLE tr.sessions (
	id              text PRIMARY KEY,
	host            text,
	harness         text NOT NULL,
	native_id       text,
	parent_id       text,
	display_name    text,
	cwd             text,
	git_root        text,
	git_remote_url  text,
	git_branch      text,
	model           text,
	started_at      timestamptz,
	ended_at        timestamptz,
	account_id      text,
	organization_id text
);
CREATE INDEX ON tr.sessions(host);
CREATE TABLE tr.events (
	seq                bigint PRIMARY KEY,
	id                 text NOT NULL UNIQUE,
	session_id         text NOT NULL REFERENCES tr.sessions(id),
	ts                 timestamptz NOT NULL,
	kind               text NOT NULL,
	role               text,
	actor              text,
	input_kind         text,
	text               text,
	tool_name          text,
	model              text,
	tokens_in          bigint,
	tokens_out         bigint,
	tokens_cache_read  bigint,
	tokens_cache_write bigint,
	fts tsvector GENERATED ALWAYS AS (to_tsvector('simple', coalesce(text, ''))) STORED
);
CREATE INDEX ON tr.events(session_id, ts);
CREATE INDEX ON tr.events(ts);
CREATE INDEX ON tr.events USING gin(fts);
`

// views is the analysis interface, version 1. Its columns only grow;
// a breaking change gets *_v2 next to *_v1.
const views = `
CREATE SCHEMA analytics;
CREATE VIEW analytics.sessions_v1 AS
SELECT s.id AS session_id, s.host, s.harness,
       COALESCE(s.git_remote_url, p.git_remote_url, s.git_root, p.git_root, s.cwd, p.cwd) AS project,
       s.git_branch, s.model, COALESCE(s.account_id, p.account_id) AS account_id,
       s.parent_id, s.parent_id IS NOT NULL AS is_subagent,
       s.started_at, s.ended_at, s.ended_at - s.started_at AS duration
FROM tr.sessions s LEFT JOIN tr.sessions p ON p.id = s.parent_id;

CREATE VIEW analytics.events_v1 AS
SELECT e.id AS event_id, e.session_id, s.host, s.project, s.is_subagent,
       e.ts, (e.ts AT TIME ZONE 'Europe/Berlin')::date AS day_local,
       e.kind, e.role, e.actor, e.input_kind, e.tool_name, e.model,
       e.tokens_in, e.tokens_out, e.tokens_cache_read, e.tokens_cache_write
FROM tr.events e JOIN analytics.sessions_v1 s ON s.session_id = e.session_id;

CREATE VIEW analytics.daily_v1 AS
SELECT day_local, host, project,
       count(DISTINCT session_id) AS sessions,
       count(*) FILTER (WHERE actor = 'human') AS human_prompts,
       count(*) FILTER (WHERE actor = 'agent') AS agent_events,
       count(*) FILTER (WHERE kind = 'tool_use') AS tool_calls,
       COALESCE(sum(tokens_in), 0) AS tokens_in, COALESCE(sum(tokens_out), 0) AS tokens_out
FROM analytics.events_v1 GROUP BY 1, 2, 3;

-- The redacted text, separately granted.
CREATE VIEW analytics.event_text_v1 AS
SELECT e.id AS event_id, e.session_id, e.ts, e.actor, e.text, e.fts FROM tr.events e;

DO $$ BEGIN
	IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'tr_analyst') THEN CREATE ROLE tr_analyst LOGIN PASSWORD 'analyst'; END IF;
	IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'tr_reader') THEN CREATE ROLE tr_reader LOGIN PASSWORD 'reader'; END IF;
END $$;
ALTER ROLE tr_analyst SET default_transaction_read_only = on;
ALTER ROLE tr_reader SET default_transaction_read_only = on;
GRANT USAGE ON SCHEMA analytics TO tr_analyst, tr_reader;
GRANT SELECT ON analytics.sessions_v1, analytics.events_v1, analytics.daily_v1 TO tr_analyst, tr_reader;
GRANT SELECT ON analytics.event_text_v1 TO tr_reader;
`

// examples are the analysis queries of the report, against the views.
var examples = []struct{ name, q string }{
	{"Q1 Nutzung je Host und Projekt", `
SELECT host, regexp_replace(project, '^.*/', '') AS project,
       count(DISTINCT session_id) FILTER (WHERE NOT is_subagent) AS sessions,
       count(DISTINCT session_id) FILTER (WHERE is_subagent) AS subagents,
       count(*) FILTER (WHERE actor = 'human') AS human_prompts,
       count(*) FILTER (WHERE kind = 'tool_use') AS tool_calls,
       round(count(*) FILTER (WHERE actor = 'agent')::numeric / NULLIF(count(*) FILTER (WHERE actor = 'human'), 0), 1)::float8 AS agent_per_prompt
FROM analytics.events_v1 GROUP BY 1, 2 ORDER BY 1, 2`},
	{"Q2 Top-Werkzeuge je Host (Anteil an den Tool-Aufrufen)", `
SELECT host, tool_name, calls, pct FROM (
	SELECT host, tool_name, count(*) AS calls,
	       round(100.0 * count(*) / sum(count(*)) OVER (PARTITION BY host), 1)::float8 AS pct,
	       row_number() OVER (PARTITION BY host ORDER BY count(*) DESC) AS rank
	FROM analytics.events_v1 WHERE kind = 'tool_use' GROUP BY 1, 2) t
WHERE rank <= 4 ORDER BY host, calls DESC`},
	{"Q3 Monatstrend: eigene Prompts, Subagenten, Kernzeit-Anteil", `
SELECT to_char(day_local, 'YYYY-MM') AS month,
       count(*) FILTER (WHERE actor = 'human') AS human_prompts,
       count(DISTINCT session_id) FILTER (WHERE is_subagent) AS subagent_sessions,
       round(100.0 * count(*) FILTER (WHERE actor = 'human' AND extract(hour FROM ts AT TIME ZONE 'Europe/Berlin') BETWEEN 9 AND 16)
             / NULLIF(count(*) FILTER (WHERE actor = 'human'), 0), 1)::float8 AS core_hours_pct
FROM analytics.events_v1 GROUP BY 1 ORDER BY 1`},
}

func main() {
	dbPath := flag.String("db", "build/pool/central.db", "SQLite archive to project")
	dsn := flag.String("dsn", "", "PostgreSQL DSN of a throwaway database (superuser)")
	flag.Parse()
	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "pgspike: give -dsn")
		os.Exit(2)
	}
	if err := run(context.Background(), *dbPath, *dsn); err != nil {
		fmt.Fprintln(os.Stderr, "pgspike:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dbPath, dsn string) error {
	lite, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return err
	}
	defer func() { _ = lite.Close() }()
	pg, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = pg.Close(ctx) }()
	if _, err := pg.Exec(ctx, schema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	start := time.Now()
	ns, err := copyRows(ctx, lite, pg, "sessions",
		[]string{"id", "host", "harness", "native_id", "parent_id", "display_name", "cwd", "git_root", "git_remote_url", "git_branch", "model", "started_at", "ended_at", "account_id", "organization_id"},
		`SELECT id, host, harness, native_id, parent_id, display_name, cwd, git_root, git_remote_url, git_branch, model, started_at, ended_at, account_id, organization_id FROM sessions`,
		map[int]bool{11: true, 12: true})
	if err != nil {
		return err
	}
	ne, err := copyRows(ctx, lite, pg, "events",
		[]string{"seq", "id", "session_id", "ts", "kind", "role", "actor", "input_kind", "text", "tool_name", "model", "tokens_in", "tokens_out", "tokens_cache_read", "tokens_cache_write"},
		`SELECT seq, id, session_id, ts, kind, role, actor, input_kind, text, tool_name, model, tokens_in, tokens_out, tokens_cache_read, tokens_cache_write FROM events`,
		map[int]bool{3: true})
	if err != nil {
		return err
	}
	load := time.Since(start)
	if _, err := pg.Exec(ctx, views); err != nil {
		return fmt.Errorf("views: %w", err)
	}
	if _, err := pg.Exec(ctx, "ANALYZE"); err != nil {
		return err
	}
	var size int64
	if err := pg.QueryRow(ctx, "SELECT pg_total_relation_size('tr.events') + pg_total_relation_size('tr.sessions')").Scan(&size); err != nil {
		return err
	}
	fmt.Printf("## Laden\n\n%d Sessions, %d Events in %s (COPY), tr.* %.1f MiB mit Indexen\n\n", ns, ne, load.Round(time.Millisecond), float64(size)/(1<<20))

	// Sum equals parts in PostgreSQL too.
	fmt.Println("## Zählung je Host (SQLite gegen PostgreSQL)")
	fmt.Println()
	fmt.Println("| host | Sessions SQLite | Sessions PG | Events SQLite | Events PG | Tool-Aufrufe SQLite | Tool-Aufrufe PG |")
	fmt.Println("|---|---|---|---|---|---|---|")
	rows, err := lite.QueryContext(ctx, `SELECT s.host, count(DISTINCT s.id), count(e.seq), count(e.seq) FILTER (WHERE e.kind = 'tool_use')
		FROM sessions s LEFT JOIN events e ON e.session_id = s.id GROUP BY s.host ORDER BY s.host`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var host sql.NullString
		var s, e, tc int64
		if err := rows.Scan(&host, &s, &e, &tc); err != nil {
			return err
		}
		var ps, pe, ptc int64
		if err := pg.QueryRow(ctx, `SELECT count(DISTINCT s.session_id), count(e.event_id), count(e.event_id) FILTER (WHERE e.kind = 'tool_use')
			FROM analytics.sessions_v1 s LEFT JOIN analytics.events_v1 e ON e.session_id = s.session_id WHERE s.host IS NOT DISTINCT FROM $1`, host).Scan(&ps, &pe, &ptc); err != nil {
			return err
		}
		fmt.Printf("| %s | %d | %d | %d | %d | %d | %d |\n", host.String, s, ps, e, pe, tc, ptc)
	}
	_ = rows.Close()
	fmt.Println()

	fmt.Println("## Volltext: FTS5 (unicode61 remove_diacritics 2) gegen tsvector ('simple')")
	fmt.Println()
	fmt.Println("| Suche | FTS5 Treffer | FTS5 Zeit | tsvector Treffer | tsvector Zeit |")
	fmt.Println("|---|---|---|---|---|")
	for _, q := range []struct{ fts, ts string }{
		{"goreleaser cask", "goreleaser cask"},
		{"gpx", "gpx"},
		{"export*", "export:*"},
		{"\"GPX-Export\"", "\"GPX-Export\""},
		{"ergänze", "ergänze"},
		{"erganze", "erganze"},
	} {
		var a, b int
		t0 := time.Now()
		if err := lite.QueryRowContext(ctx, "SELECT count(*) FROM events_fts WHERE events_fts MATCH ?", q.fts).Scan(&a); err != nil {
			return fmt.Errorf("fts5 %q: %w", q.fts, err)
		}
		d1 := time.Since(t0)
		t0 = time.Now()
		tq := "websearch_to_tsquery('simple', $1)"
		if strings.HasSuffix(q.ts, ":*") {
			tq = "to_tsquery('simple', $1)"
		}
		if err := pg.QueryRow(ctx, "SELECT count(*) FROM tr.events WHERE fts @@ "+tq, q.ts).Scan(&b); err != nil {
			return fmt.Errorf("tsvector %q: %w", q.ts, err)
		}
		d2 := time.Since(t0)
		fmt.Printf("| `%s` | %d | %s | %d | %s |\n", q.fts, a, d1.Round(10*time.Microsecond), b, d2.Round(10*time.Microsecond))
	}
	fmt.Println()

	// Roles: the analyst sees the views, not the tables and not the text.
	fmt.Println("## Rollen")
	fmt.Println()
	analyst := strings.Replace(dsn, "postgres:spike@", "tr_analyst:analyst@", 1)
	ac, err := pgx.Connect(ctx, analyst)
	if err != nil {
		return err
	}
	defer func() { _ = ac.Close(ctx) }()
	for _, q := range []string{
		"SELECT count(*) FROM analytics.daily_v1",
		"SELECT count(*) FROM tr.events",
		"SELECT count(*) FROM analytics.event_text_v1",
		"CREATE TABLE analytics.x (i int)",
	} {
		var n int64
		err := ac.QueryRow(ctx, q).Scan(&n)
		res := fmt.Sprintf("ok (%d)", n)
		if err != nil {
			res = "verweigert: " + strings.SplitN(err.Error(), " (SQLSTATE", 2)[0]
		}
		fmt.Printf("- tr_analyst `%s`: %s\n", q, res)
	}
	fmt.Println()

	for _, ex := range examples {
		if err := table(ctx, ac, ex.name, ex.q); err != nil {
			return err
		}
	}
	return nil
}

// copyRows copies the result of q from SQLite into tr.<table>; columns
// whose index is in times are ISO 8601 text and become timestamptz.
func copyRows(ctx context.Context, lite *sql.DB, pg *pgx.Conn, table string, cols []string, q string, times map[int]bool) (int64, error) {
	rows, err := lite.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var data [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return 0, err
		}
		for i := range vals {
			if s, ok := vals[i].(string); ok && times[i] {
				t, err := time.Parse(time.RFC3339Nano, s)
				if err != nil {
					return 0, fmt.Errorf("%s: %s: %w", table, cols[i], err)
				}
				vals[i] = t
			}
		}
		data = append(data, vals)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return pg.CopyFrom(ctx, pgx.Identifier{"tr", table}, cols, pgx.CopyFromRows(data))
}

// table runs q and prints the result as a Markdown table.
func table(ctx context.Context, pg *pgx.Conn, name, q string) error {
	rows, err := pg.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer rows.Close()
	fmt.Printf("## %s\n\n", name)
	var head []string
	for _, f := range rows.FieldDescriptions() {
		head = append(head, f.Name)
	}
	fmt.Println("| " + strings.Join(head, " | ") + " |")
	fmt.Println("|" + strings.Repeat("---|", len(head)))
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return err
		}
		var cells []string
		for _, v := range vals {
			cells = append(cells, fmt.Sprint(v))
		}
		fmt.Println("| " + strings.Join(cells, " | ") + " |")
	}
	fmt.Println()
	return rows.Err()
}
