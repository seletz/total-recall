package star

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/seletz/total-recall/analytics/internal/duck"
	"github.com/seletz/total-recall/internal/project"
)

//go:embed project
var projectFS embed.FS

// ProjectFS is the model project (dbt_project.yml, macros.sql, models/,
// tests/, contract.txt).
func ProjectFS() fs.FS {
	sub, _ := fs.Sub(projectFS, "project")
	return sub
}

// The seeds: tables Go computes from the configuration, with the models
// they are computed from.
var seeds = map[string][]string{
	"seed_project_name": {"dim_project_key"},
	"seed_account_name": {"dim_account_key"},
	// computed by worktime.Compute (worktime.go) from all sources
	"fact_work_slice": {"fact_event", "dim_actor", "dim_session", "dim_project_key"},
	"fact_work_day":   {"fact_work_slice"},
}

// sourceAlias is the name the archive is attached under for a run.
const sourceAlias = "src"

// Options configure one run.
type Options struct {
	// Archive is the source archive (a total-recall SQLite file), attached
	// read-only.
	Archive string
	// Source names the archive for the watermark: seq counts per archive.
	Source string
	// Host is the host of every session of the archive.
	Host string
	// ArchiveID is the archive's own ID (archive_meta, total-recall schema
	// v11), "" when unknown. With it a run replaces a source that loaded
	// the same archive under another name (a renamed host) and loads in
	// full when the source had another archive before.
	ArchiveID string
	// TZ is the primary time zone: local days of the marts and views.
	TZ *time.Location
	// ExtraZones are further zones of the bridge.
	ExtraZones []*time.Location
	// Home is the home directory "~" stands for in project paths.
	Home string
	// Projects are the [[project]] definitions (zero value: derived names).
	Projects project.Resolver
	// Accounts names a login (config [[account]]); nil leaves names empty.
	Accounts func(account, organization string) (name, group string)
	// Worktime configures the work-time facts; nil leaves them empty.
	Worktime *Worktime
	// Full reloads all facts of this source instead of the events above
	// the watermark.
	Full bool
	// Now is the clock of the batch record; default time.Now.
	Now func() time.Time
}

// Report describes a finished run.
type Report struct {
	BatchID  int64
	Source   string
	Mode     string // full or incremental
	SeqFrom  int64  // events.seq in (SeqFrom, SeqTo] were new
	SeqTo    int64
	Events   int // staged: all events of the touched sessions
	Sessions int
	Models   int
	Tests    int
	Elapsed  time.Duration
	// Replaced are the sources that had loaded the same archive under
	// another name; their facts were removed (see Options.ArchiveID).
	Replaced []string
}

// ErrContract means the published views differ from contract.txt.
var ErrContract = errors.New("published views differ from contract.txt")

