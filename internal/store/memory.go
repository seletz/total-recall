package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/seletz/total-recall/internal/blobref"
)

// Project memory (schema v8, #192): the files a harness keeps as memory
// per project, versioned. The store knows no harness paths: a project is
// a harness name and the adapter's native key (Claude Code: the directory
// name below the projects dir) with the project's working directory once
// the adapter found it; a file is a path relative to the memory
// directory. Ingest scans the files, redacts the content and hands the
// changes to WriteMemory; the content goes to blobs once per hash. The
// read functions serve the memory command and the TUI view that follows:
// projects, files now or at a point in time, the versions of a file, the
// content of a version, and the full-text search over the current
// content (memory_fts, apart from the event index).

// MemoryMediaType is the media type of a memory file's blob unless the
// change names another one.
const MemoryMediaType = "text/markdown"

// MemoryUpdate is what one scan saw of a project's memory.
type MemoryUpdate struct {
	// Harness and Key name the project: the harness and its native project
	// key. Both are required.
	Harness, Key string
	// Cwd is the project's working directory; "" keeps the stored one.
	Cwd string
	// Observed is the time of the scan (default: now); a deletion is
	// recorded at this time.
	Observed time.Time
	// Changes are the files that are new, changed or gone since the last
	// scan. A file whose content did not change only updates its size and
	// mtime.
	Changes []MemoryChange
}

// MemoryChange is one file of a MemoryUpdate.
type MemoryChange struct {
	// Path is relative to the memory directory, slash-separated, without
	// "." or ".." components.
	Path string
	// Size and MTime are the file's state on disk, kept to find changes
	// without reading the file (MemoryState).
	Size  int64
	MTime time.Time
	// Content is the redacted content; ignored for a deletion.
	Content []byte
	// MediaType of the content; default MemoryMediaType.
	MediaType string
	// Deleted records that the file is gone.
	Deleted bool
}

// MemoryWriteResult counts what WriteMemory stored.
type MemoryWriteResult struct {
	// Versions are new contents (new files, changes, files that came
	// back), Deleted the deletions, Touched files whose size or mtime
	// changed but not their content.
	Versions, Deleted, Touched int
	// Blobs counts contents the archive did not have yet, BlobBytes their
	// size.
	Blobs     int
	BlobBytes int64
}

// MemoryFileState is a file's last known state for the next scan.
type MemoryFileState struct {
	Size  int64
	MTime time.Time
	// SHA256 of the current content; "" when the file is deleted.
	SHA256 string
}

// MemoryProjectState is the last known state of a project's memory.
type MemoryProjectState struct {
	Cwd string
	// Files by path, deleted ones included.
	Files map[string]MemoryFileState
}

// checkMemoryPath refuses paths that are empty, absolute or leave the
// memory directory.
func checkMemoryPath(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return fmt.Errorf("store: memory file path %q: want a clean path relative to the memory directory", p)
	}
	return nil
}

