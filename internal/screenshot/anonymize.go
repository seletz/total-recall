// Package screenshot renders the TUI screenshots of the documentation: it
// drives the TUI headlessly at a fixed size and a fixed clock against the
// synthetic demo archive (package demo) and captures the frames;
// tools/screenshots draws them as PNG files and writes the text frames as
// the goldens of TestFramesGolden, which fails when the TUI drifts from
// the checked-in images.
//
// Anonymize is an optional local tool (tools/anonymize) that copies a real
// archive with every name and text removed, to look at the stats of one's
// own archive without its contents; the docs do not use it. Nothing in
// this package holds real data.
package screenshot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // database/sql driver
)

// Words are the neutral project names, assigned by activity rank (most
// events first). Projects beyond the list are named projekt-<rank>.
var Words = []string{
	"atlas", "nordlicht", "kompass", "leuchtturm", "fjord", "polarstern", "horizont", "sextant",
	"meridian", "boje", "anker", "lotse", "treibholz", "seegang", "brandung", "hallig",
	"kutter", "mole", "reede", "schlick", "kaimauer", "sandbank", "steuerbord", "backbord",
}

// ProgramName is left out of the needles: it is the name of this program
// and appears in every frame's header.
const ProgramName = "total-recall"

// Report is what Anonymize found and did.
type Report struct {
	// Sessions and Events are the row counts of the copy; they equal those
	// of the source.
	Sessions, Events int
	// Needles are the original names the copy no longer holds: base names
	// of working directories and git roots, repository paths of remotes
	// and session titles, none shorter than four characters. Frames
	// rendered from the copy are checked against them (ScanText).
	Needles []string
}

