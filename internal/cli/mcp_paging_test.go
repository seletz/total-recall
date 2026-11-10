package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/mcp"
)

// pagedDemo serves the MCP tools over the demo archive with the bound max.
type pagedDemo struct {
	t   *testing.T
	s   *mcpSession
	e   *testEnv
	srv *mcp.Server
}

func newPagedDemo(t *testing.T, max int) *pagedDemo {
	s, e := demoMCP(t)
	e.now = demo.Now // the CLI and the server see the same clock
	run := func(ctx context.Context, argv []string) (string, bool) {
		var out bytes.Buffer
		app := &App{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &bytes.Buffer{}, Args: append(append([]string{}, s.args...), argv...), Home: e.dir, Now: func() time.Time { return e.now },
			Getenv: func(k string) string {
				if k == "TOTAL_RECALL_CONFIG" {
					return e.configPath
				}
				return ""
			}}
		code := Main(app)
		return out.String(), code != 0
	}
	return &pagedDemo{t: t, s: s, e: e, srv: &mcp.Server{Name: "t", Tools: mcpTools(), MaxBytes: max, Run: run}}
}

// call returns the first content item and the hint (nil when absent).
func (p *pagedDemo) call(tool string, args map[string]any) (first string, hint *mcp.Hint, isErr bool) {
	p.t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	var m struct {
		Result struct {
			Content []struct{ Text string }
			IsError bool
		}
	}
	if err := json.Unmarshal(p.srv.Handle(context.Background(), b), &m); err != nil || len(m.Result.Content) == 0 {
		p.t.Fatalf("%s: %v", tool, err)
	}
	if len(m.Result.Content) == 2 {
		hint = &mcp.Hint{}
		if err := json.Unmarshal([]byte(m.Result.Content[1].Text), hint); err != nil {
			p.t.Fatalf("%s: hint: %v", tool, err)
		}
	}
	return m.Result.Content[0].Text, hint, m.Result.IsError
}

// crawl fetches all pages of the call.
func (p *pagedDemo) crawl(tool string, args map[string]any) (pages []string, hints []*mcp.Hint) {
	p.t.Helper()
	a := map[string]any{}
	for k, v := range args {
		a[k] = v
	}
	for i := 0; i < 2000; i++ {
		first, h, isErr := p.call(tool, a)
		if isErr {
			p.t.Fatalf("%s page %d: %s", tool, i+1, first)
		}
		pages, hints = append(pages, first), append(hints, h)
		if h == nil || h.Next == nil {
			return
		}
		a["cursor"] = h.Next.Cursor
	}
	p.t.Fatal("pages do not end")
	return
}

// cli returns the compact JSON the CLI prints for the command line.
func (p *pagedDemo) cli(args ...string) string {
	p.t.Helper()
	r := p.e.run(p.t, "", append(append(append([]string{}, p.s.args...), args...), "--json")...)
	if r.code != 0 {
		p.t.Fatalf("cli %v: exit %d: %s", args, r.code, r.stderr)
	}
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(r.stdout)); err != nil {
		p.t.Fatal(err)
	}
	return b.String()
}