// WriteMemory records one scan of a project's memory in one transaction:
// the project (and its cwd), and per change a new version with the
// content as a blob, a deletion, or only the new size and mtime when the
// content is the same. The full-text index follows the current content.
func (s *Store) WriteMemory(ctx context.Context, u MemoryUpdate) (MemoryWriteResult, error) {
	var res MemoryWriteResult
	if s.readOnly {
		return res, fmt.Errorf("store: read-only")
	}
	if u.Harness == "" || u.Key == "" {
		return res, fmt.Errorf("store: memory update without harness or key")
	}
	for _, c := range u.Changes {
		if err := checkMemoryPath(c.Path); err != nil {
			return res, err
		}
	}
	observed := u.Observed
	if observed.IsZero() {
		observed = s.now()
	}
	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO memory_projects (harness, key, cwd) VALUES (?, ?, ?)
		ON CONFLICT(harness, key) DO UPDATE SET cwd = COALESCE(excluded.cwd, memory_projects.cwd)`, u.Harness, u.Key, nullStr(u.Cwd)); err != nil {
		return res, fmt.Errorf("store: memory project %s:%s: %w", u.Harness, u.Key, err)
	}
	var pid int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM memory_projects WHERE harness = ? AND key = ?`, u.Harness, u.Key).Scan(&pid); err != nil {
		return res, fmt.Errorf("store: memory project %s:%s: %w", u.Harness, u.Key, err)
	}
	for _, c := range u.Changes {
		if err := writeMemoryChange(ctx, tx, pid, c, observed, &res); err != nil {
			return res, fmt.Errorf("store: memory %s:%s %s: %w", u.Harness, u.Key, c.Path, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("store: commit: %w", err)
	}
	return res, nil
}

func writeMemoryChange(ctx context.Context, tx *sql.Tx, pid int64, c MemoryChange, observed time.Time, res *MemoryWriteResult) error {
	var fid int64
	var cur sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id, sha256 FROM memory_files WHERE project_id = ? AND path = ?`, pid, c.Path).Scan(&fid, &cur)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if c.Deleted {
		if !exists || !cur.Valid {
			return nil
		}
		for _, q := range []struct {
			q    string
			args []any
		}{
			{`UPDATE memory_files SET sha256 = NULL, size = 0, mtime = NULL WHERE id = ?`, []any{fid}},
			{`INSERT INTO memory_versions (file_id, ts, observed_at, sha256, bytes) VALUES (?, ?, ?, NULL, 0)`, []any{fid, formatTime(observed), formatTime(observed)}},
			{`DELETE FROM memory_fts WHERE rowid = ?`, []any{fid}},
		} {
			if _, err := tx.ExecContext(ctx, q.q, q.args...); err != nil {
				return err
			}
		}
		res.Deleted++
		return nil
	}
	h := blobref.Hash(c.Content)
	if exists && cur.Valid && cur.String == h {
		if _, err := tx.ExecContext(ctx, `UPDATE memory_files SET size = ?, mtime = ? WHERE id = ?`, c.Size, formatTime(c.MTime), fid); err != nil {
			return err
		}
		res.Touched++
		return nil
	}
	mt := c.MediaType
	if mt == "" {
		mt = MemoryMediaType
	}
	isNew, err := insertBlob(ctx, tx, h, mt, c.Content)
	if err != nil {
		return err
	}
	if isNew {
		res.Blobs++
		res.BlobBytes += int64(len(c.Content))
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `UPDATE memory_files SET sha256 = ?, size = ?, mtime = ? WHERE id = ?`, h, c.Size, formatTime(c.MTime), fid); err != nil {
			return err
		}
	} else {
		r, err := tx.ExecContext(ctx, `INSERT INTO memory_files (project_id, path, sha256, size, mtime) VALUES (?, ?, ?, ?, ?)`, pid, c.Path, h, c.Size, formatTime(c.MTime))
		if err != nil {
			return err
		}
		if fid, err = r.LastInsertId(); err != nil {
			return err
		}
	}
	at := c.MTime
	if at.IsZero() {
		at = observed
	}
	for _, q := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO memory_versions (file_id, ts, observed_at, sha256, bytes) VALUES (?, ?, ?, ?, ?)`, []any{fid, formatTime(at), formatTime(observed), h, len(c.Content)}},
		{`DELETE FROM memory_fts WHERE rowid = ?`, []any{fid}},
		{`INSERT INTO memory_fts (rowid, text) VALUES (?, ?)`, []any{fid, string(c.Content)}},
	} {
		if _, err := tx.ExecContext(ctx, q.q, q.args...); err != nil {
			return err
		}
	}
	res.Versions++
	return nil
}

