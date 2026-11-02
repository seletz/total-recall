package fixturedb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tf "github.com/seletz/total-recall/internal/testfixture"
)

var ctx = context.Background()

// row is one human/agent/tool event with its session's dimension columns.
type row struct {
	ts                      time.Time
	actor, session, project string
	parent                  string
}

func loadRows(t *testing.T, st interface {
	Query(context.Context, string) ([]string, [][]any, error)
}) []row {
	t.Helper()
	_, rows, err := st.Query(ctx, `SELECT e.ts, e.actor, e.session_id,
		COALESCE(NULLIF(s.git_root, ''), s.cwd), COALESCE(s.parent_id, '')
		FROM events e JOIN sessions s ON s.id = e.session_id`)
	if err != nil {
		t.Fatal(err)
	}
	var out []row
	for _, r := range rows {
		ts, err := time.Parse(time.RFC3339Nano, r[0].(string))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, row{ts: ts, actor: r[1].(string), session: r[2].(string), project: r[3].(string), parent: r[4].(string)})
	}
	return out
}

func isoWeek(ts time.Time) string {
	y, w := ts.In(tf.ActivityLocation).ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

func TestActivityWeeks(t *testing.T) {
	st, path := Activity(t)
	if path == "" {
		t.Fatal("empty db path")
	}
	rows := loadRows(t, st)

	type cell struct {
		sessions map[string]bool
		c        tf.ActivityCount
	}
	got := map[string]*cell{} // "<project>|<week>"
	for _, r := range rows {
		if r.parent != "" {
			continue // the subagent is documented separately
		}
		k := r.project + "|" + isoWeek(r.ts)
		c := got[k]
		if c == nil {
			c = &cell{sessions: map[string]bool{}}
			got[k] = c
		}
		switch r.actor {
		case "human":
			c.c.Human++
			c.sessions[r.session] = true
		case "agent":
			c.c.Agent++
			c.sessions[r.session] = true
		case "tool":
			c.c.Tool++
		default:
			t.Errorf("unexpected actor %q in top-level session %s", r.actor, r.session)
		}
	}
	seen := map[string]bool{}
	for _, w := range tf.ActivityWeeks {
		t.Run(w.Project+"/"+w.Week, func(t *testing.T) {
			k := w.Project + "|" + w.Week
			seen[k] = true
			c := got[k]
			if c == nil {
				t.Fatalf("no events for %s in %s", w.Project, w.Week)
			}
			if len(c.sessions) != w.Sessions || c.c != w.ActivityCount {
				t.Errorf("got sessions=%d %+v, want sessions=%d %+v", len(c.sessions), c.c, w.Sessions, w.ActivityCount)
			}
		})
	}
	for k := range got {
		if !seen[k] {
			t.Errorf("undocumented activity in %s: %+v", k, got[k].c)
		}
	}
}

func TestActivityTotals(t *testing.T) {
	st, _ := Activity(t)
	rows := loadRows(t, st)
	got := map[string]*struct {
		sessions map[string]bool
		c        tf.ActivityCount
	}{}
	for _, r := range rows {
		if r.parent != "" {
			continue
		}
		c := got[r.project]
		if c == nil {
			c = &struct {
				sessions map[string]bool
				c        tf.ActivityCount
			}{sessions: map[string]bool{}}
			got[r.project] = c
		}
		c.sessions[r.session] = true
		switch r.actor {
		case "human":
			c.c.Human++
		case "agent":
			c.c.Agent++
		case "tool":
			c.c.Tool++
		}
	}
	for _, tt := range tf.ActivityTotals {
		t.Run(tt.Project, func(t *testing.T) {
			c := got[tt.Project]
			if c == nil {
				t.Fatalf("no events for project %q", tt.Project)
			}
			if len(c.sessions) != tt.Sessions || c.c != tt.ActivityCount {
				t.Errorf("got sessions=%d %+v, want sessions=%d %+v", len(c.sessions), c.c, tt.Sessions, tt.ActivityCount)
			}
		})
	}
	if len(got) != len(tf.ActivityTotals) {
		t.Errorf("%d projects in db, %d documented", len(got), len(tf.ActivityTotals))
	}
}

func TestActivitySubagent(t *testing.T) {
	st, _ := Activity(t)
	rows := loadRows(t, st)
	var peer, agent, tool int
	for _, r := range rows {
		if r.parent != "claude:"+tf.ActivitySessionAlpha1 {
			continue
		}
		if r.session != "claude:"+tf.ActivitySubagent {
			t.Errorf("subagent session id %q, want %q", r.session, "claude:"+tf.ActivitySubagent)
		}
		if r.project != tf.ActivityAlpha {
			t.Errorf("subagent project key %q, want %q", r.project, tf.ActivityAlpha)
		}
		switch r.actor {
		case "peer":
			peer++
		case "agent":
			agent++
		case "tool":
			tool++
		}
	}
	want := tf.ActivitySubagentCount
	if got := (tf.ActivityCount{Peer: peer, Agent: agent, Tool: tool}); got != want {
		t.Errorf("subagent events %+v, want %+v", got, want)
	}
}

// TestActivityGit checks the deterministic git resolution: worktree sessions
// share alpha's root and remote, gamma has none.
func TestActivityGit(t *testing.T) {
	st, _ := Activity(t)
	_, rows, err := st.Query(ctx, `SELECT cwd, COALESCE(git_root, ''), COALESCE(git_remote_url, '') FROM sessions WHERE parent_id IS NULL GROUP BY cwd, git_root, git_remote_url ORDER BY cwd`)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ cwd, root, remote string }{
		{tf.ActivityAlpha, tf.ActivityAlpha, tf.ActivityAlphaRemote},
		{tf.ActivityAlphaWorktree, tf.ActivityAlpha, tf.ActivityAlphaRemote},
		{tf.ActivityBeta, tf.ActivityBeta, tf.ActivityBetaRemote},
		{tf.ActivityGamma, "", ""},
	}
	if len(rows) != len(tests) {
		t.Fatalf("%d distinct (cwd, root, remote) rows, want %d: %v", len(rows), len(tests), rows)
	}
	for i, tt := range tests {
		got := [3]string{rows[i][0].(string), rows[i][1].(string), rows[i][2].(string)}
		if got != [3]string{tt.cwd, tt.root, tt.remote} {
			t.Errorf("row %d = %v, want %v", i, got, tt)
		}
	}
}

