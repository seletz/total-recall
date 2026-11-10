package cli

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/demo"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// chartCategories are the categories and colours of the chart tests and
// of the figures in the docs (tools/charts).
const chartCategories = `
[[worktime.category]]
name = "kompass"
match = ["https://git.invalid/mara/kompass"]
color = "blue"

[[worktime.category]]
name = "privat"
match = ["~/dotfiles", "~/notizen"]
color = "purple"
`

// chartEnv is a test environment on the demo archive with the demo clock.
func chartEnv(t *testing.T) (*testEnv, string) {
	t.Helper()
	_, db := fixturedb.Demo(t)
	e := newTestEnv(t)
	e.now = demo.Now
	e.home = demo.Home
	appendConfig(t, e, chartCategories)
	return e, db
}

// TestChartGolden compares the SVG of every figure for October of the
// demo archive with golden files (go test ./internal/cli -run TestChartGolden -update).
func TestChartGolden(t *testing.T) {
	t.Parallel()
	e, db := chartEnv(t)
	tests := []struct {
		name string
		args []string
	}{
		{"Calendar", []string{"calendar", "--month", "2026-10"}},
		{"Projects", []string{"projects", "--month", "2026-10"}},
		{"ProjectsTop3", []string{"projects", "--month", "2026-10", "--top", "3"}},
		{"Punchcard", []string{"punchcard", "--month", "2026-10"}},
		{"Worktime", []string{"worktime", "--month", "2026-10"}},
		{"WorktimeLastMonth", []string{"worktime", "--range", "last-month"}},
		{"Workday", []string{"workday", "--month", "2026-10"}},
		{"WorkdayDE", []string{"workday", "--month", "2026-10", "--lang", "de"}},
		{"WorkdayHoliday", []string{"workday", "--month", "2026-06"}},
		{"WorkdayWeek", []string{"workday", "--range", "last-week"}},
		{"WorkdayEmpty", []string{"workday", "--month", "2026-03"}},
		{"CalendarDark", []string{"calendar", "--month", "2026-10", "--background", "#1e1e1e"}},
		{"CalendarBare", []string{"calendar", "--month", "2026-10", "--no-title", "--no-legend", "--transparent"}},
		{"CalendarWeek", []string{"calendar", "--since", "2026-10-05", "--until", "2026-10-11"}},
		{"CalendarScaled", []string{"calendar", "--month", "2026-10", "--scale", "2"}},
		{"PunchcardBox", []string{"punchcard", "--month", "2026-10", "--width", "800", "--height", "400"}},
		{"WorktimeEmpty", []string{"worktime", "--month", "2026-03"}},
		{"ProjectsEmpty", []string{"projects", "--month", "2026-03"}},
		{"CalendarDE", []string{"calendar", "--month", "2026-10", "--lang", "de"}},
		{"ProjectsDE", []string{"projects", "--month", "2026-10", "--lang", "de", "--top", "3"}},
		{"PunchcardDE", []string{"punchcard", "--month", "2026-10", "--lang", "de"}},
		{"WorktimeDE", []string{"worktime", "--month", "2026-10", "--lang", "de"}},
		{"CalendarWeekDE", []string{"calendar", "--since", "2026-10-05", "--until", "2026-10-11", "--lang", "de"}},
		{"WorktimeTitle", []string{"worktime", "--month", "2026-10", "--title", "Kompass im Oktober"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := e.run(t, "", append([]string{"--db", db, "chart"}, tt.args...)...)
			if r.code != 0 {
				t.Fatalf("exit %d: %s", r.code, r.stderr)
			}
			golden.RequireEqual(t, []byte(r.stdout))
		})
	}
}

// TestChartConfig: [chart] sets the defaults, flags override them.
func TestChartConfig(t *testing.T) {
	t.Parallel()
	e, db := chartEnv(t)
	appendConfig(t, e, "\n[chart]\ntitle = false\nlegend = false\nheatmap = [\"green\", \"#08519c\"]\ntop = 2\n")
	r := e.run(t, "", "--db", db, "chart", "calendar", "--month", "2026-10")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if strings.Contains(r.stdout, "Own messages") || strings.Contains(r.stdout, "total ") {
		t.Error("title or legend drawn although switched off")
	}
	if !strings.Contains(r.stdout, "#59a14f") || strings.Contains(r.stdout, "#ee9a5f") {
		t.Error("configured heatmap not used")
	}
	r = e.run(t, "", "--db", db, "chart", "projects", "--month", "2026-10")
	if n := strings.Count(r.stdout, "<text x=\"162\""); n != 3 {
		t.Errorf("rows = %d, want 3 (top 2 plus other)", n)
	}
	r = e.run(t, "", "--db", db, "chart", "projects", "--month", "2026-10", "--top", "0")
	if n := strings.Count(r.stdout, "<text x=\"162\""); n < 5 {
		t.Errorf("rows = %d, want every project with --top 0", n)
	}
}

