package screenshot

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// centre returns the colour at the centre of a cell.
func centre(img *image.RGBA, col, row int) color.RGBA {
	x := padX + col*cellW + cellW/2
	y := titleBar + row*cellH + cellH/2
	return img.RGBAAt(x, y)
}

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) < 24 && d(a.G, b.G) < 24 && d(a.B, b.B) < 24
}

// TestImageCells: the image has one cell per terminal cell plus the window
// chrome; a background colour fills its cell, a full block and a
// horizontal rule are drawn in the foreground, an empty cell shows the
// window background.
func TestImageCells(t *testing.T) {
	f := Frame{Shot: Shot{Width: 4, Height: 2}, Content: "\x1b[48;2;200;0;0mab\x1b[0m\n─\x1b[38;2;0;200;0m█\x1b[0m"}
	img, err := Image(f)
	if err != nil {
		t.Fatal(err)
	}
	if w, h := img.Bounds().Dx(), img.Bounds().Dy(); w != 4*cellW+2*padX || h != 2*cellH+titleBar+padY {
		t.Errorf("image is %dx%d, want %dx%d", w, h, 4*cellW+2*padX, 2*cellH+titleBar+padY)
	}
	// The background of "a" fills the cell up to its corner.
	if c := img.RGBAAt(padX+1, titleBar+1); !near(c, color.RGBA{200, 0, 0, 255}) {
		t.Errorf("corner of a red cell = %v", c)
	}
	if c := centre(img, 1, 1); !near(c, color.RGBA{0, 200, 0, 255}) {
		t.Errorf("full block = %v, want green", c)
	}
	if c := centre(img, 0, 1); !near(c, foreground) {
		t.Errorf("rule centre = %v, want the foreground %v", c, foreground)
	}
	if c := centre(img, 3, 0); !near(c, background) {
		t.Errorf("empty cell = %v, want the background %v", c, background)
	}
	// Outside the rounded corner the image is transparent.
	if c := img.RGBAAt(0, 0); c.A != 0 {
		t.Errorf("corner pixel = %v, want transparent", c)
	}
}

// TestImageShades: the shade blocks blend the foreground over the
// background in three steps.
func TestImageShades(t *testing.T) {
	f := Frame{Shot: Shot{Width: 3, Height: 1}, Content: "\x1b[38;2;255;255;255m░▒▓\x1b[0m"}
	img, err := Image(f)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for col := range 3 {
		c := centre(img, col, 0)
		if int(c.R) <= last {
			t.Errorf("shade %d = %v is not lighter than the one before", col, c)
		}
		last = int(c.R)
	}
}

// TestImageWideFrame renders a real frame and encodes it as PNG.
func TestImageWideFrame(t *testing.T) {
	f, err := Render(Shots[0], env(t))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := PNG(&buf, f); err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != Width*cellW+2*padX {
		t.Errorf("png width = %d, want %d", cfg.Width, Width*cellW+2*padX)
	}
}

// TestImageHalfLines: the half-length box-drawing glyphs of the compact
// heatmap axis and the rounded corners of the help overlay are drawn as
// shapes, not as glyphs of the font. A half line covers the centre and
// its own half of the cell and leaves the other half empty; a rounded
// corner meets the neighbouring cells at their midpoints and leaves the
// far corner empty.
func TestImageHalfLines(t *testing.T) {
	cases := []struct {
		ch         rune
		drawn      [][2]int // points in the cell, in eighths of its size (x, y)
		empty      [][2]int
		descriptor string
	}{
		{'╵', [][2]int{{4, 1}, {4, 4}}, [][2]int{{4, 7}, {1, 4}}, "up"},
		{'╷', [][2]int{{4, 7}, {4, 4}}, [][2]int{{4, 1}, {1, 4}}, "down"},
		{'╴', [][2]int{{1, 4}, {4, 4}}, [][2]int{{7, 4}, {4, 1}}, "left"},
		{'╶', [][2]int{{7, 4}, {4, 4}}, [][2]int{{1, 4}, {4, 1}}, "right"},
		{'╭', [][2]int{{7, 4}, {4, 7}}, [][2]int{{1, 1}, {4, 4}, {1, 4}, {4, 1}}, "rounded top-left"},
		{'╮', [][2]int{{1, 4}, {4, 7}}, [][2]int{{7, 1}, {4, 4}, {7, 4}, {4, 1}}, "rounded top-right"},
		{'╰', [][2]int{{7, 4}, {4, 1}}, [][2]int{{1, 7}, {4, 4}, {1, 4}, {4, 7}}, "rounded bottom-left"},
		{'╯', [][2]int{{1, 4}, {4, 1}}, [][2]int{{7, 7}, {4, 4}, {7, 4}, {4, 7}}, "rounded bottom-right"},
	}
	for _, c := range cases {
		t.Run(c.descriptor, func(t *testing.T) {
			f := Frame{Shot: Shot{Width: 1, Height: 1}, Content: string(c.ch)}
			img, err := Image(f)
			if err != nil {
				t.Fatal(err)
			}
			at := func(p [2]int) color.RGBA {
				return img.RGBAAt(padX+p[0]*cellW/8, titleBar+p[1]*cellH/8)
			}
			for _, p := range c.drawn {
				if got := at(p); !near(got, foreground) {
					t.Errorf("%q at %v = %v, want the foreground", c.ch, p, got)
				}
			}
			for _, p := range c.empty {
				if got := at(p); !near(got, background) {
					t.Errorf("%q at %v = %v, want the background", c.ch, p, got)
				}
			}
		})
	}
}

// TestImageQuadrants: the quadrant blocks and the small square of the
// project bars are drawn as shapes: a quadrant block fills two opposite
// corners of its cell and leaves the other two, the small square fills the
// centre and leaves the corners.
func TestImageQuadrants(t *testing.T) {
	f := Frame{Shot: Shot{Width: 3, Height: 1}, Content: "\x1b[38;2;255;255;255m▞▚▪\x1b[0m"}
	img, err := Image(f)
	if err != nil {
		t.Fatal(err)
	}
	white := color.RGBA{255, 255, 255, 255}
	at := func(col int, dx, dy int) color.RGBA {
		return img.RGBAAt(padX+col*cellW+dx, titleBar+dy)
	}
	q := cellW / 4
	tests := []struct {
		name   string
		col    int
		dx, dy int
		lit    bool
	}{
		{"▞ upper right", 0, 3 * q, q, true},
		{"▞ lower left", 0, q, cellH - q, true},
		{"▞ upper left", 0, q, q, false},
		{"▞ lower right", 0, 3 * q, cellH - q, false},
		{"▚ upper left", 1, q, q, true},
		{"▚ lower right", 1, 3 * q, cellH - q, true},
		{"▚ upper right", 1, 3 * q, q, false},
		{"▪ centre", 2, cellW / 2, cellH / 2, true},
		{"▪ corner", 2, 1, 1, false},
	}
	for _, tt := range tests {
		c := at(tt.col, tt.dx, tt.dy)
		if lit := near(c, white); lit != tt.lit {
			t.Errorf("%s = %v, lit %v, want %v", tt.name, c, lit, tt.lit)
		}
	}
}