// Anonymize copies the archive at src to dst and replaces everything in the
// copy that could identify a project or a conversation, keeping only what
// the stats, heatmap and activity views draw from: times, actors, kinds and
// the session structure (parent, harness, model).
//
// src is opened read-only and copied with VACUUM INTO; it is never written.
// dst is replaced if it exists. In the copy, projects (git root, else cwd)
// are ranked by their number of events and renamed from Words; a git root
// becomes /w/<name>, its remote https://git.invalid/demo/<name>, a working
// directory at the root /w/<name> and any other one of the project
// /w/<name>__worktrees/wt<rank>. Branches become main (the most frequent)
// and topic-<rank>, titles are dropped, agent names become "subagent".
// Every event loses its text, raw JSON, tool name and source path; the
// sources and dirs tables are emptied and the full-text index is rebuilt,
// so it is empty too.
func Anonymize(ctx context.Context, src, dst string) (Report, error) {
	if err := copyDB(ctx, src, dst); err != nil {
		return Report{}, err
	}
	db, err := sql.Open("sqlite", "file:"+escape(dst)+"?_pragma=busy_timeout(10000)&_pragma=foreign_keys(0)")
	if err != nil {
		return Report{}, fmt.Errorf("anonymize: open %s: %w", dst, err)
	}
	defer func() { _ = db.Close() }()
	rep, err := needles(ctx, db)
	if err != nil {
		return Report{}, err
	}
	if err := scrub(ctx, db); err != nil {
		return Report{}, err
	}
	if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
		return Report{}, fmt.Errorf("anonymize: vacuum: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions").Scan(&rep.Sessions); err != nil {
		return Report{}, fmt.Errorf("anonymize: count sessions: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM events").Scan(&rep.Events); err != nil {
		return Report{}, fmt.Errorf("anonymize: count events: %w", err)
	}
	return rep, nil
}

// escape makes a path safe for a SQLite URI.
func escape(p string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(p)
}

// copyDB writes a consistent snapshot of src to dst through a read-only
// connection (VACUUM INTO needs a target that does not exist).
func copyDB(ctx context.Context, src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("anonymize: %w", err)
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("anonymize: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("anonymize: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+escape(src)+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return fmt.Errorf("anonymize: open %s: %w", src, err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("anonymize: copy %s: %w", src, err)
	}
	return nil
}

// needles collects the original names of the copy before they are
// replaced; see Report.Needles.
func needles(ctx context.Context, db *sql.DB) (Report, error) {
	seen := map[string]bool{}
	var rep Report
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) < 4 || s == ProgramName || seen[s] {
			return
		}
		seen[s] = true
		rep.Needles = append(rep.Needles, s)
	}
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT COALESCE(cwd, ''), COALESCE(git_root, ''), COALESCE(git_remote_url, ''),
		COALESCE(name, ''), COALESCE(ai_title, '') FROM sessions`)
	if err != nil {
		return rep, fmt.Errorf("anonymize: read sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cwd, root, remote, name, title string
		if err := rows.Scan(&cwd, &root, &remote, &name, &title); err != nil {
			return rep, fmt.Errorf("anonymize: read sessions: %w", err)
		}
		for _, p := range []string{cwd, root} {
			if p != "" {
				add(filepath.Base(p))
			}
		}
		if remote != "" {
			if u, err := url.Parse(remote); err == nil {
				add(strings.Trim(u.Path, "/"))
				add(path.Base(u.Path))
			}
		}
		add(name)
		add(title)
	}
	return rep, rows.Err()
}

// scrub runs the replacement in one transaction.
func scrub(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("anonymize: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "CREATE TEMP TABLE words(rank INTEGER PRIMARY KEY, word TEXT NOT NULL)"); err != nil {
		return fmt.Errorf("anonymize: %w", err)
	}
	for _, w := range Words {
		if _, err := tx.ExecContext(ctx, "INSERT INTO words(word) VALUES (?)", w); err != nil {
			return fmt.Errorf("anonymize: %w", err)
		}
	}
	for _, q := range scrubSQL {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("anonymize: %w\n%s", err, q)
		}
	}
	return tx.Commit()
}

// key is the project key of a session as the stats view groups it: the git
// root, else the working directory.
const key = "COALESCE(NULLIF(git_root, ''), cwd, '')"

// scrubSQL are the statements of scrub, in order. The temporary tables rank
// projects, directories and branches; the updates read the old values of
// the row (SQLite evaluates the right-hand sides before the assignment).
var scrubSQL = []string{
	`CREATE TEMP TABLE proj AS
	 WITH k AS (SELECT id AS sid, ` + key + ` AS key FROM sessions),
	      n AS (SELECT k.key, count(e.seq) AS events, count(DISTINCT k.sid) AS sessions
	            FROM k LEFT JOIN events e ON e.session_id = k.sid GROUP BY k.key)
	 SELECT key, row_number() OVER (ORDER BY events DESC, sessions DESC, key) AS rank FROM n`,
	`CREATE TEMP TABLE projname AS
	 SELECT p.key, COALESCE(w.word, 'projekt-' || p.rank) AS name FROM proj p LEFT JOIN words w ON w.rank = p.rank`,
	`CREATE TEMP TABLE dirmap AS
	 WITH d AS (SELECT cwd, ` + key + ` AS key,
	                   (git_root IS NULL OR git_root = '' OR cwd = git_root) AS is_root, count(*) AS n
	            FROM sessions WHERE cwd IS NOT NULL GROUP BY cwd, key)
	 SELECT d.cwd, d.key, d.is_root, row_number() OVER (PARTITION BY d.key ORDER BY d.is_root DESC, d.n DESC, d.cwd) AS rank FROM d`,
	`CREATE TEMP TABLE branchmap AS
	 SELECT git_branch, row_number() OVER (ORDER BY count(*) DESC, git_branch) AS rank
	 FROM sessions WHERE git_branch IS NOT NULL AND git_branch <> '' GROUP BY git_branch`,
	`CREATE TEMP TABLE accountmap AS
	 SELECT account_id, row_number() OVER (ORDER BY count(*) DESC, account_id) AS rank
	 FROM sessions WHERE account_id IS NOT NULL AND account_id <> '' GROUP BY account_id`,
	`CREATE TEMP TABLE orgmap AS
	 SELECT organization_id, row_number() OVER (ORDER BY count(*) DESC, organization_id) AS rank
	 FROM sessions WHERE organization_id IS NOT NULL AND organization_id <> '' GROUP BY organization_id`,
	`UPDATE sessions SET
	   git_remote_url = CASE WHEN git_remote_url IS NULL OR git_remote_url = '' THEN git_remote_url
	                    ELSE 'https://git.invalid/demo/' || (SELECT name FROM projname WHERE projname.key = ` + key + `) END,
	   git_root = CASE WHEN git_root IS NULL OR git_root = '' THEN git_root
	              ELSE '/w/' || (SELECT name FROM projname WHERE projname.key = ` + key + `) END,
	   cwd = (SELECT CASE WHEN m.is_root THEN '/w/' || p.name ELSE '/w/' || p.name || '__worktrees/wt' || m.rank END
	          FROM dirmap m JOIN projname p ON p.key = m.key
	          WHERE m.cwd = sessions.cwd AND m.key = ` + key + `),
	   git_branch = (SELECT CASE WHEN b.rank = 1 THEN 'main' ELSE 'topic-' || b.rank END
	                 FROM branchmap b WHERE b.git_branch = sessions.git_branch),
	   name = NULL,
	   ai_title = NULL,
	   agent_name = CASE WHEN agent_name IS NULL OR agent_name = '' THEN agent_name ELSE 'subagent' END,
	   account_id = (SELECT 'account-' || a.rank FROM accountmap a WHERE a.account_id = sessions.account_id),
	   organization_id = (SELECT 'org-' || o.rank FROM orgmap o WHERE o.organization_id = sessions.organization_id)`,
	`UPDATE events SET text = NULL, raw = NULL, raw_truncated = 0, tool_name = NULL, source_id = NULL`,
	`INSERT INTO events_fts(events_fts) VALUES ('rebuild')`,
	`DELETE FROM session_meta`,
	`DELETE FROM sources`,
	`DELETE FROM dirs`,
}

// Scan looks for the needles in every text column of every table of db
// and returns "table.column" of the first hit, or "" when the database is
// clean. The full-text index is covered through its content table.
func Scan(ctx context.Context, db *sql.DB, needles []string) (string, error) {
	tables, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'
		AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'events_fts%'`)
	if err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}
	var names []string
	for tables.Next() {
		var n string
		if err := tables.Scan(&n); err != nil {
			return "", fmt.Errorf("scan: %w", err)
		}
		names = append(names, n)
	}
	if err := tables.Close(); err != nil {
		return "", fmt.Errorf("scan: %w", err)
	}
	for _, t := range names {
		cols, err := textColumns(ctx, db, t)
		if err != nil {
			return "", err
		}
		for _, c := range cols {
			q := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM "%s" WHERE instr("%s", ?) > 0)`, t, c)
			for _, n := range needles {
				var hit bool
				if err := db.QueryRowContext(ctx, q, n).Scan(&hit); err != nil {
					return "", fmt.Errorf("scan %s.%s: %w", t, c, err)
				}
				if hit {
					return t + "." + c, nil
				}
			}
		}
	}
	return "", nil
}

// textColumns lists the columns of table that are declared TEXT or have no
// declared type (generated columns included).
func textColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_xinfo("%s")`, table))
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var cid, notnull, pk, hidden int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk, &hidden); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if typ == "" || strings.EqualFold(typ, "TEXT") {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}

// ScanText returns the first needle that occurs in text, or "" when none
// does.
func ScanText(text string, needles []string) string {
	for _, n := range needles {
		if n != "" && strings.Contains(text, n) {
			return n
		}
	}
	return ""
}
