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
	fs := frames(t)
	var buf bytes.Buffer
	if err := PNG(&buf, fs[0]); err != nil {
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
