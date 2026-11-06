package screenshot

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// originals are the names of the Activity fixture that must not survive
// the anonymization: project directories, the remote host, a branch and a
// session title.
var originals = []string{"alpha", "beta", "gamma", "example.test", "feature", "Beta release", "/w/alpha"}

func anonymized(t *testing.T) (Report, *sql.DB, string) {
	t.Helper()
	src, srcPath := fixturedb.Activity(t)
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	// The fixture records all carry the branch main; give the worktree
	// sessions their own one, as a real archive would have.
	rw, err := sql.Open("sqlite", "file:"+srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rw.Exec("UPDATE sessions SET git_branch = 'feature' WHERE cwd = ?", testfixture.ActivityAlphaWorktree); err != nil {
		t.Fatal(err)
	}
	// Since v1.3.0 the archive holds images and the JSON values of meta
	// records as blobs: a screenshot linked to an existing event and a
	// meta record carrying a system prompt, both on existing rows so the
	// counts the other tests compare stay as they are. The contents are
	// plain bytes here; the store compresses them, which is why the text
	// scan (Scan) cannot find them and the tables must be emptied.
	for _, q := range []string{
		`INSERT INTO blobs (sha256, media_type, bytes, encoding, data) VALUES ('aa11', 'image/png', 9, '', x'89504e47426574612073')`,
		`INSERT INTO blobs (sha256, media_type, bytes, encoding, data) VALUES ('bb22', 'application/json', 22, '', CAST('{"prompt":"Beta release"}' AS BLOB))`,
		`INSERT INTO event_blobs (event_id, position, sha256) SELECT id, 0, 'aa11' FROM events ORDER BY seq LIMIT 1`,
		`INSERT INTO session_meta (id, session_id, ts, type, raw) SELECT 'claude:meta-1', id, started_at, 'api-request-shape', CAST('Beta release' AS BLOB) FROM sessions ORDER BY id LIMIT 1`,
		`INSERT INTO meta_blobs (meta_id, position, sha256) VALUES ('claude:meta-1', 0, 'bb22')`,
	} {
		if _, err := rw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := rw.Close(); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "anon.db")
	rep, err := Anonymize(context.Background(), srcPath, dst)
	if err != nil {
		t.Fatalf("Anonymize: %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+dst+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return rep, db, dst
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// TestAnonymizeRemovesOriginals: no original project, path, remote, branch
// or title is left in any table, and the full-text index is empty.
func TestAnonymizeRemovesOriginals(t *testing.T) {
	_, db, _ := anonymized(t)
	if hit, err := Scan(context.Background(), db, originals); err != nil {
		t.Fatal(err)
	} else if hit != "" {
		t.Errorf("original data left in %s", hit)
	}
	for _, q := range []string{
		"SELECT count(*) FROM events WHERE text IS NOT NULL OR raw IS NOT NULL OR raw_truncated <> 0 OR tool_name IS NOT NULL OR source_id IS NOT NULL",
		"SELECT count(*) FROM sessions WHERE name IS NOT NULL OR ai_title IS NOT NULL",
		"SELECT count(*) FROM sources",
		"SELECT count(*) FROM dirs",
		`SELECT count(*) FROM events_fts WHERE events_fts MATCH '"release"'`,
		// Attachments, system prompts and meta records hold content the
		// text scan cannot see (compressed BLOBs): nothing of them stays.
		"SELECT count(*) FROM blobs",
		"SELECT count(*) FROM event_blobs",
		"SELECT count(*) FROM meta_blobs",
		"SELECT count(*) FROM session_meta",
	} {
		if n := count(t, db, q); n != 0 {
			t.Errorf("%s = %d, want 0", q, n)
		}
	}
}

// TestAnonymizeKeepsActivity: the counts, times and actors the stats and
// activity views draw from are unchanged, and the subagent keeps its parent.
func TestAnonymizeKeepsActivity(t *testing.T) {
	rep, db, _ := anonymized(t)
	total := 0
	for _, c := range testfixture.ActivityTotals {
		total += c.Human + c.Agent + c.Tool
	}
	s := testfixture.ActivitySubagentCount
	total += s.Peer + s.Agent + s.Tool
	if n := count(t, db, "SELECT count(*) FROM events"); n != total || rep.Events != total {
		t.Errorf("events = %d (report %d), want %d", n, rep.Events, total)
	}
	if n := count(t, db, "SELECT count(*) FROM sessions"); n != 17 || rep.Sessions != 17 {
		t.Errorf("sessions = %d (report %d), want 17", n, rep.Sessions)
	}
	if n := count(t, db, "SELECT count(*) FROM events WHERE actor = 'human'"); n != 22 {
		t.Errorf("human events = %d, want 22", n)
	}
	if n := count(t, db, "SELECT count(*) FROM events WHERE ts IS NULL OR ts_local IS NULL"); n != 0 {
		t.Errorf("%d events without time", n)
	}
	if n := count(t, db, "SELECT count(*) FROM sessions WHERE parent_id IS NOT NULL"); n != 1 {
		t.Errorf("subagents = %d, want 1", n)
	}
}

// TestAnonymizeNames: projects are named by their activity rank from the
// word list (alpha has the most events, then beta, then gamma), the
// worktree becomes a directory below the project, remotes and branches are
// neutral (the subagent has no branch and keeps none), and the store reads
// the result.
func TestAnonymizeNames(t *testing.T) {
	_, db, path := anonymized(t)
	rows, err := db.Query("SELECT DISTINCT cwd, COALESCE(git_root, ''), COALESCE(git_remote_url, ''), COALESCE(git_branch, '') FROM sessions ORDER BY cwd, git_branch")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var cwd, root, remote, branch string
		if err := rows.Scan(&cwd, &root, &remote, &branch); err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{cwd, root, remote, branch}, " "))
	}
	want := []string{
		"/w/atlas /w/atlas https://git.invalid/demo/atlas ",
		"/w/atlas /w/atlas https://git.invalid/demo/atlas main",
		"/w/atlas__worktrees/wt2 /w/atlas https://git.invalid/demo/atlas topic-2",
		"/w/kompass   main",
		"/w/nordlicht /w/nordlicht https://git.invalid/demo/nordlicht main",
	}
	if !slices.Equal(got, want) {
		t.Errorf("sessions =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	st, err := store.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("store opens the copy: %v", err)
	}
	defer func() { _ = st.Close() }()
	hits, err := st.Search(context.Background(), "release", store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("search finds %d hits in the anonymized copy", len(hits))
	}
}

// TestAnonymizeNeedles: the report names what to look for in the rendered
// frames: the original directory names, repository paths and titles, but
// nothing shorter than four characters.
func TestAnonymizeNeedles(t *testing.T) {
	rep, _, _ := anonymized(t)
	for _, want := range []string{"alpha", "beta", "gamma", "feature", "o/alpha", "o/beta", "Beta release"} {
		if !slices.Contains(rep.Needles, want) {
			t.Errorf("needles lack %q: %q", want, rep.Needles)
		}
	}
	for _, n := range rep.Needles {
		if len(n) < 4 {
			t.Errorf("needle %q is too short", n)
		}
	}
}

// TestScanText finds the first needle in a frame and ignores clean text.
func TestScanText(t *testing.T) {
	tests := []struct {
		text, want string
	}{
		{"a frame with /Users/someone in it", "/Users"},
		{"clean frame", ""},
		{"second needle gamma", "gamma"},
	}
	for _, tt := range tests {
		if got := ScanText(tt.text, []string{"/Users", "gamma"}); got != tt.want {
			t.Errorf("ScanText(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}