// reassemble puts the pages together: the first page with the entries of
// the paged list of all pages.
func reassemble(t *testing.T, pages []string, unit string) string {
	t.Helper()
	list := func(p string) []json.RawMessage {
		var l []json.RawMessage
		if unit == "" {
			if err := json.Unmarshal([]byte(p), &l); err != nil {
				t.Fatal(err)
			}
			return l
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(p), &m); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(m[unit], &l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	var all []json.RawMessage
	for _, p := range pages {
		all = append(all, list(p)...)
	}
	var joined bytes.Buffer
	joined.WriteByte('[')
	for i, u := range all {
		if i > 0 {
			joined.WriteByte(',')
		}
		joined.Write(u)
	}
	joined.WriteByte(']')
	if unit == "" {
		return joined.String()
	}
	var first map[string]json.RawMessage
	_ = json.Unmarshal([]byte(pages[0]), &first)
	var out bytes.Buffer
	out.WriteByte('{')
	for i, k := range jsonKeys(pages[0]) {
		if i > 0 {
			out.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		out.Write(kb)
		out.WriteByte(':')
		if k == unit {
			out.Write(joined.Bytes())
		} else {
			out.Write(first[k])
		}
	}
	out.WriteByte('}')
	return out.String()
}

func jsonKeys(doc string) []string {
	dec := json.NewDecoder(strings.NewReader(doc))
	_, _ = dec.Token()
	var keys []string
	for dec.More() {
		t, _ := dec.Token()
		keys = append(keys, t.(string))
		var skip json.RawMessage
		_ = dec.Decode(&skip)
	}
	return keys
}

// TestMCPPagesAddUp runs the real tools over the demo archive with a bound
// that forces pages: no page is larger than the bound or cut, and all pages
// together are byte for byte the compact JSON of the CLI.
func TestMCPPagesAddUp(t *testing.T) {
	const bound = 12 << 10
	list, _, _ := newPagedDemo(t, 16<<20).call("sessions", map[string]any{"limit": 500})
	var rows []struct {
		ID     string `json:"id"`
		Events int
	}
	if err := json.Unmarshal([]byte(list), &rows); err != nil || len(rows) == 0 {
		t.Fatalf("sessions: %v", err)
	}
	big := rows[0] // the session with the most events
	for _, r := range rows {
		if r.Events > big.Events {
			big = r
		}
	}
	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
		cli  []string
		unit string
		max  int // bound; 0 = bound
	}{
		{"stats worktime with sessions and summary", "stats",
			map[string]any{"worktime": true, "sessions": true, "summary": true, "since": "2026-05-01", "until": "2026-10-31"},
			[]string{"stats", "--worktime", "--sessions", "--summary", "--since=2026-05-01", "--until=2026-10-31"}, "days", 0},
		{"stats worktime for a month", "stats",
			map[string]any{"worktime": true, "sessions": true, "summary": true, "month": "2026-09"},
			[]string{"stats", "--worktime", "--sessions", "--summary", "--month=2026-09"}, "days", 0},
		{"stats by project", "stats",
			map[string]any{"period": "day", "by": "project", "since": "2026-05-01", "until": "2026-10-31"},
			[]string{"stats", "--period=day", "--by=project", "--since=2026-05-01", "--until=2026-10-31"}, "buckets", 0},
		{"stats workday", "stats",
			map[string]any{"workday": true, "since": "2026-01-01", "until": "2026-12-31"},
			[]string{"stats", "--workday", "--since=2026-01-01", "--until=2026-12-31"}, "days", 0},
		{"stats workday with blocks", "stats",
			map[string]any{"workday": true, "blocks": true, "since": "2026-01-01", "until": "2026-12-31"},
			[]string{"stats", "--workday", "--blocks", "--since=2026-01-01", "--until=2026-12-31"}, "days", 0},
		{"stats per day", "stats",
			map[string]any{"since": "2026-01-01", "until": "2026-12-31"},
			[]string{"stats", "--since=2026-01-01", "--until=2026-12-31"}, "by_day", 14 << 10},
		{"search events", "search", map[string]any{"query": "the", "limit": 200}, []string{"search", "the", "--limit=200"}, "", 0},
		{"search all types", "search", map[string]any{"query": "the", "type": "event,session,memory", "limit": 200}, []string{"search", "the", "--type=event,session,memory", "--limit=200"}, "", 0},
		{"sessions", "sessions", map[string]any{"limit": 500}, []string{"sessions", "--limit=500"}, "", 0},
		{"show", "show", map[string]any{"session": big.ID, "all": true, "limit": 1000}, []string{"show", big.ID, "--all", "--limit=1000"}, "events", 4 << 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bound := cmp.Or(tc.max, bound)
			p := newPagedDemo(t, bound)
			want := p.cli(tc.cli...)
			if len(want) <= bound {
				t.Skipf("only %d bytes in the demo archive", len(want))
			}
			pages, hints := p.crawl(tc.tool, tc.args)
			if len(pages) < 2 {
				t.Fatalf("%d pages for %d bytes", len(pages), len(want))
			}
			for i, pg := range pages {
				if len(pg) > bound || !json.Valid([]byte(pg)) {
					t.Errorf("page %d: %d bytes (bound %d), valid %v", i+1, len(pg), bound, json.Valid([]byte(pg)))
				}
				if h := hints[i]; h == nil || h.Truncated || h.Page == nil || h.Page.Index != i+1 || h.Page.Of != len(pages) || h.Page.Unit != tc.unit {
					t.Fatalf("page %d: hint = %+v", i+1, h)
				}
			}
			if got := reassemble(t, pages, tc.unit); got != want {
				i := 0
				for i < len(got) && i < len(want) && got[i] == want[i] {
					i++
				}
				t.Errorf("%d pages do not add up to the CLI JSON (%d vs %d bytes), first difference at %d:\n got: %.120s\nwant: %.120s", len(pages), len(got), len(want), i, got[max(0, i-40):], want[max(0, i-40):])
			}
		})
	}
}

type worktimePage struct {
	Days []struct {
		Day        string
		Raw        int `json:"raw_seconds"`
		Rounded    int `json:"rounded_seconds"`
		Categories []struct {
			Category string
			Raw      int `json:"raw_seconds"`
			Rounded  int `json:"rounded_seconds"`
			Projects []struct {
				Project string
				Raw     int `json:"raw_seconds"`
			}
			Sessions []struct {
				Raw int `json:"raw_seconds"`
			}
		}
	}
	Categories []json.RawMessage
	Total      json.RawMessage
}

// TestMCPWorktimeSumsOnEveryPage is the invariant behind #230: on every
// page a day is the sum of its categories, a category the sum of its
// projects, the totals are the same on every page and equal the sum of the
// days of all pages, and no category, project or session is missing.
func TestMCPWorktimeSumsOnEveryPage(t *testing.T) {
	// A bound of about four days: the list of days itself spans many pages.
	full := newPagedDemo(t, 16<<20)
	args := map[string]any{"worktime": true, "sessions": true, "summary": true, "since": "2026-05-01", "until": "2026-10-31"}
	cliArgs := []string{"stats", "--worktime", "--sessions", "--summary", "--since=2026-05-01", "--until=2026-10-31"}
	want := full.cli(cliArgs...)
	var whole worktimePage
	if err := json.Unmarshal([]byte(want), &whole); err != nil {
		t.Fatal(err)
	}
	var wholeTotal struct {
		Raw     int `json:"raw_seconds"`
		Rounded int `json:"rounded_seconds"`
	}
	_ = json.Unmarshal(whole.Total, &wholeTotal)
	var m map[string]json.RawMessage
	_ = json.Unmarshal([]byte(want), &m)
	var days []json.RawMessage
	_ = json.Unmarshal(m["days"], &days)
	rest, biggest := len(want), 0
	for _, d := range days {
		rest -= len(d) + 1
		biggest = max(biggest, len(d))
	}
	bound := rest + 4*biggest

	p := newPagedDemo(t, bound)
	pages, hints := p.crawl("stats", args)
	if len(pages) < len(days)/10 {
		t.Fatalf("%d pages for %d days", len(pages), len(days))
	}
	var rawSum, roundedSum, nDays, nCats, nProjects, nSessions int
	for i, pg := range pages {
		if len(pg) > bound || hints[i] == nil || hints[i].Truncated {
			t.Fatalf("page %d: %d bytes (bound %d), hint %+v", i+1, len(pg), bound, hints[i])
		}
		var w worktimePage
		if err := json.Unmarshal([]byte(pg), &w); err != nil {
			t.Fatal(err)
		}
		if string(w.Total) != string(whole.Total) || len(w.Categories) != len(whole.Categories) {
			t.Errorf("page %d: totals differ from the whole", i+1)
		}
		for _, d := range w.Days {
			raw, rounded := 0, 0
			for _, c := range d.Categories {
				pr := 0
				for _, pj := range c.Projects {
					pr += pj.Raw
				}
				if pr != c.Raw {
					t.Errorf("%s %s: category %d s, projects %d s (%d projects)", d.Day, c.Category, c.Raw, pr, len(c.Projects))
				}
				raw, rounded = raw+c.Raw, rounded+c.Rounded
				nCats++
				nProjects += len(c.Projects)
				nSessions += len(c.Sessions)
			}
			if raw != d.Raw || rounded != d.Rounded {
				t.Errorf("%s: day %d/%d s, categories %d/%d s", d.Day, d.Raw, d.Rounded, raw, rounded)
			}
			rawSum, roundedSum = rawSum+d.Raw, roundedSum+d.Rounded
			nDays++
		}
	}
	if nDays != len(whole.Days) || rawSum != wholeTotal.Raw || roundedSum != wholeTotal.Rounded {
		t.Errorf("%d of %d days, raw %d (total %d), rounded %d (total %d)", nDays, len(whole.Days), rawSum, wholeTotal.Raw, roundedSum, wholeTotal.Rounded)
	}
	var wc, wp, ws int
	for _, d := range whole.Days {
		for _, c := range d.Categories {
			wc++
			wp += len(c.Projects)
			ws += len(c.Sessions)
		}
	}
	if nCats != wc || nProjects != wp || nSessions != ws {
		t.Errorf("categories %d/%d, projects %d/%d, sessions %d/%d", nCats, wc, nProjects, wp, nSessions, ws)
	}
	if got := reassemble(t, pages, "days"); got != want {
		t.Errorf("pages do not add up to the CLI JSON")
	}
}

func TestMCPCursorErrors(t *testing.T) {
	p := newPagedDemo(t, 6<<10)
	args := map[string]any{"limit": 500}
	_, h, _ := p.call("sessions", args)
	if h == nil || h.Next == nil {
		t.Fatalf("no second page: %+v", h)
	}
	for _, tc := range []struct {
		name, tool string
		args       map[string]any
		want       string
	}{
		{"malformed", "sessions", map[string]any{"limit": 500, "cursor": "x"}, "invalid cursor"},
		{"mid-page", "sessions", map[string]any{"limit": 500, "cursor": "1-" + strings.SplitN(h.Next.Cursor, "-", 2)[1]}, "invalid cursor"},
		{"other arguments", "sessions", map[string]any{"limit": 499, "cursor": h.Next.Cursor}, "cursor belongs to a different query"},
		{"other tool", "search", map[string]any{"query": "the", "cursor": h.Next.Cursor}, "cursor belongs to a different query"},
	} {
		first, _, isErr := p.call(tc.tool, tc.args)
		if !isErr || !strings.Contains(first, `"kind":"usage"`) || !strings.Contains(first, tc.want) {
			t.Errorf("%s: %v %s", tc.name, isErr, first)
		}
	}
	// The same call with the cursor works.
	if first, _, isErr := p.call("sessions", map[string]any{"limit": 500, "cursor": h.Next.Cursor}); isErr {
		t.Errorf("good cursor: %s", first)
	}
	// status has no pages: the argument is unknown there.
	if first, _, isErr := p.call("status", map[string]any{"cursor": "0-x"}); !isErr || !strings.Contains(first, `"kind":"usage"`) {
		t.Errorf("status with cursor: %v %s", isErr, first)
	}
}

// timelineBound returns a bound that splits the CLI JSON of the command
// into about pages pages. A fixed small bound would make the number of
// pages, and with it the runs of the CLI (every page runs the command
// again), grow with the size of the demo archive.
func timelineBound(p *pagedDemo, pages int, cli ...string) int {
	return len(p.cli(cli...))/pages + 1
}

// TestMCPTimelinePagesAddUp: all pages of timeline together are byte for
// byte the CLI JSON, for the human events and, with every actor and kind,
// for the largest list the demo archive gives in a window.
func TestMCPTimelinePagesAddUp(t *testing.T) {
	for _, tc := range []struct {
		name string
		args map[string]any
		cli  []string
	}{
		{"human", map[string]any{"since": "2026-10-19", "until": "2026-10-23"}, []string{"timeline", "--since=2026-10-19", "--until=2026-10-23", "--fields=" + mcpTimelineFields}},
		{"all actors and kinds", map[string]any{"since": "2026-10-20", "until": "2026-10-20", "actor": "all", "all": true},
			[]string{"timeline", "--since=2026-10-20", "--until=2026-10-20", "--actor=all", "--all", "--fields=" + mcpTimelineFields}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bound := timelineBound(newPagedDemo(t, 16<<20), 4, tc.cli...)
			p := newPagedDemo(t, bound)
			want := p.cli(tc.cli...)
			pages, hints := p.crawl("timeline", tc.args)
			if len(pages) < 3 || len(pages) > 6 {
				t.Fatalf("%d pages for %d bytes, bound %d", len(pages), len(want), bound)
			}
			for i, pg := range pages {
				if len(pg) > bound || hints[i] == nil || hints[i].Truncated || hints[i].Page == nil || hints[i].Page.Index != i+1 {
					t.Fatalf("page %d: %d bytes (bound %d), hint %+v", i+1, len(pg), bound, hints[i])
				}
			}
			if got := reassemble(t, pages, ""); got != want {
				t.Errorf("%d pages do not add up to the CLI JSON (%d vs %d bytes)", len(pages), len(got), len(want))
			}
		})
	}
}

