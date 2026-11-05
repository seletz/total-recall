package store

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/model"
)

// fixture: three sessions in two repos and two harnesses.
func seed(t *testing.T, s *Store) {
	t.Helper()
	sessions := []model.Session{
		{ID: "claude:aaa111", Harness: "claude", NativeID: "aaa111", Name: "Fix login", Cwd: "/w/app", GitRoot: "/w/app",
			GitRemoteURL: "https://github.com/acme/app", StartedAt: ts("2026-10-31T22:00:00Z"), EndedAt: ts("2026-10-31T23:30:00Z")},
		{ID: "claude:aab222", Harness: "claude", NativeID: "aab222", Name: "Write docs", Cwd: "/w/docs",
			StartedAt: ts("2026-10-15T08:00:00Z"), EndedAt: ts("2026-10-15T09:00:00Z")},
		{ID: "other:ccc333", Harness: "other", NativeID: "ccc333", Name: "Login audit", Cwd: "/w/app/sub", GitRoot: "/w/app",
			GitRemoteURL: "https://github.com/acme/app", StartedAt: ts("2026-09-01T10:00:00Z"), EndedAt: ts("2026-09-01T10:10:00Z")},
	}
	evs := []model.Event{
		// session aaa111 crosses midnight in Europe/Berlin (UTC+1 on 2026-10-31).
		ev("claude:a1", "claude:aaa111", "2026-10-31T22:00:00Z", model.ActorHuman, model.KindMessage, "user", "please fix the login bug"),
		ev("claude:a2", "claude:aaa111", "2026-10-31T22:10:00Z", model.ActorAgent, model.KindMessage, "assistant", "looking at the login handler"),
		ev("claude:a3", "claude:aaa111", "2026-10-31T22:11:00Z", model.ActorAgent, model.KindToolUse, "assistant", "grep login"),
		ev("claude:a4", "claude:aaa111", "2026-10-31T22:12:00Z", model.ActorTool, model.KindToolResult, "tool", "login.go:12"),
		ev("claude:a5", "claude:aaa111", "2026-10-31T22:20:00Z", model.ActorAgent, model.KindMessage, "assistant", "fixed the session cookie"),
		ev("claude:a6", "claude:aaa111", "2026-10-31T23:10:00Z", model.ActorHuman, model.KindMessage, "user", "now run the tests"),
		ev("claude:a7", "claude:aaa111", "2026-10-31T23:20:00Z", model.ActorAutomation, model.KindMessage, "user", "<task-notification> done"),
		ev("claude:a8", "claude:aaa111", "2026-10-31T23:30:00Z", model.ActorHuman, model.KindMessage, "user", "no answer to this one"),
		ev("claude:b1", "claude:aab222", "2026-10-15T08:00:00Z", model.ActorHuman, model.KindMessage, "user", "write the Grüße docs"),
		ev("claude:b2", "claude:aab222", "2026-10-15T09:00:00Z", model.ActorAgent, model.KindMessage, "assistant", "docs written"),
		ev("other:c1", "other:ccc333", "2026-09-01T10:00:00Z", model.ActorHuman, model.KindMessage, "user", "audit login-flow"),
		ev("other:c2", "other:ccc333", "2026-09-01T10:10:00Z", model.ActorPeer, model.KindMessage, "user", "peer says hi"),
	}
	if err := s.WriteBatch(ctx, SourceState{Path: "/src/a.jsonl", Harness: "claude", Offset: 10}, sessions, evs); err != nil {
		t.Fatal(err)
	}
}

func ids(list []SessionInfo) string {
	var out []string
	for _, s := range list {
		out = append(out, s.ID)
	}
	return strings.Join(out, ",")
}

