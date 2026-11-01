package chart

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/go-analyze/charts"

	"github.com/seletz/total-recall/internal/store"
)

// threeSeries is a small stacked spec with distinct, non-zero segments in
// every bar, so each series colour covers some pixels.
func threeSeries() Spec {
	return Spec{
		Title:  "events",
		Period: store.PeriodWeek,
		Labels: []string{"W40", "W41", "W42", "W43", "W44", "W45"},
		Series: []Series{
			{Key: "alpha", Name: "alpha", Color: Palette[0], Values: []float64{12, 30, 25, 40, 18, 22}},
			{Key: "beta", Name: "beta", Color: Palette[1], Values: []float64{8, 14, 20, 10, 25, 16}},
			{Key: store.OtherKey, Name: "other", Color: OtherColor, Values: []float64{5, 9, 7, 11, 6, 8}},
		},
	}
}

func TestPixelSize(t *testing.T) {
	tests := []struct {
		name       string
		cols, rows int
		cell       CellSize
		wantW      int
		wantH      int
	}{
		{"default cell", 80, 24, DefaultCellSize, 800, 480},
		{"zero cell falls back", 80, 24, CellSize{}, 800, 480},
		{"partial cell falls back per side", 10, 10, CellSize{Width: 7}, 70, 200},
		{"retina cell", 40, 10, CellSize{Width: 18, Height: 40}, 720, 400},
		{"no cells", 0, 10, DefaultCellSize, 0, 200},
		{"negative cells", -3, -1, DefaultCellSize, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := PixelSize(tt.cols, tt.rows, tt.cell)
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("PixelSize(%d, %d, %+v) = %d×%d, want %d×%d", tt.cols, tt.rows, tt.cell, w, h, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestCellSizeFontSize(t *testing.T) {
	tests := []struct {
		name string
		cell CellSize
		want float64
	}{
		{"default cell gives the library default", DefaultCellSize, 12},
		{"zero cell falls back", CellSize{}, 12},
		{"retina cell scales up", CellSize{Width: 18, Height: 40}, 24},
		{"tiny cell is clamped", CellSize{Width: 5, Height: 8}, minFontSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cell.FontSize(); got != tt.want {
				t.Errorf("%+v.FontSize() = %v, want %v", tt.cell, got, tt.want)
			}
		})
	}
}

func TestToBarChartOption(t *testing.T) {
	spec := threeSeries()
	opt := ToBarChartOption(spec)

	if opt.StackSeries == nil || !*opt.StackSeries {
		t.Error("StackSeries not set")
	}
	if opt.Title.Text != "events" {
		t.Errorf("Title.Text = %q, want %q", opt.Title.Text, "events")
	}
	if got := opt.CategoryAxis.Labels; !equalStrings(got, spec.Labels) {
		t.Errorf("CategoryAxis.Labels = %v, want %v", got, spec.Labels)
	}
	if len(opt.SeriesList) != len(spec.Series) {
		t.Fatalf("len(SeriesList) = %d, want %d", len(opt.SeriesList), len(spec.Series))
	}
	wantNames := []string{"alpha", "beta", "other"}
	if !equalStrings(opt.Legend.SeriesNames, wantNames) {
		t.Errorf("Legend.SeriesNames = %v, want %v", opt.Legend.SeriesNames, wantNames)
	}
	for i, s := range opt.SeriesList {
		if s.Name != spec.Series[i].Name {
			t.Errorf("SeriesList[%d].Name = %q, want %q", i, s.Name, spec.Series[i].Name)
		}
		if !equalFloats(s.Values, spec.Series[i].Values) {
			t.Errorf("SeriesList[%d].Values = %v, want %v", i, s.Values, spec.Series[i].Values)
		}
	}
	if opt.Theme == nil {
		t.Fatal("Theme is nil; chartpicture would fall back to its own palette")
	}
	for i, s := range spec.Series {
		if got, want := opt.Theme.GetSeriesColor(i), toChartColor(s.Color); got != want {
			t.Errorf("Theme.GetSeriesColor(%d) = %v, want %v", i, got, want)
		}
	}
	if !opt.Theme.IsDark() {
		t.Error("Theme.IsDark() = false, want dark-terminal text colours")
	}
	if bg := opt.Theme.GetBackgroundColor(); !bg.IsTransparent() {
		t.Errorf("Theme background = %v, want transparent", bg)
	}
	if opt.Legend.FontStyle.FontSize != DefaultCellSize.FontSize() {
		t.Errorf("Legend font size = %v, want %v", opt.Legend.FontStyle.FontSize, DefaultCellSize.FontSize())
	}
	if opt.ValueFormatter == nil {
		t.Fatal("ValueFormatter is nil")
	}
	for v, want := range map[float64]string{70: "70", 0: "0", 0.75: "0.75", 1200: "1200"} {
		if got := opt.ValueFormatter(v); got != want {
			t.Errorf("ValueFormatter(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestToBarChartOptionAt(t *testing.T) {
	cell := CellSize{Width: 18, Height: 40}
	opt := ToBarChartOptionAt(threeSeries(), cell)
	want := cell.FontSize()
	for name, got := range map[string]float64{
		"title":    opt.Title.FontStyle.FontSize,
		"legend":   opt.Legend.FontStyle.FontSize,
		"category": opt.CategoryAxis.LabelFontStyle.FontSize,
		"value":    opt.ValueAxis[0].LabelFontStyle.FontSize,
	} {
		if got != want {
			t.Errorf("%s font size = %v, want %v", name, got, want)
		}
	}
}

func TestToBarChartOptionEmpty(t *testing.T) {
	opt := ToBarChartOption(Spec{Title: "sessions"})
	if len(opt.SeriesList) != 0 {
		t.Errorf("len(SeriesList) = %d, want 0", len(opt.SeriesList))
	}
	if opt.Theme == nil {
		t.Error("Theme is nil")
	}
	if opt.Title.Text != "sessions" {
		t.Errorf("Title.Text = %q, want %q", opt.Title.Text, "sessions")
	}
}

func TestRenderPNG(t *testing.T) {
	tests := []struct {
		name   string
		spec   Spec
		w, h   int
		colors []color.Color // every one must appear in the picture
	}{
		{
			name:   "three series",
			spec:   threeSeries(),
			w:      600,
			h:      300,
			colors: []color.Color{Palette[0], Palette[1], OtherColor},
		},
		{
			name: "single series, two bars",
			spec: Spec{
				Title:  "sessions",
				Period: store.PeriodDay,
				Labels: []string{"01", "02"},
				Series: []Series{{Key: "k", Name: "k", Color: Palette[3], Values: []float64{3, 1}}},
			},
			w:      400,
			h:      200,
			colors: []color.Color{Palette[3]},
		},
		{
			name: "series with no values draws the rest",
			spec: Spec{
				Title:  "events",
				Labels: []string{"Oct", "Nov"},
				Series: []Series{
					{Key: "a", Name: "a", Color: Palette[2], Values: []float64{5, 7}},
					{Key: "b", Name: "b", Color: Palette[4]},
				},
			},
			w:      400,
			h:      200,
			colors: []color.Color{Palette[2]},
		},
		{
			name:   "retina sized",
			spec:   threeSeries(),
			w:      1440,
			h:      800,
			colors: []color.Color{Palette[0], Palette[1], OtherColor},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := RenderPNG(tt.spec, tt.w, tt.h)
			if err != nil {
				t.Fatalf("RenderPNG: %v", err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if b := img.Bounds(); b.Dx() != tt.w || b.Dy() != tt.h {
				t.Errorf("size = %d×%d, want %d×%d", b.Dx(), b.Dy(), tt.w, tt.h)
			}
			if opaque(img) == 0 {
				t.Fatal("picture is empty")
			}
			if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
				t.Errorf("corner pixel alpha = %d, want 0 (transparent background)", a)
			}
			for _, c := range tt.colors {
				if !hasColor(img, c) {
					t.Errorf("colour %v not in picture", toChartColor(c))
				}
			}
		})
	}
}

func TestRenderPNGEmptySpec(t *testing.T) {
	data, err := RenderPNG(Spec{Title: "events"}, 300, 150)
	if err != nil {
		t.Fatalf("RenderPNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 300 || b.Dy() != 150 {
		t.Errorf("size = %d×%d, want 300×150", b.Dx(), b.Dy())
	}
}

func TestRenderPNGBadSize(t *testing.T) {
	tests := []struct {
		name string
		w, h int
	}{
		{"zero width", 0, 100},
		{"zero height", 100, 0},
		{"negative", -1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := RenderPNG(threeSeries(), tt.w, tt.h); err == nil {
				t.Errorf("RenderPNG(%d, %d) = nil error, want error", tt.w, tt.h)
			}
		})
	}
}

func TestToChartColor(t *testing.T) {
	tests := []struct {
		name string
		in   color.Color
		want charts.Color
	}{
		{"palette blue", Palette[0], charts.Color{R: 0x4e, G: 0x79, B: 0xa7, A: 255}},
		{"other grey", OtherColor, charts.Color{R: 0x8c, G: 0x8c, B: 0x8c, A: 255}},
		{"nrgba with alpha", color.NRGBA{R: 10, G: 20, B: 30, A: 128}, charts.Color{R: 10, G: 20, B: 30, A: 128}},
		{"nil is transparent", nil, charts.ColorTransparent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toChartColor(tt.in); got != tt.want {
				t.Errorf("toChartColor(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// opaque counts the pixels with a non-zero alpha.
func opaque(img image.Image) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				n++
			}
		}
	}
	return n
}

// hasColor reports whether some pixel matches c within a small tolerance,
// which allows for the renderer's rounding but not for anti-aliased edges.
func hasColor(img image.Image, c color.Color) bool {
	want := color.NRGBAModel.Convert(c).(color.NRGBA)
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if got.A == 255 && near(got.R, want.R) && near(got.G, want.G) && near(got.B, want.B) {
				return true
			}
		}
	}
	return false
}

func near(a, b uint8) bool {
	d := int(a) - int(b)
	return d >= -2 && d <= 2
}