// TestMCPTimelineMatchesStatsOnEveryPage: with a bound that forces pages,
// the events of all pages of timeline are the human events stats counts
// for the window, in total and per project; no event is missing or doubled.
func TestMCPTimelineMatchesStatsOnEveryPage(t *testing.T) {
	const since, until = "2026-10-12", "2026-10-23"
	cli := []string{"timeline", "--since=" + since, "--until=" + until, "--fields=" + mcpTimelineFields}
	bound := timelineBound(newPagedDemo(t, 16<<20), 4, cli...)
	p := newPagedDemo(t, bound)
	pages, hints := p.crawl("timeline", map[string]any{"since": since, "until": until})
	if len(pages) < 3 || len(pages) > 6 {
		t.Fatalf("%d pages, bound %d", len(pages), bound)
	}
	seen := map[string]bool{}
	perProject := map[string]int{}
	total := 0
	for i, pg := range pages {
		if len(pg) > bound || hints[i] == nil || hints[i].Truncated {
			t.Fatalf("page %d: %d bytes, hint %+v", i+1, len(pg), hints[i])
		}
		var evs []TimelineEventJSON
		if err := json.Unmarshal([]byte(pg), &evs); err != nil {
			t.Fatal(err)
		}
		for _, ev := range evs {
			if seen[ev.EventID] {
				t.Errorf("event %s on two pages", ev.EventID)
			}
			seen[ev.EventID] = true
			perProject[string(ev.Project)]++
			total++
		}
	}
	var st ActivitySeriesJSON
	if err := json.Unmarshal([]byte(p.cli("stats", "--period=day", "--by=project", "--since="+since, "--until="+until)), &st); err != nil {
		t.Fatal(err)
	}
	if total != st.Total.Human || total == 0 {
		t.Errorf("pages hold %d events, stats counts %d human events", total, st.Total.Human)
	}
	for _, c := range st.ByKey {
		if perProject[c.Key] != c.Human {
			t.Errorf("project %s: pages %d, stats %d", c.Key, perProject[c.Key], c.Human)
		}
	}
}