func TestSessions(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	tests := []struct {
		name string
		f    SessionFilter
		want string
	}{
		{"all newest first", SessionFilter{}, "claude:aaa111,claude:aab222,other:ccc333"},
		{"since", SessionFilter{Since: ts("2026-10-01T00:00:00Z")}, "claude:aaa111,claude:aab222"},
		{"since overlaps end", SessionFilter{Since: ts("2026-10-31T23:00:00Z")}, "claude:aaa111"},
		{"until", SessionFilter{Until: ts("2026-10-01T00:00:00Z")}, "other:ccc333"},
		{"repo substring remote", SessionFilter{Repo: "acme/app"}, "claude:aaa111,other:ccc333"},
		{"repo substring cwd", SessionFilter{Repo: "docs"}, "claude:aab222"},
		{"harness", SessionFilter{Harness: "other"}, "other:ccc333"},
		{"name case-insensitive", SessionFilter{Name: "login"}, "claude:aaa111,other:ccc333"},
		{"limit", SessionFilter{Limit: 1}, "claude:aaa111"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Sessions(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if ids(got) != tt.want {
				t.Errorf("got %s, want %s", ids(got), tt.want)
			}
		})
	}
	got, _ := s.Sessions(ctx, SessionFilter{Harness: "claude", Name: "Fix"})
	if len(got) != 1 || got[0].HumanEvents != 3 || got[0].AgentEvents != 3 || got[0].Events != 8 || got[0].Repo() != "https://github.com/acme/app" {
		t.Errorf("counts = %+v", got)
	}
}

func TestFindSession(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	tests := []struct {
		ref, want string
		err       error
	}{
		{"claude:aaa111", "claude:aaa111", nil},
		{"aaa111", "claude:aaa111", nil},
		{"aaa", "claude:aaa111", nil},
		{"claude:aab", "claude:aab222", nil},
		{"aa", "", ErrAmbiguous},
		{"zzz", "", ErrNotFound},
		{"", "", ErrNotFound},
	}
	for _, tt := range tests {
		got, err := s.FindSession(ctx, tt.ref)
		if tt.err != nil {
			if err != tt.err {
				t.Errorf("FindSession(%q) err = %v, want %v", tt.ref, err, tt.err)
			}
			continue
		}
		if err != nil || got.ID != tt.want {
			t.Errorf("FindSession(%q) = %q, %v", tt.ref, got.ID, err)
		}
	}
}

