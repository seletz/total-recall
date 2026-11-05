package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/model"
	"github.com/seletz/total-recall/internal/store/rawz"
)

// Attachments (schema v6, #181): the images and documents a transcript
// line carried as base64 live in the table blobs, once per SHA-256, and
// event_blobs links every event to the attachments its raw line refers to
// (blobref references, in order). This file is the only place that writes
// or reads blobs.data: the writer (blobWriter, from WriteBatchOpts), Blob
// for `total-recall blob`, RawLine for a line with its data put back. The
// read paths of show, search and the TUI load only the metadata
// (SessionAttachments) or nothing.

// BlobInfo describes a stored attachment without its data.
type BlobInfo struct {
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
	// Bytes is the size as delivered; StoredBytes what the archive holds
	// (smaller when the blob is zstd-compressed).
	Bytes       int64 `json:"bytes"`
	StoredBytes int64 `json:"stored_bytes"`
}

// EventAttachment is one attachment of an event (SessionAttachments).
type EventAttachment struct {
	BlobInfo
	// Position is the attachment's index among the event's attachments,
	// the order of the references in its raw line.
	Position int `json:"position"`
}

// BlobStats counts the stored attachments for status.
type BlobStats struct {
	// Count is the number of distinct blobs, Bytes their size as
	// delivered, StoredBytes the size in the archive.
	Count       int64
	Bytes       int64
	StoredBytes int64
}

// blobEncodingZstd marks a blob stored as a zstd frame (no dictionary).
const blobEncodingZstd = "zstd"

// compressedTypes are media types whose data is compressed already: they
// are stored as delivered. Everything else (PDF, text, SVG, BMP, unknown)
// is zstd-compressed.
var compressedTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true, "image/heic": true,
	"video/mp4": true, "video/webm": true, "video/quicktime": true,
	"audio/mpeg": true, "audio/ogg": true, "audio/aac": true,
	"application/zip": true, "application/gzip": true, "application/zstd": true, "application/x-xz": true, "application/x-bzip2": true,
}

// blobEncoding returns the stored encoding for a media type: "" (as
// delivered) for compressed formats, blobEncodingZstd otherwise.
func blobEncoding(mediaType string) string {
	if compressedTypes[mediaType] {
		return ""
	}
	return blobEncodingZstd
}

var (
	blobCodecOnce sync.Once
	blobCodec     *rawz.Codec
	blobCodecErr  error
)

// plainCodec is the zstd codec without a dictionary for blob data.
func plainCodec() (*rawz.Codec, error) {
	blobCodecOnce.Do(func() { blobCodec, blobCodecErr = rawz.New() })
	return blobCodec, blobCodecErr
}

// encodeBlob returns the stored form of data for mediaType and its encoding.
func encodeBlob(mediaType string, data []byte) (stored []byte, encoding string, err error) {
	if blobEncoding(mediaType) == "" {
		return data, "", nil
	}
	c, err := plainCodec()
	if err != nil {
		return nil, "", err
	}
	return c.Encode(string(data)), blobEncodingZstd, nil
}

