package store

// Spike #347: delivering a local archive to a central one.
//
// A pool host keeps its local archive (the hooks write there as always, so
// the local archive is the buffer when the central one is unreachable).
// Export reads what is new since the last acknowledged delivery as a
// Changeset of rows, Import writes a changeset into the central archive in
// one transaction and records the delivering host on each session. Rows
// are copied as stored: text and raw are already redacted by the host,
// raw stays a compressed frame, blobs keep their stored encoding. Import is
// idempotent (IDs, ON CONFLICT DO NOTHING), so a batch delivered twice
// after a lost acknowledgement changes nothing; the sender advances its
// SyncState only after the central archive acknowledged the batch.

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/blobref"
	"github.com/seletz/total-recall/internal/model"
)

// SyncState is what a sender has delivered to one central archive: the
// highest event and meta seq acknowledged and a hash of every session row
// as it was sent (session rows change without new events, e.g. a rename).
type SyncState struct {
	Events   int64             `json:"events"`
	Meta     int64             `json:"meta"`
	Sessions map[string]string `json:"sessions"`
}

// Commit advances the state past an acknowledged changeset.
func (st *SyncState) Commit(cs Changeset) {
	if cs.NextEvents > st.Events {
		st.Events = cs.NextEvents
	}
	if cs.NextMeta > st.Meta {
		st.Meta = cs.NextMeta
	}
	if st.Sessions == nil {
		st.Sessions = map[string]string{}
	}
	for _, s := range cs.Sessions {
		st.Sessions[s.ID] = sessionHash(s)
	}
}

// Changeset is one delivery: rows of the sender's archive, as stored.
type Changeset struct {
	// Schema is the sender's schema version; a central archive refuses a
	// newer one (it might not read the rows, e.g. a new raw dictionary).
	Schema   int             `json:"schema"`
	Sessions []model.Session `json:"sessions,omitempty"`
	Sources  []SyncSource    `json:"sources,omitempty"`
	Events   []SyncEvent     `json:"events,omitempty"`
	Meta     []SyncMeta      `json:"meta,omitempty"`
	Blobs    []SyncBlob      `json:"blobs,omitempty"`
	// NextEvents and NextMeta are the highest seq in the changeset (the
	// sender's state after acknowledgement).
	NextEvents int64 `json:"next_events"`
	NextMeta   int64 `json:"next_meta"`
}

// Empty reports whether the changeset carries nothing.
func (cs Changeset) Empty() bool {
	return len(cs.Sessions) == 0 && len(cs.Events) == 0 && len(cs.Meta) == 0
}

// SyncSource is a sources row of the sender.
type SyncSource struct {
	Path       string `json:"path"`
	Harness    string `json:"harness"`
	Size       int64  `json:"size"`
	MTime      string `json:"mtime,omitempty"`
	Offset     int64  `json:"offset"`
	IngestedAt string `json:"ingested_at,omitempty"`
}

// SyncEvent is an events row of the sender with its blob links.
type SyncEvent struct {
	ID               string   `json:"id"`
	SessionID        string   `json:"session_id"`
	TS               string   `json:"ts"`
	Kind             string   `json:"kind"`
	Role             *string  `json:"role,omitempty"`
	Actor            *string  `json:"actor,omitempty"`
	Text             *string  `json:"text,omitempty"`
	ToolName         *string  `json:"tool_name,omitempty"`
	Model            *string  `json:"model,omitempty"`
	TokensIn         *int64   `json:"tokens_in,omitempty"`
	TokensOut        *int64   `json:"tokens_out,omitempty"`
	TokensCacheRead  *int64   `json:"tokens_cache_read,omitempty"`
	TokensCacheWrite *int64   `json:"tokens_cache_write,omitempty"`
	SourcePath       string   `json:"source_path,omitempty"`
	SourceOffset     *int64   `json:"source_offset,omitempty"`
	Raw              []byte   `json:"raw,omitempty"`
	RawTruncated     int64    `json:"raw_truncated,omitempty"`
	InputKind        *string  `json:"input_kind,omitempty"`
	Blobs            []string `json:"blobs,omitempty"` // sha256 by position
}

// SyncMeta is a session_meta row of the sender with its blob links.
type SyncMeta struct {
	ID           string   `json:"id"`
	SessionID    string   `json:"session_id"`
	TS           *string  `json:"ts,omitempty"`
	Type         string   `json:"type"`
	SourcePath   string   `json:"source_path,omitempty"`
	SourceOffset *int64   `json:"source_offset,omitempty"`
	Raw          []byte   `json:"raw"`
	Blobs        []string `json:"blobs,omitempty"`
}

