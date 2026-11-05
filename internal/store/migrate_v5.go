package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	"github.com/seletz/total-recall/internal/store/rawz"
)

// migrateBatch is the number of events migrateRawV5 copies per SELECT.
const migrateBatch = 1000

// migrateRawV5 is the Go part of migration 5 (see the SQL block in
// migrations and migrationNotes): it copies events into events_v5 in
// batches, compressing raw (rawz) and mapping source_path to sources.id.
// seq is kept, so events_fts (external content keyed by seq) stays valid.
// Then it swaps the tables, recreates indexes and triggers and checks the
// full-text index against the new table. Until #180 the step also trimmed
// raw to an extract with the harness's trimmer; the archive keeps whole
// lines now, so the line is only compressed.
func (s *Store) migrateRawV5(ctx context.Context, tx *sql.Tx, report func(done, total int64)) error {
	codec, err := rawz.Default()
	if err != nil {
		return err
	}
	var total int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM events").Scan(&total); err != nil {
		return fmt.Errorf("count events: %w", err)
	}
	sources, err := sourceIDs(ctx, tx)
	if err != nil {
		return err
	}
	ins, err := tx.PrepareContext(ctx, `INSERT INTO events_v5
		(seq, id, session_id, ts, ts_local, kind, role, actor, text, tool_name, model, tokens_in, tokens_out, source_id, source_offset, raw, raw_truncated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer func() { _ = ins.Close() }()

	type row struct {
		seq                                         int64
		id, sessionID, ts, kind                     string
		tsLocal, role, actor, text, toolName, model sql.NullString
		tokensIn, tokensOut, sourceOffset           sql.NullInt64
		sourcePath                                  sql.NullString
		raw                                         []byte
		rawTruncated                                int64
	}
	var done int64
	last := int64(math.MinInt64)
	report(0, total)
	for {
		rows, err := tx.QueryContext(ctx, `SELECT seq, id, session_id, ts, ts_local, kind, role, actor, text, tool_name, model,
				tokens_in, tokens_out, source_path, source_offset, raw, raw_truncated
			FROM events WHERE seq > ? ORDER BY seq LIMIT ?`, last, migrateBatch)
		if err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.seq, &r.id, &r.sessionID, &r.ts, &r.tsLocal, &r.kind, &r.role, &r.actor, &r.text, &r.toolName, &r.model,
				&r.tokensIn, &r.tokensOut, &r.sourcePath, &r.sourceOffset, &r.raw, &r.rawTruncated); err != nil {
				_ = rows.Close()
				return fmt.Errorf("read events: %w", err)
			}
			batch = append(batch, r)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("read events: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			var sourceID any
			if r.sourcePath.Valid && r.sourcePath.String != "" {
				id, ok := sources[r.sourcePath.String]
				if !ok {
					return fmt.Errorf("event %s: no sources row for its path", r.id)
				}
				sourceID = id
			}
			blob := r.raw
			if len(r.raw) > 0 && !rawz.IsFrame(r.raw) {
				blob = codec.Encode(string(r.raw))
			}
			if _, err := ins.ExecContext(ctx, r.seq, r.id, r.sessionID, r.ts, r.tsLocal, r.kind, r.role, r.actor, r.text, r.toolName, r.model,
				r.tokensIn, r.tokensOut, sourceID, r.sourceOffset, blob, r.rawTruncated); err != nil {
				return fmt.Errorf("write event %s: %w", r.id, err)
			}
			last = r.seq
		}
		done += int64(len(batch))
		report(done, total)
		if len(batch) < migrateBatch {
			break
		}
	}

	// The views read events; SQLite refuses to rename a table while a view
	// names a table that does not exist. Drop them around the swap and
	// recreate them from their stored definitions.
	views, err := viewDefinitions(ctx, tx)
	if err != nil {
		return err
	}
	var stmts []string
	for _, v := range views {
		stmts = append(stmts, "DROP VIEW "+v.name)
	}
	stmts = append(stmts, "DROP TABLE events", "ALTER TABLE events_v5 RENAME TO events", eventsIndexSQL)
	for _, v := range views {
		stmts = append(stmts, v.sql)
	}
	// FTS5 checks its index against the content table and fails if they
	// disagree: seq was kept, so they must agree.
	stmts = append(stmts, "INSERT INTO events_fts(events_fts, rank) VALUES ('integrity-check', 1)")
	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", firstLine(q), err)
		}
	}
	return nil
}

type viewDef struct{ name, sql string }

// viewDefinitions returns the views of the schema with their CREATE
// statements, in creation order.
func viewDefinitions(ctx context.Context, tx *sql.Tx) ([]viewDef, error) {
	rows, err := tx.QueryContext(ctx, "SELECT name, sql FROM sqlite_master WHERE type = 'view' ORDER BY rowid")
	if err != nil {
		return nil, fmt.Errorf("read views: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []viewDef
	for rows.Next() {
		var v viewDef
		if err := rows.Scan(&v.name, &v.sql); err != nil {
			return nil, fmt.Errorf("read views: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// sourceIDs maps every source path to its id.
func sourceIDs(ctx context.Context, tx *sql.Tx) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, path FROM sources")
	if err != nil {
		return nil, fmt.Errorf("read sources: %w", err)
	}
	defer func() { _ = rows.Close() }()
	m := map[string]int64{}
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			return nil, fmt.Errorf("read sources: %w", err)
		}
		m[p] = id
	}
	return m, rows.Err()
}

func firstLine(q string) string {
	for i, c := range q {
		if c == '\n' && i > 0 {
			return q[:i]
		}
	}
	return q
}
