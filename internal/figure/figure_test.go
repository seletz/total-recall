package figure

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/canvas"
)

var berlin = mustLoc("Europe/Berlin")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

func TestLevel(t *testing.T) {
	tests := []struct {
		v, mx, n, want int
	}{
		{0, 10, 4, 0},
		{3, 0, 4, 0},
		{1, 10, 4, 1},
		{2, 10, 4, 1},
		{3, 10, 4, 2},
		{5, 10, 4, 2},
		{6, 10, 4, 3},
		{8, 10, 4, 3},
		{9, 10, 4, 4},
		{10, 10, 4, 4},
		{1, 1, 4, 3},
		{7, 7, 2, 2},
		{1, 7, 2, 1},
		{5, 5, 0, 0},
	}
	for _, tt := range tests {
		if got := Level(tt.v, tt.mx, tt.n); got != tt.want {
			t.Errorf("Level(%d, %d, %d) = %d, want %d", tt.v, tt.mx, tt.n, got, tt.want)
		}
	}
}

func TestPeriod(t *testing.T) {
	d := func(y int, m time.Month, day, h int) time.Time { return time.Date(y, m, day, h, 0, 0, 0, berlin) }
	tests := []struct {
		name         string
		since, until time.Time
		wantName     string
		wantSlug     string
	}{
		{"month", d(2026, 10, 1, 0), d(2026, 11, 1, 0), "October 2026", "2026-10"},
		{"month at day start", d(2026, 10, 1, 4), d(2026, 11, 1, 4), "October 2026", "2026-10"},
		{"december", d(2026, 12, 1, 0), d(2027, 1, 1, 0), "December 2026", "2026-12"},
		{"two months", d(2026, 10, 1, 0), d(2026, 12, 1, 0), "2026-10-01 – 2026-11-30", "2026-10-01_2026-11-30"},
		{"days", d(2026, 10, 5, 0), d(2026, 10, 12, 0), "2026-10-05 – 2026-10-11", "2026-10-05_2026-10-11"},
		{"open end", d(2026, 10, 5, 0), time.Time{}, "2026-10-05 – …", "2026-10-05_…"},
		{"open start", time.Time{}, d(2026, 10, 12, 0), "… – 2026-10-11", "…_2026-10-11"},
		{"utc month", time.Date(2026, 9, 30, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC), "October 2026", "2026-10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PeriodName(tt.since, tt.until, berlin); got != tt.wantName {
				t.Errorf("PeriodName = %q, want %q", got, tt.wantName)
			}
			if got := PeriodSlug(tt.since, tt.until, berlin); got != tt.wantSlug {
				t.Errorf("PeriodSlug = %q, want %q", got, tt.wantSlug)
			}
		})
	}
}

func TestAssignColors(t *testing.T) {
	got := AssignColors([]string{"a", "b", "c", "d"}, map[string]string{"b": "#4e79a7", "d": "#123456"})
	want := map[string]string{"a": "#f28e2b", "b": "#4e79a7", "c": "#59a14f", "d": "#123456"}
	for n, w := range want {
		if g := canvas.HexString(got[n]); g != w {
			t.Errorf("%s = %s, want %s", n, g, w)
		}
	}
	// More names than colours: the palette cycles.
	names := []string{"a", "b", "c", "d", "e", "f", "g"}
	got = AssignColors(names, nil)
	if canvas.HexString(got["g"]) != canvas.HexString(got["a"]) {
		t.Errorf("g = %s, want the colour of a (%s)", canvas.HexString(got["g"]), canvas.HexString(got["a"]))
	}
}

func TestColors(t *testing.T) {
	if _, err := Colors([]string{"blue", "#ABCDEF"}); err != nil {
		t.Errorf("Colors: %v", err)
	}
	for _, bad := range [][]string{{"pink"}, {""}, {"#abc"}} {
		if _, err := Colors(bad); err == nil {
			t.Errorf("Colors(%q) = nil error", bad)
		}
	}
}