// TestChartLang: [chart] lang sets the language, --lang overrides it and
// the file names stay the same.
func TestChartLang(t *testing.T) {
	t.Parallel()
	e, db := chartEnv(t)
	appendConfig(t, e, "\n[chart]\nlang = \"de\"\n")
	r := e.run(t, "", "--db", db, "chart", "worktime", "--month", "2026-10")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	for _, want := range []string{"Arbeitszeit je Woche · Oktober 2026", ">KW 41<", ">Soll<", ",5 h<"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("German chart lacks %q", want)
		}
	}
	r = e.run(t, "", "--db", db, "chart", "worktime", "--month", "2026-10", "--lang", "en")
	if !strings.Contains(r.stdout, "Working time per week · October 2026") || strings.Contains(r.stdout, "KW ") {
		t.Error("--lang en does not override [chart] lang")
	}
	r = e.run(t, "", "--db", db, "chart", "projects", "--month", "2026-10", "--top", "2")
	if !strings.Contains(r.stdout, ">Sonstige</text>") {
		t.Error("folded projects are not named in German")
	}
	out := t.TempDir()
	r = e.run(t, "", "--db", db, "chart", "calendar", "--month", "2026-10", "--out", out)
	if r.code != 0 || strings.TrimSpace(r.stdout) != filepath.Join(out, "2026-10-calendar.svg") {
		t.Errorf("file name with lang = de: %q (exit %d)", r.stdout, r.code)
	}
}

// TestChartFiles writes all figures as PNG into a directory and checks
// names and sizes.
func TestChartFiles(t *testing.T) {
	t.Parallel()
	e, db := chartEnv(t)
	out := filepath.Join(t.TempDir(), "charts")
	r := e.run(t, "", "--db", db, "chart", "all", "--month", "2026-10", "--format", "png", "--scale", "1", "--out", out)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	want := []string{"2026-10-calendar.png", "2026-10-projects.png", "2026-10-punchcard.png", "2026-10-worktime.png", "2026-10-workday.png"}
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if len(lines) != len(want) {
		t.Fatalf("stdout = %q", r.stdout)
	}
	for i, name := range want {
		if lines[i] != filepath.Join(out, name) {
			t.Errorf("line %d = %q, want %q", i, lines[i], filepath.Join(out, name))
		}
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if bd := img.Bounds(); bd.Dx() < 300 || bd.Dy() < 150 {
			t.Errorf("%s: %v, want at least 300×150 at scale 1", name, bd)
		}
	}
	// A week range names the files by its days; --width sets the pixel width.
	r = e.run(t, "", "--db", db, "chart", "calendar", "--since", "2026-10-05", "--until", "2026-10-11", "--format", "png", "--width", "720", "--out", out)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	b, err := os.ReadFile(filepath.Join(out, "2026-10-05_2026-10-11-calendar.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 720 {
		t.Errorf("width = %d, want 720", img.Bounds().Dx())
	}
	// Last month relative to the demo clock (2026-11-02) is October.
	r = e.run(t, "", "--db", db, "chart", "punchcard", "--range", "last-month", "--out", out)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "2026-10-punchcard.svg")); err != nil {
		t.Error(err)
	}
}

// TestChartErrors checks the flag validation.
func TestChartErrors(t *testing.T) {
	t.Parallel()
	e, db := chartEnv(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown figure", []string{"pie"}, "unknown figure"},
		{"no figure", nil, "accepts 1 arg"},
		{"all without out", []string{"all"}, "--out"},
		{"bad format", []string{"calendar", "--format", "jpg"}, "invalid format"},
		{"range and month", []string{"calendar", "--range", "last-month", "--month", "2026-10"}, "either --range"},
		{"bad range", []string{"calendar", "--range", "yesterday"}, "invalid --range"},
		{"bad month", []string{"calendar", "--month", "2026-13"}, "invalid --month"},
		{"negative width", []string{"calendar", "--width", "-1"}, "--width"},
		{"zero scale", []string{"calendar", "--scale", "0"}, "--scale"},
		{"negative top", []string{"projects", "--top", "-2"}, "--top"},
		{"bad lang", []string{"calendar", "--lang", "fr"}, "invalid language"},
		{"bad config", []string{"calendar", "--month", "2026-10"}, "chart.format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "bad config" {
				appendConfig(t, e, "\n[chart]\nformat = \"gif\"\n")
			}
			r := e.run(t, "", append([]string{"--db", db, "chart"}, tt.args...)...)
			if r.code == 0 {
				t.Fatalf("exit 0, want an error; stdout: %s", r.stdout)
			}
			if !strings.Contains(r.stderr, tt.want) {
				t.Errorf("stderr = %q, want containing %q", r.stderr, tt.want)
			}
		})
	}
}

// TestChartProjectColors: a [[project]] definition fixes the colour of its
// project in the project chart and merges its repositories.
func TestChartProjectColors(t *testing.T) {
	t.Parallel()
	e, db := chartEnv(t)
	appendConfig(t, e, "\n[[project]]\nname = \"Kompass\"\ncolor = \"red\"\nmatch = [\"https://git.invalid/mara/kompass\"]\n")
	r := e.run(t, "", "--db", db, "chart", "projects", "--month", "2026-10")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, ">Kompass</text>") {
		t.Error("configured project name missing")
	}
	if !strings.Contains(r.stdout, "fill=\"#e15759\"") {
		t.Error("configured colour missing")
	}
	if strings.Contains(r.stdout, ">kompass-gpx</text>") || strings.Contains(r.stdout, ">kompass</text>") {
		t.Error("repositories of the definition not merged")
	}
}
