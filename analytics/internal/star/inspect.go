package star

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/seletz/total-recall/analytics/internal/duck"
)

// ErrNotBuilt means the analytics database has no schema yet.
var ErrNotBuilt = errors.New("the analytics database is empty: run build first")

// Built reports whether a run has completed in the database.
func Built(ctx context.Context, db *duck.DB) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_tables() WHERE schema_name = current_schema() AND database_name = current_database() AND table_name = 'etl_watermark'").Scan(&n)
	return n > 0, err
}

// TestResult is the outcome of one data test.
type TestResult struct {
	Name     string
	Failures int
}

// CheckResult is the outcome of Check.
type CheckResult struct {
	// ContractOK: the views match contract.txt; Diff lists the lines that
	// differ ("- " expected, "+ " found).
	ContractOK bool
	Diff       []string
	Tests      []TestResult
}

// OK reports whether contract and every test pass.
func (c CheckResult) OK() bool {
	if !c.ContractOK {
		return false
	}
	for _, t := range c.Tests {
		if t.Failures > 0 {
			return false
		}
	}
	return true
}

// Check checks the contract and runs the data tests against a built
// database without loading anything.
func Check(ctx context.Context, db *duck.DB) (CheckResult, error) {
	var r CheckResult
	if ok, err := Built(ctx, db); err != nil || !ok {
		if err == nil {
			err = ErrNotBuilt
		}
		return r, err
	}
	got, err := DescribeViews(ctx, db)
	if err != nil {
		return r, err
	}
	r.ContractOK = got == Contract()
	if !r.ContractOK {
		r.Diff = lineDiff(Contract(), got)
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
	rc := RenderContext{Sources: map[string]string{}, Vars: map[string]string{}}
	for _, t := range p.Tests {
		n, _, err := TestFailures(ctx, conn, t, rc)
		if err != nil {
			return r, err
		}
		r.Tests = append(r.Tests, TestResult{Name: t.Name, Failures: n})
	}
	return r, nil
}

// lineDiff lists the lines only in want ("- ") or only in got ("+ ").
func lineDiff(want, got string) []string {
	w, g := map[string]bool{}, map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(want), "\n") {
		w[l] = true
	}
	for _, l := range strings.Split(strings.TrimSpace(got), "\n") {
		g[l] = true
	}
	var out []string
	for l := range w {
		if !g[l] {
			out = append(out, "- "+l)
		}
	}
	for l := range g {
		if !w[l] {
			out = append(out, "+ "+l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][2:] < out[j][2:] || (out[i][2:] == out[j][2:] && out[i] < out[j]) })
	return out
}

// Source is a loaded archive with its watermark and last batch.
type Source struct {
	Name    string
	Host    string
	Archive string
	// ArchiveID is the archive's own ID, "" when unknown (an archive
	// before total-recall schema v11, or a database built before it).
	ArchiveID string
	LastSeq   int64
	Batch     Batch
	// Events and Sessions loaded from this source (fact_event).
	Events   int64
	Sessions int64
}

// Batch is one run.
type Batch struct {
	ID         int64
	Mode       string
	StartedAt  time.Time
	FinishedAt time.Time
	SeqFrom    int64
	SeqTo      int64
	Events     int64
	Sessions   int64
}

// Sources lists the loaded sources by name.
func Sources(ctx context.Context, db *duck.DB) ([]Source, error) {
	if ok, err := Built(ctx, db); err != nil || !ok {
		if err == nil {
			err = ErrNotBuilt
		}
		return nil, err
	}
	// archive_id came with the pool (#362); a database built before has
	// no such column until its next build.
	idCol := "''"
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_columns() WHERE table_name = 'etl_watermark' AND column_name = 'archive_id'").Scan(&n); err != nil {
		return nil, fmt.Errorf("sources: %w", err)
	}
	if n > 0 {
		idCol = "COALESCE(w.archive_id, '')"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT w.source, w.host, w.archive, `+idCol+`, w.last_seq,
		       b.batch_id, b.mode, b.started_at, b.finished_at, b.seq_from, b.seq_to, b.events_loaded, b.sessions_loaded,
		       (SELECT count(*) FROM fact_event f WHERE f.source_name = w.source),
		       (SELECT count(DISTINCT session_key) FROM fact_event f WHERE f.source_name = w.source)
		FROM etl_watermark w
		LEFT JOIN etl_batch b ON b.batch_id = (SELECT max(batch_id) FROM etl_batch x WHERE x.source = w.source AND x.finished_at IS NOT NULL)
		ORDER BY w.source`)
	if err != nil {
		return nil, fmt.Errorf("sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Source
	for rows.Next() {
		var s Source
		var started, finished sql.NullTime
		if err := rows.Scan(&s.Name, &s.Host, &s.Archive, &s.ArchiveID, &s.LastSeq, &s.Batch.ID, &s.Batch.Mode, &started, &finished,
			&s.Batch.SeqFrom, &s.Batch.SeqTo, &s.Batch.Events, &s.Batch.Sessions, &s.Events, &s.Sessions); err != nil {
			return nil, fmt.Errorf("sources: %w", err)
		}
		s.Batch.StartedAt, s.Batch.FinishedAt = started.Time.UTC(), finished.Time.UTC()
		out = append(out, s)
	}
	return out, rows.Err()
}

// Setting reads a value of etl_setting ("" when missing).
func Setting(ctx context.Context, db *duck.DB, key string) (string, error) {
	var v string
	err := db.QueryRowContext(ctx, "SELECT value FROM etl_setting WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// View is a published view with its columns from the contract.
type View struct {
	Name    string
	Columns []Column
}

// Column is a column of a published view.
type Column struct {
	Name string
	Type string
}

// Views parses contract.txt: the published views in name order.
func Views() []View {
	var out []View
	for _, line := range strings.Split(strings.TrimSpace(Contract()), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		if len(out) == 0 || out[len(out)-1].Name != f[0] {
			out = append(out, View{Name: f[0]})
		}
		out[len(out)-1].Columns = append(out[len(out)-1].Columns, Column{Name: f[1], Type: f[2]})
	}
	return out
}
