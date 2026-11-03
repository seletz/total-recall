package screenshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// TestMain builds the demo archive once for every test of the package,
// unless TOTAL_RECALL_DEMO_DB already names one.
func TestMain(m *testing.M) {
	os.Exit(fixturedb.DemoMain(m))
}

// frames renders every shot against the demo archive.
func frames(t *testing.T) []Frame {
	t.Helper()
	st, _ := fixturedb.Demo(t)
	out, err := RenderAll(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(Shots) {
		t.Fatalf("%d frames, want %d", len(out), len(Shots))
	}
	return out
}

// TestRenderAll renders every shot: the frame has the shot's size and shows
// the markers of its view, and no frame holds a path of the machine.
func TestRenderAll(t *testing.T) {
	markers := map[string][]string{
		"search":          {"[1 search]", "> goreleaser cask", "hits (0 ms)", "Release v0.3.0", "mara/kompass", "◄"},
		"session":         {"› session", "Release v0.3.0", "· tools", "tool_use Read", "tool_use Bash", "tool (tool_result)", "goreleaser", "mara/kompass"},
		"stats":           {"[2 stats]", "period: [day] week month", "by: [project]", "kompass", "wetterwarte", "text chart · 28 days · 0 ms"},
		"heatmap":         {"[2 stats]", "0 ms · heatmap", "Mon", "Sun"},
		"activity-week":   {"[3 activity]", "period: day [week] month", "week:", "Thu 2026-06-04", "Fronleichnam", "00:45 (+1)", "count: all [human]"},
		"activity-month":  {"[3 activity]", "period: day week [month]", "month:", "2026-06", "Fronleichnam"},
		"compact":         {"[3 activity]", "week:"},
		"help":            {"[2 stats]", "keys: stats", "closes this help", "esc back"},
		"heatmap-compact": {"[2 stats]", "░▒▓█", "heatmap"},
	}
	for _, f := range frames(t) {
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
			if hit := ScanText(plain, []string{"/Users", "/tmp", os.TempDir(), t.TempDir()}); hit != "" {
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
	st, _ := fixturedb.Demo(t)
	for _, s := range Shots {
		f1, err := Render(s, st)
		if err != nil {
			t.Fatal(err)
		}
		f2, err := Render(s, st)
		if err != nil {
			t.Fatal(err)
		}
		if f1.Content != f2.Content {
			t.Errorf("shot %s differs between two renders", s.Name)
		}
	}
}

// TestRenderNeedsArchive: a shot without an archive is an error, not an
// empty frame.
func TestRenderNeedsArchive(t *testing.T) {
	for _, s := range Shots {
		if _, err := Render(s, nil); err == nil {
			t.Errorf("shot %s renders without an archive", s.Name)
		}
	}
}

// TestShotClocks: every shot's clock lies after the activity it shows, so
// nothing is cut off by a time filter.
func TestShotClocks(t *testing.T) {
	ctx := context.Background()
	st, _ := fixturedb.Demo(t)
	hits, err := st.Search(ctx, demo.Query, store.SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range Shots {
		if s.Query == "" {
			continue
		}
		for _, h := range hits {
			if !h.Event.TS.Before(s.Now) {
				t.Errorf("shot %s: hit %s at %s is after its clock %s", s.Name, h.Event.ID, h.Event.TS, s.Now)
			}
		}
	}
	for _, s := range Shots {
		if s.Now.Before(demo.First) || s.Now.After(demo.Now) {
			t.Errorf("shot %s: clock %s outside the demo archive", s.Name, s.Now)
		}
	}
}

// TestFramesGolden compares the text frame of every shot with the golden
// file written by `mise run docs:screenshots` next to the images. A
// difference means the TUI changed and the images in the docs are stale.
func TestFramesGolden(t *testing.T) {
	const hint = "run `mise run docs:screenshots` and commit the images"
	for _, f := range frames(t) {
		t.Run(f.Shot.Name, func(t *testing.T) {
			p := GoldenPath(GoldenDir, f.Shot.Name)
			want, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("no golden for shot %s (%v); %s", f.Shot.Name, err, hint)
			}
			if got := f.Plain() + "\n"; got != string(want) {
				t.Errorf("shot %s differs from %s; %s\n%s", f.Shot.Name, p, hint, diff(string(want), got))
			}
		})
	}
	entries, err := os.ReadDir(GoldenDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".txt")
		if !shotExists(name) {
			t.Errorf("stale golden %s: no such shot; %s", filepath.Join(GoldenDir, e.Name()), hint)
		}
	}
}

func shotExists(name string) bool {
	for _, s := range Shots {
		if s.Name == name {
			return true
		}
	}
	return false
}

// diff lists the first differing lines of two frames.
func diff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	shown := 0
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, c string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			c = g[i]
		}
		if a == c {
			continue
		}
		fmt.Fprintf(&b, "line %d\n  golden: %q\n  frame:  %q\n", i+1, a, c)
		if shown++; shown == 5 {
			b.WriteString("  …\n")
			break
		}
	}
	return b.String()
}

// TestWriteGoldens: the goldens are the plain frames, one file per shot,
// and files of vanished shots are removed.
func TestWriteGoldens(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := frames(t)
	if err := WriteGoldens(dir, fs); err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		b, err := os.ReadFile(GoldenPath(dir, f.Shot.Name))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != f.Plain()+"\n" {
			t.Errorf("golden of %s is not its plain frame", f.Shot.Name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.txt")); !os.IsNotExist(err) {
		t.Error("golden of a vanished shot survived")
	}
}