// TestMCPUncategorizedOnEveryPage: the projects without a category are a
// small list beside the paged days, so it is whole and identical on every
// page; it is a part of the total, and each of its projects has exactly the
// seconds its cells have over the days of all pages.
func TestMCPUncategorizedOnEveryPage(t *testing.T) {
	type page struct {
		Days []struct {
			Categories []struct {
				Projects []struct {
					Key string
					Raw int `json:"raw_seconds"`
				}
			}
		}
		Uncategorized json.RawMessage
		Total         struct {
			Raw int `json:"raw_seconds"`
		}
	}
	args := map[string]any{"worktime": true, "since": "2026-05-01", "until": "2026-10-31"}
	want := newPagedDemo(t, 16<<20).cli("stats", "--worktime", "--since=2026-05-01", "--until=2026-10-31")
	var whole struct {
		Uncategorized []struct {
			Key string
			Raw int `json:"raw_seconds"`
		}
		Total struct {
			Raw int `json:"raw_seconds"`
		}
		Days []json.RawMessage
	}
	if err := json.Unmarshal([]byte(want), &whole); err != nil || len(whole.Uncategorized) == 0 {
		t.Fatalf("whole: %v, %d uncategorized", err, len(whole.Uncategorized))
	}
	bound := len(want) / 6
	pages, hints := newPagedDemo(t, bound).crawl("stats", args)
	if len(pages) < 3 {
		t.Fatalf("%d pages", len(pages))
	}
	perProject := map[string]int{}
	var first json.RawMessage
	for i, pg := range pages {
		if len(pg) > bound || hints[i] == nil || hints[i].Truncated {
			t.Fatalf("page %d: %d bytes (bound %d), hint %+v", i+1, len(pg), bound, hints[i])
		}
		var w page
		if err := json.Unmarshal([]byte(pg), &w); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = w.Uncategorized
		} else if string(w.Uncategorized) != string(first) {
			t.Errorf("page %d: uncategorized differs from page 1", i+1)
		}
		if w.Total.Raw != whole.Total.Raw {
			t.Errorf("page %d: total differs", i+1)
		}
		for _, d := range w.Days {
			for _, c := range d.Categories {
				for _, p := range c.Projects {
					perProject[p.Key] += p.Raw
				}
			}
		}
	}
	sum := 0
	for _, u := range whole.Uncategorized {
		if perProject[u.Key] != u.Raw {
			t.Errorf("%s: uncategorized %d s, cells of all pages %d s", u.Key, u.Raw, perProject[u.Key])
		}
		sum += u.Raw
	}
	if sum > whole.Total.Raw {
		t.Errorf("uncategorized %d exceeds the total %d", sum, whole.Total.Raw)
	}
}