func TestEventsFilter(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	msgs, err := s.Events(ctx, "claude:aaa111", false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range msgs {
		got = append(got, e.ID)
	}
	if strings.Join(got, ",") != "claude:a1,claude:a2,claude:a5,claude:a6,claude:a8" {
		t.Errorf("messages = %v", got)
	}
	all, _ := s.Events(ctx, "claude:aaa111", true)
	if len(all) != 8 {
		t.Errorf("all = %d", len(all))
	}
}

func TestSearch(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	tests := []struct {
		name  string
		q     string
		f     SearchFilter
		want  []string
		snipp string
	}{
		{"word", "login", SearchFilter{}, []string{"claude:a1", "claude:a2", "claude:a3", "claude:a4", "other:c1"}, ""},
		{"actor human", "login", SearchFilter{Actor: "human"}, []string{"claude:a1", "other:c1"}, ""},
		{"harness", "login", SearchFilter{SessionFilter: SessionFilter{Harness: "other"}}, []string{"other:c1"}, ""},
		{"repo and since", "login", SearchFilter{SessionFilter: SessionFilter{Repo: "acme", Since: ts("2026-10-01T00:00:00Z")}}, []string{"claude:a1", "claude:a2", "claude:a3", "claude:a4"}, ""},
		{"hyphen term is quoted", "login-flow", SearchFilter{}, []string{"other:c1"}, ""},
		{"case and diacritics folding", "GRÜßE", SearchFilter{}, []string{"claude:b1"}, ""},
		{"multi term AND", "session cookie", SearchFilter{}, []string{"claude:a5"}, "the [session] [cookie]"},
		{"raw fts syntax", "login NOT handler", SearchFilter{Raw: true, Actor: "agent"}, []string{"claude:a3"}, ""},
		{"prefix raw", "cook*", SearchFilter{Raw: true}, []string{"claude:a5"}, ""},
		{"limit", "login", SearchFilter{Limit: 2}, nil, ""},
		{"no hits", "kubernetes", SearchFilter{}, []string{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := s.Search(ctx, tt.q, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if tt.name == "limit" {
				if len(hits) != 2 {
					t.Errorf("limit: %d hits", len(hits))
				}
				return
			}
			got := map[string]bool{}
			for _, h := range hits {
				got[h.Event.ID] = true
				if h.Snippet == "" || h.SessionName == "" {
					t.Errorf("hit without snippet/session: %+v", h)
				}
				if tt.snipp != "" && !strings.Contains(h.Snippet, tt.snipp) {
					t.Errorf("snippet %q lacks %q", h.Snippet, tt.snipp)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("hits = %v, want %v", got, tt.want)
			}
			for _, w := range tt.want {
				if !got[w] {
					t.Errorf("missing hit %s (got %v)", w, got)
				}
			}
		})
	}
	if _, err := s.Search(ctx, "AND (", SearchFilter{Raw: true}); err == nil {
		t.Error("expected FTS syntax error")
	}
}

func TestExchangesView(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	_, rows, err := s.Query(ctx, "SELECT session_id, ts, request, response FROM exchanges WHERE session_id='claude:aaa111' ORDER BY ts")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]any{
		{"claude:aaa111", "2026-10-31T22:00:00.000Z", "please fix the login bug", "fixed the session cookie"},
		{"claude:aaa111", "2026-10-31T23:10:00.000Z", "now run the tests", nil},
		{"claude:aaa111", "2026-10-31T23:30:00.000Z", "no answer to this one", nil},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %v", rows)
	}
	for i := range want {
		for j := range want[i] {
			if rows[i][j] != want[i][j] {
				t.Errorf("row %d col %d = %v, want %v", i, j, rows[i][j], want[i][j])
			}
		}
	}
}

func TestDailyActivityView(t *testing.T) {
	s, _ := openTestStore(t) // default location Europe/Berlin
	seed(t, s)
	_, rows, err := s.Query(ctx, "SELECT day, session_id, repo, human_events, agent_events FROM daily_activity WHERE session_id='claude:aaa111' ORDER BY day")
	if err != nil {
		t.Fatal(err)
	}
	// 22:00Z..22:20Z on 10-31 is 23:xx local; 23:10Z and later is 11-01 local.
	want := [][]any{
		{"2026-10-31", "claude:aaa111", "https://github.com/acme/app", int64(1), int64(3)},
		{"2026-11-01", "claude:aaa111", "https://github.com/acme/app", int64(2), int64(0)},
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %v", rows)
	}
	for i := range want {
		for j := range want[i] {
			if rows[i][j] != want[i][j] {
				t.Errorf("row %d col %d = %v (%T), want %v", i, j, rows[i][j], rows[i][j], want[i][j])
			}
		}
	}
}

func TestStats(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	berlin, _ := time.LoadLocation("Europe/Berlin")
	st, err := s.Stats(ctx, StatsFilter{Since: ts("2026-10-01T00:00:00Z"), Until: ts("2026-11-02T00:00:00Z"), Location: berlin})
	if err != nil {
		t.Fatal(err)
	}
	if st.Total != (Activity{Sessions: 2, Human: 4, Agent: 4}) {
		t.Errorf("total = %+v", st.Total)
	}
	days := map[string]Activity{}
	for _, d := range st.ByDay {
		days[d.Day] = d.Activity
	}
	wantDays := map[string]Activity{
		"2026-10-15": {Sessions: 1, Human: 1, Agent: 1},
		"2026-10-31": {Sessions: 1, Human: 1, Agent: 3},
		"2026-11-01": {Sessions: 1, Human: 2, Agent: 0},
	}
	if len(days) != len(wantDays) {
		t.Errorf("days = %v", days)
	}
	for k, v := range wantDays {
		if days[k] != v {
			t.Errorf("day %s = %+v, want %+v", k, days[k], v)
		}
	}
	if st.ByDay[0].Day != "2026-10-15" {
		t.Errorf("days not sorted: %v", st.ByDay)
	}
	// 2026-10-31 is a Saturday (index 5, Monday=0); 23:xx local.
	if got := st.ByWeekdayHour[5][23]; got != (Activity{Sessions: 1, Human: 1, Agent: 3}) {
		t.Errorf("Sat 23h = %+v", got)
	}
	// 2026-11-01 Sunday 00:xx local.
	if got := st.ByWeekdayHour[6][0]; got != (Activity{Sessions: 1, Human: 2}) {
		t.Errorf("Sun 0h = %+v", got)
	}
	repos := map[string]Activity{}
	for _, r := range st.ByRepo {
		repos[r.Repo] = r.Activity
	}
	if repos["https://github.com/acme/app"] != (Activity{Sessions: 1, Human: 3, Agent: 3}) || repos["/w/docs"] != (Activity{Sessions: 1, Human: 1, Agent: 1}) {
		t.Errorf("repos = %v", repos)
	}
	// UTC puts all of aaa111 on 10-31.
	stu, _ := s.Stats(ctx, StatsFilter{Since: ts("2026-10-31T00:00:00Z"), Until: ts("2026-11-01T00:00:00Z"), Location: time.UTC})
	if len(stu.ByDay) != 1 || stu.ByDay[0].Activity != (Activity{Sessions: 1, Human: 3, Agent: 3}) {
		t.Errorf("utc days = %+v", stu.ByDay)
	}
}

func TestStatus(t *testing.T) {
	s, _ := openTestStore(t, WithClock(func() time.Time { return ts("2026-11-01T12:00:00Z") }))
	seed(t, s)
	st, err := s.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sessions != 3 || st.Events != 12 || st.Sources != 1 || !st.LastIngest["claude"].Equal(ts("2026-11-01T12:00:00Z")) {
		t.Errorf("status = %+v", st)
	}
}

func TestSessionsTopLevel(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	sub := model.Session{ID: "claude:agent-x", Harness: "claude", NativeID: "agent-x", ParentID: "claude:aaa111",
		StartedAt: ts("2026-10-31T10:00:00Z"), EndedAt: ts("2026-10-31T10:05:00Z")}
	if err := s.WriteBatch(ctx, SourceState{}, []model.Session{sub}, nil); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Sessions(ctx, SessionFilter{})
	top, _ := s.Sessions(ctx, SessionFilter{TopLevel: true})
	if len(all) != len(top)+1 || strings.Contains(ids(top), "agent-x") {
		t.Errorf("all = %s, top = %s", ids(all), ids(top))
	}
}

func TestSessionsHideEmpty(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	// A title-only session (no events, no start) and a session with a start
	// time but no events.
	if err := s.WriteBatch(ctx, SourceState{}, []model.Session{
		{ID: "claude:empty", Harness: "claude", NativeID: "empty", Name: "only a title"},
		{ID: "claude:timed", Harness: "claude", NativeID: "timed", StartedAt: ts("2026-08-01T10:00:00Z"), EndedAt: ts("2026-08-01T10:00:00Z")},
	}, nil); err != nil {
		t.Fatal(err)
	}
	all, _ := s.Sessions(ctx, SessionFilter{})
	hidden, _ := s.Sessions(ctx, SessionFilter{HideEmpty: true})
	if !strings.Contains(ids(all), "claude:empty") {
		t.Errorf("all = %s", ids(all))
	}
	if got, want := ids(hidden), "claude:aaa111,claude:aab222,other:ccc333,claude:timed"; got != want {
		t.Errorf("HideEmpty = %s, want %s", got, want)
	}
}

func eventIDs(evs []model.Event) string {
	var out []string
	for _, e := range evs {
		out = append(out, e.ID)
	}
	return strings.Join(out, ",")
}

func TestEventWindow(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	tests := []struct {
		name string
		f    EventFilter
		want string
		err  error
	}{
		{"conversation", EventFilter{}, "claude:a1,claude:a2,claude:a5,claude:a6,claude:a8", nil},
		{"all", EventFilter{All: true}, "claude:a1,claude:a2,claude:a3,claude:a4,claude:a5,claude:a6,claude:a7,claude:a8", nil},
		{"since inclusive", EventFilter{Since: ts("2026-10-31T22:20:00Z")}, "claude:a5,claude:a6,claude:a8", nil},
		{"until exclusive", EventFilter{Until: ts("2026-10-31T22:20:00Z")}, "claude:a1,claude:a2", nil},
		{"since and until", EventFilter{All: true, Since: ts("2026-10-31T22:10:00Z"), Until: ts("2026-10-31T22:12:00Z")}, "claude:a2,claude:a3", nil},
		{"limit", EventFilter{Limit: 2}, "claude:a1,claude:a2", nil},
		{"offset", EventFilter{Offset: 3}, "claude:a6,claude:a8", nil},
		{"limit and offset", EventFilter{Limit: 2, Offset: 1}, "claude:a2,claude:a5", nil},
		{"offset past end", EventFilter{Offset: 10}, "", nil},
		{"from event", EventFilter{FromEvent: "claude:a5"}, "claude:a5,claude:a6,claude:a8", nil},
		{"from event with limit", EventFilter{FromEvent: "claude:a5", Limit: 1}, "claude:a5", nil},
		{"from hidden event", EventFilter{FromEvent: "claude:a3"}, "claude:a5,claude:a6,claude:a8", nil},
		{"from event and since", EventFilter{FromEvent: "claude:a2", Since: ts("2026-10-31T23:00:00Z")}, "claude:a6,claude:a8", nil},
		{"from unknown event", EventFilter{FromEvent: "claude:zz"}, "", ErrNotFound},
		{"from event of another session", EventFilter{FromEvent: "claude:b1"}, "", ErrNotFound},
		{"empty window", EventFilter{Since: ts("2027-01-01T00:00:00Z")}, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evs, err := s.EventWindow(ctx, "claude:aaa111", tt.f)
			if tt.err != nil {
				if err != tt.err {
					t.Fatalf("err = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := eventIDs(evs); got != tt.want {
				t.Errorf("events = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSearchSession(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	tests := []struct {
		name, q, session string
		want             []string
	}{
		{"session filter", "login", "claude:aaa111", []string{"claude:a1", "claude:a2", "claude:a3", "claude:a4"}},
		{"other session", "login", "other:ccc333", []string{"other:c1"}},
		{"session without hits", "login", "claude:aab222", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := s.Search(ctx, tt.q, SearchFilter{SessionID: tt.session})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, h := range hits {
				got[h.Event.ID] = true
				if h.Event.SessionID != tt.session {
					t.Errorf("hit from session %s, want %s", h.Event.SessionID, tt.session)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("hits = %v, want %v", got, tt.want)
			}
			for _, w := range tt.want {
				if !got[w] {
					t.Errorf("missing hit %s", w)
				}
			}
		})
	}
}

func TestSearchBySession(t *testing.T) {
	s, _ := openTestStore(t)
	seed(t, s)
	type row struct {
		id, first, last string
		hits            int
	}
	tests := []struct {
		name  string
		q     string
		f     SearchFilter
		order SearchOrder
		want  []row
		snip  string // snippet of the first row
	}{
		{"timeline oldest first", "login", SearchFilter{}, OrderFirstHit, []row{
			{"other:ccc333", "2026-09-01T10:00:00Z", "2026-09-01T10:00:00Z", 1},
			{"claude:aaa111", "2026-10-31T22:00:00Z", "2026-10-31T22:12:00Z", 4}}, "audit [login]-flow"},
		{"order hits", "login", SearchFilter{}, OrderHits, []row{
			{"claude:aaa111", "2026-10-31T22:00:00Z", "2026-10-31T22:12:00Z", 4},
			{"other:ccc333", "2026-09-01T10:00:00Z", "2026-09-01T10:00:00Z", 1}}, "please fix the [login] bug"},
		{"actor", "login", SearchFilter{Actor: "agent"}, OrderFirstHit, []row{
			{"claude:aaa111", "2026-10-31T22:10:00Z", "2026-10-31T22:11:00Z", 2}}, "looking at the [login] handler"},
		{"since on event time", "login", SearchFilter{SessionFilter: SessionFilter{Since: ts("2026-10-31T22:05:00Z")}}, OrderFirstHit, []row{
			{"claude:aaa111", "2026-10-31T22:10:00Z", "2026-10-31T22:12:00Z", 3}}, ""},
		{"repo", "login", SearchFilter{SessionFilter: SessionFilter{Repo: "acme", Until: ts("2026-10-01T00:00:00Z")}}, OrderFirstHit, []row{
			{"other:ccc333", "2026-09-01T10:00:00Z", "2026-09-01T10:00:00Z", 1}}, ""},
		{"session", "login", SearchFilter{SessionID: "other:ccc333"}, OrderFirstHit, []row{
			{"other:ccc333", "2026-09-01T10:00:00Z", "2026-09-01T10:00:00Z", 1}}, ""},
		{"raw", "login NOT handler", SearchFilter{Raw: true}, OrderFirstHit, []row{
			{"other:ccc333", "2026-09-01T10:00:00Z", "2026-09-01T10:00:00Z", 1},
			{"claude:aaa111", "2026-10-31T22:00:00Z", "2026-10-31T22:12:00Z", 3}}, ""},
		{"limit by hits", "login", SearchFilter{SessionFilter: SessionFilter{Limit: 1}}, OrderHits, []row{
			{"claude:aaa111", "2026-10-31T22:00:00Z", "2026-10-31T22:12:00Z", 4}}, ""},
		// Three sessions match; the limit keeps the two with the latest hits, shown oldest first.
		{"limit keeps most recent", "login OR docs", SearchFilter{Raw: true, SessionFilter: SessionFilter{Limit: 2}}, OrderFirstHit, []row{
			{"claude:aab222", "2026-10-15T08:00:00Z", "2026-10-15T09:00:00Z", 2},
			{"claude:aaa111", "2026-10-31T22:00:00Z", "2026-10-31T22:12:00Z", 4}}, ""},
		{"limit one keeps the newest", "login", SearchFilter{SessionFilter: SessionFilter{Limit: 1}}, OrderFirstHit, []row{
			{"claude:aaa111", "2026-10-31T22:00:00Z", "2026-10-31T22:12:00Z", 4}}, ""},
		{"no hits", "kubernetes", SearchFilter{}, OrderFirstHit, nil, ""},
		{"empty query", "   ", SearchFilter{}, OrderFirstHit, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.SearchBySession(ctx, tt.q, tt.f, tt.order)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d rows, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				g := got[i]
				if g.SessionID != w.id || g.Hits != w.hits || g.FirstHit != ts(w.first) || g.LastHit != ts(w.last) {
					t.Errorf("row %d = %s %d %s..%s, want %s %d %s..%s", i, g.SessionID, g.Hits, g.FirstHit.Format(time.RFC3339),
						g.LastHit.Format(time.RFC3339), w.id, w.hits, w.first, w.last)
				}
				if g.SessionName == "" || g.Repo == "" || g.Snippet == "" {
					t.Errorf("row %d without name/repo/snippet: %+v", i, g)
				}
			}
			if tt.snip != "" && got[0].Snippet != tt.snip {
				t.Errorf("snippet = %q, want %q", got[0].Snippet, tt.snip)
			}
		})
	}
	if _, err := s.SearchBySession(ctx, `"unbalanced`, SearchFilter{Raw: true}, OrderFirstHit); err == nil {
		t.Error("bad FTS syntax: no error")
	}
	if _, err := s.SearchBySession(ctx, "login", SearchFilter{}, SearchOrder("rank")); err == nil {
		t.Error("unknown order: no error")
	}
}

// TestReadPathsDoNotLoadRaw: the read queries behind show, search, sessions,
// stats and the TUI never load events.raw; no read command outputs it and it
// is the largest column by far (#131). The column stays stored for the
// paths that need it (UserMessages, backup).
func TestReadPathsDoNotLoadRaw(t *testing.T) {
	for _, c := range strings.Split(eventCols, ",") {
		if strings.TrimSuffix(strings.TrimSpace(c), ".raw") != strings.TrimSpace(c) || strings.TrimSpace(c) == "raw" {
			t.Fatalf("eventCols contains raw: %q", eventCols)
		}
	}
	// Every SQL string of the package: only the writer, the one reader that
	// needs it (UserMessages), the raw codec (raw.go: RawStats, raw_text)
	// and the migrations may name the column.
	allowed := map[string]bool{"write.go": true, "reclassify.go": true, "schema.go": true, "raw.go": true, "migrate_v5.go": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	word := regexp.MustCompile(`\braw\b`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] {
			continue
		}
		src, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(src, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && word.MatchString(lit.Value) {
				t.Errorf("%s: a query names raw: %s", f, lit.Value)
			}
			return true
		})
	}
	s, _ := openTestStore(t)
	sess, evs := sampleBatch()
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM events WHERE raw IS NOT NULL AND raw <> ''"); n != len(evs) {
		t.Fatalf("stored raw rows = %d, want %d", n, len(evs))
	}
	got, err := s.Events(ctx, "claude:s1", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Raw != "" {
			t.Errorf("Events: %s has raw loaded", e.ID)
		}
	}
	hits, err := s.Search(ctx, "login", SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("Search: no hits")
	}
	for _, h := range hits {
		if h.Event.Raw != "" {
			t.Errorf("Search: %s has raw loaded", h.Event.ID)
		}
	}
	var seen int
	err = s.UserMessages(ctx, "claude", func(m UserMessage) error {
		if m.Raw != "" {
			seen++
		}
		return nil
	})
	if err != nil || seen == 0 {
		t.Errorf("UserMessages: raw rows = %d, err = %v; reindex --actors still needs raw", seen, err)
	}
}

// BenchmarkEventsLargeRaw reads a session whose events carry a raw line of
// a few KiB each, the shape of tool calls in a real archive: the cost show
// pays when the read path loads raw (#131).
func BenchmarkEventsLargeRaw(b *testing.B) {
	dir := b.TempDir()
	s, err := Open(dir + "/bench.db")
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	raw := `{"filler":"` + strings.Repeat("x", 4<<10) + `"}`
	sess := []model.Session{{ID: "claude:s1", Harness: "claude", NativeID: "s1", Cwd: "/w/app"}}
	var evs []model.Event
	for i := 0; i < 2000; i++ {
		t := time.Date(2026, 10, 31, 22, 0, i, 0, time.UTC).Format(time.RFC3339)
		e := ev(fmt.Sprintf("claude:e%04d", i), "claude:s1", t, model.ActorAgent, model.KindToolResult, "tool", "login.go:12")
		e.Raw = raw
		evs = append(evs, e)
	}
	if err := s.WriteBatch(ctx, SourceState{}, sess, evs); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := s.Events(ctx, "claude:s1", true)
		if err != nil || len(got) != len(evs) {
			b.Fatalf("Events = %d %v", len(got), err)
		}
	}
}
