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
// (SessionAttachments) or nothing. Meta records (meta.go) use the same
// writer with the link table meta_blobs.

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

// blobWriter stores the attachments of events or meta records within one
// transaction: the blobs once per hash, the links in linkTable
// (event_blobs or meta_blobs), whose first column idColumn holds the
// owner's ID.
type blobWriter struct {
	ctx                 context.Context
	tx                  *sql.Tx
	linkTable, idColumn string
	ins, link, unlink   *sql.Stmt
	prepared            bool
	err                 error
}

func newBlobWriter(ctx context.Context, tx *sql.Tx, linkTable, idColumn string) *blobWriter {
	return &blobWriter{ctx: ctx, tx: tx, linkTable: linkTable, idColumn: idColumn}
}

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
		{&w.link, fmt.Sprintf(`INSERT INTO %s (%s, position, sha256) VALUES (?, ?, ?) ON CONFLICT(%[2]s, position) DO UPDATE SET sha256 = excluded.sha256`, w.linkTable, w.idColumn)},
		{&w.unlink, fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, w.linkTable, w.idColumn)},
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

// add stores the attachments of a newly inserted event or meta record id.
func (w *blobWriter) add(id string, atts []model.Attachment, res *WriteResult) error {
	if len(atts) == 0 {
		return nil
	}
	if err := w.prepare(); err != nil {
		return err
	}
	for i, a := range atts {
		h := blobref.Hash(a.Data)
		if a.SHA256 != "" && a.SHA256 != h {
			return fmt.Errorf("store: %s: attachment %d: hash %s does not match its data (%s)", id, i, a.SHA256, h)
		}
		stored, enc, err := encodeBlob(a.MediaType, a.Data)
		if err != nil {
			return err
		}
		r, err := w.ins.ExecContext(w.ctx, h, a.MediaType, len(a.Data), enc, stored)
		if err != nil {
			return fmt.Errorf("store: %s: store blob %s: %w", id, h, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Blobs++
			res.BlobBytes += int64(len(a.Data))
		}
		if _, err := w.link.ExecContext(w.ctx, id, i, h); err != nil {
			return fmt.Errorf("store: %s: link blob %s: %w", id, h, err)
		}
	}
	return nil
}

// replace stores the attachments of a rewritten event or meta record in
// place of the ones its old raw line had.
func (w *blobWriter) replace(id string, atts []model.Attachment, res *WriteResult) error {
	if err := w.prepare(); err != nil {
		return err
	}
	if _, err := w.unlink.ExecContext(w.ctx, id); err != nil {
		return fmt.Errorf("store: %s: unlink blobs: %w", id, err)
	}
	return w.add(id, atts, res)
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

// BlobMeta lists the IDs of the meta records whose line refers to the
// blob with the full hash sha256, in archive order.
func (s *Store) BlobMeta(ctx context.Context, sha256 string) ([]string, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT DISTINCT mb.meta_id FROM meta_blobs mb LEFT JOIN session_meta m ON m.id = mb.meta_id
		WHERE mb.sha256 = ? ORDER BY m.seq, mb.meta_id`, sha256)
	if err != nil {
		return nil, fmt.Errorf("store: blob meta records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: blob meta records: %w", err)
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

// insertBlob stores data under its hash h unless the archive has it
// (memory files; events and meta records use blobWriter). isNew reports
// whether the blob was new.
func insertBlob(ctx context.Context, tx *sql.Tx, h, mediaType string, data []byte) (isNew bool, err error) {
	stored, enc, err := encodeBlob(mediaType, data)
	if err != nil {
		return false, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, media_type, bytes, encoding, data) VALUES (?, ?, ?, ?, ?) ON CONFLICT(sha256) DO NOTHING`,
		h, mediaType, len(data), enc, stored)
	if err != nil {
		return false, fmt.Errorf("store blob %s: %w", h, err)
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// blobSizes counts the blobs whose hash the subquery hashes returns, their
// size as delivered and as stored.
func (s *Store) blobSizes(ctx context.Context, hashes string) (count, bytes, stored int64, err error) {
	err = s.db().QueryRowContext(ctx, `SELECT count(*), COALESCE(sum(bytes), 0), COALESCE(sum(length(data)), 0) FROM blobs WHERE sha256 IN (`+hashes+`)`).
		Scan(&count, &bytes, &stored)
	return count, bytes, stored, err
}
