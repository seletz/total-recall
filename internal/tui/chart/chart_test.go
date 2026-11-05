package chart

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/seletz/total-recall/internal/project"
	"github.com/seletz/total-recall/internal/store"
	"github.com/seletz/total-recall/internal/testfixture"
	"github.com/seletz/total-recall/internal/testfixture/fixturedb"
)

// series builds an ActivitySeries with one bucket per row of cells; each
// row has one Activity per key, in key order.
func series(period store.Period, keys []string, start time.Time, rows ...[]store.Activity) store.ActivitySeries {
	s := store.ActivitySeries{Keys: keys}
	for i, row := range rows {
		b := store.ActivityBucket{Start: start, Cells: make([]store.ActivityCell, len(keys))}
		for j, k := range keys {
			b.Cells[j] = store.ActivityCell{Key: k, Activity: row[j]}
		}
		b.Label = fmt.Sprintf("bucket %d", i)
		s.Buckets = append(s.Buckets, b)
		switch period {
		case store.PeriodWeek:
			start = start.AddDate(0, 0, 7)
		case store.PeriodMonth:
			start = start.AddDate(0, 1, 0)
		default:
			start = start.AddDate(0, 0, 1)
		}
	}
	return s
}

func act(sessions, human, agent int) store.Activity {
	return store.Activity{Sessions: sessions, Human: human, Agent: agent}
}

func TestMetricString(t *testing.T) {
	tests := []struct {
		m    Metric
		want string
	}{
		{MetricEvents, "events"},
		{MetricHuman, "human"},
		{MetricSessions, "sessions"},
		{Metric(9), "Metric(9)"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("Metric(%d).String() = %q, want %q", int(tt.m), got, tt.want)
		}
	}
}

func TestAxisLabel(t *testing.T) {
	berlin := time.FixedZone("CET", 3600)
	tests := []struct {
		name   string
		start  time.Time
		period store.Period
		want   string
	}{
		{"day", time.Date(2026, 10, 5, 0, 0, 0, 0, berlin), store.PeriodDay, "05"},
		{"day two digits", time.Date(2026, 10, 31, 0, 0, 0, 0, berlin), store.PeriodDay, "31"},
		{"week", time.Date(2026, 10, 5, 0, 0, 0, 0, berlin), store.PeriodWeek, "W41"},
		{"week one", time.Date(2025, 12, 29, 0, 0, 0, 0, berlin), store.PeriodWeek, "W01"},
		{"month", time.Date(2026, 10, 1, 0, 0, 0, 0, berlin), store.PeriodMonth, "Oct"},
		{"month january", time.Date(2026, 1, 1, 0, 0, 0, 0, berlin), store.PeriodMonth, "Jan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AxisLabel(tt.start, tt.period); got != tt.want {
				t.Errorf("AxisLabel(%v, %v) = %q, want %q", tt.start, tt.period, got, tt.want)
			}
		})
	}
}