// Run executes one batch in one transaction: stage the touched sessions,
// build dimensions, facts, calendar, marts and views, check the contract
// and the data tests, advance the watermark. A failed run leaves the
// analytics database as it was.
func Run(ctx context.Context, db *duck.DB, o Options) (Report, error) {
	start := time.Now()
	r := Report{Source: o.Source, Mode: "incremental"}
	if o.TZ == nil {
		return r, errors.New("no time zone")
	}
	if o.Source == "" || o.Host == "" {
		return r, errors.New("source and host must be set")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	p, err := LoadProject(ProjectFS(), seeds)
	if err != nil {
		return r, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET TimeZone = 'UTC'"); err != nil {
		return r, err
	}
	if err := db.AttachSQLite(ctx, o.Archive, sourceAlias); err != nil {
		return r, err
	}
	defer func() { _, _ = db.ExecContext(context.WithoutCancel(ctx), "DETACH "+sourceAlias) }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.run(ctx, tx, p, o); err != nil {
		return r, err
	}
	if err := tx.Commit(); err != nil {
		return r, fmt.Errorf("commit: %w", err)
	}
	r.Elapsed = time.Since(start)
	return r, nil
}

const bookkeeping = `
CREATE TABLE IF NOT EXISTS etl_batch (
	batch_id        BIGINT PRIMARY KEY,
	source          VARCHAR NOT NULL,
	host            VARCHAR NOT NULL,
	mode            VARCHAR NOT NULL,
	started_at      TIMESTAMP NOT NULL,
	finished_at     TIMESTAMP,
	seq_from        BIGINT NOT NULL,
	seq_to          BIGINT NOT NULL,
	events_loaded   BIGINT NOT NULL DEFAULT 0,
	sessions_loaded BIGINT NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS etl_watermark (
	source   VARCHAR PRIMARY KEY,
	host     VARCHAR NOT NULL,
	archive  VARCHAR NOT NULL,
	last_seq BIGINT NOT NULL
);
ALTER TABLE etl_watermark ADD COLUMN IF NOT EXISTS archive_id VARCHAR;
CREATE TABLE IF NOT EXISTS etl_setting (
	key   VARCHAR PRIMARY KEY,
	value VARCHAR NOT NULL
);`

func (r *Report) run(ctx context.Context, tx *sql.Tx, p Project, o Options) error {
	for _, s := range splitSQL(bookkeeping) {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("bookkeeping: %w", err)
		}
	}
	macros, err := fs.ReadFile(ProjectFS(), "macros.sql")
	if err != nil {
		return err
	}
	for _, s := range splitSQL(string(macros)) {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("macros: %w", err)
		}
	}
	if err := r.replaceRenamed(ctx, tx, o); err != nil {
		return err
	}
	var last sql.NullInt64
	var lastID sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT last_seq, archive_id FROM etl_watermark WHERE source = ?", o.Source).Scan(&last, &lastID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("watermark: %w", err)
	}
	otherArchive := o.ArchiveID != "" && lastID.Valid && lastID.String != o.ArchiveID
	if o.Full || !last.Valid || otherArchive || len(r.Replaced) > 0 {
		r.Mode = "full"
		if err := deleteSource(ctx, tx, o.Source); err != nil {
			return err
		}
	} else {
		r.SeqFrom = last.Int64
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(max(seq), 0) FROM "+sourceAlias+".events").Scan(&r.SeqTo); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(max(batch_id), 0) + 1 FROM etl_batch").Scan(&r.BatchID); err != nil {
		return fmt.Errorf("batch: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO etl_batch (batch_id, source, host, mode, started_at, seq_from, seq_to) VALUES (?, ?, ?, ?, ?, ?, ?)",
		r.BatchID, o.Source, o.Host, r.Mode, o.Now().UTC(), r.SeqFrom, r.SeqTo); err != nil {
		return fmt.Errorf("batch: %w", err)
	}
	zones := []string{o.TZ.String()}
	for _, z := range o.ExtraZones {
		if z.String() != o.TZ.String() {
			zones = append(zones, z.String())
		}
	}
	rc := RenderContext{
		Sources: map[string]string{"tr": sourceAlias},
		Vars: map[string]string{
			"seq_from":    strconv.FormatInt(r.SeqFrom, 10),
			"batch_id":    strconv.FormatInt(r.BatchID, 10),
			"source_name": sqlText(o.Source),
			"host":        sqlText(o.Host),
			"home":        sqlText(o.Home),
			"tz":          sqlText(o.TZ.String()),
			"zones":       sqlText(strings.Join(zones, ",")),
		},
	}
	for _, m := range p.Models {
		var err error
		switch m.Name {
		case "seed_project_name":
			err = seedProjects(ctx, tx, o)
		case "seed_account_name":
			err = seedAccounts(ctx, tx, o)
		case "fact_work_slice":
			err = loadWorktime(ctx, tx, o)
		case "fact_work_day":
			// written together with fact_work_slice
		default:
			err = Build(ctx, tx, m, rc)
		}
		if err != nil {
			return err
		}
		r.Models++
		if m.Name == "stg_event" {
			if err := tx.QueryRowContext(ctx, "SELECT count(*), count(DISTINCT session_id) FROM stg_event").Scan(&r.Events, &r.Sessions); err != nil {
				return fmt.Errorf("staged: %w", err)
			}
		}
	}
	if !skipContract {
		if err := CheckContract(ctx, tx); err != nil {
			return err
		}
	}
	var failed []string
	for _, t := range p.Tests {
		n, err := TestFailures(ctx, tx, t, rc)
		if err != nil {
			return err
		}
		if n > 0 {
			failed = append(failed, fmt.Sprintf("%s: %d rows", t.Name, n))
		}
		r.Tests++
	}
	if len(failed) > 0 {
		return fmt.Errorf("data tests failed: %s", strings.Join(failed, "; "))
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO etl_watermark (source, host, archive, last_seq, archive_id) VALUES (?, ?, ?, ?, ?)`,
		o.Source, o.Host, o.Archive, r.SeqTo, sql.NullString{String: o.ArchiveID, Valid: o.ArchiveID != ""}); err != nil {
		return fmt.Errorf("watermark: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO etl_setting (key, value) VALUES ('tz', ?), ('zones', ?)`,
		o.TZ.String(), strings.Join(zones, ",")); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE etl_batch SET finished_at = ?, events_loaded = ?, sessions_loaded = ? WHERE batch_id = ?",
		o.Now().UTC(), r.Events, r.Sessions, r.BatchID); err != nil {
		return fmt.Errorf("batch: %w", err)
	}
	return nil
}