// MemoryState returns the last known state of every memory project of a
// harness by key, for the next scan.
func (s *Store) MemoryState(ctx context.Context, harness string) (map[string]MemoryProjectState, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT p.key, COALESCE(p.cwd, ''), f.path, COALESCE(f.sha256, ''), COALESCE(f.size, 0), f.mtime
		FROM memory_projects p LEFT JOIN memory_files f ON f.project_id = p.id WHERE p.harness = ?`, harness)
	if err != nil {
		return nil, fmt.Errorf("store: memory state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]MemoryProjectState{}
	for rows.Next() {
		var key, cwd, sha string
		var p, mtime sql.NullString
		var size int64
		if err := rows.Scan(&key, &cwd, &p, &sha, &size, &mtime); err != nil {
			return nil, fmt.Errorf("store: memory state: %w", err)
		}
		ps, ok := out[key]
		if !ok {
			ps = MemoryProjectState{Cwd: cwd, Files: map[string]MemoryFileState{}}
			out[key] = ps
		}
		if p.Valid {
			ps.Files[p.String] = MemoryFileState{Size: size, MTime: parseTime(mtime), SHA256: sha}
		}
	}
	return out, rows.Err()
}

// MemoryProject is a project with memory in the archive.
type MemoryProject struct {
	ID           int64
	Harness, Key string
	// Cwd is the project's working directory, "" while unknown; GitRoot
	// and GitRemoteURL come from the dirs cache for it.
	Cwd, GitRoot, GitRemoteURL string
	// Files counts the existing files, Deleted the deleted ones, Versions
	// all versions including deletions.
	Files, Deleted, Versions int
	// LastChange is the time of the latest version.
	LastChange time.Time
}

// MemoryProjects lists the projects with memory, by harness and key.
func (s *Store) MemoryProjects(ctx context.Context) ([]MemoryProject, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT p.id, p.harness, p.key, COALESCE(p.cwd, ''), COALESCE(d.git_root, ''), COALESCE(d.git_remote_url, ''),
		(SELECT count(*) FROM memory_files f WHERE f.project_id = p.id AND f.sha256 IS NOT NULL),
		(SELECT count(*) FROM memory_files f WHERE f.project_id = p.id AND f.sha256 IS NULL),
		(SELECT count(*) FROM memory_versions v JOIN memory_files f ON f.id = v.file_id WHERE f.project_id = p.id),
		(SELECT max(v.ts) FROM memory_versions v JOIN memory_files f ON f.id = v.file_id WHERE f.project_id = p.id)
		FROM memory_projects p LEFT JOIN dirs d ON d.cwd = p.cwd ORDER BY p.harness, p.key`)
	if err != nil {
		return nil, fmt.Errorf("store: memory projects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []MemoryProject{}
	for rows.Next() {
		var p MemoryProject
		var last sql.NullString
		if err := rows.Scan(&p.ID, &p.Harness, &p.Key, &p.Cwd, &p.GitRoot, &p.GitRemoteURL, &p.Files, &p.Deleted, &p.Versions, &last); err != nil {
			return nil, fmt.Errorf("store: memory projects: %w", err)
		}
		p.LastChange = parseTime(last)
		out = append(out, p)
	}
	return out, rows.Err()
}

// MemoryFilter selects memory files.
type MemoryFilter struct {
	// ProjectIDs restricts to these projects; empty means all.
	ProjectIDs []int64
	// At is the point in time; zero means now.
	At time.Time
	// Deleted includes files that are deleted at that time.
	Deleted bool
}

// MemoryFile is a memory file in the version valid at a point in time.
type MemoryFile struct {
	ProjectID int64
	Path      string
	// SHA256 of the content, "" for a deleted file.
	SHA256  string
	Bytes   int64
	Deleted bool
	// Changed is the time of this version (the file's mtime, or the scan
	// that found it gone).
	Changed time.Time
	// Versions counts the file's versions up to that time.
	Versions int
}

// projectCond adds "AND <col> IN (…)" for ids to a query.
func projectCond(col string, ids []int64, args []any) (string, []any) {
	if len(ids) == 0 {
		return "", args
	}
	ph := make([]string, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args = append(args, id)
	}
	return " AND " + col + " IN (" + strings.Join(ph, ", ") + ")", args
}

// MemoryFiles lists the memory files as they were at f.At (now when
// zero), by project and path.
func (s *Store) MemoryFiles(ctx context.Context, f MemoryFilter) ([]MemoryFile, error) {
	var args []any
	until := ""
	if !f.At.IsZero() {
		until = "WHERE v.ts <= ?"
		args = append(args, formatTime(f.At))
	}
	q := `WITH cur AS (SELECT v.file_id, max(v.seq) AS seq, count(*) AS n FROM memory_versions v ` + until + ` GROUP BY v.file_id)
		SELECT mf.project_id, mf.path, COALESCE(v.sha256, ''), v.bytes, v.ts, cur.n
		FROM cur JOIN memory_versions v ON v.seq = cur.seq JOIN memory_files mf ON mf.id = cur.file_id WHERE 1`
	if !f.Deleted {
		q += " AND v.sha256 IS NOT NULL"
	}
	cond, args := projectCond("mf.project_id", f.ProjectIDs, args)
	q += cond + " ORDER BY mf.project_id, mf.path"
	rows, err := s.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: memory files: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []MemoryFile{}
	for rows.Next() {
		var m MemoryFile
		var at sql.NullString
		if err := rows.Scan(&m.ProjectID, &m.Path, &m.SHA256, &m.Bytes, &at, &m.Versions); err != nil {
			return nil, fmt.Errorf("store: memory files: %w", err)
		}
		m.Changed, m.Deleted = parseTime(at), m.SHA256 == ""
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemoryVersion is one version of a memory file.
type MemoryVersion struct {
	Seq int64
	// TS is the time of the version (the file's mtime, or the scan that
	// found it gone), Observed the time of the scan that recorded it.
	TS, Observed time.Time
	// SHA256 of the content ("" for a deletion) and its size.
	SHA256  string
	Bytes   int64
	Deleted bool
}

const memoryVersionCols = `v.seq, v.ts, v.observed_at, COALESCE(v.sha256, ''), v.bytes`

func scanMemoryVersion(sc scanner) (MemoryVersion, error) {
	var v MemoryVersion
	var at, obs sql.NullString
	if err := sc.Scan(&v.Seq, &at, &obs, &v.SHA256, &v.Bytes); err != nil {
		return v, err
	}
	v.TS, v.Observed, v.Deleted = parseTime(at), parseTime(obs), v.SHA256 == ""
	return v, nil
}

// MemoryVersions lists the versions of a project's file, oldest first.
// ErrNotFound when the project has no such file.
func (s *Store) MemoryVersions(ctx context.Context, projectID int64, file string) ([]MemoryVersion, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT `+memoryVersionCols+` FROM memory_versions v JOIN memory_files f ON f.id = v.file_id
		WHERE f.project_id = ? AND f.path = ? ORDER BY v.seq`, projectID, file)
	if err != nil {
		return nil, fmt.Errorf("store: memory versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []MemoryVersion
	for rows.Next() {
		v, err := scanMemoryVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("store: memory versions: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: memory versions: %w", err)
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// MemoryContent returns the version of a project's file valid at at (now
// when zero) and its content. ErrNotFound when the file did not exist
// then (unknown, not yet written, or deleted).
func (s *Store) MemoryContent(ctx context.Context, projectID int64, file string, at time.Time) (MemoryVersion, string, error) {
	q := `SELECT ` + memoryVersionCols + ` FROM memory_versions v JOIN memory_files f ON f.id = v.file_id WHERE f.project_id = ? AND f.path = ?`
	args := []any{projectID, file}
	if !at.IsZero() {
		q += " AND v.ts <= ?"
		args = append(args, formatTime(at))
	}
	v, err := scanMemoryVersion(s.db().QueryRowContext(ctx, q+" ORDER BY v.seq DESC LIMIT 1", args...))
	if errors.Is(err, sql.ErrNoRows) {
		return v, "", ErrNotFound
	}
	if err != nil {
		return v, "", fmt.Errorf("store: memory content: %w", err)
	}
	if v.Deleted {
		return v, "", ErrNotFound
	}
	_, data, err := s.Blob(ctx, v.SHA256)
	if err != nil {
		return v, "", fmt.Errorf("store: memory content %s: %w", file, err)
	}
	return v, string(data), nil
}

// MemorySearchOptions narrow SearchMemory.
type MemorySearchOptions struct {
	// ProjectIDs restricts to these projects; empty means all.
	ProjectIDs []int64
	// Raw passes the query to FTS5 unchanged; otherwise every term is
	// quoted and all must match (like Search).
	Raw bool
	// Limit caps the hits; 0 means no limit.
	Limit int
	// Since/Until restrict the time of the current version to
	// [Since, Until); zero does not restrict.
	Since, Until time.Time
	// Repo is a case-insensitive substring of the project's working
	// directory, git root or remote URL; Harness an exact harness name.
	Repo, Harness string
	// HighlightStart/End wrap matches in snippets; default "[" and "]".
	HighlightStart, HighlightEnd string
}

// MemoryHit is a memory file whose current content matches.
type MemoryHit struct {
	ProjectID int64
	Path      string
	SHA256    string
	Snippet   string
	// Changed is the time of the current version.
	Changed time.Time
	Rank    float64 // bm25, lower is better
	// Harness and ProjectKey are the harness and its native key of the
	// memory project; Project is its raw project key (the git root, else
	// the working directory; empty while the directory is unknown).
	Harness, ProjectKey string
	Project             ProjectRef
}

// SearchMemory runs a full-text query over the current content of the
// memory files (memory_fts), best matches first. It does not touch the
// event index; the typed search of a later version can call it as the
// search of the type memory.
func (s *Store) SearchMemory(ctx context.Context, query string, o MemorySearchOptions) ([]MemoryHit, error) {
	if !o.Raw {
		query = ftsQuery(query)
	}
	if strings.TrimSpace(query) == "" {
		return []MemoryHit{}, nil
	}
	hs, he := o.HighlightStart, o.HighlightEnd
	if hs == "" && he == "" {
		hs, he = "[", "]"
	}
	args := []any{hs, he, query}
	const changed = `(SELECT v.ts FROM memory_versions v WHERE v.file_id = f.id ORDER BY v.seq DESC LIMIT 1)`
	q := `SELECT f.project_id, f.path, COALESCE(f.sha256, ''), snippet(memory_fts, 0, ?, ?, '…', 16), bm25(memory_fts), ` + changed + `,
		mp.harness, mp.key, COALESCE(NULLIF(d.git_root, ''), mp.cwd, ''), COALESCE(d.git_root, '') = '', COALESCE(d.git_remote_url, '')
		FROM memory_fts JOIN memory_files f ON f.id = memory_fts.rowid
		JOIN memory_projects mp ON mp.id = f.project_id
		LEFT JOIN dirs d ON d.cwd = mp.cwd
		WHERE memory_fts MATCH ?`
	cond, args := projectCond("f.project_id", o.ProjectIDs, args)
	q += cond
	if !o.Since.IsZero() {
		q += " AND " + changed + " >= ?"
		args = append(args, formatTime(o.Since))
	}
	if !o.Until.IsZero() {
		q += " AND " + changed + " < ?"
		args = append(args, formatTime(o.Until))
	}
	if o.Repo != "" {
		q += ` AND (mp.cwd LIKE ? ESCAPE '\' OR d.git_root LIKE ? ESCAPE '\' OR d.git_remote_url LIKE ? ESCAPE '\')`
		a := likeArg(o.Repo)
		args = append(args, a, a, a)
	}
	if o.Harness != "" {
		q += " AND mp.harness = ?"
		args = append(args, o.Harness)
	}
	q += " ORDER BY bm25(memory_fts), f.project_id, f.path"
	if o.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", o.Limit)
	}
	rows, err := s.db().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: search memory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []MemoryHit{}
	for rows.Next() {
		var h MemoryHit
		var at sql.NullString
		if err := rows.Scan(&h.ProjectID, &h.Path, &h.SHA256, &h.Snippet, &h.Rank, &at,
			&h.Harness, &h.ProjectKey, &h.Project.Key, &h.Project.Dir, &h.Project.Remote); err != nil {
			return nil, fmt.Errorf("store: search memory: %w", err)
		}
		h.Changed, h.Project = parseTime(at), h.Project.clean()
		if h.Project.Key == "" {
			h.Project = ProjectRef{}
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: search memory: %w", err)
	}
	return out, nil
}

// MemoryStats counts the archived memory for status.
type MemoryStats struct {
	// Projects with memory, existing and deleted files, versions.
	Projects, Files, Deleted, Versions int64
	// Blobs counts the distinct contents, Bytes their size, StoredBytes
	// their compressed size in the archive.
	Blobs, Bytes, StoredBytes int64
}

// MemoryStats counts projects, files, versions and the stored contents.
func (s *Store) MemoryStats(ctx context.Context) (MemoryStats, error) {
	var st MemoryStats
	err := s.db().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM memory_projects),
		(SELECT count(*) FROM memory_files WHERE sha256 IS NOT NULL),
		(SELECT count(*) FROM memory_files WHERE sha256 IS NULL),
		(SELECT count(*) FROM memory_versions)`).Scan(&st.Projects, &st.Files, &st.Deleted, &st.Versions)
	if err == nil {
		st.Blobs, st.Bytes, st.StoredBytes, err = s.blobSizes(ctx, `SELECT sha256 FROM memory_versions WHERE sha256 IS NOT NULL`)
	}
	if err != nil {
		return st, fmt.Errorf("store: memory stats: %w", err)
	}
	return st, nil
}

// MemoryUse is a memory file that had a blob as content.
type MemoryUse struct {
	ProjectID    int64
	Harness, Key string
	Path         string
	// TS is the time of the file's first version with this content.
	TS time.Time
}

// BlobMemory lists the memory files that had the blob with the full hash
// sha256 as content, in the order the archive saw them.
func (s *Store) BlobMemory(ctx context.Context, sha256 string) ([]MemoryUse, error) {
	rows, err := s.db().QueryContext(ctx, `SELECT p.id, p.harness, p.key, f.path, min(v.ts)
		FROM memory_versions v JOIN memory_files f ON f.id = v.file_id JOIN memory_projects p ON p.id = f.project_id
		WHERE v.sha256 = ? GROUP BY f.id ORDER BY min(v.seq)`, sha256)
	if err != nil {
		return nil, fmt.Errorf("store: blob memory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []MemoryUse{}
	for rows.Next() {
		var u MemoryUse
		var at sql.NullString
		if err := rows.Scan(&u.ProjectID, &u.Harness, &u.Key, &u.Path, &at); err != nil {
			return nil, fmt.Errorf("store: blob memory: %w", err)
		}
		u.TS = parseTime(at)
		out = append(out, u)
	}
	return out, rows.Err()
}