func TestFromSeries(t *testing.T) {
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	keys := []string{"/w/beta", "/w/alpha", store.OtherKey}
	names := map[string]string{"/w/beta": "beta", "/w/alpha": "alpha", store.OtherKey: "other"}
	three := series(store.PeriodDay, keys, start,
		[]store.Activity{act(2, 3, 10), act(1, 1, 4), act(1, 0, 2)},
		[]store.Activity{act(0, 0, 0), act(1, 2, 5), act(0, 0, 0)},
	)
	many := make([]string, 0, len(Palette)+2)
	for i := 0; i < len(Palette)+1; i++ {
		many = append(many, fmt.Sprintf("/w/p%d", i))
	}
	many = append(many, store.OtherKey)
	manyRows := make([]store.Activity, len(many))
	for i := range manyRows {
		manyRows[i] = act(1, 1, 1)
	}

	tests := []struct {
		name   string
		series store.ActivitySeries
		period store.Period
		metric Metric
		names  map[string]string
		want   Spec
	}{
		{
			name:   "events are human plus agent, series in key order",
			series: three, period: store.PeriodDay, metric: MetricEvents, names: names,
			want: Spec{
				Title:  "events",
				Period: store.PeriodDay,
				Labels: []string{"05", "06"},
				Series: []Series{
					{Key: "/w/beta", Name: "beta", Color: Palette[0], Values: []float64{13, 0}},
					{Key: "/w/alpha", Name: "alpha", Color: Palette[1], Values: []float64{5, 7}},
					{Key: store.OtherKey, Name: "other", Color: OtherColor, Values: []float64{2, 0}},
				},
			},
		},
		{
			name:   "human",
			series: three, period: store.PeriodDay, metric: MetricHuman, names: names,
			want: Spec{
				Title:  "human",
				Period: store.PeriodDay,
				Labels: []string{"05", "06"},
				Series: []Series{
					{Key: "/w/beta", Name: "beta", Color: Palette[0], Values: []float64{3, 0}},
					{Key: "/w/alpha", Name: "alpha", Color: Palette[1], Values: []float64{1, 2}},
					{Key: store.OtherKey, Name: "other", Color: OtherColor, Values: []float64{0, 0}},
				},
			},
		},
		{
			name:   "sessions, missing names fall back to the key",
			series: three, period: store.PeriodDay, metric: MetricSessions, names: nil,
			want: Spec{
				Title:  "sessions",
				Period: store.PeriodDay,
				Labels: []string{"05", "06"},
				Series: []Series{
					{Key: "/w/beta", Name: "/w/beta", Color: Palette[0], Values: []float64{2, 0}},
					{Key: "/w/alpha", Name: "/w/alpha", Color: Palette[1], Values: []float64{1, 1}},
					{Key: store.OtherKey, Name: "other", Color: OtherColor, Values: []float64{1, 0}},
				},
			},
		},
		{
			name:   "week labels",
			series: series(store.PeriodWeek, []string{"/w/a"}, start, []store.Activity{act(1, 1, 1)}, []store.Activity{act(1, 1, 1)}),
			period: store.PeriodWeek,
			metric: MetricEvents,
			want: Spec{
				Title:  "events",
				Period: store.PeriodWeek,
				Labels: []string{"W41", "W42"},
				Series: []Series{{Key: "/w/a", Name: "/w/a", Color: Palette[0], Values: []float64{2, 2}}},
			},
		},
		{
			name:   "month labels",
			series: series(store.PeriodMonth, []string{"/w/a"}, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), []store.Activity{act(1, 1, 1)}, []store.Activity{act(1, 1, 1)}),
			period: store.PeriodMonth,
			metric: MetricEvents,
			want: Spec{
				Title:  "events",
				Period: store.PeriodMonth,
				Labels: []string{"Dec", "Jan"},
				Series: []Series{{Key: "/w/a", Name: "/w/a", Color: Palette[0], Values: []float64{2, 2}}},
			},
		},
		{
			name:   "empty series",
			series: store.ActivitySeries{},
			period: store.PeriodMonth,
			metric: MetricEvents,
			want:   Spec{Title: "events", Period: store.PeriodMonth},
		},
		{
			name:   "keys without buckets",
			series: store.ActivitySeries{Keys: []string{"/w/a"}},
			period: store.PeriodDay,
			metric: MetricEvents,
			want: Spec{
				Title:  "events",
				Period: store.PeriodDay,
				Series: []Series{{Key: "/w/a", Name: "/w/a", Color: Palette[0]}},
			},
		},
		{
			name:   "more series than palette colours cycle, other stays grey",
			series: series(store.PeriodDay, many, start, manyRows),
			period: store.PeriodDay,
			metric: MetricEvents,
			want: func() Spec {
				s := Spec{Title: "events", Period: store.PeriodDay, Labels: []string{"05"}}
				for i, k := range many {
					c := Palette[i%len(Palette)]
					if k == store.OtherKey {
						c = OtherColor
					}
					s.Series = append(s.Series, Series{Key: k, Name: k, Color: c, Values: []float64{2}})
				}
				return s
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromSeries(tt.series, tt.period, tt.metric, tt.names)
			if got.Title != tt.want.Title || got.Period != tt.want.Period {
				t.Errorf("title/period = %q/%v, want %q/%v", got.Title, got.Period, tt.want.Title, tt.want.Period)
			}
			if !equalStrings(got.Labels, tt.want.Labels) {
				t.Errorf("labels = %q, want %q", got.Labels, tt.want.Labels)
			}
			if len(got.Series) != len(tt.want.Series) {
				t.Fatalf("got %d series, want %d", len(got.Series), len(tt.want.Series))
			}
			for i := range got.Series {
				g, w := got.Series[i], tt.want.Series[i]
				if g.Key != w.Key || g.Name != w.Name || !sameColor(g.Color, w.Color) || !equalFloats(g.Values, w.Values) {
					t.Errorf("series %d = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

func TestColorsAssign(t *testing.T) {
	spec := func(keys ...string) Spec {
		s := Spec{Title: "events"}
		for _, k := range keys {
			s.Series = append(s.Series, Series{Key: k, Name: k, Values: []float64{1}})
		}
		return s
	}
	colorsOf := func(s Spec) []color.Color {
		var out []color.Color
		for _, sr := range s.Series {
			out = append(out, sr.Color)
		}
		return out
	}

	var c Colors
	first := c.Assign(spec("/w/alpha", "/w/beta", store.OtherKey))
	if want := []color.Color{Palette[0], Palette[1], OtherColor}; !equalColors(colorsOf(first), want) {
		t.Errorf("first assignment = %v, want %v", colorsOf(first), want)
	}
	// Period switch: beta now leads, gamma is new, alpha third.
	second := c.Assign(spec("/w/beta", "/w/gamma", "/w/alpha", store.OtherKey))
	if want := []color.Color{Palette[1], Palette[2], Palette[0], OtherColor}; !equalColors(colorsOf(second), want) {
		t.Errorf("second assignment = %v, want %v", colorsOf(second), want)
	}
	// The input spec is not modified.
	if in := spec("/w/beta"); in.Series[0].Color != nil {
		t.Error("Assign modified its input")
	}
	// Exhausted palette cycles; other stays grey.
	keys := make([]string, 0, len(Palette)+2)
	for i := 0; i < len(Palette)+1; i++ {
		keys = append(keys, fmt.Sprintf("/w/p%d", i))
	}
	keys = append(keys, store.OtherKey)
	var fresh Colors
	third := fresh.Assign(spec(keys...))
	got := colorsOf(third)
	if !sameColor(got[len(Palette)], Palette[0]) || !sameColor(got[len(keys)-1], OtherColor) {
		t.Errorf("cycled assignment = %v", got)
	}
	// A fresh Colors agrees with FromSeries.
	fs := FromSeries(series(store.PeriodDay, []string{"/w/a", "/w/b", store.OtherKey}, time.Now(), []store.Activity{act(1, 1, 1), act(1, 1, 1), act(1, 1, 1)}), store.PeriodDay, MetricEvents, nil)
	var again Colors
	if !equalColors(colorsOf(again.Assign(fs)), colorsOf(fs)) {
		t.Errorf("fresh Colors = %v, FromSeries = %v", colorsOf(again.Assign(fs)), colorsOf(fs))
	}
}

// dayLabels returns 28 day labels starting 2026-10-05.
func daySpec() Spec {
	start := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	keys := []string{"/w/iris", "/w/webapp", "/w/notizbox", store.OtherKey}
	rows := make([][]store.Activity, 28)
	for d := range rows {
		rows[d] = []store.Activity{
			act(1, 10*d, 100*(d%7)),
			act(1, 5*d, 40*((d+3)%5)),
			act(1, 0, 30*(d%3)),
			act(1, 1, 20),
		}
	}
	names := map[string]string{"/w/iris": "iris", "/w/webapp": "webapp", "/w/notizbox": "notizbox"}
	return FromSeries(series(store.PeriodDay, keys, start, rows...), store.PeriodDay, MetricEvents, names)
}

func TestRenderText(t *testing.T) {
	st, _ := fixturedb.Activity(t)
	weeks, err := st.ActivitySeries(context.Background(), store.ActivityFilter{
		Location: testfixture.ActivityLocation, Period: store.PeriodWeek, Dimension: store.DimProject, TopN: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	months, err := st.ActivitySeries(context.Background(), store.ActivityFilter{
		Location: testfixture.ActivityLocation, Period: store.PeriodMonth, Dimension: store.DimProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		spec Spec
		w, h int
	}{
		{"days", daySpec(), 80, 12},
		{"weeks top2", FromSeries(weeks, store.PeriodWeek, MetricEvents, project.Names(weeks.Keys, nil, "")), 80, 12},
		{"months sessions", FromSeries(months, store.PeriodMonth, MetricSessions, project.Names(months.Keys, nil, "")), 80, 12},
		{"narrow", daySpec(), 40, 8},
		{"too many bars are clipped", daySpec(), 30, 8},
		{"empty", Spec{Title: "events"}, 80, 12},
		{"zero values", FromSeries(store.ActivitySeries{Keys: []string{"/w/a"}, Buckets: []store.ActivityBucket{{Cells: []store.ActivityCell{{Key: "/w/a"}}}}}, store.PeriodDay, MetricEvents, nil), 40, 8},
		{"minimum size", daySpec(), 20, 5},
		{"too small", daySpec(), 10, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := RenderText(tt.spec, tt.w, tt.h)
			plain := ansi.Strip(out)
			lines := strings.Split(plain, "\n")
			if out != "" && len(lines) != tt.h {
				t.Errorf("got %d lines, want %d", len(lines), tt.h)
			}
			for i, l := range lines {
				if ansi.StringWidth(l) > tt.w {
					t.Errorf("line %d is %d wide, want <= %d: %q", i, ansi.StringWidth(l), tt.w, l)
				}
			}
			golden.RequireEqual(t, []byte(plain))
		})
	}
}

// TestRenderTextColors checks that the bars (not only the legend) carry
// every series colour.
func TestRenderTextColors(t *testing.T) {
	out := RenderText(daySpec(), 80, 12)
	bars := out[:strings.LastIndex(out, "\n")]
	for _, s := range daySpec().Series {
		// The SGR parameters lipgloss emits for the colour, without the
		// final "m", so a segment with an added background still matches.
		rendered := lipgloss.NewStyle().Foreground(s.Color).Render("x")
		seq := strings.TrimSuffix(strings.SplitN(rendered, "x", 2)[0], "m")
		if seq == "" || !strings.Contains(bars, seq) {
			t.Errorf("series %s: bars lack its colour sequence %q", s.Name, seq)
		}
	}
}

// holidayFunc adapts a function to store.Holidays.
type holidayFunc func(time.Time) string

func (f holidayFunc) Name(day time.Time) string { return f(day) }

// TestDayMarks checks the marks of a day series: weekends, the named
// holidays, nothing for other periods or without buckets.
func TestDayMarks(t *testing.T) {
	loc := testfixture.ActivityLocation
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, loc) // Thursday
	var buckets []store.ActivityBucket
	for d := range 7 {
		buckets = append(buckets, store.ActivityBucket{Start: start.AddDate(0, 0, d)})
	}
	s := store.ActivitySeries{Buckets: buckets}
	cal := holidayFunc(func(d time.Time) string {
		if d.Day() == 1 {
			return "Neujahr"
		}
		if d.Day() == 6 {
			return "Epiphanias"
		}
		return ""
	})
	want := []Mark{{Holiday: "Neujahr"}, {}, {Weekend: true}, {Weekend: true}, {}, {Holiday: "Epiphanias"}, {}}
	got := DayMarks(s, store.PeriodDay, cal)
	if len(got) != len(want) {
		t.Fatalf("DayMarks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mark %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got := DayMarks(s, store.PeriodDay, nil); got[0].Holiday != "" || !got[2].Weekend {
		t.Errorf("without holidays = %v", got)
	}
	if DayMarks(s, store.PeriodWeek, cal) != nil || DayMarks(store.ActivitySeries{}, store.PeriodDay, cal) != nil {
		t.Error("marks for a week or without buckets")
	}
}

// TestAxisLabelMarks checks that a holiday label is coloured, a weekend
// label faint and the plain text unchanged.
func TestAxisLabelMarks(t *testing.T) {
	labels := []string{"01", "02", "03", "04"}
	marks := []Mark{{Holiday: "Neujahr"}, {}, {Weekend: true}, {Weekend: true}}
	out := axisLabels(labels, marks, 20, 2, 1)
	if plain := ansi.Strip(out); plain != "01 02 03 04" {
		t.Errorf("plain = %q", plain)
	}
	if !strings.Contains(out, holidayLabel.Render("01")) || !strings.Contains(out, weekendLabel.Render("03")) || strings.Contains(out, holidayLabel.Render("02")) {
		t.Errorf("styles missing or misplaced: %q", out)
	}
	if out := axisLabels(labels, nil, 20, 2, 1); out != "01 02 03 04" {
		t.Errorf("without marks = %q", out)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func equalColors(a, b []color.Color) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameColor(a[i], b[i]) {
			return false
		}
	}
	return true
}

func TestColorsFix(t *testing.T) {
	spec := func(keys ...string) Spec {
		s := Spec{Title: "events"}
		for _, k := range keys {
			s.Series = append(s.Series, Series{Key: k, Name: k, Values: []float64{1}})
		}
		return s
	}
	colorsOf := func(s Spec) []color.Color {
		var out []color.Color
		for _, sr := range s.Series {
			out = append(out, sr.Color)
		}
		return out
	}
	var c Colors
	c.Fix("kompass", Palette[0])             // blue is reserved for kompass
	c.Fix("iris", lipgloss.Color("#123456")) // a colour outside the palette
	got := colorsOf(c.Assign(spec("/w/alpha", "kompass", "/w/beta", "iris", store.OtherKey)))
	// Automatic colours skip the reserved blue: alpha orange, beta green.
	want := []color.Color{Palette[1], Palette[0], Palette[2], lipgloss.Color("#123456"), OtherColor}
	if !equalColors(got, want) {
		t.Errorf("assignment = %v, want %v", got, want)
	}
	// A fixed colour survives a later Fix of the same key with a new value.
	c.Fix("kompass", Palette[3])
	if got := colorsOf(c.Assign(spec("kompass"))); !sameColor(got[0], Palette[3]) {
		t.Errorf("refixed = %v", got)
	}
	// When every palette colour is reserved, the automatic ones cycle anyway.
	var all Colors
	for i, p := range Palette {
		all.Fix(fmt.Sprintf("fixed%d", i), p)
	}
	if got := colorsOf(all.Assign(spec("/w/free"))); !sameColor(got[0], Palette[0]) {
		t.Errorf("all reserved: %v", got)
	}
}

func TestPaletteMatchesProject(t *testing.T) {
	if len(Palette) != len(project.Palette) {
		t.Fatalf("chart palette has %d colours, project palette %d", len(Palette), len(project.Palette))
	}
	for i, p := range project.Palette {
		if !sameColor(Palette[i], lipgloss.Color(p.Hex)) {
			t.Errorf("Palette[%d] = %v, project %s = %s", i, Palette[i], p.Name, p.Hex)
		}
	}
}

// TestColorsColor checks the shared colour function: a key keeps its
// colour across calls, a pinned colour wins and the automatic ones skip
// it, other is always grey, and Assign agrees with Color.
func TestColorsColor(t *testing.T) {
	var c Colors
	c.Fix("/w/fixed", lipgloss.Color("#123456"))
	c.Fix("kompass", Palette[0])
	tests := []struct {
		key  string
		want color.Color
	}{
		{"/w/alpha", Palette[1]},
		{"/w/fixed", lipgloss.Color("#123456")},
		{"/w/beta", Palette[2]},
		{"/w/alpha", Palette[1]},
		{store.OtherKey, OtherColor},
		{"kompass", Palette[0]},
	}
	for _, tt := range tests {
		if got := c.Color(tt.key); !sameColor(got, tt.want) {
			t.Errorf("Color(%q) = %v, want %v", tt.key, got, tt.want)
		}
	}
	spec := Spec{Series: []Series{{Key: "/w/fixed"}, {Key: "/w/beta"}, {Key: store.OtherKey}, {Key: "/w/gamma"}}}
	got := c.Assign(spec)
	for i, want := range []color.Color{lipgloss.Color("#123456"), Palette[2], OtherColor, Palette[3]} {
		if !sameColor(got.Series[i].Color, want) {
			t.Errorf("Assign series %d = %v, want %v", i, got.Series[i].Color, want)
		}
	}
}