// TestMCPWorkdayBlocksSumsOnEveryPage: with a bound that forces many pages
// of days, the blocks of a day add up on every page (blocks plus breaks are
// the span), the totals are the same on every page and equal the sums of
// the days of all pages, and no block is missing.
func TestMCPWorkdayBlocksSumsOnEveryPage(t *testing.T) {
	type page struct {
		Days []struct {
			Day          string
			Span         int `json:"span_seconds"`
			BlockSeconds int `json:"block_seconds"`
			BreakSeconds int `json:"break_seconds"`
			Blocks       []struct {
				Seconds int
			}
		}
		Total json.RawMessage
	}
	args := map[string]any{"workday": true, "blocks": true, "since": "2026-05-01", "until": "2026-10-31"}
	want := newPagedDemo(t, 16<<20).cli("stats", "--workday", "--blocks", "--since=2026-05-01", "--until=2026-10-31")
	var whole page
	if err := json.Unmarshal([]byte(want), &whole); err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal([]byte(want), &m)
	var days []json.RawMessage
	_ = json.Unmarshal(m["days"], &days)
	rest, biggest := len(want), 0
	for _, d := range days {
		rest -= len(d) + 1
		biggest = max(biggest, len(d))
	}
	bound := rest + 5*biggest
	pages, hints := newPagedDemo(t, bound).crawl("stats", args)
	if len(pages) < len(days)/10 {
		t.Fatalf("%d pages for %d days", len(pages), len(days))
	}
	var nDays, nBlocks, spanSum int
	for i, pg := range pages {
		if len(pg) > bound || hints[i] == nil || hints[i].Truncated {
			t.Fatalf("page %d: %d bytes (bound %d), hint %+v", i+1, len(pg), bound, hints[i])
		}
		var w page
		if err := json.Unmarshal([]byte(pg), &w); err != nil {
			t.Fatal(err)
		}
		if string(w.Total) != string(whole.Total) {
			t.Errorf("page %d: totals differ from the whole", i+1)
		}
		for _, d := range w.Days {
			sum := 0
			for _, b := range d.Blocks {
				sum += b.Seconds
			}
			if sum != d.BlockSeconds || sum+d.BreakSeconds != d.Span {
				t.Errorf("%s: blocks %d, block_seconds %d, break_seconds %d, span %d", d.Day, sum, d.BlockSeconds, d.BreakSeconds, d.Span)
			}
			nDays++
			nBlocks += len(d.Blocks)
			spanSum += d.Span
		}
	}
	wholeBlocks := 0
	for _, d := range whole.Days {
		wholeBlocks += len(d.Blocks)
	}
	var tot struct {
		Span  int `json:"span_seconds"`
		Block int `json:"block_seconds"`
		Break int `json:"break_seconds"`
	}
	_ = json.Unmarshal(whole.Total, &tot)
	if nDays != len(whole.Days) || nBlocks != wholeBlocks || wholeBlocks == 0 || spanSum != tot.Span || tot.Block+tot.Break != tot.Span {
		t.Errorf("%d of %d days, %d of %d blocks, span %d (total %d), blocks %d + breaks %d", nDays, len(whole.Days), nBlocks, wholeBlocks, spanSum, tot.Span, tot.Block, tot.Break)
	}
	if got := reassemble(t, pages, "days"); got != want {
		t.Errorf("pages do not add up to the CLI JSON")
	}
}

