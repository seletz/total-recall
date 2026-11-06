package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/model"
)

// Meta records (table session_meta, schema v7): harness records that are
// no message but belong to a session, kept as their whole line like
// events.raw (redacted by ingest, compressed here with rawBlob). They are
// no events: no text, no full-text index, no statistic reads them. Large
// values the adapter took out of the line (model.Meta.Attachments) go to
// the blobs table once per hash, linked in meta_blobs like event_blobs.
// This file writes them (writeMeta, from WriteBatchOpts), counts them for
// status (MetaStats) and reads single ones for tests and tools (metaOf).

// writeMeta inserts the meta records of a batch (existing IDs are
// skipped) with their attachments and, with rewrite, replaces raw and the
// attachments of the ones that exist, unless the stored line refers to
// more blobs than the new one (keepsMoreRefs). rows keeps a record that
// comes by again from counting twice.
func (s *Store) writeMeta(ctx context.Context, tx *sql.Tx, metas []model.Meta, rewrite bool, rows *batchRows, res *WriteResult) error {
	ins, err := tx.PrepareContext(ctx, `INSERT INTO session_meta (id, session_id, ts, type, source_id, source_offset, raw)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("store: prepare: %w", err)
	}
	defer func() { _ = ins.Close() }()
	var upd, cur *sql.Stmt
	if rewrite {
		upd, err = tx.PrepareContext(ctx, `UPDATE session_meta SET raw = ? WHERE id = ?`)
		if err != nil {
			return fmt.Errorf("store: prepare: %w", err)
		}
		defer func() { _ = upd.Close() }()
		cur, err = tx.PrepareContext(ctx, `SELECT raw FROM session_meta WHERE id = ?`)
		if err != nil {
			return fmt.Errorf("store: prepare: %w", err)
		}
		defer func() { _ = cur.Close() }()
	}
	blobs := newBlobWriter(ctx, tx, "meta_blobs", "meta_id")
	defer blobs.close()
	sources := map[string]int64{}
	for _, m := range metas {
		blob := rawBlob(m.Raw)
		if cur != nil {
			keep, err := keepsMoreRefs(ctx, cur, m)
			if err != nil {
				return err
			}
			if keep {
				if rows.first(m.ID) {
					res.MetaKept++
				}
				continue
			}
		}
		if upd != nil {
			r, err := upd.ExecContext(ctx, blob, m.ID)
			if err != nil {
				return fmt.Errorf("store: rewrite raw of meta record %s: %w", m.ID, err)
			}
			if n, _ := r.RowsAffected(); n > 0 {
				if rows.first(m.ID) {
					res.RawRewritten++
					res.MetaRewritten++
				}
				if err := blobs.replace(m.ID, m.Attachments, res); err != nil {
					return err
				}
				continue
			}
		}
		var srcID any
		if m.SourcePath != "" {
			id, ok := sources[m.SourcePath]
			if !ok {
				h, _ := splitID(m.SessionID)
				if id, err = sourceID(ctx, tx, m.SourcePath, h); err != nil {
					return err
				}
				sources[m.SourcePath] = id
			}
			srcID = id
		}
		r, err := ins.ExecContext(ctx, m.ID, m.SessionID, formatTime(m.TS), m.Type, srcID, m.SourceOffset, blob)
		if err != nil {
			return fmt.Errorf("store: insert meta record %s: %w", m.ID, err)
		}
		if n, err := r.RowsAffected(); err == nil && n > 0 {
			res.MetaInserted += int(n)
			rows.first(m.ID)
			if err := blobs.add(m.ID, m.Attachments, res); err != nil {
				return err
			}
		}
	}
	return nil
}

// keepsMoreRefs reports whether the stored line of meta record m refers
// to more blobs than its new line: data that is gone from the harness (a
// backup copy it cleaned up) is no longer in the new line, and a rewrite
// must not drop what the archive already holds. An unknown record is
// not kept (it is inserted).
func keepsMoreRefs(ctx context.Context, cur *sql.Stmt, m model.Meta) (bool, error) {
	var b []byte
	err := cur.QueryRowContext(ctx, m.ID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: meta record %s: %w", m.ID, err)
	}
	old, err := rawText(b)
	if err != nil {
		return false, fmt.Errorf("store: meta record %s: %w", m.ID, err)
	}
	return strings.Count(old, blobref.Prefix) > strings.Count(m.Raw, blobref.Prefix), nil
}

// MetaStats describes the stored meta records.
type MetaStats struct {
	// Rows is the number of meta records, Bytes the size of their
	// compressed lines.
	Rows, Bytes int64
}

// MetaStats counts the meta records and their compressed size.
func (s *Store) MetaStats(ctx context.Context) (MetaStats, error) {
	var st MetaStats
	if err := s.db().QueryRowContext(ctx, `SELECT count(*), COALESCE(sum(length(raw)), 0) FROM session_meta`).Scan(&st.Rows, &st.Bytes); err != nil {
		return st, fmt.Errorf("store: meta stats: %w", err)
	}
	return st, nil
}

// metaOf returns one meta record by ID (tests and tools); ok is false for
// an unknown ID. Raw is the decompressed line.
func (s *Store) metaOf(ctx context.Context, id string) (m model.Meta, ok bool, err error) {
	var b []byte
	var tsv, srcPath sql.NullString
	err = s.db().QueryRowContext(ctx, `SELECT m.id, m.session_id, m.ts, m.type, s.path, m.source_offset, m.raw
		FROM session_meta m LEFT JOIN sources s ON s.id = m.source_id WHERE m.id = ?`, id).
		Scan(&m.ID, &m.SessionID, &tsv, &m.Type, &srcPath, &m.SourceOffset, &b)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	m.TS, m.SourcePath = parseTime(tsv), srcPath.String
	m.Raw, err = rawText(b)
	return m, true, err
}