// TestDayBoundary checks the one event pair that falls on different local
// days (and ISO weeks) in UTC and in the fixture's time zone.
func TestDayBoundary(t *testing.T) {
	st, _ := Activity(t)
	rows := loadRows(t, st)
	var n int
	for _, r := range rows {
		if r.session != "claude:"+tf.ActivitySessionWorktree1 {
			continue
		}
		n++
		if u, l := r.ts.UTC().Format("2006-01-02"), r.ts.In(tf.ActivityLocation).Format("2006-01-02"); u == l {
			t.Errorf("event %s: same day in UTC and local (%s), want a crossing", r.ts, u)
		}
		if w := isoWeek(r.ts); w != "2025-W50" {
			t.Errorf("event %s: local ISO week %s, want 2025-W50", r.ts, w)
		}
	}
	if n == 0 {
		t.Fatal("no events for worktree session 1")
	}
}

func TestSmall(t *testing.T) {
	st, _ := Small(t)
	_, rows, err := st.Query(ctx, `SELECT count(*) FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0][0].(int64); got != 3 {
		t.Errorf("Small sessions = %d, want 3", got)
	}
}

// TestDemo: the demo archive opens read-only and is populated; a prebuilt
// one named by DemoEnv and one built by the test hold the same sessions.
func TestDemo(t *testing.T) {
	st, path := Demo(t)
	_, rows, err := st.Query(ctx, `SELECT count(*) FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	built := rows[0][0].(int64)
	if built < 100 {
		t.Errorf("Demo sessions = %d, want hundreds", built)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path %s: %v", path, err)
	}
	t.Setenv(DemoEnv, path)
	st2, path2 := Demo(t)
	if path2 != path {
		t.Errorf("with %s set, path = %s, want %s", DemoEnv, path2, path)
	}
	_, rows, err = st2.Query(ctx, `SELECT count(*) FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[0][0].(int64); got != built {
		t.Errorf("prebuilt archive has %d sessions, built one %d", got, built)
	}
}

// TestDemoMissing: a DemoEnv that names no file is an error with a hint,
// not a silent rebuild.
func TestDemoMissing(t *testing.T) {
	t.Setenv(DemoEnv, filepath.Join(t.TempDir(), "missing.db"))
	ft := &fakeT{TB: t}
	func() {
		defer func() { _ = recover() }()
		Demo(ft)
	}()
	if !strings.Contains(ft.msg, "mise run demo:db") {
		t.Errorf("fatal message %q lacks the hint", ft.msg)
	}
}

// fakeT records the Fatalf message and panics instead of exiting.
type fakeT struct {
	testing.TB
	msg string
}

func (f *fakeT) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	panic(f.msg)
}

func (f *fakeT) Helper() {}

// TestActivityMonthsAndDirs checks the month split and the directory
// dimension documented on testfixture.Activity.
func TestActivityMonthsAndDirs(t *testing.T) {
	st, _ := Activity(t)
	_, rows, err := st.Query(ctx, `SELECT e.ts, e.actor, e.session_id, s.cwd FROM events e JOIN sessions s ON s.id = e.session_id WHERE s.parent_id IS NULL`)
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ project, month string }
	months := map[key]*tf.ActivityCount{}
	dirs := map[string]*tf.ActivityCount{}
	dirSessions := map[string]map[string]bool{}
	for _, r := range rows {
		ts, err := time.Parse(time.RFC3339Nano, r[0].(string))
		if err != nil {
			t.Fatal(err)
		}
		cwd := r[3].(string)
		project := cwd
		if cwd == tf.ActivityAlphaWorktree {
			project = tf.ActivityAlpha
		}
		k := key{project, ts.In(tf.ActivityLocation).Format("2006-01")}
		if months[k] == nil {
			months[k] = &tf.ActivityCount{}
		}
		if dirs[cwd] == nil {
			dirs[cwd] = &tf.ActivityCount{}
			dirSessions[cwd] = map[string]bool{}
		}
		dirSessions[cwd][r[2].(string)] = true
		for _, c := range []*tf.ActivityCount{months[k], dirs[cwd]} {
			switch r[1].(string) {
			case "human":
				c.Human++
			case "agent":
				c.Agent++
			case "tool":
				c.Tool++
			}
		}
	}
	monthTests := []struct {
		project, month string
		want           tf.ActivityCount
	}{
		{tf.ActivityAlpha, "2025-12", tf.ActivityCount{Human: 10, Agent: 16, Tool: 6}},
		{tf.ActivityAlpha, "2026-01", tf.ActivityCount{Human: 4, Agent: 6, Tool: 2}},
		{tf.ActivityBeta, "2025-12", tf.ActivityCount{Human: 4, Agent: 7, Tool: 3}},
		{tf.ActivityBeta, "2026-01", tf.ActivityCount{Human: 1, Agent: 1, Tool: 0}},
		{tf.ActivityGamma, "2025-12", tf.ActivityCount{Human: 2, Agent: 3, Tool: 1}},
		{tf.ActivityGamma, "2026-01", tf.ActivityCount{Human: 1, Agent: 2, Tool: 1}},
	}
	for _, tt := range monthTests {
		got := months[key{tt.project, tt.month}]
		if got == nil || *got != tt.want {
			t.Errorf("%s %s: got %+v, want %+v", tt.project, tt.month, got, tt.want)
		}
	}
	if len(months) != len(monthTests) {
		t.Errorf("%d project/month cells, want %d", len(months), len(monthTests))
	}
	dirTests := []struct {
		cwd      string
		sessions int
		want     tf.ActivityCount
	}{
		{tf.ActivityAlpha, 6, tf.ActivityCount{Human: 11, Agent: 17, Tool: 6}},
		{tf.ActivityAlphaWorktree, 3, tf.ActivityCount{Human: 3, Agent: 5, Tool: 2}},
		{tf.ActivityBeta, 4, tf.ActivityCount{Human: 5, Agent: 8, Tool: 3}},
		{tf.ActivityGamma, 3, tf.ActivityCount{Human: 3, Agent: 5, Tool: 2}},
	}
	for _, tt := range dirTests {
		got := dirs[tt.cwd]
		if got == nil || *got != tt.want || len(dirSessions[tt.cwd]) != tt.sessions {
			t.Errorf("%s: got sessions=%d %+v, want sessions=%d %+v", tt.cwd, len(dirSessions[tt.cwd]), got, tt.sessions, tt.want)
		}
	}
	if len(dirs) != len(dirTests) {
		t.Errorf("%d directories, want %d", len(dirs), len(dirTests))
	}
}
