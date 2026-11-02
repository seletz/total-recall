package screenshot

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// archives opens the demo archive and, standing in for the anonymized
// copy, the Activity fixture.
func archives(t *testing.T) Archives {
	t.Helper()
	anon, _ := fixturedb.Activity(t)
	return Archives{Anon: anon, Demo: demoStore(t)}
}

// TestRenderAll renders every shot: the frame has the shot's size and shows
// the markers of its view, and no frame holds a path of the machine.
func TestRenderAll(t *testing.T) {
	frames, err := RenderAll(archives(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != len(Shots) {
		t.Fatalf("%d frames, want %d", len(frames), len(Shots))
	}
	markers := map[string][]string{
		"search":         {"[1 search]", "> goreleaser cask", "hits (0 ms)", "Release v0.3.0", "mara/kompass", "◄"},
		"session":        {"› session", "Release v0.3.0", "· tools", "tool_use Read", "tool_use Bash", "tool (tool_result)", "goreleaser", "mara/kompass"},
		"stats":          {"[2 stats]", "period: [day] week month", "by: [project]", "text chart · 28 days · 0 ms"},
		"heatmap":        {"[2 stats]", "0 ms · heatmap", "Mon", "Sun"},
		"activity-week":  {"[3 activity]", "period: day [week] month", "week:", "Sun 2026-11-01", "Allerheiligen", "count: all [human]"},
		"activity-month": {"[3 activity]", "period: day week [month]", "month:", "2026-10", "Tag der deutschen Einheit"},
		"compact":        {"[3 activity]", "week:"},
	}
	for _, f := range frames {
		t.Run(f.Shot.Name, func(t *testing.T) {
			plain := f.Plain()
			lines := strings.Split(plain, "\n")
			if len(lines) != f.Shot.Height {
				t.Errorf("%d lines, want %d", len(lines), f.Shot.Height)
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > f.Shot.Width {
					t.Errorf("line %d is %d cells wide, want at most %d", i, w, f.Shot.Width)
				}
			}
			want, ok := markers[f.Shot.Name]
			if !ok {
				t.Fatalf("no markers for shot %s", f.Shot.Name)
			}
			for _, m := range want {
				if !strings.Contains(plain, m) {
					t.Errorf("frame lacks %q:\n%s", m, plain)
				}
			}
			if hit := ScanText(plain, []string{"/Users", "/tmp", t.TempDir()}); hit != "" {
				t.Errorf("frame holds %q", hit)
			}
			if !strings.Contains(f.Content, "\x1b[") {
				t.Error("frame has no colours")
			}
		})
	}
}

// TestRenderDeterministic: the same shot renders the same frame twice; the
// fixed clock keeps the durations in the status lines at zero.
func TestRenderDeterministic(t *testing.T) {
	a := archives(t)
	for _, s := range Shots {
		f1, err := Render(s, a)
		if err != nil {
			t.Fatal(err)
		}
		f2, err := Render(s, a)
		if err != nil {
			t.Fatal(err)
		}
		if f1.Content != f2.Content {
			t.Errorf("shot %s differs between two renders", s.Name)
		}
	}
}

// TestRenderNeedsArchive: a shot without its archive is an error, not an
// empty frame.
func TestRenderNeedsArchive(t *testing.T) {
	for _, s := range Shots {
		a := Archives{}
		if s.Demo {
			a.Anon = demoStore(t)
		} else {
			a.Demo = demoStore(t)
		}
		if _, err := Render(s, a); err == nil {
			t.Errorf("shot %s renders without its archive", s.Name)
		}
	}
	_ = store.OpenReadOnly
}

// TestDemoNow: the demo clock lies after the last demo conversation, so
// the search shows every hit without a time filter trimming them.
func TestDemoNow(t *testing.T) {
	ctx := context.Background()
	st := demoStore(t)
	hits, err := st.Search(ctx, DemoQuery, store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if !h.Event.TS.Before(demoNow) {
			t.Errorf("hit %s at %s is after the demo clock %s", h.Event.ID, h.Event.TS, demoNow)
		}
	}
}