// decodeBlob returns the data behind a stored blob.
func decodeBlob(encoding string, stored []byte) ([]byte, error) {
	switch encoding {
	case "":
		return stored, nil
	case blobEncodingZstd:
		c, err := plainCodec()
		if err != nil {
			return nil, err
		}
		s, err := c.Decode(stored)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	return nil, fmt.Errorf("store: blob: unknown encoding %q", encoding)
}

// blobWriter stores the attachments of events within one transaction.
type blobWriter struct {
	ctx               context.Context
	tx                *sql.Tx
	ins, link, unlink *sql.Stmt
	prepared          bool
	err               error
}

func newBlobWriter(ctx context.Context, tx *sql.Tx) *blobWriter { return &blobWriter{ctx: ctx, tx: tx} }

// prepare prepares the statements on first use: most batches have no
// attachments.
func (w *blobWriter) prepare() error {
	if w.prepared {
		return w.err
	}
	w.prepared = true
	for _, p := range []struct {
		dst *(*sql.Stmt)
		q   string
	}{
		{&w.ins, `INSERT INTO blobs (sha256, media_type, bytes, encoding, data) VALUES (?, ?, ?, ?, ?) ON CONFLICT(sha256) DO NOTHING`},
		{&w.link, `INSERT INTO event_blobs (event_id, position, sha256) VALUES (?, ?, ?) ON CONFLICT(event_id, position) DO UPDATE SET sha256 = excluded.sha256`},
		{&w.unlink, `DELETE FROM event_blobs WHERE event_id = ?`},
	} {
		st, err := w.tx.PrepareContext(w.ctx, p.q)
		if err != nil {
			w.err = fmt.Errorf("store: prepare: %w", err)
			return w.err
		}
		*p.dst = st
	}
	return nil
}

func (w *blobWriter) close() {
	for _, st := range []*sql.Stmt{w.ins, w.link, w.unlink} {
		if st != nil {
			_ = st.Close()
		}
	}
}

// add stores the attachments of a newly inserted event.
func (w *blobWriter) add(e model.Event, res *WriteResult) error {
	if len(e.Attachments) == 0 {
		return nil
	}
	if err := w.prepare(); err != nil {
		return err
	}
	for i, a := range e.Attachments {
		h := blobref.Hash(a.Data)
		if a.SHA256 != "" && a.SHA256 != h {
			return fmt.Errorf("store: event %s: attachment %d: hash %s does not match its data (%s)", e.ID, i, a.SHA256, h)
		}
		stored, enc, err := encodeBlob(a.MediaType, a.Data)
		if err != nil {
			return err
		}
		r, err := w.ins.ExecContext(w.ctx, h, a.MediaType, len(a.Data), enc, stored)
		if err != nil {
			return fmt.Errorf("store: event %s: store blob %s: %w", e.ID, h, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Blobs++
			res.BlobBytes += int64(len(a.Data))
		}
		if _, err := w.link.ExecContext(w.ctx, e.ID, i, h); err != nil {
			return fmt.Errorf("store: event %s: link blob %s: %w", e.ID, h, err)
		}
	}
	return nil
}

// replace stores the attachments of a rewritten event in place of the
// ones its old raw line had.
func (w *blobWriter) replace(e model.Event, res *WriteResult) error {
	if err := w.prepare(); err != nil {
		return err
	}
	if _, err := w.unlink.ExecContext(w.ctx, e.ID); err != nil {
		return fmt.Errorf("store: event %s: unlink blobs: %w", e.ID, err)
	}
	return w.add(e, res)
}

// Blob returns an attachment by its SHA-256 (full hex digest or a unique
// prefix) with its data decoded. ErrNotFound for an unknown hash,
// ErrAmbiguous for a prefix that matches several.
func (s *Store) Blob(ctx context.Context, ref string) (BlobInfo, []byte, error) {
	if ref == "" {
		return BlobInfo{}, nil, ErrAmbiguous
	}
	var info BlobInfo
	var enc string
	var stored []byte
	q := `SELECT sha256, media_type, bytes, length(data), encoding, data FROM blobs WHERE sha256 = ?`
	args := []any{ref}
	if len(ref) < 64 {
		q = `SELECT sha256, media_type, bytes, length(data), encoding, data FROM blobs WHERE sha256 >= ? AND sha256 < ? LIMIT 2`
		args = []any{ref, ref + "￿"}
	}
	rows, err := s.db().QueryContext(ctx, q, args...)
	if err != nil {
		return BlobInfo{}, nil, fmt.Errorf("store: blob %s: %w", ref, err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		n++
		if n > 1 {
			return BlobInfo{}, nil, ErrAmbiguous
		}
		if err := rows.Scan(&info.SHA256, &info.MediaType, &info.Bytes, &info.StoredBytes, &enc, &stored); err != nil {
			return BlobInfo{}, nil, fmt.Errorf("store: blob %s: %w", ref, err)
		}
	}
	if err := rows.Err(); err != nil {
		return BlobInfo{}, nil, fmt.Errorf("store: blob %s: %w", ref, err)
	}
	if n == 0 {
		return BlobInfo{}, nil, ErrNotFound
	}
	data, err := decodeBlob(enc, stored)
	if err != nil {
		return BlobInfo{}, nil, fmt.Errorf("store: blob %s: %w", info.SHA256, err)
	}
	return info, data, nil
}

// BlobEvents lists the IDs of the events whose raw line refers to the
// blob with the full hash sha256, in archive order.
func (s *Store) BlobEvents(ctx context.Context, sha256 string) ([]string, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT DISTINCT eb.event_id FROM event_blobs eb LEFT JOIN events e ON e.id = eb.event_id
		WHERE eb.sha256 = ? ORDER BY e.seq, eb.event_id`, sha256)
	if err != nil {
		return nil, fmt.Errorf("store: blob events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: blob events: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SessionAttachments returns the attachments of every event of a session
// that has any, by event ID and in position order: metadata only, no data.
func (s *Store) SessionAttachments(ctx context.Context, sessionID string) (map[string][]EventAttachment, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT eb.event_id, eb.position, b.sha256, b.media_type, b.bytes, length(b.data)
		FROM event_blobs eb JOIN events e ON e.id = eb.event_id JOIN blobs b ON b.sha256 = eb.sha256
		WHERE e.session_id = ? ORDER BY e.seq, eb.position`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: session attachments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]EventAttachment{}
	for rows.Next() {
		var id string
		var a EventAttachment
		if err := rows.Scan(&id, &a.Position, &a.SHA256, &a.MediaType, &a.Bytes, &a.StoredBytes); err != nil {
			return nil, fmt.Errorf("store: session attachments: %w", err)
		}
		out[id] = append(out[id], a)
	}
	return out, rows.Err()
}

// BlobStats counts the stored attachments.
func (s *Store) BlobStats(ctx context.Context) (BlobStats, error) {
	var st BlobStats
	if err := s.db().QueryRowContext(ctx, `SELECT count(*), COALESCE(sum(bytes), 0), COALESCE(sum(length(data)), 0) FROM blobs`).
		Scan(&st.Count, &st.Bytes, &st.StoredBytes); err != nil {
		return st, fmt.Errorf("store: blob stats: %w", err)
	}
	return st, nil
}

// RawLine returns the raw line of an event as the transcript had it: the
// stored line with the data of every referenced blob put back
// (blobref.Restore). "" for an event without raw; ErrNotFound for an
// unknown event; an error naming the hash when a referenced blob is
// missing.
func (s *Store) RawLine(ctx context.Context, eventID string) (string, error) {
	raw, _, ok, err := s.rawOf(ctx, eventID)
	if err != nil {
		return "", fmt.Errorf("store: raw line %s: %w", eventID, err)
	}
	if !ok {
		return "", ErrNotFound
	}
	if raw == "" {
		return "", nil
	}
	line, err := blobref.Restore([]byte(raw), func(sha string) ([]byte, error) {
		_, data, err := s.Blob(ctx, sha)
		if errors.Is(err, ErrNotFound) {
			return nil, errors.New("not in the archive")
		}
		return data, err
	})
	if err != nil {
		return "", fmt.Errorf("store: raw line %s: %w", eventID, err)
	}
	return string(line), nil
}