func TestFormat(t *testing.T) {
	for _, s := range []string{"svg", "png"} {
		if f, err := ParseFormat(s); err != nil || string(f) != s {
			t.Errorf("ParseFormat(%q) = %q, %v", s, f, err)
		}
	}
	if _, err := ParseFormat("jpg"); err == nil {
		t.Error("ParseFormat(jpg) = nil error")
	}
	s := canvas.Scene{Width: 10, Height: 10}
	if b, err := Encode(s, PNG, canvas.Size{}); err != nil || !strings.HasPrefix(string(b), "\x89PNG") {
		t.Errorf("Encode png: %v", err)
	}
	if b, err := Encode(s, SVG, canvas.Size{}); err != nil || !strings.HasPrefix(string(b), "<svg") {
		t.Errorf("Encode svg: %v", err)
	}
	if _, err := Encode(s, "gif", canvas.Size{}); err == nil {
		t.Error("Encode gif = nil error")
	}
}

func TestHelpers(t *testing.T) {
	if got := cut("abcdefgh", 5); got != "abcd…" {
		t.Errorf("cut = %q", got)
	}
	if got := cut("äöü", 3); got != "äöü" {
		t.Errorf("cut = %q", got)
	}
	if got := percent(1, 3); got != "33 %" {
		t.Errorf("percent = %q", got)
	}
	if got := percent(1, 0); got != "0 %" {
		t.Errorf("percent = %q", got)
	}
	if got := hours(7.25); got != "7.3h" {
		t.Errorf("hours = %q", got)
	}
	if got := hours(40); got != "40h" {
		t.Errorf("hours = %q", got)
	}
}

// sampleCalendar is a short month with a weekend gap.
func sampleCalendar() CalendarData {
	return CalendarData{
		First: time.Date(2026, 10, 1, 0, 0, 0, 0, berlin),
		Last:  time.Date(2026, 10, 14, 0, 0, 0, 0, berlin),
		Values: map[string]int{
			"2026-10-01": 12, "2026-10-02": 3, "2026-10-05": 40, "2026-10-06": 25,
			"2026-10-07": 1, "2026-10-08": 18, "2026-10-09": 9, "2026-10-13": 30,
		},
	}
}

func sampleProjects() []ProjectRow {
	c := AssignColors([]string{"kompass", "wetterwarte", "~/notizen", "other"}, nil)
	return []ProjectRow{
		{"kompass", 120, 3000, c["kompass"]},
		{"wetterwarte", 80, 900, c["wetterwarte"]},
		{"~/notizen", 40, 100, c["~/notizen"]},
		{"other", 10, 50, c["other"]},
	}
}

func samplePunchcard() PunchcardData {
	var d PunchcardData
	d.Human[0][9], d.Agent[0][9] = 12, 300
	d.Human[0][10], d.Agent[0][10] = 20, 450
	d.Human[2][14], d.Agent[2][14] = 5, 60
	d.Human[4][23], d.Agent[5][0] = 1, 20
	d.Agent[6][3] = 8
	return d
}

func sampleWorktime() WorktimeData {
	c := AssignColors([]string{"kompass", "privat"}, map[string]string{"privat": "#b07aa1"})
	return WorktimeData{
		Categories: []WorktimeCategory{{"kompass", c["kompass"]}, {"privat", c["privat"]}},
		Weeks: []WorktimeWeek{
			{Label: "W40", Hours: map[string]float64{"kompass": 30.5, "privat": 4}, Target: 32},
			{Label: "W41", Hours: map[string]float64{"kompass": 41.25, "privat": 2.5}, Target: 40},
			{Label: "W42", Hours: map[string]float64{}, Target: 40},
			{Label: "W43", Hours: map[string]float64{"privat": 6}, Target: 0},
		},
	}
}

