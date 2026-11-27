package analytics

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/project"
)

//go:embed sql/*.sql
var sqlFS embed.FS

//go:embed checks/*.sql
var checksFS embed.FS

//go:embed contract.txt
var contract string

// Options configure one ETL run.
type Options struct {
	// Archive is the source archive, opened read-only (ATTACH ... mode=ro).
	Archive string
	// Target is the analytics database file; created when missing.
	Target string
	// Source names the archive for the watermark (seq is per archive).
	Source string
	// Host is the host of every session when the archive has no
	// sessions.host column (a local archive before #347).
	Host string
	// TZ is the primary time zone: local days of the marts and the views.
	TZ *time.Location
	// ExtraZones are further zones of the bridge for queries in other zones.
	ExtraZones []*time.Location
	// Home is the home directory "~" stands for in project paths.
	Home string
	// Projects are the [[project]] definitions (empty: derived names).
	Projects project.Resolver
	// Accounts names a login: name and group (config [[account]]); nil
	// leaves both empty.
	Accounts func(account, organization string) (name, group string)
	// Full reloads all facts instead of the events above the watermark.
	Full bool
	// Now is the clock for the batch record; default time.Now.
	Now func() time.Time
}

// Report describes a finished run.
type Report struct {
	BatchID          int64
	Mode             string // full or incremental
	SeqFrom, SeqTo   int64  // events.seq in (SeqFrom, SeqTo] were new
	Events, Sessions int    // staged: all events of the touched sessions
	Elapsed          time.Duration
}