// replaceRenamed removes the sources that loaded o.ArchiveID under
// another name: the archive is loaded again, in full, under o.Source, so
// a renamed host is never counted twice.
func (r *Report) replaceRenamed(ctx context.Context, tx *sql.Tx, o Options) error {
	if o.ArchiveID == "" {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT source FROM etl_watermark WHERE archive_id = ? AND source <> ? ORDER BY source", o.ArchiveID, o.Source)
	if err != nil {
		return fmt.Errorf("watermark: %w", err)
	}
	var old []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			_ = rows.Close()
			return fmt.Errorf("watermark: %w", err)
		}
		old = append(old, s)
	}
	_ = rows.Close()
	for _, s := range old {
		if err := deleteSource(ctx, tx, s); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM etl_watermark WHERE source = ?", s); err != nil {
			return fmt.Errorf("watermark: %w", err)
		}
		r.Replaced = append(r.Replaced, s)
	}
	return nil
}

// deleteSource removes the facts a source loaded before a full reload of
// it; other sources keep theirs. Dimensions keep their keys (accumulating).
func deleteSource(ctx context.Context, tx *sql.Tx, source string) error {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_tables() WHERE schema_name = current_schema() AND database_name = current_database() AND table_name = 'fact_event'").Scan(&n); err != nil || n == 0 {
		return err
	}
	for _, s := range []string{
		"DELETE FROM fact_session WHERE session_key IN (SELECT session_key FROM fact_event WHERE source_name = ?)",
		"DELETE FROM fact_event WHERE source_name = ?",
	} {
		if _, err := tx.ExecContext(ctx, s, source); err != nil {
			return fmt.Errorf("full reload: %w", err)
		}
	}
	return nil
}

