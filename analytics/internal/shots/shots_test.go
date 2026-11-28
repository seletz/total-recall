package shots

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/analytics/internal/report"
	"github.com/seletz/total-recall/internal/screenshot"
)

var update = flag.Bool("update", false, "rewrite the goldens in testdata/frames")

// dbPath is the analytics database of the demo pool, built once by
// TestMain.
var dbPath string

func TestMain(m *testing.M) {
	flag.Parse()
	dir, err := os.MkdirTemp("", "analytics-shots-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if dbPath, _, err = Demo(context.Background(), dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var (
	once   sync.Once
	frames []screenshot.Frame
	errAll error
)

// rendered renders every shot once for the package.
func rendered(t *testing.T) []screenshot.Frame {
	t.Helper()
	once.Do(func() {
		for _, s := range Shots {
			f, err := Render(s, dbPath)
			if err != nil {
				errAll = err
				return
			}
			frames = append(frames, f)
		}
	})
	if errAll != nil {
		t.Fatal(errAll)
	}
	return frames
}

// TestFramesGolden: every shot equals its golden (mise run
// analytics:screenshots rewrites them with the images).
func TestFramesGolden(t *testing.T) {
	fs := rendered(t)
	if *update {
		if err := screenshot.WriteGoldens("testdata/frames", fs); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range fs {
		want, err := os.ReadFile(screenshot.GoldenPath("testdata/frames", f.Shot.Name))
		if err != nil {
			t.Errorf("%s: %v (run mise run analytics:screenshots)", f.Shot.Name, err)
			continue
		}
		if f.Plain()+"\n" != string(want) {
			t.Errorf("shot %s differs from its golden; run mise run analytics:screenshots and commit the images:\n%s", f.Shot.Name, f.Plain())
		}
	}
}

// TestFramesFit: every frame has the size of its shot.
func TestFramesFit(t *testing.T) {
	for _, f := range rendered(t) {
		lines := strings.Split(f.Plain(), "\n")
		if len(lines) != Height {
			t.Errorf("%s: %d lines, want %d", f.Shot.Name, len(lines), Height)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > Width {
				t.Errorf("%s: line %d is %d cells wide", f.Shot.Name, i, w)
			}
		}
	}
}

// TestFramesHaveGlyphs: the PNG renderer can draw every character.
func TestFramesHaveGlyphs(t *testing.T) {
	for _, f := range rendered(t) {
		missing, err := screenshot.MissingGlyphs(f.Plain())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range missing {
			t.Errorf("no glyph for %q (U+%04X) in frame %s", r, r, f.Shot.Name)
		}
	}
}

// TestKeysAndMouse: every choice is reachable with its key and with the
// mouse, and the footer shows the actions of the view first.
func TestKeysAndMouse(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
		want []string
		lack []string
	}{
		{"start", nil, []string{"[events]", "[project]", "[week]", "[all]", "26 of 26 weeks"}, nil},
		{"by key", []string{"b"}, []string{"by       project [host]", "events by host"}, nil},
		{"period key", []string{"p"}, []string{"[month]", "6 of 6 months"}, nil},
		{"host key", []string{"h"}, []string{"[host-a]"}, nil},
		{"show key back", []string{"M"}, []string{"[hours]", "hours by project"}, nil},
		{"show click keeps by", []string{"click:by=host", "click:show=tool calls"}, []string{"[tool calls]", "tool calls by host"}, nil},
		{"hours has no model", []string{"click:by=model", "click:show=hours"}, []string{"by       [category] project host"}, nil},
		{"window key", []string{"click:period=day", "left"}, []string{"35 of 181 days", "2026-09-10 … 2026-10-14"}, nil},
		{"window wheel", []string{"click:period=day", "wheel:up", "wheel:up", "wheel:down"}, []string{"2026-09-10 … 2026-10-14"}, nil},
		{"window arrow", []string{"click:period=day", "click:range=←", "click:range=→"}, []string{"2026-09-27 … 2026-10-31"}, nil},
		{"help from the footer", []string{"click:? help"}, []string{"keys: usage", "esc back • q quit"}, []string{"m show •"}},
		{"help closes", []string{"?", "esc"}, []string{"m show • b by"}, []string{"keys: usage"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Render(Shot{Name: tc.name, Keys: tc.keys}, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			plain := f.Plain()
			for _, w := range tc.want {
				if !strings.Contains(plain, w) {
					t.Errorf("frame lacks %q:\n%s", w, plain)
				}
			}
			for _, w := range tc.lack {
				if strings.Contains(plain, w) {
					t.Errorf("frame has %q:\n%s", w, plain)
				}
			}
			lines := strings.Split(plain, "\n")
			if foot := lines[len(lines)-1]; !strings.HasPrefix(foot, " m show") && !strings.HasPrefix(foot, " esc back") {
				t.Errorf("footer does not start with the view's actions: %q", foot)
			}
		})
	}
}

// TestTotalsEqualReport: sum equals parts in the view: the rows of the
// table and its row all are the numbers of report (and so of usage and
// worktime) for the window shown.
func TestTotalsEqualReport(t *testing.T) {
	rowRe := regexp.MustCompile(`(?m)^ [■ ] (\S+(?: \S+)*?)\s+([0-9:]+)$`)
	for _, tc := range []struct {
		keys      []string
		f         report.Filter
		worktime  bool
		value     func(report.Usage) int64
		valueHour func(report.Worktime) string
	}{
		{nil, report.Filter{By: "project", Period: "total"}, false, func(u report.Usage) int64 { return u.Human + u.Agent }, nil},
		{[]string{"click:show=sessions", "click:by=host"}, report.Filter{By: "host", Period: "total"}, false, func(u report.Usage) int64 { return u.Sessions }, nil},
		{[]string{"click:show=hours", "click:host=host-b"}, report.Filter{By: "project", Period: "total", Hosts: []string{"host-b"}}, true, nil,
			func(w report.Worktime) string { return fmt.Sprintf("%d:%02d", w.RawSeconds/3600, (w.RawSeconds%3600+30)/60) }},
	} {
		f, err := Render(Shot{Name: "totals", Keys: tc.keys}, dbPath)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, m := range rowRe.FindAllStringSubmatch(f.Plain(), -1) {
			got[strings.TrimSpace(m[1])] = m[2]
		}
		want := map[string]string{}
		err = Loader(dbPath)(context.Background(), func(db report.Querier) error {
			for _, by := range []string{tc.f.By, "none"} {
				ff := tc.f
				ff.By = by
				if tc.worktime {
					rows, err := report.WorktimeRows(context.Background(), db, ff)
					if err != nil {
						return err
					}
					for _, r := range rows {
						want[keyOr(r.Key)] = tc.valueHour(r)
					}
					continue
				}
				rows, err := report.UsageRows(context.Background(), db, ff)
				if err != nil {
					return err
				}
				for _, r := range rows {
					want[keyOr(r.Key)] = fmt.Sprint(tc.value(r))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%v: %s = %q in the view, %q in the report\n%s", tc.keys, k, got[k], v, f.Plain())
			}
		}
	}
}

// keyOr names a report key as the table does: all for the grouping none.
func keyOr(k *string) string {
	if k == nil {
		return "all"
	}
	return *k
}
