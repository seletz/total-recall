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
		"SELECT count(*) FROM events WHERE text IS NOT NULL OR raw IS NOT NULL OR raw_truncated <> 0 OR tool_name IS NOT NULL OR source_path IS NOT NULL",
		"SELECT count(*) FROM sessions WHERE name IS NOT NULL OR ai_title IS NOT NULL",
		"SELECT count(*) FROM sources",
		"SELECT count(*) FROM dirs",
		`SELECT count(*) FROM events_fts WHERE events_fts MATCH '"release"'`,
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
