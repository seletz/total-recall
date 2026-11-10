package cli

import (
	"strings"
	"testing"
)

type dayWorkdayJSON struct {
	Days []struct {
		Day          string `json:"day"`
		First        string `json:"first"`
		Span         int    `json:"span_seconds"`
		Target       int    `json:"target_seconds"`
		Diff         int    `json:"diff_seconds"`
		BlockSeconds int    `json:"block_seconds"`
		BreakSeconds int    `json:"break_seconds"`
		Human        int    `json:"human"`
		Agent        int    `json:"agent"`
		Blocks       []struct {
			Start   string `json:"start"`
			End     string `json:"end"`
			Seconds int    `json:"seconds"`
			Events  int    `json:"events"`
			Single  bool   `json:"single"`
		} `json:"blocks"`
	} `json:"days"`
}

type dayWorktimeJSON struct {
	Days []struct {
		Day     string `json:"day"`
		Raw     int    `json:"raw_seconds"`
		Rounded int    `json:"rounded_seconds"`
		Cats    []struct {
			Category string `json:"category"`
			Raw      int    `json:"raw_seconds"`
			Rounded  int    `json:"rounded_seconds"`
		} `json:"categories"`
	} `json:"days"`
}

// TestStatsDayDemo is the acceptance invariant of #276 on the real --json of
// the demo archive: for every active day of October 2026 and both counting
// modes, stats --day equals stats --workday --blocks and --worktime for the
// day, and its parts add up.
func TestStatsDayDemo(t *testing.T) {
	e, db := chartEnv(t)
	for _, mode := range [][]string{nil, {"--all-events"}} {
		wd := decode[dayWorkdayJSON](t, e.mustRun(t, append([]string{"--db", db, "stats", "--workday", "--blocks", "--json", "--month", "2026-10"}, mode...)...))
		wt := decode[dayWorktimeJSON](t, e.mustRun(t, "--db", db, "stats", "--worktime", "--json", "--month", "2026-10"))
		worktime := map[string][2]int{}
		cats := map[string]map[string][2]int{}
		for _, d := range wt.Days {
			worktime[d.Day] = [2]int{d.Raw, d.Rounded}
			cats[d.Day] = map[string][2]int{}
			for _, c := range d.Cats {
				cats[d.Day][c.Category] = [2]int{c.Raw, c.Rounded}
			}
		}
		active := 0
		for _, w := range wd.Days {
			if w.First == "" {
				continue
			}
			active++
			d := decode[DayJSON](t, e.mustRun(t, append([]string{"--db", db, "stats", "--day", w.Day, "--json"}, mode...)...))
			if d.Day != w.Day || d.SpanSeconds != w.Span || d.BlockSeconds != w.BlockSeconds || d.BreakSeconds != w.BreakSeconds ||
				d.TargetSeconds != w.Target || d.DiffSeconds != w.Diff || d.Human != w.Human || d.Agent != w.Agent {
				t.Errorf("%s %v: header %+v differs from workday %+v", w.Day, mode, d.SpanSeconds, w)
			}
			if d.BlockSeconds+d.BreakSeconds != d.SpanSeconds {
				t.Errorf("%s: blocks + breaks != span", w.Day)
			}
			if len(d.Blocks) != len(w.Blocks) {
				t.Fatalf("%s: %d blocks, want %d", w.Day, len(d.Blocks), len(w.Blocks))
			}
			sum, events, human, agent := 0, 0, 0, 0
			for i, b := range d.Blocks {
				if b.Start != w.Blocks[i].Start || b.End != w.Blocks[i].End || b.Seconds != w.Blocks[i].Seconds || b.Events != w.Blocks[i].Events || b.Single != w.Blocks[i].Single {
					t.Errorf("%s block %d differs from workday", w.Day, i)
				}
				ps := 0
				for _, p := range b.Projects {
					ps += p.Seconds
				}
				if ps != b.Seconds {
					t.Errorf("%s block %d: projects %d s, block %d s", w.Day, i, ps, b.Seconds)
				}
				sum += b.Seconds
				events += b.Events
				human += b.Human
				agent += b.Agent
			}
			if sum != d.BlockSeconds || human != d.Human || agent != d.Agent || events != d.Human+d.Agent {
				t.Errorf("%s: blocks sum %d/%d events %d (%d+%d)", w.Day, sum, d.BlockSeconds, events, d.Human, d.Agent)
			}
			raw, rounded, ph, pa := 0, 0, 0, 0
			for _, p := range d.Projects {
				raw += p.RawSeconds
				rounded += p.RoundedSeconds
				ph += p.Human
				pa += p.Agent
			}
			want := worktime[w.Day]
			if d.Worktime.RawSeconds != want[0] || raw != want[0] || d.Worktime.RoundedSeconds != want[1] {
				t.Errorf("%s: worktime %d/%d (projects %d), want %d/%d", w.Day, d.Worktime.RawSeconds, d.Worktime.RoundedSeconds, raw, want[0], want[1])
			}
			// The categories are what adds up to the day, raw and rounded,
			// and are the cells of --worktime.
			craw, crounded := 0, 0
			for _, c := range d.Categories {
				craw += c.RawSeconds
				crounded += c.RoundedSeconds
				if got, ok := cats[w.Day][c.Category]; !ok || got != [2]int{c.RawSeconds, c.RoundedSeconds} {
					t.Errorf("%s: category %s %d/%d, --worktime has %v", w.Day, c.Category, c.RawSeconds, c.RoundedSeconds, got)
				}
			}
			if len(d.Categories) != len(cats[w.Day]) || craw != want[0] || crounded != want[1] {
				t.Errorf("%s: categories %d (%d/%d s), want %d cells and %d/%d s", w.Day, len(d.Categories), craw, crounded, len(cats[w.Day]), want[0], want[1])
			}
			if ph != d.Human || pa != d.Agent {
				t.Errorf("%s: project events %d/%d, want %d/%d", w.Day, ph, pa, d.Human, d.Agent)
			}
		}
		if active < 5 {
			t.Fatalf("only %d active days", active)
		}
	}
}

func TestStatsDayText(t *testing.T) {
	e, db := chartEnv(t)
	out := e.mustRun(t, "--db", db, "stats", "--day", "2026-10-29")
	for _, want := range []string{"2026-10-29", "span", "PROJECT", "CATEGORY", "total", "1. "} {
		if !strings.Contains(out, want) {
			t.Errorf("text lacks %q:\n%s", want, out)
		}
	}
	if out := e.mustRun(t, "--db", db, "stats", "--day", "2020-01-01"); !strings.Contains(out, "no activity") {
		t.Errorf("empty day: %s", out)
	}
}

func TestStatsDayUsage(t *testing.T) {
	e, db := chartEnv(t)
	for _, args := range [][]string{{"--workday"}, {"--worktime"}, {"--month", "2026-10"}, {"--blocks"}, {"--period", "day"}} {
		r := e.run(t, "", append([]string{"--db", db, "stats", "--day", "2026-10-29"}, args...)...)
		if r.code == 0 {
			t.Errorf("--day %v succeeded", args)
		}
	}
	if r := e.run(t, "", "--db", db, "stats", "--day", "nonsense"); r.code == 0 {
		t.Error("bad date accepted")
	}
}
