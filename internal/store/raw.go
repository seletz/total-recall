package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"modernc.org/sqlite"

	"github.com/seletz/total-recall/internal/store/rawz"
)

// events.raw is stored as a zstd frame with an embedded dictionary
// (internal/store/rawz) since schema v5. This file is the only place in the
// store that compresses or decompresses it: rawBlob on the way in
// (WriteBatchOpts), rawText on the way out (UserMessages), the SQL function
// raw_text for `total-recall sql`, and RawStats for status.

// RawTextFunc is the SQL function that decompresses events.raw:
// raw_text(raw) gives the stored line as text (NULL for NULL). A value that
// is not a zstd frame (a text column of an older archive, a plain string)
// is returned as text unchanged, so the function works on every schema
// version.
const RawTextFunc = "raw_text"

func init() {
	sqlite.MustRegisterDeterministicScalarFunction(RawTextFunc, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		switch v := args[0].(type) {
		case nil:
			return nil, nil
		case []byte:
			return rawText(v)
		case string:
			return v, nil
		default:
			return nil, fmt.Errorf("%s: unsupported type %T", RawTextFunc, v)
		}
	})
}

// rawBlob is the stored form of a raw line: nil (NULL) for "", else the
// compressed frame.
func rawBlob(raw string) []byte { return rawz.Encode(raw) }

// rawText is the line behind a stored raw value: "" for NULL, the
// decompressed frame, or the value itself when it is not a frame (rows of
// an archive that was not migrated yet, test rows written as text).
func rawText(b []byte) (string, error) {
	if !rawz.IsFrame(b) {
		return string(b), nil
	}
	return rawz.Decode(b)
}

// RawStats describes the stored raw lines of the events table.
type RawStats struct {
	// Rows is the number of events with a raw line, Truncated the number
	// without one because the line exceeded ingest.raw_max_bytes.
	Rows, Truncated int64
	// Bytes is the size of all stored (compressed) raw values.
	Bytes int64
	// SampleRows is the number of rows the ratio was estimated from (0: no
	// compressed rows found), Ratio their uncompressed over compressed size.
	SampleRows int64
	Ratio      float64
}

// rawSampleSize is the number of rows RawStats inspects for the ratio: the
// frame headers carry the uncompressed size, so no row is decompressed, but
// reading every row of a large archive would take seconds.
const rawSampleSize = 2000

// RawStats counts the stored raw lines and their compressed size, and
// estimates the compression ratio from the frame headers of a random
// sample of rawSampleSize rows.
func (s *Store) RawStats(ctx context.Context) (RawStats, error) {
	var st RawStats
	if err := s.db().QueryRowContext(ctx, `SELECT count(raw), COALESCE(sum(length(raw)), 0), COALESCE(sum(raw_truncated), 0) FROM events`).
		Scan(&st.Rows, &st.Bytes, &st.Truncated); err != nil {
		return st, fmt.Errorf("store: raw stats: %w", err)
	}
	if st.Rows == 0 {
		return st, nil
	}
	rows, err := s.db().QueryContext(ctx, `SELECT raw FROM events WHERE seq IN (SELECT seq FROM events WHERE raw IS NOT NULL ORDER BY random() LIMIT ?)`, rawSampleSize)
	if err != nil {
		return st, fmt.Errorf("store: raw stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var packed, unpacked uint64
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return st, fmt.Errorf("store: raw stats: %w", err)
		}
		h, ok := rawz.Inspect(b)
		if !ok {
			continue
		}
		st.SampleRows++
		packed += uint64(len(b))
		unpacked += h.Size
	}
	if err := rows.Err(); err != nil {
		return st, fmt.Errorf("store: raw stats: %w", err)
	}
	if packed > 0 {
		st.Ratio = float64(unpacked) / float64(packed)
	}
	return st, nil
}

// rawOf returns the raw line of one event (tests and tools); ok is false
// for an unknown ID.
func (s *Store) rawOf(ctx context.Context, id string) (raw string, truncated, ok bool, err error) {
	var b []byte
	err = s.db().QueryRowContext(ctx, `SELECT raw, raw_truncated FROM events WHERE id = ?`, id).Scan(&b, &truncated)
	if err == sql.ErrNoRows {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	raw, err = rawText(b)
	return raw, truncated, true, err
}