// SyncBlob is a blobs row in its stored form.
type SyncBlob struct {
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes"`
	Encoding  string `json:"encoding,omitempty"`
	Data      []byte `json:"data"`
}

// ImportResult counts what Import changed.
type ImportResult struct {
	Sessions int `json:"sessions"` // sessions new to the central archive
	Events   int `json:"events"`   // events inserted
	Meta     int `json:"meta"`     // meta records inserted
	Blobs    int `json:"blobs"`    // blobs new to the central archive
	// Conflicts are sessions the central archive already holds from
	// another host (or ingested itself); their rows are not written.
	Conflicts int `json:"conflicts"`
}

// Add sums r into the receiver.
func (r *ImportResult) Add(o ImportResult) {
	r.Sessions += o.Sessions
	r.Events += o.Events
	r.Meta += o.Meta
	r.Blobs += o.Blobs
	r.Conflicts += o.Conflicts
}

func sessionHash(s model.Session) string {
	b, _ := json.Marshal(s)
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}

// Export returns the rows new since st, at most limit events and limit meta
// records (0 = all), plus every session row that changed since st or that
// a returned row belongs to, the sources and blobs the rows reference. st
// is not changed; call st.Commit after the central archive acknowledged.
func (s *Store) Export(ctx context.Context, st *SyncState, limit int) (Changeset, error) {
	cs := Changeset{Schema: s.version, NextEvents: st.Events, NextMeta: st.Meta}
	lim := ""
	if limit > 0 {
		lim = fmt.Sprintf(" LIMIT %d", limit)
	}
	db := s.db()
	need := map[string]bool{}   // sessions the rows belong to
	paths := map[string]bool{}  // sources the rows reference
	hashes := map[string]bool{} // blobs the rows reference
	links := func(table, col, id string) ([]string, error) {
		rows, err := db.QueryContext(ctx, "SELECT sha256 FROM "+table+" WHERE "+col+" = ? ORDER BY position", id)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				return nil, err
			}
			out = append(out, h)
			hashes[h] = true
		}
		return out, rows.Err()
	}

	rows, err := db.QueryContext(ctx, `SELECT e.seq, e.id, e.session_id, e.ts, e.kind, e.role, e.actor, e.text, e.tool_name, e.model,
		e.tokens_in, e.tokens_out, e.tokens_cache_read, e.tokens_cache_write, src.path, e.source_offset, e.raw, e.raw_truncated, e.input_kind
		FROM events e LEFT JOIN sources src ON src.id = e.source_id WHERE e.seq > ? ORDER BY e.seq`+lim, st.Events)
	if err != nil {
		return cs, fmt.Errorf("store: export events: %w", err)
	}
	for rows.Next() {
		var e SyncEvent
		var seq int64
		var path sql.NullString
		if err := rows.Scan(&seq, &e.ID, &e.SessionID, &e.TS, &e.Kind, &e.Role, &e.Actor, &e.Text, &e.ToolName, &e.Model,
			&e.TokensIn, &e.TokensOut, &e.TokensCacheRead, &e.TokensCacheWrite, &path, &e.SourceOffset, &e.Raw, &e.RawTruncated, &e.InputKind); err != nil {
			_ = rows.Close()
			return cs, fmt.Errorf("store: export events: %w", err)
		}
		e.SourcePath = path.String
		cs.Events = append(cs.Events, e)
		cs.NextEvents = seq
		need[e.SessionID] = true
		if e.SourcePath != "" {
			paths[e.SourcePath] = true
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return cs, fmt.Errorf("store: export events: %w", err)
	}
	for i := range cs.Events {
		if cs.Events[i].Blobs, err = links("event_blobs", "event_id", cs.Events[i].ID); err != nil {
			return cs, fmt.Errorf("store: export event blobs: %w", err)
		}
	}

	rows, err = db.QueryContext(ctx, `SELECT m.seq, m.id, m.session_id, m.ts, m.type, src.path, m.source_offset, m.raw
		FROM session_meta m LEFT JOIN sources src ON src.id = m.source_id WHERE m.seq > ? ORDER BY m.seq`+lim, st.Meta)
	if err != nil {
		return cs, fmt.Errorf("store: export meta: %w", err)
	}
	for rows.Next() {
		var m SyncMeta
		var seq int64
		var path sql.NullString
		if err := rows.Scan(&seq, &m.ID, &m.SessionID, &m.TS, &m.Type, &path, &m.SourceOffset, &m.Raw); err != nil {
			_ = rows.Close()
			return cs, fmt.Errorf("store: export meta: %w", err)
		}
		m.SourcePath = path.String
		cs.Meta = append(cs.Meta, m)
		cs.NextMeta = seq
		need[m.SessionID] = true
		if m.SourcePath != "" {
			paths[m.SourcePath] = true
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return cs, fmt.Errorf("store: export meta: %w", err)
	}
	for i := range cs.Meta {
		if cs.Meta[i].Blobs, err = links("meta_blobs", "meta_id", cs.Meta[i].ID); err != nil {
			return cs, fmt.Errorf("store: export meta blobs: %w", err)
		}
	}

	// Sessions: changed ones and the ones the rows belong to. A full scan
	// of the session rows; an archive has thousands, not millions. The
	// real implementation would track a change counter per row instead.
	rows, err = db.QueryContext(ctx, "SELECT "+sessionCols+" FROM sessions ORDER BY id")
	if err != nil {
		return cs, fmt.Errorf("store: export sessions: %w", err)
	}
	for rows.Next() {
		m, err := scanSession(rows)
		if err != nil {
			_ = rows.Close()
			return cs, fmt.Errorf("store: export sessions: %w", err)
		}
		if need[m.ID] || st.Sessions[m.ID] != sessionHash(m) {
			cs.Sessions = append(cs.Sessions, m)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return cs, fmt.Errorf("store: export sessions: %w", err)
	}

	for _, p := range sortedKeys(paths) {
		var src SyncSource
		var mtime, at sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT path, harness, size, mtime, "offset", ingested_at FROM sources WHERE path = ?`, p).
			Scan(&src.Path, &src.Harness, &src.Size, &mtime, &src.Offset, &at); err != nil {
			return cs, fmt.Errorf("store: export source %s: %w", p, err)
		}
		src.MTime, src.IngestedAt = mtime.String, at.String
		cs.Sources = append(cs.Sources, src)
	}
	for _, h := range sortedKeys(hashes) {
		var b SyncBlob
		if err := db.QueryRowContext(ctx, `SELECT sha256, media_type, bytes, encoding, data FROM blobs WHERE sha256 = ?`, h).
			Scan(&b.SHA256, &b.MediaType, &b.Bytes, &b.Encoding, &b.Data); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // store_blobs = false: the reference stays, the data was never kept
			}
			return cs, fmt.Errorf("store: export blob %s: %w", h, err)
		}
		cs.Blobs = append(cs.Blobs, b)
	}
	return cs, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Import writes a changeset delivered by host into this (central) archive
// in one transaction. Sessions get host as their provenance; a session the
// archive already has from another host, or from its own ingest, is a
// conflict and none of its rows are written. Source paths are stored as
// "<host>:<path>" (the same path exists on every host of a pool). ts_local
// is computed in this archive's time zone, not the host's. Blobs are
// checked against their hash. Rows that exist already are skipped.
func (s *Store) Import(ctx context.Context, host string, cs Changeset) (ImportResult, error) {
	var res ImportResult
	if s.readOnly {
		return res, fmt.Errorf("store: read-only")
	}
	if host == "" || strings.ContainsAny(host, ":/ ") {
		return res, fmt.Errorf("store: import: invalid host name %q", host)
	}
	if cs.Schema > LatestSchemaVersion() {
		return res, fmt.Errorf("store: import: changeset has schema v%d, this archive knows v%d: update the central archive first", cs.Schema, LatestSchemaVersion())
	}
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// owner: the host a session belongs to centrally; "" = unknown,
	// "\x00local" = ingested by the central archive itself.
	owner := map[string]string{}
	ownerOf := func(id string) (string, error) {
		if o, ok := owner[id]; ok {
			return o, nil
		}
		var h sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT host FROM sessions WHERE id = ?", id).Scan(&h)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			owner[id] = ""
		case err != nil:
			return "", fmt.Errorf("store: import: session %s: %w", id, err)
		case !h.Valid:
			owner[id] = "\x00local"
		default:
			owner[id] = h.String
		}
		return owner[id], nil
	}
	conflict := map[string]bool{}
	for _, m := range cs.Sessions {
		o, err := ownerOf(m.ID)
		if err != nil {
			return res, err
		}
		if o != "" && o != host {
			conflict[m.ID] = true
			res.Conflicts++
			continue
		}
		old, err := getSession(ctx, tx, m.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return res, err
		}
		if err := putSession(ctx, tx, mergeSession(old, m)); err != nil {
			return res, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE sessions SET host = ? WHERE id = ?", host, m.ID); err != nil {
			return res, fmt.Errorf("store: import: host of %s: %w", m.ID, err)
		}
		if o == "" {
			res.Sessions++
		}
		owner[m.ID] = host
	}
	writable := func(sessionID string) (bool, error) {
		if conflict[sessionID] {
			return false, nil
		}
		o, err := ownerOf(sessionID)
		if err != nil {
			return false, err
		}
		if o != host {
			return false, fmt.Errorf("store: import: row of session %s, which the changeset does not carry", sessionID)
		}
		return true, nil
	}

	srcIDs := map[string]int64{}
	for _, src := range cs.Sources {
		var id int64
		if err := tx.QueryRowContext(ctx, `INSERT INTO sources (path, harness, size, mtime, "offset", ingested_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET size = excluded.size, mtime = excluded.mtime, "offset" = excluded."offset", ingested_at = excluded.ingested_at
			RETURNING id`, host+":"+src.Path, src.Harness, src.Size, nullStr(src.MTime), src.Offset, nullStr(src.IngestedAt)).Scan(&id); err != nil {
			return res, fmt.Errorf("store: import source %s: %w", src.Path, err)
		}
		srcIDs[src.Path] = id
	}
	srcOf := func(path string) any {
		if id, ok := srcIDs[path]; ok {
			return id
		}
		return nil
	}

	for _, b := range cs.Blobs {
		data, err := decodeBlob(b.Encoding, b.Data)
		if err != nil {
			return res, fmt.Errorf("store: import blob %s: %w", b.SHA256, err)
		}
		if h := blobref.Hash(data); h != b.SHA256 || int64(len(data)) != b.Bytes {
			return res, fmt.Errorf("store: import blob %s: data does not match its hash (%s) or size", b.SHA256, h)
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO blobs (sha256, media_type, bytes, encoding, data) VALUES (?, ?, ?, ?, ?) ON CONFLICT(sha256) DO NOTHING`,
			b.SHA256, b.MediaType, b.Bytes, b.Encoding, b.Data)
		if err != nil {
			return res, fmt.Errorf("store: import blob %s: %w", b.SHA256, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Blobs++
		}
	}
	link := func(table, col, id string, hashes []string) error {
		for i, h := range hashes {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s (%s, position, sha256) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, table, col), id, i, h); err != nil {
				return fmt.Errorf("store: import %s of %s: %w", table, id, err)
			}
		}
		return nil
	}

	ins, err := tx.PrepareContext(ctx, `INSERT INTO events
		(id, session_id, ts, ts_local, kind, role, actor, text, tool_name, model, tokens_in, tokens_out, tokens_cache_read, tokens_cache_write, source_id, source_offset, raw, raw_truncated, input_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`)
	if err != nil {
		return res, fmt.Errorf("store: prepare: %w", err)
	}
	defer func() { _ = ins.Close() }()
	for _, e := range cs.Events {
		ok, err := writable(e.SessionID)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		var local any
		if t, err := time.Parse(time.RFC3339Nano, e.TS); err == nil {
			local = t.In(s.loc).Format(localTimeFormat)
		}
		r, err := ins.ExecContext(ctx, e.ID, e.SessionID, e.TS, local, e.Kind, e.Role, e.Actor, e.Text, e.ToolName, e.Model,
			e.TokensIn, e.TokensOut, e.TokensCacheRead, e.TokensCacheWrite, srcOf(e.SourcePath), e.SourceOffset, e.Raw, e.RawTruncated, e.InputKind)
		if err != nil {
			return res, fmt.Errorf("store: import event %s: %w", e.ID, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Events++
			if err := link("event_blobs", "event_id", e.ID, e.Blobs); err != nil {
				return res, err
			}
		}
	}
	for _, m := range cs.Meta {
		ok, err := writable(m.SessionID)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO session_meta (id, session_id, ts, type, source_id, source_offset, raw)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`, m.ID, m.SessionID, m.TS, m.Type, srcOf(m.SourcePath), m.SourceOffset, m.Raw)
		if err != nil {
			return res, fmt.Errorf("store: import meta record %s: %w", m.ID, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Meta++
			if err := link("meta_blobs", "meta_id", m.ID, m.Blobs); err != nil {
				return res, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("store: commit: %w", err)
	}
	return res, nil
}
