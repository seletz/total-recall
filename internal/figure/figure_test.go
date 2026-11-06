package figure

import (
	"image/color"
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
			if got := PeriodName(tt.since, tt.until, berlin, EN); got != tt.wantName {
				t.Errorf("PeriodName = %q, want %q", got, tt.wantName)
			}
			if got := PeriodName(tt.since, tt.until, berlin, ""); got != tt.wantName {
				t.Errorf("PeriodName(empty lang) = %q, want %q", got, tt.wantName)
			}
			if got := PeriodSlug(tt.since, tt.until, berlin); got != tt.wantSlug {
				t.Errorf("PeriodSlug = %q, want %q", got, tt.wantSlug)
			}
		})
	}
}

// TestPeriodGerman: German month names and day.month.year; the slug of
// the file name stays ISO.
func TestPeriodGerman(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, berlin) }
	tests := []struct {
		name         string
		since, until time.Time
		want         string
	}{
		{"month", d(2026, 10, 1), d(2026, 11, 1), "Oktober 2026"},
		{"march", d(2026, 3, 1), d(2026, 4, 1), "März 2026"},
		{"days", d(2026, 10, 5), d(2026, 10, 12), "05.10.2026 – 11.10.2026"},
		{"open end", d(2026, 10, 5), time.Time{}, "05.10.2026 – …"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PeriodName(tt.since, tt.until, berlin, DE); got != tt.want {
				t.Errorf("PeriodName = %q, want %q", got, tt.want)
			}
		})
	}
	if got := PeriodSlug(d(2026, 10, 5), d(2026, 10, 12), berlin); got != "2026-10-05_2026-10-11" {
		t.Errorf("PeriodSlug = %q", got)
	}
}

// TestLang checks the parsing and the per-language formats.
func TestLang(t *testing.T) {
	for _, s := range []string{"en", "de", ""} {
		if l, err := ParseLang(s); err != nil || (s != "" && string(l) != s) || (s == "" && l != EN) {
			t.Errorf("ParseLang(%q) = %q, %v", s, l, err)
		}
	}
	if _, err := ParseLang("fr"); err == nil {
		t.Error("ParseLang(fr) = nil error")
	}
	tests := []struct {
		lang               Lang
		week, hours, other string
		monday             string
	}{
		{EN, "W05", "7.3h", "other", "Mon"},
		{DE, "KW 05", "7,3 h", "Sonstige", "Mo"},
		{"", "W05", "7.3h", "other", "Mon"},
	}
	for _, tt := range tests {
		if got := tt.lang.Week(5); got != tt.week {
			t.Errorf("%q.Week = %q, want %q", tt.lang, got, tt.week)
		}
		if got := tt.lang.hours(7.25); got != tt.hours {
			t.Errorf("%q.hours = %q, want %q", tt.lang, got, tt.hours)
		}
		if got := tt.lang.Other(); got != tt.other {
			t.Errorf("%q.Other = %q, want %q", tt.lang, got, tt.other)
		}
		if got := tt.lang.Weekday(0); got != tt.monday {
			t.Errorf("%q.Weekday(0) = %q, want %q", tt.lang, got, tt.monday)
		}
	}
	if got := DE.hours(40); got != "40 h" {
		t.Errorf("DE.hours(40) = %q", got)
	}
}