// TestGolden compares the SVG of every figure with synthetic data against
// golden files (go test ./internal/figure -update).
func TestGolden(t *testing.T) {
	period := Options{Period: "October 2026"}
	tests := []struct {
		name  string
		scene canvas.Scene
	}{
		{"Calendar", Calendar(sampleCalendar(), nil, period)},
		{"CalendarBare", Calendar(sampleCalendar(), nil, Options{NoTitle: true, NoLegend: true, Transparent: true})},
		{"CalendarEmpty", Calendar(CalendarData{First: sampleCalendar().First, Last: sampleCalendar().Last}, nil, period)},
		{"Projects", Projects(sampleProjects(), period)},
		{"ProjectsBare", Projects(sampleProjects(), Options{NoTitle: true, NoLegend: true})},
		{"ProjectsEmpty", Projects(nil, period)},
		{"Punchcard", Punchcard(samplePunchcard(), period)},
		{"PunchcardBare", Punchcard(samplePunchcard(), Options{NoTitle: true, NoLegend: true})},
		{"Worktime", Worktime(sampleWorktime(), period)},
		{"WorktimeBare", Worktime(sampleWorktime(), Options{NoTitle: true, NoLegend: true})},
		{"WorktimeEmpty", Worktime(WorktimeData{}, period)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			golden.RequireEqual(t, canvas.SVG(tt.scene, canvas.Size{}))
		})
	}
}

// TestSizes checks the natural sizes against the figures of a report.
func TestSizes(t *testing.T) {
	tests := []struct {
		name            string
		scene           canvas.Scene
		w, h, tolerance float64
	}{
		{"calendar", Calendar(CalendarData{First: time.Date(2026, 10, 1, 0, 0, 0, 0, berlin), Last: time.Date(2026, 10, 31, 0, 0, 0, 0, berlin)}, nil, Options{Period: "x"}), 360, 290, 20},
		{"projects", Projects(make([]ProjectRow, 12), Options{Period: "x"}), 610, 370, 20},
		{"punchcard", Punchcard(PunchcardData{}, Options{Period: "x"}), 760, 290, 20},
		{"worktime", Worktime(WorktimeData{Weeks: make([]WorktimeWeek, 5)}, Options{Period: "x"}), 560, 260, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if d := tt.scene.Width - tt.w; d > tt.tolerance || d < -tt.tolerance {
				t.Errorf("width = %g, want about %g", tt.scene.Width, tt.w)
			}
			if d := tt.scene.Height - tt.h; d > tt.tolerance || d < -tt.tolerance {
				t.Errorf("height = %g, want about %g", tt.scene.Height, tt.h)
			}
		})
	}
}

// TestPNGPixels rasterizes the calendar and samples a cell, the empty
// colour and the background.
func TestPNGPixels(t *testing.T) {
	scene := Calendar(sampleCalendar(), nil, Options{Period: "October 2026"})
	img, err := canvas.Image(scene, canvas.Size{Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != int(scene.Width*2) || b.Dy() != int(scene.Height*2) {
		t.Errorf("bounds = %v for %g×%g at scale 2", b, scene.Width, scene.Height)
	}
	// The second week column, Monday row: 2026-10-05 has the maximum, the
	// darkest colour; Thursday of the first column is 2026-10-01 (12 of
	// 40, level 2), Saturday 2026-10-03 is empty and the Monday of the
	// first column lies before the range.
	darkest := canvas.Hex(DefaultHeatmap[3])
	if got := canvas.At(img, 2*(28+36+2), 2*(34+2)); got != darkest {
		t.Errorf("Monday W41 = %v, want %v", got, darkest)
	}
	if got := canvas.At(img, 2*(28+2), 2*(34+2)); got != Background {
		t.Errorf("Monday W40 = %v, want background", got)
	}
	if got := canvas.At(img, 2*(28+2), 2*(34+3*36+2)); got != canvas.Hex(DefaultHeatmap[1]) {
		t.Errorf("Thursday W40 = %v, want %v", got, canvas.Hex(DefaultHeatmap[1]))
	}
	if got := canvas.At(img, 2*(28+2), 2*(34+5*36+2)); got != EmptyColor {
		t.Errorf("Saturday W40 = %v, want empty", got)
	}
	if got := canvas.At(img, 2, 2); got != Background {
		t.Errorf("corner = %v, want background", got)
	}
}