// Run executes one ETL batch: extract (stage the touched sessions), load
// the dimensions and facts, rebuild the marts, publish the views, check the
// contract and the data checks, advance the watermark. All of it is one
// transaction: a failed run leaves the target as it was.
func Run(ctx context.Context, o Options) (Report, error) {
	start := time.Now()
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.TZ == nil {
		return Report{}, errors.New("analytics: no time zone")
	}
	if o.Source == "" {
		o.Source = "local"
	}
	register()
	var err error
	if o.Archive, err = filepath.Abs(o.Archive); err != nil {
		return Report{}, fmt.Errorf("analytics: %w", err)
	}
	if o.Target, err = filepath.Abs(o.Target); err != nil {
		return Report{}, fmt.Errorf("analytics: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+o.Target+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return Report{}, fmt.Errorf("analytics: open target: %w", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1) // ATTACH and temp tables are per connection
	src := url.URL{Scheme: "file", Path: o.Archive, RawQuery: "mode=ro"}
	if _, err := db.ExecContext(ctx, "ATTACH DATABASE ? AS src", src.String()); err != nil {
		return Report{}, fmt.Errorf("analytics: attach archive: %w", err)
	}
	for _, f := range []string{"sql/00_schema.sql", "sql/40_marts.sql"} {
		if err := execFile(ctx, db, f, nil, nil); err != nil {
			return Report{}, err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Report{}, fmt.Errorf("analytics: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	r, err := run(ctx, tx, o)
	if err != nil {
		return Report{}, err
	}
	if err := tx.Commit(); err != nil {
		return Report{}, fmt.Errorf("analytics: commit: %w", err)
	}
	r.Elapsed = time.Since(start)
	return r, nil
}

// trace prints the duration of a step with TR_ANALYTICS_TRACE set.
func trace(what string, t0 time.Time) {
	if os.Getenv("TR_ANALYTICS_TRACE") != "" {
		fmt.Fprintf(os.Stderr, "%8s %s\n", time.Since(t0).Round(time.Millisecond), what)
	}
}

func run(ctx context.Context, tx *sql.Tx, o Options) (Report, error) {
	r := Report{Mode: "incremental"}
	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT last_seq FROM etl_watermark WHERE source = ?", o.Source).Scan(&last); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("analytics: watermark: %w", err)
	}
	if o.Full || !last.Valid {
		r.Mode = "full"
		for _, s := range []string{"DELETE FROM fact_event", "DELETE FROM fact_session"} {
			if _, err := tx.ExecContext(ctx, s); err != nil {
				return r, fmt.Errorf("analytics: %s: %w", s, err)
			}
		}
	} else {
		r.SeqFrom = last.Int64
	}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq), 0) FROM src.events").Scan(&r.SeqTo); err != nil {
		return r, fmt.Errorf("analytics: max seq: %w", err)
	}
	tz := o.TZ.String()
	if _, err := tx.ExecContext(ctx, "INSERT INTO etl_setting (key, value) VALUES ('tz', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", tz); err != nil {
		return r, fmt.Errorf("analytics: settings: %w", err)
	}
	res, err := tx.ExecContext(ctx, "INSERT INTO etl_batch (source, mode, started_at, seq_from, seq_to) VALUES (?, ?, ?, ?, ?)",
		o.Source, r.Mode, o.Now().UTC().Format(time.RFC3339), r.SeqFrom, r.SeqTo)
	if err != nil {
		return r, fmt.Errorf("analytics: batch: %w", err)
	}
	if r.BatchID, err = res.LastInsertId(); err != nil {
		return r, fmt.Errorf("analytics: batch: %w", err)
	}

	hostExpr, err := hostExpr(ctx, tx)
	if err != nil {
		return r, err
	}
	args := []any{sql.Named("seq_from", r.SeqFrom), sql.Named("home", o.Home), sql.Named("host", o.Host)}
	if err := execFile(ctx, tx, "sql/10_stage.sql", map[string]string{"{{host_expr}}": hostExpr}, args); err != nil {
		return r, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*), COUNT(DISTINCT session_id) FROM stg_event").Scan(&r.Events, &r.Sessions); err != nil {
		return r, fmt.Errorf("analytics: staged: %w", err)
	}
	if err := execFile(ctx, tx, "sql/20_dims.sql", nil, nil); err != nil {
		return r, err
	}
	t0 := time.Now()
	if err := nameProjects(ctx, tx, o); err != nil {
		return r, err
	}
	trace("nameProjects", t0)
	if err := nameAccounts(ctx, tx, o); err != nil {
		return r, err
	}
	if err := execFile(ctx, tx, "sql/30_facts.sql", nil, []any{sql.Named("batch_id", r.BatchID)}); err != nil {
		return r, err
	}
	t0 = time.Now()
	if err := fillCalendar(ctx, tx, append([]*time.Location{o.TZ}, o.ExtraZones...)); err != nil {
		return r, err
	}
	trace("fillCalendar", t0)
	if err := execFile(ctx, tx, "sql/41_marts_refresh.sql", nil, []any{sql.Named("tz", tz)}); err != nil {
		return r, err
	}
	if err := execFile(ctx, tx, "sql/50_publish.sql", nil, nil); err != nil {
		return r, err
	}
	t0 = time.Now()
	if err := checkContract(ctx, tx); err != nil {
		return r, err
	}
	trace("checkContract", t0)
	if err := runChecks(ctx, tx); err != nil {
		return r, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO etl_watermark (source, last_seq) VALUES (?, ?)
		ON CONFLICT (source) DO UPDATE SET last_seq = excluded.last_seq`, o.Source, r.SeqTo); err != nil {
		return r, fmt.Errorf("analytics: watermark: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE etl_batch SET finished_at = ?, events_loaded = ?, sessions_loaded = ? WHERE batch_id = ?",
		o.Now().UTC().Format(time.RFC3339), r.Events, r.Sessions, r.BatchID); err != nil {
		return r, fmt.Errorf("analytics: batch: %w", err)
	}
	return r, nil
}

// execer is *sql.DB or *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// execFile runs the statements of an embedded SQL file one by one (the
// driver runs only the first statement of a string that has parameters),
// after replacing the placeholders of repl. Each statement gets the
// named arguments it references.
func execFile(ctx context.Context, db execer, name string, repl map[string]string, args []any) error {
	b, err := fs.ReadFile(sqlFS, name)
	if err != nil {
		return fmt.Errorf("analytics: %w", err)
	}
	text := string(b)
	for k, v := range repl {
		text = strings.ReplaceAll(text, k, v)
	}
	for _, stmt := range splitSQL(text) {
		t0 := time.Now()
		_, err := db.ExecContext(ctx, stmt, usedArgs(stmt, args)...)
		if os.Getenv("TR_ANALYTICS_TRACE") != "" {
			first := strings.SplitN(strings.TrimLeft(stmt, "- \n"), "\n", 2)[0]
			fmt.Fprintf(os.Stderr, "%8s %s: %.70s\n", time.Since(t0).Round(time.Millisecond), name, first)
		}
		if err != nil {
			return fmt.Errorf("analytics: %s: %w\n%s", name, err, stmt)
		}
	}
	return nil
}

// usedArgs keeps the named arguments a statement references.
func usedArgs(stmt string, args []any) []any {
	var out []any
	for _, a := range args {
		if n, ok := a.(sql.NamedArg); ok && strings.Contains(stmt, ":"+n.Name) {
			out = append(out, a)
		}
	}
	return out
}

// splitSQL splits a script at semicolons that end a line, dropping
// comment-only chunks. The scripts keep every statement-ending semicolon
// at the end of a line and none inside one.
func splitSQL(text string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		cur.WriteString(line)
		if strings.HasSuffix(strings.TrimSpace(line), ";") && !strings.HasPrefix(strings.TrimSpace(line), "--") {
			if s := strings.TrimSpace(cur.String()); hasCode(s) {
				out = append(out, s)
			}
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); hasCode(s) {
		out = append(out, s)
	}
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

// hostExpr is the SQL for the host of session s: its host column when the
// archive has one (central archive, #347), else the :host option.
func hostExpr(ctx context.Context, tx *sql.Tx) (string, error) {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('sessions', 'src') WHERE name = 'host'").Scan(&n); err != nil {
		return "", fmt.Errorf("analytics: source columns: %w", err)
	}
	if n > 0 {
		return "COALESCE(NULLIF(s.host, ''), :host)", nil
	}
	return ":host", nil
}

// nameProjects sets project_name of every dim_project row (SCD 1) as stats
// --by project names it: the first matching [[project]] definition, else
// the derived name (base name of the git root, "~" path of a directory),
// collisions resolved over the whole key set. All rows are renamed on
// every run because a name depends on the other keys and on the config.
func nameProjects(ctx context.Context, tx *sql.Tx, o Options) error {
	rows, err := tx.QueryContext(ctx, "SELECT project_key, project_path, is_dir, remote_url FROM dim_project")
	if err != nil {
		return fmt.Errorf("analytics: projects: %w", err)
	}
	type proj struct {
		key       int64
		mapped    string
		mappedDir bool
	}
	var ps []proj
	var keys, dirs []string
	seen := map[string]bool{}
	for rows.Next() {
		var p proj
		var path, remote string
		var dir bool
		if err := rows.Scan(&p.key, &path, &dir, &remote); err != nil {
			_ = rows.Close()
			return fmt.Errorf("analytics: projects: %w", err)
		}
		raw := untilde(path, o.Home)
		p.mapped, p.mappedDir = raw, dir
		if m := o.Projects.Key(raw, dir, remote); m != raw {
			p.mapped, p.mappedDir = m, false
		}
		ps = append(ps, p)
		if !seen[p.mapped] {
			seen[p.mapped] = true
			keys = append(keys, p.mapped)
			if p.mappedDir {
				dirs = append(dirs, p.mapped)
			}
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("analytics: projects: %w", err)
	}
	names := o.Projects.Names(keys, dirs, o.Home)
	for _, p := range ps {
		if _, err := tx.ExecContext(ctx, "UPDATE dim_project SET project_name = ? WHERE project_key = ?", names[p.mapped], p.key); err != nil {
			return fmt.Errorf("analytics: projects: %w", err)
		}
	}
	return nil
}

// nameAccounts sets name and group of every login from the configuration.
func nameAccounts(ctx context.Context, tx *sql.Tx, o Options) error {
	if o.Accounts == nil {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT account_key, account_id, organization_id FROM dim_account WHERE account_id <> '' OR organization_id <> ''")
	if err != nil {
		return fmt.Errorf("analytics: accounts: %w", err)
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
			return fmt.Errorf("analytics: accounts: %w", err)
		}
		as = append(as, a)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("analytics: accounts: %w", err)
	}
	for _, a := range as {
		name, group := o.Accounts(a.id, a.org)
		if _, err := tx.ExecContext(ctx, "UPDATE dim_account SET account_name = ?, account_group = ? WHERE account_key = ?", name, group, a.key); err != nil {
			return fmt.Errorf("analytics: accounts: %w", err)
		}
	}
	return nil
}

// fillCalendar adds the bridge rows of every zone for the slot range of the
// facts that are missing, and the dim_date rows of their local dates.
// SQLite has no time zone database, so Go computes local day and hour.
func fillCalendar(ctx context.Context, tx *sql.Tx, zones []*time.Location) error {
	var lo, hi sql.NullInt64
	if err := tx.QueryRowContext(ctx, "SELECT MIN(slot_key), MAX(slot_key) FROM fact_event").Scan(&lo, &hi); err != nil {
		return fmt.Errorf("analytics: calendar: %w", err)
	}
	if !lo.Valid {
		return nil
	}
	slot, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO bridge_time_slot (tz, slot_key, local_date, local_hour, iso_weekday) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return fmt.Errorf("analytics: calendar: %w", err)
	}
	defer func() { _ = slot.Close() }()
	dates := map[string]time.Time{}
	for _, loc := range zones {
		var have, haveLo, haveHi sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*), MIN(slot_key), MAX(slot_key) FROM bridge_time_slot WHERE tz = ?", loc.String()).Scan(&have, &haveLo, &haveHi); err != nil {
			return fmt.Errorf("analytics: calendar: %w", err)
		}
		for k := lo.Int64; k <= hi.Int64; k++ {
			if haveLo.Valid && k >= haveLo.Int64 && k <= haveHi.Int64 && have.Int64 == haveHi.Int64-haveLo.Int64+1 {
				continue // contiguous range already there
			}
			lt := time.Unix(k*900, 0).In(loc)
			day := lt.Format(time.DateOnly)
			dates[day] = lt
			if _, err := slot.ExecContext(ctx, loc.String(), k, day, lt.Hour(), isoWeekday(lt)); err != nil {
				return fmt.Errorf("analytics: calendar: %w", err)
			}
		}
	}
	days := make([]string, 0, len(dates))
	for d := range dates {
		days = append(days, d)
	}
	sort.Strings(days)
	for _, d := range days {
		t := dates[d]
		y, w := t.ISOWeek()
		wd := isoWeekday(t)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO dim_date (date, year, month, iso_year, iso_week, iso_weekday, is_weekend)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, d, t.Year(), int(t.Month()), y, w, wd, wd >= 6); err != nil {
			return fmt.Errorf("analytics: calendar: %w", err)
		}
	}
	return nil
}

// isoWeekday is 1 (Monday) to 7 (Sunday).
func isoWeekday(t time.Time) int {
	return (int(t.Weekday())+6)%7 + 1
}

// describeViews lists the columns and declared types of the published
// views, one "view column type" line each, sorted by view and position.
func describeViews(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT m.name, c.name, c.type
		FROM sqlite_master m, pragma_table_info(m.name) c
		WHERE m.type = 'view' AND m.name LIKE 'analytics\_%\_v%' ESCAPE '\'
		ORDER BY m.name, c.cid`)
	if err != nil {
		return "", fmt.Errorf("analytics: views: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	for rows.Next() {
		var v, c, typ string
		if err := rows.Scan(&v, &c, &typ); err != nil {
			return "", fmt.Errorf("analytics: views: %w", err)
		}
		if typ == "" {
			typ = "ANY"
		}
		fmt.Fprintf(&b, "%s %s %s\n", v, c, typ)
	}
	return b.String(), rows.Err()
}

// checkContract fails the load when a published view differs from
// contract.txt: consumers bind to names and types, so a change must be a
// deliberate new contract (a _v2 view), not a side effect.
func checkContract(ctx context.Context, tx *sql.Tx) error {
	got, err := describeViews(ctx, tx)
	if err != nil {
		return err
	}
	if contract != "" && got != contract { // empty: bootstrapping a new contract
		return fmt.Errorf("analytics: published views differ from contract.txt (run go test ./internal/analytics -run TestContract -update after a deliberate change)")
	}
	return nil
}

// runChecks runs every data check; a check returns the rows that violate it.
func runChecks(ctx context.Context, tx *sql.Tx) error {
	files, err := fs.Glob(checksFS, "checks/*.sql")
	if err != nil {
		return fmt.Errorf("analytics: checks: %w", err)
	}
	var failed []string
	for _, f := range files {
		q, err := fs.ReadFile(checksFS, f)
		if err != nil {
			return fmt.Errorf("analytics: checks: %w", err)
		}
		var n int
		t0 := time.Now()
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+strings.TrimSuffix(strings.TrimSpace(string(q)), ";")+")").Scan(&n); err != nil {
			return fmt.Errorf("analytics: check %s: %w", f, err)
		}
		trace(f, t0)
		if n > 0 {
			failed = append(failed, fmt.Sprintf("%s: %d rows", f, n))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("analytics: data checks failed: %s", strings.Join(failed, "; "))
	}
	return nil
}