// TestMCPDescriptionsSayWherePagingIs: the tool descriptions (tools/list)
// say that page, next and hint are in the second content block.
func TestMCPDescriptionsSayWherePagingIs(t *testing.T) {
	for _, tool := range mcpTools() {
		has := false
		for _, p := range tool.Params {
			has = has || p.Name == "cursor"
		}
		if got := strings.Contains(tool.Description, "SECOND content block"); got != has {
			t.Errorf("%s: cursor=%v, description mentions the second content block=%v", tool.Name, has, got)
		}
	}
}

// TestMCPTimelineFewerPages: the compact fields of the MCP default (#259)
// carry more events per page than the whole object, for the same day with
// every actor and kind and the same bound; and with fields=all (every
// field) the pages are byte for byte the CLI default output again, so
// nothing is lost, only left out on request.
func TestMCPTimelineFewerPages(t *testing.T) {
	day := map[string]any{"since": "2026-10-20", "until": "2026-10-20", "actor": "all", "all": true}
	full := []string{"timeline", "--since=2026-10-20", "--until=2026-10-20", "--actor=all", "--all"}
	bound := timelineBound(newPagedDemo(t, 16<<20), 4, full...)
	p := newPagedDemo(t, bound)

	compact, hints := p.crawl("timeline", day)
	allFields := map[string]any{"fields": "all"}
	for k, v := range day {
		allFields[k] = v
	}
	whole, _ := p.crawl("timeline", allFields)
	t.Logf("bound %d bytes: %d pages with the default fields, %d with all fields", bound, len(compact), len(whole))
	if len(compact) >= len(whole) {
		t.Errorf("default fields need %d pages, all fields %d: not fewer", len(compact), len(whole))
	}
	if got, want := reassemble(t, whole, ""), p.cli(full...); got != want {
		t.Errorf("pages with all fields do not add up to the CLI default output (%d vs %d bytes)", len(got), len(want))
	}
	// The default is the CLI with the compact fields, and every event is on
	// some page: as many events as with all fields.
	if got, want := reassemble(t, compact, ""), p.cli(append(full, "--fields="+mcpTimelineFields)...); got != want {
		t.Errorf("default pages do not add up to the CLI output with the compact fields (%d vs %d bytes)", len(got), len(want))
	}
	// The hint tells how to get fewer pages.
	if h := hints[0]; h == nil || !strings.Contains(h.Hint, "fields") || !strings.Contains(h.Hint, "text_width") {
		t.Errorf("hint = %+v, want it to name fields and text_width", h)
	}
}