// seedProjects computes the project name of every dim_project_key row as
// stats --by project names it: the first matching [[project]] definition,
// else the derived name, collisions resolved over all keys. Rewritten on
// every run, so a new definition renames without touching a fact.
func seedProjects(ctx context.Context, tx *sql.Tx, o Options) error {
	rows, err := tx.QueryContext(ctx, "SELECT project_key, project_path, is_dir, remote_url FROM dim_project_key")
	if err != nil {
		return fmt.Errorf("projects: %w", err)
	}
	type proj struct {
		key    int64
		mapped string
	}
	var ps []proj
	var keys, dirs []string
	seen := map[string]bool{}
	for rows.Next() {
		var pr proj
		var path, remote string
		var dir bool
		if err := rows.Scan(&pr.key, &path, &dir, &remote); err != nil {
			_ = rows.Close()
			return fmt.Errorf("projects: %w", err)
		}
		raw := untilde(path, o.Home)
		pr.mapped = raw
		mappedDir := dir
		if m := o.Projects.Key(raw, dir, remote); m != raw {
			pr.mapped, mappedDir = m, false
		}
		ps = append(ps, pr)
		if !seen[pr.mapped] {
			seen[pr.mapped] = true
			keys = append(keys, pr.mapped)
			if mappedDir {
				dirs = append(dirs, pr.mapped)
			}
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("projects: %w", err)
	}
	names := o.Projects.Names(keys, dirs, o.Home)
	if _, err := tx.ExecContext(ctx, "CREATE OR REPLACE TABLE seed_project_name (project_key BIGINT PRIMARY KEY, project_name VARCHAR NOT NULL)"); err != nil {
		return fmt.Errorf("projects: %w", err)
	}
	for _, pr := range ps {
		if _, err := tx.ExecContext(ctx, "INSERT INTO seed_project_name VALUES (?, ?)", pr.key, Tilde(names[pr.mapped], o.Home)); err != nil {
			return fmt.Errorf("projects: %w", err)
		}
	}
	return nil
}

// seedAccounts sets name and group of every login from the configuration.
func seedAccounts(ctx context.Context, tx *sql.Tx, o Options) error {
	if _, err := tx.ExecContext(ctx, "CREATE OR REPLACE TABLE seed_account_name (account_key BIGINT PRIMARY KEY, account_name VARCHAR NOT NULL, account_group VARCHAR NOT NULL)"); err != nil {
		return fmt.Errorf("accounts: %w", err)
	}
	if o.Accounts == nil {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT account_key, account_id, organization_id FROM dim_account_key WHERE account_id <> '' OR organization_id <> ''")
	if err != nil {
		return fmt.Errorf("accounts: %w", err)
	}
	type acc struct {
		key     int64
		id, org string
	}
	var as []acc
	for rows.Next() {
		var a acc
		if err := rows.Scan(&a.key, &a.id, &a.org); err != nil {
			_ = rows.Close()
			return fmt.Errorf("accounts: %w", err)
		}
		as = append(as, a)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("accounts: %w", err)
	}
	for _, a := range as {
		name, group := o.Accounts(a.id, a.org)
		if _, err := tx.ExecContext(ctx, "INSERT INTO seed_account_name VALUES (?, ?, ?)", a.key, name, group); err != nil {
			return fmt.Errorf("accounts: %w", err)
		}
	}
	return nil
}

// sqlText escapes s for a single-quoted SQL literal (the models quote vars).
func sqlText(s string) string { return strings.ReplaceAll(s, "'", "''") }

// splitSQL splits a script at semicolons that end a line, dropping
// comment-only chunks.
func splitSQL(text string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); hasCode(s) {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		cur.WriteString(line)
		t := strings.TrimSpace(line)
		if strings.HasSuffix(t, ";") && !strings.HasPrefix(t, "--") {
			flush()
		}
	}
	flush()
	return out
}

func hasCode(s string) bool {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "--") {
			return true
		}
	}
	return false
}

// DescribeViews lists the columns of the published views, one "view column
// TYPE" line each, sorted by view and position.
func DescribeViews(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT table_name, column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_catalog = current_database()
		  AND regexp_matches(table_name, '^analytics_.+_v[0-9]+$')
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return "", fmt.Errorf("views: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	for rows.Next() {
		var v, c, typ string
		if err := rows.Scan(&v, &c, &typ); err != nil {
			return "", fmt.Errorf("views: %w", err)
		}
		fmt.Fprintf(&b, "%s %s %s\n", v, c, typ)
	}
	return b.String(), rows.Err()
}

// skipContract is set by the test that rewrites contract.txt (-update).
var skipContract bool

// readProject reads a file of the model project.
func readProject(name string) (string, error) {
	b, err := fs.ReadFile(ProjectFS(), name)
	return string(b), err
}

// Contract returns contract.txt: the columns and types consumers may rely on.
func Contract() string {
	b, _ := fs.ReadFile(ProjectFS(), "contract.txt")
	return string(b)
}

// CheckContract fails when a published view differs from contract.txt:
// consumers bind to names and types, so a change must be a deliberate new
// contract (a _v2 view), not a side effect.
func CheckContract(ctx context.Context, tx *sql.Tx) error {
	got, err := DescribeViews(ctx, tx)
	if err != nil {
		return err
	}
	if want := Contract(); got != want {
		return fmt.Errorf("%w (after a deliberate change: go test ./internal/star -run TestContract -update)", ErrContract)
	}
	return nil
}
