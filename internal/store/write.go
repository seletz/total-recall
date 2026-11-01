package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// SourceState is the ingest progress of one transcript file (table sources).
type SourceState struct {
	Path       string
	Harness    string
	Size       int64
	MTime      time.Time
	Offset     int64 // end of the last complete line read
	IngestedAt time.Time
}

// DirInfo is a cached git resolution of a working directory (table dirs).
type DirInfo struct {
	Cwd          string
	GitRoot      string
	GitRemoteURL string
	ResolvedAt   time.Time
}

// mergeSession combines a stored session (old) with new facts (new) following
// the upsert rule of docs/design.md:
//   - StartedAt = minimum, EndedAt = maximum of the non-zero values;
//   - empty values never overwrite set ones;
//   - "last seen" fields (HarnessVersion, GitBranch, Model) take new's value
//     if new is at least as recent (new.EndedAt >= old.EndedAt); without a
//     timestamp they only fill empty fields;
//   - "first seen" fields (Cwd, GitRoot, GitRemoteURL) take new's value only
//     if new started earlier; otherwise they only fill empty fields;
//   - identity fields (Harness, NativeID, ParentID) are set once;
//   - Name, AITitle and AgentName: the last non-empty value written wins (titles can
//     be renamed; the adapter decides what is authoritative).
func mergeSession(old, new model.Session) model.Session {
	m := old
	if m.ID == "" {
		m.ID = new.ID
	}
	fill := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	fill(&m.Harness, new.Harness)
	fill(&m.NativeID, new.NativeID)
	fill(&m.ParentID, new.ParentID)

	earlier := !new.StartedAt.IsZero() && (old.StartedAt.IsZero() || new.StartedAt.Before(old.StartedAt))
	for _, f := range []struct {
		dst *string
		v   string
	}{{&m.Cwd, new.Cwd}, {&m.GitRoot, new.GitRoot}, {&m.GitRemoteURL, new.GitRemoteURL}} {
		if f.v != "" && (earlier || *f.dst == "") {
			*f.dst = f.v
		}
	}

	newer := !new.EndedAt.IsZero() && (old.EndedAt.IsZero() || !new.EndedAt.Before(old.EndedAt))
	for _, f := range []struct {
		dst *string
		v   string
	}{{&m.HarnessVersion, new.HarnessVersion}, {&m.GitBranch, new.GitBranch}, {&m.Model, new.Model}} {
		if f.v != "" && (newer || *f.dst == "") {
			*f.dst = f.v
		}
	}

	if new.Name != "" {
		m.Name = new.Name
	}
	if new.AITitle != "" {
		m.AITitle = new.AITitle
	}
	if new.AgentName != "" {
		m.AgentName = new.AgentName
	}
	if !new.StartedAt.IsZero() && (m.StartedAt.IsZero() || new.StartedAt.Before(m.StartedAt)) {
		m.StartedAt = new.StartedAt
	}
	if !new.EndedAt.IsZero() && (m.EndedAt.IsZero() || new.EndedAt.After(m.EndedAt)) {
		m.EndedAt = new.EndedAt
	}
	return m
}

// splitID splits "<harness>:<native id>".
func splitID(id string) (harness, native string) {
	if i := strings.IndexByte(id, ':'); i > 0 {
		return id[:i], id[i+1:]
	}
	return "", id
}

// WriteBatch stores one ingest step in a single transaction: it upserts the
// given sessions (merge rule, see mergeSession), inserts events (existing IDs
// are skipped, so re-reading a file is harmless) and records the source
// offset. Events whose session is not in sessions create/extend a minimal
// session row; event timestamps always widen the session's start/end.
// If src.Path is empty, no source row is written.
func (s *Store) WriteBatch(ctx context.Context, src SourceState, sessions []model.Session, events []model.Event) error {
	_, err := s.WriteBatchN(ctx, src, sessions, events)
	return err
}

