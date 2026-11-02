package demo_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/holiday"
	"github.com/seletz/total-recall/internal/screenshot"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

var ctx = context.Background()

// TestMain builds the archive once for the package (unless
// TOTAL_RECALL_DEMO_DB names a prebuilt one): a build takes a second, but
// far longer under the race detector.
func TestMain(m *testing.M) {
	os.Exit(fixturedb.DemoMain(m))
}

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// dump returns every event of the archive in ingest order.
func dump(t *testing.T, path string) string {
	t.Helper()
	db := rawDB(t, path)
	rows, err := db.Query("SELECT id, session_id, ts, actor, kind, text FROM events ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var b strings.Builder
	for rows.Next() {
		var id, sid, ts, actor, kind string
		var text sql.NullString
		if err := rows.Scan(&id, &sid, &ts, &actor, &kind, &text); err != nil {
			t.Fatal(err)
		}
		b.WriteString(strings.Join([]string{id, sid, ts, actor, kind, text.String}, "\t") + "\n")
	}
	return b.String()
}

// TestBuild: a build replaces whatever is at the path, reports its counts
// and yields the same events as the shared archive of the package, so a
// prebuilt archive and one built by a test are interchangeable.
func TestBuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.db")
	if err := os.WriteFile(path, []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := demo.Build(ctx, path)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if n := count(t, rawDB(t, path), "SELECT count(*) FROM sessions"); n != sum.Sessions || n == 0 {
		t.Errorf("sessions = %d, summary %+v", n, sum)
	}
	if n := count(t, rawDB(t, path), "SELECT count(*) FROM events"); n != sum.Events || n == 0 {
		t.Errorf("events = %d, summary %+v", n, sum)
	}
	_, shared := fixturedb.Demo(t)
	if dump(t, path) != dump(t, shared) {
		t.Error("two builds differ")
	}
}

// TestSynthetic: nothing in the archive points at the machine that built
// it; the transcripts claim to live in the fictional home directory.
func TestSynthetic(t *testing.T) {
	_, path := fixturedb.Demo(t)
	db := rawDB(t, path)
	needles := []string{"/Users", "seletz", "/tmp", "/var/", os.TempDir(), filepath.Dir(path)}
	if hit, err := screenshot.Scan(ctx, db, needles); err != nil {
		t.Fatal(err)
	} else if hit != "" {
		t.Errorf("archive holds a real path in %s", hit)
	}
	if n := count(t, db, "SELECT count(*) FROM sources WHERE path NOT LIKE ?", demo.Projects+"/%"); n != 0 {
		t.Errorf("%d sources outside %s", n, demo.Projects)
	}
	if n := count(t, db, "SELECT count(*) FROM events WHERE source_path NOT LIKE ?", demo.Projects+"/%"); n != 0 {
		t.Errorf("%d events with a source outside %s", n, demo.Projects)
	}
	if n := count(t, db, "SELECT count(*) FROM sessions WHERE display_name IS NULL"); n != 0 {
		t.Errorf("%d sessions without a title", n)
	}
	if n := count(t, db, "SELECT count(*) FROM sessions WHERE cwd NOT LIKE ?", demo.Home+"/%"); n != 0 {
		t.Errorf("%d sessions outside %s", n, demo.Home)
	}
	if n := count(t, db, "SELECT count(*) FROM events WHERE kind = 'tool_use'"); n == 0 {
		t.Error("no tool_use events; the session view shows tools")
	}
	if n := count(t, db, "SELECT count(*) FROM events WHERE ts >= ?", demo.Now.UTC().Format(time.RFC3339)); n != 0 {
		t.Errorf("%d events after the demo clock %s", n, demo.Now)
	}
}

// TestSearch: the query of the search screenshot finds hits in several
// sessions, the first one titled and in the demo repository.
func TestSearch(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	hits, err := st.Search(ctx, demo.Query, store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 4 {
		t.Fatalf("%q finds %d hits, want at least 4", demo.Query, len(hits))
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

func workdays(t *testing.T, st *store.Store, since, until time.Time) map[string]store.Workday {
	t.Helper()
	days, err := st.Workday(ctx, store.WorkdayFilter{
		Since: since, Until: until, Location: demo.Location, DayStart: 4 * time.Hour,
		HumanOnly: true, Holidays: holiday.New("Baden-Württemberg"),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]store.Workday{}
	for _, d := range days {
		out[d.Label] = d
	}
	return out
}

// TestActivityShape: the invented activity spans the whole range and has
// the features the activity view shows: weekends, days past midnight,
// a worked public holiday, idle holidays and a vacation.
func TestActivityShape(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	days := workdays(t, st, demo.First, demo.Last.AddDate(0, 0, 1))
	var active, weekend, past int
	first, last := "", ""
	for label, d := range days {
		if !d.Active() {
			continue
		}
		active++
		if first == "" || label < first {
			first = label
		}
		if last == "" || label > last {
			last = label
		}
		if store.IsWeekend(d.Day) {
			weekend++
		}
		if strings.Contains(d.LastClock(demo.Location), "(+1)") {
			past++
		}
	}
	if active < 100 {
		t.Errorf("%d active days, want at least 100", active)
	}
	if first > "2026-05-08" || last < "2026-10-26" {
		t.Errorf("activity from %s to %s, want May to late October", first, last)
	}
	if weekend < 8 {
		t.Errorf("%d active weekend days, want at least 8", weekend)
	}
	if past < 5 {
		t.Errorf("%d days past midnight, want at least 5", past)
	}
	for _, label := range []string{"2026-05-14", "2026-05-25"} {
		if d := days[label]; d.Active() || !d.IsHoliday() {
			t.Errorf("%s: active %v, holiday %q; want an idle holiday", label, d.Active(), d.Holiday)
		}
	}
	for d := time.Date(2026, 8, 10, 0, 0, 0, 0, demo.Location); d.Day() <= 21; d = d.AddDate(0, 0, 1) {
		if days[d.Format("2006-01-02")].Active() {
			t.Errorf("%s: active during the vacation", d.Format("2006-01-02"))
		}
	}
	for d := time.Date(2026, 5, 4, 0, 0, 0, 0, demo.Location); d.Day() <= 8; d = d.AddDate(0, 0, 1) {
		if !days[d.Format("2006-01-02")].Active() {
			t.Errorf("%s: no activity in the first week", d.Format("2006-01-02"))
		}
	}
}

// TestShowcaseWeek: the week of the activity screenshots is hand-laid:
// an ordinary day, a day past midnight, a short day, a worked holiday
// (Fronleichnam), a short Friday, a Saturday and an idle Sunday.
func TestShowcaseWeek(t *testing.T) {
	st, _ := fixturedb.Demo(t)
	since := time.Date(2026, 6, 1, 4, 0, 0, 0, demo.Location)
	days := workdays(t, st, since, since.AddDate(0, 0, 7))
	tests := []struct {
		day, first, last, holiday string
	}{
		{"2026-06-01", "08:52", "17:35", ""},
		{"2026-06-02", "09:10", "00:45 (+1)", ""},
		{"2026-06-03", "08:30", "16:05", ""},
		{"2026-06-04", "16:30", "17:40", "Fronleichnam"},
		{"2026-06-05", "09:30", "13:15", ""},
		{"2026-06-06", "10:15", "12:40", ""},
		{"2026-06-07", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.day, func(t *testing.T) {
			d, ok := days[tt.day]
			if !ok {
				t.Fatalf("day %s missing", tt.day)
			}
			if got := d.FirstClock(demo.Location); got != tt.first {
				t.Errorf("first = %q, want %q", got, tt.first)
			}
			if got := d.LastClock(demo.Location); got != tt.last {
				t.Errorf("last = %q, want %q", got, tt.last)
			}
			if d.Holiday != tt.holiday {
				t.Errorf("holiday = %q, want %q", d.Holiday, tt.holiday)
			}
		})
	}
}

// TestProjectsAndAgents: the archive has several projects, main sessions
// and named subagents, so the stats view has something to group by.
func TestProjectsAndAgents(t *testing.T) {
	st, path := fixturedb.Demo(t)
	series, err := st.ActivitySeries(ctx, store.ActivityFilter{
		Location: demo.Location, Period: store.PeriodMonth, Dimension: store.DimProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Keys) < 5 {
		t.Errorf("%d projects, want at least 5: %v", len(series.Keys), series.Keys)
	}
	if len(series.Buckets) < 6 {
		t.Errorf("%d months, want at least 6", len(series.Buckets))
	}
	agents, err := st.ActivitySeries(ctx, store.ActivityFilter{
		Location: demo.Location, Period: store.PeriodMonth, Dimension: store.DimAgent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(agents.Keys, " "); !strings.Contains(got, "subagent") || !strings.Contains(got, "main") {
		t.Errorf("agent keys = %v, want main and subagent", agents.Keys)
	}
	db := rawDB(t, path)
	if n := count(t, db, "SELECT count(*) FROM sessions WHERE parent_id IS NOT NULL"); n < 10 {
		t.Errorf("%d subagent sessions, want at least 10", n)
	}
	if n := count(t, db, "SELECT count(*) FROM sessions WHERE parent_id IS NOT NULL AND (agent_name IS NULL OR agent_name = '')"); n != 0 {
		t.Errorf("%d subagents without a name", n)
	}
	if n := count(t, db, "SELECT count(DISTINCT agent_name) FROM sessions WHERE parent_id IS NOT NULL"); n < 3 {
		t.Errorf("%d agent names, want at least 3", n)
	}
	if n := count(t, db, "SELECT count(*) FROM sessions WHERE git_root IS NULL AND parent_id IS NULL"); n == 0 {
		t.Error("no session outside a repository; the project names show a path then")
	}
	if n := count(t, db, "SELECT count(*) FROM sessions WHERE cwd LIKE '%__worktrees%'"); n == 0 {
		t.Error("no worktree session")
	}
}

// TestWrite: the transcripts form a Claude Code projects tree with
// subagents below their parent session.
func TestWrite(t *testing.T) {
	projects, err := demo.Write(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var files, subagents int
	err = filepath.WalkDir(projects, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return err
		}
		files++
		if strings.Contains(p, "/subagents/agent-") {
			subagents++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 200 || subagents < 10 {
		t.Errorf("%d transcripts, %d subagents; want at least 200 and 10", files, subagents)
	}
}
