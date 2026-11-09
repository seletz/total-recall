package cli

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// runTimeline runs timeline against the demo archive.
func runTimeline(t *testing.T, args ...string) []TimelineEventJSON {
	t.Helper()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	r := e.run(t, "", append([]string{"--db", db, "timeline", "--json"}, args...)...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	return decode[[]TimelineEventJSON](t, r.stdout)
}

func statsSeries(t *testing.T, args ...string) ActivitySeriesJSON {
	t.Helper()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	r := e.run(t, "", append([]string{"--db", db, "stats", "--json"}, args...)...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	return decode[ActivitySeriesJSON](t, r.stdout)
}

// TestTimelineMatchesStats is the invariant of #235: the human events of
// a window listed by timeline are exactly the human events stats counts
// for it, in total and per project; nothing drops out silently.
func TestTimelineMatchesStats(t *testing.T) {
	windows := [][]string{
		{"--since", "2026-10-26", "--until", "2026-10-30"},
		{"--since", "2026-10-01", "--until", "2026-10-31"},
		{"--since", "2026-05-04", "--until", "2026-10-31"},
		{"--since", "2026-10-20T20:00", "--until", "2026-10-21T06:00"},
	}
	for _, w := range windows {
		t.Run(strings.Join(w, " "), func(t *testing.T) {
			tl := runTimeline(t, w...)
			if len(tl) == 0 {
				t.Fatal("empty window, the check proves nothing")
			}
			st := statsSeries(t, append([]string{"--period", "day", "--by", "project"}, w...)...)
			if got := st.Total.Human; len(tl) != got {
				t.Errorf("timeline has %d events, stats counts %d human events", len(tl), got)
			}
			perProject := map[string]int{}
			sessions := map[string]bool{}
			for _, ev := range tl {
				perProject[string(ev.Project)]++
				sessions[ev.SessionID] = true
				if ev.Actor != "human" {
					t.Errorf("actor = %s", ev.Actor)
				}
			}
			sum := 0
			for _, c := range st.ByKey {
				sum += c.Human
				if perProject[c.Key] != c.Human {
					t.Errorf("project %s: timeline %d, stats %d", c.Key, perProject[c.Key], c.Human)
				}
			}
			if sum != len(tl) {
				t.Errorf("stats projects sum to %d, timeline has %d", sum, len(tl))
			}
			if len(sessions) == 0 {
				t.Error("no sessions")
			}
			// --project narrows to exactly the project's count.
			for _, c := range st.ByKey {
				if c.Human == 0 || c.Key == "other" {
					continue
				}
				one := runTimeline(t, append([]string{"--project", c.Key}, w...)...)
				for _, ev := range one {
					if string(ev.Project) != c.Key {
						t.Fatalf("--project %s returned an event of %s", c.Key, ev.Project)
					}
				}
				if len(one) != c.Human {
					t.Errorf("--project %s: %d events, stats %d", c.Key, len(one), c.Human)
				}
			}
		})
	}
}

func TestTimelineFields(t *testing.T) {
	tl := runTimeline(t, "--since", "2026-10-20T20:00", "--until", "2026-10-21T06:00", "--text-width", "30")
	if len(tl) == 0 {
		t.Fatal("no events")
	}
	if !slices.IsSortedFunc(tl, func(a, b TimelineEventJSON) int { return strings.Compare(a.TS, b.TS) }) {
		t.Error("not oldest first")
	}
	berlin := demo.Location
	lo := time.Date(2026, 10, 20, 20, 0, 0, 0, berlin)
	hi := time.Date(2026, 10, 21, 6, 0, 0, 0, berlin)
	for _, ev := range tl {
		ts, err := time.Parse("2006-01-02T15:04:05.000Z", ev.TS)
		if err != nil {
			t.Fatalf("ts %q: %v", ev.TS, err)
		}
		if ts.Before(lo) || !ts.Before(hi) {
			t.Errorf("event at %s outside the window", ev.TS)
		}
		local, err := time.Parse(time.RFC3339, ev.TSLocal)
		if err != nil || !local.Equal(ts) {
			t.Errorf("ts_local %q does not match ts %q: %v", ev.TSLocal, ev.TS, err)
		}
		if !strings.HasSuffix(ev.TSLocal, "+01:00") && !strings.HasSuffix(ev.TSLocal, "+02:00") {
			t.Errorf("ts_local %q is not Europe/Berlin", ev.TSLocal)
		}
		if ev.SessionID == "" || ev.EventID == "" || ev.Project == "" || ev.Kind != "message" {
			t.Errorf("event = %+v", ev)
		}
		if n := len([]rune(string(ev.Text))); n > 30 {
			t.Errorf("text has %d runes: %q", n, ev.Text)
		}
		if ev.TextTruncated != (ev.TextChars > 30) {
			t.Errorf("text_truncated = %v for %d chars", ev.TextTruncated, ev.TextChars)
		}
	}
}

func TestTimelineActorAndErrors(t *testing.T) {
	humans := runTimeline(t, "--since", "2026-10-27", "--until", "2026-10-27")
	agents := runTimeline(t, "--since", "2026-10-27", "--until", "2026-10-27", "--actor", "agent")
	all := runTimeline(t, "--since", "2026-10-27", "--until", "2026-10-27", "--actor", "all")
	byActor := map[string]int{}
	for _, ev := range all {
		byActor[ev.Actor]++
	}
	if len(agents) == 0 || byActor["human"] != len(humans) || byActor["agent"] != len(agents) {
		t.Errorf("human %d, agent %d, all by actor %v", len(humans), len(agents), byActor)
	}
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	for _, args := range [][]string{{"--project", "no-such-project-xyz"}, {"--since", "bogus"}} {
		if r := e.run(t, "", append([]string{"--db", db, "timeline", "--json"}, args...)...); r.code == 0 {
			t.Errorf("%v: exit 0, want an error", args)
		}
	}
}
