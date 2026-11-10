package cli

import (
	"fmt"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// TestTimelineArgErrors: a value timeline cannot use is a usage error like
// those of the other commands (same kind and exit code), never an empty
// list: an unknown actor, and a window that ends before it starts.
func TestTimelineArgErrors(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	ref := e.run(t, "", "--db", db, "sessions", "--json", "--since", "bogus")
	if ref.code == 0 || decode[ErrorJSON](t, ref.stdout).Error.Kind != "usage" {
		t.Fatalf("reference usage error: exit %d %s", ref.code, ref.stdout)
	}
	for _, tt := range []struct {
		name string
		args []string
		want string // in the message
	}{
		{"unknown actor", []string{"--actor", "bogus"}, "human, agent, peer, automation, tool, all"},
		{"since after until", []string{"--since", "2026-10-21", "--until", "2026-10-20"}, "--since"},
		{"since after until with times", []string{"--since", "2026-10-20T20:00", "--until", "2026-10-20T19:00"}, "--since"},
		{"since equals until", []string{"--since", "2026-10-20T20:00", "--until", "2026-10-20T20:00"}, "--since"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := e.run(t, "", append([]string{"--db", db, "timeline", "--json"}, tt.args...)...)
			if r.code != ref.code {
				t.Errorf("exit %d, want %d like other usage errors", r.code, ref.code)
			}
			got := decode[ErrorJSON](t, r.stdout)
			if got.Error.Kind != "usage" || !strings.Contains(got.Error.Message, tt.want) {
				t.Errorf("error = %+v, want usage mentioning %q", got.Error, tt.want)
			}
		})
	}
	// Valid bounds still work: a date as --until includes its day, and
	// since before until on one day is fine.
	for _, args := range [][]string{
		{"--since", "2026-10-20", "--until", "2026-10-20"},
		{"--since", "2026-10-20T19:00", "--until", "2026-10-20T20:00"},
		{"--actor", "all"},
	} {
		if r := e.run(t, "", append([]string{"--db", db, "timeline", "--json"}, args...)...); r.code != 0 {
			t.Errorf("%v: exit %d: %s", args, r.code, r.stdout)
		}
	}
}

// TestTimelineAllImpliesActorAll: --all without --actor takes every actor
// (tool calls and results belong to agent and tool); an explicit --actor
// wins, also an explicit human.
func TestTimelineAllImpliesActorAll(t *testing.T) {
	t.Parallel()
	w := []string{"--since", "2026-10-20", "--until", "2026-10-20"}
	actors := func(evs []TimelineEventJSON) map[string]int {
		m := map[string]int{}
		for _, ev := range evs {
			m[ev.Actor]++
		}
		return m
	}
	implied := runTimeline(t, append([]string{"--all"}, w...)...)
	explicit := runTimeline(t, append([]string{"--all", "--actor", "all"}, w...)...)
	if len(implied) == 0 || len(implied) != len(explicit) {
		t.Fatalf("--all: %d events, --all --actor all: %d", len(implied), len(explicit))
	}
	if a := actors(implied); a["agent"] == 0 || a["human"] == 0 {
		t.Errorf("--all by actor = %v, want every actor", a)
	}
	human := runTimeline(t, append([]string{"--all", "--actor", "human"}, w...)...)
	if a := actors(human); len(a) != 1 || a["human"] == 0 {
		t.Errorf("--all --actor human by actor = %v, want only human", a)
	}
	if plain := runTimeline(t, w...); len(plain) >= len(implied) {
		t.Errorf("plain %d events, --all %d: --all must add events", len(plain), len(implied))
	}
}

// TestTimelineFieldsFlag: --fields keeps the named keys of every event, in
// the fixed order of the full object, with the same values; unknown names
// are usage errors.
func TestTimelineFieldsFlag(t *testing.T) {
	t.Parallel()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	run := func(args ...string) result {
		return e.run(t, "", append([]string{"--db", db, "timeline", "--json", "--since=2026-10-20", "--until=2026-10-20", "--actor=all", "--all"}, args...)...)
	}
	full := decode[[]map[string]any](t, run().stdout)
	if len(full) == 0 {
		t.Fatal("no events")
	}
	for _, tc := range []struct {
		name   string
		fields string
		keys   []string // in output order
	}{
		{"one", "text", []string{"text"}},
		{"given order does not matter", "text,event_id,project", []string{"event_id", "project", "text"}},
		{"spaces and repeats", " kind , kind,actor", []string{"actor", "kind"}},
		{"all of them", strings.Join(timelineFieldNames, ","), timelineFieldNames},
		{"empty is all", "", timelineFieldNames},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run("--fields=" + tc.fields)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			got := decode[[]map[string]any](t, r.stdout)
			if len(got) != len(full) {
				t.Fatalf("%d events, want %d", len(got), len(full))
			}
			for i, ev := range got {
				if len(ev) != len(tc.keys) {
					t.Fatalf("event %d has keys %v, want %v", i, ev, tc.keys)
				}
				for _, k := range tc.keys {
					if fmt.Sprint(ev[k]) != fmt.Sprint(full[i][k]) {
						t.Errorf("event %d %s = %v, full has %v", i, k, ev[k], full[i][k])
					}
				}
			}
			// The order of the keys in the bytes, not only the set.
			first := strings.Index(r.stdout, "{")
			last := -1
			for _, k := range tc.keys {
				at := strings.Index(r.stdout[first:], `"`+k+`":`)
				if at < last {
					t.Errorf("key %s out of order in %.200s", k, r.stdout[first:])
				}
				last = at
			}
		})
	}
	// Without --fields, and with all of them, the bytes are the old output.
	if a, b := run().stdout, run("--fields="+strings.Join(timelineFieldNames, ",")).stdout; a != b {
		t.Error("all fields differ from the default output")
	}
	for _, bad := range []string{"nope", "text,nope", "ts_utc"} {
		r := run("--fields=" + bad)
		if r.code != 1 || !strings.Contains(r.stdout, `"kind":"usage"`) || !strings.Contains(r.stdout, "invalid --fields") {
			t.Errorf("--fields=%s: exit %d %s", bad, r.code, r.stdout)
		}
	}
}