func TestAssignColors(t *testing.T) {
	got := AssignColors([]string{"a", "b", "c", "d"}, map[string]string{"b": "#4e79a7", "d": "#123456"}, nil)
	want := map[string]string{"a": "#f28e2b", "b": "#4e79a7", "c": "#59a14f", "d": "#123456"}
	for n, w := range want {
		if g := canvas.HexString(got[n]); g != w {
			t.Errorf("%s = %s, want %s", n, g, w)
		}
	}
	// More names than colours: the palette cycles.
	names := []string{"a", "b", "c", "d", "e", "f", "g"}
	small := []color.NRGBA{canvas.Hex("#111111"), canvas.Hex("#222222")}
	got = AssignColors(names, nil, small)
	if canvas.HexString(got["c"]) != canvas.HexString(got["a"]) {
		t.Errorf("c = %s, want the colour of a (%s)", canvas.HexString(got["c"]), canvas.HexString(got["a"]))
	}
	// A configured palette replaces the default; a fixed colour still wins.
	got = AssignColors([]string{"a", "b"}, map[string]string{"a": "#222222"}, small)
	if canvas.HexString(got["a"]) != "#222222" || canvas.HexString(got["b"]) != "#111111" {
		t.Errorf("fixed colour: a = %s, b = %s", canvas.HexString(got["a"]), canvas.HexString(got["b"]))
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
	if got := EN.hours(7.25); got != "7.3h" {
		t.Errorf("hours = %q", got)
	}
	if got := EN.hours(40); got != "40h" {
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
	c := AssignColors([]string{"kompass", "wetterwarte", "~/notizen", "other"}, nil, nil)
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
	c := AssignColors([]string{"kompass", "privat"}, map[string]string{"privat": "#b07aa1"}, nil)
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

// sampleWorkday is a week: an ordinary day within and around the core
// hours, one past midnight, a day without activity, a public holiday that
// was worked and a quiet weekend.
func sampleWorkday() WorkdayData {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, berlin) }
	h := func(hours, mins int) float64 { return float64(hours-4) + float64(mins)/60 }
	core := func(r WorkdayRow) WorkdayRow {
		r.HasCore, r.CoreStart, r.CoreEnd, r.Target = true, h(9, 0), h(17, 0), 8
		return r
	}
	return WorkdayData{
		DayStart: 4, Midnight: 20,
		Span: 31*time.Hour + 5*time.Minute, Target: 32 * time.Hour, Balance: -55 * time.Minute,
		Active: 4, Workdays: 4, DayTarget: 8 * time.Hour, HasTarget: true,
		Rows: []WorkdayRow{
			core(WorkdayRow{Day: day(5), Active: true, First: h(8, 12), Last: h(17, 40), FirstText: "08:12", LastText: "17:40",
				Span: 9*time.Hour + 28*time.Minute, Diff: time.Hour + 28*time.Minute, HasDiff: true}),
			core(WorkdayRow{Day: day(6), Active: true, First: h(9, 30), Last: h(25, 20), FirstText: "09:30", LastText: "01:20 (+1)",
				Span: 15*time.Hour + 50*time.Minute, Diff: 7*time.Hour + 50*time.Minute, HasDiff: true}),
			core(WorkdayRow{Day: day(7)}),
			{Day: day(8), Active: true, First: h(10, 0), Last: h(15, 0), FirstText: "10:00", LastText: "15:00", Span: 5 * time.Hour,
				Diff: -3 * time.Hour, HasDiff: true, HasCore: true, CoreStart: h(9, 0), CoreEnd: h(17, 0), Target: 8},
			{Day: day(9), Holiday: "Feiertag"},
			{Day: day(10), Weekend: true},
			{Day: day(11), Weekend: true, Active: true, First: h(11, 0), Last: h(12, 30), FirstText: "11:00", LastText: "12:30",
				Span: 90 * time.Minute, Diff: 90 * time.Minute, HasDiff: true},
		},
	}
}

// TestGolden compares the SVG of every figure with synthetic data against
// golden files (go test ./internal/figure -update), in both languages.
func TestGolden(t *testing.T) {
	period := Options{Period: "October 2026"}
	de := Options{Period: "Oktober 2026", Lang: DE}
	tests := []struct {
		name  string
		scene canvas.Scene
	}{
		{"Calendar", Calendar(sampleCalendar(), nil, period)},
		{"CalendarBare", Calendar(sampleCalendar(), nil, Options{NoTitle: true, NoLegend: true})},
		{"CalendarEmpty", Calendar(CalendarData{First: sampleCalendar().First, Last: sampleCalendar().Last}, nil, period)},
		{"Projects", Projects(sampleProjects(), period)},
		{"ProjectsBare", Projects(sampleProjects(), Options{NoTitle: true, NoLegend: true})},
		{"ProjectsEmpty", Projects(nil, period)},
		{"Punchcard", Punchcard(samplePunchcard(), period)},
		{"PunchcardBare", Punchcard(samplePunchcard(), Options{NoTitle: true, NoLegend: true})},
		{"Worktime", Worktime(sampleWorktime(), period)},
		{"WorktimeBare", Worktime(sampleWorktime(), Options{NoTitle: true, NoLegend: true})},
		{"WorktimeEmpty", Worktime(WorktimeData{}, period)},
		{"CalendarDE", Calendar(sampleCalendar(), nil, de)},
		{"ProjectsDE", Projects(sampleProjects(), de)},
		{"PunchcardDE", Punchcard(samplePunchcard(), de)},
		{"WorktimeDE", Worktime(sampleWorktimeDE(), de)},
		{"Workday", Workday(sampleWorkday(), period)},
		{"WorkdayBare", Workday(sampleWorkday(), Options{NoTitle: true, NoLegend: true})},
		{"WorkdayDE", Workday(sampleWorkday(), de)},
		{"WorkdayNoTarget", Workday(WorkdayData{DayStart: 0, Midnight: 24, Rows: sampleWorkday().Rows[:1], Span: time.Hour, Active: 1}, period)},
		{"WorkdayEmpty", Workday(WorkdayData{DayStart: 4, Midnight: 20}, period)},
		{"CalendarTitle", Calendar(sampleCalendar(), nil, Options{Period: "October 2026", Title: "Kompass im Oktober"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			golden.RequireEqual(t, canvas.SVG(tt.scene, canvas.Size{}))
		})
	}
}

// sampleWorktimeDE is sampleWorktime with German week labels.
func sampleWorktimeDE() WorktimeData {
	d := sampleWorktime()
	for i := range d.Weeks {
		d.Weeks[i].Label = DE.Week(40 + i)
	}
	return d
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
		{"punchcard", Punchcard(PunchcardData{}, Options{Period: "x"}), 760, 270, 20},
		{"workday", Workday(WorkdayData{DayStart: 4, Midnight: 20, Rows: make([]WorkdayRow, 31)}, Options{Period: "x"}), 720, 590, 30},
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

// TestPNGPixels rasterizes the calendar on a white background and samples
// a cell, the empty colour and the background.
func TestPNGPixels(t *testing.T) {
	white := canvas.Hex("#ffffff")
	th := DefaultTheme()
	th.Background = white
	scene := Calendar(sampleCalendar(), nil, Options{Period: "October 2026", Theme: th})
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
	if got := canvas.At(img, 2*(28+36+2), 2*(36+2)); got != th.Orange[3] {
		t.Errorf("Monday W41 = %v, want %v", got, th.Orange[3])
	}
	if got := canvas.At(img, 2*(28+2), 2*(36+2)); got != white {
		t.Errorf("Monday W40 = %v, want background", got)
	}
	if got := canvas.At(img, 2*(28+2), 2*(36+3*36+2)); got != th.Orange[1] {
		t.Errorf("Thursday W40 = %v, want %v", got, th.Orange[1])
	}
	// The empty colour is translucent grey: darker than white, not black.
	if got := canvas.At(img, 2*(28+2), 2*(36+5*36+2)); got.R > 240 || got.R < 200 || got.R != got.G || got.G != got.B {
		t.Errorf("Saturday W40 = %v, want a light grey", got)
	}
	if got := canvas.At(img, 2, 2); got != white {
		t.Errorf("corner = %v, want background", got)
	}
}

// TestTransparentByDefault: without a theme the SVG has no background
// rectangle and the PNG a transparent corner; a Background colour adds
// both.
func TestTransparentByDefault(t *testing.T) {
	scene := Calendar(sampleCalendar(), nil, Options{Period: "October 2026"})
	if svg := string(canvas.SVG(scene, canvas.Size{})); strings.Contains(svg, `width="100%"`) {
		t.Error("the default SVG has a background rectangle")
	}
	img, err := canvas.Image(scene, canvas.Size{})
	if err != nil {
		t.Fatal(err)
	}
	if a := canvas.At(img, 1, 1).A; a != 0 {
		t.Errorf("corner alpha = %d, want 0", a)
	}
	th := DefaultTheme()
	th.Background = canvas.Hex("#101010")
	scene = Calendar(sampleCalendar(), nil, Options{Theme: th})
	if svg := string(canvas.SVG(scene, canvas.Size{})); !strings.Contains(svg, `width="100%" height="100%" fill="#101010"`) {
		t.Error("a Background colour must be drawn")
	}
}

// TestTheme: the zero theme takes the defaults for everything unset and
// the configured text colour reaches the text.
func TestTheme(t *testing.T) {
	th := Theme{Text: canvas.Hex("#123456")}.resolved()
	if len(th.Orange) != 4 || len(th.Blue) != 4 || len(th.Palette) == 0 || th.Text != canvas.Hex("#123456") {
		t.Errorf("resolved = %+v", th)
	}
	svg := string(canvas.SVG(Punchcard(samplePunchcard(), Options{Period: "x", Theme: Theme{Text: canvas.Hex("#123456")}}), canvas.Size{}))
	if !strings.Contains(svg, `fill="#123456"`) || strings.Contains(svg, DefaultText) {
		t.Error("the text colour of the theme is not used throughout")
	}
	def := string(canvas.SVG(Punchcard(samplePunchcard(), Options{Period: "x"}), canvas.Size{}))
	if !strings.Contains(def, `fill="`+DefaultText+`"`) {
		t.Error("the default text colour is missing")
	}
}