// WriteBatchN is WriteBatch that also returns the number of events actually
// inserted (events whose ID already existed are not counted).
func (s *Store) WriteBatchN(ctx context.Context, src SourceState, sessions []model.Session, events []model.Event) (int, error) {
	if s.readOnly {
		return 0, fmt.Errorf("store: read-only")
	}
	inserted := 0
	incoming := map[string]model.Session{}
	var order []string
	add := func(ss model.Session) {
		cur, ok := incoming[ss.ID]
		if !ok {
			order = append(order, ss.ID)
		}
		incoming[ss.ID] = mergeSession(cur, ss)
	}
	for _, ss := range sessions {
		if ss.ID == "" {
			return 0, fmt.Errorf("store: session without ID")
		}
		add(ss)
	}
	for _, e := range events {
		if e.SessionID == "" || e.ID == "" {
			return 0, fmt.Errorf("store: event %q without ID or session", e.ID)
		}
		h, n := splitID(e.SessionID)
		add(model.Session{ID: e.SessionID, Harness: h, NativeID: n, StartedAt: e.TS, EndedAt: e.TS})
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, id := range order {
		old, err := getSession(ctx, tx, id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return 0, err
		}
		if err := putSession(ctx, tx, mergeSession(old, incoming[id])); err != nil {
			return 0, err
		}
	}

	if len(events) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO events
			(id, session_id, ts, ts_local, kind, role, actor, text, tool_name, model, tokens_in, tokens_out, source_path, source_offset, raw, raw_truncated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING`)
		if err != nil {
			return 0, fmt.Errorf("store: prepare: %w", err)
		}
		defer func() { _ = stmt.Close() }()
		for _, e := range events {
			var local any
			if !e.TS.IsZero() {
				local = e.TS.In(s.loc).Format(localTimeFormat)
			}
			tsv := formatTime(e.TS)
			if tsv == nil {
				tsv = ""
			}
			res, err := stmt.ExecContext(ctx, e.ID, e.SessionID, tsv, local, string(e.Kind), nullStr(e.Role),
				nullStr(string(e.Actor)), e.Text, nullStr(e.ToolName), nullStr(e.Model), nullInt(e.TokensIn),
				nullInt(e.TokensOut), nullStr(e.SourcePath), e.SourceOffset, nullStr(e.Raw), e.RawTruncated)
			if err != nil {
				return 0, fmt.Errorf("store: insert event %s: %w", e.ID, err)
			}
			if n, err := res.RowsAffected(); err == nil {
				inserted += int(n)
			}
		}
	}

	if src.Path != "" {
		at := src.IngestedAt
		if at.IsZero() {
			at = s.now()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sources (path, harness, size, mtime, "offset", ingested_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(path) DO UPDATE SET harness = excluded.harness, size = excluded.size,
				mtime = excluded.mtime, "offset" = excluded."offset", ingested_at = excluded.ingested_at`,
			src.Path, src.Harness, src.Size, formatTime(src.MTime), src.Offset, formatTime(at)); err != nil {
			return 0, fmt.Errorf("store: update source: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return inserted, nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const sessionCols = `id, harness, harness_version, native_id, parent_id, name, agent_name, cwd,
	git_root, git_remote_url, git_branch, model, started_at, ended_at, ai_title`

type scanner interface{ Scan(dest ...any) error }

// scanSession scans sessionCols plus extra destinations.
func scanSession(sc scanner, extra ...any) (model.Session, error) {
	var m model.Session
	var ns [10]sql.NullString
	var start, end, aiTitle sql.NullString
	dest := []any{&m.ID, &m.Harness}
	for i := range ns {
		dest = append(dest, &ns[i])
	}
	dest = append(dest, &start, &end, &aiTitle)
	dest = append(dest, extra...)
	if err := sc.Scan(dest...); err != nil {
		return m, err
	}
	m.HarnessVersion, m.NativeID, m.ParentID, m.Name, m.AgentName, m.Cwd = ns[0].String, ns[1].String, ns[2].String, ns[3].String, ns[4].String, ns[5].String
	m.GitRoot, m.GitRemoteURL, m.GitBranch, m.Model = ns[6].String, ns[7].String, ns[8].String, ns[9].String
	m.StartedAt, m.EndedAt = parseTime(start), parseTime(end)
	m.AITitle = aiTitle.String
	return m, nil
}

func getSession(ctx context.Context, q queryer, id string) (model.Session, error) {
	m, err := scanSession(q.QueryRowContext(ctx, "SELECT "+sessionCols+" FROM sessions WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Session{}, ErrNotFound
	}
	if err != nil {
		return model.Session{}, fmt.Errorf("store: get session %s: %w", id, err)
	}
	return m, nil
}

func putSession(ctx context.Context, tx *sql.Tx, m model.Session) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO sessions (`+sessionCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET harness = excluded.harness, harness_version = excluded.harness_version,
			native_id = excluded.native_id, parent_id = excluded.parent_id, name = excluded.name,
			agent_name = excluded.agent_name, cwd = excluded.cwd, git_root = excluded.git_root,
			git_remote_url = excluded.git_remote_url, git_branch = excluded.git_branch, model = excluded.model,
			started_at = excluded.started_at, ended_at = excluded.ended_at, ai_title = excluded.ai_title`,
		m.ID, m.Harness, nullStr(m.HarnessVersion), nullStr(m.NativeID), nullStr(m.ParentID), nullStr(m.Name),
		nullStr(m.AgentName), nullStr(m.Cwd), nullStr(m.GitRoot), nullStr(m.GitRemoteURL), nullStr(m.GitBranch),
		nullStr(m.Model), formatTime(m.StartedAt), formatTime(m.EndedAt), nullStr(m.AITitle))
	if err != nil {
		return fmt.Errorf("store: upsert session %s: %w", m.ID, err)
	}
	return nil
}

// Source returns the stored ingest state of path; ok is false if unknown.
func (s *Store) Source(ctx context.Context, path string) (SourceState, bool, error) {
	st := SourceState{Path: path}
	var mtime, at sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT harness, size, mtime, "offset", ingested_at FROM sources WHERE path = ?`, path).
		Scan(&st.Harness, &st.Size, &mtime, &st.Offset, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceState{}, false, nil
	}
	if err != nil {
		return SourceState{}, false, fmt.Errorf("store: source %s: %w", path, err)
	}
	st.MTime, st.IngestedAt = parseTime(mtime), parseTime(at)
	return st, true, nil
}

// Dir returns the cached git info for cwd; ok is false if not cached.
func (s *Store) Dir(ctx context.Context, cwd string) (DirInfo, bool, error) {
	d := DirInfo{Cwd: cwd}
	var root, url, at sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT git_root, git_remote_url, resolved_at FROM dirs WHERE cwd = ?`, cwd).Scan(&root, &url, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return DirInfo{}, false, nil
	}
	if err != nil {
		return DirInfo{}, false, fmt.Errorf("store: dir %s: %w", cwd, err)
	}
	d.GitRoot, d.GitRemoteURL, d.ResolvedAt = root.String, url.String, parseTime(at)
	return d, true, nil
}

// PutDir stores or replaces the cached git info for d.Cwd.
func (s *Store) PutDir(ctx context.Context, d DirInfo) error {
	if s.readOnly {
		return fmt.Errorf("store: read-only")
	}
	at := d.ResolvedAt
	if at.IsZero() {
		at = s.now()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO dirs (cwd, git_root, git_remote_url, resolved_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(cwd) DO UPDATE SET git_root = excluded.git_root, git_remote_url = excluded.git_remote_url,
			resolved_at = excluded.resolved_at`,
		d.Cwd, nullStr(d.GitRoot), nullStr(d.GitRemoteURL), formatTime(at))
	if err != nil {
		return fmt.Errorf("store: put dir %s: %w", d.Cwd, err)
	}
	return nil
}

// SessionCwds returns the distinct non-empty working directories of all
// sessions, sorted.
func (s *Store) SessionCwds(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT cwd FROM sessions WHERE cwd IS NOT NULL AND cwd <> '' ORDER BY cwd")
	if err != nil {
		return nil, fmt.Errorf("store: session cwds: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("store: session cwds: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetSessionGit sets git root and remote URL of all sessions whose cwd is
// cwd and returns how many rows changed. Unlike WriteBatch it overwrites
// existing values: it is used to repair stale git info (ingest --regit).
func (s *Store) SetSessionGit(ctx context.Context, cwd, root, remoteURL string) (int64, error) {
	if s.readOnly {
		return 0, fmt.Errorf("store: read-only")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET git_root = ?, git_remote_url = ?
		WHERE cwd = ? AND (git_root IS NOT ? OR git_remote_url IS NOT ?)`,
		nullStr(root), nullStr(remoteURL), cwd, nullStr(root), nullStr(remoteURL))
	if err != nil {
		return 0, fmt.Errorf("store: set session git %s: %w", cwd, err)
	}
	return res.RowsAffected()
}
