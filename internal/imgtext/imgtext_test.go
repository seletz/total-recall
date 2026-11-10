package imgtext

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func solid(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestFit(t *testing.T) {
	tests := []struct {
		name         string
		iw, ih, w, h int
		wantW, wantH int
	}{
		{"wide image fills width", 100, 50, 20, 20, 20, 5},
		{"tall image fills height", 50, 100, 20, 6, 6, 6},
		{"square", 40, 40, 10, 10, 10, 5},
		{"smaller than the room is not enlarged", 4, 4, 20, 20, 4, 2},
		{"one pixel", 1, 1, 20, 20, 1, 1},
		{"no room", 10, 10, 0, 5, 0, 0},
		{"no height", 10, 10, 5, 0, 0, 0},
		{"empty image", 0, 0, 5, 5, 0, 0},
		{"very flat", 1000, 1, 10, 10, 10, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := Fit(tt.iw, tt.ih, tt.w, tt.h)
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("Fit(%d,%d,%d,%d) = %d,%d, want %d,%d", tt.iw, tt.ih, tt.w, tt.h, w, h, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestRenderShades(t *testing.T) {
	tests := []struct {
		name string
		c    color.Color
		dark bool
		want string // the glyph of every cell
	}{
		{"white on dark", color.White, true, "█"},
		{"black on dark", color.Black, true, " "},
		{"white on light", color.White, false, " "},
		{"black on light", color.Black, false, "█"},
		{"mid grey on dark", color.Gray{Y: 128}, true, "▒"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := Render(solid(8, 8, tt.c), 4, 4, tt.dark)
			if len(lines) != 2 {
				t.Fatalf("got %d lines, want 2", len(lines))
			}
			for _, l := range lines {
				if got := ansi.Strip(l); got != strings.Repeat(tt.want, 4) {
					t.Errorf("line = %q, want 4×%q", got, tt.want)
				}
			}
		})
	}
}

func TestRenderColour(t *testing.T) {
	lines := Render(solid(4, 4, color.RGBA{R: 255, A: 255}), 2, 2, true)
	if len(lines) != 1 || !strings.Contains(lines[0], "38;2;255;0;0") {
		t.Errorf("a red cell carries its foreground colour: %q", lines)
	}
}

func TestRenderTwoColours(t *testing.T) {
	// Left half white, right half black: the cells follow.
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			if x < 4 {
				img.Set(x, y, color.White)
			} else {
				img.Set(x, y, color.Black)
			}
		}
	}
	lines := Render(img, 8, 8, true)
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if got := ansi.Strip(lines[0]); got != "████    " {
		t.Errorf("line 0 = %q", got)
	}
}

func TestRenderWidthNeverExceeded(t *testing.T) {
	for _, d := range []struct{ iw, ih, w, h int }{{300, 20, 7, 7}, {20, 300, 7, 7}, {3, 3, 1, 1}, {640, 480, 40, 12}} {
		for _, l := range Render(solid(d.iw, d.ih, color.RGBA{10, 200, 30, 255}), d.w, d.h, true) {
			if ansi.StringWidth(l) > d.w {
				t.Errorf("%+v: line wider than %d: %d", d, d.w, ansi.StringWidth(l))
			}
		}
		if n := len(Render(solid(d.iw, d.ih, color.White), d.w, d.h, true)); n > d.h {
			t.Errorf("%+v: %d lines > %d", d, n, d.h)
		}
	}
}

func TestRenderNothing(t *testing.T) {
	if got := Render(solid(4, 4, color.White), 0, 5, true); got != nil {
		t.Errorf("no width: %q", got)
	}
	if got := Render(nil, 5, 5, true); got != nil {
		t.Errorf("nil image: %q", got)
	}
}

func TestRenderTransparent(t *testing.T) {
	// A transparent pixel counts as the background: nothing is drawn.
	lines := Render(solid(4, 4, color.NRGBA{255, 255, 255, 0}), 2, 2, true)
	if got := ansi.Strip(lines[0]); got != "  " {
		t.Errorf("transparent = %q", got)
	}
}
