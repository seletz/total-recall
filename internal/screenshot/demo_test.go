package screenshot

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/seletz/total-recall/internal/store"
)

func demoStore(t *testing.T) *store.Store {
	t.Helper()
	path, err := BuildDemo(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("BuildDemo: %v", err)
	}
	st, err := store.OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestDemoSearch: the demo archive answers the query of the search
// screenshot with hits in more than one session, so the list and the
// preview have something to show.
func TestDemoSearch(t *testing.T) {
	st := demoStore(t)
	hits, err := st.Search(context.Background(), DemoQuery, store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 4 {
		t.Fatalf("%q finds %d hits, want at least 4", DemoQuery, len(hits))
	}
	sessions := map[string]bool{}
	for _, h := range hits {
		sessions[h.Event.SessionID] = true
	}
	if len(sessions) < 2 {
		t.Errorf("hits in %d sessions, want at least 2", len(sessions))
	}
	if hits[0].SessionName == "" || strings.HasPrefix(hits[0].SessionName, "claude:") {
		t.Errorf("first hit has no title: %q", hits[0].SessionName)
	}
	if !strings.Contains(hits[0].Repo, "kompass") {
		t.Errorf("first hit repo = %q, want the demo repository", hits[0].Repo)
	}
}

// TestDemoIsSynthetic: nothing in the demo archive looks like a real path
// or user, and every session has a title and events.
func TestDemoIsSynthetic(t *testing.T) {
	path, err := BuildDemo(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if hit, err := Scan(context.Background(), db, []string{"/Users", "seletz", "/tmp", t.TempDir()}); err != nil {
		t.Fatal(err)
	} else if hit != "" {
		t.Errorf("demo archive holds a real path in %s", hit)
	}
	if n := count(t, db, "SELECT count(*) FROM sessions"); n != len(demoSessions) {
		t.Errorf("sessions = %d, want %d", n, len(demoSessions))
	}
	if n := count(t, db, "SELECT count(*) FROM sessions WHERE display_name IS NULL OR git_remote_url IS NULL"); n != 0 {
		t.Errorf("%d sessions without title or remote", n)
	}
	if n := count(t, db, "SELECT count(*) FROM events WHERE kind = 'tool_use'"); n == 0 {
		t.Error("no tool_use events; the session view shows tools")
	}
}

// TestDemoDeterministic: two builds yield the same sessions and events,
// also into a directory that already holds an earlier build.
func TestDemoDeterministic(t *testing.T) {
	dir := t.TempDir()
	dump := func() string {
		path, err := BuildDemo(context.Background(), dir)
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.Close() }()
		rows, err := db.Query("SELECT id, session_id, ts, actor, text FROM events ORDER BY seq")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var b strings.Builder
		for rows.Next() {
			var id, sid, ts, actor string
			var text sql.NullString
			if err := rows.Scan(&id, &sid, &ts, &actor, &text); err != nil {
				t.Fatal(err)
			}
			b.WriteString(strings.Join([]string{id, sid, ts, actor, text.String}, "\t") + "\n")
		}
		return b.String()
	}
	if a, b := dump(), dump(); a != b {
		t.Error("two builds differ")
	}
}
