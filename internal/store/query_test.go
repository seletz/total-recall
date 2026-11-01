package store

import (
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
